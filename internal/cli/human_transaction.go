package cli

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

type transactionFlags struct {
	date        string
	memo        string
	reference   string
	key         string
	draft       bool
	paymentFrom string
	depositTo   string
}

type humanTransactionLine struct {
	Line        int    `json:"line"`
	Account     string `json:"account"`
	AccountName string `json:"account_name"`
	Description string `json:"description,omitempty"`
	DebitCents  int64  `json:"debit_cents"`
	CreditCents int64  `json:"credit_cents"`
}

type humanTransaction struct {
	Company          string                 `json:"company"`
	Book             string                 `json:"book"`
	Number           int64                  `json:"number,omitempty"`
	Kind             string                 `json:"kind"`
	Date             string                 `json:"date"`
	Period           string                 `json:"period"`
	Status           string                 `json:"status"`
	Description      string                 `json:"description"`
	Reference        string                 `json:"reference,omitempty"`
	TotalDebitCents  int64                  `json:"total_debit_cents"`
	TotalCreditCents int64                  `json:"total_credit_cents"`
	Lines            []humanTransactionLine `json:"lines,omitempty"`
	DryRun           bool                   `json:"dry_run"`
}

type humanCorrection struct {
	Company        string           `json:"company"`
	OriginalNumber int64            `json:"original_number"`
	Reason         string           `json:"reason"`
	Reversal       humanTransaction `json:"reversal"`
	Replacement    humanTransaction `json:"replacement"`
	DryRun         bool             `json:"dry_run"`
}

func newSpendCommand(opts *options) *cobra.Command {
	flags := transactionFlags{date: "today"}
	command := &cobra.Command{
		Use:   "spend AMOUNT ACCOUNT [DESCRIPTION...]",
		Short: "Record money spent, posting immediately by default",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, err := positiveMoney(args[0])
			if err != nil {
				return err
			}
			context, err := loadPostingContext(cmd, opts, flags.date)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(context.store)
			category, err := resolveHumanAccount(context.accounts, args[1])
			if err != nil {
				return err
			}
			payment, err := resolveDefaultTransactionAccount(context.accounts, flags.paymentFrom, context.resolved.Company.Defaults.PaymentAccount, []string{"BANK", "CREDIT_CARD"}, "--from", "defaults.payment-account")
			if err != nil {
				return err
			}
			if category.Code == payment.Code {
				return apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNTS_SAME", "the spending and payment accounts must be different")
			}
			description, err := transactionDescription(flags.memo, args[2:], "Spend: "+category.Name)
			if err != nil {
				return err
			}
			input := ledger.CreateJournalInput{
				Book: context.resolved.Company.BookCode, PostingDate: context.date, Period: context.period.Code,
				Description: description, Reference: flags.reference,
				Lines: []ledger.JournalLineInput{
					{Account: category.Code, Description: description, DebitCents: amount},
					{Account: payment.Code, Description: description, CreditCents: amount},
				},
			}
			setManualSource(&input, flags.key)
			return commitHumanJournal(cmd, opts, context, input, flags.draft)
		},
	}
	addHumanTransactionFlags(command, &flags)
	command.Flags().StringVar(&flags.paymentFrom, "from", "", "payment account (uses the configured or only bank/card account when omitted)")
	return command
}

func newReceiveCommand(opts *options) *cobra.Command {
	flags := transactionFlags{date: "today"}
	command := &cobra.Command{
		Use:   "receive AMOUNT ACCOUNT [DESCRIPTION...]",
		Short: "Record money received, posting immediately by default",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, err := positiveMoney(args[0])
			if err != nil {
				return err
			}
			context, err := loadPostingContext(cmd, opts, flags.date)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(context.store)
			source, err := resolveHumanAccount(context.accounts, args[1])
			if err != nil {
				return err
			}
			deposit, err := resolveDefaultTransactionAccount(context.accounts, flags.depositTo, context.resolved.Company.Defaults.DepositAccount, []string{"BANK"}, "--to", "defaults.deposit-account")
			if err != nil {
				return err
			}
			if source.Code == deposit.Code {
				return apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNTS_SAME", "the source and deposit accounts must be different")
			}
			description, err := transactionDescription(flags.memo, args[2:], "Receive: "+source.Name)
			if err != nil {
				return err
			}
			input := ledger.CreateJournalInput{
				Book: context.resolved.Company.BookCode, PostingDate: context.date, Period: context.period.Code,
				Description: description, Reference: flags.reference,
				Lines: []ledger.JournalLineInput{
					{Account: deposit.Code, Description: description, DebitCents: amount},
					{Account: source.Code, Description: description, CreditCents: amount},
				},
			}
			setManualSource(&input, flags.key)
			return commitHumanJournal(cmd, opts, context, input, flags.draft)
		},
	}
	addHumanTransactionFlags(command, &flags)
	command.Flags().StringVar(&flags.depositTo, "to", "", "deposit account (uses the configured or only bank account when omitted)")
	return command
}

