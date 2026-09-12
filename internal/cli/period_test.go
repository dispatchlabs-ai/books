package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func yearCloseCLIDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	service := ledger.NewService(store, "test")
	for _, period := range []ledger.CreatePeriodInput{
		{Code: "2025-01", StartDate: "2025-01-01", EndDate: "2025-01-31", FiscalYear: 2025, PeriodNumber: 1},
		{Code: "2025-12", StartDate: "2025-12-01", EndDate: "2025-12-31", FiscalYear: 2025, PeriodNumber: 12, YearEnd: true},
	} {
		if _, err := service.CreatePeriod(ctx, period); err != nil {
			_ = store.Close()
			t.Fatalf("create period %s: %v", period.Code, err)
		}
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); err != nil {
		_ = store.Close()
		t.Fatalf("create entity: %v", err)
	}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"TESTCO"}},
		{Code: "3900", Name: "Retained Earnings", Type: "EQUITY", BookCodes: []string{"TESTCO"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"TESTCO"}},
		{Code: "5000", Name: "Expense", Type: "EXPENSE", BookCodes: []string{"TESTCO"}},
	} {
		if _, err := service.CreateAccount(ctx, account); err != nil {
			_ = store.Close()
			t.Fatalf("create account %s: %v", account.Code, err)
		}
	}
	for _, input := range []ledger.CreateJournalInput{
		{
			Book: "TESTCO", PostingDate: "2025-01-15", Period: "2025-01", Description: "Sale",
			Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 10_000}, {Account: "4000", CreditCents: 10_000}},
		},
		{
			Book: "TESTCO", PostingDate: "2025-12-20", Period: "2025-12", Description: "Expense",
			Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 4_000}, {Account: "1000", CreditCents: 4_000}},
		},
	} {
		journal, err := service.CreateJournal(ctx, input)
		if err != nil {
			_ = store.Close()
			t.Fatalf("create journal %q: %v", input.Description, err)
		}
		if _, err := service.PostJournal(ctx, journal.ID); err != nil {
			_ = store.Close()
			t.Fatalf("post journal %q: %v", input.Description, err)
		}
	}
	if _, err := service.ClosePeriod(ctx, "TESTCO", "2025-01", false); err != nil {
		_ = store.Close()
		t.Fatalf("close earlier period: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close setup database: %v", err)
	}
	return path
}

func TestYearCloseCLIDryRunAndPost(t *testing.T) {
	path := yearCloseCLIDatabase(t)
	dryRun := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json", "--dry-run",
		"period", "year-close", "--book", "testco", "--year", "2025", "--retained-earnings", "3900",
	)
	preview := dryRun["data"].(map[string]any)
	if preview["dry_run"] != true || preview["net_income_cents"] != "60.00" || preview["journal"] != nil {
		t.Fatalf("dry-run output = %#v", preview)
	}
	store, err := storesqlite.Open(context.Background(), path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatalf("open after dry run: %v", err)
	}
	var closingCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM journal_entries WHERE kind = 'CLOSING'`).Scan(&closingCount); err != nil {
		_ = store.Close()
		t.Fatalf("count closing journals after dry run: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close after dry run: %v", err)
	}
	if closingCount != 0 {
		t.Fatalf("dry run created %d closing journals", closingCount)
	}

	posted := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json",
		"period", "year-close", "--book", "testco", "--year", "2025", "--retained-earnings", "3900",
	)
	result := posted["data"].(map[string]any)
	journal, ok := result["journal"].(map[string]any)
	if result["dry_run"] != false || result["net_income_cents"] != "60.00" || !ok {
		t.Fatalf("post output = %#v", result)
	}
	if journal["kind"] != "CLOSING" || journal["status"] != "POSTED" || journal["reference"] != "YEAR-CLOSE-2025" {
		t.Fatalf("posted journal output = %#v", journal)
	}
}
