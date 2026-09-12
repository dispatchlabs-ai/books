package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type PeriodCloseResult struct {
	BookCode       string   `json:"book"`
	PeriodCode     string   `json:"period"`
	EndDate        string   `json:"end_date"`
	Digest         string   `json:"digest,omitempty"`
	Closed         bool     `json:"closed"`
	ValidationOnly bool     `json:"validation_only"`
	Errors         []string `json:"errors"`
}

func invalidPrecoverageIdentityBindingsForBook(ctx context.Context, q queryer, bookID string) (int, error) {
	rows, err := q.QueryContext(ctx, `SELECT valid.id, valid.statement_account_id,
		binding.active_identity_count, binding.active_identity_digest
		FROM valid_statement_account_precoverage_closures valid
		JOIN statement_account_precoverage_identity_bindings binding ON binding.closure_id = valid.id
		JOIN statement_accounts sa ON sa.id = valid.statement_account_id
		WHERE sa.book_id = ?
		ORDER BY valid.id`, bookID)
	if err != nil {
		return 0, storesqlite.MapError("read precoverage identity bindings", err)
	}
	type binding struct {
		closureID, statementAccountID, digest string
		count                                 int
	}
	var bindings []binding
	for rows.Next() {
		var candidate binding
		if err := rows.Scan(&candidate.closureID, &candidate.statementAccountID, &candidate.count, &candidate.digest); err != nil {
			_ = rows.Close()
			return 0, storesqlite.MapError("scan precoverage identity binding", err)
		}
		bindings = append(bindings, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, storesqlite.MapError("read precoverage identity bindings", err)
	}
	if err := rows.Close(); err != nil {
		return 0, storesqlite.MapError("close precoverage identity bindings", err)
	}
	invalid := 0
	for _, binding := range bindings {
		count, digest, err := storesqlite.ActiveStatementAccountIdentityDigest(ctx, q, binding.statementAccountID)
		if err != nil {
			return 0, err
		}
		if count != binding.count || digest != binding.digest {
			invalid++
		}
	}
	return invalid, nil
}

func closePreflight(ctx context.Context, q queryer, bookID, periodID string) (string, []string, error) {
	var startDate, endDate, status string
	err := q.QueryRowContext(ctx, `SELECT fp.start_date, fp.end_date, bp.status
        FROM fiscal_periods fp JOIN book_periods bp ON bp.period_id = fp.id
        WHERE fp.id = ? AND bp.book_id = ?`, periodID, bookID).Scan(&startDate, &endDate, &status)
	if err == sql.ErrNoRows {
		return "", nil, apperr.New(apperr.NotFound, "BOOK_PERIOD_NOT_FOUND", "period is not configured for this book")
	}
	if err != nil {
		return "", nil, storesqlite.MapError("read book period", err)
	}
	if _, err := storesqlite.VerifyAudit(ctx, q); err != nil {
		return "", nil, err
	}
	var errorsFound []string
	if status == "CLOSED" {
		errorsFound = append(errorsFound, "period is already closed")
	}
	var activeBook int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM books WHERE id = ? AND status = 'ACTIVE'`, bookID).Scan(&activeBook); err != nil {
		return "", nil, err
	}
	if activeBook != 1 {
		errorsFound = append(errorsFound, "period book is not active")
	}
	var earlierOpen int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_periods earlier
		JOIN fiscal_periods earlier_period ON earlier_period.id = earlier.period_id
		WHERE earlier.book_id = ? AND earlier.status = 'OPEN' AND earlier_period.end_date < ?`, bookID, startDate).Scan(&earlierOpen); err != nil {
		return "", nil, err
	}
	if earlierOpen != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d earlier fiscal period(s) remain open", earlierOpen))
	}
	var laterClosed int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_periods later
		JOIN fiscal_periods later_period ON later_period.id = later.period_id
		WHERE later.book_id = ? AND later.status = 'CLOSED' AND later_period.start_date > ?`, bookID, endDate).Scan(&laterClosed); err != nil {
		return "", nil, err
	}
	if laterClosed != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d later fiscal period(s) are already closed", laterClosed))
	}
	var draftCount int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries
        WHERE book_id = ? AND status = 'DRAFT' AND posting_date BETWEEN ? AND ?`, bookID, startDate, endDate).Scan(&draftCount); err != nil {
		return "", nil, err
	}
	if draftCount != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d unresolved draft journal(s) remain in the period", draftCount))
	}
	var debits, credits int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(jl.debit_cents), 0), COALESCE(SUM(jl.credit_cents), 0)
        FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
        WHERE je.book_id = ? AND je.status = 'POSTED' AND je.posting_date <= ?`, bookID, endDate).Scan(&debits, &credits); err != nil {
		return "", nil, err
	}
	if debits != credits {
		errorsFound = append(errorsFound, "trial balance is not balanced")
	}
	var unclosedProfitLoss int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT a.id
		FROM fiscal_periods current_period
		JOIN fiscal_periods year_period ON year_period.fiscal_year = current_period.fiscal_year
		JOIN journal_entries je ON je.period_id = year_period.id
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE current_period.id = ? AND current_period.is_year_end = 1
		  AND je.book_id = ? AND je.status = 'POSTED'
		  AND a.account_type IN ('REVENUE', 'EXPENSE')
		GROUP BY a.id HAVING SUM(jl.debit_cents - jl.credit_cents) <> 0
	)`, periodID, bookID).Scan(&unclosedProfitLoss); err != nil {
		return "", nil, err
	}
	if unclosedProfitLoss != 0 {
		errorsFound = append(errorsFound, "fiscal-year profit-and-loss balances require an active closing journal")
	}
	var invalidPrecoverageLifecycles int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM statement_account_precoverage_closures closure
		JOIN statement_accounts sa ON sa.id = closure.statement_account_id
		LEFT JOIN valid_statement_account_precoverage_closures valid ON valid.id = closure.id
		WHERE sa.book_id = ? AND valid.id IS NULL`, bookID).Scan(&invalidPrecoverageLifecycles); err != nil {
		return "", nil, storesqlite.MapError("check precoverage lifecycle validity", err)
	}
	if invalidPrecoverageLifecycles != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d invalid precoverage statement-account lifecycle(s) block period close", invalidPrecoverageLifecycles))
	}
	invalidIdentityBindings, err := invalidPrecoverageIdentityBindingsForBook(ctx, q, bookID)
	if err != nil {
		return "", nil, err
	}
	if invalidIdentityBindings != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d precoverage identity binding(s) no longer match the complete active identity set", invalidIdentityBindings))
	}
	var unreconciled int
	if err := q.QueryRowContext(ctx, `WITH required AS (
		SELECT sa.id,
			MAX(sa.reconciliation_required_from, ?) AS required_start,
			MIN(COALESCE(sa.reconciliation_required_through, ?), ?) AS required_end
		FROM statement_accounts sa
		WHERE sa.book_id = ? AND sa.required_for_close = 1
		  AND sa.reconciliation_required_from <= ? AND COALESCE(sa.reconciliation_required_through, ?) >= ?
		  AND NOT EXISTS (
		      SELECT 1 FROM valid_statement_account_precoverage_closures valid
		      WHERE valid.statement_account_id = sa.id
		  )
	)
	SELECT COUNT(*) FROM required
		WHERE COALESCE((
			SELECT SUM(CAST(
				julianday(MIN(r.end_date, required.required_end)) -
				julianday(MAX(r.start_date, required.required_start)) + 1
				AS INTEGER))
			FROM reconciliations r
			JOIN reconciliation_status status ON status.reconciliation_id = r.id
			WHERE r.statement_account_id = required.id
			  AND r.status = 'COMPLETED'
			  AND r.beginning_balance_cents + status.statement_activity_cents = r.ending_balance_cents
			  AND status.ledger_beginning_balance_cents = r.beginning_balance_cents
			  AND status.ledger_ending_balance_cents = r.ending_balance_cents
			  AND status.fully_allocated_statement_count = status.statement_transaction_count
			  AND status.fully_allocated_control_line_count = status.control_line_count
			  AND r.end_date >= required.required_start
			  AND r.start_date <= required.required_end
		  ), 0) <> CAST(julianday(required.required_end) - julianday(required.required_start) + 1 AS INTEGER)`,
		startDate, endDate, endDate, bookID, endDate, endDate, startDate).Scan(&unreconciled); err != nil {
		return "", nil, err
	}
	if unreconciled != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d required statement account(s) are not reconciled through period end", unreconciled))
	}
	var suspense int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
        SELECT a.id FROM accounts a JOIN book_accounts ba ON ba.account_id = a.id AND ba.book_id = ?
        JOIN journal_lines jl ON jl.account_id = a.id JOIN journal_entries je ON je.id = jl.journal_entry_id
        WHERE a.subtype = 'SUSPENSE' AND je.book_id = ? AND je.status = 'POSTED' AND je.posting_date <= ?
        GROUP BY a.id HAVING SUM(jl.debit_cents - jl.credit_cents) <> 0
    )`, bookID, bookID, endDate).Scan(&suspense); err != nil {
		return "", nil, err
	}
	if suspense != 0 {
		errorsFound = append(errorsFound, fmt.Sprintf("%d suspense account(s) have nonzero balances", suspense))
	}
	return endDate, errorsFound, nil
}