func newTransferCommand(opts *options) *cobra.Command {
	flags := transactionFlags{date: "today"}
	command := &cobra.Command{
		Use:   "transfer AMOUNT FROM TO [DESCRIPTION...]",
		Short: "Move value between two balance-sheet accounts",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, err := positiveMoney(args[0])
			if err != nil {
				return err
			}
			context, err := loadPostingContext(cmd, opts, flags.date)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(context.store)
			from, err := resolveHumanAccount(context.accounts, args[1])
			if err != nil {
				return err
			}
			to, err := resolveHumanAccount(context.accounts, args[2])
			if err != nil {
				return err
			}
			if from.Code == to.Code {
				return apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNTS_SAME", "transfer accounts must be different")
			}
			if !isBalanceSheetAccount(from) || !isBalanceSheetAccount(to) {
				return apperr.New(apperr.Invalid, "TRANSFER_ACCOUNT_INVALID", "transfer requires two balance-sheet accounts (asset, liability, or equity)")
			}
			description, err := transactionDescription(flags.memo, args[3:], "Transfer: "+from.Name+" to "+to.Name)
			if err != nil {
				return err
			}
			input := ledger.CreateJournalInput{
				Book: context.resolved.Company.BookCode, PostingDate: context.date, Period: context.period.Code,
				Description: description, Reference: flags.reference,
				Lines: []ledger.JournalLineInput{
					{Account: to.Code, Description: description, DebitCents: amount},
					{Account: from.Code, Description: description, CreditCents: amount},
				},
			}
			setManualSource(&input, flags.key)
			return commitHumanJournal(cmd, opts, context, input, flags.draft)
		},
	}
	addHumanTransactionFlags(command, &flags)
	return command
}

func newHumanJournalAddCommand(opts *options) *cobra.Command {
	var inputPath string
	var draft bool
	command := &cobra.Command{
		Use:   "add",
		Short: "Record a multi-line journal from JSON, posting by default",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(inputPath) == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required; use --input - to read JSON from stdin")
			}
			var file journalFile
			if err := readJSONInput(inputPath, &file); err != nil {
				return err
			}
			context, input, err := prepareHumanJournal(cmd, opts, file)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(context.store)
			return commitHumanJournal(cmd, opts, context, input, draft)
		},
	}
	command.Flags().StringVarP(&inputPath, "input", "i", "", "JSON file or - for explicit stdin")
	command.Flags().BoolVar(&draft, "draft", false, "save as a draft instead of posting")
	return command
}

func prepareHumanJournal(cmd *cobra.Command, opts *options, file journalFile) (*postingContext, ledger.CreateJournalInput, error) {
	dateText := file.PostingDate
	if strings.TrimSpace(dateText) == "" {
		dateText = "today"
	}
	context, err := loadPostingContext(cmd, opts, dateText)
	if err != nil {
		return nil, ledger.CreateJournalInput{}, err
	}
	if strings.TrimSpace(file.Book) == "" {
		file.Book = context.resolved.Company.BookCode
	}
	if !strings.EqualFold(file.Book, context.resolved.Company.BookCode) {
		_ = context.store.Close()
		return nil, ledger.CreateJournalInput{}, apperr.New(apperr.Invalid, "BOOK_INVALID", "journal add uses the selected company's book; use the lower-level journal create command for another book")
	}
	file.Book = context.resolved.Company.BookCode
	file.PostingDate = context.date
	if strings.TrimSpace(file.Period) == "" {
		file.Period = context.period.Code
	} else if !strings.EqualFold(file.Period, context.period.Code) {
		_ = context.store.Close()
		return nil, ledger.CreateJournalInput{}, apperr.New(apperr.Invalid, "PERIOD_DATE_MISMATCH", fmt.Sprintf("%s belongs to period %s", context.date, context.period.Code))
	}
	for index := range file.Lines {
		account, err := resolveHumanAccount(context.accounts, file.Lines[index].Account)
		if err != nil {
			_ = context.store.Close()
			return nil, ledger.CreateJournalInput{}, apperr.Wrap(apperr.Invalid, "JOURNAL_ACCOUNT_INVALID", fmt.Sprintf("resolve line %d account", index+1), err)
		}
		file.Lines[index].Account = account.Code
	}
	input, err := file.ledgerInput()
	if err != nil {
		_ = context.store.Close()
		return nil, ledger.CreateJournalInput{}, err
	}
	if len(input.Lines) < 2 {
		_ = context.store.Close()
		return nil, ledger.CreateJournalInput{}, apperr.New(apperr.Invalid, "JOURNAL_LINES_REQUIRED", "a journal requires at least two lines")
	}
	var debits, credits int64
	for _, line := range input.Lines {
		debits += line.DebitCents
		credits += line.CreditCents
	}
	if debits == 0 || debits != credits {
		_ = context.store.Close()
		return nil, ledger.CreateJournalInput{}, apperr.New(apperr.Validation, "JOURNAL_UNBALANCED", fmt.Sprintf("debits %s must equal credits %s", money.Format(debits), money.Format(credits)))
	}
	return context, input, nil
}

