package budget

import (
	"context"
	"errors"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTwoCompleteMonthsAtYearBoundary(t *testing.T) {
	from, to, m, e := Window("2027-01-15")
	if e != nil || from != "2026-11-01" || to != "2026-12-31" || m[1] != "2026-12" {
		t.Fatal(from, to, m, e)
	}
}
func TestExpenseAttributionAndReversal(t *testing.T) {
	target := cashflow.Amount(10000)
	p := Plan{Buckets: []Bucket{{Account: "cash", Monthly: &target}, {Account: "food", ExpenseAccounts: []string{"meals"}}}, Assignments: []Assignment{{Journal: "purchase", Line: 1, Account: "cash"}}}
	expenses := []Expense{{Journal: "purchase", Line: 1, Date: "2026-07-02", ExpenseAccount: "meals", Amount: 10001}, {Journal: "reverse", ReversalOf: "purchase", Line: 1, Date: "2026-08-01", ExpenseAccount: "meals", Amount: -10001}, {Journal: "other", Line: 1, Date: "2026-08-05", ExpenseAccount: "meals", Amount: 401}, {Journal: "direct", Line: 1, Date: "2026-07-09", ExpenseAccount: "supplies", Bucket: "cash", Basis: "Direct bank expense", Amount: 800}, {Journal: "unknown", Line: 1, Date: "2026-08-06", ExpenseAccount: "other", Amount: 203}, {Journal: "future", Line: 1, Date: "2026-09-01", Amount: 99999}}
	r, e := Summarize("2026-09-15", p, expenses)
	if e != nil {
		t.Fatal(e)
	}
	if r.Rows[0].Average != 400 || r.Rows[1].Average != 201 || r.Unassigned.Average != 102 || len(r.Expenses) != 5 {
		t.Fatalf("%+v", r)
	}
	if r.Expenses[1].Bucket != "cash" {
		t.Fatal("reversal lost assignment")
	}
}
func TestUnknownIsNotZeroAndRulesAreUnique(t *testing.T) {
	p := Plan{Buckets: []Bucket{{Account: "a", ExpenseAccounts: []string{"x"}}, {Account: "b", ExpenseAccounts: []string{"x"}}}}
	if _, e := Summarize("2026-09-01", p, nil); e == nil {
		t.Fatal("duplicate rule")
	}
	p.Buckets = p.Buckets[:1]
	r, e := Summarize("2026-09-01", p, nil)
	if e != nil || r.Rows[0].Monthly != nil {
		t.Fatal("unset target became zero")
	}
	if _, e = Add(cashflow.Amount(math.MaxInt64), 1); e == nil {
		t.Fatal("overflow")
	}
}
func TestStoreRevisionConflictAndIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plans")
	p, e := Load(root, "one")
	if e != nil || p.Revision != "" {
		t.Fatal(e)
	}
	if _, e = os.Stat(root); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("read created state")
	}
	p.Buckets = []Bucket{{Account: "cash"}}
	first, e := Save(context.Background(), root, "one", "tester", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Save(context.Background(), root, "one", "tester", p); e == nil {
		t.Fatal("stale revision accepted")
	}
	other, e := Load(root, "two")
	if e != nil || len(other.Buckets) != 0 {
		t.Fatal("cross-company read")
	}
	var wg sync.WaitGroup
	success := 0
	var lock sync.Mutex
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := first
			v.Buckets = []Bucket{{Account: []string{"a", "b"}[i]}}
			_, err := Save(context.Background(), root, "one", "tester", v)
			if err == nil {
				lock.Lock()
				success++
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatal("concurrent writes", success)
	}
	files, e := filepath.Glob(filename(root, "one") + ".history/*.json")
	if e != nil || len(files) != 2 {
		t.Fatal("history not preserved", files, e)
	}
}
