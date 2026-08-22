package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func createPrecoverageClosureFixture(t *testing.T) (fixture, ledger.StatementAccount, ledger.CloseStatementAccountBeforeCoverageInput) {
	t.Helper()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, true)
	identity, err := f.service.AddStatementAccountIdentity(context.Background(), ledger.AddStatementAccountIdentityInput{
		StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceRealm: "GLOBAL",
		ExternalID: "provider-account-2276", AccountNumber: "1234562276", Name: "Owner's Comp", Active: true,
		Evidence: ledger.StatementAccountIdentityEvidence{
			SourceKind: "PROVIDER_ACCOUNT_EXPORT", SourcePath: "/evidence/accounts.json",
			SourceSHA256: strings.Repeat("a", 64), Locator: "accounts[id=provider-account-2276]",
			PayloadSHA256: strings.Repeat("b", 64),
		},
	})
	if err != nil {
		t.Fatalf("add exact provider identity: %v", err)
	}
	if identity.StatementAccountID != account.ID {
		t.Fatalf("identity target = %s, want %s", identity.StatementAccountID, account.ID)
	}
	input := ledger.CloseStatementAccountBeforeCoverageInput{
		StatementAccount: account.Code,
		Identity: ledger.PrecoverageClosureIdentity{
			SourceSystem: "PROVIDER_API", SourceRealm: "GLOBAL", ExternalID: "provider-account-2276",
		},
		ClosedOn: "2025-09-19",
		ClosureEvidence: ledger.PrecoverageClosureEvidence{
			SourceKind: "PROVIDER_CLOSURE_LETTER", SourcePath: "/evidence/closure-2276.pdf",
			SourceSHA256: strings.Repeat("c", 64), Locator: "page 1; account ending 2276; closed 2025-09-19",
		},
		ZeroEvidence: ledger.PrecoverageZeroEvidence{
			SourceKind: "PROVIDER_ACCOUNT_SNAPSHOT", SourcePath: "/evidence/accounts.json",
			SourceSHA256: strings.Repeat("d", 64), Locator: "accounts[id=provider-account-2276]",
			PayloadSHA256: strings.Repeat("e", 64), ObservedOn: "2026-04-28",
			ProviderStatus: "ARCHIVED", CurrentBalanceCents: 0, AvailableBalanceCents: 0,
		},
		AccountHolder: "Acme, Inc.", AccountSuffix: "2276",
		Reason:          "Provider closed the exact zero-balance account before required reconciliation coverage",
		InputSourcePath: "/evidence/close-2276.json", InputSourceSHA256: strings.Repeat("f", 64),
	}
	return f, account, input
}

func requireAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("operation unexpectedly succeeded; want %s", code)
	}
	appError, ok := apperr.As(err)
	if !ok || appError.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func insertDirectPrecoverageClosure(ctx context.Context, f fixture, id, statementAccountID, identityID, actor string, input ledger.CloseStatementAccountBeforeCoverageInput) error {
	_, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_account_precoverage_closures
		(id, statement_account_id, statement_account_identity_id, closed_on,
		 closure_evidence_source_kind, closure_evidence_source_path, closure_evidence_source_sha256, closure_evidence_locator,
		 zero_evidence_source_kind, zero_evidence_source_path, zero_evidence_source_sha256, zero_evidence_locator,
		 zero_evidence_payload_sha256, zero_observed_on, provider_status, current_balance_cents,
		 available_balance_cents, account_holder, account_suffix, reason, input_source_path,
		 input_source_sha256, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?, ?, '2026-08-10T00:00:00Z', ?)`,
		id, statementAccountID, identityID, input.ClosedOn,
		input.ClosureEvidence.SourceKind, input.ClosureEvidence.SourcePath, input.ClosureEvidence.SourceSHA256, input.ClosureEvidence.Locator,
		input.ZeroEvidence.SourceKind, input.ZeroEvidence.SourcePath, input.ZeroEvidence.SourceSHA256, input.ZeroEvidence.Locator,
		input.ZeroEvidence.PayloadSHA256, input.ZeroEvidence.ObservedOn, input.ZeroEvidence.ProviderStatus,
		input.AccountHolder, input.AccountSuffix, input.Reason, input.InputSourcePath, input.InputSourceSHA256, actor)
	return err
}

func createDirectPrecoverageLifecycle(t *testing.T) (fixture, ledger.StatementAccount, ledger.CloseStatementAccountBeforeCoverageInput, ledger.StatementAccountPrecoverageClosure) {
	t.Helper()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	preview, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var identityID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM statement_account_identities
		WHERE statement_account_id = ? AND source_system = 'PROVIDER_API'`, account.ID).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if err := insertDirectPrecoverageClosure(ctx, f, "direct-closure", account.ID, identityID, "direct", input); err != nil {
		t.Fatalf("insert structurally valid direct lifecycle row: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_account_precoverage_identity_bindings
		(closure_id, active_identity_count, active_identity_digest, created_at, created_by)
		VALUES ('direct-closure', ?, ?, '2026-08-10T00:00:00Z', 'direct')`,
		preview.ActiveIdentityCount, preview.ActiveIdentityDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = reconciliation_required_from,
		    archived_at = '2026-08-10T00:00:00Z', archived_by = 'direct', archive_reason = ?
		WHERE id = ?`, input.Reason, account.ID); err != nil {
		t.Fatalf("complete structurally valid direct lifecycle: %v", err)
	}
	return f, account, input, preview
}

