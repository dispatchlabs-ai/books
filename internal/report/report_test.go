package report_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type fixture struct {
	store   *storesqlite.Store
	reports *report.Service
	ledger  *ledger.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ledgers := ledger.NewService(store, "test")

	northstar, err := ledgers.CreateEntity(ctx, ledger.CreateEntityInput{
		Code: "NORTHSTAR", LegalName: "Northstar, Inc.", Currency: "USD", BookCode: "NORTHSTAR",
	})
	if err != nil {
		t.Fatalf("create Northstar: %v", err)
	}
	acme, err := ledgers.CreateEntity(ctx, ledger.CreateEntityInput{
		Code: "ACME", LegalName: "Acme, Inc.", Currency: "USD", BookCode: "ACME",
	})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}
	group, err := ledgers.CreateGroup(ctx, ledger.CreateGroupInput{
		Code: "NORTHSTAR-GROUP", Name: "Northstar Consolidated", ParentEntity: northstar.Code,
		EliminationBookCode: "NORTHSTAR-ELIM",
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := ledgers.AddOwnership(ctx, northstar.Code, acme.Code, "2026-07-01", ""); err != nil {
		t.Fatalf("record Northstar ownership of Acme: %v", err)
	}
	for _, period := range []ledger.CreatePeriodInput{
		{Code: "2026-06", StartDate: "2026-06-01", EndDate: "2026-06-30", FiscalYear: 2026, PeriodNumber: 6},
		{Code: "2026-07", StartDate: "2026-07-01", EndDate: "2026-07-31", FiscalYear: 2026, PeriodNumber: 7},
	} {
		if _, err := ledgers.CreatePeriod(ctx, period); err != nil {
			t.Fatalf("create period %s: %v", period.Code, err)
		}
	}
	bookCodes := []string{northstar.BookCode, acme.BookCode, group.EliminationBookCode}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", NormalBalance: "DEBIT", StatementSection: "CASH", BookCodes: bookCodes},
		{Code: "1100", Name: "Intercompany Receivable", Type: "ASSET", NormalBalance: "DEBIT", StatementSection: "CURRENT_ASSETS", BookCodes: bookCodes},
		{Code: "2000", Name: "Intercompany Payable", Type: "LIABILITY", NormalBalance: "CREDIT", StatementSection: "CURRENT_LIABILITIES", BookCodes: bookCodes},
		{Code: "3000", Name: "Opening Equity", Type: "EQUITY", NormalBalance: "CREDIT", StatementSection: "EQUITY", BookCodes: bookCodes},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", NormalBalance: "CREDIT", StatementSection: "REVENUE", BookCodes: bookCodes},
		{Code: "5000", Name: "Operating Expense", Type: "EXPENSE", NormalBalance: "DEBIT", StatementSection: "OPERATING_EXPENSE", BookCodes: bookCodes},
		{Code: "9999", Name: "Unused", Type: "EXPENSE", NormalBalance: "DEBIT", StatementSection: "OTHER_EXPENSE", BookCodes: bookCodes},
	} {
		if _, err := ledgers.CreateAccount(ctx, account); err != nil {
			t.Fatalf("create account %s: %v", account.Code, err)
		}
	}

	post := func(input ledger.CreateJournalInput) {
		t.Helper()
		journal, err := ledgers.CreateJournal(ctx, input)
		if err != nil {
			t.Fatalf("create journal %s: %v", input.Description, err)
		}
		if _, err := ledgers.PostJournal(ctx, journal.ID); err != nil {
			t.Fatalf("post journal %s: %v", input.Description, err)
		}
	}
	post(ledger.CreateJournalInput{
		Book: northstar.BookCode, PostingDate: "2026-06-30", Period: "2026-06", Description: "Opening balance",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 300000}, {Account: "3000", CreditCents: 300000}},
	})
	post(ledger.CreateJournalInput{
		Book: northstar.BookCode, PostingDate: "2026-07-01", Period: "2026-07", Description: "Northstar sales",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 1000000}, {Account: "4000", CreditCents: 1000000}},
	})
	post(ledger.CreateJournalInput{
		Book: northstar.BookCode, PostingDate: "2026-07-15", Period: "2026-07", Description: "Northstar costs",
		Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 250000}, {Account: "1000", CreditCents: 250000}},
	})
	post(ledger.CreateJournalInput{
		Book: northstar.BookCode, PostingDate: "2026-07-20", Period: "2026-07", Description: "Management charge",
		Lines: []ledger.JournalLineInput{{Account: "1100", DebitCents: 10000}, {Account: "4000", CreditCents: 10000}},
	})
	post(ledger.CreateJournalInput{
		Book: acme.BookCode, PostingDate: "2026-07-02", Period: "2026-07", Description: "Acme sales",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 2000000}, {Account: "4000", CreditCents: 2000000}},
	})
	post(ledger.CreateJournalInput{
		Book: acme.BookCode, PostingDate: "2026-07-16", Period: "2026-07", Description: "Acme costs",
		Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 400000}, {Account: "1000", CreditCents: 400000}},
	})
	post(ledger.CreateJournalInput{
		Book: acme.BookCode, PostingDate: "2026-07-20", Period: "2026-07", Description: "Management fee",
		Lines: []ledger.JournalLineInput{{Account: "5000", DebitCents: 10000}, {Account: "2000", CreditCents: 10000}},
	})
	post(ledger.CreateJournalInput{
		Book: group.EliminationBookCode, PostingDate: "2026-07-31", Period: "2026-07", Description: "Intercompany eliminations",
		Lines: []ledger.JournalLineInput{
			{Account: "4000", DebitCents: 10000},
			{Account: "2000", DebitCents: 10000},
			{Account: "5000", CreditCents: 10000},
			{Account: "1100", CreditCents: 10000},
		},
	})
	// A balanced draft proves every report filters on POSTED rather than merely
	// relying on journal line presence.
	if _, err := ledgers.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: northstar.BookCode, PostingDate: "2026-07-31", Period: "2026-07", Description: "Unposted sale",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 99999999}, {Account: "4000", CreditCents: 99999999}},
	}); err != nil {
		t.Fatalf("create draft journal: %v", err)
	}

	return &fixture{store: store, reports: report.NewService(store), ledger: ledgers}
}

func newNestedOwnershipFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ledgers := ledger.NewService(store, "test")
	for _, input := range []ledger.CreateEntityInput{
		{Code: "PARENT", LegalName: "Parent Corp.", Currency: "USD"},
		{Code: "HOLDCO", LegalName: "Intermediate Holding Co.", Currency: "USD"},
		{Code: "SUB", LegalName: "Operating Subsidiary", Currency: "USD"},
	} {
		if _, err := ledgers.CreateEntity(ctx, input); err != nil {
			t.Fatalf("create %s: %v", input.Code, err)
		}
	}
	group, err := ledgers.CreateGroup(ctx, ledger.CreateGroupInput{
		Code: "PARENT-GROUP", Name: "Parent Consolidated", ParentEntity: "PARENT", EliminationBookCode: "PARENT-ELIM",
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := ledgers.AddOwnership(ctx, "PARENT", "HOLDCO", "2026-01-01", "2026-07-15"); err != nil {
		t.Fatalf("record parent-to-holding ownership: %v", err)
	}
	if _, err := ledgers.AddOwnership(ctx, "HOLDCO", "SUB", "2026-07-01", ""); err != nil {
		t.Fatalf("record holding-to-subsidiary ownership: %v", err)
	}
	for _, period := range []ledger.CreatePeriodInput{
		{Code: "2026-06", StartDate: "2026-06-01", EndDate: "2026-06-30", FiscalYear: 2026, PeriodNumber: 6},
		{Code: "2026-07", StartDate: "2026-07-01", EndDate: "2026-07-31", FiscalYear: 2026, PeriodNumber: 7},
	} {
		if _, err := ledgers.CreatePeriod(ctx, period); err != nil {
			t.Fatalf("create period %s: %v", period.Code, err)
		}
	}
	bookCodes := []string{"PARENT", "HOLDCO", "SUB", group.EliminationBookCode}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", NormalBalance: "DEBIT", StatementSection: "CASH", BookCodes: bookCodes},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", NormalBalance: "CREDIT", StatementSection: "REVENUE", BookCodes: bookCodes},
	} {
		if _, err := ledgers.CreateAccount(ctx, account); err != nil {
			t.Fatalf("create account %s: %v", account.Code, err)
		}
	}
	postSale := func(book, date, period, description string, cents int64) {
		t.Helper()
		journal, err := ledgers.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: book, PostingDate: date, Period: period, Description: description,
			Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: cents}, {Account: "4000", CreditCents: cents}},
		})
		if err != nil {
			t.Fatalf("create %s: %v", description, err)
		}
		if _, err := ledgers.PostJournal(ctx, journal.ID); err != nil {
			t.Fatalf("post %s: %v", description, err)
		}
	}
	postSale("PARENT", "2026-06-10", "2026-06", "Parent sale", 2500)
	postSale("HOLDCO", "2026-06-20", "2026-06", "Holding sale", 5000)
	postSale("SUB", "2026-06-25", "2026-06", "Pre-consolidation subsidiary sale", 10000)
	postSale("SUB", "2026-07-10", "2026-07", "Consolidated subsidiary sale", 20000)
	postSale("SUB", "2026-07-20", "2026-07", "Post-consolidation subsidiary sale", 30000)
	return &fixture{store: store, reports: report.NewService(store), ledger: ledgers}
}

