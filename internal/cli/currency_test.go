package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/money"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleCurrencyCompanyJourneys(t *testing.T) {
	for _, tc := range []struct{ code, amount, bad string }{
		{"EUR", "123.45", "1.001"}, {"JPY", "123", "1.1"}, {"KWD", "123.456", "1.0001"}, {"CLF", "123.4567", "1.00001"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("BOOKS_HOME", home)
			t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
			t.Setenv("BOOKS_DB", "")
			t.Setenv("BOOKS_ACTOR", "currency-test")
			executeHumanJSON(t, "init", "--name", "Example Personal Books", "--company", "example", "--currency", tc.code, "--start", "2026-01-01")
			executeHumanJSON(t, "account", "add", "bank", "Checking")
			env, _ := executeHumanJSON(t, "receive", tc.amount, "Revenue", "Synthetic receipt", "--to", "Checking", "--date", "2026-01-15", "--key", "receipt")
			tx := env["data"].(map[string]any)
			if tx["total_debit_cents"] != tc.amount || tx["currency"] != tc.code {
				t.Fatal(tx)
			}
			requireApplicationCode(t, executeHumanFailure(t, "receive", tc.bad, "Revenue", "--to", "Checking", "--date", "2026-01-15", "--key", "bad"), "AMOUNT_INVALID")
			env, _ = executeHumanJSON(t, "tb", "--as-of", "2026-01-31")
			report := env["data"].(map[string]any)
			if report["total_debit_cents"] != tc.amount || report["total_credit_cents"] != tc.amount {
				t.Fatal(report)
			}
			root, _ := newRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetArgs([]string{"tb", "--as-of", "2026-01-31"})
			if e := root.Execute(); e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(output.String(), tc.amount) {
				t.Fatal(output.String())
			}
			// HTTP transports expose the same exact amount as integer minor units.
			token := strings.Repeat("x", 32)
			hash := sha256.Sum256([]byte(token))
			server, e := httpapi.New(context.Background(), filepath.Join(home, "books.toml"), httpapi.Config{Schema: "books.server/v1", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "currency-test", TokenSHA256: hex.EncodeToString(hash[:]), Companies: map[string][]string{"example": {"read"}}}}})
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("GET", "/v1/companies/example/reports/trial-balance?as_of=2026-01-31", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if e = server.Close(); e != nil {
				t.Fatal(e)
			}
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			var api map[string]any
			if e = json.Unmarshal(w.Body.Bytes(), &api); e != nil {
				t.Fatal(e)
			}
			units, e := money.ParseCurrency(tc.amount, tc.code)
			if e != nil {
				t.Fatal(e)
			}
			apiReport := api["data"].(map[string]any)
			if apiReport["total_debit_cents"] != fmt.Sprint(units) || apiReport["scope"].(map[string]any)["currency"] != tc.code {
				t.Fatal(apiReport)
			}
			// Both plan and apply use the same scale and retain integer minor units.
			planPath := filepath.Join(home, "reconciliation.json")
			executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-01-31", "--ending", tc.amount, "--out", planPath)
			var saved manualReconciliationPlan
			if e := readJSONInput(planPath, &saved); e != nil {
				t.Fatal(e)
			}
			if saved.Currency != tc.code || saved.Schema != "books.reconciliation-plan/v4" {
				t.Fatal(saved)
			}
			original := saved
			saved.Currency = "USD"
			saved.Digest, _ = manualPlanDigest(saved)
			altered := filepath.Join(home, "wrong-currency-plan.json")
			rawPlan, _ := json.Marshal(saved)
			if e := os.WriteFile(altered, rawPlan, 0600); e != nil {
				t.Fatal(e)
			}
			requireApplicationCode(t, executeHumanFailure(t, "reconcile", "apply", "--plan", altered), "PLAN_CURRENCY_MISMATCH")
			saved = original
			saved.Schema = manualReconciliationPlanSchema
			saved.Currency = ""
			saved.Digest, _ = manualPlanDigest(saved)
			rawPlan, _ = json.Marshal(saved)
			if e := os.WriteFile(altered, rawPlan, 0600); e != nil {
				t.Fatal(e)
			}
			requireApplicationCode(t, executeHumanFailure(t, "reconcile", "apply", "--plan", altered), "PLAN_CURRENCY_MISMATCH")
			executeHumanJSON(t, "reconcile", "apply", "--plan", planPath)
			// Explicit-profile CSV parses into the same currency and posts once.
			source := filepath.Join(home, "source.csv")
			profile := filepath.Join(home, "profile.json")
			choicesPath := filepath.Join(home, "choices.json")
			if e := os.WriteFile(source, []byte("Date,Amount,Description\n2026-02-15,"+tc.amount+",Synthetic sale\n"), 0600); e != nil {
				t.Fatal(e)
			}
			options := map[string]any{"format": "CSV", "institution": "FAKE", "account_id": "FAKE", "currency": tc.code, "date_layout": "2006-01-02", "tabular": map[string]any{"header_row": 1, "date_column": "Date", "amount_column": "Amount", "description_columns": []string{"Description"}, "decimal_separator": "."}}
			raw, _ := json.Marshal(options)
			if e := os.WriteFile(profile, raw, 0600); e != nil {
				t.Fatal(e)
			}
			env, _ = executeHumanJSON(t, "bank-import", "upload", "--input", source, "--options", profile, "--key", "upload")
			job := env["data"].(map[string]any)
			if job["status"] != "READY" {
				t.Fatal(job)
			}
			account := job["document"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
			tr := account["transactions"].([]any)[0].(map[string]any)
			choices := map[string]any{"post": true, "mappings": []any{map[string]any{"account_key": account["key"], "statement_account": "EXAMPLE-1000", "classifications": []any{map[string]any{"transaction_id": tr["id"], "contra_account": "4000"}}}}}
			raw, _ = json.Marshal(choices)
			if e := os.WriteFile(choicesPath, raw, 0600); e != nil {
				t.Fatal(e)
			}
			env, _ = executeHumanJSON(t, "bank-import", "preview", job["id"].(string), "--input", choicesPath, "--key", "preview")
			plan := env["data"].(map[string]any)
			executeHumanJSON(t, "bank-import", "apply", plan["id"].(string), "--digest", plan["digest"].(string), "--commit")
			backup := filepath.Join(home, "backup.sqlite")
			executeHumanJSON(t, "backup", "--out", backup)
			executeHumanJSON(t, "restore", "--from", backup, "--confirm", "example")
			env, _ = executeHumanJSON(t, "tb", "--as-of", "2026-02-28")
			unit, _ := money.Lookup(tc.code)
			if env["data"].(map[string]any)["total_debit_cents"] != unit.Format(units*2) {
				t.Fatal(env)
			}
			executeHumanJSON(t, "doctor")
			executeHumanJSON(t, "audit", "verify")
			registry, e := booksconfig.Load(filepath.Join(home, "books.toml"))
			if e != nil {
				t.Fatal(e)
			}
			company := registry.Companies["example"]
			company.Currency = "USD"
			registry.Companies["example"] = company
			if e = booksconfig.Save(filepath.Join(home, "books.toml"), registry); e != nil {
				t.Fatal(e)
			}
			requireApplicationCode(t, executeHumanFailure(t, "tb", "--as-of", "2026-02-28"), "COMPANY_DATABASE_MISMATCH")
		})
	}
}

