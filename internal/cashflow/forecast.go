// Package cashflow calculates dated scenarios without changing the actual ledger.
package cashflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// Amount uses integer minor units and a JSON string, including for currencies
// whose smallest unit is not a cent.
type Amount int64

func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(fmt.Sprint(int64(a))) }
func (a *Amount) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("amount must be a minor-unit string")
	}
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil || fmt.Sprint(n) != s {
		return fmt.Errorf("invalid integer amount")
	}
	*a = Amount(n)
	return nil
}

type Account struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // bank or card; card liabilities are negative
	Opening  Amount `json:"opening"`
	Floor    Amount `json:"floor"`
	Reserved bool   `json:"reserved"`
	Evidence string `json:"evidence"`
}
type Event struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // inflow, outflow, transfer
	Account   string `json:"account"`
	ToAccount string `json:"to_account,omitempty"`
	Arrival   string `json:"arrival,omitempty"`
	Amount    Amount `json:"amount"`
	Status    string `json:"status"` // confirmed, estimated, proposed, actual
	Category  string `json:"category,omitempty"`
	Evidence  string `json:"evidence"`
	Replaces  string `json:"replaces,omitempty"` // explicit expected-event ID; never heuristic matching
}
type Plan struct {
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Currency    string    `json:"currency"`
	AsOf        string    `json:"as_of"` // opening balances are end-of-day
	Through     string    `json:"through"`
	Accounts    []Account `json:"accounts"`
	Events      []Event   `json:"events"` // dated occurrences, including every recurring occurrence
	Assumptions []string  `json:"assumptions"`
}
type Movement struct {
	Event   Event  `json:"event"`
	Delta   Amount `json:"delta"`
	Balance Amount `json:"balance"`
}
type Day struct {
	Date      string     `json:"date"`
	Account   string     `json:"account"`
	Opening   Amount     `json:"opening"`
	Inflow    Amount     `json:"inflow"`
	Outflow   Amount     `json:"outflow"`
	Closing   Amount     `json:"closing"`
	Floor     Amount     `json:"floor"`
	Shortfall Amount     `json:"shortfall"`
	Movements []Movement `json:"movements"`
}
type Total struct {
	Date        string `json:"date"`
	BankCash    Amount `json:"bank_cash"`
	WorkingCash Amount `json:"working_cash"`
	CardBalance Amount `json:"card_balance"`
	InTransit   Amount `json:"in_transit"`
}
type Low struct {
	Account   string `json:"account"`
	Date      string `json:"date"`
	Balance   Amount `json:"balance"`
	Shortfall Amount `json:"shortfall"`
}
type Variance struct {
	Expected         Event  `json:"expected"`
	Actual           Event  `json:"actual"`
	AmountDifference Amount `json:"amount_difference"`
}
type Result struct {
	Version   string     `json:"version"`
	Digest    string     `json:"digest"`
	Plan      Plan       `json:"plan"`
	Days      []Day      `json:"days"`
	Totals    []Total    `json:"totals"`
	Lows      []Low      `json:"lows"`
	Variances []Variance `json:"variances"`
	Warnings  []string   `json:"warnings"`
}

func date(s string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil || t.Format("2006-01-02") != s {
		return t, fmt.Errorf("invalid date %q", s)
	}
	return t, nil
}
func add(a, b Amount) (Amount, error) {
	if (b > 0 && a > Amount(math.MaxInt64)-b) || (b < 0 && a < Amount(math.MinInt64)-b) {
		return 0, fmt.Errorf("cash amount overflow")
	}
	return a + b, nil
}
func gap(f, b Amount) (Amount, error) {
	if b >= f {
		return 0, nil
	}
	if b == Amount(math.MinInt64) {
		return 0, fmt.Errorf("cash amount overflow")
	}
	return add(f, -b)
}

