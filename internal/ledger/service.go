package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type Service struct {
	store *storesqlite.Store
	actor string
}

func NewService(store *storesqlite.Store, actor string) *Service {
	return &Service{store: store, actor: strings.TrimSpace(actor)}
}

func normalizeCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

func validateDate(value, field string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return apperr.New(apperr.Invalid, "DATE_INVALID", field+" must be an ISO-8601 date")
	}
	return nil
}

func nilIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func (s *Service) requireActor() error {
	if s.actor == "" {
		return apperr.New(apperr.Invalid, "ACTOR_REQUIRED", "actor is required for every mutation")
	}
	return nil
}

func lookupID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, code string) (string, error) {
	allowed := map[string]bool{"entities": true, "books": true, "accounts": true, "fiscal_periods": true, "consolidation_groups": true}
	if !allowed[table] {
		return "", fmt.Errorf("unsupported lookup table %q", table)
	}
	var id string
	err := q.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE code = ?", normalizeCode(code)).Scan(&id)
	if err == sql.ErrNoRows {
		return "", apperr.New(apperr.NotFound, "RESOURCE_NOT_FOUND", fmt.Sprintf("%s %q was not found", strings.TrimSuffix(table, "s"), code))
	}
	if err != nil {
		return "", storesqlite.MapError("look up "+table, err)
	}
	return id, nil
}

