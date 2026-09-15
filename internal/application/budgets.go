package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/budget"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

type BudgetRequest struct {
	AsOf string `json:"as_of"`
}
type BudgetSaveRequest struct {
	AsOf string      `json:"as_of"`
	Plan budget.Plan `json:"plan"`
}
type BudgetAccount = budget.Account
type BudgetResult = budget.Result

func (s *Service) budgetRoot() string { return s.store.Path() + ".budgets" }
func budgetBank(a ledger.Account) bool {
	return a.Type == "ASSET" && (a.Subtype == "BANK" || a.Subtype == "")
}
func (s *Service) validateBudget(ctx context.Context, p budget.Plan) error {
	invalid := func(msg string) error { return apperr.New(apperr.Invalid, "BUDGET_INVALID", msg) }
	if len(p.Buckets) > 500 || len(p.Assignments) > 10000 {
		return invalid("budget is too large")
	}
	if _, e := budget.Summarize("2000-03-01", p, nil); e != nil {
		return invalid(e.Error())
	}
	accounts, e := s.Accounts(ctx)
	if e != nil {
		return e
	}
	bycode := map[string]ledger.Account{}
	for _, a := range accounts {
		bycode[a.Code] = a
	}
	for _, b := range p.Buckets {
		a, ok := bycode[b.Account]
		if !ok || !budgetBank(a) {
			return invalid("bucket must name a bank asset account in this book")
		}
		for _, code := range b.ExpenseAccounts {
			a, ok := bycode[code]
			if !ok || a.Type != "EXPENSE" {
				return invalid("category must name an expense account in this book")
			}
		}
	}
	journals := map[string]ledger.Journal{}
	for _, a := range p.Assignments {
		j, ok := journals[a.Journal]
		if !ok {
			j, e = s.Journal(ctx, JournalReadRequest{ID: a.Journal})
			if e != nil {
				return e
			}
			journals[a.Journal] = j
		}
		found := false
		for _, l := range j.Lines {
			if l.LineNumber == a.Line && bycode[l.AccountCode].Type == "EXPENSE" && j.Status == "POSTED" {
				found = true
			}
		}
		if !found {
			return invalid("assignment must reference a posted expense line in this book")
		}
	}
	return nil
}
func (s *Service) Budget(ctx context.Context, input BudgetRequest) (BudgetResult, error) {
	from, to, _, e := budget.Window(input.AsOf)
	if e != nil {
		return BudgetResult{}, apperr.New(apperr.Invalid, "BUDGET_DATE_INVALID", e.Error())
	}
	p, e := budget.Load(s.budgetRoot(), s.Identity())
	if e != nil {
		return BudgetResult{}, e
	}
	accounts, e := s.Accounts(ctx)
	if e != nil {
		return BudgetResult{}, e
	}
	out := BudgetResult{Accounts: []BudgetAccount{}}
	known := map[string]bool{}
	for _, b := range p.Buckets {
		known[b.Account] = true
	}
	for _, a := range accounts {
		out.Accounts = append(out.Accounts, BudgetAccount{Code: a.Code, Name: a.Name, Type: a.Type, Subtype: a.Subtype})
		if a.Type == "ASSET" && a.Subtype == "BANK" && !known[a.Code] {
			p.Buckets = append(p.Buckets, budget.Bucket{Account: a.Code, ExpenseAccounts: []string{}})
			known[a.Code] = true
		}
	}
	gl, e := s.GeneralLedger(ctx, GeneralLedgerRequest{From: from, To: to})
	if e != nil {
		return out, e
	}
	closing := map[string]bool{}
	reversal := map[string]string{}
	journalRows, e := s.store.DB().QueryContext(ctx, `SELECT j.id,j.kind,COALESCE(j.reversal_of_id,'') FROM journal_entries j JOIN books b ON b.id=j.book_id WHERE b.code=? AND j.posting_date BETWEEN ? AND ?`, s.company.Book, from, to)
	if e != nil {
		return out, e
	}
	for journalRows.Next() {
		var id, kind, original string
		if e = journalRows.Scan(&id, &kind, &original); e != nil {
			_ = journalRows.Close()
			return out, e
		}
		closing[id] = kind == "CLOSING" || kind == "CLOSING_REVERSAL"
		reversal[id] = original
	}
	if e = journalRows.Err(); e != nil {
		_ = journalRows.Close()
		return out, e
	}
	if e = journalRows.Close(); e != nil {
		return out, e
	}

	// A unique bank counterparty identifies direct spending. Never follow transfer
	// chains or classify purchases by a later card-payment date.
	bankByJournal := map[string]map[string]bool{}
	for _, a := range gl.Accounts {
		if !known[a.Account.Code] {
			continue
		}
		for _, l := range a.Lines {
			if l.Synthetic || l.ChangeCents == 0 || closing[l.JournalID] {
				continue
			}
			if bankByJournal[l.JournalID] == nil {
				bankByJournal[l.JournalID] = map[string]bool{}
			}
			bankByJournal[l.JournalID][a.Account.Code] = true
		}
	}
	expenses := []budget.Expense{}
	for _, a := range gl.Accounts {
		if a.Account.Type != "EXPENSE" {
			continue
		}
		for _, l := range a.Lines {
			if l.Synthetic || l.ChangeCents == 0 || closing[l.JournalID] {
				continue
			}
			x := budget.Expense{ReversalOf: reversal[l.JournalID], Journal: l.JournalID, Line: l.LineNumber, Date: l.PostingDate, Description: l.JournalDescription, ExpenseAccount: a.Account.Code, Amount: cashflow.Amount(l.ChangeCents)}
			if codes := bankByJournal[l.JournalID]; len(codes) == 1 {
				for code := range codes {
					x.Bucket = code
					x.Basis = "Direct bank expense"
				}
			}
			expenses = append(expenses, x)
		}
	}
	result, e := budget.Summarize(input.AsOf, p, expenses)
	result.Accounts = out.Accounts
	result.CanEdit = true
	return result, e
}
func (s *Service) SaveBudget(ctx context.Context, input BudgetSaveRequest) (BudgetResult, error) {
	if _, _, _, e := budget.Window(input.AsOf); e != nil {
		return BudgetResult{}, apperr.New(apperr.Invalid, "BUDGET_DATE_INVALID", e.Error())
	}
	if strings.TrimSpace(s.actor) == "" {
		return BudgetResult{}, fmt.Errorf("budget actor required")
	}
	if e := s.validateBudget(ctx, input.Plan); e != nil {
		return BudgetResult{}, e
	}
	if _, e := budget.Save(ctx, s.budgetRoot(), s.Identity(), s.actor, input.Plan); e != nil {
		return BudgetResult{}, e
	}
	return s.Budget(ctx, BudgetRequest{AsOf: input.AsOf})
}
