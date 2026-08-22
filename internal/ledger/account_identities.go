package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type identityScanner interface {
	Scan(dest ...any) error
}

const accountIdentitySelect = `SELECT ai.id, ai.entity_id, e.code, ai.account_id, a.code,
	ai.source_system, ai.external_id, ai.account_number, ai.account_name, ai.source_active,
	ai.evidence_source_kind, ai.evidence_source_path, ai.evidence_source_sha256,
	ai.evidence_locator, COALESCE(ai.evidence_payload_sha256, ''), ai.created_at
	FROM account_identities ai
	JOIN entities e ON e.id = ai.entity_id
	JOIN accounts a ON a.id = ai.account_id`

func scanAccountIdentity(scanner identityScanner) (AccountIdentity, error) {
	var result AccountIdentity
	var active int
	err := scanner.Scan(
		&result.ID, &result.EntityID, &result.EntityCode, &result.AccountID, &result.AccountCode,
		&result.SourceSystem, &result.ExternalID, &result.AccountNumber, &result.Name, &active,
		&result.Evidence.SourceKind, &result.Evidence.SourcePath, &result.Evidence.SourceSHA256,
		&result.Evidence.Locator, &result.Evidence.PayloadSHA256, &result.CreatedAt,
	)
	result.Active = active == 1
	return result, err
}

