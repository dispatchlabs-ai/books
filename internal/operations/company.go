package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"reflect"
	"slices"
	"strings"
)

type CompanyOperation interface {
	Descriptor() Descriptor
	NewInput() any
	Invoke(context.Context, *application.Service, Access, any) (any, error)
}
type companyOperation[I, O any] struct {
	descriptor Descriptor
	run        func(context.Context, *application.Service, Access, I) (O, error)
}

func (o companyOperation[I, O]) Descriptor() Descriptor { return o.descriptor }
func (o companyOperation[I, O]) NewInput() any          { return new(I) }
func (o companyOperation[I, O]) Invoke(ctx context.Context, app *application.Service, a Access, input any) (any, error) {
	if err := authorizeCompany(app, a, strings.Split(o.descriptor.Grant, "+")); err != nil {
		return nil, err
	}
	r, ok := input.(*I)
	if !ok || r == nil {
		return nil, apperr.New(apperr.Invalid, "OPERATION_INPUT_INVALID", "operation input type does not match")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return o.run(ctx, app.AsActor(a.actor), a, *r)
}
func authorizeCompany(app *application.Service, a Access, grants []string) error {
	denied := func() error {
		return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company operation is not available to this principal")
	}
	if app == nil || app.Company().Key == "" || strings.TrimSpace(a.actor) == "" {
		return denied()
	}
	if a.local {
		return nil
	}
	if a.company != app.Company().Key || !slices.Contains(a.grants, "read") {
		return denied()
	}
	for _, grant := range grants {
		if !slices.Contains(a.grants, grant) {
			return denied()
		}
	}
	return nil
}
func companyOp[I, O any](id, grant, effect string, run func(context.Context, *application.Service, Access, I) (O, error)) CompanyOperation {
	return companyOperation[I, O]{Descriptor{ID: id, Version: 1, Scope: "company", Grant: grant, Effect: effect, Input: reflect.TypeFor[I](), Output: reflect.TypeFor[O]()}, run}
}
func allowedJournalKinds(a Access) []string {
	if a.local || slices.Contains(a.grants, "manage") {
		return nil
	}
	return []string{"STANDARD"}
}
func LookupCompanyOperation(id string) (CompanyOperation, bool) {
	for _, op := range CompanyOperations() {
		if op.Descriptor().ID == id {
			return op, true
		}
	}
	return nil, false
}

func remoteKey(a Access, key string) error {
	if a.local {
		return nil
	}
	if len(key) < 1 || len(key) > 128 {
		return apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "provide a key of 1 to 128 printable ASCII characters without spaces")
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "idempotency key must contain printable ASCII without spaces")
		}
	}
	return nil
}
