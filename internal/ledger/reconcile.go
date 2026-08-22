package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type CreateStatementAccountInput struct {
	Code                       string `json:"code"`
	Entity                     string `json:"entity"`
	Book                       string `json:"book"`
	GLAccount                  string `json:"gl_account"`
	Name                       string `json:"name"`
	Kind                       string `json:"kind"`
	Currency                   string `json:"currency"`
	RequiredForClose           bool   `json:"required_for_close"`
	ReconciliationRequiredFrom string `json:"reconciliation_required_from"`
}

type StatementAccount struct {
	ID                            string `json:"id"`
	Code                          string `json:"code"`
	EntityCode                    string `json:"entity"`
	BookCode                      string `json:"book"`
	GLAccountCode                 string `json:"gl_account"`
	Name                          string `json:"name"`
	Kind                          string `json:"kind"`
	Currency                      string `json:"currency"`
	RequiredForClose              bool   `json:"required_for_close"`
	ReconciliationRequiredFrom    string `json:"reconciliation_required_from"`
	ReconciliationRequiredThrough string `json:"reconciliation_required_through,omitempty"`
	Status                        string `json:"status"`
	ArchivedAt                    string `json:"archived_at,omitempty"`
	ArchivedBy                    string `json:"archived_by,omitempty"`
	ArchiveReason                 string `json:"archive_reason,omitempty"`
}

type ArchiveStatementAccountInput struct {
	Code                          string `json:"code"`
	ReconciliationRequiredThrough string `json:"reconciliation_required_through"`
	Reason                        string `json:"reason"`
}

type StatementTransactionInput struct {
	ExternalID          string          `json:"external_id"`
	PostedDate          string          `json:"posted_date"`
	Description         string          `json:"description"`
	AmountCents         int64           `json:"amount_cents"`
	Disposition         string          `json:"disposition,omitempty"`
	ExclusionReason     string          `json:"exclusion_reason,omitempty"`
	TaxType             string          `json:"tax_type,omitempty"`
	TaxAccountingPeriod string          `json:"tax_accounting_period,omitempty"`
	RawJSON             json.RawMessage `json:"raw_json,omitempty"`
	ResolutionReason    string          `json:"resolution_reason,omitempty"`
	ResolutionEvidence  json.RawMessage `json:"resolution_evidence,omitempty"`
}

type StatementImportInput struct {
	StatementAccount string                      `json:"statement_account"`
	SourceSystem     string                      `json:"source_system"`
	SourceName       string                      `json:"source_name"`
	FileSHA256       string                      `json:"file_sha256"`
	Transactions     []StatementTransactionInput `json:"transactions"`
}

type ImportResult struct {
	BatchID                   string `json:"batch_id"`
	Changed                   bool   `json:"changed"`
	RecordCount               int    `json:"record_count"`
	StatementTransactionCount int    `json:"statement_transaction_count"`
	SourceOnlyCount           int    `json:"source_only_count"`
	SkippedCount              int    `json:"skipped_count"`
}

const (
	SourceDispositionPosted      = "POSTED"
	SourceDispositionPending     = "PENDING"
	SourceDispositionNeedsReview = "NEEDS_REVIEW"
	SourceDispositionSourceOnly  = "SOURCE_ONLY"
)

type Reconciliation struct {
	ID                             string `json:"id"`
	StatementAccountCode           string `json:"statement_account"`
	StartDate                      string `json:"start_date"`
	EndDate                        string `json:"end_date"`
	BeginningBalanceCents          int64  `json:"beginning_balance_cents"`
	StatementActivityCents         int64  `json:"statement_activity_cents"`
	EndingBalanceCents             int64  `json:"ending_balance_cents"`
	CalculatedEndingCents          int64  `json:"calculated_ending_cents"`
	LedgerBeginningCents           int64  `json:"ledger_beginning_cents"`
	LedgerEndingCents              int64  `json:"ledger_ending_cents"`
	OpeningOutstandingCents        int64  `json:"opening_outstanding_cents"`
	EndingOutstandingCents         int64  `json:"ending_outstanding_cents"`
	OutstandingLineCount           int    `json:"outstanding_line_count"`
	OutstandingMismatchCount       int    `json:"outstanding_mismatch_count"`
	BeginningDifferenceCents       int64  `json:"beginning_difference_cents"`
	StatementDifferenceCents       int64  `json:"statement_difference_cents"`
	LedgerDifferenceCents          int64  `json:"ledger_difference_cents"`
	StatementTransactionCount      int    `json:"statement_transaction_count"`
	FullyAllocatedStatementCount   int    `json:"fully_allocated_statement_count"`
	ControlLineCount               int    `json:"control_line_count"`
	FullyAllocatedControlLineCount int    `json:"fully_allocated_control_line_count"`
	AllocationCount                int    `json:"allocation_count"`
	Status                         string `json:"status"`
	AbandonedAt                    string `json:"abandoned_at,omitempty"`
	AbandonedBy                    string `json:"abandoned_by,omitempty"`
	AbandonReason                  string `json:"abandon_reason,omitempty"`
}

// ManualReconciliationPrior binds a reviewed manual reconciliation plan to the
// exact reconciliation boundary that existed when the plan was generated.
type ManualReconciliationPrior struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	EndDate            string `json:"end_date"`
	EndingBalanceCents int64  `json:"ending_balance_cents"`
}

// ManualReconciliationLine binds one synthetic statement transaction to the
// posted control-account line it clears.
type ManualReconciliationLine struct {
	JournalLineID string          `json:"journal_line_id"`
	ExternalID    string          `json:"external_id"`
	LedgerDate    string          `json:"ledger_date"`
	StatementDate string          `json:"statement_date"`
	Description   string          `json:"description"`
	AmountCents   int64           `json:"amount_cents"`
	RawJSON       json.RawMessage `json:"raw_json"`
}

// ManualReconciliationInput is the complete optimistic-concurrency contract
// for applying a reviewed manual reconciliation plan.
type ManualReconciliationInput struct {
	StatementAccount                    string                     `json:"statement_account"`
	TargetReconciliationID              string                     `json:"target_reconciliation_id,omitempty"`
	SourceName                          string                     `json:"source_name"`
	PlanDigest                          string                     `json:"plan_digest"`
	StartDate                           string                     `json:"start_date"`
	EndDate                             string                     `json:"end_date"`
	BeginningBalanceCents               int64                      `json:"beginning_balance_cents"`
	EndingBalanceCents                  int64                      `json:"ending_balance_cents"`
	ExpectedLedgerBeginningCents        int64                      `json:"expected_ledger_beginning_cents"`
	ExpectedLedgerEndingCents           int64                      `json:"expected_ledger_ending_cents"`
	ExpectedOpeningOutstandingCents     int64                      `json:"expected_opening_outstanding_cents"`
	ExpectedEndingOutstandingCents      int64                      `json:"expected_ending_outstanding_cents"`
	ExpectedTargetBeginningBalanceCents int64                      `json:"expected_target_beginning_balance_cents,omitempty"`
	ExpectedTargetEndingBalanceCents    int64                      `json:"expected_target_ending_balance_cents,omitempty"`
	ExpectedTargetReopenedAt            string                     `json:"expected_target_reopened_at,omitempty"`
	ExpectedStatementTransactionCount   int                        `json:"expected_statement_transaction_count"`
	PriorReconciliation                 *ManualReconciliationPrior `json:"prior_reconciliation,omitempty"`
	ExpectedLines                       []ManualReconciliationLine `json:"expected_lines"`
	Lines                               []ManualReconciliationLine `json:"lines"`
	Outstanding                         []ManualReconciliationLine `json:"outstanding"`
}

type ReconciliationFilter struct {
	StatementAccount string
	Status           string
	FromDate         string
	ToDate           string
}

type ReconciliationAllocation struct {
	ID                     string `json:"id"`
	ReconciliationID       string `json:"reconciliation_id"`
	StatementTransactionID string `json:"statement_transaction_id"`
	StatementExternalID    string `json:"statement_external_id"`
	StatementPostedDate    string `json:"statement_posted_date"`
	JournalLineID          string `json:"journal_line_id"`
	JournalID              string `json:"journal_id"`
	JournalEntryNumber     int64  `json:"journal_entry_number"`
	JournalLineNumber      int    `json:"journal_line_number"`
	AllocatedAmountCents   int64  `json:"allocated_amount_cents"`
}

