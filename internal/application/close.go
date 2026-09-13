package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"strings"
	"time"
)

const (
	periodClosePlanSchema = "books.period-close-plan/v1"
	yearClosePlanSchema   = "books.year-close-plan/v1"
)

type PeriodClosePlan struct {
	Schema       string `json:"schema"`
	Company      string `json:"company"`
	Book         string `json:"book"`
	Period       string `json:"period"`
	EndDate      string `json:"end_date"`
	LedgerDigest string `json:"ledger_digest"`
	CreatedAt    string `json:"created_at"`
	Digest       string `json:"digest"`
}
type PeriodCloseOutput struct {
	Plan   PeriodClosePlan `json:"plan"`
	Status string          `json:"status"`
	DryRun bool            `json:"dry_run"`
}
type YearClosePlan struct {
	Currency         string                    `json:"currency,omitempty"`
	Schema           string                    `json:"schema"`
	Company          string                    `json:"company"`
	Book             string                    `json:"book"`
	FiscalYear       int                       `json:"fiscal_year"`
	RetainedEarnings string                    `json:"retained_earnings"`
	NetIncomeCents   int64                     `json:"net_income_cents"`
	Journal          ledger.CreateJournalInput `json:"journal"`
	JournalDigest    string                    `json:"journal_digest"`
	CreatedAt        string                    `json:"created_at"`
	Digest           string                    `json:"digest"`
}
type YearCloseOutput struct {
	Plan        YearClosePlan `json:"plan"`
	Status      string        `json:"status"`
	Transaction *Transaction  `json:"transaction,omitempty"`
	DryRun      bool          `json:"dry_run"`
}

func DigestJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func periodCloseAlreadyApplied(ctx context.Context, store *storesqlite.Store, plan PeriodClosePlan) (bool, error) {
	var status, endDate, closeDigest string
	err := store.DB().QueryRowContext(ctx, `SELECT bp.status, fp.end_date, COALESCE(bp.close_digest, '')
		FROM book_periods bp
		JOIN books b ON b.id = bp.book_id
		JOIN fiscal_periods fp ON fp.id = bp.period_id
		WHERE b.code = ? AND fp.code = ?`, plan.Book, plan.Period).Scan(&status, &endDate, &closeDigest)
	if err == sql.ErrNoRows {
		return false, apperr.New(apperr.NotFound, "BOOK_PERIOD_NOT_FOUND", "period is not configured for this book")
	}
	if err != nil {
		return false, err
	}
	if status != "CLOSED" {
		return false, nil
	}
	if _, err := storesqlite.VerifyAudit(ctx, store.DB()); err != nil {
		return false, err
	}
	if endDate != plan.EndDate || closeDigest != plan.LedgerDigest {
		return false, apperr.New(apperr.Conflict, "CLOSE_PLAN_ALREADY_CLOSED", "period is already closed with evidence that does not match this plan")
	}
	return true, nil
}
func stampYearCloseJournal(input ledger.CreateJournalInput, company string, fiscalYear int) (ledger.CreateJournalInput, error) {
	seed := input
	seed.SourceSystem = ""
	seed.SourceKey = ""
	digest, err := DigestJSON(seed)
	if err != nil {
		return input, err
	}
	input.SourceSystem = "BOOKS_YEAR_CLOSE"
	input.SourceKey = fmt.Sprintf("%s:%d:%s", company, fiscalYear, digest[:16])
	return input, nil
}
func DigestPeriodClosePlan(plan PeriodClosePlan) (string, error) {
	plan.Digest = ""
	return DigestJSON(plan)
}
func DigestYearClosePlan(plan YearClosePlan) (string, error) {
	plan.Digest = ""
	return DigestJSON(plan)
}

