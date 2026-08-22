package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type StatementAccountIdentityEvidence struct {
	SourceKind    string `json:"source_kind"`
	SourcePath    string `json:"source_path"`
	SourceSHA256  string `json:"source_sha256"`
	Locator       string `json:"locator"`
	PayloadSHA256 string `json:"payload_sha256,omitempty"`
}

type AddStatementAccountIdentityInput struct {
	StatementAccount string                           `json:"statement_account"`
	SourceSystem     string                           `json:"source_system"`
	SourceRealm      string                           `json:"source_realm"`
	ExternalID       string                           `json:"external_id"`
	AccountNumber    string                           `json:"account_number,omitempty"`
	Name             string                           `json:"name"`
	Active           bool                             `json:"active"`
	Evidence         StatementAccountIdentityEvidence `json:"evidence"`
}

type StatementAccountIdentity struct {
	ID                 string                           `json:"id"`
	StatementAccountID string                           `json:"statement_account_id"`
	StatementAccount   string                           `json:"statement_account"`
	EntityCode         string                           `json:"entity"`
	SourceSystem       string                           `json:"source_system"`
	SourceRealm        string                           `json:"source_realm"`
	ExternalID         string                           `json:"external_id"`
	AccountNumber      string                           `json:"account_number,omitempty"`
	Name               string                           `json:"name"`
	Active             bool                             `json:"active"`
	Evidence           StatementAccountIdentityEvidence `json:"evidence"`
	CreatedAt          string                           `json:"created_at"`
	CreatedBy          string                           `json:"created_by"`
}

type StatementAccountIdentityFilter struct {
	StatementAccount string
	Entity           string
	SourceSystem     string
	SourceRealm      string
}

type statementAccountIdentityScanner interface {
	Scan(dest ...any) error
}

type statementAccountIdentityQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const statementAccountIdentitySelect = `SELECT sai.id, sai.statement_account_id, sa.code, e.code,
	sai.source_system, sai.source_realm, sai.external_id, sai.account_number, sai.account_name, sai.source_active,
	sai.evidence_source_kind, sai.evidence_source_path, sai.evidence_source_sha256,
	sai.evidence_locator, COALESCE(sai.evidence_payload_sha256, ''), sai.created_at, sai.created_by
	FROM statement_account_identities sai
	JOIN statement_accounts sa ON sa.id = sai.statement_account_id
	JOIN entities e ON e.id = sa.entity_id`

func scanStatementAccountIdentity(scanner statementAccountIdentityScanner) (StatementAccountIdentity, error) {
	var result StatementAccountIdentity
	var active int
	err := scanner.Scan(
		&result.ID, &result.StatementAccountID, &result.StatementAccount, &result.EntityCode,
		&result.SourceSystem, &result.SourceRealm, &result.ExternalID, &result.AccountNumber, &result.Name, &active,
		&result.Evidence.SourceKind, &result.Evidence.SourcePath, &result.Evidence.SourceSHA256,
		&result.Evidence.Locator, &result.Evidence.PayloadSHA256, &result.CreatedAt, &result.CreatedBy,
	)
	result.Active = active == 1
	return result, err
}

func normalizeStatementAccountIdentityInput(input AddStatementAccountIdentityInput) AddStatementAccountIdentityInput {
	input.StatementAccount = normalizeCode(input.StatementAccount)
	input.SourceSystem = normalizeCode(input.SourceSystem)
	input.SourceRealm = normalizeCode(input.SourceRealm)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.AccountNumber = strings.TrimSpace(input.AccountNumber)
	input.Name = strings.TrimSpace(input.Name)
	input.Evidence.SourceKind = normalizeCode(input.Evidence.SourceKind)
	input.Evidence.SourcePath = strings.TrimSpace(input.Evidence.SourcePath)
	input.Evidence.SourceSHA256 = strings.ToLower(strings.TrimSpace(input.Evidence.SourceSHA256))
	input.Evidence.Locator = strings.TrimSpace(input.Evidence.Locator)
	input.Evidence.PayloadSHA256 = strings.ToLower(strings.TrimSpace(input.Evidence.PayloadSHA256))
	return input
}