func findStatementRow(t *testing.T, rows []report.StatementRow, code string) report.StatementRow {
	t.Helper()
	for _, row := range rows {
		if row.Account.Code == code {
			return row
		}
	}
	t.Fatalf("statement row %s not found", code)
	return report.StatementRow{}
}

func findLedgerAccount(t *testing.T, rows []report.GeneralLedgerAccount, code string) report.GeneralLedgerAccount {
	t.Helper()
	for _, row := range rows {
		if row.Account.Code == code {
			return row
		}
	}
	t.Fatalf("ledger account %s not found", code)
	return report.GeneralLedgerAccount{}
}

func findTrialBalanceRow(t *testing.T, rows []report.TrialBalanceRow, code string) report.TrialBalanceRow {
	t.Helper()
	for _, row := range rows {
		if row.Account.Code == code {
			return row
		}
	}
	t.Fatalf("trial-balance row %s not found", code)
	return report.TrialBalanceRow{}
}

func findEntityAmount(t *testing.T, amount report.Breakdown, code string) int64 {
	t.Helper()
	for _, entity := range amount.ByEntity {
		if entity.EntityCode == code {
			return entity.Cents
		}
	}
	t.Fatalf("entity amount %s not found in %#v", code, amount.ByEntity)
	return 0
}

func findScopedBook(t *testing.T, scope report.ResolvedScope, entityCode string) report.ScopedBook {
	t.Helper()
	for _, book := range scope.Books {
		if book.EntityCode == entityCode {
			return book
		}
	}
	t.Fatalf("scoped book for %s not found in %#v", entityCode, scope.Books)
	return report.ScopedBook{}
}

func assertGeneralLedgerClosesToTrialBalance(t *testing.T, gl report.GeneralLedgerReport, tb report.TrialBalanceReport) {
	t.Helper()
	trialBalances := make(map[string]int64, len(tb.Rows))
	for _, row := range tb.Rows {
		trialBalances[row.Account.ID] = row.Balance.ConsolidatedCents
	}
	ledgerAccounts := make(map[string]bool, len(gl.Accounts))
	for _, account := range gl.Accounts {
		ledgerAccounts[account.Account.ID] = true
		if got, want := account.ClosingBalance.ConsolidatedCents, trialBalances[account.Account.ID]; got != want {
			t.Fatalf("GL closing %s = %d, trial-balance amount = %d", account.Account.Code, got, want)
		}
	}
	for _, row := range tb.Rows {
		if row.Balance.ConsolidatedCents != 0 && !ledgerAccounts[row.Account.ID] {
			t.Fatalf("trial-balance account %s with %d cents is absent from GL", row.Account.Code, row.Balance.ConsolidatedCents)
		}
	}
}