func TestSingleCurrencyYearClose(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
	t.Setenv("BOOKS_DB", "")
	t.Setenv("BOOKS_ACTOR", "currency-test")
	executeHumanJSON(t, "init", "--name", "Example", "--company", "example", "--currency", "KWD", "--start", "2026-01-01")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-deposit", "--no-reconcile")
	executeHumanJSON(t, "receive", "12.345", "Revenue", "--date", "2026-02-02", "--key", "receipt")
	for month := 1; month <= 11; month++ {
		executeHumanJSON(t, "period", "close", "--book", "EXAMPLE", "--period", fmt.Sprintf("2026-%02d", month))
	}
	env, _ := executeHumanJSON(t, "year-close", "plan", "2026")
	data := env["data"].(map[string]any)
	plan := data["plan"].(map[string]any)
	if plan["currency"] != "KWD" || plan["schema"] != "books.year-close-plan/v2" || plan["net_income_cents"] != "12.345" {
		t.Fatal(plan)
	}
	executeHumanJSON(t, "year-close", "apply", "--plan", data["plan_path"].(string))
	executeHumanJSON(t, "year-close", "apply", "--plan", data["plan_path"].(string))
	executeHumanJSON(t, "doctor")
	executeHumanJSON(t, "audit", "verify")
}

func TestSingleCurrencyConsolidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
	t.Setenv("BOOKS_DB", "")
	t.Setenv("BOOKS_ACTOR", "currency-test")
	executeHumanJSON(t, "init", "--name", "Example", "--company", "example", "--currency", "EUR", "--start", "2026-01-01")
	executeHumanJSON(t, "entity", "create", "--code", "CHILD", "--name", "Example Child", "--currency", "EUR")
	executeHumanJSON(t, "ownership", "set", "--parent", "EXAMPLE", "--child", "CHILD", "--from", "2026-01-01")
	executeHumanJSON(t, "group", "create", "--code", "GROUP", "--name", "Example Group", "--parent", "EXAMPLE")
	env, _ := executeHumanJSON(t, "report", "trial-balance", "--group", "GROUP", "--as-of", "2026-01-31")
	if env["data"].(map[string]any)["scope"].(map[string]any)["currency"] != "EUR" {
		t.Fatal(env)
	}
	executeHumanJSON(t, "entity", "create", "--code", "FOREIGN", "--name", "Example Foreign", "--currency", "USD")
	executeHumanJSON(t, "ownership", "set", "--parent", "EXAMPLE", "--child", "FOREIGN", "--from", "2026-01-01")
	requireApplicationCode(t, executeHumanFailure(t, "report", "trial-balance", "--group", "GROUP", "--as-of", "2026-01-31"), "GROUP_CURRENCY_MISMATCH")
}
