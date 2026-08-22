package cli

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

const manualReconciliationPlanSchema = "books.reconciliation-plan/v3"

type manualReconciliationLine struct {
	JournalNumber int64  `json:"transaction_number"`
	LineNumber    int    `json:"line_number"`
	JournalLineID string `json:"journal_line_id"`
	Date          string `json:"date"`
	StatementDate string `json:"statement_date"`
	Description   string `json:"description"`
	AmountCents   int64  `json:"amount_cents"`
	ExternalID    string `json:"external_id"`
}

type manualReconciliationPrior struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	EndDate            string `json:"end_date"`
	EndingBalanceCents int64  `json:"ending_balance_cents"`
}

type manualReconciliationPlan struct {
	Schema                    string                     `json:"schema"`
	Company                   string                     `json:"company"`
	Book                      string                     `json:"book"`
	StatementAccount          string                     `json:"statement_account"`
	ControlAccount            string                     `json:"control_account"`
	AccountKind               string                     `json:"account_kind"`
	TargetReconciliationID    string                     `json:"target_reconciliation_id,omitempty"`
	TargetPriorBeginningCents int64                      `json:"target_prior_beginning_cents,omitempty"`
	TargetPriorEndingCents    int64                      `json:"target_prior_ending_cents,omitempty"`
	TargetReopenedAt          string                     `json:"target_reopened_at,omitempty"`
	StartDate                 string                     `json:"start_date"`
	EndDate                   string                     `json:"end_date"`
	BeginningBalanceCents     int64                      `json:"beginning_balance_cents"`
	EndingBalanceCents        int64                      `json:"ending_balance_cents"`
	LedgerBeginningCents      int64                      `json:"ledger_beginning_cents"`
	LedgerEndingCents         int64                      `json:"ledger_ending_cents"`
	OpeningOutstandingCents   int64                      `json:"opening_outstanding_cents"`
	EndingOutstandingCents    int64                      `json:"ending_outstanding_cents"`
	AdjustedBeginningCents    int64                      `json:"adjusted_beginning_cents"`
	AdjustedEndingCents       int64                      `json:"adjusted_ending_cents"`
	StatementTransactionCount int                        `json:"statement_transaction_count"`
	PriorReconciliation       *manualReconciliationPrior `json:"prior_reconciliation,omitempty"`
	ActivityCents             int64                      `json:"activity_cents"`
	Candidates                []manualReconciliationLine `json:"candidates"`
	Cleared                   []manualReconciliationLine `json:"cleared"`
	Outstanding               []manualReconciliationLine `json:"outstanding"`
	ControlDigest             string                     `json:"control_digest"`
	Ready                     bool                       `json:"ready"`
	Blockers                  []string                   `json:"blockers"`
	CreatedAt                 string                     `json:"created_at"`
	Digest                    string                     `json:"digest"`
}

type manualReconciliationPlanOutput struct {
	Plan     manualReconciliationPlan `json:"plan"`
	PlanPath string                   `json:"plan_path,omitempty"`
	Written  bool                     `json:"written"`
}

type manualReconciliationApplyOutput struct {
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

func newManualReconciliationPlanCommand(opts *options) *cobra.Command {
	var through, startText, beginningText, endingText, cleared, outputPath string
	command := &cobra.Command{
		Use:   "plan ACCOUNT",
		Short: "Create a deterministic manual bank-reconciliation plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManualReconciliationPlan(cmd, opts, args[0], "", through, startText, beginningText, endingText, cleared, outputPath)
		},
	}
	command.Flags().StringVar(&through, "through", "", "statement ending date (required)")
	command.Flags().StringVar(&startText, "start", "", "starting date (normally inferred from account/prior reconciliation)")
	command.Flags().StringVar(&beginningText, "beginning", "", "statement beginning balance (normally inferred)")
	command.Flags().StringVar(&endingText, "ending", "", "statement ending balance (required; debts may be entered as a positive amount owed)")
	command.Flags().StringVar(&cleared, "cleared", "all", "transaction numbers cleared by the statement, such as 4,7-10, none, or all")
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path (defaults inside the selected company's plans directory)")
	return command
}

func newManualReconciliationReplanCommand(opts *options) *cobra.Command {
	var endingText, cleared, outputPath string
	command := &cobra.Command{
		Use: "replan RECONCILIATION_ID", Short: "Revise and rebuild an explicitly reopened reconciliation", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManualReconciliationPlan(cmd, opts, "", args[0], "", "", "", endingText, cleared, outputPath)
		},
	}
	command.Flags().StringVar(&endingText, "ending", "", "revised statement ending balance (required)")
	command.Flags().StringVar(&cleared, "cleared", "all", "transaction numbers cleared by the statement, such as 4,7-10, none, or all")
	command.Flags().StringVarP(&outputPath, "out", "o", "", "plan path (defaults inside the selected company's plans directory)")
	return command
}

