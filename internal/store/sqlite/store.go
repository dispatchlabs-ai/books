package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"

	_ "github.com/mattn/go-sqlite3"
)

const (
	ApplicationID        = 1112493899
	CurrentSchemaVersion = 1
)

type Mode int

const (
	ReadOnly Mode = iota
	ReadWrite
	Create
)

type Store struct {
	db   *sql.DB
	path string
	mode Mode
}

func Open(ctx context.Context, path string, mode Mode) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, apperr.New(apperr.Invalid, "DATABASE_PATH_REQUIRED", "database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, apperr.Wrap(apperr.Invalid, "DATABASE_PATH_INVALID", "resolve database path", err)
	}
	created := false
	if mode == Create {
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, apperr.Wrap(apperr.Unavailable, "DATABASE_DIRECTORY_FAILED", "create database directory", err)
		}
		file, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if errors.Is(err, os.ErrExist) {
			return nil, apperr.New(apperr.Conflict, "DATABASE_EXISTS", "database already exists")
		}
		if err != nil {
			return nil, apperr.Wrap(apperr.Unavailable, "DATABASE_UNAVAILABLE", "create database file", err)
		}
		created = true
		if err := file.Close(); err != nil {
			_ = removeDatabaseFiles(abs)
			return nil, apperr.Wrap(apperr.Unavailable, "DATABASE_UNAVAILABLE", "create database file", err)
		}
	}
	cleanupCreate := func() {
		if created {
			_ = removeDatabaseFiles(abs)
		}
	}

	query := url.Values{}
	switch mode {
	case ReadOnly:
		query.Set("mode", "ro")
		query.Set("_query_only", "1")
	case ReadWrite:
		query.Set("mode", "rw")
		query.Set("_txlock", "immediate")
	case Create:
		query.Set("mode", "rw")
		query.Set("_txlock", "immediate")
	default:
		cleanupCreate()
		return nil, apperr.New(apperr.Invalid, "DATABASE_MODE_INVALID", "invalid database open mode")
	}
	query.Set("_busy_timeout", "10000")
	query.Set("_foreign_keys", "1")
	query.Set("_recursive_triggers", "1")
	if mode != ReadOnly {
		query.Set("_journal_mode", "DELETE")
		query.Set("_synchronous", "EXTRA")
		query.Set("_locking_mode", "NORMAL")
	}
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		cleanupCreate()
		return nil, mapSQLiteError("open database", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		cleanupCreate()
		return nil, mapSQLiteError("open database", err)
	}
	s := &Store{db: db, path: abs, mode: mode}
	if err := s.verifyConnection(ctx); err != nil {
		_ = db.Close()
		cleanupCreate()
		return nil, err
	}
	if mode == Create {
		if err := os.Chmod(abs, 0o600); err != nil {
			_ = db.Close()
			cleanupCreate()
			return nil, apperr.Wrap(apperr.Unavailable, "DATABASE_PERMISSIONS_FAILED", "secure database file", err)
		}
	}
	return s, nil
}

func (s *Store) verifyConnection(ctx context.Context) error {
	var foreignKeys, recursiveTriggers int
	if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return mapSQLiteError("verify foreign keys", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA recursive_triggers").Scan(&recursiveTriggers); err != nil {
		return mapSQLiteError("verify recursive triggers", err)
	}
	if foreignKeys != 1 || recursiveTriggers != 1 {
		return apperr.New(apperr.Integrity, "SQLITE_SAFETY_DISABLED", "required SQLite safety settings are disabled")
	}
	return nil
}

func (s *Store) VerifySchema(ctx context.Context) error {
	var applicationID, version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return mapSQLiteError("read application id", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return mapSQLiteError("read schema version", err)
	}
	if applicationID != ApplicationID {
		return apperr.New(apperr.Integrity, "DATABASE_WRONG_APPLICATION", "file is not a books database")
	}
	if version > CurrentSchemaVersion {
		return apperr.New(apperr.Conflict, "DATABASE_TOO_NEW", "database schema is newer than this CLI")
	}
	if version < CurrentSchemaVersion {
		return apperr.New(apperr.Conflict, "MIGRATION_REQUIRED", "database migration is required")
	}
	if err := verifyMigrationLedgerCompleteness(ctx, s.db, version); err != nil {
		return err
	}
	if err := verifyMigrationChecksums(ctx, s.db); err != nil {
		return err
	}
	return verifyLiveSchema(ctx, s.db)
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }
func (s *Store) Mode() Mode   { return s.mode }

func (s *Store) Begin(ctx context.Context) (*sql.Tx, error) {
	if s.mode == ReadOnly {
		return nil, apperr.New(apperr.Conflict, "DATABASE_READ_ONLY", "database is open read-only")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapSQLiteError("begin write transaction", err)
	}
	return tx, nil
}

func (s *Store) Close() error { return s.db.Close() }

func UTCNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func normalizeMutationActor(actor string) (string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "", apperr.New(apperr.Invalid, "ACTOR_REQUIRED", "actor is required for every mutation")
	}
	return actor, nil
}

func removeDatabaseFiles(path string) error {
	var result error
	for _, candidate := range []string{path, path + "-journal", path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func mapSQLiteError(action string, err error) error {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "later closed periods must be reopened first"):
		return apperr.New(apperr.Conflict, "PERIOD_REOPEN_ORDER", "later closed periods must be reopened first")
	case strings.Contains(message, "later completed reconciliations must be reopened first"):
		return apperr.New(apperr.Conflict, "RECONCILIATION_REOPEN_ORDER", "later completed reconciliations must be reopened first")
	case strings.Contains(message, "overlapping closed book periods must be reopened first"):
		return apperr.New(apperr.Conflict, "RECONCILIATION_REOPEN_PERIOD_CLOSED", "overlapping closed book periods must be reopened first")
	case strings.Contains(message, "fiscal periods overlap"):
		return apperr.New(apperr.Conflict, "PERIOD_OVERLAP", "fiscal period overlaps an existing period")
	case strings.Contains(message, "database is locked"), strings.Contains(message, "database is busy"):
		return apperr.Wrap(apperr.Unavailable, "DATABASE_BUSY", action, err)
	case strings.Contains(message, "constraint failed"), strings.Contains(message, "unique constraint"), strings.Contains(message, "foreign key constraint"):
		return apperr.Wrap(apperr.Validation, "ACCOUNTING_CONSTRAINT", action, err)
	case strings.Contains(message, "unable to open database file"), strings.Contains(message, "readonly database"):
		return apperr.Wrap(apperr.Unavailable, "DATABASE_UNAVAILABLE", action, err)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}

// MapError converts driver errors into the stable application error contract.
func MapError(action string, err error) error { return mapSQLiteError(action, err) }
