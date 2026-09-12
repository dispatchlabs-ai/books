package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type yearCloseFixture struct {
	store   *storesqlite.Store
	service *ledger.Service
}

func newYearCloseFixture(t *testing.T) yearCloseFixture {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := ledger.NewService(store, "test")
	for _, period := range []ledger.CreatePeriodInput{
		{Code: "2025-01", StartDate: "2025-01-01", EndDate: "2025-01-31", FiscalYear: 2025, PeriodNumber: 1},
		{Code: "2025-12", StartDate: "2025-12-01", EndDate: "2025-12-31", FiscalYear: 2025, PeriodNumber: 12, YearEnd: true},
	} {
		if _, err := service.CreatePeriod(ctx, period); err != nil {
			t.Fatalf("create period %s: %v", period.Code, err)
		}
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); err != nil {
		t.Fatalf("create entity: %v", err)
	}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"TESTCO"}},
		{Code: "3900", Name: "Retained Earnings", Type: "EQUITY", BookCodes: []string{"TESTCO"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"TESTCO"}},
		{Code: "5000", Name: "Expense", Type: "EXPENSE", BookCodes: []string{"TESTCO"}},
	} {
		if _, err := service.CreateAccount(ctx, account); err != nil {
			t.Fatalf("create account %s: %v", account.Code, err)
		}
	}
	fixture := yearCloseFixture{store: store, service: service}
	fixture.post(t, ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-01-15", Period: "2025-01", Description: "January sale",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 10_000},
			{Account: "4000", CreditCents: 10_000},
		},
	})
	fixture.post(t, ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-20", Period: "2025-12", Description: "December expense",
		Lines: []ledger.JournalLineInput{
			{Account: "5000", DebitCents: 4_000},
			{Account: "1000", CreditCents: 4_000},
		},
	})
	return fixture
}

func (f yearCloseFixture) post(t *testing.T, input ledger.CreateJournalInput) ledger.Journal {
	t.Helper()
	journal, err := f.service.CreateJournal(context.Background(), input)
	if err != nil {
		t.Fatalf("create journal %q: %v", input.Description, err)
	}
	posted, err := f.service.PostJournal(context.Background(), journal.ID)
	if err != nil {
		t.Fatalf("post journal %q: %v", input.Description, err)
	}
	return posted
}

func (f yearCloseFixture) closeEarlierPeriod(t *testing.T) {
	t.Helper()
	if _, err := f.service.ClosePeriod(context.Background(), "TESTCO", "2025-01", false); err != nil {
		t.Fatalf("close earlier period: %v", err)
	}
}

func requireAppError(t *testing.T, err error, code, messageFragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("operation unexpectedly succeeded; want %s", code)
	}
	appError, ok := apperr.As(err)
	if !ok || appError.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
	if messageFragment != "" && !strings.Contains(appError.Message, messageFragment) {
		t.Fatalf("error message = %q, want fragment %q", appError.Message, messageFragment)
	}
}

func TestFiscalYearCloseRejectsNonExactBalances(t *testing.T) {
	tests := []struct {
		name  string
		lines []ledger.JournalLineInput
	}{
		{
			name: "partial revenue close",
			lines: []ledger.JournalLineInput{
				{Account: "4000", DebitCents: 9_999},
				{Account: "5000", CreditCents: 4_000},
				{Account: "3900", CreditCents: 5_999},
			},
		},
		{
			name: "expense closed in wrong direction",
			lines: []ledger.JournalLineInput{
				{Account: "4000", DebitCents: 10_000},
				{Account: "5000", DebitCents: 4_000},
				{Account: "3900", CreditCents: 14_000},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			f := newYearCloseFixture(t)
			f.closeEarlierPeriod(t)
			journal, err := f.service.CreateJournal(context.Background(), ledger.CreateJournalInput{
				Book: "TESTCO", Kind: "CLOSING", PostingDate: "2025-12-31", Period: "2025-12",
				Description: "Incorrect close", Lines: testCase.lines,
			})
			if err != nil {
				t.Fatalf("create closing journal: %v", err)
			}
			validation, err := f.service.ValidateJournal(context.Background(), journal.ID)
			if err != nil {
				t.Fatalf("validate closing journal: %v", err)
			}
			if validation.Valid || !strings.Contains(strings.Join(validation.Errors, "; "), "exactly zero") {
				t.Fatalf("validation = %+v, want exact-balance error", validation)
			}
			_, err = f.service.PostJournal(context.Background(), journal.ID)
			requireAppError(t, err, "JOURNAL_INVALID", "exactly zero")
		})
	}
}