func runManualReconciliationPlan(cmd *cobra.Command, opts *options, accountSelector, targetID, through, startText, beginningText, endingText, cleared, outputPath string) error {
	if strings.TrimSpace(endingText) == "" || (strings.TrimSpace(targetID) == "" && strings.TrimSpace(through) == "") {
		return apperr.New(apperr.Invalid, "RECONCILIATION_INPUT_REQUIRED", "--ending is required, and a new plan also requires --through")
	}
	resolved, err := opts.resolveCompany()
	if err != nil {
		return err
	}
	store, err := openRead(cmd, opts)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service := ledger.NewService(store, opts.actor)
	var statementAccount ledger.StatementAccount
	var targetPriorBeginning int64
	var targetPriorEnding int64
	var targetReopenedAt string
	if strings.TrimSpace(targetID) != "" {
		var status string
		err := store.DB().QueryRowContext(cmd.Context(), `SELECT sa.code, r.status, r.start_date, r.end_date,
			r.beginning_balance_cents, r.ending_balance_cents, COALESCE(r.reopened_at, '')
			FROM reconciliations r JOIN statement_accounts sa ON sa.id = r.statement_account_id
			WHERE r.id = ?`, strings.TrimSpace(targetID)).Scan(
			&accountSelector, &status, &startText, &through, &targetPriorBeginning, &targetPriorEnding, &targetReopenedAt)
		if err == sql.ErrNoRows {
			return apperr.New(apperr.NotFound, "RECONCILIATION_NOT_FOUND", "the reconciliation to replan was not found")
		}
		if err != nil {
			return err
		}
		if status != "OPEN" || targetReopenedAt == "" {
			return apperr.New(apperr.Conflict, "RECONCILIATION_REPLAN_TARGET_INVALID", "replan requires an explicitly reopened reconciliation")
		}
		var nonManualEvidence int
		if err := store.DB().QueryRowContext(cmd.Context(), `SELECT COUNT(*)
			FROM reconciliations reconciliation_row
			JOIN statement_transactions transaction_row
			  ON transaction_row.statement_account_id = reconciliation_row.statement_account_id
			 AND transaction_row.posted_date BETWEEN reconciliation_row.start_date AND reconciliation_row.end_date
			JOIN source_identities identity_row ON identity_row.id = transaction_row.source_identity_id
			WHERE reconciliation_row.id = ? AND identity_row.source_system <> 'MANUAL_RECONCILIATION'`, strings.TrimSpace(targetID)).Scan(&nonManualEvidence); err != nil {
			return err
		}
		if nonManualEvidence != 0 {
			return apperr.New(apperr.Validation, "RECONCILIATION_REPLAN_SOURCE_UNSUPPORTED", "the short replan workflow only revises manual reconciliations; use the evidence-backed reconciliation commands for provider statements")
		}
	}
	statementAccount, err = resolveManualStatementAccount(cmd, service, accountSelector, resolved.Company.EntityCode)
	if err != nil {
		return err
	}
	endDate, err := parseHumanDate(through)
	if err != nil {
		return err
	}
	ending, err := parseStatementBalance(endingText, statementAccount.Kind)
	if err != nil {
		return apperr.Wrap(apperr.Invalid, "ENDING_BALANCE_INVALID", "parse ending balance", err)
	}
	startDate, beginning, prior, boundaryBlockers, err := manualReconciliationBoundary(cmd, store, statementAccount, startText, beginningText, strings.TrimSpace(targetID))
	if err != nil {
		return err
	}
	if endDate < startDate {
		return apperr.New(apperr.Invalid, "RECONCILIATION_DATES_INVALID", "--through precedes the reconciliation start date")
	}
	allLines, err := manualControlLines(cmd, store, statementAccount.Code, startDate, endDate, strings.TrimSpace(targetID))
	if err != nil {
		return err
	}
	for i := range allLines {
		allLines[i].StatementDate = allLines[i].Date
		if allLines[i].StatementDate < startDate {
			allLines[i].StatementDate = startDate
		}
	}
	selected, outstanding, err := selectManualControlLines(allLines, cleared)
	if err != nil {
		return err
	}
	plan := manualReconciliationPlan{
		Schema: manualReconciliationPlanSchema, Company: resolved.Key, Book: resolved.Company.BookCode,
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
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("cleared activity produces statement ending %s, not %s", money.Format(plan.BeginningBalanceCents+plan.ActivityCents), money.Format(plan.EndingBalanceCents)))
	}
	if err := store.DB().QueryRowContext(cmd.Context(), `SELECT COUNT(*) FROM statement_transactions st
		JOIN statement_accounts sa ON sa.id = st.statement_account_id
		WHERE sa.code = ? AND st.posted_date BETWEEN ? AND ?`, statementAccount.Code, startDate, endDate).Scan(&plan.StatementTransactionCount); err != nil {
		return err
	}
	if plan.StatementTransactionCount != 0 && plan.TargetReconciliationID == "" {
		plan.Blockers = append(plan.Blockers, "statement activity already exists in this range; explicitly reopen and replan the existing reconciliation")
	}
	plan.LedgerBeginningCents, err = controlBalance(cmd, store, statementAccount.Code, "<", startDate)
	if err != nil {
		return err
	}
	plan.LedgerEndingCents, err = controlBalance(cmd, store, statementAccount.Code, "<=", endDate)
	if err != nil {
		return err
	}
	if prior != nil {
		if err := store.DB().QueryRowContext(cmd.Context(), `SELECT COALESCE(SUM(outstanding_amount_cents), 0)
			FROM reconciliation_outstanding_items WHERE reconciliation_id = ?`, prior.ID).Scan(&plan.OpeningOutstandingCents); err != nil {
			return err
		}
	}
	plan.AdjustedBeginningCents = plan.BeginningBalanceCents + plan.OpeningOutstandingCents
	plan.AdjustedEndingCents = plan.EndingBalanceCents + plan.EndingOutstandingCents
	if plan.LedgerBeginningCents != plan.AdjustedBeginningCents {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("book beginning %s does not equal statement beginning plus opening outstanding items %s", money.Format(plan.LedgerBeginningCents), money.Format(plan.AdjustedBeginningCents)))
	}
	if plan.LedgerEndingCents != plan.AdjustedEndingCents {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("book ending %s does not equal statement ending plus remaining outstanding items %s", money.Format(plan.LedgerEndingCents), money.Format(plan.AdjustedEndingCents)))
	}
	plan.Ready = len(plan.Blockers) == 0
	plan.ControlDigest, err = manualControlDigest(allLines)
	if err != nil {
		return err
	}
	plan.Digest, err = manualPlanDigest(plan)
	if err != nil {
		return err
	}
	written := false
	if !opts.dryRun {
		if strings.TrimSpace(outputPath) == "" {
			prefix := "reconcile"
			if plan.TargetReconciliationID != "" {
				prefix = "rereconcile"
			}
			outputPath = filepath.Join(resolved.Plans, fmt.Sprintf("%s-%s-%s.json", prefix, strings.ToLower(statementAccount.GLAccountCode), endDate))
		}
		outputPath, err = filepath.Abs(filepath.Clean(outputPath))
		if err != nil {
			return apperr.Wrap(apperr.Invalid, "PLAN_PATH_INVALID", "resolve plan path", err)
		}
		if err := writeExclusiveJSON(outputPath, plan); err != nil {
			return err
		}
		written = true
	}
	output := manualReconciliationPlanOutput{Plan: plan, PlanPath: outputPath, Written: written}
	if !plan.Ready {
		message := strings.Join(plan.Blockers, "; ")
		if written {
			message += fmt.Sprintf("; blocked plan written to %s", output.PlanPath)
		}
		return apperr.New(apperr.Validation, "RECONCILIATION_PLAN_BLOCKED", message)
	}
	return writeManualReconciliationPlan(cmd, opts, output)
}

