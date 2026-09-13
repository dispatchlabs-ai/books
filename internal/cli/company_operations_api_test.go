package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompanyOperationAdapters(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	path := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", path)
	t.Setenv("BOOKS_ACTOR", "company-ops-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	app, err := application.Open(context.Background(), path, "acme", "company-ops-test", storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	for _, op := range operations.CompanyOperations() {
		_, err := op.Invoke(context.Background(), app, operations.CompanyAccess("outsider", "other", []string{"read", "post", "import", "manage"}), op.NewInput())
		e, ok := apperr.As(err)
		if !ok || e.Code != "COMPANY_NOT_FOUND" {
			t.Fatalf("%s scope bypass %v", op.Descriptor().ID, err)
		}
		for _, missing := range strings.Split(op.Descriptor().Grant, "+") {
			grants := []string{}
			for _, grant := range []string{"read", "post", "import", "manage"} {
				if grant != missing {
					grants = append(grants, grant)
				}
			}
			_, err := op.Invoke(context.Background(), app, operations.CompanyAccess("limited", "acme", grants), op.NewInput())
			e, ok := apperr.As(err)
			if !ok || e.Code != "COMPANY_NOT_FOUND" {
				t.Fatalf("%s missing %s bypass %v", op.Descriptor().ID, missing, err)
			}
		}
	}
	for _, id := range []string{"spend", "receive", "transfer", "journal_add", "account_add"} {
		op, _ := operations.LookupCompanyOperation(id)
		_, err := op.Invoke(context.Background(), app, operations.CompanyAccess("workflow", "acme", []string{"read", "post", "manage"}), op.NewInput())
		e, ok := apperr.As(err)
		want := "IDEMPOTENCY_KEY_INVALID"
		if id == "account_add" {
			want = "ACCOUNT_INPUT_REQUIRED"
		}
		if !ok || e.Code != want {
			t.Fatalf("%s retry guard: %v", id, err)
		}
	}
	token := strings.Repeat("w", 32)
	sum := sha256.Sum256([]byte(token))
	server, err := httpapi.New(context.Background(), path, httpapi.Config{ArtifactDirectory: filepath.Join(home, "artifacts"), Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "workflow", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": {"read", "post", "manage", "import"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request := httptest.NewRequest("POST", "/v1/companies/acme/operations/receive", strings.NewReader(`{"amount":"35.00","account":"Revenue","description":"Shared workflow","date":"2026-01-15","key":"shared-operation"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var httpResult map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &httpResult); err != nil {
		t.Fatal(err)
	}
	mserver, err := mcpserver.New(context.Background(), mcpserver.Policy{ArtifactDirectory: filepath.Join(home, "artifacts"), Schema: "books.mcp-policy/v1", Actor: "workflow", ConfigPath: path, Companies: map[string][]string{"acme": {"read", "post", "manage", "import"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mserver.Close() }()
	left, right := mcp.NewInMemoryTransports()
	ss, err := mserver.MCP.Connect(context.Background(), left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "company-conformance", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	retry, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "books_company_receive", Arguments: map[string]any{"company": "acme", "input": map[string]any{"amount": "35.00", "account": "Revenue", "description": "Shared workflow", "date": "2026-01-15", "key": "shared-operation"}}})
	if err != nil || retry.IsError {
		t.Fatalf("retry %v %+v", err, retry)
	}
	want := httpResult["data"].(map[string]any)
	got := retry.StructuredContent.(map[string]any)["result"].(map[string]any)
	if want["number"] == nil || want["number"] != got["number"] {
		t.Fatal("HTTP/MCP retry created a different journal")
	}
	report, _ := executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
	if report["data"].(map[string]any)["total_debit_cents"] != "35.00" {
		t.Fatal("retry duplicated posting")
	}
	// Transfer an exact maximum-size source in bounded HTTP chunks, then retain it
	// through MCP. This tests transport capacity; parser validity is separate.
	source := bytes.Repeat([]byte("x"), 8<<20)
	sourceSum := sha256.Sum256(source)
	httpCall := func(id string, input any) map[string]any {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/v1/companies/acme/operations/"+id, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != 200 {
			t.Fatal(id, res.Code, res.Body.String())
		}
		var result map[string]any
		if err = json.Unmarshal(res.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result["data"].(map[string]any)
	}
	ref := httpCall("artifact_begin", map[string]any{"key": "maximum-upload", "name": "source.ofx", "size": strconv.Itoa(len(source)), "sha256": hex.EncodeToString(sourceSum[:])})
	for offset := 0; offset < len(source); offset += artifact.MaxChunk {
		httpCall("artifact_write", map[string]any{"id": ref["id"], "offset": strconv.Itoa(offset), "base64": base64.StdEncoding.EncodeToString(source[offset:min(offset+artifact.MaxChunk, len(source))])})
	}
	httpCall("artifact_finish", map[string]any{"id": ref["id"]})
	uploaded, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "books_company_bank_import_upload", Arguments: map[string]any{"company": "acme", "input": map[string]any{"artifact": ref["id"], "key": "maximum-import", "name": "source.ofx"}}})
	if err != nil || uploaded.IsError {
		t.Fatalf("maximum MCP upload: %v %+v", err, uploaded)
	}
	job := uploaded.StructuredContent.(map[string]any)["result"].(map[string]any)
	retained, err := app.Source(context.Background(), job["id"].(string))
	if err != nil || !bytes.Equal(retained, source) {
		t.Fatal("maximum upload retention", err)
	}

	large, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "books_company_import_source_read", Arguments: map[string]any{"company": "acme", "input": map[string]any{"id": job["id"]}}})
	if err != nil || large.IsError {
		t.Fatalf("large result %v %+v", err, large)
	}
	resultRef := large.StructuredContent.(map[string]any)["artifact"].(map[string]any)
	var resultBytes []byte
	for offset := 0; ; {
		chunk, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "books_company_artifact_read", Arguments: map[string]any{"company": "acme", "input": map[string]any{"id": resultRef["id"], "offset": strconv.Itoa(offset)}}})
		if err != nil || chunk.IsError {
			t.Fatalf("result chunk %v %+v", err, chunk)
		}
		value := chunk.StructuredContent.(map[string]any)["result"].(map[string]any)
		part, err := base64.StdEncoding.DecodeString(value["base64"].(string))
		if err != nil {
			t.Fatal(err)
		}
		resultBytes = append(resultBytes, part...)
		offset += len(part)
		if value["eof"] == true {
			break
		}
	}
	var restored struct {
		Result struct {
			Base64 string `json:"base64"`
		} `json:"result"`
	}
	if err = json.Unmarshal(resultBytes, &restored); err != nil {
		t.Fatal(err)
	}
	restoredSource, err := base64.StdEncoding.DecodeString(restored.Result.Base64)
	if err != nil || !bytes.Equal(restoredSource, source) {
		t.Fatal("large result download mismatch", err)
	}

}
