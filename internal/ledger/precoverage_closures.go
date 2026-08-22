package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

const PrecoverageClosureDisposition = "CLOSED_BEFORE_REQUIRED_COVERAGE"

type PrecoverageClosureIdentity struct {
	SourceSystem string `json:"source_system"`
	SourceRealm  string `json:"source_realm"`
	ExternalID   string `json:"external_id"`
}

type PrecoverageClosureEvidence struct {
	SourceKind   string `json:"source_kind"`
	SourcePath   string `json:"source_path"`
	SourceSHA256 string `json:"source_sha256"`
	Locator      string `json:"locator"`
}

type PrecoverageZeroEvidence struct {
	SourceKind            string `json:"source_kind"`
	SourcePath            string `json:"source_path"`
	SourceSHA256          string `json:"source_sha256"`
	Locator               string `json:"locator"`
	PayloadSHA256         string `json:"payload_sha256"`
	ObservedOn            string `json:"observed_on"`
	ProviderStatus        string `json:"provider_status"`
	CurrentBalanceCents   int64  `json:"current_balance_cents"`
	AvailableBalanceCents int64  `json:"available_balance_cents"`
}

type CloseStatementAccountBeforeCoverageInput struct {
	StatementAccount  string                     `json:"statement_account"`
	Identity          PrecoverageClosureIdentity `json:"identity"`
	ClosedOn          string                     `json:"closed_on"`
	ClosureEvidence   PrecoverageClosureEvidence `json:"closure_evidence"`
	ZeroEvidence      PrecoverageZeroEvidence    `json:"zero_evidence"`
	AccountHolder     string                     `json:"account_holder"`
	AccountSuffix     string                     `json:"account_suffix"`
	Reason            string                     `json:"reason"`
	InputSourcePath   string                     `json:"input_source_path"`
	InputSourceSHA256 string                     `json:"input_source_sha256"`
}

type StatementAccountPrecoverageClosure struct {
	ID                            string                     `json:"id,omitempty"`
	StatementAccountID            string                     `json:"statement_account_id"`
	StatementAccount              string                     `json:"statement_account"`
	StatementAccountIdentityID    string                     `json:"statement_account_identity_id"`
	ActiveIdentityCount           int                        `json:"active_identity_count"`
	ActiveIdentityDigest          string                     `json:"active_identity_digest"`
	EntityCode                    string                     `json:"entity"`
	BookCode                      string                     `json:"book"`
	GLAccountCode                 string                     `json:"gl_account"`
	ReconciliationRequiredFrom    string                     `json:"reconciliation_required_from"`
	ReconciliationRequiredThrough string                     `json:"reconciliation_required_through"`
	CoverageDisposition           string                     `json:"coverage_disposition"`
	ClosedOn                      string                     `json:"closed_on"`
	ClosureEvidence               PrecoverageClosureEvidence `json:"closure_evidence"`
	ZeroEvidence                  PrecoverageZeroEvidence    `json:"zero_evidence"`
	AccountHolder                 string                     `json:"account_holder"`
	AccountSuffix                 string                     `json:"account_suffix"`
	Reason                        string                     `json:"reason"`
	InputSourcePath               string                     `json:"input_source_path"`
	InputSourceSHA256             string                     `json:"input_source_sha256"`
	ControlBalanceAtClosureCents  int64                      `json:"control_balance_at_closure_cents"`
	CurrentControlBalanceCents    int64                      `json:"current_control_balance_cents"`
	PostClosureControlLineCount   int                        `json:"post_closure_control_line_count"`
	DraftControlLineCount         int                        `json:"draft_control_line_count"`
	Status                        string                     `json:"status"`
	ArchivedAt                    string                     `json:"archived_at,omitempty"`
	ArchivedBy                    string                     `json:"archived_by,omitempty"`
	CreatedAt                     string                     `json:"created_at,omitempty"`
	CreatedBy                     string                     `json:"created_by,omitempty"`
	Changed                       bool                       `json:"changed"`
}

type PrecoverageClosureFilter struct {
	StatementAccount string
	Entity           string
}

type precoverageClosureQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type precoverageClosureScanner interface {
	Scan(...any) error
}

const precoverageClosureSelect = `SELECT closure.id, closure.statement_account_id, sa.code,
	closure.statement_account_identity_id,
	COALESCE(binding.active_identity_count, 0), COALESCE(binding.active_identity_digest, ''),
	e.code, b.code, a.code,
	sa.reconciliation_required_from, COALESCE(sa.reconciliation_required_through, ''),
	closure.closed_on,
	closure.closure_evidence_source_kind, closure.closure_evidence_source_path,
	closure.closure_evidence_source_sha256, closure.closure_evidence_locator,
	closure.zero_evidence_source_kind, closure.zero_evidence_source_path,
	closure.zero_evidence_source_sha256, closure.zero_evidence_locator,
	closure.zero_evidence_payload_sha256, closure.zero_observed_on,
	closure.provider_status, closure.current_balance_cents, closure.available_balance_cents,
	closure.account_holder, closure.account_suffix, closure.reason,
	closure.input_source_path, closure.input_source_sha256,
	closure.created_at, closure.created_by, sa.status, sa.archived_at, sa.archived_by,
	COALESCE((
	    SELECT SUM(jl.debit_cents - jl.credit_cents)
	    FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
	    WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
	      AND je.status = 'POSTED' AND je.posting_date <= closure.closed_on
	), 0),
	COALESCE((
	    SELECT SUM(jl.debit_cents - jl.credit_cents)
	    FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
	    WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
	      AND je.status = 'POSTED'
	), 0),
	COALESCE((
	    SELECT COUNT(*)
	    FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
	    WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
	      AND je.status = 'POSTED' AND je.posting_date > closure.closed_on
	), 0),
	COALESCE((
	    SELECT COUNT(*)
	    FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
	    WHERE je.book_id = sa.book_id AND jl.account_id = sa.gl_account_id
	      AND je.status = 'DRAFT'
	), 0)
	FROM statement_account_precoverage_closures closure
	LEFT JOIN statement_account_precoverage_identity_bindings binding ON binding.closure_id = closure.id
	JOIN statement_accounts sa ON sa.id = closure.statement_account_id
	JOIN entities e ON e.id = sa.entity_id
	JOIN books b ON b.id = sa.book_id
	JOIN accounts a ON a.id = sa.gl_account_id`

func scanPrecoverageClosure(scanner precoverageClosureScanner) (StatementAccountPrecoverageClosure, error) {
	var result StatementAccountPrecoverageClosure
	err := scanner.Scan(
		&result.ID, &result.StatementAccountID, &result.StatementAccount,
		&result.StatementAccountIdentityID, &result.ActiveIdentityCount, &result.ActiveIdentityDigest,
		&result.EntityCode, &result.BookCode, &result.GLAccountCode,
		&result.ReconciliationRequiredFrom, &result.ReconciliationRequiredThrough,
		&result.ClosedOn,
		&result.ClosureEvidence.SourceKind, &result.ClosureEvidence.SourcePath,
		&result.ClosureEvidence.SourceSHA256, &result.ClosureEvidence.Locator,
		&result.ZeroEvidence.SourceKind, &result.ZeroEvidence.SourcePath,
		&result.ZeroEvidence.SourceSHA256, &result.ZeroEvidence.Locator,
		&result.ZeroEvidence.PayloadSHA256, &result.ZeroEvidence.ObservedOn,
		&result.ZeroEvidence.ProviderStatus, &result.ZeroEvidence.CurrentBalanceCents,
		&result.ZeroEvidence.AvailableBalanceCents,
		&result.AccountHolder, &result.AccountSuffix, &result.Reason,
		&result.InputSourcePath, &result.InputSourceSHA256,
		&result.CreatedAt, &result.CreatedBy, &result.Status, &result.ArchivedAt, &result.ArchivedBy,
		&result.ControlBalanceAtClosureCents, &result.CurrentControlBalanceCents,
		&result.PostClosureControlLineCount, &result.DraftControlLineCount,
	)
	result.CoverageDisposition = PrecoverageClosureDisposition
	return result, err
}

