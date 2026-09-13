package application

import (
	"context"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"strings"
	"time"
)

func ValidateCompanyRestore(ctx context.Context, resolved booksconfig.ResolvedCompany, source string) (
	booksconfig.ResolvedCompany, storesqlite.RestoreValidation, storesqlite.RestoreExpectation, bool, error,
) {
	expected := storesqlite.RestoreExpectation{
		DatabaseUUID: resolved.Company.DatabaseUUID,
		EntityCode:   resolved.Company.EntityCode,
		BookCode:     resolved.Company.BookCode,
	}
	validation, err := storesqlite.ValidateRestore(ctx, resolved.Database, source, expected)
	if err != nil {
		return booksconfig.ResolvedCompany{}, storesqlite.RestoreValidation{}, storesqlite.RestoreExpectation{}, false, err
	}
	backfillIdentity := false
	if expected.DatabaseUUID == "" {
		if validation.PreviousTargetDatabaseUUID == "" {
			return booksconfig.ResolvedCompany{}, storesqlite.RestoreValidation{}, storesqlite.RestoreExpectation{}, false,
				apperr.New(apperr.Conflict, "RESTORE_DATABASE_IDENTITY_MISSING", "books.toml has no database UUID for this company; restore cannot safely adopt a backup while the registered database is missing")
		}
		expected.DatabaseUUID = validation.PreviousTargetDatabaseUUID
		backfillIdentity = true
	}
	return resolved, validation, expected, backfillIdentity, nil
}

type FiscalYearResult struct {
	Company        string `json:"company"`
	FiscalYear     int    `json:"fiscal_year"`
	PeriodsCreated int    `json:"periods_created"`
	DryRun         bool   `json:"dry_run"`
}

func (s *Service) Periods(ctx context.Context) ([]ledger.Period, error) {
	return s.ledger().ListPeriods(ctx, s.company.Book)
}
func (s *Service) AddFiscalYear(ctx context.Context, year int, dryRun bool) (FiscalYearResult, error) {
	if year < 1900 || year > 9999 {
		return FiscalYearResult{}, apperr.New(apperr.Invalid, "FISCAL_YEAR_INVALID", "year must be a four-digit fiscal year")
	}
	settings, err := s.settings()
	if err != nil {
		return FiscalYearResult{}, err
	}
	endMonth := time.Month(settings.FiscalYearEnd)
	startMonth := endMonth%12 + 1
	startYear := year
	if startMonth != time.January {
		startYear--
	}
	planned := MonthlyPeriods(time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.UTC), endMonth)
	created, err := s.ledger().ConfigureBookPeriods(ctx, s.company.Book, planned, dryRun)
	if err != nil {
		return FiscalYearResult{}, err
	}
	return FiscalYearResult{Company: s.company.Key, FiscalYear: year, PeriodsCreated: created, DryRun: dryRun}, nil
}

type ReopenPeriodResult struct {
	Company string `json:"company"`
	Period  string `json:"period"`
	Reason  string `json:"reason"`
	Status  string `json:"status"`
	DryRun  bool   `json:"dry_run,omitempty"`
}

func (s *Service) ReopenPeriod(ctx context.Context, period, reason string, dryRun bool) (ReopenPeriodResult, error) {
	period = strings.ToUpper(strings.TrimSpace(period))
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ReopenPeriodResult{}, apperr.New(apperr.Invalid, "REOPEN_REASON_REQUIRED", "an audit reason is required")
	}
	result := ReopenPeriodResult{Company: s.company.Key, Period: period, Reason: reason, Status: "OPEN", DryRun: dryRun}
	if !dryRun {
		return result, s.ledger().ReopenPeriod(ctx, s.company.Book, period, reason)
	}
	periods, err := s.Periods(ctx)
	if err != nil {
		return ReopenPeriodResult{}, err
	}
	for _, p := range periods {
		if p.Code == period {
			if p.BookStatus != "CLOSED" {
				return ReopenPeriodResult{}, apperr.New(apperr.Conflict, "PERIOD_NOT_CLOSED", "book period is not closed")
			}
			result.Status = "OPEN (PREVIEW)"
			return result, nil
		}
	}
	return ReopenPeriodResult{}, apperr.New(apperr.NotFound, "BOOK_PERIOD_NOT_FOUND", "period is not configured for this book")
}