func addHumanTransactionFlags(command *cobra.Command, flags *transactionFlags) {
	command.Flags().StringVar(&flags.date, "date", flags.date, "posting date: YYYY-MM-DD, today, or yesterday")
	command.Flags().StringVarP(&flags.memo, "memo", "m", "", "transaction description (or use trailing positional words)")
	command.Flags().StringVar(&flags.reference, "reference", "", "optional check, invoice, or other reference")
	command.Flags().StringVar(&flags.key, "key", "", "optional idempotency key for safe retries")
	command.Flags().BoolVar(&flags.draft, "draft", false, "save as a draft instead of posting")
}

func positiveMoney(value string) (int64, error) {
	amount, err := money.Parse(value)
	if err != nil {
		return 0, apperr.Wrap(apperr.Invalid, "AMOUNT_INVALID", "amount must be an unformatted decimal such as 42.50", err)
	}
	if amount <= 0 {
		return 0, apperr.New(apperr.Invalid, "AMOUNT_INVALID", "amount must be greater than zero")
	}
	return amount, nil
}

func isBalanceSheetAccount(account ledger.Account) bool {
	switch account.Type {
	case "ASSET", "LIABILITY", "EQUITY":
		return true
	default:
		return false
	}
}

func transactionDescription(flag string, trailing []string, fallback string) (string, error) {
	fromArgs := strings.TrimSpace(strings.Join(trailing, " "))
	fromFlag := strings.TrimSpace(flag)
	if fromArgs != "" && fromFlag != "" {
		return "", apperr.New(apperr.Invalid, "DESCRIPTION_DUPLICATE", "supply a trailing description or --memo, not both")
	}
	if fromFlag != "" {
		return fromFlag, nil
	}
	if fromArgs != "" {
		return fromArgs, nil
	}
	return fallback, nil
}

func setManualSource(input *ledger.CreateJournalInput, key string) {
	if strings.TrimSpace(key) != "" {
		input.SourceSystem = "MANUAL"
		input.SourceKey = strings.TrimSpace(key)
	}
}

type postingContext struct {
	store    *storesqlite.Store
	resolved resolvedCompanyAlias
	accounts []ledger.Account
	period   ledger.Period
	date     string
}

// resolvedCompanyAlias keeps posting helpers compact without leaking configuration
// internals into their public output types.
type resolvedCompanyAlias struct {
	Key     string
	Company struct {
		BookCode string
		Defaults struct {
			PaymentAccount string
			DepositAccount string
		}
	}
}

func loadPostingContext(cmd *cobra.Command, opts *options, dateText string) (*postingContext, error) {
	resolved, err := opts.resolveCompany()
	if err != nil {
		return nil, err
	}
	date, err := parseHumanDate(dateText)
	if err != nil {
		return nil, err
	}
	store, err := openRead(cmd, opts)
	if err != nil {
		return nil, err
	}
	service := ledger.NewService(store, opts.actor)
	accounts, err := service.ListAccounts(cmd.Context(), resolved.Company.BookCode)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	periods, err := service.ListPeriods(cmd.Context(), resolved.Company.BookCode)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	var selected *ledger.Period
	for index := range periods {
		if periods[index].StartDate <= date && date <= periods[index].EndDate {
			selected = &periods[index]
			break
		}
	}
	if selected == nil {
		_ = store.Close()
		return nil, apperr.New(apperr.Validation, "PERIOD_NOT_CONFIGURED", fmt.Sprintf("no fiscal period contains %s", date))
	}
	if selected.BookStatus != "OPEN" {
		_ = store.Close()
		return nil, apperr.New(apperr.Conflict, "PERIOD_CLOSED", fmt.Sprintf("period %s is %s", selected.Code, selected.BookStatus))
	}
	alias := resolvedCompanyAlias{Key: resolved.Key}
	alias.Company.BookCode = resolved.Company.BookCode
	alias.Company.Defaults.PaymentAccount = resolved.Company.Defaults.PaymentAccount
	alias.Company.Defaults.DepositAccount = resolved.Company.Defaults.DepositAccount
	return &postingContext{store: store, resolved: alias, accounts: accounts, period: *selected, date: date}, nil
}

func parseHumanDate(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "today":
		return time.Now().Format("2006-01-02"), nil
	case "yesterday":
		return time.Now().AddDate(0, 0, -1).Format("2006-01-02"), nil
	default:
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
		if err != nil {
			return "", apperr.New(apperr.Invalid, "DATE_INVALID", "date must be YYYY-MM-DD, today, or yesterday")
		}
		return parsed.Format("2006-01-02"), nil
	}
}

