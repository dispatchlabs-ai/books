package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type BankClassification struct {
	TransactionID string `json:"transaction_id"`
	ContraAccount string `json:"contra_account"`
}
type BankAccountMapping struct {
	IdentityDecisions []BankIdentityDecision `json:"identity_decisions,omitempty"`
	AccountKey        string                 `json:"account_key"`
	StatementAccount  string                 `json:"statement_account"`
	Classifications   []BankClassification   `json:"classifications,omitempty"`
}
type BankAccountExclusion struct {
	AccountKey string `json:"account_key"`
	Reason     string `json:"reason"`
}
type BankImportChoices struct {
	Mappings   []BankAccountMapping   `json:"mappings"`
	Exclusions []BankAccountExclusion `json:"exclusions,omitempty"`
	Post       bool                   `json:"post"`
}
type BankImportSummary struct {
	RetainedObservations int `json:"retained_observations,omitempty"`
	ImportedTransactions int `json:"imported_transactions"`
	SkippedTransactions  int `json:"skipped_transactions"`
	NewJournals          int `json:"new_journals"`
	ExistingJournals     int `json:"existing_journals"`
}
type BankImportPlan struct {
	ID             string            `json:"id"`
	JobID          string            `json:"job_id"`
	Digest         string            `json:"digest"`
	LedgerRevision string            `json:"ledger_revision"`
	Choices        BankImportChoices `json:"choices"`
	Summary        BankImportSummary `json:"summary"`
	CreatedAt      string            `json:"created_at"`
}
type BankImportAccountReceipt struct {
	StatementAccount string `json:"statement_account"`
	BatchID          string `json:"batch_id"`
}
type BankImportReceipt struct {
	JobID      string                     `json:"job_id"`
	PlanID     string                     `json:"plan_id"`
	PlanDigest string                     `json:"plan_digest"`
	Summary    BankImportSummary          `json:"summary"`
	Accounts   []BankImportAccountReceipt `json:"accounts"`
	JournalIDs []string                   `json:"journal_ids"`
	AppliedAt  string                     `json:"applied_at"`
}

func normalizeBankChoices(v BankImportChoices) (BankImportChoices, error) {
	// Copy caller slices: normalization must not mutate another request or a saved plan.
	v.Mappings = append([]BankAccountMapping(nil), v.Mappings...)
	v.Exclusions = append([]BankAccountExclusion(nil), v.Exclusions...)
	if len(v.Mappings) < 1 || len(v.Mappings) > banking.MaxAccounts || len(v.Exclusions) > banking.MaxAccounts {
		return v, apperr.New(apperr.Input, "IMPORT_MAPPING_REQUIRED", "map at least one account within the supported limit")
	}
	for i := range v.Mappings {
		m := &v.Mappings[i]
		m.StatementAccount = normalizeCode(m.StatementAccount)
		m.IdentityDecisions = append([]BankIdentityDecision(nil), m.IdentityDecisions...)
		if len(m.IdentityDecisions) > banking.MaxTransactions {
			return v, apperr.New(apperr.Input, "IMPORT_LIMIT_EXCEEDED", "too many identity decisions")
		}
		for j := range m.IdentityDecisions {
			decision := &m.IdentityDecisions[j]
			decision.Reason = strings.TrimSpace(decision.Reason)
			if decision.TransactionID == "" || len(decision.TransactionID) > banking.MaxTransactionID || (decision.Action != "new" && decision.Action != "duplicate") || decision.Reason == "" || len(decision.Reason) > 1000 || (decision.Action == "duplicate" && decision.ExistingSourceID == "") || (decision.Action == "new" && decision.ExistingSourceID != "") {
				return v, apperr.New(apperr.Input, "IMPORT_IDENTITY_INVALID", "each decision requires transaction_id, action new or duplicate, and a reason; duplicate also requires existing_source_id")
			}
		}
		sort.Slice(m.IdentityDecisions, func(a, b int) bool {
			return m.IdentityDecisions[a].TransactionID < m.IdentityDecisions[b].TransactionID
		})
		m.Classifications = append([]BankClassification(nil), m.Classifications...)
		if len(m.Classifications) > banking.MaxTransactions {
			return v, apperr.New(apperr.Input, "IMPORT_LIMIT_EXCEEDED", "too many classifications")
		}
		if !v.Post && len(m.Classifications) > 0 {
			return v, apperr.New(apperr.Input, "IMPORT_CLASSIFICATION_INVALID", "classifications require post=true")
		}
		for j := range m.Classifications {
			m.Classifications[j].ContraAccount = normalizeCode(m.Classifications[j].ContraAccount)
		}
		sort.Slice(m.Classifications, func(a, b int) bool { return m.Classifications[a].TransactionID < m.Classifications[b].TransactionID })
	}
	for i := range v.Exclusions {
		v.Exclusions[i].Reason = strings.TrimSpace(v.Exclusions[i].Reason)
		if len(v.Exclusions[i].Reason) < 1 || len(v.Exclusions[i].Reason) > 1000 {
			return v, apperr.New(apperr.Input, "IMPORT_EXCLUSION_REASON_REQUIRED", "every excluded account requires a reason of at most 1000 characters")
		}
	}
	sort.Slice(v.Mappings, func(i, j int) bool { return v.Mappings[i].AccountKey < v.Mappings[j].AccountKey })
	sort.Slice(v.Exclusions, func(i, j int) bool { return v.Exclusions[i].AccountKey < v.Exclusions[j].AccountKey })
	return v, nil
}

