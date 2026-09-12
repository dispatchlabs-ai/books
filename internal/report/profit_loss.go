package report

import "context"

// ProfitLoss returns posted revenue and expense activity for an inclusive date
// range. Revenue and expense rows use their natural financial-statement signs.
func (s *Service) ProfitLoss(ctx context.Context, input ProfitLossInput) (ProfitLossReport, error) {
	return withReportSnapshot(ctx, s, func(snapshot *Service) (ProfitLossReport, error) {
		return snapshot.profitLoss(ctx, input)
	})
}

func (s *Service) profitLoss(ctx context.Context, input ProfitLossInput) (ProfitLossReport, error) {
	if err := validateRange(input.FromDate, input.ToDate); err != nil {
		return ProfitLossReport{}, err
	}
	scope, err := s.resolveRangeScope(ctx, input.Scope, input.FromDate, input.ToDate)
	if err != nil {
		return ProfitLossReport{}, err
	}
	if err := s.verifyPostedJournalIntegrity(ctx, scope, input.ToDate); err != nil {
		return ProfitLossReport{}, err
	}
	lines, err := s.loadOperatingLines(ctx, scope, input.FromDate, input.ToDate)
	if err != nil {
		return ProfitLossReport{}, err
	}
	balances, err := aggregateBalances(lines)
	if err != nil {
		return ProfitLossReport{}, err
	}
	if input.IncludeZero {
		catalog, err := s.loadAccountCatalog(ctx, scope)
		if err != nil {
			return ProfitLossReport{}, err
		}
		includeCatalogAccounts(balances, catalog)
	}

	revenueBooks := centsByBook{}
	expenseBooks := centsByBook{}
	report := ProfitLossReport{Scope: scope, FromDate: input.FromDate, ToDate: input.ToDate}
	for _, balance := range sortedBalances(balances) {
		if balance.Account.Type != "REVENUE" && balance.Account.Type != "EXPENSE" {
			continue
		}
		if !input.IncludeZero && !anyBookAmount(balance.ByBook) {
			continue
		}
		if balance.Account.Type == "REVENUE" {
			natural, err := scaleCents(balance.ByBook, -1)
			if err != nil {
				return ProfitLossReport{}, err
			}
			if err := addCents(revenueBooks, natural, 1); err != nil {
				return ProfitLossReport{}, err
			}
			amount, err := breakdown(scope, natural)
			if err != nil {
				return ProfitLossReport{}, err
			}
			report.Revenue = append(report.Revenue, StatementRow{Account: balance.Account, Amount: amount})
			continue
		}
		natural, err := scaleCents(balance.ByBook, 1)
		if err != nil {
			return ProfitLossReport{}, err
		}
		if err := addCents(expenseBooks, natural, 1); err != nil {
			return ProfitLossReport{}, err
		}
		amount, err := breakdown(scope, natural)
		if err != nil {
			return ProfitLossReport{}, err
		}
		report.Expenses = append(report.Expenses, StatementRow{Account: balance.Account, Amount: amount})
	}
	report.TotalRevenue, err = breakdown(scope, revenueBooks)
	if err != nil {
		return ProfitLossReport{}, err
	}
	report.TotalExpenses, err = breakdown(scope, expenseBooks)
	if err != nil {
		return ProfitLossReport{}, err
	}
	netIncome := centsByBook{}
	if err := addCents(netIncome, revenueBooks, 1); err != nil {
		return ProfitLossReport{}, err
	}
	if err := addCents(netIncome, expenseBooks, -1); err != nil {
		return ProfitLossReport{}, err
	}
	report.NetIncome, err = breakdown(scope, netIncome)
	if err != nil {
		return ProfitLossReport{}, err
	}
	return report, nil
}