func (s *Service) CreateEntity(ctx context.Context, input CreateEntityInput) (Entity, error) {
	if err := s.requireActor(); err != nil {
		return Entity{}, err
	}
	input.Code = normalizeCode(input.Code)
	input.Currency = normalizeCode(input.Currency)
	input.BookCode = normalizeCode(input.BookCode)
	input.LegalName = strings.TrimSpace(input.LegalName)
	input.BookName = strings.TrimSpace(input.BookName)
	input.Basis = normalizeCode(input.Basis)
	if input.Code == "" || input.LegalName == "" || input.Currency == "" {
		return Entity{}, apperr.New(apperr.Invalid, "ENTITY_INVALID", "entity code, legal name, and currency are required")
	}
	if !money.IsSupportedCurrency(input.Currency) {
		return Entity{}, apperr.New(apperr.Invalid, "CURRENCY_NOT_SUPPORTED", "this release supports USD as its only functional currency")
	}
	if input.BookCode == "" {
		input.BookCode = input.Code
	}
	if input.BookName == "" {
		input.BookName = input.LegalName + " Actual"
	}
	if input.Basis == "" {
		input.Basis = "ACCRUAL"
	}
	if input.Basis != "ACCRUAL" {
		return Entity{}, apperr.New(apperr.Invalid, "BASIS_NOT_SUPPORTED", "cash-basis accounting is not supported; accounting basis must be ACCRUAL")
	}
	entityID, err := storesqlite.NewID()
	if err != nil {
		return Entity{}, err
	}
	bookID, err := storesqlite.NewID()
	if err != nil {
		return Entity{}, err
	}
	now := storesqlite.UTCNow()
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Entity{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := tx.ExecContext(ctx, `INSERT INTO entities
        (id, code, legal_name, functional_currency, created_at) VALUES (?, ?, ?, ?, ?)`,
		entityID, input.Code, input.LegalName, input.Currency, now); err != nil {
		return Entity{}, storesqlite.MapError("create entity", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO books
        (id, code, name, kind, entity_id, accounting_basis, currency, created_at)
        VALUES (?, ?, ?, 'ACTUAL', ?, ?, ?, ?)`,
		bookID, input.BookCode, input.BookName, entityID, input.Basis, input.Currency, now); err != nil {
		return Entity{}, storesqlite.MapError("create entity book", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO book_periods(book_id, period_id)
        SELECT ?, id FROM fiscal_periods`, bookID); err != nil {
		return Entity{}, storesqlite.MapError("open existing periods for entity", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "entity create", AggregateType: "entity", AggregateID: entityID,
		Payload: map[string]any{"code": input.Code, "legal_name": input.LegalName, "currency": input.Currency, "book_id": bookID, "book_code": input.BookCode},
	}); err != nil {
		return Entity{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entity{}, storesqlite.MapError("commit entity", err)
	}
	return Entity{ID: entityID, Code: input.Code, LegalName: input.LegalName, FunctionalCurrency: input.Currency, Status: "ACTIVE", BookID: bookID, BookCode: input.BookCode}, nil
}

func (s *Service) ListEntities(ctx context.Context) ([]Entity, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT e.id, e.code, e.legal_name, e.functional_currency, e.status,
        COALESCE(b.id, ''), COALESCE(b.code, '')
        FROM entities e LEFT JOIN books b ON b.entity_id = e.id AND b.kind = 'ACTUAL' AND b.status = 'ACTIVE'
        ORDER BY e.code`)
	if err != nil {
		return nil, storesqlite.MapError("list entities", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []Entity
	for rows.Next() {
		var entity Entity
		if err := rows.Scan(&entity.ID, &entity.Code, &entity.LegalName, &entity.FunctionalCurrency, &entity.Status, &entity.BookID, &entity.BookCode); err != nil {
			return nil, err
		}
		result = append(result, entity)
	}
	return result, rows.Err()
}

func (s *Service) ListBooks(ctx context.Context) ([]Book, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT b.id, b.code, b.name, b.kind,
        COALESCE(e.code, ''), COALESCE(g.code, ''), b.accounting_basis, b.currency,
        b.status, b.created_at
        FROM books b
        LEFT JOIN entities e ON e.id = b.entity_id
        LEFT JOIN consolidation_groups g ON g.id = b.group_id
        ORDER BY b.code`)
	if err != nil {
		return nil, storesqlite.MapError("list books", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []Book
	for rows.Next() {
		var book Book
		if err := rows.Scan(&book.ID, &book.Code, &book.Name, &book.Kind, &book.EntityCode,
			&book.GroupCode, &book.AccountingBasis, &book.Currency, &book.Status, &book.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, book)
	}
	return result, rows.Err()
}

func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (Group, error) {
	if err := s.requireActor(); err != nil {
		return Group{}, err
	}
	input.Code = normalizeCode(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.EliminationBookCode = normalizeCode(input.EliminationBookCode)
	if input.Code == "" || input.Name == "" || strings.TrimSpace(input.ParentEntity) == "" {
		return Group{}, apperr.New(apperr.Invalid, "GROUP_INVALID", "group code, name, and parent entity are required")
	}
	if input.EliminationBookCode == "" {
		input.EliminationBookCode = input.Code + "-ELIM"
	}
	if strings.TrimSpace(input.EliminationBookName) == "" {
		input.EliminationBookName = input.Name + " Eliminations"
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Group{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	parentID, err := lookupID(ctx, tx, "entities", input.ParentEntity)
	if err != nil {
		return Group{}, err
	}
	var currency string
	if err := tx.QueryRowContext(ctx, "SELECT functional_currency FROM entities WHERE id = ?", parentID).Scan(&currency); err != nil {
		return Group{}, storesqlite.MapError("read parent currency", err)
	}
	groupID, err := storesqlite.NewID()
	if err != nil {
		return Group{}, err
	}
	bookID, err := storesqlite.NewID()
	if err != nil {
		return Group{}, err
	}
	now := storesqlite.UTCNow()
	if _, err := tx.ExecContext(ctx, `INSERT INTO consolidation_groups
        (id, code, name, parent_entity_id, currency, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		groupID, input.Code, input.Name, parentID, currency, now); err != nil {
		return Group{}, storesqlite.MapError("create consolidation group", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO books
        (id, code, name, kind, group_id, accounting_basis, currency, created_at)
        VALUES (?, ?, ?, 'ELIMINATION', ?, 'ACCRUAL', ?, ?)`,
		bookID, input.EliminationBookCode, input.EliminationBookName, groupID, currency, now); err != nil {
		return Group{}, storesqlite.MapError("create elimination book", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO book_periods(book_id, period_id)
        SELECT ?, id FROM fiscal_periods`, bookID); err != nil {
		return Group{}, storesqlite.MapError("open periods for elimination book", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "group create", AggregateType: "consolidation_group", AggregateID: groupID,
		Payload: map[string]any{"code": input.Code, "parent_entity_id": parentID, "currency": currency, "elimination_book_id": bookID},
	}); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(); err != nil {
		return Group{}, storesqlite.MapError("commit consolidation group", err)
	}
	return Group{ID: groupID, Code: input.Code, Name: input.Name, ParentEntityID: parentID, Currency: currency, EliminationBookID: bookID, EliminationBookCode: input.EliminationBookCode}, nil
}

func (s *Service) AddOwnership(ctx context.Context, parent, child, effectiveFrom, effectiveTo string) (string, error) {
	if err := s.requireActor(); err != nil {
		return "", err
	}
	if err := validateDate(effectiveFrom, "effective from"); err != nil {
		return "", err
	}
	if effectiveTo != "" {
		if err := validateDate(effectiveTo, "effective to"); err != nil {
			return "", err
		}
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	parentID, err := lookupID(ctx, tx, "entities", parent)
	if err != nil {
		return "", err
	}
	childID, err := lookupID(ctx, tx, "entities", child)
	if err != nil {
		return "", err
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ownership_interests
        (id, parent_entity_id, child_entity_id, ownership_bps, effective_from, effective_to, created_at)
        VALUES (?, ?, ?, 10000, ?, ?, ?)`, id, parentID, childID, effectiveFrom, nilIfEmpty(effectiveTo), storesqlite.UTCNow()); err != nil {
		return "", storesqlite.MapError("set ownership", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "ownership set", AggregateType: "ownership_interest", AggregateID: id,
		Payload: map[string]any{"parent_entity_id": parentID, "child_entity_id": childID, "ownership_bps": 10000, "effective_from": effectiveFrom, "effective_to": effectiveTo},
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", storesqlite.MapError("commit ownership", err)
	}
	return id, nil
}

func (s *Service) CreateAccount(ctx context.Context, input CreateAccountInput) (Account, error) {
	if err := s.requireActor(); err != nil {
		return Account{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	result, err := s.createAccountTx(ctx, tx, input)
	if err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, storesqlite.MapError("commit account", err)
	}
	return result, nil
}

func (s *Service) createAccountTx(ctx context.Context, tx *sql.Tx, input CreateAccountInput) (Account, error) {
	input.Code = normalizeCode(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.Type = normalizeCode(input.Type)
	input.Subtype = normalizeCode(input.Subtype)
	input.NormalBalance = normalizeCode(input.NormalBalance)
	input.StatementSection = normalizeCode(input.StatementSection)
	if input.ActiveFrom == "" {
		input.ActiveFrom = "1900-01-01"
	}
	if err := validateDate(input.ActiveFrom, "active from"); err != nil {
		return Account{}, err
	}
	validTypes := map[string]bool{"ASSET": true, "LIABILITY": true, "EQUITY": true, "REVENUE": true, "EXPENSE": true}
	if input.Code == "" || input.Name == "" || !validTypes[input.Type] {
		return Account{}, apperr.New(apperr.Invalid, "ACCOUNT_INVALID", "account code, name, and valid type are required")
	}
	if input.NormalBalance == "" {
		if input.Type == "ASSET" || input.Type == "EXPENSE" {
			input.NormalBalance = "DEBIT"
		} else {
			input.NormalBalance = "CREDIT"
		}
	}
	if input.NormalBalance != "DEBIT" && input.NormalBalance != "CREDIT" {
		return Account{}, apperr.New(apperr.Invalid, "NORMAL_BALANCE_INVALID", "normal balance must be DEBIT or CREDIT")
	}
	id, err := storesqlite.NewID()
	if err != nil {
		return Account{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO accounts
        (id, code, name, account_type, subtype, normal_balance, statement_section, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, input.Code, input.Name, input.Type, input.Subtype,
		input.NormalBalance, input.StatementSection, storesqlite.UTCNow()); err != nil {
		return Account{}, storesqlite.MapError("create account", err)
	}
	bookCodes := append([]string(nil), input.BookCodes...)
	sort.Strings(bookCodes)
	for _, bookCode := range bookCodes {
		bookID, err := lookupID(ctx, tx, "books", bookCode)
		if err != nil {
			return Account{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO book_accounts
            (book_id, account_id, active_from, created_at) VALUES (?, ?, ?, ?)`,
			bookID, id, input.ActiveFrom, storesqlite.UTCNow()); err != nil {
			return Account{}, storesqlite.MapError("enable account for book", err)
		}
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "account create", AggregateType: "account", AggregateID: id,
		Payload: map[string]any{"code": input.Code, "name": input.Name, "type": input.Type, "subtype": input.Subtype, "normal_balance": input.NormalBalance, "book_codes": bookCodes},
	}); err != nil {
		return Account{}, err
	}
	return Account{ID: id, Code: input.Code, Name: input.Name, Type: input.Type, Subtype: input.Subtype, NormalBalance: input.NormalBalance, StatementSection: input.StatementSection}, nil
}

func (s *Service) ListAccounts(ctx context.Context, bookCode string) ([]Account, error) {
	query := `SELECT a.id, a.code, a.name, a.account_type, a.subtype, a.normal_balance, a.statement_section FROM accounts a`
	var args []any
	bookCode = normalizeCode(bookCode)
	if bookCode != "" {
		query = `SELECT a.id, a.code, a.name, a.account_type, a.subtype, a.normal_balance,
            a.statement_section, b.code, ba.posting_enabled, ba.active_from,
            COALESCE(ba.active_to, '')
            FROM accounts a
            JOIN book_accounts ba ON ba.account_id = a.id
            JOIN books b ON b.id = ba.book_id
            WHERE b.code = ?`
		args = append(args, bookCode)
	}
	query += " ORDER BY a.code"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list accounts", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []Account
	for rows.Next() {
		var account Account
		if bookCode == "" {
			if err := rows.Scan(&account.ID, &account.Code, &account.Name, &account.Type, &account.Subtype, &account.NormalBalance, &account.StatementSection); err != nil {
				return nil, err
			}
		} else {
			var postingEnabled int
			if err := rows.Scan(&account.ID, &account.Code, &account.Name, &account.Type, &account.Subtype,
				&account.NormalBalance, &account.StatementSection, &account.BookCode, &postingEnabled,
				&account.ActiveFrom, &account.ActiveTo); err != nil {
				return nil, err
			}
			enabled := postingEnabled == 1
			account.PostingEnabled = &enabled
		}
		result = append(result, account)
	}
	return result, rows.Err()
}

func (s *Service) ConfigureBookAccount(ctx context.Context, bookCode, accountCode, activeFrom, activeTo string, postingEnabled bool) error {
	if err := s.requireActor(); err != nil {
		return err
	}
	if err := validateDate(activeFrom, "active from"); err != nil {
		return err
	}
	if activeTo != "" {
		if err := validateDate(activeTo, "active to"); err != nil {
			return err
		}
		if activeTo < activeFrom {
			return apperr.New(apperr.Invalid, "ACCOUNT_DATES_INVALID", "account active-to date precedes active-from date")
		}
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
	accountID, err := lookupID(ctx, tx, "accounts", accountCode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO book_accounts
		(book_id, account_id, posting_enabled, active_from, active_to, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id, account_id) DO UPDATE SET
		  posting_enabled = excluded.posting_enabled,
		  active_from = excluded.active_from,
		  active_to = excluded.active_to`,
		bookID, accountID, postingEnabled, activeFrom, nilIfEmpty(activeTo), storesqlite.UTCNow())
	if err != nil {
		return storesqlite.MapError("configure book account", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: s.actor, Command: "account configure", AggregateType: "book_account", AggregateID: bookID + ":" + accountID,
		Payload: map[string]any{"book": normalizeCode(bookCode), "account": normalizeCode(accountCode), "active_from": activeFrom, "active_to": activeTo, "posting_enabled": postingEnabled},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) CreatePeriod(ctx context.Context, input CreatePeriodInput) (Period, error) {
	if err := s.requireActor(); err != nil {
		return Period{}, err
	}
	input, err := normalizePeriodInput(input)
	if err != nil {
		return Period{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Period{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	period, err := createPeriodInTransaction(ctx, tx, s.actor, input)
	if err != nil {
		return Period{}, err
	}
	if err := tx.Commit(); err != nil {
		return Period{}, storesqlite.MapError("commit period", err)
	}
	return period, nil
}

func (s *Service) PreviewMissingPeriods(ctx context.Context, inputs []CreatePeriodInput) (int, error) {
	normalized, err := normalizePeriodInputs(inputs)
	if err != nil {
		return 0, err
	}
	missing, err := findMissingPeriods(ctx, s.store.DB(), normalized)
	if err != nil {
		return 0, err
	}
	return len(missing), nil
}

func (s *Service) CreateMissingPeriods(ctx context.Context, inputs []CreatePeriodInput) (int, error) {
	if err := s.requireActor(); err != nil {
		return 0, err
	}
	normalized, err := normalizePeriodInputs(inputs)
	if err != nil {
		return 0, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	missing, err := findMissingPeriods(ctx, tx, normalized)
	if err != nil {
		return 0, err
	}
	for _, input := range missing {
		if _, err := createPeriodInTransaction(ctx, tx, s.actor, input); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, storesqlite.MapError("commit missing fiscal periods", err)
	}
	return len(missing), nil
}

type periodQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func normalizePeriodInput(input CreatePeriodInput) (CreatePeriodInput, error) {
	input.Code = normalizeCode(input.Code)
	if input.Code == "" || input.FiscalYear < 1900 || input.PeriodNumber < 1 || input.PeriodNumber > 53 {
		return CreatePeriodInput{}, apperr.New(apperr.Invalid, "PERIOD_INVALID", "period code, fiscal year, and period number are required")
	}
	if err := validateDate(input.StartDate, "start date"); err != nil {
		return CreatePeriodInput{}, err
	}
	if err := validateDate(input.EndDate, "end date"); err != nil {
		return CreatePeriodInput{}, err
	}
	if input.EndDate < input.StartDate {
		return CreatePeriodInput{}, apperr.New(apperr.Invalid, "PERIOD_INVALID", "period end date precedes its start date")
	}
	return input, nil
}

func normalizePeriodInputs(inputs []CreatePeriodInput) ([]CreatePeriodInput, error) {
	result := make([]CreatePeriodInput, 0, len(inputs))
	seen := make(map[string]CreatePeriodInput, len(inputs))
	for _, input := range inputs {
		normalized, err := normalizePeriodInput(input)
		if err != nil {
			return nil, err
		}
		if existing, ok := seen[normalized.Code]; ok {
			if !samePeriodDefinition(existing, normalized) {
				return nil, apperr.New(apperr.Conflict, "PERIOD_DEFINITION_CONFLICT", "the requested fiscal periods define one code more than once with different boundaries")
			}
			continue
		}
		seen[normalized.Code] = normalized
		result = append(result, normalized)
	}
	return result, nil
}

func findMissingPeriods(ctx context.Context, query periodQueryer, inputs []CreatePeriodInput) ([]CreatePeriodInput, error) {
	missing := make([]CreatePeriodInput, 0, len(inputs))
	for _, input := range inputs {
		var existing CreatePeriodInput
		err := query.QueryRowContext(ctx, `SELECT code, start_date, end_date, fiscal_year, period_number, is_year_end
			FROM fiscal_periods WHERE code = ?`, input.Code).Scan(
			&existing.Code, &existing.StartDate, &existing.EndDate, &existing.FiscalYear, &existing.PeriodNumber, &existing.YearEnd)
		if err == nil {
			if !samePeriodDefinition(existing, input) {
				return nil, apperr.New(apperr.Conflict, "PERIOD_DEFINITION_CONFLICT", fmt.Sprintf("existing fiscal period %s has different boundaries or fiscal metadata", input.Code))
			}
			continue
		}
		if err != sql.ErrNoRows {
			return nil, storesqlite.MapError("read existing fiscal period", err)
		}
		var overlapCode string
		err = query.QueryRowContext(ctx, `SELECT code FROM fiscal_periods
			WHERE end_date >= ? AND ? >= start_date ORDER BY start_date LIMIT 1`, input.StartDate, input.EndDate).Scan(&overlapCode)
		if err == nil {
			return nil, apperr.New(apperr.Conflict, "PERIOD_OVERLAP", fmt.Sprintf("fiscal period %s overlaps existing period %s", input.Code, overlapCode))
		}
		if err != sql.ErrNoRows {
			return nil, storesqlite.MapError("check fiscal period overlap", err)
		}
		missing = append(missing, input)
	}
	return missing, nil
}

func samePeriodDefinition(left, right CreatePeriodInput) bool {
	return left.Code == right.Code && left.StartDate == right.StartDate && left.EndDate == right.EndDate &&
		left.FiscalYear == right.FiscalYear && left.PeriodNumber == right.PeriodNumber && left.YearEnd == right.YearEnd
}

func createPeriodInTransaction(ctx context.Context, tx *sql.Tx, actor string, input CreatePeriodInput) (Period, error) {
	id, err := storesqlite.NewID()
	if err != nil {
		return Period{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fiscal_periods
		(id, code, start_date, end_date, fiscal_year, period_number, is_year_end, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, input.Code, input.StartDate, input.EndDate, input.FiscalYear, input.PeriodNumber, input.YearEnd, storesqlite.UTCNow()); err != nil {
		return Period{}, storesqlite.MapError("create fiscal period", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO book_periods(book_id, period_id)
		SELECT id, ? FROM books WHERE status = 'ACTIVE'`, id); err != nil {
		return Period{}, storesqlite.MapError("open fiscal period for books", err)
	}
	if _, err := storesqlite.AppendAudit(ctx, tx, storesqlite.AuditInput{
		Actor: actor, Command: "period create", AggregateType: "fiscal_period", AggregateID: id,
		Payload: map[string]any{"code": input.Code, "start_date": input.StartDate, "end_date": input.EndDate, "fiscal_year": input.FiscalYear, "period_number": input.PeriodNumber, "year_end": input.YearEnd},
	}); err != nil {
		return Period{}, err
	}
	return Period{ID: id, Code: input.Code, StartDate: input.StartDate, EndDate: input.EndDate, FiscalYear: input.FiscalYear, PeriodNumber: input.PeriodNumber, YearEnd: input.YearEnd}, nil
}

func (s *Service) ListPeriods(ctx context.Context, bookCode string) ([]Period, error) {
	query := `SELECT fp.id, fp.code, fp.start_date, fp.end_date, fp.fiscal_year, fp.period_number, fp.is_year_end FROM fiscal_periods fp`
	var args []any
	bookCode = normalizeCode(bookCode)
	if bookCode != "" {
		query = `SELECT fp.id, fp.code, fp.start_date, fp.end_date, fp.fiscal_year,
            fp.period_number, fp.is_year_end, b.code, bp.status, COALESCE(bp.close_digest, '')
            FROM fiscal_periods fp
            JOIN book_periods bp ON bp.period_id = fp.id
            JOIN books b ON b.id = bp.book_id
            WHERE b.code = ?`
		args = append(args, bookCode)
	}
	query += " ORDER BY fp.start_date"
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storesqlite.MapError("list periods", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []Period
	for rows.Next() {
		var period Period
		var yearEnd int
		if bookCode == "" {
			if err := rows.Scan(&period.ID, &period.Code, &period.StartDate, &period.EndDate, &period.FiscalYear, &period.PeriodNumber, &yearEnd); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&period.ID, &period.Code, &period.StartDate, &period.EndDate,
				&period.FiscalYear, &period.PeriodNumber, &yearEnd, &period.BookCode,
				&period.BookStatus, &period.CloseDigest); err != nil {
				return nil, err
			}
		}
		period.YearEnd = yearEnd == 1
		result = append(result, period)
	}
	return result, rows.Err()
}

func sourceDigest(input CreateJournalInput) (string, error) {
	copyInput := input
	copyInput.SourceSystem = normalizeCode(copyInput.SourceSystem)
	payload, err := json.Marshal(copyInput)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
