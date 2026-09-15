package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/budget"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetAccountingAdaptersAndDryRun(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	config := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", config)
	t.Setenv("BOOKS_ACTOR", "budget-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	executeHumanJSON(t, "account", "add", "bank", "Savings")
	executeHumanJSON(t, "spend", "40.00", "General Expense", "July purchase", "--date", "2026-07-03")
	executeHumanJSON(t, "spend", "60.00", "General Expense", "August purchase", "--date", "2026-08-03")
	executeHumanJSON(t, "transfer", "100.00", "Checking", "Savings", "--date", "2026-08-04")
	ctx := context.Background()
	app, e := application.Open(ctx, config, "acme", "budget-test", storesqlite.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = app.Close() }()
	r, e := app.Budget(ctx, application.BudgetRequest{AsOf: "2026-09-15"})
	if e != nil {
		t.Fatal(e)
	}
	cash, expense := "", ""
	for _, a := range r.Accounts {
		if a.Name == "Checking" {
			cash = a.Code
		}
		if a.Name == "General Expense" {
			expense = a.Code
		}
	}
	if len(r.Expenses) != 2 {
		t.Fatalf("transfers counted: %+v", r)
	}
	for _, row := range r.Rows {
		if row.Account == cash && row.Average != 5000 {
			t.Fatal(row)
		}
	}
	target := cashflow.Amount(8000)
	request := application.BudgetSaveRequest{AsOf: "2026-09-15", Plan: budget.Plan{Buckets: []budget.Bucket{{Account: cash, Monthly: &target, ExpenseAccounts: []string{expense}}}, Assignments: []budget.Assignment{}}}
	raw, _ := json.Marshal(request)
	file := filepath.Join(home, "budget.json")
	if e = os.WriteFile(file, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_ = executeHumanFailure(t, "--dry-run", "budget", "save", "--input", file)
	untouched, e := app.Budget(ctx, application.BudgetRequest{AsOf: "2026-09-15"})
	if e != nil || untouched.Plan.Revision != "" {
		t.Fatal("dry-run changed planning state", e)
	}
	executeHumanJSON(t, "budget", "save", "--input", file)
	saved, e := app.Budget(ctx, application.BudgetRequest{AsOf: "2026-09-15"})
	if e != nil || saved.Plan.Revision == "" {
		t.Fatal(e)
	}
	token := strings.Repeat("b", 32)
	sum := sha256.Sum256([]byte(token))
	for _, write := range []bool{false, true} {
		grants := []string{"read"}
		if write {
			grants = append(grants, "budget")
		}
		server, e := httpapi.New(ctx, config, httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "budget-reader", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": grants}}}})
		if e != nil {
			t.Fatal(e)
		}
		run := func(company, op string, body []byte) *httptest.ResponseRecorder {
			q := httptest.NewRequest("POST", "/v1/companies/"+company+"/operations/"+op, strings.NewReader(string(body)))
			q.Header.Set("Authorization", "Bearer "+token)
			q.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, q)
			return w
		}
		w := run("acme", "budget_get", []byte(`{"as_of":"2026-09-15"}`))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"average":"5000"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if w = run("foreign", "budget_get", []byte(`{"as_of":"2026-09-15"}`)); w.Code != 404 {
			t.Fatal("scope", w.Code)
		}
		request.Plan = saved.Plan
		body, _ := json.Marshal(request)
		w = run("acme", "budget_save", body)
		if write && w.Code != 200 || !write && w.Code != 404 {
			t.Fatal("grant", write, w.Code, w.Body.String())
		}
		_ = server.Close()

		ms, err := mcpserver.New(ctx, mcpserver.Policy{Schema: "books.mcp-policy/v1", Actor: "budget-test", ConfigPath: config, Companies: map[string][]string{"acme": grants}})
		if err != nil {
			t.Fatal(err)
		}
		left, right := mcp.NewInMemoryTransports()
		ss, err := ms.MCP.Connect(ctx, left, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "budget-test", Version: "1"}, nil)
		cs, err := client.Connect(ctx, right, nil)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tool := range tools.Tools {
			if tool.Name == "books_company_budget_save" {
				found = true
			}
		}
		if found != write {
			t.Fatal("MCP budget discovery bypass", write, found)
		}
		if write {
			current, err := app.Budget(ctx, application.BudgetRequest{AsOf: "2026-09-15"})
			if err != nil {
				t.Fatal(err)
			}
			request.Plan = current.Plan
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "books_company_budget_save", Arguments: map[string]any{"company": "acme", "input": request}})
			if err != nil || result.IsError {
				detail, _ := json.Marshal(result)
				t.Fatalf("MCP budget save: %v %s", err, detail)
			}
		}
		_ = cs.Close()
		_ = ss.Close()
		_ = ms.Close()
	}
}
func TestBudgetExcludesFiscalClosing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.sqlite")
	store, e := storesqlite.Init(ctx, path, "USD", "budget-test")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = store.Close() }()
	svc := ledger.NewService(store, "budget-test")
	if _, e = svc.CreatePeriod(ctx, ledger.CreatePeriodInput{Code: "2026-12", StartDate: "2026-12-01", EndDate: "2026-12-31", FiscalYear: 2026, PeriodNumber: 12, YearEnd: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); e != nil {
		t.Fatal(e)
	}
	for _, a := range []ledger.CreateAccountInput{{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"TESTCO"}}, {Code: "3900", Name: "Equity", Type: "EQUITY", BookCodes: []string{"TESTCO"}}, {Code: "5000", Name: "Expense", Type: "EXPENSE", BookCodes: []string{"TESTCO"}}} {
		if _, e = svc.CreateAccount(ctx, a); e != nil {
			t.Fatal(e)
		}
	}
	for _, input := range []ledger.CreateJournalInput{{Book: "TESTCO", PostingDate: "2026-12-02", Period: "2026-12", Description: "Expense", Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 5000}, {Account: "1000", CreditCents: 5000}}}, {Book: "TESTCO", Kind: "CLOSING", PostingDate: "2026-12-31", Period: "2026-12", Description: "Closing", Lines: []ledger.JournalLineInput{{Account: "5000", CreditCents: 5000}, {Account: "3900", DebitCents: 5000}}}} {
		j, err := svc.CreateJournal(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.PostJournal(ctx, j.ID); err != nil {
			t.Fatal(err)
		}
	}
	var uuid string
	if e = store.DB().QueryRow("SELECT database_uuid FROM database_metadata").Scan(&uuid); e != nil {
		t.Fatal(e)
	}
	app, e := application.Bind(ctx, store, booksconfig.ResolvedCompany{Key: "testco", Database: path, Company: booksconfig.Company{Name: "Test Co", EntityCode: "TESTCO", BookCode: "TESTCO", Currency: "USD", DatabaseUUID: uuid}}, "budget-test")
	if e != nil {
		t.Fatal(e)
	}
	r, e := app.SaveBudget(ctx, application.BudgetSaveRequest{AsOf: "2027-01-15", Plan: budget.Plan{Buckets: []budget.Bucket{{Account: "1000", ExpenseAccounts: []string{"5000"}}}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Expenses) != 1 || r.Rows[0].Average != 2500 {
		t.Fatal("fiscal close counted as spending", r)
	}
}
