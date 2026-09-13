// QuickBooks setup accepts retained local source paths. It is shared with
// trusted local clients; it is not exposed as a server-filesystem HTTP import.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/importer"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const quickBooksPlanSchema = "books.quickbooks-import-plan/v1"

type QuickBooksSource struct {
	Kind           importer.SourceKind `json:"kind"`
	Path           string              `json:"path"`
	AccountCatalog string              `json:"account_catalog"`
	StartDate      string              `json:"start_date"`
	EndDate        string              `json:"end_date"`
}

type QuickBooksPlan struct {
	Schema       string           `json:"schema"`
	Company      string           `json:"company"`
	Entity       string           `json:"entity"`
	Book         string           `json:"book"`
	Currency     string           `json:"currency"`
	Source       QuickBooksSource `json:"source"`
	Import       importer.Plan    `json:"import"`
	ImportDigest string           `json:"import_digest"`
	AccountCount int              `json:"account_count"`
	JournalCount int              `json:"journal_count"`
	Ready        bool             `json:"ready"`
	Blockers     []string         `json:"blockers"`
	CreatedAt    string           `json:"created_at"`
	Digest       string           `json:"digest"`
}

type QuickBooksStep struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
}

type QuickBooksResult struct {
	Completed         []QuickBooksStep `json:"completed,omitempty"`
	BatchID           string           `json:"batch_id,omitempty"`
	Company           string           `json:"company"`
	Accounts          int              `json:"accounts"`
	StatementControls int              `json:"statement_controls"`
	PeriodsCreated    int              `json:"periods_created"`
	Journals          int              `json:"journals"`
	JournalsCreated   int              `json:"journals_created"`
	JournalsPosted    int              `json:"journals_posted"`
	Status            string           `json:"status"`
	PlanDigest        string           `json:"plan_digest"`
	DryRun            bool             `json:"dry_run"`
}

type QuickBooksRequest struct{ From, Accounts, Start, Through, Mode string }