func TestEntityReportsUsePostedExactCentBalances(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	scope := report.Scope{EntityCode: "northstar"}

	gl, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31", AccountCode: "1000",
	})
	if err != nil {
		t.Fatalf("general ledger: %v", err)
	}
	cash := findLedgerAccount(t, gl.Accounts, "1000")
	if got, want := cash.OpeningBalance.ConsolidatedCents, int64(300000); got != want {
		t.Fatalf("opening cash = %d, want %d", got, want)
	}
	if len(cash.Lines) != 2 {
		t.Fatalf("cash lines = %d, want 2", len(cash.Lines))
	}
	if got, want := cash.Lines[0].RunningBalanceCents, int64(1300000); got != want {
		t.Fatalf("first running balance = %d, want %d", got, want)
	}
	if got, want := cash.ClosingBalance.ConsolidatedCents, int64(1050000); got != want {
		t.Fatalf("closing cash = %d, want %d", got, want)
	}

	pl, err := fx.reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("profit and loss: %v", err)
	}
	if got, want := pl.TotalRevenue.ConsolidatedCents, int64(1010000); got != want {
		t.Fatalf("revenue = %d, want %d", got, want)
	}
	if got, want := pl.TotalExpenses.ConsolidatedCents, int64(250000); got != want {
		t.Fatalf("expenses = %d, want %d", got, want)
	}
	if got, want := pl.NetIncome.ConsolidatedCents, int64(760000); got != want {
		t.Fatalf("net income = %d, want %d", got, want)
	}
	if got := findStatementRow(t, pl.Revenue, "4000").Amount.ConsolidatedCents; got != 1010000 {
		t.Fatalf("revenue row = %d, want 1010000", got)
	}

	bs, err := fx.reports.BalanceSheet(ctx, report.BalanceSheetInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("balance sheet: %v", err)
	}
	if got, want := bs.TotalAssets.ConsolidatedCents, int64(1060000); got != want {
		t.Fatalf("assets = %d, want %d", got, want)
	}
	if got, want := bs.CurrentEarnings.ConsolidatedCents, int64(760000); got != want {
		t.Fatalf("current earnings = %d, want %d", got, want)
	}
	if got, want := bs.TotalEquity.ConsolidatedCents, int64(1060000); got != want {
		t.Fatalf("total equity = %d, want %d", got, want)
	}
}

func TestConsolidatedReportsExposeEntitiesEliminationsAndConsolidated(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	scope := report.Scope{GroupCode: "NORTHSTAR-GROUP"}

	resolvedBefore, err := fx.reports.ResolveScope(ctx, scope, "2026-06-30")
	if err != nil {
		t.Fatalf("resolve June group: %v", err)
	}
	if len(resolvedBefore.Books) != 2 || resolvedBefore.Books[0].EntityCode != "NORTHSTAR" || resolvedBefore.Books[1].Kind != "ELIMINATION" {
		t.Fatalf("June books = %#v, want Northstar plus eliminations", resolvedBefore.Books)
	}
	resolved, err := fx.reports.ResolveScope(ctx, scope, "2026-07-31")
	if err != nil {
		t.Fatalf("resolve July group: %v", err)
	}
	if len(resolved.Books) != 3 {
		t.Fatalf("July books = %d, want 3", len(resolved.Books))
	}

	pl, err := fx.reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("group profit and loss: %v", err)
	}
	if got, want := pl.TotalRevenue.ConsolidatedCents, int64(3000000); got != want {
		t.Fatalf("group revenue = %d, want %d", got, want)
	}
	if got, want := pl.TotalRevenue.EliminationsCents, int64(-10000); got != want {
		t.Fatalf("revenue eliminations = %d, want %d", got, want)
	}
	if len(pl.TotalRevenue.ByEntity) != 2 || findEntityAmount(t, pl.TotalRevenue, "NORTHSTAR") != 1010000 || findEntityAmount(t, pl.TotalRevenue, "ACME") != 2000000 {
		t.Fatalf("entity revenue = %#v", pl.TotalRevenue.ByEntity)
	}
	if got, want := pl.TotalExpenses.ConsolidatedCents, int64(650000); got != want {
		t.Fatalf("group expenses = %d, want %d", got, want)
	}
	if got, want := pl.NetIncome.ConsolidatedCents, int64(2350000); got != want {
		t.Fatalf("group net income = %d, want %d", got, want)
	}

	tb, err := fx.reports.TrialBalance(ctx, report.TrialBalanceInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("group trial balance: %v", err)
	}
	if tb.TotalDebitCents != 3300000 || tb.TotalCreditCents != 3300000 {
		t.Fatalf("trial balance totals = %d/%d, want 3300000/3300000", tb.TotalDebitCents, tb.TotalCreditCents)
	}

	bs, err := fx.reports.BalanceSheet(ctx, report.BalanceSheetInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("group balance sheet: %v", err)
	}
	if got, want := bs.TotalAssets.ConsolidatedCents, int64(2650000); got != want {
		t.Fatalf("assets = %d, want %d", got, want)
	}
	if got := bs.TotalLiabilities.ConsolidatedCents; got != 0 {
		t.Fatalf("liabilities = %d, want 0", got)
	}
	if got, want := bs.TotalEquity.ConsolidatedCents, int64(2650000); got != want {
		t.Fatalf("equity = %d, want %d", got, want)
	}
}