func directPrecoverageClosureAuditPayload(t *testing.T, preview ledger.StatementAccountPrecoverageClosure, archivedBy string) map[string]any {
	t.Helper()
	preview.ID = "direct-closure"
	preview.CreatedAt = "2026-08-10T00:00:00Z"
	preview.CreatedBy = "direct"
	preview.ArchivedAt = "2026-08-10T00:00:00Z"
	preview.ArchivedBy = archivedBy
	preview.Changed = true
	data, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	payload := make(map[string]any)
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "active_identity_count")
	delete(payload, "active_identity_digest")
	return payload
}

func appendDirectPrecoverageAudits(t *testing.T, f fixture, account ledger.StatementAccount, preview ledger.StatementAccountPrecoverageClosure, closurePayload any) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: "direct", Command: "statement-account lifecycle close-before-coverage",
		AggregateType: "statement_account_precoverage_closure", AggregateID: "direct-closure",
		Payload: closurePayload,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: "direct", Command: "statement-account precoverage bind-identities",
		AggregateType: "statement_account_precoverage_identity_binding", AggregateID: "direct-closure",
		Payload: map[string]any{
			"closure_id": "direct-closure", "statement_account_id": account.ID,
			"active_identity_count":  preview.ActiveIdentityCount,
			"active_identity_digest": preview.ActiveIdentityDigest,
			"created_at":             "2026-08-10T00:00:00Z", "created_by": "direct",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPrecoverageClosureArchivesExactZeroAccountAndExemptsLaterPeriod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("required unreconciled statement account did not block period close before lifecycle evidence")
	}
	preview, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
	if err != nil {
		t.Fatalf("validate precoverage closure: %v", err)
	}
	if !preview.Changed || preview.StatementAccountID != account.ID || preview.Status != "ARCHIVED" ||
		preview.CoverageDisposition != ledger.PrecoverageClosureDisposition ||
		preview.ReconciliationRequiredThrough != "2026-07-01" ||
		preview.ActiveIdentityCount != 1 || len(preview.ActiveIdentityDigest) != 64 ||
		preview.ControlBalanceAtClosureCents != 0 || preview.CurrentControlBalanceCents != 0 ||
		preview.PostClosureControlLineCount != 0 {
		t.Fatalf("unexpected precoverage closure preview: %+v", preview)
	}
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatalf("commit precoverage closure: %v", err)
	}
	if created.ID == "" || !created.Changed || created.ArchivedAt == "" || created.ArchivedBy != "test" || created.CreatedBy != "test" {
		t.Fatalf("unexpected committed precoverage closure: %+v", created)
	}
	var boundCount int
	var boundDigest string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT active_identity_count, active_identity_digest
		FROM statement_account_precoverage_identity_bindings WHERE closure_id = ?`, created.ID).Scan(&boundCount, &boundDigest); err != nil {
		t.Fatal(err)
	}
	if boundCount != created.ActiveIdentityCount || boundDigest != created.ActiveIdentityDigest {
		t.Fatalf("stored identity binding = %d/%s, want %d/%s", boundCount, boundDigest, created.ActiveIdentityCount, created.ActiveIdentityDigest)
	}
	var auditBefore int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	repeated, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatalf("idempotent precoverage closure: %v", err)
	}
	if repeated.Changed || repeated.ID != created.ID {
		t.Fatalf("idempotent result = %+v, want unchanged %s", repeated, created.ID)
	}
	var auditAfter int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("idempotent closure appended audit events: before=%d after=%d", auditBefore, auditAfter)
	}
	listed, err := f.service.ListStatementAccountPrecoverageClosures(ctx, ledger.PrecoverageClosureFilter{StatementAccount: account.Code})
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID || listed[0].CurrentControlBalanceCents != 0 {
		t.Fatalf("listed precoverage closures = %+v, %v", listed, err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err != nil {
		t.Fatalf("precoverage closure did not exempt the legally impossible statement period: %v", err)
	}
	if _, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-01", Period: "2026-07", Description: "Forbidden later control draft",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
	}); err == nil {
		t.Fatal("precoverage-closed control unexpectedly accepted a new draft journal line")
	}
	if err := directCloseBookPeriod(ctx, f, "ACME", "2026-07"); err != nil {
		t.Fatalf("direct close trigger did not honor precoverage closure: %v", err)
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after lifecycle certificate and period close: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "UPDATE statement_account_precoverage_closures SET reason = reason WHERE id = ?", created.ID); err == nil {
		t.Fatal("immutable precoverage closure unexpectedly updated")
	}
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM statement_account_precoverage_closures WHERE id = ?", created.ID); err == nil {
		t.Fatal("immutable precoverage closure unexpectedly deleted")
	}
}

func TestDuplicatePrecoverageClosureAuditInvalidatesLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: created.CreatedBy, Command: "statement-account lifecycle close-before-coverage",
		AggregateType: "statement_account_precoverage_closure", AggregateID: created.ID,
		Payload: created,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("duplicate closure audit unexpectedly preserved reconciliation exemption")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want duplicate audit to invalidate one certificate", result.InvalidStatementLifecycles)
	}
}

func TestDuplicatePrecoverageIdentityBindingAuditInvalidatesLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: created.CreatedBy, Command: "statement-account precoverage bind-identities",
		AggregateType: "statement_account_precoverage_identity_binding", AggregateID: created.ID,
		Payload: map[string]any{
			"closure_id": created.ID, "statement_account_id": created.StatementAccountID,
			"active_identity_count":  created.ActiveIdentityCount,
			"active_identity_digest": created.ActiveIdentityDigest,
			"created_at":             created.CreatedAt, "created_by": created.CreatedBy,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("duplicate identity-binding audit unexpectedly preserved reconciliation exemption")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want duplicate binding audit to invalidate one certificate", result.InvalidStatementLifecycles)
	}
}

func TestPrecoverageLifecycleOperationsRequireValidAuditChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err != nil {
		t.Fatal(err)
	}
	var previousHash string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT event_hash FROM audit_events
		ORDER BY sequence DESC LIMIT 1`).Scan(&previousHash); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO audit_events
		(event_id, occurred_at, actor, app_version, command, aggregate_type, aggregate_id,
		 payload_json, previous_hash, event_hash)
		VALUES ('direct-invalid-audit', '2026-08-10T00:00:00Z', 'direct', '0.3.1',
		        'direct invalid audit', 'database', 'direct-invalid-audit', '{}', ?, ?)`,
		previousHash, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("supported period close trusted a certificate on an invalid audit chain")
	} else {
		requireAppErrorCode(t, err, "AUDIT_CHAIN_INVALID")
	}
	if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err == nil {
		t.Fatal("supported lifecycle retry trusted an invalid audit chain")
	} else {
		requireAppErrorCode(t, err, "AUDIT_CHAIN_INVALID")
	}
	if _, err := f.store.Doctor(ctx); err == nil {
		t.Fatal("doctor trusted an invalid audit chain")
	} else {
		requireAppErrorCode(t, err, "AUDIT_CHAIN_INVALID")
	}
}

func TestPrecoverageClosureRejectsNonzeroAndLaterControlActivity(t *testing.T) {
	t.Run("nonzero control", func(t *testing.T) {
		ctx := context.Background()
		f, _, input := createPrecoverageClosureFixture(t)
		journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: "ACME", PostingDate: "2026-07-01", Period: "2026-07", Description: "Unsupported residual",
			Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 69_900}, {Account: "4000", CreditCents: 69_900}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.PostJournal(ctx, journal.ID); err != nil {
			t.Fatal(err)
		}
		_, err = f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
		requireAppErrorCode(t, err, "PRECOVERAGE_CONTROL_BALANCE_NONZERO")
	})

	t.Run("later net-zero activity", func(t *testing.T) {
		ctx := context.Background()
		f, _, input := createPrecoverageClosureFixture(t)
		for index, lines := range [][]ledger.JournalLineInput{
			{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
			{{Account: "4000", DebitCents: 100}, {Account: "1000", CreditCents: 100}},
		} {
			journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
				Book: "ACME", PostingDate: "2026-07-01", Period: "2026-07",
				Description: "Post-closure control activity " + string(rune('1'+index)), Lines: lines,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.PostJournal(ctx, journal.ID); err != nil {
				t.Fatal(err)
			}
		}
		_, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
		requireAppErrorCode(t, err, "PRECOVERAGE_HAS_LATER_CONTROL_ACTIVITY")
	})

	t.Run("unresolved draft activity", func(t *testing.T) {
		ctx := context.Background()
		f, _, input := createPrecoverageClosureFixture(t)
		if _, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: "ACME", PostingDate: "2026-07-01", Period: "2026-07", Description: "Unresolved draft",
			Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}},
		}); err != nil {
			t.Fatal(err)
		}
		_, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
		requireAppErrorCode(t, err, "PRECOVERAGE_HAS_DRAFT_CONTROL_ACTIVITY")
	})
}

func TestPrecoverageClosureRequiresExactIdentityAndProviderZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	identityMismatch := input
	identityMismatch.Identity.ExternalID = "different-provider-account"
	_, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, identityMismatch)
	requireAppErrorCode(t, err, "PRECOVERAGE_IDENTITY_MISMATCH")

	suffixMismatch := input
	suffixMismatch.AccountSuffix = "9999"
	_, err = f.service.ValidateStatementAccountPrecoverageClosure(ctx, suffixMismatch)
	requireAppErrorCode(t, err, "PRECOVERAGE_IDENTITY_MISMATCH")

	nonzeroProvider := input
	nonzeroProvider.ZeroEvidence.CurrentBalanceCents = 1
	_, err = f.service.ValidateStatementAccountPrecoverageClosure(ctx, nonzeroProvider)
	requireAppErrorCode(t, err, "PRECOVERAGE_PROVIDER_BALANCE_NONZERO")

	lateClosure := input
	lateClosure.ClosedOn = "2026-07-01"
	lateClosure.ZeroEvidence.ObservedOn = "2026-08-01"
	_, err = f.service.ValidateStatementAccountPrecoverageClosure(ctx, lateClosure)
	requireAppErrorCode(t, err, "PRECOVERAGE_CLOSURE_DATE_INVALID")
}

func TestPrecoverageClosureDirectSQLRequiresCanonicalDatesAndAbsolutePaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, testCase := range []struct {
		name   string
		mutate func(*ledger.CloseStatementAccountBeforeCoverageInput)
	}{
		{name: "invalid closure calendar date", mutate: func(input *ledger.CloseStatementAccountBeforeCoverageInput) { input.ClosedOn = "2025-02-30" }},
		{name: "invalid observation calendar date", mutate: func(input *ledger.CloseStatementAccountBeforeCoverageInput) {
			input.ZeroEvidence.ObservedOn = "2026-02-30"
		}},
		{name: "relative closure evidence", mutate: func(input *ledger.CloseStatementAccountBeforeCoverageInput) {
			input.ClosureEvidence.SourcePath = "closure.pdf"
		}},
		{name: "relative zero evidence", mutate: func(input *ledger.CloseStatementAccountBeforeCoverageInput) {
			input.ZeroEvidence.SourcePath = "accounts.json"
		}},
		{name: "relative retained input", mutate: func(input *ledger.CloseStatementAccountBeforeCoverageInput) { input.InputSourcePath = "close.json" }},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			f, account, input := createPrecoverageClosureFixture(t)
			testCase.mutate(&input)
			if _, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input); err == nil {
				t.Fatal("supported validation accepted noncanonical direct-SQL test input")
			}
			var identityID string
			if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM statement_account_identities
				WHERE statement_account_id = ? AND source_system = 'PROVIDER_API'`, account.ID).Scan(&identityID); err != nil {
				t.Fatal(err)
			}
			if err := insertDirectPrecoverageClosure(ctx, f, "direct-noncanonical", account.ID, identityID, "direct", input); err == nil {
				t.Fatal("database trigger accepted a noncanonical precoverage certificate")
			}
		})
	}
}