func resolveDefaultTransactionAccount(accounts []ledger.Account, explicit, configured string, subtypes []string, flag, setting string) (ledger.Account, error) {
	allowed := make(map[string]bool, len(subtypes))
	for _, subtype := range subtypes {
		allowed[subtype] = true
	}
	resolveEligible := func(selector string) (ledger.Account, error) {
		account, err := resolveHumanAccount(accounts, selector)
		if err != nil {
			return ledger.Account{}, err
		}
		if !allowed[normalizedSubtype(account.Subtype)] {
			return ledger.Account{}, apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNT_KIND_INVALID", fmt.Sprintf("%s requires an eligible %s account", flag, strings.ToLower(strings.Join(subtypes, " or "))))
		}
		return account, nil
	}
	if strings.TrimSpace(explicit) != "" {
		return resolveEligible(explicit)
	}
	if strings.TrimSpace(configured) != "" {
		return resolveEligible(configured)
	}
	var candidates []ledger.Account
	for _, account := range accounts {
		if allowed[normalizedSubtype(account.Subtype)] && account.PostingEnabled != nil && *account.PostingEnabled {
			candidates = append(candidates, account)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return ledger.Account{}, apperr.New(apperr.Validation, "DEFAULT_ACCOUNT_MISSING", fmt.Sprintf("no eligible account exists; pass %s or add a bank/card account", flag))
	}
	return ledger.Account{}, apperr.New(apperr.Invalid, "DEFAULT_ACCOUNT_AMBIGUOUS", fmt.Sprintf("more than one eligible account exists; pass %s or set %s", flag, setting))
}

func commitHumanJournal(cmd *cobra.Command, opts *options, context *postingContext, input ledger.CreateJournalInput, draft bool) error {
	if err := validateHumanJournal(context, input); err != nil {
		return err
	}
	preview := humanTransactionFromInput(context.resolved.Key, input, draft, opts.dryRun)
	if opts.dryRun {
		return writeHumanTransaction(cmd, opts, preview)
	}
	_ = context.store.Close()
	context.store = nil
	store, err := openWrite(cmd, opts)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service := ledger.NewService(store, opts.actor)
	var journal ledger.Journal
	if draft {
		journal, err = service.CreateJournal(cmd.Context(), input)
	} else {
		journal, err = service.CreateAndPostJournal(cmd.Context(), input)
	}
	if err != nil {
		return err
	}
	if journal.Status == "ABANDONED" {
		return apperr.New(apperr.Conflict, "TRANSACTION_KEY_ABANDONED", "this idempotency key belongs to an abandoned transaction; use a new --key for revised activity")
	}
	return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(context.resolved.Key, journal))
}

func validateHumanJournal(context *postingContext, input ledger.CreateJournalInput) error {
	if input.Book != context.resolved.Company.BookCode || input.Period != context.period.Code || input.PostingDate != context.date {
		return apperr.New(apperr.Invalid, "JOURNAL_CONTEXT_INVALID", "journal book, period, and date must match the selected company posting context")
	}
	if strings.TrimSpace(input.Description) == "" || len(input.Lines) < 2 {
		return apperr.New(apperr.Invalid, "JOURNAL_INVALID", "description and at least two lines are required")
	}
	accounts := make(map[string]ledger.Account, len(context.accounts))
	for _, account := range context.accounts {
		accounts[account.Code] = account
	}
	var debits, credits int64
	for index, line := range input.Lines {
		account, ok := accounts[strings.ToUpper(strings.TrimSpace(line.Account))]
		if !ok {
			return apperr.New(apperr.NotFound, "ACCOUNT_NOT_FOUND", fmt.Sprintf("journal line %d account %q was not found", index+1, line.Account))
		}
		if account.PostingEnabled == nil || !*account.PostingEnabled || context.date < account.ActiveFrom || (account.ActiveTo != "" && context.date > account.ActiveTo) {
			return apperr.New(apperr.Validation, "ACCOUNT_NOT_ACTIVE", fmt.Sprintf("account %s is not enabled for posting on %s", account.Code, context.date))
		}
		if line.DebitCents < 0 || line.CreditCents < 0 || (line.DebitCents > 0) == (line.CreditCents > 0) {
			return apperr.New(apperr.Invalid, "JOURNAL_LINE_INVALID", fmt.Sprintf("line %d must contain exactly one positive debit or credit", index+1))
		}
		debits += line.DebitCents
		credits += line.CreditCents
	}
	if debits == 0 || debits != credits {
		return apperr.New(apperr.Validation, "JOURNAL_UNBALANCED", fmt.Sprintf("debits %s must equal credits %s", money.Format(debits), money.Format(credits)))
	}
	return nil
}

