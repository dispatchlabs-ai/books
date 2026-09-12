package cli

import (
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

func newEntityCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "entity", Short: "Manage legal entities and their actual books"}
	var code, name, currency, book, bookName, basis string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an entity and its actual book",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.CreateEntityInput{Code: code, LegalName: name, Currency: currency, BookCode: book, BookName: bookName, Basis: basis}
			if err := requireCommit(opts, "entity create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CreateEntity(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"ENTITY", "LEGAL NAME", "CURRENCY", "BOOK", "ID"}, [][]string{{result.Code, result.LegalName, result.FunctionalCurrency, result.BookCode, result.ID}})
		},
	}
	create.Flags().StringVar(&code, "code", "", "unique entity code")
	create.Flags().StringVar(&name, "name", "", "legal name")
	create.Flags().StringVar(&currency, "currency", "USD", "functional currency")
	create.Flags().StringVar(&book, "book", "", "actual book code (defaults to entity code)")
	create.Flags().StringVar(&bookName, "book-name", "", "actual book name")
	create.Flags().StringVar(&basis, "basis", "ACCRUAL", "accounting basis (currently ACCRUAL only)")
	list := &cobra.Command{
		Use:   "list",
		Short: "List legal entities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			entities, err := ledger.NewService(store, opts.actor).ListEntities(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(entities))
			for _, entity := range entities {
				rows = append(rows, []string{entity.Code, entity.LegalName, entity.FunctionalCurrency, entity.Status, entity.BookCode, entity.ID})
			}
			return writeResult(cmd, opts.format, entities, []string{"ENTITY", "LEGAL NAME", "CURRENCY", "STATUS", "BOOK", "ID"}, rows)
		},
	}
	command.AddCommand(create, list)
	return command
}

func newOwnershipCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "ownership", Short: "Manage effective-dated wholly owned relationships"}
	var parent, child, from, to string
	set := &cobra.Command{
		Use:   "set",
		Short: "Record 100% parent ownership",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "ownership set"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			id, err := ledger.NewService(store, opts.actor).AddOwnership(cmd.Context(), parent, child, from, to)
			if err != nil {
				return err
			}
			data := map[string]any{"id": id, "parent": strings.ToUpper(parent), "child": strings.ToUpper(child), "effective_from": from, "effective_to": to, "ownership_bps": 10000}
			return writeResult(cmd, opts.format, data, []string{"PARENT", "CHILD", "FROM", "TO", "OWNERSHIP", "ID"}, [][]string{{strings.ToUpper(parent), strings.ToUpper(child), from, to, "100.00%", id}})
		},
	}
	set.Flags().StringVar(&parent, "parent", "", "parent entity code")
	set.Flags().StringVar(&child, "child", "", "child entity code")
	set.Flags().StringVar(&from, "from", "", "effective date")
	set.Flags().StringVar(&to, "to", "", "optional end date")
	list := &cobra.Command{
		Use:   "list",
		Short: "List ownership history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			rowsDB, err := store.DB().QueryContext(cmd.Context(), `SELECT oi.id, p.code, c.code, oi.ownership_bps,
                oi.effective_from, COALESCE(oi.effective_to, '') FROM ownership_interests oi
                JOIN entities p ON p.id = oi.parent_entity_id JOIN entities c ON c.id = oi.child_entity_id
                ORDER BY oi.effective_from, p.code, c.code`)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)
			type record struct {
				ID, Parent, Child, From, To string
				OwnershipBPS                int `json:"ownership_bps"`
			}
			var data []record
			var rows [][]string
			for rowsDB.Next() {
				var r record
				if err := rowsDB.Scan(&r.ID, &r.Parent, &r.Child, &r.OwnershipBPS, &r.From, &r.To); err != nil {
					return err
				}
				data = append(data, r)
				rows = append(rows, []string{r.Parent, r.Child, r.From, r.To, fmt.Sprintf("%.2f%%", float64(r.OwnershipBPS)/100), r.ID})
			}
			return writeResult(cmd, opts.format, data, []string{"PARENT", "CHILD", "FROM", "TO", "OWNERSHIP", "ID"}, rows)
		},
	}
	command.AddCommand(set, list)
	return command
}
