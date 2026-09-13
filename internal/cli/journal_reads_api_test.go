package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"math"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/application"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestJournalReadsAPI(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	config := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_CONFIG", config)
	t.Setenv("BOOKS_ACTOR", "journal-read-test")
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
	store, err := storesqlite.Open(context.Background(), resolved.Database, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := ledger.NewService(store, "journal-read-test")
	if _, err := svc.CreateEntity(context.Background(), ledger.CreateEntityInput{Code: "OTHER", LegalName: "Other Example", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, book := range []string{resolved.Company.BookCode, "OTHER"} {
		for _, code := range []string{"1000", "4000"} {
			if err := svc.ConfigureBookAccount(context.Background(), book, code, "2026-01-01", "", true); err != nil {
				t.Fatal(err)
			}
		}
		j, err := svc.CreateJournal(context.Background(), ledger.CreateJournalInput{Book: book, PostingDate: "2026-01-15", Period: "2026-01", Description: "Synthetic draft", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 9007199254740993}, {Account: "4000", CreditCents: 9007199254740993}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	app, err := application.Bind(context.Background(), store, resolved, "journal-read-test")
	if err != nil {
		t.Fatal(err)
	}
	testReportPolicy(t, app, operations.JournalShow(), application.JournalReadRequest{ID: ids[0]})
	testReportPolicy(t, app, operations.JournalValidate(), application.JournalReadRequest{ID: ids[0]})
	token := strings.Repeat("j", 32)
	sum := sha256.Sum256([]byte(token))
	server, err := httpapi.New(context.Background(), config, httpapi.Config{Schema: "books.server/v2", Listen: "127.0.0.1:0", Principals: []httpapi.Principal{{ID: "reader", TokenSHA256: hex.EncodeToString(sum[:]), Companies: map[string][]string{"acme": {"read"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	get := func(path, credential string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	base := "/v1/companies/acme/journals/"
	for _, suffix := range []string{"", "/validation"} {
		data := get(base+ids[0]+suffix, token, 200)["data"].(map[string]any)
		key := "total_debit_cents"
		cmd := "show"
		if suffix != "" {
			key = "debit_cents"
			cmd = "validate"
			if data["valid"] != true {
				t.Fatal(data)
			}
		}
		if data[key] != "9007199254740993" {
			t.Fatalf("lost precision: %v", data)
		}
		cli, _ := executeHumanJSON(t, "journal", cmd, "--id", ids[0])
		if cli["data"].(map[string]any)[key] != "90071992547409.93" {
			t.Fatal("CLI amount mismatch")
		}
		foreign := get(base+ids[1]+suffix, token, 404)
		missing := get(base+"missing"+suffix, token, 404)
		if foreign["error"].(map[string]any)["code"] != "JOURNAL_NOT_FOUND" || missing["error"].(map[string]any)["code"] != "JOURNAL_NOT_FOUND" {
			t.Fatal("existence leakage")
		}
		get(base+ids[0]+suffix, "", 401)
		get(strings.Replace(base, "/acme/", "/other/", 1)+ids[0]+suffix, token, 404)
		get(base+ids[0]+suffix+"?book=OTHER", token, 400)
	}
	j, err := svc.GetJournal(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "DRAFT" {
		t.Fatal("validation posted draft")
	}
	// Invalid accounting is a successful validation response with valid=false.
	invalid, err := svc.CreateJournal(context.Background(), ledger.CreateJournalInput{Book: resolved.Company.BookCode, PostingDate: "2026-01-15", Period: "2026-01", Description: "Unbalanced draft", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	result := get(base+invalid.ID+"/validation", token, 200)["data"].(map[string]any)
	if result["valid"] != false || len(result["errors"].([]any)) == 0 {
		t.Fatal("invalid draft not explained")
	}
	// Cross-book reversal references must not inspect foreign target metadata.
	cross, err := svc.CreateJournal(context.Background(), ledger.CreateJournalInput{Book: resolved.Company.BookCode, PostingDate: "2026-01-15", Period: "2026-01", Description: "Cross book reversal", ReversalOfID: ids[1], Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	crossResult := get(base+cross.ID+"/validation", token, 200)["data"].(map[string]any)
	errors := crossResult["errors"].([]any)
	if crossResult["valid"] != false || len(errors) != 1 || errors[0] != "reversal target must belong to the same book" {
		t.Fatalf("foreign validation leakage: %v", crossResult)
	}
	// Reject aggregate overflow atomically, even though each line fits int64.
	for _, credit := range []bool{false, true} {
		lines := []ledger.JournalLineInput{{Account: "1000", DebitCents: math.MaxInt64}, {Account: "1000", DebitCents: 1}}
		if credit {
			for i := range lines {
				lines[i].CreditCents = lines[i].DebitCents
				lines[i].DebitCents = 0
			}
		}
		_, err := svc.CreateJournal(context.Background(), ledger.CreateJournalInput{Book: resolved.Company.BookCode, PostingDate: "2026-01-15", Period: "2026-01", Description: "Overflow draft", SourceSystem: "TEST", SourceKey: "overflow", Lines: lines})
		e, ok := apperr.As(err)
		if !ok || e.Code != "AMOUNT_OVERFLOW" {
			t.Fatalf("overflow not rejected: %v", err)
		}
	}
	// The same idempotency key remains available after both failed creations.
	if _, err := svc.CreateJournal(context.Background(), ledger.CreateJournalInput{Book: resolved.Company.BookCode, PostingDate: "2026-01-15", Period: "2026-01", Description: "Recovered draft", SourceSystem: "TEST", SourceKey: "overflow", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 1}}}); err != nil {
		t.Fatal(err)
	}

}
