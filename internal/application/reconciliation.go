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
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"sort"
	"strings"
	"time"
)

const ReconciliationPlanSchema = "books.reconciliation-plan/v3"

type ReconciliationPlan struct {
	Currency                  string               `json:"currency,omitempty"`
	Schema                    string               `json:"schema"`
	Company                   string               `json:"company"`
	Book                      string               `json:"book"`
	StatementAccount          string               `json:"statement_account"`
	ControlAccount            string               `json:"control_account"`
	AccountKind               string               `json:"account_kind"`
	TargetReconciliationID    string               `json:"target_reconciliation_id,omitempty"`
	TargetPriorBeginningCents int64                `json:"target_prior_beginning_cents,omitempty"`
	TargetPriorEndingCents    int64                `json:"target_prior_ending_cents,omitempty"`
	TargetReopenedAt          string               `json:"target_reopened_at,omitempty"`
	StartDate                 string               `json:"start_date"`
	EndDate                   string               `json:"end_date"`
	BeginningBalanceCents     int64                `json:"beginning_balance_cents"`
	EndingBalanceCents        int64                `json:"ending_balance_cents"`
	LedgerBeginningCents      int64                `json:"ledger_beginning_cents"`
	LedgerEndingCents         int64                `json:"ledger_ending_cents"`
	OpeningOutstandingCents   int64                `json:"opening_outstanding_cents"`
	EndingOutstandingCents    int64                `json:"ending_outstanding_cents"`
	AdjustedBeginningCents    int64                `json:"adjusted_beginning_cents"`
	AdjustedEndingCents       int64                `json:"adjusted_ending_cents"`
	StatementTransactionCount int                  `json:"statement_transaction_count"`
	PriorReconciliation       *ReconciliationPrior `json:"prior_reconciliation,omitempty"`
	ActivityCents             int64                `json:"activity_cents"`
	Candidates                []ReconciliationLine `json:"candidates"`
	Cleared                   []ReconciliationLine `json:"cleared"`
	Outstanding               []ReconciliationLine `json:"outstanding"`
	ControlDigest             string               `json:"control_digest"`
	Ready                     bool                 `json:"ready"`
	Blockers                  []string             `json:"blockers"`
	CreatedAt                 string               `json:"created_at"`
	Digest                    string               `json:"digest"`
}

type ReconciliationLine struct {
	JournalNumber int64  `json:"transaction_number"`
	LineNumber    int    `json:"line_number"`
	JournalLineID string `json:"journal_line_id"`
	Date          string `json:"date"`
	StatementDate string `json:"statement_date"`
	Description   string `json:"description"`
	AmountCents   int64  `json:"amount_cents"`
	ExternalID    string `json:"external_id"`
}

type ReconciliationPrior struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	EndDate            string `json:"end_date"`
	EndingBalanceCents int64  `json:"ending_balance_cents"`
}

type ReconciliationOutput struct {
	Company          string `json:"company"`
	StatementAccount string `json:"statement_account"`
	StartDate        string `json:"start_date"`
	EndDate          string `json:"end_date"`
	EndingCents      int64  `json:"ending_cents"`
	TransactionCount int    `json:"transaction_count"`
	AllocationCount  int    `json:"allocation_count"`
	Status           string `json:"status"`
	PlanDigest       string `json:"plan_digest"`
	DryRun           bool   `json:"dry_run"`
}

type ReconciliationRequest struct {
	StatementAccount string  `json:"statement_account"`
	TargetID         string  `json:"target_reconciliation_id,omitempty"`
	Through          string  `json:"through"`
	Start            string  `json:"start,omitempty"`
	Beginning        string  `json:"beginning,omitempty"`
	Ending           string  `json:"ending"`
	ClearAll         bool    `json:"clear_all"`
	Cleared          []int64 `json:"cleared"`
}

func strictDate(value string) (string, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", apperr.New(apperr.Invalid, "DATE_INVALID", "date must use YYYY-MM-DD")
	}
	return date.Format("2006-01-02"), nil
}