func (s *Service) ClosePeriod(ctx context.Context, bookCode, periodCode string, dryRun bool) (PeriodCloseResult, error) {
	return s.closePeriod(ctx, bookCode, periodCode, dryRun, "", "")
}

// ClosePeriodFromPlan compares the reviewed end date and ledger digest inside
// the same write transaction that records the close. A stale plan never closes
// the period.
func (s *Service) ClosePeriodFromPlan(ctx context.Context, bookCode, periodCode, expectedEndDate, expectedDigest string) (PeriodCloseResult, error) {
	if strings.TrimSpace(expectedEndDate) == "" || strings.TrimSpace(expectedDigest) == "" {
		return PeriodCloseResult{}, apperr.New(apperr.Invalid, "CLOSE_EXPECTATION_REQUIRED", "reviewed close end date and digest are required")
	}
	return s.closePeriod(ctx, bookCode, periodCode, false, expectedEndDate, expectedDigest)
}

func (s *Service) closePeriod(ctx context.Context, bookCode, periodCode string, dryRun bool, expectedEndDate, expectedDigest string) (PeriodCloseResult, error) {
	if err := s.requireActor(); err != nil {
		return PeriodCloseResult{}, err
	}
	result := PeriodCloseResult{BookCode: normalizeCode(bookCode), PeriodCode: normalizeCode(periodCode), ValidationOnly: dryRun, Errors: []string{}}
	if dryRun {
		bookID, err := lookupID(ctx, s.store.DB(), "books", result.BookCode)
		if err != nil {
			return result, err
		}
		periodID, err := lookupID(ctx, s.store.DB(), "fiscal_periods", result.PeriodCode)
		if err != nil {
			return result, err
		}
		result.EndDate, result.Errors, err = closePreflight(ctx, s.store.DB(), bookID, periodID)
		if err != nil {
			return result, err
		}
		if len(result.Errors) != 0 {
			return result, apperr.New(apperr.Validation, "PERIOD_CLOSE_BLOCKED", strings.Join(result.Errors, "; "))
		}
		result.Digest, err = storesqlite.ComputeBookDigest(ctx, s.store.DB(), bookID, result.EndDate)
		return result, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	bookID, err := lookupID(ctx, tx, "books", result.BookCode)
	if err != nil {
		return result, err
	}
	periodID, err := lookupID(ctx, tx, "fiscal_periods", result.PeriodCode)
	if err != nil {
		return result, err
	}
	if expectedDigest != "" {
		var currentStatus, currentEndDate, currentDigest string
		if err := tx.QueryRowContext(ctx, `SELECT bp.status, fp.end_date, COALESCE(bp.close_digest, '')
			FROM book_periods bp JOIN fiscal_periods fp ON fp.id = bp.period_id
			WHERE bp.book_id = ? AND bp.period_id = ?`, bookID, periodID).Scan(&currentStatus, &currentEndDate, &currentDigest); err != nil {
			return result, storesqlite.MapError("read reviewed period close state", err)
		}
		if currentStatus == "CLOSED" {
			if currentEndDate != expectedEndDate || currentDigest != expectedDigest {
				return result, apperr.New(apperr.Conflict, "CLOSE_PLAN_ALREADY_CLOSED", "period is already closed with evidence that does not match this plan")
			}
			if _, err := storesqlite.VerifyAudit(ctx, tx); err != nil {
				return result, err
			}
			result.EndDate = currentEndDate
			result.Digest = currentDigest
			result.Closed = true
			return result, nil
		}
	}
	result.EndDate, result.Errors, err = closePreflight(ctx, tx, bookID, periodID)
	if err != nil {
		return result, err
	}
	if len(result.Errors) != 0 {
		return result, apperr.New(apperr.Validation, "PERIOD_CLOSE_BLOCKED", strings.Join(result.Errors, "; "))
	}
	result.Digest, err = storesqlite.ComputeBookDigest(ctx, tx, bookID, result.EndDate)
	if err != nil {
		return result, err
	}
	if expectedDigest != "" && (result.EndDate != expectedEndDate || result.Digest != expectedDigest) {
		return result, apperr.New(apperr.Conflict, "CLOSE_PLAN_STALE", "ledger content changed after planning; generate and review a new close plan")
	}
	now := storesqlite.UTCNow()
	updated, err := tx.ExecContext(ctx, `UPDATE book_periods SET status = 'CLOSED', closed_at = ?, closed_by = ?,
        close_digest = ? WHERE book_id = ? AND period_id = ? AND status = 'OPEN'`,
		now, s.actor, result.Digest, bookID, periodID)
	if err != nil {
		return result, storesqlite.MapError("close period", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return result, apperr.New(apperr.Conflict, "PERIOD_NOT_OPEN", "book period is no longer open")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "period close", AggregateType: "book_period", AggregateID: bookID + ":" + periodID,
		Payload: map[string]any{"book": result.BookCode, "period": result.PeriodCode, "end_date": result.EndDate, "digest": result.Digest},
	}); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, storesqlite.MapError("commit period close", err)
	}
	result.Closed = true
	return result, nil
}

