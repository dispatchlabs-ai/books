package cli

import (
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func newAccountCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "account", Short: "Manage the master chart of accounts and book activation"}
	var code, name, accountType, subtype, normal, section, books, activeFrom string
	create := &cobra.Command{
		Use: "create", Short: "Create a master account and enable it in selected books", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.CreateAccountInput{Code: code, Name: name, Type: accountType, Subtype: subtype, NormalBalance: normal, StatementSection: section, BookCodes: splitCSV(books), ActiveFrom: activeFrom}
			if err := requireCommit(opts, "account create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CreateAccount(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"ACCOUNT", "NAME", "TYPE", "SUBTYPE", "NORMAL", "ID"}, [][]string{{result.Code, result.Name, result.Type, result.Subtype, result.NormalBalance, result.ID}})
		},
	}
	create.Flags().StringVar(&code, "code", "", "unique account code")
	create.Flags().StringVar(&name, "name", "", "account name")
	create.Flags().StringVar(&accountType, "type", "", "ASSET, LIABILITY, EQUITY, REVENUE, or EXPENSE")
	create.Flags().StringVar(&subtype, "subtype", "", "account subtype")
	create.Flags().StringVar(&normal, "normal", "", "DEBIT or CREDIT (defaults from type)")
	create.Flags().StringVar(&section, "section", "", "financial-statement section")
	create.Flags().StringVar(&books, "books", "", "comma-separated book codes")
	create.Flags().StringVar(&activeFrom, "active-from", "1900-01-01", "posting activation date")
	var book string
	list := &cobra.Command{
		Use: "list", Short: "List accounts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			accounts, err := ledger.NewService(store, opts.actor).ListAccounts(cmd.Context(), book)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(accounts))
			if strings.TrimSpace(book) != "" {
				for _, account := range accounts {
					posting := ""
					if account.PostingEnabled != nil {
						posting = fmt.Sprint(*account.PostingEnabled)
					}
					rows = append(rows, []string{account.BookCode, account.Code, account.Name, account.Type,
						account.Subtype, account.NormalBalance, account.StatementSection, posting,
						account.ActiveFrom, account.ActiveTo, account.ID})
				}
				return writeResult(cmd, opts.format, accounts,
					[]string{"BOOK", "ACCOUNT", "NAME", "TYPE", "SUBTYPE", "NORMAL", "SECTION", "POSTING", "ACTIVE FROM", "ACTIVE TO", "ID"}, rows)
			}
			for _, account := range accounts {
				rows = append(rows, []string{account.Code, account.Name, account.Type, account.Subtype,
					account.NormalBalance, account.StatementSection, account.ID})
			}
			return writeResult(cmd, opts.format, accounts, []string{"ACCOUNT", "NAME", "TYPE", "SUBTYPE", "NORMAL", "SECTION", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&book, "book", "", "optional book code")
	var configureAccount, configureBook, configureFrom, configureTo string
	var postingEnabled bool
	configure := &cobra.Command{
		Use: "configure", Short: "Enable or configure an account's posting activation in one book", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "account configure"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).ConfigureBookAccount(cmd.Context(), configureBook, configureAccount, configureFrom, configureTo, postingEnabled); err != nil {
				return err
			}
			data := map[string]any{"book": strings.ToUpper(configureBook), "account": strings.ToUpper(configureAccount), "active_from": configureFrom, "active_to": configureTo, "posting_enabled": postingEnabled}
			return writeResult(cmd, opts.format, data, []string{"BOOK", "ACCOUNT", "ACTIVE FROM", "ACTIVE TO", "POSTING"}, [][]string{{strings.ToUpper(configureBook), strings.ToUpper(configureAccount), configureFrom, configureTo, fmt.Sprint(postingEnabled)}})
		},
	}
	configure.Flags().StringVar(&configureBook, "book", "", "book code")
	configure.Flags().StringVar(&configureAccount, "account", "", "account code")
	configure.Flags().StringVar(&configureFrom, "active-from", "1900-01-01", "first posting date")
	configure.Flags().StringVar(&configureTo, "active-to", "", "optional last posting date")
	configure.Flags().BoolVar(&postingEnabled, "posting-enabled", true, "allow new postings")
	command.AddCommand(newHumanAccountAddCommand(opts), create, list, configure, newAccountIdentityCommand(opts))
	return command
}

