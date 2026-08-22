package report

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

const (
	scopeEntity = "ENTITY"
	scopeGroup  = "GROUP"
)

type Service struct {
	store          *storesqlite.Store
	reader         reportQueryer
	decorateReader func(reportQueryer) reportQueryer
}

func NewService(store *storesqlite.Store) *Service {
	return &Service{store: store}
}

type reportQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) db() reportQueryer {
	if s.reader != nil {
		return s.reader
	}
	return s.store.DB()
}

func withReportSnapshot[T any](ctx context.Context, service *Service, build func(*Service) (T, error)) (T, error) {
	if service.reader != nil {
		return build(service)
	}
	tx, err := service.store.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		var zero T
		return zero, storesqlite.MapError("begin report snapshot", err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	var reader reportQueryer = tx
	if service.decorateReader != nil {
		reader = service.decorateReader(reader)
	}
	snapshot := &Service{store: service.store, reader: reader}
	result, err := build(snapshot)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, storesqlite.MapError("complete report snapshot", err)
	}
	return result, nil
}

func normalizeCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func validateDate(value, field string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return apperr.New(apperr.Invalid, "REPORT_DATE_INVALID", field+" must be an ISO-8601 date")
	}
	return nil
}

func validateRange(from, to string) error {
	if err := validateDate(from, "from date"); err != nil {
		return err
	}
	if err := validateDate(to, "to date"); err != nil {
		return err
	}
	if from > to {
		return apperr.New(apperr.Invalid, "REPORT_RANGE_INVALID", "from date cannot be after to date")
	}
	return nil
}

// ResolveScope selects the actual and elimination books that constitute a scope
// at an as-of date. Legal-entity books are never collapsed here.
func (s *Service) ResolveScope(ctx context.Context, scope Scope, asOfDate string) (ResolvedScope, error) {
	return withReportSnapshot(ctx, s, func(snapshot *Service) (ResolvedScope, error) {
		return snapshot.resolveScope(ctx, scope, asOfDate)
	})
}

func (s *Service) resolveScope(ctx context.Context, scope Scope, asOfDate string) (ResolvedScope, error) {
	if err := validateDate(asOfDate, "as-of date"); err != nil {
		return ResolvedScope{}, err
	}
	entityCode := normalizeCode(scope.EntityCode)
	groupCode := normalizeCode(scope.GroupCode)
	if (entityCode == "") == (groupCode == "") {
		return ResolvedScope{}, apperr.New(apperr.Invalid, "REPORT_SCOPE_INVALID", "exactly one entity or group scope is required")
	}
	if entityCode != "" {
		return s.resolveEntityScope(ctx, entityCode, asOfDate)
	}
	return s.resolveGroupScope(ctx, groupCode, asOfDate)
}

// resolveRangeScope selects the group's parent and every recursively owned
// descendant whose effective ownership path overlaps an inclusive report
// range. The intersected path intervals are later applied to each posting date.
func (s *Service) resolveRangeScope(ctx context.Context, scope Scope, fromDate, throughDate string) (ResolvedScope, error) {
	entityCode := normalizeCode(scope.EntityCode)
	groupCode := normalizeCode(scope.GroupCode)
	if (entityCode == "") == (groupCode == "") {
		return ResolvedScope{}, apperr.New(apperr.Invalid, "REPORT_SCOPE_INVALID", "exactly one entity or group scope is required")
	}
	if entityCode != "" {
		return s.resolveEntityScope(ctx, entityCode, throughDate)
	}
	return s.resolveGroupScopeForRange(ctx, groupCode, fromDate, throughDate)
}

func (s *Service) resolveEntityScope(ctx context.Context, code, asOfDate string) (ResolvedScope, error) {
	var result ResolvedScope
	var book ScopedBook
	err := s.db().QueryRowContext(ctx, `SELECT e.id, e.code, e.legal_name, e.functional_currency,
		b.id, b.code, b.name, b.kind, b.accounting_basis
		FROM entities e
		JOIN books b ON b.entity_id = e.id AND b.kind = 'ACTUAL' AND b.status = 'ACTIVE'
		WHERE e.code = ?`, code).Scan(
		&result.ID, &result.Code, &result.Name, &result.Currency,
		&book.BookID, &book.BookCode, &book.BookName, &book.Kind, &book.Basis,
	)
	if err == sql.ErrNoRows {
		return ResolvedScope{}, apperr.New(apperr.NotFound, "REPORT_ENTITY_NOT_FOUND", fmt.Sprintf("entity %q with an active actual book was not found", code))
	}
	if err != nil {
		return ResolvedScope{}, storesqlite.MapError("resolve report entity", err)
	}
	if book.Basis != "ACCRUAL" {
		return ResolvedScope{}, apperr.New(apperr.Validation, "BASIS_NOT_SUPPORTED", "financial reports currently support accrual-basis books only")
	}
	result.Kind = scopeEntity
	result.Basis = book.Basis
	result.AsOfDate = asOfDate
	book.EntityID = result.ID
	book.EntityCode = result.Code
	book.EntityName = result.Name
	result.Books = []ScopedBook{book}
	return result, nil
}

