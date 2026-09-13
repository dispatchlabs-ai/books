package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"

	"github.com/spf13/cobra"
)

type quickBooksImportPlan = application.QuickBooksPlan

type quickBooksPlanOutput struct {
	Plan     quickBooksImportPlan `json:"plan"`
	PlanPath string               `json:"plan_path,omitempty"`
	Written  bool                 `json:"written"`
}

type quickBooksApplyOutput = application.QuickBooksResult

type quickBooksBuildFlags struct {
	from, accounts, start, through, mode, output string
}

func newImportCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "import", Short: "Inspect, plan, and apply initial accounting-system imports"}
	command.AddCommand(newQuickBooksCommand(opts))
	return command
}

func newQuickBooksCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "quickbooks", Short: "Initial import from reviewed QuickBooks exports"}
	command.AddCommand(newQuickBooksBuildCommand(opts, "inspect", false), newQuickBooksBuildCommand(opts, "plan", true), newQuickBooksApplyCommand(opts))
	return command
}

func newQuickBooksBuildCommand(opts *options, use string, writePlan bool) *cobra.Command {
	flags := quickBooksBuildFlags{mode: "auto"}
	short := "Inspect QuickBooks exports without changing Books"
	if writePlan {
		short = "Build a reviewable, content-hashed QuickBooks import plan"
	}
	command := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openWorkflow(cmd, opts, false)
			if err != nil {
				return err
			}
			defer func() { _ = app.Close() }()
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if flags.start != "" {
				flags.start, err = parseHumanDate(flags.start)
				if err != nil {
					return err
				}
			}
			if flags.through != "" {
				flags.through, err = parseHumanDate(flags.through)
				if err != nil {
					return err
				}
			}
			plan, err := app.PlanQuickBooks(cmd.Context(), application.QuickBooksRequest{From: flags.from, Accounts: flags.accounts, Start: flags.start, Through: flags.through, Mode: flags.mode})
			if err != nil {
				return err
			}

			written := false
			if writePlan && !opts.dryRun {
				if flags.output == "" {
					flags.output = filepath.Join(resolved.Plans, fmt.Sprintf("quickbooks-%s-%s.json", plan.Source.StartDate, plan.Source.EndDate))
				}
				flags.output, err = filepath.Abs(filepath.Clean(flags.output))
				if err != nil {
					return err
				}
				if err := writeExclusiveJSON(flags.output, plan); err != nil {
					return err
				}
				written = true
			}
			output := quickBooksPlanOutput{Plan: plan, PlanPath: flags.output, Written: written}
			if !plan.Ready {
				message := strings.Join(plan.Blockers, "; ")
				if written {
					message += fmt.Sprintf("; blocked plan written to %s", output.PlanPath)
				}
				return apperr.New(apperr.Validation, "QUICKBOOKS_PLAN_BLOCKED", message)
			}
			return writeQuickBooksPlan(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&flags.from, "from", "", "QuickBooks object directory, GeneralLedger JSON, or journal XLSX")
	command.Flags().StringVar(&flags.accounts, "accounts", "", "Account.json path (inferred from the source directory when omitted)")
	command.Flags().StringVar(&flags.start, "start", "", "inclusive import start (inferred for JSON exports)")
	command.Flags().StringVar(&flags.through, "through", "", "inclusive import cutoff (inferred for JSON exports)")
	command.Flags().StringVar(&flags.mode, "mode", flags.mode, "auto, general-ledger, objects, or journal")
	if writePlan {
		command.Flags().StringVarP(&flags.output, "out", "o", "", "plan path")
	}
	return command
}

func writeQuickBooksPlan(cmd *cobra.Command, opts *options, output quickBooksPlanOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"COMPANY", "SOURCE", "START", "END", "ACCOUNTS", "JOURNALS", "DIAGNOSTICS", "READY", "PLAN"},
		[][]string{{output.Plan.Company, string(output.Plan.Source.Kind), output.Plan.Source.StartDate, output.Plan.Source.EndDate,
			fmt.Sprint(output.Plan.AccountCount), fmt.Sprint(output.Plan.JournalCount), fmt.Sprint(len(output.Plan.Import.Diagnostics)), fmt.Sprint(output.Plan.Ready), output.PlanPath}})
}

func newQuickBooksApplyCommand(opts *options) *cobra.Command {
	var planPath string
	var draft bool
	command := &cobra.Command{
		Use: "apply", Short: "Apply a reviewed QuickBooks plan and post all journals atomically", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan quickBooksImportPlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			app, err := openWorkflow(cmd, opts, true)
			if err != nil {
				return err
			}
			defer func() { _ = app.Close() }()
			output, err := app.ApplyQuickBooks(cmd.Context(), plan, filepath.Base(planPath), draft, opts.dryRun)
			if err != nil {
				return err
			}
			return writeQuickBooksApply(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed QuickBooks plan JSON")
	command.Flags().BoolVar(&draft, "draft", false, "import journals as drafts instead of atomically posting the batch")
	return command
}

func writeQuickBooksApply(cmd *cobra.Command, opts *options, output quickBooksApplyOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"COMPANY", "ACCOUNTS", "STATEMENT CONTROLS", "PERIODS CREATED", "JOURNALS", "CREATED", "POSTED", "STATUS", "DRY RUN"},
		[][]string{{output.Company, fmt.Sprint(output.Accounts), fmt.Sprint(output.StatementControls), fmt.Sprint(output.PeriodsCreated), fmt.Sprint(output.Journals), fmt.Sprint(output.JournalsCreated), fmt.Sprint(output.JournalsPosted), output.Status, fmt.Sprint(output.DryRun)}})
}
