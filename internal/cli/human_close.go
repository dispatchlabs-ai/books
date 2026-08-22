package cli

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

const (
	periodClosePlanSchema = "books.period-close-plan/v1"
	yearClosePlanSchema   = "books.year-close-plan/v1"
)

type periodClosePlan struct {
	Schema       string `json:"schema"`
	Company      string `json:"company"`
	Book         string `json:"book"`
	Period       string `json:"period"`
	EndDate      string `json:"end_date"`
	LedgerDigest string `json:"ledger_digest"`
	CreatedAt    string `json:"created_at"`
	Digest       string `json:"digest"`
}

type periodCloseOutput struct {
	Plan     periodClosePlan `json:"plan"`
	PlanPath string          `json:"plan_path,omitempty"`
	Status   string          `json:"status"`
	DryRun   bool            `json:"dry_run"`
}

type yearClosePlan struct {
	Schema           string                    `json:"schema"`
	Company          string                    `json:"company"`
	Book             string                    `json:"book"`
	FiscalYear       int                       `json:"fiscal_year"`
	RetainedEarnings string                    `json:"retained_earnings"`
	NetIncomeCents   int64                     `json:"net_income_cents"`
	Journal          ledger.CreateJournalInput `json:"journal"`
	JournalDigest    string                    `json:"journal_digest"`
	CreatedAt        string                    `json:"created_at"`
	Digest           string                    `json:"digest"`
}

type yearCloseOutput struct {
	Plan        yearClosePlan     `json:"plan"`
	PlanPath    string            `json:"plan_path,omitempty"`
	Status      string            `json:"status"`
	Transaction *humanTransaction `json:"transaction,omitempty"`
	DryRun      bool              `json:"dry_run"`
}

func newCloseCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "close", Short: "Plan and apply an auditable period close"}
	command.AddCommand(newPeriodClosePlanCommand(opts), newPeriodCloseApplyCommand(opts))
	return command
}

func newPeriodClosePlanCommand(opts *options) *cobra.Command {
	var outputPath string
	command := &cobra.Command{
		Use: "plan PERIOD", Short: "Validate a period and write its immutable close plan", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			period := strings.ToUpper(strings.TrimSpace(args[0]))
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ClosePeriod(cmd.Context(), resolved.Company.BookCode, period, true)
			if err != nil {
				return err
			}
			plan := periodClosePlan{
				Schema: periodClosePlanSchema, Company: resolved.Key, Book: resolved.Company.BookCode,
				Period: period, EndDate: result.EndDate, LedgerDigest: result.Digest,
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			plan.Digest, err = digestPeriodClosePlan(plan)
			if err != nil {
				return err
			}
			if !opts.dryRun {
				if outputPath == "" {
					outputPath = filepath.Join(resolved.Plans, "close-"+strings.ToLower(period)+".json")
				}
				outputPath, err = filepath.Abs(filepath.Clean(outputPath))
				if err != nil {
					return err
				}
				if err := writeExclusiveJSON(outputPath, plan); err != nil {
					return err
				}
			}
			return writePeriodClose(cmd, opts, periodCloseOutput{Plan: plan, PlanPath: outputPath, Status: "READY", DryRun: opts.dryRun})
		},
	}
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path (defaults inside the company plans directory)")
	return command
}

func newPeriodCloseApplyCommand(opts *options) *cobra.Command {
	var planPath string
	command := &cobra.Command{
		Use: "apply", Short: "Close a period from a reviewed, non-stale plan", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan periodClosePlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			if plan.Schema != periodClosePlanSchema {
				return apperr.New(apperr.Invalid, "CLOSE_PLAN_INVALID", "period close plan schema is unsupported")
			}
			digest, err := digestPeriodClosePlan(plan)
			if err != nil {
				return err
			}
			if digest != plan.Digest {
				return apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "period close plan changed after generation")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if resolved.Key != plan.Company || resolved.Company.BookCode != plan.Book {
				return apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to --company %s", plan.Company))
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			alreadyClosed, err := periodCloseAlreadyApplied(cmd, store, plan)
			if err != nil {
				_ = store.Close()
				return err
			}
			if alreadyClosed {
				_ = store.Close()
				return writePeriodClose(cmd, opts, periodCloseOutput{Plan: plan, PlanPath: planPath, Status: "CLOSED"})
			}
			current, err := ledger.NewService(store, opts.actor).ClosePeriod(cmd.Context(), plan.Book, plan.Period, true)
			_ = store.Close()
			if err != nil {
				return err
			}
			if current.Digest != plan.LedgerDigest || current.EndDate != plan.EndDate {
				return apperr.New(apperr.Conflict, "CLOSE_PLAN_STALE", "ledger content changed after planning; generate and review a new close plan")
			}
			if opts.dryRun {
				return writePeriodClose(cmd, opts, periodCloseOutput{Plan: plan, PlanPath: planPath, Status: "VALIDATED", DryRun: true})
			}
			store, err = openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			_, err = ledger.NewService(store, opts.actor).ClosePeriodFromPlan(cmd.Context(), plan.Book, plan.Period, plan.EndDate, plan.LedgerDigest)
			if err != nil {
				return err
			}
			return writePeriodClose(cmd, opts, periodCloseOutput{Plan: plan, PlanPath: planPath, Status: "CLOSED"})
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed period close plan JSON")
	return command
}

func newYearCloseCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "year-close", Short: "Plan and apply the fiscal-year transfer to retained earnings"}
	command.AddCommand(newYearClosePlanCommand(opts), newYearCloseApplyCommand(opts))
	return command
}