// BankLedgerRevision ignores only source-upload/parse/preview bookkeeping.
// Every accounting mutation, including another apply, invalidates older previews.
// The whole-database revision is deliberately conservative for shared ledgers.
func BankLedgerRevision(ctx context.Context, q queryer) (string, error) {
	var hash string
	e := q.QueryRowContext(ctx, `SELECT event_hash FROM audit_events WHERE command NOT IN ('bank-import upload','bank-import parse','bank-import preview') ORDER BY sequence DESC LIMIT 1`).Scan(&hash)
	if e == sql.ErrNoRows {
		return strings.Repeat("0", 64), nil
	}
	return hash, e
}

func (s *Service) PreviewBankImport(ctx context.Context, book, jobID, key string, choices BankImportChoices) (BankImportPlan, error) {
	if e := s.requireActor(); e != nil {
		return BankImportPlan{}, e
	}
	if e := validateOperationKey(key); e != nil {
		return BankImportPlan{}, e
	}
	choices, e := normalizeBankChoices(choices)
	if e != nil {
		return BankImportPlan{}, e
	}
	request, e := bankJSON(choices)
	if e != nil {
		return BankImportPlan{}, e
	}
	requestHash := bankHash(request)
	tx, e := s.store.Begin(ctx)
	if e != nil {
		return BankImportPlan{}, e
	}
	defer func() { _ = tx.Rollback() }()
	job, e := readBankImportJob(ctx, tx, book, jobID)
	if e != nil {
		return BankImportPlan{}, e
	}
	var priorID, priorHash string
	e = tx.QueryRowContext(ctx, `SELECT id,request_sha256 FROM bank_import_plans WHERE job_id=? AND preview_key=?`, jobID, key).Scan(&priorID, &priorHash)
	if e == nil {
		if priorHash != requestHash {
			return BankImportPlan{}, apperr.New(apperr.Conflict, "IDEMPOTENCY_CONFLICT", "preview key was used with different choices")
		}
		return readBankImportPlan(ctx, tx, book, priorID)
	}
	if e != sql.ErrNoRows {
		return BankImportPlan{}, e
	}
	if job.Status != "READY" || job.Document == nil {
		return BankImportPlan{}, apperr.New(apperr.Conflict, "IMPORT_JOB_NOT_READY", "only a parsed, unapplied job can be previewed")
	}
	revision, e := BankLedgerRevision(ctx, tx)
	if e != nil {
		return BankImportPlan{}, e
	}
	// Execute the actual ledger path under a savepoint, then roll back all domain
	// writes before persisting the preview. Validation and apply cannot diverge.
	if _, e = tx.ExecContext(ctx, "SAVEPOINT bank_import_preview"); e != nil {
		return BankImportPlan{}, e
	}
	simulated, e := s.runBankImportTx(ctx, tx, book, job, choices)
	if e != nil {
		return BankImportPlan{}, e
	}
	if _, e = tx.ExecContext(ctx, "ROLLBACK TO bank_import_preview"); e != nil {
		return BankImportPlan{}, e
	}
	if _, e = tx.ExecContext(ctx, "RELEASE bank_import_preview"); e != nil {
		return BankImportPlan{}, e
	}
	id, e := storesqlite.NewID()
	if e != nil {
		return BankImportPlan{}, e
	}
	plan := BankImportPlan{ID: id, JobID: jobID, LedgerRevision: revision, Choices: choices, Summary: simulated.Summary, CreatedAt: storesqlite.UTCNow()}
	payload, e := bankJSON(plan)
	if e != nil {
		return plan, e
	}
	plan.Digest = bankHash(payload)
	if _, e = tx.ExecContext(ctx, `INSERT INTO bank_import_plans(id,job_id,preview_key,request_sha256,ledger_revision,plan_json,plan_sha256,created_at,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, id, jobID, key, requestHash, revision, string(payload), plan.Digest, plan.CreatedAt, s.actor); e != nil {
		return BankImportPlan{}, storesqlite.MapError("save import preview", e)
	}
	if _, e = storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{Actor: s.actor, Command: "bank-import preview", AggregateType: "bank_import_plan", AggregateID: id, Payload: map[string]any{"book": normalizeCode(book), "job_id": jobID, "plan_digest": plan.Digest}}); e != nil {
		return BankImportPlan{}, e
	}
	if e = tx.Commit(); e != nil {
		return BankImportPlan{}, storesqlite.MapError("commit import preview", e)
	}
	return plan, nil
}
func (s *Service) GetBankImportPlan(ctx context.Context, book, id string) (BankImportPlan, error) {
	return readBankImportPlan(ctx, s.store.DB(), book, id)
}
func readBankImportPlan(ctx context.Context, q queryer, book, id string) (BankImportPlan, error) {
	var data, digest string
	var p BankImportPlan
	e := q.QueryRowContext(ctx, `SELECT p.plan_json,p.plan_sha256 FROM bank_import_plans p JOIN bank_import_jobs j ON j.id=p.job_id JOIN books b ON b.id=j.book_id WHERE p.id=? AND b.code=?`, id, normalizeCode(book)).Scan(&data, &digest)
	if e == sql.ErrNoRows {
		return p, apperr.New(apperr.NotFound, "IMPORT_PLAN_NOT_FOUND", "import plan was not found in this company")
	}
	if e != nil {
		return p, e
	}
	if bankHash([]byte(data)) != digest {
		return p, apperr.New(apperr.Integrity, "IMPORT_PLAN_INVALID", "import plan hash does not match")
	}
	if e = json.Unmarshal([]byte(data), &p); e != nil {
		return p, e
	}
	p.Digest = digest
	return p, nil
}

// ApplyBankImport commits the entire selected-account import, mappings, optional
// journals, evidence links and durable receipt in one immediate transaction.
func (s *Service) ApplyBankImport(ctx context.Context, book, planID, digest string) (BankImportReceipt, error) {
	if e := s.requireActor(); e != nil {
		return BankImportReceipt{}, e
	}
	tx, e := s.store.Begin(ctx)
	if e != nil {
		return BankImportReceipt{}, e
	}
	defer func() { _ = tx.Rollback() }()
	plan, e := readBankImportPlan(ctx, tx, book, planID)
	if e != nil {
		return BankImportReceipt{}, e
	}
	if plan.Digest != digest {
		return BankImportReceipt{}, apperr.New(apperr.Conflict, "IMPORT_PLAN_MISMATCH", "apply must name the exact preview digest")
	}
	job, e := readBankImportJob(ctx, tx, book, plan.JobID)
	if e != nil {
		return BankImportReceipt{}, e
	}
	if job.Status == "APPLIED" {
		if job.Receipt == nil || job.Receipt.PlanID != planID || job.Receipt.PlanDigest != digest {
			return BankImportReceipt{}, apperr.New(apperr.Conflict, "IMPORT_ALREADY_APPLIED", "job was applied using another preview")
		}
		return *job.Receipt, nil
	}
	if job.Status != "READY" {
		return BankImportReceipt{}, apperr.New(apperr.Conflict, "IMPORT_JOB_NOT_READY", "job is not ready for apply")
	}
	revision, e := BankLedgerRevision(ctx, tx)
	if e != nil {
		return BankImportReceipt{}, e
	}
	if revision != plan.LedgerRevision {
		return BankImportReceipt{}, apperr.New(apperr.Conflict, "IMPORT_PLAN_STALE", "ledger changed; create and inspect a new preview")
	}
	if _, e = bankImportSource(ctx, tx, book, job.ID); e != nil {
		return BankImportReceipt{}, e
	}
	receipt, e := s.runBankImportTx(ctx, tx, book, job, plan.Choices)
	if e != nil {
		return receipt, e
	}
	receipt.JobID = job.ID
	receipt.PlanID = planID
	receipt.PlanDigest = digest
	receipt.AppliedAt = storesqlite.UTCNow()
	payload, e := bankJSON(receipt)
	if e != nil {
		return receipt, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE bank_import_jobs SET status='APPLIED',applied_plan_id=?,receipt_json=?,receipt_sha256=?,completed_at=? WHERE id=?`, planID, string(payload), bankHash(payload), receipt.AppliedAt, job.ID); e != nil {
		return BankImportReceipt{}, storesqlite.MapError("save import receipt", e)
	}
	if _, e = storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{Actor: s.actor, Command: "bank-import apply", AggregateType: "bank_import_job", AggregateID: job.ID, Payload: map[string]any{"book": normalizeCode(book), "plan_id": planID, "receipt_sha256": bankHash(payload)}}); e != nil {
		return BankImportReceipt{}, e
	}
	if e = tx.Commit(); e != nil {
		return BankImportReceipt{}, storesqlite.MapError("commit bank import", e)
	}
	return receipt, nil
}

