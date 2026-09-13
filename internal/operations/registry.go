package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

type RegistryAccess struct {
	actor  string
	grants []string
}

func ScopedRegistryAccess(actor string, grants []string) RegistryAccess {
	return RegistryAccess{actor: actor, grants: slices.Clone(grants)}
}
func ValidRegistryGrants(grants []string) bool {
	if len(grants) == 0 {
		return true
	}
	seen := map[string]bool{}
	for _, g := range grants {
		if seen[g] || (g != "read" && g != "manage") {
			return false
		}
		seen[g] = true
	}
	return seen["read"]
}

// CompanyGrants supports an explicit operator opt-in for every registered
// company, including future registrations. Exact company entries take precedence.
func CompanyGrants(companies map[string][]string, key string) []string {
	if key == "" || key == "*" {
		return nil
	}
	if grants, ok := companies[key]; ok {
		return grants
	}
	return companies["*"]
}

type RegistryOperation interface {
	Descriptor() Descriptor
	NewInput() any
	Execute(context.Context, *application.Registry, RegistryAccess, any) (any, error)
}
type registryOperation[I, O any] struct {
	descriptor Descriptor
	run        func(context.Context, *application.Registry, string, I) (O, error)
}

func (o registryOperation[I, O]) Descriptor() Descriptor { return o.descriptor }
func (o registryOperation[I, O]) NewInput() any          { return new(I) }
func (o registryOperation[I, O]) Execute(c context.Context, r *application.Registry, a RegistryAccess, input any) (any, error) {
	if r == nil || !filepath.IsAbs(r.Path()) || strings.TrimSpace(a.actor) == "" || !slices.Contains(a.grants, "read") || !slices.Contains(a.grants, o.descriptor.Grant) {
		return nil, apperr.New(apperr.NotFound, "REGISTRY_NOT_AVAILABLE", "registry operation is not available to this principal")
	}
	v, ok := input.(*I)
	if !ok || v == nil {
		return nil, apperr.New(apperr.Invalid, "OPERATION_INPUT_INVALID", "operation input type does not match")
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return o.run(c, r, a.actor, *v)
}
func registryOp[I, O any](id, grant string, run func(context.Context, *application.Registry, string, I) (O, error)) RegistryOperation {
	effect := "read"
	if grant == "manage" {
		effect = "write"
	}
	return registryOperation[I, O]{Descriptor{ID: id, Version: 1, Scope: "registry", Grant: grant, Effect: effect, Input: reflect.TypeFor[I](), Output: reflect.TypeFor[O]()}, run}
}

type CompanyCreateRequest struct {
	Options    application.CompanyCreateOptions `json:"options"`
	Initialize bool                             `json:"initialize"`
	DryRun     bool                             `json:"dry_run"`
}
type CompanyDefaultRequest struct {
	Company string `json:"company"`
	DryRun  bool   `json:"dry_run"`
}
type ConfigGetRequest struct {
	Key     string `json:"key"`
	Company string `json:"company"`
}
type ConfigSetRequest struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Company string `json:"company"`
}
type ConfigSetResult struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	ConfigPath string `json:"config_path"`
}
type ConfigPathResult struct {
	ConfigPath string `json:"config_path"`
}

func RegistryOperations() []RegistryOperation {
	return []RegistryOperation{
		registryOp("company_add", "manage", func(c context.Context, r *application.Registry, a string, i CompanyCreateRequest) (application.CompanyCreateResult, error) {
			o := i.Options
			if o.Currency == "" {
				o.Currency = "USD"
			}
			if o.Basis == "" {
				o.Basis = "accrual"
			}
			if o.FiscalYearEnd == "" {
				o.FiscalYearEnd = "december"
			}
			if o.Periods == "" {
				o.Periods = "monthly"
			}
			if o.Chart == "" {
				o.Chart = "starter"
			}
			if i.Initialize {
				o.MakeDefault = true
			}
			return application.CreateRegisteredCompany(c, r.Path(), a, i.Initialize, i.DryRun, o)
		}),
		registryOp("company_default", "manage", func(c context.Context, r *application.Registry, a string, i CompanyDefaultRequest) (application.DefaultCompanyResult, error) {
			return r.Default(c, a, i.Company, i.DryRun)
		}),
		registryOp("company_list", "read", func(_ context.Context, r *application.Registry, _ string, _ EmptyRequest) ([]application.RegisteredCompany, error) {
			return r.Companies()
		}),
		registryOp("config_get", "read", func(_ context.Context, r *application.Registry, _ string, i ConfigGetRequest) (application.ConfigurationValue, error) {
			return r.Get(i.Key, i.Company)
		}),
		registryOp("config_path", "read", func(_ context.Context, r *application.Registry, _ string, _ EmptyRequest) (ConfigPathResult, error) {
			return ConfigPathResult{r.Path()}, nil
		}),
		registryOp("config_set", "manage", func(c context.Context, r *application.Registry, a string, i ConfigSetRequest) (ConfigSetResult, error) {
			_, value, err := application.SetConfiguration(c, r.Path(), i.Company, i.Key, i.Value, a)
			return ConfigSetResult{i.Key, value, r.Path()}, err
		}),
	}
}
func LookupRegistryOperation(id string) (RegistryOperation, bool) {
	for _, op := range RegistryOperations() {
		if op.Descriptor().ID == id {
			return op, true
		}
	}
	return nil, false
}
