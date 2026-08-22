package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func registeredDatabase(t *testing.T, home, company string) string {
	t.Helper()
	configPath := filepath.Join(home, "books.toml")
	value, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := value.Resolve(configPath, company)
	if err != nil {
		t.Fatal(err)
	}
	return resolved.Database
}

func queryRegisteredInt(t *testing.T, home, query string, arguments ...any) int {
	t.Helper()
	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var result int
	if err := store.DB().QueryRowContext(t.Context(), query, arguments...).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requireApplicationCode(t *testing.T, err error, expected string) {
	t.Helper()
	applicationError, ok := apperr.As(err)
	if !ok || applicationError.Code != expected {
		t.Fatalf("error = %#v, want application code %s", err, expected)
	}
}

func executeContractFailure(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := executeArgs(args, &stdout, &stderr)
	if err == nil {
		t.Fatalf("books %s unexpectedly succeeded\nstdout: %s\nstderr: %s", strings.Join(args, " "), stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("books %s wrote a success stream on failure: %s", strings.Join(args, " "), stdout.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(stderr.Bytes()))
	var result map[string]any
	if decodeErr := decoder.Decode(&result); decodeErr != nil {
		t.Fatalf("decode error envelope %q: %v", stderr.String(), decodeErr)
	}
	var extra any
	if decodeErr := decoder.Decode(&extra); !errors.Is(decodeErr, io.EOF) {
		t.Fatalf("failure emitted more than one JSON value: %q (%v)", stderr.String(), decodeErr)
	}
	if result["schema"] != "books.cli/v1" || result["ok"] != false {
		t.Fatalf("failure envelope = %#v", result)
	}
	return result, err
}

func TestRegisteredCompanyCommandRejectsDatabaseOverride(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	registered := registeredDatabase(t, home, "acme")
	alternate := filepath.Join(home, "alternate.sqlite")
	data, err := os.ReadFile(registered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alternate, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOOKS_DB", alternate)

	err = executeHumanFailure(t, "spend", "1.00", "General Expense", "must not post", "--date", "2026-01-02")
	requireApplicationCode(t, err, "DATABASE_OVERRIDE_CONFLICT")
	if journals := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM journal_entries`); journals != 0 {
		t.Fatalf("registered company received %d journals through a database override", journals)
	}
	if journals := func() int {
		store, openErr := storesqlite.Open(t.Context(), alternate, storesqlite.ReadOnly)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
		var count int
		if queryErr := store.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM journal_entries`).Scan(&count); queryErr != nil {
			t.Fatal(queryErr)
		}
		return count
	}(); journals != 0 {
		t.Fatalf("override database received %d journals", journals)
	}
}

func TestConcurrentCompanyAddsRetainBothRegistrations(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	type result struct {
		name string
		err  error
		out  string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for _, company := range []struct{ key, name string }{{"beta", "Beta LLC"}, {"gamma", "Gamma LLC"}} {
		company := company
		wait.Add(1)
		go func() {
			defer wait.Done()
			root, _ := newRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"--actor", "test", "--format", "json", "company", "add", "--name", company.name, "--company", company.key, "--start", "2026-01-01"})
			<-start
			results <- result{name: company.key, err: root.Execute(), out: output.String()}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("company %s: %v\n%s", result.name, result.err, result.out)
		}
	}
	configPath := filepath.Join(home, "books.toml")
	value, err := booksconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, company := range []string{"acme", "beta", "gamma"} {
		if _, ok := value.Companies[company]; !ok {
			t.Fatalf("company %s was lost from concurrent update: %#v", company, value.Companies)
		}
		if _, err := os.Stat(filepath.Join(home, "companies", company, "ledger.sqlite")); err != nil {
			t.Fatalf("company %s database: %v", company, err)
		}
	}
}

func TestConcurrentAutomaticAccountCodesRetryWithoutLosingEitherAccount(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "empty")
	type result struct {
		name string
		err  error
		out  string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for _, name := range []string{"Rent", "Utilities"} {
		name := name
		wait.Add(1)
		go func() {
			defer wait.Done()
			root, _ := newRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"--actor", "test", "--format", "json", "account", "add", "expense", name})
			<-start
			results <- result{name: name, err: root.Execute(), out: output.String()}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("account %s: %v\n%s", result.name, result.err, result.out)
		}
	}
	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	rows, err := store.DB().QueryContext(t.Context(), `SELECT code, name FROM accounts ORDER BY code`)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	got := map[string]string{}
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			t.Fatal(err)
		}
		got[code] = name
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["5000"] == "" || got["5010"] == "" || got["5000"] == got["5010"] {
		t.Fatalf("concurrent automatic account allocation = %#v, want Rent and Utilities at 5000/5010", got)
	}
}

