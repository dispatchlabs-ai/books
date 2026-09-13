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

	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestGeneralLedgerAPI(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
	t.Setenv("BOOKS_ACTOR", "general-ledger-test")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	executeHumanJSON(t, "receive", "1000.00", "Revenue", "Opening sale", "--date", "2026-01-01", "--key", "gl-opening")
	executeHumanJSON(t, "spend", "50.00", "General Expense", "In-range expense", "--date", "2026-02-01", "--key", "gl-expense")
	// A foreign book in the same SQLite file must not contribute to this report.
	registry, err := booksconfig.Load(filepath.Join(home, "books.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(filepath.Join(home, "books.toml"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storesqlite.Open(context.Background(), resolved.Database, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := ledger.NewService(store, "general-ledger-test")
	if _, err = svc.CreateEntity(context.Background(), ledger.CreateEntityInput{Code: "OTHER", LegalName: "Other Example", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"1000", "4000"} {
		if err = svc.ConfigureBookAccount(context.Background(), "OTHER", code, "2026-01-01", "", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.CreateAndPostJournal(context.Background(), ledger.CreateJournalInput{Book: "OTHER", PostingDate: "2026-02-02", Period: "2026-02", Description: "Foreign secret sale", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 77700}, {Account: "4000", CreditCents: 77700}}}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("r", 32)
	hash := sha256.Sum256([]byte(token))
	server, err := httpapi.New(context.Background(), filepath.Join(home, "books.toml"), httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "reader", TokenSHA256: hex.EncodeToString(hash[:]), Companies: map[string][]string{"acme": {"read"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	request := func(path, credential string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, w.Code, want, w.Body.String())
		}
		var env map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &env); e != nil {
			t.Fatal(e)
		}
		return env
	}
	base := "/v1/companies/acme/reports/general-ledger"
	query := "?from=2026-02-01&to=2026-02-28&account=Checking"
	env := request(base+query, token, 200)
	data := env["data"].(map[string]any)
	accounts := data["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("unexpected accounts: %v", accounts)
	}
	account := accounts[0].(map[string]any)
	if account["opening_balance"].(map[string]any)["consolidated_cents"] != "100000" || account["closing_balance"].(map[string]any)["consolidated_cents"] != "95000" {
		t.Fatalf("wrong exact balances: %v", account)
	}
	line := account["lines"].([]any)[0].(map[string]any)
	if line["credit_cents"] != "5000" || line["entity_code"] != "ACME" {
		t.Fatalf("wrong line: %v", line)
	}
	cli, _ := executeHumanJSON(t, "gl", "--from", "2026-02-01", "--to", "2026-02-28", "--account", "Checking")
	cliAccount := cli["data"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
	if cliAccount["closing_balance"].(map[string]any)["consolidated_cents"] != "950.00" {
		t.Fatalf("CLI/API amount mismatch: %v", cliAccount)
	}
	request(base+query, "", 401)
	request(strings.Replace(base, "/acme/", "/other/", 1)+query, token, 404)
	for _, q := range []string{"?from=bad&to=2026-02-28", query + "&entity=OTHER", query + "&group=ALL", query + "&include_zero=yes", query + "&from=2026-01-01"} {
		request(base+q, token, 400)
	}
	request(base+"?from=2026-02-01&to=2026-02-28&include_zero=true", token, 200)
}
