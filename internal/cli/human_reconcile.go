package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"

	"github.com/spf13/cobra"
)

const manualReconciliationPlanSchema = "books.reconciliation-plan/v3"

type manualReconciliationPlan = application.ReconciliationPlan

type manualReconciliationPlanOutput struct {
	Plan     manualReconciliationPlan `json:"plan"`
	PlanPath string                   `json:"plan_path,omitempty"`
	Written  bool                     `json:"written"`
}

type manualReconciliationApplyOutput = application.ReconciliationOutput

func newManualReconciliationPlanCommand(opts *options) *cobra.Command {
	var through, startText, beginningText, endingText, cleared, outputPath string
	command := &cobra.Command{
		Use:   "plan ACCOUNT",
		Short: "Create a deterministic manual bank-reconciliation plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManualReconciliationPlan(cmd, opts, args[0], "", through, startText, beginningText, endingText, cleared, outputPath)
		},
	}
	command.Flags().StringVar(&through, "through", "", "statement ending date (required)")
	command.Flags().StringVar(&startText, "start", "", "starting date (normally inferred from account/prior reconciliation)")
	command.Flags().StringVar(&beginningText, "beginning", "", "statement beginning balance (normally inferred)")
	command.Flags().StringVar(&endingText, "ending", "", "statement ending balance (required; debts may be entered as a positive amount owed)")
	command.Flags().StringVar(&cleared, "cleared", "all", "transaction numbers cleared by the statement, such as 4,7-10, none, or all")
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path (defaults inside the selected company's plans directory)")
	return command
}

func newManualReconciliationReplanCommand(opts *options) *cobra.Command {
	var endingText, cleared, outputPath string
	command := &cobra.Command{
		Use: "replan RECONCILIATION_ID", Short: "Revise and rebuild an explicitly reopened reconciliation", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManualReconciliationPlan(cmd, opts, "", args[0], "", "", "", endingText, cleared, outputPath)
		},
	}
	command.Flags().StringVar(&endingText, "ending", "", "revised statement ending balance (required)")
	command.Flags().StringVar(&cleared, "cleared", "all", "transaction numbers cleared by the statement, such as 4,7-10, none, or all")
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path (defaults inside the selected company's plans directory)")
	return command
}

func runManualReconciliationPlan(cmd *cobra.Command, opts *options, accountSelector, targetID, through, startText, beginningText, endingText, cleared, outputPath string) error {
	app, err := openWorkflow(cmd, opts, false)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	resolved, err := opts.resolveCompany()
	if err != nil {
		return err
	}
	if through != "" {
		through, err = parseHumanDate(through)
		if err != nil {
			return err
		}
	}
	if startText != "" {
		startText, err = parseHumanDate(startText)
		if err != nil {
			return err
		}
	}
	r := application.ReconciliationRequest{StatementAccount: accountSelector, TargetID: targetID, Through: through, Start: startText, Beginning: beginningText, Ending: endingText}
	switch strings.ToLower(strings.TrimSpace(cleared)) {
	case "", "all":
		r.ClearAll = true
	case "none":
	default:
		numbers, e := parseNumberSet(cleared)
		if e != nil {
			return e
		}
		for n := range numbers {
			r.Cleared = append(r.Cleared, n)
		}
	}
	plan, err := app.PlanReconciliation(cmd.Context(), r)
	if err != nil {
		return err
	}

	written := false
	if !opts.dryRun {
		if strings.TrimSpace(outputPath) == "" {
			prefix := "reconcile"
			if plan.TargetReconciliationID != "" {
				prefix = "rereconcile"
			}
			outputPath = filepath.Join(resolved.Plans, fmt.Sprintf("%s-%s-%s.json", prefix, strings.ToLower(plan.ControlAccount), plan.EndDate))
		}
		outputPath, err = filepath.Abs(filepath.Clean(outputPath))
		if err != nil {
			return apperr.Wrap(apperr.Invalid, "PLAN_PATH_INVALID", "resolve plan path", err)
		}
		if err := writeExclusiveJSON(outputPath, plan); err != nil {
			return err
		}
		written = true
	}
	output := manualReconciliationPlanOutput{Plan: plan, PlanPath: outputPath, Written: written}
	if !plan.Ready {
		message := strings.Join(plan.Blockers, "; ")
		if written {
			message += fmt.Sprintf("; blocked plan written to %s", output.PlanPath)
		}
		return apperr.New(apperr.Validation, "RECONCILIATION_PLAN_BLOCKED", message)
	}
	return writeManualReconciliationPlan(cmd, opts, output)
}

