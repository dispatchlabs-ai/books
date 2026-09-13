package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"path/filepath"
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
	server, err := httpapi.New(context.Background(), path, httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "workflow", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": {"read", "post", "manage", "import"}}}}})
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
	mserver, err := mcpserver.New(context.Background(), mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "workflow", ConfigPath: path, Companies: map[string][]string{"acme": {"read", "post", "manage", "import"}}})
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
}
