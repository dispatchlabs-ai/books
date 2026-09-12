package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/money"
)

// A matching date and amount is a review candidate, never proof of identity.
// Operators can preserve legitimate repeated payments or identify an existing
// source explicitly. Neither path silently collapses source observations.
type BankIdentityDecision struct {
	TransactionID    string `json:"transaction_id"`
	Action           string `json:"action"`
	ExistingSourceID string `json:"existing_source_id,omitempty"`
	Reason           string `json:"reason"`
}
type BankIdentityCandidate struct {
	SourceRecordID string `json:"source_record_id"`
	SourceSystem   string `json:"source_system"`
	ExternalID     string `json:"external_id"`
	Description    string `json:"description"`
	Disposition    string `json:"disposition"`
}
type BankTransactionMatch struct {
	AccountKey    string                  `json:"account_key"`
	TransactionID string                  `json:"transaction_id"`
	PostedDate    string                  `json:"posted_date"`
	Amount        string                  `json:"amount"`
	Candidates    []BankIdentityCandidate `json:"candidates"`
}
type BankImportMatches struct {
	JobID          string                 `json:"job_id"`
	LedgerRevision string                 `json:"ledger_revision"`
	Matches        []BankTransactionMatch `json:"matches"`
}

func bankSourceSystem(format string, a banking.Account) string {
	prefix := "FI-"
	if banking.Family(format) == "OFX" {
		prefix = "OFX-"
	}
	return prefix + strings.ToUpper(a.Key[:56])
}
func bankIdentityCandidates(ctx context.Context, q queryer, targetID, sourceSystem, currency string, t banking.Transaction) ([]BankIdentityCandidate, error) {
	amount, e := money.ParseCurrency(t.Amount, currency)
	if e != nil {
		return nil, e
	}
	// Stable provider IDs, including deterministic IDs for an exact file replay,
	// keep their existing lifecycle. Cross-format comparisons concern new IDs.
	var existing int
	if e = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM current_source_records WHERE statement_account_id=? AND source_system=? AND external_id=?`, targetID, sourceSystem, t.ID).Scan(&existing); e != nil {
		return nil, e
	}
	if existing > 0 {
		return []BankIdentityCandidate{}, nil
	}
	rows, e := q.QueryContext(ctx, `SELECT id,source_system,external_id,description,disposition FROM current_source_records WHERE statement_account_id=? AND transaction_date=? AND amount_cents=? AND disposition<>'SOURCE_ONLY' AND (? OR source_system<>?) ORDER BY id LIMIT 101`, targetID, t.PostedDate, amount, t.Identity == "FILE", sourceSystem)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []BankIdentityCandidate{}
	for rows.Next() {
		var c BankIdentityCandidate
		if e = rows.Scan(&c.SourceRecordID, &c.SourceSystem, &c.ExternalID, &c.Description, &c.Disposition); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(out) > 100 {
		return nil, apperr.New(apperr.Input, "IMPORT_MATCH_LIMIT", "more than 100 possible matches for one movement; use a smaller source or resolve existing source observations first")
	}
	return out, nil
}
func bankTarget(ctx context.Context, q queryer, bookID string, m BankAccountMapping, a banking.Account) (id, control string, err error) {
	var kind, currency string
	err = q.QueryRowContext(ctx, `SELECT sa.id,gl.code,sa.account_kind,sa.currency FROM statement_accounts sa JOIN accounts gl ON gl.id=sa.gl_account_id WHERE sa.code=? AND sa.book_id=? AND sa.status='ACTIVE'`, m.StatementAccount, bookID).Scan(&id, &control, &kind, &currency)
	if err == sql.ErrNoRows {
		return "", "", apperr.New(apperr.NotFound, "IMPORT_TARGET_NOT_FOUND", "mapped statement account is not active in this company")
	}
	if err != nil {
		return "", "", err
	}
	if kind != a.Kind || currency != a.Currency {
		return "", "", apperr.New(apperr.Conflict, "IMPORT_ACCOUNT_MISMATCH", "source and target account kinds or currencies differ")
	}
	return id, control, nil
}
func (s *Service) MatchBankImport(ctx context.Context, book, jobID string, choices BankImportChoices) (BankImportMatches, error) {
	result := BankImportMatches{JobID: jobID, Matches: []BankTransactionMatch{}}
	choices, e := normalizeBankChoices(choices)
	if e != nil {
		return result, e
	}
	tx, e := s.store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return result, e
	}
	defer func() { _ = tx.Rollback() }()
	job, e := readBankImportJob(ctx, tx, book, jobID)
	if e != nil {
		return result, e
	}
	if job.Document == nil || !banking.SupportedParser(job.Document.Parser) {
		return result, apperr.New(apperr.Conflict, "IMPORT_JOB_NOT_READY", "a parsed import job is required")
	}
	bookID, e := bankBookID(ctx, tx, book)
	if e != nil {
		return result, e
	}
	result.LedgerRevision, e = BankLedgerRevision(ctx, tx)
	if e != nil {
		return result, e
	}
	accounts := map[string]banking.Account{}
	for _, a := range job.Document.Accounts {
		accounts[a.Key] = a
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	total := 0
	for _, m := range choices.Mappings {
		a, ok := accounts[m.AccountKey]
		if !ok || seen[m.AccountKey] || targets[m.StatementAccount] {
			return result, apperr.New(apperr.Input, "IMPORT_MAPPING_INVALID", "unknown or repeated source or target mapping")
		}
		seen[m.AccountKey] = true
		targets[m.StatementAccount] = true
		id, _, e := bankTarget(ctx, tx, bookID, m, a)
		if e != nil {
			return result, e
		}
		system := bankSourceSystem(job.Document.Format, a)
		for _, t := range a.Transactions {
			candidates, e := bankIdentityCandidates(ctx, tx, id, system, a.Currency, t)
			if e != nil {
				return result, e
			}
			if len(candidates) == 0 {
				continue
			}
			total += len(candidates)
			if total > 20000 {
				return result, apperr.New(apperr.Input, "IMPORT_MATCH_LIMIT", "too many candidate matches; use a smaller statement")
			}
			result.Matches = append(result.Matches, BankTransactionMatch{AccountKey: a.Key, TransactionID: t.ID, PostedDate: t.PostedDate, Amount: t.Amount, Candidates: candidates})
		}
	}
	return result, nil
}
func bankDisposition(t banking.Transaction) (string, string) {
	switch t.Status {
	case "PENDING":
		return SourceDispositionPending, "Source reports pending activity"
	case "REVIEW":
		return SourceDispositionNeedsReview, "Source requires review before materialization"
	}
	return SourceDispositionPosted, ""
}
func prepareBankMovements(ctx context.Context, tx *sql.Tx, targetID, system string, account banking.Account, mapping BankAccountMapping) ([]StatementTransactionInput, map[string]bool, error) {
	decisions := map[string]BankIdentityDecision{}
	for _, decision := range mapping.IdentityDecisions {
		if _, ok := decisions[decision.TransactionID]; ok {
			return nil, nil, apperr.New(apperr.Input, "IMPORT_IDENTITY_INVALID", "repeated identity decision")
		}
		decisions[decision.TransactionID] = decision
	}
	inputs := []StatementTransactionInput{}
	nonPostable := map[string]bool{}
	for _, t := range account.Transactions {
		amount, e := money.ParseCurrency(t.Amount, account.Currency)
		if e != nil {
			return nil, nil, e
		}
		raw, e := bankJSON(t)
		if e != nil {
			return nil, nil, e
		}
		disposition, reason := bankDisposition(t)
		candidates, e := bankIdentityCandidates(ctx, tx, targetID, system, account.Currency, t)
		if e != nil {
			return nil, nil, e
		}
		decision, hasDecision := decisions[t.ID]
		delete(decisions, t.ID)
		var currentDisposition, currentReason, currentHash string
		e = tx.QueryRowContext(ctx, `SELECT disposition,COALESCE(exclusion_reason,''),payload_sha256 FROM current_source_records WHERE statement_account_id=? AND source_system=? AND external_id=?`, targetID, system, t.ID).Scan(&currentDisposition, &currentReason, &currentHash)
		if e != nil && e != sql.ErrNoRows {
			return nil, nil, e
		}
		existing := e == nil
		if len(candidates) > 0 && !hasDecision {
			return nil, nil, apperr.New(apperr.Conflict, "IMPORT_IDENTITY_REVIEW_REQUIRED", fmt.Sprintf("transaction %s has possible existing matches; inspect bank-import matches and supply an identity_decision", t.ID))
		}
		if hasDecision {
			if len(candidates) == 0 && !existing {
				return nil, nil, apperr.New(apperr.Input, "IMPORT_IDENTITY_INVALID", "identity decision names a movement without possible existing matches")
			}
			if decision.Action == "duplicate" {
				found := false
				for _, candidate := range candidates {
					if candidate.SourceRecordID == decision.ExistingSourceID {
						if (t.Status == "" || t.Status == "POSTED") && candidate.Disposition != SourceDispositionPosted {
							return nil, nil, apperr.New(apperr.Conflict, "IMPORT_DUPLICATE_NOT_POSTED", "booked activity can only duplicate a posted source; resolve the pending or review source first")
						}
						found = true
					}
				}
				reason = "Duplicate of source " + decision.ExistingSourceID + ": " + decision.Reason
				disposition = SourceDispositionSourceOnly
				if !found && (!existing || currentDisposition != disposition || currentReason != reason || currentHash != bankHash(raw)) {
					return nil, nil, apperr.New(apperr.Conflict, "IMPORT_IDENTITY_INVALID", "duplicate decision must identify a current matching source observation")
				}
			} else if existing && currentDisposition == SourceDispositionSourceOnly {
				return nil, nil, apperr.New(apperr.Conflict, "IMPORT_IDENTITY_INVALID", "an excluded source requires an explicit source-resolution workflow before materialization")
			}
		} else if existing && currentDisposition == SourceDispositionSourceOnly && currentHash == bankHash(raw) && strings.HasPrefix(currentReason, "Duplicate of source ") {
			disposition, reason = currentDisposition, currentReason
		}
		if disposition != SourceDispositionPosted {
			nonPostable[t.ID] = true
		}
		inputs = append(inputs, StatementTransactionInput{ExternalID: t.ID, PostedDate: t.PostedDate, Description: t.Description, AmountCents: amount, Disposition: disposition, ExclusionReason: reason, RawJSON: raw})
	}
	if len(decisions) > 0 {
		return nil, nil, apperr.New(apperr.Input, "IMPORT_IDENTITY_INVALID", "identity decision names a transaction absent from this account")
	}
	return inputs, nonPostable, nil
}

// Generic statement imports identify exact-file replays by file hash. Before
// taking that fast path, bank imports also bind the parsed options and decisions
// to the durable observations; changing a profile cannot bypass validation.
func validateBankReplay(ctx context.Context, tx *sql.Tx, targetID string, input StatementImportInput) error {
	var batchID string
	e := tx.QueryRowContext(ctx, `SELECT id FROM import_batches WHERE source_system=? AND file_sha256=? AND status='COMPLETED'`, input.SourceSystem, input.FileSHA256).Scan(&batchID)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT record_count FROM import_batches WHERE id=?`, batchID).Scan(&count); e != nil {
		return e
	}
	if count != len(input.Transactions) {
		return apperr.New(apperr.Conflict, "IMPORT_REPLAY_CONFLICT", "same file was already imported with different parsed records")
	}
	for _, t := range input.Transactions {
		var hash, date, description, disposition, reason string
		var amount int64
		e = tx.QueryRowContext(ctx, `SELECT payload_sha256,transaction_date,description,amount_cents,disposition,COALESCE(exclusion_reason,'') FROM current_source_records WHERE statement_account_id=? AND source_system=? AND external_id=?`, targetID, input.SourceSystem, t.ExternalID).Scan(&hash, &date, &description, &amount, &disposition, &reason)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == sql.ErrNoRows || hash != bankHash(t.RawJSON) || date != t.PostedDate || description != strings.TrimSpace(t.Description) || amount != t.AmountCents || disposition != t.Disposition || reason != t.ExclusionReason {
			return apperr.New(apperr.Conflict, "IMPORT_REPLAY_CONFLICT", "same file was already imported with different evidence, profile, or source disposition")
		}
	}
	return nil
}
