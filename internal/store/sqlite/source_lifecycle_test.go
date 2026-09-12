package sqlite_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func TestPendingSourceObservationFinalizesAsOnePostedMaterialization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)

	first, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "pending.json",
		FileSHA256: strings.Repeat("1", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "provider-1", PostedDate: "2026-07-30", Description: "Pending receipt",
			AmountCents: 12_345, Disposition: ledger.SourceDispositionPending,
			ExclusionReason: "provider pending", RawJSON: json.RawMessage(`{"id":"provider-1","pending":true}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.StatementTransactionCount != 0 || first.SourceOnlyCount != 1 {
		t.Fatalf("pending import = %+v", first)
	}
	initial, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial current source = %+v, %v", initial, err)
	}
	if initial[0].Revision != 1 || !initial[0].Current || initial[0].Disposition != ledger.SourceDispositionPending {
		t.Fatalf("initial observation = %+v", initial[0])
	}

	finalized, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "posted.json",
		FileSHA256: strings.Repeat("2", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "provider-1", PostedDate: "2026-07-31", Description: "Posted receipt",
			AmountCents: 12_345, RawJSON: json.RawMessage(`{"id":"provider-1","pending":false}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.StatementTransactionCount != 1 || finalized.SourceOnlyCount != 0 {
		t.Fatalf("finalized import = %+v", finalized)
	}
	current, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(current) != 1 {
		t.Fatalf("current source = %+v, %v", current, err)
	}
	if current[0].Revision != 2 || !current[0].Current || current[0].Disposition != ledger.SourceDispositionPosted ||
		current[0].SupersedesSourceRecordID != initial[0].ID || current[0].ObservationKind != "PROVIDER" {
		t.Fatalf("final observation = %+v", current[0])
	}
	history, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{IncludeHistory: true})
	if err != nil || len(history) != 2 || history[0].Current || !history[1].Current {
		t.Fatalf("source history = %+v, %v", history, err)
	}
	var transactionCount int
	var materializedSourceID, materializedIdentityID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*), MIN(source_record_id), MIN(source_identity_id)
		FROM statement_transactions`).Scan(&transactionCount, &materializedSourceID, &materializedIdentityID); err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 || materializedSourceID != current[0].ID || materializedIdentityID != current[0].SourceIdentityID {
		t.Fatalf("materialization count=%d source=%q identity=%q", transactionCount, materializedSourceID, materializedIdentityID)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_transactions
		(id, statement_account_id, source_identity_id, source_record_id, posted_date, description, amount_cents, created_at)
		SELECT 'duplicate-materialization', statement_account_id, source_identity_id, source_record_id,
		       posted_date, description, amount_cents, created_at
		FROM statement_transactions LIMIT 1`); err == nil {
		t.Fatal("logical source identity materialized twice")
	}

	_, err = f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "changed-posted.json",
		FileSHA256: strings.Repeat("3", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "provider-1", PostedDate: "2026-07-31", Description: "Changed after posting",
			AmountCents: 12_346, RawJSON: json.RawMessage(`{"id":"provider-1","amount":12346}`),
		}},
	})
	if appError, ok := apperr.As(err); !ok || appError.Code != "SOURCE_MATERIALIZED" {
		t.Fatalf("terminal posted transition error = %#v", err)
	}
	if result, err := f.store.Doctor(ctx); err != nil || !result.OK {
		t.Fatalf("doctor after finalized lifecycle = %+v, %v", result, err)
	}
}