func normalizePrecoverageClosureInput(input CloseStatementAccountBeforeCoverageInput) CloseStatementAccountBeforeCoverageInput {
	input.StatementAccount = normalizeCode(input.StatementAccount)
	input.Identity.SourceSystem = normalizeCode(input.Identity.SourceSystem)
	input.Identity.SourceRealm = normalizeCode(input.Identity.SourceRealm)
	input.Identity.ExternalID = strings.TrimSpace(input.Identity.ExternalID)
	input.ClosedOn = strings.TrimSpace(input.ClosedOn)
	input.ClosureEvidence.SourceKind = normalizeCode(input.ClosureEvidence.SourceKind)
	input.ClosureEvidence.SourcePath = strings.TrimSpace(input.ClosureEvidence.SourcePath)
	input.ClosureEvidence.SourceSHA256 = strings.ToLower(strings.TrimSpace(input.ClosureEvidence.SourceSHA256))
	input.ClosureEvidence.Locator = strings.TrimSpace(input.ClosureEvidence.Locator)
	input.ZeroEvidence.SourceKind = normalizeCode(input.ZeroEvidence.SourceKind)
	input.ZeroEvidence.SourcePath = strings.TrimSpace(input.ZeroEvidence.SourcePath)
	input.ZeroEvidence.SourceSHA256 = strings.ToLower(strings.TrimSpace(input.ZeroEvidence.SourceSHA256))
	input.ZeroEvidence.Locator = strings.TrimSpace(input.ZeroEvidence.Locator)
	input.ZeroEvidence.PayloadSHA256 = strings.ToLower(strings.TrimSpace(input.ZeroEvidence.PayloadSHA256))
	input.ZeroEvidence.ObservedOn = strings.TrimSpace(input.ZeroEvidence.ObservedOn)
	input.ZeroEvidence.ProviderStatus = normalizeCode(input.ZeroEvidence.ProviderStatus)
	input.AccountHolder = strings.TrimSpace(input.AccountHolder)
	input.AccountSuffix = strings.TrimSpace(input.AccountSuffix)
	input.Reason = strings.TrimSpace(input.Reason)
	input.InputSourcePath = strings.TrimSpace(input.InputSourcePath)
	input.InputSourceSHA256 = strings.ToLower(strings.TrimSpace(input.InputSourceSHA256))
	return input
}

func validatePrecoverageSHA(value, field string) error {
	if len(value) != 64 {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", field+" must be a 64-character SHA-256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", field+" must be a hexadecimal SHA-256 digest")
	}
	return nil
}

func validatePrecoverageClosureInput(input CloseStatementAccountBeforeCoverageInput) error {
	if input.StatementAccount == "" || input.Identity.SourceSystem == "" || input.Identity.SourceRealm == "" || input.Identity.ExternalID == "" {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "statement account and exact source identity are required")
	}
	if err := validateDate(input.ClosedOn, "provider closure date"); err != nil {
		return err
	}
	if err := validateDate(input.ZeroEvidence.ObservedOn, "zero-evidence observation date"); err != nil {
		return err
	}
	if input.ZeroEvidence.ObservedOn < input.ClosedOn {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "zero-evidence observation date precedes the provider closure date")
	}
	if input.ClosureEvidence.SourceKind != "PROVIDER_CLOSURE_LETTER" || input.ZeroEvidence.SourceKind != "PROVIDER_ACCOUNT_SNAPSHOT" {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "provider closure letter and provider account snapshot evidence are required")
	}
	if input.ZeroEvidence.ProviderStatus != "ARCHIVED" && input.ZeroEvidence.ProviderStatus != "CLOSED" {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "provider snapshot status must be ARCHIVED or CLOSED")
	}
	if input.ZeroEvidence.CurrentBalanceCents != 0 || input.ZeroEvidence.AvailableBalanceCents != 0 {
		return apperr.New(apperr.Validation, "PRECOVERAGE_PROVIDER_BALANCE_NONZERO", "provider current and available balances must both be exactly zero")
	}
	if input.ClosureEvidence.SourcePath == "" || input.ClosureEvidence.Locator == "" ||
		input.ZeroEvidence.SourcePath == "" || input.ZeroEvidence.Locator == "" ||
		input.AccountHolder == "" || input.AccountSuffix == "" || input.Reason == "" || input.InputSourcePath == "" {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "complete evidence paths, locators, holder, suffix, reason, and input source are required")
	}
	for _, path := range []string{input.ClosureEvidence.SourcePath, input.ZeroEvidence.SourcePath, input.InputSourcePath} {
		if !filepath.IsAbs(path) {
			return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "evidence and input source paths must be absolute")
		}
	}
	if len(input.Identity.SourceSystem) > 64 || len(input.Identity.SourceRealm) > 128 || len(input.Identity.ExternalID) > 512 ||
		len(input.AccountHolder) > 512 || len(input.AccountSuffix) > 32 || len(input.Reason) > 1024 {
		return apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_INVALID", "precoverage closure evidence exceeds a supported field length")
	}
	for _, digest := range []struct{ value, field string }{
		{input.ClosureEvidence.SourceSHA256, "closure evidence SHA-256"},
		{input.ZeroEvidence.SourceSHA256, "zero evidence SHA-256"},
		{input.ZeroEvidence.PayloadSHA256, "zero evidence payload SHA-256"},
		{input.InputSourceSHA256, "input source SHA-256"},
	} {
		if err := validatePrecoverageSHA(digest.value, digest.field); err != nil {
			return err
		}
	}
	return nil
}

