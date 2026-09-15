package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/cashflow"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	"strings"
)

type NumberRequest struct {
	Number int64 `json:"number"`
}
type TransactionListRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}
type FiscalYearRequest struct {
	Year   int  `json:"fiscal_year"`
	DryRun bool `json:"dry_run"`
}
type CompanyPeriodRequest struct {
	Period string `json:"period"`
	Reason string `json:"reason"`
	DryRun bool   `json:"dry_run"`
}
type ReconcileApplyRequest struct {
	Plan   application.ReconciliationPlan `json:"plan"`
	DryRun bool                           `json:"dry_run"`
}
type ReconcileListRequest struct {
	Account string `json:"account"`
	Status  string `json:"status"`
	From    string `json:"from"`
	To      string `json:"to"`
}
type ReconcileReopenRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type CloseApplyRequest struct {
	Plan   application.PeriodClosePlan `json:"plan"`
	DryRun bool                        `json:"dry_run"`
}
type YearPlanRequest struct {
	Year     int    `json:"fiscal_year"`
	Retained string `json:"retained_earnings"`
}
type YearApplyRequest struct {
	Plan   application.YearClosePlan `json:"plan"`
	DryRun bool                      `json:"dry_run"`
}
type TransactionStatusRequest struct {
	Number int64 `json:"number"`
	DryRun bool  `json:"dry_run"`
}
type ReverseRequest struct {
	Number      int64  `json:"number"`
	Date        string `json:"date"`
	Description string `json:"description"`
	Draft       bool   `json:"draft"`
	DryRun      bool   `json:"dry_run"`
}