func discoverQuickBooksSourceFS(flags QuickBooksRequest, files fs.FS) (QuickBooksSource, error) {
	path := filepath.Clean(flags.From)
	var err error
	local := files == nil
	if local {
		files = importer.LocalFiles{}
		path, err = filepath.Abs(path)
	}
	if err != nil {
		return QuickBooksSource{}, apperr.Wrap(apperr.Invalid, "QUICKBOOKS_SOURCE_INVALID", "resolve source path", err)
	}
	info, err := fs.Stat(files, path)
	if err != nil {
		return QuickBooksSource{}, apperr.Wrap(apperr.Input, "QUICKBOOKS_SOURCE_NOT_FOUND", "inspect QuickBooks source", err)
	}
	mode := strings.ToLower(strings.TrimSpace(flags.Mode))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "general-ledger" && mode != "objects" && mode != "journal" {
		return QuickBooksSource{}, apperr.New(apperr.Invalid, "QUICKBOOKS_MODE_INVALID", "--mode must be auto, general-ledger, objects, or journal")
	}
	source := QuickBooksSource{}
	if info.IsDir() {
		generalLedger := filepath.Join(path, "GeneralLedger.json")
		_, glErr := fs.Stat(files, generalLedger)
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
	accountCatalog := strings.TrimSpace(flags.Accounts)
	if accountCatalog == "" {
		base := path
		if !info.IsDir() {
			base = filepath.Dir(path)
		}
		accountCatalog = filepath.Join(base, "Account.json")
	}
	accountCatalog = filepath.Clean(accountCatalog)
	if local {
		accountCatalog, err = filepath.Abs(accountCatalog)
	}
	if err != nil {
		return source, err
	}
	if stat, statErr := fs.Stat(files, accountCatalog); statErr != nil || stat.IsDir() {
		return source, apperr.New(apperr.NotFound, "QUICKBOOKS_ACCOUNTS_NOT_FOUND", fmt.Sprintf("account catalog was not found at %s; pass --accounts", accountCatalog))
	}
	source.AccountCatalog = accountCatalog
	inferredStart, inferredEnd, err := inferQuickBooksBoundsFS(source, files)
	if err != nil {
		return source, err
	}
	source.StartDate, source.EndDate = inferredStart, inferredEnd
	if flags.Start != "" {
		source.StartDate, err = strictDate(flags.Start)
		if err != nil {
			return source, err
		}
	}
	if flags.Through != "" {
		source.EndDate, err = strictDate(flags.Through)
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

func inferQuickBooksBoundsFS(source QuickBooksSource, files fs.FS) (string, string, error) {
	if source.Kind == importer.SourceJournalXLSX {
		return "", "", nil
	}
	if source.Kind == importer.SourceGeneralLedger {
		data, err := fs.ReadFile(files, source.Path)
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
		data, err := fs.ReadFile(files, path)
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

func quickBooksRequest(entity, book, currency string, source QuickBooksSource) importer.Request {
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

func quickBooksAccountBlockers(ctx context.Context, store *storesqlite.Store, planned []importer.MasterAccount) ([]string, error) {
	existing, err := ledger.NewService(store, "import-preflight").ListAccounts(ctx, "")
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

func DigestQuickBooksPlan(plan QuickBooksPlan) (string, error) {
	plan.Digest = ""
	return DigestJSON(plan)
}

func importPeriodsToCreate(ctx context.Context, store *storesqlite.Store, fiscalYearEnd int, sourceStart, sourceEnd string, plan importer.Plan) ([]ledger.CreatePeriodInput, error) {
	existing, err := ledger.NewService(store, "import-preflight").ListPeriods(ctx, "")
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

func applyQuickBooksAccounts(ctx context.Context, service *ledger.Service, plan QuickBooksPlan, completed *[]QuickBooksStep) (int, error) {
	existing, err := service.ListAccounts(ctx, "")
	if err != nil {
		return 0, err
	}
	byCode := make(map[string]ledger.Account, len(existing))
	for _, account := range existing {
		byCode[account.Code] = account
	}
	for _, account := range plan.Import.Accounts {
		if _, ok := byCode[account.Code]; !ok {
			if _, err := service.CreateAccount(ctx, ledger.CreateAccountInput{
				Code: account.Code, Name: account.Name, Type: account.Type, Subtype: account.Subtype,
				NormalBalance: account.NormalBalance, StatementSection: account.StatementSection,
			}); err != nil {
				return 0, err
			}
			*completed = append(*completed, QuickBooksStep{"create-account", account.Code})
		}
		for _, activation := range account.Activations {
			if activation.BookCode != plan.Book {
				continue
			}
			if err := service.ConfigureBookAccount(ctx, activation.BookCode, account.Code, activation.ActiveFrom, activation.ActiveTo, activation.PostingEnabled); err != nil {
				return 0, err
			}
			*completed = append(*completed, QuickBooksStep{"configure-book-account", activation.BookCode + "/" + account.Code})
		}
		for _, identity := range account.Identities {
			if _, err := service.AddAccountIdentity(ctx, ledger.AddAccountIdentityInput{
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
			*completed = append(*completed, QuickBooksStep{"account-identity", identity.EntityCode + "/" + account.Code + "/" + identity.SourceSystem + "/" + identity.ExternalID})
		}
	}
	existingStatements, err := service.ListStatementAccounts(ctx, plan.Entity)
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
		if _, err := service.CreateStatementAccount(ctx, ledger.CreateStatementAccountInput{
			Code: statementCode, Entity: plan.Entity, Book: plan.Book, GLAccount: account.Code,
			Name: account.Name, Kind: kind, Currency: plan.Currency, RequiredForClose: true,
			ReconciliationRequiredFrom: plan.Source.StartDate,
		}); err != nil {
			return 0, err
		}
		*completed = append(*completed, QuickBooksStep{"create-statement-account", statementCode})
	}
	return controlled, nil
}

func countQuickBooksStatementControls(plan QuickBooksPlan) int {
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
	subtype := NormalizedSubtype(account.Subtype)
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
	digest, err := DigestJSON(map[string]string{"entity": entity, "account": account})
	if err != nil {
		return "", err
	}
	prefix := strings.ToUpper(strings.TrimSpace(entity))
	if len(prefix) > 48 {
		prefix = prefix[:48]
	}
	return prefix + "-QBO-" + digest[:8], nil
}

func (s *Service) PlanQuickBooks(ctx context.Context, flags QuickBooksRequest) (QuickBooksPlan, error) {
	return s.PlanQuickBooksFromFS(ctx, flags, nil)
}
func (s *Service) PlanQuickBooksFromFS(ctx context.Context, flags QuickBooksRequest, files fs.FS) (QuickBooksPlan, error) {
	if flags.From == "" {
		return QuickBooksPlan{}, apperr.New(apperr.Invalid, "QUICKBOOKS_SOURCE_REQUIRED", "--from is required")
	}
	resolved := s.resolved
	source, err := discoverQuickBooksSourceFS(flags, files)
	if err != nil {
		return QuickBooksPlan{}, err
	}
	request := quickBooksRequest(resolved.Company.EntityCode, resolved.Company.BookCode, resolved.Company.Currency, source)
	if files == nil {
		files = importer.LocalFiles{}
	}
	built, err := importer.BuildFromFS(ctx, request, files)
	if err != nil {
		return QuickBooksPlan{}, apperr.Wrap(apperr.Input, "QUICKBOOKS_INSPECTION_FAILED", "inspect QuickBooks export", err)
	}
	importDigest, err := DigestJSON(built)
	if err != nil {
		return QuickBooksPlan{}, err
	}
	journalCount := countImportedJournals(built)
	plan := QuickBooksPlan{
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
	store := s.store
	ledgerBlockers, err := quickBooksAccountBlockers(ctx, store, built.Accounts)
	if err != nil {
		return QuickBooksPlan{}, err
	}
	plan.Blockers = append(plan.Blockers, ledgerBlockers...)
	plan.Ready = len(plan.Blockers) == 0
	plan.Digest, err = DigestQuickBooksPlan(plan)
	if err != nil {
		return QuickBooksPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyQuickBooks(ctx context.Context, plan QuickBooksPlan, sourceName string, draft, dryRun bool) (QuickBooksResult, error) {
	return s.ApplyQuickBooksFromFS(ctx, plan, sourceName, draft, dryRun, importer.LocalFiles{})
}
func (s *Service) ApplyQuickBooksFromFS(ctx context.Context, plan QuickBooksPlan, sourceName string, draft, dryRun bool, files fs.FS) (QuickBooksResult, error) {
	if plan.Schema != quickBooksPlanSchema || !plan.Ready {
		return QuickBooksResult{}, apperr.New(apperr.Invalid, "QUICKBOOKS_PLAN_INVALID", "plan schema is unsupported or the plan is blocked")
	}
	digest, err := DigestQuickBooksPlan(plan)
	if err != nil {
		return QuickBooksResult{}, err
	}
	if digest != plan.Digest {
		return QuickBooksResult{}, apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "QuickBooks plan changed after generation")
	}
	resolved := s.resolved
	if resolved.Key != plan.Company || resolved.Company.EntityCode != plan.Entity || resolved.Company.BookCode != plan.Book || resolved.Company.Currency != plan.Currency {
		return QuickBooksResult{}, apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to --company %s", plan.Company))
	}
	rebuilt, err := importer.BuildFromFS(ctx, quickBooksRequest(plan.Entity, plan.Book, plan.Currency, plan.Source), files)
	if err != nil {
		return QuickBooksResult{}, apperr.Wrap(apperr.Input, "QUICKBOOKS_SOURCE_CHANGED", "reinspect QuickBooks source", err)
	}
	rebuiltDigest, err := DigestJSON(rebuilt)
	if err != nil {
		return QuickBooksResult{}, err
	}
	plannedDigest, err := DigestJSON(plan.Import)
	if err != nil {
		return QuickBooksResult{}, err
	}
	if rebuiltDigest != plan.ImportDigest || plannedDigest != plan.ImportDigest {
		return QuickBooksResult{}, apperr.New(apperr.Conflict, "QUICKBOOKS_PLAN_STALE", "QuickBooks source content changed; generate and review a new plan")
	}
	store := s.store
	blockers, err := quickBooksAccountBlockers(ctx, store, plan.Import.Accounts)
	if err != nil {

		return QuickBooksResult{}, err
	}
	missingPeriods, err := importPeriodsToCreate(ctx, store, resolved.Company.FiscalYearEnd, plan.Source.StartDate, plan.Source.EndDate, plan.Import)

	if err != nil {
		return QuickBooksResult{}, err
	}
	if len(blockers) != 0 {
		return QuickBooksResult{}, apperr.New(apperr.Conflict, "QUICKBOOKS_ACCOUNT_CONFLICT", strings.Join(blockers, "; "))
	}
	output := QuickBooksResult{Company: plan.Company, Accounts: len(plan.Import.Accounts), StatementControls: countQuickBooksStatementControls(plan), PeriodsCreated: len(missingPeriods), Journals: plan.JournalCount, Status: "VALIDATED", PlanDigest: plan.Digest, DryRun: dryRun}
	if dryRun {
		return output, nil
	}
	service := s.ledger()

	// Setup and import are legacy multi-transaction operations. Retain every
	// completed step if a later phase fails so a local client can recover safely.
	partial := func(cause error) (QuickBooksResult, error) {
		if len(output.Completed) == 0 {
			return QuickBooksResult{}, cause
		}
		output.Status = "PARTIAL"
		progress, _ := json.Marshal(output.Completed)
		causeCode := "OPERATION_FAILED"
		if appError, ok := apperr.As(cause); ok {
			causeCode = appError.Code
		}
		return output, &apperr.Error{Kind: apperr.Conflict, Code: "QUICKBOOKS_APPLY_PARTIAL", Message: fmt.Sprintf("QuickBooks apply stopped (%s); completed durable steps: %s; batch: %s", causeCode, progress, output.BatchID), Hint: "Inspect the reported steps, resolve the blocker, and retry the same reviewed plan. Setup and source import converge on their existing identities; posting the batch is atomic.", Err: cause}
	}
	for _, period := range missingPeriods {
		if _, err := service.CreatePeriod(ctx, period); err != nil {
			return partial(err)
		}
		output.Completed = append(output.Completed, QuickBooksStep{"create-period", period.Code})
	}
	if _, err := applyQuickBooksAccounts(ctx, service, plan, &output.Completed); err != nil {
		return partial(err)
	}
	records := make([]ledger.JournalImportRecord, 0, plan.JournalCount)
	for _, entityPlan := range plan.Import.Entities {
		for _, journal := range entityPlan.Journals {
			raw, _ := json.Marshal(journal.Evidence)
			records = append(records, ledger.JournalImportRecord{Journal: journal.Input, RawJSON: raw})
		}
	}
	imported, err := service.ImportJournals(ctx, ledger.JournalImportInput{
		SourceSystem: "QBO", SourceName: sourceName, FileSHA256: plan.ImportDigest,
		Entity: plan.Entity, Records: records,
	})
	if err != nil {
		return partial(err)
	}
	output.BatchID = imported.BatchID
	output.Completed = append(output.Completed, QuickBooksStep{"import-journals", imported.BatchID})
	output.JournalsCreated = imported.CreatedCount
	if draft {
		output.Status = "DRAFT"
		return output, nil
	}
	posted, err := service.PostImportBatch(ctx, imported.BatchID, false)
	if err != nil {
		return partial(err)
	}
	output.Completed = append(output.Completed, QuickBooksStep{"post-import-batch", imported.BatchID})
	output.JournalsPosted = posted.PostedCount + posted.AlreadyPosted
	output.Status = "POSTED"
	return output, nil
}