func (s *Service) PlanReconciliation(ctx context.Context, r ReconciliationRequest) (ReconciliationPlan, error) {
	accountSelector, targetID, through, startText, beginningText, endingText := r.StatementAccount, r.TargetID, r.Through, r.Start, r.Beginning, r.Ending
	if strings.TrimSpace(endingText) == "" || (strings.TrimSpace(targetID) == "" && strings.TrimSpace(through) == "") {
		return ReconciliationPlan{}, apperr.New(apperr.Invalid, "RECONCILIATION_INPUT_REQUIRED", "--ending is required, and a new plan also requires --through")
	}
	resolved := s.resolved
	store := s.store
	unit, err := money.Lookup(s.company.Currency)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	var statementAccount ledger.StatementAccount
	var targetPriorBeginning int64
	var targetPriorEnding int64
	var targetReopenedAt string
	if strings.TrimSpace(targetID) != "" {
		var status string
		err := store.DB().QueryRowContext(ctx, `SELECT sa.code, r.status, r.start_date, r.end_date,
			r.beginning_balance_cents, r.ending_balance_cents, COALESCE(r.reopened_at, '')
			FROM reconciliations r JOIN statement_accounts sa ON sa.id = r.statement_account_id
			WHERE r.id = ? AND sa.book_id = (SELECT id FROM books WHERE code = ?)`, strings.TrimSpace(targetID), s.company.Book).Scan(
			&accountSelector, &status, &startText, &through, &targetPriorBeginning, &targetPriorEnding, &targetReopenedAt)
		if err == sql.ErrNoRows {
			return ReconciliationPlan{}, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "the reconciliation to replan was not found")
		}
		if err != nil {
			return ReconciliationPlan{}, err
		}
		if status != "OPEN" || targetReopenedAt == "" {
			return ReconciliationPlan{}, apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_TARGET_INVALID", "replan requires an explicitly reopened reconciliation")
		}
		var nonManualEvidence int
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*)
			FROM reconciliations reconciliation_row
			JOIN statement_transactions transaction_row
			  ON transaction_row.statement_account_id = reconciliation_row.statement_account_id
			 AND transaction_row.posted_date BETWEEN reconciliation_row.start_date AND reconciliation_row.end_date
			JOIN source_identities identity_row ON identity_row.id = transaction_row.source_identity_id
			WHERE reconciliation_row.id = ? AND identity_row.source_system <> 'MANUAL_RECONCILIATION'`, strings.TrimSpace(targetID)).Scan(&nonManualEvidence); err != nil {
			return ReconciliationPlan{}, err
		}
		if nonManualEvidence != 0 {
			return ReconciliationPlan{}, apperr.New(apperr.Validation, "RECONCILIATION_REPLAN_SOURCE_UNSUPPORTED", "the short replan workflow only revises manual reconciliations; use the evidence-backed reconciliation commands for provider statements")
		}
	}
	statementAccount, err = s.resolveStatementAccount(ctx, accountSelector)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	endDate, err := strictDate(through)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	ending, err := parseStatementBalance(endingText, statementAccount.Kind, unit)
	if err != nil {
		return ReconciliationPlan{}, apperr.Wrap(apperr.Invalid, "ENDING_BALANCE_INVALID", "parse ending balance", err)
	}
	startDate, beginning, prior, boundaryBlockers, err := manualReconciliationBoundary(ctx, store, statementAccount, startText, beginningText, strings.TrimSpace(targetID), unit)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	if endDate < startDate {
		return ReconciliationPlan{}, apperr.New(apperr.Invalid, "RECONCILIATION_DATES_INVALID", "--through precedes the reconciliation start date")
	}
	allLines, err := manualControlLines(ctx, store, statementAccount.Code, startDate, endDate, strings.TrimSpace(targetID))
	if err != nil {
		return ReconciliationPlan{}, err
	}
	for i := range allLines {
		allLines[i].StatementDate = allLines[i].Date
		if allLines[i].StatementDate < startDate {
			allLines[i].StatementDate = startDate
		}
	}
	selected, outstanding, err := selectManualControlLines(allLines, r.ClearAll, r.Cleared)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	plan := ReconciliationPlan{
		Schema: currencyPlanSchema(ReconciliationPlanSchema, "books.reconciliation-plan/v4", resolved.Company.Currency), Currency: planCurrency(resolved.Company.Currency), Company: resolved.Key, Book: resolved.Company.BookCode,
		StatementAccount: statementAccount.Code, ControlAccount: statementAccount.GLAccountCode,
		AccountKind: statementAccount.Kind, TargetReconciliationID: strings.TrimSpace(targetID),
		TargetPriorBeginningCents: targetPriorBeginning, TargetPriorEndingCents: targetPriorEnding, TargetReopenedAt: targetReopenedAt,
		StartDate: startDate, EndDate: endDate, BeginningBalanceCents: beginning, EndingBalanceCents: ending,
		PriorReconciliation: prior, Candidates: allLines, Cleared: selected, Outstanding: outstanding,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	plan.Blockers = append(plan.Blockers, boundaryBlockers...)
	for _, line := range selected {
		plan.ActivityCents += line.AmountCents
	}
	for _, line := range outstanding {
		plan.EndingOutstandingCents += line.AmountCents
	}
	if plan.BeginningBalanceCents+plan.ActivityCents != plan.EndingBalanceCents {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("cleared activity produces statement ending %s, not %s", unit.Format(plan.BeginningBalanceCents+plan.ActivityCents), unit.Format(plan.EndingBalanceCents)))
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM statement_transactions st
		JOIN statement_accounts sa ON sa.id = st.statement_account_id
		WHERE sa.code = ? AND st.posted_date BETWEEN ? AND ?`, statementAccount.Code, startDate, endDate).Scan(&plan.StatementTransactionCount); err != nil {
		return ReconciliationPlan{}, err
	}
	if plan.StatementTransactionCount != 0 && plan.TargetReconciliationID == "" {
		plan.Blockers = append(plan.Blockers, "statement activity already exists in this range; explicitly reopen and replan the existing reconciliation")
	}
	plan.LedgerBeginningCents, err = controlBalance(ctx, store, statementAccount.Code, "<", startDate)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	plan.LedgerEndingCents, err = controlBalance(ctx, store, statementAccount.Code, "<=", endDate)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	if prior != nil {
		if err := store.DB().QueryRowContext(ctx, `SELECT COALESCE(SUM(outstanding_amount_cents), 0)
			FROM reconciliation_outstanding_items WHERE reconciliation_id = ?`, prior.ID).Scan(&plan.OpeningOutstandingCents); err != nil {
			return ReconciliationPlan{}, err
		}
	}
	plan.AdjustedBeginningCents = plan.BeginningBalanceCents + plan.OpeningOutstandingCents
	plan.AdjustedEndingCents = plan.EndingBalanceCents + plan.EndingOutstandingCents
	if plan.LedgerBeginningCents != plan.AdjustedBeginningCents {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("book beginning %s does not equal statement beginning plus opening outstanding items %s", unit.Format(plan.LedgerBeginningCents), unit.Format(plan.AdjustedBeginningCents)))
	}
	if plan.LedgerEndingCents != plan.AdjustedEndingCents {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("book ending %s does not equal statement ending plus remaining outstanding items %s", unit.Format(plan.LedgerEndingCents), unit.Format(plan.AdjustedEndingCents)))
	}
	plan.Ready = len(plan.Blockers) == 0
	plan.ControlDigest, err = manualControlDigest(allLines)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	plan.Digest, err = DigestReconciliationPlan(plan)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyReconciliation(ctx context.Context, plan ReconciliationPlan, sourceName string, dryRun bool) (ReconciliationOutput, error) {
	if (plan.Schema != ReconciliationPlanSchema && plan.Schema != "books.reconciliation-plan/v4") || !plan.Ready {
		return ReconciliationOutput{}, apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "plan schema is unsupported or the plan is blocked")
	}
	digest, err := DigestReconciliationPlan(plan)
	if err != nil {
		return ReconciliationOutput{}, err
	}
	if digest != plan.Digest {
		return ReconciliationOutput{}, apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "reconciliation plan content changed after it was generated")
	}
	resolved := s.resolved
	if !validPlanCurrency(plan.Schema, ReconciliationPlanSchema, "books.reconciliation-plan/v4", plan.Currency, resolved.Company.Currency) {
		return ReconciliationOutput{}, apperr.New(apperr.Invalid, "PLAN_CURRENCY_MISMATCH", "plan currency does not match the company's immutable currency")
	}
	if resolved.Key != plan.Company || resolved.Company.BookCode != plan.Book {
		return ReconciliationOutput{}, apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to company %s; select it with --company %s", plan.Company, plan.Company))
	}
	output := ReconciliationOutput{
		Company: plan.Company, StatementAccount: plan.StatementAccount, StartDate: plan.StartDate,
		EndDate: plan.EndDate, EndingCents: plan.EndingBalanceCents, TransactionCount: len(plan.Cleared),
		AllocationCount: len(plan.Cleared), Status: "VALIDATED", PlanDigest: plan.Digest, DryRun: dryRun,
	}
	service := s.ledger()
	if _, err := s.resolveStatementAccount(ctx, plan.StatementAccount); err != nil {
		return ReconciliationOutput{}, err
	}
	if plan.TargetReconciliationID != "" {
		var found string
		if err := s.store.DB().QueryRowContext(ctx, `SELECT r.id FROM reconciliations r JOIN statement_accounts sa ON sa.id=r.statement_account_id JOIN books b ON b.id=sa.book_id WHERE r.id=? AND b.code=? AND sa.code=?`, plan.TargetReconciliationID, s.company.Book, plan.StatementAccount).Scan(&found); err != nil {
			return ReconciliationOutput{}, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "reconciliation was not found in this company")
		}
	}
	input := ledger.ManualReconciliationInput{
		StatementAccount:                    plan.StatementAccount,
		TargetReconciliationID:              plan.TargetReconciliationID,
		SourceName:                          sourceName,
		PlanDigest:                          plan.Digest,
		StartDate:                           plan.StartDate,
		EndDate:                             plan.EndDate,
		BeginningBalanceCents:               plan.BeginningBalanceCents,
		EndingBalanceCents:                  plan.EndingBalanceCents,
		ExpectedLedgerBeginningCents:        plan.LedgerBeginningCents,
		ExpectedLedgerEndingCents:           plan.LedgerEndingCents,
		ExpectedOpeningOutstandingCents:     plan.OpeningOutstandingCents,
		ExpectedEndingOutstandingCents:      plan.EndingOutstandingCents,
		ExpectedTargetBeginningBalanceCents: plan.TargetPriorBeginningCents,
		ExpectedTargetEndingBalanceCents:    plan.TargetPriorEndingCents,
		ExpectedTargetReopenedAt:            plan.TargetReopenedAt,
		ExpectedStatementTransactionCount:   plan.StatementTransactionCount,
	}
	if plan.PriorReconciliation != nil {
		input.PriorReconciliation = &ledger.ManualReconciliationPrior{
			ID: plan.PriorReconciliation.ID, Status: plan.PriorReconciliation.Status,
			EndDate: plan.PriorReconciliation.EndDate, EndingBalanceCents: plan.PriorReconciliation.EndingBalanceCents,
		}
	}
	convertLine := func(line ReconciliationLine, evidence bool) ledger.ManualReconciliationLine {
		var raw json.RawMessage
		if evidence {
			raw, _ = json.Marshal(map[string]any{"plan_digest": plan.Digest, "transaction_number": line.JournalNumber, "line_number": line.LineNumber, "ledger_date": line.Date, "statement_date": line.StatementDate, "provenance": "OPERATOR_ATTESTATION"})
		}
		return ledger.ManualReconciliationLine{
			JournalLineID: line.JournalLineID, ExternalID: line.ExternalID, LedgerDate: line.Date,
			StatementDate: line.StatementDate, Description: line.Description, AmountCents: line.AmountCents, RawJSON: raw,
		}
	}
	for _, line := range plan.Candidates {
		input.ExpectedLines = append(input.ExpectedLines, convertLine(line, false))
	}
	for _, line := range plan.Cleared {
		input.Lines = append(input.Lines, convertLine(line, true))
	}
	for _, line := range plan.Outstanding {
		input.Outstanding = append(input.Outstanding, convertLine(line, false))
	}
	if dryRun {
		if err := service.ValidateManualReconciliation(ctx, input); err != nil {
			return ReconciliationOutput{}, err
		}
		return output, nil
	}
	reconciliation, err := service.ApplyManualReconciliation(ctx, input)
	if err != nil {
		return ReconciliationOutput{}, err
	}
	output.Status = reconciliation.Status
	output.AllocationCount = reconciliation.AllocationCount
	return output, nil
}