func (s *Service) CreateStatementAccount(ctx context.Context, input CreateStatementAccountInput) (StatementAccount, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccount{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return StatementAccount{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := s.createStatementAccountTx(ctx, tx, input)
	if err != nil {
		return StatementAccount{}, err
	}
	if err := tx.Commit(); err != nil {
		return StatementAccount{}, storesqlite.MapError("commit statement account", err)
	}
	return result, nil
}

// CreateAccountWithStatement creates a GL account and its reconciliation
// control in one SQLite transaction. Either both audited objects exist or
// neither does.
func (s *Service) CreateAccountWithStatement(ctx context.Context, accountInput CreateAccountInput, statementInput CreateStatementAccountInput) (Account, StatementAccount, error) {
	if err := s.requireActor(); err != nil {
		return Account{}, StatementAccount{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Account{}, StatementAccount{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	account, err := s.createAccountTx(ctx, tx, accountInput)
	if err != nil {
		return Account{}, StatementAccount{}, err
	}
	statement, err := s.createStatementAccountTx(ctx, tx, statementInput)
	if err != nil {
		return Account{}, StatementAccount{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, StatementAccount{}, storesqlite.MapError("commit account and statement account", err)
	}
	return account, statement, nil
}

func (s *Service) createStatementAccountTx(ctx context.Context, tx *sql.Tx, input CreateStatementAccountInput) (StatementAccount, error) {
	input.Code = normalizeCode(input.Code)
	input.Entity = normalizeCode(input.Entity)
	input.Book = normalizeCode(input.Book)
	input.GLAccount = normalizeCode(input.GLAccount)
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = normalizeCode(input.Kind)
	input.Currency = normalizeCode(input.Currency)
	input.ReconciliationRequiredFrom = strings.TrimSpace(input.ReconciliationRequiredFrom)
	if input.Code == "" || input.Entity == "" || input.Book == "" || input.GLAccount == "" || input.Name == "" {
		return StatementAccount{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_INVALID", "code, entity, book, GL account, and name are required")
	}
	if err := validateDate(input.ReconciliationRequiredFrom, "statement account reconciliation-required-from date"); err != nil {
		return StatementAccount{}, err
	}
	if input.Kind != "BANK" && input.Kind != "CREDIT_CARD" && input.Kind != "LOAN" && input.Kind != "INVESTMENT" {
		return StatementAccount{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_KIND_INVALID", "statement account kind must be BANK, CREDIT_CARD, LOAN, or INVESTMENT")
	}
	if !money.IsSupportedCurrency(input.Currency) {
		return StatementAccount{}, apperr.New(apperr.Invalid, "CURRENCY_NOT_SUPPORTED", "this release supports USD as its only functional currency")
	}
	entityID, err := lookupID(ctx, tx, "entities", input.Entity)
	if err != nil {
		return StatementAccount{}, err
	}
	bookID, err := lookupID(ctx, tx, "books", input.Book)
	if err != nil {
		return StatementAccount{}, err
	}
	accountID, err := lookupID(ctx, tx, "accounts", input.GLAccount)
	if err != nil {
		return StatementAccount{}, err
	}
	var bookCurrency, entityCurrency, accountType string
	err = tx.QueryRowContext(ctx, `SELECT b.currency, e.functional_currency, a.account_type
        FROM books b
        JOIN entities e ON e.id = b.entity_id
        JOIN book_accounts ba ON ba.book_id = b.id AND ba.account_id = ?
        JOIN accounts a ON a.id = ba.account_id
        WHERE b.id = ? AND b.entity_id = ? AND b.kind = 'ACTUAL'`, accountID, bookID, entityID).Scan(
		&bookCurrency, &entityCurrency, &accountType)
	if err == sql.ErrNoRows {
		return StatementAccount{}, apperr.New(apperr.Validation, "STATEMENT_ACCOUNT_LINK_INVALID", "statement account must use an enabled control account in the entity actual book")
	}
	if err != nil {
		return StatementAccount{}, storesqlite.MapError("validate statement account link", err)
	}
	if input.Currency != bookCurrency || input.Currency != entityCurrency {
		return StatementAccount{}, apperr.New(apperr.Validation, "STATEMENT_ACCOUNT_CURRENCY_MISMATCH", "statement account currency must equal the actual book and entity currency")
	}
	expectedType := "LIABILITY"
	if input.Kind == "BANK" || input.Kind == "INVESTMENT" {
		expectedType = "ASSET"
	}
	if accountType != expectedType {
		return StatementAccount{}, apperr.New(apperr.Validation, "STATEMENT_ACCOUNT_GL_TYPE_INVALID", fmt.Sprintf("%s statement accounts require a %s control account", input.Kind, expectedType))
	}
	var assignedCode string
	err = tx.QueryRowContext(ctx, `SELECT code FROM statement_accounts
        WHERE book_id = ? AND gl_account_id = ?`, bookID, accountID).Scan(&assignedCode)
	if err == nil {
		return StatementAccount{}, apperr.New(apperr.Conflict, "STATEMENT_CONTROL_ACCOUNT_ASSIGNED", fmt.Sprintf("control account is already assigned to statement account %s", assignedCode))
	}
	if err != sql.ErrNoRows {
		return StatementAccount{}, storesqlite.MapError("check statement control account assignment", err)
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return StatementAccount{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO statement_accounts
        (id, code, entity_id, book_id, gl_account_id, name, account_kind, currency,
		 required_for_close, reconciliation_required_from, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, input.Code, entityID, bookID, accountID,
		input.Name, input.Kind, input.Currency, input.RequiredForClose,
		input.ReconciliationRequiredFrom, storesqlite.UTCNow()); err != nil {
		return StatementAccount{}, storesqlite.MapError("create statement account", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement-account create", AggregateType: "statement_account", AggregateID: id,
		Payload: map[string]any{"code": input.Code, "entity": input.Entity, "book": input.Book, "gl_account": input.GLAccount, "kind": input.Kind, "required_for_close": input.RequiredForClose, "reconciliation_required_from": input.ReconciliationRequiredFrom},
	}); err != nil {
		return StatementAccount{}, err
	}
	return StatementAccount{ID: id, Code: input.Code, EntityCode: input.Entity, BookCode: input.Book, GLAccountCode: input.GLAccount, Name: input.Name, Kind: input.Kind, Currency: input.Currency, RequiredForClose: input.RequiredForClose, ReconciliationRequiredFrom: input.ReconciliationRequiredFrom, Status: "ACTIVE"}, nil
}

func (s *Service) ListStatementAccounts(ctx context.Context, entity string) ([]StatementAccount, error) {
	query := `SELECT sa.id, sa.code, e.code, b.code, a.code, sa.name, sa.account_kind, sa.currency,
		sa.required_for_close, sa.reconciliation_required_from,
		COALESCE(sa.reconciliation_required_through, ''), sa.status,
		sa.archived_at, sa.archived_by, sa.archive_reason
        FROM statement_accounts sa JOIN entities e ON e.id = sa.entity_id
        JOIN books b ON b.id = sa.book_id JOIN accounts a ON a.id = sa.gl_account_id`
	var args []any
	if entity != "" {
		query += " WHERE e.code = ?"
		args = append(args, normalizeCode(entity))
	}
	query += " ORDER BY sa.code"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list statement accounts", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []StatementAccount
	for rows.Next() {
		var account StatementAccount
		if err := rows.Scan(&account.ID, &account.Code, &account.EntityCode, &account.BookCode, &account.GLAccountCode,
			&account.Name, &account.Kind, &account.Currency, &account.RequiredForClose,
			&account.ReconciliationRequiredFrom, &account.ReconciliationRequiredThrough, &account.Status, &account.ArchivedAt, &account.ArchivedBy, &account.ArchiveReason); err != nil {
			return nil, err
		}
		result = append(result, account)
	}
	return result, rows.Err()
}

type statementAccountArchiveQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func normalizeStatementAccountArchiveInput(input ArchiveStatementAccountInput) (ArchiveStatementAccountInput, error) {
	input.Code = normalizeCode(input.Code)
	input.ReconciliationRequiredThrough = strings.TrimSpace(input.ReconciliationRequiredThrough)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Code == "" || input.ReconciliationRequiredThrough == "" || input.Reason == "" {
		return ArchiveStatementAccountInput{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_ARCHIVE_INVALID", "statement account code, reconciliation-required-through date, and archive reason are required")
	}
	if err := validateDate(input.ReconciliationRequiredThrough, "statement account reconciliation-required-through date"); err != nil {
		return ArchiveStatementAccountInput{}, err
	}
	return input, nil
}

func validateStatementAccountArchive(ctx context.Context, queryer statementAccountArchiveQueryer, input ArchiveStatementAccountInput) (StatementAccount, error) {
	var result StatementAccount
	err := queryer.QueryRowContext(ctx, `SELECT sa.id, sa.code, e.code, b.code, a.code, sa.name,
		sa.account_kind, sa.currency, sa.required_for_close,
		sa.reconciliation_required_from, COALESCE(sa.reconciliation_required_through, ''), sa.status,
		sa.archived_at, sa.archived_by, sa.archive_reason
		FROM statement_accounts sa
		JOIN entities e ON e.id = sa.entity_id
		JOIN books b ON b.id = sa.book_id
		JOIN accounts a ON a.id = sa.gl_account_id
		WHERE sa.code = ?`, input.Code).Scan(
		&result.ID, &result.Code, &result.EntityCode, &result.BookCode, &result.GLAccountCode,
		&result.Name, &result.Kind, &result.Currency,
		&result.RequiredForClose, &result.ReconciliationRequiredFrom, &result.ReconciliationRequiredThrough, &result.Status,
		&result.ArchivedAt, &result.ArchivedBy, &result.ArchiveReason)
	if err == sql.ErrNoRows {
		return StatementAccount{}, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	}
	if err != nil {
		return StatementAccount{}, storesqlite.MapError("read statement account", err)
	}
	if result.Status != "ACTIVE" {
		return StatementAccount{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_NOT_ACTIVE", "statement account is not active")
	}
	if input.ReconciliationRequiredThrough < result.ReconciliationRequiredFrom {
		return StatementAccount{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_ARCHIVE_DATE_INVALID", "reconciliation-required-through date precedes the statement account reconciliation-required-from date")
	}
	var unmaterializedPosted int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM current_source_records source
		WHERE source.materialization_kind = 'STATEMENT'
		  AND source.statement_account_id = ?
		  AND source.disposition = 'POSTED'
		  AND NOT EXISTS (
		      SELECT 1 FROM statement_transactions materialized
		      WHERE materialized.source_identity_id = source.source_identity_id
		        AND materialized.source_record_id = source.id
		  )`, result.ID).Scan(&unmaterializedPosted); err != nil {
		return StatementAccount{}, storesqlite.MapError("check statement account source materialization", err)
	}
	if unmaterializedPosted != 0 {
		return StatementAccount{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_HAS_UNMATERIALIZED_SOURCE", "current posted statement source must be materialized before archiving the statement account")
	}
	var openReconciliations int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliations
		WHERE statement_account_id = ? AND status = 'OPEN'`, result.ID).Scan(&openReconciliations); err != nil {
		return StatementAccount{}, storesqlite.MapError("check statement account reconciliations", err)
	}
	if openReconciliations != 0 {
		return StatementAccount{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_HAS_OPEN_RECONCILIATION", "complete or abandon open reconciliation work before archiving the statement account")
	}
	var laterActivity int
	if err := queryer.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM statement_transactions WHERE statement_account_id = ? AND posted_date > ?) +
		(SELECT COUNT(*) FROM reconciliations WHERE statement_account_id = ? AND end_date > ? AND status <> 'ABANDONED')`,
		result.ID, input.ReconciliationRequiredThrough, result.ID, input.ReconciliationRequiredThrough).Scan(&laterActivity); err != nil {
		return StatementAccount{}, storesqlite.MapError("check statement account archive date", err)
	}
	if laterActivity != 0 {
		return StatementAccount{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_HAS_LATER_ACTIVITY", "reconciliation-required-through date precedes statement transactions or non-abandoned reconciliation evidence")
	}
	return result, nil
}

// ValidateStatementAccountArchive performs the exact target, boundary, source,
// reconciliation, and activity checks used by ArchiveStatementAccount without
// writing. The returned value describes the validated target state.
func (s *Service) ValidateStatementAccountArchive(ctx context.Context, input ArchiveStatementAccountInput) (StatementAccount, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccount{}, err
	}
	input, err := normalizeStatementAccountArchiveInput(input)
	if err != nil {
		return StatementAccount{}, err
	}
	result, err := validateStatementAccountArchive(ctx, s.store.DB(), input)
	if err != nil {
		return StatementAccount{}, err
	}
	result.Status = "ARCHIVED"
	result.ReconciliationRequiredThrough = input.ReconciliationRequiredThrough
	result.ArchivedBy = s.actor
	result.ArchiveReason = input.Reason
	return result, nil
}

func (s *Service) ArchiveStatementAccount(ctx context.Context, input ArchiveStatementAccountInput) (StatementAccount, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccount{}, err
	}
	input, err := normalizeStatementAccountArchiveInput(input)
	if err != nil {
		return StatementAccount{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return StatementAccount{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := validateStatementAccountArchive(ctx, tx, input)
	if err != nil {
		return StatementAccount{}, err
	}
	archivedAt := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = ?, archived_at = ?, archived_by = ?, archive_reason = ?
		WHERE id = ? AND status = 'ACTIVE'`, input.ReconciliationRequiredThrough, archivedAt, s.actor, input.Reason, result.ID); err != nil {
		return StatementAccount{}, storesqlite.MapError("archive statement account", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement-account archive", AggregateType: "statement_account", AggregateID: result.ID,
		Payload: map[string]any{"code": result.Code, "reconciliation_required_through": input.ReconciliationRequiredThrough, "reason": input.Reason},
	}); err != nil {
		return StatementAccount{}, err
	}
	if err := tx.Commit(); err != nil {
		return StatementAccount{}, storesqlite.MapError("commit statement account archive", err)
	}
	result.Status = "ARCHIVED"
	result.ReconciliationRequiredThrough = input.ReconciliationRequiredThrough
	result.ArchivedAt = archivedAt
	result.ArchivedBy = s.actor
	result.ArchiveReason = input.Reason
	return result, nil
}

func (s *Service) ImportStatementTransactions(ctx context.Context, input StatementImportInput) (ImportResult, error) {
	if err := s.requireActor(); err != nil {
		return ImportResult{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := s.importStatementTransactionsTx(ctx, tx, input)
	if err != nil {
		return ImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, storesqlite.MapError("commit statement import", err)
	}
	return result, nil
}

func (s *Service) importStatementTransactionsTx(ctx context.Context, tx *sql.Tx, input StatementImportInput) (ImportResult, error) {
	input.StatementAccount = normalizeCode(input.StatementAccount)
	input.SourceSystem = normalizeCode(input.SourceSystem)
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.FileSHA256 = strings.ToLower(strings.TrimSpace(input.FileSHA256))
	if input.StatementAccount == "" || input.SourceSystem == "" || input.SourceName == "" || len(input.FileSHA256) != 64 {
		return ImportResult{}, apperr.New(apperr.Invalid, "IMPORT_INVALID", "statement account, source system, source name, and a SHA-256 are required")
	}
	if _, err := hex.DecodeString(input.FileSHA256); err != nil {
		return ImportResult{}, apperr.New(apperr.Invalid, "IMPORT_INVALID", "file SHA-256 is invalid")
	}
	var statementAccountID, entityID, bookID, statementStatus string
	err := tx.QueryRowContext(ctx, `SELECT id, entity_id, book_id, status FROM statement_accounts WHERE code = ?`, input.StatementAccount).Scan(&statementAccountID, &entityID, &bookID, &statementStatus)
	if err == sql.ErrNoRows {
		return ImportResult{}, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	}
	if err != nil {
		return ImportResult{}, err
	}
	if statementStatus != "ACTIVE" {
		return ImportResult{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_NOT_ACTIVE", "cannot import transactions for an archived statement account")
	}
	var priorBatchID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM import_batches WHERE source_system = ? AND file_sha256 = ? AND status = 'COMPLETED'`, input.SourceSystem, input.FileSHA256).Scan(&priorBatchID)
	if err == nil {
		result := ImportResult{BatchID: priorBatchID, Changed: false, RecordCount: len(input.Transactions), SkippedCount: len(input.Transactions)}
		if err := tx.QueryRowContext(ctx, `SELECT
            COALESCE(SUM(CASE WHEN disposition = 'POSTED' THEN 1 ELSE 0 END), 0),
            COALESCE(SUM(CASE WHEN disposition <> 'POSTED' THEN 1 ELSE 0 END), 0)
            FROM source_records WHERE import_batch_id = ?`, priorBatchID).Scan(
			&result.StatementTransactionCount, &result.SourceOnlyCount); err != nil {
			return ImportResult{}, storesqlite.MapError("read prior statement import counts", err)
		}
		return result, nil
	}
	if err != sql.ErrNoRows {
		return ImportResult{}, err
	}
	batchID, err := storesqlite.NewID()
	if err != nil {
		return ImportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO import_batches
        (id, source_system, entity_id, source_name, file_sha256, status, record_count, created_at)
        VALUES (?, ?, ?, ?, ?, 'STAGED', ?, ?)`, batchID, input.SourceSystem, entityID, input.SourceName, input.FileSHA256, len(input.Transactions), storesqlite.UTCNow()); err != nil {
		return ImportResult{}, storesqlite.MapError("stage statement import", err)
	}
	result := ImportResult{BatchID: batchID, Changed: true, RecordCount: len(input.Transactions)}
	for i, record := range input.Transactions {
		record.ExternalID = strings.TrimSpace(record.ExternalID)
		record.Description = strings.TrimSpace(record.Description)
		record.Disposition = normalizeCode(record.Disposition)
		if record.Disposition == "" {
			record.Disposition = SourceDispositionPosted
		}
		record.ExclusionReason = strings.TrimSpace(record.ExclusionReason)
		record.TaxType = normalizeCode(record.TaxType)
		record.TaxAccountingPeriod = strings.TrimSpace(record.TaxAccountingPeriod)
		if record.ExternalID == "" || record.Description == "" {
			return ImportResult{}, apperr.New(apperr.Input, "IMPORT_ROW_INVALID", fmt.Sprintf("transaction %d requires external id and description", i+1))
		}
		if err := validateDate(record.PostedDate, fmt.Sprintf("transaction %d posted date", i+1)); err != nil {
			return ImportResult{}, err
		}
		switch record.Disposition {
		case SourceDispositionPosted:
			if record.ExclusionReason != "" {
				return ImportResult{}, apperr.New(apperr.Input, "IMPORT_ROW_DISPOSITION_INVALID", fmt.Sprintf("transaction %d POSTED disposition cannot have an exclusion reason", i+1))
			}
			result.StatementTransactionCount++
		case SourceDispositionPending, SourceDispositionNeedsReview, SourceDispositionSourceOnly:
			if record.ExclusionReason == "" {
				return ImportResult{}, apperr.New(apperr.Input, "IMPORT_ROW_DISPOSITION_INVALID", fmt.Sprintf("transaction %d non-POSTED disposition requires an exclusion reason", i+1))
			}
			result.SourceOnlyCount++
		default:
			return ImportResult{}, apperr.New(apperr.Input, "IMPORT_ROW_DISPOSITION_INVALID", fmt.Sprintf("transaction %d disposition must be POSTED, PENDING, NEEDS_REVIEW, or SOURCE_ONLY", i+1))
		}
		raw := record.RawJSON
		if len(raw) == 0 {
			raw, err = json.Marshal(record)
			if err != nil {
				return ImportResult{}, err
			}
		}
		if !json.Valid(raw) {
			return ImportResult{}, apperr.New(apperr.Input, "IMPORT_ROW_INVALID", fmt.Sprintf("transaction %d raw_json is invalid", i+1))
		}
		candidate, err := prepareSourceObservation(sourceObservationInput{
			ImportBatchID: batchID, TransactionDate: record.PostedDate,
			Description: record.Description, AmountCents: record.AmountCents,
			TaxType: record.TaxType, TaxPeriod: record.TaxAccountingPeriod,
			Disposition: record.Disposition, ExclusionReason: record.ExclusionReason,
			RawJSON: raw, ResolutionReason: record.ResolutionReason,
			ResolutionJSON: record.ResolutionEvidence,
		})
		if err != nil {
			return ImportResult{}, apperr.Wrap(apperr.Input, "IMPORT_ROW_RESOLUTION_INVALID", fmt.Sprintf("transaction %d source observation is invalid", i+1), err)
		}
		sourceIdentityID, identityCreated, err := s.ensureSourceIdentity(ctx, tx, sourceIdentityInput{
			EntityID: entityID, BookID: bookID, MaterializationKind: sourceMaterializationStatement,
			StatementAccountID: statementAccountID,
			SourceSystem:       input.SourceSystem, SourceAccount: input.StatementAccount,
			ExternalID: record.ExternalID,
		})
		if err != nil {
			return ImportResult{}, err
		}
		current, currentExists, err := readCurrentSourceObservation(ctx, tx, sourceIdentityID)
		if err != nil {
			return ImportResult{}, err
		}
		if !identityCreated && !currentExists {
			return ImportResult{}, apperr.New(apperr.Integrity, "SOURCE_IDENTITY_WITHOUT_OBSERVATION", fmt.Sprintf("transaction %q has a source identity without an observation", record.ExternalID))
		}
		if currentExists && sourceObservationMatches(current, candidate) {
			result.SkippedCount++
			continue
		}
		if currentExists {
			if err := validateSourceTransition(current, candidate); err != nil {
				return ImportResult{}, err
			}
		} else if candidate.ObservationKind == sourceObservationResolution {
			return ImportResult{}, apperr.New(apperr.Input, "SOURCE_RESOLUTION_WITHOUT_PRIOR", fmt.Sprintf("transaction %q cannot be resolved before a provider observation exists", record.ExternalID))
		}
		var predecessor *currentSourceObservation
		if currentExists {
			predecessor = &current
		}
		sourceRecordID, _, err := s.insertSourceObservation(ctx, tx, sourceIdentityID, candidate, predecessor)
		if err != nil {
			return ImportResult{}, err
		}
		if record.Disposition != SourceDispositionPosted {
			continue
		}
		transactionID, err := storesqlite.NewID()
		if err != nil {
			return ImportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO statement_transactions
            (id, statement_account_id, source_identity_id, source_record_id, posted_date, description, amount_cents, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, transactionID, statementAccountID, sourceIdentityID, sourceRecordID,
			record.PostedDate, record.Description, record.AmountCents, storesqlite.UTCNow()); err != nil {
			return ImportResult{}, storesqlite.MapError("import statement transaction", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE import_batches SET status = 'COMPLETED', completed_at = ? WHERE id = ?`, storesqlite.UTCNow(), batchID); err != nil {
		return ImportResult{}, storesqlite.MapError("complete import batch", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement import", AggregateType: "import_batch", AggregateID: batchID,
		Payload: map[string]any{"statement_account": input.StatementAccount, "source_system": input.SourceSystem, "source_name": input.SourceName, "file_sha256": input.FileSHA256, "record_count": len(input.Transactions), "statement_transaction_count": result.StatementTransactionCount, "source_only_count": result.SourceOnlyCount, "skipped_count": result.SkippedCount},
	}); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

type manualReconciliationState struct {
	StatementAccountID string
	ReconciliationID   string
	Replanning         bool
	Applied            *Reconciliation
}

type manualControlLine struct {
	JournalLineID string
	PostedDate    string
	Description   string
	AmountCents   int64
}

// ValidateManualReconciliation performs the same state comparison as apply,
// without taking a write lock or creating accounting evidence.
func (s *Service) ValidateManualReconciliation(ctx context.Context, input ManualReconciliationInput) error {
	input, err := normalizeManualReconciliationInput(input)
	if err != nil {
		return err
	}
	_, err = validateManualReconciliationState(ctx, s.store.DB(), input)
	return err
}

// ApplyManualReconciliation imports the plan's synthetic statement evidence,
// creates and allocates the reconciliation, and completes it in one SQLite
// transaction. Any validation or balancing failure rolls back the whole apply.
func (s *Service) ApplyManualReconciliation(ctx context.Context, input ManualReconciliationInput) (Reconciliation, error) {
	if err := s.requireActor(); err != nil {
		return Reconciliation{}, err
	}
	input, err := normalizeManualReconciliationInput(input)
	if err != nil {
		return Reconciliation{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	state, err := validateManualReconciliationState(ctx, tx, input)
	if err != nil {
		return Reconciliation{}, err
	}
	if state.Applied != nil {
		return *state.Applied, nil
	}

	reconciliationID := state.ReconciliationID
	if !state.Replanning {
		reconciliationID, err = storesqlite.NewID()
		if err != nil {
			return Reconciliation{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliations
			(id, statement_account_id, start_date, end_date, beginning_balance_cents, ending_balance_cents, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, reconciliationID, state.StatementAccountID, input.StartDate, input.EndDate,
			input.BeginningBalanceCents, input.EndingBalanceCents, storesqlite.UTCNow(), s.actor); err != nil {
			return Reconciliation{}, storesqlite.MapError("start manual reconciliation", err)
		}
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "reconcile start", AggregateType: "reconciliation", AggregateID: reconciliationID,
			Payload: map[string]any{"statement_account": input.StatementAccount, "start_date": input.StartDate, "end_date": input.EndDate, "beginning_balance_cents": input.BeginningBalanceCents, "ending_balance_cents": input.EndingBalanceCents, "plan_digest": input.PlanDigest},
		}); err != nil {
			return Reconciliation{}, err
		}
	} else {
		var priorBeginning, priorEnding int64
		if err := tx.QueryRowContext(ctx, `SELECT beginning_balance_cents, ending_balance_cents
			FROM reconciliations WHERE id = ? AND status = 'OPEN'`, reconciliationID).Scan(&priorBeginning, &priorEnding); err != nil {
			return Reconciliation{}, storesqlite.MapError("read reopened reconciliation", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reconciliations
			SET beginning_balance_cents = ?, ending_balance_cents = ?
			WHERE id = ? AND status = 'OPEN'`, input.BeginningBalanceCents, input.EndingBalanceCents, reconciliationID); err != nil {
			return Reconciliation{}, storesqlite.MapError("revise reconciliation balances", err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, journal_line_id, outstanding_amount_cents
			FROM reconciliation_outstanding_items WHERE reconciliation_id = ? ORDER BY id`, reconciliationID)
		if err != nil {
			return Reconciliation{}, storesqlite.MapError("read replaced reconciliation outstanding items", err)
		}
		type replacedOutstanding struct {
			id, journalLineID string
			amount            int64
		}
		var replaced []replacedOutstanding
		for rows.Next() {
			var item replacedOutstanding
			if err := rows.Scan(&item.id, &item.journalLineID, &item.amount); err != nil {
				_ = rows.Close()
				return Reconciliation{}, err
			}
			replaced = append(replaced, item)
		}
		if err := rows.Close(); err != nil {
			return Reconciliation{}, err
		}
		for _, item := range replaced {
			if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
				Actor: s.actor, Command: "reconcile unmark-outstanding", AggregateType: "reconciliation_outstanding_item", AggregateID: item.id,
				Payload: map[string]any{"reconciliation_id": reconciliationID, "journal_line_id": item.journalLineID, "outstanding_amount_cents": item.amount, "plan_digest": input.PlanDigest},
			}); err != nil {
				return Reconciliation{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM reconciliation_outstanding_items WHERE reconciliation_id = ?`, reconciliationID); err != nil {
			return Reconciliation{}, storesqlite.MapError("replace reconciliation outstanding items", err)
		}
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "reconcile revise", AggregateType: "reconciliation", AggregateID: reconciliationID,
			Payload: map[string]any{
				"prior_beginning_balance_cents": priorBeginning, "beginning_balance_cents": input.BeginningBalanceCents,
				"prior_ending_balance_cents": priorEnding, "ending_balance_cents": input.EndingBalanceCents,
				"plan_digest": input.PlanDigest,
			},
		}); err != nil {
			return Reconciliation{}, err
		}
	}

	existingAllocations := make(map[string]int64)
	if state.Replanning {
		rows, err := tx.QueryContext(ctx, `SELECT journal_line_id, SUM(allocated_amount_cents)
			FROM reconciliation_allocations WHERE reconciliation_id = ? GROUP BY journal_line_id`, reconciliationID)
		if err != nil {
			return Reconciliation{}, storesqlite.MapError("read reopened reconciliation allocations", err)
		}
		for rows.Next() {
			var lineID string
			var amount int64
			if err := rows.Scan(&lineID, &amount); err != nil {
				_ = rows.Close()
				return Reconciliation{}, err
			}
			existingAllocations[lineID] = amount
		}
		if err := rows.Close(); err != nil {
			return Reconciliation{}, err
		}
	}

	selectedByID := make(map[string]ManualReconciliationLine, len(input.Lines))
	newLines := make([]ManualReconciliationLine, 0, len(input.Lines))
	for _, line := range input.Lines {
		selectedByID[line.JournalLineID] = line
		if amount, exists := existingAllocations[line.JournalLineID]; exists {
			if amount != line.AmountCents {
				return Reconciliation{}, apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_CONFLICT", "an existing cleared allocation no longer matches the reviewed line")
			}
			continue
		}
		newLines = append(newLines, line)
	}
	for lineID := range existingAllocations {
		if _, retained := selectedByID[lineID]; !retained {
			return Reconciliation{}, apperr.New(apperr.Validation, "RECONCILIATION_REPLAN_CLEARED_IMMUTABLE", "the short replan workflow cannot remove previously attested cleared activity")
		}
	}

	transactions := make([]StatementTransactionInput, 0, len(newLines))
	for _, line := range newLines {
		transactions = append(transactions, StatementTransactionInput{
			ExternalID: line.ExternalID, PostedDate: line.StatementDate, Description: line.Description,
			AmountCents: line.AmountCents, RawJSON: line.RawJSON,
		})
	}
	var importResult ImportResult
	if len(newLines) != 0 || !state.Replanning {
		importResult, err = s.importStatementTransactionsTx(ctx, tx, StatementImportInput{
			StatementAccount: input.StatementAccount,
			SourceSystem:     "MANUAL_RECONCILIATION",
			SourceName:       input.SourceName,
			FileSHA256:       input.PlanDigest,
			Transactions:     transactions,
		})
		if err != nil {
			return Reconciliation{}, err
		}
		if !importResult.Changed || importResult.StatementTransactionCount != len(newLines) {
			return Reconciliation{}, apperr.New(apperr.Conflict, "RECONCILIATION_PLAN_CONFLICT", "manual reconciliation evidence already exists or is incomplete")
		}
		attested, err := tx.ExecContext(ctx, `INSERT INTO source_record_operator_attestations
			(source_record_id, attested_at, attested_by)
			SELECT source_row.id, source_row.created_at, source_row.created_by
			FROM source_records source_row
			WHERE source_row.import_batch_id = ?`, importResult.BatchID)
		if err != nil {
			return Reconciliation{}, storesqlite.MapError("attest manual reconciliation evidence", err)
		}
		if affected, _ := attested.RowsAffected(); affected != int64(len(newLines)) {
			return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_EVIDENCE_MISSING", "manual reconciliation evidence was not fully marked as operator-attested")
		}
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "reconcile attest", AggregateType: "import_batch", AggregateID: importResult.BatchID,
			Payload: map[string]any{"reconciliation_id": reconciliationID, "operator_attestation_count": len(newLines), "plan_digest": input.PlanDigest},
		}); err != nil {
			return Reconciliation{}, err
		}
	}

	for _, line := range newLines {
		var statementTransactionID string
		err := tx.QueryRowContext(ctx, `SELECT st.id
			FROM statement_transactions st
			JOIN source_identities si ON si.id = st.source_identity_id
			JOIN source_records sr ON sr.id = st.source_record_id
			WHERE sr.import_batch_id = ? AND si.statement_account_id = ?
			  AND si.source_system = 'MANUAL_RECONCILIATION' AND si.external_id = ?`,
			importResult.BatchID, state.StatementAccountID, line.ExternalID).Scan(&statementTransactionID)
		if err == sql.ErrNoRows {
			return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_EVIDENCE_MISSING", fmt.Sprintf("manual statement evidence %q was not materialized", line.ExternalID))
		}
		if err != nil {
			return Reconciliation{}, storesqlite.MapError("read manual statement evidence", err)
		}
		allocationID, err := storesqlite.NewID()
		if err != nil {
			return Reconciliation{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_allocations
			(id, reconciliation_id, statement_transaction_id, journal_line_id, allocated_amount_cents, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, allocationID, reconciliationID, statementTransactionID,
			line.JournalLineID, line.AmountCents, storesqlite.UTCNow(), s.actor); err != nil {
			return Reconciliation{}, storesqlite.MapError("create manual reconciliation allocation", err)
		}
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "reconcile allocate", AggregateType: "reconciliation_allocation", AggregateID: allocationID,
			Payload: map[string]any{"reconciliation_id": reconciliationID, "statement_transaction_id": statementTransactionID, "journal_line_id": line.JournalLineID, "allocated_amount_cents": line.AmountCents, "plan_digest": input.PlanDigest},
		}); err != nil {
			return Reconciliation{}, err
		}
	}
	for _, line := range input.Outstanding {
		outstandingID, err := storesqlite.NewID()
		if err != nil {
			return Reconciliation{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_outstanding_items
			(id, reconciliation_id, journal_line_id, outstanding_amount_cents, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?)`, outstandingID, reconciliationID, line.JournalLineID,
			line.AmountCents, storesqlite.UTCNow(), s.actor); err != nil {
			return Reconciliation{}, storesqlite.MapError("record reconciliation outstanding item", err)
		}
		if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
			Actor: s.actor, Command: "reconcile mark-outstanding", AggregateType: "reconciliation_outstanding_item", AggregateID: outstandingID,
			Payload: map[string]any{"reconciliation_id": reconciliationID, "journal_line_id": line.JournalLineID, "outstanding_amount_cents": line.AmountCents, "plan_digest": input.PlanDigest},
		}); err != nil {
			return Reconciliation{}, err
		}
	}

	status, err := reconciliationStatus(ctx, tx, reconciliationID)
	if err != nil {
		return Reconciliation{}, err
	}
	if err := validateReconciliationCompletion(status); err != nil {
		return Reconciliation{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE reconciliations SET status = 'COMPLETED', completed_at = ?, completed_by = ?
		WHERE id = ? AND status = 'OPEN'`, storesqlite.UTCNow(), s.actor, reconciliationID)
	if err != nil {
		return Reconciliation{}, storesqlite.MapError("complete manual reconciliation", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Reconciliation{}, apperr.New(apperr.Conflict, "RECONCILIATION_NOT_OPEN", "reconciliation is no longer open")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile complete", AggregateType: "reconciliation", AggregateID: reconciliationID,
		Payload: map[string]any{"ending_balance_cents": status.EndingBalanceCents, "statement_transaction_count": status.StatementTransactionCount, "control_line_count": status.ControlLineCount, "outstanding_line_count": status.OutstandingLineCount, "allocation_count": status.AllocationCount, "plan_digest": input.PlanDigest},
	}); err != nil {
		return Reconciliation{}, err
	}
	status, err = reconciliationStatus(ctx, tx, reconciliationID)
	if err != nil {
		return Reconciliation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reconciliation{}, storesqlite.MapError("commit manual reconciliation", err)
	}
	return status, nil
}

func normalizeManualReconciliationInput(input ManualReconciliationInput) (ManualReconciliationInput, error) {
	input.StatementAccount = normalizeCode(input.StatementAccount)
	input.TargetReconciliationID = strings.TrimSpace(input.TargetReconciliationID)
	input.ExpectedTargetReopenedAt = strings.TrimSpace(input.ExpectedTargetReopenedAt)
	input.SourceName = strings.TrimSpace(input.SourceName)
	input.PlanDigest = strings.ToLower(strings.TrimSpace(input.PlanDigest))
	if input.StatementAccount == "" || input.SourceName == "" || len(input.PlanDigest) != 64 {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "statement account, source name, and plan SHA-256 are required")
	}
	if _, err := hex.DecodeString(input.PlanDigest); err != nil {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "plan SHA-256 is invalid")
	}
	if err := validateDate(input.StartDate, "reconciliation start date"); err != nil {
		return input, err
	}
	if err := validateDate(input.EndDate, "reconciliation end date"); err != nil {
		return input, err
	}
	if input.EndDate < input.StartDate {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_DATES_INVALID", "reconciliation end date precedes start date")
	}
	if input.ExpectedStatementTransactionCount < 0 || (input.TargetReconciliationID == "" && input.ExpectedStatementTransactionCount != 0) {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "a new manual reconciliation plan must start without statement evidence in its range")
	}
	if input.TargetReconciliationID != "" && input.ExpectedTargetReopenedAt == "" {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "a reconciliation replan must bind the exact reopen event")
	}
	if input.ExpectedLedgerBeginningCents-input.ExpectedOpeningOutstandingCents != input.BeginningBalanceCents ||
		input.ExpectedLedgerEndingCents-input.ExpectedEndingOutstandingCents != input.EndingBalanceCents {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "reviewed book balances must agree with statement balances after outstanding items")
	}
	if input.PriorReconciliation != nil {
		input.PriorReconciliation.ID = strings.TrimSpace(input.PriorReconciliation.ID)
		input.PriorReconciliation.Status = normalizeCode(input.PriorReconciliation.Status)
		if input.PriorReconciliation.ID == "" || input.PriorReconciliation.Status != "COMPLETED" {
			return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "the bound prior reconciliation must be completed")
		}
		if err := validateDate(input.PriorReconciliation.EndDate, "prior reconciliation end date"); err != nil {
			return input, err
		}
	}
	cleanLine := func(line *ManualReconciliationLine, index int, collection string, statementDateRequired bool) error {
		line.JournalLineID = strings.TrimSpace(line.JournalLineID)
		line.ExternalID = strings.TrimSpace(line.ExternalID)
		line.Description = strings.TrimSpace(line.Description)
		if line.JournalLineID == "" || line.ExternalID == "" || line.Description == "" || line.AmountCents == 0 {
			return apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", fmt.Sprintf("manual reconciliation %s line %d is incomplete", collection, index+1))
		}
		if err := validateDate(line.LedgerDate, fmt.Sprintf("manual reconciliation %s line %d ledger date", collection, index+1)); err != nil {
			return err
		}
		if line.LedgerDate > input.EndDate {
			return apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", fmt.Sprintf("manual reconciliation %s line %d is after the plan end", collection, index+1))
		}
		if statementDateRequired {
			if err := validateDate(line.StatementDate, fmt.Sprintf("manual reconciliation %s line %d statement date", collection, index+1)); err != nil {
				return err
			}
			if line.StatementDate < input.StartDate || line.StatementDate > input.EndDate {
				return apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", fmt.Sprintf("manual reconciliation %s line %d statement date falls outside the plan range", collection, index+1))
			}
		}
		if len(line.RawJSON) > 0 && !json.Valid(line.RawJSON) {
			return apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", fmt.Sprintf("manual reconciliation %s line %d raw evidence is invalid JSON", collection, index+1))
		}
		return nil
	}
	expected := make(map[string]ManualReconciliationLine, len(input.ExpectedLines))
	externalIDs := make(map[string]bool, len(input.ExpectedLines))
	for i := range input.ExpectedLines {
		line := &input.ExpectedLines[i]
		if err := cleanLine(line, i, "candidate", true); err != nil {
			return input, err
		}
		if _, duplicate := expected[line.JournalLineID]; duplicate || externalIDs[line.ExternalID] {
			return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "candidate journal-line and external identifiers must be unique")
		}
		expected[line.JournalLineID] = *line
		externalIDs[line.ExternalID] = true
	}
	partitioned := make(map[string]bool, len(input.ExpectedLines))
	matchingCandidate := func(line ManualReconciliationLine) bool {
		candidate, ok := expected[line.JournalLineID]
		return ok && candidate.ExternalID == line.ExternalID && candidate.LedgerDate == line.LedgerDate &&
			candidate.StatementDate == line.StatementDate && candidate.Description == line.Description && candidate.AmountCents == line.AmountCents
	}
	var activity int64
	for i := range input.Lines {
		line := &input.Lines[i]
		if err := cleanLine(line, i, "cleared", true); err != nil {
			return input, err
		}
		if partitioned[line.JournalLineID] || !matchingCandidate(*line) {
			return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "cleared lines must be a unique subset of the reviewed candidates")
		}
		partitioned[line.JournalLineID] = true
		activity += line.AmountCents
	}
	var outstanding int64
	for i := range input.Outstanding {
		line := &input.Outstanding[i]
		if err := cleanLine(line, i, "outstanding", true); err != nil {
			return input, err
		}
		if partitioned[line.JournalLineID] || !matchingCandidate(*line) {
			return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "outstanding lines must be the remaining unique reviewed candidates")
		}
		partitioned[line.JournalLineID] = true
		outstanding += line.AmountCents
	}
	if len(partitioned) != len(expected) {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "every reviewed candidate must be marked cleared or outstanding")
	}
	if input.BeginningBalanceCents+activity != input.EndingBalanceCents {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "manual statement activity does not reach the ending balance")
	}
	if outstanding != input.ExpectedEndingOutstandingCents {
		return input, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "reviewed outstanding items do not equal the adjusted ending balance")
	}
	return input, nil
}

func validateManualReconciliationState(ctx context.Context, q queryer, input ManualReconciliationInput) (manualReconciliationState, error) {
	var state manualReconciliationState
	var accountStatus string
	err := q.QueryRowContext(ctx, `SELECT id, status FROM statement_accounts WHERE code = ?`, input.StatementAccount).Scan(&state.StatementAccountID, &accountStatus)
	if err == sql.ErrNoRows {
		return state, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	}
	if err != nil {
		return state, storesqlite.MapError("read manual reconciliation account", err)
	}
	if accountStatus != "ACTIVE" {
		return state, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_NOT_ACTIVE", "cannot reconcile an archived statement account")
	}
	if input.TargetReconciliationID != "" {
		var targetAccountID, targetStatus, targetStart, targetEnd, reopenedAt string
		var targetBeginning, targetEnding int64
		err := q.QueryRowContext(ctx, `SELECT statement_account_id, status, start_date, end_date,
			beginning_balance_cents, ending_balance_cents, COALESCE(reopened_at, '')
			FROM reconciliations WHERE id = ?`, input.TargetReconciliationID).Scan(
			&targetAccountID, &targetStatus, &targetStart, &targetEnd, &targetBeginning, &targetEnding, &reopenedAt)
		if err == sql.ErrNoRows {
			return state, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "the reconciliation targeted by this replan was not found")
		}
		if err != nil {
			return state, storesqlite.MapError("read reconciliation replan target", err)
		}
		if targetAccountID != state.StatementAccountID || targetStart != input.StartDate || targetEnd != input.EndDate {
			return state, apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_CONFLICT", "the reopened reconciliation boundary no longer matches this plan")
		}
		state.ReconciliationID = input.TargetReconciliationID
		state.Replanning = true
		if reopenedAt != input.ExpectedTargetReopenedAt {
			return state, manualReconciliationPlanStale("the reconciliation reopen event changed")
		}
		if targetStatus == "COMPLETED" {
			status, err := verifyReplannedManualReconciliation(ctx, q, input, state.StatementAccountID, state.ReconciliationID)
			if err != nil {
				return state, err
			}
			state.Applied = &status
			return state, nil
		}
		if targetStatus != "OPEN" || reopenedAt == "" {
			return state, apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_TARGET_INVALID", "replan requires an explicitly reopened reconciliation")
		}
		if targetBeginning != input.ExpectedTargetBeginningBalanceCents {
			return state, manualReconciliationPlanStale("the reopened reconciliation beginning balance changed")
		}
		if targetEnding != input.ExpectedTargetEndingBalanceCents {
			return state, manualReconciliationPlanStale("the reopened reconciliation ending balance changed")
		}
		var nonManualEvidence int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
			FROM reconciliations reconciliation_row
			JOIN statement_transactions transaction_row
			  ON transaction_row.statement_account_id = reconciliation_row.statement_account_id
			 AND transaction_row.posted_date BETWEEN reconciliation_row.start_date AND reconciliation_row.end_date
			JOIN source_identities identity_row ON identity_row.id = transaction_row.source_identity_id
			WHERE reconciliation_row.id = ? AND identity_row.source_system <> 'MANUAL_RECONCILIATION'`, input.TargetReconciliationID).Scan(&nonManualEvidence); err != nil {
			return state, storesqlite.MapError("validate reconciliation replan source", err)
		}
		if nonManualEvidence != 0 {
			return state, apperr.New(apperr.Validation, "RECONCILIATION_REPLAN_SOURCE_UNSUPPORTED", "the short replan workflow only revises manual reconciliations")
		}
		if err := validateManualReconciliationSnapshot(ctx, q, input, state.StatementAccountID, state.ReconciliationID, input.ExpectedStatementTransactionCount); err != nil {
			return state, err
		}
		if err := validateReplanExistingSelections(ctx, q, input, state.ReconciliationID); err != nil {
			return state, err
		}
		return state, nil
	}

	var batchID string
	batchErr := q.QueryRowContext(ctx, `SELECT id FROM import_batches
		WHERE source_system = 'MANUAL_RECONCILIATION' AND file_sha256 = ? AND status = 'COMPLETED'`, input.PlanDigest).Scan(&batchID)
	if batchErr != nil && batchErr != sql.ErrNoRows {
		return state, storesqlite.MapError("read manual reconciliation batch", batchErr)
	}
	var reconciliationID, reconciliationStatusValue string
	var reconciliationBeginning, reconciliationEnding int64
	reconciliationErr := q.QueryRowContext(ctx, `SELECT id, status, beginning_balance_cents, ending_balance_cents
		FROM reconciliations WHERE statement_account_id = ? AND start_date = ? AND end_date = ? AND status <> 'ABANDONED'`,
		state.StatementAccountID, input.StartDate, input.EndDate).Scan(
		&reconciliationID, &reconciliationStatusValue, &reconciliationBeginning, &reconciliationEnding)
	if reconciliationErr != nil && reconciliationErr != sql.ErrNoRows {
		return state, storesqlite.MapError("read existing manual reconciliation", reconciliationErr)
	}

	batchExists := batchErr == nil
	reconciliationExists := reconciliationErr == nil
	if batchExists || reconciliationExists {
		if !batchExists || !reconciliationExists || reconciliationStatusValue != "COMPLETED" ||
			reconciliationBeginning != input.BeginningBalanceCents || reconciliationEnding != input.EndingBalanceCents {
			return state, apperr.New(apperr.Conflict, "RECONCILIATION_PLAN_CONFLICT", "existing statement evidence or reconciliation does not exactly match this plan")
		}
		status, err := verifyAppliedManualReconciliation(ctx, q, input, state.StatementAccountID, batchID, reconciliationID)
		if err != nil {
			return state, err
		}
		state.Applied = &status
		state.ReconciliationID = reconciliationID
		return state, nil
	}

	if err := validateManualReconciliationSnapshot(ctx, q, input, state.StatementAccountID, "", input.ExpectedStatementTransactionCount); err != nil {
		return state, err
	}
	return state, nil
}

func validateReplanExistingSelections(ctx context.Context, q queryer, input ManualReconciliationInput, reconciliationID string) error {
	selected := make(map[string]int64, len(input.Lines))
	for _, line := range input.Lines {
		selected[line.JournalLineID] = line.AmountCents
	}
	rows, err := q.QueryContext(ctx, `SELECT journal_line_id, SUM(allocated_amount_cents)
		FROM reconciliation_allocations WHERE reconciliation_id = ? GROUP BY journal_line_id`, reconciliationID)
	if err != nil {
		return storesqlite.MapError("read reconciliation replan allocations", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var lineID string
		var amount int64
		if err := rows.Scan(&lineID, &amount); err != nil {
			return err
		}
		selectedAmount, ok := selected[lineID]
		if !ok {
			return apperr.New(apperr.Validation, "RECONCILIATION_REPLAN_CLEARED_IMMUTABLE", "the short replan workflow cannot remove previously attested cleared activity")
		}
		if selectedAmount != amount {
			return apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_CONFLICT", "a previously cleared allocation no longer matches the reviewed plan")
		}
	}
	return rows.Err()
}

func validateManualReconciliationSnapshot(ctx context.Context, q queryer, input ManualReconciliationInput, statementAccountID, excludedReconciliationID string, expectedStatementCount int) error {
	var overlapCount int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliations
		WHERE statement_account_id = ? AND status <> 'ABANDONED' AND id <> ?
		  AND end_date >= ? AND start_date <= ?`, statementAccountID, excludedReconciliationID, input.StartDate, input.EndDate).Scan(&overlapCount); err != nil {
		return storesqlite.MapError("check manual reconciliation overlap", err)
	}
	if overlapCount != 0 {
		return manualReconciliationPlanStale("the reconciliation boundary changed")
	}

	prior, err := latestManualReconciliationPrior(ctx, q, statementAccountID, excludedReconciliationID, input.StartDate)
	if err != nil {
		return err
	}
	if !manualReconciliationPriorMatches(prior, input.PriorReconciliation) {
		return manualReconciliationPlanStale("the prior reconciliation changed")
	}

	lines, err := currentManualControlLines(ctx, q, statementAccountID, input.StartDate, input.EndDate, excludedReconciliationID)
	if err != nil {
		return err
	}
	if !manualControlLinesMatch(lines, input.ExpectedLines) {
		return manualReconciliationPlanStale("posted control-account activity changed")
	}

	var ledgerBeginning, ledgerEnding int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.id = ? AND je.posting_date < ?`, statementAccountID, input.StartDate).Scan(&ledgerBeginning); err != nil {
		return storesqlite.MapError("read manual reconciliation beginning", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.id = ? AND je.posting_date <= ?`, statementAccountID, input.EndDate).Scan(&ledgerEnding); err != nil {
		return storesqlite.MapError("read manual reconciliation ending", err)
	}
	if ledgerBeginning != input.ExpectedLedgerBeginningCents || ledgerEnding != input.ExpectedLedgerEndingCents {
		return manualReconciliationPlanStale("book boundary balances changed")
	}
	var openingOutstanding int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(item.outstanding_amount_cents), 0)
		FROM reconciliations prior
		JOIN reconciliation_outstanding_items item ON item.reconciliation_id = prior.id
		WHERE prior.statement_account_id = ? AND prior.status = 'COMPLETED' AND prior.end_date < ?
		  AND prior.end_date = (
			SELECT MAX(candidate.end_date) FROM reconciliations candidate
			WHERE candidate.statement_account_id = ? AND candidate.status = 'COMPLETED' AND candidate.end_date < ?
		  )`, statementAccountID, input.StartDate, statementAccountID, input.StartDate).Scan(&openingOutstanding); err != nil {
		return storesqlite.MapError("read prior reconciliation outstanding balance", err)
	}
	if openingOutstanding != input.ExpectedOpeningOutstandingCents {
		return manualReconciliationPlanStale("the opening outstanding balance changed")
	}

	var statementCount int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM statement_transactions
		WHERE statement_account_id = ? AND posted_date BETWEEN ? AND ?`, statementAccountID, input.StartDate, input.EndDate).Scan(&statementCount); err != nil {
		return storesqlite.MapError("read manual statement activity", err)
	}
	if statementCount != expectedStatementCount {
		return manualReconciliationPlanStale("statement activity changed")
	}
	return nil
}

