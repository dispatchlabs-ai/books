package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func testStatementAccountIdentityInput() ledger.AddStatementAccountIdentityInput {
	return ledger.AddStatementAccountIdentityInput{
		StatementAccount: "acme-cash", SourceSystem: "finances", SourceRealm: "global", ExternalID: "cash-old-id",
		AccountNumber: "0110", Name: "Example Bank Base", Active: true,
		Evidence: ledger.StatementAccountIdentityEvidence{
			SourceKind: "sqlite_account_registry", SourcePath: "/evidence/finance.sqlite",
			SourceSHA256: strings.Repeat("A", 64), Locator: "accounts#cash-old-id",
			PayloadSHA256: strings.Repeat("B", 64),
		},
	}
}

func TestStatementAccountIdentitiesSupportAliasesAndAreImmutable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	cash := createCashStatementAccount(t, f, true)
	input := testStatementAccountIdentityInput()
	if _, err := f.service.ValidateStatementAccountIdentity(ctx, input); err != nil {
		t.Fatalf("validate identity preview: %v", err)
	}
	created, err := f.service.AddStatementAccountIdentity(ctx, input)
	if err != nil {
		t.Fatalf("add statement account identity: %v", err)
	}
	if created.StatementAccountID != cash.ID || created.StatementAccount != cash.Code ||
		created.EntityCode != "ACME" || created.SourceSystem != "FINANCES" || created.SourceRealm != "GLOBAL" ||
		created.Evidence.SourceKind != "SQLITE_ACCOUNT_REGISTRY" || created.CreatedBy != "test" {
		t.Fatalf("identity was not normalized or linked: %+v", created)
	}
	if created.Evidence.SourceSHA256 != strings.Repeat("a", 64) || created.Evidence.PayloadSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("identity evidence hashes were not normalized: %+v", created.Evidence)
	}
	repeated, err := f.service.AddStatementAccountIdentity(ctx, input)
	if err != nil || repeated.ID != created.ID {
		t.Fatalf("idempotent identity = %+v, %v", repeated, err)
	}

	alias := input
	alias.SourceSystem = "PROVIDER"
	alias.ExternalID = "provider-account-0110"
	alias.Evidence.SourceKind = "PROVIDER_ACCOUNT_EXPORT"
	alias.Evidence.Locator = "accounts/provider-account-0110"
	alias.Evidence.PayloadSHA256 = ""
	second, err := f.service.AddStatementAccountIdentity(ctx, alias)
	if err != nil {
		t.Fatalf("add alias for same statement account: %v", err)
	}
	if second.StatementAccountID != created.StatementAccountID || second.ID == created.ID {
		t.Fatalf("alias did not preserve many-to-one mapping: first=%+v second=%+v", created, second)
	}
	identities, err := f.service.ListStatementAccountIdentities(ctx, ledger.StatementAccountIdentityFilter{
		StatementAccount: "acme-cash", Entity: "acme",
	})
	if err != nil || len(identities) != 2 {
		t.Fatalf("statement account aliases = %+v, %v", identities, err)
	}

	if _, err := f.service.CreateEntity(ctx, ledger.CreateEntityInput{
		Code: "NORTHSTAR", LegalName: "Northstar, Inc.", Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
		Code: "1100", Name: "Brokerage", Type: "ASSET", BookCodes: []string{"NORTHSTAR"},
	}); err != nil {
		t.Fatal(err)
	}
	brokerage, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "NORTHSTAR-BROKERAGE", Entity: "NORTHSTAR", Book: "NORTHSTAR", GLAccount: "1100",
		Name: "TradeStation", Kind: "INVESTMENT", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		t.Fatalf("create investment statement account: %v", err)
	}
	conflict := input
	conflict.StatementAccount = brokerage.Code
	if _, err := f.service.AddStatementAccountIdentity(ctx, conflict); err == nil {
		t.Fatal("same-realm source-system/external-id reuse unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "STATEMENT_ACCOUNT_IDENTITY_CONFLICT" {
		t.Fatalf("identity reuse error = %v, want STATEMENT_ACCOUNT_IDENTITY_CONFLICT", err)
	}
	registry := input
	registry.StatementAccount = brokerage.Code
	registry.SourceSystem = "ACCOUNT_REGISTRY"
	registry.ExternalID = "northstar-tradestation"
	registry.AccountNumber = "••9042"
	registry.Name = "TradeStation Brokerage"
	registry.Evidence.SourceKind = "ACCOUNT_REGISTRY_JSON"
	registry.Evidence.Locator = "accounts#northstar-tradestation"
	if _, err := f.service.AddStatementAccountIdentity(ctx, registry); err != nil {
		t.Fatalf("add registry brokerage alias: %v", err)
	}
	qboCash := input
	qboCash.SourceSystem = "QBO"
	qboCash.SourceRealm = "ACME_QBO"
	qboCash.ExternalID = "1150040000"
	qboCash.AccountNumber = "10100"
	qboCash.Name = "Acme operating"
	qboCash.Evidence.SourceKind = "QBO_OBJECT_DIRECTORY"
	qboCash.Evidence.SourcePath = "/evidence/acme/Account.json"
	qboCash.Evidence.SourceSHA256 = strings.Repeat("c", 64)
	qboCash.Evidence.Locator = "Account.json#1150040000"
	qboCash.Evidence.PayloadSHA256 = strings.Repeat("d", 64)
	if _, err := f.service.AddStatementAccountIdentity(ctx, qboCash); err != nil {
		t.Fatalf("add first realm-local QBO identity: %v", err)
	}
	qboBrokerage := qboCash
	qboBrokerage.StatementAccount = brokerage.Code
	qboBrokerage.SourceRealm = "NORTHSTAR_QBO"
	qboBrokerage.AccountNumber = "154"
	qboBrokerage.Name = "TradeStation"
	qboBrokerage.Evidence.SourcePath = "/evidence/northstar/Account.json"
	qboBrokerage.Evidence.SourceSHA256 = strings.Repeat("e", 64)
	qboBrokerage.Evidence.PayloadSHA256 = strings.Repeat("f", 64)
	if _, err := f.service.AddStatementAccountIdentity(ctx, qboBrokerage); err != nil {
		t.Fatalf("same QBO external id in a different realm should succeed: %v", err)
	}
	northstarQBO, err := f.service.ListStatementAccountIdentities(ctx, ledger.StatementAccountIdentityFilter{
		SourceSystem: "qbo", SourceRealm: "northstar_qbo",
	})
	if err != nil || len(northstarQBO) != 1 || northstarQBO[0].StatementAccount != brokerage.Code {
		t.Fatalf("Northstar QBO realm filter = %+v, %v", northstarQBO, err)
	}
	qboSameRealmConflict := qboCash
	qboSameRealmConflict.StatementAccount = brokerage.Code
	qboSameRealmConflict.AccountNumber = "154"
	qboSameRealmConflict.Name = "TradeStation"
	if _, err := f.service.AddStatementAccountIdentity(ctx, qboSameRealmConflict); err == nil {
		t.Fatal("same QBO external id in one realm unexpectedly remapped")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "STATEMENT_ACCOUNT_IDENTITY_CONFLICT" {
		t.Fatalf("same-realm QBO conflict = %v, want STATEMENT_ACCOUNT_IDENTITY_CONFLICT", err)
	}

	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_account_identities
		(id, statement_account_id, source_system, source_realm, external_id, account_number, account_name,
		 source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		 evidence_locator, evidence_payload_sha256, created_at, created_by)
		SELECT 'duplicate-identity', ?, source_system, source_realm, external_id, account_number, account_name,
		       source_active, evidence_source_kind, evidence_source_path, evidence_source_sha256,
		       evidence_locator, evidence_payload_sha256, created_at, created_by
		FROM statement_account_identities WHERE id = ?`, brokerage.ID, created.ID); err == nil {
		t.Fatal("direct duplicate source-system/source-realm/external-id mapping unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, "UPDATE statement_account_identities SET account_name = 'Changed' WHERE id = ?", created.ID); err == nil {
		t.Fatal("direct statement account identity update unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM statement_account_identities WHERE id = ?", created.ID); err == nil {
		t.Fatal("direct statement account identity delete unexpectedly succeeded")
	}
	columns, err := f.store.DB().QueryContext(ctx, "PRAGMA table_info(statement_accounts)")
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(columns)
	var foundReconciliationFrom, foundReconciliationThrough bool
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "source_system" || name == "external_id" || name == "active_from" || name == "active_through" {
			t.Fatalf("legacy identity column %q remains on statement_accounts", name)
		}
		foundReconciliationFrom = foundReconciliationFrom || name == "reconciliation_required_from"
		foundReconciliationThrough = foundReconciliationThrough || name == "reconciliation_required_through"
	}
	if err := columns.Err(); err != nil {
		t.Fatal(err)
	}
	if !foundReconciliationFrom || !foundReconciliationThrough {
		t.Fatal("statement_accounts is missing explicit reconciliation coverage bounds")
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after statement account identity checks: %v", err)
	}
}

func TestInvestmentStatementAccountRequiresAssetControl(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "BAD-INVESTMENT", Entity: "ACME", Book: "ACME", GLAccount: "4000",
		Name: "Bad Brokerage", Kind: "INVESTMENT", Currency: "USD", ReconciliationRequiredFrom: "2026-07-01",
	}); err == nil {
		t.Fatal("investment statement account with revenue control unexpectedly succeeded")
	}
	var entityID, bookID, revenueID string
	if err := f.store.DB().QueryRowContext(ctx, "SELECT id FROM entities WHERE code = 'ACME'").Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, "SELECT id FROM books WHERE code = 'ACME'").Scan(&bookID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, "SELECT id FROM accounts WHERE code = '4000'").Scan(&revenueID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO statement_accounts
		(id, code, entity_id, book_id, gl_account_id, name, account_kind, currency, reconciliation_required_from, created_at)
		VALUES ('bad-investment-direct', 'BAD-INVESTMENT-DIRECT', ?, ?, ?, 'Bad', 'INVESTMENT', 'USD', '2026-07-01', '2026-08-04T00:00:00Z')`,
		entityID, bookID, revenueID); err == nil {
		t.Fatal("direct-SQL INVESTMENT-to-REVENUE statement account unexpectedly succeeded")
	}
}
