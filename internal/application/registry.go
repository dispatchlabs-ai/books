// Local registry administration is shared by trusted in-process clients.
// These operations accept operator-owned paths and are not company HTTP routes.
package application

import (
	"context"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CompanyCreateOptions struct {
	Name          string
	Key           string
	Currency      string
	Basis         string
	Start         string
	FiscalYearEnd string
	Periods       string
	Chart         string
	MakeDefault   bool
}

type CompanyCreateResult struct {
	Company      string `json:"company"`
	Name         string `json:"name"`
	EntityCode   string `json:"entity_code"`
	Currency     string `json:"currency"`
	Basis        string `json:"basis"`
	StartDate    string `json:"start_date"`
	PeriodCount  int    `json:"period_count"`
	Chart        string `json:"chart"`
	AccountCount int    `json:"account_count"`
	ConfigPath   string `json:"config_path"`
	Database     string `json:"database"`
	Backups      string `json:"backups"`
	Plans        string `json:"plans"`
	Default      bool   `json:"default"`
	DryRun       bool   `json:"dry_run"`
}

func ConfigMutationError(action string, err error) error {
	if _, ok := apperr.As(err); ok {
		return err
	}
	if os.IsNotExist(err) {
		return apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", err)
	}
	return apperr.Wrap(apperr.Unavailable, "CONFIG_WRITE_FAILED", action, err)
}

func MonthlyPeriods(start time.Time, endMonth time.Month) []ledger.CreatePeriodInput {
	result := make([]ledger.CreatePeriodInput, 0, 12)
	endYear := start.AddDate(0, 11, 0).Year()
	for index := 0; index < 12; index++ {
		periodStart := start.AddDate(0, index, 0)
		periodEnd := periodStart.AddDate(0, 1, 0).AddDate(0, 0, -1)
		result = append(result, ledger.CreatePeriodInput{
			Code: periodStart.Format("2006-01"), StartDate: periodStart.Format("2006-01-02"), EndDate: periodEnd.Format("2006-01-02"),
			FiscalYear: endYear, PeriodNumber: index + 1, YearEnd: periodEnd.Month() == endMonth,
		})
	}
	return result
}

func starterAccounts() []ledger.CreateAccountInput {
	return []ledger.CreateAccountInput{
		{Code: "1100", Name: "Accounts Receivable", Type: "ASSET", Subtype: "ACCOUNTS_RECEIVABLE", StatementSection: "BALANCE_SHEET"},
		{Code: "2000", Name: "Accounts Payable", Type: "LIABILITY", Subtype: "ACCOUNTS_PAYABLE", StatementSection: "BALANCE_SHEET"},
		{Code: "3000", Name: "Owner Equity", Type: "EQUITY", Subtype: "CONTRIBUTED_CAPITAL", StatementSection: "BALANCE_SHEET"},
		{Code: "3100", Name: "Retained Earnings", Type: "EQUITY", Subtype: "RETAINED_EARNINGS", StatementSection: "BALANCE_SHEET"},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", Subtype: "OPERATING_REVENUE", StatementSection: "INCOME_STATEMENT"},
		{Code: "5000", Name: "General Expense", Type: "EXPENSE", Subtype: "OPERATING_EXPENSE", StatementSection: "INCOME_STATEMENT"},
	}
}

func parseMonth(value string) (time.Month, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	months := map[string]time.Month{
		"1": time.January, "01": time.January, "jan": time.January, "january": time.January,
		"2": time.February, "02": time.February, "feb": time.February, "february": time.February,
		"3": time.March, "03": time.March, "mar": time.March, "march": time.March,
		"4": time.April, "04": time.April, "apr": time.April, "april": time.April,
		"5": time.May, "05": time.May, "may": time.May,
		"6": time.June, "06": time.June, "jun": time.June, "june": time.June,
		"7": time.July, "07": time.July, "jul": time.July, "july": time.July,
		"8": time.August, "08": time.August, "aug": time.August, "august": time.August,
		"9": time.September, "09": time.September, "sep": time.September, "september": time.September,
		"10": time.October, "oct": time.October, "october": time.October,
		"11": time.November, "nov": time.November, "november": time.November,
		"12": time.December, "dec": time.December, "december": time.December,
	}
	month, ok := months[trimmed]
	if !ok {
		return 0, apperr.New(apperr.Invalid, "FISCAL_YEAR_END_INVALID", "--fiscal-year-end must be a month name or number")
	}
	return month, nil
}

func fiscalStart(value string, endMonth time.Month) (time.Time, error) {
	if strings.TrimSpace(value) != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil || parsed.Day() != 1 {
			return time.Time{}, apperr.New(apperr.Invalid, "START_DATE_INVALID", "--start must be an ISO date on the first day of a month")
		}
		if parsed.Month() != endMonth%12+1 {
			return time.Time{}, apperr.New(apperr.Invalid, "START_DATE_INVALID", "--start must begin the month immediately after --fiscal-year-end")
		}
		return parsed, nil
	}
	return time.Time{}, apperr.New(apperr.Invalid, "START_DATE_INVALID", "an explicit fiscal start date is required")
}

