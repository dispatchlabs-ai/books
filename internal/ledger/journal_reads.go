package ledger

import (
	"context"
	"database/sql"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

// Company journal reads hold one snapshot across authorization and the result.
// Draft edits in another connection cannot change the target between those reads.
func (s *Service) GetBookJournal(ctx context.Context, book, id string) (Journal, error) {
	tx, err := s.store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Journal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = checkJournalBook(ctx, tx, book, id); err != nil {
		return Journal{}, err
	}
	return getJournal(ctx, tx, id)
}

func (s *Service) ValidateBookJournal(ctx context.Context, book, id string) (JournalValidation, error) {
	tx, err := s.store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return JournalValidation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = checkJournalBook(ctx, tx, book, id); err != nil {
		return JournalValidation{}, err
	}
	journal, err := getJournal(ctx, tx, id)
	if err != nil {
		return JournalValidation{}, err
	}
	if journal.ReversalOfID != "" {
		if err := checkJournalBook(ctx, tx, book, journal.ReversalOfID); err != nil {
			if e, ok := apperr.As(err); ok && e.Code == "JOURNAL_NOT_FOUND" {
				return JournalValidation{JournalID: id, Currency: journal.Currency, Valid: false, Errors: []string{"reversal target must belong to the same book"}, DebitCents: journal.TotalDebitCents, CreditCents: journal.TotalCreditCents}, nil
			}
			return JournalValidation{}, err
		}
	}
	return validateJournalQuery(ctx, tx, id)
}

func checkJournalBook(ctx context.Context, q queryer, book, id string) error {
	var found int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM journal_entries j JOIN books b ON b.id=j.book_id WHERE j.id=? AND b.code=?`, id, book).Scan(&found)
	if err == sql.ErrNoRows {
		return apperr.New(apperr.NotFound, "JOURNAL_NOT_FOUND", "journal was not found")
	}
	return err
}
