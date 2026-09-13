package operations

import (
	"context"
	"encoding/base64"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

type BankUploadRequest struct {
	Key     string          `json:"key"`
	Name    string          `json:"name"`
	Base64  string          `json:"base64"`
	Options banking.Options `json:"options"`
}
type BankChoicesRequest struct {
	Job     string                   `json:"job"`
	Key     string                   `json:"key"`
	Choices ledger.BankImportChoices `json:"choices"`
}
type BankApplyRequest struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}
type SourceContent struct {
	Base64 string `json:"base64"`
}

func companyImportOperations() []CompanyOperation {
	return []CompanyOperation{
		companyOp("bank_import_upload", "import", "write", func(c context.Context, s *application.Service, a Access, r BankUploadRequest) (ledger.BankImportJob, error) {
			data, err := base64.StdEncoding.DecodeString(r.Base64)
			if err != nil {
				return ledger.BankImportJob{}, apperr.New(apperr.Invalid, "INPUT_ENCODING_INVALID", "source must be valid base64")
			}
			return s.UploadWithOptions(c, r.Key, r.Name, data, r.Options)
		}),
		companyOp("bank_import_process", "import", "write", func(c context.Context, s *application.Service, a Access, r IDRequest) (ledger.BankImportJob, error) {
			return s.Process(c, r.ID)
		}),
		companyOp("bank_import_show", "read", "read", func(c context.Context, s *application.Service, a Access, r IDRequest) (ledger.BankImportJob, error) {
			return s.Job(c, r.ID)
		}),
		companyOp("bank_import_plan", "read", "read", func(c context.Context, s *application.Service, a Access, r IDRequest) (ledger.BankImportPlan, error) {
			return s.Plan(c, r.ID)
		}),
		companyOp("bank_import_preview", "import", "write", func(c context.Context, s *application.Service, a Access, r BankChoicesRequest) (ledger.BankImportPlan, error) {
			return s.Preview(c, r.Job, r.Key, r.Choices)
		}),
		companyOp("bank_import_matches", "read", "read", func(c context.Context, s *application.Service, a Access, r BankChoicesRequest) (ledger.BankImportMatches, error) {
			return s.Matches(c, r.Job, r.Choices)
		}),
		companyOp("bank_import_apply", "import+post", "write", func(c context.Context, s *application.Service, a Access, r BankApplyRequest) (ledger.BankImportReceipt, error) {
			return s.Apply(c, r.ID, r.Digest)
		}),
		companyOp("import_source_read", "read", "read", func(c context.Context, s *application.Service, a Access, r IDRequest) (SourceContent, error) {
			data, err := s.Source(c, r.ID)
			return SourceContent{Base64: base64.StdEncoding.EncodeToString(data)}, err
		}),
	}
}