func TestAccountAddRollsBackGLWhenStatementAccountIsInvalid(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	err := executeHumanFailure(t, "account", "add", "bank", "Foreign Bank", "--currency", "EUR")
	requireApplicationCode(t, err, "CURRENCY_NOT_SUPPORTED")
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM accounts WHERE name = 'Foreign Bank'`); count != 0 {
		t.Fatalf("failed account add retained %d GL accounts", count)
	}

	envelope, _ := executeHumanJSON(t, "account", "add", "bank", "Foreign Bank", "--currency", "USD")
	if code := envelope["data"].(map[string]any)["code"]; code != "1000" {
		t.Fatalf("successful retry code = %v, want 1000", code)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM accounts WHERE name = 'Foreign Bank'`); count != 1 {
		t.Fatalf("successful retry created %d GL accounts", count)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM statement_accounts WHERE name = 'Foreign Bank'`); count != 1 {
		t.Fatalf("successful retry created %d statement accounts", count)
	}
}

func TestAccountAndStatementAccountShareOneServiceTransaction(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err := service.CreateAccount(t.Context(), ledger.CreateAccountInput{
		Code: "1999", Name: "Existing Control", Type: "ASSET", Subtype: "BANK", StatementSection: "BALANCE_SHEET",
		BookCodes: []string{"ACME"}, ActiveFrom: "2026-01-01",
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := service.CreateStatementAccount(t.Context(), ledger.CreateStatementAccountInput{
		Code: "ACME-1000", Entity: "ACME", Book: "ACME", GLAccount: "1999", Name: "Existing Control",
		Kind: "BANK", Currency: "USD", RequiredForClose: true, ReconciliationRequiredFrom: "2026-01-01",
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	err = executeHumanFailure(t, "account", "add", "bank", "Must Roll Back", "--code", "1000")
	if err == nil {
		t.Fatal("statement-account collision unexpectedly succeeded")
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM accounts WHERE code = '1000'`); count != 0 {
		t.Fatalf("statement-account failure retained %d GL accounts", count)
	}
}

func TestManualReconciliationRejectsBoundaryStalenessWithoutWrites(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-deposit", "--reconcile-from", "2026-02-01")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "opening", "--date", "2026-01-02")
	executeHumanJSON(t, "receive", "20.00", "Revenue", "february", "--date", "2026-02-05")
	planEnvelope, _ := executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-02-28", "--ending", "120.00")
	planPath := planEnvelope["data"].(map[string]any)["plan_path"].(string)

	// This changes both ledger boundaries without changing the in-range control lines.
	executeHumanJSON(t, "receive", "10.00", "Revenue", "late January", "--date", "2026-01-15")
	err := executeHumanFailure(t, "reconcile", "apply", "--plan", planPath)
	requireApplicationCode(t, err, "RECONCILIATION_PLAN_STALE")

	checks := map[string]string{
		"statement transactions": `SELECT COUNT(*) FROM statement_transactions`,
		"reconciliations":        `SELECT COUNT(*) FROM reconciliations`,
		"allocations":            `SELECT COUNT(*) FROM reconciliation_allocations`,
		"manual import batches":  `SELECT COUNT(*) FROM import_batches WHERE source_system = 'MANUAL_RECONCILIATION'`,
	}
	for name, query := range checks {
		if count := queryRegisteredInt(t, home, query); count != 0 {
			t.Fatalf("stale apply retained %d %s", count, name)
		}
	}
}