func CreateRegisteredCompany(ctx context.Context, configPath, actor string, initialize, dryRun bool, values CompanyCreateOptions) (CompanyCreateResult, error) {
	values.Name = strings.TrimSpace(values.Name)
	if values.Name == "" {
		return CompanyCreateResult{}, apperr.New(apperr.Invalid, "COMPANY_NAME_REQUIRED", "--name is required; example: books init --name \"Acme Services, Inc.\"")
	}
	key := strings.ToLower(strings.TrimSpace(values.Key))
	if key == "" {
		key = booksconfig.DeriveCompanyKey(values.Name)
	}
	if err := booksconfig.ValidateCompanyKey(key); err != nil {
		return CompanyCreateResult{}, apperr.Wrap(apperr.Invalid, "COMPANY_KEY_INVALID", "validate company key", err)
	}
	currency := strings.ToUpper(strings.TrimSpace(values.Currency))
	if len(currency) != 3 || strings.IndexFunc(currency, func(character rune) bool { return character < 'A' || character > 'Z' }) != -1 {
		return CompanyCreateResult{}, apperr.New(apperr.Invalid, "CURRENCY_INVALID", "--currency must be a three-letter code such as USD")
	}
	basis := strings.ToUpper(strings.TrimSpace(values.Basis))
	if basis != "ACCRUAL" {
		return CompanyCreateResult{}, apperr.New(apperr.Invalid, "BASIS_NOT_SUPPORTED", "cash-basis accounting is not supported; use --basis accrual")
	}
	if strings.ToLower(strings.TrimSpace(values.Periods)) != "monthly" {
		return CompanyCreateResult{}, apperr.New(apperr.Invalid, "PERIOD_CADENCE_UNSUPPORTED", "--periods currently supports monthly")
	}
	chart := strings.ToLower(strings.TrimSpace(values.Chart))
	if chart != "starter" && chart != "empty" {
		return CompanyCreateResult{}, apperr.New(apperr.Invalid, "CHART_INVALID", "--chart must be starter or empty")
	}
	endMonth, err := parseMonth(values.FiscalYearEnd)
	if err != nil {
		return CompanyCreateResult{}, err
	}
	startDate, err := fiscalStart(values.Start, endMonth)
	if err != nil {
		return CompanyCreateResult{}, err
	}
	periods := MonthlyPeriods(startDate, endMonth)
	accountCount := 0
	if chart == "starter" {
		accountCount = len(starterAccounts())
	}
	prepare := func(current *booksconfig.Config) (booksconfig.ResolvedCompany, CompanyCreateResult, error) {
		if current.Companies == nil {
			current.Companies = make(map[string]booksconfig.Company)
		}
		if _, exists := current.Companies[key]; exists {
			return booksconfig.ResolvedCompany{}, CompanyCreateResult{}, apperr.New(apperr.Conflict, "COMPANY_EXISTS", fmt.Sprintf("company %q is already registered", key))
		}
		company := booksconfig.NewCompany(key, values.Name, currency, basis)
		company.FiscalYearEnd = int(endMonth)
		current.Companies[key] = company
		if current.DefaultCompany == "" || values.MakeDefault {
			current.DefaultCompany = key
		}
		resolved, resolveErr := current.Resolve(configPath, key)
		if resolveErr != nil {
			return booksconfig.ResolvedCompany{}, CompanyCreateResult{}, apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve new company", resolveErr)
		}
		result := CompanyCreateResult{
			Company: key, Name: values.Name, EntityCode: company.EntityCode, Currency: currency, Basis: basis,
			StartDate: startDate.Format("2006-01-02"), PeriodCount: len(periods), Chart: chart, AccountCount: accountCount,
			ConfigPath: configPath, Database: resolved.Database, Backups: resolved.Backups, Plans: resolved.Plans,
			Default: current.DefaultCompany == key, DryRun: dryRun,
		}
		return resolved, result, nil
	}

	if dryRun {
		var current booksconfig.Config
		if initialize {
			if _, statErr := os.Stat(configPath); statErr == nil {
				return CompanyCreateResult{}, apperr.New(apperr.Conflict, "CONFIG_EXISTS", fmt.Sprintf("Books is already initialized at %s; use books company add", configPath))
			} else if !os.IsNotExist(statErr) {
				return CompanyCreateResult{}, apperr.Wrap(apperr.Unavailable, "CONFIG_STAT_FAILED", "inspect Books configuration", statErr)
			}
			current = booksconfig.New()
		} else {
			loaded, loadErr := booksconfig.Load(configPath)
			if loadErr != nil {
				return CompanyCreateResult{}, apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", loadErr)
			}
			current = loaded
		}
		_, result, prepareErr := prepare(&current)
		if prepareErr != nil {
			return CompanyCreateResult{}, prepareErr
		}
		return result, nil
	}
	var result CompanyCreateResult
	var companyRoot string
	createdRoot := false
	cleanup := func() {
		if createdRoot {
			_ = os.RemoveAll(companyRoot)
		}
	}
	var initial *booksconfig.Config
	if initialize {
		value := booksconfig.New()
		initial = &value
	}
	_, err = booksconfig.Update(configPath, initial, func(current *booksconfig.Config, existed bool) error {
		if initialize && existed {
			return apperr.New(apperr.Conflict, "CONFIG_EXISTS", fmt.Sprintf("Books is already initialized at %s; use books company add", configPath))
		}
		resolved, prepared, prepareErr := prepare(current)
		if prepareErr != nil {
			return prepareErr
		}
		result = prepared
		companyRoot = filepath.Dir(resolved.Database)
		if _, statErr := os.Lstat(companyRoot); statErr == nil {
			return apperr.New(apperr.Conflict, "COMPANY_DIRECTORY_EXISTS", fmt.Sprintf("company directory already exists: %s", companyRoot))
		} else if !os.IsNotExist(statErr) {
			return apperr.Wrap(apperr.Unavailable, "COMPANY_DIRECTORY_STAT_FAILED", "inspect company directory", statErr)
		}
		if directoryErr := booksconfig.EnsureCompanyDirectories(resolved); directoryErr != nil {
			return apperr.Wrap(apperr.Unavailable, "COMPANY_DIRECTORY_FAILED", "create company directories", directoryErr)
		}
		createdRoot = true
		store, initErr := storesqlite.Init(ctx, resolved.Database, currency, actor)
		if initErr != nil {
			return initErr
		}
		company := current.Companies[key]
		if scanErr := store.DB().QueryRowContext(ctx, `SELECT database_uuid
			FROM database_metadata WHERE singleton = 1`).Scan(&company.DatabaseUUID); scanErr != nil {
			_ = store.Close()
			return storesqlite.MapError("read initialized company database identity", scanErr)
		}
		current.Companies[key] = company
		service := ledger.NewService(store, actor)
		if _, createErr := service.CreateEntity(ctx, ledger.CreateEntityInput{
			Code: company.EntityCode, LegalName: company.Name, Currency: company.Currency,
			BookCode: company.BookCode, BookName: company.Name + " Actual", Basis: company.Basis,
		}); createErr != nil {
			_ = store.Close()
			return createErr
		}
		for _, period := range periods {
			if _, periodErr := service.CreatePeriod(ctx, period); periodErr != nil {
				_ = store.Close()
				return periodErr
			}
		}
		if chart == "starter" {
			for _, account := range starterAccounts() {
				account.BookCodes = []string{company.BookCode}
				account.ActiveFrom = startDate.Format("2006-01-02")
				if _, accountErr := service.CreateAccount(ctx, account); accountErr != nil {
					_ = store.Close()
					return accountErr
				}
			}
			company.Defaults.RetainedEarnings = "3100"
			current.Companies[key] = company
		}
		if _, doctorErr := store.Doctor(ctx); doctorErr != nil {
			_ = store.Close()
			return doctorErr
		}
		if closeErr := store.Close(); closeErr != nil {
			return closeErr
		}
		if chmodErr := os.Chmod(resolved.Database, 0o600); chmodErr != nil {
			return apperr.Wrap(apperr.Unavailable, "DATABASE_PERMISSIONS_FAILED", "secure company database", chmodErr)
		}
		return nil
	})
	if err != nil {
		cleanup()
		return CompanyCreateResult{}, ConfigMutationError("write Books configuration", err)
	}
	createdRoot = false
	return result, nil
}

