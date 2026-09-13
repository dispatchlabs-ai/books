package application

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/report"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

// Database binds an administrator-selected opaque handle to a verified store.
// Clients select the handle, never a filesystem path. Authority covers all books.
type Database struct {
	identity string
	key      string
	store    *storesqlite.Store
}

func BindDatabase(ctx context.Context, key string, store *storesqlite.Store, expectedUUID string) (*Database, error) {
	if err := store.VerifySchema(ctx); err != nil {
		return nil, err
	}
	var uuid string
	if err := store.DB().QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton=1`).Scan(&uuid); err != nil {
		return nil, err
	}
	if expectedUUID != "" && expectedUUID != uuid {
		return nil, apperr.New(apperr.Conflict, "DATABASE_IDENTITY_MISMATCH", "configured database identity does not match")
	}
	if key == "" {
		return nil, apperr.New(apperr.Invalid, "DATABASE_REQUIRED", "database handle is required")
	}
	return &Database{key: key, store: store, identity: uuid}, nil
}
func OpenDatabase(ctx context.Context, key, path, uuid string) (*Database, error) {
	if uuid == "" {
		return nil, apperr.New(apperr.Invalid, "DATABASE_IDENTITY_REQUIRED", "configured database UUID is required")
	}
	store, err := storesqlite.Open(ctx, path, storesqlite.ReadWrite)
	if err != nil {
		return nil, err
	}
	db, err := BindDatabase(ctx, key, store, uuid)
	if err != nil {
		_ = store.Close()
	}
	return db, err
}
func (d *Database) Key() string { return d.key }
func (d *Database) Close() error {
	if d.store == nil {
		return nil
	}
	return d.store.Close()
}
func (d *Database) Ledger(actor string) *ledger.Service { return ledger.NewService(d.store, actor) }
func (d *Database) Reports() *report.Service            { return report.NewService(d.store) }

func (d *Database) Identity() string { return d.identity }
