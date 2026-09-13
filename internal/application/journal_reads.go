package application

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

type JournalReadRequest struct {
	ID string `json:"id"`
}

func (s *Service) Journal(ctx context.Context, input JournalReadRequest) (ledger.Journal, error) {
	return s.ledger().GetBookJournal(ctx, s.company.Book, input.ID)
}
func (s *Service) ValidateJournal(ctx context.Context, input JournalReadRequest) (ledger.JournalValidation, error) {
	return s.ledger().ValidateBookJournal(ctx, s.company.Book, input.ID)
}
