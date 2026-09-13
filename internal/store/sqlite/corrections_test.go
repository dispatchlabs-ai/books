package sqlite_test

import (
	"context"
	"sync"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func TestCorrectionRollsBackReplacementOnReversalConflict(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	original, err := f.service.GetJournalByNumber(ctx, "TESTCO", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ReverseAndRecordJournal(ctx, original.ID, "2025-12-20", "2025-12", "Separate reversal", true); err != nil {
		t.Fatal(err)
	}
	counts := func() (int, int) {
		t.Helper()
		var journals, audits int
		if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journal_entries").Scan(&journals); err != nil {
			t.Fatal(err)
		}
		if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&audits); err != nil {
			t.Fatal(err)
		}
		return journals, audits
	}
	beforeJ, beforeA := counts()
	_, err = f.service.CorrectJournal(ctx, ledger.CorrectionInput{OriginalID: original.ID, Reason: "Correct sale", Replacement: ledger.CreateJournalInput{Book: "TESTCO", PostingDate: "2025-12-20", Period: "2025-12", Description: "Corrected sale", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 9000}, {Account: "4000", CreditCents: 9000}}}})
	requireAppError(t, err, "REVERSAL_ALREADY_EXISTS", "")
	afterJ, afterA := counts()
	if beforeJ != afterJ || beforeA != afterA {
		t.Fatalf("partial correction: journals %d -> %d, audit %d -> %d", beforeJ, afterJ, beforeA, afterA)
	}
}

func TestConcurrentCorrectionsCommitOnePair(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	original, err := f.service.GetJournalByNumber(ctx, "TESTCO", 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, amount := range []int64{8000, 9000} {
		wg.Add(1)
		go func(amount int64) {
			defer wg.Done()
			<-start
			_, err := f.service.CorrectJournal(ctx, ledger.CorrectionInput{OriginalID: original.ID, Reason: "Correct sale", Replacement: ledger.CreateJournalInput{Book: "TESTCO", PostingDate: "2025-12-20", Period: "2025-12", Description: "Corrected sale", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: amount}, {Account: "4000", CreditCents: amount}}}})
			errs <- err
		}(amount)
	}
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireAppError(t, err, "CORRECTION_ALREADY_EXISTS", "")
		}
	}
	if success != 1 {
		t.Fatalf("successes %d", success)
	}
	var total, drafts int
	if err = f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*),SUM(CASE WHEN status='DRAFT' THEN 1 ELSE 0 END) FROM journal_entries").Scan(&total, &drafts); err != nil {
		t.Fatal(err)
	}
	if total != 4 || drafts != 0 {
		t.Fatalf("journals=%d drafts=%d", total, drafts)
	}
}

func TestYearClosePlanFreshnessCheckedBeforeWrites(t *testing.T) {
	f := newYearCloseFixture(t)
	ctx := context.Background()
	f.closeEarlierPeriod(t)
	input := ledger.FiscalYearCloseInput{Book: "TESTCO", FiscalYear: 2025, RetainedEarnings: "3900"}
	plan, err := f.service.PrepareFiscalYearClose(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, ledger.CreateJournalInput{Book: "TESTCO", PostingDate: "2025-12-21", Period: "2025-12", Description: "Late sale", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}}})
	_, err = f.service.PostFiscalYearCloseFromPlan(ctx, input, plan.Input)
	requireAppError(t, err, "YEAR_CLOSE_PLAN_STALE", "")
	var count int
	if err = f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journal_entries WHERE kind='CLOSING'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale plan created %d journals", count)
	}
}
