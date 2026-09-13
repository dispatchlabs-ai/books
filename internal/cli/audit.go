package cli

import (
	"fmt"

	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

func newAuditCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "audit", Short: "Inspect and verify the hash-chained mutation log"}
	var limit int
	list := &cobra.Command{
		Use: "list", Short: "List audit events", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			data, err := store.ListAudit(cmd.Context(), limit)
			if err != nil {
				return err
			}
			var rows [][]string
			for _, value := range data {
				rows = append(rows, []string{fmt.Sprint(value.Sequence), value.OccurredAt, value.Actor, value.Command, value.AggregateType, value.AggregateID, value.EventHash})
			}

			return writeResult(cmd, opts.format, data, []string{"SEQUENCE", "OCCURRED", "ACTOR", "COMMAND", "TYPE", "AGGREGATE", "HASH"}, rows)
		},
	}
	list.Flags().IntVar(&limit, "limit", 100, "maximum events")
	verify := &cobra.Command{
		Use: "verify", Short: "Verify every audit hash and link", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			count, err := storesqlite.VerifyAudit(cmd.Context(), store.DB())
			if err != nil {
				return err
			}
			data := map[string]any{"valid": true, "event_count": count}
			return writeResult(cmd, opts.format, data, []string{"VALID", "EVENTS"}, [][]string{{"true", fmt.Sprint(count)}})
		},
	}
	command.AddCommand(list, verify)
	return command
}
