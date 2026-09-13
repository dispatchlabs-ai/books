package ledger

import (
	"context"
	"database/sql"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"strings"
)

func (s *Service) TargetCurrency(ctx context.Context, kind, key string) (money.Currency, error) {
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
	err := s.store.DB().QueryRowContext(ctx, query, key).Scan(&currency)
	if err == sql.ErrNoRows {
		return currency, apperr.New(apperr.NotFound, "CURRENCY_TARGET_NOT_FOUND", "money target was not found")
	}
	if err != nil {
		return currency, storesqlite.MapError("read target currency", err)
	}
	return currency, nil
}
