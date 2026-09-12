package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func initializeMigrationTestDatabase(t *testing.T, path string) string {
	t.Helper()
	ctx := context.Background()
	store, err := Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	var databaseUUID string
	if err := store.DB().QueryRowContext(ctx,
		`SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return databaseUUID
}

func TestSchemaVerificationPreservesWhitespaceInsideSQLLiterals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "literal-drift.sqlite")
	store, err := Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	var viewSQL string
	if err := store.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE type = 'view' AND name = 'valid_statement_account_precoverage_closures'`).Scan(&viewSQL); err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(viewSQL,
		"'statement-account lifecycle close-before-coverage'",
		"'statement-account  lifecycle close-before-coverage'", 1)
	if drifted == viewSQL {
		t.Fatal("audit command literal was not found in lifecycle view")
	}
	if _, err := store.DB().ExecContext(ctx, "DROP VIEW valid_statement_account_precoverage_closures"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, drifted); err != nil {
		t.Fatal(err)
	}
	err = store.VerifySchema(ctx)
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "DATABASE_SCHEMA_DRIFT" {
		t.Fatalf("schema verification error = %v, want DATABASE_SCHEMA_DRIFT", err)
	}
}

func TestWritableMigrationOpenRejectsReplacedInspectedTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "target.sqlite")
	replacementPath := filepath.Join(directory, "replacement.sqlite")
	displacedPath := filepath.Join(directory, "displaced.sqlite")
	initializeMigrationTestDatabase(t, targetPath)
	replacementUUID := initializeMigrationTestDatabase(t, replacementPath)
	inspected, err := inspectMigrationTarget(ctx, targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(targetPath, displacedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, targetPath); err != nil {
		t.Fatal(err)
	}
	store, err := openInspectedMigrationTarget(ctx, inspected)
	if store != nil {
		_ = store.Close()
		t.Fatal("writable migration open accepted a replaced inspected path")
	}
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "DATABASE_TARGET_CHANGED" {
		t.Fatalf("writable migration open error = %v, want DATABASE_TARGET_CHANGED", err)
	}
	replacement, err := Open(ctx, targetPath, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replacement.Close() }()
	if err := replacement.VerifySchema(ctx); err != nil {
		t.Fatalf("replacement target schema changed: %v", err)
	}
	var actualUUID string
	if err := replacement.DB().QueryRowContext(ctx,
		`SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&actualUUID); err != nil {
		t.Fatal(err)
	}
	if actualUUID != replacementUUID {
		t.Fatalf("replacement database identity changed: got %q want %q", actualUUID, replacementUUID)
	}
}

// Build the historical version with its unchanged embedded SQL, rather than
// downgrading a current database or copying any operator data.
func TestBankImportSchemaUpgradeAndRollback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.sqlite")
	s, err := Open(ctx, path, Create)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	if err = applyMigrationBatchForDatabase(ctx, s.DB(), migrations, migrations[:1], verifyMigrationTargetState, ""); err != nil {
		t.Fatal(err)
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO database_metadata(singleton,database_uuid,created_at,base_currency) VALUES(1,?,?,'USD')`, id, UTCNow()); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err = AppendAudit(ctx, tx, AuditInput{Actor: "test", Command: "db init", AggregateType: "database", AggregateID: id, Payload: map[string]any{"base_currency": "USD"}}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	failedVerification := func(context.Context, *sql.Tx, int, []migration) error {
		return errors.New("injected verification failure")
	}
	if err = applyMigrationBatchForDatabase(ctx, s.DB(), migrations, migrations[1:], failedVerification, id); err == nil {
		t.Fatal("injected migration failure ignored")
	}
	var version, count int
	if err = s.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("version after rollback: %d %v", version, err)
	}
	if err = s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name='bank_import_jobs'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration leaked schema: %d %v", count, err)
	}
	if err = Migrate(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err = s.VerifySchema(ctx); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Doctor(ctx); err != nil || !d.OK {
		t.Fatalf("upgraded doctor: %+v %v", d, err)
	}
	var gotID string
	if err = s.DB().QueryRowContext(ctx, "SELECT database_uuid FROM database_metadata").Scan(&gotID); err != nil || gotID != id {
		t.Fatal("migration changed identity")
	}
}