func TestConsolidatedRangeReportsExcludePreOwnershipActivity(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	journal, err := fx.ledger.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-06-30", Period: "2026-06", Description: "Acme pre-ownership sale",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 600000}, {Account: "4000", CreditCents: 600000}},
	})
	if err != nil {
		t.Fatalf("create pre-ownership journal: %v", err)
	}
	if _, err := fx.ledger.PostJournal(ctx, journal.ID); err != nil {
		t.Fatalf("post pre-ownership journal: %v", err)
	}

	scope := report.Scope{GroupCode: "NORTHSTAR-GROUP"}
	pl, err := fx.reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: scope, FromDate: "2026-06-01", ToDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("range profit and loss: %v", err)
	}
	if got, want := pl.TotalRevenue.ConsolidatedCents, int64(3000000); got != want {
		t.Fatalf("range revenue = %d, want %d; pre-ownership revenue must be excluded", got, want)
	}
	if got, want := findEntityAmount(t, pl.TotalRevenue, "ACME"), int64(2000000); got != want {
		t.Fatalf("Acme range revenue = %d, want %d", got, want)
	}

	gl, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31", AccountCode: "1000",
	})
	if err != nil {
		t.Fatalf("range general ledger: %v", err)
	}
	cash := findLedgerAccount(t, gl.Accounts, "1000")
	if got, want := cash.OpeningBalance.ConsolidatedCents, int64(300000); got != want {
		t.Fatalf("range opening cash = %d, want %d; pre-ownership cash must be excluded", got, want)
	}
	if got, want := cash.ClosingBalance.ConsolidatedCents, int64(3250000); got != want {
		t.Fatalf("range closing cash = %d, want %d", got, want)
	}
	var contribution *report.GeneralLedgerLine
	for index := range cash.Lines {
		if cash.Lines[index].SyntheticKind == "CONSOLIDATION_PERIMETER_ENTRY" {
			contribution = &cash.Lines[index]
			break
		}
	}
	if contribution == nil {
		t.Fatal("consolidation-perimeter entry not found")
	}
	if !contribution.Synthetic || contribution.EntityCode != "ACME" || contribution.PostingDate != "2026-07-01" || contribution.DebitCents != 600000 {
		t.Fatalf("consolidation-perimeter entry = %#v", contribution)
	}

	// An as-of balance sheet and trial balance include the complete balance of an
	// entity that is a member on that date. The synthetic GL contribution makes
	// that pre-ownership balance visible without treating it as period income.
	bs, err := fx.reports.BalanceSheet(ctx, report.BalanceSheetInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("as-of balance sheet: %v", err)
	}
	if got, want := bs.TotalAssets.ConsolidatedCents, int64(3250000); got != want {
		t.Fatalf("as-of assets = %d, want %d; current member's complete balance must be included", got, want)
	}
	tb, err := fx.reports.TrialBalance(ctx, report.TrialBalanceInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("as-of trial balance: %v", err)
	}
	if got, want := cash.ClosingBalance.ConsolidatedCents, findTrialBalanceRow(t, tb.Rows, "1000").Balance.ConsolidatedCents; got != want {
		t.Fatalf("GL closing cash = %d, trial-balance cash = %d", got, want)
	}
	fullGL, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("full range general ledger: %v", err)
	}
	assertGeneralLedgerClosesToTrialBalance(t, fullGL, tb)
}

