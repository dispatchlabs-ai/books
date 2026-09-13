package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
)

func TestCompanyReportOptionsAndExactAmounts(t *testing.T) {
	for _, tc := range []struct{ currency, amount string }{{"USD", "90071992547409.93"}, {"JPY", "9007199254740993"}, {"KWD", "9007199254740.993"}} {
		t.Run(tc.currency, func(t *testing.T) {
			home := t.TempDir()
			configPath := filepath.Join(home, "books.toml")
			t.Setenv("BOOKS_HOME", home)
			t.Setenv("BOOKS_CONFIG", configPath)
			t.Setenv("BOOKS_DB", "")
			t.Setenv("BOOKS_ACTOR", "report-options-test")
			executeHumanJSON(t, "init", "--name", "Report Example", "--company", "acme", "--currency", tc.currency, "--start", "2026-01-01")
			executeHumanJSON(t, "account", "add", "bank", "Checking")
			token := strings.Repeat("t", 32)
			sum := sha256.Sum256([]byte(token))
			server, err := httpapi.New(context.Background(), configPath, httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "report-reader", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": {"read"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			get := func(report, query string, want int) map[string]any {
				t.Helper()
				r := httptest.NewRequest("GET", "/v1/companies/acme/reports/"+report+"?"+query, nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				if w.Code != want {
					t.Fatalf("%s: %d %s", report, w.Code, w.Body.String())
				}
				var env map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatal(err)
				}
				data, _ := env["data"].(map[string]any)
				return data
			}
			reports := []struct {
				name, command, query, list, total string
				flags                             []string
			}{
				{"trial-balance", "tb", "as_of=2026-01-31", "rows", "total_debit_cents", []string{"--as-of", "2026-01-31"}},
				{"profit-loss", "pl", "from=2026-01-01&to=2026-01-31", "revenue", "total_revenue", []string{"--from", "2026-01-01", "--to", "2026-01-31"}},
				{"balance-sheet", "bs", "as_of=2026-01-31", "assets", "total_assets", []string{"--as-of", "2026-01-31"}},
			}
			for _, r := range reports {
				empty := get(r.name, r.query, 200)
				zeros := get(r.name, r.query+"&include_zero=true", 200)
				rows, _ := empty[r.list].([]any)
				zeroRows, _ := zeros[r.list].([]any)
				if len(rows) != 0 || len(zeroRows) == 0 {
					t.Fatalf("%s include_zero ineffective: %v / %v", r.name, rows, zeroRows)
				}
				get(r.name, r.query+"&include_zero=invalid", 400)
				get(r.name, r.query+"&include_zero=true&include_zero=false", 400)
			}
			executeHumanJSON(t, "receive", tc.amount, "Revenue", "Exact amount", "--date", "2026-01-15", "--key", "exact-report")
			registry, err := booksconfig.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := registry.Resolve(configPath, "acme")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range reports {
				data := get(r.name, r.query+"&include_zero=true", 200)
				total := data[r.total]
				if m, ok := total.(map[string]any); ok {
					total = m["consolidated_cents"]
				}
				if total != "9007199254740993" {
					t.Fatalf("%s lost integer precision: %v", r.name, total)
				}
				if data["scope"].(map[string]any)["currency"] != tc.currency {
					t.Fatal("wrong currency")
				}
				args := append([]string{r.command, "--include-zero"}, r.flags...)
				cli, _ := executeHumanJSON(t, args...)
				cliData := cli["data"].(map[string]any)
				cliTotal := cliData[r.total]
				if m, ok := cliTotal.(map[string]any); ok {
					cliTotal = m["consolidated_cents"]
				}
				if cliTotal != tc.amount {
					t.Fatalf("CLI/API mismatch: %v", cliTotal)
				}
				rawArgs := append([]string{"--db", resolved.Database, r.command, "--entity", "ACME", "--include-zero"}, r.flags...)
				raw, _ := executeHumanJSON(t, rawArgs...)
				if !reflect.DeepEqual(cli["data"], raw["data"]) {
					t.Fatalf("%s raw database fallback differs", r.name)
				}
				t.Setenv("BOOKS_DB", resolved.Database)
				envArgs := append([]string{r.command, "--entity", "ACME", "--include-zero"}, r.flags...)
				viaEnv, _ := executeHumanJSON(t, envArgs...)
				if !reflect.DeepEqual(raw["data"], viaEnv["data"]) {
					t.Fatalf("%s environment database fallback differs", r.name)
				}
				t.Setenv("BOOKS_DB", "")
			}
		})
	}
}
