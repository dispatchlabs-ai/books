package cli

import (
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	"github.com/spf13/cobra"
)

func newCashForecastCommand(opts *options) *cobra.Command {
	var input string
	cmd := &cobra.Command{Use: "cash-forecast", Short: "Project every day by account from an explicit dated scenario", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var plan cashflow.Plan
		if err := readJSONInput(input, &plan); err != nil {
			return err
		}
		app, err := openWorkflow(cmd, opts, false)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		result, err := app.CashForecast(cmd.Context(), plan)
		if err != nil {
			return err
		}
		rows := [][]string{}
		for _, d := range result.Days {
			rows = append(rows, []string{d.Date, d.Account, fmt.Sprint(d.Opening), fmt.Sprint(d.Inflow), fmt.Sprint(d.Outflow), fmt.Sprint(d.Closing), fmt.Sprint(d.Shortfall)})
		}
		return writeResult(cmd, opts.format, result, []string{"date", "account", "opening_minor", "inflow_minor", "outflow_minor", "closing_minor", "shortfall_minor"}, rows)
	}}
	cmd.Flags().StringVarP(&input, "input", "i", "", "dated cash-plan JSON file")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