func PersistCompanyDatabaseUUID(resolved booksconfig.ResolvedCompany, databaseUUID string) (booksconfig.ResolvedCompany, error) {
	updated, err := booksconfig.Update(resolved.ConfigPath, nil, func(current *booksconfig.Config, _ bool) error {
		company, ok := current.Companies[resolved.Key]
		if !ok {
			return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is no longer registered", resolved.Key))
		}
		currentResolved, err := current.Resolve(resolved.ConfigPath, resolved.Key)
		if err != nil {
			return apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve company while binding database identity", err)
		}
		if currentResolved.Database != resolved.Database {
			return apperr.New(apperr.Conflict, "COMPANY_CONFIG_CHANGED", "registered company database path changed while its identity was being bound")
		}
		if company.DatabaseUUID != "" && company.DatabaseUUID != databaseUUID {
			return apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "registered company database identity changed while books.toml was being updated")
		}
		company.DatabaseUUID = databaseUUID
		current.Companies[resolved.Key] = company
		return nil
	})
	if err != nil {
		return booksconfig.ResolvedCompany{}, ConfigMutationError("bind registered company database identity", err)
	}
	updatedResolved, err := updated.Resolve(resolved.ConfigPath, resolved.Key)
	if err != nil {
		return booksconfig.ResolvedCompany{}, apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve identity-bound company", err)
	}
	return updatedResolved, nil
}

func VerifyCompanyIdentity(ctx context.Context, store *storesqlite.Store, resolved booksconfig.ResolvedCompany) (string, error) {
	var databaseUUID string
	if err := store.DB().QueryRowContext(ctx, `SELECT database_uuid
		FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		return "", storesqlite.MapError("read registered company database identity", err)
	}
	var companyMatches int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*)
		FROM books book JOIN entities entity ON entity.id = book.entity_id
		WHERE book.code = ? AND entity.code = ? AND book.currency=? AND entity.functional_currency=book.currency`, resolved.Company.BookCode, resolved.Company.EntityCode, resolved.Company.Currency).Scan(&companyMatches); err != nil {
		return "", storesqlite.MapError("verify registered company database identity", err)
	}
	if companyMatches != 1 || (resolved.Company.DatabaseUUID != "" && resolved.Company.DatabaseUUID != databaseUUID) {
		return "", apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "registered company database identity does not match books.toml")
	}
	return databaseUUID, nil
}

// Dashboard counts are scoped even when several companies share one database.
type Dashboard struct {
	Company            string `json:"company"`
	Name               string `json:"name"`
	EntityCode         string `json:"entity_code"`
	Currency           string `json:"currency"`
	Accounts           int    `json:"accounts"`
	PostedTransactions int    `json:"posted_transactions"`
	Drafts             int    `json:"drafts"`
	OpenPeriods        int    `json:"open_periods"`
}

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	result := Dashboard{Company: s.company.Key, Name: s.company.Name, EntityCode: s.company.Entity, Currency: s.company.Currency}
	for _, q := range []struct {
		query  string
		target *int
	}{
		{`SELECT COUNT(*) FROM book_accounts ba JOIN books b ON b.id=ba.book_id WHERE b.code=?`, &result.Accounts},
		{`SELECT COUNT(*) FROM journal_entries j JOIN books b ON b.id=j.book_id WHERE b.code=? AND j.status='POSTED'`, &result.PostedTransactions},
		{`SELECT COUNT(*) FROM journal_entries j JOIN books b ON b.id=j.book_id WHERE b.code=? AND j.status='DRAFT'`, &result.Drafts},
		{`SELECT COUNT(*) FROM book_periods bp JOIN books b ON b.id=bp.book_id WHERE b.code=? AND bp.status='OPEN'`, &result.OpenPeriods},
	} {
		if err := s.store.DB().QueryRowContext(ctx, q.query, s.company.Book).Scan(q.target); err != nil {
			return Dashboard{}, err
		}
	}
	return result, nil
}
