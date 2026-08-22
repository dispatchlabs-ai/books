package report_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestClosingJournalRollsTrialBalanceWithoutErasingProfitAndLoss(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service := ledger.NewService(store, "test")
	if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2025-12", StartDate: "2025-12-01", EndDate: "2025-12-31", FiscalYear: 2025, PeriodNumber: 12, YearEnd: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"TESTCO"}},
		{Code: "3900", Name: "Retained Earnings", Type: "EQUITY", BookCodes: []string{"TESTCO"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"TESTCO"}},
		{Code: "5000", Name: "Expense", Type: "EXPENSE", BookCodes: []string{"TESTCO"}},
	} {
		if _, err := service.CreateAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	post := func(input ledger.CreateJournalInput) {
		t.Helper()
		journal, err := service.CreateJournal(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PostJournal(ctx, journal.ID); err != nil {
			t.Fatal(err)
		}
	}
	post(ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-15", Period: "2025-12", Description: "Sale",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 10_000}, {Account: "4000", CreditCents: 10_000}},
	})
	post(ledger.CreateJournalInput{
		Book: "TESTCO", PostingDate: "2025-12-20", Period: "2025-12", Description: "Expense",
		Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 4_000}, {Account: "1000", CreditCents: 4_000}},
	})
	post(ledger.CreateJournalInput{
		Book: "TESTCO", Kind: "CLOSING", PostingDate: "2025-12-31", Period: "2025-12", Description: "Close 2025",
		Lines: []ledger.JournalLineInput{
			{Account: "4000", DebitCents: 10_000},
			{Account: "5000", CreditCents: 4_000},
			{Account: "3900", CreditCents: 6_000},
		},
	})

	reports := report.NewService(store)
	pl, err := reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: report.Scope{EntityCode: "TESTCO"}, FromDate: "2025-01-01", ToDate: "2025-12-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pl.TotalRevenue.ConsolidatedCents != 10_000 || pl.TotalExpenses.ConsolidatedCents != 4_000 || pl.NetIncome.ConsolidatedCents != 6_000 {
		t.Fatalf("closing journal changed operating P&L: %+v", pl)
	}
	tb, err := reports.TrialBalance(ctx, report.TrialBalanceInput{
		Scope: report.Scope{EntityCode: "TESTCO"}, AsOfDate: "2025-12-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tb.TotalDebitCents != 6_000 || tb.TotalCreditCents != 6_000 {
		t.Fatalf("post-close trial balance = %d/%d, want 6000/6000", tb.TotalDebitCents, tb.TotalCreditCents)
	}
	bs, err := reports.BalanceSheet(ctx, report.BalanceSheetInput{
		Scope: report.Scope{EntityCode: "TESTCO"}, AsOfDate: "2025-12-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bs.PostedEquity.ConsolidatedCents != 6_000 || bs.CurrentEarnings.ConsolidatedCents != 0 || bs.TotalEquity.ConsolidatedCents != 6_000 {
		t.Fatalf("post-close equity is wrong: %+v", bs)
	}
}

func TestClosingJournalRejectsBalanceSheetAccounts(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service := ledger.NewService(store, "test")
	if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2025-12", StartDate: "2025-12-01", EndDate: "2025-12-31", FiscalYear: 2025, PeriodNumber: 12, YearEnd: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "TESTCO", LegalName: "Test Co", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"TESTCO"}},
		{Code: "3900", Name: "Retained Earnings", Type: "EQUITY", BookCodes: []string{"TESTCO"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"TESTCO"}},
	} {
		if _, err := service.CreateAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "TESTCO", Kind: "CLOSING", PostingDate: "2025-12-31", Period: "2025-12", Description: "Invalid close",
		Lines: []ledger.JournalLineInput{
			{Account: "4000", DebitCents: 100},
			{Account: "1000", CreditCents: 50},
			{Account: "3900", CreditCents: 50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PostJournal(ctx, journal.ID); err == nil {
		t.Fatal("closing journal with an asset account unexpectedly posted")
	}
}