func humanTransactionFromInput(company string, input ledger.CreateJournalInput, draft, dryRun bool) humanTransaction {
	status := "POSTED"
	if draft {
		status = "DRAFT"
	}
	if dryRun {
		status = "PREVIEW"
	}
	result := humanTransaction{Company: company, Book: input.Book, Kind: input.Kind, Date: input.PostingDate, Period: input.Period, Status: status, Description: input.Description, Reference: input.Reference, DryRun: dryRun}
	if result.Kind == "" {
		result.Kind = "STANDARD"
	}
	for index, line := range input.Lines {
		result.Lines = append(result.Lines, humanTransactionLine{Line: index + 1, Account: line.Account, Description: line.Description, DebitCents: line.DebitCents, CreditCents: line.CreditCents})
		result.TotalDebitCents += line.DebitCents
		result.TotalCreditCents += line.CreditCents
	}
	return result
}

func humanTransactionFromJournal(company string, journal ledger.Journal) humanTransaction {
	result := humanTransaction{Company: company, Book: journal.BookCode, Number: journal.EntryNumber, Kind: journal.Kind, Date: journal.PostingDate, Period: journal.PeriodCode, Status: journal.Status, Description: journal.Description, Reference: journal.Reference, TotalDebitCents: journal.TotalDebitCents, TotalCreditCents: journal.TotalCreditCents}
	for _, line := range journal.Lines {
		result.Lines = append(result.Lines, humanTransactionLine{Line: line.LineNumber, Account: line.AccountCode, AccountName: line.AccountName, Description: line.Description, DebitCents: line.DebitCents, CreditCents: line.CreditCents})
	}
	return result
}

func writeHumanTransaction(cmd *cobra.Command, opts *options, value humanTransaction) error {
	rows := make([][]string, 0, len(value.Lines))
	for _, line := range value.Lines {
		rows = append(rows, []string{formatTransactionNumber(value.Number), value.Date, value.Status, strconv.Itoa(line.Line), line.Account, line.AccountName, line.Description, money.Format(line.DebitCents), money.Format(line.CreditCents)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{formatTransactionNumber(value.Number), value.Date, value.Status, "", "", "", value.Description, money.Format(value.TotalDebitCents), money.Format(value.TotalCreditCents)})
	}
	return writeResult(cmd, opts.format, value, []string{"NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "ACCOUNT NAME", "DESCRIPTION", "DEBIT", "CREDIT"}, rows)
}

func formatTransactionNumber(number int64) string {
	if number == 0 {
		return "(preview)"
	}
	return strconv.FormatInt(number, 10)
}

func findJournalByNumber(cmd *cobra.Command, service *ledger.Service, book string, text string) (ledger.Journal, error) {
	text = strings.TrimPrefix(strings.TrimSpace(text), "#")
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || number < 1 {
		return ledger.Journal{}, apperr.New(apperr.Invalid, "TRANSACTION_NUMBER_INVALID", "transaction number must be a positive integer")
	}
	values, err := service.ListJournals(cmd.Context(), book, "", "", "")
	if err != nil {
		return ledger.Journal{}, err
	}
	for _, value := range values {
		if value.EntryNumber == number {
			return service.GetJournal(cmd.Context(), value.ID)
		}
	}
	return ledger.Journal{}, apperr.New(apperr.NotFound, "TRANSACTION_NOT_FOUND", fmt.Sprintf("transaction %d was not found", number))
}

func newTxCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "tx", Short: "List, inspect, post, or abandon transactions by number"}
	var from, to, status string
	list := &cobra.Command{
		Use: "list", Short: "List transactions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if from != "" {
				if from, err = parseHumanDate(from); err != nil {
					return err
				}
			}
			if to != "" {
				if to, err = parseHumanDate(to); err != nil {
					return err
				}
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journals, err := ledger.NewService(store, opts.actor).ListJournals(cmd.Context(), resolved.Company.BookCode, from, to, status)
			if err != nil {
				return err
			}
			values := make([]humanTransaction, 0, len(journals))
			rows := make([][]string, 0, len(journals))
			for _, journal := range journals {
				value := humanTransactionFromJournal(resolved.Key, journal)
				value.Lines = nil
				values = append(values, value)
				rows = append(rows, []string{strconv.FormatInt(journal.EntryNumber, 10), journal.PostingDate, journal.Status, journal.Description, journal.Reference, money.Format(journal.TotalDebitCents)})
			}
			return writeResult(cmd, opts.format, values, []string{"NUMBER", "DATE", "STATUS", "DESCRIPTION", "REFERENCE", "TOTAL"}, rows)
		},
	}
	list.Flags().StringVar(&from, "from", "", "earliest date")
	list.Flags().StringVar(&to, "to", "", "latest date")
	list.Flags().StringVar(&status, "status", "", "DRAFT, POSTED, or ABANDONED")
	show := &cobra.Command{
		Use: "show NUMBER", Short: "Show a transaction and its lines", Args: cobra.ExactArgs(1),
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
			journal, err := findJournalByNumber(cmd, ledger.NewService(store, opts.actor), resolved.Company.BookCode, args[0])
			if err != nil {
				return err
			}
			return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(resolved.Key, journal))
		},
	}
	post := newTxStatusCommand(opts, "post NUMBER", "Post a draft transaction", "post")
	abandon := newTxStatusCommand(opts, "abandon NUMBER", "Abandon a draft transaction", "abandon")
	command.AddCommand(list, show, post, abandon)
	return command
}

