package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

const syntheticCSV = "Date,Amount,Description\n2026-07-15,123.45,Synthetic sale\n"
const syntheticProfile = `{"format":"CSV","institution":"FAKE","account_id":"FAKE-ACCOUNT","currency":"USD","date_layout":"2006-01-02","tabular":{"header_row":1,"date_column":"Date","amount_column":"Amount","description_columns":["Description"],"decimal_separator":"."}}`

func TestBankCSVCLIJourney(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	for name, raw := range map[string]string{"source.csv": syntheticCSV, "profile.json": syntheticProfile} {
		if e := os.WriteFile(filepath.Join(home, name), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	env, _ := executeHumanJSON(t, "bank-import", "formats")
	if len(env["data"].([]any)) != len(banking.Capabilities()) {
		t.Fatal("missing capabilities")
	}
	env, _ = executeHumanJSON(t, "bank-import", "upload", "--input", filepath.Join(home, "source.csv"), "--options", filepath.Join(home, "profile.json"), "--key", "csv-upload")
	job := env["data"].(map[string]any)
	if job["status"] != "READY" {
		t.Fatal(job)
	}
	account := job["document"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
	transaction := account["transactions"].([]any)[0].(map[string]any)["id"].(string)
	choices := ledger.BankImportChoices{Post: true, Mappings: []ledger.BankAccountMapping{{AccountKey: account["key"].(string), StatementAccount: "ACME-1000", Classifications: []ledger.BankClassification{{TransactionID: transaction, ContraAccount: "4000"}}}}}
	data, e := json.Marshal(choices)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(home, "choices.json")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	env, _ = executeHumanJSON(t, "bank-import", "matches", job["id"].(string), "--input", path)
	if len(env["data"].(map[string]any)["matches"].([]any)) != 0 {
		t.Fatal("unexpected candidates")
	}
	env, _ = executeHumanJSON(t, "bank-import", "preview", job["id"].(string), "--input", path, "--key", "csv-preview")
	plan := env["data"].(map[string]any)
	executeHumanJSON(t, "bank-import", "apply", plan["id"].(string), "--digest", plan["digest"].(string), "--commit")
	executeHumanJSON(t, "doctor")
}
func TestBankMultipartAPIAndReadOnlyMatches(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	token := strings.Repeat("synthetic-token-", 3)
	digest := sha256.Sum256([]byte(token))
	readerToken := strings.Repeat("synthetic-reader-", 3)
	readerDigest := sha256.Sum256([]byte(readerToken))
	config := httpapi.Config{Schema: "books.server/v1", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "synthetic-client", TokenSHA256: hex.EncodeToString(digest[:]), Companies: map[string][]string{"acme": {"read", "import", "post"}}}, {ID: "synthetic-reader", TokenSHA256: hex.EncodeToString(readerDigest[:]), Companies: map[string][]string{"acme": {"read"}}}}}
	server, e := httpapi.New(context.Background(), filepath.Join(home, "books.toml"), config)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = server.Close() }()
	request := func(method, path, contentType string, body []byte, key, match, credential string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Idempotency-Key", key)
		if match != "" {
			r.Header.Set("If-Match", `"`+match+`"`)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s=%d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	field, e := writer.CreateFormField("options")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = field.Write([]byte(syntheticProfile)); e != nil {
		t.Fatal(e)
	}
	file, e := writer.CreateFormFile("file", "synthetic.csv")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.Write([]byte(syntheticCSV)); e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	env := request("POST", "/v1/companies/acme/imports", writer.FormDataContentType(), b.Bytes(), "multipart", "", token, 202)
	job := env["data"].(map[string]any)
	id := job["id"].(string)
	if job["source_name"] != "synthetic.csv" {
		t.Fatal(job)
	}
	server.ProcessPending(context.Background())
	env = request("GET", "/v1/companies/acme/imports/"+id, "", nil, "", "", token, 200)
	job = env["data"].(map[string]any)
	if job["status"] != "READY" || job["options"].(map[string]any)["format"] != "CSV" {
		t.Fatal(job)
	}
	account := job["document"].(map[string]any)["accounts"].([]any)[0].(map[string]any)
	transaction := account["transactions"].([]any)[0].(map[string]any)["id"].(string)
	choices := ledger.BankImportChoices{Post: true, Mappings: []ledger.BankAccountMapping{{AccountKey: account["key"].(string), StatementAccount: "ACME-1000", Classifications: []ledger.BankClassification{{TransactionID: transaction, ContraAccount: "4000"}}}}}
	data, e := json.Marshal(choices)
	if e != nil {
		t.Fatal(e)
	}
	env = request("POST", "/v1/companies/acme/imports/"+id+"/matches", "application/json", data, "", "", readerToken, 200)
	if len(env["data"].(map[string]any)["matches"].([]any)) != 0 {
		t.Fatal(env)
	}
	request("POST", "/v1/companies/acme/imports/"+id+"/previews", "application/json", data, "read-cannot-write", "", readerToken, 403)
	env = request("POST", "/v1/companies/acme/imports/"+id+"/previews", "application/json", data, "multipart-plan", "", token, 201)
	plan := env["data"].(map[string]any)
	request("POST", "/v1/companies/acme/import-plans/"+plan["id"].(string)+"/apply", "", nil, "", plan["digest"].(string), token, 200)
	r := httptest.NewRequest("GET", "/v1/companies/acme/imports/"+id+"/source", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != syntheticCSV || !strings.Contains(w.Header().Get("Content-Disposition"), "synthetic.csv") {
		t.Fatalf("source download=%d %s", w.Code, w.Body.String())
	}
	env = request("GET", "/v1/capabilities", "", nil, "", "", token, 200)
	if len(env["data"].(map[string]any)["formats"].([]any)) != len(banking.Capabilities()) {
		t.Fatal("API capability list incomplete")
	}
	executeHumanJSON(t, "doctor")
}
