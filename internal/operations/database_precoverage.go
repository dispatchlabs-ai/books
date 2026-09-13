package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	"strings"
)

type PrecoverageRequest struct {
	Input  string `json:"input"`
	Commit bool   `json:"commit"`
	DryRun bool   `json:"dry_run"`
}
type PrecoverageResult struct {
	ledger.StatementAccountPrecoverageClosure
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run"`
}

func precoverageDatabaseOperations() []DatabaseOperation {
	return []DatabaseOperation{
		databaseOp("statement_account_lifecycle_close_before_coverage", "manage", func(c context.Context, d *application.Database, a string, r PrecoverageRequest) (PrecoverageResult, error) {
			service := d.Ledger(a)
			input, err := application.ReadPrecoverageClosureInputFS("/artifacts/"+r.Input, artifact.Sources(c), func(key string) (money.Currency, error) { return service.TargetCurrency(c, "statement", key) })
			if err != nil {
				return PrecoverageResult{}, err
			}
			var result ledger.StatementAccountPrecoverageClosure
			if !r.Commit || r.DryRun {
				result, err = service.ValidateStatementAccountPrecoverageClosure(c, input)
			} else {
				if err = artifact.Retain(c, []string{r.Input, strings.TrimPrefix(input.ClosureEvidence.SourcePath, "/artifacts/"), strings.TrimPrefix(input.ZeroEvidence.SourcePath, "/artifacts/")}); err != nil {
					return PrecoverageResult{}, err
				}
				result, err = service.CloseStatementAccountBeforeCoverage(c, input)
			}
			return PrecoverageResult{StatementAccountPrecoverageClosure: result, Committed: r.Commit && !r.DryRun, DryRun: r.DryRun}, err
		}),
	}
}
