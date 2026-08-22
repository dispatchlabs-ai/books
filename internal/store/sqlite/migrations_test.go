package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type migrationFileSnapshot struct {
	contents       []byte
	mode           os.FileMode
	size           int64
	modification   time.Time
	directoryNames []string
}

type sqliteSchemaEntry struct {
	typeName  string
	name      string
	tableName string
	sql       string
}

type unrelatedDatabaseState struct {
	applicationID int
	userVersion   int
	schema        []sqliteSchemaEntry
	marker        string
}

func sqliteDSN(path string, query url.Values) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func createUnrelatedDatabase(t *testing.T, path string, applicationID int) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteDSN(path, url.Values{"mode": {"rwc"}}))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA application_id = " + strconv.Itoa(applicationID),
		"PRAGMA user_version = 1",
		"CREATE TABLE customer_data (id INTEGER PRIMARY KEY, marker TEXT NOT NULL)",
		"INSERT INTO customer_data(marker) VALUES ('preserve-me')",
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatalf("prepare unrelated database: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func captureMigrationFile(t *testing.T, path string) migrationFileSnapshot {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return migrationFileSnapshot{
		contents: contents, mode: info.Mode(), size: info.Size(), modification: info.ModTime(), directoryNames: names,
	}
}

func readUnrelatedDatabaseState(t *testing.T, path string) unrelatedDatabaseState {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteDSN(path, url.Values{"mode": {"ro"}, "_query_only": {"1"}}))
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(db)
	var state unrelatedDatabaseState
	if err := db.QueryRow("PRAGMA application_id").Scan(&state.applicationID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&state.userVersion); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_schema ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var entry sqliteSchemaEntry
		if err := rows.Scan(&entry.typeName, &entry.name, &entry.tableName, &entry.sql); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		state.schema = append(state.schema, entry)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT marker FROM customer_data WHERE id = 1").Scan(&state.marker); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestMigrateRefusesNonBooksDatabaseWithoutModification(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		applicationID   int
		expectedErrCode string
	}{
		{name: "unrelated application id", applicationID: 246802468, expectedErrCode: "DATABASE_WRONG_APPLICATION"},
		{name: "forged application id without books metadata", applicationID: storesqlite.ApplicationID, expectedErrCode: "DATABASE_MIGRATION_METADATA_INVALID"},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "unrelated.sqlite")
			createUnrelatedDatabase(t, path, testCase.applicationID)
			beforeState := readUnrelatedDatabaseState(t, path)
			beforeFile := captureMigrationFile(t, path)

			err := storesqlite.Migrate(context.Background(), path)
			if err == nil {
				t.Fatal("migration of unrelated database unexpectedly succeeded")
			}
			appError, ok := apperr.As(err)
			if !ok || appError.Code != testCase.expectedErrCode {
				t.Fatalf("migration error = %v, want code %s", err, testCase.expectedErrCode)
			}

			afterFile := captureMigrationFile(t, path)
			if !bytes.Equal(afterFile.contents, beforeFile.contents) {
				t.Fatal("unrelated database bytes changed")
			}
			if afterFile.mode != beforeFile.mode || afterFile.size != beforeFile.size || !afterFile.modification.Equal(beforeFile.modification) {
				t.Fatalf("unrelated database file state changed: before=(%s,%d,%s) after=(%s,%d,%s)",
					beforeFile.mode, beforeFile.size, beforeFile.modification,
					afterFile.mode, afterFile.size, afterFile.modification)
			}
			if !reflect.DeepEqual(afterFile.directoryNames, beforeFile.directoryNames) {
				t.Fatalf("migration left a sidecar file: before=%v after=%v", beforeFile.directoryNames, afterFile.directoryNames)
			}
			afterState := readUnrelatedDatabaseState(t, path)
			if !reflect.DeepEqual(afterState, beforeState) {
				t.Fatalf("unrelated database state changed: before=%+v after=%+v", beforeState, afterState)
			}
		})
	}
}

func TestMigrateAcceptsInitializedBooksDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := storesqlite.Migrate(ctx, path); err != nil {
		t.Fatalf("migrate initialized books database: %v", err)
	}
	store, err = storesqlite.Open(ctx, path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	if err := store.VerifySchema(ctx); err != nil {
		t.Fatalf("verify migrated books database: %v", err)
	}
}

func TestVerifySchemaRejectsIncompleteMigrationLedger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	if _, err := store.DB().ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	err = store.VerifySchema(ctx)
	if err == nil {
		t.Fatal("schema verification unexpectedly accepted a missing migration row")
	}
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "DATABASE_MIGRATION_METADATA_INVALID" {
		t.Fatalf("verify schema error = %v, want DATABASE_MIGRATION_METADATA_INVALID", err)
	}
}

func TestVerifySchemaAndDoctorRejectMissingCriticalTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	if _, err := store.DB().ExecContext(ctx, "DROP TRIGGER journal_entries_guard_posted_update"); err != nil {
		t.Fatal(err)
	}
	for name, verify := range map[string]func() error{
		"verify schema": func() error { return store.VerifySchema(ctx) },
		"doctor": func() error {
			_, err := store.Doctor(ctx)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := verify()
			if err == nil {
				t.Fatal("missing critical trigger unexpectedly passed verification")
			}
			appError, ok := apperr.As(err)
			if !ok || appError.Code != "DATABASE_SCHEMA_DRIFT" {
				t.Fatalf("verification error = %v, want DATABASE_SCHEMA_DRIFT", err)
			}
		})
	}
}
