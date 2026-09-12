package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

const syntheticOFX = `<OFX><BANKMSGSRSV1><STMTTRNRS><TRNUID>test</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>USD</CURDEF><BANKACCTFROM><BANKID>FAKE</BANKID><ACCTID>1234</ACCTID><ACCTTYPE>CHECKING</ACCTTYPE></BANKACCTFROM><BANKTRANLIST><DTSTART>20260701</DTSTART><DTEND>20260731</DTEND><STMTTRN><TRNTYPE>CREDIT</TRNTYPE><DTPOSTED>20260715</DTPOSTED><TRNAMT>123.45</TRNAMT><FITID>one</FITID><NAME>Synthetic sale</NAME></STMTTRN></BANKTRANLIST><LEDGERBAL><BALAMT>123.45</BALAMT><DTASOF>20260731</DTASOF></LEDGERBAL></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`

func TestBankBackendPermissionsAndRestart(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	// Put two real entities in one disposable SQLite file to prove isolation
	// comes from company/book scope rather than separate filesystem paths.
	configPath := filepath.Join(home, "books.toml")
	registry, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(configPath, "acme")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storesqlite.Open(context.Background(), resolved.Database, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err = service.CreateEntity(context.Background(), ledger.CreateEntityInput{Code: "OTHER", LegalName: "Other Example", Currency: "USD"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err = service.ConfigureBookAccount(context.Background(), "OTHER", "1000", "2026-01-01", "", true); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err = service.CreateStatementAccount(context.Background(), ledger.CreateStatementAccountInput{Code: "OTHER-CASH", Entity: "OTHER", Book: "OTHER", GLAccount: "1000", Name: "Other checking", Kind: "BANK", Currency: "USD", ReconciliationRequiredFrom: "2026-01-01"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	other := registry.Companies["acme"]
	other.Name = "Other Example"
	other.EntityCode = "OTHER"
	other.BookCode = "OTHER"
	registry.Companies["other"] = other
	if err = booksconfig.Save(configPath, registry); err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{"reader": strings.Repeat("r", 32), "importer": strings.Repeat("i", 32), "poster": strings.Repeat("p", 32), "other": strings.Repeat("o", 32)}
	c := httpapi.Config{Schema: "books.server/v1", Listen: "127.0.0.1:0", AllowedOrigins: []string{"http://localhost:3000"}}
	for _, id := range []string{"reader", "importer", "poster", "other"} {
		hash := sha256.Sum256([]byte(tokens[id]))
		grants := []string{"read"}
		company := "acme"
		if id == "importer" || id == "poster" {
			grants = append(grants, "import")
		}
		if id == "poster" {
			grants = append(grants, "post")
		}
		if id == "other" {
			company = "other"
		}
		c.Principals = append(c.Principals, httpapi.Principal{ID: id, TokenSHA256: hex.EncodeToString(hash[:]), Companies: map[string][]string{company: grants}})
	}
	server, err := httpapi.New(context.Background(), filepath.Join(home, "books.toml"), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	request := func(actor, method, path, body, key, match string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tokens[actor])
		r.Header.Set("Idempotency-Key", key)
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/imports") {
				r.Header.Set("Content-Type", "application/octet-stream")
			}
		}
		if match != "" {
			r.Header.Set("If-Match", `"`+match+`"`)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s as %s: %d %s", method, path, actor, w.Code, w.Body.String())
		}
		var env map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env == nil {
			return nil
		}
		data, _ := env["data"].(map[string]any)
		return data
	}
	base := "/v1/companies/acme"
	request("", "GET", base+"/accounts", "", "", "", 401)
	request("other", "GET", base+"/accounts", "", "", "", 404)
	request("reader", "POST", base+"/imports?name=synthetic.ofx", syntheticOFX, "denied", "", 403)
	job := request("importer", "POST", base+"/imports?name=synthetic.ofx", syntheticOFX, "upload", "", 202)
	id := job["id"].(string)
	if job["status"] != "UPLOADED" {
		t.Fatal(job)
	}
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	server, err = httpapi.New(context.Background(), filepath.Join(home, "books.toml"), c)
	if err != nil {
		t.Fatal(err)
	}
	server.ProcessPending(context.Background())
	job = request("reader", "GET", base+"/imports/"+id, "", "", "", 200)
	if job["status"] != "READY" {
		t.Fatal(job)
	}
	account := job["document"].(map[string]any)["accounts"].([]any)[0].(map[string]any)["key"].(string)
	choices, _ := json.Marshal(map[string]any{"post": true, "mappings": []any{map[string]any{"account_key": account, "statement_account": "ACME-1000", "classifications": []any{map[string]any{"transaction_id": "one", "contra_account": "4000"}}}}})
	preview := base + "/imports/" + id + "/previews"
	request("importer", "POST", preview, string(choices), "preview-denied", "", 403)
	request("poster", "POST", preview, strings.Replace(string(choices), "ACME-1000", "OTHER-CASH", 1), "forged-mapping", "", 404)
	plan := request("poster", "POST", preview, string(choices), "preview", "", 201)
	pid := plan["id"].(string)
	digest := plan["digest"].(string)
	request("other", "GET", "/v1/companies/other/imports/"+id, "", "", "", 404)
	request("other", "GET", "/v1/companies/other/imports/"+id+"/source", "", "", "", 404)
	request("other", "GET", "/v1/companies/other/import-plans/"+pid, "", "", "", 404)
	apply := base + "/import-plans/" + pid + "/apply"
	request("importer", "POST", apply, "", "", digest, 403)
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	c.Principals[2].Companies["acme"] = []string{"read", "import"}
	server, err = httpapi.New(context.Background(), configPath, c)
	if err != nil {
		t.Fatal(err)
	}
	request("poster", "POST", apply, "", "", digest, 403)
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	c.Principals[2].Companies["acme"] = []string{"read", "import", "post"}
	server, err = httpapi.New(context.Background(), configPath, c)
	if err != nil {
		t.Fatal(err)
	}
	request("poster", "POST", apply, "", "", "wrong", 409)
	first := request("poster", "POST", apply, "", "", digest, 200)
	again := request("poster", "POST", apply, "", "", digest, 200)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("replay differs")
	}
	request("reader", "GET", base+"/reports/trial-balance?as_of=2026-07-31", "", "", "", 200)
	r := httptest.NewRequest("GET", base+"/accounts", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Authorization", "Bearer "+tokens["poster"])
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestBankImportCLIJourney(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	file := filepath.Join(home, "synthetic.ofx")
	if err := os.WriteFile(file, []byte(syntheticOFX), 0600); err != nil {
		t.Fatal(err)
	}
	env, _ := executeHumanJSON(t, "bank-import", "upload", "--input", file, "--key", "upload")
	job := env["data"].(map[string]any)
	id := job["id"].(string)
	account := job["document"].(map[string]any)["accounts"].([]any)[0].(map[string]any)["key"].(string)
	choices := `{"post":true,"mappings":[{"account_key":"` + account + `","statement_account":"ACME-1000","classifications":[{"transaction_id":"one","contra_account":"4000"}]}]}`
	input := filepath.Join(home, "choices.json")
	if err := os.WriteFile(input, []byte(choices), 0600); err != nil {
		t.Fatal(err)
	}
	env, _ = executeHumanJSON(t, "bank-import", "preview", id, "--input", input, "--key", "preview")
	plan := env["data"].(map[string]any)
	pid := plan["id"].(string)
	digest := plan["digest"].(string)
	_ = executeHumanFailure(t, "bank-import", "apply", pid, "--digest", digest)
	executeHumanJSON(t, "bank-import", "apply", pid, "--digest", digest, "--commit")
	executeHumanJSON(t, "bank-import", "apply", pid, "--digest", digest, "--commit")
	env, _ = executeHumanJSON(t, "tx", "list")
	if len(env["data"].([]any)) != 1 {
		t.Fatal(env)
	}
	executeHumanJSON(t, "doctor")
}
