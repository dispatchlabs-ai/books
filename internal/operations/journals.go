package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

func JournalShow() TypedOperation[application.JournalReadRequest, ledger.Journal] {
	return companyRead("journal_show", func(ctx context.Context, app *application.Service, in application.JournalReadRequest) (ledger.Journal, error) {
		return app.Journal(ctx, in)
	})
}
func JournalValidate() TypedOperation[application.JournalReadRequest, ledger.JournalValidation] {
	return companyRead("journal_validate", func(ctx context.Context, app *application.Service, in application.JournalReadRequest) (ledger.JournalValidation, error) {
		return app.ValidateJournal(ctx, in)
	})
}