// SetConfiguration updates registry preferences atomically. It is a local admin
// entry point; company HTTP clients use SetAccountDefault below.
func SetConfiguration(ctx context.Context, path, selectedCompany, key, setting, actor string) (booksconfig.Config, string, error) {
	return setConfiguration(ctx, path, selectedCompany, key, setting, actor, nil)
}

func setConfiguration(ctx context.Context, path, selectedCompany, key, setting, actor string, expected *booksconfig.ResolvedCompany) (booksconfig.Config, string, error) {
	setting = strings.TrimSpace(setting)
	value, updateErr := booksconfig.Update(path, nil, func(value *booksconfig.Config, _ bool) error {
		switch key {
		case "default-company":
			setting = strings.ToLower(setting)
			if _, ok := value.Companies[setting]; !ok {
				return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is not registered", setting))
			}
			value.DefaultCompany = setting
		case "output":
			setting = strings.ToLower(setting)
			switch setting {
			case "table", "json", "jsonl", "csv":
				value.Defaults.Output = setting
			default:
				return apperr.New(apperr.Invalid, "FORMAT_INVALID", "output must be table, json, jsonl, or csv")
			}
		case "defaults.payment-account", "defaults.deposit-account", "defaults.retained-earnings":
			selected := strings.ToLower(strings.TrimSpace(selectedCompany))
			if selected == "" {
				selected = value.DefaultCompany
			}
			company, ok := value.Companies[selected]
			if !ok {
				return apperr.New(apperr.Invalid, "COMPANY_NOT_SELECTED", "supply --company or configure default-company")
			}
			resolved, resolveErr := value.Resolve(path, selected)
			if resolveErr != nil {
				return apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve selected company", resolveErr)
			}
			if expected != nil && (resolved.Database != expected.Database || resolved.Company.DatabaseUUID != expected.Company.DatabaseUUID || resolved.Company.BookCode != expected.Company.BookCode || resolved.Company.EntityCode != expected.Company.EntityCode || resolved.Company.Currency != expected.Company.Currency) {
				return apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "company registration changed")
			}
			store, openErr := storesqlite.Open(ctx, resolved.Database, storesqlite.ReadOnly)
			if openErr != nil {
				return openErr
			}
			if _, verifyErr := VerifyCompanyIdentity(ctx, store, resolved); verifyErr != nil {
				_ = store.Close()
				return verifyErr
			}
			accounts, listErr := ledger.NewService(store, actor).ListAccounts(ctx, company.BookCode)
			_ = store.Close()
			if listErr != nil {
				return listErr
			}
			account, resolveAccountErr := ResolveAccount(accounts, setting)
			if resolveAccountErr != nil {
				return resolveAccountErr
			}
			switch key {
			case "defaults.payment-account":
				if subtype := NormalizedSubtype(account.Subtype); subtype != "BANK" && subtype != "CREDIT_CARD" {
					return apperr.New(apperr.Invalid, "DEFAULT_PAYMENT_ACCOUNT_INVALID", "payment default must be a bank or credit-card account")
				}
				company.Defaults.PaymentAccount = account.Code
			case "defaults.deposit-account":
				if NormalizedSubtype(account.Subtype) != "BANK" {
					return apperr.New(apperr.Invalid, "DEFAULT_DEPOSIT_ACCOUNT_INVALID", "deposit default must be a bank account")
				}
				company.Defaults.DepositAccount = account.Code
			case "defaults.retained-earnings":
				if account.Type != "EQUITY" {
					return apperr.New(apperr.Invalid, "RETAINED_EARNINGS_ACCOUNT_INVALID", "retained-earnings default must be an equity account")
				}
				company.Defaults.RetainedEarnings = account.Code
			}
			setting = account.Code
			value.Companies[selected] = company
		default:
			return apperr.New(apperr.Invalid, "CONFIG_KEY_UNSUPPORTED", "supported keys are default-company, output, and defaults.{payment-account,deposit-account,retained-earnings}")
		}
		return nil
	})
	if updateErr != nil {
		return booksconfig.Config{}, "", ConfigMutationError("write Books configuration", updateErr)
	}
	return value, setting, nil
}

