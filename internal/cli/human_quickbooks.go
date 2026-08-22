package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/importer"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

const quickBooksPlanSchema = "books.quickbooks-import-plan/v1"

type quickBooksSource struct {
	Kind           importer.SourceKind `json:"kind"`
	Path           string              `json:"path"`
	AccountCatalog string              `json:"account_catalog"`
	StartDate      string              `json:"start_date"`
	EndDate        string              `json:"end_date"`
}

type quickBooksImportPlan struct {
	Schema       string           `json:"schema"`
	Company      string           `json:"company"`
	Entity       string           `json:"entity"`
	Book         string           `json:"book"`
	Currency     string           `json:"currency"`
	Source       quickBooksSource `json:"source"`
	Import       importer.Plan    `json:"import"`
	ImportDigest string           `json:"import_digest"`
	AccountCount int              `json:"account_count"`
	JournalCount int              `json:"journal_count"`
	Ready        bool             `json:"ready"`
	Blockers     []string         `json:"blockers"`
	CreatedAt    string           `json:"created_at"`
	Digest       string           `json:"digest"`
}

type quickBooksPlanOutput struct {
	Plan     quickBooksImportPlan `json:"plan"`
	PlanPath string               `json:"plan_path,omitempty"`
	Written  bool                 `json:"written"`
}

type quickBooksApplyOutput struct {
	Company           string `json:"company"`
	Accounts          int    `json:"accounts"`
	StatementControls int    `json:"statement_controls"`
	PeriodsCreated    int    `json:"periods_created"`
	Journals          int    `json:"journals"`
	JournalsCreated   int    `json:"journals_created"`
	JournalsPosted    int    `json:"journals_posted"`
	Status            string `json:"status"`
	PlanDigest        string `json:"plan_digest"`
	DryRun            bool   `json:"dry_run"`
}

type quickBooksBuildFlags struct {
	from, accounts, start, through, mode, output string
}

func newImportCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "import", Short: "Inspect, plan, and apply initial accounting-system imports"}
	command.AddCommand(newQuickBooksCommand(opts))
	return command
}

func newQuickBooksCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "quickbooks", Short: "Initial import from reviewed QuickBooks exports"}
	command.AddCommand(newQuickBooksBuildCommand(opts, "inspect", false), newQuickBooksBuildCommand(opts, "plan", true), newQuickBooksApplyCommand(opts))
	return command
}

