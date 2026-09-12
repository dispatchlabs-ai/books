package cli

import (
	"context"
	"database/sql"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
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
	var query string
	switch kind {
	case "book":
		query = "SELECT currency FROM books WHERE code=?"
		key = strings.ToUpper(strings.TrimSpace(key))
	case "statement":
		query = "SELECT currency FROM statement_accounts WHERE code=?"
		key = strings.ToUpper(strings.TrimSpace(key))
	case "reconciliation":
		query = "SELECT sa.currency FROM reconciliations r JOIN statement_accounts sa ON sa.id=r.statement_account_id WHERE r.id=?"
	case "journal":
		query = "SELECT b.currency FROM journal_entries j JOIN books b ON b.id=j.book_id WHERE j.id=?"
	default:
		return money.Currency{}, apperr.New(apperr.Invalid, "CURRENCY_SCOPE_INVALID", "a money target is required")
	}
	var currency money.Currency
	err := store.DB().QueryRowContext(cmd.Context(), query, key).Scan(&currency)
	if err == sql.ErrNoRows {
		return currency, apperr.New(apperr.NotFound, "CURRENCY_TARGET_NOT_FOUND", "money target was not found")
	}
	if err != nil {
		return currency, storesqlite.MapError("read target currency", err)
	}
	setOutputCurrency(cmd, currency)
	return currency, nil
}

// Legacy USD plans retain their published schema and digest representation.
func planCurrency(code string) string {
	if code == "USD" {
		return ""
	}
	return code
}
func currencyPlanSchema(legacy, current, code string) string {
	if code == "USD" {
		return legacy
	}
	return current
}
func validPlanCurrency(schema, legacy, current, planned, actual string) bool {
	return (schema == legacy && planned == "" && actual == "USD") || (schema == current && planned == actual && planned != "")
}
