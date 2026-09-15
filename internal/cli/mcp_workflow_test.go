package cli

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"path/filepath"
	"testing"
)

// This is a deterministic accounting/protocol oracle, not a model-selection benchmark.
func TestMCPContinuousWorkflow(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	path := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", path)
	t.Setenv("BOOKS_ACTOR", "mcp-workflow")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	ctx := context.Background()
	server, err := mcpserver.New(ctx, mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "mcp-workflow", ConfigPath: path, ArtifactDirectory: filepath.Join(home, "artifacts"), Companies: map[string][]string{"acme": {"read", "post", "manage", "import"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	connect := func() *mcp.ClientSession {
		t.Helper()
		left, right := mcp.NewInMemoryTransports()
		ss, err := server.MCP.Connect(ctx, left, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "workflow-oracle", Version: "1"}, nil).Connect(ctx, right, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}
	cs := connect()
	call := func(op string, input any) map[string]any {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "books_company_" + op, Arguments: map[string]any{"company": "acme", "input": input}})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %+v", op, err, r)
		}
		return r.StructuredContent.(map[string]any)["result"].(map[string]any)
	}
	income := map[string]any{"amount": "1000.00", "account": "Revenue", "description": "Synthetic consulting", "date": "2026-01-15", "key": "workflow-income"}
	received := call("receive", income)
	if received["status"] != "POSTED" {
		t.Fatal(received)
	}
	// Lose the response at the caller and reconnect; retry exactly the original request.
	number := received["number"]
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	cs = connect()
	replay := call("receive", income)
	if replay["number"] != number {
		t.Fatal("retry created another transaction")
	}
	draft := call("spend", map[string]any{"amount": "50.00", "account": "General Expense", "description": "Synthetic software", "date": "2026-01-16", "key": "workflow-expense", "draft": true})
	if draft["status"] != "DRAFT" {
		t.Fatal(draft)
	}
	// A draft must not affect posted balances.
	before, _ := executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
	if before["data"].(map[string]any)["total_debit_cents"] != "1000.00" {
		t.Fatal("draft affected posted balance")
	}
	posted := call("tx_post", map[string]any{"number": draft["number"]})
	if posted["status"] != "POSTED" {
		t.Fatal(posted)
	}
	reversal := call("reverse", map[string]any{"number": draft["number"], "date": "2026-01-17", "description": "Synthetic reversal"})
	if reversal["status"] != "POSTED" {
		t.Fatal(reversal)
	}
	original := call("tx_show", map[string]any{"number": draft["number"]})
	if original["status"] != "POSTED" {
		t.Fatal("posted history changed", original)
	}
	denied, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "books_company_receive", Arguments: map[string]any{"company": "other", "input": income}})
	if err != nil || !denied.IsError {
		t.Fatal("wrong company accepted", err, denied)
	}
	// Compare to the independent CLI presentation of shared ledger state.
	balance := call("report_trial_balance", map[string]any{"as_of": "2026-01-31"})
	if balance["total_debit_cents"] != "100000" || balance["total_credit_cents"] != "100000" {
		t.Fatal(balance)
	}
	cli, _ := executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
	if cli["data"].(map[string]any)["total_debit_cents"] != "1000.00" {
		t.Fatal("CLI ledger oracle mismatch")
	}
	executeHumanJSON(t, "doctor")
	executeHumanJSON(t, "audit", "verify")
}
