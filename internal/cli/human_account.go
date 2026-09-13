package cli

import (
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

type humanAccountResult = application.AccountResult

type humanAccountListItem struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	NormalBalance    string `json:"normal_balance"`
	StatementSection string `json:"statement_section"`
	PostingEnabled   bool   `json:"posting_enabled"`
	ActiveFrom       string `json:"active_from"`
	ActiveTo         string `json:"active_to,omitempty"`
}

func newAccountsCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "List the selected company's chart of accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			accounts, err := ledger.NewService(store, opts.actor).ListAccounts(cmd.Context(), resolved.Company.BookCode)
			if err != nil {
				return err
			}
			values := make([]humanAccountListItem, 0, len(accounts))
			rows := make([][]string, 0, len(accounts))
			for _, account := range accounts {
				postingEnabled := account.PostingEnabled != nil && *account.PostingEnabled
				posting := "no"
				if postingEnabled {
					posting = "yes"
				}
				kind := application.AccountKind(account)
				values = append(values, humanAccountListItem{
					Code: account.Code, Name: account.Name, Kind: kind, Type: account.Type, Subtype: account.Subtype,
					NormalBalance: account.NormalBalance, StatementSection: account.StatementSection,
					PostingEnabled: postingEnabled, ActiveFrom: account.ActiveFrom, ActiveTo: account.ActiveTo,
				})
				rows = append(rows, []string{account.Code, account.Name, kind, account.Type, account.Subtype, posting, account.ActiveFrom})
			}
			return writeResult(cmd, opts.format, values,
				[]string{"CODE", "NAME", "KIND", "TYPE", "SUBTYPE", "POSTING", "ACTIVE FROM"}, rows)
		},
	}
}

func newHumanAccountAddCommand(opts *options) *cobra.Command {
	var code, activeFrom, reconcileFrom, currency string
	var noReconcile, defaultPayment, defaultDeposit, retainedEarnings bool
	command := &cobra.Command{
		Use:   "add KIND NAME...",
		Short: "Add an account using bookkeeping-friendly defaults",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openWorkflow(cmd, opts, true)
			if err != nil {
				return err
			}
			defer func() { _ = app.Close() }()
			result, err := app.AddAccount(cmd.Context(), application.AccountRequest{Kind: args[0], Name: strings.Join(args[1:], " "), Code: code, ActiveFrom: activeFrom, ReconcileFrom: reconcileFrom, Currency: currency, NoReconcile: noReconcile, DefaultPayment: defaultPayment, DefaultDeposit: defaultDeposit, RetainedEarnings: retainedEarnings, DryRun: opts.dryRun})
			if err != nil {
				return err
			}
			opts.loadedConfig = nil
			opts.resolved = nil
			return writeHumanAccountResult(cmd, opts, result)
		},
	}
	command.Flags().StringVar(&code, "code", "", "account code (automatically assigned when omitted)")
	command.Flags().StringVar(&activeFrom, "active-from", "", "first posting date (defaults to the first configured period)")
	command.Flags().StringVar(&reconcileFrom, "reconcile-from", "", "first required reconciliation date for statement accounts")
	command.Flags().StringVar(&currency, "currency", "", "statement currency (defaults to company currency)")
	command.Flags().BoolVar(&noReconcile, "no-reconcile", false, "do not create a statement/reconciliation account")
	command.Flags().BoolVar(&defaultPayment, "default-payment", false, "use this account when spend --from is omitted")
	command.Flags().BoolVar(&defaultDeposit, "default-deposit", false, "use this account when receive --to is omitted")
	command.Flags().BoolVar(&retainedEarnings, "retained-earnings", false, "use this equity account for year close")
	return command
}

func writeHumanAccountResult(cmd *cobra.Command, opts *options, result humanAccountResult) error {
	return writeResult(cmd, opts.format, result,
		[]string{"CODE", "NAME", "KIND", "BOOK", "STATEMENT ACCOUNT", "ACTIVE FROM", "DRY RUN"},
		[][]string{{result.Code, result.Name, result.Kind, result.Book, result.StatementAccount, result.ActiveFrom, fmt.Sprint(result.DryRun)}})
}
