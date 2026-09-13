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
	"github.com/dispatchlabs-ai/books/internal/application"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/operations"
)

func TestDatabaseAPI(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	config := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", config)
	t.Setenv("BOOKS_ACTOR", "database-api-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "account", "add", "bank", "Checking")
	registry, err := booksconfig.Load(config)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(config, "acme")
	if err != nil {
		t.Fatal(err)
	}
	principal := func(id, token string, company bool, grants []string) httpapi.Principal {
		h := sha256.Sum256([]byte(token))
		p := httpapi.Principal{ID: id, TokenSHA256: hex.EncodeToString(h[:])}
		if company {
			p.Companies = map[string][]string{"acme": {"read", "manage"}}
		} else {
			p.Databases = map[string][]string{"example": grants}
		}
		return p
	}
	owner := strings.Repeat("o", 32)
	reader := strings.Repeat("r", 32)
	company := strings.Repeat("c", 32)
	cfg := httpapi.Config{Schema: "books.server/v3", Listen: "127.0.0.1:0", Databases: map[string]httpapi.DatabaseConfig{"example": {Path: resolved.Database, UUID: resolved.Company.DatabaseUUID}}, Principals: []httpapi.Principal{principal("owner", owner, false, []string{"read", "manage"}), principal("reader", reader, false, []string{"read"}), principal("company", company, true, nil)}}
	server, err := httpapi.New(context.Background(), config, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	call := func(op, body, token string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/databases/example/operations/"+op, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %d: %s", op, w.Code, w.Body.String())
		}
		var env map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		return env
	}
	call("entity_list", "{}", company, 404)
	call("entity_create", `{"code":"CHILD","legal_name":"Child Example","currency":"USD"}`, reader, 404)
	call("entity_create", `{"code":"CHILD","legal_name":"Child Example","currency":"USD"}`, owner, 200)
	call("ownership_set", `{"parent":"ACME","child":"CHILD","from":"2026-01-01"}`, owner, 200)
	call("group_create", `{"code":"ALL","name":"Example Group","parent_entity":"ACME"}`, owner, 200)
	data := call("report_trial_balance", `{"group":"ALL","as_of":"2026-01-31","include_zero":true}`, reader, 200)["data"].(map[string]any)
	if data["scope"].(map[string]any)["kind"] != "GROUP" {
		t.Fatal(data)
	}
	call("entity_create", `{"code":"NO","path":"/tmp/escape"}`, owner, 400)
	call("entity_list", `{"unknown":1}`, reader, 400)
	call("entity_list", `{"unknown":1,"unknown":2}`, reader, 400)
	accounts := call("statement_account_list", `{"entity":"ACME"}`, owner, 200)["data"].([]any)
	code := accounts[0].(map[string]any)["code"].(string)
	identity := map[string]any{"statement_account": code, "source_system": "BANK", "source_realm": "EXAMPLE", "external_id": "account-1", "name": "Checking", "active": true, "evidence": map[string]any{"source_kind": "TEST", "source_path": "synthetic.json", "source_sha256": strings.Repeat("a", 64), "locator": "account-1"}, "dry_run": true}
	raw, _ := json.Marshal(identity)
	call("statement_account_identity_add", string(raw), owner, 200)
	identities, _ := call("statement_account_identity_list", `{}`, owner, 200)["data"].([]any)
	if len(identities) != 0 {
		t.Fatal("identity preview committed")
	}
	identity["dry_run"] = false
	raw, _ = json.Marshal(identity)
	call("statement_account_identity_add", string(raw), owner, 200)
	archive := map[string]any{"code": code, "reconciliation_required_through": "2026-01-31", "reason": "Synthetic preview", "dry_run": true}
	raw, _ = json.Marshal(archive)
	call("statement_account_archive", string(raw), owner, 200)
	accounts = call("statement_account_list", `{"entity":"ACME"}`, owner, 200)["data"].([]any)
	if accounts[0].(map[string]any)["status"] != "ACTIVE" {
		t.Fatal("archive preview committed")
	}
	// Typed policy rejects every write for a read-only grant before domain input.
	db, err := application.OpenDatabase(context.Background(), "example", resolved.Database, resolved.Company.DatabaseUUID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	seen := map[string]bool{}
	for _, op := range operations.DatabaseOperations() {
		d := op.Descriptor()
		if seen[d.ID] {
			t.Fatalf("duplicate %s", d.ID)
		}
		seen[d.ID] = true
		if d.Grant == "manage" {
			_, err := op.Execute(context.Background(), db, operations.ScopedDatabaseAccess("reader", "example", []string{"read"}), op.NewInput())
			e, ok := apperr.As(err)
			if !ok || e.Code != "DATABASE_NOT_FOUND" {
				t.Fatalf("%s bypass: %v", d.ID, err)
			}
		}
		if _, err := op.Execute(context.Background(), db, operations.ScopedDatabaseAccess("owner", "other", []string{"read", "manage"}), op.NewInput()); err == nil {
			t.Fatalf("%s wrong DB accepted", d.ID)
		}
	}

	imported := call("journal_import", `{"source_system":"TEST","source_name":"synthetic.json","file_sha256":"`+strings.Repeat("a", 64)+`","entity":"ACME","records":[{"journal":{"book":"ACME","posting_date":"2026-01-15","period":"2026-01","description":"Evidence import","source_key":"raw-evidence","lines":[{"account":"1000","debit_cents":"100"},{"account":"4000","credit_cents":"100"}]},"raw_json":{"external_id":9007199254740993,"nested":{"ok":true}}}]}`, owner, 200)
	if imported["data"].(map[string]any)["created_count"] != float64(1) {
		t.Fatal(imported)
	}
	sources := call("source_list", `{"source_account":"ACME"}`, reader, 200)["data"].([]any)
	if len(sources) != 1 {
		t.Fatal(sources)
	}
	rawDigest := sha256.Sum256([]byte(`{"external_id":9007199254740993,"nested":{"ok":true}}`))
	if sources[0].(map[string]any)["raw_json_sha256"] != hex.EncodeToString(rawDigest[:]) {
		t.Fatal("raw evidence changed")
	}
	bad := cfg
	bad.Schema = "books.server/v2"
	if bad.Validate() == nil {
		t.Fatal("old schema accepts database grants")
	}
	bad = cfg
	bad.Databases = map[string]httpapi.DatabaseConfig{"example": {Path: resolved.Database, UUID: "00000000-0000-0000-0000-000000000000"}}
	if s, err := httpapi.New(context.Background(), config, bad); err == nil {
		_ = s.Close()
		t.Fatal("identity mismatch accepted")
	}
}
