package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

type BookRequest struct {
	Book string `json:"book"`
}
type EntityRequest struct {
	Entity string `json:"entity"`
}
type IDRequest struct {
	ID string `json:"id"`
}
type IDModeRequest struct {
	ID     string `json:"id"`
	DryRun bool   `json:"dry_run"`
	Reason string `json:"reason"`
}
type AccountConfigureRequest struct {
	Book           string `json:"book"`
	Account        string `json:"account"`
	From           string `json:"from"`
	To             string `json:"to"`
	PostingEnabled bool   `json:"posting_enabled"`
}
type PeriodRequest struct {
	Book   string `json:"book"`
	Period string `json:"period"`
	Reason string `json:"reason"`
	DryRun bool   `json:"dry_run"`
}
type YearRequest struct {
	Input  ledger.FiscalYearCloseInput `json:"input"`
	DryRun bool                        `json:"dry_run"`
}
type JournalEditRequest struct {
	ID      string                    `json:"id"`
	Journal ledger.CreateJournalInput `json:"journal"`
}
type JournalReverseRequest struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Period      string `json:"period"`
	Description string `json:"description"`
}
type JournalListRequest struct {
	Book   string `json:"book"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}
type SourceLinkRequest struct {
	Source  string `json:"source"`
	Journal string `json:"journal"`
	Role    string `json:"role"`
}
type StatementTransactionsRequest struct {
	Account     string `json:"account"`
	From        string `json:"from"`
	To          string `json:"to"`
	Unallocated bool   `json:"unallocated"`
}
type ReconciliationStartRequest struct {
	Account   string `json:"account"`
	From      string `json:"from"`
	To        string `json:"to"`
	Beginning int64  `json:"beginning"`
	Ending    int64  `json:"ending"`
}
type AllocationRequest struct {
	Reconciliation string `json:"reconciliation"`
	Transaction    string `json:"transaction"`
	JournalLine    string `json:"journal_line"`
	Amount         int64  `json:"amount"`
}

func ledgerDatabaseOperations() []DatabaseOperation {
	return []DatabaseOperation{
		databaseOp("account_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreateAccountInput) (ledger.Account, error) {
			return d.Ledger(a).CreateAccount(c, r)
		}),
		databaseOp("account_list", "read", func(c context.Context, d *application.Database, a string, r BookRequest) ([]ledger.Account, error) {
			return d.Ledger(a).ListAccounts(c, r.Book)
		}),
		databaseOp("account_configure", "manage", func(c context.Context, d *application.Database, a string, r AccountConfigureRequest) (EmptyRequest, error) {
			err := d.Ledger(a).ConfigureBookAccount(c, r.Book, r.Account, r.From, r.To, r.PostingEnabled)
			return EmptyRequest{}, err
		}),
		databaseOp("account_identity_add", "manage", func(c context.Context, d *application.Database, a string, r ledger.AddAccountIdentityInput) (ledger.AccountIdentity, error) {
			return d.Ledger(a).AddAccountIdentity(c, r)
		}),
		databaseOp("account_identity_list", "read", func(c context.Context, d *application.Database, a string, r ledger.AccountIdentityFilter) ([]ledger.AccountIdentity, error) {
			return d.Ledger(a).ListAccountIdentities(c, r)
		}),
		databaseOp("period_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreatePeriodInput) (ledger.Period, error) {
			return d.Ledger(a).CreatePeriod(c, r)
		}),
		databaseOp("period_list", "read", func(c context.Context, d *application.Database, a string, r BookRequest) ([]ledger.Period, error) {
			return d.Ledger(a).ListPeriods(c, r.Book)
		}),
		databaseOp("period_close", "manage", func(c context.Context, d *application.Database, a string, r PeriodRequest) (ledger.PeriodCloseResult, error) {
			return d.Ledger(a).ClosePeriod(c, r.Book, r.Period, r.DryRun)
		}),
		databaseOp("period_reopen", "manage", func(c context.Context, d *application.Database, a string, r PeriodRequest) (EmptyRequest, error) {
			err := d.Ledger(a).ReopenPeriod(c, r.Book, r.Period, r.Reason)
			return EmptyRequest{}, err
		}),
		databaseOp("period_year_close", "manage", func(c context.Context, d *application.Database, a string, r YearRequest) (ledger.FiscalYearCloseResult, error) {
			return d.Ledger(a).PostFiscalYearClose(c, r.Input, r.DryRun)
		}),
		databaseOp("journal_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreateJournalInput) (ledger.Journal, error) {
			return d.Ledger(a).CreateJournal(c, r)
		}),
		databaseOp("journal_edit", "manage", func(c context.Context, d *application.Database, a string, r JournalEditRequest) (ledger.Journal, error) {
			return d.Ledger(a).ReplaceDraft(c, r.ID, r.Journal)
		}),
		databaseOp("journal_show", "read", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.Journal, error) {
			return d.Ledger(a).GetJournal(c, r.ID)
		}),
		databaseOp("journal_validate", "read", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.JournalValidation, error) {
			return d.Ledger(a).ValidateJournal(c, r.ID)
		}),
		databaseOp("journal_post", "manage", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.Journal, error) {
			return d.Ledger(a).PostJournal(c, r.ID)
		}),
		databaseOp("journal_abandon", "manage", func(c context.Context, d *application.Database, a string, r IDRequest) (EmptyRequest, error) {
			err := d.Ledger(a).AbandonJournal(c, r.ID)
			return EmptyRequest{}, err
		}),
		databaseOp("journal_reverse", "manage", func(c context.Context, d *application.Database, a string, r JournalReverseRequest) (ledger.Journal, error) {
			return d.Ledger(a).ReverseJournal(c, r.ID, r.Date, r.Period, r.Description)
		}),
		databaseOp("journal_list", "read", func(c context.Context, d *application.Database, a string, r JournalListRequest) ([]ledger.Journal, error) {
			return d.Ledger(a).ListJournals(c, r.Book, r.From, r.To, r.Status)
		}),
		databaseOp("journal_import", "manage", func(c context.Context, d *application.Database, a string, r ledger.JournalImportInput) (ledger.JournalImportResult, error) {
			return d.Ledger(a).ImportJournals(c, r)
		}),
		databaseOp("journal_post_batch", "manage", func(c context.Context, d *application.Database, a string, r IDModeRequest) (ledger.ImportBatchPostResult, error) {
			return d.Ledger(a).PostImportBatch(c, r.ID, r.DryRun)
		}),
		databaseOp("import_batch_list", "read", func(c context.Context, d *application.Database, a string, r ledger.ImportBatchFilter) ([]ledger.ImportBatch, error) {
			return d.Ledger(a).ListImportBatches(c, r)
		}),
		databaseOp("import_batch_show", "read", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.ImportBatch, error) {
			return d.Ledger(a).GetImportBatch(c, r.ID)
		}),
		databaseOp("source_list", "read", func(c context.Context, d *application.Database, a string, r ledger.SourceRecordFilter) ([]ledger.SourceRecord, error) {
			return d.Ledger(a).ListSourceRecords(c, r)
		}),
		databaseOp("source_show", "read", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.SourceRecord, error) {
			return d.Ledger(a).GetSourceRecord(c, r.ID)
		}),
		databaseOp("source_links", "read", func(c context.Context, d *application.Database, a string, r IDRequest) ([]ledger.SourceJournalLink, error) {
			return d.Ledger(a).ListSourceJournalLinks(c, r.ID)
		}),
		databaseOp("source_link_journal", "manage", func(c context.Context, d *application.Database, a string, r SourceLinkRequest) (ledger.SourceJournalLink, error) {
			return d.Ledger(a).LinkSourceRecordJournal(c, r.Source, r.Journal, r.Role)
		}),
		databaseOp("statement_account_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreateStatementAccountInput) (ledger.StatementAccount, error) {
			return d.Ledger(a).CreateStatementAccount(c, r)
		}),
		databaseOp("statement_account_list", "read", func(c context.Context, d *application.Database, a string, r EntityRequest) ([]ledger.StatementAccount, error) {
			return d.Ledger(a).ListStatementAccounts(c, r.Entity)
		}),
		databaseOp("statement_account_archive", "manage", func(c context.Context, d *application.Database, a string, r ledger.ArchiveStatementAccountInput) (ledger.StatementAccount, error) {
			return d.Ledger(a).ArchiveStatementAccount(c, r)
		}),
		databaseOp("statement_account_identity_add", "manage", func(c context.Context, d *application.Database, a string, r ledger.AddStatementAccountIdentityInput) (ledger.StatementAccountIdentity, error) {
			return d.Ledger(a).AddStatementAccountIdentity(c, r)
		}),
		databaseOp("statement_account_identity_list", "read", func(c context.Context, d *application.Database, a string, r ledger.StatementAccountIdentityFilter) ([]ledger.StatementAccountIdentity, error) {
			return d.Ledger(a).ListStatementAccountIdentities(c, r)
		}),
		databaseOp("statement_account_lifecycle_list", "read", func(c context.Context, d *application.Database, a string, r ledger.PrecoverageClosureFilter) ([]ledger.StatementAccountPrecoverageClosure, error) {
			return d.Ledger(a).ListStatementAccountPrecoverageClosures(c, r)
		}),
		databaseOp("statement_import", "manage", func(c context.Context, d *application.Database, a string, r ledger.StatementImportInput) (ledger.ImportResult, error) {
			return d.Ledger(a).ImportStatementTransactions(c, r)
		}),
		databaseOp("transaction_list", "read", func(c context.Context, d *application.Database, a string, r StatementTransactionsRequest) ([]ledger.StatementTransactionSummary, error) {
			return d.Ledger(a).ListStatementTransactions(c, r.Account, r.From, r.To, r.Unallocated)
		}),
		databaseOp("reconcile_start", "manage", func(c context.Context, d *application.Database, a string, r ReconciliationStartRequest) (ledger.Reconciliation, error) {
			return d.Ledger(a).StartReconciliation(c, r.Account, r.From, r.To, r.Beginning, r.Ending)
		}),
		databaseOp("reconcile_allocate", "manage", func(c context.Context, d *application.Database, a string, r AllocationRequest) (string, error) {
			return d.Ledger(a).AllocateReconciliation(c, r.Reconciliation, r.Transaction, r.JournalLine, r.Amount)
		}),
		databaseOp("reconcile_unallocate", "manage", func(c context.Context, d *application.Database, a string, r IDRequest) (EmptyRequest, error) {
			err := d.Ledger(a).RemoveReconciliationAllocation(c, r.ID)
			return EmptyRequest{}, err
		}),
		databaseOp("reconcile_allocations", "read", func(c context.Context, d *application.Database, a string, r IDRequest) ([]ledger.ReconciliationAllocation, error) {
			return d.Ledger(a).ListReconciliationAllocations(c, r.ID)
		}),
		databaseOp("reconcile_list", "read", func(c context.Context, d *application.Database, a string, r ledger.ReconciliationFilter) ([]ledger.Reconciliation, error) {
			return d.Ledger(a).ListReconciliations(c, r)
		}),
		databaseOp("reconcile_status", "read", func(c context.Context, d *application.Database, a string, r IDRequest) (ledger.Reconciliation, error) {
			return d.Ledger(a).ReconciliationStatus(c, r.ID)
		}),
		databaseOp("reconcile_complete", "manage", func(c context.Context, d *application.Database, a string, r IDModeRequest) (ledger.Reconciliation, error) {
			return d.Ledger(a).CompleteReconciliation(c, r.ID, r.DryRun)
		}),
		databaseOp("reconcile_abandon", "manage", func(c context.Context, d *application.Database, a string, r IDModeRequest) (ledger.Reconciliation, error) {
			return d.Ledger(a).AbandonReconciliation(c, r.ID, r.Reason, r.DryRun)
		}),
		databaseOp("reconcile_reopen", "manage", func(c context.Context, d *application.Database, a string, r IDModeRequest) (EmptyRequest, error) {
			err := d.Ledger(a).ReopenReconciliation(c, r.ID, r.Reason)
			return EmptyRequest{}, err
		}),
	}
}