func newTxStatusCommand(opts *options, use, short, action string) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			journal, err := findJournalByNumber(cmd, ledger.NewService(store, opts.actor), resolved.Company.BookCode, args[0])
			_ = store.Close()
			if err != nil {
				return err
			}
			switch action {
			case "post":
				if journal.Status == "POSTED" {
					return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(resolved.Key, journal))
				}
				if journal.Status != "DRAFT" {
					return apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "only a draft transaction can be posted")
				}
			case "abandon":
				if journal.Status == "ABANDONED" {
					return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(resolved.Key, journal))
				}
				if journal.Status != "DRAFT" {
					return apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "only a draft transaction can be abandoned")
				}
			}
			if opts.dryRun {
				if action == "post" {
					store, err := openRead(cmd, opts)
					if err != nil {
						return err
					}
					defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
					validation, err := ledger.NewService(store, opts.actor).ValidateJournal(cmd.Context(), journal.ID)
					if err != nil {
						return err
					}
					if !validation.Valid {
						return apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(validation.Errors, "; "))
					}
				}
				value := humanTransactionFromJournal(resolved.Key, journal)
				value.Status = strings.ToUpper(action) + " (PREVIEW)"
				value.DryRun = true
				return writeHumanTransaction(cmd, opts, value)
			}
			store, err = openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service := ledger.NewService(store, opts.actor)
			if action == "post" {
				journal, err = service.PostJournal(cmd.Context(), journal.ID)
			} else {
				err = service.AbandonJournal(cmd.Context(), journal.ID)
				if err == nil {
					journal.Status = "ABANDONED"
				}
			}
			if err != nil {
				return err
			}
			return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(resolved.Key, journal))
		},
	}
}