func TestPendingSourceRemovalRequiresResolutionEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)

	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "pending.json",
		FileSHA256: strings.Repeat("4", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "removed-1", PostedDate: "2026-07-31", Description: "Pending charge",
			AmountCents: -500, Disposition: ledger.SourceDispositionPending,
			ExclusionReason: "provider pending", RawJSON: json.RawMessage(`{"id":"removed-1","pending":true}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	withoutEvidence := ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "removed.json",
		FileSHA256: strings.Repeat("5", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "removed-1", PostedDate: "2026-07-31", Description: "Removed pending charge",
			AmountCents: -500, Disposition: ledger.SourceDispositionSourceOnly,
			ExclusionReason: "provider removed before posting", RawJSON: json.RawMessage(`{"id":"removed-1","removed":true}`),
		}},
	}
	if _, err := f.service.ImportStatementTransactions(ctx, withoutEvidence); err == nil {
		t.Fatal("PENDING to SOURCE_ONLY succeeded without resolution evidence")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "SOURCE_TRANSITION_EVIDENCE_REQUIRED" {
		t.Fatalf("missing removal evidence error = %#v", err)
	}
	withoutEvidence.Transactions[0].ResolutionReason = "Provider confirmed the pending authorization disappeared"
	withoutEvidence.Transactions[0].ResolutionEvidence = json.RawMessage(`{"snapshot":"2026-08-02","provider_status":"absent"}`)
	result, err := f.service.ImportStatementTransactions(ctx, withoutEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatementTransactionCount != 0 || result.SourceOnlyCount != 1 {
		t.Fatalf("removed result = %+v", result)
	}
	current, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(current) != 1 {
		t.Fatalf("removed current = %+v, %v", current, err)
	}
	if current[0].Revision != 2 || current[0].ObservationKind != "RESOLUTION" ||
		current[0].Disposition != ledger.SourceDispositionSourceOnly || current[0].ResolutionReason == "" ||
		len(current[0].ResolutionEvidenceSHA) != 64 || current[0].StatementTransactionID != "" {
		t.Fatalf("removed resolution = %+v", current[0])
	}
	if result, err := f.store.Doctor(ctx); err != nil || !result.OK {
		t.Fatalf("doctor after removal resolution = %+v, %v", result, err)
	}
}

func TestNeedsReviewPostingRequiresExplicitResolutionEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)

	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "review.json",
		FileSHA256: strings.Repeat("6", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "review-1", PostedDate: "2026-07-31", Description: "Ambiguous deposit",
			AmountCents: 900, Disposition: ledger.SourceDispositionNeedsReview,
			ExclusionReason: "duplicate candidate", RawJSON: json.RawMessage(`{"id":"review-1","status":"posted"}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "reviewed.json",
		FileSHA256: strings.Repeat("7", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "review-1", PostedDate: "2026-07-31", Description: "Ambiguous deposit",
			AmountCents: 900, RawJSON: json.RawMessage(`{"id":"review-1","status":"posted"}`),
		}},
	}
	if _, err := f.service.ImportStatementTransactions(ctx, resolved); err == nil {
		t.Fatal("NEEDS_REVIEW to POSTED succeeded without resolution evidence")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "SOURCE_TRANSITION_EVIDENCE_REQUIRED" {
		t.Fatalf("missing review evidence error = %#v", err)
	}
	resolved.Transactions[0].ResolutionReason = "Matched against the provider statement and deposit detail"
	resolved.Transactions[0].ResolutionEvidence = json.RawMessage(`{"statement_page":3,"deposit_trace":"trace-42"}`)
	result, err := f.service.ImportStatementTransactions(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatementTransactionCount != 1 || result.SourceOnlyCount != 0 {
		t.Fatalf("review resolution result = %+v", result)
	}
	current, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(current) != 1 {
		t.Fatalf("review current = %+v, %v", current, err)
	}
	if current[0].Revision != 2 || current[0].ObservationKind != "RESOLUTION" ||
		current[0].Disposition != ledger.SourceDispositionPosted || current[0].StatementTransactionID == "" {
		t.Fatalf("review resolution = %+v", current[0])
	}
	if result, err := f.store.Doctor(ctx); err != nil || !result.OK {
		t.Fatalf("doctor after review resolution = %+v, %v", result, err)
	}
}

func TestArchivedStatementAccountRejectsPostedSourceRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account := createCashStatementAccount(t, f, false)
	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: account.Code, SourceSystem: "BANK", SourceName: "pending-before-archive.json",
		FileSHA256: strings.Repeat("8", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "archive-pending", PostedDate: "2026-07-31", Description: "Pending before archive",
			AmountCents: 100, Disposition: ledger.SourceDispositionPending,
			ExclusionReason: "provider pending", RawJSON: json.RawMessage(`{"id":"archive-pending","pending":true}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ArchiveStatementAccount(ctx, ledger.ArchiveStatementAccountInput{
		Code: account.Code, ReconciliationRequiredThrough: "2026-07-31", Reason: "Provider account closed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
		 observation_kind, transaction_date, description, amount_cents, disposition,
		 payload_sha256, raw_json, created_at, created_by)
		SELECT 'posted-after-archive', source_identity_id, import_batch_id, revision + 1, id,
		       'PROVIDER', transaction_date, description, amount_cents, 'POSTED',
		       payload_sha256, raw_json, '2026-08-04T00:00:00Z', 'direct'
		FROM current_source_records WHERE external_id = 'archive-pending'`); err == nil {
		t.Fatal("POSTED source revision for an archived statement account unexpectedly succeeded")
	}
	current, err := f.service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(current) != 1 || current[0].Revision != 1 ||
		current[0].Disposition != ledger.SourceDispositionPending {
		t.Fatalf("source changed after rejected archived posting: %+v, %v", current, err)
	}
}

func TestDoctorDetectsInvalidSourceRevisionAndMissingMaterialization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)
	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "review-before-tamper.json",
		FileSHA256: strings.Repeat("9", 64), Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "tampered-review", PostedDate: "2026-07-31", Description: "Review before tamper",
			AmountCents: 100, Disposition: ledger.SourceDispositionNeedsReview,
			ExclusionReason: "manual review", RawJSON: json.RawMessage(`{"id":"tampered-review"}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var triggerSQL string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'source_records_validate_insert'`).Scan(&triggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `DROP TRIGGER source_records_validate_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
		 observation_kind, transaction_date, description, amount_cents, disposition,
		 payload_sha256, raw_json, created_at, created_by)
		SELECT 'invalid-posted-revision', source_identity_id, import_batch_id, revision + 1, id,
		       'PROVIDER', transaction_date, description, amount_cents, 'POSTED',
		       payload_sha256, raw_json, '2026-08-04T00:00:00Z', 'tamper'
		FROM current_source_records WHERE external_id = 'tampered-review'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, triggerSQL); err != nil {
		t.Fatal(err)
	}
	result, err := f.store.Doctor(ctx)
	if err == nil {
		t.Fatal("doctor unexpectedly accepted an invalid provider resolution with no materialization")
	}
	if result.InvalidSourceRevisions != 1 || result.InvalidMaterializations != 1 {
		t.Fatalf("doctor source failures = revisions %d, materializations %d", result.InvalidSourceRevisions, result.InvalidMaterializations)
	}
}

func TestDoctorRecomputesSourceEvidenceHashes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	createCashStatementAccount(t, f, false)
	_, err := f.service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "hash-evidence.json",
		FileSHA256: strings.Repeat("a", 64),
		Transactions: []ledger.StatementTransactionInput{
			{
				ExternalID: "bad-payload-hash", PostedDate: "2026-07-30", Description: "Payload evidence",
				AmountCents: 100, Disposition: ledger.SourceDispositionPending,
				ExclusionReason: "provider pending", RawJSON: json.RawMessage(`{"id":"bad-payload-hash"}`),
			},
			{
				ExternalID: "bad-resolution-hash", PostedDate: "2026-07-31", Description: "Resolution evidence",
				AmountCents: 200, Disposition: ledger.SourceDispositionPending,
				ExclusionReason: "provider pending", RawJSON: json.RawMessage(`{"id":"bad-resolution-hash"}`),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
		 observation_kind, transaction_date, description, amount_cents, disposition,
		 exclusion_reason, payload_sha256, raw_json, created_at, created_by)
		SELECT 'mismatched-payload-hash', source_identity_id, import_batch_id, revision + 1, id,
		       'PROVIDER', transaction_date, description, amount_cents, disposition,
		       exclusion_reason, ?, raw_json, '2026-08-04T00:00:00Z', 'tamper'
		FROM current_source_records WHERE external_id = 'bad-payload-hash'`, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
		 observation_kind, transaction_date, description, amount_cents, disposition,
		 exclusion_reason, payload_sha256, raw_json, resolution_reason,
		 resolution_evidence_json, resolution_evidence_sha256, created_at, created_by)
		SELECT 'mismatched-resolution-hash', source_identity_id, import_batch_id, revision + 1, id,
		       'RESOLUTION', transaction_date, description, amount_cents, 'SOURCE_ONLY',
		       'provider removed pending item', payload_sha256, raw_json, 'Provider confirmed removal',
		       '{"provider_status":"absent"}', ?, '2026-08-04T00:00:00Z', 'tamper'
		FROM current_source_records WHERE external_id = 'bad-resolution-hash'`, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}

	result, err := f.store.Doctor(ctx)
	if err == nil {
		t.Fatal("doctor unexpectedly accepted mismatched source evidence hashes")
	}
	if result.InvalidSourceEvidence != 2 || result.InvalidSourceRevisions != 0 || result.InvalidMaterializations != 0 {
		t.Fatalf("doctor source evidence failures = %+v", result)
	}
}
