package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func normalizeJournalInput(input CreateJournalInput) CreateJournalInput {
	input.Book = normalizeCode(input.Book)
	input.Kind = normalizeCode(input.Kind)
	if input.Kind == "" {
		input.Kind = "STANDARD"
	}
	input.Period = normalizeCode(input.Period)
	input.Description = strings.TrimSpace(input.Description)
	input.Reference = strings.TrimSpace(input.Reference)
	input.SourceSystem = normalizeCode(input.SourceSystem)
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	input.TaxType = normalizeCode(input.TaxType)
	input.TaxAccountingPeriod = strings.TrimSpace(input.TaxAccountingPeriod)
	for i := range input.Lines {
		input.Lines[i].Account = normalizeCode(input.Lines[i].Account)
		input.Lines[i].Description = strings.TrimSpace(input.Lines[i].Description)
		input.Lines[i].CounterpartyEntity = normalizeCode(input.Lines[i].CounterpartyEntity)
		input.Lines[i].IntercompanyKey = strings.TrimSpace(input.Lines[i].IntercompanyKey)
	}
	return input
}

func validateJournalInput(input CreateJournalInput) error {
	if input.Book == "" || input.Period == "" || input.Description == "" {
		return apperr.New(apperr.Invalid, "JOURNAL_INVALID", "book, period, posting date, and description are required")
	}
	if err := validateDate(input.PostingDate, "posting date"); err != nil {
		return err
	}
	if input.Kind != "STANDARD" && input.Kind != "CLOSING" && input.Kind != "CLOSING_REVERSAL" {
		return apperr.New(apperr.Invalid, "JOURNAL_KIND_INVALID", "journal kind must be STANDARD, CLOSING, or CLOSING_REVERSAL")
	}
	if (input.SourceSystem == "") != (input.SourceKey == "") {
		return apperr.New(apperr.Invalid, "SOURCE_KEY_INVALID", "source system and source key must be provided together")
	}
	for i, line := range input.Lines {
		if line.Account == "" {
			return apperr.New(apperr.Invalid, "JOURNAL_LINE_INVALID", fmt.Sprintf("line %d account is required", i+1))
		}
		if line.DebitCents < 0 || line.CreditCents < 0 || (line.DebitCents > 0) == (line.CreditCents > 0) {
			return apperr.New(apperr.Invalid, "JOURNAL_LINE_INVALID", fmt.Sprintf("line %d must contain exactly one positive debit or credit", i+1))
		}
		if (line.CounterpartyEntity == "") != (line.IntercompanyKey == "") {
			return apperr.New(apperr.Invalid, "INTERCOMPANY_INVALID", fmt.Sprintf("line %d requires both counterparty entity and intercompany key", i+1))
		}
	}
	return nil
}

func (s *Service) CreateJournal(ctx context.Context, raw CreateJournalInput) (Journal, error) {
	return s.createJournal(ctx, raw, false)
}

// CreateAndPostJournal creates and posts a journal in one SQLite transaction.
// A posting validation failure rolls back the draft, its lines, and its audit
// event. Existing idempotent drafts are posted in place; existing posted
// journals are returned unchanged.
func (s *Service) CreateAndPostJournal(ctx context.Context, raw CreateJournalInput) (Journal, error) {
	return s.createJournal(ctx, raw, true)
}