func TestFiscalYearCloseRequiresPriorPeriodsClosed(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	_, err := f.service.PrepareFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	})
	requireAppError(t, err, "FISCAL_YEAR_CLOSE_BLOCKED", "earlier fiscal-year periods must be closed")
	var closingDrafts int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries
		WHERE kind = 'CLOSING' AND status = 'DRAFT'`).Scan(&closingDrafts); err != nil {
		t.Fatal(err)
	}
	if closingDrafts != 0 {
		t.Fatalf("blocked fiscal-year close left %d closing drafts", closingDrafts)
	}

	f.closeEarlierPeriod(t)
	result, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	}, false)
	if err != nil {
		t.Fatalf("prepare and post exact close after closing prior periods: %v", err)
	}
	if result.Journal == nil || result.Journal.Kind != "CLOSING" || result.Journal.Status != "POSTED" {
		t.Fatalf("posted close = %+v", result.Journal)
	}
}

func TestFiscalYearCloseAllowsNonzeroActivityWithZeroNetIncome(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	f.post(t, ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-21", Period: "2025-12", Description: "Break-even expense",
		Lines: []ledger.JournalLineInput{
			{Account: "5000", DebitCents: 6_000},
			{Account: "1000", CreditCents: 6_000},
		},
	})
	f.closeEarlierPeriod(t)

	prepared, err := f.service.PrepareFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	})
	if err != nil {
		t.Fatalf("prepare break-even close: %v", err)
	}
	if prepared.NetIncome != 0 {
		t.Fatalf("net income = %d, want 0", prepared.NetIncome)
	}
	if len(prepared.Input.Lines) != 2 {
		t.Fatalf("break-even close lines = %+v, want only revenue and expense lines", prepared.Input.Lines)
	}
	for _, line := range prepared.Input.Lines {
		if line.Account == "3900" {
			t.Fatalf("break-even close contains retained-earnings line: %+v", prepared.Input.Lines)
		}
	}

	result, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	}, false)
	if err != nil {
		t.Fatalf("post break-even close: %v", err)
	}
	if result.Journal == nil || result.Journal.Status != "POSTED" || len(result.Journal.Lines) != 2 {
		t.Fatalf("posted break-even close = %+v", result)
	}
	if _, err := f.service.ClosePeriod(ctx, "TESTCO", "2025-12", false); err != nil {
		t.Fatalf("close break-even year-end period: %v", err)
	}
}

func TestYearEndPeriodCloseRequiresZeroProfitAndLossBalances(t *testing.T) {
	t.Run("nonzero balances require a closing journal", func(t *testing.T) {
		f := newYearCloseFixture(t)
		ctx := context.Background()
		f.closeEarlierPeriod(t)
		_, err := f.service.ClosePeriod(ctx, "TESTCO", "2025-12", false)
		requireAppError(t, err, "PERIOD_CLOSE_BLOCKED", "require an active closing journal")

		result, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
			Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
		}, false)
		if err != nil {
			t.Fatalf("post fiscal-year close: %v", err)
		}
		if result.Journal == nil || result.Journal.Status != "POSTED" {
			t.Fatalf("fiscal-year close = %+v", result)
		}
		if _, err := f.service.ClosePeriod(ctx, "TESTCO", "2025-12", false); err != nil {
			t.Fatalf("close year-end after closing P&L: %v", err)
		}
	})

	t.Run("zero profit and loss year needs no synthetic close", func(t *testing.T) {
		ctx := context.Background()
		store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
		if err != nil {
			t.Fatalf("init database: %v", err)
		}
		defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
		service := ledger.NewService(store, "test")
		if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
			Code: "2025-12", StartDate: "2025-12-01", EndDate: "2025-12-31",
			FiscalYear: 2025, PeriodNumber: 12, YearEnd: true,
		}); err != nil {
			t.Fatalf("create year-end period: %v", err)
		}
		if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{
			Code: "DORMANT", LegalName: "Dormant Co", Currency: "USD",
		}); err != nil {
			t.Fatalf("create dormant entity: %v", err)
		}
		result, err := service.ClosePeriod(ctx, "DORMANT", "2025-12", false)
		if err != nil {
			t.Fatalf("close zero-P&L year: %v", err)
		}
		if !result.Closed {
			t.Fatalf("zero-P&L close result = %+v", result)
		}
	})
}

func TestFiscalYearCloseReopenCorrectionAndReclose(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	f.closeEarlierPeriod(t)

	first, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	}, false)
	if err != nil {
		t.Fatalf("post first fiscal-year close: %v", err)
	}
	if first.Journal == nil || first.Journal.Status != "POSTED" || first.NetIncome != 6_000 {
		t.Fatalf("first close = %+v, want posted 6000 net income", first)
	}
	if _, err := f.service.ClosePeriod(ctx, "TESTCO", "2025-12", false); err != nil {
		t.Fatalf("close year-end period: %v", err)
	}
	if err := f.service.ReopenPeriod(ctx, "TESTCO", "2025-12", "Record year-end correction"); err != nil {
		t.Fatalf("reopen year-end period: %v", err)
	}

	blocked, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-31", Period: "2025-12", Description: "Premature correction",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 1_000},
			{Account: "4000", CreditCents: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("create premature correction: %v", err)
	}
	_, err = f.service.PostJournal(ctx, blocked.ID)
	requireAppError(t, err, "JOURNAL_INVALID", "closing journal must be reopened")
	if err := f.service.AbandonJournal(ctx, blocked.ID); err != nil {
		t.Fatalf("abandon blocked correction: %v", err)
	}

	reversal, err := f.service.ReverseJournal(ctx, first.Journal.ID, "2025-12-31", "2025-12", "Reopen 2025 close")
	if err != nil {
		t.Fatalf("create closing reversal: %v", err)
	}
	reversal, err = f.service.PostJournal(ctx, reversal.ID)
	if err != nil {
		t.Fatalf("post closing reversal: %v", err)
	}
	if reversal.Kind != "CLOSING_REVERSAL" || reversal.ReversalOfID != first.Journal.ID {
		t.Fatalf("closing reversal = %+v", reversal)
	}

	f.post(t, ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-31", Period: "2025-12", Description: "Year-end correction",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 1_000},
			{Account: "4000", CreditCents: 1_000},
		},
	})
	second, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	}, false)
	if err != nil {
		t.Fatalf("post replacement fiscal-year close: %v", err)
	}
	if second.Journal == nil || second.Journal.ID == first.Journal.ID || second.NetIncome != 7_000 {
		t.Fatalf("replacement close = %+v, want a new 7000 close", second)
	}
	if _, err := f.service.ClosePeriod(ctx, "TESTCO", "2025-12", false); err != nil {
		t.Fatalf("reclose corrected year-end period: %v", err)
	}

	var activeCloses int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*)
		FROM journal_entries close_entry
		WHERE close_entry.kind = 'CLOSING' AND close_entry.status = 'POSTED'
		  AND NOT EXISTS (
			SELECT 1 FROM journal_entries reversal
			WHERE reversal.reversal_of_id = close_entry.id
			  AND reversal.kind = 'CLOSING_REVERSAL' AND reversal.status = 'POSTED'
		  )`).Scan(&activeCloses); err != nil {
		t.Fatalf("count active closes: %v", err)
	}
	if activeCloses != 1 {
		t.Fatalf("active closes = %d, want 1", activeCloses)
	}
	balances := map[string]int64{}
	rows, err := f.store.DB().QueryContext(ctx, `SELECT a.code, SUM(jl.debit_cents - jl.credit_cents)
		FROM journal_entries je
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.status = 'POSTED' AND a.code IN ('3900', '4000', '5000')
		GROUP BY a.code`)
	if err != nil {
		t.Fatalf("read post-close balances: %v", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var code string
		var balance int64
		if err := rows.Scan(&code, &balance); err != nil {
			t.Fatalf("scan post-close balance: %v", err)
		}
		balances[code] = balance
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read post-close balances: %v", err)
	}
	if balances["4000"] != 0 || balances["5000"] != 0 || balances["3900"] != -7_000 {
		t.Fatalf("post-close balances = %#v, want P&L zero and retained earnings -7000", balances)
	}
}

