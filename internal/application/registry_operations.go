package application

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"strings"
)

// Registry paths come only from operator configuration, never request bodies.
type Registry struct{ path string }

func NewRegistry(path string) *Registry { return &Registry{path: path} }
func (r *Registry) Path() string        { return r.path }

type RegisteredCompany struct {
	Default    bool   `json:"default"`
	Company    string `json:"company"`
	Name       string `json:"name"`
	Currency   string `json:"currency"`
	EntityCode string `json:"entity_code"`
	Database   string `json:"database"`
}

func (r *Registry) Companies() ([]RegisteredCompany, error) {
	cfg, err := booksconfig.Load(r.path)
	if err != nil {
		return nil, ConfigMutationError("read registry", err)
	}
	out := []RegisteredCompany{}
	for _, key := range cfg.CompanyKeys() {
		c, err := cfg.Resolve(r.path, key)
		if err != nil {
			return nil, err
		}
		out = append(out, RegisteredCompany{key == cfg.DefaultCompany, key, c.Company.Name, c.Company.Currency, c.Company.EntityCode, c.Database})
	}
	return out, nil
}

type ConfigurationValue struct {
	DefaultCompany *string          `json:"default_company,omitempty"`
	Output         *string          `json:"output,omitempty"`
	Defaults       *AccountDefaults `json:"defaults,omitempty"`
}

func (r *Registry) Get(key, company string) (ConfigurationValue, error) {
	out := ConfigurationValue{}
	cfg, err := booksconfig.Load(r.path)
	if err != nil {
		return out, ConfigMutationError("read configuration", err)
	}
	switch key {
	case "":
		out.DefaultCompany = &cfg.DefaultCompany
		out.Output = &cfg.Defaults.Output
	case "default-company":
		out.DefaultCompany = &cfg.DefaultCompany
	case "output":
		out.Output = &cfg.Defaults.Output
	case "defaults":
		if company == "" {
			company = cfg.DefaultCompany
		}
		c, err := cfg.Resolve(r.path, company)
		if err != nil {
			return out, err
		}
		d := c.Company.Defaults
		out.Defaults = &AccountDefaults{d.PaymentAccount, d.DepositAccount, d.RetainedEarnings}
	default:
		return out, apperr.New(apperr.Invalid, "CONFIG_KEY_UNSUPPORTED", "supported keys are default-company, output, and defaults")
	}
	return out, nil
}

type DefaultCompanyResult struct {
	DefaultCompany string `json:"default_company"`
	ConfigPath     string `json:"config_path"`
	DryRun         bool   `json:"dry_run"`
}

func (r *Registry) Default(ctx context.Context, actor, company string, dryRun bool) (DefaultCompanyResult, error) {
	key := strings.ToLower(strings.TrimSpace(company))
	out := DefaultCompanyResult{key, r.path, dryRun}
	if dryRun {
		cfg, err := booksconfig.Load(r.path)
		if err != nil {
			return out, ConfigMutationError("read configuration", err)
		}
		if _, ok := cfg.Companies[key]; !ok {
			return out, apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company is not registered")
		}
		return out, nil
	}
	_, _, err := SetConfiguration(ctx, r.path, "", "default-company", key, actor)
	return out, err
}
