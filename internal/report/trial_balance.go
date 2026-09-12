package report

import (
	"context"
	"fmt"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

// TrialBalance returns account balances through a date. Balance breakdowns use
// signed debit-minus-credit cents; row debit and credit columns are the
// conventional presentation of the consolidated signed balance.
func (s *Service) TrialBalance(ctx context.Context, input TrialBalanceInput) (TrialBalanceReport, error) {
	return withReportSnapshot(ctx, s, func(snapshot *Service) (TrialBalanceReport, error) {
		return snapshot.trialBalance(ctx, input)
	})
}

func (s *Service) trialBalance(ctx context.Context, input TrialBalanceInput) (TrialBalanceReport, error) {
	if err := validateDate(input.AsOfDate, "as-of date"); err != nil {
		return TrialBalanceReport{}, err
	}
	scope, err := s.ResolveScope(ctx, input.Scope, input.AsOfDate)
	if err != nil {
		return TrialBalanceReport{}, err
	}
	if err := s.verifyPostedJournalIntegrity(ctx, scope, input.AsOfDate); err != nil {
		return TrialBalanceReport{}, err
	}
	lines, err := s.loadPostedLines(ctx, scope, "", input.AsOfDate, "")
	if err != nil {
		return TrialBalanceReport{}, err
	}
	balances, err := aggregateBalances(lines)
	if err != nil {
		return TrialBalanceReport{}, err
	}
	if input.IncludeZero {
		catalog, err := s.loadAccountCatalog(ctx, scope)
		if err != nil {
			return TrialBalanceReport{}, err
		}
		includeCatalogAccounts(balances, catalog)
	}

	controlBooks, err := totalBooks(balances, "ASSET", "LIABILITY", "EQUITY", "REVENUE", "EXPENSE")
	if err != nil {
		return TrialBalanceReport{}, err
	}
	control, err := breakdown(scope, controlBooks)
	if err != nil {
		return TrialBalanceReport{}, err
	}
	if !componentBalancesZero(control) {
		return TrialBalanceReport{}, apperr.New(
			apperr.Integrity,
			"TRIAL_BALANCE_OUT_OF_BALANCE",
			fmt.Sprintf("trial balance is out of balance by %d consolidated cents (eliminations %d cents)", control.ConsolidatedCents, control.EliminationsCents),
		)
	}

	report := TrialBalanceReport{Scope: scope, AsOfDate: input.AsOfDate}
	for _, balance := range sortedBalances(balances) {
		amount, err := breakdown(scope, balance.ByBook)
		if err != nil {
			return TrialBalanceReport{}, err
		}
		if !input.IncludeZero && !anyBookAmount(balance.ByBook) {
			continue
		}
		row := TrialBalanceRow{Account: balance.Account, Balance: amount}
		if amount.ConsolidatedCents >= 0 {
			row.DebitCents = amount.ConsolidatedCents
			report.TotalDebitCents, err = checkedAdd(report.TotalDebitCents, row.DebitCents)
			if err != nil {
				return TrialBalanceReport{}, err
			}
		} else {
			row.CreditCents, err = checkedNegate(amount.ConsolidatedCents)
			if err != nil {
				return TrialBalanceReport{}, err
			}
			report.TotalCreditCents, err = checkedAdd(report.TotalCreditCents, row.CreditCents)
			if err != nil {
				return TrialBalanceReport{}, err
			}
		}
		report.Rows = append(report.Rows, row)
	}
	if report.TotalDebitCents != report.TotalCreditCents {
		return TrialBalanceReport{}, apperr.New(apperr.Integrity, "TRIAL_BALANCE_OUT_OF_BALANCE", "trial balance debit and credit columns do not balance")
	}
	return report, nil
}