func newManualReconciliationApplyCommand(opts *options) *cobra.Command {
	var planPath string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a previously reviewed manual reconciliation plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(planPath) == "" {
				return apperr.New(apperr.Invalid, "PLAN_REQUIRED", "--plan is required")
			}
			var plan manualReconciliationPlan
			if err := readJSONInput(planPath, &plan); err != nil {
				return err
			}
			if plan.Schema != manualReconciliationPlanSchema || !plan.Ready {
				return apperr.New(apperr.Invalid, "RECONCILIATION_PLAN_INVALID", "plan schema is unsupported or the plan is blocked")
			}
			digest, err := manualPlanDigest(plan)
			if err != nil {
				return err
			}
			if digest != plan.Digest {
				return apperr.New(apperr.Integrity, "PLAN_DIGEST_MISMATCH", "reconciliation plan content changed after it was generated")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if resolved.Key != plan.Company || resolved.Company.BookCode != plan.Book {
				return apperr.New(apperr.Invalid, "PLAN_COMPANY_MISMATCH", fmt.Sprintf("plan belongs to company %s; select it with --company %s", plan.Company, plan.Company))
			}
			output := manualReconciliationApplyOutput{
				Company: plan.Company, StatementAccount: plan.StatementAccount, StartDate: plan.StartDate,
				EndDate: plan.EndDate, EndingCents: plan.EndingBalanceCents, TransactionCount: len(plan.Cleared),
				AllocationCount: len(plan.Cleared), Status: "VALIDATED", PlanDigest: plan.Digest, DryRun: opts.dryRun,
			}
			var store *storesqlite.Store
			if opts.dryRun {
				store, err = openRead(cmd, opts)
			} else {
				store, err = openWrite(cmd, opts)
			}
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service := ledger.NewService(store, opts.actor)
			input := ledger.ManualReconciliationInput{
				StatementAccount:                    plan.StatementAccount,
				TargetReconciliationID:              plan.TargetReconciliationID,
				SourceName:                          filepath.Base(planPath),
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
			convertLine := func(line manualReconciliationLine, evidence bool) ledger.ManualReconciliationLine {
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
			if opts.dryRun {
				if err := service.ValidateManualReconciliation(cmd.Context(), input); err != nil {
					return err
				}
				return writeManualReconciliationApply(cmd, opts, output)
			}
			reconciliation, err := service.ApplyManualReconciliation(cmd.Context(), input)
			if err != nil {
				return err
			}
			output.Status = reconciliation.Status
			output.AllocationCount = reconciliation.AllocationCount
			return writeManualReconciliationApply(cmd, opts, output)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "reviewed reconciliation plan JSON")
	return command
}

func resolveManualStatementAccount(cmd *cobra.Command, service *ledger.Service, selector, entity string) (ledger.StatementAccount, error) {
	values, err := service.ListStatementAccounts(cmd.Context(), entity)
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

func parseStatementBalance(value, accountKind string) (int64, error) {
	result, err := money.Parse(value)
	if err != nil {
		return 0, err
	}
	if (accountKind == "CREDIT_CARD" || accountKind == "LOAN") && result > 0 {
		result = -result
	}
	return result, nil
}

func manualReconciliationBoundary(cmd *cobra.Command, store *storesqlite.Store, account ledger.StatementAccount, startText, beginningText, excludedReconciliationID string) (string, int64, *manualReconciliationPrior, []string, error) {
	var priorID, priorStatus, priorEnd string
	var priorEnding int64
	priorUpperBound := ""
	if strings.TrimSpace(excludedReconciliationID) != "" {
		priorUpperBound = strings.TrimSpace(startText)
	}
	err := store.DB().QueryRowContext(cmd.Context(), `SELECT r.id, r.status, r.end_date, r.ending_balance_cents
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
		startDate, err = parseHumanDate(startDate)
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
		beginning, err = parseStatementBalance(beginningText, account.Kind)
		if err != nil {
			return "", 0, nil, nil, apperr.Wrap(apperr.Invalid, "BEGINNING_BALANCE_INVALID", "parse beginning balance", err)
		}
	} else if priorExists {
		beginning = priorEnding
	} else {
		beginning, err = controlBalance(cmd, store, account.Code, "<", startDate)
		if err != nil {
			return "", 0, nil, nil, err
		}
	}
	if priorExists && beginning != priorEnding {
		blockers = append(blockers, fmt.Sprintf("beginning balance must carry forward %s from the prior reconciliation", money.Format(priorEnding)))
	}
	var prior *manualReconciliationPrior
	if priorExists {
		prior = &manualReconciliationPrior{ID: priorID, Status: priorStatus, EndDate: priorEnd, EndingBalanceCents: priorEnding}
	}
	return startDate, beginning, prior, blockers, nil
}

func manualControlLines(cmd *cobra.Command, store *storesqlite.Store, statementAccount, startDate, endDate, excludedReconciliationID string) ([]manualReconciliationLine, error) {
	rows, err := store.DB().QueryContext(cmd.Context(), `WITH candidates AS (
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
	var result []manualReconciliationLine
	for rows.Next() {
		var line manualReconciliationLine
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

func controlBalance(cmd *cobra.Command, store *storesqlite.Store, statementAccount, operator, date string) (int64, error) {
	if operator != "<" && operator != "<=" {
		return 0, fmt.Errorf("unsupported balance operator %q", operator)
	}
	query := `SELECT COALESCE(SUM(jl.debit_cents - jl.credit_cents), 0)
		FROM statement_accounts sa
		JOIN journal_entries je ON je.book_id = sa.book_id AND je.status = 'POSTED'
		JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.account_id = sa.gl_account_id
		WHERE sa.code = ? AND je.posting_date ` + operator + ` ?`
	var result int64
	if err := store.DB().QueryRowContext(cmd.Context(), query, strings.ToUpper(statementAccount), date).Scan(&result); err != nil {
		return 0, err
	}
	return result, nil
}

func manualControlDigest(lines []manualReconciliationLine) (string, error) {
	data, err := json.Marshal(lines)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func manualPlanDigest(plan manualReconciliationPlan) (string, error) {
	plan.Digest = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeExclusiveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return apperr.Wrap(apperr.Unavailable, "PLAN_DIRECTORY_FAILED", "create plan directory", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return apperr.New(apperr.Conflict, "PLAN_EXISTS", fmt.Sprintf("plan already exists at %s; choose another --out path", path))
		}
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "create plan", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "write plan", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "sync plan", err)
	}
	if err := file.Close(); err != nil {
		return apperr.Wrap(apperr.Unavailable, "PLAN_WRITE_FAILED", "close plan", err)
	}
	complete = true
	return nil
}

func selectManualControlLines(lines []manualReconciliationLine, selector string) ([]manualReconciliationLine, []manualReconciliationLine, error) {
	selector = strings.TrimSpace(strings.ToLower(selector))
	if selector == "" || selector == "all" {
		return append([]manualReconciliationLine(nil), lines...), nil, nil
	}
	if selector == "none" {
		return nil, append([]manualReconciliationLine(nil), lines...), nil
	}
	numbers, err := parseNumberSet(selector)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[int64]bool)
	var selected, omitted []manualReconciliationLine
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

func parseNumberSet(value string) (map[int64]bool, error) {
	result := map[int64]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "--cleared contains an empty transaction number")
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", "--cleared must contain numbers or inclusive ranges")
		}
		first, err := strconv.ParseInt(bounds[0], 10, 64)
		if err != nil || first < 1 {
			return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", fmt.Sprintf("invalid transaction number %q", part))
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseInt(bounds[1], 10, 64)
			if err != nil || last < first || last-first > 100000 {
				return nil, apperr.New(apperr.Invalid, "CLEARED_INVALID", fmt.Sprintf("invalid transaction range %q", part))
			}
		}
		for number := first; number <= last; number++ {
			result[number] = true
		}
	}
	return result, nil
}

func writeManualReconciliationPlan(cmd *cobra.Command, opts *options, output manualReconciliationPlanOutput) error {
	plan := output.Plan
	return writeResult(cmd, opts.format, output,
		[]string{"ACCOUNT", "START", "END", "STATEMENT BEGINNING", "CLEARED ACTIVITY", "STATEMENT ENDING", "OUTSTANDING", "ADJUSTED ENDING", "CLEARED", "READY", "PLAN"},
		[][]string{{plan.ControlAccount, plan.StartDate, plan.EndDate, money.Format(plan.BeginningBalanceCents), money.Format(plan.ActivityCents), money.Format(plan.EndingBalanceCents), money.Format(plan.EndingOutstandingCents), money.Format(plan.AdjustedEndingCents), strconv.Itoa(len(plan.Cleared)), fmt.Sprint(plan.Ready), output.PlanPath}})
}

func writeManualReconciliationApply(cmd *cobra.Command, opts *options, output manualReconciliationApplyOutput) error {
	return writeResult(cmd, opts.format, output,
		[]string{"ACCOUNT", "START", "END", "ENDING", "TRANSACTIONS", "ALLOCATIONS", "STATUS", "DRY RUN"},
		[][]string{{output.StatementAccount, output.StartDate, output.EndDate, money.Format(output.EndingCents), strconv.Itoa(output.TransactionCount), strconv.Itoa(output.AllocationCount), output.Status, fmt.Sprint(output.DryRun)}})
}

func sortManualLines(lines []manualReconciliationLine) {
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