func TestConsolidatedRangeReportsIncludeOnlyActivityThroughOwnershipEnd(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	if _, err := fx.store.DB().ExecContext(ctx, `UPDATE ownership_interests SET effective_to = '2026-07-15'
		WHERE parent_entity_id = (SELECT id FROM entities WHERE code = 'NORTHSTAR')
		  AND child_entity_id = (SELECT id FROM entities WHERE code = 'ACME')`); err != nil {
		t.Fatalf("end Northstar ownership of Acme: %v", err)
	}

	scope := report.Scope{GroupCode: "NORTHSTAR-GROUP"}
	pl, err := fx.reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("range profit and loss: %v", err)
	}
	if got, want := findEntityAmount(t, pl.TotalRevenue, "ACME"), int64(2000000); got != want {
		t.Fatalf("Acme revenue through ownership end = %d, want %d", got, want)
	}
	if got := findEntityAmount(t, pl.TotalExpenses, "ACME"); got != 0 {
		t.Fatalf("Acme expense after ownership end = %d, want 0", got)
	}
	if got, want := pl.NetIncome.ConsolidatedCents, int64(2760000); got != want {
		t.Fatalf("range net income = %d, want %d", got, want)
	}

	gl, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{
		Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31", AccountCode: "1000",
	})
	if err != nil {
		t.Fatalf("range general ledger: %v", err)
	}
	cash := findLedgerAccount(t, gl.Accounts, "1000")
	if got, want := len(cash.Lines), 4; got != want {
		t.Fatalf("cash lines through ownership end = %d, want %d", got, want)
	}
	if got, want := cash.ClosingBalance.ConsolidatedCents, int64(1050000); got != want {
		t.Fatalf("range closing cash = %d, want %d", got, want)
	}
	var removal *report.GeneralLedgerLine
	for index := range cash.Lines {
		if cash.Lines[index].SyntheticKind == "CONSOLIDATION_PERIMETER_EXIT" {
			removal = &cash.Lines[index]
			break
		}
	}
	if removal == nil {
		t.Fatal("consolidation-perimeter exit line not found")
	}
	if !removal.Synthetic || removal.EntityCode != "ACME" || removal.PostingDate != "2026-07-16" || removal.CreditCents != 2000000 {
		t.Fatalf("consolidation-perimeter exit = %#v", removal)
	}
	tb, err := fx.reports.TrialBalance(ctx, report.TrialBalanceInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("as-of trial balance: %v", err)
	}
	if got, want := cash.ClosingBalance.ConsolidatedCents, findTrialBalanceRow(t, tb.Rows, "1000").Balance.ConsolidatedCents; got != want {
		t.Fatalf("GL closing cash = %d, trial-balance cash = %d", got, want)
	}
	fullGL, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{Scope: scope, FromDate: "2026-07-01", ToDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("full range general ledger: %v", err)
	}
	assertGeneralLedgerClosesToTrialBalance(t, fullGL, tb)

	resolved, err := fx.reports.ResolveScope(ctx, scope, "2026-07-31")
	if err != nil {
		t.Fatalf("resolve group after ownership end: %v", err)
	}
	if len(resolved.Books) != 2 || resolved.Books[0].EntityCode != "NORTHSTAR" || resolved.Books[1].Kind != "ELIMINATION" {
		t.Fatalf("as-of books after ownership end = %#v, want Northstar plus eliminations", resolved.Books)
	}
}

