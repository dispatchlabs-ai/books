package sqlite

import (
	"context"
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
