package report

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type hookedReportQueryer struct {
	delegate reportQueryer
	count    int
	before   func(int)
}

func (q *hookedReportQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.count++
	q.before(q.count)
	return q.delegate.QueryContext(ctx, query, args...)
}

func (q *hookedReportQueryer) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.count++
	q.before(q.count)
	return q.delegate.QueryRowContext(ctx, query, args...)
}

func TestTrialBalanceUsesOneSnapshotAcrossScopeAndPostingQueries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	writerStore, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writerStore.Close() })
	writer := ledger.NewService(writerStore, "writer")
	if _, err := writer.CreateEntity(ctx, ledger.CreateEntityInput{
		Code: "ACME", LegalName: "Acme, Inc.", Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-07", StartDate: "2026-07-01", EndDate: "2026-07-31",
		FiscalYear: 2026, PeriodNumber: 7,
	}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"ACME"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"ACME"}},
	} {
		if _, err := writer.CreateAccount(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	draft, err := writer.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Concurrent sale",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 5_000}, {Account: "4000", CreditCents: 5_000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var journalMode string
	if err := writerStore.DB().QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	readerStore, err := storesqlite.Open(ctx, path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readerStore.Close() })

	reports := NewService(readerStore)
	var writerErr error
	writerCommitted := false
	reports.decorateReader = func(delegate reportQueryer) reportQueryer {
		return &hookedReportQueryer{delegate: delegate, before: func(queryNumber int) {
			if queryNumber != 2 || writerCommitted {
				return
			}
			_, writerErr = writer.PostJournal(ctx, draft.ID)
			writerCommitted = writerErr == nil
		}}
	}

	before, err := reports.TrialBalance(ctx, TrialBalanceInput{
		Scope: Scope{EntityCode: "ACME"}, AsOfDate: "2026-07-31",
	})
	if writerErr != nil {
		t.Fatalf("commit concurrent journal: %v", writerErr)
	}
	if err != nil {
		t.Fatalf("report from original snapshot: %v", err)
	}
	if !writerCommitted {
		t.Fatal("writer was not committed between report queries")
	}
	if len(before.Rows) != 0 || before.TotalDebitCents != 0 || before.TotalCreditCents != 0 {
		t.Fatalf("report mixed in a later committed journal: %+v", before)
	}

	after, err := reports.TrialBalance(ctx, TrialBalanceInput{
		Scope: Scope{EntityCode: "ACME"}, AsOfDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("report after writer commit: %v", err)
	}
	if len(after.Rows) != 2 || after.TotalDebitCents != 5_000 || after.TotalCreditCents != 5_000 {
		t.Fatalf("new snapshot did not include committed journal: %+v", after)
	}
}