func TestPrecoverageClosureRequiresTerminalCurrentSourceDispositions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, disposition := range []string{ledger.SourceDispositionPending, ledger.SourceDispositionNeedsReview} {
		disposition := disposition
		t.Run(disposition, func(t *testing.T) {
			f, account, input := createPrecoverageClosureFixture(t)
			if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
				StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceName: "unresolved.json",
				FileSHA256: strings.Repeat("1", 64),
				Transactions: []ledger.StatementTransactionInput{{
					ExternalID: "unresolved-" + strings.ToLower(disposition), PostedDate: "2026-07-15",
					Description: "Unresolved provider observation", AmountCents: -2_500,
					Disposition: disposition, ExclusionReason: "awaiting terminal review",
				}},
			}); err != nil {
				t.Fatal(err)
			}
			_, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
			requireAppErrorCode(t, err, "PRECOVERAGE_SOURCE_UNRESOLVED")
			var identityID string
			if err := f.store.DB().QueryRowContext(ctx, `SELECT id FROM statement_account_identities
				WHERE statement_account_id = ? AND source_system = 'PROVIDER_API'`, account.ID).Scan(&identityID); err != nil {
				t.Fatal(err)
			}
			if err := insertDirectPrecoverageClosure(ctx, f, "direct-"+strings.ToLower(disposition), account.ID, identityID, "direct", input); err == nil {
				t.Fatal("database trigger accepted a precoverage closure with unresolved current source")
			}
		})
	}

	t.Run("SOURCE_ONLY is terminal", func(t *testing.T) {
		f, account, input := createPrecoverageClosureFixture(t)
		if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
			StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceName: "source-only.json",
			FileSHA256: strings.Repeat("2", 64),
			Transactions: []ledger.StatementTransactionInput{{
				ExternalID: "source-only-after-close", PostedDate: "2026-07-15",
				Description: "Non-transaction provider metadata", AmountCents: 0,
				Disposition: ledger.SourceDispositionSourceOnly, ExclusionReason: "provider metadata, not account activity",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err != nil {
			t.Fatalf("terminal SOURCE_ONLY evidence blocked closure: %v", err)
		}
		if _, err := f.store.Doctor(ctx); err != nil {
			t.Fatalf("doctor after terminal source evidence: %v", err)
		}
	})
}

func TestPrecoverageClosureFreezesCompleteActiveIdentitySet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	secondAlias := ledger.AddStatementAccountIdentityInput{
		StatementAccount: account.Code, SourceSystem: "FINANCES", SourceRealm: "GLOBAL",
		ExternalID: "finances-provider-account-2276", AccountNumber: "2276", Name: "Owner's Comp", Active: true,
		Evidence: ledger.StatementAccountIdentityEvidence{
			SourceKind: "SQLITE_ACCOUNT_REGISTRY", SourcePath: "/evidence/finance.sqlite",
			SourceSHA256: strings.Repeat("3", 64), Locator: "accounts[provider-account-2276]",
			PayloadSHA256: strings.Repeat("4", 64),
		},
	}
	secondIdentity, err := f.service.AddStatementAccountIdentity(ctx, secondAlias)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ActiveIdentityCount != 2 || len(preview.ActiveIdentityDigest) != 64 {
		t.Fatalf("multiple-alias binding = %+v", preview)
	}
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.ActiveIdentityCount != 2 || created.ActiveIdentityDigest != preview.ActiveIdentityDigest {
		t.Fatalf("committed identity binding = %+v, preview = %+v", created, preview)
	}
	idempotentIdentity, err := f.service.AddStatementAccountIdentity(ctx, secondAlias)
	if err != nil {
		t.Fatalf("idempotent post-certificate identity add: %v", err)
	}
	if idempotentIdentity.ID != secondIdentity.ID {
		t.Fatalf("idempotent post-certificate identity = %s, want %s", idempotentIdentity.ID, secondIdentity.ID)
	}

	postCertificateAlias := secondAlias
	postCertificateAlias.SourceSystem = "QBO"
	postCertificateAlias.SourceRealm = "ACME_QBO"
	postCertificateAlias.ExternalID = "new-active-alias"
	postCertificateAlias.AccountNumber = "10100"
	postCertificateAlias.Name = "New alias"
	postCertificateAlias.Evidence.SourceKind = "QBO_OBJECT_DIRECTORY"
	postCertificateAlias.Evidence.Locator = "Account.json#new-active-alias"
	if _, err := f.service.ValidateStatementAccountIdentity(ctx, postCertificateAlias); err == nil {
		t.Fatal("post-certificate identity preview unexpectedly succeeded")
	} else {
		requireAppErrorCode(t, err, "STATEMENT_ACCOUNT_IDENTITY_SET_FROZEN")
	}
	if _, err := f.service.AddStatementAccountIdentity(ctx, postCertificateAlias); err == nil {
		t.Fatal("post-certificate identity add unexpectedly succeeded")
	} else {
		requireAppErrorCode(t, err, "STATEMENT_ACCOUNT_IDENTITY_SET_FROZEN")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_account_identities
		(id, statement_account_id, source_system, source_realm, external_id, account_number, account_name,
		 source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		 evidence_locator, evidence_payload_sha256, created_at, created_by)
		SELECT 'direct-post-certificate-alias', statement_account_id, 'QBO', 'ACME_QBO',
		       'direct-post-certificate-alias', '10100', 'Direct alias', 1,
		       evidence_source_kind, evidence_source_path, evidence_source_sha256,
		       evidence_locator, evidence_payload_sha256, created_at, created_by
		FROM statement_account_identities WHERE id = ?`, created.StatementAccountIdentityID); err == nil {
		t.Fatal("direct post-certificate identity add unexpectedly succeeded")
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after blocked identity changes: %v", err)
	}
}

func TestPrecoverageClosureRejectsContradictoryProviderAliases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	if _, err := f.service.AddStatementAccountIdentity(ctx, ledger.AddStatementAccountIdentityInput{
		StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceRealm: "GLOBAL",
		ExternalID: "different-provider-account", AccountNumber: "9876549999", Name: "Different account", Active: true,
		Evidence: ledger.StatementAccountIdentityEvidence{
			SourceKind: "PROVIDER_ACCOUNT_EXPORT", SourcePath: "/evidence/accounts.json",
			SourceSHA256: strings.Repeat("5", 64), Locator: "accounts[different-provider-account]",
			PayloadSHA256: strings.Repeat("6", 64),
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.ValidateStatementAccountPrecoverageClosure(ctx, input)
	requireAppErrorCode(t, err, "PRECOVERAGE_IDENTITY_SET_CONFLICT")
}

func TestDoctorDetectsPrecoverageIdentityDigestDriftWithUnchangedCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-08", StartDate: "2026-08-01", EndDate: "2026-08-31",
		FiscalYear: 2026, PeriodNumber: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("close period before constructing later identity drift: %v", err)
	}
	var immutableTriggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'statement_account_identities_immutable_update'`).Scan(&immutableTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER statement_account_identities_immutable_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE statement_account_identities
		SET evidence_locator = evidence_locator || '#tampered'
		WHERE id = ?`, created.StatementAccountIdentityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, immutableTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if err := f.store.VerifySchema(ctx); err != nil {
		t.Fatalf("restored schema after constructing identity drift: %v", err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-08", true); err == nil {
		t.Fatal("supported period close ignored same-count identity digest drift")
	} else {
		requireAppErrorCode(t, err, "PERIOD_CLOSE_BLOCKED")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want digest drift to invalidate one certificate", result.InvalidStatementLifecycles)
	}
}

func TestPrecoverageClosureBlocksLaterSourceObservationsAndRevisions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceName: "late.json",
		FileSHA256: strings.Repeat("8", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "late-pending", PostedDate: "2026-07-15", Description: "Late pending evidence",
			AmountCents: -2_500, Disposition: ledger.SourceDispositionPending, ExclusionReason: "awaiting review",
		}},
	}); err == nil {
		t.Fatal("supported post-certificate source import unexpectedly succeeded")
	} else {
		requireAppErrorCode(t, err, "STATEMENT_ACCOUNT_NOT_ACTIVE")
	}

	var entityID, bookID string
	if err := f.store.DB().QueryRowContext(ctx, "SELECT entity_id, book_id FROM statement_accounts WHERE id = ?", account.ID).Scan(&entityID, &bookID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := tx.ExecContext(ctx, `INSERT INTO import_batches
		(id, source_system, entity_id, source_name, file_sha256, status, record_count, created_at, completed_at)
		VALUES ('guard-batch', 'PROVIDER_API', ?, 'guard.json', ?, 'COMPLETED', 1,
		        '2026-08-10T00:00:00Z', '2026-08-10T00:00:00Z')`, entityID, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_identities
		(id, entity_id, book_id, materialization_kind, statement_account_id, source_system,
		 source_account, external_id, created_at, created_by)
		VALUES ('guard-identity', ?, ?, 'STATEMENT', ?, 'PROVIDER_API', ?, 'guard-pending',
		        '2026-08-10T00:00:00Z', 'direct')`, entityID, bookID, account.ID, account.Code); err != nil {
		t.Fatal(err)
	}
	rawJSON := `{"id":"guard-pending"}`
	rawDigest := sha256.Sum256([]byte(rawJSON))
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, observation_kind, transaction_date,
		 description, amount_cents, disposition, exclusion_reason, payload_sha256, raw_json, created_at, created_by)
		VALUES ('guard-record', 'guard-identity', 'guard-batch', 1, 'PROVIDER', '2026-07-15',
		        'Guard pending evidence', -2500, 'PENDING', 'awaiting review', ?, ?,
		        '2026-08-10T00:00:00Z', 'direct')`, hex.EncodeToString(rawDigest[:]), rawJSON); err == nil {
		t.Fatal("direct post-certificate source observation unexpectedly succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after blocked source observations: %v", err)
	}
}

func TestDoctorAndPeriodCloseDetectPostCertificateSourceAndIdentityDrift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input := createPrecoverageClosureFixture(t)
	created, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var sourceTriggerSQL, identityTriggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'source_records_block_precoverage_closure_insert'`).Scan(&sourceTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'statement_account_identities_block_precoverage_closure_insert'`).Scan(&identityTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER source_records_block_precoverage_closure_insert"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER statement_account_identities_block_precoverage_closure_insert"); err != nil {
		t.Fatal(err)
	}
	var entityID, bookID string
	if err := f.store.DB().QueryRowContext(ctx, "SELECT entity_id, book_id FROM statement_accounts WHERE id = ?", account.ID).Scan(&entityID, &bookID); err != nil {
		t.Fatal(err)
	}
	rawJSON := `{"id":"late-pending"}`
	rawDigest := sha256.Sum256([]byte(rawJSON))
	for _, statement := range []string{
		`INSERT INTO import_batches (id, source_system, entity_id, source_name, file_sha256, status, record_count, created_at, completed_at)
		 VALUES ('late-batch', 'PROVIDER_API', '` + entityID + `', 'late.json', '` + strings.Repeat("7", 64) + `', 'COMPLETED', 1, '2026-08-10T00:00:00Z', '2026-08-10T00:00:00Z')`,
		`INSERT INTO source_identities (id, entity_id, book_id, materialization_kind, statement_account_id,
		 source_system, source_account, external_id, created_at, created_by)
		 VALUES ('late-source-identity', '` + entityID + `', '` + bookID + `', 'STATEMENT', '` + account.ID + `',
		 'PROVIDER_API', '` + account.Code + `', 'late-pending', '2026-08-10T00:00:00Z', 'direct')`,
		`INSERT INTO source_records (id, source_identity_id, import_batch_id, revision, observation_kind,
		 transaction_date, description, amount_cents, disposition, exclusion_reason, payload_sha256, raw_json, created_at, created_by)
		 VALUES ('late-source-record', 'late-source-identity', 'late-batch', 1, 'PROVIDER',
		 '2026-07-15', 'Late pending evidence', -2500, 'PENDING', 'awaiting review', '` + hex.EncodeToString(rawDigest[:]) + `', '` + rawJSON + `', '2026-08-10T00:00:00Z', 'direct')`,
		`INSERT INTO statement_account_identities
		 (id, statement_account_id, source_system, source_realm, external_id, account_number, account_name,
		  source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		  evidence_locator, evidence_payload_sha256, created_at, created_by)
		 SELECT 'late-active-alias', statement_account_id, 'QBO', 'ACME_QBO', 'late-active-alias',
		        '10100', 'Late active alias', 1, evidence_source_kind, evidence_source_path,
		        evidence_source_sha256, evidence_locator, evidence_payload_sha256, created_at, created_by
		 FROM statement_account_identities WHERE id = '` + created.StatementAccountIdentityID + `'`,
	} {
		if _, err := f.store.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.DB().ExecContext(ctx, sourceTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, identityTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("period close ignored post-certificate lifecycle drift")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want 1", result.InvalidStatementLifecycles)
	}
}

func TestPrecoverageClosureMakesCompletedReconciliationTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _, input := createPrecoverageClosureFixture(t)
	reconciliation, err := f.service.StartReconciliation(ctx, input.StatementAccount, input.ClosedOn, input.ClosedOn, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteReconciliation(ctx, reconciliation.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReopenReconciliation(ctx, reconciliation.ID, "rework certified evidence"); err == nil {
		t.Fatal("supported reopen invalidated a precoverage closure")
	} else {
		requireAppErrorCode(t, err, "RECONCILIATION_PRECOVERAGE_CLOSED")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET status = 'OPEN', completed_at = NULL, completed_by = NULL,
		    reopened_at = '2026-08-10T00:00:00Z', reopened_by = 'direct', reopen_reason = 'rework'
		WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("direct reopen invalidated a precoverage closure")
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO reconciliations
		(id, statement_account_id, start_date, end_date, beginning_balance_cents, ending_balance_cents,
		 status, created_at, created_by)
		SELECT 'direct-later-reconciliation', statement_account_id, date(start_date, '-1 day'), date(end_date, '-1 day'),
		       beginning_balance_cents, ending_balance_cents, 'OPEN', '2026-08-10T00:00:00Z', 'direct'
		FROM reconciliations WHERE id = ?`, reconciliation.ID); err == nil {
		t.Fatal("direct reconciliation insert bypassed terminal precoverage closure")
	}
	var reopenTriggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'reconciliations_block_precoverage_closure_reopen'`).Scan(&reopenTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "DROP TRIGGER reconciliations_block_precoverage_closure_reopen"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reconciliations
		SET status = 'OPEN', completed_at = NULL, completed_by = NULL,
		    reopened_at = '2026-08-10T00:00:00Z', reopened_by = 'direct', reopen_reason = 'bypass guard'
		WHERE id = ?`, reconciliation.ID); err != nil {
		t.Fatalf("construct bypassed reopen drift: %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, reopenTriggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("period close exemption ignored a reopened precoverage reconciliation")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want 1", result.InvalidStatementLifecycles)
	}
}

