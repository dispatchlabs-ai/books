package cli

import (
	"encoding/json"
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
			rowsDB, err := store.DB().QueryContext(cmd.Context(), `SELECT sequence, event_id, occurred_at, actor,
                app_version, command, aggregate_type, aggregate_id, payload_json, previous_hash, event_hash
                FROM audit_events ORDER BY sequence DESC LIMIT ?`, limit)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)
			type item struct {
				Sequence      int64           `json:"sequence"`
				EventID       string          `json:"event_id"`
				OccurredAt    string          `json:"occurred_at"`
				Actor         string          `json:"actor"`
				AppVersion    string          `json:"app_version"`
				Command       string          `json:"command"`
				AggregateType string          `json:"aggregate_type"`
				AggregateID   string          `json:"aggregate_id"`
				Payload       json.RawMessage `json:"payload"`
				PreviousHash  string          `json:"previous_hash"`
				EventHash     string          `json:"event_hash"`
			}
			var data []item
			var rows [][]string
			for rowsDB.Next() {
				var value item
				var payload string
				if err := rowsDB.Scan(&value.Sequence, &value.EventID, &value.OccurredAt, &value.Actor, &value.AppVersion, &value.Command, &value.AggregateType, &value.AggregateID, &payload, &value.PreviousHash, &value.EventHash); err != nil {
					return err
				}
				value.Payload = json.RawMessage(payload)
				data = append(data, value)
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
