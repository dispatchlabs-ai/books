package report

import (
	"context"
	"fmt"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

// BalanceSheet returns posted balances through a date. Assets use debit-natural
// signs; liabilities and equity use credit-natural signs. Unclosed cumulative
// revenue less expense is presented as current earnings within total equity.
func (s *Service) BalanceSheet(ctx context.Context, input BalanceSheetInput) (BalanceSheetReport, error) {
	return withReportSnapshot(ctx, s, func(snapshot *Service) (BalanceSheetReport, error) {
		return snapshot.balanceSheet(ctx, input)
	})
}

func (s *Service) balanceSheet(ctx context.Context, input BalanceSheetInput) (BalanceSheetReport, error) {
	if err := validateDate(input.AsOfDate, "as-of date"); err != nil {
		return BalanceSheetReport{}, err
	}
	scope, err := s.ResolveScope(ctx, input.Scope, input.AsOfDate)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	if err := s.verifyPostedJournalIntegrity(ctx, scope, input.AsOfDate); err != nil {
		return BalanceSheetReport{}, err
	}
	lines, err := s.loadPostedLines(ctx, scope, "", input.AsOfDate, "")
	if err != nil {
		return BalanceSheetReport{}, err
	}
	balances, err := aggregateBalances(lines)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	if input.IncludeZero {
		catalog, err := s.loadAccountCatalog(ctx, scope)
		if err != nil {
			return BalanceSheetReport{}, err
		}
		includeCatalogAccounts(balances, catalog)
	}

	assetBooks := centsByBook{}
	liabilityBooks := centsByBook{}
	equityBooks := centsByBook{}
	report := BalanceSheetReport{Scope: scope, AsOfDate: input.AsOfDate}
	for _, balance := range sortedBalances(balances) {
		if balance.Account.Type != "ASSET" && balance.Account.Type != "LIABILITY" && balance.Account.Type != "EQUITY" {
			continue
		}
		if !input.IncludeZero && !anyBookAmount(balance.ByBook) {
			continue
		}
		switch balance.Account.Type {
		case "ASSET":
			natural, err := scaleCents(balance.ByBook, 1)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			if err := addCents(assetBooks, natural, 1); err != nil {
				return BalanceSheetReport{}, err
			}
			amount, err := breakdown(scope, natural)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			report.Assets = append(report.Assets, StatementRow{Account: balance.Account, Amount: amount})
		case "LIABILITY":
			natural, err := scaleCents(balance.ByBook, -1)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			if err := addCents(liabilityBooks, natural, 1); err != nil {
				return BalanceSheetReport{}, err
			}
			amount, err := breakdown(scope, natural)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			report.Liabilities = append(report.Liabilities, StatementRow{Account: balance.Account, Amount: amount})
		case "EQUITY":
			natural, err := scaleCents(balance.ByBook, -1)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			if err := addCents(equityBooks, natural, 1); err != nil {
				return BalanceSheetReport{}, err
			}
			amount, err := breakdown(scope, natural)
			if err != nil {
				return BalanceSheetReport{}, err
			}
			report.Equity = append(report.Equity, StatementRow{Account: balance.Account, Amount: amount})
		}
	}

	profitLossBooks, err := totalBooks(balances, "REVENUE", "EXPENSE")
	if err != nil {
		return BalanceSheetReport{}, err
	}
	currentEarningsBooks, err := scaleCents(profitLossBooks, -1)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	totalEquityBooks := centsByBook{}
	if err := addCents(totalEquityBooks, equityBooks, 1); err != nil {
		return BalanceSheetReport{}, err
	}
	if err := addCents(totalEquityBooks, currentEarningsBooks, 1); err != nil {
		return BalanceSheetReport{}, err
	}
	report.TotalAssets, err = breakdown(scope, assetBooks)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	report.TotalLiabilities, err = breakdown(scope, liabilityBooks)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	report.PostedEquity, err = breakdown(scope, equityBooks)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	report.CurrentEarnings, err = breakdown(scope, currentEarningsBooks)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	report.TotalEquity, err = breakdown(scope, totalEquityBooks)
	if err != nil {
		return BalanceSheetReport{}, err
	}

	equationDifference := centsByBook{}
	if err := addCents(equationDifference, assetBooks, 1); err != nil {
		return BalanceSheetReport{}, err
	}
	if err := addCents(equationDifference, liabilityBooks, -1); err != nil {
		return BalanceSheetReport{}, err
	}
	if err := addCents(equationDifference, totalEquityBooks, -1); err != nil {
		return BalanceSheetReport{}, err
	}
	control, err := breakdown(scope, equationDifference)
	if err != nil {
		return BalanceSheetReport{}, err
	}
	if !componentBalancesZero(control) {
		return BalanceSheetReport{}, apperr.New(
			apperr.Integrity,
			"BALANCE_SHEET_OUT_OF_BALANCE",
			fmt.Sprintf("balance sheet equation differs by %d consolidated cents (eliminations %d cents)", control.ConsolidatedCents, control.EliminationsCents),
		)
	}
	return report, nil
}
