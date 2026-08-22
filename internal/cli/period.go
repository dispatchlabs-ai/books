package cli

import (
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
)

func newPeriodCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "period", Short: "Create, close, reopen, and inspect fiscal periods"}
	var code, start, end string
	var year, number int
	var yearEnd bool
	create := &cobra.Command{
		Use: "create", Short: "Create a fiscal period and open it for active books", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.CreatePeriodInput{Code: code, StartDate: start, EndDate: end, FiscalYear: year, PeriodNumber: number, YearEnd: yearEnd}
			if err := requireCommit(opts, "period create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CreatePeriod(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"PERIOD", "START", "END", "YEAR", "NUMBER", "ID"}, [][]string{{result.Code, result.StartDate, result.EndDate, fmt.Sprint(result.FiscalYear), fmt.Sprint(result.PeriodNumber), result.ID}})
		},
	}
	create.Flags().StringVar(&code, "code", "", "period code")
	create.Flags().StringVar(&start, "start", "", "start date")
	create.Flags().StringVar(&end, "end", "", "end date")
	create.Flags().IntVar(&year, "year", 0, "fiscal year")
	create.Flags().IntVar(&number, "number", 0, "period number")
	create.Flags().BoolVar(&yearEnd, "year-end", false, "mark this as the fiscal year's final period")
	var book string
	list := &cobra.Command{
		Use: "list", Short: "List fiscal periods", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			periods, err := ledger.NewService(store, opts.actor).ListPeriods(cmd.Context(), book)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(periods))
			if strings.TrimSpace(book) != "" {
				for _, p := range periods {
					rows = append(rows, []string{p.BookCode, p.Code, p.StartDate, p.EndDate,
						fmt.Sprint(p.FiscalYear), fmt.Sprint(p.PeriodNumber), fmt.Sprint(p.YearEnd),
						p.BookStatus, p.CloseDigest, p.ID})
				}
				return writeResult(cmd, opts.format, periods,
					[]string{"BOOK", "PERIOD", "START", "END", "YEAR", "NUMBER", "YEAR END", "STATUS", "CLOSE DIGEST", "ID"}, rows)
			}
			for _, p := range periods {
				rows = append(rows, []string{p.Code, p.StartDate, p.EndDate, fmt.Sprint(p.FiscalYear), fmt.Sprint(p.PeriodNumber), fmt.Sprint(p.YearEnd), p.ID})
			}
			return writeResult(cmd, opts.format, periods, []string{"PERIOD", "START", "END", "YEAR", "NUMBER", "YEAR END", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&book, "book", "", "optional book code")
	var closeBook, closePeriod string
	closeCommand := &cobra.Command{
		Use: "close", Short: "Validate and close a book period", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opener := openWrite
			if opts.dryRun {
				opener = openRead
			}
			store, err := opener(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ClosePeriod(cmd.Context(), closeBook, closePeriod, opts.dryRun)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"BOOK", "PERIOD", "END", "DIGEST", "CLOSED", "DRY RUN"}, [][]string{{result.BookCode, result.PeriodCode, result.EndDate, result.Digest, fmt.Sprint(result.Closed), fmt.Sprint(result.ValidationOnly)}})
		},
	}
	closeCommand.Flags().StringVar(&closeBook, "book", "", "book code")
	closeCommand.Flags().StringVar(&closePeriod, "period", "", "period code")
	var reopenReason string
	reopen := &cobra.Command{
		Use: "reopen", Short: "Reopen a closed book period with an audit reason", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "period reopen"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).ReopenPeriod(cmd.Context(), closeBook, closePeriod, reopenReason); err != nil {
				return err
			}
			data := map[string]any{"book": strings.ToUpper(closeBook), "period": strings.ToUpper(closePeriod), "reopened": true, "reason": reopenReason}
			return writeResult(cmd, opts.format, data, []string{"BOOK", "PERIOD", "REOPENED", "REASON"}, [][]string{{strings.ToUpper(closeBook), strings.ToUpper(closePeriod), "true", reopenReason}})
		},
	}
	reopen.Flags().StringVar(&closeBook, "book", "", "book code")
	reopen.Flags().StringVar(&closePeriod, "period", "", "period code")
	reopen.Flags().StringVar(&reopenReason, "reason", "", "required audit reason")
	var yearCloseBook, retainedEarnings string
	var closeYear int
	yearClose := &cobra.Command{
		Use: "year-close", Short: "Derive and post an exact fiscal-year close to retained earnings", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opener := openWrite
			if opts.dryRun {
				opener = openRead
			}
			store, err := opener(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).PostFiscalYearClose(cmd.Context(), ledger.FiscalYearCloseInput{
				Book: yearCloseBook, FiscalYear: closeYear, RetainedEarnings: retainedEarnings,
			}, opts.dryRun)
			if err != nil {
				return err
			}
			journalID := ""
			if result.Journal != nil {
				journalID = result.Journal.ID
			}
			return writeResult(cmd, opts.format, result,
				[]string{"BOOK", "YEAR", "NET INCOME", "JOURNAL", "DRY RUN"},
				[][]string{{strings.ToUpper(yearCloseBook), fmt.Sprint(closeYear), money.Format(result.NetIncome), journalID, fmt.Sprint(result.DryRun)}})
		},
	}
	yearClose.Flags().StringVar(&yearCloseBook, "book", "", "book code")
	yearClose.Flags().IntVar(&closeYear, "year", 0, "fiscal year")
	yearClose.Flags().StringVar(&retainedEarnings, "retained-earnings", "", "equity account code")
	command.AddCommand(create, list, closeCommand, reopen, yearClose)
	return command
}
