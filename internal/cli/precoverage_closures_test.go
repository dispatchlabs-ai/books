package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func writeTestEvidenceFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func precoverageCLIInput(t *testing.T, databasePath string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, databasePath, storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	account, err := service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-LEGACY-2276", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Legacy Example Bank 2276", Kind: "BANK", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	externalID := "provider-account-2276"
	if _, err := service.AddStatementAccountIdentity(ctx, ledger.AddStatementAccountIdentityInput{
		StatementAccount: account.Code, SourceSystem: "PROVIDER_API", SourceRealm: "GLOBAL",
		ExternalID: externalID, AccountNumber: "1234562276", Name: "Owner's Comp", Active: true,
		Evidence: ledger.StatementAccountIdentityEvidence{
			SourceKind: "PROVIDER_ACCOUNT_EXPORT", SourcePath: "/evidence/accounts.json",
			SourceSHA256: strings.Repeat("a", 64), Locator: "accounts[id=provider-account-2276]",
		},
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	closurePath := filepath.Join(directory, "closure.pdf")
	closureSHA := writeTestEvidenceFile(t, closurePath, []byte("formal provider closure evidence"))
	record := map[string]any{
		"id": externalID, "status": "archived", "currentBalance": 0,
		"availableBalance": 0, "legalBusinessName": "Acme, Inc.",
		"accountNumber": "1234562276", "name": "Owner's Comp",
	}
	canonicalRecord, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	payloadDigest := sha256.Sum256(canonicalRecord)
	payloadSHA := hex.EncodeToString(payloadDigest[:])
	snapshotDocument := map[string]any{"accounts": []any{record}, "page": map[string]any{"total": 1}}
	snapshotData, err := json.MarshalIndent(snapshotDocument, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(directory, "accounts.json")
	snapshotSHA := writeTestEvidenceFile(t, snapshotPath, snapshotData)
	input := map[string]any{
		"statement_account": account.Code,
		"identity": map[string]any{
			"source_system": "PROVIDER_API", "source_realm": "GLOBAL", "external_id": externalID,
		},
		"closed_on": "2025-09-19",
		"closure_evidence": map[string]any{
			"source_kind": "PROVIDER_CLOSURE_LETTER", "source_path": closurePath,
			"source_sha256": closureSHA, "locator": "page 1; id=provider-account-2276; account 1234562276; closed 2025-09-19",
		},
		"zero_evidence": map[string]any{
			"source_kind": "PROVIDER_ACCOUNT_SNAPSHOT", "source_path": snapshotPath,
			"source_sha256": snapshotSHA, "locator": "accounts[id=provider-account-2276]",
			"payload_sha256": payloadSHA, "observed_on": "2026-04-28", "provider_status": "ARCHIVED",
			"current_balance": "0.00", "available_balance": "0.00",
		},
		"account_holder": "Acme, Inc.", "account_suffix": "2276",
		"reason": "Provider closed the exact zero-balance account before required reconciliation coverage",
	}
	inputData, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(directory, "close-2276.json")
	writeTestEvidenceFile(t, inputPath, inputData)
	return inputPath, closurePath, externalID
}

func requirePrivateProviderIdentityRedacted(t *testing.T, label string, value any, externalID string) {
	t.Helper()
	serialized, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serialized, []byte(externalID)) || bytes.Contains(serialized, []byte("1234562276")) {
		t.Fatalf("%s exposed private provider identity: %s", label, serialized)
	}
}

func TestPrecoverageClosureCLIValidatesEvidenceCommitsAndRerunsIdempotently(t *testing.T) {
	t.Parallel()
	path := accountIdentityCLIDatabase(t)
	inputPath, _, externalID := precoverageCLIInput(t, path)
	baseArgs := []string{
		"--db", path, "--actor", "lifecycle-test", "--format", "json",
		"statement-account", "lifecycle", "close-before-coverage", "--input", inputPath,
	}
	preview := executeJSONCommand(t, baseArgs...)
	previewData := preview["data"].(map[string]any)
	if previewData["statement_account"] != "ACME-LEGACY-2276" || previewData["committed"] != false ||
		previewData["changed"] != true || previewData["coverage_disposition"] != ledger.PrecoverageClosureDisposition ||
		previewData["control_balance_at_closure_cents"] != "0.00" || previewData["current_control_balance_cents"] != "0.00" ||
		previewData["active_identity_count"] != float64(1) || len(previewData["active_identity_digest"].(string)) != 64 {
		t.Fatalf("unexpected lifecycle preview: %#v", previewData)
	}
	requirePrivateProviderIdentityRedacted(t, "lifecycle preview", preview, externalID)
	listedAccounts := executeJSONCommand(t, "--db", path, "--format", "json", "statement-account", "list")
	if listedAccounts["data"].([]any)[0].(map[string]any)["status"] != "ACTIVE" {
		t.Fatalf("preview changed statement account: %#v", listedAccounts)
	}
	committedArgs := append(append([]string{}, baseArgs...), "--commit")
	committed := executeJSONCommand(t, committedArgs...)
	committedData := committed["data"].(map[string]any)
	closureID, _ := committedData["id"].(string)
	if closureID == "" || committedData["committed"] != true || committedData["changed"] != true ||
		committedData["status"] != "ARCHIVED" || committedData["archived_by"] != "lifecycle-test" {
		t.Fatalf("unexpected lifecycle commit: %#v", committedData)
	}
	requirePrivateProviderIdentityRedacted(t, "lifecycle commit", committed, externalID)
	repeated := executeJSONCommand(t, committedArgs...)
	repeatedData := repeated["data"].(map[string]any)
	if repeatedData["id"] != closureID || repeatedData["changed"] != false || repeatedData["committed"] != true {
		t.Fatalf("idempotent lifecycle rerun = %#v", repeatedData)
	}
	requirePrivateProviderIdentityRedacted(t, "lifecycle retry", repeated, externalID)
	listed := executeJSONCommand(t, "--db", path, "--format", "json", "statement-account", "lifecycle", "list", "--statement-account", "ACME-LEGACY-2276")
	closures := listed["data"].([]any)
	if len(closures) != 1 || closures[0].(map[string]any)["id"] != closureID {
		t.Fatalf("listed lifecycle evidence = %#v", listed)
	}
	requirePrivateProviderIdentityRedacted(t, "lifecycle list", listed, externalID)
	store, err := storesqlite.Open(context.Background(), path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var lifecycleAudits int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM audit_events
		WHERE command = 'statement-account lifecycle close-before-coverage'`).Scan(&lifecycleAudits); err != nil {
		t.Fatal(err)
	}
	if lifecycleAudits != 1 {
		t.Fatalf("lifecycle audit events = %d, want 1", lifecycleAudits)
	}
}

func TestPrecoverageClosureCLIRejectsChangedEvidenceBytes(t *testing.T) {
	t.Parallel()
	path := accountIdentityCLIDatabase(t)
	inputPath, closurePath, _ := precoverageCLIInput(t, path)
	if err := os.WriteFile(closurePath, []byte("changed closure evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, _ := newRootCommand()
	root.SetArgs([]string{
		"--db", path, "--format", "json", "statement-account", "lifecycle",
		"close-before-coverage", "--input", inputPath,
	})
	err := root.Execute()
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "EVIDENCE_DIGEST_MISMATCH" {
		t.Fatalf("changed evidence error = %v, want EVIDENCE_DIGEST_MISMATCH", err)
	}
}

func TestPrecoverageClosureCLIRejectsRelativeInputPath(t *testing.T) {
	t.Parallel()
	root, _ := newRootCommand()
	root.SetArgs([]string{
		"--db", filepath.Join(t.TempDir(), "books.sqlite"), "--format", "json",
		"statement-account", "lifecycle", "close-before-coverage", "--input", "close-2276.json",
	})
	err := root.Execute()
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "INPUT_PATH_INVALID" {
		t.Fatalf("relative input error = %v, want INPUT_PATH_INVALID", err)
	}
}