func newQuickBooksBuildCommand(opts *options, use string, writePlan bool) *cobra.Command {
	flags := quickBooksBuildFlags{mode: "auto"}
	short := "Inspect QuickBooks exports without changing Books"
	if writePlan {
		short = "Build a reviewable, content-hashed QuickBooks import plan"
	}
	command := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.from == "" {
				return apperr.New(apperr.Invalid, "QUICKBOOKS_SOURCE_REQUIRED", "--from is required")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			source, err := discoverQuickBooksSource(flags)
			if err != nil {
				return err
			}
			request := quickBooksRequest(resolved.Company.EntityCode, resolved.Company.BookCode, resolved.Company.Currency, source)
			built, err := importer.Build(cmd.Context(), request)
			if err != nil {
				return apperr.Wrap(apperr.Input, "QUICKBOOKS_INSPECTION_FAILED", "inspect QuickBooks export", err)
			}
			importDigest, err := digestJSON(built)
			if err != nil {
				return err
			}
			journalCount := countImportedJournals(built)
			plan := quickBooksImportPlan{
				Schema: quickBooksPlanSchema, Company: resolved.Key, Entity: resolved.Company.EntityCode,
				Book: resolved.Company.BookCode, Currency: resolved.Company.Currency, Source: source, Import: built,
				ImportDigest: importDigest, AccountCount: len(built.Accounts), JournalCount: journalCount,
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			for _, diagnostic := range built.Diagnostics {
				if diagnostic.Severity == importer.SeverityError {
					plan.Blockers = append(plan.Blockers, diagnostic.Code+": "+diagnostic.Message)
				}
			}
			if journalCount == 0 {
				plan.Blockers = append(plan.Blockers, "the selected export produced no importable journals")
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			ledgerBlockers, err := quickBooksAccountBlockers(cmd, store, built.Accounts)
			_ = store.Close()
			if err != nil {
				return err
			}
			plan.Blockers = append(plan.Blockers, ledgerBlockers...)
			plan.Ready = len(plan.Blockers) == 0
			plan.Digest, err = digestQuickBooksPlan(plan)
			if err != nil {
				return err
			}
			written := false
			if writePlan && !opts.dryRun {
				if flags.output == "" {
					flags.output = filepath.Join(resolved.Plans, fmt.Sprintf("quickbooks-%s-%s.json", source.StartDate, source.EndDate))
				}
				flags.output, err = filepath.Abs(filepath.Clean(flags.output))
				if err != nil {
					return err
				}
				if err := writeExclusiveJSON(flags.output, plan); err != nil {
					return err
				}
				written = true
			}
			output := quickBooksPlanOutput{Plan: plan, PlanPath: flags.output, Written: written}
			if !plan.Ready {
				message := strings.Join(plan.Blockers, "; ")
				if written {
					message += fmt.Sprintf("; blocked plan written to %s", output.PlanPath)
				}
				return apperr.New(apperr.Validation, "QUICKBOOKS_PLAN_BLOCKED", message)
			}
			return writeQuickBooksPlan(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&flags.from, "from", "", "QuickBooks object directory, GeneralLedger JSON, or journal XLSX")
	command.Flags().StringVar(&flags.accounts, "accounts", "", "Account.json path (inferred from the source directory when omitted)")
	command.Flags().StringVar(&flags.start, "start", "", "inclusive import start (inferred for JSON exports)")
	command.Flags().StringVar(&flags.through, "through", "", "inclusive import cutoff (inferred for JSON exports)")
	command.Flags().StringVar(&flags.mode, "mode", flags.mode, "auto, general-ledger, objects, or journal")
	if writePlan {
		command.Flags().StringVarP(&flags.output, "out", "o", "", "plan path")
	}
	return command
}

func discoverQuickBooksSource(flags quickBooksBuildFlags) (quickBooksSource, error) {
	path, err := filepath.Abs(filepath.Clean(flags.from))
	if err != nil {
		return quickBooksSource{}, apperr.Wrap(apperr.Invalid, "QUICKBOOKS_SOURCE_INVALID", "resolve source path", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return quickBooksSource{}, apperr.Wrap(apperr.Input, "QUICKBOOKS_SOURCE_NOT_FOUND", "inspect QuickBooks source", err)
	}
	mode := strings.ToLower(strings.TrimSpace(flags.mode))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "general-ledger" && mode != "objects" && mode != "journal" {
		return quickBooksSource{}, apperr.New(apperr.Invalid, "QUICKBOOKS_MODE_INVALID", "--mode must be auto, general-ledger, objects, or journal")
	}
	source := quickBooksSource{}
	if info.IsDir() {
		generalLedger := filepath.Join(path, "GeneralLedger.json")
		_, glErr := os.Stat(generalLedger)
		if mode == "general-ledger" || (mode == "auto" && glErr == nil) {
			if glErr != nil {
				return source, apperr.New(apperr.NotFound, "GENERAL_LEDGER_NOT_FOUND", fmt.Sprintf("%s does not contain GeneralLedger.json", path))
			}
			source.Kind, source.Path = importer.SourceGeneralLedger, generalLedger
		} else if mode == "objects" || mode == "auto" {
			source.Kind, source.Path = importer.SourceQBOObjectDir, path
		} else {
			return source, apperr.New(apperr.Invalid, "QUICKBOOKS_SOURCE_INVALID", "journal mode requires an XLSX file")
		}
	} else {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json":
			if mode != "auto" && mode != "general-ledger" {
				return source, apperr.New(apperr.Invalid, "QUICKBOOKS_MODE_INVALID", "a JSON file requires general-ledger mode")
			}
			source.Kind, source.Path = importer.SourceGeneralLedger, path
		case ".xlsx":
			if mode != "auto" && mode != "journal" {
				return source, apperr.New(apperr.Invalid, "QUICKBOOKS_MODE_INVALID", "an XLSX file requires journal mode")
			}
			source.Kind, source.Path = importer.SourceJournalXLSX, path
		default:
			return source, apperr.New(apperr.Invalid, "QUICKBOOKS_SOURCE_INVALID", "source must be a directory, .json, or .xlsx")
		}
	}
	accountCatalog := strings.TrimSpace(flags.accounts)
	if accountCatalog == "" {
		base := path
		if !info.IsDir() {
			base = filepath.Dir(path)
		}
		accountCatalog = filepath.Join(base, "Account.json")
	}
	accountCatalog, err = filepath.Abs(filepath.Clean(accountCatalog))
	if err != nil {
		return source, err
	}
	if stat, statErr := os.Stat(accountCatalog); statErr != nil || stat.IsDir() {
		return source, apperr.New(apperr.NotFound, "QUICKBOOKS_ACCOUNTS_NOT_FOUND", fmt.Sprintf("account catalog was not found at %s; pass --accounts", accountCatalog))
	}
	source.AccountCatalog = accountCatalog
	inferredStart, inferredEnd, err := inferQuickBooksBounds(source)
	if err != nil {
		return source, err
	}
	source.StartDate, source.EndDate = inferredStart, inferredEnd
	if flags.start != "" {
		source.StartDate, err = parseHumanDate(flags.start)
		if err != nil {
			return source, err
		}
	}
	if flags.through != "" {
		source.EndDate, err = parseHumanDate(flags.through)
		if err != nil {
			return source, err
		}
	}
	if source.StartDate == "" || source.EndDate == "" {
		return source, apperr.New(apperr.Invalid, "QUICKBOOKS_DATES_REQUIRED", "--start and --through are required when the source does not declare date bounds")
	}
	if source.EndDate < source.StartDate {
		return source, apperr.New(apperr.Invalid, "QUICKBOOKS_DATES_INVALID", "import cutoff precedes its start")
	}
	return source, nil
}

func inferQuickBooksBounds(source quickBooksSource) (string, string, error) {
	if source.Kind == importer.SourceJournalXLSX {
		return "", "", nil
	}
	if source.Kind == importer.SourceGeneralLedger {
		data, err := os.ReadFile(source.Path)
		if err != nil {
			return "", "", err
		}
		var header struct {
			Header struct {
				Start string `json:"StartPeriod"`
				End   string `json:"EndPeriod"`
			} `json:"Header"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return "", "", apperr.Wrap(apperr.Input, "QUICKBOOKS_JSON_INVALID", "read GeneralLedger report dates", err)
		}
		return header.Header.Start, header.Header.End, nil
	}
	transactionTypes := []string{"Bill", "BillPayment", "CreditMemo", "Deposit", "Invoice", "JournalEntry", "Payment", "Purchase", "RefundReceipt", "SalesReceipt", "Transfer", "VendorCredit"}
	var dates []string
	for _, transactionType := range transactionTypes {
		path := filepath.Join(source.Path, transactionType+".json")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		var envelope struct {
			Rows []struct {
				Date string `json:"TxnDate"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return "", "", apperr.Wrap(apperr.Input, "QUICKBOOKS_JSON_INVALID", "read QBO object dates", err)
		}
		for _, row := range envelope.Rows {
			if row.Date != "" {
				dates = append(dates, row.Date)
			}
		}
	}
	if len(dates) == 0 {
		return "", "", nil
	}
	sort.Strings(dates)
	return dates[0], dates[len(dates)-1], nil
}

func quickBooksRequest(entity, book, currency string, source quickBooksSource) importer.Request {
	return importer.Request{Entities: []importer.EntityRequest{{
		EntityCode: entity, BookCode: book, Currency: currency, StartDate: source.StartDate, CutoffDate: source.EndDate,
		AccountCatalogPath: source.AccountCatalog,
		Sources:            []importer.Source{{Kind: source.Kind, Path: source.Path, StartDate: source.StartDate, EndDate: source.EndDate}},
	}}}
}

func countImportedJournals(plan importer.Plan) int {
	count := 0
	for _, entity := range plan.Entities {
		count += len(entity.Journals)
	}
	return count
}

func quickBooksAccountBlockers(cmd *cobra.Command, store *storesqlite.Store, planned []importer.MasterAccount) ([]string, error) {
	existing, err := ledger.NewService(store, "import-preflight").ListAccounts(cmd.Context(), "")
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]ledger.Account, len(existing))
	for _, account := range existing {
		byCode[account.Code] = account
	}
	var blockers []string
	for _, account := range planned {
		current, ok := byCode[account.Code]
		if !ok {
			continue
		}
		if current.Name != account.Name || current.Type != account.Type || current.Subtype != account.Subtype || current.NormalBalance != account.NormalBalance || current.StatementSection != account.StatementSection {
			blockers = append(blockers, fmt.Sprintf("planned account %s conflicts with existing %q; initialize imports with --chart empty or resolve the chart conflict", account.Code, current.Name))
		}
	}
	return blockers, nil
}

func digestQuickBooksPlan(plan quickBooksImportPlan) (string, error) {
	plan.Digest = ""
	return digestJSON(plan)
}

func writeQuickBooksPlan(cmd *cobra.Command, opts *options, output quickBooksPlanOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"COMPANY", "SOURCE", "START", "END", "ACCOUNTS", "JOURNALS", "DIAGNOSTICS", "READY", "PLAN"},
		[][]string{{output.Plan.Company, string(output.Plan.Source.Kind), output.Plan.Source.StartDate, output.Plan.Source.EndDate,
			fmt.Sprint(output.Plan.AccountCount), fmt.Sprint(output.Plan.JournalCount), fmt.Sprint(len(output.Plan.Import.Diagnostics)), fmt.Sprint(output.Plan.Ready), output.PlanPath}})
}

