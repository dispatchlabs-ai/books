package operations

import (
	"context"
	"testing"
)

func TestTypedReportCatalog(t *testing.T) {
	inventory := map[string]Operation{}
	for _, op := range Catalog() {
		inventory[op.ID] = op
	}
	seen := map[string]bool{}
	for _, d := range TypedReports() {
		op, ok := inventory[d.ID]
		if !ok || seen[d.ID] || len(op.CLI) == 0 || len(op.HTTP) == 0 || op.Gap == "" {
			t.Fatalf("missing or duplicate report binding: %s", d.ID)
		}
		seen[d.ID] = true
		if d.Version != 1 || d.Scope != "company" || d.Grant != "read" || d.Effect != "read" || d.Input == nil || d.Output == nil {
			t.Fatalf("incomplete descriptor: %+v", d)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected four typed reports, got %d", len(seen))
	}
	// Callers receive values; editing metadata cannot weaken executable policy.
	op := TrialBalance()
	d := op.Descriptor()
	d.Grant = "manage"
	if op.Descriptor().Grant != "read" {
		t.Fatal("descriptor mutation changed policy")
	}
	var empty TypedOperation[struct{}, struct{}]
	if _, err := empty.Execute(context.Background(), nil, Access{}, struct{}{}); err == nil {
		t.Fatal("zero operation accepted")
	}
}