func (s *Service) createJournal(ctx context.Context, raw CreateJournalInput, post bool) (Journal, error) {
	if err := s.requireActor(); err != nil {
		return Journal{}, err
	}
	input := normalizeJournalInput(raw)
	if err := validateJournalInput(input); err != nil {
		return Journal{}, err
	}
	var digest string
	var err error
	if input.SourceSystem != "" {
		digest, err = sourceDigest(input)
		if err != nil {
			return Journal{}, err
		}
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Journal{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	bookID, err := lookupID(ctx, tx, "books", input.Book)
	if err != nil {
		return Journal{}, err
	}
	periodID, err := lookupID(ctx, tx, "fiscal_periods", input.Period)
	if err != nil {
		return Journal{}, err
	}
	if input.SourceSystem != "" {
		var existingID, existingDigest, existingStatus string
		err := tx.QueryRowContext(ctx, `SELECT id, source_payload_sha256 FROM journal_entries
			WHERE book_id = ? AND source_system = ? AND source_key = ?`, bookID, input.SourceSystem, input.SourceKey).Scan(&existingID, &existingDigest)
		if err == nil {
			err = tx.QueryRowContext(ctx, `SELECT status FROM journal_entries WHERE id = ?`, existingID).Scan(&existingStatus)
		}
		if err == nil {
			if existingDigest != digest {
				return Journal{}, apperr.New(apperr.Conflict, "SOURCE_CONFLICT", "source key already exists with different content")
			}
			if post && existingStatus == "DRAFT" {
				if _, err := s.postJournalTx(ctx, tx, existingID); err != nil {
					return Journal{}, err
				}
			}
			if err := tx.Commit(); err != nil {
				return Journal{}, storesqlite.MapError("commit idempotent journal", err)
			}
			return s.GetJournal(ctx, existingID)
		}
		if err != sql.ErrNoRows {
			return Journal{}, storesqlite.MapError("check source idempotency", err)
		}
	}
	journalID, err := storesqlite.NewID()
	if err != nil {
		return Journal{}, err
	}
	var entryNumber int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(entry_number), 0) + 1
        FROM journal_entries WHERE book_id = ?`, bookID).Scan(&entryNumber); err != nil {
		return Journal{}, storesqlite.MapError("allocate journal number", err)
	}
	now := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `INSERT INTO journal_entries
		(id, book_id, entry_number, kind, posting_date, period_id, status, description, reference,
		 source_system, source_key, source_payload_sha256, tax_type, tax_accounting_period,
		 reversal_of_id, created_at, created_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'DRAFT', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		journalID, bookID, entryNumber, input.Kind, input.PostingDate, periodID, input.Description,
		nilIfEmpty(input.Reference), nilIfEmpty(input.SourceSystem), nilIfEmpty(input.SourceKey), nilIfEmpty(digest),
		nilIfEmpty(input.TaxType), nilIfEmpty(input.TaxAccountingPeriod), nilIfEmpty(input.ReversalOfID), now, s.actor, now); err != nil {
		return Journal{}, storesqlite.MapError("create journal draft", err)
	}
	if err := s.insertJournalLines(ctx, tx, journalID, input.Lines); err != nil {
		return Journal{}, err
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "journal create", AggregateType: "journal", AggregateID: journalID,
		Payload: map[string]any{"book": input.Book, "entry_number": entryNumber, "kind": input.Kind, "posting_date": input.PostingDate, "period": input.Period, "description": input.Description, "source_system": input.SourceSystem, "source_key": input.SourceKey, "line_count": len(input.Lines)},
	}); err != nil {
		return Journal{}, err
	}
	if post {
		if _, err := s.postJournalTx(ctx, tx, journalID); err != nil {
			return Journal{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Journal{}, storesqlite.MapError("commit journal", err)
	}
	return s.GetJournal(ctx, journalID)
}

func (s *Service) insertJournalLines(ctx context.Context, tx *sql.Tx, journalID string, lines []JournalLineInput) error {
	for i, line := range lines {
		accountID, err := lookupID(ctx, tx, "accounts", line.Account)
		if err != nil {
			return err
		}
		var counterpartyID any
		if line.CounterpartyEntity != "" {
			id, err := lookupID(ctx, tx, "entities", line.CounterpartyEntity)
			if err != nil {
				return err
			}
			counterpartyID = id
		}
		lineID, err := storesqlite.NewID()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO journal_lines
            (id, journal_entry_id, line_number, account_id, description, debit_cents, credit_cents,
             counterparty_entity_id, intercompany_key, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			lineID, journalID, i+1, accountID, line.Description, line.DebitCents, line.CreditCents,
			counterpartyID, nilIfEmpty(line.IntercompanyKey), storesqlite.UTCNow()); err != nil {
			return storesqlite.MapError(fmt.Sprintf("create journal line %d", i+1), err)
		}
	}
	return nil
}

func (s *Service) ReplaceDraft(ctx context.Context, journalID string, raw CreateJournalInput) (Journal, error) {
	if err := s.requireActor(); err != nil {
		return Journal{}, err
	}
	input := normalizeJournalInput(raw)
	if err := validateJournalInput(input); err != nil {
		return Journal{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Journal{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var status, sourceSystem, reversalID string
	var sourceLinked bool
	if err := tx.QueryRowContext(ctx, `SELECT status, COALESCE(reversal_of_id, '')
		, COALESCE(source_system, ''), EXISTS (
			SELECT 1 FROM source_record_journals srj WHERE srj.journal_entry_id = journal_entries.id
		) FROM journal_entries WHERE id = ?`, journalID).Scan(&status, &reversalID, &sourceSystem, &sourceLinked); err == sql.ErrNoRows {
		return Journal{}, apperr.New(apperr.NotFound, "JOURNAL_NOT_FOUND", "journal was not found")
	} else if err != nil {
		return Journal{}, storesqlite.MapError("read journal", err)
	}
	if status != "DRAFT" {
		return Journal{}, apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "only draft journals can be edited")
	}
	if reversalID != "" {
		return Journal{}, apperr.New(apperr.Conflict, "REVERSAL_DRAFT_IMMUTABLE", "reversal drafts cannot be edited; abandon and create a new reversal")
	}
	if sourceSystem != "" || sourceLinked {
		return Journal{}, apperr.New(apperr.Conflict, "SOURCE_DRAFT_IMMUTABLE", "source-derived drafts cannot be edited; abandon and import a corrected source record")
	}
	bookID, err := lookupID(ctx, tx, "books", input.Book)
	if err != nil {
		return Journal{}, err
	}
	periodID, err := lookupID(ctx, tx, "fiscal_periods", input.Period)
	if err != nil {
		return Journal{}, err
	}
	var digest any
	if input.SourceSystem != "" {
		value, err := sourceDigest(input)
		if err != nil {
			return Journal{}, err
		}
		digest = value
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM journal_lines WHERE journal_entry_id = ?", journalID); err != nil {
		return Journal{}, storesqlite.MapError("replace journal lines", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE journal_entries SET
		book_id = ?, kind = ?, posting_date = ?, period_id = ?, description = ?, reference = ?,
		source_system = ?, source_key = ?, source_payload_sha256 = ?, tax_type = ?, tax_accounting_period = ?, updated_at = ?
		WHERE id = ?`, bookID, input.Kind, input.PostingDate, periodID, input.Description, nilIfEmpty(input.Reference),
		nilIfEmpty(input.SourceSystem), nilIfEmpty(input.SourceKey), digest, nilIfEmpty(input.TaxType),
		nilIfEmpty(input.TaxAccountingPeriod), storesqlite.UTCNow(), journalID); err != nil {
		return Journal{}, storesqlite.MapError("update journal draft", err)
	}
	if err := s.insertJournalLines(ctx, tx, journalID, input.Lines); err != nil {
		return Journal{}, err
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "journal edit", AggregateType: "journal", AggregateID: journalID,
		Payload: map[string]any{"posting_date": input.PostingDate, "period": input.Period, "line_count": len(input.Lines)},
	}); err != nil {
		return Journal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Journal{}, storesqlite.MapError("commit journal edit", err)
	}
	return s.GetJournal(ctx, journalID)
}

func (s *Service) GetJournal(ctx context.Context, journalID string) (Journal, error) {
	var journal Journal
	err := s.store.DB().QueryRowContext(ctx, `SELECT je.id, je.book_id, b.code, je.entry_number, je.kind, je.posting_date,
        fp.code, je.status, je.description, COALESCE(je.reference, ''), COALESCE(je.source_system, ''),
        COALESCE(je.source_key, ''), COALESCE(je.tax_type, ''), COALESCE(je.tax_accounting_period, ''),
        COALESCE(je.reversal_of_id, ''), je.created_at, COALESCE(je.posted_at, '')
        FROM journal_entries je JOIN books b ON b.id = je.book_id
        JOIN fiscal_periods fp ON fp.id = je.period_id WHERE je.id = ?`, journalID).Scan(
		&journal.ID, &journal.BookID, &journal.BookCode, &journal.EntryNumber, &journal.Kind, &journal.PostingDate,
		&journal.PeriodCode, &journal.Status, &journal.Description, &journal.Reference,
		&journal.SourceSystem, &journal.SourceKey, &journal.TaxType, &journal.TaxAccountingPeriod,
		&journal.ReversalOfID, &journal.CreatedAt, &journal.PostedAt)
	if err == sql.ErrNoRows {
		return Journal{}, apperr.New(apperr.NotFound, "JOURNAL_NOT_FOUND", "journal was not found")
	}
	if err != nil {
		return Journal{}, storesqlite.MapError("read journal", err)
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT jl.id, jl.line_number, jl.account_id, a.code, a.name,
        jl.description, jl.debit_cents, jl.credit_cents, COALESCE(e.code, ''), COALESCE(jl.intercompany_key, '')
        FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
        LEFT JOIN entities e ON e.id = jl.counterparty_entity_id
        WHERE jl.journal_entry_id = ? ORDER BY jl.line_number`, journalID)
	if err != nil {
		return Journal{}, storesqlite.MapError("read journal lines", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var line JournalLine
		if err := rows.Scan(&line.ID, &line.LineNumber, &line.AccountID, &line.AccountCode, &line.AccountName,
			&line.Description, &line.DebitCents, &line.CreditCents, &line.CounterpartyEntity, &line.IntercompanyKey); err != nil {
			return Journal{}, err
		}
		journal.TotalDebitCents += line.DebitCents
		journal.TotalCreditCents += line.CreditCents
		journal.Lines = append(journal.Lines, line)
	}
	return journal, rows.Err()
}

func validateJournalQuery(ctx context.Context, q queryer, journalID string) (JournalValidation, error) {
	result := JournalValidation{JournalID: journalID, Errors: []string{}}
	var status, kind, bookID, postingDate, periodID, reversalID, taxType, taxAccountingPeriod string
	err := q.QueryRowContext(ctx, `SELECT status, kind, book_id, posting_date, period_id,
		COALESCE(reversal_of_id, ''), COALESCE(tax_type, ''), COALESCE(tax_accounting_period, '')
		FROM journal_entries WHERE id = ?`, journalID).Scan(
		&status, &kind, &bookID, &postingDate, &periodID, &reversalID, &taxType, &taxAccountingPeriod,
	)
	if err == sql.ErrNoRows {
		return result, apperr.New(apperr.NotFound, "JOURNAL_NOT_FOUND", "journal was not found")
	}
	if err != nil {
		return result, storesqlite.MapError("read journal for validation", err)
	}
	if status != "DRAFT" {
		result.Errors = append(result.Errors, "journal is not a draft")
	}
	var activeBook int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM books
		WHERE id = ? AND status = 'ACTIVE'`, bookID).Scan(&activeBook); err != nil {
		return result, storesqlite.MapError("validate journal book", err)
	}
	if activeBook != 1 {
		result.Errors = append(result.Errors, "journal book is not active")
	}
	var lineCount int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(debit_cents), 0), COALESCE(SUM(credit_cents), 0)
        FROM journal_lines WHERE journal_entry_id = ?`, journalID).Scan(&lineCount, &result.DebitCents, &result.CreditCents); err != nil {
		return result, storesqlite.MapError("total journal", err)
	}
	if lineCount < 2 {
		result.Errors = append(result.Errors, "journal requires at least two lines")
	}
	if result.DebitCents == 0 {
		result.Errors = append(result.Errors, "journal total must be nonzero")
	}
	if result.DebitCents != result.CreditCents {
		result.Errors = append(result.Errors, "journal debits and credits do not balance")
	}
	var periodOK int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM fiscal_periods fp
        JOIN book_periods bp ON bp.period_id = fp.id AND bp.book_id = ?
        WHERE fp.id = ? AND ? BETWEEN fp.start_date AND fp.end_date AND bp.status = 'OPEN'`,
		bookID, periodID, postingDate).Scan(&periodOK); err != nil {
		return result, storesqlite.MapError("validate journal period", err)
	}
	if periodOK != 1 {
		result.Errors = append(result.Errors, "posting date is outside an open book period")
	}
	var laterClosed int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_periods later
		JOIN fiscal_periods later_period ON later_period.id = later.period_id
		WHERE later.book_id = ? AND later.status = 'CLOSED'
		  AND later_period.id <> ? AND ? <= later_period.end_date`,
		bookID, periodID, postingDate).Scan(&laterClosed); err != nil {
		return result, storesqlite.MapError("validate later closed periods", err)
	}
	if laterClosed != 0 {
		result.Errors = append(result.Errors, "a later closed period prevents this posting")
	}
	var completedReconciliationLines int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM journal_lines jl
		JOIN statement_accounts sa ON sa.book_id = ? AND sa.gl_account_id = jl.account_id
		JOIN reconciliations r ON r.statement_account_id = sa.id
		 AND r.status = 'COMPLETED' AND ? <= r.end_date
		WHERE jl.journal_entry_id = ?`, bookID, postingDate, journalID).Scan(&completedReconciliationLines); err != nil {
		return result, storesqlite.MapError("validate completed reconciliation controls", err)
	}
	if completedReconciliationLines != 0 {
		result.Errors = append(result.Errors, "control-account posting would invalidate a completed reconciliation; reopen it first")
	}
	var invalidAccounts int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_lines jl
        LEFT JOIN book_accounts ba ON ba.book_id = ? AND ba.account_id = jl.account_id
        WHERE jl.journal_entry_id = ? AND (ba.account_id IS NULL OR ba.posting_enabled <> 1
          OR ? < ba.active_from OR (ba.active_to IS NOT NULL AND ? > ba.active_to))`,
		bookID, journalID, postingDate, postingDate).Scan(&invalidAccounts); err != nil {
		return result, storesqlite.MapError("validate journal accounts", err)
	}
	if invalidAccounts != 0 {
		result.Errors = append(result.Errors, "journal contains inactive or foreign book accounts")
	}
	switch kind {
	case "CLOSING":
		var closingDateOK, profitLossLines, equityLines, otherLines int
		var profitLossChange int64
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM fiscal_periods fp
			WHERE fp.id = ? AND fp.is_year_end = 1 AND ? = fp.end_date`, periodID, postingDate).Scan(&closingDateOK); err != nil {
			return result, storesqlite.MapError("validate closing date", err)
		}
		if err := q.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN a.account_type IN ('REVENUE', 'EXPENSE') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN a.account_type = 'EQUITY' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN a.account_type NOT IN ('REVENUE', 'EXPENSE', 'EQUITY') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN a.account_type IN ('REVENUE', 'EXPENSE')
				THEN jl.debit_cents - jl.credit_cents ELSE 0 END), 0)
			FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
			WHERE jl.journal_entry_id = ?`, journalID).Scan(&profitLossLines, &equityLines, &otherLines, &profitLossChange); err != nil {
			return result, storesqlite.MapError("validate closing accounts", err)
		}
		if closingDateOK != 1 {
			result.Errors = append(result.Errors, "closing journal must post on the fiscal year's final date")
		}
		expectedEquityLines := 1
		if profitLossChange == 0 {
			expectedEquityLines = 0
		}
		if profitLossLines == 0 || equityLines != expectedEquityLines || otherLines != 0 {
			result.Errors = append(result.Errors, "closing journal requires exact profit-and-loss lines and an equity line only when net income is nonzero")
		}
		var mismatchedBalances int
		if err := q.QueryRowContext(ctx, `WITH year_bounds AS (
				SELECT MIN(year_period.start_date) AS start_date,
				       MAX(year_period.end_date) AS end_date
				FROM fiscal_periods current_period
				JOIN fiscal_periods year_period ON year_period.fiscal_year = current_period.fiscal_year
				WHERE current_period.id = ?
			), operating AS (
				SELECT jl.account_id, SUM(jl.debit_cents - jl.credit_cents) AS balance_cents
				FROM journal_entries je
				JOIN journal_lines jl ON jl.journal_entry_id = je.id
				JOIN year_bounds bounds
				WHERE je.book_id = ? AND je.status = 'POSTED' AND je.kind = 'STANDARD'
				  AND je.posting_date BETWEEN bounds.start_date AND bounds.end_date
				GROUP BY jl.account_id
			), closing_lines AS (
				SELECT account_id, COUNT(*) AS line_count,
				       SUM(debit_cents - credit_cents) AS change_cents
				FROM journal_lines WHERE journal_entry_id = ? GROUP BY account_id
			)
			SELECT COUNT(*)
			FROM accounts a
			JOIN book_accounts ba ON ba.account_id = a.id AND ba.book_id = ?
			LEFT JOIN operating o ON o.account_id = a.id
			LEFT JOIN closing_lines c ON c.account_id = a.id
			WHERE a.account_type IN ('REVENUE', 'EXPENSE') AND (
			  COALESCE(o.balance_cents, 0) + COALESCE(c.change_cents, 0) <> 0 OR
			  (COALESCE(o.balance_cents, 0) <> 0 AND COALESCE(c.line_count, 0) <> 1) OR
			  (COALESCE(o.balance_cents, 0) = 0 AND COALESCE(c.line_count, 0) <> 0)
			)`, periodID, bookID, journalID, bookID).Scan(&mismatchedBalances); err != nil {
			return result, storesqlite.MapError("validate closing balances", err)
		}
		if mismatchedBalances != 0 {
			result.Errors = append(result.Errors, "closing journal must exactly zero every fiscal-year profit-and-loss balance")
		}
		var earlierOpen int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
			FROM fiscal_periods current_period
			JOIN fiscal_periods earlier_period ON earlier_period.fiscal_year = current_period.fiscal_year
			  AND earlier_period.end_date < current_period.start_date
			JOIN book_periods earlier_book_period ON earlier_book_period.period_id = earlier_period.id
			  AND earlier_book_period.book_id = ?
			WHERE current_period.id = ? AND earlier_book_period.status <> 'CLOSED'`, bookID, periodID).Scan(&earlierOpen); err != nil {
			return result, storesqlite.MapError("validate earlier fiscal periods", err)
		}
		if earlierOpen != 0 {
			result.Errors = append(result.Errors, "all earlier fiscal-year periods must be closed before posting a closing journal")
		}
		var activeClose int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
			FROM fiscal_periods current_period
			JOIN fiscal_periods close_period ON close_period.fiscal_year = current_period.fiscal_year
			JOIN journal_entries close_entry ON close_entry.period_id = close_period.id
			WHERE current_period.id = ? AND close_entry.book_id = ?
			  AND close_entry.kind = 'CLOSING' AND close_entry.status = 'POSTED'
			  AND NOT EXISTS (
				SELECT 1 FROM journal_entries close_reversal
				WHERE close_reversal.reversal_of_id = close_entry.id
				  AND close_reversal.kind = 'CLOSING_REVERSAL' AND close_reversal.status = 'POSTED'
			  )`, periodID, bookID).Scan(&activeClose); err != nil {
			return result, storesqlite.MapError("validate active fiscal year close", err)
		}
		if activeClose != 0 {
			result.Errors = append(result.Errors, "fiscal year already has an active closing journal")
		}
	case "STANDARD":
		var existingClose int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*)
			FROM fiscal_periods current_period
			JOIN fiscal_periods close_period ON close_period.fiscal_year = current_period.fiscal_year
			JOIN journal_entries close_entry ON close_entry.period_id = close_period.id
			WHERE current_period.id = ? AND close_entry.book_id = ?
			  AND close_entry.kind = 'CLOSING' AND close_entry.status = 'POSTED'
			  AND NOT EXISTS (
				SELECT 1 FROM journal_entries close_reversal
				WHERE close_reversal.reversal_of_id = close_entry.id
				  AND close_reversal.kind = 'CLOSING_REVERSAL' AND close_reversal.status = 'POSTED'
			  )`, periodID, bookID).Scan(&existingClose); err != nil {
			return result, storesqlite.MapError("validate fiscal year close", err)
		}
		if existingClose != 0 {
			result.Errors = append(result.Errors, "fiscal year is closed; its closing journal must be reopened before standard posting")
		}
	}
	if reversalID != "" {
		var originalPostingDate, originalTaxType, originalTaxAccountingPeriod string
		err := q.QueryRowContext(ctx, `SELECT posting_date, COALESCE(tax_type, ''),
			COALESCE(tax_accounting_period, '') FROM journal_entries WHERE id = ?`, reversalID).Scan(
			&originalPostingDate, &originalTaxType, &originalTaxAccountingPeriod,
		)
		if err != nil && err != sql.ErrNoRows {
			return result, storesqlite.MapError("read reversal metadata", err)
		}
		if err == nil {
			if postingDate < originalPostingDate {
				result.Errors = append(result.Errors, "reversal posting date cannot precede the original journal")
			}
			if taxType != originalTaxType || taxAccountingPeriod != originalTaxAccountingPeriod {
				result.Errors = append(result.Errors, "reversal tax type and accounting period must exactly match the original journal")
			}
		}
		var reversalOK int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries original
			WHERE original.id = ? AND original.book_id = ? AND original.status = 'POSTED'
			  AND ((? = 'STANDARD' AND original.kind = 'STANDARD') OR
			       (? = 'CLOSING_REVERSAL' AND original.kind = 'CLOSING'
			        AND original.period_id = ? AND original.posting_date = ?))`,
			reversalID, bookID, kind, kind, periodID, postingDate).Scan(&reversalOK); err != nil {
			return result, err
		}
		if reversalOK != 1 {
			result.Errors = append(result.Errors, "reversal does not reference a posted journal in the same book")
		}
		var reversalMismatch int
		if err := q.QueryRowContext(ctx, `SELECT
			CASE WHEN
			  (SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = ?) <>
			  (SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = ?)
			  OR EXISTS (
				SELECT 1 FROM journal_lines reversal_line
				LEFT JOIN journal_lines original_line
				  ON original_line.journal_entry_id = ?
				 AND original_line.line_number = reversal_line.line_number
				 AND original_line.account_id = reversal_line.account_id
				 AND original_line.debit_cents = reversal_line.credit_cents
				 AND original_line.credit_cents = reversal_line.debit_cents
				 AND original_line.description = reversal_line.description
				 AND original_line.counterparty_entity_id IS reversal_line.counterparty_entity_id
				 AND original_line.intercompany_key IS reversal_line.intercompany_key
				WHERE reversal_line.journal_entry_id = ? AND original_line.id IS NULL
			  ) THEN 1 ELSE 0 END`, journalID, reversalID, reversalID, journalID).Scan(&reversalMismatch); err != nil {
			return result, storesqlite.MapError("validate reversal lines", err)
		}
		if reversalMismatch != 0 {
			result.Errors = append(result.Errors, "reversal lines must exactly negate the original journal")
		}
	} else if kind == "CLOSING_REVERSAL" {
		result.Errors = append(result.Errors, "closing reversal must reference a closing journal")
	}
	result.Valid = len(result.Errors) == 0
	return result, nil
}

