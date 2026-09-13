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

// AsOfReportRequest and RangeReportRequest are shared company report inputs.
// Scope is supplied by the bound application, not by an adapter's request body.
type AsOfReportRequest struct {
	AsOf        string `json:"as_of"`
	IncludeZero bool   `json:"include_zero"`
}
type RangeReportRequest struct {
	From        string `json:"from"`
	To          string `json:"to"`
	IncludeZero bool   `json:"include_zero"`
}

func (s *Service) TrialBalanceWithOptions(ctx context.Context, input AsOfReportRequest) (report.TrialBalanceReport, error) {
	return report.NewService(s.store).TrialBalance(ctx, report.TrialBalanceInput{Scope: report.Scope{EntityCode: s.company.Entity}, AsOfDate: input.AsOf, IncludeZero: input.IncludeZero})
}
func (s *Service) BalanceSheetWithOptions(ctx context.Context, input AsOfReportRequest) (report.BalanceSheetReport, error) {
	return report.NewService(s.store).BalanceSheet(ctx, report.BalanceSheetInput{Scope: report.Scope{EntityCode: s.company.Entity}, AsOfDate: input.AsOf, IncludeZero: input.IncludeZero})
}
func (s *Service) ProfitLossWithOptions(ctx context.Context, input RangeReportRequest) (report.ProfitLossReport, error) {
	return report.NewService(s.store).ProfitLoss(ctx, report.ProfitLossInput{Scope: report.Scope{EntityCode: s.company.Entity}, FromDate: input.From, ToDate: input.To, IncludeZero: input.IncludeZero})
}
