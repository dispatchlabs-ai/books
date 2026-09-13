package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"strings"
	"testing"
)

func TestRegistryFreshAgentJourney(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", path)
	t.Setenv("BOOKS_ACTOR", "registry-test")
	t.Setenv("BOOKS_DB", "")
	ctx := context.Background()
	root := filepath.Join(home, "artifacts")
	grants := map[string][]string{"*": {"read", "import", "post", "manage"}}
	policy := mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "owner", ConfigPath: path, ArtifactDirectory: root, Registry: []string{"read", "manage"}, Companies: grants}
	ms, err := mcpserver.New(ctx, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ms.Close() }()
	left, right := mcp.NewInMemoryTransports()
	ss, err := ms.MCP.Connect(ctx, left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "registry-test", Version: "1"}, nil).Connect(ctx, right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	call := func(name string, args any) any {
		t.Helper()
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || out.IsError {
			t.Fatalf("%s: %v %+v", name, err, out)
		}
		return out.StructuredContent.(map[string]any)["result"]
	}
	token := strings.Repeat("r", 32)
	digest := sha256.Sum256([]byte(token))
	server, err := httpapi.New(ctx, path, httpapi.Config{Schema: "books.server/v4", Listen: "127.0.0.1:0", ArtifactDirectory: root, Principals: []httpapi.Principal{{ID: "owner", TokenSHA256: hex.EncodeToString(digest[:]), Registry: []string{"read", "manage"}, Companies: grants}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	httpCall := func(url string, input any) map[string]any {
		t.Helper()
		data, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", url, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(url, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// Caller-owned maps cannot alter either server's frozen authority.
	grants["*"] = []string{"read"}
	add := map[string]any{"input": map[string]any{"initialize": true, "options": map[string]any{"key": "example", "name": "Example Company", "start": "2026-01-01"}}}
	call("books_registry_company_add", add)
	call("books_company_account_add", map[string]any{"company": "example", "input": map[string]any{"kind": "bank", "name": "Checking", "code": "1000", "active_from": "2026-01-01"}})
	// Existing operation contract supplies the same default-account flags as CLI.
	httpCall("/v1/admin/registry/operations/config_set", map[string]any{"company": "example", "key": "defaults.deposit-account", "value": "1000"})
	call("books_company_receive", map[string]any{"company": "example", "input": map[string]any{"amount": "12.34", "account": "Revenue", "date": "2026-01-15", "key": "first-receipt"}})
	app, err := application.Open(ctx, path, "example", "owner", storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	scope := artifact.Bind(artifact.WithRoot(ctx, root), "owner", "company:"+app.Identity())
	_ = app.Close()
	// Large JSON files cross both adapters without raising the inline frame limit.
	large := append([]byte(strings.Repeat(" ", 2<<20)), []byte(`{"amount":"12.34","account":"Revenue","date":"2026-01-15","key":"first-receipt"}`)...)
	ref, err := artifact.Put(scope, "receipt.json", large)
	if err != nil {
		t.Fatal(err)
	}
	call("books_company_receive", map[string]any{"company": "example", "input": map[string]any{"input_artifact": ref.ID}})
	httpCall("/v1/companies/example/operations/receive", map[string]any{"input_artifact": ref.ID})
	httpCall("/v1/admin/registry/operations/company_add", map[string]any{"options": map[string]any{"key": "second", "name": "Second Company", "start": "2026-01-01"}})
	call("books_registry_company_default", map[string]any{"input": map[string]any{"company": "second", "dry_run": true}})
	call("books_registry_company_default", map[string]any{"input": map[string]any{"company": "second"}})
	companies := call("books_registry_company_list", map[string]any{"input": map[string]any{}}).([]any)
	if len(companies) != 2 {
		t.Fatal(companies)
	}
	for _, key := range []string{"", "output", "default-company", "defaults"} {
		call("books_registry_config_get", map[string]any{"input": map[string]any{"key": key, "company": "example"}})
	}
	call("books_registry_config_path", map[string]any{"input": map[string]any{}})
	call("books_registry_config_set", map[string]any{"input": map[string]any{"key": "output", "value": "json"}})
	call("books_health", map[string]any{})
	call("books_capabilities", map[string]any{})
	httpCall("/v1/companies/second/operations/report_trial_balance", map[string]any{"as_of": "2026-01-31"})
	tb, _ := executeHumanJSON(t, "--company", "example", "tb", "--as-of", "2026-01-31")
	if tb["data"].(map[string]any)["total_debit_cents"] != "12.34" {
		t.Fatal(tb)
	}
	// Authorization is enforced by the shared backend, independently of discovery.
	registry := application.NewRegistry(path)
	for _, op := range operations.RegistryOperations() {
		for _, a := range []operations.RegistryAccess{{}, operations.ScopedRegistryAccess("", []string{"read", "manage"})} {
			_, err := op.Execute(ctx, registry, a, op.NewInput())
			e, ok := apperr.As(err)
			if !ok || e.Code != "REGISTRY_NOT_AVAILABLE" {
				t.Fatal(op.Descriptor().ID, err)
			}
		}
		if op.Descriptor().Effect == "write" {
			_, err := op.Execute(ctx, registry, operations.ScopedRegistryAccess("reader", []string{"read"}), op.NewInput())
			if err == nil {
				t.Fatal("registry write allowed for reader")
			}
		}
	}
}