func latestManualReconciliationPrior(ctx context.Context, q queryer, statementAccountID, excludedReconciliationID, beforeStart string) (*ManualReconciliationPrior, error) {
	var result ManualReconciliationPrior
	err := q.QueryRowContext(ctx, `SELECT id, status, end_date, ending_balance_cents
		FROM reconciliations WHERE statement_account_id = ? AND status <> 'ABANDONED' AND id <> ?
		  AND end_date < ?
		ORDER BY end_date DESC LIMIT 1`, statementAccountID, excludedReconciliationID, beforeStart).Scan(
		&result.ID, &result.Status, &result.EndDate, &result.EndingBalanceCents)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, storesqlite.MapError("read prior reconciliation boundary", err)
	}
	return &result, nil
}

func manualReconciliationPriorMatches(actual, expected *ManualReconciliationPrior) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return actual.ID == expected.ID && actual.Status == expected.Status && actual.EndDate == expected.EndDate &&
		actual.EndingBalanceCents == expected.EndingBalanceCents
}

func currentManualControlLines(ctx context.Context, q queryer, statementAccountID, startDate, endDate, excludedReconciliationID string) ([]manualControlLine, error) {
	rows, err := q.QueryContext(ctx, `WITH candidates AS (
		SELECT jl.id, je.posting_date,
			COALESCE(NULLIF(jl.description, ''), je.description) AS description,
			(jl.debit_cents - jl.credit_cents) - COALESCE((
				SELECT SUM(allocation.allocated_amount_cents)
				FROM reconciliation_allocations allocation
				JOIN reconciliations allocated ON allocated.id = allocation.reconciliation_id
				WHERE allocation.journal_line_id = jl.id AND allocated.status <> 'ABANDONED'
				  AND allocated.id <> ? AND allocated.end_date <= ?
			), 0) AS remaining_cents,
			je.entry_number, jl.line_number
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.id = ?
		  AND je.posting_date BETWEEN MIN(sa.reconciliation_required_from, ?) AND ?
	)
	SELECT id, posting_date, description, remaining_cents FROM candidates
	WHERE remaining_cents <> 0 ORDER BY posting_date, entry_number, line_number`,
		excludedReconciliationID, endDate, statementAccountID, startDate, endDate)
	if err != nil {
		return nil, storesqlite.MapError("read manual reconciliation control lines", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []manualControlLine
	for rows.Next() {
		var line manualControlLine
		if err := rows.Scan(&line.JournalLineID, &line.PostedDate, &line.Description, &line.AmountCents); err != nil {
			return nil, err
		}
		result = append(result, line)
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("read manual reconciliation control lines", err)
	}
	return result, nil
}

func manualControlLinesMatch(actual []manualControlLine, expected []ManualReconciliationLine) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i].JournalLineID != expected[i].JournalLineID || actual[i].PostedDate != expected[i].LedgerDate ||
			actual[i].Description != expected[i].Description || actual[i].AmountCents != expected[i].AmountCents {
			return false
		}
	}
	return true
}