func validateStatementIdentitySHA256(value, field string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if len(value) != 64 {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", field+" must be a 64-character SHA-256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", field+" must be a hexadecimal SHA-256 digest")
	}
	return nil
}

func validateStatementAccountIdentityInput(input AddStatementAccountIdentityInput) error {
	if input.StatementAccount == "" || input.SourceSystem == "" || input.SourceRealm == "" || input.ExternalID == "" || input.Name == "" {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", "statement account, source system, source realm, external id, and source account name are required")
	}
	if len(input.SourceSystem) > 64 || len(input.SourceRealm) > 128 || len(input.ExternalID) > 512 || len(input.AccountNumber) > 128 || len(input.Name) > 512 {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", "statement account identity exceeds a supported field length")
	}
	if input.Evidence.SourceKind == "" || input.Evidence.SourcePath == "" || input.Evidence.Locator == "" {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", "evidence source kind, source path, source SHA-256, and locator are required")
	}
	if len(input.Evidence.SourceKind) > 64 {
		return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_IDENTITY_INVALID", "evidence source kind exceeds 64 characters")
	}
	if err := validateStatementIdentitySHA256(input.Evidence.SourceSHA256, "evidence source SHA-256", true); err != nil {
		return err
	}
	return validateStatementIdentitySHA256(input.Evidence.PayloadSHA256, "evidence payload SHA-256", false)
}

func sameStatementAccountIdentity(existing StatementAccountIdentity, input AddStatementAccountIdentityInput, statementAccountID string) bool {
	return existing.StatementAccountID == statementAccountID &&
		existing.SourceSystem == input.SourceSystem &&
		existing.SourceRealm == input.SourceRealm &&
		existing.ExternalID == input.ExternalID &&
		existing.AccountNumber == input.AccountNumber &&
		existing.Name == input.Name &&
		existing.Active == input.Active &&
		existing.Evidence == input.Evidence
}

func resolveStatementAccountIdentityTarget(ctx context.Context, queryer statementAccountIdentityQueryer, code string) (string, string, error) {
	var id, entityCode string
	err := queryer.QueryRowContext(ctx, `SELECT sa.id, e.code
		FROM statement_accounts sa JOIN entities e ON e.id = sa.entity_id
		WHERE sa.code = ?`, code).Scan(&id, &entityCode)
	if err == sql.ErrNoRows {
		return "", "", apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", "statement account was not found")
	}
	if err != nil {
		return "", "", storesqlite.MapError("look up statement account identity target", err)
	}
	return id, entityCode, nil
}

