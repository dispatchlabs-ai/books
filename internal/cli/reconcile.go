package cli

import (
	"fmt"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
)

func reconciliationRows(value ledger.Reconciliation) [][]string {
	return [][]string{{value.ID, value.StatementAccountCode, value.StartDate, value.EndDate,
		money.Format(value.BeginningBalanceCents), money.Format(value.LedgerBeginningCents), money.Format(value.OpeningOutstandingCents), money.Format(value.BeginningDifferenceCents),
		money.Format(value.StatementActivityCents), money.Format(value.EndingBalanceCents), money.Format(value.CalculatedEndingCents),
		money.Format(value.StatementDifferenceCents), money.Format(value.LedgerEndingCents), money.Format(value.EndingOutstandingCents), money.Format(value.LedgerDifferenceCents),
		fmt.Sprint(value.OutstandingLineCount), fmt.Sprint(value.OutstandingMismatchCount),
		fmt.Sprintf("%d/%d", value.FullyAllocatedStatementCount, value.StatementTransactionCount),
		fmt.Sprintf("%d/%d", value.FullyAllocatedControlLineCount, value.ControlLineCount),
		fmt.Sprint(value.AllocationCount), value.Status, value.AbandonedAt, value.AbandonedBy, value.AbandonReason}}
}

type reconciliationAbandonOutput struct {
	Reconciliation ledger.Reconciliation `json:"reconciliation"`
	Committed      bool                  `json:"committed"`
	DryRun         bool                  `json:"dry_run"`
}

var reconciliationHeaders = []string{"ID", "ACCOUNT", "START", "END", "STATEMENT BEGINNING", "BOOK BEGINNING", "OPENING OUTSTANDING", "BEGINNING DIFF", "ACTIVITY", "STATEMENT ENDING", "CALCULATED ENDING", "STATEMENT DIFF", "BOOK ENDING", "ENDING OUTSTANDING", "BOOK DIFF", "OUTSTANDING ITEMS", "OUTSTANDING MISMATCHES", "STATEMENT ALLOCATED", "GL LINES ALLOCATED", "ALLOCATIONS", "STATUS", "ABANDONED AT", "ABANDONED BY", "ABANDON REASON"}

func newReconcileCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "reconcile", Short: "Allocate statement activity to posted GL control-account lines"}
	var listAccount, listStatus, listFrom, listTo string
	list := &cobra.Command{
		Use: "list", Short: "List reconciliations and current differences", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListReconciliations(cmd.Context(), ledger.ReconciliationFilter{
				StatementAccount: listAccount, Status: listStatus, FromDate: listFrom, ToDate: listTo,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result))
			for _, value := range result {
				rows = append(rows, reconciliationRows(value)[0])
			}
			return writeResult(cmd, opts.format, result, reconciliationHeaders, rows)
		},
	}
	list.Flags().StringVar(&listAccount, "account", "", "optional statement-account code")
	list.Flags().StringVar(&listStatus, "status", "", "optional OPEN, COMPLETED, or ABANDONED")
	list.Flags().StringVar(&listFrom, "from", "", "optional earliest reconciliation start date")
	list.Flags().StringVar(&listTo, "to", "", "optional latest reconciliation end date")
	var account, startDate, endDate, beginningText, endingText string
	start := &cobra.Command{
		Use: "start", Short: "Start a non-overlapping reconciliation period", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "reconcile start"); err != nil {
				return err
			}
			beginning, err := money.Parse(beginningText)
			if err != nil {
				return apperr.Wrap(apperr.Invalid, "BALANCE_INVALID", "invalid beginning balance", err)
			}
			ending, err := money.Parse(endingText)
			if err != nil {
				return apperr.Wrap(apperr.Invalid, "BALANCE_INVALID", "invalid ending balance", err)
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).StartReconciliation(cmd.Context(), account, startDate, endDate, beginning, ending)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, reconciliationHeaders, reconciliationRows(result))
		},
	}
	start.Flags().StringVar(&account, "account", "", "statement-account code")
	start.Flags().StringVar(&startDate, "start", "", "statement start date")
	start.Flags().StringVar(&endDate, "end", "", "statement end date")
	start.Flags().StringVar(&beginningText, "beginning", "", "signed GL beginning balance")
	start.Flags().StringVar(&endingText, "ending", "", "signed GL ending balance")
	var reconciliationID, transactionID, lineID, allocationText string
	allocate := &cobra.Command{
		Use: "allocate", Short: "Allocate a signed amount between statement activity and a posted control-account line", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "reconcile allocate"); err != nil {
				return err
			}
			allocation, err := money.Parse(allocationText)
			if err != nil {
				return apperr.Wrap(apperr.Invalid, "ALLOCATION_AMOUNT_INVALID", "invalid allocation amount", err)
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			id, err := ledger.NewService(store, opts.actor).AllocateReconciliation(cmd.Context(), reconciliationID, transactionID, lineID, allocation)
			if err != nil {
				return err
			}
			data := map[string]any{"id": id, "reconciliation_id": reconciliationID, "statement_transaction_id": transactionID, "journal_line_id": lineID, "allocated_amount_cents": allocation}
			return writeResult(cmd, opts.format, data, []string{"ALLOCATION", "RECONCILIATION", "TRANSACTION", "JOURNAL LINE", "AMOUNT"}, [][]string{{id, reconciliationID, transactionID, lineID, money.Format(allocation)}})
		},
	}
	allocate.Flags().StringVar(&reconciliationID, "reconciliation", "", "reconciliation id")
	allocate.Flags().StringVar(&transactionID, "transaction", "", "statement transaction id")
	allocate.Flags().StringVar(&lineID, "journal-line", "", "posted control-account journal line id")
	allocate.Flags().StringVar(&allocationText, "amount", "", "signed allocation amount")
	var allocationID string
	unallocate := &cobra.Command{
		Use: "unallocate", Short: "Remove an allocation from an open reconciliation", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "reconcile unallocate"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).RemoveReconciliationAllocation(cmd.Context(), allocationID); err != nil {
				return err
			}
			return writeResult(cmd, opts.format, map[string]any{"id": allocationID, "removed": true}, []string{"ALLOCATION", "REMOVED"}, [][]string{{allocationID, "true"}})
		},
	}
	unallocate.Flags().StringVar(&allocationID, "id", "", "reconciliation allocation id")
	allocations := &cobra.Command{
		Use: "allocations", Short: "List a reconciliation's signed allocations", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListReconciliationAllocations(cmd.Context(), reconciliationID)
			if err != nil {
				return err
			}
			var rows [][]string
			for _, allocation := range result {
				rows = append(rows, []string{allocation.ID, allocation.StatementPostedDate, allocation.StatementExternalID,
					allocation.StatementTransactionID, fmt.Sprint(allocation.JournalEntryNumber), fmt.Sprint(allocation.JournalLineNumber),
					allocation.JournalLineID, money.Format(allocation.AllocatedAmountCents)})
			}
			return writeResult(cmd, opts.format, result,
				[]string{"ID", "STATEMENT DATE", "STATEMENT EXTERNAL ID", "TRANSACTION", "JOURNAL", "LINE", "JOURNAL LINE ID", "AMOUNT"}, rows)
		},
	}
	allocations.Flags().StringVar(&reconciliationID, "id", "", "reconciliation id")
	status := &cobra.Command{
		Use: "status", Short: "Show reconciliation differences and allocation coverage", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ReconciliationStatus(cmd.Context(), reconciliationID)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, reconciliationHeaders, reconciliationRows(result))
		},
	}
	status.Flags().StringVar(&reconciliationID, "id", "", "reconciliation id")
	complete := &cobra.Command{
		Use: "complete", Short: "Complete only when source activity, allocations, and ledger balances are exact", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var storeMode = openWrite
			if opts.dryRun {
				storeMode = openRead
			}
			store, err := storeMode(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CompleteReconciliation(cmd.Context(), reconciliationID, opts.dryRun)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, reconciliationHeaders, reconciliationRows(result))
		},
	}
	complete.Flags().StringVar(&reconciliationID, "id", "", "reconciliation id")
	var abandonReason string
	var commitAbandon bool
	abandon := &cobra.Command{
		Use: "abandon", Short: "Abandon mistaken open reconciliation work with an audit reason", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			preview := !commitAbandon || opts.dryRun
			storeMode := openWrite
			if preview {
				storeMode = openRead
			}
			store, err := storeMode(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).AbandonReconciliation(cmd.Context(), reconciliationID, abandonReason, preview)
			if err != nil {
				return err
			}
			output := reconciliationAbandonOutput{Reconciliation: result, Committed: !preview, DryRun: opts.dryRun}
			headers := append(append([]string{}, reconciliationHeaders...), "COMMITTED", "DRY RUN")
			rows := reconciliationRows(result)
			rows[0] = append(rows[0], fmt.Sprint(output.Committed), fmt.Sprint(output.DryRun))
			return writeResult(cmd, opts.format, output, headers, rows)
		},
	}
	abandon.Flags().StringVar(&reconciliationID, "id", "", "open reconciliation id")
	abandon.Flags().StringVar(&abandonReason, "reason", "", "required audit reason")
	abandon.Flags().BoolVar(&commitAbandon, "commit", false, "commit the abandonment; otherwise validate and preview it")
	var reopenReason string
	reopen := &cobra.Command{
		Use: "reopen", Short: "Reopen a completed reconciliation with an audit reason", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "reconcile reopen"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).ReopenReconciliation(cmd.Context(), reconciliationID, reopenReason); err != nil {
				return err
			}
			return writeResult(cmd, opts.format, map[string]any{"id": reconciliationID, "status": "OPEN", "reason": reopenReason}, []string{"ID", "STATUS", "REASON"}, [][]string{{reconciliationID, "OPEN", reopenReason}})
		},
	}
	reopen.Flags().StringVar(&reconciliationID, "id", "", "reconciliation id")
	reopen.Flags().StringVar(&reopenReason, "reason", "", "required audit reason")
	command.AddCommand(newManualReconciliationPlanCommand(opts), newManualReconciliationReplanCommand(opts), newManualReconciliationApplyCommand(opts), list, start, allocate, unallocate, allocations, status, complete, abandon, reopen)
	return command
}
