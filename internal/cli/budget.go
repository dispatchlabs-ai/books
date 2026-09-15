package cli

import (
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/spf13/cobra"
)

func newBudgetCommand(opts *options) *cobra.Command {
	root := &cobra.Command{Use: "budget", Short: "Manage monthly spending buckets and compare two complete months"}
	var asOf, input string
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		app, e := openWorkflow(cmd, opts, false)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		r, e := app.Budget(cmd.Context(), application.BudgetRequest{AsOf: asOf})
		if e != nil {
			return e
		}
		return writeResult(cmd, opts.format, r, []string{"from", "to"}, [][]string{{r.From, r.To}})
	}}
	show.Flags().StringVar(&asOf, "as-of", "", "date selecting the previous two complete months")
	_ = show.MarkFlagRequired("as-of")
	save := &cobra.Command{Use: "save", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if e := requireCommit(opts, "budget save"); e != nil {
			return e
		}
		var r application.BudgetSaveRequest
		if e := readJSONInput(input, &r); e != nil {
			return e
		}
		app, e := openWorkflow(cmd, opts, true)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		out, e := app.SaveBudget(cmd.Context(), r)
		if e != nil {
			return e
		}
		return writeResult(cmd, opts.format, out, []string{"revision"}, [][]string{{out.Plan.Revision}})
	}}
	save.Flags().StringVarP(&input, "input", "i", "", "JSON with as_of and plan, including expected revision")
	_ = save.MarkFlagRequired("input")
	root.AddCommand(show, save)
	return root
}