func newYearClosePlanCommand(opts *options) *cobra.Command {
	var retainedEarnings, outputPath string
	command := &cobra.Command{
		Use: "plan YEAR", Short: "Derive and write the exact fiscal-year closing journal", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			year, err := strconv.Atoi(args[0])
			if err != nil || year < 1900 {
				return apperr.New(apperr.Invalid, "FISCAL_YEAR_INVALID", "YEAR must be a four-digit fiscal year")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if retainedEarnings == "" {
				retainedEarnings = resolved.Company.Defaults.RetainedEarnings
			}
			if retainedEarnings == "" {
				return apperr.New(apperr.Validation, "RETAINED_EARNINGS_REQUIRED", "pass --retained-earnings or set defaults.retained-earnings")
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			prepared, err := ledger.NewService(store, opts.actor).PostFiscalYearClose(cmd.Context(), ledger.FiscalYearCloseInput{
				Book: resolved.Company.BookCode, FiscalYear: year, RetainedEarnings: retainedEarnings,
			}, true)
			if err != nil {
				return err
			}
			prepared.Input, err = stampYearCloseJournal(prepared.Input, resolved.Key, year)
			if err != nil {
				return err
			}
			journalDigest, err := digestJSON(prepared.Input)
			if err != nil {
				return err
			}
			plan := yearClosePlan{
				Schema: yearClosePlanSchema, Company: resolved.Key, Book: resolved.Company.BookCode,
				FiscalYear: year, RetainedEarnings: strings.ToUpper(retainedEarnings), NetIncomeCents: prepared.NetIncome,
				Journal: prepared.Input, JournalDigest: journalDigest, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			plan.Digest, err = digestYearClosePlan(plan)
			if err != nil {
				return err
			}
			if !opts.dryRun {
				if outputPath == "" {
					outputPath = filepath.Join(resolved.Plans, fmt.Sprintf("year-close-%d.json", year))
				}
				outputPath, err = filepath.Abs(filepath.Clean(outputPath))
				if err != nil {
					return err
				}
				if err := writeExclusiveJSON(outputPath, plan); err != nil {
					return err
				}
			}
			return writeYearClose(cmd, opts, yearCloseOutput{Plan: plan, PlanPath: outputPath, Status: "READY", DryRun: opts.dryRun})
		},
	}
	command.Flags().StringVar(&retainedEarnings, "retained-earnings", "", "equity account (uses the company default when omitted)")
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path")
	return command
}

func newYearCloseApplyCommand(opts *options) *cobra.Command {
	var planPath string
	command := &cobra.Command{
		Use: "apply", Short: "Post a reviewed, non-stale fiscal-year close", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan yearClosePlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			if plan.Schema != yearClosePlanSchema {
				return apperr.New(apperr.Invalid, "YEAR_CLOSE_PLAN_INVALID", "year close plan schema is unsupported")
			}
			digest, err := digestYearClosePlan(plan)
			if err != nil {
				return err
			}
			if digest != plan.Digest {
				return apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "year close plan changed after generation")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if resolved.Key != plan.Company || resolved.Company.BookCode != plan.Book {
				return apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to --company %s", plan.Company))
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			prepared, err := ledger.NewService(store, opts.actor).PostFiscalYearClose(cmd.Context(), ledger.FiscalYearCloseInput{
				Book: plan.Book, FiscalYear: plan.FiscalYear, RetainedEarnings: plan.RetainedEarnings,
			}, true)
			_ = store.Close()
			if err != nil {
				return err
			}
			prepared.Input, err = stampYearCloseJournal(prepared.Input, plan.Company, plan.FiscalYear)
			if err != nil {
				return err
			}
			currentDigest, err := digestJSON(prepared.Input)
			if err != nil {
				return err
			}
			if currentDigest != plan.JournalDigest || prepared.NetIncome != plan.NetIncomeCents {
				return apperr.New(apperr.Conflict, "YEAR_CLOSE_PLAN_STALE", "fiscal-year activity changed; generate and review a new year-close plan")
			}
			if opts.dryRun {
				return writeYearClose(cmd, opts, yearCloseOutput{Plan: plan, PlanPath: planPath, Status: "VALIDATED", DryRun: true})
			}
			store, err = openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service := ledger.NewService(store, opts.actor)
			journal, err := service.CreateAndPostJournal(cmd.Context(), plan.Journal)
			if err != nil {
				return err
			}
			if journal.Status == "ABANDONED" {
				return apperr.New(apperr.Conflict, "YEAR_CLOSE_ABANDONED", "the reviewed fiscal-year close was previously abandoned; generate a new plan after correcting the ledger")
			}
			output := yearCloseOutput{Plan: plan, PlanPath: planPath, Status: "POSTED"}
			value := humanTransactionFromJournal(resolved.Key, journal)
			output.Transaction = &value
			return writeYearClose(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed fiscal-year close plan JSON")
	return command
}

func digestJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func periodCloseAlreadyApplied(cmd *cobra.Command, store *storesqlite.Store, plan periodClosePlan) (bool, error) {
	var status, endDate, closeDigest string
	err := store.DB().QueryRowContext(cmd.Context(), `SELECT bp.status, fp.end_date, COALESCE(bp.close_digest, '')
		FROM book_periods bp
		JOIN books b ON b.id = bp.book_id
		JOIN fiscal_periods fp ON fp.id = bp.period_id
		WHERE b.code = ? AND fp.code = ?`, plan.Book, plan.Period).Scan(&status, &endDate, &closeDigest)
	if err == sql.ErrNoRows {
		return false, apperr.New(apperr.NotFound, "BOOK_PERIOD_NOT_FOUND", "period is not configured for this book")
	}
	if err != nil {
		return false, err
	}
	if status != "CLOSED" {
		return false, nil
	}
	if _, err := storesqlite.VerifyAudit(cmd.Context(), store.DB()); err != nil {
		return false, err
	}
	if endDate != plan.EndDate || closeDigest != plan.LedgerDigest {
		return false, apperr.New(apperr.Conflict, "CLOSE_PLAN_ALREADY_CLOSED", "period is already closed with evidence that does not match this plan")
	}
	return true, nil
}

func stampYearCloseJournal(input ledger.CreateJournalInput, company string, fiscalYear int) (ledger.CreateJournalInput, error) {
	seed := input
	seed.SourceSystem = ""
	seed.SourceKey = ""
	digest, err := digestJSON(seed)
	if err != nil {
		return input, err
	}
	input.SourceSystem = "BOOKS_YEAR_CLOSE"
	input.SourceKey = fmt.Sprintf("%s:%d:%s", company, fiscalYear, digest[:16])
	return input, nil
}

func digestPeriodClosePlan(plan periodClosePlan) (string, error) {
	plan.Digest = ""
	return digestJSON(plan)
}

func digestYearClosePlan(plan yearClosePlan) (string, error) {
	plan.Digest = ""
	return digestJSON(plan)
}

func writePeriodClose(cmd *cobra.Command, opts *options, output periodCloseOutput) error {
	return writeResult(cmd, opts.format, output, []string{"PERIOD", "END", "LEDGER DIGEST", "STATUS", "PLAN", "DRY RUN"},
		[][]string{{output.Plan.Period, output.Plan.EndDate, output.Plan.LedgerDigest, output.Status, output.PlanPath, fmt.Sprint(output.DryRun)}})
}

func writeYearClose(cmd *cobra.Command, opts *options, output yearCloseOutput) error {
	number := ""
	if output.Transaction != nil {
		number = strconv.FormatInt(output.Transaction.Number, 10)
	}
	return writeResult(cmd, opts.format, output, []string{"YEAR", "NET INCOME", "RETAINED EARNINGS", "STATUS", "TRANSACTION", "PLAN", "DRY RUN"},
		[][]string{{strconv.Itoa(output.Plan.FiscalYear), money.Format(output.Plan.NetIncomeCents), output.Plan.RetainedEarnings, output.Status, number, output.PlanPath, fmt.Sprint(output.DryRun)}})
}
