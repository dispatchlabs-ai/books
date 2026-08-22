package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	"github.com/dispatchlabs-ai/books/internal/report"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

func scopeFrom(entity, group string) report.Scope {
	return report.Scope{EntityCode: entity, GroupCode: group}
}

func entityBreakdown(value report.Breakdown) string {
	parts := make([]string, 0, len(value.ByEntity))
	for _, amount := range value.ByEntity {
		parts = append(parts, amount.EntityCode+"="+money.Format(amount.Cents))
	}
	return strings.Join(parts, ", ")
}

func newReportCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "report", Short: "Generate posted-only entity or consolidated financial reports"}
	command.AddCommand(newGLCommand(opts), newTBCommand(opts), newPLCommand(opts), newBSCommand(opts))
	return command
}

func addScopeFlags(command *cobra.Command, entity, group *string) {
	command.Flags().StringVar(entity, "entity", "", "legal entity code (exclusive with --group)")
	command.Flags().StringVar(group, "group", "", "consolidation group code (exclusive with --entity)")
}

func newGLCommand(opts *options) *cobra.Command {
	var entity, group, from, to, account string
	var zero bool
	command := &cobra.Command{
		Use: "general-ledger", Aliases: []string{"gl"}, Short: "Generate a detailed general ledger", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := applyHumanReportDefaults(cmd, opts, store, &entity, &group, &from, &to, "gl"); err != nil {
				return err
			}
			if strings.TrimSpace(account) != "" && strings.TrimSpace(opts.database) == "" {
				resolved, err := opts.resolveCompany()
				if err != nil {
					return err
				}
				accounts, err := ledger.NewService(store, opts.actor).ListAccounts(cmd.Context(), resolved.Company.BookCode)
				if err != nil {
					return err
				}
				selected, err := resolveHumanAccount(accounts, account)
				if err != nil {
					return err
				}
				account = selected.Code
			}
			result, err := report.NewService(store).GeneralLedger(cmd.Context(), report.GeneralLedgerInput{Scope: scopeFrom(entity, group), FromDate: from, ToDate: to, AccountCode: account, IncludeZero: zero})
			if err != nil {
				return err
			}
			var rows [][]string
			for _, accountResult := range result.Accounts {
				rows = append(rows, []string{accountResult.Account.Code, from, "OPENING", "", "", "Opening balance", "", "", money.Format(accountResult.OpeningBalance.ConsolidatedCents)})
				for _, line := range accountResult.Lines {
					rows = append(rows, []string{accountResult.Account.Code, line.PostingDate, line.BookCode, line.EntityCode, fmt.Sprintf("%d", line.EntryNumber), firstNonempty(line.LineDescription, line.JournalDescription), money.Format(line.DebitCents), money.Format(line.CreditCents), money.Format(line.RunningBalanceCents)})
				}
				rows = append(rows, []string{accountResult.Account.Code, to, "CLOSING", "", "", "Closing balance", "", "", money.Format(accountResult.ClosingBalance.ConsolidatedCents)})
			}
			return writeResult(cmd, opts.format, result, []string{"ACCOUNT", "DATE", "BOOK", "ENTITY", "JOURNAL", "DESCRIPTION", "DEBIT", "CREDIT", "BALANCE"}, rows)
		},
	}
	addScopeFlags(command, &entity, &group)
	command.Flags().StringVar(&from, "from", "", "inclusive start date")
	command.Flags().StringVar(&to, "to", "", "inclusive end date")
	command.Flags().StringVar(&account, "account", "", "optional account code")
	command.Flags().BoolVar(&zero, "include-zero", false, "include zero-balance accounts")
	return command
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func newTBCommand(opts *options) *cobra.Command {
	var entity, group, asOf string
	var zero bool
	command := &cobra.Command{
		Use: "trial-balance", Aliases: []string{"tb"}, Short: "Generate a trial balance that must balance exactly", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := applyHumanReportDefaults(cmd, opts, store, &entity, &group, nil, &asOf, "as-of"); err != nil {
				return err
			}
			result, err := report.NewService(store).TrialBalance(cmd.Context(), report.TrialBalanceInput{Scope: scopeFrom(entity, group), AsOfDate: asOf, IncludeZero: zero})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result.Rows)+1)
			for _, row := range result.Rows {
				rows = append(rows, []string{row.Account.Code, row.Account.Name, row.Account.Type, entityBreakdown(row.Balance), money.Format(row.Balance.EliminationsCents), money.Format(row.DebitCents), money.Format(row.CreditCents)})
			}
			rows = append(rows, []string{"TOTAL", "", "", "", "", money.Format(result.TotalDebitCents), money.Format(result.TotalCreditCents)})
			return writeResult(cmd, opts.format, result, []string{"ACCOUNT", "NAME", "TYPE", "BY ENTITY", "ELIMINATIONS", "DEBIT", "CREDIT"}, rows)
		},
	}
	addScopeFlags(command, &entity, &group)
	command.Flags().StringVar(&asOf, "as-of", "", "inclusive as-of date")
	command.Flags().BoolVar(&zero, "include-zero", false, "include zero-balance accounts")
	return command
}

