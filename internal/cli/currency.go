package cli

import (
	"context"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/spf13/cobra"
)

type currencyContextKey struct{}

func setOutputCurrency(cmd *cobra.Command, currency money.Currency) {
	cmd.SetContext(context.WithValue(cmd.Context(), currencyContextKey{}, currency))
}
func outputCurrency(cmd *cobra.Command) money.Currency {
	currency, _ := cmd.Context().Value(currencyContextKey{}).(money.Currency)
	return currency
}

// Resolve money at the authoritative target, including advanced --db commands
// that may select a different book from the registered company's default.
func targetCurrency(cmd *cobra.Command, store *storesqlite.Store, kind, key string) (money.Currency, error) {
	currency, err := ledger.NewService(store, "").TargetCurrency(cmd.Context(), kind, key)
	if err == nil {
		setOutputCurrency(cmd, currency)
	}
	return currency, err
}