func validateStatementAccountIdentityMapping(ctx context.Context, queryer statementAccountIdentityQueryer, input AddStatementAccountIdentityInput) (string, string, *StatementAccountIdentity, error) {
	statementAccountID, entityCode, err := resolveStatementAccountIdentityTarget(ctx, queryer, input.StatementAccount)
	if err != nil {
		return "", "", nil, err
	}
	existing, err := scanStatementAccountIdentity(queryer.QueryRowContext(ctx, statementAccountIdentitySelect+`
		WHERE sai.source_system = ? AND sai.source_realm = ? AND sai.external_id = ?`, input.SourceSystem, input.SourceRealm, input.ExternalID))
	if err == nil {
		if sameStatementAccountIdentity(existing, input, statementAccountID) {
			return statementAccountID, entityCode, &existing, nil
		}
		return "", "", nil, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_IDENTITY_CONFLICT", "realm-local external statement account identity was already recorded with a different mapping or evidence")
	}
	if err != sql.ErrNoRows {
		return "", "", nil, storesqlite.MapError("look up statement account identity", err)
	}
	var frozen int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM statement_account_precoverage_closures
		WHERE statement_account_id = ?`, statementAccountID).Scan(&frozen); err != nil {
		return "", "", nil, storesqlite.MapError("check statement account identity lifecycle", err)
	}
	if frozen != 0 {
		return "", "", nil, apperr.New(apperr.Conflict, "STATEMENT_ACCOUNT_IDENTITY_SET_FROZEN", "precoverage closure freezes the complete statement-account identity set")
	}
	return statementAccountID, entityCode, nil, nil
}

// ValidateStatementAccountIdentity performs the same normalization, evidence,
// target, idempotency, and conflict checks as AddStatementAccountIdentity
// without writing. It returns the exact normalized input for a trustworthy CLI
// preview before --commit.
func (s *Service) ValidateStatementAccountIdentity(ctx context.Context, input AddStatementAccountIdentityInput) (AddStatementAccountIdentityInput, error) {
	input = normalizeStatementAccountIdentityInput(input)
	if err := validateStatementAccountIdentityInput(input); err != nil {
		return AddStatementAccountIdentityInput{}, err
	}
	_, _, _, err := validateStatementAccountIdentityMapping(ctx, s.store.DB(), input)
	if err != nil {
		return AddStatementAccountIdentityInput{}, err
	}
	return input, nil
}

// AddStatementAccountIdentity records an immutable source alias for a
// statement account. Multiple aliases may map to one statement account, while
// a source-system/source-realm/external-id tuple may map only once.
// Repeating identical evidence is idempotent.
func (s *Service) AddStatementAccountIdentity(ctx context.Context, input AddStatementAccountIdentityInput) (StatementAccountIdentity, error) {
	if err := s.requireActor(); err != nil {
		return StatementAccountIdentity{}, err
	}
	input = normalizeStatementAccountIdentityInput(input)
	if err := validateStatementAccountIdentityInput(input); err != nil {
		return StatementAccountIdentity{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return StatementAccountIdentity{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	statementAccountID, entityCode, existing, err := validateStatementAccountIdentityMapping(ctx, tx, input)
	if err != nil {
		return StatementAccountIdentity{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return StatementAccountIdentity{}, err
	}
	now := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `INSERT INTO statement_account_identities
		(id, statement_account_id, source_system, source_realm, external_id, account_number, account_name,
		 source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		 evidence_locator, evidence_payload_sha256, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, statementAccountID, input.SourceSystem, input.SourceRealm, input.ExternalID, input.AccountNumber, input.Name,
		input.Active, input.Evidence.SourceKind, input.Evidence.SourcePath, input.Evidence.SourceSHA256,
		input.Evidence.Locator, nilIfEmpty(input.Evidence.PayloadSHA256), now, s.actor); err != nil {
		return StatementAccountIdentity{}, storesqlite.MapError("add statement account identity", err)
	}
	result := StatementAccountIdentity{
		ID: id, StatementAccountID: statementAccountID, StatementAccount: input.StatementAccount,
		EntityCode: entityCode, SourceSystem: input.SourceSystem, SourceRealm: input.SourceRealm, ExternalID: input.ExternalID,
		AccountNumber: input.AccountNumber, Name: input.Name, Active: input.Active,
		Evidence: input.Evidence, CreatedAt: now, CreatedBy: s.actor,
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "statement-account identity add", AggregateType: "statement_account_identity", AggregateID: id,
		Payload: result,
	}); err != nil {
		return StatementAccountIdentity{}, err
	}
	if err := tx.Commit(); err != nil {
		return StatementAccountIdentity{}, storesqlite.MapError("commit statement account identity", err)
	}
	return result, nil
}

// ListStatementAccountIdentities lists immutable source aliases with optional
// statement-account, entity, and source-system filters.
func (s *Service) ListStatementAccountIdentities(ctx context.Context, filter StatementAccountIdentityFilter) ([]StatementAccountIdentity, error) {
	filter.StatementAccount = normalizeCode(filter.StatementAccount)
	filter.Entity = normalizeCode(filter.Entity)
	filter.SourceSystem = normalizeCode(filter.SourceSystem)
	filter.SourceRealm = normalizeCode(filter.SourceRealm)
	query := statementAccountIdentitySelect
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
	if filter.SourceSystem != "" {
		conditions = append(conditions, "sai.source_system = ?")
		args = append(args, filter.SourceSystem)
	}
	if filter.SourceRealm != "" {
		conditions = append(conditions, "sai.source_realm = ?")
		args = append(args, filter.SourceRealm)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY e.code, sa.code, sai.source_system, sai.source_realm, sai.external_id"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list statement account identities", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []StatementAccountIdentity
	for rows.Next() {
		identity, err := scanStatementAccountIdentity(rows)
		if err != nil {
			return nil, storesqlite.MapError("scan statement account identity", err)
		}
		result = append(result, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("list statement account identities", err)
	}
	return result, nil
}
