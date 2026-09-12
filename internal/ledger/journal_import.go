package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type JournalImportRecord struct {
	Journal CreateJournalInput `json:"journal"`
	RawJSON json.RawMessage    `json:"raw_json,omitempty"`
}

type JournalImportInput struct {
	SourceSystem string                `json:"source_system"`
	SourceName   string                `json:"source_name"`
	FileSHA256   string                `json:"file_sha256"`
	Entity       string                `json:"entity,omitempty"`
	Records      []JournalImportRecord `json:"records"`
}

type JournalImportResult struct {
	BatchID      string   `json:"batch_id"`
	Changed      bool     `json:"changed"`
	JournalIDs   []string `json:"journal_ids"`
	CreatedCount int      `json:"created_count"`
	SkippedCount int      `json:"skipped_count"`
}

type ImportBatchPostResult struct {
	BatchID       string `json:"batch_id"`
	Changed       bool   `json:"changed"`
	PostedCount   int    `json:"posted_count"`
	AlreadyPosted int    `json:"already_posted"`
}

func (s *Service) ImportJournals(ctx context.Context, input JournalImportInput) (JournalImportResult, error) {
	if err := s.requireActor(); err != nil {
		return JournalImportResult{}, err
	}
	input.SourceSystem = normalizeCode(input.SourceSystem)
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.FileSHA256 = strings.ToLower(strings.TrimSpace(input.FileSHA256))
	input.Entity = normalizeCode(input.Entity)
	if input.SourceSystem == "" || input.SourceName == "" || len(input.FileSHA256) != 64 || len(input.Records) == 0 {
		return JournalImportResult{}, apperr.New(apperr.Invalid, "JOURNAL_IMPORT_INVALID", "source system, source name, SHA-256, and at least one record are required")
	}
	if _, err := hex.DecodeString(input.FileSHA256); err != nil {
		return JournalImportResult{}, apperr.New(apperr.Invalid, "JOURNAL_IMPORT_INVALID", "file SHA-256 is invalid")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return JournalImportResult{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var entityID string
	var entityValue any
	if input.Entity != "" {
		id, err := lookupID(ctx, tx, "entities", input.Entity)
		if err != nil {
			return JournalImportResult{}, err
		}
		entityID = id
		entityValue = id
	}
	var priorBatchID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM import_batches
		WHERE source_system = ? AND file_sha256 = ? AND status = 'COMPLETED'`, input.SourceSystem, input.FileSHA256).Scan(&priorBatchID)
	if err == nil {
		return JournalImportResult{BatchID: priorBatchID, Changed: false, SkippedCount: len(input.Records)}, nil
	}
	if err != sql.ErrNoRows {
		return JournalImportResult{}, storesqlite.MapError("check journal import batch", err)
	}
	batchID, err := storesqlite.NewID()
	if err != nil {
		return JournalImportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO import_batches
		(id, source_system, entity_id, source_name, file_sha256, status, record_count, created_at)
		VALUES (?, ?, ?, ?, ?, 'STAGED', ?, ?)`, batchID, input.SourceSystem, entityValue, input.SourceName,
		input.FileSHA256, len(input.Records), storesqlite.UTCNow()); err != nil {
		return JournalImportResult{}, storesqlite.MapError("stage journal import", err)
	}
	result := JournalImportResult{BatchID: batchID, Changed: true}
	nextNumbers := map[string]int64{}
	for recordIndex, record := range input.Records {
		journalInput := normalizeJournalInput(record.Journal)
		if journalInput.SourceSystem == "" {
			journalInput.SourceSystem = input.SourceSystem
		}
		if journalInput.SourceSystem != input.SourceSystem || journalInput.SourceKey == "" {
			return JournalImportResult{}, apperr.New(apperr.Input, "JOURNAL_IMPORT_SOURCE_INVALID", fmt.Sprintf("record %d must have the batch source system and a source key", recordIndex+1))
		}
		if err := validateJournalInput(journalInput); err != nil {
			return JournalImportResult{}, apperr.Wrap(apperr.Input, "JOURNAL_IMPORT_RECORD_INVALID", fmt.Sprintf("record %d is invalid", recordIndex+1), err)
		}
		digest, err := sourceDigest(journalInput)
		if err != nil {
			return JournalImportResult{}, err
		}
		bookID, err := lookupID(ctx, tx, "books", journalInput.Book)
		if err != nil {
			return JournalImportResult{}, err
		}
		var bookEntityID sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT entity_id FROM books WHERE id = ?", bookID).Scan(&bookEntityID); err != nil {
			return JournalImportResult{}, storesqlite.MapError("read journal import book entity", err)
		}
		if input.Entity != "" {
			if !bookEntityID.Valid || bookEntityID.String != entityID {
				return JournalImportResult{}, apperr.New(apperr.Input, "JOURNAL_IMPORT_ENTITY_MISMATCH", fmt.Sprintf("record %d book does not belong to the batch entity", recordIndex+1))
			}
		}
		periodID, err := lookupID(ctx, tx, "fiscal_periods", journalInput.Period)
		if err != nil {
			return JournalImportResult{}, err
		}
		var existingID, existingDigest string
		err = tx.QueryRowContext(ctx, `SELECT id, source_payload_sha256 FROM journal_entries
			WHERE book_id = ? AND source_system = ? AND source_key = ?`, bookID, input.SourceSystem, journalInput.SourceKey).Scan(&existingID, &existingDigest)
		if err == nil {
			if existingDigest != digest {
				return JournalImportResult{}, apperr.New(apperr.Conflict, "SOURCE_CONFLICT", fmt.Sprintf("record %d source key has different journal content", recordIndex+1))
			}
			result.JournalIDs = append(result.JournalIDs, existingID)
			result.SkippedCount++
			continue
		}
		if err != sql.ErrNoRows {
			return JournalImportResult{}, storesqlite.MapError("check journal import source", err)
		}
		entryNumber, ok := nextNumbers[bookID]
		if !ok {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(entry_number), 0) + 1
				FROM journal_entries WHERE book_id = ?`, bookID).Scan(&entryNumber); err != nil {
				return JournalImportResult{}, err
			}
		}
		nextNumbers[bookID] = entryNumber + 1
		journalID, err := storesqlite.NewID()
		if err != nil {
			return JournalImportResult{}, err
		}
		now := storesqlite.UTCNow()
		if _, err := tx.ExecContext(ctx, `INSERT INTO journal_entries
			(id, book_id, entry_number, kind, posting_date, period_id, status, description, reference,
			 source_system, source_key, source_payload_sha256, tax_type, tax_accounting_period,
			 reversal_of_id, created_at, created_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'DRAFT', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			journalID, bookID, entryNumber, journalInput.Kind, journalInput.PostingDate, periodID, journalInput.Description,
			nilIfEmpty(journalInput.Reference), input.SourceSystem, journalInput.SourceKey, digest,
			nilIfEmpty(journalInput.TaxType), nilIfEmpty(journalInput.TaxAccountingPeriod), nilIfEmpty(journalInput.ReversalOfID),
			now, s.actor, now); err != nil {
			return JournalImportResult{}, storesqlite.MapError("create imported journal", err)
		}
		if err := s.insertJournalLines(ctx, tx, journalID, journalInput.Lines); err != nil {
			return JournalImportResult{}, err
		}
		raw := record.RawJSON
		if len(raw) == 0 {
			raw, err = json.Marshal(record)
			if err != nil {
				return JournalImportResult{}, err
			}
		}
		if !json.Valid(raw) {
			return JournalImportResult{}, apperr.New(apperr.Input, "JOURNAL_IMPORT_RAW_INVALID", fmt.Sprintf("record %d raw JSON is invalid", recordIndex+1))
		}
		var total int64
		for _, line := range journalInput.Lines {
			if line.DebitCents > math.MaxInt64-total {
				return JournalImportResult{}, apperr.New(apperr.Integrity, "AMOUNT_OVERFLOW", fmt.Sprintf("record %d debit total exceeds int64 cents", recordIndex+1))
			}
			total += line.DebitCents
		}
		candidate, err := prepareSourceObservation(sourceObservationInput{
			ImportBatchID: batchID, TransactionDate: journalInput.PostingDate,
			Description: journalInput.Description, AmountCents: total,
			TaxType: journalInput.TaxType, TaxPeriod: journalInput.TaxAccountingPeriod,
			Disposition: SourceDispositionPosted, RawJSON: raw,
		})
		if err != nil {
			return JournalImportResult{}, err
		}
		sourceIdentityID, identityCreated, err := s.ensureSourceIdentity(ctx, tx, sourceIdentityInput{
			EntityID: bookEntityID.String, BookID: bookID,
			MaterializationKind: sourceMaterializationJournal,
			SourceSystem:        input.SourceSystem, SourceAccount: journalInput.Book,
			ExternalID: journalInput.SourceKey,
		})
		if err != nil {
			return JournalImportResult{}, err
		}
		if _, exists, err := readCurrentSourceObservation(ctx, tx, sourceIdentityID); err != nil {
			return JournalImportResult{}, err
		} else if exists || !identityCreated {
			return JournalImportResult{}, apperr.New(apperr.Integrity, "JOURNAL_SOURCE_WITHOUT_ENTRY", fmt.Sprintf("record %d source identity exists without its journal entry", recordIndex+1))
		}
		sourceRecordID, _, err := s.insertSourceObservation(ctx, tx, sourceIdentityID, candidate, nil)
		if err != nil {
			return JournalImportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO source_record_journals
            (source_record_id, journal_entry_id, link_role, created_at, created_by)
            VALUES (?, ?, 'PRIMARY', ?, ?)`, sourceRecordID, journalID, now, s.actor); err != nil {
			return JournalImportResult{}, storesqlite.MapError("link journal source record", err)
		}
		result.JournalIDs = append(result.JournalIDs, journalID)
		result.CreatedCount++
	}
	if _, err := tx.ExecContext(ctx, `UPDATE import_batches SET status = 'COMPLETED', completed_at = ? WHERE id = ?`, storesqlite.UTCNow(), batchID); err != nil {
		return JournalImportResult{}, storesqlite.MapError("complete journal import", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "journal import", AggregateType: "import_batch", AggregateID: batchID,
		Payload: map[string]any{"source_system": input.SourceSystem, "source_name": input.SourceName, "file_sha256": input.FileSHA256, "record_count": len(input.Records), "created_count": result.CreatedCount, "skipped_count": result.SkippedCount},
	}); err != nil {
		return JournalImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return JournalImportResult{}, storesqlite.MapError("commit journal import", err)
	}
	return result, nil
}

func (s *Service) PostImportBatch(ctx context.Context, batchID string, dryRun bool) (ImportBatchPostResult, error) {
	if err := s.requireActor(); err != nil {
		return ImportBatchPostResult{}, err
	}
	result := ImportBatchPostResult{BatchID: batchID}
	if dryRun {
		rows, err := s.store.DB().QueryContext(ctx, `SELECT DISTINCT srj.journal_entry_id, je.status
			FROM source_records sr
			JOIN source_identities si ON si.id = sr.source_identity_id
			JOIN source_record_journals srj ON srj.source_record_id = sr.id
			JOIN journal_entries je ON je.id = srj.journal_entry_id
			WHERE sr.import_batch_id = ? AND srj.link_role = 'PRIMARY'
			  AND je.source_system = si.source_system AND je.source_key = si.external_id
			ORDER BY srj.journal_entry_id`, batchID)
		if err != nil {
			return result, storesqlite.MapError("read import batch journals", err)
		}
		var draftIDs []string
		for rows.Next() {
			var journalID, status string
			if err := rows.Scan(&journalID, &status); err != nil {
				_ = rows.Close()
				return result, err
			}
			if status == "POSTED" {
				result.AlreadyPosted++
				continue
			}
			if status != "DRAFT" {
				_ = rows.Close()
				return result, apperr.New(apperr.Conflict, "IMPORT_BATCH_JOURNAL_STATE", "import batch contains a journal that is neither draft nor posted")
			}
			draftIDs = append(draftIDs, journalID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return result, err
		}
		_ = rows.Close()
		if len(draftIDs) == 0 && result.AlreadyPosted == 0 {
			return result, apperr.New(apperr.NotFound, "IMPORT_BATCH_NOT_FOUND", "import batch has no journal records")
		}
		for _, journalID := range draftIDs {
			validation, err := validateJournalQuery(ctx, s.store.DB(), journalID)
			if err != nil {
				return result, err
			}
			if !validation.Valid {
				return result, apperr.New(apperr.Validation, "JOURNAL_INVALID", journalID+": "+strings.Join(validation.Errors, "; "))
			}
			result.PostedCount++
		}
		result.Changed = result.PostedCount > 0
		return result, nil
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT srj.journal_entry_id, je.status
		FROM source_records sr
		JOIN source_identities si ON si.id = sr.source_identity_id
		JOIN source_record_journals srj ON srj.source_record_id = sr.id
		JOIN journal_entries je ON je.id = srj.journal_entry_id
		WHERE sr.import_batch_id = ? AND srj.link_role = 'PRIMARY'
		  AND je.source_system = si.source_system AND je.source_key = si.external_id
		ORDER BY srj.journal_entry_id`, batchID)
	if err != nil {
		return result, storesqlite.MapError("read import batch journals", err)
	}
	var draftIDs []string
	for rows.Next() {
		var journalID, status string
		if err := rows.Scan(&journalID, &status); err != nil {
			_ = rows.Close()
			return result, err
		}
		if status == "POSTED" {
			result.AlreadyPosted++
			continue
		}
		if status != "DRAFT" {
			_ = rows.Close()
			return result, apperr.New(apperr.Conflict, "IMPORT_BATCH_JOURNAL_STATE", "import batch contains a journal that is neither draft nor posted")
		}
		draftIDs = append(draftIDs, journalID)
	}
	_ = rows.Close()
	if len(draftIDs) == 0 && result.AlreadyPosted == 0 {
		return result, apperr.New(apperr.NotFound, "IMPORT_BATCH_NOT_FOUND", "import batch has no journal records")
	}
	for _, journalID := range draftIDs {
		validation, err := validateJournalQuery(ctx, tx, journalID)
		if err != nil {
			return result, err
		}
		if !validation.Valid {
			return result, apperr.New(apperr.Validation, "JOURNAL_INVALID", journalID+": "+strings.Join(validation.Errors, "; "))
		}
	}
	now := storesqlite.UTCNow()
	for _, journalID := range draftIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE journal_entries SET status = 'POSTED', posted_at = ?, posted_by = ?, updated_at = ?
			WHERE id = ? AND status = 'DRAFT'`, now, s.actor, now, journalID); err != nil {
			return result, storesqlite.MapError("post imported journal", err)
		}
	}
	sortedIDs := append([]string(nil), draftIDs...)
	sort.Strings(sortedIDs)
	idHash := sha256.Sum256([]byte(strings.Join(sortedIDs, "\n")))
	if len(draftIDs) > 0 {
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "journal import post", AggregateType: "import_batch", AggregateID: batchID,
			Payload: map[string]any{"posted_count": len(draftIDs), "journal_id_sha256": hex.EncodeToString(idHash[:])},
		}); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, storesqlite.MapError("commit imported journal posting", err)
	}
	result.PostedCount = len(draftIDs)
	result.Changed = len(draftIDs) > 0
	return result, nil
}
