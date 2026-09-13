package application

import "github.com/dispatchlabs-ai/books/internal/apperr"

func PrepareManualJournal(input *JournalInput, key string) error {
	if input.SourceSystem != "" || input.SourceKey != "" {
		return apperr.New(apperr.Invalid, "JOURNAL_SOURCE_RESERVED", "source identity is assigned by the workflow")
	}
	if input.Kind != "" && input.Kind != "STANDARD" {
		return apperr.New(apperr.Invalid, "JOURNAL_KIND_INVALID", "use year-close workflows for closing journals")
	}
	if key != "" {
		input.SourceSystem = "MANUAL"
		input.SourceKey = key
	}
	return nil
}
