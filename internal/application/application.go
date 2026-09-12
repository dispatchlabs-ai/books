// Package application binds client workflows to one registered company.
// Transport handlers never select arbitrary ledger books or filesystem paths.
package application

import (
	"context"
	"database/sql"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/banking"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type Company struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Entity   string `json:"entity"`
	Book     string `json:"book"`
	Currency string `json:"currency"`
	Basis    string `json:"basis"`
}
type Service struct {
	store   *storesqlite.Store
	company Company
	actor   string
}

func Open(ctx context.Context, configPath, company, actor string, mode storesqlite.Mode) (*Service, error) {
	if strings.TrimSpace(company) == "" {
		return nil, apperr.New(apperr.Invalid, "COMPANY_REQUIRED", "select a registered company explicitly")
	}
	config, e := booksconfig.Load(configPath)
	if e != nil {
		return nil, apperr.Wrap(apperr.Unavailable, "COMPANY_CONFIG_UNAVAILABLE", "Books configuration could not be loaded", e)
	}
	resolved, e := config.Resolve(configPath, company)
	if e != nil {
		return nil, apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company is not registered")
	}
	store, e := storesqlite.Open(ctx, resolved.Database, mode)
	if e != nil {
		return nil, e
	}
	closeError := func(e error) (*Service, error) { _ = store.Close(); return nil, e }
	if e = store.VerifySchema(ctx); e != nil {
		return closeError(e)
	}
	var uuid string
	if e = store.DB().QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton=1`).Scan(&uuid); e != nil {
		return closeError(e)
	}
	if resolved.Company.DatabaseUUID == "" || resolved.Company.DatabaseUUID != uuid {
		return closeError(apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "company must be bound to this database; verify it through the CLI first"))
	}
	var bookID string
	e = store.DB().QueryRowContext(ctx, `SELECT b.id FROM books b JOIN entities e ON e.id=b.entity_id WHERE b.code=? AND e.code=? AND b.kind='ACTUAL' AND b.status='ACTIVE' AND e.status='ACTIVE' AND b.currency=? AND e.functional_currency=b.currency AND b.accounting_basis='ACCRUAL'`, resolved.Company.BookCode, resolved.Company.EntityCode, resolved.Company.Currency).Scan(&bookID)
	if e == sql.ErrNoRows {
		return closeError(apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "configured company does not match an active actual book"))
	}
	if e != nil {
		return closeError(e)
	}
	return &Service{store: store, company: Company{Key: resolved.Key, Name: resolved.Company.Name, Entity: resolved.Company.EntityCode, Book: resolved.Company.BookCode, Currency: resolved.Company.Currency, Basis: "ACCRUAL"}, actor: actor}, nil
}
func (s *Service) Close() error     { return s.store.Close() }
func (s *Service) Company() Company { return s.company }

// AsActor is used only after the transport authenticates the principal.
func (s *Service) AsActor(actor string) *Service { copy := *s; copy.actor = actor; return &copy }
func (s *Service) ledger() *ledger.Service       { return ledger.NewService(s.store, s.actor) }
func (s *Service) Upload(ctx context.Context, key, name string, data []byte) (ledger.BankImportJob, error) {
	return s.ledger().UploadBankImport(ctx, s.company.Book, key, name, data)
}
func (s *Service) UploadWithOptions(ctx context.Context, key, name string, data []byte, options banking.Options) (ledger.BankImportJob, error) {
	return s.ledger().UploadBankImportWithOptions(ctx, s.company.Book, key, name, data, options)
}
func (s *Service) Process(ctx context.Context, id string) (ledger.BankImportJob, error) {
	return s.ledger().ProcessBankImport(ctx, s.company.Book, id)
}
func (s *Service) Job(ctx context.Context, id string) (ledger.BankImportJob, error) {
	return s.ledger().GetBankImportJob(ctx, s.company.Book, id)
}
func (s *Service) Source(ctx context.Context, id string) ([]byte, error) {
	return s.ledger().BankImportSource(ctx, s.company.Book, id)
}
func (s *Service) Preview(ctx context.Context, job, key string, choices ledger.BankImportChoices) (ledger.BankImportPlan, error) {
	return s.ledger().PreviewBankImport(ctx, s.company.Book, job, key, choices)
}
func (s *Service) Plan(ctx context.Context, id string) (ledger.BankImportPlan, error) {
	return s.ledger().GetBankImportPlan(ctx, s.company.Book, id)
}
func (s *Service) Apply(ctx context.Context, id, digest string) (ledger.BankImportReceipt, error) {
	return s.ledger().ApplyBankImport(ctx, s.company.Book, id, digest)
}
func (s *Service) Pending(ctx context.Context) ([]string, error) {
	return s.ledger().PendingBankImportIDs(ctx, s.company.Book, 20)
}
func (s *Service) Accounts(ctx context.Context) ([]ledger.Account, error) {
	return s.ledger().ListAccounts(ctx, s.company.Book)
}
func (s *Service) StatementAccounts(ctx context.Context) ([]ledger.StatementAccount, error) {
	values, e := s.ledger().ListStatementAccounts(ctx, s.company.Entity)
	if e != nil {
		return nil, e
	}
	out := []ledger.StatementAccount{}
	for _, a := range values {
		if a.BookCode == s.company.Book {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *Service) TrialBalance(ctx context.Context, asOf string) (report.TrialBalanceReport, error) {
	return report.NewService(s.store).TrialBalance(ctx, report.TrialBalanceInput{Scope: report.Scope{EntityCode: s.company.Entity}, AsOfDate: asOf})
}
func (s *Service) BalanceSheet(ctx context.Context, asOf string) (report.BalanceSheetReport, error) {
	return report.NewService(s.store).BalanceSheet(ctx, report.BalanceSheetInput{Scope: report.Scope{EntityCode: s.company.Entity}, AsOfDate: asOf})
}
func (s *Service) ProfitLoss(ctx context.Context, from, to string) (report.ProfitLossReport, error) {
	return report.NewService(s.store).ProfitLoss(ctx, report.ProfitLossInput{Scope: report.Scope{EntityCode: s.company.Entity}, FromDate: from, ToDate: to})
}

// Transactions uses the monotonic per-book journal number as its cursor. The
// result describes current journal states; it is not an offline replication log.
func (s *Service) Transactions(ctx context.Context, after int64, limit int) ([]ledger.Journal, error) {
	if after < 0 || limit < 1 || limit > 200 {
		return nil, apperr.New(apperr.Invalid, "PAGE_INVALID", "after must be nonnegative and limit between 1 and 200")
	}
	rows, e := s.store.DB().QueryContext(ctx, `SELECT je.id FROM journal_entries je JOIN books b ON b.id=je.book_id WHERE b.code=? AND je.entry_number>? ORDER BY je.entry_number LIMIT ?`, s.company.Book, after, limit)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	iterationError := rows.Err()
	if e = rows.Close(); e != nil {
		return nil, e
	}
	if iterationError != nil {
		return nil, iterationError
	}
	result := []ledger.Journal{}
	for _, id := range ids {
		j, e := s.ledger().GetJournal(ctx, id)
		if e != nil {
			return nil, e
		}
		result = append(result, j)
	}
	return result, nil
}

func (s *Service) Matches(ctx context.Context, job string, choices ledger.BankImportChoices) (ledger.BankImportMatches, error) {
	return s.ledger().MatchBankImport(ctx, s.company.Book, job, choices)
}
