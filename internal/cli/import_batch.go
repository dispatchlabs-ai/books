package cli

import (
	"fmt"

	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

var importBatchHeaders = []string{
	"ID", "SOURCE", "ENTITY", "NAME", "FILE SHA256", "STATUS", "DECLARED", "PRESERVED",
	"POSTED SOURCE", "PENDING", "NEEDS REVIEW", "SOURCE ONLY", "JOURNAL LINKS", "JOURNALS",
	"DRAFT", "POSTED", "ABANDONED", "CREATED", "COMPLETED",
}

func importBatchRow(value ledger.ImportBatch) []string {
	return []string{
		value.ID, value.SourceSystem, value.EntityCode, value.SourceName, value.FileSHA256, value.Status,
		fmt.Sprint(value.RecordCount), fmt.Sprint(value.SourceRecordCount), fmt.Sprint(value.PostedSourceCount),
		fmt.Sprint(value.PendingSourceCount), fmt.Sprint(value.ReviewSourceCount), fmt.Sprint(value.SourceOnlyCount),
		fmt.Sprint(value.JournalLinkCount), fmt.Sprint(value.JournalCount), fmt.Sprint(value.DraftJournalCount),
		fmt.Sprint(value.PostedJournalCount), fmt.Sprint(value.AbandonedJournalCount), value.CreatedAt, value.CompletedAt,
	}
}

func newImportBatchCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "import-batch", Short: "Inspect immutable import batch metadata"}
	var sourceSystem, entity, status string
	list := &cobra.Command{
		Use: "list", Short: "List import batches", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListImportBatches(cmd.Context(), ledger.ImportBatchFilter{
				SourceSystem: sourceSystem, Entity: entity, Status: status,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result))
			for _, batch := range result {
				rows = append(rows, importBatchRow(batch))
			}
			return writeResult(cmd, opts.format, result, importBatchHeaders, rows)
		},
	}
	list.Flags().StringVar(&sourceSystem, "source-system", "", "optional source system")
	list.Flags().StringVar(&entity, "entity", "", "optional entity code")
	list.Flags().StringVar(&status, "status", "", "optional STAGED, COMPLETED, or FAILED")
	var id string
	show := &cobra.Command{
		Use: "show", Short: "Show one import batch and its preservation counts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).GetImportBatch(cmd.Context(), id)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, importBatchHeaders, [][]string{importBatchRow(result)})
		},
	}
	show.Flags().StringVar(&id, "id", "", "import batch id")
	command.AddCommand(list, show)
	return command
}