func (s *Service) ValidateJournal(ctx context.Context, journalID string) (JournalValidation, error) {
	return validateJournalQuery(ctx, s.store.DB(), journalID)
}

func (s *Service) PostJournal(ctx context.Context, journalID string) (Journal, error) {
	if err := s.requireActor(); err != nil {
		return Journal{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Journal{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := s.postJournalTx(ctx, tx, journalID); err != nil {
		return Journal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Journal{}, storesqlite.MapError("commit journal posting", err)
	}
	return s.GetJournal(ctx, journalID)
}

func (s *Service) postJournalTx(ctx context.Context, tx *sql.Tx, journalID string) (JournalValidation, error) {
	validation, err := validateJournalQuery(ctx, tx, journalID)
	if err != nil {
		return JournalValidation{}, err
	}
	if !validation.Valid {
		return validation, apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(validation.Errors, "; "))
	}
	now := storesqlite.UTCNow()
	result, err := tx.ExecContext(ctx, `UPDATE journal_entries
		SET status = 'POSTED', posted_at = ?, posted_by = ?, updated_at = ?
		WHERE id = ? AND status = 'DRAFT'`, now, s.actor, now, journalID)
	if err != nil {
		return validation, storesqlite.MapError("post journal", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return validation, apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "journal is no longer a draft")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "journal post", AggregateType: "journal", AggregateID: journalID,
		Payload: map[string]any{"debit_cents": validation.DebitCents, "credit_cents": validation.CreditCents},
	}); err != nil {
		return validation, err
	}
	return validation, nil
}

func (s *Service) AbandonJournal(ctx context.Context, journalID string) error {
	if err := s.requireActor(); err != nil {
		return err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := tx.ExecContext(ctx, `UPDATE journal_entries SET status = 'ABANDONED', updated_at = ?
        WHERE id = ? AND status = 'DRAFT'`, storesqlite.UTCNow(), journalID)
	if err != nil {
		return storesqlite.MapError("abandon journal", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "journal was not found or is not a draft")
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "journal abandon", AggregateType: "journal", AggregateID: journalID, Payload: map[string]any{},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ReverseJournal(ctx context.Context, originalID, postingDate, periodCode, description string) (Journal, error) {
	if err := s.requireActor(); err != nil {
		return Journal{}, err
	}
	if err := validateDate(postingDate, "posting date"); err != nil {
		return Journal{}, err
	}
	original, err := s.GetJournal(ctx, originalID)
	if err != nil {
		return Journal{}, err
	}
	if original.Status != "POSTED" {
		return Journal{}, apperr.New(apperr.Validation, "REVERSAL_INVALID", "only a posted journal can be reversed")
	}
	if postingDate < original.PostingDate {
		return Journal{}, apperr.New(apperr.Validation, "REVERSAL_DATE_INVALID", "reversal posting date cannot precede the original journal")
	}
	if strings.TrimSpace(description) == "" {
		description = "Reversal of " + original.BookCode + " journal " + fmt.Sprint(original.EntryNumber)
	}
	kind := "STANDARD"
	if original.Kind == "CLOSING" {
		kind = "CLOSING_REVERSAL"
		if postingDate != original.PostingDate || normalizeCode(periodCode) != original.PeriodCode {
			return Journal{}, apperr.New(apperr.Validation, "CLOSING_REVERSAL_PERIOD_INVALID", "a closing journal must be reversed on its original year-end date and period after reopening that period")
		}
	}
	if original.Kind == "CLOSING_REVERSAL" {
		return Journal{}, apperr.New(apperr.Validation, "REVERSAL_INVALID", "a closing reversal cannot itself be reversed")
	}
	input := CreateJournalInput{
		Book: original.BookCode, Kind: kind, PostingDate: postingDate, Period: periodCode, Description: description,
		Reference: "REV-" + fmt.Sprint(original.EntryNumber), ReversalOfID: original.ID,
		TaxType: original.TaxType, TaxAccountingPeriod: original.TaxAccountingPeriod,
	}
	for _, line := range original.Lines {
		input.Lines = append(input.Lines, JournalLineInput{
			Account: line.AccountCode, Description: line.Description, DebitCents: line.CreditCents,
			CreditCents: line.DebitCents, CounterpartyEntity: line.CounterpartyEntity, IntercompanyKey: line.IntercompanyKey,
		})
	}
	return s.CreateJournal(ctx, input)
}

func (s *Service) ListJournals(ctx context.Context, bookCode, from, to, status string) ([]Journal, error) {
	query := `SELECT je.id, je.book_id, b.code, je.entry_number, je.kind, je.posting_date, fp.code, je.status,
        je.description, COALESCE(je.reference, ''), COALESCE(je.source_system, ''), COALESCE(je.source_key, ''),
        COALESCE(je.tax_type, ''), COALESCE(je.tax_accounting_period, ''), COALESCE(je.reversal_of_id, ''),
        je.created_at, COALESCE(je.posted_at, ''),
        COALESCE(SUM(jl.debit_cents), 0), COALESCE(SUM(jl.credit_cents), 0)
        FROM journal_entries je JOIN books b ON b.id = je.book_id
        JOIN fiscal_periods fp ON fp.id = je.period_id LEFT JOIN journal_lines jl ON jl.journal_entry_id = je.id WHERE 1=1`
	var args []any
	if bookCode != "" {
		query += " AND b.code = ?"
		args = append(args, normalizeCode(bookCode))
	}
	if from != "" {
		if err := validateDate(from, "from"); err != nil {
			return nil, err
		}
		query += " AND je.posting_date >= ?"
		args = append(args, from)
	}
	if to != "" {
		if err := validateDate(to, "to"); err != nil {
			return nil, err
		}
		query += " AND je.posting_date <= ?"
		args = append(args, to)
	}
	if status != "" {
		query += " AND je.status = ?"
		args = append(args, normalizeCode(status))
	}
	query += ` GROUP BY je.id ORDER BY je.posting_date, b.code, je.entry_number`
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list journals", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var journals []Journal
	for rows.Next() {
		var journal Journal
		if err := rows.Scan(&journal.ID, &journal.BookID, &journal.BookCode, &journal.EntryNumber,
			&journal.Kind, &journal.PostingDate, &journal.PeriodCode, &journal.Status, &journal.Description, &journal.Reference,
			&journal.SourceSystem, &journal.SourceKey, &journal.TaxType, &journal.TaxAccountingPeriod,
			&journal.ReversalOfID, &journal.CreatedAt, &journal.PostedAt, &journal.TotalDebitCents, &journal.TotalCreditCents); err != nil {
			return nil, err
		}
		journals = append(journals, journal)
	}
	return journals, rows.Err()
}
