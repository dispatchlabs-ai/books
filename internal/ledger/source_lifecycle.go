package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

const (
	sourceMaterializationStatement = "STATEMENT"
	sourceMaterializationJournal   = "JOURNAL"
	sourceObservationProvider      = "PROVIDER"
	sourceObservationResolution    = "RESOLUTION"
)

type sourceIdentityInput struct {
	EntityID            string
	BookID              string
	MaterializationKind string
	StatementAccountID  string
	SourceSystem        string
	SourceAccount       string
	ExternalID          string
}

type sourceObservationInput struct {
	ImportBatchID    string
	TransactionDate  string
	Description      string
	AmountCents      int64
	TaxType          string
	TaxPeriod        string
	Disposition      string
	ExclusionReason  string
	RawJSON          json.RawMessage
	ResolutionReason string
	ResolutionJSON   json.RawMessage
}

type preparedSourceObservation struct {
	sourceObservationInput
	ObservationKind     string
	PayloadSHA256       string
	ResolutionSHA256    string
	ResolutionJSONValue any
}

type currentSourceObservation struct {
	ID               string
	Revision         int
	ObservationKind  string
	TransactionDate  string
	Description      string
	AmountCents      int64
	TaxType          string
	TaxPeriod        string
	Disposition      string
	ExclusionReason  string
	PayloadSHA256    string
	ResolutionReason string
	ResolutionSHA256 string
}