func newPLCommand(opts *options) *cobra.Command {
	var entity, group, from, to string
	var zero bool
	command := &cobra.Command{
		Use: "profit-loss", Aliases: []string{"income-statement", "pl"}, Short: "Generate a profit and loss statement", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := applyHumanReportDefaults(cmd, opts, store, &entity, &group, &from, &to, "pl"); err != nil {
				return err
			}
			result, err := report.NewService(store).ProfitLoss(cmd.Context(), report.ProfitLossInput{Scope: scopeFrom(entity, group), FromDate: from, ToDate: to, IncludeZero: zero})
			if err != nil {
				return err
			}
			var rows [][]string
			appendRows := func(section string, values []report.StatementRow) {
				for _, row := range values {
					rows = append(rows, []string{section, row.Account.Code, row.Account.Name, entityBreakdown(row.Amount), money.Format(row.Amount.EliminationsCents), money.Format(row.Amount.ConsolidatedCents)})
				}
			}
			appendRows("REVENUE", result.Revenue)
			rows = append(rows, []string{"TOTAL REVENUE", "", "", entityBreakdown(result.TotalRevenue), money.Format(result.TotalRevenue.EliminationsCents), money.Format(result.TotalRevenue.ConsolidatedCents)})
			appendRows("EXPENSE", result.Expenses)
			rows = append(rows, []string{"TOTAL EXPENSES", "", "", entityBreakdown(result.TotalExpenses), money.Format(result.TotalExpenses.EliminationsCents), money.Format(result.TotalExpenses.ConsolidatedCents)})
			rows = append(rows, []string{"NET INCOME", "", "", entityBreakdown(result.NetIncome), money.Format(result.NetIncome.EliminationsCents), money.Format(result.NetIncome.ConsolidatedCents)})
			return writeResult(cmd, opts.format, result, []string{"SECTION", "ACCOUNT", "NAME", "BY ENTITY", "ELIMINATIONS", "CONSOLIDATED"}, rows)
		},
	}
	addScopeFlags(command, &entity, &group)
	command.Flags().StringVar(&from, "from", "", "inclusive start date")
	command.Flags().StringVar(&to, "to", "", "inclusive end date")
	command.Flags().BoolVar(&zero, "include-zero", false, "include zero-balance accounts")
	return command
}

