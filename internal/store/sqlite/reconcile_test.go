package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func createCashStatementAccount(t *testing.T, f fixture, required bool) ledger.StatementAccount {
	t.Helper()
	account, err := f.service.CreateStatementAccount(context.Background(), ledger.CreateStatementAccountInput{
		Code:                       "ACME-CASH",
		Entity:                     "ACME",
		Book:                       "ACME",
		GLAccount:                  "1000",
		Name:                       "Acme Cash",
		Kind:                       "BANK",
		Currency:                   "USD",
		RequiredForClose:           required,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		t.Fatalf("create statement account: %v", err)
	}
	return account
}

func TestReconciliationSignedManyToManyAllocationsAndStaleGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, true)

	journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-10", Period: "2026-07", Description: "Batched receipts",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: 6_000},
			{Account: "1000", DebitCents: 9_000},
			{Account: "4000", CreditCents: 15_000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err = f.service.PostJournal(ctx, journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cashLines []string
	for _, line := range journal.Lines {
		if line.AccountCode == "1000" {
			cashLines = append(cashLines, line.ID)
		}
	}
	if len(cashLines) != 2 {
		t.Fatalf("cash lines = %d, want 2", len(cashLines))
	}

	_, err = f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH",
		SourceSystem:     "BANK",
		SourceName:       "july.csv",
		FileSHA256:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Transactions: []ledger.StatementTransactionInput{
			{ExternalID: "receipt-1", PostedDate: "2026-07-10", Description: "Receipt one", AmountCents: 10_000},
			{ExternalID: "receipt-2", PostedDate: "2026-07-10", Description: "Receipt two", AmountCents: 5_000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	transactionIDs := map[string]string{}
	rows, err := f.store.DB().QueryContext(ctx, `SELECT si.external_id, st.id
		FROM statement_transactions st JOIN source_identities si ON si.id = st.source_identity_id
		ORDER BY si.external_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var externalID, id string
		if err := rows.Scan(&externalID, &id); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		transactionIDs[externalID] = id
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	reconciliation, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-01", "2026-07-31", 0, 15_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AllocateReconciliation(ctx, reconciliation.ID, transactionIDs["receipt-1"], cashLines[0], 6_000); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AllocateReconciliation(ctx, reconciliation.ID, transactionIDs["receipt-1"], cashLines[1], 4_000); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
        SET status = 'COMPLETED', completed_at = '2026-08-04T00:00:00Z', completed_by = 'direct'
        WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("direct completion with incomplete allocations unexpectedly succeeded")
	}
	if _, err := f.service.AllocateReconciliation(ctx, reconciliation.ID, transactionIDs["receipt-2"], cashLines[1], 6_000); err == nil {
		t.Fatal("over-allocation unexpectedly succeeded")
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err == nil {
		t.Fatal("completion with a partially allocated statement and GL line unexpectedly succeeded")
	}
	if _, err := f.service.AllocateReconciliation(ctx, reconciliation.ID, transactionIDs["receipt-2"], cashLines[1], 5_000); err != nil {
		t.Fatal(err)
	}
	completed, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false)
	if err != nil {
		t.Fatalf("complete reconciliation: %v", err)
	}
	if completed.Status != "COMPLETED" || completed.StatementTransactionCount != 2 ||
		completed.FullyAllocatedStatementCount != 2 || completed.ControlLineCount != 2 ||
		completed.FullyAllocatedControlLineCount != 2 || completed.AllocationCount != 3 {
		t.Fatalf("unexpected completed status: %+v", completed)
	}
	allocations, err := f.service.ListReconciliationAllocations(ctx, reconciliation.ID)
	if err != nil || len(allocations) != 3 {
		t.Fatalf("list allocations = %d, %v", len(allocations), err)
	}

	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH",
		SourceSystem:     "BANK",
		SourceName:       "late-july.csv",
		FileSHA256:       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Transactions: []ledger.StatementTransactionInput{
			{ExternalID: "late-receipt", PostedDate: "2026-07-20", Description: "Late receipt", AmountCents: 100},
		},
	}); err == nil {
		t.Fatal("statement import into a completed reconciliation unexpectedly succeeded")
	}
	held, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH",
		SourceSystem:     "BANK",
		SourceName:       "held-july.csv",
		FileSHA256:       "abababababababababababababababababababababababababababababababab",
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "held-receipt", PostedDate: "2026-07-20", Description: "Held receipt", AmountCents: 100,
			Disposition: ledger.SourceDispositionNeedsReview, ExclusionReason: "duplicate candidate",
		}},
	})
	if err != nil || held.SourceOnlyCount != 1 || held.StatementTransactionCount != 0 {
		t.Fatalf("preserve held source after reconciliation: %+v, %v", held, err)
	}
	unchanged, err := f.service.ReconciliationStatus(ctx, reconciliation.ID)
	if err != nil || unchanged.StatementTransactionCount != 2 {
		t.Fatalf("held source changed reconciliation population: %+v, %v", unchanged, err)
	}

	lateJournal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-20", Period: "2026-07", Description: "Late cash posting",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	validation, err := f.service.ValidateJournal(ctx, lateJournal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid || !strings.Contains(strings.Join(validation.Errors, "; "), "completed reconciliation") {
		t.Fatalf("posting validation missed completed reconciliation control: %+v", validation)
	}
	if _, err := f.service.PostJournal(ctx, lateJournal.ID); err == nil {
		t.Fatal("control-account posting before a completed reconciliation end unexpectedly succeeded")
	}
	if err := f.service.AbandonJournal(ctx, lateJournal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("close reconciled period: %v", err)
	}
	if err := f.service.ReopenReconciliation(ctx, reconciliation.ID, "rework allocation"); err == nil {
		t.Fatal("reopening a reconciliation under a closed period unexpectedly succeeded")
	} else {
		requireAppErrorCode(t, err, "RECONCILIATION_REOPEN_PERIOD_CLOSED")
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor: %v", err)
	}
}

func TestStatementAccountCurrencyTypeAndUniquenessConstraints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
		Code: "2000", Name: "Credit Card", Type: "LIABILITY", BookCodes: []string{"ACME"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
		Code: "1100", Name: "Reserve Cash", Type: "ASSET", BookCodes: []string{"ACME"},
	}); err != nil {
		t.Fatal(err)
	}
	invalid := []ledger.CreateStatementAccountInput{
		{Code: "BAD-REVENUE-BANK", Entity: "ACME", Book: "ACME", GLAccount: "4000", Name: "Bad", Kind: "BANK", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01"},
		{Code: "BAD-EUR-BANK", Entity: "ACME", Book: "ACME", GLAccount: "1000", Name: "Bad", Kind: "BANK", Currency: "EUR", ReconciliationRequiredFrom: "2026-07-01"},
		{Code: "BAD-ASSET-CARD", Entity: "ACME", Book: "ACME", GLAccount: "1000", Name: "Bad", Kind: "CREDIT_CARD", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01"},
	}
	for _, input := range invalid {
		if _, err := f.service.CreateStatementAccount(ctx, input); err == nil {
			t.Fatalf("invalid statement account %s unexpectedly succeeded", input.Code)
		}
	}
	cash := createCashStatementAccount(t, f, false)
	if _, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "DUPLICATE-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Duplicate", Kind: "BANK", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01",
	}); err == nil {
		t.Fatal("second statement account for one control account unexpectedly succeeded")
	}
	if _, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-CARD", Entity: "ACME", Book: "ACME", GLAccount: "2000",
		Name: "Card", Kind: "CREDIT_CARD", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01",
	}); err != nil {
		t.Fatalf("valid liability statement account: %v", err)
	}
	filtered, err := f.service.ListStatementAccounts(ctx, "acme")
	if err != nil || len(filtered) != 2 {
		t.Fatalf("filtered statement accounts = %d, %v", len(filtered), err)
	}

	var entityID, bookID, revenueID, cashAccountID, reserveAccountID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM entities WHERE code = 'ACME'`).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM books WHERE code = 'ACME'`).Scan(&bookID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM accounts WHERE code = '4000'`).Scan(&revenueID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM accounts WHERE code = '1000'`).Scan(&cashAccountID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM accounts WHERE code = '1100'`).Scan(&reserveAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_accounts
		(id, code, entity_id, book_id, gl_account_id, name, account_kind, currency, reconciliation_required_from, created_at)
		VALUES ('bad-direct', 'BAD-DIRECT', ?, ?, ?, 'Bad direct', 'BANK', 'USD', '2026-07-01', '2026-08-04T00:00:00Z')`,
		entityID, bookID, revenueID); err == nil {
		t.Fatal("direct-SQL BANK-to-REVENUE statement account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts SET currency = 'EUR' WHERE id = ?`, cash.ID); err == nil {
		t.Fatal("direct-SQL currency mismatch update unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE accounts SET account_type = 'LIABILITY', normal_balance = 'CREDIT' WHERE id = ?`, cashAccountID); err == nil {
		t.Fatal("direct-SQL reclassification of an assigned control account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE accounts SET subtype = 'SUSPENSE' WHERE id = ?`, cashAccountID); err == nil {
		t.Fatal("direct-SQL subtype reclassification of an assigned control account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE accounts SET statement_section = 'OTHER_ASSET' WHERE id = ?`, cashAccountID); err == nil {
		t.Fatal("direct-SQL statement-section reclassification of an assigned control account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_accounts
		(id, code, entity_id, book_id, gl_account_id, name, account_kind, currency, reconciliation_required_from, created_at)
		VALUES ('duplicate-direct', 'DUPLICATE-DIRECT', ?, ?, ?, 'Duplicate direct', 'BANK', 'USD', '2026-07-01', '2026-08-04T00:00:00Z')`,
		entityID, bookID, cashAccountID); err == nil {
		t.Fatal("direct-SQL duplicate control-account assignment unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_accounts
		(id, code, entity_id, book_id, gl_account_id, name, account_kind, currency,
		 reconciliation_required_from, reconciliation_required_through, status,
		 archived_at, archived_by, archive_reason, created_at)
		VALUES ('archived-direct', 'ARCHIVED-DIRECT', ?, ?, ?, 'Archived direct', 'BANK', 'USD',
		        '2026-07-01', '2026-07-31', 'ARCHIVED', '2026-08-04T00:00:00Z',
		        'direct', 'bypass', '2026-08-04T00:00:00Z')`, entityID, bookID, reserveAccountID); err == nil {
		t.Fatal("direct-SQL initially archived statement account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts SET created_at = '2026-08-05T00:00:00Z' WHERE id = ?`, cash.ID); err == nil {
		t.Fatal("direct-SQL statement account creation timestamp mutation unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM statement_accounts WHERE id = ?`, cash.ID); err == nil {
		t.Fatal("direct-SQL statement account deletion unexpectedly succeeded")
	}
}

func TestStatementAccountArchiveRequiresEvidenceAndBlocksNewActivity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)
	if _, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{Code: account.Code}); err == nil {
		t.Fatal("archive without a reason unexpectedly succeeded")
	}
	archived, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{
		Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Bank confirmation shows account closed before cutover",
	})
	if err != nil {
		t.Fatalf("archive statement account: %v", err)
	}
	if archived.Status != "ARCHIVED" || archived.ArchivedAt == "" || archived.ArchivedBy == "" || archived.ArchiveReason == "" {
		t.Fatalf("incomplete archive evidence: %+v", archived)
	}
	if archived.ReconciliationRequiredThrough != "2026-07-31" {
		t.Fatalf("archive reconciliation-required-through = %q", archived.ReconciliationRequiredThrough)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("archive unexpectedly bypassed reconciliation for its final active period")
	}
	listed, err := f.service.ListStatementAccounts(ctx, "ACME")
	if err != nil || len(listed) != 1 || listed[0].Status != "ARCHIVED" || listed[0].ArchiveReason != archived.ArchiveReason {
		t.Fatalf("archived account list = %+v, %v", listed, err)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: account.Code, SourceSystem: "BANK", SourceName: "after-close.csv",
		FileSHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Transactions: []ledger.StatementTransactionInput{{ExternalID: "late", PostedDate: "2026-07-01", Description: "Late", AmountCents: 1}},
	}); err == nil {
		t.Fatal("import into archived statement account unexpectedly succeeded")
	}
	if _, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-31", 0, 0); err == nil {
		t.Fatal("reconciliation for archived statement account unexpectedly succeeded")
	}
	if _, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Again"}); err == nil {
		t.Fatal("second archive unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts SET required_for_close = 0 WHERE id = ?`, account.ID); err == nil {
		t.Fatal("direct close-policy change unexpectedly succeeded")
	}
}

func TestStatementAccountArchiveEffectiveDateEndsFutureCloseCoverage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Acme Cash", Kind: "BANK", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-06-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{
		Code: account.Code, ReconciliationRequiredThrough: "2026-06-30", Reason: "Closed and fully reconciled before system control began",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err != nil {
		t.Fatalf("expired statement account blocked later period: %v", err)
	}
}

func TestStatementAccountArchiveRejectsOpenReconciliation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)
	if _, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-31", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = '2026-07-31',
		    archived_at = '2026-08-04T00:00:00Z', archived_by = 'direct', archive_reason = 'bypass'
		WHERE id = ?`, account.ID); err == nil {
		t.Fatal("direct archive with open reconciliation unexpectedly succeeded")
	}
	if _, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Closed"}); err == nil {
		t.Fatal("archive with open reconciliation unexpectedly succeeded")
	}
}

func TestStatementAccountArchiveIgnoresAbandonedFutureReconciliation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)
	mistake, err := f.service.StartReconciliation(ctx, account.Code, "2026-08-01", "2026-08-31", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AbandonReconciliation(ctx, mistake.ID, "Wrong future statement period", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: account.Code, SourceSystem: "BANK", SourceName: "july-before-archive.csv",
		FileSHA256: strings.Repeat("9", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "july-before-archive", PostedDate: "2026-07-15", Description: "Archived account evidence", AmountCents: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	archived, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{
		Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Reconciliation control replaced",
	})
	if err != nil {
		t.Fatalf("abandoned future work blocked archive: %v", err)
	}
	if archived.Status != "ARCHIVED" || archived.ReconciliationRequiredThrough != "2026-07-31" {
		t.Fatalf("unexpected archived statement account: %+v", archived)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO reconciliations
		(id, statement_account_id, start_date, end_date, beginning_balance_cents, ending_balance_cents,
		 status, created_at, created_by)
		VALUES ('archived-reconciliation-bypass', ?, '2026-09-01', '2026-09-30', 0, 0,
		        'OPEN', '2026-09-30T00:00:00Z', 'direct')`, account.ID); err == nil {
		t.Fatal("direct reconciliation insert for archived statement account unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_transactions
		(id, statement_account_id, source_identity_id, source_record_id,
		 posted_date, description, amount_cents, created_at)
		SELECT 'archived-transaction-bypass', statement_account_id, source_identity_id, source_record_id,
		       posted_date, description, amount_cents, '2026-09-01T00:00:00Z'
		FROM statement_transactions WHERE statement_account_id = ?`, account.ID); err == nil {
		t.Fatal("direct statement transaction insert for archived statement account unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "new statement transactions require an active statement account") {
		t.Fatalf("archived statement transaction error = %v, want active-account guard", err)
	}
}

func TestStatementAccountArchiveRejectsUnmaterializedCurrentPostedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO import_batches
		(id, source_system, entity_id, source_name, file_sha256, status, record_count, created_at)
		SELECT 'unmaterialized-batch', 'BANK', id, 'unmaterialized.csv', ?, 'STAGED', 1,
		       '2026-08-04T00:00:00Z'
		FROM entities WHERE code = 'ACME'`, strings.Repeat("8", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_identities
		(id, entity_id, book_id, materialization_kind, statement_account_id,
		 source_system, source_account, external_id, created_at, created_by)
		SELECT 'unmaterialized-identity', e.id, b.id, 'STATEMENT', ?, 'BANK', ?,
		       'unmaterialized-posted', '2026-08-04T00:00:00Z', 'direct'
		FROM entities e JOIN books b ON b.entity_id = e.id
		WHERE e.code = 'ACME' AND b.code = 'ACME'`, account.ID, account.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, observation_kind,
		 transaction_date, description, amount_cents, disposition, payload_sha256,
		 raw_json, created_at, created_by)
		VALUES ('unmaterialized-record', 'unmaterialized-identity', 'unmaterialized-batch', 1, 'PROVIDER',
		        '2026-07-15', 'Unmaterialized posted source', 1, 'POSTED', ?,
		        '{}', '2026-08-04T00:00:00Z', 'direct')`, strings.Repeat("7", 64)); err != nil {
		t.Fatal(err)
	}
	input := ledger.ArchiveStatementAccountInput{
		Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Control replaced",
	}
	if _, err := f.service.ValidateStatementAccountArchive(ctx, input); err == nil {
		t.Fatal("archive preview with unmaterialized current POSTED source unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "STATEMENT_ACCOUNT_HAS_UNMATERIALIZED_SOURCE" {
		t.Fatalf("archive preview error = %v, want STATEMENT_ACCOUNT_HAS_UNMATERIALIZED_SOURCE", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = '2026-07-31',
		    archived_at = '2026-08-04T00:00:00Z', archived_by = 'direct', archive_reason = 'bypass'
		WHERE id = ?`, account.ID); err == nil {
		t.Fatal("direct archive with unmaterialized current POSTED source unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "current posted statement source must be materialized before archive") {
		t.Fatalf("direct archive error = %v, want source-materialization guard", err)
	}
}

func TestStatementAccountDirectArchiveRejectsLaterEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	cash := createCashStatementAccount(t, f, true)
	reconciliation, err := f.service.StartReconciliation(ctx, cash.Code, "2026-08-01", "2026-08-31", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = '2026-07-31',
		    archived_at = '2026-08-04T00:00:00Z', archived_by = 'direct', archive_reason = 'bypass'
		WHERE id = ?`, cash.ID); err == nil {
		t.Fatal("direct archive before completed reconciliation evidence unexpectedly succeeded")
	}

	if _, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
		Code: "1100", Name: "Second Cash", Type: "ASSET", BookCodes: []string{"ACME"},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-SECOND-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1100",
		Name: "Second cash", Kind: "BANK", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: second.Code, SourceSystem: "BANK", SourceName: "august.csv",
		FileSHA256: "1212121212121212121212121212121212121212121212121212121212121212",
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "august-activity", PostedDate: "2026-08-01", Description: "Later activity", AmountCents: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = '2026-07-31',
		    archived_at = '2026-08-04T00:00:00Z', archived_by = 'direct', archive_reason = 'bypass'
		WHERE id = ?`, second.ID); err == nil {
		t.Fatal("direct archive before statement transaction evidence unexpectedly succeeded")
	}
}

func TestStatementImportPreservesNonPostedSourceDispositions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)
	input := ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "source-complete.csv",
		FileSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Transactions: []ledger.StatementTransactionInput{
			{ExternalID: "posted", PostedDate: "2026-07-01", Description: "Posted", AmountCents: 100},
			{ExternalID: "pending", PostedDate: "2026-07-02", Description: "Pending", AmountCents: -20, Disposition: ledger.SourceDispositionPending, ExclusionReason: "provider pending"},
			{ExternalID: "review", PostedDate: "2026-07-03", Description: "Review", AmountCents: -30, Disposition: ledger.SourceDispositionNeedsReview, ExclusionReason: "duplicate candidate"},
			{ExternalID: "source-only", PostedDate: "2026-07-04", Description: "Evidence", AmountCents: 40, Disposition: ledger.SourceDispositionSourceOnly, ExclusionReason: "not statement activity"},
		},
	}
	result, err := f.service.ImportStatementTransactions(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RecordCount != 4 || result.StatementTransactionCount != 1 || result.SourceOnlyCount != 3 {
		t.Fatalf("unexpected import counts: %+v", result)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "bad-disposition.csv",
		FileSHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "bad-pending", PostedDate: "2026-07-05", Description: "Bad pending",
			AmountCents: 1, Disposition: ledger.SourceDispositionPending,
		}},
	}); err == nil {
		t.Fatal("PENDING source without an exclusion reason unexpectedly succeeded")
	}
	var sources, transactions int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM source_records WHERE import_batch_id = ?`, result.BatchID).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM statement_transactions`).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if sources != 4 || transactions != 1 {
		t.Fatalf("sources=%d transactions=%d, want 4/1", sources, transactions)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
	        (id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
	         observation_kind, transaction_date, description, amount_cents, disposition,
	         payload_sha256, raw_json, created_at, created_by)
	        SELECT 'bad-direct-source', source_identity_id, import_batch_id, revision + 1, id,
	               'PROVIDER', transaction_date, description, amount_cents, 'PENDING',
	               payload_sha256, raw_json, '2026-08-04T00:00:00Z', 'direct'
	        FROM current_source_records
	        WHERE source_account = 'ACME-CASH' AND external_id = 'pending'`); err == nil {
		t.Fatal("direct-SQL PENDING source without an exclusion reason unexpectedly succeeded")
	}
	records, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{SourceAccount: "ACME-CASH"})
	if err != nil || len(records) != 4 {
		t.Fatalf("list source records = %d, %v", len(records), err)
	}
	var pendingSourceID string
	for _, record := range records {
		if record.ExternalID == "pending" {
			pendingSourceID = record.ID
			if record.Disposition != ledger.SourceDispositionPending || record.StatementTransactionID != "" {
				t.Fatalf("pending source materialized unexpectedly: %+v", record)
			}
		}
	}
	if pendingSourceID == "" {
		t.Fatal("pending source record was not preserved")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_transactions
	        (id, statement_account_id, source_identity_id, source_record_id,
	         posted_date, description, amount_cents, created_at)
	        SELECT 'bad-pending-transaction', sa.id, sr.source_identity_id, sr.id,
	               sr.transaction_date, sr.description, sr.amount_cents, '2026-08-04T00:00:00Z'
	        FROM statement_accounts sa JOIN source_records sr ON sr.id = ?
	        WHERE sa.code = 'ACME-CASH'`, pendingSourceID); err == nil {
		t.Fatal("direct-SQL materialization of a PENDING source unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE source_records SET disposition = 'POSTED' WHERE id = ?`, pendingSourceID); err == nil {
		t.Fatal("source disposition mutation unexpectedly succeeded")
	}
	evidenceJournal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-02", Period: "2026-07", Description: "Pending evidence target",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 20}, {Account: "4000", CreditCents: 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, pendingSourceID, evidenceJournal.ID, "EVIDENCE"); err == nil {
		t.Fatal("PENDING source linked to a journal unexpectedly")
	}
	duplicate, err := f.service.ImportStatementTransactions(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Changed || duplicate.StatementTransactionCount != 1 || duplicate.SourceOnlyCount != 3 {
		t.Fatalf("unexpected idempotent counts: %+v", duplicate)
	}
}