type precoverageClosureTarget struct {
	statementAccountID   string
	entityCode           string
	bookCode             string
	glAccountCode        string
	bookID               string
	glAccountID          string
	status               string
	requiredFrom         string
	requiredThrough      string
	archivedAt           string
	archivedBy           string
	archiveReason        string
	identityID           string
	identityNumber       string
	activeIdentityCount  int
	activeIdentityDigest string
}

func lookupPrecoverageClosureTarget(ctx context.Context, q precoverageClosureQueryer, input CloseStatementAccountBeforeCoverageInput) (precoverageClosureTarget, error) {
	var target precoverageClosureTarget
	err := q.QueryRowContext(ctx, `SELECT sa.id, e.code, b.code, a.code, sa.book_id, sa.gl_account_id,
		sa.status, sa.reconciliation_required_from, COALESCE(sa.reconciliation_required_through, ''),
		sa.archived_at, sa.archived_by, sa.archive_reason
		FROM statement_accounts sa
		JOIN entities e ON e.id = sa.entity_id
		JOIN books b ON b.id = sa.book_id
		JOIN accounts a ON a.id = sa.gl_account_id
		WHERE sa.code = ?`, input.StatementAccount).Scan(
		&target.statementAccountID, &target.entityCode, &target.bookCode, &target.glAccountCode,
		&target.bookID, &target.glAccountID, &target.status, &target.requiredFrom, &target.requiredThrough,
		&target.archivedAt, &target.archivedBy, &target.archiveReason,
	)
	if err == sql.ErrNoRows {
		return target, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	}
	if err != nil {
		return target, storesqlite.MapError("read precoverage closure target", err)
	}
	err = q.QueryRowContext(ctx, `SELECT id, account_number
		FROM statement_account_identities
		WHERE statement_account_id = ? AND source_system = ? AND source_realm = ? AND external_id = ?
		  AND source_active = 1`,
		target.statementAccountID, input.Identity.SourceSystem, input.Identity.SourceRealm, input.Identity.ExternalID,
	).Scan(&target.identityID, &target.identityNumber)
	if err == sql.ErrNoRows {
		return target, apperr.New(apperr.Conflict, "PRECOVERAGE_IDENTITY_MISMATCH", "exact provider identity is not mapped to the statement account")
	}
	if err != nil {
		return target, storesqlite.MapError("read precoverage closure identity", err)
	}
	if len(target.identityNumber) < len(input.AccountSuffix) || !strings.HasSuffix(target.identityNumber, input.AccountSuffix) {
		return target, apperr.New(apperr.Conflict, "PRECOVERAGE_IDENTITY_MISMATCH", "provider identity account number does not match the closure evidence suffix")
	}
	target.activeIdentityCount, target.activeIdentityDigest, err = storesqlite.ActiveStatementAccountIdentityDigest(ctx, q, target.statementAccountID)
	if err != nil {
		return target, err
	}
	if target.activeIdentityCount == 0 {
		return target, apperr.New(apperr.Conflict, "PRECOVERAGE_IDENTITY_MISMATCH", "precoverage closure requires at least one active statement-account identity")
	}
	var contradictoryAliases int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM statement_account_identities selected
		JOIN statement_account_identities alias
		  ON alias.statement_account_id = selected.statement_account_id
		 AND alias.source_active = 1
		 AND alias.source_system = selected.source_system
		 AND alias.source_realm = selected.source_realm
		 AND alias.id <> selected.id
		WHERE selected.id = ? AND alias.account_number <> selected.account_number`, target.identityID).Scan(&contradictoryAliases); err != nil {
		return target, storesqlite.MapError("check precoverage provider aliases", err)
	}
	if contradictoryAliases != 0 {
		return target, apperr.New(apperr.Conflict, "PRECOVERAGE_IDENTITY_SET_CONFLICT", "active aliases in the certified provider realm identify different provider accounts")
	}
	return target, nil
}

func samePrecoverageClosure(existing StatementAccountPrecoverageClosure, target precoverageClosureTarget, input CloseStatementAccountBeforeCoverageInput) bool {
	return existing.StatementAccountID == target.statementAccountID &&
		existing.StatementAccountIdentityID == target.identityID &&
		existing.ActiveIdentityCount == target.activeIdentityCount &&
		existing.ActiveIdentityDigest == target.activeIdentityDigest &&
		existing.ReconciliationRequiredFrom == target.requiredFrom &&
		existing.ReconciliationRequiredThrough == target.requiredFrom &&
		existing.ClosedOn == input.ClosedOn &&
		existing.ClosureEvidence == input.ClosureEvidence &&
		existing.ZeroEvidence == input.ZeroEvidence &&
		existing.AccountHolder == input.AccountHolder && existing.AccountSuffix == input.AccountSuffix &&
		existing.Reason == input.Reason && existing.InputSourcePath == input.InputSourcePath &&
		existing.InputSourceSHA256 == input.InputSourceSHA256 && existing.Status == "ARCHIVED" &&
		existing.ArchivedAt != "" && existing.ArchivedBy != "" &&
		existing.ControlBalanceAtClosureCents == 0 && existing.CurrentControlBalanceCents == 0 &&
		existing.PostClosureControlLineCount == 0 && existing.DraftControlLineCount == 0
}

func validatePrecoverageClosure(ctx context.Context, q precoverageClosureQueryer, input CloseStatementAccountBeforeCoverageInput) (StatementAccountPrecoverageClosure, error) {
	if _, err := storesqlite.VerifyAudit(ctx, q); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	target, err := lookupPrecoverageClosureTarget(ctx, q, input)
	if err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	existing, err := scanPrecoverageClosure(q.QueryRowContext(ctx, precoverageClosureSelect+`
		WHERE closure.statement_account_id = ?`, target.statementAccountID))
	if err == nil {
		if samePrecoverageClosure(existing, target, input) {
			var validLifecycle int
			if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
				FROM valid_statement_account_precoverage_closures WHERE id = ?`, existing.ID).Scan(&validLifecycle); err != nil {
				return StatementAccountPrecoverageClosure{}, storesqlite.MapError("verify existing precoverage lifecycle", err)
			}
			if validLifecycle != 1 {
				return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Integrity, "PRECOVERAGE_LIFECYCLE_INVALID", "existing precoverage closure is not one complete, audit-bound terminal lifecycle")
			}
			existing.Changed = false
			return existing, nil
		}
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_CLOSURE_CONFLICT", "statement account already has different precoverage closure evidence")
	}
	if err != sql.ErrNoRows {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("read precoverage closure evidence", err)
	}
	if target.status != "ACTIVE" {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_NOT_ACTIVE", "statement account is not active")
	}
	if target.requiredThrough != "" {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_CLOSURE_CONFLICT", "active statement account already has a reconciliation-required-through boundary")
	}
	if input.ClosedOn >= target.requiredFrom {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Invalid, "PRECOVERAGE_CLOSURE_DATE_INVALID", "provider closure date must precede reconciliation-required-from")
	}
	var unresolvedSource, unmaterializedPosted, openReconciliations, laterEvidence int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM current_source_records source
		WHERE source.materialization_kind = 'STATEMENT'
		  AND source.statement_account_id = ?
		  AND source.disposition IN ('PENDING', 'NEEDS_REVIEW')`, target.statementAccountID).Scan(&unresolvedSource); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check precoverage source dispositions", err)
	}
	if unresolvedSource != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_SOURCE_UNRESOLVED", "current PENDING and NEEDS_REVIEW statement source must be resolved to POSTED or SOURCE_ONLY before precoverage closure")
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM current_source_records source
		WHERE source.materialization_kind = 'STATEMENT'
		  AND source.statement_account_id = ? AND source.disposition = 'POSTED'
		  AND NOT EXISTS (
		      SELECT 1 FROM statement_transactions materialized
		      WHERE materialized.source_identity_id = source.source_identity_id
		        AND materialized.source_record_id = source.id
		  )`, target.statementAccountID).Scan(&unmaterializedPosted); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check precoverage source materialization", err)
	}
	if unmaterializedPosted != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_HAS_UNMATERIALIZED_SOURCE", "current posted statement source must be materialized before precoverage closure")
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliations
		WHERE statement_account_id = ? AND status = 'OPEN'`, target.statementAccountID).Scan(&openReconciliations); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check precoverage reconciliations", err)
	}
	if openReconciliations != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_HAS_OPEN_RECONCILIATION", "complete or abandon open reconciliation work before precoverage closure")
	}
	if err := q.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM statement_transactions WHERE statement_account_id = ? AND posted_date > ?) +
		(SELECT COUNT(*) FROM reconciliations WHERE statement_account_id = ? AND status <> 'ABANDONED' AND end_date > ?)`,
		target.statementAccountID, input.ClosedOn, target.statementAccountID, input.ClosedOn,
	).Scan(&laterEvidence); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check precoverage statement activity", err)
	}
	if laterEvidence != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_HAS_LATER_ACTIVITY", "provider closure date precedes statement or reconciliation activity")
	}
	var balanceAtClosure, currentBalance int64
	var postClosureLines, draftControlLines int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = ? AND jl.account_id = ? AND je.status = 'POSTED' AND je.posting_date <= ?`,
		target.bookID, target.glAccountID, input.ClosedOn).Scan(&balanceAtClosure); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("read control balance at provider closure", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = ? AND jl.account_id = ? AND je.status = 'POSTED'`,
		target.bookID, target.glAccountID).Scan(&currentBalance); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("read current control balance", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = ? AND jl.account_id = ? AND je.status = 'POSTED' AND je.posting_date > ?`,
		target.bookID, target.glAccountID, input.ClosedOn).Scan(&postClosureLines); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check post-closure control activity", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.book_id = ? AND jl.account_id = ? AND je.status = 'DRAFT'`,
		target.bookID, target.glAccountID).Scan(&draftControlLines); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("check draft control activity", err)
	}
	if balanceAtClosure != 0 || currentBalance != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_CONTROL_BALANCE_NONZERO", "Books control balance must be exactly zero at provider closure and currently")
	}
	if postClosureLines != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_HAS_LATER_CONTROL_ACTIVITY", "provider closure date precedes posted control-account activity")
	}
	if draftControlLines != 0 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_HAS_DRAFT_CONTROL_ACTIVITY", "draft control-account activity must be resolved before precoverage closure")
	}
	return StatementAccountPrecoverageClosure{
		StatementAccountID: target.statementAccountID, StatementAccount: input.StatementAccount,
		StatementAccountIdentityID: target.identityID, EntityCode: target.entityCode,
		ActiveIdentityCount: target.activeIdentityCount, ActiveIdentityDigest: target.activeIdentityDigest,
		BookCode: target.bookCode, GLAccountCode: target.glAccountCode,
		ReconciliationRequiredFrom: target.requiredFrom, ReconciliationRequiredThrough: target.requiredFrom,
		CoverageDisposition: PrecoverageClosureDisposition, ClosedOn: input.ClosedOn,
		ClosureEvidence: input.ClosureEvidence, ZeroEvidence: input.ZeroEvidence,
		AccountHolder: input.AccountHolder, AccountSuffix: input.AccountSuffix, Reason: input.Reason,
		InputSourcePath: input.InputSourcePath, InputSourceSHA256: input.InputSourceSHA256,
		ControlBalanceAtClosureCents: balanceAtClosure, CurrentControlBalanceCents: currentBalance,
		PostClosureControlLineCount: postClosureLines, Status: "ARCHIVED", Changed: true,
		DraftControlLineCount: draftControlLines,
	}, nil
}

