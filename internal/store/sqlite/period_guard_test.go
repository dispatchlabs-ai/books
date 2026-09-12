package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func directCloseBookPeriod(ctx context.Context, f fixture, bookCode, periodCode string) error {
	var bookID, periodID, endDate string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT b.id, fp.id, fp.end_date
		FROM books b
		JOIN book_periods bp ON bp.book_id = b.id
		JOIN fiscal_periods fp ON fp.id = bp.period_id
		WHERE b.code = ? AND fp.code = ?`, bookCode, periodCode).Scan(&bookID, &periodID, &endDate); err != nil {
		return err
	}
	digest, err := storesqlite.ComputeBookDigest(ctx, f.store.DB(), bookID, endDate)
	if err != nil {
		return err
	}
	_, err = f.store.DB().ExecContext(ctx, `UPDATE book_periods
		SET status = 'CLOSED', closed_at = '2026-08-04T00:00:00Z', closed_by = 'direct', close_digest = ?
		WHERE book_id = ? AND period_id = ? AND status = 'OPEN'`, digest, bookID, periodID)
	return err
}

func requireDirectCloseError(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("direct period close unexpectedly succeeded; want %q", message)
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("direct period close error = %v, want %q", err, message)
	}
}

func TestBookPeriodLifecycleEvidenceIsGuarded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET status = 'CLOSED'
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("close without lifecycle evidence unexpectedly succeeded")
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("service close: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE fiscal_periods SET end_date = '2026-07-30'
		WHERE code = '2026-07'`); err == nil {
		t.Fatal("closed fiscal-period dates unexpectedly changed")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET close_digest = ?
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`, strings.Repeat("a", 64)); err == nil {
		t.Fatal("closed-period evidence unexpectedly changed")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET status = 'OPEN'
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("reopen without audit evidence unexpectedly succeeded")
	}
	if err := f.service.ReopenPeriod(ctx, "ACME", "2026-07", "Correct July close"); err != nil {
		t.Fatalf("service reopen: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET reopen_reason = 'rewritten'
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("open-period lifecycle evidence unexpectedly changed outside a transition")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET period_id = period_id
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("book-period identity update unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM book_periods
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("book-period deletion unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET status = 'CLOSED'
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("period reclose with stale close evidence unexpectedly succeeded")
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("service reclose: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE book_periods SET status = 'OPEN'
		WHERE book_id = (SELECT id FROM books WHERE code = 'ACME')
		  AND period_id = (SELECT id FROM fiscal_periods WHERE code = '2026-07')`); err == nil {
		t.Fatal("period reopen with stale reopen evidence unexpectedly succeeded")
	}
}

func TestBookPeriodsMustBeReopenedInReverseChronologicalOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-08", StartDate: "2026-08-01", EndDate: "2026-08-31", FiscalYear: 2026, PeriodNumber: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-08", false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenPeriod(ctx, "ACME", "2026-07", "out-of-order attempt"); err == nil {
		t.Fatal("earlier period reopened while a later period remained closed")
	} else {
		requireAppErrorCode(t, err, "PERIOD_REOPEN_ORDER")
	}
	periods, err := f.service.ListPeriods(ctx, "ACME")
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range periods {
		if period.Code == "2026-07" && period.BookStatus != "CLOSED" {
			t.Fatalf("failed reopen changed July status to %s", period.BookStatus)
		}
	}
	if err := f.service.ReopenPeriod(ctx, "ACME", "2026-08", "unwind in reverse order"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenPeriod(ctx, "ACME", "2026-07", "historical correction"); err != nil {
		t.Fatal(err)
	}
}

