package cli

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestReportBackendAuthorization(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "books.toml")
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", config)
	t.Setenv("BOOKS_ACTOR", "report-policy-test")
	t.Setenv("BOOKS_DB", "")
	executeHumanJSON(t, "init", "--name", "Policy Example", "--company", "acme", "--currency", "USD", "--start", "2026-01-01")
	app, err := application.Open(context.Background(), config, "acme", "initial-actor", storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	for _, args := range [][]string{
		{"gl", "--from", "2026-01-01", "--to", "2026-01-31"},
		{"tb", "--as-of", "2026-01-31"},
		{"bs", "--as-of", "2026-01-31"},
		{"pl", "--from", "2026-01-01", "--to", "2026-01-31"},
	} {
		expected, _ := executeHumanJSON(t, args...)
		for _, actor := range []string{"", " "} {
			actual, _ := executeHumanJSON(t, append([]string{"--actor", actor}, args...)...)
			if !reflect.DeepEqual(expected["data"], actual["data"]) {
				t.Fatal("blank actor changed read-only report")
			}
		}
	}
	testReportPolicy(t, app, operations.GeneralLedger(), application.GeneralLedgerRequest{From: "2026-01-01", To: "2026-01-31"})
	testReportPolicy(t, app, operations.TrialBalance(), application.AsOfReportRequest{AsOf: "2026-01-31"})
	testReportPolicy(t, app, operations.BalanceSheet(), application.AsOfReportRequest{AsOf: "2026-01-31"})
	testReportPolicy(t, app, operations.ProfitLoss(), application.RangeReportRequest{From: "2026-01-01", To: "2026-01-31"})
}

func testReportPolicy[I, O any](t *testing.T, app *application.Service, op operations.TypedOperation[I, O], input I) {
	t.Helper()
	t.Run(op.Descriptor().ID, func(t *testing.T) {
		for name, access := range map[string]operations.Access{
			"zero":              {},
			"other company":     operations.CompanyAccess("reader", "other", []string{"read"}),
			"missing read":      operations.CompanyAccess("reader", "acme", []string{"manage", "post", "import"}),
			"empty actor":       operations.CompanyAccess("", "acme", []string{"read"}),
			"empty local actor": operations.TrustedLocalAccess(" "),
		} {
			t.Run(name, func(t *testing.T) {
				_, err := op.Execute(context.Background(), app, access, input)
				e, ok := apperr.As(err)
				if !ok || e.Code != "COMPANY_NOT_FOUND" {
					t.Fatalf("unexpected denial: %v", err)
				}
			})
		}
		grants := []string{"read"}
		access := operations.CompanyAccess("reader", "acme", grants)
		grants[0] = "manage"
		remote, err := op.Execute(context.Background(), app, access, input)
		if err != nil {
			t.Fatal(err)
		}
		local, err := op.Execute(context.Background(), app, operations.TrustedLocalAccess("owner"), input)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(remote, local) {
			t.Fatal("local and scoped execution differ")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := op.Execute(ctx, app, access, input); err != context.Canceled {
			t.Fatalf("cancellation: %v", err)
		}
		if _, err := op.Execute(context.Background(), nil, access, input); err == nil {
			t.Fatal("nil application accepted")
		}
	})
}