func (s *Service) resolveStatementAccount(ctx context.Context, selector string) (ledger.StatementAccount, error) {
	values, err := s.StatementAccounts(ctx)
	if err != nil {
		return ledger.StatementAccount{}, err
	}
	normalized := normalizeAccountSelector(selector)
	var matches []ledger.StatementAccount
	for _, value := range values {
		if value.Status != "ACTIVE" {
			continue
		}
		if strings.EqualFold(value.Code, selector) || strings.EqualFold(value.GLAccountCode, selector) || strings.EqualFold(value.Name, selector) {
			return value, nil
		}
		if normalized != "" && strings.Contains(normalizeAccountSelector(value.Name), normalized) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return ledger.StatementAccount{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_AMBIGUOUS", "account name is ambiguous; use its GL account code")
	}
	return ledger.StatementAccount{}, apperr.New(apperr.NotFound, "STATEMENT_ACCOUNT_NOT_FOUND", fmt.Sprintf("no active statement account matches %q", selector))
}

func parseStatementBalance(value, accountKind string, currencies ...money.Currency) (int64, error) {
	currency := money.Currency{}
	if len(currencies) > 0 {
		currency = currencies[0]
	}
	result, err := currency.Parse(value)
	if err != nil {
		return 0, err
	}
	if (accountKind == "CREDIT_CARD" || accountKind == "LOAN") && result > 0 {
		result = -result
	}
	return result, nil
}

func manualReconciliationBoundary(ctx context.Context, store *storesqlite.Store, account ledger.StatementAccount, startText, beginningText, excludedReconciliationID string, unit money.Currency) (string, int64, *ReconciliationPrior, []string, error) {
	var priorID, priorStatus, priorEnd string
	var priorEnding int64
	priorUpperBound := ""
	if strings.TrimSpace(excludedReconciliationID) != "" {
		priorUpperBound = strings.TrimSpace(startText)
	}
	err := store.DB().QueryRowContext(ctx, `SELECT r.id, r.status, r.end_date, r.ending_balance_cents
		FROM reconciliations r JOIN statement_accounts sa ON sa.id = r.statement_account_id
		WHERE sa.code = ? AND r.status <> 'ABANDONED' AND r.id <> ?
		  AND (? = '' OR r.end_date < ?)
		ORDER BY r.end_date DESC LIMIT 1`, account.Code, excludedReconciliationID, priorUpperBound, priorUpperBound).Scan(&priorID, &priorStatus, &priorEnd, &priorEnding)
	priorExists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return "", 0, nil, nil, err
	}
	startDate := strings.TrimSpace(startText)
	if startDate != "" {
		startDate, err = strictDate(startDate)
		if err != nil {
			return "", 0, nil, nil, err
		}
	} else if priorExists {
		parsed, parseErr := time.Parse("2006-01-02", priorEnd)
		if parseErr != nil {
			return "", 0, nil, nil, parseErr
		}
		startDate = parsed.AddDate(0, 0, 1).Format("2006-01-02")
	} else {
		startDate = account.ReconciliationRequiredFrom
	}
	var blockers []string
	if priorExists {
		if priorStatus != "COMPLETED" {
			blockers = append(blockers, fmt.Sprintf("prior reconciliation through %s is %s", priorEnd, priorStatus))
		}
		expected, _ := time.Parse("2006-01-02", priorEnd)
		expectedStart := expected.AddDate(0, 0, 1).Format("2006-01-02")
		if startDate != expectedStart {
			blockers = append(blockers, fmt.Sprintf("start date must adjoin the prior reconciliation at %s", expectedStart))
		}
	} else if strings.TrimSpace(excludedReconciliationID) == "" && startDate != account.ReconciliationRequiredFrom {
		blockers = append(blockers, fmt.Sprintf("first reconciliation must start at the account coverage date %s", account.ReconciliationRequiredFrom))
	}
	var beginning int64
	if strings.TrimSpace(beginningText) != "" {
		beginning, err = parseStatementBalance(beginningText, account.Kind, unit)
		if err != nil {
			return "", 0, nil, nil, apperr.Wrap(apperr.Invalid, "BEGINNING_BALANCE_INVALID", "parse beginning balance", err)
		}
	} else if priorExists {
		beginning = priorEnding
	} else {
		beginning, err = controlBalance(ctx, store, account.Code, "<", startDate)
		if err != nil {
			return "", 0, nil, nil, err
		}
	}
	if priorExists && beginning != priorEnding {
		blockers = append(blockers, fmt.Sprintf("beginning balance must carry forward %s from the prior reconciliation", unit.Format(priorEnding)))
	}
	var prior *ReconciliationPrior
	if priorExists {
		prior = &ReconciliationPrior{ID: priorID, Status: priorStatus, EndDate: priorEnd, EndingBalanceCents: priorEnding}
	}
	return startDate, beginning, prior, blockers, nil
}