func normalizeAccountIdentityInput(input AddAccountIdentityInput) AddAccountIdentityInput {
	input.Entity = normalizeCode(input.Entity)
	input.Account = normalizeCode(input.Account)
	input.SourceSystem = normalizeCode(input.SourceSystem)
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

func validateIdentitySHA256(value, field string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if len(value) != 64 {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", field+" must be a 64-character SHA-256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", field+" must be a hexadecimal SHA-256 digest")
	}
	return nil
}

func validateAccountIdentityInput(input AddAccountIdentityInput) error {
	if input.Entity == "" || input.Account == "" || input.SourceSystem == "" || input.ExternalID == "" || input.Name == "" {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", "entity, account, source system, external id, and source account name are required")
	}
	if len(input.SourceSystem) > 64 || len(input.ExternalID) > 512 || len(input.AccountNumber) > 128 || len(input.Name) > 512 {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", "external account identity exceeds a supported field length")
	}
	if input.Evidence.SourceKind == "" || input.Evidence.SourcePath == "" || input.Evidence.Locator == "" {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", "evidence source kind, source path, source SHA-256, and locator are required")
	}
	if len(input.Evidence.SourceKind) > 64 {
		return apperr.New(apperr.Invalid, "ACCOUNT_IDENTITY_INVALID", "evidence source kind exceeds 64 characters")
	}
	if err := validateIdentitySHA256(input.Evidence.SourceSHA256, "evidence source SHA-256", true); err != nil {
		return err
	}
	return validateIdentitySHA256(input.Evidence.PayloadSHA256, "evidence payload SHA-256", false)
}

func sameAccountIdentity(existing AccountIdentity, input AddAccountIdentityInput, entityID, accountID string) bool {
	return existing.EntityID == entityID &&
		existing.AccountID == accountID &&
		existing.SourceSystem == input.SourceSystem &&
		existing.ExternalID == input.ExternalID &&
		existing.AccountNumber == input.AccountNumber &&
		existing.Name == input.Name &&
		existing.Active == input.Active &&
		existing.Evidence == input.Evidence
}

// AddAccountIdentity records an immutable, realm-local external account mapping.
// Repeating an identical input is idempotent; changing a previously observed
// identity is a conflict and requires preserving both the original evidence and
// an explicit migration decision outside this table.
func (s *Service) AddAccountIdentity(ctx context.Context, input AddAccountIdentityInput) (AccountIdentity, error) {
	if err := s.requireActor(); err != nil {
		return AccountIdentity{}, err
	}
	input = normalizeAccountIdentityInput(input)
	if err := validateAccountIdentityInput(input); err != nil {
		return AccountIdentity{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return AccountIdentity{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	entityID, err := lookupID(ctx, tx, "entities", input.Entity)
	if err != nil {
		return AccountIdentity{}, err
	}
	accountID, err := lookupID(ctx, tx, "accounts", input.Account)
	if err != nil {
		return AccountIdentity{}, err
	}
	var accountEnabled int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM books b
		JOIN book_accounts ba ON ba.book_id = b.id AND ba.account_id = ?
		WHERE b.entity_id = ? AND b.kind = 'ACTUAL' AND b.status = 'ACTIVE'
	)`, accountID, entityID).Scan(&accountEnabled); err != nil {
		return AccountIdentity{}, storesqlite.MapError("validate external account identity", err)
	}
	if accountEnabled != 1 {
		return AccountIdentity{}, apperr.New(apperr.Validation, "ACCOUNT_IDENTITY_ACCOUNT_NOT_ENABLED", "account is not enabled in the entity's active actual book")
	}
	existing, err := scanAccountIdentity(tx.QueryRowContext(ctx, accountIdentitySelect+`
		WHERE ai.entity_id = ? AND ai.source_system = ? AND ai.external_id = ?`, entityID, input.SourceSystem, input.ExternalID))
	if err == nil {
		if sameAccountIdentity(existing, input, entityID, accountID) {
			return existing, nil
		}
		return AccountIdentity{}, apperr.New(apperr.Conflict, "ACCOUNT_IDENTITY_CONFLICT", "external account identity was already recorded with different mapping or evidence")
	}
	if err != sql.ErrNoRows {
		return AccountIdentity{}, storesqlite.MapError("look up external account identity", err)
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return AccountIdentity{}, err
	}
	now := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_identities
		(id, entity_id, account_id, source_system, external_id, account_number, account_name,
		 source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		 evidence_locator, evidence_payload_sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, entityID, accountID, input.SourceSystem, input.ExternalID, input.AccountNumber, input.Name,
		input.Active, input.Evidence.SourceKind, input.Evidence.SourcePath, input.Evidence.SourceSHA256,
		input.Evidence.Locator, nilIfEmpty(input.Evidence.PayloadSHA256), now); err != nil {
		return AccountIdentity{}, storesqlite.MapError("add external account identity", err)
	}
	result := AccountIdentity{
		ID: id, EntityID: entityID, EntityCode: input.Entity, AccountID: accountID, AccountCode: input.Account,
		SourceSystem: input.SourceSystem, ExternalID: input.ExternalID, AccountNumber: input.AccountNumber,
		Name: input.Name, Active: input.Active, Evidence: input.Evidence, CreatedAt: now,
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "account identity add", AggregateType: "account_identity", AggregateID: id,
		Payload: result,
	}); err != nil {
		return AccountIdentity{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccountIdentity{}, storesqlite.MapError("commit external account identity", err)
	}
	return result, nil
}

// ListAccountIdentities returns immutable source mappings, optionally filtered
// by entity code, master account code, or source system.
func (s *Service) ListAccountIdentities(ctx context.Context, filter AccountIdentityFilter) ([]AccountIdentity, error) {
	filter.Entity = normalizeCode(filter.Entity)
	filter.Account = normalizeCode(filter.Account)
	filter.SourceSystem = normalizeCode(filter.SourceSystem)
	query := accountIdentitySelect
	var conditions []string
	var args []any
	if filter.Entity != "" {
		conditions = append(conditions, "e.code = ?")
		args = append(args, filter.Entity)
	}
	if filter.Account != "" {
		conditions = append(conditions, "a.code = ?")
		args = append(args, filter.Account)
	}
	if filter.SourceSystem != "" {
		conditions = append(conditions, "ai.source_system = ?")
		args = append(args, filter.SourceSystem)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY e.code, ai.source_system, ai.external_id, a.code"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list external account identities", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []AccountIdentity
	for rows.Next() {
		identity, err := scanAccountIdentity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, identity)
	}
	return result, rows.Err()
}
