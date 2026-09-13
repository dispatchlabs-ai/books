package application

import (
	"context"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/report"
)

// GeneralLedgerRequest contains company-local report choices. Entity and group
// scope come from the bound service, never from client-controlled selectors.
type GeneralLedgerRequest struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Account     string `json:"account,omitempty"`
	IncludeZero bool   `json:"include_zero"`
}

func (s *Service) GeneralLedger(ctx context.Context, input GeneralLedgerRequest) (report.GeneralLedgerReport, error) {
	code := ""
	if strings.TrimSpace(input.Account) != "" {
		accounts, err := s.Accounts(ctx)
		if err != nil {
			return report.GeneralLedgerReport{}, err
		}
		account, err := ResolveAccount(accounts, input.Account)
		if err != nil {
			return report.GeneralLedgerReport{}, err
		}
		code = account.Code
	}
	return report.NewService(s.store).GeneralLedger(ctx, report.GeneralLedgerInput{
		Scope: report.Scope{EntityCode: s.company.Entity}, FromDate: input.From,
		ToDate: input.To, AccountCode: code, IncludeZero: input.IncludeZero,
	})
}