func newCorrectCommand(opts *options) *cobra.Command {
	var inputPath, reason string
	var draft bool
	command := &cobra.Command{
		Use:   "correct NUMBER",
		Short: "Reverse a posted transaction and record its corrected replacement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(inputPath) == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required; use --input - to read JSON from stdin")
			}
			if strings.TrimSpace(reason) == "" {
				return apperr.New(apperr.Invalid, "CORRECTION_REASON_REQUIRED", "--reason is required for the audit trail")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			readStore, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			original, err := findJournalByNumber(cmd, ledger.NewService(readStore, opts.actor), resolved.Company.BookCode, args[0])
			closeErr := readStore.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if original.Status != "POSTED" {
				return apperr.New(apperr.Conflict, "CORRECTION_REQUIRES_POSTED", "only a posted transaction can be corrected; edit or abandon a draft instead")
			}
			var file journalFile
			if err := readJSONInput(inputPath, &file); err != nil {
				return err
			}
			context, replacementInput, err := prepareHumanJournal(cmd, opts, file)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(context.store)
			if replacementInput.PostingDate < original.PostingDate {
				return apperr.New(apperr.Validation, "CORRECTION_DATE_INVALID", "the corrected transaction date cannot precede the original transaction")
			}
			correctionPrefix := "transaction-" + strconv.FormatInt(original.EntryNumber, 10) + "-"
			seed := replacementInput
			seed.SourceSystem, seed.SourceKey = "", ""
			seedDigest, err := digestJSON(map[string]any{"journal": seed, "reason": strings.TrimSpace(reason)})
			if err != nil {
				return err
			}
			replacementInput.SourceSystem = "MANUAL_CORRECTION"
			replacementInput.SourceKey = correctionPrefix + seedDigest[:16]
			if replacementInput.Reference == "" {
				replacementInput.Reference = "CORRECTS-" + strconv.FormatInt(original.EntryNumber, 10)
			}
			reversalInput := ledger.CreateJournalInput{
				Book: original.BookCode, Kind: "STANDARD", PostingDate: replacementInput.PostingDate,
				Period: replacementInput.Period, Description: "Correction of transaction " + strconv.FormatInt(original.EntryNumber, 10) + ": " + strings.TrimSpace(reason),
				Reference: "REV-" + strconv.FormatInt(original.EntryNumber, 10), ReversalOfID: original.ID,
			}
			for _, line := range original.Lines {
				reversalInput.Lines = append(reversalInput.Lines, ledger.JournalLineInput{
					Account: line.AccountCode, Description: line.Description, DebitCents: line.CreditCents,
					CreditCents: line.DebitCents, CounterpartyEntity: line.CounterpartyEntity, IntercompanyKey: line.IntercompanyKey,
				})
			}
			if err := validateHumanJournal(context, replacementInput); err != nil {
				return err
			}
			if err := validateHumanJournal(context, reversalInput); err != nil {
				return err
			}
			if opts.dryRun {
				result := humanCorrection{
					Company: resolved.Key, OriginalNumber: original.EntryNumber, Reason: strings.TrimSpace(reason), DryRun: true,
					Reversal:    humanTransactionFromInput(resolved.Key, reversalInput, draft, true),
					Replacement: humanTransactionFromInput(resolved.Key, replacementInput, draft, true),
				}
				return writeCorrection(cmd, opts, result)
			}
			_ = context.store.Close()
			context.store = nil
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service := ledger.NewService(store, opts.actor)
			var priorCorrectionKey string
			err = store.DB().QueryRowContext(cmd.Context(), `SELECT source_key FROM journal_entries
				WHERE book_id = ? AND source_system = 'MANUAL_CORRECTION'
				  AND source_key LIKE ? AND status <> 'ABANDONED' LIMIT 1`, original.BookID, correctionPrefix+"%").Scan(&priorCorrectionKey)
			if err == nil && priorCorrectionKey != replacementInput.SourceKey {
				return apperr.New(apperr.Conflict, "CORRECTION_ALREADY_EXISTS", fmt.Sprintf("transaction %d already has a different active correction", original.EntryNumber))
			}
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			// Create both drafts before posting either side. This keeps validation
			// failures recoverable and preserves the original posted transaction.
			replacement, err := service.CreateJournal(cmd.Context(), replacementInput)
			if err != nil {
				return err
			}
			if replacement.Status == "ABANDONED" {
				return apperr.New(apperr.Conflict, "CORRECTION_ABANDONED", "the identical correction was previously abandoned; use a materially revised replacement")
			}
			var reversal ledger.Journal
			var reversalID string
			err = store.DB().QueryRowContext(cmd.Context(), `SELECT id FROM journal_entries
				WHERE reversal_of_id = ? AND status <> 'ABANDONED'`, original.ID).Scan(&reversalID)
			switch err {
			case nil:
				reversal, err = service.GetJournal(cmd.Context(), reversalID)
			case sql.ErrNoRows:
				reversal, err = service.ReverseJournal(cmd.Context(), original.ID, reversalInput.PostingDate, reversalInput.Period, reversalInput.Description)
			default:
				return err
			}
			if err != nil {
				return err
			}
			for _, candidate := range []ledger.Journal{reversal, replacement} {
				if candidate.Status != "DRAFT" {
					continue
				}
				validation, err := service.ValidateJournal(cmd.Context(), candidate.ID)
				if err != nil {
					return err
				}
				if !validation.Valid {
					return apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(validation.Errors, "; "))
				}
			}
			if !draft {
				if reversal.Status == "DRAFT" {
					reversal, err = service.PostJournal(cmd.Context(), reversal.ID)
					if err != nil {
						return err
					}
				}
				if replacement.Status == "DRAFT" {
					replacement, err = service.PostJournal(cmd.Context(), replacement.ID)
					if err != nil {
						return apperr.Wrap(apperr.Unavailable, "CORRECTION_PARTIAL", fmt.Sprintf("reversal %d posted but replacement %d could not be posted; inspect both transactions", reversal.EntryNumber, replacement.EntryNumber), err)
					}
				}
			}
			result := humanCorrection{
				Company: resolved.Key, OriginalNumber: original.EntryNumber, Reason: strings.TrimSpace(reason),
				Reversal: humanTransactionFromJournal(resolved.Key, reversal), Replacement: humanTransactionFromJournal(resolved.Key, replacement),
			}
			return writeCorrection(cmd, opts, result)
		},
	}
	command.Flags().StringVarP(&inputPath, "input", "i", "", "corrected journal JSON file or - for explicit stdin")
	command.Flags().StringVar(&reason, "reason", "", "required audit explanation")
	command.Flags().BoolVar(&draft, "draft", false, "create the reversal and replacement as validated drafts")
	return command
}

func writeCorrection(cmd *cobra.Command, opts *options, result humanCorrection) error {
	rows := [][]string{
		{"original", strconv.FormatInt(result.OriginalNumber, 10), "POSTED", result.Reason, ""},
		{"reversal", formatTransactionNumber(result.Reversal.Number), result.Reversal.Status, result.Reversal.Description, money.Format(result.Reversal.TotalDebitCents)},
		{"replacement", formatTransactionNumber(result.Replacement.Number), result.Replacement.Status, result.Replacement.Description, money.Format(result.Replacement.TotalDebitCents)},
	}
	return writeResult(cmd, opts.format, result, []string{"ROLE", "NUMBER", "STATUS", "DESCRIPTION", "TOTAL"}, rows)
}