func manualControlLines(ctx context.Context, store *storesqlite.Store, statementAccount, startDate, endDate, excludedReconciliationID string) ([]ReconciliationLine, error) {
	rows, err := store.DB().QueryContext(ctx, `WITH candidates AS (
		SELECT je.entry_number, jl.line_number, jl.id, je.posting_date,
			COALESCE(NULLIF(jl.description, ''), je.description) AS description,
			(jl.debit_cents - jl.credit_cents) - COALESCE((
				SELECT SUM(allocation.allocated_amount_cents)
				FROM reconciliation_allocations allocation
				JOIN reconciliations allocated ON allocated.id = allocation.reconciliation_id
				WHERE allocation.journal_line_id = jl.id AND allocated.status <> 'ABANDONED'
				  AND allocated.id <> ? AND allocated.end_date <= ?
			), 0) AS remaining_cents
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.code = ?
		  AND je.posting_date BETWEEN MIN(sa.reconciliation_required_from, ?) AND ?
	)
	SELECT entry_number, line_number, id, posting_date, description, remaining_cents
	FROM candidates WHERE remaining_cents <> 0
		ORDER BY posting_date, entry_number, line_number`, excludedReconciliationID, endDate, strings.ToUpper(statementAccount), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []ReconciliationLine
	for rows.Next() {
		var line ReconciliationLine
		if err := rows.Scan(&line.JournalNumber, &line.LineNumber, &line.JournalLineID, &line.Date, &line.Description, &line.AmountCents); err != nil {
			return nil, err
		}
		line.ExternalID = fmt.Sprintf("reconcile:%s:%d:%d", strings.ToLower(statementAccount), line.JournalNumber, line.LineNumber)
		result = append(result, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortManualLines(result)
	return result, nil
}

func controlBalance(ctx context.Context, store *storesqlite.Store, statementAccount, operator, date string) (int64, error) {
	if operator != "<" && operator != "<=" {
		return 0, fmt.Errorf("unsupported balance operator %q", operator)
	}
	query := `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.code = ? AND je.posting_date ` + operator + ` ?`
	var result int64
	if err := store.DB().QueryRowContext(ctx, query, strings.ToUpper(statementAccount), date).Scan(&result); err != nil {
		return 0, err
	}
	return result, nil
}

func manualControlDigest(lines []ReconciliationLine) (string, error) {
	data, err := json.Marshal(lines)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func DigestReconciliationPlan(plan ReconciliationPlan) (string, error) {
	plan.Digest = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func sortManualLines(lines []ReconciliationLine) {
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Date != lines[j].Date {
			return lines[i].Date < lines[j].Date
		}
		if lines[i].JournalNumber != lines[j].JournalNumber {
			return lines[i].JournalNumber < lines[j].JournalNumber
		}
		return lines[i].LineNumber < lines[j].LineNumber
	})
}

func selectManualControlLines(lines []ReconciliationLine, clearAll bool, cleared []int64) ([]ReconciliationLine, []ReconciliationLine, error) {
	if clearAll && len(cleared) > 0 {
		return nil, nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "choose either all or explicit cleared transaction numbers")
	}
	if clearAll {
		return append([]ReconciliationLine(nil), lines...), nil, nil
	}
	if len(cleared) > 100000 {
		return nil, nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "too many cleared transaction numbers")
	}
	numbers := make(map[int64]bool, len(cleared))
	for _, number := range cleared {
		if number < 1 {
			return nil, nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "transaction numbers must be positive")
		}
		numbers[number] = true
	}

	seen := make(map[int64]bool)
	var selected, omitted []ReconciliationLine
	for _, line := range lines {
		if numbers[line.JournalNumber] {
			selected = append(selected, line)
			seen[line.JournalNumber] = true
		} else {
			omitted = append(omitted, line)
		}
	}
	for number := range numbers {
		if !seen[number] {
			return nil, nil, apperr.New(apperr.NotFound, "CLEARED_TRANSACTION_NOT_FOUND", fmt.Sprintf("transaction %d has no eligible outstanding control-account line through this statement date", number))
		}
	}
	return selected, omitted, nil
}

