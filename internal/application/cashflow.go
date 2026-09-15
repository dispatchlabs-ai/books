package application

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
)

// CashForecast binds an evidence-backed scenario to the selected company's
// accounts. Supplied opening snapshots are planning inputs, never ledger writes.
func (s *Service) CashForecast(ctx context.Context, plan cashflow.Plan) (cashflow.Result, error) {
	fail := func(msg string) (cashflow.Result, error) {
		return cashflow.Result{}, apperr.New(apperr.Invalid, "CASH_PLAN_INVALID", msg)
	}
	if plan.Currency != s.company.Currency {
		return fail("plan currency differs from company currency")
	}
	accounts, err := s.Accounts(ctx)
	if err != nil {
		return cashflow.Result{}, err
	}
	for _, p := range plan.Accounts {
		found := false
		for _, a := range accounts {
			if a.Code == p.Code && a.PostingEnabled != nil && *a.PostingEnabled && (a.ActiveFrom == "" || a.ActiveFrom <= plan.AsOf) && (a.ActiveTo == "" || a.ActiveTo >= plan.Through) {
				// Older cash controls may have no subtype. The supplied plan must
				// attest their cash identity; reject incompatible explicit subtypes.
				if (p.Kind == "bank" && a.Type == "ASSET" && (a.Subtype == "BANK" || a.Subtype == "")) || (p.Kind == "card" && a.Type == "LIABILITY" && a.Subtype == "CREDIT_CARD") {
					found = true
				}
			}
		}
		if !found {
			return fail("plan account is not an active compatible account in this book: " + p.Code)
		}
	}
	result, err := cashflow.Project(plan)
	if err != nil {
		return fail(err.Error())
	}
	return result, nil
}
