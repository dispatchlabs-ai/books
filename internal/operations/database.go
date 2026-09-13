package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	"reflect"
	"slices"
	"strings"
)

type DatabaseAccess struct {
	actor, key string
	grants     []string
	local      bool
}

func ScopedDatabaseAccess(actor, key string, grants []string) DatabaseAccess {
	return DatabaseAccess{actor: actor, key: key, grants: slices.Clone(grants)}
}
func LocalDatabaseAccess(actor string) DatabaseAccess {
	return DatabaseAccess{actor: actor, local: true}
}

// DatabaseOperation supplies typed allocation and invocation to all adapters.
// Implementations are registered by the backend; adapters cannot replace policy.
type DatabaseOperation interface {
	Descriptor() Descriptor
	NewInput() any
	Execute(context.Context, *application.Database, DatabaseAccess, any) (any, error)
}
type databaseOperation[I, O any] struct {
	descriptor Descriptor
	run        func(context.Context, *application.Database, string, I) (O, error)
}

func (o databaseOperation[I, O]) Descriptor() Descriptor { return o.descriptor }
func (o databaseOperation[I, O]) NewInput() any          { return new(I) }
func (o databaseOperation[I, O]) Execute(ctx context.Context, db *application.Database, a DatabaseAccess, input any) (any, error) {
	if db == nil || strings.TrimSpace(a.actor) == "" || (!a.local && (a.key != db.Key() || !slices.Contains(a.grants, "read") || !slices.Contains(a.grants, o.descriptor.Grant))) {
		return nil, apperr.New(apperr.NotFound, "DATABASE_NOT_FOUND", "database operation is not available to this principal")
	}
	in, ok := input.(*I)
	if !ok || in == nil {
		return nil, apperr.New(apperr.Invalid, "OPERATION_INPUT_INVALID", "operation input type does not match")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = artifact.Bind(ctx, a.actor, "database:"+db.Identity())
	return o.run(ctx, db, a.actor, *in)
}
func databaseOp[I, O any](id, grant string, run func(context.Context, *application.Database, string, I) (O, error)) DatabaseOperation {
	effect := "read"
	if grant != "read" {
		effect = "write"
	}
	return databaseOpWithEffect(id, grant, effect, run)
}
func databaseOpWithEffect[I, O any](id, grant, effect string, run func(context.Context, *application.Database, string, I) (O, error)) DatabaseOperation {
	return databaseOperation[I, O]{Descriptor{ID: id, Version: 1, Scope: "database", Grant: grant, Effect: effect, Input: reflect.TypeFor[I](), Output: reflect.TypeFor[O]()}, run}
}

type EmptyRequest struct{}
type OwnershipRequest struct {
	Parent string `json:"parent"`
	Child  string `json:"child"`
	From   string `json:"from"`
	To     string `json:"to"`
}
type DatabaseReportRequest struct {
	Entity      string `json:"entity"`
	Group       string `json:"group"`
	From        string `json:"from"`
	To          string `json:"to"`
	AsOf        string `json:"as_of"`
	Account     string `json:"account"`
	IncludeZero bool   `json:"include_zero"`
}

func (r DatabaseReportRequest) scope() report.Scope {
	return report.Scope{EntityCode: r.Entity, GroupCode: r.Group}
}

func DatabaseOperations() []DatabaseOperation {
	return append([]DatabaseOperation{
		databaseOp("entity_list", "read", func(c context.Context, d *application.Database, a string, _ EmptyRequest) ([]ledger.Entity, error) {
			return d.Ledger(a).ListEntities(c)
		}),
		databaseOp("entity_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreateEntityInput) (ledger.Entity, error) {
			return d.Ledger(a).CreateEntity(c, r)
		}),
		databaseOp("book_list", "read", func(c context.Context, d *application.Database, a string, _ EmptyRequest) ([]ledger.Book, error) {
			return d.Ledger(a).ListBooks(c)
		}),
		databaseOp("group_list", "read", func(c context.Context, d *application.Database, a string, _ EmptyRequest) ([]ledger.ConsolidationGroup, error) {
			return d.Ledger(a).ListGroups(c)
		}),
		databaseOp("group_create", "manage", func(c context.Context, d *application.Database, a string, r ledger.CreateGroupInput) (ledger.Group, error) {
			return d.Ledger(a).CreateGroup(c, r)
		}),
		databaseOp("ownership_list", "read", func(c context.Context, d *application.Database, a string, _ EmptyRequest) ([]ledger.OwnershipInterest, error) {
			return d.Ledger(a).ListOwnership(c)
		}),
		databaseOp("ownership_set", "manage", func(c context.Context, d *application.Database, a string, r OwnershipRequest) (string, error) {
			return d.Ledger(a).AddOwnership(c, r.Parent, r.Child, r.From, r.To)
		}),
		databaseOp("report_general_ledger", "read", func(c context.Context, d *application.Database, _ string, r DatabaseReportRequest) (report.GeneralLedgerReport, error) {
			return d.Reports().GeneralLedger(c, report.GeneralLedgerInput{Scope: r.scope(), FromDate: r.From, ToDate: r.To, AccountCode: r.Account, IncludeZero: r.IncludeZero})
		}),
		databaseOp("report_trial_balance", "read", func(c context.Context, d *application.Database, _ string, r DatabaseReportRequest) (report.TrialBalanceReport, error) {
			return d.Reports().TrialBalance(c, report.TrialBalanceInput{Scope: r.scope(), AsOfDate: r.AsOf, IncludeZero: r.IncludeZero})
		}),
		databaseOp("report_balance_sheet", "read", func(c context.Context, d *application.Database, _ string, r DatabaseReportRequest) (report.BalanceSheetReport, error) {
			return d.Reports().BalanceSheet(c, report.BalanceSheetInput{Scope: r.scope(), AsOfDate: r.AsOf, IncludeZero: r.IncludeZero})
		}),
		databaseOp("report_profit_loss", "read", func(c context.Context, d *application.Database, _ string, r DatabaseReportRequest) (report.ProfitLossReport, error) {
			return d.Reports().ProfitLoss(c, report.ProfitLossInput{Scope: r.scope(), FromDate: r.From, ToDate: r.To, IncludeZero: r.IncludeZero})
		}),
	}, append(ledgerDatabaseOperations(), append(inspectionDatabaseOperations(), append(databaseArtifactOperations(), precoverageDatabaseOperations()...)...)...)...)
}
func LookupDatabaseOperation(id string) (DatabaseOperation, bool) {
	for _, o := range DatabaseOperations() {
		if o.Descriptor().ID == id {
			return o, true
		}
	}
	return nil, false
}