func (s *Service) runBankImportTx(ctx context.Context, tx *sql.Tx, book string, job BankImportJob, choices BankImportChoices) (BankImportReceipt, error) {
	receipt := BankImportReceipt{Accounts: []BankImportAccountReceipt{}, JournalIDs: []string{}}
	bookID, e := bankBookID(ctx, tx, book)
	if e != nil {
		return receipt, e
	}
	if job.Document == nil || !banking.SupportedParser(job.Document.Parser) {
		return receipt, apperr.New(apperr.Conflict, "IMPORT_PARSER_UNSUPPORTED", "parsed evidence requires a supported parser version")
	}
	accounts := map[string]banking.Account{}
	for _, a := range job.Document.Accounts {
		accounts[a.Key] = a
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	for _, x := range choices.Exclusions {
		if _, ok := accounts[x.AccountKey]; !ok || seen[x.AccountKey] {
			return receipt, apperr.New(apperr.Input, "IMPORT_MAPPING_INVALID", "unknown or repeated excluded account")
		}
		seen[x.AccountKey] = true
	}
	for _, mapping := range choices.Mappings {
		account, ok := accounts[mapping.AccountKey]
		if !ok || seen[mapping.AccountKey] || targets[mapping.StatementAccount] {
			return receipt, apperr.New(apperr.Input, "IMPORT_MAPPING_INVALID", "every source account and target must have one explicit mapping")
		}
		seen[mapping.AccountKey] = true
		targets[mapping.StatementAccount] = true
		targetID, control, e := bankTarget(ctx, tx, bookID, mapping, account)
		if e != nil {
			return receipt, e
		}
		if e = s.bindBankAccountTx(ctx, tx, job, account, mapping.StatementAccount, targetID); e != nil {
			return receipt, e
		}
		sourceSystem := bankSourceSystem(job.Document.Format, account)
		input := StatementImportInput{StatementAccount: mapping.StatementAccount, SourceSystem: sourceSystem, SourceName: "bank-import:" + job.ID + "/" + account.Key, FileSHA256: job.SourceSHA256, Transactions: []StatementTransactionInput{}}
		var nonPostable map[string]bool
		input.Transactions, nonPostable, e = prepareBankMovements(ctx, tx, targetID, sourceSystem, account, mapping)
		if e != nil {
			return receipt, e
		}
		if e = validateBankReplay(ctx, tx, targetID, input); e != nil {
			return receipt, e
		}
		imported, e := s.importStatementTransactionsTx(ctx, tx, input)
		if e != nil {
			return receipt, e
		}
		receipt.Accounts = append(receipt.Accounts, BankImportAccountReceipt{StatementAccount: mapping.StatementAccount, BatchID: imported.BatchID})
		if imported.Changed {
			var materialized, retained int
			if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(disposition='POSTED'),0),COALESCE(SUM(disposition<>'POSTED'),0) FROM source_records WHERE import_batch_id=?`, imported.BatchID).Scan(&materialized, &retained); e != nil {
				return receipt, e
			}
			receipt.Summary.ImportedTransactions += materialized
			receipt.Summary.RetainedObservations += retained
		}
		receipt.Summary.SkippedTransactions += imported.SkippedCount
		if choices.Post {
			if e = s.postBankTransactionsTx(ctx, tx, book, bookID, targetID, control, sourceSystem, account, mapping, nonPostable, &receipt); e != nil {
				return receipt, e
			}
		}
	}
	if len(seen) != len(accounts) {
		return receipt, apperr.New(apperr.Input, "IMPORT_ACCOUNT_UNMAPPED", "map every source account or explicitly exclude it with a reason")
	}
	return receipt, nil
}

func (s *Service) bindBankAccountTx(ctx context.Context, tx *sql.Tx, job BankImportJob, a banking.Account, target, targetID string) error {
	family := banking.Family(job.Document.Format)
	realm := "STATEMENTS-V1"
	if family == "OFX" {
		realm = "BANK-CARD-V1"
	}
	var existingTarget string
	e := tx.QueryRowContext(ctx, `SELECT statement_account_id FROM statement_account_identities WHERE source_system=? AND source_realm=? AND external_id=?`, family, realm, a.Key).Scan(&existingTarget)
	if e == nil {
		if existingTarget != targetID {
			return apperr.New(apperr.Conflict, "IMPORT_ACCOUNT_ALREADY_MAPPED", "source account is already mapped to another statement account")
		}
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	var other int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM statement_account_identities WHERE statement_account_id=? AND source_system=? AND source_realm=?`, targetID, family, realm).Scan(&other); e != nil {
		return e
	}
	if other != 0 {
		return apperr.New(apperr.Conflict, "IMPORT_TARGET_ALREADY_MAPPED", "target is already bound to another account in this format family")
	}
	_, e = s.addStatementAccountIdentityTx(ctx, tx, AddStatementAccountIdentityInput{StatementAccount: target, SourceSystem: family, SourceRealm: realm, ExternalID: a.Key, AccountNumber: a.AccountID, Name: a.Kind, Active: true, Evidence: StatementAccountIdentityEvidence{SourceKind: family, SourcePath: "bank-import:" + job.ID, SourceSHA256: job.SourceSHA256, Locator: "account:" + a.Key}})
	return e
}

func (s *Service) postBankTransactionsTx(ctx context.Context, tx *sql.Tx, book, bookID, targetID, control, sourceSystem string, account banking.Account, mapping BankAccountMapping, nonPostable map[string]bool, receipt *BankImportReceipt) error {
	classifications := map[string]string{}
	for _, c := range mapping.Classifications {
		if _, exists := classifications[c.TransactionID]; exists || c.ContraAccount == "" {
			return apperr.New(apperr.Input, "IMPORT_CLASSIFICATION_INVALID", "every classification needs one transaction ID and contra account")
		}
		classifications[c.TransactionID] = c.ContraAccount
	}
	for _, t := range account.Transactions {
		amount, e := money.ParseCurrency(t.Amount, account.Currency)
		if e != nil {
			return e
		}
		contra, classified := classifications[t.ID]
		delete(classifications, t.ID)
		if amount == 0 || nonPostable[t.ID] {
			if classified {
				return apperr.New(apperr.Input, "IMPORT_CLASSIFICATION_INVALID", "zero, pending, review, and duplicate observations do not create journals")
			}
			continue
		}
		if !classified {
			return apperr.New(apperr.Input, "IMPORT_CLASSIFICATION_REQUIRED", "posting requires an explicit contra account for every booked nonzero transaction")
		}
		// Transfer matching is a distinct accounting workflow. Posting both imported
		// bank legs independently would double the transfer; this slice refuses it.
		var contraType string
		e = tx.QueryRowContext(ctx, `SELECT a.account_type FROM accounts a JOIN book_accounts ba ON ba.account_id=a.id WHERE a.code=? AND ba.book_id=?`, contra, bookID).Scan(&contraType)
		if e == sql.ErrNoRows {
			return apperr.New(apperr.NotFound, "IMPORT_CONTRA_NOT_FOUND", "contra account is not configured in this company")
		}
		if e != nil {
			return e
		}
		if contraType != "REVENUE" && contraType != "EXPENSE" && contraType != "EQUITY" {
			return apperr.New(apperr.Input, "IMPORT_TRANSFER_UNSUPPORTED", "automatic bank import posting supports revenue, expense, and equity classifications; import transfer evidence without posting")
		}
		var period string
		e = tx.QueryRowContext(ctx, `SELECT fp.code FROM fiscal_periods fp JOIN book_periods bp ON bp.period_id=fp.id WHERE bp.book_id=? AND ? BETWEEN fp.start_date AND fp.end_date`, bookID, t.PostedDate).Scan(&period)
		if e == sql.ErrNoRows {
			return apperr.New(apperr.Conflict, "IMPORT_PERIOD_MISSING", "transaction has no fiscal period in this book")
		}
		if e != nil {
			return e
		}
		var sourceID string
		e = tx.QueryRowContext(ctx, `SELECT id FROM current_source_records WHERE statement_account_id=? AND source_system=? AND external_id=?`, targetID, sourceSystem, t.ID).Scan(&sourceID)
		if e != nil {
			return e
		}
		key := "statement:" + t.ID
		var existingID string
		e = tx.QueryRowContext(ctx, `SELECT id FROM journal_entries WHERE book_id=? AND source_system=? AND source_key=?`, bookID, sourceSystem, key).Scan(&existingID)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		var otherLinks int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_record_journals WHERE source_record_id=? AND journal_entry_id<>?`, sourceID, existingID).Scan(&otherLinks); e != nil {
			return e
		}
		if otherLinks != 0 {
			return apperr.New(apperr.Conflict, "IMPORT_SOURCE_ALREADY_LINKED", "source already has accounting links; use the existing journal workflow")
		}
		lines := []JournalLineInput{{Account: control}, {Account: contra}}
		if amount > 0 {
			lines[0].DebitCents = amount
			lines[1].CreditCents = amount
		} else {
			lines[0].CreditCents = -amount
			lines[1].DebitCents = -amount
		}
		journal, e := s.createJournalTx(ctx, tx, CreateJournalInput{Book: book, Kind: "STANDARD", PostingDate: t.PostedDate, Period: period, Description: t.Description, SourceSystem: sourceSystem, SourceKey: key, Lines: lines}, true)
		if e != nil {
			return e
		}
		if journal.Status != "POSTED" {
			return apperr.New(apperr.Conflict, "IMPORT_JOURNAL_NOT_POSTED", "existing import journal is not posted")
		}
		if _, e = s.linkSourceRecordJournalTx(ctx, tx, sourceID, journal.ID, "EVIDENCE"); e != nil {
			return e
		}
		receipt.JournalIDs = append(receipt.JournalIDs, journal.ID)
		if existingID == "" {
			receipt.Summary.NewJournals++
		} else {
			receipt.Summary.ExistingJournals++
		}
	}
	if len(classifications) != 0 {
		return apperr.New(apperr.Input, "IMPORT_CLASSIFICATION_INVALID", "classification names a transaction absent from this account")
	}
	return nil
}
