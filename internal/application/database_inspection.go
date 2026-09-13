package application

import (
	"context"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type AuditVerification struct {
	Valid      bool  `json:"valid"`
	EventCount int64 `json:"event_count"`
}

func (d *Database) Audit(ctx context.Context, limit int) ([]storesqlite.AuditEventRecord, error) {
	return d.store.ListAudit(ctx, limit)
}
func (d *Database) VerifyAudit(ctx context.Context) (AuditVerification, error) {
	tx, err := d.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return AuditVerification{}, err
	}
	defer func() { _ = tx.Rollback() }()
	count, err := storesqlite.VerifyAudit(ctx, tx)
	return AuditVerification{Valid: err == nil, EventCount: count}, err
}
func (d *Database) Status(ctx context.Context) (storesqlite.DatabaseInfo, error) {
	return d.store.DatabaseInfo(ctx)
}
func (d *Database) Doctor(ctx context.Context) (storesqlite.DoctorResult, error) {
	return d.store.Doctor(ctx)
}