func verifyAppliedManualReconciliation(ctx context.Context, q queryer, input ManualReconciliationInput, statementAccountID, batchID, reconciliationID string) (Reconciliation, error) {
	if err := validateManualReconciliationSnapshot(ctx, q, input, statementAccountID, reconciliationID, len(input.Lines)); err != nil {
		return Reconciliation{}, err
	}
	status, err := reconciliationStatus(ctx, q, reconciliationID)
	if err != nil {
		return Reconciliation{}, err
	}
	if status.Status != "COMPLETED" || status.StartDate != input.StartDate || status.EndDate != input.EndDate ||
		status.BeginningBalanceCents != input.BeginningBalanceCents || status.EndingBalanceCents != input.EndingBalanceCents ||
		status.BeginningDifferenceCents != 0 || status.StatementDifferenceCents != 0 || status.LedgerDifferenceCents != 0 ||
		status.OpeningOutstandingCents != input.ExpectedOpeningOutstandingCents ||
		status.EndingOutstandingCents != input.ExpectedEndingOutstandingCents ||
		status.OutstandingLineCount != len(input.Outstanding) || status.OutstandingMismatchCount != 0 ||
		status.StatementTransactionCount != len(input.Lines) || status.FullyAllocatedStatementCount != len(input.Lines) ||
		status.ControlLineCount != len(input.Lines) || status.FullyAllocatedControlLineCount != len(input.Lines) || status.AllocationCount != len(input.Lines) {
		return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", "completed reconciliation no longer exactly matches the manual plan")
	}

	type appliedLine struct {
		TransactionID string
		PostedDate    string
		Description   string
		AmountCents   int64
	}
	applied := make(map[string]appliedLine, len(input.Lines))
	rows, err := q.QueryContext(ctx, `SELECT si.external_id, st.id, st.posted_date, st.description, st.amount_cents
		FROM source_records sr
		JOIN source_identities si ON si.id = sr.source_identity_id
		JOIN statement_transactions st ON st.source_identity_id = si.id AND st.source_record_id = sr.id
		WHERE sr.import_batch_id = ? AND si.statement_account_id = ? AND si.source_system = 'MANUAL_RECONCILIATION'`,
		batchID, statementAccountID)
	if err != nil {
		return Reconciliation{}, storesqlite.MapError("read applied manual evidence", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var externalID string
		var line appliedLine
		if err := rows.Scan(&externalID, &line.TransactionID, &line.PostedDate, &line.Description, &line.AmountCents); err != nil {
			return Reconciliation{}, err
		}
		if _, duplicate := applied[externalID]; duplicate {
			return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", "manual plan has duplicate materialized evidence")
		}
		applied[externalID] = line
	}
	if err := rows.Err(); err != nil {
		return Reconciliation{}, storesqlite.MapError("read applied manual evidence", err)
	}
	if len(applied) != len(input.Lines) {
		return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", "manual plan evidence count changed")
	}
	for _, expected := range input.Lines {
		actual, ok := applied[expected.ExternalID]
		if !ok || actual.PostedDate != expected.StatementDate || actual.Description != expected.Description || actual.AmountCents != expected.AmountCents {
			return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", fmt.Sprintf("manual plan evidence %q changed", expected.ExternalID))
		}
		var allocationCount int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliation_allocations
			WHERE reconciliation_id = ? AND statement_transaction_id = ? AND journal_line_id = ? AND allocated_amount_cents = ?`,
			reconciliationID, actual.TransactionID, expected.JournalLineID, expected.AmountCents).Scan(&allocationCount); err != nil {
			return Reconciliation{}, storesqlite.MapError("verify manual reconciliation allocation", err)
		}
		if allocationCount != 1 {
			return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", fmt.Sprintf("manual allocation for %q changed", expected.ExternalID))
		}
	}
	if err := verifyManualOutstandingItems(ctx, q, reconciliationID, input.Outstanding); err != nil {
		return Reconciliation{}, err
	}
	return status, nil
}

func verifyReplannedManualReconciliation(ctx context.Context, q queryer, input ManualReconciliationInput, statementAccountID, reconciliationID string) (Reconciliation, error) {
	if err := validateManualReconciliationSnapshot(ctx, q, input, statementAccountID, reconciliationID, len(input.Lines)); err != nil {
		return Reconciliation{}, err
	}
	status, err := reconciliationStatus(ctx, q, reconciliationID)
	if err != nil {
		return Reconciliation{}, err
	}
	if status.Status != "COMPLETED" || status.StartDate != input.StartDate || status.EndDate != input.EndDate ||
		status.BeginningBalanceCents != input.BeginningBalanceCents || status.EndingBalanceCents != input.EndingBalanceCents ||
		status.BeginningDifferenceCents != 0 || status.StatementDifferenceCents != 0 || status.LedgerDifferenceCents != 0 ||
		status.OpeningOutstandingCents != input.ExpectedOpeningOutstandingCents ||
		status.EndingOutstandingCents != input.ExpectedEndingOutstandingCents ||
		status.OutstandingLineCount != len(input.Outstanding) || status.OutstandingMismatchCount != 0 ||
		status.StatementTransactionCount != len(input.Lines) || status.FullyAllocatedStatementCount != len(input.Lines) ||
		status.ControlLineCount != len(input.Lines) || status.FullyAllocatedControlLineCount != len(input.Lines) || status.AllocationCount != len(input.Lines) {
		return Reconciliation{}, apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", "replanned reconciliation no longer exactly matches the reviewed plan")
	}
	if err := validateReplanExistingSelections(ctx, q, input, reconciliationID); err != nil {
		return Reconciliation{}, err
	}
	if err := verifyManualOutstandingItems(ctx, q, reconciliationID, input.Outstanding); err != nil {
		return Reconciliation{}, err
	}
	return status, nil
}

func verifyManualOutstandingItems(ctx context.Context, q queryer, reconciliationID string, expected []ManualReconciliationLine) error {
	actual := make(map[string]int64, len(expected))
	rows, err := q.QueryContext(ctx, `SELECT journal_line_id, outstanding_amount_cents
		FROM reconciliation_outstanding_items WHERE reconciliation_id = ?`, reconciliationID)
	if err != nil {
		return storesqlite.MapError("read manual reconciliation outstanding items", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var lineID string
		var amount int64
		if err := rows.Scan(&lineID, &amount); err != nil {
			return err
		}
		actual[lineID] = amount
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", "reviewed outstanding-item count changed")
	}
	for _, line := range expected {
		if actual[line.JournalLineID] != line.AmountCents {
			return apperr.New(apperr.Integrity, "RECONCILIATION_PLAN_EVIDENCE_MISMATCH", fmt.Sprintf("outstanding item %q changed", line.ExternalID))
		}
	}
	return nil
}

func manualReconciliationPlanStale(detail string) error {
	return apperr.New(apperr.Conflict, "RECONCILIATION_PLAN_STALE", detail+"; generate and review a new plan")
}

func (s *Service) StartReconciliation(ctx context.Context, statementAccount, startDate, endDate string, beginning, ending int64) (Reconciliation, error) {
	if err := s.requireActor(); err != nil {
		return Reconciliation{}, err
	}
	if err := validateDate(startDate, "start date"); err != nil {
		return Reconciliation{}, err
	}
	if err := validateDate(endDate, "end date"); err != nil {
		return Reconciliation{}, err
	}
	if endDate < startDate {
		return Reconciliation{}, apperr.New(apperr.Invalid, "RECONCILIATION_DATES_INVALID", "reconciliation end date precedes start date")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var accountID, statementStatus string
	if err := tx.QueryRowContext(ctx, "SELECT id, status FROM statement_accounts WHERE code = ?", normalizeCode(statementAccount)).Scan(&accountID, &statementStatus); err == sql.ErrNoRows {
		return Reconciliation{}, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	} else if err != nil {
		return Reconciliation{}, err
	}
	if statementStatus != "ACTIVE" {
		return Reconciliation{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_NOT_ACTIVE", "cannot start a reconciliation for an archived statement account")
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return Reconciliation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliations
        (id, statement_account_id, start_date, end_date, beginning_balance_cents, ending_balance_cents, created_at, created_by)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, accountID, startDate, endDate, beginning, ending, storesqlite.UTCNow(), s.actor); err != nil {
		return Reconciliation{}, storesqlite.MapError("start reconciliation", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile start", AggregateType: "reconciliation", AggregateID: id,
		Payload: map[string]any{"statement_account": normalizeCode(statementAccount), "start_date": startDate, "end_date": endDate, "beginning_balance_cents": beginning, "ending_balance_cents": ending},
	}); err != nil {
		return Reconciliation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reconciliation{}, err
	}
	return s.ReconciliationStatus(ctx, id)
}

func (s *Service) AllocateReconciliation(ctx context.Context, reconciliationID, transactionID, journalLineID string, amountCents int64) (string, error) {
	if err := s.requireActor(); err != nil {
		return "", err
	}
	if amountCents == 0 {
		return "", apperr.New(apperr.Invalid, "ALLOCATION_AMOUNT_INVALID", "reconciliation allocation must be nonzero")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	id, err := storesqlite.NewID()
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_allocations
        (id, reconciliation_id, statement_transaction_id, journal_line_id, allocated_amount_cents, created_at, created_by)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, id, reconciliationID, transactionID, journalLineID, amountCents, storesqlite.UTCNow(), s.actor); err != nil {
		return "", storesqlite.MapError("create reconciliation allocation", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile allocate", AggregateType: "reconciliation_allocation", AggregateID: id,
		Payload: map[string]any{"reconciliation_id": reconciliationID, "statement_transaction_id": transactionID, "journal_line_id": journalLineID, "allocated_amount_cents": amountCents},
	}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Service) RemoveReconciliationAllocation(ctx context.Context, allocationID string) error {
	if err := s.requireActor(); err != nil {
		return err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var reconciliationID string
	if err := tx.QueryRowContext(ctx, "SELECT reconciliation_id FROM reconciliation_allocations WHERE id = ?", allocationID).Scan(&reconciliationID); err == sql.ErrNoRows {
		return apperr.New(apperr.NotFound, "RECONCILIATION_ALLOCATION_NOT_FOUND", "reconciliation allocation was not found")
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM reconciliation_allocations WHERE id = ?", allocationID); err != nil {
		return storesqlite.MapError("remove reconciliation allocation", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile unallocate", AggregateType: "reconciliation_allocation", AggregateID: allocationID,
		Payload: map[string]any{"reconciliation_id": reconciliationID},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListReconciliationAllocations(ctx context.Context, reconciliationID string) ([]ReconciliationAllocation, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT ri.id, ri.reconciliation_id,
	        st.id, si.external_id, st.posted_date, jl.id, je.id, je.entry_number,
	        jl.line_number, ri.allocated_amount_cents
	        FROM reconciliation_allocations ri
	        JOIN statement_transactions st ON st.id = ri.statement_transaction_id
	        JOIN source_identities si ON si.id = st.source_identity_id
        JOIN journal_lines jl ON jl.id = ri.journal_line_id
        JOIN journal_entries je ON je.id = jl.journal_entry_id
        WHERE ri.reconciliation_id = ?
	        ORDER BY st.posted_date, si.external_id, je.entry_number, jl.line_number`, reconciliationID)
	if err != nil {
		return nil, storesqlite.MapError("list reconciliation allocations", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []ReconciliationAllocation
	for rows.Next() {
		var allocation ReconciliationAllocation
		if err := rows.Scan(&allocation.ID, &allocation.ReconciliationID,
			&allocation.StatementTransactionID, &allocation.StatementExternalID,
			&allocation.StatementPostedDate, &allocation.JournalLineID,
			&allocation.JournalID, &allocation.JournalEntryNumber,
			&allocation.JournalLineNumber, &allocation.AllocatedAmountCents); err != nil {
			return nil, err
		}
		result = append(result, allocation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		var exists int
		if err := s.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliations WHERE id = ?`, reconciliationID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "reconciliation was not found")
		}
	}
	return result, nil
}

func (s *Service) ListReconciliations(ctx context.Context, filter ReconciliationFilter) ([]Reconciliation, error) {
	filter.StatementAccount = normalizeCode(filter.StatementAccount)
	filter.Status = normalizeCode(filter.Status)
	if filter.Status != "" && filter.Status != "OPEN" && filter.Status != "COMPLETED" && filter.Status != "ABANDONED" {
		return nil, apperr.New(apperr.Invalid, "RECONCILIATION_STATUS_INVALID", "status must be OPEN, COMPLETED, or ABANDONED")
	}
	if filter.FromDate != "" {
		if err := validateDate(filter.FromDate, "reconciliation from date"); err != nil {
			return nil, err
		}
	}
	if filter.ToDate != "" {
		if err := validateDate(filter.ToDate, "reconciliation to date"); err != nil {
			return nil, err
		}
	}
	if filter.FromDate != "" && filter.ToDate != "" && filter.ToDate < filter.FromDate {
		return nil, apperr.New(apperr.Invalid, "RECONCILIATION_DATE_RANGE_INVALID", "reconciliation to date precedes from date")
	}
	query := `SELECT r.id, sa.code, r.start_date, r.end_date,
        r.beginning_balance_cents, status.statement_activity_cents, r.ending_balance_cents,
		status.book_beginning_balance_cents, status.book_ending_balance_cents,
		status.opening_outstanding_cents, status.ending_outstanding_cents,
		status.outstanding_line_count, status.outstanding_mismatch_count,
		status.statement_transaction_count, status.fully_allocated_statement_count,
        status.control_line_count, status.fully_allocated_control_line_count,
		status.allocation_count, r.status,
		COALESCE(r.abandoned_at, ''), COALESCE(r.abandoned_by, ''), COALESCE(r.abandon_reason, '')
        FROM reconciliations r
        JOIN statement_accounts sa ON sa.id = r.statement_account_id
        JOIN reconciliation_status status ON status.reconciliation_id = r.id
        WHERE 1=1`
	var args []any
	if filter.StatementAccount != "" {
		query += " AND sa.code = ?"
		args = append(args, filter.StatementAccount)
	}
	if filter.Status != "" {
		query += " AND r.status = ?"
		args = append(args, filter.Status)
	}
	if filter.FromDate != "" {
		query += " AND r.start_date >= ?"
		args = append(args, filter.FromDate)
	}
	if filter.ToDate != "" {
		query += " AND r.end_date <= ?"
		args = append(args, filter.ToDate)
	}
	query += " ORDER BY r.start_date, sa.code, r.id"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list reconciliations", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []Reconciliation
	for rows.Next() {
		var value Reconciliation
		if err := rows.Scan(&value.ID, &value.StatementAccountCode, &value.StartDate, &value.EndDate,
			&value.BeginningBalanceCents, &value.StatementActivityCents, &value.EndingBalanceCents,
			&value.LedgerBeginningCents, &value.LedgerEndingCents,
			&value.OpeningOutstandingCents, &value.EndingOutstandingCents,
			&value.OutstandingLineCount, &value.OutstandingMismatchCount,
			&value.StatementTransactionCount, &value.FullyAllocatedStatementCount,
			&value.ControlLineCount, &value.FullyAllocatedControlLineCount,
			&value.AllocationCount, &value.Status,
			&value.AbandonedAt, &value.AbandonedBy, &value.AbandonReason); err != nil {
			return nil, err
		}
		value.CalculatedEndingCents = value.BeginningBalanceCents + value.StatementActivityCents
		value.BeginningDifferenceCents = value.LedgerBeginningCents - value.OpeningOutstandingCents - value.BeginningBalanceCents
		value.StatementDifferenceCents = value.CalculatedEndingCents - value.EndingBalanceCents
		value.LedgerDifferenceCents = value.LedgerEndingCents - value.EndingOutstandingCents - value.EndingBalanceCents
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Service) ReconciliationStatus(ctx context.Context, id string) (Reconciliation, error) {
	return reconciliationStatus(ctx, s.store.DB(), id)
}

func reconciliationStatus(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Reconciliation, error) {
	var result Reconciliation
	err := q.QueryRowContext(ctx, `SELECT r.id, sa.code, r.start_date, r.end_date,
        r.beginning_balance_cents, status.statement_activity_cents, r.ending_balance_cents,
		status.book_beginning_balance_cents, status.book_ending_balance_cents,
		status.opening_outstanding_cents, status.ending_outstanding_cents,
		status.outstanding_line_count, status.outstanding_mismatch_count,
		status.statement_transaction_count, status.fully_allocated_statement_count,
        status.control_line_count, status.fully_allocated_control_line_count,
		status.allocation_count, r.status,
		COALESCE(r.abandoned_at, ''), COALESCE(r.abandoned_by, ''), COALESCE(r.abandon_reason, '')
        FROM reconciliations r
        JOIN statement_accounts sa ON sa.id = r.statement_account_id
        JOIN reconciliation_status status ON status.reconciliation_id = r.id
        WHERE r.id = ?`, id).Scan(
		&result.ID, &result.StatementAccountCode, &result.StartDate, &result.EndDate,
		&result.BeginningBalanceCents, &result.StatementActivityCents, &result.EndingBalanceCents,
		&result.LedgerBeginningCents, &result.LedgerEndingCents,
		&result.OpeningOutstandingCents, &result.EndingOutstandingCents,
		&result.OutstandingLineCount, &result.OutstandingMismatchCount,
		&result.StatementTransactionCount, &result.FullyAllocatedStatementCount,
		&result.ControlLineCount, &result.FullyAllocatedControlLineCount,
		&result.AllocationCount, &result.Status,
		&result.AbandonedAt, &result.AbandonedBy, &result.AbandonReason)
	if err == sql.ErrNoRows {
		return result, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "reconciliation was not found")
	}
	if err != nil {
		return result, storesqlite.MapError("read reconciliation", err)
	}
	result.CalculatedEndingCents = result.BeginningBalanceCents + result.StatementActivityCents
	result.BeginningDifferenceCents = result.LedgerBeginningCents - result.OpeningOutstandingCents - result.BeginningBalanceCents
	result.StatementDifferenceCents = result.CalculatedEndingCents - result.EndingBalanceCents
	result.LedgerDifferenceCents = result.LedgerEndingCents - result.EndingOutstandingCents - result.EndingBalanceCents
	return result, nil
}

func (s *Service) CompleteReconciliation(ctx context.Context, id string, dryRun bool) (Reconciliation, error) {
	if err := s.requireActor(); err != nil {
		return Reconciliation{}, err
	}
	if dryRun {
		status, err := s.ReconciliationStatus(ctx, id)
		if err != nil {
			return status, err
		}
		if err := validateReconciliationCompletion(status); err != nil {
			return status, err
		}
		return status, nil
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	status, err := reconciliationStatus(ctx, tx, id)
	if err != nil {
		return status, err
	}
	if err := validateReconciliationCompletion(status); err != nil {
		return status, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE reconciliations SET status = 'COMPLETED', completed_at = ?, completed_by = ?
		WHERE id = ? AND status = 'OPEN'`, storesqlite.UTCNow(), s.actor, id)
	if err != nil {
		return status, storesqlite.MapError("complete reconciliation", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return status, apperr.New(apperr.Conflict, "RECONCILIATION_NOT_OPEN", "reconciliation is no longer open")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile complete", AggregateType: "reconciliation", AggregateID: id,
		Payload: map[string]any{"ending_balance_cents": status.EndingBalanceCents, "statement_transaction_count": status.StatementTransactionCount, "control_line_count": status.ControlLineCount, "allocation_count": status.AllocationCount},
	}); err != nil {
		return status, err
	}
	if err := tx.Commit(); err != nil {
		return status, err
	}
	return s.ReconciliationStatus(ctx, id)
}

func validateReconciliationCompletion(status Reconciliation) error {
	if status.Status != "OPEN" {
		return apperr.New(apperr.Conflict, "RECONCILIATION_NOT_OPEN", "reconciliation is not open")
	}
	if status.BeginningDifferenceCents != 0 || status.StatementDifferenceCents != 0 || status.LedgerDifferenceCents != 0 ||
		status.OutstandingMismatchCount != 0 ||
		status.FullyAllocatedStatementCount != status.StatementTransactionCount ||
		status.FullyAllocatedControlLineCount != status.ControlLineCount {
		return apperr.New(apperr.Validation, "RECONCILIATION_NOT_BALANCED", "reconciliation has incomplete allocations, unreviewed outstanding items, or nonzero adjusted beginning, statement, or ledger differences")
	}
	return nil
}

func (s *Service) AbandonReconciliation(ctx context.Context, id, reason string, dryRun bool) (Reconciliation, error) {
	if err := s.requireActor(); err != nil {
		return Reconciliation{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Reconciliation{}, apperr.New(apperr.Invalid, "ABANDON_REASON_REQUIRED", "a reason is required to abandon a reconciliation")
	}
	status, err := s.ReconciliationStatus(ctx, id)
	if err != nil {
		return status, err
	}
	if status.Status != "OPEN" {
		return status, apperr.New(apperr.Conflict, "RECONCILIATION_NOT_OPEN", "only an open reconciliation can be abandoned")
	}
	if dryRun {
		if err := validateNoLaterReconciliation(ctx, s.store.DB(), id, status.EndDate); err != nil {
			return status, err
		}
		status.Status = "ABANDONED"
		status.AbandonedBy = s.actor
		status.AbandonReason = reason
		return status, nil
	}

	tx, err := s.store.Begin(ctx)
	if err != nil {
		return status, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if err := validateNoLaterReconciliation(ctx, tx, id, status.EndDate); err != nil {
		return status, err
	}
	abandonedAt := storesqlite.UTCNow()
	result, err := tx.ExecContext(ctx, `UPDATE reconciliations
		SET status = 'ABANDONED', abandoned_at = ?, abandoned_by = ?, abandon_reason = ?
		WHERE id = ? AND status = 'OPEN'`, abandonedAt, s.actor, reason, id)
	if err != nil {
		return status, storesqlite.MapError("abandon reconciliation", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return status, apperr.New(apperr.Conflict, "RECONCILIATION_NOT_OPEN", "only an open reconciliation can be abandoned")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile abandon", AggregateType: "reconciliation", AggregateID: id,
		Payload: map[string]any{
			"reason":                  reason,
			"statement_account":       status.StatementAccountCode,
			"start_date":              status.StartDate,
			"end_date":                status.EndDate,
			"beginning_balance_cents": status.BeginningBalanceCents,
			"ending_balance_cents":    status.EndingBalanceCents,
		},
	}); err != nil {
		return status, err
	}
	if err := tx.Commit(); err != nil {
		return status, storesqlite.MapError("commit reconciliation abandonment", err)
	}
	return s.ReconciliationStatus(ctx, id)
}

func validateNoLaterReconciliation(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id, endDate string) error {
	var later int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM reconciliations current
		JOIN reconciliations later ON later.statement_account_id = current.statement_account_id
		WHERE current.id = ? AND later.id <> current.id
		  AND later.status <> 'ABANDONED' AND later.end_date > ?`, id, endDate).Scan(&later); err != nil {
		return storesqlite.MapError("check later reconciliation work", err)
	}
	if later != 0 {
		return apperr.New(apperr.Conflict, "RECONCILIATION_HAS_LATER_WORK", "later reconciliation work must be abandoned first")
	}
	return nil
}

func (s *Service) ReopenReconciliation(ctx context.Context, id, reason string) error {
	if err := s.requireActor(); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apperr.New(apperr.Invalid, "REOPEN_REASON_REQUIRED", "a reason is required to reopen a reconciliation")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var precoverageClosed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM reconciliations reconciliation
		JOIN statement_account_precoverage_closures closure
		  ON closure.statement_account_id = reconciliation.statement_account_id
		WHERE reconciliation.id = ? AND reconciliation.status = 'COMPLETED'`, id).Scan(&precoverageClosed); err != nil {
		return storesqlite.MapError("check reconciliation precoverage lifecycle", err)
	}
	if precoverageClosed != 0 {
		return apperr.New(apperr.Conflict, "RECONCILIATION_PRECOVERAGE_CLOSED", "precoverage closure is terminal; its completed reconciliations cannot be reopened")
	}
	var laterCompleted int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM reconciliations current
		JOIN reconciliations later ON later.statement_account_id = current.statement_account_id
		WHERE current.id = ? AND current.status = 'COMPLETED'
		  AND later.id <> current.id AND later.start_date > current.end_date
		  AND later.status = 'COMPLETED'`, id).Scan(&laterCompleted); err != nil {
		return storesqlite.MapError("check reconciliation reopen order", err)
	}
	if laterCompleted != 0 {
		return apperr.New(apperr.Conflict, "RECONCILIATION_REOPEN_ORDER", "later completed reconciliations must be reopened first")
	}
	var overlappingClosedPeriods int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM reconciliations reconciliation
		JOIN statement_accounts account ON account.id = reconciliation.statement_account_id
		JOIN book_periods period_status ON period_status.book_id = account.book_id AND period_status.status = 'CLOSED'
		JOIN fiscal_periods period ON period.id = period_status.period_id
		WHERE reconciliation.id = ? AND reconciliation.status = 'COMPLETED'
		  AND period.end_date >= reconciliation.start_date
		  AND reconciliation.end_date >= period.start_date`, id).Scan(&overlappingClosedPeriods); err != nil {
		return storesqlite.MapError("check reconciliation period reopen order", err)
	}
	if overlappingClosedPeriods != 0 {
		return apperr.New(apperr.Conflict, "RECONCILIATION_REOPEN_PERIOD_CLOSED", "overlapping closed book periods must be reopened first")
	}
	result, err := tx.ExecContext(ctx, `UPDATE reconciliations SET status = 'OPEN', completed_at = NULL,
        completed_by = NULL, reopened_at = ?, reopened_by = ?, reopen_reason = ? WHERE id = ? AND status = 'COMPLETED'`,
		storesqlite.UTCNow(), s.actor, reason, id)
	if err != nil {
		return storesqlite.MapError("reopen reconciliation", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return apperr.New(apperr.Conflict, "RECONCILIATION_NOT_COMPLETED", "reconciliation is not completed")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "reconcile reopen", AggregateType: "reconciliation", AggregateID: id,
		Payload: map[string]any{"reason": reason},
	}); err != nil {
		return err
	}
	return tx.Commit()
}
