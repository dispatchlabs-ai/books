package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type AuditListRequest struct {
	Limit int `json:"limit"`
}

func inspectionDatabaseOperations() []DatabaseOperation {
	return []DatabaseOperation{
		databaseOp("audit_list", "read", func(c context.Context, d *application.Database, _ string, r AuditListRequest) ([]storesqlite.AuditEventRecord, error) {
			if r.Limit == 0 {
				r.Limit = 100
			}
			return d.Audit(c, r.Limit)
		}),
		databaseOp("audit_verify", "read", func(c context.Context, d *application.Database, _ string, _ EmptyRequest) (application.AuditVerification, error) {
			return d.VerifyAudit(c)
		}),
		databaseOp("db_status", "read", func(c context.Context, d *application.Database, _ string, _ EmptyRequest) (storesqlite.DatabaseInfo, error) {
			return d.Status(c)
		}),
		databaseOp("db_doctor", "read", func(c context.Context, d *application.Database, _ string, _ EmptyRequest) (storesqlite.DoctorResult, error) {
			return d.Doctor(c)
		}),
	}
}