func (s *Service) SetAccountDefault(ctx context.Context, key, selector string) (booksconfig.CompanyDefaults, error) {
	switch key {
	case "payment-account", "deposit-account", "retained-earnings":
	default:
		return booksconfig.CompanyDefaults{}, apperr.New(apperr.Invalid, "CONFIG_KEY_UNSUPPORTED", "choose payment-account, deposit-account, or retained-earnings")
	}
	if _, err := s.settings(); err != nil {
		return booksconfig.CompanyDefaults{}, err
	}
	value, _, err := setConfiguration(ctx, s.resolved.ConfigPath, s.company.Key, "defaults."+key, selector, s.actor, &s.resolved)
	if err != nil {
		return booksconfig.CompanyDefaults{}, err
	}
	return value.Companies[s.company.Key].Defaults, nil
}

type AccountDefaults struct {
	PaymentAccount   string `json:"payment_account"`
	DepositAccount   string `json:"deposit_account"`
	RetainedEarnings string `json:"retained_earnings"`
}

func (s *Service) AccountDefaults() (AccountDefaults, error) {
	settings, err := s.settings()
	if err != nil {
		return AccountDefaults{}, err
	}
	d := settings.Defaults
	return AccountDefaults{d.PaymentAccount, d.DepositAccount, d.RetainedEarnings}, nil
}