func TestAbandonedClosingReversalCanBeRetried(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	f.closeEarlierPeriod(t)
	closeResult, err := f.service.PostFiscalYearClose(ctx, ledger.FiscalYearCloseInput{
		Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900",
	}, false)
	if err != nil {
		t.Fatalf("post fiscal-year close: %v", err)
	}

	first, err := f.service.ReverseJournal(ctx, closeResult.Journal.ID, "2025-12-31", "2025-12", "First reversal attempt")
	if err != nil {
		t.Fatalf("create first reversal: %v", err)
	}
	if err := f.service.AbandonJournal(ctx, first.ID); err != nil {
		t.Fatalf("abandon first reversal: %v", err)
	}
	second, err := f.service.ReverseJournal(ctx, closeResult.Journal.ID, "2025-12-31", "2025-12", "Second reversal attempt")
	if err != nil {
		t.Fatalf("retry reversal after abandon: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("retry returned the abandoned reversal")
	}
	posted, err := f.service.PostJournal(ctx, second.ID)
	if err != nil {
		t.Fatalf("post retried reversal: %v", err)
	}
	if posted.Status != "POSTED" || posted.Kind != "CLOSING_REVERSAL" {
		t.Fatalf("retried reversal = %+v", posted)
	}
}

func TestSourceDerivedDraftIsImmutable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Imported sale",
		SourceSystem: "QBO", SourceKey: "invoice:immutable",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 1_000},
			{Account: "4000", CreditCents: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("create source-derived draft: %v", err)
	}
	_, err = f.service.ReplaceDraft(ctx, journal.ID, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Tampered sale",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 2_000},
			{Account: "4000", CreditCents: 2_000},
		},
	})
	requireAppError(t, err, "SOURCE_DRAFT_IMMUTABLE", "cannot be edited")
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries SET description = 'direct tamper' WHERE id = ?`, journal.ID); err == nil {
		t.Fatal("direct source-derived journal header mutation unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_lines SET debit_cents = 999 WHERE journal_entry_id = ? AND line_number = 1`, journal.ID); err == nil {
		t.Fatal("direct source-derived journal line mutation unexpectedly succeeded")
	}
	unchanged, err := f.service.GetJournal(ctx, journal.ID)
	if err != nil {
		t.Fatalf("read source-derived draft: %v", err)
	}
	if unchanged.Description != "Imported sale" || unchanged.Lines[0].DebitCents != 1_000 {
		t.Fatalf("source-derived draft changed: %+v", unchanged)
	}
}