func (s *Service) ValidateStatementAccountPrecoverageClosure(ctx context.Context, input CloseStatementAccountBeforeCoverageInput) (StatementAccountPrecoverageClosure, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	input = normalizePrecoverageClosureInput(input)
	if err := validatePrecoverageClosureInput(input); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	return validatePrecoverageClosure(ctx, s.store.DB(), input)
}

func precoverageClosureAuditPayload(result StatementAccountPrecoverageClosure) map[string]any {
	return map[string]any{
		"id": result.ID, "statement_account_id": result.StatementAccountID,
		"statement_account": result.StatementAccount, "statement_account_identity_id": result.StatementAccountIdentityID,
		"entity": result.EntityCode, "book": result.BookCode, "gl_account": result.GLAccountCode,
		"reconciliation_required_from":    result.ReconciliationRequiredFrom,
		"reconciliation_required_through": result.ReconciliationRequiredThrough,
		"coverage_disposition":            result.CoverageDisposition, "closed_on": result.ClosedOn,
		"closure_evidence": result.ClosureEvidence, "zero_evidence": result.ZeroEvidence,
		"account_holder": result.AccountHolder, "account_suffix": result.AccountSuffix,
		"reason": result.Reason, "input_source_path": result.InputSourcePath,
		"input_source_sha256":              result.InputSourceSHA256,
		"control_balance_at_closure_cents": result.ControlBalanceAtClosureCents,
		"current_control_balance_cents":    result.CurrentControlBalanceCents,
		"post_closure_control_line_count":  result.PostClosureControlLineCount,
		"draft_control_line_count":         result.DraftControlLineCount,
		"status":                           result.Status, "archived_at": result.ArchivedAt, "archived_by": result.ArchivedBy,
		"created_at": result.CreatedAt, "created_by": result.CreatedBy, "changed": result.Changed,
	}
}

