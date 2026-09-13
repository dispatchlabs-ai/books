package operations

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/report"
)

// Access is supplied by a trusted adapter after authentication, never decoded
// from client input. The zero value denies access. This initial policy supports
// company reads only; it does not imply database or registry administration.
type Access struct {
	actor   string
	company string
	grants  []string
	local   bool
}

// CompanyAccess snapshots an authenticated principal's grants for one company.
func CompanyAccess(actor, company string, grants []string) Access {
	return Access{actor: actor, company: company, grants: slices.Clone(grants)}
}

// TrustedLocalAccess explicitly selects the existing local owner's authority.
// Network adapters must never use this constructor for client requests.
func TrustedLocalAccess(actor string) Access { return Access{actor: actor, local: true} }

// Descriptor describes an implemented typed backend binding. Types describe the
// domain contract; adapters still own lossless wire formatting. JSON Schema and
// complete operation coverage are separate rollout steps.
type Descriptor struct {
	ID      string
	Version int
	Scope   string
	Grant   string
	Effect  string
	Input   reflect.Type
	Output  reflect.Type
}

// TypedOperation couples the contract, policy and application invocation. Its
// fields are private so adapters cannot replace the required policy or handler.
type TypedOperation[I, O any] struct {
	descriptor Descriptor
	run        func(context.Context, *application.Service, I) (O, error)
}

func (o TypedOperation[I, O]) Descriptor() Descriptor { return o.descriptor }

func (o TypedOperation[I, O]) Execute(ctx context.Context, app *application.Service, access Access, input I) (O, error) {
	var zero O
	if o.run == nil || o.descriptor.Scope != "company" || o.descriptor.Grant != "read" {
		return zero, apperr.New(apperr.Unavailable, "OPERATION_UNAVAILABLE", "operation is not available")
	}
	if app == nil || strings.TrimSpace(access.actor) == "" || app.Company().Key == "" ||
		(!access.local && (access.company != app.Company().Key || !slices.Contains(access.grants, o.descriptor.Grant))) {
		return zero, apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company is not available to this principal")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return o.run(ctx, app.AsActor(access.actor), input)
}

func companyRead[I, O any](id string, run func(context.Context, *application.Service, I) (O, error)) TypedOperation[I, O] {
	return TypedOperation[I, O]{descriptor: Descriptor{ID: id, Version: 1, Scope: "company", Grant: "read", Effect: "read", Input: reflect.TypeFor[I](), Output: reflect.TypeFor[O]()}, run: run}
}

func GeneralLedger() TypedOperation[application.GeneralLedgerRequest, report.GeneralLedgerReport] {
	return companyRead("report_general_ledger", func(ctx context.Context, app *application.Service, in application.GeneralLedgerRequest) (report.GeneralLedgerReport, error) {
		return app.GeneralLedger(ctx, in)
	})
}
func TrialBalance() TypedOperation[application.AsOfReportRequest, report.TrialBalanceReport] {
	return companyRead("report_trial_balance", func(ctx context.Context, app *application.Service, in application.AsOfReportRequest) (report.TrialBalanceReport, error) {
		return app.TrialBalanceWithOptions(ctx, in)
	})
}
func BalanceSheet() TypedOperation[application.AsOfReportRequest, report.BalanceSheetReport] {
	return companyRead("report_balance_sheet", func(ctx context.Context, app *application.Service, in application.AsOfReportRequest) (report.BalanceSheetReport, error) {
		return app.BalanceSheetWithOptions(ctx, in)
	})
}
func ProfitLoss() TypedOperation[application.RangeReportRequest, report.ProfitLossReport] {
	return companyRead("report_profit_loss", func(ctx context.Context, app *application.Service, in application.RangeReportRequest) (report.ProfitLossReport, error) {
		return app.ProfitLossWithOptions(ctx, in)
	})
}

// TypedReports lists only implemented company report contracts, not all Books
// capabilities. Every ID must have bindings and explicit remaining catalog gaps.
func TypedReports() []Descriptor {
	return []Descriptor{GeneralLedger().Descriptor(), TrialBalance().Descriptor(), BalanceSheet().Descriptor(), ProfitLoss().Descriptor()}
}
