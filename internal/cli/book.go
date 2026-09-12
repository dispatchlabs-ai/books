package cli

import (
	"github.com/dispatchlabs-ai/books/internal/ledger"

	"github.com/spf13/cobra"
)

func newBookCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "book", Short: "Inspect actual and elimination books"}
	list := &cobra.Command{
		Use: "list", Short: "List books", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListBooks(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result))
			for _, book := range result {
				rows = append(rows, []string{book.Code, book.Name, book.Kind, book.EntityCode,
					book.GroupCode, book.AccountingBasis, book.Currency, book.Status, book.ID})
			}
			return writeResult(cmd, opts.format, result,
				[]string{"BOOK", "NAME", "KIND", "ENTITY", "GROUP", "BASIS", "CURRENCY", "STATUS", "ID"}, rows)
		},
	}
	command.AddCommand(list)
	return command
}