func TestManualReconciliationCarriesOutstandingItemsIntoLaterStatements(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	executeHumanJSON(t, "receive", "1000.00", "Owner Equity", "Opening capital", "--to", "Checking", "--date", "2026-01-01")
	executeHumanJSON(t, "spend", "100.00", "General Expense", "Outstanding check", "--from", "Checking", "--date", "2026-01-31")

	janPlanEnvelope, _ := executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-01-31", "--ending", "1000.00", "--cleared", "1")
	janOutput := janPlanEnvelope["data"].(map[string]any)
	janPlan := janOutput["plan"].(map[string]any)
	if janPlan["ending_outstanding_cents"] != "-100.00" || janPlan["adjusted_ending_cents"] != "900.00" ||
		len(janPlan["cleared"].([]any)) != 1 || len(janPlan["outstanding"].([]any)) != 1 {
		t.Fatalf("January outstanding-item plan = %#v", janPlan)
	}
	executeHumanJSON(t, "reconcile", "apply", "--plan", janOutput["plan_path"].(string))
	janClose := filepath.Join(home, "jan-close.json")
	executeHumanJSON(t, "close", "plan", "2026-01", "--out", janClose)
	executeHumanJSON(t, "close", "apply", "--plan", janClose)

	febPlanEnvelope, _ := executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-02-28", "--ending", "900.00", "--cleared", "2")
	febOutput := febPlanEnvelope["data"].(map[string]any)
	febPlan := febOutput["plan"].(map[string]any)
	if febPlan["opening_outstanding_cents"] != "-100.00" || febPlan["ending_outstanding_cents"] != "0.00" ||
		febPlan["adjusted_beginning_cents"] != "900.00" || len(febPlan["cleared"].([]any)) != 1 {
		t.Fatalf("February cleared-outstanding plan = %#v", febPlan)
	}
	executeHumanJSON(t, "reconcile", "apply", "--plan", febOutput["plan_path"].(string))
	doctor, _ := executeHumanJSON(t, "doctor")
	if doctor["data"].(map[string]any)["ok"] != true {
		t.Fatalf("doctor after outstanding-item carry-forward = %#v", doctor)
	}

	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM reconciliations WHERE status = 'COMPLETED'`); count != 2 {
		t.Fatalf("completed reconciliations = %d, want 2", count)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM reconciliation_outstanding_items`); count != 1 {
		t.Fatalf("immutable reviewed outstanding items = %d, want the January evidence row", count)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM source_record_operator_attestations`); count != 2 {
		t.Fatalf("durable operator-attestation markers = %d, want 2", count)
	}
	sourceEnvelope, _ := executeHumanJSON(t, "source", "list", "--account", "ACME-1000", "--from", "2026-01-01", "--to", "2026-02-28")
	sources := sourceEnvelope["data"].([]any)
	if len(sources) != 2 {
		t.Fatalf("manual reconciliation source rows = %#v", sources)
	}
	for _, raw := range sources {
		source := raw.(map[string]any)
		if source["source_system"] != "MANUAL_RECONCILIATION" || source["observation_kind"] != "OPERATOR_ATTESTATION" {
			t.Fatalf("manual reconciliation provenance = %#v", source)
		}
	}
}

func TestFailedDefaultPostIsAtomicAndReopenedReconciliationCanBeReplanned(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	executeHumanJSON(t, "receive", "100.00", "Owner Equity", "Opening capital", "--to", "Checking", "--date", "2026-01-01")
	reconcilePath := filepath.Join(home, "initial-reconciliation.json")
	executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-01-31", "--ending", "100.00", "--cleared", "all", "--out", reconcilePath)
	executeHumanJSON(t, "reconcile", "apply", "--plan", reconcilePath)
	closePath := filepath.Join(home, "initial-close.json")
	executeHumanJSON(t, "close", "plan", "2026-01", "--out", closePath)
	executeHumanJSON(t, "close", "apply", "--plan", closePath)

	reconciliations, _ := executeHumanJSON(t, "reconcile", "list")
	reconciliationID := reconciliations["data"].([]any)[0].(map[string]any)["id"].(string)
	executeHumanJSON(t, "reopen", "2026-01", "--reason", "late bank fee")
	err := executeHumanFailure(t, "spend", "1.00", "General Expense", "Late bank fee", "--from", "Checking", "--date", "2026-01-31", "--key", "late-fee")
	requireApplicationCode(t, err, "JOURNAL_INVALID")
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM journal_entries WHERE status = 'DRAFT'`); count != 0 {
		t.Fatalf("failed default-post command retained %d drafts", count)
	}

	executeHumanJSON(t, "reconcile", "reopen", "--id", reconciliationID, "--reason", "include late bank fee")
	posted, _ := executeHumanJSON(t, "spend", "1.00", "General Expense", "Late bank fee", "--from", "Checking", "--date", "2026-01-31", "--key", "late-fee")
	if posted["data"].(map[string]any)["status"] != "POSTED" {
		t.Fatalf("retry after reopening reconciliation = %#v", posted)
	}
	replanPath := filepath.Join(home, "replanned-reconciliation.json")
	replanEnvelope, _ := executeHumanJSON(t, "reconcile", "replan", reconciliationID, "--ending", "99.00", "--cleared", "all", "--out", replanPath)
	replan := replanEnvelope["data"].(map[string]any)["plan"].(map[string]any)
	if replan["target_reconciliation_id"] != reconciliationID || replan["ending_balance_cents"] != "99.00" {
		t.Fatalf("reopened reconciliation replan = %#v", replan)
	}
	executeHumanJSON(t, "reconcile", "apply", "--plan", replanPath)
	// The exact replan remains safe to retry after the target is completed again.
	executeHumanJSON(t, "reconcile", "apply", "--plan", replanPath)
	reclosePath := filepath.Join(home, "reclose.json")
	executeHumanJSON(t, "close", "plan", "2026-01", "--out", reclosePath)
	executeHumanJSON(t, "close", "apply", "--plan", reclosePath)
	executeHumanJSON(t, "doctor")
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM journal_entries WHERE status = 'POSTED'`); count != 2 {
		t.Fatalf("posted journals after recovery = %d, want 2", count)
	}
}

func TestReopenedConvertedReconciliationReviewsActivityBeforeCurrentCoverage(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err := service.CreateAccount(t.Context(), ledger.CreateAccountInput{
		Code: "1000", Name: "Converted Checking", Type: "ASSET", Subtype: "BANK",
		StatementSection: "BALANCE_SHEET", BookCodes: []string{"ACME"}, ActiveFrom: "2026-01-01",
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	statementAccount, err := service.CreateStatementAccount(t.Context(), ledger.CreateStatementAccountInput{
		Code: "ACME-CONVERTED", Entity: "ACME", Book: "ACME", GLAccount: "1000",
		Name: "Converted Checking", Kind: "BANK", Currency: "USD", RequiredForClose: true,
		ReconciliationRequiredFrom: "2026-07-01",
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	post := func(date, description string, lines []ledger.JournalLineInput) ledger.Journal {
		t.Helper()
		journal, createErr := service.CreateJournal(t.Context(), ledger.CreateJournalInput{
			Book: "ACME", PostingDate: date, Period: date[:7], Description: description, Lines: lines,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		journal, postErr := service.PostJournal(t.Context(), journal.ID)
		if postErr != nil {
			t.Fatal(postErr)
		}
		return journal
	}
	opening := post("2026-01-15", "Converted opening activity", []ledger.JournalLineInput{
		{Account: "1000", DebitCents: 100},
		{Account: "3000", CreditCents: 100},
	})
	var openingControlLineID string
	var openingControlLineNumber int
	for _, line := range opening.Lines {
		if line.AccountCode == "1000" {
			openingControlLineID = line.ID
			openingControlLineNumber = line.LineNumber
		}
	}
	if openingControlLineID == "" {
		_ = store.Close()
		t.Fatal("opening journal is missing its statement control line")
	}
	planDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rawEvidence, err := json.Marshal(map[string]any{
		"plan_digest": planDigest, "transaction_number": opening.EntryNumber,
		"line_number": openingControlLineNumber, "ledger_date": "2026-01-15",
		"statement_date": "2026-01-15", "provenance": "OPERATOR_ATTESTATION",
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	openingLine := ledger.ManualReconciliationLine{
		JournalLineID: openingControlLineID,
		ExternalID: fmt.Sprintf("reconcile:%s:%d:%d", strings.ToLower(statementAccount.Code),
			opening.EntryNumber, openingControlLineNumber),
		LedgerDate: "2026-01-15", StatementDate: "2026-01-15",
		Description: "Converted opening activity", AmountCents: 100,
		RawJSON: rawEvidence,
	}
	reconciliation, err := service.ApplyManualReconciliation(t.Context(), ledger.ManualReconciliationInput{
		StatementAccount:                  statementAccount.Code,
		SourceName:                        "converted-january.json",
		PlanDigest:                        planDigest,
		StartDate:                         "2026-01-01",
		EndDate:                           "2026-01-31",
		BeginningBalanceCents:             0,
		EndingBalanceCents:                100,
		ExpectedLedgerBeginningCents:      0,
		ExpectedLedgerEndingCents:         100,
		ExpectedStatementTransactionCount: 0,
		ExpectedLines:                     []ledger.ManualReconciliationLine{openingLine},
		Lines:                             []ledger.ManualReconciliationLine{openingLine},
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("create converted reconciliation fixture: %v", err)
	}
	if err := service.ReopenReconciliation(t.Context(), reconciliation.ID, "review converted history"); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	post("2026-01-20", "Converted precoverage deposit", []ledger.JournalLineInput{
		{Account: "1000", DebitCents: 50},
		{Account: "3000", CreditCents: 50},
	})
	post("2026-01-21", "Converted precoverage payment", []ledger.JournalLineInput{
		{Account: "5000", DebitCents: 50},
		{Account: "1000", CreditCents: 50},
	})
	if _, err := service.CompleteReconciliation(t.Context(), reconciliation.ID, false); err == nil {
		_ = store.Close()
		t.Fatal("reopened reconciliation completed without reviewing new precoverage control lines")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	planPath := filepath.Join(home, "converted-reconciliation.json")
	planEnvelope, _ := executeHumanJSON(t, "reconcile", "replan", reconciliation.ID,
		"--ending", "1.00", "--cleared", "1", "--out", planPath)
	plan := planEnvelope["data"].(map[string]any)["plan"].(map[string]any)
	if candidates := plan["candidates"].([]any); len(candidates) != 3 {
		t.Fatalf("converted reconciliation candidates = %#v, want all three historical control lines", candidates)
	}
	if cleared := plan["cleared"].([]any); len(cleared) != 1 {
		t.Fatalf("converted reconciliation cleared lines = %#v, want the retained statement line", cleared)
	}
	if outstanding := plan["outstanding"].([]any); len(outstanding) != 2 {
		t.Fatalf("converted reconciliation outstanding lines = %#v, want both new reviewed lines", outstanding)
	}
	executeHumanJSON(t, "reconcile", "apply", "--plan", planPath)
	executeHumanJSON(t, "doctor")
	executeHumanJSON(t, "audit", "verify")
}

func TestHistoricalReconciliationCorrectionCascadesThroughLaterManualInterval(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-payment", "--default-deposit")
	executeHumanJSON(t, "receive", "100.00", "Owner Equity", "Opening capital", "--date", "2026-01-02")

	janPlan := filepath.Join(home, "jan-original.json")
	executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-01-31", "--ending", "100.00", "--out", janPlan)
	executeHumanJSON(t, "reconcile", "apply", "--plan", janPlan)
	executeHumanJSON(t, "spend", "10.00", "General Expense", "February payment", "--date", "2026-02-05")
	febPlan := filepath.Join(home, "feb-original.json")
	executeHumanJSON(t, "reconcile", "plan", "Checking", "--through", "2026-02-28", "--ending", "90.00", "--out", febPlan)
	executeHumanJSON(t, "reconcile", "apply", "--plan", febPlan)

	for _, period := range []string{"2026-01", "2026-02"} {
		planPath := filepath.Join(home, "close-"+period+".json")
		executeHumanJSON(t, "close", "plan", period, "--out", planPath)
		executeHumanJSON(t, "close", "apply", "--plan", planPath)
	}
	reconciliations, _ := executeHumanJSON(t, "reconcile", "list")
	rows := reconciliations["data"].([]any)
	janID := rows[0].(map[string]any)["id"].(string)
	febID := rows[1].(map[string]any)["id"].(string)

	// Reopen successor controls first, then revise the historical interval and
	// carry its new statement ending into the successor's opening balance.
	err := executeHumanFailure(t, "reopen", "2026-01", "--reason", "out-of-order probe")
	requireApplicationCode(t, err, "PERIOD_REOPEN_ORDER")
	executeHumanJSON(t, "reopen", "2026-02", "--reason", "cascade historical correction")
	executeHumanJSON(t, "reopen", "2026-01", "--reason", "record late January fee")
	err = executeHumanFailure(t, "reconcile", "reopen", "--id", janID, "--reason", "out-of-order probe")
	requireApplicationCode(t, err, "RECONCILIATION_REOPEN_ORDER")
	executeHumanJSON(t, "reconcile", "reopen", "--id", febID, "--reason", "cascade historical correction")
	executeHumanJSON(t, "reconcile", "reopen", "--id", janID, "--reason", "record late January fee")
	executeHumanJSON(t, "spend", "1.00", "General Expense", "Late January fee", "--date", "2026-01-31")

	janReplanPath := filepath.Join(home, "jan-replanned.json")
	executeHumanJSON(t, "reconcile", "replan", janID, "--ending", "99.00", "--out", janReplanPath)
	executeHumanJSON(t, "reconcile", "apply", "--plan", janReplanPath)
	febReplanPath := filepath.Join(home, "feb-replanned.json")
	febReplanEnvelope, _ := executeHumanJSON(t, "reconcile", "replan", febID, "--ending", "89.00", "--out", febReplanPath)
	febReplan := febReplanEnvelope["data"].(map[string]any)["plan"].(map[string]any)
	if febReplan["beginning_balance_cents"] != "99.00" || febReplan["target_prior_beginning_cents"] != "100.00" {
		t.Fatalf("cascaded February replan = %#v", febReplan)
	}
	executeHumanJSON(t, "reconcile", "apply", "--plan", febReplanPath)
	executeHumanJSON(t, "reconcile", "apply", "--plan", febReplanPath)

	for _, period := range []string{"2026-01", "2026-02"} {
		planPath := filepath.Join(home, "reclose-"+period+".json")
		executeHumanJSON(t, "close", "plan", period, "--out", planPath)
		executeHumanJSON(t, "close", "apply", "--plan", planPath)
	}
	executeHumanJSON(t, "doctor")
	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var beginning, ending int64
	if err := store.DB().QueryRowContext(t.Context(), `SELECT beginning_balance_cents, ending_balance_cents
		FROM reconciliations WHERE id = ?`, febID).Scan(&beginning, &ending); err != nil {
		t.Fatal(err)
	}
	if beginning != 9_900 || ending != 8_900 {
		t.Fatalf("cascaded February balances = %d/%d, want 9900/8900", beginning, ending)
	}
}

func TestCashBasisIsRejectedAndAccrualReportsDeclareTheirBasis(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", "")
	t.Setenv("BOOKS_DB", "")
	err := executeHumanFailure(t, "init", "--name", "Cash Basis Co", "--company", "cashco", "--basis", "cash", "--start", "2026-01-01")
	requireApplicationCode(t, err, "BASIS_NOT_SUPPORTED")
	if _, statErr := os.Stat(filepath.Join(home, "books.toml")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected cash-basis initialization created config state: %v", statErr)
	}

	executeHumanJSON(t, "init", "--name", "Accrual Co", "--company", "accrual", "--start", "2026-01-01")
	profitLoss, _ := executeHumanJSON(t, "pl", "--from", "2026-01-01", "--to", "2026-01-31")
	scope := profitLoss["data"].(map[string]any)["scope"].(map[string]any)
	if scope["basis"] != "ACCRUAL" {
		t.Fatalf("financial report scope omitted accrual basis: %#v", scope)
	}
}

func TestYearClosePlanRejectsOpenPrerequisitePeriodsWithoutDrafts(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Cash", "--no-reconcile", "--default-deposit")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "Annual revenue", "--to", "Cash", "--date", "2026-01-15")
	planPath := filepath.Join(home, "too-early-year-close.json")
	err := executeHumanFailure(t, "year-close", "plan", "2026", "--out", planPath)
	requireApplicationCode(t, err, "FISCAL_YEAR_CLOSE_BLOCKED")
	if _, statErr := os.Stat(planPath); !os.IsNotExist(statErr) {
		t.Fatalf("blocked year-close plan was written: %v", statErr)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM journal_entries WHERE kind = 'CLOSING'`); count != 0 {
		t.Fatalf("blocked year close retained %d closing journals", count)
	}
}

