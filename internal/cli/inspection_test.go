package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type inspectionFixture struct {
	path             string
	batchID          string
	sourceRecordID   string
	reconciliationID string
	invalidJournalID string
}

func inspectionCLIDatabase(t *testing.T) inspectionFixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-07", StartDate: "2026-07-01", EndDate: "2026-07-31", FiscalYear: 2026, PeriodNumber: 7,
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "ACME", LegalName: "Acme, Inc.", Currency: "USD"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	for _, input := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"ACME"}, ActiveFrom: "2026-01-01"},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"ACME"}, ActiveFrom: "2026-01-01"},
	} {
		if _, err := service.CreateAccount(ctx, input); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	if _, err := service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
		Code: "ACME-CASH", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Acme Cash", Kind: "BANK", Currency: "USD",
		RequiredForClose: false, ReconciliationRequiredFrom: "2026-07-01",
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	importResult, err := service.ImportStatementTransactions(ctx, ledger.StatementImportInput{
		StatementAccount: "ACME-CASH", SourceSystem: "BANK", SourceName: "july.json",
		FileSHA256: strings.Repeat("a", 64),
		Transactions: []ledger.StatementTransactionInput{{
			ExternalID: "bank-1", PostedDate: "2026-07-05", Description: "Receipt",
			AmountCents: 1234, RawJSON: json.RawMessage(`{"id":"bank-1","secret":"preserved-only"}`),
		}},
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	records, err := service.ListSourceRecords(ctx, ledger.SourceRecordFilter{})
	if err != nil || len(records) != 1 {
		_ = store.Close()
		t.Fatalf("source records = %+v, %v", records, err)
	}
	reconciliation, err := service.StartReconciliation(ctx, "ACME-CASH", "2026-07-01", "2026-07-31", 0, 1234)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	invalidJournal, err := service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Unbalanced draft",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 1234}, {Account: "4000", CreditCents: 1000}},
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return inspectionFixture{
		path: path, batchID: importResult.BatchID, sourceRecordID: records[0].ID,
		reconciliationID: reconciliation.ID, invalidJournalID: invalidJournal.ID,
	}
}

func TestInspectionCommandsExposeOperationalMetadata(t *testing.T) {
	fixture := inspectionCLIDatabase(t)

	books := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "book", "list")
	bookRows := books["data"].([]any)
	if len(bookRows) != 1 || bookRows[0].(map[string]any)["code"] != "ACME" {
		t.Fatalf("books = %#v", bookRows)
	}

	accounts := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "account", "list", "--book", "acme")
	account := accounts["data"].([]any)[0].(map[string]any)
	if account["book"] != "ACME" || account["posting_enabled"] != true || account["active_from"] != "2026-01-01" {
		t.Fatalf("book account = %#v", account)
	}

	periods := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "period", "list", "--book", "acme")
	period := periods["data"].([]any)[0].(map[string]any)
	if period["book"] != "ACME" || period["book_status"] != "OPEN" {
		t.Fatalf("book period = %#v", period)
	}

	reconciliations := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "reconcile", "list", "--account", "acme-cash", "--status", "open")
	reconciliationRows := reconciliations["data"].([]any)
	if len(reconciliationRows) != 1 || reconciliationRows[0].(map[string]any)["id"] != fixture.reconciliationID {
		t.Fatalf("reconciliations = %#v", reconciliationRows)
	}

	batches := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "import-batch", "list", "--source-system", "bank")
	batchRows := batches["data"].([]any)
	if len(batchRows) != 1 || batchRows[0].(map[string]any)["id"] != fixture.batchID {
		t.Fatalf("import batches = %#v", batchRows)
	}
	batch := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "import-batch", "show", "--id", fixture.batchID)["data"].(map[string]any)
	if batch["file_sha256"] != strings.Repeat("a", 64) || batch["source_record_count"] != float64(1) {
		t.Fatalf("import batch = %#v", batch)
	}

	sourceEnvelope := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "source", "show", "--id", fixture.sourceRecordID)
	source := sourceEnvelope["data"].(map[string]any)
	if source["source_locator"] != "BANK:ACME-CASH:bank-1" || source["import_file_sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("source metadata = %#v", source)
	}
	if hash, ok := source["raw_json_sha256"].(string); !ok || len(hash) != 64 {
		t.Fatalf("raw JSON hash = %#v", source["raw_json_sha256"])
	}
	encoded, err := json.Marshal(sourceEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"raw_json":`)) || bytes.Contains(encoded, []byte("preserved-only")) {
		t.Fatalf("source show leaked raw JSON: %s", encoded)
	}
	sourceRows := executeJSONCommand(t, "--db", fixture.path, "--format", "json", "source", "list", "--history")["data"].([]any)
	if len(sourceRows) != 1 || sourceRows[0].(map[string]any)["revision"] != float64(1) ||
		sourceRows[0].(map[string]any)["current"] != true {
		t.Fatalf("source history rows = %#v", sourceRows)
	}
}

func TestJournalValidateRendersResultAndReturnsValidationError(t *testing.T) {
	fixture := inspectionCLIDatabase(t)
	root, _ := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--db", fixture.path, "--format", "json", "journal", "validate", "--id", fixture.invalidJournalID})
	err := root.Execute()
	appError, ok := apperr.As(err)
	if !ok || appError.Kind != apperr.Validation || appError.Code != "JOURNAL_INVALID" || ExitCode(err) != 4 {
		t.Fatalf("validation error = %#v", err)
	}
	var envelope map[string]any
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode rendered validation %q: %v", output.String(), decodeErr)
	}
	data := envelope["data"].(map[string]any)
	if data["valid"] != false || data["debit_cents"] != "12.34" || data["credit_cents"] != "10.00" {
		t.Fatalf("validation output = %#v", data)
	}
}

func TestReadOnlyClosePreviews(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-01", StartDate: "2026-01-01", EndDate: "2026-01-31", FiscalYear: 2026, PeriodNumber: 1,
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly, err := storesqlite.Open(ctx, path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(readOnly)
	result, err := ledger.NewService(readOnly, "test").ClosePeriod(ctx, "TESTCO", "2026-01", true)
	if err != nil || result.Closed || !result.ValidationOnly || len(result.Digest) != 64 {
		t.Fatalf("read-only close preview = %+v, %v", result, err)
	}
}

func TestInspectionCommandHelpSurface(t *testing.T) {
	t.Parallel()
	root, _ := newRootCommand()
	want := map[string]map[string]bool{
		"book":              {"list": true},
		"import-batch":      {"list": true, "show": true},
		"source":            {"list": true, "show": true},
		"reconcile":         {"list": true, "status": true},
		"statement-account": {"list": true, "archive": true, "identity": true, "lifecycle": true},
	}
	for _, command := range root.Commands() {
		children, ok := want[command.Name()]
		if !ok {
			continue
		}
		for _, child := range command.Commands() {
			delete(children, child.Name())
		}
	}
	for command, missing := range want {
		if len(missing) != 0 {
			t.Fatalf("%s missing subcommands: %#v", command, missing)
		}
	}
}

func TestStatementAccountArchiveRequiresCommitAndListsReason(t *testing.T) {
	path := accountIdentityCLIDatabase(t)
	created := executeJSONCommand(t, "--db", path, "--actor", "test", "--format", "json",
		"statement-account", "create", "--code", "acme-cash", "--entity", "acme",
		"--book", "acme", "--account", "1000", "--name", "Acme Cash", "--kind", "BANK",
		"--reconcile-from", "2026-01-01", "--required-for-close=false")
	createdData := created["data"].(map[string]any)
	if createdData["reconciliation_required_from"] != "2026-01-01" || createdData["status"] != "ACTIVE" {
		t.Fatalf("created statement account = %#v", createdData)
	}

	root, _ := newRootCommand()
	root.SetArgs([]string{"--db", path, "--format", "json", "statement-account", "archive",
		"--code", "acme-cash", "--reconcile-through", "2025-12-31", "--reason", "invalid boundary"})
	if err := root.Execute(); err == nil {
		t.Fatal("invalid archive preview unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "STATEMENT_ACCOUNT_ARCHIVE_DATE_INVALID" {
		t.Fatalf("invalid archive preview error = %v, want STATEMENT_ACCOUNT_ARCHIVE_DATE_INVALID", err)
	}

	preview := executeJSONCommand(t, "--db", path, "--format", "json", "statement-account", "archive",
		"--code", "acme-cash", "--reconcile-through", "2026-07-31", "--reason", "superseded account")
	previewData := preview["data"].(map[string]any)
	if previewData["committed"] != false || previewData["reconciliation_required_through"] != "2026-07-31" {
		t.Fatalf("archive preview = %#v", preview)
	}
	listed := executeJSONCommand(t, "--db", path, "--format", "json", "statement-account", "list")
	if listed["data"].([]any)[0].(map[string]any)["status"] != "ACTIVE" {
		t.Fatalf("preview changed account: %#v", listed)
	}

	archived := executeJSONCommand(t, "--db", path, "--actor", "auditor", "--format", "json",
		"statement-account", "archive", "--code", "acme-cash", "--reconcile-through", "2026-07-31",
		"--reason", "superseded account", "--commit")
	archivedData := archived["data"].(map[string]any)
	if archivedData["status"] != "ARCHIVED" || archivedData["reconciliation_required_from"] != "2026-01-01" ||
		archivedData["reconciliation_required_through"] != "2026-07-31" || archivedData["archive_reason"] != "superseded account" ||
		archivedData["archived_by"] != "auditor" {
		t.Fatalf("archived statement account = %#v", archivedData)
	}
	listed = executeJSONCommand(t, "--db", path, "--format", "json", "statement-account", "list")
	listedAccount := listed["data"].([]any)[0].(map[string]any)
	if listedAccount["status"] != "ARCHIVED" || listedAccount["reconciliation_required_from"] != "2026-01-01" ||
		listedAccount["reconciliation_required_through"] != "2026-07-31" || listedAccount["archive_reason"] != "superseded account" {
		t.Fatalf("archived list output = %#v", listedAccount)
	}
}

func TestUnsupportedStatementDryRunsFailBeforeInputOrDatabaseAccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "statement account create",
			args: []string{"--dry-run", "statement-account", "create", "--reconcile-from", "2026-01-01"},
		},
		{
			name: "statement import",
			args: []string{"--dry-run", "statement", "import", "--input", "/file-that-must-not-be-read.json"},
		},
		{
			name: "source link journal",
			args: []string{"--dry-run", "source", "link-journal", "--source-record", "source", "--journal", "journal"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _ := newRootCommand()
			root.SetArgs(test.args)
			err := root.Execute()
			appError, ok := apperr.As(err)
			if !ok || appError.Code != "DRY_RUN_UNSUPPORTED" {
				t.Fatalf("dry-run error = %v, want DRY_RUN_UNSUPPORTED", err)
			}
		})
	}
}

func TestStatementAccountReconciliationCoverageFlagsAreRequired(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		flag string
		args []string
	}{
		{
			name: "create", flag: "reconcile-from",
			args: []string{"--dry-run", "statement-account", "create", "--code", "CASH", "--entity", "ENTITY",
				"--book", "BOOK", "--account", "1000", "--name", "Cash", "--kind", "BANK"},
		},
		{
			name: "archive", flag: "reconcile-through",
			args: []string{"statement-account", "archive", "--code", "CASH", "--reason", "closed"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _ := newRootCommand()
			root.SetArgs(test.args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), test.flag) {
				t.Fatalf("missing --%s error = %v", test.flag, err)
			}
		})
	}
}

func TestYearCloseTableFormatsCents(t *testing.T) {
	path := yearCloseCLIDatabase(t)
	root, _ := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--db", path, "--actor", "test", "--dry-run",
		"period", "year-close", "--book", "testco", "--year", "2025", "--retained-earnings", "3900"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "60.00") {
		t.Fatalf("year-close table did not format cents: %q", output.String())
	}
}
