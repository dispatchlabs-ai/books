package ledger

import (
	"context"
	"database/sql"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type ImportBatch struct {
	ID                    string `json:"id"`
	SourceSystem          string `json:"source_system"`
	EntityCode            string `json:"entity,omitempty"`
	SourceName            string `json:"source_name"`
	FileSHA256            string `json:"file_sha256"`
	Status                string `json:"status"`
	RecordCount           int    `json:"record_count"`
	SourceRecordCount     int    `json:"source_record_count"`
	PostedSourceCount     int    `json:"posted_source_count"`
	PendingSourceCount    int    `json:"pending_source_count"`
	ReviewSourceCount     int    `json:"needs_review_source_count"`
	SourceOnlyCount       int    `json:"source_only_count"`
	JournalLinkCount      int    `json:"journal_link_count"`
	JournalCount          int    `json:"journal_count"`
	DraftJournalCount     int    `json:"draft_journal_count"`
	PostedJournalCount    int    `json:"posted_journal_count"`
	AbandonedJournalCount int    `json:"abandoned_journal_count"`
	CreatedAt             string `json:"created_at"`
	CompletedAt           string `json:"completed_at,omitempty"`
}

type ImportBatchFilter struct {
	SourceSystem string
	Entity       string
	Status       string
}

const importBatchSelect = `SELECT ib.id, ib.source_system, COALESCE(e.code, ''),
    ib.source_name, ib.file_sha256, ib.status, ib.record_count,
    COUNT(DISTINCT sr.id),
    COUNT(DISTINCT CASE WHEN sr.disposition = 'POSTED' THEN sr.id END),
    COUNT(DISTINCT CASE WHEN sr.disposition = 'PENDING' THEN sr.id END),
    COUNT(DISTINCT CASE WHEN sr.disposition = 'NEEDS_REVIEW' THEN sr.id END),
    COUNT(DISTINCT CASE WHEN sr.disposition = 'SOURCE_ONLY' THEN sr.id END),
    COUNT(srj.journal_entry_id), COUNT(DISTINCT srj.journal_entry_id),
    COUNT(DISTINCT CASE WHEN je.status = 'DRAFT' THEN je.id END),
    COUNT(DISTINCT CASE WHEN je.status = 'POSTED' THEN je.id END),
    COUNT(DISTINCT CASE WHEN je.status = 'ABANDONED' THEN je.id END),
    ib.created_at, COALESCE(ib.completed_at, '')
    FROM import_batches ib
    LEFT JOIN entities e ON e.id = ib.entity_id
    LEFT JOIN source_records sr ON sr.import_batch_id = ib.id
    LEFT JOIN source_record_journals srj ON srj.source_record_id = sr.id
    LEFT JOIN journal_entries je ON je.id = srj.journal_entry_id`

const importBatchGroup = ` GROUP BY ib.id, ib.source_system, e.code, ib.source_name,
    ib.file_sha256, ib.status, ib.record_count, ib.created_at, ib.completed_at`

type importBatchScanner interface {
	Scan(...any) error
}

func scanImportBatch(scanner importBatchScanner) (ImportBatch, error) {
	var value ImportBatch
	err := scanner.Scan(&value.ID, &value.SourceSystem, &value.EntityCode, &value.SourceName,
		&value.FileSHA256, &value.Status, &value.RecordCount, &value.SourceRecordCount,
		&value.PostedSourceCount, &value.PendingSourceCount, &value.ReviewSourceCount,
		&value.SourceOnlyCount, &value.JournalLinkCount, &value.JournalCount,
		&value.DraftJournalCount, &value.PostedJournalCount, &value.AbandonedJournalCount,
		&value.CreatedAt, &value.CompletedAt)
	return value, err
}

func (s *Service) ListImportBatches(ctx context.Context, filter ImportBatchFilter) ([]ImportBatch, error) {
	filter.SourceSystem = normalizeCode(filter.SourceSystem)
	filter.Entity = normalizeCode(filter.Entity)
	filter.Status = normalizeCode(filter.Status)
	if filter.Status != "" && filter.Status != "STAGED" && filter.Status != "COMPLETED" && filter.Status != "FAILED" {
		return nil, apperr.New(apperr.Invalid, "IMPORT_BATCH_STATUS_INVALID", "status must be STAGED, COMPLETED, or FAILED")
	}
	query := importBatchSelect + " WHERE 1=1"
	var args []any
	if filter.SourceSystem != "" {
		query += " AND ib.source_system = ?"
		args = append(args, filter.SourceSystem)
	}
	if filter.Entity != "" {
		query += " AND e.code = ?"
		args = append(args, filter.Entity)
	}
	if filter.Status != "" {
		query += " AND ib.status = ?"
		args = append(args, filter.Status)
	}
	query += importBatchGroup + " ORDER BY ib.created_at, ib.id"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list import batches", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []ImportBatch
	for rows.Next() {
		value, err := scanImportBatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Service) GetImportBatch(ctx context.Context, id string) (ImportBatch, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ImportBatch{}, apperr.New(apperr.Invalid, "IMPORT_BATCH_ID_REQUIRED", "import batch id is required")
	}
	value, err := scanImportBatch(s.store.DB().QueryRowContext(ctx,
		importBatchSelect+" WHERE ib.id = ?"+importBatchGroup, id))
	if err == sql.ErrNoRows {
		return value, apperr.New(apperr.NotFound, "IMPORT_BATCH_NOT_FOUND", "import batch was not found")
	}
	if err != nil {
		return value, storesqlite.MapError("read import batch", err)
	}
	return value, nil
}
