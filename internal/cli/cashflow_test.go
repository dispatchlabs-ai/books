package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCashForecastAdapters(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	config := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", config)
	t.Setenv("BOOKS_ACTOR", "cash-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	app, e := application.Open(context.Background(), config, "acme", "cash-test", storesqlite.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = app.Close() }()
	ac, e := app.Accounts(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	code := ""
	for _, a := range ac {
		if a.Name == "Checking" {
			code = a.Code
		}
	}
	if code == "" {
		t.Fatal("no synthetic account")
	}
	p := cashflow.Plan{Version: "books.cash-plan/v1", Name: "Synthetic", Currency: "USD", AsOf: "2026-01-01", Through: "2026-01-03", Accounts: []cashflow.Account{{Code: code, Name: "Checking", Kind: "bank", Opening: 100000, Evidence: "Synthetic opening snapshot"}}, Events: []cashflow.Event{{ID: "pay", Date: "2026-01-02", Name: "Pay", Kind: "inflow", Account: code, Amount: 20000, Status: "estimated", Evidence: "Synthetic schedule"}}}
	data, _ := json.Marshal(p)
	file := filepath.Join(home, "cash-plan.json")
	if e = os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	executeHumanJSON(t, "cash-forecast", "--input", file)
	token := strings.Repeat("f", 32)
	sum := sha256.Sum256([]byte(token))
	server, e := httpapi.New(context.Background(), config, httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "reader", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": {"read"}}}}})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = server.Close() }()
	run := func(company string, raw []byte) *httptest.ResponseRecorder {
		q := httptest.NewRequest("POST", "/v1/companies/"+company+"/operations/cash_forecast", strings.NewReader(string(raw)))
		q.Header.Set("Authorization", "Bearer "+token)
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, q)
		return w
	}
	w := run("acme", data)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got struct {
		Data cashflow.Result `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	if len(got.Data.Days) != 3 || got.Data.Days[2].Closing != 120000 {
		t.Fatal(w.Body.String())
	}
	if w = run("other", data); w.Code != 404 {
		t.Fatal("company scope", w.Code)
	}
	p.Accounts[0].Code = "not-this-book"
	data, _ = json.Marshal(p)
	if w = run("acme", data); w.Code == 200 {
		t.Fatal("foreign account accepted")
	}
	executeHumanJSON(t, "doctor")
	executeHumanJSON(t, "audit", "verify")
}