// Project is deterministic. Opening balances include all events through AsOf.
// Plans are immutable inputs; callers retain input/output digests for comparison.
func Project(p Plan) (Result, error) {
	r := Result{Version: "books.cashflow/v1", Plan: p, Days: []Day{}, Totals: []Total{}, Lows: []Low{}, Variances: []Variance{}, Warnings: []string{}}
	start, err := date(p.AsOf)
	if err != nil {
		return r, err
	}
	end, err := date(p.Through)
	if err != nil {
		return r, err
	}
	if p.Version != "books.cash-plan/v1" || p.Name == "" || len(p.Accounts) == 0 || len(p.Accounts) > 100 || len(p.Events) > 20000 || end.Before(start) || end.Sub(start) > 730*24*time.Hour {
		return r, fmt.Errorf("invalid plan version, name, size or date range")
	}
	accounts := map[string]Account{}
	balances := map[string]Amount{}
	lows := map[string]Low{}
	for _, a := range p.Accounts {
		if a.Code == "" || a.Name == "" || a.Evidence == "" || (a.Kind != "bank" && a.Kind != "card") || a.Floor < 0 || a.Opening == Amount(math.MinInt64) {
			return r, fmt.Errorf("invalid account %q", a.Code)
		}
		if _, ok := accounts[a.Code]; ok {
			return r, fmt.Errorf("duplicate account %q", a.Code)
		}
		accounts[a.Code] = a
		balances[a.Code] = a.Opening
		g := Amount(0)
		if a.Kind == "bank" {
			g, err = gap(a.Floor, a.Opening)
			if err != nil {
				return r, err
			}
		}
		lows[a.Code] = Low{a.Code, p.AsOf, a.Opening, g}
	}
	events := map[string]Event{}
	replaced := map[string]bool{}
	for _, e := range p.Events {
		if _, err = date(e.Date); err != nil {
			return r, err
		}
		a, ok := accounts[e.Account]
		if !ok || e.ID == "" || e.Name == "" || e.Evidence == "" || e.Amount <= 0 || (e.Kind != "inflow" && e.Kind != "outflow" && e.Kind != "transfer") || (e.Status != "confirmed" && e.Status != "estimated" && e.Status != "proposed" && e.Status != "actual") {
			return r, fmt.Errorf("invalid event %q", e.ID)
		}
		if _, ok = events[e.ID]; ok {
			return r, fmt.Errorf("duplicate event %q", e.ID)
		}
		if e.Kind == "transfer" {
			b, ok := accounts[e.ToAccount]
			if !ok || e.ToAccount == e.Account || a.Kind != "bank" || (b.Kind != "bank" && b.Kind != "card") {
				return r, fmt.Errorf("invalid transfer %q", e.ID)
			}
			if _, err = date(e.Arrival); err != nil {
				return r, err
			}
			if e.Arrival < e.Date {
				return r, fmt.Errorf("arrival precedes departure")
			}
		} else if e.ToAccount != "" || e.Arrival != "" {
			return r, fmt.Errorf("non-transfer has a destination")
		}
		events[e.ID] = e
	}
	for _, e := range p.Events {
		if e.Replaces != "" {
			old, ok := events[e.Replaces]
			if !ok || old.Replaces != "" || old.Status == "actual" || e.Status != "actual" || e.Account != old.Account || e.Kind != old.Kind || e.ToAccount != old.ToAccount || replaced[old.ID] {
				return r, fmt.Errorf("invalid actual match %q", e.ID)
			}
			replaced[old.ID] = true
			r.Variances = append(r.Variances, Variance{old, e, e.Amount - old.Amount})
		}
	}
	type leg struct {
		event   Event
		delta   Amount
		transit Amount
	}
	calendar := map[string]map[string][]leg{}
	transit := Amount(0)
	put := func(day, account string, l leg) {
		if calendar[day] == nil {
			calendar[day] = map[string][]leg{}
		}
		calendar[day][account] = append(calendar[day][account], l)
	}
	for _, e := range p.Events {
		if replaced[e.ID] {
			continue
		}
		if e.Date <= p.AsOf && e.Status != "actual" {
			return r, fmt.Errorf("unresolved event %q is on or before opening date; match an actual or explicitly reschedule it", e.ID)
		}
		delta := e.Amount
		if e.Kind != "inflow" {
			delta = -delta
		}
		if e.Date > p.AsOf && e.Date <= p.Through {
			tr := Amount(0)
			if e.Kind == "transfer" {
				tr = e.Amount
			}
			put(e.Date, e.Account, leg{e, delta, tr})
		}
		if e.Kind == "transfer" {
			if e.Date <= p.AsOf && e.Arrival > p.AsOf {
				transit, err = add(transit, e.Amount)
				if err != nil {
					return r, err
				}
			}
			if e.Arrival > p.AsOf && e.Arrival <= p.Through {
				put(e.Arrival, e.ToAccount, leg{e, e.Amount, -e.Amount})
			}
		}
		if e.Status == "proposed" && e.Date <= p.Through && (e.Date > p.AsOf || e.Arrival > p.AsOf) {
			r.Warnings = append(r.Warnings, "Includes proposed transfer/event: "+e.ID)
		}
	}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		ds := day.Format("2006-01-02")
		total := Total{Date: ds}
		for _, a := range p.Accounts {
			row := Day{Date: ds, Account: a.Code, Opening: balances[a.Code], Floor: a.Floor, Movements: []Movement{}}
			legs := calendar[ds][a.Code]
			sort.Slice(legs, func(i, j int) bool {
				if legs[i].delta < 0 != (legs[j].delta < 0) {
					return legs[i].delta < 0
				}
				return legs[i].event.ID < legs[j].event.ID
			})
			for _, l := range legs {
				balances[a.Code], err = add(balances[a.Code], l.delta)
				if err != nil {
					return r, err
				}
				transit, err = add(transit, l.transit)
				if err != nil {
					return r, err
				}
				if l.delta > 0 {
					row.Inflow, err = add(row.Inflow, l.delta)
				} else {
					row.Outflow, err = add(row.Outflow, -l.delta)
				}
				if err != nil {
					return r, err
				}
				row.Movements = append(row.Movements, Movement{l.event, l.delta, balances[a.Code]})
			}
			row.Closing = balances[a.Code]
			if a.Kind == "bank" {
				row.Shortfall, err = gap(a.Floor, row.Closing)
				if err != nil {
					return r, err
				}
				total.BankCash, err = add(total.BankCash, row.Closing)
				if err != nil {
					return r, err
				}
				if !a.Reserved {
					total.WorkingCash, err = add(total.WorkingCash, row.Closing)
				}
			} else {
				total.CardBalance, err = add(total.CardBalance, row.Closing)
			}
			if err != nil {
				return r, err
			}
			if row.Closing < lows[a.Code].Balance {
				lows[a.Code] = Low{a.Code, ds, row.Closing, row.Shortfall}
			}
			r.Days = append(r.Days, row)
		}
		total.InTransit = transit
		r.Totals = append(r.Totals, total)
	}
	for _, a := range p.Accounts {
		r.Lows = append(r.Lows, lows[a.Code])
	}
	r.Warnings = append(r.Warnings, "Balances are end-of-day; intraday availability is not established. Planned movements do not execute payments.")
	raw, err := json.Marshal(p)
	if err != nil {
		return r, err
	}
	sum := sha256.Sum256(raw)
	r.Digest = hex.EncodeToString(sum[:])
	return r, nil
}