func TestDoctorAndPeriodCloseRequireConsistentPrecoverageAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, input, preview := createDirectPrecoverageLifecycle(t)
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("unaudited direct lifecycle row unexpectedly exempted required reconciliation coverage")
	}
	if _, err := f.service.CloseStatementAccountBeforeCoverage(ctx, input); err == nil {
		t.Fatal("supported retry treated unaudited direct lifecycle as idempotent success")
	} else {
		requireAppErrorCode(t, err, "PRECOVERAGE_LIFECYCLE_INVALID")
	}
	if err := directCloseBookPeriod(ctx, f, "ACME", "2026-07"); err == nil {
		t.Fatal("database close trigger accepted an unaudited direct lifecycle row")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count = %d, want 1", result.InvalidStatementLifecycles)
	}

	// The closure event must not claim an identity set. The separately audited
	// binding aggregate is the sole authoritative identity-set assertion.
	closurePayload := directPrecoverageClosureAuditPayload(t, preview, "direct")
	closurePayload["active_identity_count"] = preview.ActiveIdentityCount + 1
	closurePayload["active_identity_digest"] = strings.Repeat("0", 64)
	appendDirectPrecoverageAudits(t, f, account, preview, closurePayload)
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("conflicting identity claims unexpectedly exempted reconciliation coverage")
	}
	result, err = f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count after conflicting identity claims = %d, want 1", result.InvalidStatementLifecycles)
	}
}

func TestPrecoverageClosureAuditMustMatchArchiveTransition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, account, _, preview := createDirectPrecoverageLifecycle(t)
	closurePayload := directPrecoverageClosureAuditPayload(t, preview, "different-archive-actor")
	appendDirectPrecoverageAudits(t, f, account, preview, closurePayload)

	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", true); err == nil {
		t.Fatal("archive-inconsistent lifecycle audit unexpectedly exempted reconciliation coverage")
	}
	result, err := f.store.Doctor(ctx)
	requireAppErrorCode(t, err, "DATABASE_INVARIANT_FAILED")
	if result.InvalidStatementLifecycles != 1 {
		t.Fatalf("invalid lifecycle count after archive-inconsistent audit = %d, want 1", result.InvalidStatementLifecycles)
	}
}
