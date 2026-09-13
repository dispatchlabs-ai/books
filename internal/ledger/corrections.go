package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"strings"
)

// ReverseAndRecordJournal resolves retries, derives the linked reversal, and
// optionally posts it inside one transaction. Conflicting retries never reuse
// another request's reversal.
func (s *Service) ReverseAndRecordJournal(ctx context.Context, originalID, date, period, description string, draft bool, allowedKinds ...string) (Journal, error) {
	if err := s.requireActor(); err != nil {
		return Journal{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Journal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	original, err := getJournal(ctx, tx, originalID)
	if err != nil {
		return Journal{}, err
	}
	if err = CheckJournalKind(original.Kind, allowedKinds); err != nil {
		return Journal{}, err
	}
	input, err := reversalInput(ctx, tx, originalID, date, period, description)
	if err != nil {
		return Journal{}, err
	}
	journal, err := s.ensureReversalTx(ctx, tx, input, !draft)
	if err != nil {
		return Journal{}, err
	}
	if err = tx.Commit(); err != nil {
		return Journal{}, storesqlite.MapError("commit reversal", err)
	}
	return journal, nil
}

func (s *Service) ensureReversalTx(ctx context.Context, tx *sql.Tx, input CreateJournalInput, post bool) (Journal, error) {
	j, err := existingReversal(ctx, tx, input)
	if err != nil {
		return Journal{}, err
	}
	if j.ID == "" {
		return s.createJournalTx(ctx, tx, input, post)
	}
	if post && j.Status == "DRAFT" {
		if _, err = s.postJournalTx(ctx, tx, j.ID); err != nil {
			return Journal{}, err
		}
		return getJournal(ctx, tx, j.ID)
	}
	return j, nil
}

type CorrectionInput struct {
	AllowedKinds []string
	OriginalID   string
	Replacement  CreateJournalInput
	Reason       string
	Draft        bool
}
type CorrectionResult struct {
	Original    Journal
	Reversal    Journal
	Replacement Journal
}

// PrepareCorrection derives stable source identities and reversal content. The
// committed operation rederives these values under its write transaction.
func (s *Service) PrepareCorrection(ctx context.Context, input CorrectionInput) (CreateJournalInput, CreateJournalInput, error) {
	_, reversal, replacement, err := prepareCorrection(ctx, s.store.DB(), input)
	if err == nil {
		_, err = existingReversal(ctx, s.store.DB(), reversal)
	}
	return reversal, replacement, err
}
func prepareCorrection(ctx context.Context, q queryer, input CorrectionInput) (Journal, CreateJournalInput, CreateJournalInput, error) {
	original, err := getJournal(ctx, q, input.OriginalID)
	if err != nil {
		return original, CreateJournalInput{}, CreateJournalInput{}, err
	}
	if err = CheckJournalKind(original.Kind, input.AllowedKinds); err != nil {
		return original, CreateJournalInput{}, CreateJournalInput{}, err
	}
	replacement := input.Replacement
	fail := func(err error) (Journal, CreateJournalInput, CreateJournalInput, error) {
		return original, CreateJournalInput{}, replacement, err
	}
	if original.Status != "POSTED" {
		return fail(apperr.New(apperr.Conflict, "CORRECTION_REQUIRES_POSTED", "only a posted transaction can be corrected; edit or abandon a draft instead"))
	}
	if normalizeCode(replacement.Book) != original.BookCode {
		return fail(apperr.New(apperr.Invalid, "BOOK_INVALID", "correction must use the original book"))
	}
	if strings.TrimSpace(input.Reason) == "" {
		return fail(apperr.New(apperr.Invalid, "CORRECTION_REASON_REQUIRED", "a correction reason is required for the audit trail"))
	}
	if replacement.PostingDate < original.PostingDate {
		return fail(apperr.New(apperr.Validation, "CORRECTION_DATE_INVALID", "the corrected transaction date cannot precede the original transaction"))
	}
	seed := replacement
	seed.SourceSystem, seed.SourceKey = "", ""
	raw, err := json.Marshal(map[string]any{"journal": seed, "reason": strings.TrimSpace(input.Reason)})
	if err != nil {
		return fail(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	prefix := fmt.Sprintf("transaction-%d-", original.EntryNumber)
	replacement.SourceSystem = "MANUAL_CORRECTION"
	replacement.SourceKey = prefix + digest[:16]
	if replacement.Reference == "" {
		replacement.Reference = fmt.Sprintf("CORRECTS-%d", original.EntryNumber)
	}
	var prior string
	err = q.QueryRowContext(ctx, `SELECT source_key FROM journal_entries WHERE book_id=? AND source_system='MANUAL_CORRECTION' AND source_key LIKE ? AND status<>'ABANDONED' LIMIT 1`, original.BookID, prefix+"%").Scan(&prior)
	if err == nil && prior != replacement.SourceKey {
		return fail(apperr.New(apperr.Conflict, "CORRECTION_ALREADY_EXISTS", "transaction already has a different active correction"))
	}
	if err != nil && err != sql.ErrNoRows {
		return fail(err)
	}
	reversal, err := reversalInput(ctx, q, original.ID, replacement.PostingDate, replacement.Period, fmt.Sprintf("Correction of transaction %d: %s", original.EntryNumber, strings.TrimSpace(input.Reason)))
	return original, reversal, replacement, err
}

// CorrectJournal commits both drafts or both posted journals together. Every
// conflict and posting validation runs before the transaction can commit.
func (s *Service) CorrectJournal(ctx context.Context, input CorrectionInput) (CorrectionResult, error) {
	if err := s.requireActor(); err != nil {
		return CorrectionResult{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return CorrectionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	original, reversalInput, replacementInput, err := prepareCorrection(ctx, tx, input)
	if err != nil {
		return CorrectionResult{}, err
	}
	replacement, err := s.createJournalTx(ctx, tx, replacementInput, false)
	if err != nil {
		return CorrectionResult{}, err
	}
	if replacement.Status == "ABANDONED" {
		return CorrectionResult{}, apperr.New(apperr.Conflict, "CORRECTION_ABANDONED", "the identical correction was previously abandoned; use a materially revised replacement")
	}
	reversal, err := s.ensureReversalTx(ctx, tx, reversalInput, false)
	if err != nil {
		return CorrectionResult{}, err
	}
	for _, j := range []Journal{replacement, reversal} {
		if j.Status != "DRAFT" {
			continue
		}
		validation, e := validateJournalQuery(ctx, tx, j.ID)
		if e != nil {
			return CorrectionResult{}, e
		}
		if !validation.Valid {
			return CorrectionResult{}, apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(validation.Errors, "; "))
		}
	}
	if !input.Draft {
		for _, j := range []Journal{reversal, replacement} {
			if j.Status == "DRAFT" {
				if _, err = s.postJournalTx(ctx, tx, j.ID); err != nil {
					return CorrectionResult{}, err
				}
			}
		}
	}
	reversal, err = getJournal(ctx, tx, reversal.ID)
	if err != nil {
		return CorrectionResult{}, err
	}
	replacement, err = getJournal(ctx, tx, replacement.ID)
	if err != nil {
		return CorrectionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return CorrectionResult{}, storesqlite.MapError("commit correction", err)
	}
	return CorrectionResult{Original: original, Reversal: reversal, Replacement: replacement}, nil
}

func existingReversal(ctx context.Context, q queryer, input CreateJournalInput) (Journal, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM journal_entries WHERE reversal_of_id=? AND status<>'ABANDONED'`, input.ReversalOfID).Scan(&id)
	if err == sql.ErrNoRows {
		return Journal{}, nil
	}
	if err != nil {
		return Journal{}, storesqlite.MapError("read existing reversal", err)
	}
	j, err := getJournal(ctx, q, id)
	if err != nil {
		return Journal{}, err
	}
	if j.PostingDate != input.PostingDate || j.PeriodCode != normalizeCode(input.Period) || j.Description != strings.TrimSpace(input.Description) {
		return Journal{}, apperr.New(apperr.Conflict, "REVERSAL_ALREADY_EXISTS", "transaction already has a different active reversal")
	}

	return j, nil
}

// CheckJournalKind enforces a caller's explicit operation scope. An empty set
// means trusted local administration; remote callers provide their allowed kinds.
func CheckJournalKind(kind string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	for _, candidate := range allowed {
		if kind == candidate {
			return nil
		}
	}
	return apperr.New(apperr.Invalid, "PERMISSION_DENIED", "journal kind requires additional permission")
}