func (s *Service) CloseStatementAccountBeforeCoverage(ctx context.Context, input CloseStatementAccountBeforeCoverageInput) (StatementAccountPrecoverageClosure, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	input = normalizePrecoverageClosureInput(input)
	if err := validatePrecoverageClosureInput(input); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := validatePrecoverageClosure(ctx, tx, input)
	if err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	if !result.Changed {
		return result, nil
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	now := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `INSERT INTO statement_account_precoverage_closures
		(id, statement_account_id, statement_account_identity_id, closed_on,
		 closure_evidence_source_kind, closure_evidence_source_path, closure_evidence_source_sha256, closure_evidence_locator,
		 zero_evidence_source_kind, zero_evidence_source_path, zero_evidence_source_sha256, zero_evidence_locator,
		 zero_evidence_payload_sha256, zero_observed_on, provider_status,
		 current_balance_cents, available_balance_cents, account_holder, account_suffix, reason,
		 input_source_path, input_source_sha256, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, result.StatementAccountID, result.StatementAccountIdentityID, input.ClosedOn,
		input.ClosureEvidence.SourceKind, input.ClosureEvidence.SourcePath, input.ClosureEvidence.SourceSHA256, input.ClosureEvidence.Locator,
		input.ZeroEvidence.SourceKind, input.ZeroEvidence.SourcePath, input.ZeroEvidence.SourceSHA256, input.ZeroEvidence.Locator,
		input.ZeroEvidence.PayloadSHA256, input.ZeroEvidence.ObservedOn, input.ZeroEvidence.ProviderStatus,
		input.ZeroEvidence.CurrentBalanceCents, input.ZeroEvidence.AvailableBalanceCents,
		input.AccountHolder, input.AccountSuffix, input.Reason,
		input.InputSourcePath, input.InputSourceSHA256, now, s.actor,
	); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("record statement-account precoverage closure", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO statement_account_precoverage_identity_bindings
		(closure_id, active_identity_count, active_identity_digest, created_at, created_by)
		VALUES (?, ?, ?, ?, ?)`, id, result.ActiveIdentityCount, result.ActiveIdentityDigest, now, s.actor); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("bind precoverage statement-account identities", err)
	}
	archivedAt := storesqlite.UTCNow()
	update, err := tx.ExecContext(ctx, `UPDATE statement_accounts
		SET status = 'ARCHIVED', reconciliation_required_through = reconciliation_required_from,
		    archived_at = ?, archived_by = ?, archive_reason = ?
		WHERE id = ? AND status = 'ACTIVE'`, archivedAt, s.actor, input.Reason, result.StatementAccountID)
	if err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("archive precoverage statement account", err)
	}
	if affected, err := update.RowsAffected(); err != nil || affected != 1 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Conflict, "PRECOVERAGE_CLOSURE_STALE", "statement account changed before precoverage closure could commit")
	}
	result.ID = id
	result.CreatedAt = now
	result.CreatedBy = s.actor
	result.ArchivedAt = archivedAt
	result.ArchivedBy = s.actor
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement-account lifecycle close-before-coverage",
		AggregateType: "statement_account_precoverage_closure", AggregateID: id,
		Payload: precoverageClosureAuditPayload(result),
	}); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement-account precoverage bind-identities",
		AggregateType: "statement_account_precoverage_identity_binding", AggregateID: id,
		Payload: map[string]any{
			"closure_id": id, "statement_account_id": result.StatementAccountID,
			"active_identity_count": result.ActiveIdentityCount, "active_identity_digest": result.ActiveIdentityDigest,
			"created_at": now, "created_by": s.actor,
		},
	}); err != nil {
		return StatementAccountPrecoverageClosure{}, err
	}
	var validLifecycle int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM valid_statement_account_precoverage_closures WHERE id = ?`, id).Scan(&validLifecycle); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("verify committed precoverage lifecycle", err)
	}
	if validLifecycle != 1 {
		return StatementAccountPrecoverageClosure{}, apperr.New(apperr.Integrity, "PRECOVERAGE_LIFECYCLE_INVALID", "precoverage closure did not produce one complete, audit-bound lifecycle")
	}
	if err := tx.Commit(); err != nil {
		return StatementAccountPrecoverageClosure{}, storesqlite.MapError("commit statement-account precoverage closure", err)
	}
	return result, nil
}

func (s *Service) ListStatementAccountPrecoverageClosures(ctx context.Context, filter PrecoverageClosureFilter) ([]StatementAccountPrecoverageClosure, error) {
	filter.StatementAccount = normalizeCode(filter.StatementAccount)
	filter.Entity = normalizeCode(filter.Entity)
	query := precoverageClosureSelect
	var conditions []string
	var args []any
	if filter.StatementAccount != "" {
		conditions = append(conditions, "sa.code = ?")
		args = append(args, filter.StatementAccount)
	}
	if filter.Entity != "" {
		conditions = append(conditions, "e.code = ?")
		args = append(args, filter.Entity)
	}
	if len(conditions) != 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY e.code, sa.code"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list statement-account precoverage closures", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []StatementAccountPrecoverageClosure
	for rows.Next() {
		closure, err := scanPrecoverageClosure(rows)
		if err != nil {
			return nil, storesqlite.MapError("scan statement-account precoverage closure", err)
		}
		closure.Changed = false
		result = append(result, closure)
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("list statement-account precoverage closures", err)
	}
	return result, nil
}