func (s *Service) PlanPeriodClose(ctx context.Context, period string) (PeriodClosePlan, error) {
	period = strings.ToUpper(strings.TrimSpace(period))
	result, err := s.ledger().ClosePeriod(ctx, s.company.Book, period, true)
	if err != nil {
		return PeriodClosePlan{}, err
	}
	plan := PeriodClosePlan{Schema: periodClosePlanSchema, Company: s.company.Key, Book: s.company.Book, Period: period, EndDate: result.EndDate, LedgerDigest: result.Digest, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	plan.Digest, err = DigestPeriodClosePlan(plan)
	return plan, err
}
func (s *Service) ApplyPeriodClose(ctx context.Context, plan PeriodClosePlan, dryRun bool) (PeriodCloseOutput, error) {
	result := PeriodCloseOutput{Plan: plan, Status: "VALIDATED", DryRun: dryRun}
	if plan.Schema != periodClosePlanSchema {
		return result, apperr.New(apperr.Invalid, "CLOSE_PLAN_INVALID", "period close plan schema is unsupported")
	}
	digest, err := DigestPeriodClosePlan(plan)
	if err != nil {
		return result, err
	}
	if digest != plan.Digest {
		return result, apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "period close plan changed after generation")
	}
	if plan.Company != s.company.Key || plan.Book != s.company.Book {
		return result, apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", "plan belongs to another company or book")
	}
	already, err := periodCloseAlreadyApplied(ctx, s.store, plan)
	if err != nil {
		return result, err
	}
	if already {
		result.Status = "CLOSED"
		result.DryRun = false
		return result, nil
	}
	if dryRun {
		current, err := s.ledger().ClosePeriod(ctx, plan.Book, plan.Period, true)
		if err != nil {
			return result, err
		}
		if current.Digest != plan.LedgerDigest || current.EndDate != plan.EndDate {
			return result, apperr.New(apperr.Conflict, "CLOSE_PLAN_STALE", "ledger content changed after planning; generate and review a new close plan")
		}
		return result, nil
	}
	if _, err = s.ledger().ClosePeriodFromPlan(ctx, plan.Book, plan.Period, plan.EndDate, plan.LedgerDigest); err != nil {
		return result, err
	}
	result.Status = "CLOSED"
	return result, nil
}
func (s *Service) PlanYearClose(ctx context.Context, year int, retained string) (YearClosePlan, error) {
	if year < 1900 {
		return YearClosePlan{}, apperr.New(apperr.Invalid, "FISCAL_YEAR_INVALID", "year must be a four-digit fiscal year")
	}
	if retained == "" {
		settings, err := s.settings()
		if err != nil {
			return YearClosePlan{}, err
		}
		retained = settings.Defaults.RetainedEarnings
	}
	if retained == "" {
		return YearClosePlan{}, apperr.New(apperr.Validation, "RETAINED_EARNINGS_REQUIRED", "provide retained earnings or set defaults.retained-earnings")
	}
	prepared, err := s.ledger().PrepareFiscalYearClose(ctx, ledger.FiscalYearCloseInput{Book: s.company.Book, FiscalYear: year, RetainedEarnings: retained})
	if err != nil {
		return YearClosePlan{}, err
	}
	prepared.Input, err = stampYearCloseJournal(prepared.Input, s.company.Key, year)
	if err != nil {
		return YearClosePlan{}, err
	}
	journalDigest, err := DigestJSON(prepared.Input)
	if err != nil {
		return YearClosePlan{}, err
	}
	plan := YearClosePlan{Schema: currencyPlanSchema(yearClosePlanSchema, "books.year-close-plan/v2", s.company.Currency), Currency: planCurrency(s.company.Currency), Company: s.company.Key, Book: s.company.Book, FiscalYear: year, RetainedEarnings: strings.ToUpper(retained), NetIncomeCents: prepared.NetIncome, Journal: prepared.Input, JournalDigest: journalDigest, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	plan.Digest, err = DigestYearClosePlan(plan)
	return plan, err
}
func (s *Service) ApplyYearClose(ctx context.Context, plan YearClosePlan, dryRun bool) (YearCloseOutput, error) {
	result := YearCloseOutput{Plan: plan, Status: "VALIDATED", DryRun: dryRun}
	if plan.Schema != yearClosePlanSchema && plan.Schema != "books.year-close-plan/v2" {
		return result, apperr.New(apperr.Invalid, "YEAR_CLOSE_PLAN_INVALID", "year close plan schema is unsupported")
	}
	digest, err := DigestYearClosePlan(plan)
	if err != nil {
		return result, err
	}
	if digest != plan.Digest {
		return result, apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "year close plan changed after generation")
	}
	if !validPlanCurrency(plan.Schema, yearClosePlanSchema, "books.year-close-plan/v2", plan.Currency, s.company.Currency) {
		return result, apperr.New(apperr.Invalid, "PLAN_CURRENCY_MISMATCH", "plan currency does not match the company's immutable currency")
	}
	if plan.Company != s.company.Key || plan.Book != s.company.Book {
		return result, apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", "plan belongs to another company or book")
	}
	current, err := s.PlanYearClose(ctx, plan.FiscalYear, plan.RetainedEarnings)
	if err != nil {
		return result, err
	}
	expectedDigest, err := DigestJSON(plan.Journal)
	if err != nil {
		return result, err
	}
	if current.JournalDigest != plan.JournalDigest || expectedDigest != plan.JournalDigest || current.NetIncomeCents != plan.NetIncomeCents {
		return result, apperr.New(apperr.Conflict, "YEAR_CLOSE_PLAN_STALE", "fiscal-year activity changed; generate and review a new year-close plan")
	}
	if dryRun {
		return result, nil
	}
	posted, err := s.ledger().PostFiscalYearCloseFromPlan(ctx, ledger.FiscalYearCloseInput{Book: s.company.Book, FiscalYear: plan.FiscalYear, RetainedEarnings: plan.RetainedEarnings}, plan.Journal)
	if err != nil {
		return result, err
	}
	transaction := TransactionFromJournal(s.company.Key, *posted.Journal)
	result.Transaction = &transaction
	result.Status = "POSTED"
	return result, nil
}
func planCurrency(code string) string {
	if code == "USD" {
		return ""
	}
	return code
}
func currencyPlanSchema(legacy, current, code string) string {
	if code == "USD" {
		return legacy
	}
	return current
}
func validPlanCurrency(schema, legacy, current, planned, actual string) bool {
	return (schema == legacy && planned == "" && actual == "USD") || (schema == current && planned == actual && planned != "")
}