func newQuickBooksApplyCommand(opts *options) *cobra.Command {
	var planPath string
	var draft bool
	command := &cobra.Command{
		Use: "apply", Short: "Apply a reviewed QuickBooks plan and post all journals atomically", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan quickBooksImportPlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			if plan.Schema != quickBooksPlanSchema || !plan.Ready {
				return apperr.New(apperr.Invalid, "QUICKBOOKS_PLAN_INVALID", "plan schema is unsupported or the plan is blocked")
			}
			digest, err := digestQuickBooksPlan(plan)
			if err != nil {
				return err
			}
			if digest != plan.Digest {
				return apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "QuickBooks plan changed after generation")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if resolved.Key != plan.Company || resolved.Company.EntityCode != plan.Entity || resolved.Company.BookCode != plan.Book {
				return apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to --company %s", plan.Company))
			}
			rebuilt, err := importer.Build(cmd.Context(), quickBooksRequest(plan.Entity, plan.Book, plan.Currency, plan.Source))
			if err != nil {
				return apperr.Wrap(apperr.Input, "QUICKBOOKS_SOURCE_CHANGED", "reinspect QuickBooks source", err)
			}
			rebuiltDigest, err := digestJSON(rebuilt)
			if err != nil {
				return err
			}
			if rebuiltDigest != plan.ImportDigest {
				return apperr.New(apperr.Conflict, "QUICKBOOKS_PLAN_STALE", "QuickBooks source content changed; generate and review a new plan")
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			blockers, err := quickBooksAccountBlockers(cmd, store, plan.Import.Accounts)
			if err != nil {
				_ = store.Close()
				return err
			}
			missingPeriods, err := importPeriodsToCreate(cmd, store, resolved.Company.FiscalYearEnd, plan.Source.StartDate, plan.Source.EndDate, plan.Import)
			_ = store.Close()
			if err != nil {
				return err
			}
			if len(blockers) != 0 {
				return apperr.New(apperr.Conflict, "QUICKBOOKS_ACCOUNT_CONFLICT", strings.Join(blockers, "; "))
			}
			output := quickBooksApplyOutput{Company: plan.Company, Accounts: len(plan.Import.Accounts), StatementControls: countQuickBooksStatementControls(plan), PeriodsCreated: len(missingPeriods), Journals: plan.JournalCount, Status: "VALIDATED", PlanDigest: plan.Digest, DryRun: opts.dryRun}
			if opts.dryRun {
				return writeQuickBooksApply(cmd, opts, output)
			}
			store, err = openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service := ledger.NewService(store, opts.actor)
			for _, period := range missingPeriods {
				if _, err := service.CreatePeriod(cmd.Context(), period); err != nil {
					return err
				}
			}
			if _, err := applyQuickBooksAccounts(cmd, service, plan); err != nil {
				return err
			}
			records := make([]ledger.JournalImportRecord, 0, plan.JournalCount)
			for _, entityPlan := range plan.Import.Entities {
				for _, journal := range entityPlan.Journals {
					raw, _ := json.Marshal(journal.Evidence)
					records = append(records, ledger.JournalImportRecord{Journal: journal.Input, RawJSON: raw})
				}
			}
			imported, err := service.ImportJournals(cmd.Context(), ledger.JournalImportInput{
				SourceSystem: "QBO", SourceName: filepath.Base(planPath), FileSHA256: plan.ImportDigest,
				Entity: plan.Entity, Records: records,
			})
			if err != nil {
				return err
			}
			output.JournalsCreated = imported.CreatedCount
			if draft {
				output.Status = "DRAFT"
				return writeQuickBooksApply(cmd, opts, output)
			}
			posted, err := service.PostImportBatch(cmd.Context(), imported.BatchID, false)
			if err != nil {
				return err
			}
			output.JournalsPosted = posted.PostedCount + posted.AlreadyPosted
			output.Status = "POSTED"
			return writeQuickBooksApply(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed QuickBooks plan JSON")
	command.Flags().BoolVar(&draft, "draft", false, "import journals as drafts instead of atomically posting the batch")
	return command
}

func importPeriodsToCreate(cmd *cobra.Command, store *storesqlite.Store, fiscalYearEnd int, sourceStart, sourceEnd string, plan importer.Plan) ([]ledger.CreatePeriodInput, error) {
	existing, err := ledger.NewService(store, "import-preflight").ListPeriods(cmd.Context(), "")
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]ledger.Period, len(existing))
	for _, period := range existing {
		byCode[period.Code] = period
	}
	months := map[string]bool{}
	if first, firstErr := time.Parse("2006-01-02", sourceStart); firstErr == nil {
		if last, lastErr := time.Parse("2006-01-02", sourceEnd); lastErr == nil {
			cursor := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
			limit := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, time.UTC)
			for !cursor.After(limit) {
				months[cursor.Format("2006-01")] = true
				cursor = cursor.AddDate(0, 1, 0)
			}
		}
	}
	for _, entity := range plan.Entities {
		for _, journal := range entity.Journals {
			postingDate, err := time.Parse("2006-01-02", journal.Input.PostingDate)
			if err != nil {
				return nil, apperr.New(apperr.Integrity, "IMPORT_POSTING_DATE_INVALID", fmt.Sprintf("planned journal has invalid posting date %q", journal.Input.PostingDate))
			}
			months[postingDate.Format("2006-01")] = true
		}
	}
	if fiscalYearEnd < 1 || fiscalYearEnd > 12 {
		fiscalYearEnd = 12
	}
	var result []ledger.CreatePeriodInput
	for code := range months {
		start, err := time.Parse("2006-01", code)
		if err != nil {
			return nil, err
		}
		end := start.AddDate(0, 1, 0).AddDate(0, 0, -1)
		if prior, ok := byCode[code]; ok {
			if prior.StartDate != start.Format("2006-01-02") || prior.EndDate != end.Format("2006-01-02") {
				return nil, apperr.New(apperr.Conflict, "IMPORT_PERIOD_CONFLICT", fmt.Sprintf("period %s exists with different dates", code))
			}
			continue
		}
		fiscalYear := start.Year()
		if int(start.Month()) > fiscalYearEnd {
			fiscalYear++
		}
		startMonth := fiscalYearEnd%12 + 1
		periodNumber := (int(start.Month())-startMonth+12)%12 + 1
		result = append(result, ledger.CreatePeriodInput{
			Code: code, StartDate: start.Format("2006-01-02"), EndDate: end.Format("2006-01-02"),
			FiscalYear: fiscalYear, PeriodNumber: periodNumber, YearEnd: int(start.Month()) == fiscalYearEnd,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartDate < result[j].StartDate })
	return result, nil
}

func applyQuickBooksAccounts(cmd *cobra.Command, service *ledger.Service, plan quickBooksImportPlan) (int, error) {
	existing, err := service.ListAccounts(cmd.Context(), "")
	if err != nil {
		return 0, err
	}
	byCode := make(map[string]ledger.Account, len(existing))
	for _, account := range existing {
		byCode[account.Code] = account
	}
	for _, account := range plan.Import.Accounts {
		if _, ok := byCode[account.Code]; !ok {
			if _, err := service.CreateAccount(cmd.Context(), ledger.CreateAccountInput{
				Code: account.Code, Name: account.Name, Type: account.Type, Subtype: account.Subtype,
				NormalBalance: account.NormalBalance, StatementSection: account.StatementSection,
			}); err != nil {
				return 0, err
			}
		}
		for _, activation := range account.Activations {
			if activation.BookCode != plan.Book {
				continue
			}
			if err := service.ConfigureBookAccount(cmd.Context(), activation.BookCode, account.Code, activation.ActiveFrom, activation.ActiveTo, activation.PostingEnabled); err != nil {
				return 0, err
			}
		}
		for _, identity := range account.Identities {
			if _, err := service.AddAccountIdentity(cmd.Context(), ledger.AddAccountIdentityInput{
				Entity: identity.EntityCode, Account: account.Code, SourceSystem: identity.SourceSystem,
				ExternalID: identity.ExternalID, AccountNumber: identity.AccountNum, Name: identity.Name, Active: identity.Active,
				Evidence: ledger.AccountIdentityEvidence{
					SourceKind: string(identity.Evidence.SourceKind), SourcePath: identity.Evidence.SourcePath,
					SourceSHA256: identity.Evidence.SourceSHA256, Locator: identity.Evidence.Locator,
					PayloadSHA256: identity.Evidence.PayloadSHA256,
				},
			}); err != nil {
				return 0, err
			}
		}
	}
	existingStatements, err := service.ListStatementAccounts(cmd.Context(), plan.Entity)
	if err != nil {
		return 0, err
	}
	statementByGL := make(map[string]ledger.StatementAccount, len(existingStatements))
	for _, statement := range existingStatements {
		statementByGL[statement.GLAccountCode] = statement
	}
	controlled := 0
	for _, account := range plan.Import.Accounts {
		kind := quickBooksStatementKind(account)
		if kind == "" || !quickBooksAccountActiveForEntity(account, plan.Entity) {
			continue
		}
		controlled++
		if statement, exists := statementByGL[account.Code]; exists {
			if statement.Status != "ACTIVE" || statement.Kind != kind || statement.Currency != plan.Currency || !statement.RequiredForClose || statement.ReconciliationRequiredFrom != plan.Source.StartDate {
				return 0, apperr.New(apperr.Conflict, "QUICKBOOKS_STATEMENT_CONTROL_CONFLICT", fmt.Sprintf("existing statement control %s for imported account %s does not match the reviewed import setup", statement.Code, account.Code))
			}
			continue
		}
		statementCode, err := quickBooksStatementCode(plan.Entity, account.Code)
		if err != nil {
			return 0, err
		}
		if _, err := service.CreateStatementAccount(cmd.Context(), ledger.CreateStatementAccountInput{
			Code: statementCode, Entity: plan.Entity, Book: plan.Book, GLAccount: account.Code,
			Name: account.Name, Kind: kind, Currency: plan.Currency, RequiredForClose: true,
			ReconciliationRequiredFrom: plan.Source.StartDate,
		}); err != nil {
			return 0, err
		}
	}
	return controlled, nil
}

func countQuickBooksStatementControls(plan quickBooksImportPlan) int {
	count := 0
	for _, account := range plan.Import.Accounts {
		if quickBooksStatementKind(account) != "" && quickBooksAccountActiveForEntity(account, plan.Entity) {
			count++
		}
	}
	return count
}

func quickBooksAccountActiveForEntity(account importer.MasterAccount, entity string) bool {
	for _, identity := range account.Identities {
		if identity.EntityCode == entity && identity.Active {
			return true
		}
	}
	return false
}

func quickBooksStatementKind(account importer.MasterAccount) string {
	subtype := normalizedSubtype(account.Subtype)
	switch {
	case account.Type == "ASSET" && subtype == "BANK":
		return "BANK"
	case account.Type == "ASSET" && subtype == "INVESTMENT":
		return "INVESTMENT"
	case account.Type == "LIABILITY" && subtype == "CREDIT_CARD":
		return "CREDIT_CARD"
	case account.Type == "LIABILITY" && strings.Contains(subtype, "LOAN"):
		return "LOAN"
	default:
		return ""
	}
}

func quickBooksStatementCode(entity, account string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(entity)) + "-" + strings.ToUpper(strings.TrimSpace(account))
	if len(code) <= 64 {
		return code, nil
	}
	digest, err := digestJSON(map[string]string{"entity": entity, "account": account})
	if err != nil {
		return "", err
	}
	prefix := strings.ToUpper(strings.TrimSpace(entity))
	if len(prefix) > 48 {
		prefix = prefix[:48]
	}
	return prefix + "-QBO-" + digest[:8], nil
}

func writeQuickBooksApply(cmd *cobra.Command, opts *options, output quickBooksApplyOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"COMPANY", "ACCOUNTS", "STATEMENT CONTROLS", "PERIODS CREATED", "JOURNALS", "CREATED", "POSTED", "STATUS", "DRY RUN"},
		[][]string{{output.Company, fmt.Sprint(output.Accounts), fmt.Sprint(output.StatementControls), fmt.Sprint(output.PeriodsCreated), fmt.Sprint(output.Journals), fmt.Sprint(output.JournalsCreated), fmt.Sprint(output.JournalsPosted), output.Status, fmt.Sprint(output.DryRun)}})
}
