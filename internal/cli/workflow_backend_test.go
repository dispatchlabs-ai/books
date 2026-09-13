package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
)

// The CLI initializes the disposable company; HTTP and CLI then operate on
// the same journals, plans and defaults through the application boundary.
func TestWorkflowBackendJourney(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	tokens := map[string]string{"reader": strings.Repeat("r", 32), "poster": strings.Repeat("p", 32), "manager": strings.Repeat("m", 32)}
	config := httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0"}
	for _, role := range []string{"reader", "poster", "manager"} {
		sum := sha256.Sum256([]byte(tokens[role]))
		grants := []string{"read"}
		if role != "reader" {
			grants = append(grants, "post")
		}
		if role == "manager" {
			grants = append(grants, "manage")
		}
		config.Principals = append(config.Principals, httpapi.Principal{ID: role, TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": grants}})
	}
	server, err := httpapi.New(context.Background(), filepath.Join(home, "books.toml"), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	request := func(role, method, path string, body any, key string, want int) map[string]any {
		t.Helper()
		bytes, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/v1/companies/acme/"+path, strings.NewReader(string(bytes)))
		r.Header.Set("Authorization", "Bearer "+tokens[role])
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var env map[string]any
		if err = json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		data, _ := env["data"].(map[string]any)
		return data
	}
	account := map[string]any{"kind": "bank", "name": "Checking", "code": "1000", "active_from": "2026-01-01", "default_payment": true, "default_deposit": true}
	request("poster", "POST", "accounts", account, "", 403)
	request("manager", "POST", "accounts", account, "", 200)
	spend := map[string]any{"amount": "12.34", "account": "5000", "date": "2026-01-15", "description": "Supplies"}
	request("reader", "POST", "transactions/spend", spend, "spend-one", 403)
	request("poster", "POST", "transactions/spend", spend, "", 400)
	first := request("poster", "POST", "transactions/spend", spend, "spend-one", 200)
	second := request("poster", "POST", "transactions/spend", spend, "spend-one", 200)
	if first["number"] != second["number"] || first["total_debit_cents"] != "1234" || first["status"] != "POSTED" {
		t.Fatal(first, second)
	}
	spend["amount"] = "12.35"
	request("poster", "POST", "transactions/spend", spend, "spend-one", 409)
	number := first["number"].(string)
	env, _ := executeHumanJSON(t, "tx", "show", number)
	if env["data"].(map[string]any)["total_debit_cents"] != "12.34" {
		t.Fatal(env)
	}
	journal := map[string]any{"posting_date": "2026-01-15", "description": "Corrected supplies", "lines": []any{map[string]any{"account": "5000", "debit": "10.00"}, map[string]any{"account": "1000", "credit": "10.00"}}}
	correction := map[string]any{"journal": journal, "reason": "Correct amount"}
	corrected := request("poster", "POST", "transactions/"+number+"/correct", correction, "", 200)
	replay := request("poster", "POST", "transactions/"+number+"/correct", correction, "", 200)
	if corrected["replacement"].(map[string]any)["number"] != replay["replacement"].(map[string]any)["number"] {
		t.Fatal(corrected, replay)
	}
	plan := request("reader", "POST", "reconciliations/plan", map[string]any{"statement_account": "ACME-1000", "through": "2026-01-31", "beginning": "0.00", "ending": "-10.00", "clear_all": true}, "", 200)
	if plan["ready"] != true || plan["ending_balance_cents"] != "-1000" {
		t.Fatal(plan)
	}
	request("reader", "POST", "reconciliations/apply", map[string]any{"plan": plan}, "", 403)
	request("poster", "POST", "reconciliations/apply", map[string]any{"plan": plan}, "", 200)
	request("poster", "POST", "reconciliations/apply", map[string]any{"plan": plan}, "", 200)
	closePlan := request("reader", "POST", "close/plan", map[string]any{"period": "2026-01"}, "", 200)
	request("poster", "POST", "close/apply", map[string]any{"plan": closePlan}, "", 403)
	request("manager", "POST", "close/apply", map[string]any{"plan": closePlan}, "", 200)
	request("manager", "POST", "close/apply", map[string]any{"plan": closePlan}, "", 200)
	request("manager", "POST", "periods/2026-01/reopen", map[string]any{"reason": "More evidence"}, "", 200)
	executeHumanJSON(t, "doctor")
}

func TestQuickBooksPartialApplyReportsRecoveryTarget(t *testing.T) {
	setupHumanCLIHome(t, "2023-01-01", "empty")
	testdata, err := filepath.Abs(filepath.Join("..", "importer", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := executeHumanJSON(t, "import", "quickbooks", "plan", "--from", filepath.Join(testdata, "general_ledger.json"), "--accounts", filepath.Join(testdata, "accounts.json"))
	path := plan["data"].(map[string]any)["plan_path"].(string)
	executeHumanJSON(t, "period", "close", "--book", "ACME", "--period", "2023-01")
	executeHumanJSON(t, "period", "close", "--book", "ACME", "--period", "2023-02")
	failed := executeHumanFailure(t, "import", "quickbooks", "apply", "--plan", path)
	problem, ok := apperr.As(failed)
	if !ok || problem.Code != "QUICKBOOKS_APPLY_PARTIAL" || !strings.Contains(problem.Message, "import-journals") || !strings.Contains(problem.Message, "batch:") {
		t.Fatal(failed)
	}
	executeHumanJSON(t, "period", "reopen", "--book", "ACME", "--period", "2023-02", "--reason", "Complete reviewed import")
	executeHumanJSON(t, "period", "reopen", "--book", "ACME", "--period", "2023-01", "--reason", "Review new statement control coverage")
	result, _ := executeHumanJSON(t, "import", "quickbooks", "apply", "--plan", path)
	if result["data"].(map[string]any)["status"] != "POSTED" {
		t.Fatal(result)
	}
	executeHumanJSON(t, "doctor")
}