func TestSourceJournalManyToManyLinksAndEntityRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)
	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "evidence.csv",
		FileSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Transactions: []ledger.StatementTransactionInput{
			{ExternalID: "evidence-1", PostedDate: "2026-07-05", Description: "Evidence one", AmountCents: 100},
			{ExternalID: "evidence-2", PostedDate: "2026-07-06", Description: "Evidence two", AmountCents: 200},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceIDs := map[string]string{}
	rows, err := f.store.DB().QueryContext(ctx, `SELECT si.external_id, sr.id
		FROM source_records sr JOIN source_identities si ON si.id = sr.source_identity_id
		WHERE si.source_account = 'ACME-CASH'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var externalID, id string
		if err := rows.Scan(&externalID, &id); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		sourceIDs[externalID] = id
	}
	_ = rows.Close()
	journalInput := ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-05", Period: "2026-07", Description: "Evidence journal one",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 300}, {Account: "4000", CreditCents: 300}},
	}
	firstJournal, err := f.service.CreateJournal(ctx, journalInput)
	if err != nil {
		t.Fatal(err)
	}
	journalInput.Description = "Evidence journal two"
	journalInput.PostingDate = "2026-07-06"
	secondJournal, err := f.service.CreateJournal(ctx, journalInput)
	if err != nil {
		t.Fatal(err)
	}
	firstLink, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], firstJournal.ID, "EVIDENCE")
	if err != nil || !firstLink.Changed {
		t.Fatalf("first source link: %+v, %v", firstLink, err)
	}
	idempotent, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], firstJournal.ID, "EVIDENCE")
	if err != nil || idempotent.Changed {
		t.Fatalf("idempotent source link: %+v, %v", idempotent, err)
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], secondJournal.ID, "EVIDENCE"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-2"], firstJournal.ID, "EVIDENCE"); err != nil {
		t.Fatal(err)
	}
	group, err := f.service.CreateGroup(ctx, ledger.CreateGroupInput{
		Code: "ACME-GROUP", Name: "Acme Consolidated", ParentEntity: "ACME",
		EliminationBookCode: "ACME-ELIM", EliminationBookName: "Acme Eliminations",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"1000", "4000"} {
		if err := f.service.ConfigureBookAccount(ctx, group.EliminationBookCode, account, "2026-07-01", "", true); err != nil {
			t.Fatal(err)
		}
	}
	eliminationJournal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: group.EliminationBookCode, PostingDate: "2026-07-05", Period: "2026-07", Description: "Acme elimination mirror",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], eliminationJournal.ID, "EVIDENCE"); err == nil {
		t.Fatal("cross-book EVIDENCE link unexpectedly succeeded")
	} else {
		requireAppErrorCode(t, err, "SOURCE_JOURNAL_BOOK_MISMATCH")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_record_journals
		(source_record_id, journal_entry_id, link_role, created_at, created_by)
		VALUES (?, ?, 'EVIDENCE', '2026-08-04T00:00:00Z', 'direct')`, sourceIDs["evidence-1"], eliminationJournal.ID); err == nil {
		t.Fatal("direct-SQL cross-book EVIDENCE link unexpectedly succeeded")
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], eliminationJournal.ID, "MIRROR"); err != nil {
		t.Fatalf("explicit cross-book MIRROR link: %v", err)
	}
	links, err := f.service.ListSourceJournalLinks(ctx, sourceIDs["evidence-1"])
	if err != nil || len(links) != 3 {
		t.Fatalf("source links = %d, %v", len(links), err)
	}
	journalInput.Description = "Changed linked journal"
	if _, err := f.service.ReplaceDraft(ctx, firstJournal.ID, journalInput); err == nil {
		t.Fatal("editing a source-linked draft unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries SET description = 'Direct change' WHERE id = ?`, firstJournal.ID); err == nil {
		t.Fatal("direct-SQL edit of a source-linked draft unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `DELETE FROM source_record_journals
        WHERE source_record_id = ? AND journal_entry_id = ?`, sourceIDs["evidence-1"], firstJournal.ID); err == nil {
		t.Fatal("deleting an immutable source-journal link unexpectedly succeeded")
	}

	if _, err := f.service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "NORTHSTAR", LegalName: "Northstar, Inc.", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"1000", "4000"} {
		if err := f.service.ConfigureBookAccount(ctx, "NORTHSTAR", account, "2026-07-01", "", true); err != nil {
			t.Fatal(err)
		}
	}
	northstarJournal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "NORTHSTAR", PostingDate: "2026-07-07", Period: "2026-07", Description: "Northstar mirror",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], northstarJournal.ID, "EVIDENCE"); err == nil {
		t.Fatal("cross-entity EVIDENCE link unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_record_journals
        (source_record_id, journal_entry_id, link_role, created_at, created_by)
        VALUES (?, ?, 'EVIDENCE', '2026-08-04T00:00:00Z', 'direct')`, sourceIDs["evidence-1"], northstarJournal.ID); err == nil {
		t.Fatal("direct-SQL cross-entity EVIDENCE link unexpectedly succeeded")
	}
	if _, err := f.service.LinkSourceRecordJournal(ctx, sourceIDs["evidence-1"], northstarJournal.ID, "MIRROR"); err != nil {
		t.Fatalf("explicit cross-entity MIRROR link: %v", err)
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after many-to-many source links: %v", err)
	}
	var linkTriggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
        WHERE type = 'trigger' AND name = 'source_record_journals_validate_insert'`).Scan(&linkTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER source_record_journals_validate_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_record_journals
        (source_record_id, journal_entry_id, link_role, created_at, created_by)
		VALUES (?, ?, 'EVIDENCE', '2026-08-04T00:00:00Z', 'tamper')`, sourceIDs["evidence-2"], eliminationJournal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, linkTriggerSQL); err != nil {
		t.Fatal(err)
	}
	doctor, err := f.store.Doctor(ctx)
	if err == nil || doctor.InvalidSourceLinks != 1 {
		t.Fatalf("doctor did not detect invalid source link: %+v, %v", doctor, err)
	}
}