func TestCloseApplyRejectsStalePlanBeforeClosing(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-deposit", "--no-reconcile")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "first", "--date", "2026-01-02")
	planEnvelope, _ := executeHumanJSON(t, "close", "plan", "2026-01")
	planPath := planEnvelope["data"].(map[string]any)["plan_path"].(string)
	executeHumanJSON(t, "receive", "10.00", "Revenue", "after review", "--date", "2026-01-03")
	var plan periodClosePlan
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	writeStore, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	_, serviceErr := ledger.NewService(writeStore, "test").ClosePeriodFromPlan(t.Context(), plan.Book, plan.Period, plan.EndDate, plan.LedgerDigest)
	if err := writeStore.Close(); err != nil {
		t.Fatal(err)
	}
	requireApplicationCode(t, serviceErr, "CLOSE_PLAN_STALE")

	err = executeHumanFailure(t, "close", "apply", "--plan", planPath)
	requireApplicationCode(t, err, "CLOSE_PLAN_STALE")

	store, err := storesqlite.Open(t.Context(), registeredDatabase(t, home, "acme"), storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var status string
	var closeDigest *string
	if err := store.DB().QueryRowContext(t.Context(), `SELECT bp.status, bp.close_digest
		FROM book_periods bp JOIN books b ON b.id = bp.book_id JOIN fiscal_periods fp ON fp.id = bp.period_id
		WHERE b.code = 'ACME' AND fp.code = '2026-01'`).Scan(&status, &closeDigest); err != nil {
		t.Fatal(err)
	}
	if status != "OPEN" || closeDigest != nil {
		t.Fatalf("stale close state = status %s digest %v", status, closeDigest)
	}
}

func TestRegeneratedQuickBooksPlanReusesDeterministicBatch(t *testing.T) {
	home := setupHumanCLIHome(t, "2023-01-01", "empty")
	testdata, err := filepath.Abs(filepath.Join("..", "importer", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	generalLedger := filepath.Join(testdata, "general_ledger.json")
	accounts := filepath.Join(testdata, "accounts.json")
	planA := filepath.Join(home, "plan-a.json")
	planB := filepath.Join(home, "plan-b.json")
	executeHumanJSON(t, "import", "quickbooks", "plan", "--from", generalLedger, "--accounts", accounts, "--out", planA)
	executeHumanJSON(t, "import", "quickbooks", "apply", "--plan", planA, "--draft")
	executeHumanJSON(t, "import", "quickbooks", "plan", "--from", generalLedger, "--accounts", accounts, "--out", planB)
	postedEnvelope, _ := executeHumanJSON(t, "import", "quickbooks", "apply", "--plan", planB)
	if status := postedEnvelope["data"].(map[string]any)["status"]; status != "POSTED" {
		t.Fatalf("regenerated apply status = %v", status)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM import_batches WHERE source_system = 'QBO'`); count != 1 {
		t.Fatalf("identical import created %d QBO batches", count)
	}
	if count := queryRegisteredInt(t, home, `SELECT COUNT(*) FROM journal_entries WHERE source_system = 'QBO' AND status = 'POSTED'`); count != 1 {
		t.Fatalf("identical import left %d posted QBO journals", count)
	}
}

func TestEarlyCobraFailuresHonorJSONContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", "")
	t.Setenv("BOOKS_DB", "")
	tests := []struct {
		name   string
		args   []string
		code   string
		detail string
	}{
		{name: "unknown command", args: []string{"--json", "definitely-not-a-command"}, code: "COMMAND_NOT_FOUND", detail: `unknown command "definitely-not-a-command"`},
		{name: "missing argument", args: []string{"--json", "spend", "10.00"}, code: "ARGUMENT_INVALID", detail: "requires at least 2 arg(s), only received 1"},
		{name: "unknown flag", args: []string{"--json", "spend", "1.00", "5000", "--badflag"}, code: "FLAG_INVALID", detail: "unknown flag: --badflag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := executeContractFailure(t, test.args...)
			requireApplicationCode(t, err, test.code)
			body := envelope["error"].(map[string]any)
			if body["code"] != test.code {
				t.Fatalf("error envelope code = %v, want %s", body["code"], test.code)
			}
			if message, _ := body["message"].(string); !strings.Contains(message, test.detail) {
				t.Fatalf("error envelope message = %q, want detail %q", message, test.detail)
			}
			if ExitCode(err) != 2 {
				t.Fatalf("exit code = %d, want 2", ExitCode(err))
			}
		})
	}

	configured := booksconfig.New()
	configured.Defaults.Output = "json"
	if err := booksconfig.Save(filepath.Join(home, "books.toml"), configured); err != nil {
		t.Fatal(err)
	}
	envelope, err := executeContractFailure(t, "definitely-not-a-command")
	requireApplicationCode(t, err, "COMMAND_NOT_FOUND")
	if envelope["error"].(map[string]any)["code"] != "COMMAND_NOT_FOUND" {
		t.Fatalf("configured-output error = %#v", envelope)
	}
}

func TestBlockedPlansEmitOneFailureEnvelope(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	executeHumanJSON(t, "account", "add", "bank", "Checking", "--default-deposit")
	executeHumanJSON(t, "receive", "100.00", "Revenue", "--date", "2026-01-02")
	reconciliationPath := filepath.Join(home, "blocked-reconciliation.json")
	envelope, err := executeContractFailure(t, "--actor", "test", "--json", "reconcile", "plan", "Checking", "--through", "2026-01-31", "--ending", "0", "--out", reconciliationPath)
	requireApplicationCode(t, err, "RECONCILIATION_PLAN_BLOCKED")
	if envelope["error"].(map[string]any)["code"] != "RECONCILIATION_PLAN_BLOCKED" {
		t.Fatalf("reconciliation error = %#v", envelope)
	}
	if _, err := os.Stat(reconciliationPath); err != nil {
		t.Fatalf("blocked reconciliation plan was not retained: %v", err)
	}

	executeHumanJSON(t, "account", "add", "bank", "Conflicting QBO Bank", "--code", "10000", "--no-reconcile")
	testdata, err := filepath.Abs(filepath.Join("..", "importer", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	quickBooksPath := filepath.Join(home, "blocked-quickbooks.json")
	envelope, err = executeContractFailure(t, "--actor", "test", "--json", "import", "quickbooks", "plan",
		"--from", filepath.Join(testdata, "general_ledger.json"), "--accounts", filepath.Join(testdata, "accounts.json"), "--out", quickBooksPath)
	requireApplicationCode(t, err, "QUICKBOOKS_PLAN_BLOCKED")
	if envelope["error"].(map[string]any)["code"] != "QUICKBOOKS_PLAN_BLOCKED" {
		t.Fatalf("QuickBooks error = %#v", envelope)
	}
	if _, err := os.Stat(quickBooksPath); err != nil {
		t.Fatalf("blocked QuickBooks plan was not retained: %v", err)
	}
}
