package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
)

type JournalAddRequest struct {
	Journal application.JournalInput `json:"journal"`
	Key     string                   `json:"key"`
	Draft   bool                     `json:"draft"`
	DryRun  bool                     `json:"dry_run"`
}
type CorrectionRequest struct {
	Number  int64                    `json:"number"`
	Journal application.JournalInput `json:"journal"`
	Reason  string                   `json:"reason"`
	Draft   bool                     `json:"draft"`
	DryRun  bool                     `json:"dry_run"`
}
type DefaultRequest struct {
	Key     string `json:"key"`
	Account string `json:"account"`
}

func extraCompanyOperations() []CompanyOperation {
	return []CompanyOperation{
		companyOp("journal_add", "post", "write", func(c context.Context, s *application.Service, a Access, r JournalAddRequest) (application.Transaction, error) {
			if err := remoteKey(a, r.Key); err != nil {
				return application.Transaction{}, err
			}
			if err := application.PrepareManualJournal(&r.Journal, r.Key); err != nil {
				return application.Transaction{}, err
			}
			return s.AddJournal(c, r.Journal, r.Draft, r.DryRun)
		}),
		companyOp("correct", "post", "write", func(c context.Context, s *application.Service, a Access, r CorrectionRequest) (application.Correction, error) {
			if err := application.PrepareManualJournal(&r.Journal, ""); err != nil {
				return application.Correction{}, err
			}
			return s.CorrectTransaction(c, r.Number, r.Journal, r.Reason, r.Draft, r.DryRun, allowedJournalKinds(a)...)
		}),
		companyOp("account_defaults_set", "manage", "write", func(c context.Context, s *application.Service, a Access, r DefaultRequest) (application.AccountDefaults, error) {
			if _, err := s.SetAccountDefault(c, r.Key, r.Account); err != nil {
				return application.AccountDefaults{}, err
			}
			return s.AccountDefaults()
		}),
	}
}
