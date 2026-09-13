package cli

import (
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/application"
	"path/filepath"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

type precoverageClosureCommandOutput struct {
	ledger.StatementAccountPrecoverageClosure
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run"`
}

func newStatementAccountLifecycleCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "lifecycle", Short: "Record audited statement-account lifecycle evidence"}
	var inputPath string
	var commit bool
	closeBeforeCoverage := &cobra.Command{
		Use: "close-before-coverage", Short: "Certify an exact-zero provider closure before required reconciliation coverage", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" || inputPath == "-" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input must be an absolute retained JSON file path")
			}
			if !filepath.IsAbs(inputPath) {
				return apperr.New(apperr.Invalid, "INPUT_PATH_INVALID", "--input must be an absolute retained JSON file path")
			}
			var store *storesqlite.Store
			var err error
			if !commit || opts.dryRun {
				store, err = openRead(cmd, opts)
			} else {
				store, err = openWrite(cmd, opts)
			}
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			input, err := application.ReadPrecoverageClosureInput(inputPath, func(key string) (money.Currency, error) { return targetCurrency(cmd, store, "statement", key) })
			if err != nil {
				return err
			}
			service := ledger.NewService(store, opts.actor)
			var result ledger.StatementAccountPrecoverageClosure
			if !commit || opts.dryRun {
				result, err = service.ValidateStatementAccountPrecoverageClosure(cmd.Context(), input)
			} else {
				result, err = service.CloseStatementAccountBeforeCoverage(cmd.Context(), input)
			}
			if err != nil {
				return err
			}
			output := precoverageClosureCommandOutput{StatementAccountPrecoverageClosure: result, Committed: commit && !opts.dryRun, DryRun: opts.dryRun}
			return writeResult(cmd, opts.format, precoverageClosureMachineOutput(output), precoverageClosureHeaders, [][]string{precoverageClosureRow(output)})
		},
	}
	closeBeforeCoverage.Flags().StringVarP(&inputPath, "input", "i", "", "absolute retained lifecycle JSON input path")
	closeBeforeCoverage.Flags().BoolVar(&commit, "commit", false, "commit the lifecycle evidence and archive; otherwise preview")
	_ = closeBeforeCoverage.MarkFlagRequired("input")

	var statementAccount, entity string
	list := &cobra.Command{
		Use: "list", Short: "List immutable precoverage closure certificates", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListStatementAccountPrecoverageClosures(cmd.Context(), ledger.PrecoverageClosureFilter{StatementAccount: statementAccount, Entity: entity})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result))
			machine := make([]map[string]any, 0, len(result))
			for _, closure := range result {
				output := precoverageClosureCommandOutput{StatementAccountPrecoverageClosure: closure, Committed: true}
				rows = append(rows, precoverageClosureRow(output))
				machine = append(machine, precoverageClosureMachineOutput(output))
			}
			return writeResult(cmd, opts.format, machine, precoverageClosureHeaders, rows)
		},
	}
	list.Flags().StringVar(&statementAccount, "statement-account", "", "optional statement-account code")
	list.Flags().StringVar(&entity, "entity", "", "optional entity code")
	command.AddCommand(closeBeforeCoverage, list)
	return command
}

var precoverageClosureHeaders = []string{
	"ACCOUNT", "ENTITY", "CLOSED ON", "DISPOSITION", "STATUS", "CONTROL AT CLOSURE",
	"CURRENT CONTROL", "POST-CLOSE LINES", "DRAFT LINES", "ACTIVE IDENTITIES", "IDENTITY DIGEST",
	"INPUT SHA256", "CHANGED", "COMMITTED", "ID",
}

func precoverageClosureRow(value precoverageClosureCommandOutput) []string {
	return []string{
		value.StatementAccount, value.EntityCode, value.ClosedOn, value.CoverageDisposition, value.Status,
		value.Currency.Format(value.ControlBalanceAtClosureCents), value.Currency.Format(value.CurrentControlBalanceCents),
		fmt.Sprint(value.PostClosureControlLineCount), fmt.Sprint(value.DraftControlLineCount),
		fmt.Sprint(value.ActiveIdentityCount), value.ActiveIdentityDigest, value.InputSourceSHA256, fmt.Sprint(value.Changed),
		fmt.Sprint(value.Committed), value.ID,
	}
}

func precoverageClosureMachineOutput(value precoverageClosureCommandOutput) map[string]any {
	closure := value.StatementAccountPrecoverageClosure
	closureEvidence := map[string]any{
		"source_kind":   closure.ClosureEvidence.SourceKind,
		"source_path":   closure.ClosureEvidence.SourcePath,
		"source_sha256": closure.ClosureEvidence.SourceSHA256,
		"locator":       "official provider closure evidence",
	}
	zeroEvidence := map[string]any{
		"source_kind":             closure.ZeroEvidence.SourceKind,
		"source_path":             closure.ZeroEvidence.SourcePath,
		"source_sha256":           closure.ZeroEvidence.SourceSHA256,
		"locator":                 "exact provider account object",
		"payload_sha256":          closure.ZeroEvidence.PayloadSHA256,
		"observed_on":             closure.ZeroEvidence.ObservedOn,
		"provider_status":         closure.ZeroEvidence.ProviderStatus,
		"current_balance_cents":   closure.ZeroEvidence.CurrentBalanceCents,
		"available_balance_cents": closure.ZeroEvidence.AvailableBalanceCents,
	}
	return map[string]any{
		"currency": closure.Currency, "id": closure.ID, "statement_account_id": closure.StatementAccountID,
		"statement_account":             closure.StatementAccount,
		"statement_account_identity_id": closure.StatementAccountIdentityID,
		"active_identity_count":         closure.ActiveIdentityCount,
		"active_identity_digest":        closure.ActiveIdentityDigest,
		"entity":                        closure.EntityCode, "book": closure.BookCode, "gl_account": closure.GLAccountCode,
		"reconciliation_required_from":    closure.ReconciliationRequiredFrom,
		"reconciliation_required_through": closure.ReconciliationRequiredThrough,
		"coverage_disposition":            closure.CoverageDisposition, "closed_on": closure.ClosedOn,
		"closure_evidence": closureEvidence, "zero_evidence": zeroEvidence,
		"account_holder": closure.AccountHolder, "account_suffix": closure.AccountSuffix,
		"reason": closure.Reason, "input_source_path": closure.InputSourcePath,
		"input_source_sha256":              closure.InputSourceSHA256,
		"control_balance_at_closure_cents": closure.ControlBalanceAtClosureCents,
		"current_control_balance_cents":    closure.CurrentControlBalanceCents,
		"post_closure_control_line_count":  closure.PostClosureControlLineCount,
		"draft_control_line_count":         closure.DraftControlLineCount,
		"status":                           closure.Status, "archived_at": closure.ArchivedAt, "archived_by": closure.ArchivedBy,
		"created_at": closure.CreatedAt, "created_by": closure.CreatedBy,
		"changed": closure.Changed, "committed": value.Committed, "dry_run": value.DryRun,
	}
}