func (s *Service) Reconciliations(ctx context.Context, account, status, from, to string) ([]ledger.Reconciliation, error) {
	if account != "" {
		selected, err := s.resolveStatementAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		account = selected.Code
	}
	return s.ledger().ListReconciliations(ctx, ledger.ReconciliationFilter{Book: s.company.Book, StatementAccount: account, Status: status, FromDate: from, ToDate: to})
}
func (s *Service) Reconciliation(ctx context.Context, id string) (ledger.Reconciliation, error) {
	var found string
	err := s.store.DB().QueryRowContext(ctx, `SELECT r.id FROM reconciliations r JOIN statement_accounts sa ON sa.id=r.statement_account_id JOIN books b ON b.id=sa.book_id WHERE r.id=? AND b.code=?`, id, s.company.Book).Scan(&found)
	if err == sql.ErrNoRows {
		return ledger.Reconciliation{}, apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "reconciliation was not found in this company")
	}
	if err != nil {
		return ledger.Reconciliation{}, err
	}
	return s.ledger().ReconciliationStatus(ctx, id)
}
func (s *Service) ReopenReconciliation(ctx context.Context, id, reason string) (ledger.Reconciliation, error) {
	if err := s.ledger().ReopenBookReconciliation(ctx, s.company.Book, id, reason); err != nil {
		return ledger.Reconciliation{}, err
	}
	return s.Reconciliation(ctx, id)
}
