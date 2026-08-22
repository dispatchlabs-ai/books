package cli

import (
	"context"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestReconcileAbandonCLIRequiresCommitAfterValidatedPreview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := accountIdentityCLIDatabase(t)
	store, err := storesqlite.Open(ctx, path, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	account, err := service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Acme Cash", Kind: "BANK", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	reconciliation, err := service.StartReconciliation(ctx, account.Code, "2026-07-01", "2026-07-31", 0, 100)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	preview := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json",
		"reconcile", "abandon", "--id", reconciliation.ID, "--reason", "Wrong statement balance",
	)
	previewData := preview["data"].(map[string]any)
	if previewData["committed"] != false || previewData["dry_run"] != false {
		t.Fatalf("unexpected preview output: %#v", previewData)
	}
	previewReconciliation := previewData["reconciliation"].(map[string]any)
	if previewReconciliation["status"] != "ABANDONED" || previewReconciliation["abandon_reason"] != "Wrong statement balance" {
		t.Fatalf("unexpected reconciliation preview: %#v", previewReconciliation)
	}
	store, err = storesqlite.Open(ctx, path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	current, err := ledger.NewService(store, "test").ReconciliationStatus(ctx, reconciliation.ID)
	_ = store.Close()
	if err != nil || current.Status != "OPEN" {
		t.Fatalf("preview changed reconciliation: %+v, %v", current, err)
	}

	committed := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json",
		"reconcile", "abandon", "--id", reconciliation.ID, "--reason", "Wrong statement balance", "--commit",
	)
	committedData := committed["data"].(map[string]any)
	if committedData["committed"] != true || committedData["dry_run"] != false {
		t.Fatalf("unexpected committed output: %#v", committedData)
	}
	committedReconciliation := committedData["reconciliation"].(map[string]any)
	if committedReconciliation["status"] != "ABANDONED" || committedReconciliation["abandoned_at"] == "" || committedReconciliation["abandoned_by"] != "test" {
		t.Fatalf("incomplete committed evidence: %#v", committedReconciliation)
	}
}