func TestManualReconciliationEvidenceBindsTheExactAllocatedJournalLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, false)

	postReceipt := func() ledger.Journal {
		t.Helper()
		journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Duplicate-shaped receipt",
			Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 10_000}, {Account: "4000", CreditCents: 10_000}},
		})
		if err != nil {
			t.Fatal(err)
		}
		journal, err = f.service.PostJournal(ctx, journal.ID)
		if err != nil {
			t.Fatal(err)
		}
		return journal
	}
	firstJournal := postReceipt()
	secondJournal := postReceipt()
	controlLine := func(journal ledger.Journal) ledger.JournalLine {
		t.Helper()
		for _, line := range journal.Lines {
			if line.AccountCode == "1000" {
				return line
			}
		}
		t.Fatal("journal is missing its cash control line")
		return ledger.JournalLine{}
	}
	firstControl := controlLine(firstJournal)
	secondControl := controlLine(secondJournal)
	planDigest := strings.Repeat("a", 64)
	lineFor := func(journal ledger.Journal, line ledger.JournalLine, withEvidence bool) ledger.ManualReconciliationLine {
		result := ledger.ManualReconciliationLine{
			JournalLineID: line.ID,
			ExternalID:    fmt.Sprintf("reconcile:%s:%d:%d", strings.ToLower(account.Code), journal.EntryNumber, line.LineNumber),
			LedgerDate:    "2026-07-15", StatementDate: "2026-07-15",
			Description: "Duplicate-shaped receipt", AmountCents: 10_000,
		}
		if withEvidence {
			raw, err := json.Marshal(map[string]any{
				"plan_digest": planDigest, "transaction_number": journal.EntryNumber,
				"line_number": line.LineNumber, "ledger_date": "2026-07-15",
				"statement_date": "2026-07-15", "provenance": "OPERATOR_ATTESTATION",
			})
			if err != nil {
				t.Fatal(err)
			}
			result.RawJSON = raw
		}
		return result
	}
	first := lineFor(firstJournal, firstControl, true)
	second := lineFor(secondJournal, secondControl, false)
	input := ledger.ManualReconciliationInput{
		StatementAccount: account.Code, SourceName: "manual-july.json", PlanDigest: planDigest,
		StartDate: "2026-07-01", EndDate: "2026-07-31",
		BeginningBalanceCents: 0, EndingBalanceCents: 10_000,
		ExpectedLedgerBeginningCents: 0, ExpectedLedgerEndingCents: 20_000,
		ExpectedOpeningOutstandingCents: 0, ExpectedEndingOutstandingCents: 10_000,
		ExpectedStatementTransactionCount: 0,
		ExpectedLines:                     []ledger.ManualReconciliationLine{first, second},
		Lines:                             []ledger.ManualReconciliationLine{first}, Outstanding: []ledger.ManualReconciliationLine{second},
	}

	wrong := input
	wrong.Lines = append([]ledger.ManualReconciliationLine(nil), input.Lines...)
	wrongEvidence, err := json.Marshal(map[string]any{
		"plan_digest": planDigest, "transaction_number": secondJournal.EntryNumber,
		"line_number": secondControl.LineNumber, "ledger_date": "2026-07-15",
		"statement_date": "2026-07-15", "provenance": "OPERATOR_ATTESTATION",
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong.Lines[0].RawJSON = wrongEvidence
	if _, err := f.service.ApplyManualReconciliation(ctx, wrong); err == nil {
		t.Fatal("manual evidence naming a different journal line unexpectedly succeeded")
	}
	var sourceCount int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM source_records`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 0 {
		t.Fatalf("failed manual reconciliation retained %d source records", sourceCount)
	}

	if _, err := f.service.ApplyManualReconciliation(ctx, input); err != nil {
		t.Fatalf("apply exact manual reconciliation: %v", err)
	}
	if result, err := f.store.Doctor(ctx); err != nil || !result.OK {
		t.Fatalf("doctor rejected exact manual evidence: %+v, %v", result, err)
	}

	var allocationID, allocationTriggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM reconciliation_allocations LIMIT 1`).Scan(&allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliation_allocations SET journal_line_id = ? WHERE id = ?`, secondControl.ID, allocationID); err == nil {
		t.Fatal("immutable allocation unexpectedly changed")
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'reconciliation_allocations_validate_update'`).Scan(&allocationTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER reconciliation_allocations_validate_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliation_allocations SET journal_line_id = ? WHERE id = ?`, secondControl.ID, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, allocationTriggerSQL); err != nil {
		t.Fatal(err)
	}
	doctor, err := f.store.Doctor(ctx)
	if err == nil || doctor.InvalidSourceEvidence != 1 {
		t.Fatalf("doctor did not detect substituted manual control line: %+v, %v", doctor, err)
	}
}

func TestReconciliationContinuityAndSplitPeriodCoverage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, true)

	first, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-01", "2026-07-15", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-16", "2026-07-31", 0, 0); err == nil {
		t.Fatal("starting after an open prior reconciliation unexpectedly succeeded")
	}
	if _, err := f.service.CompleteReconciliation(ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-17", "2026-07-31", 0, 0); err == nil {
		t.Fatal("reconciliation gap unexpectedly succeeded")
	}
	if _, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-16", "2026-07-31", 1, 1); err == nil {
		t.Fatal("incorrect carried beginning balance unexpectedly succeeded")
	}
	second, err := f.service.StartReconciliation(ctx, "ACME-CASH", "2026-07-16", "2026-07-31", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, second.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("two adjoining reconciliations did not cover the fiscal period: %v", err)
	}
}

func TestAbandonedReconciliationIsAuditedImmutableAndExcludedFromContinuity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)

	first, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-15", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	mistake, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-16", "2026-07-31", 0, 123)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET completed_at = '2026-08-04T00:00:00Z' WHERE id = ?`, mistake.ID); err == nil {
		t.Fatal("open reconciliation unexpectedly accepted partial completion evidence")
	}
	if _, err := f.service.AbandonReconciliation(ctx, mistake.ID, "  ", false); err == nil {
		t.Fatal("abandonment without a reason unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "ABANDON_REASON_REQUIRED" {
		t.Fatalf("abandonment error = %v, want ABANDON_REASON_REQUIRED", err)
	}
	preview, err := f.service.AbandonReconciliation(ctx, mistake.ID, "Ending balance was entered from the wrong statement", true)
	if err != nil {
		t.Fatalf("preview abandonment: %v", err)
	}
	if preview.Status != "ABANDONED" || preview.AbandonedBy != "test" || preview.AbandonReason == "" {
		t.Fatalf("incomplete abandonment preview: %+v", preview)
	}
	unchanged, err := f.service.ReconciliationStatus(ctx, mistake.ID)
	if err != nil || unchanged.Status != "OPEN" {
		t.Fatalf("dry-run mutated reconciliation: %+v, %v", unchanged, err)
	}
	var auditCount int
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
		WHERE command = 'reconcile abandon' AND aggregate_id = ?`, mistake.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("dry-run wrote %d audit events", auditCount)
	}

	abandoned, err := f.service.AbandonReconciliation(ctx, mistake.ID, "  Ending balance was entered from the wrong statement  ", false)
	if err != nil {
		t.Fatalf("abandon reconciliation: %v", err)
	}
	if abandoned.Status != "ABANDONED" || abandoned.AbandonedAt == "" || abandoned.AbandonedBy != "test" ||
		abandoned.AbandonReason != "Ending balance was entered from the wrong statement" {
		t.Fatalf("incomplete abandonment evidence: %+v", abandoned)
	}
	listed, err := f.service.ListReconciliations(ctx, ledger.ReconciliationFilter{Status: "abandoned"})
	if err != nil || len(listed) != 1 || listed[0].ID != mistake.ID || listed[0].AbandonReason != abandoned.AbandonReason {
		t.Fatalf("abandoned reconciliation list = %+v, %v", listed, err)
	}
	var auditPayload string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT payload_json FROM audit_events
		WHERE command = 'reconcile abandon' AND aggregate_id = ?`, mistake.ID).Scan(&auditPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditPayload, abandoned.AbandonReason) {
		t.Fatalf("abandonment audit payload %q does not contain reason", auditPayload)
	}
	if _, err := f.service.CompleteReconciliation(ctx, mistake.ID, false); err == nil {
		t.Fatal("abandoned reconciliation unexpectedly completed")
	}
	if err := f.service.ReopenReconciliation(ctx, mistake.ID, "retry"); err == nil {
		t.Fatal("abandoned reconciliation unexpectedly reopened")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET abandon_reason = 'rewritten evidence' WHERE id = ?`, mistake.ID); err == nil {
		t.Fatal("abandoned reconciliation evidence unexpectedly changed")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO reconciliations
		(id, statement_account_id, start_date, end_date, beginning_balance_cents, ending_balance_cents,
		 status, created_at, created_by, abandoned_at, abandoned_by, abandon_reason)
		VALUES ('direct-abandoned', ?, '2026-07-16', '2026-07-31', 0, 0,
		 'ABANDONED', '2026-08-04T00:00:00Z', 'direct', '2026-08-04T00:00:00Z', 'direct', 'bypass')`, account.ID); err == nil {
		t.Fatal("direct insertion of a terminal reconciliation unexpectedly succeeded")
	}

	corrected, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-16", "2026-07-31", 0, 0)
	if err != nil {
		t.Fatalf("abandoned period blocked corrected reconciliation: %v", err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, corrected.ID, false); err != nil {
		t.Fatalf("complete corrected reconciliation: %v", err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("abandoned work blocked corrected period close: %v", err)
	}
	if result, err := f.store.Doctor(ctx); err != nil || !result.OK || result.InvalidReconciliations != 0 {
		t.Fatalf("doctor treated abandoned overlap as active: %+v, %v", result, err)
	}
}

func TestReconciliationsMustBeAbandonedInReverseChronologicalOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, false)
	first, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-15", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	second, err := f.service.StartReconciliation(ctx, account.Code, "2026-07-16", "2026-07-31", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, second.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenReconciliation(ctx, first.ID, "out-of-order attempt"); err == nil {
		t.Fatal("earlier reconciliation reopened while later work remained completed")
	} else {
		requireAppErrorCode(t, err, "RECONCILIATION_REOPEN_ORDER")
	}
	firstStatus, err := f.service.ReconciliationStatus(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstStatus.Status != "COMPLETED" {
		t.Fatalf("failed reopen changed earlier reconciliation to %s", firstStatus.Status)
	}
	if err := f.service.ReopenReconciliation(ctx, second.ID, "unwind in reverse order"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenReconciliation(ctx, first.ID, "incorrect opening period"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AbandonReconciliation(ctx, first.ID, "incorrect opening period", true); err == nil {
		t.Fatal("earlier reconciliation abandonment preview unexpectedly succeeded while later work exists")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "RECONCILIATION_HAS_LATER_WORK" {
		t.Fatalf("abandonment error = %v, want RECONCILIATION_HAS_LATER_WORK", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET status = 'ABANDONED', abandoned_at = '2026-08-04T00:00:00Z', abandoned_by = 'direct', abandon_reason = 'bypass'
		WHERE id = ?`, first.ID); err == nil {
		t.Fatal("direct SQL abandoned an earlier reconciliation while later work exists")
	}
	if _, err := f.service.AbandonReconciliation(ctx, second.ID, "dependent on incorrect opening period", false); err != nil {
		t.Fatalf("abandon later reconciliation: %v", err)
	}
	if _, err := f.service.AbandonReconciliation(ctx, first.ID, "incorrect opening period", false); err != nil {
		t.Fatalf("abandon earlier reconciliation after later work: %v", err)
	}
}

func TestReconciliationAcceptsSignedCreditAllocations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	for _, account := range []ledger.CreateAccountInput{
		{Code: "2000", Name: "Credit Card", Type: "LIABILITY", BookCodes: []string{"ACME"}},
		{Code: "5000", Name: "Card Expense", Type: "EXPENSE", BookCodes: []string{"ACME"}},
	} {
		if _, err := f.service.CreateAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	statementAccount, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-CARD", Entity: "ACME", Book: "ACME", GLAccount: "2000",
		Name: "Acme Card", Kind: "CREDIT_CARD", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-12", Period: "2026-07", Description: "Card charge",
		Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 7_500}, {Account: "2000", CreditCents: 7_500}},
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err = f.service.PostJournal(ctx, journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	var controlLineID string
	for _, line := range journal.Lines {
		if line.AccountCode == "2000" {
			controlLineID = line.ID
		}
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: statementAccount.Code, SourceSystem: "CARD", SourceName: "card.csv",
		FileSHA256:   "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Transactions: []ledger.StatementTransactionInput{{ExternalID: "charge-1", PostedDate: "2026-07-12", Description: "Charge", AmountCents: -7_500}},
	}); err != nil {
		t.Fatal(err)
	}
	var transactionID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT st.id FROM statement_transactions st
		JOIN source_identities si ON si.id = st.source_identity_id WHERE si.external_id = 'charge-1'`).Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	reconciliation, err := f.service.StartReconciliation(ctx, statementAccount.Code, "2026-07-01", "2026-07-31", 0, -7_500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AllocateReconciliation(ctx, reconciliation.ID, transactionID, controlLineID, -7_500); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatalf("complete signed credit reconciliation: %v", err)
	}
}

func TestDoctorRevalidatesCompletedReconciliations(t *testing.T) {
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
	var triggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
        WHERE type = 'trigger' AND name = 'statement_transactions_guard_completed_insert'`).Scan(&triggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER statement_transactions_guard_completed_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: account.Code, SourceSystem: "BANK", SourceName: "tampered.csv",
		FileSHA256: strings.Repeat("f", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "tampered", PostedDate: "2026-07-15", Description: "Tampered row", AmountCents: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, triggerSQL); err != nil {
		t.Fatal(err)
	}
	result, err := f.store.Doctor(ctx)
	if err == nil {
		t.Fatal("doctor unexpectedly accepted a stale completed reconciliation")
	}
	if result.InvalidReconciliations != 1 {
		t.Fatalf("invalid reconciliations = %d, want 1", result.InvalidReconciliations)
	}
}
