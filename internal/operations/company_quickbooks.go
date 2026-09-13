package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
)

type QuickBooksPlanRequest struct {
	Files    []artifact.File `json:"files"`
	From     string          `json:"from"`
	Accounts string          `json:"accounts"`
	Start    string          `json:"start"`
	Through  string          `json:"through"`
	Mode     string          `json:"mode"`
}
type QuickBooksApplyRequest struct {
	Files      []artifact.File            `json:"files"`
	Plan       application.QuickBooksPlan `json:"plan"`
	SourceName string                     `json:"source_name"`
	Draft      bool                       `json:"draft"`
	DryRun     bool                       `json:"dry_run"`
}

func companyQuickBooksOperations() []CompanyOperation {
	plan := func(c context.Context, s *application.Service, _ Access, r QuickBooksPlanRequest) (application.QuickBooksPlan, error) {
		files, err := artifact.Bundle(c, r.Files)
		if err != nil {
			return application.QuickBooksPlan{}, err
		}
		return s.PlanQuickBooksFromFS(c, application.QuickBooksRequest{From: r.From, Accounts: r.Accounts, Start: r.Start, Through: r.Through, Mode: r.Mode}, files)
	}
	return []CompanyOperation{
		companyOp("import_quickbooks_inspect", "read", "read", plan),
		companyOp("import_quickbooks_plan", "read", "read", plan),
		companyOp("import_quickbooks_apply", "import+post+manage", "write", func(c context.Context, s *application.Service, _ Access, r QuickBooksApplyRequest) (application.QuickBooksResult, error) {
			files, err := artifact.Bundle(c, r.Files)
			if err != nil {
				return application.QuickBooksResult{}, err
			}
			if r.SourceName == "" {
				r.SourceName = "quickbooks-plan.json"
			}
			if !r.DryRun {
				ids := make([]string, 0, len(r.Files))
				for _, file := range r.Files {
					ids = append(ids, file.Artifact)
				}
				if err = artifact.Retain(c, ids); err != nil {
					return application.QuickBooksResult{}, err
				}
			}
			return s.ApplyQuickBooksFromFS(c, r.Plan, r.SourceName, r.Draft, r.DryRun, files)
		}),
	}
}