func (s *Service) resolveGroupScope(ctx context.Context, code, asOfDate string) (ResolvedScope, error) {
	return s.resolveGroupScopeDates(ctx, code, asOfDate, asOfDate, false)
}

func (s *Service) resolveGroupScopeForRange(ctx context.Context, code, fromDate, throughDate string) (ResolvedScope, error) {
	return s.resolveGroupScopeDates(ctx, code, fromDate, throughDate, true)
}

func (s *Service) resolveGroupScopeDates(ctx context.Context, code, fromDate, throughDate string, retainIntervals bool) (ResolvedScope, error) {
	result := ResolvedScope{Kind: scopeGroup, AsOfDate: throughDate}
	err := s.db().QueryRowContext(ctx, `SELECT id, code, name, currency
		FROM consolidation_groups WHERE code = ?`, code).Scan(
		&result.ID, &result.Code, &result.Name, &result.Currency,
	)
	if err == sql.ErrNoRows {
		return ResolvedScope{}, apperr.New(apperr.NotFound, "REPORT_GROUP_NOT_FOUND", fmt.Sprintf("consolidation group %q was not found", code))
	}
	if err != nil {
		return ResolvedScope{}, storesqlite.MapError("resolve report group", err)
	}

	rows, err := s.db().QueryContext(ctx, `WITH RECURSIVE perimeter(entity_id, effective_from, effective_to, depth) AS (
		SELECT parent_entity_id, '0001-01-01', NULL, 0
		FROM consolidation_groups WHERE id = ?
		UNION ALL
		SELECT oi.child_entity_id,
			CASE WHEN oi.effective_from > p.effective_from THEN oi.effective_from ELSE p.effective_from END,
			CASE
				WHEN p.effective_to IS NULL THEN oi.effective_to
				WHEN oi.effective_to IS NULL THEN p.effective_to
				WHEN oi.effective_to < p.effective_to THEN oi.effective_to
				ELSE p.effective_to
			END,
			p.depth + 1
		FROM perimeter p
		JOIN ownership_interests oi ON oi.parent_entity_id = p.entity_id AND oi.ownership_bps = 10000
		WHERE oi.effective_from <= COALESCE(p.effective_to, '9999-12-31')
		  AND p.effective_from <= COALESCE(oi.effective_to, '9999-12-31')
	)
		SELECT e.id, e.code, e.legal_name, e.functional_currency,
		b.id, b.code, b.name, b.kind, b.currency, b.accounting_basis, p.effective_from, COALESCE(p.effective_to, ''), p.depth
		FROM perimeter p
		JOIN entities e ON e.id = p.entity_id
		LEFT JOIN books b ON b.entity_id = e.id AND b.kind = 'ACTUAL' AND b.status = 'ACTIVE'
		WHERE p.depth = 0 OR (p.effective_from <= ? AND (p.effective_to IS NULL OR p.effective_to >= ?))
		ORDER BY e.code, p.effective_from`, result.ID, throughDate, fromDate)
	if err != nil {
		return ResolvedScope{}, storesqlite.MapError("resolve consolidation perimeter", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	bookIndexes := make(map[string]int)
	for rows.Next() {
		var book ScopedBook
		var entityCurrency, effectiveFrom, effectiveTo string
		var depth int
		var bookID, bookCode, bookName, bookKind, bookCurrency, bookBasis sql.NullString
		if err := rows.Scan(
			&book.EntityID, &book.EntityCode, &book.EntityName, &entityCurrency,
			&bookID, &bookCode, &bookName, &bookKind, &bookCurrency, &bookBasis, &effectiveFrom, &effectiveTo, &depth,
		); err != nil {
			return ResolvedScope{}, storesqlite.MapError("read consolidation perimeter", err)
		}
		if !bookID.Valid {
			return ResolvedScope{}, apperr.New(apperr.Integrity, "GROUP_ENTITY_BOOK_MISSING", fmt.Sprintf("consolidated entity %s has no active actual book", book.EntityCode))
		}
		if entityCurrency != result.Currency || bookCurrency.String != result.Currency {
			return ResolvedScope{}, apperr.New(apperr.Integrity, "GROUP_CURRENCY_MISMATCH", fmt.Sprintf("consolidated entity %s does not use %s", book.EntityCode, result.Currency))
		}
		if bookBasis.String != "ACCRUAL" {
			return ResolvedScope{}, apperr.New(apperr.Validation, "BASIS_NOT_SUPPORTED", fmt.Sprintf("financial reports currently support accrual-basis books only; %s is %s", book.EntityCode, bookBasis.String))
		}
		book.BookID = bookID.String
		book.BookCode = bookCode.String
		book.BookName = bookName.String
		book.Kind = bookKind.String
		book.Basis = bookBasis.String
		if retainIntervals && depth > 0 {
			interval := ConsolidationInterval{EffectiveFrom: effectiveFrom, EffectiveTo: effectiveTo}
			if index, ok := bookIndexes[book.BookID]; ok {
				result.Books[index].ConsolidationIntervals = append(result.Books[index].ConsolidationIntervals, interval)
				continue
			}
			book.ConsolidationIntervals = []ConsolidationInterval{interval}
		}
		bookIndexes[book.BookID] = len(result.Books)
		result.Books = append(result.Books, book)
	}
	if err := rows.Err(); err != nil {
		return ResolvedScope{}, storesqlite.MapError("read consolidation perimeter", err)
	}

	var elimination ScopedBook
	var eliminationCurrency string
	err = s.db().QueryRowContext(ctx, `SELECT id, code, name, kind, currency, accounting_basis
		FROM books
		WHERE group_id = ? AND kind = 'ELIMINATION' AND status = 'ACTIVE'`, result.ID).Scan(
		&elimination.BookID, &elimination.BookCode, &elimination.BookName, &elimination.Kind, &eliminationCurrency, &elimination.Basis,
	)
	if err == sql.ErrNoRows {
		return ResolvedScope{}, apperr.New(apperr.Integrity, "GROUP_ELIMINATION_BOOK_MISSING", "consolidation group has no active elimination book")
	}
	if err != nil {
		return ResolvedScope{}, storesqlite.MapError("resolve elimination book", err)
	}
	if eliminationCurrency != result.Currency {
		return ResolvedScope{}, apperr.New(apperr.Integrity, "GROUP_CURRENCY_MISMATCH", "elimination book currency differs from the group currency")
	}
	if elimination.Basis != "ACCRUAL" {
		return ResolvedScope{}, apperr.New(apperr.Validation, "BASIS_NOT_SUPPORTED", "financial reports currently support accrual-basis books only")
	}
	result.Books = append(result.Books, elimination)
	result.Basis = "ACCRUAL"
	return result, nil
}

type centsByBook map[string]int64

func addCents(target centsByBook, source centsByBook, multiplier int64) error {
	for bookID, cents := range source {
		if multiplier == -1 {
			var err error
			cents, err = checkedNegate(cents)
			if err != nil {
				return err
			}
		} else if multiplier != 1 {
			return apperr.New(apperr.Integrity, "REPORT_ARITHMETIC_INVALID", "internal report multiplier must be one or negative one")
		}
		value, err := checkedAdd(target[bookID], cents)
		if err != nil {
			return err
		}
		target[bookID] = value
	}
	return nil
}

func scaleCents(source centsByBook, multiplier int64) (centsByBook, error) {
	result := make(centsByBook, len(source))
	if err := addCents(result, source, multiplier); err != nil {
		return nil, err
	}
	return result, nil
}

func breakdown(scope ResolvedScope, amounts centsByBook) (Breakdown, error) {
	result := Breakdown{}
	entities := make(map[string]int64)
	for _, book := range scope.Books {
		amount := amounts[book.BookID]
		if book.Kind == "ELIMINATION" {
			value, err := checkedAdd(result.EliminationsCents, amount)
			if err != nil {
				return Breakdown{}, err
			}
			result.EliminationsCents = value
			continue
		}
		value, err := checkedAdd(entities[book.EntityID], amount)
		if err != nil {
			return Breakdown{}, err
		}
		entities[book.EntityID] = value
	}
	for _, book := range scope.Books {
		if book.Kind != "ACTUAL" {
			continue
		}
		if _, seen := entities[book.EntityID]; !seen {
			continue
		}
		result.ByEntity = append(result.ByEntity, EntityAmount{
			EntityID: book.EntityID, EntityCode: book.EntityCode, EntityName: book.EntityName,
			Cents: entities[book.EntityID],
		})
		delete(entities, book.EntityID)
	}
	for _, amount := range result.ByEntity {
		value, err := checkedAdd(result.ConsolidatedCents, amount.Cents)
		if err != nil {
			return Breakdown{}, err
		}
		result.ConsolidatedCents = value
	}
	value, err := checkedAdd(result.ConsolidatedCents, result.EliminationsCents)
	if err != nil {
		return Breakdown{}, err
	}
	result.ConsolidatedCents = value
	return result, nil
}

func componentBalancesZero(value Breakdown) bool {
	if value.EliminationsCents != 0 || value.ConsolidatedCents != 0 {
		return false
	}
	for _, amount := range value.ByEntity {
		if amount.Cents != 0 {
			return false
		}
	}
	return true
}
