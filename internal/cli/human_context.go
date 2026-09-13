package cli

import (
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/application"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"

	"github.com/spf13/cobra"
)

type companyCreateOptions = application.CompanyCreateOptions

type companyCreateResult = application.CompanyCreateResult

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
			values.MakeDefault = true
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
					return application.ConfigMutationError("set default company", updateErr)
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
		Currency: "USD", Basis: "accrual", FiscalYearEnd: "december", Periods: "monthly", Chart: "starter",
	}
}

func addCompanyCreateFlags(command *cobra.Command, values *companyCreateOptions, first bool) {
	command.Flags().StringVar(&values.Name, "name", "", "legal company name (required)")
	command.Flags().StringVar(&values.Key, "company", "", "registry key (derived from name when omitted)")
	command.Flags().StringVar(&values.Currency, "currency", values.Currency, "three-letter functional currency")
	command.Flags().StringVar(&values.Basis, "basis", values.Basis, "accounting basis (currently accrual only)")
	command.Flags().StringVar(&values.Start, "start", "", "first fiscal period start date (defaults to current fiscal year)")
	command.Flags().StringVar(&values.FiscalYearEnd, "fiscal-year-end", values.FiscalYearEnd, "fiscal year ending month")
	command.Flags().StringVar(&values.Periods, "periods", values.Periods, "period cadence (monthly)")
	command.Flags().StringVar(&values.Chart, "chart", values.Chart, "initial chart: starter or empty")
	if !first {
		command.Flags().BoolVar(&values.MakeDefault, "default", false, "make this the default company")
	}
}

func createRegisteredCompany(cmd *cobra.Command, opts *options, configPath string, initialize bool, values companyCreateOptions) error {
	month, err := parseMonth(values.FiscalYearEnd)
	if err != nil {
		return err
	}
	start, err := fiscalStart(values.Start, month)
	if err != nil {
		return err
	}
	values.Start = start.Format("2006-01-02")
	result, err := application.CreateRegisteredCompany(cmd.Context(), configPath, opts.actor, initialize, opts.dryRun, values)
	if err != nil {
		return err
	}
	opts.loadedConfig = nil
	opts.resolved = nil
	return writeCompanyCreateResult(cmd, opts, result)
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
			key := args[0]
			value, setting, err := application.SetConfiguration(cmd.Context(), path, opts.company, key, args[1], opts.actor)
			if err != nil {
				return err
			}

			opts.loadedConfig = &value
			opts.resolved = nil
			data := map[string]any{"key": key, "value": setting, "config_path": path}
			return writeResult(cmd, opts.format, data, []string{"KEY", "VALUE", "CONFIG"}, [][]string{{key, setting, path}})
		},
	}
}

func runDashboard(cmd *cobra.Command, opts *options) error {
	app, err := openWorkflow(cmd, opts, false)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	summary, err := app.Dashboard(cmd.Context())
	if err != nil {
		return err
	}
	resolved, err := opts.resolveCompany()
	if err != nil {
		return err
	}
	data := map[string]any{"company": summary.Company, "name": summary.Name, "entity_code": summary.EntityCode, "currency": summary.Currency, "database": resolved.Database, "accounts": summary.Accounts, "posted_transactions": summary.PostedTransactions, "drafts": summary.Drafts, "open_periods": summary.OpenPeriods}
	return writeResult(cmd, opts.format, data, []string{"COMPANY", "NAME", "CURRENCY", "ACCOUNTS", "POSTED", "DRAFTS", "OPEN PERIODS", "DATABASE"}, [][]string{{summary.Company, summary.Name, summary.Currency, strconv.Itoa(summary.Accounts), strconv.Itoa(summary.PostedTransactions), strconv.Itoa(summary.Drafts), strconv.Itoa(summary.OpenPeriods), resolved.Database}})
}
