// Package budget attributes posted expenses to spending buckets. Funding and
// liability settlements never become a second expense.
package budget

import (
	"fmt"
	"math"
	"time"

	"github.com/dispatchlabs-ai/books/internal/cashflow"
)

type Bucket struct {
	Account         string           `json:"account"`
	Monthly         *cashflow.Amount `json:"monthly"`
	ExpenseAccounts []string         `json:"expense_accounts"`
}
type Assignment struct {
	Journal string `json:"journal"`
	Line    int    `json:"line"`
	Account string `json:"account"`
}
type Plan struct {
	Revision    string       `json:"revision"`
	Buckets     []Bucket     `json:"buckets"`
	Assignments []Assignment `json:"assignments"`
}
type Expense struct {
	ReversalOf     string          `json:"reversal_of,omitempty"`
	Journal        string          `json:"journal"`
	Line           int             `json:"line"`
	Date           string          `json:"date"`
	Description    string          `json:"description"`
	ExpenseAccount string          `json:"expense_account"`
	Amount         cashflow.Amount `json:"amount"`
	Bucket         string          `json:"bucket"`
	Basis          string          `json:"basis"`
}
type Row struct {
	Account string             `json:"account"`
	Months  [2]cashflow.Amount `json:"months"`
	Average cashflow.Amount    `json:"average"`
	Monthly *cashflow.Amount   `json:"monthly"`
	Count   int                `json:"count"`
}
type Account struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}
type Result struct {
	CanEdit    bool      `json:"can_edit"`
	Accounts   []Account `json:"accounts"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Months     [2]string `json:"months"`
	Rows       []Row     `json:"rows"`
	Unassigned Row       `json:"unassigned"`
	Expenses   []Expense `json:"expenses"`
	Plan       Plan      `json:"plan"`
}

func Window(asOf string) (string, string, [2]string, error) {
	d, e := time.Parse("2006-01-02", asOf)
	if e != nil {
		return "", "", [2]string{}, fmt.Errorf("invalid as_of date")
	}
	start := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	first := start.AddDate(0, -2, 0)
	second := start.AddDate(0, -1, 0)
	return first.Format("2006-01-02"), start.AddDate(0, 0, -1).Format("2006-01-02"), [2]string{first.Format("2006-01"), second.Format("2006-01")}, nil
}
func Add(a, b cashflow.Amount) (cashflow.Amount, error) {
	if b > 0 && a > cashflow.Amount(math.MaxInt64)-b || b < 0 && a < cashflow.Amount(math.MinInt64)-b {
		return 0, fmt.Errorf("budget amount overflow")
	}
	return a + b, nil
}
func half(n cashflow.Amount) cashflow.Amount {
	if n%2 == 0 {
		return n / 2
	}
	if n > 0 {
		return n/2 + 1
	}
	return n/2 - 1
}
func Summarize(asOf string, p Plan, expenses []Expense) (Result, error) {
	from, to, months, e := Window(asOf)
	if e != nil {
		return Result{}, e
	}
	r := Result{From: from, To: to, Months: months, Plan: p, Rows: []Row{}, Expenses: []Expense{}}
	buckets := map[string]int{}
	rules := map[string]string{}
	assignments := map[string]string{}
	for _, b := range p.Buckets {
		if b.Account == "" {
			return r, fmt.Errorf("bucket account required")
		}
		if _, ok := buckets[b.Account]; ok {
			return r, fmt.Errorf("duplicate bucket")
		}
		buckets[b.Account] = len(r.Rows)
		r.Rows = append(r.Rows, Row{Account: b.Account, Monthly: b.Monthly})
		if b.Monthly != nil && *b.Monthly < 0 {
			return r, fmt.Errorf("monthly budget cannot be negative")
		}
		for _, a := range b.ExpenseAccounts {
			if _, ok := rules[a]; ok {
				return r, fmt.Errorf("expense account has multiple buckets")
			}
			rules[a] = b.Account
		}
	}
	for _, a := range p.Assignments {
		key := fmt.Sprintf("%s:%d", a.Journal, a.Line)
		if a.Journal == "" || a.Line < 1 {
			return r, fmt.Errorf("invalid assignment")
		}
		if _, ok := buckets[a.Account]; !ok {
			return r, fmt.Errorf("unknown assigned bucket")
		}
		if _, ok := assignments[key]; ok {
			return r, fmt.Errorf("duplicate assignment")
		}
		assignments[key] = a.Account
	}
	for _, x := range expenses {
		if x.Date < from || x.Date > to {
			continue
		}
		month := 0
		if x.Date[:7] == months[1] {
			month = 1
		}
		if a, ok := assignments[fmt.Sprintf("%s:%d", x.Journal, x.Line)]; ok {
			x.Bucket = a
			x.Basis = "Purchase assignment"
		} else if a, ok := assignments[fmt.Sprintf("%s:%d", x.ReversalOf, x.Line)]; ok && x.ReversalOf != "" {
			x.Bucket = a
			x.Basis = "Reversed purchase assignment"
		} else if a, ok := rules[x.ExpenseAccount]; ok {
			x.Bucket = a
			x.Basis = "Expense category"
		}
		target := &r.Unassigned
		if i, ok := buckets[x.Bucket]; ok {
			target = &r.Rows[i]
		} else {
			x.Bucket = ""
			x.Basis = "Unassigned"
		}
		n, err := Add(target.Months[month], x.Amount)
		if err != nil {
			return r, err
		}
		target.Months[month] = n
		target.Count++
		r.Expenses = append(r.Expenses, x)
	}
	for i := range r.Rows {
		n, err := Add(r.Rows[i].Months[0], r.Rows[i].Months[1])
		if err != nil {
			return r, err
		}
		r.Rows[i].Average = half(n)
	}
	n, err := Add(r.Unassigned.Months[0], r.Unassigned.Months[1])
	if err != nil {
		return r, err
	}
	r.Unassigned.Average = half(n)
	return r, nil
}
