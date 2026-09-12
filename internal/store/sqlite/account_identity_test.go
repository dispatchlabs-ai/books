package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func testAccountIdentityInput() ledger.AddAccountIdentityInput {
	return ledger.AddAccountIdentityInput{
		Entity: "acme", Account: "1000", SourceSystem: "qbo", ExternalID: "283",
		AccountNumber: "10100", Name: "Sample Bank Operating", Active: true,
		Evidence: ledger.AccountIdentityEvidence{
			SourceKind: "qbo_object_directory", SourcePath: "/evidence/Account.json",
			SourceSHA256: strings.Repeat("A", 64), Locator: "Account.json#/rows/4",
			PayloadSHA256: strings.Repeat("B", 64),
		},
	}
}

func TestAccountIdentityIsRealmScopedIdempotentAndImmutable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	input := testAccountIdentityInput()
	created, err := f.service.AddAccountIdentity(ctx, input)
	if err != nil {
		t.Fatalf("add external account identity: %v", err)
	}
	if created.EntityCode != "ACME" || created.AccountCode != "1000" || created.SourceSystem != "QBO" {
		t.Fatalf("identity was not normalized: %+v", created)
	}
	if created.Evidence.SourceSHA256 != strings.Repeat("a", 64) || created.Evidence.PayloadSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("identity evidence hashes were not normalized: %+v", created.Evidence)
	}
	repeated, err := f.service.AddAccountIdentity(ctx, input)
	if err != nil {
		t.Fatalf("repeat identical identity: %v", err)
	}
	if repeated.ID != created.ID {
		t.Fatalf("idempotent identity returned %s, want %s", repeated.ID, created.ID)
	}

	identities, err := f.service.ListAccountIdentities(ctx, ledger.AccountIdentityFilter{Entity: "acme", SourceSystem: "qbo"})
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(identities) != 1 || identities[0].ID != created.ID || identities[0].ExternalID != "283" {
		t.Fatalf("unexpected identity list: %+v", identities)
	}
	var auditCount int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events WHERE command = 'account identity add'").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("identity audit events = %d, want 1", auditCount)
	}

	conflict := input
	conflict.Name = "Changed Source Name"
	if _, err := f.service.AddAccountIdentity(ctx, conflict); err == nil {
		t.Fatal("changed external identity unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "ACCOUNT_IDENTITY_CONFLICT" {
		t.Fatalf("changed identity error = %v, want ACCOUNT_IDENTITY_CONFLICT", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "UPDATE account_identities SET account_name = 'Changed' WHERE id = ?", created.ID); err == nil {
		t.Fatal("direct external identity update unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM account_identities WHERE id = ?", created.ID); err == nil {
		t.Fatal("direct external identity delete unexpectedly succeeded")
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after identity checks: %v", err)
	}
}

func TestAccountIdentityRequiresEntityBookActivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	account, err := f.service.CreateAccount(ctx, ledger.CreateAccountInput{
		Code: "1999", Name: "Unconfigured", Type: "ASSET",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := testAccountIdentityInput()
	input.Account = account.Code
	if _, err := f.service.AddAccountIdentity(ctx, input); err == nil {
		t.Fatal("identity for account outside entity book unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "ACCOUNT_IDENTITY_ACCOUNT_NOT_ENABLED" {
		t.Fatalf("unconfigured identity error = %v, want ACCOUNT_IDENTITY_ACCOUNT_NOT_ENABLED", err)
	}

	var entityID string
	if err := f.store.DB().QueryRowContext(ctx, "SELECT id FROM entities WHERE code = 'ACME'").Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `INSERT INTO account_identities
		(id, entity_id, account_id, source_system, external_id, account_name, source_active,
		 evidence_source_kind, evidence_source_path, evidence_source_sha256, evidence_locator, created_at)
		VALUES ('direct-invalid', ?, ?, 'QBO', '999', 'Unconfigured', 1,
		 'QBO_OBJECT_DIRECTORY', '/evidence/Account.json', ?, 'Account.json#/rows/9', '2026-08-04T00:00:00Z')`,
		entityID, account.ID, strings.Repeat("c", 64)); err == nil {
		t.Fatal("direct identity for account outside entity book unexpectedly succeeded")
	}
}
