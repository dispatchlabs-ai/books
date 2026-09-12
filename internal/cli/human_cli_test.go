package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func executeHumanJSON(t *testing.T, args ...string) (map[string]any, []byte) {
	t.Helper()
	root, _ := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(append([]string{"--actor", "test", "--format", "json"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("books %s: %v\n%s", strings.Join(args, " "), err, output.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if envelope["ok"] != true {
		t.Fatalf("command did not return ok: %#v", envelope)
	}
	return envelope, output.Bytes()
}

func executeHumanFailure(t *testing.T, args ...string) error {
	t.Helper()
	root, _ := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(append([]string{"--actor", "test", "--format", "json"}, args...))
	err := root.Execute()
	if err == nil {
		t.Fatalf("books %s unexpectedly succeeded\n%s", strings.Join(args, " "), output.String())
	}
	return err
}

func setupHumanCLIHome(t *testing.T, start, chart string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", "")
	t.Setenv("BOOKS_DB", "")
	args := []string{"init", "--name", "Acme Services", "--company", "acme", "--start", start, "--chart", chart}
	executeHumanJSON(t, args...)
	return home
}

func TestHumanCompanyAccountAndTransactionJourney(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	configPath := filepath.Join(home, "books.toml")
	loaded, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.Resolve(configPath, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(resolved.Database); err != nil {
		t.Fatalf("company database: %v", err)
	}

	accountEnvelope, _ := executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	account := accountEnvelope["data"].(map[string]any)
	if account["code"] != "1000" || account["statement_account"] != "ACME-1000" {
		t.Fatalf("bank account = %#v", account)
	}
	executeHumanJSON(t, "config", "set", "defaults.payment-account", "checking")
	if err := executeHumanFailure(t, "config", "set", "defaults.payment-account", "General Expense"); err == nil {
		t.Fatal("expense account was accepted as the payment default")
	}
	executeHumanJSON(t, "config", "set", "output", "json")
	root, _ := newRootCommand()
	var defaultOutput bytes.Buffer
	root.SetOut(&defaultOutput)
	root.SetErr(&defaultOutput)
	root.SetArgs([]string{"--actor", "test", "accounts"})
	if err := root.Execute(); err != nil {
		t.Fatalf("config default output: %v", err)
	}
	var defaultEnvelope map[string]any
	if err := json.Unmarshal(defaultOutput.Bytes(), &defaultEnvelope); err != nil || defaultEnvelope["ok"] != true {
		t.Fatalf("configured JSON output = %q (%v)", defaultOutput.String(), err)
	}
	if bytes.Contains(defaultOutput.Bytes(), []byte(`"id"`)) {
		t.Fatalf("human account output leaked an internal id: %s", defaultOutput.String())
	}
	executeHumanJSON(t, "receive", "100.00", "Revenue", "Opening receipt", "--date", "2026-01-02", "--key", "receipt-1")
	_ = executeHumanFailure(t, "spend", "1.00", "General Expense", "--from", "Revenue", "--date", "2026-01-03")
	spendEnvelope, spendJSON := executeHumanJSON(t, "spend", "42.50", "General Expense", "Printer ink", "--date", "2026-01-03")
	spend := spendEnvelope["data"].(map[string]any)
	if spend["number"] != float64(2) || spend["status"] != "POSTED" || spend["total_debit_cents"] != "42.50" {
		t.Fatalf("spend = %#v", spend)
	}
	if bytes.Contains(spendJSON, []byte(`"id"`)) {
		t.Fatalf("human transaction output leaked an internal id: %s", spendJSON)
	}
	executeHumanJSON(t, "gl", "--from", "2026-01-01", "--to", "2026-01-31", "--account", "checking")
	if err := executeHumanFailure(t, "transfer", "1.00", "Revenue", "Checking", "--date", "2026-01-04"); err == nil {
		t.Fatal("income account was accepted as a transfer endpoint")
	}
	listEnvelope, _ := executeHumanJSON(t, "tx", "list")
	if transactions := listEnvelope["data"].([]any); len(transactions) != 2 {
		t.Fatalf("transactions = %#v", transactions)
	}
	executeHumanJSON(t, "spend", "1.00", "General Expense", "Preview", "--date", "2026-01-04", "--dry-run")
	listEnvelope, _ = executeHumanJSON(t, "tx", "list")
	if transactions := listEnvelope["data"].([]any); len(transactions) != 2 {
		t.Fatalf("dry-run created a transaction: %#v", transactions)
	}
	correctionPath := filepath.Join(home, "correction.json")
	correctionJSON := []byte(`{
  "posting_date": "2026-01-04",
  "description": "Corrected printer ink",
  "lines": [
    {"account": "General Expense", "debit": "40.00"},
    {"account": "Checking", "credit": "40.00"}
  ]
}`)
	if err := os.WriteFile(correctionPath, correctionJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	executeHumanJSON(t, "correct", "2", "--reason", "Receipt total was wrong", "--input", correctionPath)
	// The derived correction source key and active-reversal lookup make retries safe.
	executeHumanJSON(t, "correct", "2", "--reason", "Receipt total was wrong", "--input", correctionPath)
	listEnvelope, _ = executeHumanJSON(t, "tx", "list")
	if transactions := listEnvelope["data"].([]any); len(transactions) != 4 {
		t.Fatalf("correction retry duplicated activity: %#v", transactions)
	}
	draftEnvelope, _ := executeHumanJSON(t, "spend", "2.00", "General Expense", "Abandoned retry", "--date", "2026-01-05", "--key", "abandoned-key", "--draft")
	draftNumber := int(draftEnvelope["data"].(map[string]any)["number"].(float64))
	executeHumanJSON(t, "tx", "abandon", fmt.Sprint(draftNumber))
	executeHumanJSON(t, "tx", "abandon", fmt.Sprint(draftNumber))
	_ = executeHumanFailure(t, "spend", "2.00", "General Expense", "Abandoned retry", "--date", "2026-01-05", "--key", "abandoned-key")
	postDraftEnvelope, _ := executeHumanJSON(t, "receive", "3.00", "Revenue", "Post retry", "--date", "2026-01-06", "--key", "post-key", "--draft")
	postDraftNumber := int(postDraftEnvelope["data"].(map[string]any)["number"].(float64))
	executeHumanJSON(t, "tx", "post", fmt.Sprint(postDraftNumber))
	executeHumanJSON(t, "tx", "post", fmt.Sprint(postDraftNumber))

	loaded, err = booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	defaults := loaded.Companies["acme"].Defaults
	if defaults.PaymentAccount != "1000" || defaults.DepositAccount != "1000" {
		t.Fatalf("company defaults = %+v", defaults)
	}
}

func TestMultipleRegisteredCompaniesRemainIsolated(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "company", "add", "--name", "Beta LLC", "--company", "beta", "--start", "2026-01-01")
	companiesEnvelope, _ := executeHumanJSON(t, "companies")
	companies := companiesEnvelope["data"].([]any)
	if len(companies) != 2 {
		t.Fatalf("companies = %#v", companies)
	}
	executeHumanJSON(t, "company", "default", "beta")
	betaDashboard, _ := executeHumanJSON(t)
	if betaDashboard["data"].(map[string]any)["company"] != "beta" {
		t.Fatalf("default dashboard = %#v", betaDashboard)
	}
	acmeDashboard, _ := executeHumanJSON(t, "--company", "ACME")
	if acmeDashboard["data"].(map[string]any)["company"] != "acme" {
		t.Fatalf("selected dashboard = %#v", acmeDashboard)
	}
	for _, key := range []string{"acme", "beta"} {
		if _, err := os.Stat(filepath.Join(home, "companies", key, "ledger.sqlite")); err != nil {
			t.Fatalf("%s database: %v", key, err)
		}
	}
}

func TestManualReconciliationAndPeriodClosePlans(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "--date", "2026-01-02")
	executeHumanJSON(t, "spend", "42.50", "General Expense", "--date", "2026-01-03")

	reconciliationEnvelope, _ := executeHumanJSON(t, "reconcile", "plan", "checking", "--through", "2026-01-31", "--ending", "57.50")
	reconciliationOutput := reconciliationEnvelope["data"].(map[string]any)
	reconciliationPlanPath := reconciliationOutput["plan_path"].(string)
	if reconciliationPlanPath != filepath.Join(home, "companies", "acme", "plans", "reconcile-1000-2026-01-31.json") {
		t.Fatalf("reconciliation plan path = %q", reconciliationPlanPath)
	}
	applyEnvelope, _ := executeHumanJSON(t, "reconcile", "apply", "--plan", reconciliationPlanPath)
	if applyEnvelope["data"].(map[string]any)["status"] != "COMPLETED" {
		t.Fatalf("reconciliation apply = %#v", applyEnvelope)
	}
	// Applying the same immutable plan is idempotent.
	executeHumanJSON(t, "reconcile", "apply", "--plan", reconciliationPlanPath)

	closeEnvelope, _ := executeHumanJSON(t, "close", "plan", "2026-01")
	closePath := closeEnvelope["data"].(map[string]any)["plan_path"].(string)
	closedEnvelope, _ := executeHumanJSON(t, "close", "apply", "--plan", closePath)
	if closedEnvelope["data"].(map[string]any)["status"] != "CLOSED" {
		t.Fatalf("period close = %#v", closedEnvelope)
	}
	// A process retry observes the exact stored close evidence and succeeds.
	executeHumanJSON(t, "close", "apply", "--plan", closePath)
	executeHumanJSON(t, "doctor")
}

func TestYearClosePlanAndApply(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit", "--no-reconcile")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "--date", "2026-02-02")
	executeHumanJSON(t, "spend", "40.00", "General Expense", "--date", "2026-02-03")
	for month := 1; month <= 11; month++ {
		period := fmt.Sprintf("2026-%02d", month)
		executeHumanJSON(t, "period", "close", "--book", "ACME", "--period", period)
	}
	planEnvelope, _ := executeHumanJSON(t, "year-close", "plan", "2026")
	data := planEnvelope["data"].(map[string]any)
	plan := data["plan"].(map[string]any)
	if plan["net_income_cents"] != "60.00" {
		t.Fatalf("year close plan = %#v", plan)
	}
	applyEnvelope, _ := executeHumanJSON(t, "year-close", "apply", "--plan", data["plan_path"].(string))
	if applyEnvelope["data"].(map[string]any)["status"] != "POSTED" {
		t.Fatalf("year close apply = %#v", applyEnvelope)
	}
	// The source-bound closing journal makes an exact agent retry idempotent.
	executeHumanJSON(t, "year-close", "apply", "--plan", data["plan_path"].(string))
	loaded, err := booksconfig.Load(filepath.Join(home, "books.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.Resolve(filepath.Join(home, "books.toml"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storesqlite.Open(t.Context(), resolved.Database, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var closingJournals int
	if err := store.DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM journal_entries WHERE kind = 'CLOSING' AND status = 'POSTED'").Scan(&closingJournals); err != nil {
		t.Fatal(err)
	}
	if closingJournals != 1 {
		t.Fatalf("closing journals = %d, want 1", closingJournals)
	}
}

func TestRestoreRejectsAnotherRegisteredCompanyBackup(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	backup := filepath.Join(home, "acme.sqlite")
	executeHumanJSON(t, "backup", "--out", backup)
	executeHumanJSON(t, "company", "add", "--name", "Beta LLC", "--company", "beta", "--start", "2026-01-01")
	err := executeHumanFailure(t, "--company", "beta", "restore", "--from", backup, "--dry-run")
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "RESTORE_DATABASE_MISMATCH" {
		t.Fatalf("restore error = %#v", err)
	}
}

func TestRestoreMissingRegisteredDatabaseRequiresMatchingLineage(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	configPath := filepath.Join(home, "books.toml")
	loaded, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.Resolve(configPath, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Company.DatabaseUUID == "" {
		t.Fatal("new company config did not retain its database UUID")
	}

	originalBackup := filepath.Join(home, "original.sqlite")
	executeHumanJSON(t, "backup", "--out", originalBackup)

	imposterPath := filepath.Join(home, "imposter.sqlite")
	imposter, err := storesqlite.Init(t.Context(), imposterPath, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.NewService(imposter, "test").CreateEntity(t.Context(), ledger.CreateEntityInput{
		Code: "ACME", LegalName: "Unrelated Legal Entity LLC", Currency: "USD",
		BookCode: "ACME", BookName: "Unrelated Legal Entity LLC Actual", Basis: "ACCRUAL",
	}); err != nil {
		_ = imposter.Close()
		t.Fatal(err)
	}
	if err := imposter.Close(); err != nil {
		t.Fatal(err)
	}

	lostPath := filepath.Join(home, "lost-original.sqlite")
	if err := os.Rename(resolved.Database, lostPath); err != nil {
		t.Fatal(err)
	}
	mismatchErr := executeHumanFailure(t, "restore", "--from", imposterPath, "--confirm", "acme")
	requireApplicationCode(t, mismatchErr, "RESTORE_DATABASE_MISMATCH")
	if _, err := os.Stat(resolved.Database); !os.IsNotExist(err) {
		t.Fatalf("failed restore unexpectedly recreated target: %v", err)
	}
	directMismatchErr := executeHumanFailure(t, "db", "restore", "--from", imposterPath, "--confirm", resolved.Database)
	requireApplicationCode(t, directMismatchErr, "RESTORE_DATABASE_MISMATCH")
	if _, err := os.Stat(resolved.Database); !os.IsNotExist(err) {
		t.Fatalf("failed low-level restore unexpectedly recreated registered target: %v", err)
	}

	restoredEnvelope, _ := executeHumanJSON(t, "restore", "--from", originalBackup, "--confirm", "acme")
	restored := restoredEnvelope["data"].(map[string]any)
	if restored["source_database_uuid"] != resolved.Company.DatabaseUUID {
		t.Fatalf("restored database UUID = %v, want %s", restored["source_database_uuid"], resolved.Company.DatabaseUUID)
	}
	if restored["pre_restore_backup"] != "" {
		t.Fatalf("restore to missing target created a pre-restore backup: %#v", restored)
	}
	executeHumanJSON(t, "doctor")
}

func TestLegacyCompanyConfigBackfillsDatabaseLineage(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	configPath := filepath.Join(home, "books.toml")
	loaded, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := loaded.Companies["acme"].DatabaseUUID
	if expected == "" {
		t.Fatal("new company config did not retain its database UUID")
	}
	backupPath := filepath.Join(home, "legacy-backfill.sqlite")
	executeHumanJSON(t, "backup", "--out", backupPath)
	company := loaded.Companies["acme"]
	company.DatabaseUUID = ""
	loaded.Companies["acme"] = company
	if err := booksconfig.Save(configPath, loaded); err != nil {
		t.Fatal(err)
	}

	executeHumanJSON(t, "restore", "--from", backupPath, "--dry-run")
	previewed, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := previewed.Companies["acme"].DatabaseUUID; got != "" {
		t.Fatalf("restore dry-run mutated legacy database UUID to %q", got)
	}

	executeHumanJSON(t, "doctor")
	backfilled, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := backfilled.Companies["acme"].DatabaseUUID; got != expected {
		t.Fatalf("backfilled database UUID = %q, want %q", got, expected)
	}
}

func TestConcurrentIdenticalFiscalYearAddsConverge(t *testing.T) {
	setupHumanCLIHome(t, "2026-01-01", "empty")
	type outcome struct {
		created int
		err     error
		output  string
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			root, _ := newRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"--actor", "test", "--format", "json", "periods", "add", "2027"})
			if err := root.Execute(); err != nil {
				results <- outcome{err: err, output: output.String()}
				return
			}
			var envelope struct {
				Data struct {
					PeriodsCreated int `json:"periods_created"`
				} `json:"data"`
			}
			if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
				results <- outcome{err: err, output: output.String()}
				return
			}
			results <- outcome{created: envelope.Data.PeriodsCreated, output: output.String()}
		}()
	}
	close(start)
	created := make([]int, 0, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent periods add failed: %v\n%s", result.err, result.output)
		}
		created = append(created, result.created)
	}
	sort.Ints(created)
	if created[0] != 0 || created[1] != 12 {
		t.Fatalf("concurrent period creation counts = %v, want [0 12]", created)
	}
	periodsEnvelope, _ := executeHumanJSON(t, "periods")
	var fiscalYearPeriods int
	for _, value := range periodsEnvelope["data"].([]any) {
		if value.(map[string]any)["fiscal_year"] == float64(2027) {
			fiscalYearPeriods++
		}
	}
	if fiscalYearPeriods != 12 {
		t.Fatalf("2027 period count = %d, want 12", fiscalYearPeriods)
	}
	executeHumanJSON(t, "doctor")
}

func TestQuickBooksPlanAndApplyFromScratch(t *testing.T) {
	home := setupHumanCLIHome(t, "2023-01-01", "empty")
	testdata, err := filepath.Abs(filepath.Join("..", "importer", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	generalLedger := filepath.Join(testdata, "general_ledger.json")
	accounts := filepath.Join(testdata, "accounts.json")
	planEnvelope, _ := executeHumanJSON(t, "import", "quickbooks", "plan", "--from", generalLedger, "--accounts", accounts)
	planPath := planEnvelope["data"].(map[string]any)["plan_path"].(string)
	if !strings.HasPrefix(planPath, filepath.Join(home, "companies", "acme", "plans")) {
		t.Fatalf("QuickBooks plan path = %q", planPath)
	}
	applyEnvelope, _ := executeHumanJSON(t, "import", "quickbooks", "apply", "--plan", planPath)
	apply := applyEnvelope["data"].(map[string]any)
	if apply["status"] != "POSTED" || apply["accounts"] != float64(3) ||
		apply["statement_controls"] != float64(1) || apply["journals"] != float64(1) ||
		apply["journals_posted"] != float64(1) {
		t.Fatalf("QuickBooks apply = %#v", apply)
	}
	// Source keys and the import batch make an exact retry safe.
	executeHumanJSON(t, "import", "quickbooks", "apply", "--plan", planPath)

	configPath := filepath.Join(home, "books.toml")
	loaded, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.Resolve(configPath, "acme")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storesqlite.Open(t.Context(), resolved.Database, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	var posted, accountsImported, identities, statementControls int
	if err := store.DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM journal_entries WHERE status = 'POSTED'").Scan(&posted); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM accounts").Scan(&accountsImported); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM account_identities").Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM statement_accounts
		WHERE status = 'ACTIVE' AND required_for_close = 1 AND reconciliation_required_from = '2023-01-01'`).Scan(&statementControls); err != nil {
		t.Fatal(err)
	}
	if posted != 1 || accountsImported != 3 || identities != 3 || statementControls != 1 {
		t.Fatalf("posted=%d accounts=%d identities=%d statement_controls=%d", posted, accountsImported, identities, statementControls)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	err = executeHumanFailure(t, "close", "plan", "2023-01")
	requireApplicationCode(t, err, "PERIOD_CLOSE_BLOCKED")
}
