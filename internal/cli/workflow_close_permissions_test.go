package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/httpapi"
)

// Closing journals require management permission even through ordinary
// transaction routes; returned year-close plans must survive HTTP roundtrips.
func TestWorkflowClosingJournalPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
	t.Setenv("BOOKS_DB", "")
	t.Setenv("BOOKS_ACTOR", "workflow-close-test")
	executeHumanJSON(t, "init", "--name", "Example Company", "--company", "acme", "--start", "2026-01-01", "--chart", "starter")
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
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-deposit", "--no-reconcile")
	executeHumanJSON(t, "receive", "10.00", "Revenue", "--date", "2026-02-02", "--key", "receipt")
	for month := 1; month <= 11; month++ {
		executeHumanJSON(t, "period", "close", "--book", "ACME", "--period", fmt.Sprintf("2026-%02d", month))
	}
	plan := request("reader", "POST", "year-close/plan", map[string]any{"fiscal_year": 2026}, "", 200)
	applied := request("manager", "POST", "year-close/apply", map[string]any{"plan": plan}, "", 200)
	if plan["net_income_cents"] != "1000" || applied["status"] != "POSTED" {
		t.Fatal(plan, applied)
	}
	replay := request("manager", "POST", "year-close/apply", map[string]any{"plan": plan}, "", 200)
	if applied["transaction"].(map[string]any)["number"] != replay["transaction"].(map[string]any)["number"] {
		t.Fatal(applied, replay)
	}
	number := applied["transaction"].(map[string]any)["number"].(string)
	for _, action := range []string{"reverse", "undo"} {
		request("poster", "POST", "transactions/"+number+"/"+action, map[string]any{"date": "2026-12-31"}, "", 403)
	}
	dashboard := request("reader", "GET", "dashboard", nil, "", 200)
	if dashboard["posted_transactions"] != float64(2) || dashboard["drafts"] != float64(0) {
		t.Fatalf("denied requests changed journals: %+v", dashboard)
	}
	reversed := request("manager", "POST", "transactions/"+number+"/reverse", map[string]any{"date": "2026-12-31"}, "", 200)
	if reversed["kind"] != "CLOSING_REVERSAL" || reversed["status"] != "POSTED" || reversed["total_debit_cents"] != "1000" {
		t.Fatal(reversed)
	}
}