func accountIdentityRow(value ledger.AccountIdentity) []string {
	return []string{
		value.EntityCode, value.AccountCode, value.SourceSystem, value.ExternalID,
		value.AccountNumber, value.Name, fmt.Sprint(value.Active), value.Evidence.SourceKind,
		value.Evidence.SourcePath, value.Evidence.Locator, value.ID,
	}
}

func newAccountIdentityCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "identity", Short: "Manage immutable external account identities"}
	var entity, account, sourceSystem, externalID, accountNumber, name string
	var sourceKind, sourcePath, sourceSHA256, locator, payloadSHA256 string
	var active bool
	add := &cobra.Command{
		Use: "add", Short: "Tie an external account identity to a master account", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.AddAccountIdentityInput{
				Entity: entity, Account: account, SourceSystem: sourceSystem, ExternalID: externalID,
				AccountNumber: accountNumber, Name: name, Active: active,
				Evidence: ledger.AccountIdentityEvidence{
					SourceKind: sourceKind, SourcePath: sourcePath, SourceSHA256: sourceSHA256,
					Locator: locator, PayloadSHA256: payloadSHA256,
				},
			}
			if err := requireCommit(opts, "account identity add"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).AddAccountIdentity(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result,
				[]string{"ENTITY", "ACCOUNT", "SOURCE", "EXTERNAL ID", "NUMBER", "NAME", "ACTIVE", "SOURCE KIND", "SOURCE PATH", "LOCATOR", "ID"},
				[][]string{accountIdentityRow(result)})
		},
	}
	add.Flags().StringVar(&entity, "entity", "", "entity code for the external realm")
	add.Flags().StringVar(&account, "account", "", "master account code")
	add.Flags().StringVar(&sourceSystem, "source-system", "", "external source system, such as QBO")
	add.Flags().StringVar(&externalID, "external-id", "", "realm-local external account id")
	add.Flags().StringVar(&accountNumber, "account-number", "", "account number observed in the source")
	add.Flags().StringVar(&name, "name", "", "account name observed in the source")
	add.Flags().BoolVar(&active, "active", true, "whether the account was active in the source")
	add.Flags().StringVar(&sourceKind, "source-kind", "", "evidence source kind")
	add.Flags().StringVar(&sourcePath, "source-path", "", "path or stable name of the evidence source")
	add.Flags().StringVar(&sourceSHA256, "source-sha256", "", "SHA-256 of the evidence source")
	add.Flags().StringVar(&locator, "locator", "", "location of the account inside the evidence source")
	add.Flags().StringVar(&payloadSHA256, "payload-sha256", "", "optional SHA-256 of the source account payload")

	var listEntity, listAccount, listSourceSystem string
	list := &cobra.Command{
		Use: "list", Short: "List external account identities", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			identities, err := ledger.NewService(store, opts.actor).ListAccountIdentities(cmd.Context(), ledger.AccountIdentityFilter{
				Entity: listEntity, Account: listAccount, SourceSystem: listSourceSystem,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(identities))
			for _, identity := range identities {
				rows = append(rows, accountIdentityRow(identity))
			}
			return writeResult(cmd, opts.format, identities,
				[]string{"ENTITY", "ACCOUNT", "SOURCE", "EXTERNAL ID", "NUMBER", "NAME", "ACTIVE", "SOURCE KIND", "SOURCE PATH", "LOCATOR", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&listEntity, "entity", "", "optional entity code")
	list.Flags().StringVar(&listAccount, "account", "", "optional master account code")
	list.Flags().StringVar(&listSourceSystem, "source-system", "", "optional external source system")
	command.AddCommand(add, list)
	return command
}
