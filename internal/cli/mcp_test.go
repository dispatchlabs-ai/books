package cli

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPStdio(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	path := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", path)
	t.Setenv("BOOKS_ACTOR", "mcp-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := cfg.Resolve(path, "acme")
	if err != nil {
		t.Fatal(err)
	}
	policy := mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "mcp-test", Databases: map[string]mcpserver.DatabasePolicy{"example": {Path: resolved.Database, UUID: resolved.Company.DatabaseUUID, Grants: []string{"read", "manage"}}}}
	data, _ := json.Marshal(policy)
	policyPath := filepath.Join(home, "mcp.json")
	if err := os.WriteFile(policyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "books")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/books")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "books-conformance-client", Version: "1"}, nil)
	ambient := filepath.Join(home, "ambient.toml")
	if err := os.WriteFile(ambient, []byte("invalid = ["), 0600); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binary, "mcp", "--policy", policyPath)
	process.Env = append(os.Environ(), "BOOKS_CONFIG="+ambient)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: process}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(operations.DatabaseOperations())+2 {
		t.Fatalf("tools %d", len(tools.Tools))
	}
	call := func(name string, input any) *mcp.CallToolResult {
		t.Helper()
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "books_db_" + name, Arguments: map[string]any{"database": "example", "input": input}})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError {
			t.Fatalf("%s: %+v", name, r.Content)
		}
		return r
	}
	created := call("journal_create", map[string]any{"book": "ACME", "posting_date": "2026-01-15", "period": "2026-01", "description": "MCP synthetic sale", "lines": []any{map[string]any{"account": "1000", "debit_cents": "9007199254740993"}, map[string]any{"account": "4000", "credit_cents": "9007199254740993"}}})
	value := created.StructuredContent.(map[string]any)["result"].(map[string]any)
	id := value["id"].(string)
	call("journal_validate", map[string]any{"id": id})
	call("journal_post", map[string]any{"id": id})
	report := call("report_trial_balance", map[string]any{"entity": "ACME", "as_of": "2026-01-31"})
	if report.StructuredContent.(map[string]any)["result"].(map[string]any)["total_debit_cents"] != "9007199254740993" {
		t.Fatal("MCP precision loss")
	}
	cli, _ := executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
	if cli["data"].(map[string]any)["total_debit_cents"] != "90071992547409.93" {
		t.Fatal("CLI/MCP mismatch")
	}

	largeMutation := call("journal_create", map[string]any{"book": "ACME", "posting_date": "2026-01-15", "period": "2026-01", "description": strings.Repeat("x", 600<<10), "lines": []any{map[string]any{"account": "1000", "debit_cents": "1"}, map[string]any{"account": "4000", "credit_cents": "1"}}})
	delivered := largeMutation.StructuredContent.(map[string]any)
	if delivered["delivery_warning"] == nil || delivered["result"].(map[string]any)["id"] == "" {
		t.Fatal("large committed draft lost on artifact delivery failure")
	}
	denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "books_db_entity_list", Arguments: map[string]any{"database": "other", "input": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !denied.IsError {
		t.Fatal("foreign handle accepted")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	hugeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	huge := exec.CommandContext(hugeCtx, binary, "mcp", "--policy", policyPath)
	huge.Stdin = strings.NewReader(`{"oversized":"` + strings.Repeat("x", mcpserver.MaxFrameBytes+1))
	_, hugeErr := huge.CombinedOutput()
	stop()
	if hugeCtx.Err() == context.DeadlineExceeded || hugeErr == nil {
		t.Fatal("oversized unterminated frame not rejected")
	}
	// A read-only launch omits mutation tools, and cannot be broadened by input.
	entry := policy.Databases["example"]
	entry.Grants = []string{"read"}
	policy.Databases["example"] = entry
	server, err := mcpserver.New(ctx, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	left, right := mcp.NewInMemoryTransports()
	ss, err := server.MCP.Connect(ctx, left, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	ro, err := client.Connect(ctx, right, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	listed, err := ro.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "books_health" || tool.Name == "books_capabilities" {
			continue
		}
		op, ok := operations.LookupDatabaseOperation(strings.TrimPrefix(tool.Name, "books_db_"))
		if !ok || op.Descriptor().Grant != "read" {
			t.Fatal("write tool exposed")
		}
	}
	if err := os.Chmod(policyPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpserver.LoadPolicy(policyPath); err == nil {
		t.Fatal("public policy accepted")
	}
}