func CompanyOperations() []CompanyOperation {
	return append([]CompanyOperation{
		companyOp("cash_forecast", "read", "read", func(c context.Context, s *application.Service, a Access, r cashflow.Plan) (cashflow.Result, error) {
			return s.CashForecast(c, r)
		}),
		companyOp("account_list", "read", "read", func(c context.Context, s *application.Service, a Access, r EmptyRequest) ([]ledger.Account, error) {
			return s.Accounts(c)
		}),
		companyOp("statement_account_list", "read", "read", func(c context.Context, s *application.Service, a Access, r EmptyRequest) ([]ledger.StatementAccount, error) {
			return s.StatementAccounts(c)
		}),
		companyOp("dashboard", "read", "read", func(c context.Context, s *application.Service, a Access, r EmptyRequest) (application.Dashboard, error) {
			return s.Dashboard(c)
		}),
		companyOp("period_list", "read", "read", func(c context.Context, s *application.Service, a Access, r EmptyRequest) ([]ledger.Period, error) {
			return s.Periods(c)
		}),
		companyOp("account_defaults_get", "read", "read", func(c context.Context, s *application.Service, a Access, r EmptyRequest) (application.AccountDefaults, error) {
			return s.AccountDefaults()
		}),
		companyOp("report_general_ledger", "read", "read", func(c context.Context, s *application.Service, a Access, r application.GeneralLedgerRequest) (report.GeneralLedgerReport, error) {
			return s.GeneralLedger(c, r)
		}),
		companyOp("report_trial_balance", "read", "read", func(c context.Context, s *application.Service, a Access, r application.AsOfReportRequest) (report.TrialBalanceReport, error) {
			return s.TrialBalanceWithOptions(c, r)
		}),
		companyOp("report_balance_sheet", "read", "read", func(c context.Context, s *application.Service, a Access, r application.AsOfReportRequest) (report.BalanceSheetReport, error) {
			return s.BalanceSheetWithOptions(c, r)
		}),
		companyOp("report_profit_loss", "read", "read", func(c context.Context, s *application.Service, a Access, r application.RangeReportRequest) (report.ProfitLossReport, error) {
			return s.ProfitLossWithOptions(c, r)
		}),
		companyOp("journal_show", "read", "read", func(c context.Context, s *application.Service, a Access, r application.JournalReadRequest) (ledger.Journal, error) {
			return s.Journal(c, r)
		}),
		companyOp("journal_validate", "read", "read", func(c context.Context, s *application.Service, a Access, r application.JournalReadRequest) (ledger.JournalValidation, error) {
			return s.ValidateJournal(c, r)
		}),
		companyOp("tx_show", "read", "read", func(c context.Context, s *application.Service, a Access, r NumberRequest) (ledger.Journal, error) {
			return s.JournalByNumber(c, r.Number)
		}),
		companyOp("tx_list", "read", "read", func(c context.Context, s *application.Service, a Access, r TransactionListRequest) ([]application.Transaction, error) {
			return s.ListTransactions(c, r.From, r.To, r.Status)
		}),
		companyOp("account_add", "manage", "write", func(c context.Context, s *application.Service, a Access, r application.AccountRequest) (application.AccountResult, error) {
			if !a.local && (strings.TrimSpace(r.Code) == "" || strings.TrimSpace(r.ActiveFrom) == "") {
				return application.AccountResult{}, apperr.New(apperr.Invalid, "ACCOUNT_INPUT_REQUIRED", "remote account creation requires an explicit code and active_from date")
			}
			return s.AddAccount(c, r)
		}),
		companyOp("periods_add", "manage", "write", func(c context.Context, s *application.Service, a Access, r FiscalYearRequest) (application.FiscalYearResult, error) {
			return s.AddFiscalYear(c, r.Year, r.DryRun)
		}),
		companyOp("period_reopen", "manage", "write", func(c context.Context, s *application.Service, a Access, r CompanyPeriodRequest) (application.ReopenPeriodResult, error) {
			return s.ReopenPeriod(c, r.Period, r.Reason, r.DryRun)
		}),
		companyOp("reconcile_replan", "read", "read", func(c context.Context, s *application.Service, a Access, r application.ReconciliationRequest) (application.ReconciliationPlan, error) {
			if r.TargetID == "" {
				return application.ReconciliationPlan{}, apperr.New(apperr.Invalid, "RECONCILIATION_REQUIRED", "replan requires a reopened reconciliation ID")
			}
			return s.PlanReconciliation(c, r)
		}),
		companyOp("reconcile_plan", "read", "read", func(c context.Context, s *application.Service, a Access, r application.ReconciliationRequest) (application.ReconciliationPlan, error) {
			return s.PlanReconciliation(c, r)
		}),
		companyOp("reconcile_apply", "post", "write", func(c context.Context, s *application.Service, a Access, r ReconcileApplyRequest) (application.ReconciliationOutput, error) {
			return s.ApplyReconciliation(c, r.Plan, "manual-reconciliation-"+r.Plan.Digest+".json", r.DryRun)
		}),
		companyOp("reconcile_list", "read", "read", func(c context.Context, s *application.Service, a Access, r ReconcileListRequest) ([]ledger.Reconciliation, error) {
			return s.Reconciliations(c, r.Account, r.Status, r.From, r.To)
		}),
		companyOp("reconcile_status", "read", "read", func(c context.Context, s *application.Service, a Access, r IDRequest) (ledger.Reconciliation, error) {
			return s.Reconciliation(c, r.ID)
		}),
		companyOp("reconcile_reopen", "manage", "write", func(c context.Context, s *application.Service, a Access, r ReconcileReopenRequest) (ledger.Reconciliation, error) {
			return s.ReopenReconciliation(c, r.ID, r.Reason)
		}),
		companyOp("close_plan", "read", "read", func(c context.Context, s *application.Service, a Access, r CompanyPeriodRequest) (application.PeriodClosePlan, error) {
			return s.PlanPeriodClose(c, r.Period)
		}),
		companyOp("close_apply", "manage", "write", func(c context.Context, s *application.Service, a Access, r CloseApplyRequest) (application.PeriodCloseOutput, error) {
			return s.ApplyPeriodClose(c, r.Plan, r.DryRun)
		}),
		companyOp("year_close_plan", "read", "read", func(c context.Context, s *application.Service, a Access, r YearPlanRequest) (application.YearClosePlan, error) {
			return s.PlanYearClose(c, r.Year, r.Retained)
		}),
		companyOp("year_close_apply", "post+manage", "write", func(c context.Context, s *application.Service, a Access, r YearApplyRequest) (application.YearCloseOutput, error) {
			return s.ApplyYearClose(c, r.Plan, r.DryRun)
		}),
		companyOp("spend", "post", "write", func(c context.Context, s *application.Service, a Access, r application.TransactionRequest) (application.Transaction, error) {
			if err := remoteKey(a, r.Key); err != nil {
				return application.Transaction{}, err
			}
			return s.RecordTransaction(c, "spend", r)
		}),
		companyOp("receive", "post", "write", func(c context.Context, s *application.Service, a Access, r application.TransactionRequest) (application.Transaction, error) {
			if err := remoteKey(a, r.Key); err != nil {
				return application.Transaction{}, err
			}
			return s.RecordTransaction(c, "receive", r)
		}),
		companyOp("transfer", "post", "write", func(c context.Context, s *application.Service, a Access, r application.TransactionRequest) (application.Transaction, error) {
			if err := remoteKey(a, r.Key); err != nil {
				return application.Transaction{}, err
			}
			return s.RecordTransaction(c, "transfer", r)
		}),
		companyOp("tx_post", "post", "write", func(c context.Context, s *application.Service, a Access, r TransactionStatusRequest) (application.Transaction, error) {
			return s.ChangeTransactionStatus(c, r.Number, "post", r.DryRun, allowedJournalKinds(a)...)
		}),
		companyOp("tx_abandon", "post", "write", func(c context.Context, s *application.Service, a Access, r TransactionStatusRequest) (application.Transaction, error) {
			return s.ChangeTransactionStatus(c, r.Number, "abandon", r.DryRun, allowedJournalKinds(a)...)
		}),
		companyOp("reverse", "post", "write", func(c context.Context, s *application.Service, a Access, r ReverseRequest) (application.Transaction, error) {
			return s.ReverseTransaction(c, r.Number, r.Date, r.Description, r.Draft, false, r.DryRun, allowedJournalKinds(a)...)
		}),
		companyOp("undo", "post", "write", func(c context.Context, s *application.Service, a Access, r ReverseRequest) (application.Transaction, error) {
			return s.ReverseTransaction(c, r.Number, r.Date, r.Description, r.Draft, true, r.DryRun, allowedJournalKinds(a)...)
		}),
	}, append(append(extraCompanyOperations(), companyImportOperations()...), append(companyArtifactOperations(), companyQuickBooksOperations()...)...)...)
}
