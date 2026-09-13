package cli

import (
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/spf13/cobra"
	"path/filepath"
	"strconv"
	"strings"
)

type periodClosePlan = application.PeriodClosePlan
type yearClosePlan = application.YearClosePlan
type periodCloseOutput struct {
	Plan     periodClosePlan `json:"plan"`
	PlanPath string          `json:"plan_path,omitempty"`
	Status   string          `json:"status"`
	DryRun   bool            `json:"dry_run"`
}
type yearCloseOutput struct {
	Plan        yearClosePlan     `json:"plan"`
	PlanPath    string            `json:"plan_path,omitempty"`
	Status      string            `json:"status"`
	Transaction *humanTransaction `json:"transaction,omitempty"`
	DryRun      bool              `json:"dry_run"`
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
		[][]string{{strconv.Itoa(output.Plan.FiscalYear), outputCurrency(cmd).Format(output.Plan.NetIncomeCents), output.Plan.RetainedEarnings, output.Status, number, output.PlanPath, fmt.Sprint(output.DryRun)}})
}
func newCloseCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "close", Short: "Plan and apply an auditable period close"}
	command.AddCommand(newPeriodClosePlanCommand(opts), newPeriodCloseApplyCommand(opts))
	return command
}
func newYearCloseCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "year-close", Short: "Plan and apply the fiscal-year transfer to retained earnings"}
	command.AddCommand(newYearClosePlanCommand(opts), newYearCloseApplyCommand(opts))
	return command
}

func saveWorkflowPlan(cmd *cobra.Command, opts *options, path, defaultName string, plan any) (string, error) {
	if opts.dryRun {
		return path, nil
	}
	if path == "" {
		resolved, err := opts.resolveCompany()
		if err != nil {
			return "", err
		}
		path = filepath.Join(resolved.Plans, defaultName)
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if err = writeExclusiveJSON(path, plan); err != nil {
		return "", err
	}
	return path, nil
}
func newPeriodClosePlanCommand(opts *options) *cobra.Command {
	var path string
	command := &cobra.Command{Use: "plan PERIOD", Short: "Validate a period and write its immutable close plan", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, err := openWorkflow(cmd, opts, false)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		plan, err := app.PlanPeriodClose(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		path, err = saveWorkflowPlan(cmd, opts, path, "close-"+strings.ToLower(plan.Period)+".json", plan)
		if err != nil {
			return err
		}
		return writePeriodClose(cmd, opts, periodCloseOutput{Plan: plan, PlanPath: path, Status: "READY", DryRun: opts.dryRun})
	}}
	command.Flags().StringVarP(&path, "out", "o", "", "plan path (defaults inside the company plans directory)")
	return command
}
func newPeriodCloseApplyCommand(opts *options) *cobra.Command {
	var path string
	command := &cobra.Command{Use: "apply", Short: "Close a period from a reviewed, non-stale plan", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
		}
		var plan periodClosePlan
		if err := readJSONInput(path, &plan); err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.ApplyPeriodClose(cmd.Context(), plan, opts.dryRun)
		if err != nil {
			return err
		}
		return writePeriodClose(cmd, opts, periodCloseOutput{Plan: result.Plan, PlanPath: path, Status: result.Status, DryRun: result.DryRun})
	}}
	command.Flags().StringVar(&path, "plan", "", "reviewed period close plan JSON")
	return command
}
func newYearClosePlanCommand(opts *options) *cobra.Command {
	var retained, path string
	command := &cobra.Command{Use: "plan YEAR", Short: "Derive and write the exact fiscal-year closing journal", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		year, err := strconv.Atoi(args[0])
		if err != nil {
			return apperr.New(apperr.Invalid, "FISCAL_YEAR_INVALID", "YEAR must be a four-digit fiscal year")
		}
		app, err := openWorkflow(cmd, opts, false)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		plan, err := app.PlanYearClose(cmd.Context(), year, retained)
		if err != nil {
			return err
		}
		path, err = saveWorkflowPlan(cmd, opts, path, fmt.Sprintf("year-close-%d.json", year), plan)
		if err != nil {
			return err
		}
		return writeYearClose(cmd, opts, yearCloseOutput{Plan: plan, PlanPath: path, Status: "READY", DryRun: opts.dryRun})
	}}
	command.Flags().StringVar(&retained, "retained-earnings", "", "equity account (uses the company default when omitted)")
	command.Flags().StringVarP(&path, "out", "o", "", "plan path")
	return command
}
func newYearCloseApplyCommand(opts *options) *cobra.Command {
	var path string
	command := &cobra.Command{Use: "apply", Short: "Post a reviewed, non-stale fiscal-year close", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
		}
		var plan yearClosePlan
		if err := readJSONInput(path, &plan); err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, true)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.ApplyYearClose(cmd.Context(), plan, opts.dryRun)
		if err != nil {
			return err
		}
		return writeYearClose(cmd, opts, yearCloseOutput{Plan: result.Plan, PlanPath: path, Status: result.Status, Transaction: result.Transaction, DryRun: result.DryRun})
	}}
	command.Flags().StringVar(&path, "plan", "", "reviewed fiscal-year close plan JSON")
	return command
}
