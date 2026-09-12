package cli

import (
	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

func newGroupCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "group", Short: "Manage ownership-derived consolidation groups"}
	var code, name, parent, eliminationBook, eliminationName string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a consolidation group and elimination book",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.CreateGroupInput{Code: code, Name: name, ParentEntity: parent, EliminationBookCode: eliminationBook, EliminationBookName: eliminationName}
			if err := requireCommit(opts, "group create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CreateGroup(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"GROUP", "NAME", "CURRENCY", "ELIMINATION BOOK", "ID"}, [][]string{{result.Code, result.Name, result.Currency, result.EliminationBookCode, result.ID}})
		},
	}
	create.Flags().StringVar(&code, "code", "", "group code")
	create.Flags().StringVar(&name, "name", "", "group name")
	create.Flags().StringVar(&parent, "parent", "", "parent entity code")
	create.Flags().StringVar(&eliminationBook, "elimination-book", "", "elimination book code")
	create.Flags().StringVar(&eliminationName, "elimination-name", "", "elimination book name")
	list := &cobra.Command{
		Use: "list", Short: "List consolidation groups", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			rowsDB, err := store.DB().QueryContext(cmd.Context(), `SELECT g.id, g.code, g.name, e.code, g.currency,
                COALESCE(b.id, ''), COALESCE(b.code, '') FROM consolidation_groups g
                JOIN entities e ON e.id = g.parent_entity_id
                LEFT JOIN books b ON b.group_id = g.id AND b.kind = 'ELIMINATION' AND b.status = 'ACTIVE'
                ORDER BY g.code`)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)
			type item struct {
				ID                string `json:"id"`
				Code              string `json:"code"`
				Name              string `json:"name"`
				Parent            string `json:"parent"`
				Currency          string `json:"currency"`
				EliminationBookID string `json:"elimination_book_id"`
				EliminationBook   string `json:"elimination_book"`
			}
			var data []item
			var rows [][]string
			for rowsDB.Next() {
				var value item
				if err := rowsDB.Scan(&value.ID, &value.Code, &value.Name, &value.Parent, &value.Currency, &value.EliminationBookID, &value.EliminationBook); err != nil {
					return err
				}
				data = append(data, value)
				rows = append(rows, []string{value.Code, value.Name, value.Parent, value.Currency, value.EliminationBook, value.ID})
			}
			return writeResult(cmd, opts.format, data, []string{"GROUP", "NAME", "PARENT", "CURRENCY", "ELIMINATION BOOK", "ID"}, rows)
		},
	}
	command.AddCommand(create, list)
	return command
}