func TestBookPeriodCloseTriggerRejectsAccountingPreflightBypasses(t *testing.T) {
	t.Run("inactive book", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.store.DB().Exec(`UPDATE books SET status = 'ARCHIVED' WHERE code = 'ACME'`); err != nil {
			t.Fatal(err)
		}
		requireDirectCloseError(t, directCloseBookPeriod(context.Background(), f, "ACME", "2026-07"), "book is not active")
	})

	t.Run("earlier open period", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		if _, err := f.service.CreatePeriod(ctx, ledger.CreatePeriodInput{
			Code: "2026-06", StartDate: "2026-06-01", EndDate: "2026-06-30", FiscalYear: 2026, PeriodNumber: 6,
		}); err != nil {
			t.Fatal(err)
		}
		requireDirectCloseError(t, directCloseBookPeriod(ctx, f, "ACME", "2026-07"), "earlier fiscal periods must be closed first")
	})

	t.Run("later closed period", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.CreatePeriod(ctx, ledger.CreatePeriodInput{
			Code: "2026-06", StartDate: "2026-06-01", EndDate: "2026-06-30", FiscalYear: 2026, PeriodNumber: 6,
		}); err != nil {
			t.Fatal(err)
		}
		requireDirectCloseError(t, directCloseBookPeriod(ctx, f, "ACME", "2026-06"), "later fiscal period is already closed")
	})

	t.Run("draft journal", func(t *testing.T) {
		f := newFixture(t)
		f.createJournal(t, 10_000, 10_000)
		requireDirectCloseError(t, directCloseBookPeriod(context.Background(), f, "ACME", "2026-07"), "unresolved draft journals")
	})

	t.Run("unbalanced trial balance", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		journal := f.createJournal(t, 10_000, 9_999)
		var triggerSQL string
		if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
			WHERE type = 'trigger' AND name = 'journal_entries_validate_post'`).Scan(&triggerSQL); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER journal_entries_validate_post`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries
			SET status = 'POSTED', posted_at = '2026-08-04T00:00:00Z', posted_by = 'tamper'
			WHERE id = ?`, journal.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().ExecContext(ctx, triggerSQL); err != nil {
			t.Fatal(err)
		}
		requireDirectCloseError(t, directCloseBookPeriod(ctx, f, "ACME", "2026-07"), "trial balance must balance")
	})

	t.Run("required reconciliation", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		createCashStatementAccount(t, f, true)
		requireDirectCloseError(t, directCloseBookPeriod(ctx, f, "ACME", "2026-07"), "required statement accounts must be reconciled")
	})

	t.Run("suspense balance", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		if _, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
			Code: "1999", Name: "Suspense", Type: "ASSET", Subtype: "SUSPENSE", BookCodes: []string{"ACME"},
		}); err != nil {
			t.Fatal(err)
		}
		journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Suspense item",
			Lines: []ledger.JournalLineInput{{Account: "1999", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.PostJournal(ctx, journal.ID); err != nil {
			t.Fatal(err)
		}
		requireDirectCloseError(t, directCloseBookPeriod(ctx, f, "ACME", "2026-07"), "suspense accounts must have zero balances")
	})
}

func TestYearEndDirectCloseRequiresClosedProfitAndLoss(t *testing.T) {
	ctx := context.Background()
	f := newYearCloseFixture(t)
	f.closeEarlierPeriod(t)
	base := fixture{store: f.store, service: f.service}
	requireDirectCloseError(t, directCloseBookPeriod(ctx, base, "TESTCO", "2025-12"), "profit-and-loss balances require an active closing journal")
}

func TestJournalValidationRejectsInactiveBookBeforePosting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	journal := f.createJournal(t, 10_000, 10_000)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE books SET status = 'ARCHIVED' WHERE code = 'ACME'`); err != nil {
		t.Fatal(err)
	}
	validation, err := f.service.ValidateJournal(ctx, journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid || !strings.Contains(strings.Join(validation.Errors, "; "), "book is not active") {
		t.Fatalf("validation missed inactive book: %+v", validation)
	}
	if _, err := f.service.PostJournal(ctx, journal.ID); err == nil {
		t.Fatal("posting to inactive book unexpectedly succeeded")
	}
}

func TestReconciliationLifecycleEvidenceIsGuarded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, false)
	reconciliation, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-31", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenReconciliation(ctx, reconciliation.ID, "Review July evidence"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations SET reopen_reason = 'rewritten'
		WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("open reconciliation reopen evidence unexpectedly changed")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations SET created_by = 'rewritten'
		WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("reconciliation creation evidence unexpectedly changed")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET status = 'COMPLETED', completed_at = '2026-08-04T00:00:00Z', completed_by = 'direct',
			reopen_reason = 'erased history'
		WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("completion unexpectedly rewrote prior reopen evidence")
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET status = 'OPEN', completed_at = NULL, completed_by = NULL
		WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("reconciliation reopen with stale evidence unexpectedly succeeded")
	}
	if err := f.service.ReopenReconciliation(ctx, reconciliation.ID, "Second evidence review"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatal(err)
	}
}
