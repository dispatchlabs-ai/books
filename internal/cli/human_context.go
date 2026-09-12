package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

type companyCreateOptions struct {
	name          string
	key           string
	currency      string
	basis         string
	start         string
	fiscalYearEnd string
	periods       string
	chart         string
	makeDefault   bool
}

type companyCreateResult struct {
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

func newInitCommand(opts *options) *cobra.Command {
	values := defaultCompanyCreateOptions()
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.books/books.toml and the first registered company",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := opts.resolveConfigPath()
			if err != nil {
				return err
			}
			values.makeDefault = true
			return createRegisteredCompany(cmd, opts, path, true, values)
		},
	}
	addCompanyCreateFlags(command, &values, true)
	return command
}

func newCompanyCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "company", Short: "Manage companies registered in ~/.books/books.toml"}
	values := defaultCompanyCreateOptions()
	add := &cobra.Command{
		Use:   "add",
		Short: "Create and register another company",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := opts.resolveConfigPath()
			if err != nil {
				return err
			}
			return createRegisteredCompany(cmd, opts, path, false, values)
		},
	}
	addCompanyCreateFlags(add, &values, false)
	defaultCommand := &cobra.Command{
		Use: "default COMPANY", Short: "Make a registered company the default", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := opts.resolveConfigPath()
			if err != nil {
				return err
			}
			key := strings.ToLower(strings.TrimSpace(args[0]))
			if opts.dryRun {
				value, loadErr := booksconfig.Load(path)
				if loadErr != nil {
					return apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", loadErr)
				}
				if _, ok := value.Companies[key]; !ok {
					return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is not registered", key))
				}
			} else {
				value, updateErr := booksconfig.Update(path, nil, func(value *booksconfig.Config, _ bool) error {
					if _, ok := value.Companies[key]; !ok {
						return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is not registered", key))
					}
					value.DefaultCompany = key
					return nil
				})
				if updateErr != nil {
					return configMutationError("set default company", updateErr)
				}
				opts.loadedConfig = &value
				opts.resolved = nil
			}
			data := map[string]any{"default_company": key, "config_path": path, "dry_run": opts.dryRun}
			return writeResult(cmd, opts.format, data, []string{"DEFAULT COMPANY", "CONFIG", "DRY RUN"}, [][]string{{key, path, fmt.Sprint(opts.dryRun)}})
		},
	}
	command.AddCommand(add, defaultCommand, newCompanyListCommand(opts, "list"))
	return command
}

func newCompaniesCommand(opts *options) *cobra.Command {
	return newCompanyListCommand(opts, "companies")
}

func newCompanyListCommand(opts *options, use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "List registered companies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			value, path, err := opts.loadConfig()
			if err != nil {
				return err
			}
			type row struct {
				Default    bool   `json:"default"`
				Company    string `json:"company"`
				Name       string `json:"name"`
				Currency   string `json:"currency"`
				EntityCode string `json:"entity_code"`
				Database   string `json:"database"`
			}
			var data []row
			var rows [][]string
			for _, key := range value.CompanyKeys() {
				resolved, err := value.Resolve(path, key)
				if err != nil {
					return err
				}
				item := row{Default: key == value.DefaultCompany, Company: key, Name: resolved.Company.Name, Currency: resolved.Company.Currency, EntityCode: resolved.Company.EntityCode, Database: resolved.Database}
				data = append(data, item)
				marker := ""
				if item.Default {
					marker = "*"
				}
				rows = append(rows, []string{marker, item.Company, item.Name, item.Currency, item.EntityCode, item.Database})
			}
			return writeResult(cmd, opts.format, data, []string{"DEFAULT", "COMPANY", "NAME", "CURRENCY", "ENTITY", "DATABASE"}, rows)
		},
	}
}

func defaultCompanyCreateOptions() companyCreateOptions {
	return companyCreateOptions{
		currency: "USD", basis: "accrual", fiscalYearEnd: "december", periods: "monthly", chart: "starter",
	}
}

func addCompanyCreateFlags(command *cobra.Command, values *companyCreateOptions, first bool) {
	command.Flags().StringVar(&values.name, "name", "", "legal company name (required)")
	command.Flags().StringVar(&values.key, "company", "", "registry key (derived from name when omitted)")
	command.Flags().StringVar(&values.currency, "currency", values.currency, "three-letter functional currency")
	command.Flags().StringVar(&values.basis, "basis", values.basis, "accounting basis (currently accrual only)")
	command.Flags().StringVar(&values.start, "start", "", "first fiscal period start date (defaults to current fiscal year)")
	command.Flags().StringVar(&values.fiscalYearEnd, "fiscal-year-end", values.fiscalYearEnd, "fiscal year ending month")
	command.Flags().StringVar(&values.periods, "periods", values.periods, "period cadence (monthly)")
	command.Flags().StringVar(&values.chart, "chart", values.chart, "initial chart: starter or empty")
	if !first {
		command.Flags().BoolVar(&values.makeDefault, "default", false, "make this the default company")
	}
}