func (s *Service) ReopenPeriod(ctx context.Context, bookCode, periodCode, reason string) error {
	if err := s.requireActor(); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apperr.New(apperr.Invalid, "REOPEN_REASON_REQUIRED", "a reason is required to reopen a period")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	bookID, err := lookupID(ctx, tx, "books", bookCode)
	if err != nil {
		return err
	}
	periodID, err := lookupID(ctx, tx, "fiscal_periods", periodCode)
	if err != nil {
		return err
	}
	var laterClosed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM book_periods current
		JOIN fiscal_periods current_period ON current_period.id = current.period_id
		JOIN fiscal_periods later_period ON later_period.start_date > current_period.end_date
		JOIN book_periods later ON later.period_id = later_period.id AND later.book_id = current.book_id
		WHERE current.book_id = ? AND current.period_id = ? AND current.status = 'CLOSED'
		  AND later.status = 'CLOSED'`, bookID, periodID).Scan(&laterClosed); err != nil {
		return storesqlite.MapError("check period reopen order", err)
	}
	if laterClosed != 0 {
		return apperr.New(apperr.Conflict, "PERIOD_REOPEN_ORDER", "later closed periods must be reopened first")
	}
	result, err := tx.ExecContext(ctx, `UPDATE book_periods SET status = 'OPEN', reopened_at = ?, reopened_by = ?, reopen_reason = ?
	        WHERE book_id = ? AND period_id = ? AND status = 'CLOSED'`, storesqlite.UTCNow(), s.actor, reason, bookID, periodID)
	if err != nil {
		return storesqlite.MapError("reopen period", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return apperr.New(apperr.Conflict, "PERIOD_NOT_CLOSED", "book period is not closed")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "period reopen", AggregateType: "book_period", AggregateID: bookID + ":" + periodID,
		Payload: map[string]any{"reason": reason},
	}); err != nil {
		return err
	}
	return tx.Commit()
}
