package report

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type postingLine struct {
	JournalID          string
	BookID             string
	BookCode           string
	BookKind           string
	EntityID           string
	EntityCode         string
	EntryNumber        int64
	PostingDate        string
	Reference          string
	JournalDescription string
	LineNumber         int
	LineDescription    string
	Account            Account
	DebitCents         int64
	CreditCents        int64
	SyntheticKind      string
}

const (
	syntheticPerimeterEntry = "CONSOLIDATION_PERIMETER_ENTRY"
	syntheticPerimeterExit  = "CONSOLIDATION_PERIMETER_EXIT"
)

func placeholders(count int) string {
	if count <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func bookArgs(scope ResolvedScope) []any {
	args := make([]any, 0, len(scope.Books))
	for _, book := range scope.Books {
		args = append(args, book.BookID)
	}
	return args
}

func withoutConsolidationIntervals(scope ResolvedScope) ResolvedScope {
	result := scope
	result.Books = append([]ScopedBook(nil), scope.Books...)
	for index := range result.Books {
		result.Books[index].ConsolidationIntervals = nil
	}
	return result
}

// scopedDatePredicate binds each owned book in a range scope to the intersected
// dates of its ownership path from the group parent. Parent, entity-scope, and
// elimination books have no retained intervals and remain unbounded.
func scopedDatePredicate(scope ResolvedScope, bookExpression, dateExpression string) (string, []any) {
	conditions := make([]string, 0, len(scope.Books))
	args := make([]any, 0, len(scope.Books))
	for _, book := range scope.Books {
		if len(book.ConsolidationIntervals) == 0 {
			conditions = append(conditions, bookExpression+" = ?")
			args = append(args, book.BookID)
			continue
		}
		for _, interval := range book.ConsolidationIntervals {
			condition := "(" + bookExpression + " = ? AND " + dateExpression + " >= ?"
			args = append(args, book.BookID, interval.EffectiveFrom)
			if interval.EffectiveTo != "" {
				condition += " AND " + dateExpression + " <= ?"
				args = append(args, interval.EffectiveTo)
			}
			conditions = append(conditions, condition+")")
		}
	}
	if len(conditions) == 0 {
		return "0", args
	}
	return "(" + strings.Join(conditions, " OR ") + ")", args
}

func (s *Service) verifyPostedJournalIntegrity(ctx context.Context, scope ResolvedScope, throughDate string) error {
	predicate, args := scopedDatePredicate(scope, "je.book_id", "je.posting_date")
	query := `SELECT je.id, b.code, je.entry_number,
		COUNT(jl.id), COALESCE(SUM(jl.debit_cents), 0), COALESCE(SUM(jl.credit_cents), 0)
		FROM journal_entries je
		JOIN books b ON b.id = je.book_id
		LEFT JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.status = 'POSTED' AND ` + predicate + `
		  AND je.posting_date <= ?
		GROUP BY je.id
		HAVING COUNT(jl.id) < 2
		    OR COALESCE(SUM(jl.debit_cents), 0) = 0
		    OR COALESCE(SUM(jl.debit_cents), 0) <> COALESCE(SUM(jl.credit_cents), 0)
		ORDER BY je.posting_date, b.code, je.entry_number
		LIMIT 1`
	args = append(args, throughDate)
	var journalID, bookCode string
	var entryNumber, lineCount, debitCents, creditCents int64
	err := s.db().QueryRowContext(ctx, query, args...).Scan(
		&journalID, &bookCode, &entryNumber, &lineCount, &debitCents, &creditCents,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return storesqlite.MapError("verify posted journals for report", err)
	}
	return apperr.New(
		apperr.Integrity,
		"POSTED_JOURNAL_INVALID",
		fmt.Sprintf("posted journal %s #%d (%s) is invalid: %d lines, %d debit cents, %d credit cents", bookCode, entryNumber, journalID, lineCount, debitCents, creditCents),
	)
}

func (s *Service) loadAccountCatalog(ctx context.Context, scope ResolvedScope) ([]Account, error) {
	query := `SELECT DISTINCT a.id, a.code, a.name, a.account_type, a.subtype, a.normal_balance, a.statement_section
		FROM accounts a
		JOIN book_accounts ba ON ba.account_id = a.id
		WHERE ba.book_id IN (` + placeholders(len(scope.Books)) + `)
		ORDER BY a.code`
	rows, err := s.db().QueryContext(ctx, query, bookArgs(scope)...)
	if err != nil {
		return nil, storesqlite.MapError("load report account catalog", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var accounts []Account
	for rows.Next() {
		var account Account
		if err := rows.Scan(
			&account.ID, &account.Code, &account.Name, &account.Type, &account.Subtype,
			&account.NormalBalance, &account.StatementSection,
		); err != nil {
			return nil, storesqlite.MapError("read report account", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("read report accounts", err)
	}
	return accounts, nil
}

func (s *Service) loadPostedLines(ctx context.Context, scope ResolvedScope, fromDate, throughDate, accountCode string) ([]postingLine, error) {
	predicate, args := scopedDatePredicate(scope, "je.book_id", "je.posting_date")
	query := `SELECT je.id, b.id, b.code, b.kind, COALESCE(e.id, ''), COALESCE(e.code, ''),
		je.entry_number, je.posting_date, COALESCE(je.reference, ''), je.description,
		jl.line_number, jl.description,
		a.id, a.code, a.name, a.account_type, a.subtype, a.normal_balance, a.statement_section,
		jl.debit_cents, jl.credit_cents
		FROM journal_entries je
		JOIN books b ON b.id = je.book_id
		LEFT JOIN entities e ON e.id = b.entity_id
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.status = 'POSTED' AND ` + predicate + `
		  AND je.posting_date <= ?`
	args = append(args, throughDate)
	if fromDate != "" {
		query += " AND je.posting_date >= ?"
		args = append(args, fromDate)
	}
	if accountCode != "" {
		query += " AND a.code = ?"
		args = append(args, normalizeCode(accountCode))
	}
	query += ` ORDER BY a.code, je.posting_date, b.code, je.entry_number, jl.line_number, jl.id`
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("load posted report lines", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var lines []postingLine
	for rows.Next() {
		var line postingLine
		if err := rows.Scan(
			&line.JournalID, &line.BookID, &line.BookCode, &line.BookKind, &line.EntityID, &line.EntityCode,
			&line.EntryNumber, &line.PostingDate, &line.Reference, &line.JournalDescription,
			&line.LineNumber, &line.LineDescription,
			&line.Account.ID, &line.Account.Code, &line.Account.Name, &line.Account.Type,
			&line.Account.Subtype, &line.Account.NormalBalance, &line.Account.StatementSection,
			&line.DebitCents, &line.CreditCents,
		); err != nil {
			return nil, storesqlite.MapError("read posted report line", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("read posted report lines", err)
	}
	return lines, nil
}

type boundaryBalance struct {
	Account Account
	Cents   int64
}

// loadConsolidationBoundaryLines makes a consolidated general ledger continuous
// across ownership-driven perimeter changes. On entry, the complete owned-book
// balance immediately before the effective date enters consolidation. On exit,
// the complete balance through the inclusive ownership-path end date leaves on
// the following day. These presentation-only lines never enter journal tables
// or profit-and-loss reports.
func (s *Service) loadConsolidationBoundaryLines(ctx context.Context, scope ResolvedScope, throughDate, accountCode string) ([]postingLine, error) {
	if scope.Kind != scopeGroup {
		return nil, nil
	}
	var result []postingLine
	for _, book := range scope.Books {
		for _, interval := range book.ConsolidationIntervals {
			balances, err := s.loadBookBalancesAtBoundary(ctx, book.BookID, interval.EffectiveFrom, false, accountCode)
			if err != nil {
				return nil, err
			}
			for index, balance := range balances {
				line, err := consolidationBoundaryLine(book, balance, interval.EffectiveFrom, interval.EffectiveFrom, index+1, syntheticPerimeterEntry)
				if err != nil {
					return nil, err
				}
				result = append(result, line)
			}

			if interval.EffectiveTo == "" || interval.EffectiveTo >= throughDate {
				continue
			}
			exitDate, err := nextDate(interval.EffectiveTo)
			if err != nil {
				return nil, err
			}
			balances, err = s.loadBookBalancesAtBoundary(ctx, book.BookID, interval.EffectiveTo, true, accountCode)
			if err != nil {
				return nil, err
			}
			for index, balance := range balances {
				balance.Cents, err = checkedNegate(balance.Cents)
				if err != nil {
					return nil, err
				}
				line, err := consolidationBoundaryLine(book, balance, exitDate, interval.EffectiveTo, index+1, syntheticPerimeterExit)
				if err != nil {
					return nil, err
				}
				result = append(result, line)
			}
		}
	}
	return result, nil
}

func (s *Service) loadBookBalancesAtBoundary(ctx context.Context, bookID, boundaryDate string, inclusive bool, accountCode string) ([]boundaryBalance, error) {
	operator := "<"
	if inclusive {
		operator = "<="
	}
	query := `SELECT a.id, a.code, a.name, a.account_type, a.subtype, a.normal_balance, a.statement_section,
		jl.debit_cents, jl.credit_cents
		FROM journal_entries je
		JOIN journal_lines jl ON jl.journal_entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.status = 'POSTED' AND je.book_id = ? AND je.posting_date ` + operator + ` ?`
	args := []any{bookID, boundaryDate}
	if accountCode != "" {
		query += " AND a.code = ?"
		args = append(args, normalizeCode(accountCode))
	}
	query += " ORDER BY a.code, a.id, je.posting_date, je.entry_number, jl.line_number, jl.id"
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("load owned-book boundary balances", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)

	balances := make(map[string]*boundaryBalance)
	for rows.Next() {
		var account Account
		var debitCents, creditCents int64
		if err := rows.Scan(
			&account.ID, &account.Code, &account.Name, &account.Type, &account.Subtype,
			&account.NormalBalance, &account.StatementSection, &debitCents, &creditCents,
		); err != nil {
			return nil, storesqlite.MapError("read owned-book boundary balance", err)
		}
		balance := balances[account.ID]
		if balance == nil {
			balance = &boundaryBalance{Account: account}
			balances[account.ID] = balance
		}
		balance.Cents, err = checkedAdd(balance.Cents, debitCents-creditCents)
		if err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, storesqlite.MapError("read owned-book boundary balances", err)
	}
	result := make([]boundaryBalance, 0, len(balances))
	for _, balance := range balances {
		if balance.Cents != 0 {
			result = append(result, *balance)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Account.Code == result[j].Account.Code {
			return result[i].Account.ID < result[j].Account.ID
		}
		return result[i].Account.Code < result[j].Account.Code
	})
	return result, nil
}

func consolidationBoundaryLine(book ScopedBook, balance boundaryBalance, postingDate, effectiveDate string, lineNumber int, kind string) (postingLine, error) {
	line := postingLine{
		JournalID: kind + ":" + book.BookID + ":" + effectiveDate,
		BookID:    book.BookID, BookCode: book.BookCode, BookKind: book.Kind,
		EntityID: book.EntityID, EntityCode: book.EntityCode,
		PostingDate: postingDate, Reference: kind, LineNumber: lineNumber, Account: balance.Account,
		SyntheticKind: kind,
	}
	switch kind {
	case syntheticPerimeterEntry:
		line.JournalDescription = fmt.Sprintf("Consolidation perimeter entry for %s beginning %s", book.EntityCode, effectiveDate)
		line.LineDescription = fmt.Sprintf("Owned-book balance immediately before %s", effectiveDate)
	case syntheticPerimeterExit:
		line.JournalDescription = fmt.Sprintf("Consolidation perimeter exit for %s ending %s", book.EntityCode, effectiveDate)
		line.LineDescription = fmt.Sprintf("Remove owned-book balance through %s", effectiveDate)
	default:
		return postingLine{}, apperr.New(apperr.Integrity, "REPORT_SYNTHETIC_KIND_INVALID", "unsupported consolidation boundary kind")
	}
	if balance.Cents > 0 {
		line.DebitCents = balance.Cents
	} else {
		credit, err := checkedNegate(balance.Cents)
		if err != nil {
			return postingLine{}, err
		}
		line.CreditCents = credit
	}
	return line, nil
}

func nextDate(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", apperr.New(apperr.Integrity, "REPORT_CONSOLIDATION_DATE_INVALID", "ownership-derived consolidation perimeter contains an invalid effective date")
	}
	return parsed.AddDate(0, 0, 1).Format("2006-01-02"), nil
}

func sortPostingLines(lines []postingLine) {
	syntheticRank := func(kind string) int {
		switch kind {
		case syntheticPerimeterExit:
			return 0
		case syntheticPerimeterEntry:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		left, right := lines[i], lines[j]
		if left.Account.Code != right.Account.Code {
			return left.Account.Code < right.Account.Code
		}
		if left.Account.ID != right.Account.ID {
			return left.Account.ID < right.Account.ID
		}
		if left.PostingDate != right.PostingDate {
			return left.PostingDate < right.PostingDate
		}
		if left.BookCode != right.BookCode {
			return left.BookCode < right.BookCode
		}
		if syntheticRank(left.SyntheticKind) != syntheticRank(right.SyntheticKind) {
			return syntheticRank(left.SyntheticKind) < syntheticRank(right.SyntheticKind)
		}
		if left.EntryNumber != right.EntryNumber {
			return left.EntryNumber < right.EntryNumber
		}
		if left.LineNumber != right.LineNumber {
			return left.LineNumber < right.LineNumber
		}
		return left.JournalID < right.JournalID
	})
}

func (s *Service) loadOperatingLines(ctx context.Context, scope ResolvedScope, fromDate, throughDate string) ([]postingLine, error) {
	lines, err := s.loadPostedLines(ctx, scope, fromDate, throughDate, "")
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return lines, nil
	}
	closing := make(map[string]bool)
	predicate, args := scopedDatePredicate(scope, "book_id", "posting_date")
	query := `SELECT id FROM journal_entries WHERE kind IN ('CLOSING', 'CLOSING_REVERSAL') AND ` + predicate + `
		AND posting_date BETWEEN ? AND ?`
	args = append(args, fromDate, throughDate)
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("load closing journals", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		closing[id] = true
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]postingLine, 0, len(lines))
	for _, line := range lines {
		if !closing[line.JournalID] {
			result = append(result, line)
		}
	}
	return result, nil
}

type accountBalance struct {
	Account Account
	ByBook  centsByBook
}

func aggregateBalances(lines []postingLine) (map[string]*accountBalance, error) {
	result := make(map[string]*accountBalance)
	for _, line := range lines {
		balance := result[line.Account.ID]
		if balance == nil {
			balance = &accountBalance{Account: line.Account, ByBook: centsByBook{}}
			result[line.Account.ID] = balance
		}
		value, err := checkedAdd(balance.ByBook[line.BookID], line.DebitCents-line.CreditCents)
		if err != nil {
			return nil, err
		}
		balance.ByBook[line.BookID] = value
	}
	return result, nil
}

func includeCatalogAccounts(balances map[string]*accountBalance, catalog []Account) {
	for _, account := range catalog {
		if balances[account.ID] == nil {
			balances[account.ID] = &accountBalance{Account: account, ByBook: centsByBook{}}
		}
	}
}

func sortedBalances(balances map[string]*accountBalance) []*accountBalance {
	result := make([]*accountBalance, 0, len(balances))
	for _, balance := range balances {
		result = append(result, balance)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Account.Code == result[j].Account.Code {
			return result[i].Account.ID < result[j].Account.ID
		}
		return result[i].Account.Code < result[j].Account.Code
	})
	return result
}

func totalBooks(balances map[string]*accountBalance, accountTypes ...string) (centsByBook, error) {
	allowed := make(map[string]bool, len(accountTypes))
	for _, accountType := range accountTypes {
		allowed[accountType] = true
	}
	result := centsByBook{}
	for _, balance := range balances {
		if allowed[balance.Account.Type] {
			if err := addCents(result, balance.ByBook, 1); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
