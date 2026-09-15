package cashflow

import (
	"encoding/json"
	"math"
	"testing"
)

func sample() Plan {
	return Plan{Version: "books.cash-plan/v1", Name: "Example", Currency: "USD", AsOf: "2026-09-01", Through: "2026-09-05", Accounts: []Account{{Code: "checking", Name: "Checking", Kind: "bank", Opening: 100000, Floor: 10000, Evidence: "snapshot"}, {Code: "reserve", Name: "Reserve", Kind: "bank", Reserved: true, Evidence: "snapshot"}, {Code: "card", Name: "Card", Kind: "card", Opening: -20000, Evidence: "statement"}}, Events: []Event{{ID: "bill", Date: "2026-09-02", Name: "Bill", Kind: "outflow", Account: "checking", Amount: 90000, Status: "confirmed", Evidence: "invoice"}, {ID: "salary", Date: "2026-09-03", Name: "Pay", Kind: "inflow", Account: "checking", Amount: 100000, Status: "estimated", Evidence: "pay schedule"}, {ID: "fund", Date: "2026-09-03", Arrival: "2026-09-04", Name: "Reserve transfer", Kind: "transfer", Account: "checking", ToAccount: "reserve", Amount: 30000, Status: "proposed", Evidence: "scenario"}, {ID: "paycard", Date: "2026-09-04", Arrival: "2026-09-05", Name: "Card payment", Kind: "transfer", Account: "checking", ToAccount: "card", Amount: 20000, Status: "confirmed", Evidence: "statement"}}}
}
func TestDailyProjectionAndTransit(t *testing.T) {
	r, e := Project(sample())
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Days) != 15 || r.Totals[2].InTransit != 30000 || r.Totals[4].InTransit != 0 || r.Totals[4].BankCash != 90000 || r.Totals[4].CardBalance != 0 || r.Lows[0].Balance != 10000 {
		t.Fatalf("bad projection: %+v", r)
	}
	if len(r.Days[0].Movements) != 0 {
		t.Fatal("opening duplicated")
	}
	for _, d := range r.Days {
		if d.Opening+d.Inflow-d.Outflow != d.Closing {
			t.Fatal(d)
		}
	}
}
func TestActualReplacesExpected(t *testing.T) {
	p := sample()
	p.Events = append(p.Events, Event{ID: "actualbill", Date: "2026-09-02", Name: "Paid", Kind: "outflow", Account: "checking", Amount: 95000, Status: "actual", Evidence: "journal", Replaces: "bill"})
	r, e := Project(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Variances) != 1 || r.Variances[0].AmountDifference != 5000 || r.Lows[0].Shortfall != 5000 {
		t.Fatal(r)
	}
}
func TestInvalidAndOverflow(t *testing.T) {
	for _, mutate := range []func(*Plan){func(p *Plan) { p.Events[0].Account = "foreign" }, func(p *Plan) { p.Events[0].ID = p.Events[1].ID }, func(p *Plan) { p.Events[2].Arrival = "2026-09-02" }, func(p *Plan) { p.Accounts[0].Opening = Amount(math.MaxInt64); p.Events[0].Kind = "inflow" }, func(p *Plan) { p.Through = "2030-01-01" }} {
		p := sample()
		mutate(&p)
		if _, e := Project(p); e == nil {
			t.Fatal("accepted invalid scenario")
		}
	}
}
func TestAmountJSON(t *testing.T) {
	for _, s := range []string{`1`, `"1.5"`, `"01"`, `"9223372036854775808"`} {
		var a Amount
		if json.Unmarshal([]byte(s), &a) == nil {
			t.Fatal(s)
		}
	}
	var a Amount
	if e := json.Unmarshal([]byte(`"9007199254740993"`), &a); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(a)
	if string(b) != `"9007199254740993"` {
		t.Fatal(string(b))
	}
}

func TestOpeningBoundaryRequiresEvidence(t *testing.T) {
	p := sample()
	p.Events[0].Date = p.AsOf
	if _, e := Project(p); e == nil {
		t.Fatal("unpaid past bill disappeared")
	}
	p = sample()
	p.Events[2].Date = p.AsOf
	if _, e := Project(p); e == nil {
		t.Fatal("proposed past departure invented cash")
	}
	p.Events[2].Status = "actual"
	r, e := Project(p)
	if e != nil {
		t.Fatal(e)
	}
	if r.Totals[0].InTransit != 30000 || r.Totals[3].InTransit != 20000 {
		t.Fatal(r.Totals)
	}
}