func newManualReconciliationApplyCommand(opts *options) *cobra.Command {
	var planPath string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a previously reviewed manual reconciliation plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(planPath) == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan manualReconciliationPlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			app, err := openWorkflow(cmd, opts, true)
			if err != nil {
				return err
			}
			defer func() { _ = app.Close() }()
			output, err := app.ApplyReconciliation(cmd.Context(), plan, filepath.Base(planPath), opts.dryRun)
			if err != nil {
				return err
			}
			return writeManualReconciliationApply(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed reconciliation plan JSON")
	return command
}

func writeExclusiveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return apperr.Wrap(apperr.Unavailable, "PLAN_DIRECTORY_FAILED", "create plan directory", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return apperr.New(apperr.Conflict, "PLAN_EXISTS", fmt.Sprintf("plan already exists at %s; choose another --out path", path))
		}
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "create plan", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "write plan", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "sync plan", err)
	}
	if err := file.Close(); err != nil {
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "close plan", err)
	}
	complete = true
	return nil
}

func parseNumberSet(value string) (map[int64]bool, error) {
	result := map[int64]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "--cleared contains an empty transaction number")
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "--cleared must contain numbers or inclusive ranges")
		}
		first, err := strconv.ParseInt(bounds[0], 10, 64)
		if err != nil || first < 1 {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", fmt.Sprintf("invalid transaction number %q", part))
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseInt(bounds[1], 10, 64)
			if err != nil || last < first || last-first > 100000 {
				return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", fmt.Sprintf("invalid transaction range %q", part))
			}
		}
		for number := first; ; number++ {
			result[number] = true
			if len(result) > 100000 {
				return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "too many cleared transaction numbers")
			}
			if number == last {
				break
			}
		}
	}
	return result, nil
}

func writeManualReconciliationPlan(cmd *cobra.Command, opts *options, output manualReconciliationPlanOutput) error {
	plan := output.Plan
	return writeResult(cmd, opts.format, output,
		[]string{"ACCOUNT", "START", "END", "STATEMENT BEGINNING", "CLEARED ACTIVITY", "STATEMENT ENDING", "OUTSTANDING", "ADJUSTED ENDING", "CLEARED", "READY", "PLAN"},
		[][]string{{plan.ControlAccount, plan.StartDate, plan.EndDate, outputCurrency(cmd).Format(plan.BeginningBalanceCents), outputCurrency(cmd).Format(plan.ActivityCents), outputCurrency(cmd).Format(plan.EndingBalanceCents), outputCurrency(cmd).Format(plan.EndingOutstandingCents), outputCurrency(cmd).Format(plan.AdjustedEndingCents), strconv.Itoa(len(plan.Cleared)), fmt.Sprint(plan.Ready), output.PlanPath}})
}

func writeManualReconciliationApply(cmd *cobra.Command, opts *options, output manualReconciliationApplyOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"ACCOUNT", "START", "END", "ENDING", "TRANSACTIONS", "ALLOCATIONS", "STATUS", "DRY RUN"},
		[][]string{{output.StatementAccount, output.StartDate, output.EndDate, outputCurrency(cmd).Format(output.EndingCents), strconv.Itoa(output.TransactionCount), strconv.Itoa(output.AllocationCount), output.Status, fmt.Sprint(output.DryRun)}})
}

func manualPlanDigest(plan manualReconciliationPlan) (string, error) {
	return application.DigestReconciliationPlan(plan)
}
