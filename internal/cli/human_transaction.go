package cli

import (
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/spf13/cobra"
	"strconv"
	"strings"
	"time"
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

type humanTransaction = application.Transaction
type humanCorrection = application.Correction

func addHumanTransactionFlags(command *cobra.Command, flags *transactionFlags) {
	command.Flags().StringVar(&flags.date, "date", flags.date, "posting date: YYYY-MM-DD, today, or yesterday")
	command.Flags().StringVarP(&flags.memo, "memo", "m", "", "transaction description (or use trailing positional words)")
	command.Flags().StringVar(&flags.reference, "reference", "", "optional check, invoice, or other reference")
	command.Flags().StringVar(&flags.key, "key", "", "optional idempotency key for safe retries")
	command.Flags().BoolVar(&flags.draft, "draft", false, "save as a draft instead of posting")
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

func writeHumanTransaction(cmd *cobra.Command, opts *options, value humanTransaction) error {
	rows := make([][]string, 0, len(value.Lines))
	for _, line := range value.Lines {
		rows = append(rows, []string{formatTransactionNumber(value.Number), value.Date, value.Status, strconv.Itoa(line.Line), line.Account, line.AccountName, line.Description, value.Currency.Format(line.DebitCents), value.Currency.Format(line.CreditCents)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{formatTransactionNumber(value.Number), value.Date, value.Status, "", "", "", value.Description, value.Currency.Format(value.TotalDebitCents), value.Currency.Format(value.TotalCreditCents)})
	}
	return writeResult(cmd, opts.format, value, []string{"NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "ACCOUNT NAME", "DESCRIPTION", "DEBIT", "CREDIT"}, rows)
}

func formatTransactionNumber(number int64) string {
	if number == 0 {
		return "(preview)"
	}
	return strconv.FormatInt(number, 10)
}

func writeCorrection(cmd *cobra.Command, opts *options, result humanCorrection) error {
	rows := [][]string{
		{"original", strconv.FormatInt(result.OriginalNumber, 10), "POSTED", result.Reason, ""},
		{"reversal", formatTransactionNumber(result.Reversal.Number), result.Reversal.Status, result.Reversal.Description, result.Reversal.Currency.Format(result.Reversal.TotalDebitCents)},
		{"replacement", formatTransactionNumber(result.Replacement.Number), result.Replacement.Status, result.Replacement.Description, result.Replacement.Currency.Format(result.Replacement.TotalDebitCents)},
	}
	return writeResult(cmd, opts.format, result, []string{"ROLE", "NUMBER", "STATUS", "DESCRIPTION", "TOTAL"}, rows)
}

func newSpendCommand(opts *options) *cobra.Command {
	return newRoutineTransactionCommand(opts, "spend")
}
func newReceiveCommand(opts *options) *cobra.Command {
	return newRoutineTransactionCommand(opts, "receive")
}
func newTransferCommand(opts *options) *cobra.Command {
	return newRoutineTransactionCommand(opts, "transfer")
}

func newRoutineTransactionCommand(opts *options, kind string) *cobra.Command {
	flags := transactionFlags{date: "today"}
	use := kind + " AMOUNT ACCOUNT [DESCRIPTION...]"
	minimum := 2
	if kind == "transfer" {
		use = "transfer AMOUNT FROM TO [DESCRIPTION...]"
		minimum = 3
	}
	command := &cobra.Command{Use: use, Short: "Record a transaction, posting immediately by default", Args: cobra.MinimumNArgs(minimum), RunE: func(cmd *cobra.Command, args []string) error {
		date, err := parseHumanDate(flags.date)
		if err != nil {
			return err
		}
		description, err := transactionDescription(flags.memo, args[minimum:], "")
		if err != nil {
			return err
		}
		request := application.TransactionRequest{Amount: args[0], Account: args[1], From: flags.paymentFrom, To: flags.depositTo, Date: date, Description: description, Reference: flags.reference, Key: flags.key, Draft: flags.draft, DryRun: opts.dryRun}
		if kind == "transfer" {
			request.Account = ""
			request.From = args[1]
			request.To = args[2]
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.RecordTransaction(cmd.Context(), kind, request)
		if err != nil {
			return err
		}
		return writeHumanTransaction(cmd, opts, result)
	}}
	addHumanTransactionFlags(command, &flags)
	if kind == "spend" {
		command.Flags().StringVar(&flags.paymentFrom, "from", "", "payment account (uses the configured or only bank/card account when omitted)")
	}
	if kind == "receive" {
		command.Flags().StringVar(&flags.depositTo, "to", "", "deposit account (uses the configured or only bank account when omitted)")
	}
	return command
}

func readHumanJournal(path string) (journalFile, error) {
	var input journalFile
	if strings.TrimSpace(path) == "" {
		return input, apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required; use --input - to read JSON from stdin")
	}
	if err := readJSONInput(path, &input); err != nil {
		return input, err
	}
	var err error
	input.PostingDate, err = parseHumanDate(input.PostingDate)
	return input, err
}
func newHumanJournalAddCommand(opts *options) *cobra.Command {
	var path string
	var draft bool
	command := &cobra.Command{Use: "add", Short: "Record a multi-line journal from JSON, posting by default", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		input, err := readHumanJournal(path)
		if err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.AddJournal(cmd.Context(), input, draft, opts.dryRun)
		if err != nil {
			return err
		}
		return writeHumanTransaction(cmd, opts, result)
	}}
	command.Flags().StringVarP(&path, "input", "i", "", "JSON file or - for explicit stdin")
	command.Flags().BoolVar(&draft, "draft", false, "save as a draft instead of posting")
	return command
}

func transactionNumber(text string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(text), "#"), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.New(apperr.Invalid, "TRANSACTION_NUMBER_INVALID", "transaction number must be a positive integer")
	}
	return n, nil
}
func newTxCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "tx", Short: "List, inspect, post, or abandon transactions by number"}
	var from, to, status string
	list := &cobra.Command{Use: "list", Short: "List transactions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if from != "" {
			from, err = parseHumanDate(from)
			if err != nil {
				return err
			}
		}
		if to != "" {
			to, err = parseHumanDate(to)
			if err != nil {
				return err
			}
		}
		app, err := openWorkflow(cmd, opts, false)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		values, err := app.ListTransactions(cmd.Context(), from, to, status)
		if err != nil {
			return err
		}
		rows := make([][]string, 0, len(values))
		for i := range values {
			v := &values[i]
			v.Lines = nil
			rows = append(rows, []string{strconv.FormatInt(v.Number, 10), v.Date, v.Status, v.Description, v.Reference, v.Currency.Format(v.TotalDebitCents)})
		}
		return writeResult(cmd, opts.format, values, []string{"NUMBER", "DATE", "STATUS", "DESCRIPTION", "REFERENCE", "TOTAL"}, rows)
	}}
	list.Flags().StringVar(&from, "from", "", "earliest date")
	list.Flags().StringVar(&to, "to", "", "latest date")
	list.Flags().StringVar(&status, "status", "", "DRAFT, POSTED, or ABANDONED")
	show := &cobra.Command{Use: "show NUMBER", Short: "Show a transaction and its lines", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		number, err := transactionNumber(args[0])
		if err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, false)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		journal, err := app.JournalByNumber(cmd.Context(), number)
		if err != nil {
			return err
		}
		return writeHumanTransaction(cmd, opts, application.TransactionFromJournal(app.Company().Key, journal))
	}}
	command.AddCommand(list, show, newTxStatusCommand(opts, "post NUMBER", "Post a draft transaction", "post"), newTxStatusCommand(opts, "abandon NUMBER", "Abandon a draft transaction", "abandon"))
	return command
}
func newTxStatusCommand(opts *options, use, short, action string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		number, err := transactionNumber(args[0])
		if err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.ChangeTransactionStatus(cmd.Context(), number, action, opts.dryRun)
		if err != nil {
			return err
		}
		return writeHumanTransaction(cmd, opts, result)
	}}
}
func newCorrectCommand(opts *options) *cobra.Command {
	var path, reason string
	var draft bool
	command := &cobra.Command{Use: "correct NUMBER", Short: "Reverse a posted transaction and record its corrected replacement", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		number, err := transactionNumber(args[0])
		if err != nil {
			return err
		}
		input, err := readHumanJournal(path)
		if err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.CorrectTransaction(cmd.Context(), number, input, reason, draft, opts.dryRun)
		if err != nil {
			return err
		}
		return writeCorrection(cmd, opts, result)
	}}
	command.Flags().StringVarP(&path, "input", "i", "", "corrected journal JSON file or - for explicit stdin")
	command.Flags().StringVar(&reason, "reason", "", "required audit explanation")
	command.Flags().BoolVar(&draft, "draft", false, "create the reversal and replacement as validated drafts")
	return command
}
func newReverseCommand(opts *options) *cobra.Command {
	var date, memo string
	var draft bool
	command := &cobra.Command{Use: "reverse NUMBER", Short: "Reverse a posted transaction with an immutable linked entry", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return reverseHumanTransaction(cmd, opts, args[0], date, memo, draft, false)
	}}
	command.Flags().StringVar(&date, "date", "today", "reversal date")
	command.Flags().StringVarP(&memo, "memo", "m", "", "reversal description")
	command.Flags().BoolVar(&draft, "draft", false, "create a draft reversal instead of posting")
	return command
}
func newUndoCommand(opts *options) *cobra.Command {
	var date, reason string
	command := &cobra.Command{Use: "undo NUMBER", Short: "Abandon a draft or reverse a posted transaction", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return reverseHumanTransaction(cmd, opts, args[0], date, reason, false, true)
	}}
	command.Flags().StringVar(&date, "date", "today", "reversal date for a posted transaction")
	command.Flags().StringVar(&reason, "reason", "", "why the transaction is being undone")
	return command
}
func reverseHumanTransaction(cmd *cobra.Command, opts *options, number, dateText, memo string, draft, undo bool) error {
	n, err := transactionNumber(number)
	if err != nil {
		return err
	}
	date, err := parseHumanDate(dateText)
	if err != nil {
		return err
	}
	app, err := openWorkflow(cmd, opts, true)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	result, err := app.ReverseTransaction(cmd.Context(), n, date, memo, draft, undo, opts.dryRun)
	if err != nil {
		return err
	}
	return writeHumanTransaction(cmd, opts, result)
}