func newBSCommand(opts *options) *cobra.Command {
	var entity, group, asOf string
	var zero bool
	command := &cobra.Command{
		Use: "balance-sheet", Aliases: []string{"bs"}, Short: "Generate a balance sheet whose equation must hold exactly", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := applyHumanReportDefaults(cmd, opts, store, &entity, &group, nil, &asOf, "as-of"); err != nil {
				return err
			}
			result, err := report.NewService(store).BalanceSheet(cmd.Context(), report.BalanceSheetInput{Scope: scopeFrom(entity, group), AsOfDate: asOf, IncludeZero: zero})
			if err != nil {
				return err
			}
			var rows [][]string
			appendRows := func(section string, values []report.StatementRow) {
				for _, row := range values {
					rows = append(rows, []string{section, row.Account.Code, row.Account.Name, entityBreakdown(row.Amount), money.Format(row.Amount.EliminationsCents), money.Format(row.Amount.ConsolidatedCents)})
				}
			}
			appendRows("ASSET", result.Assets)
			rows = append(rows, []string{"TOTAL ASSETS", "", "", entityBreakdown(result.TotalAssets), money.Format(result.TotalAssets.EliminationsCents), money.Format(result.TotalAssets.ConsolidatedCents)})
			appendRows("LIABILITY", result.Liabilities)
			rows = append(rows, []string{"TOTAL LIABILITIES", "", "", entityBreakdown(result.TotalLiabilities), money.Format(result.TotalLiabilities.EliminationsCents), money.Format(result.TotalLiabilities.ConsolidatedCents)})
			appendRows("EQUITY", result.Equity)
			rows = append(rows, []string{"POSTED EQUITY", "", "", entityBreakdown(result.PostedEquity), money.Format(result.PostedEquity.EliminationsCents), money.Format(result.PostedEquity.ConsolidatedCents)})
			rows = append(rows, []string{"CURRENT EARNINGS", "", "", entityBreakdown(result.CurrentEarnings), money.Format(result.CurrentEarnings.EliminationsCents), money.Format(result.CurrentEarnings.ConsolidatedCents)})
			rows = append(rows, []string{"TOTAL EQUITY", "", "", entityBreakdown(result.TotalEquity), money.Format(result.TotalEquity.EliminationsCents), money.Format(result.TotalEquity.ConsolidatedCents)})
			return writeResult(cmd, opts.format, result, []string{"SECTION", "ACCOUNT", "NAME", "BY ENTITY", "ELIMINATIONS", "CONSOLIDATED"}, rows)
		},
	}
	addScopeFlags(command, &entity, &group)
	command.Flags().StringVar(&asOf, "as-of", "", "inclusive as-of date")
	command.Flags().BoolVar(&zero, "include-zero", false, "include zero-balance accounts")
	return command
}

func newRootReportAlias(opts *options, name string, command *cobra.Command) *cobra.Command {
	command.Use = name
	command.Aliases = nil
	return command
}

func applyHumanReportDefaults(cmd *cobra.Command, opts *options, store *storesqlite.Store, entity, group, from, to *string, mode string) error {
	if entity != nil && group != nil && strings.TrimSpace(*entity) == "" && strings.TrimSpace(*group) == "" && strings.TrimSpace(opts.database) == "" {
		resolved, err := opts.resolveCompany()
		if err != nil {
			return err
		}
		*entity = resolved.Company.EntityCode
	}
	needsDates := (from != nil && strings.TrimSpace(*from) == "") || (to != nil && strings.TrimSpace(*to) == "")
	if !needsDates {
		return nil
	}
	book := ""
	if strings.TrimSpace(opts.database) == "" {
		resolved, err := opts.resolveCompany()
		if err != nil {
			return err
		}
		book = resolved.Company.BookCode
	}
	if book == "" {
		return nil
	}
	periods, err := ledger.NewService(store, opts.actor).ListPeriods(cmd.Context(), book)
	if err != nil {
		return err
	}
	if len(periods) == 0 {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	selected := periods[0]
	for _, period := range periods {
		if period.StartDate <= today && today <= period.EndDate {
			selected = period
			break
		}
		if period.EndDate <= today {
			selected = period
		}
	}
	effectiveEnd := today
	if effectiveEnd > selected.EndDate {
		effectiveEnd = selected.EndDate
	}
	if effectiveEnd < selected.StartDate {
		effectiveEnd = selected.EndDate
	}
	if to != nil && strings.TrimSpace(*to) == "" {
		*to = effectiveEnd
	}
	if from != nil && strings.TrimSpace(*from) == "" {
		*from = selected.StartDate
		if mode == "pl" {
			for _, period := range periods {
				if period.FiscalYear == selected.FiscalYear && period.StartDate < *from {
					*from = period.StartDate
				}
			}
		}
	}
	return nil
}