func TestConsolidationPerimeterRecursivelyIntersectsNestedOwnershipDates(t *testing.T) {
	ctx := context.Background()
	fx := newNestedOwnershipFixture(t)
	scope := report.Scope{GroupCode: "PARENT-GROUP"}

	for _, check := range []struct {
		date     string
		entities []string
	}{
		{date: "2025-12-31", entities: []string{"PARENT"}},
		{date: "2026-06-30", entities: []string{"HOLDCO", "PARENT"}},
		{date: "2026-07-10", entities: []string{"HOLDCO", "PARENT", "SUB"}},
		{date: "2026-07-31", entities: []string{"PARENT"}},
	} {
		resolved, err := fx.reports.ResolveScope(ctx, scope, check.date)
		if err != nil {
			t.Fatalf("resolve group at %s: %v", check.date, err)
		}
		var entities []string
		for _, book := range resolved.Books {
			if book.Kind == "ACTUAL" {
				entities = append(entities, book.EntityCode)
			}
		}
		if len(entities) != len(check.entities) {
			t.Fatalf("entities at %s = %v, want %v", check.date, entities, check.entities)
		}
		for index := range entities {
			if entities[index] != check.entities[index] {
				t.Fatalf("entities at %s = %v, want %v", check.date, entities, check.entities)
			}
		}
	}

	pl, err := fx.reports.ProfitLoss(ctx, report.ProfitLossInput{
		Scope: scope, FromDate: "2026-06-01", ToDate: "2026-07-31",
	})
	if err != nil {
		t.Fatalf("nested-ownership profit and loss: %v", err)
	}
	if got, want := pl.TotalRevenue.ConsolidatedCents, int64(27500); got != want {
		t.Fatalf("nested-ownership revenue = %d, want %d", got, want)
	}
	for entity, want := range map[string]int64{"PARENT": 2500, "HOLDCO": 5000, "SUB": 20000} {
		if got := findEntityAmount(t, pl.TotalRevenue, entity); got != want {
			t.Fatalf("%s range revenue = %d, want %d", entity, got, want)
		}
	}
	holding := findScopedBook(t, pl.Scope, "HOLDCO")
	if len(holding.ConsolidationIntervals) != 1 || holding.ConsolidationIntervals[0].EffectiveFrom != "2026-01-01" || holding.ConsolidationIntervals[0].EffectiveTo != "2026-07-15" {
		t.Fatalf("holding consolidation intervals = %#v", holding.ConsolidationIntervals)
	}
	subsidiary := findScopedBook(t, pl.Scope, "SUB")
	if len(subsidiary.ConsolidationIntervals) != 1 || subsidiary.ConsolidationIntervals[0].EffectiveFrom != "2026-07-01" || subsidiary.ConsolidationIntervals[0].EffectiveTo != "2026-07-15" {
		t.Fatalf("subsidiary intersected intervals = %#v", subsidiary.ConsolidationIntervals)
	}

	gl, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{
		Scope: scope, FromDate: "2026-06-01", ToDate: "2026-07-31", AccountCode: "1000",
	})
	if err != nil {
		t.Fatalf("nested-ownership general ledger: %v", err)
	}
	cash := findLedgerAccount(t, gl.Accounts, "1000")
	if got, want := cash.ClosingBalance.ConsolidatedCents, int64(2500); got != want {
		t.Fatalf("nested-ownership closing cash = %d, want %d", got, want)
	}
	var subsidiaryEntry, subsidiaryExit bool
	for _, line := range cash.Lines {
		if line.EntityCode != "SUB" {
			continue
		}
		if line.SyntheticKind == "CONSOLIDATION_PERIMETER_ENTRY" && line.PostingDate == "2026-07-01" && line.DebitCents == 10000 {
			subsidiaryEntry = true
		}
		if line.SyntheticKind == "CONSOLIDATION_PERIMETER_EXIT" && line.PostingDate == "2026-07-16" && line.CreditCents == 30000 {
			subsidiaryExit = true
		}
	}
	if !subsidiaryEntry || !subsidiaryExit {
		t.Fatalf("subsidiary boundary lines missing: entry=%t exit=%t, lines=%#v", subsidiaryEntry, subsidiaryExit, cash.Lines)
	}
	tb, err := fx.reports.TrialBalance(ctx, report.TrialBalanceInput{Scope: scope, AsOfDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("nested-ownership trial balance: %v", err)
	}
	fullGL, err := fx.reports.GeneralLedger(ctx, report.GeneralLedgerInput{Scope: scope, FromDate: "2026-06-01", ToDate: "2026-07-31"})
	if err != nil {
		t.Fatalf("full nested-ownership general ledger: %v", err)
	}
	assertGeneralLedgerClosesToTrialBalance(t, fullGL, tb)
}

func TestReportsRejectInvalidScopeAndLedgerIntegrityBreach(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	_, err := fx.reports.TrialBalance(ctx, report.TrialBalanceInput{
		Scope: report.Scope{EntityCode: "NORTHSTAR", GroupCode: "NORTHSTAR-GROUP"}, AsOfDate: "2026-07-31",
	})
	if appError, ok := apperr.As(err); !ok || appError.Code != "REPORT_SCOPE_INVALID" {
		t.Fatalf("invalid scope error = %#v, want REPORT_SCOPE_INVALID", err)
	}

	if _, err := fx.store.DB().ExecContext(ctx, "DROP TRIGGER journal_lines_validate_update"); err != nil {
		t.Fatalf("drop test protection trigger: %v", err)
	}
	if _, err := fx.store.DB().ExecContext(ctx, `UPDATE journal_lines SET debit_cents = debit_cents + 1
		WHERE id = (SELECT jl.id FROM journal_lines jl JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE je.status = 'POSTED' AND jl.debit_cents > 0 LIMIT 1)`); err != nil {
		t.Fatalf("corrupt test ledger: %v", err)
	}
	_, err = fx.reports.BalanceSheet(ctx, report.BalanceSheetInput{
		Scope: report.Scope{GroupCode: "NORTHSTAR-GROUP"}, AsOfDate: "2026-07-31",
	})
	if appError, ok := apperr.As(err); !ok || appError.Kind != apperr.Integrity {
		t.Fatalf("integrity error = %#v, want integrity application error", err)
	}
}