func newReverseCommand(opts *options) *cobra.Command {
	var date, memo string
	var draft bool
	command := &cobra.Command{
		Use: "reverse NUMBER", Short: "Reverse a posted transaction with an immutable linked entry", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return reverseHumanTransaction(cmd, opts, args[0], date, memo, draft, false)
		},
	}
	command.Flags().StringVar(&date, "date", "today", "reversal date")
	command.Flags().StringVarP(&memo, "memo", "m", "", "reversal description")
	command.Flags().BoolVar(&draft, "draft", false, "create a draft reversal instead of posting")
	return command
}

func newUndoCommand(opts *options) *cobra.Command {
	var date, reason string
	command := &cobra.Command{
		Use: "undo NUMBER", Short: "Abandon a draft or reverse a posted transaction", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			journal, err := findJournalByNumber(cmd, ledger.NewService(store, opts.actor), resolved.Company.BookCode, args[0])
			_ = store.Close()
			if err != nil {
				return err
			}
			if journal.Status == "DRAFT" {
				return newTxStatusCommand(opts, "undo NUMBER", "", "abandon").RunE(cmd, args)
			}
			return reverseHumanTransaction(cmd, opts, args[0], date, reason, false, true)
		},
	}
	command.Flags().StringVar(&date, "date", "today", "reversal date for a posted transaction")
	command.Flags().StringVar(&reason, "reason", "", "why the transaction is being undone")
	return command
}

func reverseHumanTransaction(cmd *cobra.Command, opts *options, number, dateText, memo string, draft, undo bool) error {
	resolved, err := opts.resolveCompany()
	if err != nil {
		return err
	}
	date, err := parseHumanDate(dateText)
	if err != nil {
		return err
	}
	store, err := openRead(cmd, opts)
	if err != nil {
		return err
	}
	service := ledger.NewService(store, opts.actor)
	original, err := findJournalByNumber(cmd, service, resolved.Company.BookCode, number)
	if err != nil {
		_ = store.Close()
		return err
	}
	periods, err := service.ListPeriods(cmd.Context(), resolved.Company.BookCode)
	_ = store.Close()
	if err != nil {
		return err
	}
	period := ""
	for _, candidate := range periods {
		if candidate.StartDate <= date && date <= candidate.EndDate && candidate.BookStatus == "OPEN" {
			period = candidate.Code
			break
		}
	}
	if period == "" {
		return apperr.New(apperr.Validation, "OPEN_PERIOD_NOT_FOUND", fmt.Sprintf("no open fiscal period contains %s", date))
	}
	if strings.TrimSpace(memo) == "" && undo {
		memo = "Undo transaction " + strconv.FormatInt(original.EntryNumber, 10)
	}
	if opts.dryRun {
		input := ledger.CreateJournalInput{Book: original.BookCode, Kind: "STANDARD", PostingDate: date, Period: period, Description: memo}
		if input.Description == "" {
			input.Description = "Reversal of " + original.BookCode + " journal " + strconv.FormatInt(original.EntryNumber, 10)
		}
		for _, line := range original.Lines {
			input.Lines = append(input.Lines, ledger.JournalLineInput{Account: line.AccountCode, Description: line.Description, DebitCents: line.CreditCents, CreditCents: line.DebitCents})
		}
		return writeHumanTransaction(cmd, opts, humanTransactionFromInput(resolved.Key, input, draft, true))
	}
	store, err = openWrite(cmd, opts)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service = ledger.NewService(store, opts.actor)
	expectedMemo := strings.TrimSpace(memo)
	if expectedMemo == "" {
		expectedMemo = "Reversal of " + original.BookCode + " journal " + strconv.FormatInt(original.EntryNumber, 10)
	}
	var reversal ledger.Journal
	var existingID string
	err = store.DB().QueryRowContext(cmd.Context(), `SELECT id FROM journal_entries
		WHERE reversal_of_id = ? AND status <> 'ABANDONED'`, original.ID).Scan(&existingID)
	switch err {
	case nil:
		reversal, err = service.GetJournal(cmd.Context(), existingID)
		if err == nil && (reversal.PostingDate != date || reversal.PeriodCode != period || reversal.Description != expectedMemo) {
			return apperr.New(apperr.Conflict, "REVERSAL_ALREADY_EXISTS", fmt.Sprintf("transaction %d already has a different active reversal", original.EntryNumber))
		}
	case sql.ErrNoRows:
		reversal, err = service.ReverseJournal(cmd.Context(), original.ID, date, period, memo)
	default:
		return err
	}
	if err != nil {
		return err
	}
	if !draft && reversal.Status == "DRAFT" {
		reversal, err = service.PostJournal(cmd.Context(), reversal.ID)
		if err != nil {
			return err
		}
	}
	return writeHumanTransaction(cmd, opts, humanTransactionFromJournal(resolved.Key, reversal))
}