func (s *Service) ensureSourceIdentity(ctx context.Context, tx *sql.Tx, input sourceIdentityInput) (string, bool, error) {
	var id, bookID, kind, sourceSystem, sourceAccount, externalID string
	var entityID sql.NullString
	var statementAccountID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, entity_id, book_id, materialization_kind, statement_account_id,
		source_system, source_account, external_id
		FROM source_identities
		WHERE source_system = ? AND source_account = ? AND external_id = ?`,
		input.SourceSystem, input.SourceAccount, input.ExternalID).Scan(
		&id, &entityID, &bookID, &kind, &statementAccountID, &sourceSystem, &sourceAccount, &externalID)
	if err == nil {
		if entityID.String != input.EntityID || bookID != input.BookID || kind != input.MaterializationKind ||
			statementAccountID.String != input.StatementAccountID ||
			sourceSystem != input.SourceSystem || sourceAccount != input.SourceAccount || externalID != input.ExternalID {
			return "", false, apperr.New(apperr.Conflict, "SOURCE_IDENTITY_CONFLICT", "logical source identity is already bound to different accounting context")
		}
		return id, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, storesqlite.MapError("read source identity", err)
	}
	id, err = storesqlite.NewID()
	if err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_identities
		(id, entity_id, book_id, materialization_kind, statement_account_id, source_system, source_account,
		 external_id, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, nilIfEmpty(input.EntityID), input.BookID,
		input.MaterializationKind, nilIfEmpty(input.StatementAccountID), input.SourceSystem, input.SourceAccount, input.ExternalID,
		storesqlite.UTCNow(), s.actor); err != nil {
		return "", false, storesqlite.MapError("create source identity", err)
	}
	return id, true, nil
}

func prepareSourceObservation(input sourceObservationInput) (preparedSourceObservation, error) {
	prepared := preparedSourceObservation{sourceObservationInput: input, ObservationKind: sourceObservationProvider}
	prepared.Description = strings.TrimSpace(prepared.Description)
	prepared.TaxType = normalizeCode(prepared.TaxType)
	prepared.TaxPeriod = strings.TrimSpace(prepared.TaxPeriod)
	prepared.Disposition = normalizeCode(prepared.Disposition)
	prepared.ExclusionReason = strings.TrimSpace(prepared.ExclusionReason)
	prepared.ResolutionReason = strings.TrimSpace(prepared.ResolutionReason)
	if !json.Valid(prepared.RawJSON) {
		return prepared, apperr.New(apperr.Input, "SOURCE_RAW_JSON_INVALID", "source raw JSON must be valid JSON")
	}
	payloadHash := sha256.Sum256(prepared.RawJSON)
	prepared.PayloadSHA256 = hex.EncodeToString(payloadHash[:])

	evidence := strings.TrimSpace(string(prepared.ResolutionJSON))
	if prepared.ResolutionReason == "" && evidence == "" {
		return prepared, nil
	}
	if prepared.ResolutionReason == "" || evidence == "" {
		return prepared, apperr.New(apperr.Input, "SOURCE_RESOLUTION_EVIDENCE_INVALID", "source resolution requires both a reason and JSON evidence")
	}
	if !json.Valid(prepared.ResolutionJSON) || evidence == "null" {
		return prepared, apperr.New(apperr.Input, "SOURCE_RESOLUTION_EVIDENCE_INVALID", "source resolution evidence must be non-null valid JSON")
	}
	prepared.ObservationKind = sourceObservationResolution
	evidenceHash := sha256.Sum256(prepared.ResolutionJSON)
	prepared.ResolutionSHA256 = hex.EncodeToString(evidenceHash[:])
	prepared.ResolutionJSONValue = string(prepared.ResolutionJSON)
	return prepared, nil
}

func readCurrentSourceObservation(ctx context.Context, tx *sql.Tx, sourceIdentityID string) (currentSourceObservation, bool, error) {
	var current currentSourceObservation
	err := tx.QueryRowContext(ctx, `SELECT id, revision, observation_kind, transaction_date,
		description, amount_cents, COALESCE(tax_type, ''), COALESCE(tax_accounting_period, ''),
		disposition, COALESCE(exclusion_reason, ''), payload_sha256,
		COALESCE(resolution_reason, ''), COALESCE(resolution_evidence_sha256, '')
		FROM current_source_records WHERE source_identity_id = ?`, sourceIdentityID).Scan(
		&current.ID, &current.Revision, &current.ObservationKind, &current.TransactionDate,
		&current.Description, &current.AmountCents, &current.TaxType, &current.TaxPeriod,
		&current.Disposition, &current.ExclusionReason, &current.PayloadSHA256,
		&current.ResolutionReason, &current.ResolutionSHA256)
	if err == sql.ErrNoRows {
		return current, false, nil
	}
	if err != nil {
		return current, false, storesqlite.MapError("read current source observation", err)
	}
	return current, true, nil
}

func sourceObservationMatches(current currentSourceObservation, candidate preparedSourceObservation) bool {
	return current.ObservationKind == candidate.ObservationKind &&
		current.TransactionDate == candidate.TransactionDate &&
		current.Description == candidate.Description &&
		current.AmountCents == candidate.AmountCents &&
		current.TaxType == candidate.TaxType &&
		current.TaxPeriod == candidate.TaxPeriod &&
		current.Disposition == candidate.Disposition &&
		current.ExclusionReason == candidate.ExclusionReason &&
		current.PayloadSHA256 == candidate.PayloadSHA256 &&
		current.ResolutionReason == candidate.ResolutionReason &&
		current.ResolutionSHA256 == candidate.ResolutionSHA256
}

func validateSourceTransition(current currentSourceObservation, candidate preparedSourceObservation) error {
	if current.Disposition == SourceDispositionPosted {
		return apperr.New(apperr.Conflict, "SOURCE_MATERIALIZED", "a posted source observation is terminal; use a new provider identity for a correction")
	}
	if candidate.ObservationKind == sourceObservationResolution {
		if candidate.Disposition == current.Disposition {
			return apperr.New(apperr.Input, "SOURCE_RESOLUTION_NO_TRANSITION", "source resolution evidence must change the disposition")
		}
		return nil
	}
	allowed := false
	switch current.Disposition {
	case SourceDispositionPending:
		allowed = candidate.Disposition == SourceDispositionPending ||
			candidate.Disposition == SourceDispositionNeedsReview ||
			candidate.Disposition == SourceDispositionPosted
	case SourceDispositionNeedsReview:
		allowed = candidate.Disposition == SourceDispositionNeedsReview
	case SourceDispositionSourceOnly:
		allowed = candidate.Disposition == SourceDispositionSourceOnly
	}
	if !allowed {
		return apperr.New(apperr.Validation, "SOURCE_TRANSITION_EVIDENCE_REQUIRED", "this source disposition transition requires an explicit resolution reason and JSON evidence")
	}
	return nil
}

func (s *Service) insertSourceObservation(ctx context.Context, tx *sql.Tx, sourceIdentityID string, candidate preparedSourceObservation, current *currentSourceObservation) (string, int, error) {
	revision := 1
	var supersedes any
	if current != nil {
		revision = current.Revision + 1
		supersedes = current.ID
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return "", 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_records
		(id, source_identity_id, import_batch_id, revision, supersedes_source_record_id,
		 observation_kind, transaction_date, description, amount_cents, tax_type,
		 tax_accounting_period, disposition, exclusion_reason, payload_sha256, raw_json,
		 resolution_reason, resolution_evidence_json, resolution_evidence_sha256,
		 created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sourceIdentityID, candidate.ImportBatchID, revision, supersedes,
		candidate.ObservationKind, candidate.TransactionDate, candidate.Description,
		candidate.AmountCents, nilIfEmpty(candidate.TaxType), nilIfEmpty(candidate.TaxPeriod),
		candidate.Disposition, nilIfEmpty(candidate.ExclusionReason), candidate.PayloadSHA256,
		string(candidate.RawJSON), nilIfEmpty(candidate.ResolutionReason), candidate.ResolutionJSONValue,
		nilIfEmpty(candidate.ResolutionSHA256), storesqlite.UTCNow(), s.actor); err != nil {
		return "", 0, storesqlite.MapError("insert source observation", err)
	}
	return id, revision, nil
}
