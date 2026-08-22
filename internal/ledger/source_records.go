package ledger

import (
	"context"
	"database/sql"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type SourceRecordFilter struct {
	SourceAccount  string
	Disposition    string
	FromDate       string
	ToDate         string
	IncludeHistory bool
}

type SourceRecord struct {
	ID                       string `json:"id"`
	SourceIdentityID         string `json:"source_identity_id"`
	ImportBatchID            string `json:"import_batch_id"`
	ImportSourceName         string `json:"import_source_name"`
	ImportFileSHA256         string `json:"import_file_sha256"`
	EntityCode               string `json:"entity,omitempty"`
	BookCode                 string `json:"book,omitempty"`
	SourceSystem             string `json:"source_system"`
	SourceAccount            string `json:"source_account"`
	ExternalID               string `json:"external_id"`
	SourceLocator            string `json:"source_locator"`
	MaterializationKind      string `json:"materialization_kind"`
	Revision                 int    `json:"revision"`
	SupersedesSourceRecordID string `json:"supersedes_source_record_id,omitempty"`
	ObservationKind          string `json:"observation_kind"`
	Current                  bool   `json:"current"`
	TransactionDate          string `json:"transaction_date,omitempty"`
	Description              string `json:"description"`
	AmountCents              *int64 `json:"amount_cents,omitempty"`
	TaxType                  string `json:"tax_type,omitempty"`
	TaxAccountingPeriod      string `json:"tax_accounting_period,omitempty"`
	Disposition              string `json:"disposition"`
	ExclusionReason          string `json:"exclusion_reason,omitempty"`
	RawJSONSHA256            string `json:"raw_json_sha256"`
	ResolutionReason         string `json:"resolution_reason,omitempty"`
	ResolutionEvidenceSHA    string `json:"resolution_evidence_sha256,omitempty"`
	CreatedAt                string `json:"created_at"`
	CreatedBy                string `json:"created_by"`
	StatementTransactionID   string `json:"statement_transaction_id,omitempty"`
	JournalLinkCount         int    `json:"journal_link_count"`
}

type SourceJournalLink struct {
	SourceRecordID string `json:"source_record_id"`
	JournalID      string `json:"journal_id"`
	BookCode       string `json:"book"`
	EntryNumber    int64  `json:"entry_number"`
	LinkRole       string `json:"link_role"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	Changed        bool   `json:"changed,omitempty"`
}

func (s *Service) ListSourceRecords(ctx context.Context, filter SourceRecordFilter) ([]SourceRecord, error) {
	filter.SourceAccount = normalizeCode(filter.SourceAccount)
	filter.Disposition = normalizeCode(filter.Disposition)
	if filter.Disposition != "" {
		switch filter.Disposition {
		case SourceDispositionPosted, SourceDispositionPending, SourceDispositionNeedsReview, SourceDispositionSourceOnly:
		default:
			return nil, apperr.New(apperr.Invalid, "SOURCE_DISPOSITION_INVALID", "disposition must be POSTED, PENDING, NEEDS_REVIEW, or SOURCE_ONLY")
		}
	}
	if filter.FromDate != "" {
		if err := validateDate(filter.FromDate, "source from date"); err != nil {
			return nil, err
		}
	}
	if filter.ToDate != "" {
		if err := validateDate(filter.ToDate, "source to date"); err != nil {
			return nil, err
		}
	}
	if filter.FromDate != "" && filter.ToDate != "" && filter.ToDate < filter.FromDate {
		return nil, apperr.New(apperr.Invalid, "SOURCE_DATE_RANGE_INVALID", "source to date precedes from date")
	}
	query := `SELECT sr.id, sr.source_identity_id, sr.import_batch_id,
	        ib.source_name, ib.file_sha256, COALESCE(e.code, ''), b.code,
	        si.source_system, si.source_account, si.external_id, si.materialization_kind,
	        sr.revision, COALESCE(sr.supersedes_source_record_id, ''),
	        CASE WHEN EXISTS (
	                 SELECT 1 FROM source_record_operator_attestations attestation
	                 WHERE attestation.source_record_id = sr.id
	             ) AND sr.observation_kind = 'PROVIDER'
	             THEN 'OPERATOR_ATTESTATION' ELSE sr.observation_kind END,
	        NOT EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id),
	        sr.transaction_date, sr.description, sr.amount_cents,
	        COALESCE(sr.tax_type, ''), COALESCE(sr.tax_accounting_period, ''),
	        sr.disposition, COALESCE(sr.exclusion_reason, ''), sr.payload_sha256,
	        COALESCE(sr.resolution_reason, ''), COALESCE(sr.resolution_evidence_sha256, ''),
	        sr.created_at, sr.created_by, COALESCE(st.id, ''),
	        (SELECT COUNT(*) FROM source_record_journals srj WHERE srj.source_record_id = sr.id)
	        FROM source_records sr
	        JOIN source_identities si ON si.id = sr.source_identity_id
	        JOIN import_batches ib ON ib.id = sr.import_batch_id
	        LEFT JOIN entities e ON e.id = si.entity_id
	        JOIN books b ON b.id = si.book_id
	        LEFT JOIN statement_transactions st ON st.source_record_id = sr.id
	        WHERE 1=1`
	var args []any
	if !filter.IncludeHistory {
		query += " AND NOT EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id)"
	}
	if filter.SourceAccount != "" {
		query += " AND si.source_account = ?"
		args = append(args, filter.SourceAccount)
	}
	if filter.Disposition != "" {
		query += " AND sr.disposition = ?"
		args = append(args, filter.Disposition)
	}
	if filter.FromDate != "" {
		query += " AND sr.transaction_date >= ?"
		args = append(args, filter.FromDate)
	}
	if filter.ToDate != "" {
		query += " AND sr.transaction_date <= ?"
		args = append(args, filter.ToDate)
	}
	query += ` ORDER BY sr.transaction_date, si.source_account, si.external_id, sr.revision`
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list source records", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []SourceRecord
	for rows.Next() {
		var record SourceRecord
		var amount sql.NullInt64
		if err := rows.Scan(&record.ID, &record.SourceIdentityID, &record.ImportBatchID, &record.ImportSourceName,
			&record.ImportFileSHA256, &record.EntityCode, &record.BookCode,
			&record.SourceSystem, &record.SourceAccount, &record.ExternalID, &record.MaterializationKind,
			&record.Revision, &record.SupersedesSourceRecordID, &record.ObservationKind, &record.Current,
			&record.TransactionDate, &record.Description, &amount, &record.TaxType,
			&record.TaxAccountingPeriod, &record.Disposition, &record.ExclusionReason,
			&record.RawJSONSHA256, &record.ResolutionReason, &record.ResolutionEvidenceSHA,
			&record.CreatedAt, &record.CreatedBy,
			&record.StatementTransactionID, &record.JournalLinkCount); err != nil {
			return nil, err
		}
		record.SourceLocator = strings.Join([]string{record.SourceSystem, record.SourceAccount, record.ExternalID}, ":")
		if amount.Valid {
			value := amount.Int64
			record.AmountCents = &value
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

// GetSourceRecord returns immutable source metadata without returning raw_json.
func (s *Service) GetSourceRecord(ctx context.Context, id string) (SourceRecord, error) {
	var record SourceRecord
	var amount sql.NullInt64
	err := s.store.DB().QueryRowContext(ctx, `SELECT sr.id, sr.source_identity_id, sr.import_batch_id,
	        ib.source_name, ib.file_sha256, COALESCE(e.code, ''), b.code,
	        si.source_system, si.source_account, si.external_id, si.materialization_kind,
	        sr.revision, COALESCE(sr.supersedes_source_record_id, ''),
	        CASE WHEN EXISTS (
	                 SELECT 1 FROM source_record_operator_attestations attestation
	                 WHERE attestation.source_record_id = sr.id
	             ) AND sr.observation_kind = 'PROVIDER'
	             THEN 'OPERATOR_ATTESTATION' ELSE sr.observation_kind END,
	        NOT EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id),
	        sr.transaction_date, sr.description, sr.amount_cents,
	        COALESCE(sr.tax_type, ''), COALESCE(sr.tax_accounting_period, ''),
	        sr.disposition, COALESCE(sr.exclusion_reason, ''), sr.payload_sha256,
	        COALESCE(sr.resolution_reason, ''), COALESCE(sr.resolution_evidence_sha256, ''),
	        sr.created_at, sr.created_by, COALESCE(st.id, ''),
	        (SELECT COUNT(*) FROM source_record_journals srj WHERE srj.source_record_id = sr.id)
	        FROM source_records sr
	        JOIN source_identities si ON si.id = sr.source_identity_id
	        JOIN import_batches ib ON ib.id = sr.import_batch_id
	        LEFT JOIN entities e ON e.id = si.entity_id
	        JOIN books b ON b.id = si.book_id
	        LEFT JOIN statement_transactions st ON st.source_record_id = sr.id
	        WHERE sr.id = ?`, strings.TrimSpace(id)).Scan(
		&record.ID, &record.SourceIdentityID, &record.ImportBatchID, &record.ImportSourceName, &record.ImportFileSHA256,
		&record.EntityCode, &record.BookCode, &record.SourceSystem, &record.SourceAccount,
		&record.ExternalID, &record.MaterializationKind, &record.Revision,
		&record.SupersedesSourceRecordID, &record.ObservationKind, &record.Current,
		&record.TransactionDate, &record.Description, &amount, &record.TaxType,
		&record.TaxAccountingPeriod, &record.Disposition, &record.ExclusionReason,
		&record.RawJSONSHA256, &record.ResolutionReason, &record.ResolutionEvidenceSHA,
		&record.CreatedAt, &record.CreatedBy, &record.StatementTransactionID,
		&record.JournalLinkCount)
	if err == sql.ErrNoRows {
		return record, apperr.New(apperr.NotFound, "SOURCE_RECORD_NOT_FOUND", "source record was not found")
	}
	if err != nil {
		return record, storesqlite.MapError("read source record", err)
	}
	if amount.Valid {
		value := amount.Int64
		record.AmountCents = &value
	}
	record.SourceLocator = strings.Join([]string{record.SourceSystem, record.SourceAccount, record.ExternalID}, ":")
	return record, nil
}

func (s *Service) LinkSourceRecordJournal(ctx context.Context, sourceRecordID, journalID, role string) (SourceJournalLink, error) {
	if err := s.requireActor(); err != nil {
		return SourceJournalLink{}, err
	}
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	journalID = strings.TrimSpace(journalID)
	role = normalizeCode(role)
	if sourceRecordID == "" || journalID == "" {
		return SourceJournalLink{}, apperr.New(apperr.Invalid, "SOURCE_JOURNAL_LINK_INVALID", "source record and journal IDs are required")
	}
	switch role {
	case "PRIMARY", "EVIDENCE", "MIRROR", "ELIMINATION":
	default:
		return SourceJournalLink{}, apperr.New(apperr.Invalid, "SOURCE_JOURNAL_ROLE_INVALID", "link role must be PRIMARY, EVIDENCE, MIRROR, or ELIMINATION")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return SourceJournalLink{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var sourceEntityID, journalEntityID sql.NullString
	var sourceBookID, journalBookID string
	var disposition, bookKind, bookCode string
	var entryNumber int64
	err = tx.QueryRowContext(ctx, `SELECT si.entity_id, b.entity_id, si.book_id, je.book_id,
	        sr.disposition, b.kind, b.code, je.entry_number
	        FROM source_records sr
	        JOIN source_identities si ON si.id = sr.source_identity_id
	        CROSS JOIN journal_entries je
	        JOIN books b ON b.id = je.book_id
	        WHERE sr.id = ? AND je.id = ?
	          AND NOT EXISTS (SELECT 1 FROM source_records successor WHERE successor.supersedes_source_record_id = sr.id)`, sourceRecordID, journalID).Scan(
		&sourceEntityID, &journalEntityID, &sourceBookID, &journalBookID,
		&disposition, &bookKind, &bookCode, &entryNumber)
	if err == sql.ErrNoRows {
		return SourceJournalLink{}, apperr.New(apperr.NotFound, "SOURCE_OR_JOURNAL_NOT_FOUND", "source record or journal was not found")
	}
	if err != nil {
		return SourceJournalLink{}, storesqlite.MapError("read source and journal", err)
	}
	if disposition != SourceDispositionPosted {
		return SourceJournalLink{}, apperr.New(apperr.Validation, "SOURCE_DISPOSITION_NOT_POSTED", "only POSTED source records may link to journals")
	}
	if role == "EVIDENCE" && sourceBookID != journalBookID {
		return SourceJournalLink{}, apperr.New(apperr.Validation, "SOURCE_JOURNAL_BOOK_MISMATCH", "EVIDENCE links must remain in the source record's exact book")
	}
	crossEntity := sourceEntityID.Valid && journalEntityID.Valid && sourceEntityID.String != journalEntityID.String
	if crossEntity && bookKind != "ELIMINATION" && role != "MIRROR" && role != "ELIMINATION" {
		return SourceJournalLink{}, apperr.New(apperr.Validation, "SOURCE_JOURNAL_ENTITY_MISMATCH", "cross-entity links require a MIRROR or ELIMINATION role")
	}
	result := SourceJournalLink{SourceRecordID: sourceRecordID, JournalID: journalID, BookCode: bookCode, EntryNumber: entryNumber, LinkRole: role}
	var existingRole, createdAt, createdBy string
	err = tx.QueryRowContext(ctx, `SELECT link_role, created_at, created_by
        FROM source_record_journals WHERE source_record_id = ? AND journal_entry_id = ?`, sourceRecordID, journalID).Scan(
		&existingRole, &createdAt, &createdBy)
	if err == nil {
		if existingRole != role {
			return SourceJournalLink{}, apperr.New(apperr.Conflict, "SOURCE_JOURNAL_LINK_CONFLICT", "source record and journal are already linked with a different immutable role")
		}
		result.CreatedAt = createdAt
		result.CreatedBy = createdBy
		return result, nil
	}
	if err != sql.ErrNoRows {
		return SourceJournalLink{}, storesqlite.MapError("read source-journal link", err)
	}
	result.CreatedAt = storesqlite.UTCNow()
	result.CreatedBy = s.actor
	result.Changed = true
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_record_journals
        (source_record_id, journal_entry_id, link_role, created_at, created_by)
        VALUES (?, ?, ?, ?, ?)`, sourceRecordID, journalID, role, result.CreatedAt, result.CreatedBy); err != nil {
		return SourceJournalLink{}, storesqlite.MapError("link source record to journal", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "source link-journal", AggregateType: "source_record_journal",
		AggregateID: sourceRecordID + ":" + journalID,
		Payload:     map[string]any{"source_record_id": sourceRecordID, "journal_id": journalID, "link_role": role},
	}); err != nil {
		return SourceJournalLink{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceJournalLink{}, storesqlite.MapError("commit source-journal link", err)
	}
	return result, nil
}

func (s *Service) ListSourceJournalLinks(ctx context.Context, sourceRecordID string) ([]SourceJournalLink, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT srj.source_record_id, srj.journal_entry_id,
        b.code, je.entry_number, srj.link_role, srj.created_at, srj.created_by
        FROM source_record_journals srj
        JOIN journal_entries je ON je.id = srj.journal_entry_id
        JOIN books b ON b.id = je.book_id
        WHERE srj.source_record_id = ?
        ORDER BY b.code, je.entry_number`, strings.TrimSpace(sourceRecordID))
	if err != nil {
		return nil, storesqlite.MapError("list source-journal links", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []SourceJournalLink
	for rows.Next() {
		var link SourceJournalLink
		if err := rows.Scan(&link.SourceRecordID, &link.JournalID, &link.BookCode,
			&link.EntryNumber, &link.LinkRole, &link.CreatedAt, &link.CreatedBy); err != nil {
			return nil, err
		}
		result = append(result, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		var exists int
		if err := s.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM source_records WHERE id = ?`, strings.TrimSpace(sourceRecordID)).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, apperr.New(apperr.NotFound, "SOURCE_RECORD_NOT_FOUND", "source record was not found")
		}
	}
	return result, nil
}