func createRegisteredCompany(cmd *cobra.Command, opts *options, configPath string, initialize bool, values companyCreateOptions) error {
	values.name = strings.TrimSpace(values.name)
	if values.name == "" {
		return apperr.New(apperr.Invalid, "COMPANY_NAME_REQUIRED", "--name is required; example: books init --name \"Acme Services, Inc.\"")
	}
	key := strings.ToLower(strings.TrimSpace(values.key))
	if key == "" {
		key = booksconfig.DeriveCompanyKey(values.name)
	}
	if err := booksconfig.ValidateCompanyKey(key); err != nil {
		return apperr.Wrap(apperr.Invalid, "COMPANY_KEY_INVALID", "validate company key", err)
	}
	currency := strings.ToUpper(strings.TrimSpace(values.currency))
	if len(currency) != 3 || strings.IndexFunc(currency, func(character rune) bool { return character < 'A' || character > 'Z' }) != -1 {
		return apperr.New(apperr.Invalid, "CURRENCY_INVALID", "--currency must be a three-letter code such as USD")
	}
	basis := strings.ToUpper(strings.TrimSpace(values.basis))
	if basis != "ACCRUAL" {
		return apperr.New(apperr.Invalid, "BASIS_NOT_SUPPORTED", "cash-basis accounting is not supported; use --basis accrual")
	}
	if strings.ToLower(strings.TrimSpace(values.periods)) != "monthly" {
		return apperr.New(apperr.Invalid, "PERIOD_CADENCE_UNSUPPORTED", "--periods currently supports monthly")
	}
	chart := strings.ToLower(strings.TrimSpace(values.chart))
	if chart != "starter" && chart != "empty" {
		return apperr.New(apperr.Invalid, "CHART_INVALID", "--chart must be starter or empty")
	}
	endMonth, err := parseMonth(values.fiscalYearEnd)
	if err != nil {
		return err
	}
	startDate, err := fiscalStart(values.start, endMonth)
	if err != nil {
		return err
	}
	periods := monthlyPeriods(startDate, endMonth)
	accountCount := 0
	if chart == "starter" {
		accountCount = len(starterAccounts())
	}
	prepare := func(current *booksconfig.Config) (booksconfig.ResolvedCompany, companyCreateResult, error) {
		if current.Companies == nil {
			current.Companies = make(map[string]booksconfig.Company)
		}
		if _, exists := current.Companies[key]; exists {
			return booksconfig.ResolvedCompany{}, companyCreateResult{}, apperr.New(apperr.Conflict, "COMPANY_EXISTS", fmt.Sprintf("company %q is already registered", key))
		}
		company := booksconfig.NewCompany(key, values.name, currency, basis)
		company.FiscalYearEnd = int(endMonth)
		current.Companies[key] = company
		if current.DefaultCompany == "" || values.makeDefault {
			current.DefaultCompany = key
		}
		resolved, resolveErr := current.Resolve(configPath, key)
		if resolveErr != nil {
			return booksconfig.ResolvedCompany{}, companyCreateResult{}, apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve new company", resolveErr)
		}
		result := companyCreateResult{
			Company: key, Name: values.name, EntityCode: company.EntityCode, Currency: currency, Basis: basis,
			StartDate: startDate.Format("2006-01-02"), PeriodCount: len(periods), Chart: chart, AccountCount: accountCount,
			ConfigPath: configPath, Database: resolved.Database, Backups: resolved.Backups, Plans: resolved.Plans,
			Default: current.DefaultCompany == key, DryRun: opts.dryRun,
		}
		return resolved, result, nil
	}
	if opts.dryRun {
		var current booksconfig.Config
		if initialize {
			if _, statErr := os.Stat(configPath); statErr == nil {
				return apperr.New(apperr.Conflict, "CONFIG_EXISTS", fmt.Sprintf("Books is already initialized at %s; use books company add", configPath))
			} else if !os.IsNotExist(statErr) {
				return apperr.Wrap(apperr.Unavailable, "CONFIG_STAT_FAILED", "inspect Books configuration", statErr)
			}
			current = booksconfig.New()
		} else {
			loaded, loadErr := booksconfig.Load(configPath)
			if loadErr != nil {
				return apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", loadErr)
			}
			current = loaded
		}
		_, result, prepareErr := prepare(&current)
		if prepareErr != nil {
			return prepareErr
		}
		return writeCompanyCreateResult(cmd, opts, result)
	}
	var result companyCreateResult
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
	updatedConfig, err := booksconfig.Update(configPath, initial, func(current *booksconfig.Config, existed bool) error {
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
		store, initErr := storesqlite.Init(cmd.Context(), resolved.Database, currency, opts.actor)
		if initErr != nil {
			return initErr
		}
		company := current.Companies[key]
		if scanErr := store.DB().QueryRowContext(cmd.Context(), `SELECT database_uuid
			FROM database_metadata WHERE singleton = 1`).Scan(&company.DatabaseUUID); scanErr != nil {
			_ = store.Close()
			return storesqlite.MapError("read initialized company database identity", scanErr)
		}
		current.Companies[key] = company
		service := ledger.NewService(store, opts.actor)
		if _, createErr := service.CreateEntity(cmd.Context(), ledger.CreateEntityInput{
			Code: company.EntityCode, LegalName: company.Name, Currency: company.Currency,
			BookCode: company.BookCode, BookName: company.Name + " Actual", Basis: company.Basis,
		}); createErr != nil {
			_ = store.Close()
			return createErr
		}
		for _, period := range periods {
			if _, periodErr := service.CreatePeriod(cmd.Context(), period); periodErr != nil {
				_ = store.Close()
				return periodErr
			}
		}
		if chart == "starter" {
			for _, account := range starterAccounts() {
				account.BookCodes = []string{company.BookCode}
				account.ActiveFrom = startDate.Format("2006-01-02")
				if _, accountErr := service.CreateAccount(cmd.Context(), account); accountErr != nil {
					_ = store.Close()
					return accountErr
				}
			}
			company.Defaults.RetainedEarnings = "3100"
			current.Companies[key] = company
		}
		if _, doctorErr := store.Doctor(cmd.Context()); doctorErr != nil {
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
		return configMutationError("write Books configuration", err)
	}
	createdRoot = false
	opts.loadedConfig = &updatedConfig
	opts.resolved = nil
	return writeCompanyCreateResult(cmd, opts, result)
}

func configMutationError(action string, err error) error {
	if _, ok := apperr.As(err); ok {
		return err
	}
	if os.IsNotExist(err) {
		return apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", err)
	}
	return apperr.Wrap(apperr.Unavailable, "CONFIG_WRITE_FAILED", action, err)
}

func writeCompanyCreateResult(cmd *cobra.Command, opts *options, result companyCreateResult) error {
	return writeResult(cmd, opts.format, result,
		[]string{"COMPANY", "NAME", "CURRENCY", "START", "PERIODS", "CHART", "DATABASE", "DEFAULT", "DRY RUN"},
		[][]string{{result.Company, result.Name, result.Currency, result.StartDate, strconv.Itoa(result.PeriodCount), result.Chart, result.Database, fmt.Sprint(result.Default), fmt.Sprint(result.DryRun)}})
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
	now := time.Now()
	startMonth := endMonth%12 + 1
	year := now.Year()
	if now.Month() < startMonth {
		year--
	}
	return time.Date(year, startMonth, 1, 0, 0, 0, 0, time.Local), nil
}

func monthlyPeriods(start time.Time, endMonth time.Month) []ledger.CreatePeriodInput {
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

func newConfigCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "Inspect or update ~/.books/books.toml without interactive input"}
	command.AddCommand(
		&cobra.Command{
			Use: "path", Short: "Print the resolved Books configuration path", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				path, err := opts.resolveConfigPath()
				if err != nil {
					return err
				}
				return writeResult(cmd, opts.format, map[string]any{"path": path}, []string{"PATH"}, [][]string{{path}})
			},
		},
		newConfigGetCommand(opts),
		newConfigSetCommand(opts),
	)
	return command
}

func newConfigGetCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use: "get [KEY]", Short: "Read configuration values", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value, _, err := opts.loadConfig()
			if err != nil {
				return err
			}
			key := ""
			if len(args) == 1 {
				key = args[0]
			}
			switch key {
			case "":
				data := map[string]any{"default_company": value.DefaultCompany, "output": value.Defaults.Output}
				return writeResult(cmd, opts.format, data, []string{"KEY", "VALUE"}, [][]string{{"default-company", value.DefaultCompany}, {"output", value.Defaults.Output}})
			case "default-company":
				return writeResult(cmd, opts.format, map[string]any{"default_company": value.DefaultCompany}, []string{"KEY", "VALUE"}, [][]string{{"default-company", value.DefaultCompany}})
			case "output":
				return writeResult(cmd, opts.format, map[string]any{"output": value.Defaults.Output}, []string{"KEY", "VALUE"}, [][]string{{"output", value.Defaults.Output}})
			case "defaults":
				resolved, err := opts.resolveCompany()
				if err != nil {
					return err
				}
				defaults := resolved.Company.Defaults
				return writeResult(cmd, opts.format, defaults, []string{"PAYMENT ACCOUNT", "DEPOSIT ACCOUNT", "RETAINED EARNINGS"}, [][]string{{defaults.PaymentAccount, defaults.DepositAccount, defaults.RetainedEarnings}})
			default:
				return apperr.New(apperr.Invalid, "CONFIG_KEY_UNSUPPORTED", "supported keys are default-company, output, and defaults")
			}
		},
	}
}

func newConfigSetCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use: "set KEY VALUE", Short: "Set one supported configuration value", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.dryRun {
				return apperr.New(apperr.Validation, "DRY_RUN_UNSUPPORTED", "config set does not need a dry run; configuration writes are atomic")
			}
			path, err := opts.resolveConfigPath()
			if err != nil {
				return err
			}
			key, setting := args[0], strings.TrimSpace(args[1])
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
					selected := strings.ToLower(strings.TrimSpace(opts.company))
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
					store, openErr := storesqlite.Open(cmd.Context(), resolved.Database, storesqlite.ReadOnly)
					if openErr != nil {
						return openErr
					}
					if verifyErr := store.VerifySchema(cmd.Context()); verifyErr != nil {
						_ = store.Close()
						return verifyErr
					}
					accounts, listErr := ledger.NewService(store, opts.actor).ListAccounts(cmd.Context(), company.BookCode)
					_ = store.Close()
					if listErr != nil {
						return listErr
					}
					account, resolveAccountErr := resolveHumanAccount(accounts, setting)
					if resolveAccountErr != nil {
						return resolveAccountErr
					}
					switch key {
					case "defaults.payment-account":
						if subtype := normalizedSubtype(account.Subtype); subtype != "BANK" && subtype != "CREDIT_CARD" {
							return apperr.New(apperr.Invalid, "DEFAULT_PAYMENT_ACCOUNT_INVALID", "payment default must be a bank or credit-card account")
						}
						company.Defaults.PaymentAccount = account.Code
					case "defaults.deposit-account":
						if normalizedSubtype(account.Subtype) != "BANK" {
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
				return configMutationError("write Books configuration", updateErr)
			}
			opts.loadedConfig = &value
			opts.resolved = nil
			data := map[string]any{"key": key, "value": setting, "config_path": path}
			return writeResult(cmd, opts.format, data, []string{"KEY", "VALUE", "CONFIG"}, [][]string{{key, setting, path}})
		},
	}
}

func runDashboard(cmd *cobra.Command, opts *options) error {
	resolved, err := opts.resolveCompany()
	if err != nil {
		return err
	}
	store, err := storesqlite.Open(cmd.Context(), resolved.Database, storesqlite.ReadOnly)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	var accountCount, postedCount, draftCount, openPeriods int
	queries := []struct {
		query  string
		target *int
	}{
		{"SELECT COUNT(*) FROM accounts", &accountCount},
		{"SELECT COUNT(*) FROM journal_entries WHERE status = 'POSTED'", &postedCount},
		{"SELECT COUNT(*) FROM journal_entries WHERE status = 'DRAFT'", &draftCount},
		{"SELECT COUNT(*) FROM book_periods WHERE status = 'OPEN'", &openPeriods},
	}
	for _, query := range queries {
		if err := store.DB().QueryRowContext(cmd.Context(), query.query).Scan(query.target); err != nil {
			return err
		}
	}
	data := map[string]any{
		"company": resolved.Key, "name": resolved.Company.Name, "entity_code": resolved.Company.EntityCode,
		"currency": resolved.Company.Currency, "database": resolved.Database, "accounts": accountCount,
		"posted_transactions": postedCount, "drafts": draftCount, "open_periods": openPeriods,
	}
	return writeResult(cmd, opts.format, data,
		[]string{"COMPANY", "NAME", "CURRENCY", "ACCOUNTS", "POSTED", "DRAFTS", "OPEN PERIODS", "DATABASE"},
		[][]string{{resolved.Key, resolved.Company.Name, resolved.Company.Currency, strconv.Itoa(accountCount), strconv.Itoa(postedCount), strconv.Itoa(draftCount), strconv.Itoa(openPeriods), resolved.Database}})
}
