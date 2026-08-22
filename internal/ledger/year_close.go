package ledger

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type FiscalYearCloseInput struct {
	Book             string `json:"book"`
	FiscalYear       int    `json:"fiscal_year"`
	RetainedEarnings string `json:"retained_earnings_account"`
}

type FiscalYearCloseResult struct {
	Input      CreateJournalInput `json:"journal_input"`
	NetIncome  int64              `json:"net_income_cents"`
	Journal    *Journal           `json:"journal,omitempty"`
	Validation *JournalValidation `json:"validation,omitempty"`
	DryRun     bool               `json:"dry_run"`
}

// PrepareFiscalYearClose derives an exact close from posted STANDARD journals.
// It does not write the database; PostFiscalYearClose performs the audited write.
func (s *Service) PrepareFiscalYearClose(ctx context.Context, input FiscalYearCloseInput) (FiscalYearCloseResult, error) {
	input.Book = normalizeCode(input.Book)
	input.RetainedEarnings = normalizeCode(input.RetainedEarnings)
	if input.Book == "" || input.FiscalYear < 1900 || input.RetainedEarnings == "" {
		return FiscalYearCloseResult{}, apperr.New(apperr.Invalid, "FISCAL_YEAR_CLOSE_INVALID", "book, fiscal year, and retained-earnings account are required")
	}
	var bookID, periodCode, postingDate string
	err := s.store.DB().QueryRowContext(ctx, `SELECT b.id, fp.code, fp.end_date
		FROM books b CROSS JOIN fiscal_periods fp
		WHERE b.code = ? AND fp.fiscal_year = ? AND fp.is_year_end = 1`,
		input.Book, input.FiscalYear).Scan(&bookID, &periodCode, &postingDate)
	if err == sql.ErrNoRows {
		return FiscalYearCloseResult{}, apperr.New(apperr.NotFound, "FISCAL_YEAR_CLOSE_PERIOD_NOT_FOUND", "book or marked year-end period was not found")
	}
	if err != nil {
		return FiscalYearCloseResult{}, storesqlite.MapError("read fiscal year close period", err)
	}
	var yearEndStatus string
	if err := s.store.DB().QueryRowContext(ctx, `SELECT bp.status
		FROM book_periods bp
		JOIN fiscal_periods fp ON fp.id = bp.period_id
		WHERE bp.book_id = ? AND fp.code = ?`, bookID, periodCode).Scan(&yearEndStatus); err != nil {
		return FiscalYearCloseResult{}, storesqlite.MapError("read fiscal year close book period", err)
	}
	if yearEndStatus != "OPEN" {
		return FiscalYearCloseResult{}, apperr.New(apperr.Validation, "FISCAL_YEAR_CLOSE_BLOCKED", "the fiscal year-end period must be open before planning its closing journal")
	}
	var earlierOpen int
	if err := s.store.DB().QueryRowContext(ctx, `SELECT COUNT(*)
		FROM fiscal_periods fp
		JOIN book_periods bp ON bp.period_id = fp.id AND bp.book_id = ?
		WHERE fp.fiscal_year = ? AND fp.end_date < ? AND bp.status <> 'CLOSED'`,
		bookID, input.FiscalYear, postingDate).Scan(&earlierOpen); err != nil {
		return FiscalYearCloseResult{}, storesqlite.MapError("validate earlier fiscal periods", err)
	}
	if earlierOpen != 0 {
		return FiscalYearCloseResult{}, apperr.New(apperr.Validation, "FISCAL_YEAR_CLOSE_BLOCKED", "all earlier fiscal-year periods must be closed before planning a closing journal")
	}
	var retainedType string
	if err := s.store.DB().QueryRowContext(ctx, `SELECT a.account_type
		FROM accounts a JOIN book_accounts ba ON ba.account_id = a.id
		WHERE ba.book_id = ? AND a.code = ?`, bookID, input.RetainedEarnings).Scan(&retainedType); err == sql.ErrNoRows {
		return FiscalYearCloseResult{}, apperr.New(apperr.NotFound, "RETAINED_EARNINGS_NOT_FOUND", "retained-earnings account is not enabled for the book")
	} else if err != nil {
		return FiscalYearCloseResult{}, storesqlite.MapError("read retained earnings account", err)
	}
	if retainedType != "EQUITY" {
		return FiscalYearCloseResult{}, apperr.New(apperr.Validation, "RETAINED_EARNINGS_INVALID", "retained-earnings account must be equity")
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT a.code, SUM(jl.debit_cents - jl.credit_cents) AS balance_cents
		FROM journal_entries je
		JOIN fiscal_periods current_period ON current_period.id = je.period_id
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.book_id = ? AND je.status = 'POSTED' AND je.kind = 'STANDARD'
		  AND current_period.fiscal_year = ? AND a.account_type IN ('REVENUE', 'EXPENSE')
		GROUP BY a.id, a.code HAVING balance_cents <> 0 ORDER BY a.code`, bookID, input.FiscalYear)
	if err != nil {
		return FiscalYearCloseResult{}, storesqlite.MapError("derive fiscal year close", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	journalInput := CreateJournalInput{
		Book: input.Book, Kind: "CLOSING", PostingDate: postingDate, Period: periodCode,
		Description: fmt.Sprintf("Close fiscal year %d to retained earnings", input.FiscalYear),
		Reference:   fmt.Sprintf("YEAR-CLOSE-%d", input.FiscalYear),
	}
	var operatingBalance int64
	for rows.Next() {
		var account string
		var balance int64
		if err := rows.Scan(&account, &balance); err != nil {
			return FiscalYearCloseResult{}, err
		}
		operatingBalance += balance
		line := JournalLineInput{Account: account, Description: fmt.Sprintf("Close %d balance", input.FiscalYear)}
		if balance > 0 {
			line.CreditCents = balance
		} else {
			line.DebitCents = -balance
		}
		journalInput.Lines = append(journalInput.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return FiscalYearCloseResult{}, err
	}
	if len(journalInput.Lines) == 0 {
		return FiscalYearCloseResult{}, apperr.New(apperr.Validation, "FISCAL_YEAR_ALREADY_ZERO", "fiscal year has no nonzero profit-and-loss balances to close")
	}
	if operatingBalance != 0 {
		equity := JournalLineInput{Account: input.RetainedEarnings, Description: fmt.Sprintf("Fiscal year %d net income", input.FiscalYear)}
		if operatingBalance > 0 {
			equity.DebitCents = operatingBalance
		} else {
			equity.CreditCents = -operatingBalance
		}
		journalInput.Lines = append(journalInput.Lines, equity)
	}
	return FiscalYearCloseResult{Input: journalInput, NetIncome: -operatingBalance}, nil
}

func (s *Service) PostFiscalYearClose(ctx context.Context, input FiscalYearCloseInput, dryRun bool) (FiscalYearCloseResult, error) {
	result, err := s.PrepareFiscalYearClose(ctx, input)
	if err != nil {
		return result, err
	}
	result.DryRun = dryRun
	if dryRun {
		return result, nil
	}
	posted, err := s.CreateAndPostJournal(ctx, result.Input)
	if err != nil {
		return result, err
	}
	if posted.Status == "ABANDONED" {
		return result, apperr.New(apperr.Conflict, "FISCAL_YEAR_CLOSE_ABANDONED", "the derived fiscal-year close was previously abandoned; correct the ledger before retrying")
	}
	if posted.Status != "POSTED" {
		return result, apperr.New(apperr.Integrity, "FISCAL_YEAR_CLOSE_NOT_POSTED", "the derived fiscal-year close did not reach posted state")
	}
	result.Journal = &posted
	return result, nil
}
