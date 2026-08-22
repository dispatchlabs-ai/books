package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/money"
	"github.com/dispatchlabs-ai/books/internal/version"

	driver "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

type schemaObject struct {
	Type      string
	Name      string
	TableName string
	SQL       string
}

type migrationSchemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type expectedSchemaCacheEntry struct {
	once    sync.Once
	objects []schemaObject
	err     error
}

const migrationLedgerDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL,
    app_version TEXT NOT NULL,
    sqlite_version TEXT NOT NULL
) STRICT`

var expectedSchemaCache sync.Map

func Init(ctx context.Context, path, currency, actor string) (*Store, error) {
	currency = money.NormalizeCurrency(currency)
	if !money.IsSupportedCurrency(currency) {
		return nil, apperr.New(apperr.Invalid, "CURRENCY_NOT_SUPPORTED", "this release supports USD as its only functional currency")
	}
	actor, err := normalizeMutationActor(actor)
	if err != nil {
		return nil, err
	}
	s, err := Open(ctx, path, Create)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.Close()
			_ = removeDatabaseFiles(s.Path())
		}
	}()
	if err := applyMigrations(ctx, s.db); err != nil {
		return nil, err
	}
	databaseID, err := NewID()
	if err != nil {
		return nil, err
	}
	tx, err := s.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO database_metadata(singleton, database_uuid, created_at, base_currency) VALUES(1, ?, ?, ?)",
		databaseID, UTCNow(), currency); err != nil {
		return nil, mapSQLiteError("initialize database metadata", err)
	}
	if _, err := AppendAudit(ctx, tx, AuditInput{
		Actor: actor, Command: "db init", AggregateType: "database", AggregateID: databaseID,
		Payload: map[string]any{"base_currency": currency},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, mapSQLiteError("commit database initialization", err)
	}
	if err := s.VerifySchema(ctx); err != nil {
		return nil, err
	}
	cleanup = false
	return s, nil
}

type inspectedMigrationTarget struct {
	path         string
	databaseUUID string
	fileInfo     os.FileInfo
}

func Migrate(ctx context.Context, path string) error {
	target, err := inspectMigrationTarget(ctx, path)
	if err != nil {
		return err
	}
	s, err := openInspectedMigrationTarget(ctx, target)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(s)
	if err := applyMigrationsForDatabase(ctx, s.db, target.databaseUUID); err != nil {
		return err
	}
	return s.VerifySchema(ctx)
}

// ValidateMigrationTarget verifies, without opening the file for writes, that
// path is an initialized Books database eligible for forward migrations.
func ValidateMigrationTarget(ctx context.Context, path string) error {
	_, err := inspectMigrationTarget(ctx, path)
	return err
}

func inspectMigrationTarget(ctx context.Context, path string) (inspectedMigrationTarget, error) {
	abs, err := filepathAbs(path)
	if err != nil {
		return inspectedMigrationTarget{}, err
	}
	initialInfo, err := os.Stat(abs)
	if err != nil {
		return inspectedMigrationTarget{}, mapSQLiteError("inspect migration target", err)
	}
	s, err := Open(ctx, path, ReadOnly)
	if err != nil {
		return inspectedMigrationTarget{}, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(s)
	if err := verifyMigrationTarget(ctx, s.db); err != nil {
		return inspectedMigrationTarget{}, err
	}
	if err := verifyForwardMigrationEligibility(ctx, s.db); err != nil {
		return inspectedMigrationTarget{}, err
	}
	finalInfo, err := os.Stat(s.Path())
	if err != nil {
		return inspectedMigrationTarget{}, mapSQLiteError("inspect migration target identity", err)
	}
	if !os.SameFile(initialInfo, finalInfo) {
		return inspectedMigrationTarget{}, migrationTargetChanged()
	}
	var databaseUUID string
	if err := s.db.QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		return inspectedMigrationTarget{}, invalidMigrationMetadata("read books database identity", err)
	}
	return inspectedMigrationTarget{path: s.Path(), databaseUUID: databaseUUID, fileInfo: finalInfo}, nil
}

func filepathAbs(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", apperr.New(apperr.Invalid, "DATABASE_PATH_REQUIRED", "database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", apperr.Wrap(apperr.Invalid, "DATABASE_PATH_INVALID", "resolve database path", err)
	}
	return abs, nil
}

func openInspectedMigrationTarget(ctx context.Context, target inspectedMigrationTarget) (*Store, error) {
	s, err := Open(ctx, target.path, ReadWrite)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*Store, error) {
		_ = s.Close()
		return nil, err
	}
	currentInfo, err := os.Stat(s.Path())
	if err != nil {
		return closeOnError(mapSQLiteError("recheck migration target identity", err))
	}
	if !os.SameFile(target.fileInfo, currentInfo) {
		return closeOnError(migrationTargetChanged())
	}
	if err := verifyMigrationTarget(ctx, s.db); err != nil {
		return closeOnError(err)
	}
	var databaseUUID string
	if err := s.db.QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		return closeOnError(invalidMigrationMetadata("recheck books database identity", err))
	}
	if databaseUUID != target.databaseUUID {
		return closeOnError(migrationTargetChanged())
	}
	return s, nil
}

func migrationTargetChanged() error {
	return apperr.New(apperr.Integrity, "DATABASE_TARGET_CHANGED", "database path or identity changed after read-only migration inspection")
}

func verifyForwardMigrationEligibility(ctx context.Context, source *sql.DB) error {
	var schemaVersion int
	if err := source.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schemaVersion); err != nil {
		return mapSQLiteError("read schema version", err)
	}
	if schemaVersion == CurrentSchemaVersion {
		return nil
	}
	clone, err := sql.Open("sqlite3", "file:books-migration-preflight?mode=memory&cache=private&_foreign_keys=1&_recursive_triggers=1&_txlock=immediate")
	if err != nil {
		return mapSQLiteError("open migration preflight database", err)
	}
	clone.SetMaxOpenConns(1)
	clone.SetMaxIdleConns(1)
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(clone)
	if err := clone.PingContext(ctx); err != nil {
		return mapSQLiteError("open migration preflight database", err)
	}
	if err := cloneSQLiteDatabase(ctx, clone, source); err != nil {
		return MapError("copy migration preflight database", err)
	}
	if err := applyMigrations(ctx, clone); err != nil {
		return err
	}
	return verifyMigrationTarget(ctx, clone)
}

func cloneSQLiteDatabase(ctx context.Context, destination, source *sql.DB) error {
	sourceConn, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(sourceConn)
	destinationConn, err := destination.Conn(ctx)
	if err != nil {
		return err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(destinationConn)
	return destinationConn.Raw(func(destinationRaw any) error {
		return sourceConn.Raw(func(sourceRaw any) error {
			destinationSQLite, ok := destinationRaw.(*driver.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected destination SQLite driver %T", destinationRaw)
			}
			sourceSQLite, ok := sourceRaw.(*driver.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected source SQLite driver %T", sourceRaw)
			}
			backup, err := destinationSQLite.Backup("main", sourceSQLite, "main")
			if err != nil {
				return err
			}
			for {
				done, err := backup.Step(256)
				if err != nil {
					_ = backup.Close()
					return err
				}
				if done {
					return backup.Finish()
				}
			}
		})
	})
}

func verifyMigrationTarget(ctx context.Context, db *sql.DB) error {
	var applicationID, schemaVersion int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return mapSQLiteError("read application id", err)
	}
	if applicationID != ApplicationID {
		return apperr.New(apperr.Integrity, "DATABASE_WRONG_APPLICATION", "file is not a books database")
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schemaVersion); err != nil {
		return mapSQLiteError("read schema version", err)
	}
	if schemaVersion > CurrentSchemaVersion {
		return apperr.New(apperr.Conflict, "DATABASE_TOO_NEW", "database schema is newer than this CLI")
	}
	if schemaVersion < 1 {
		return invalidMigrationMetadata("books database has no recorded schema version", nil)
	}

	if err := verifyInitializedMigrationMetadata(ctx, db); err != nil {
		return err
	}
	if err := verifyMigrationLedgerCompleteness(ctx, db, schemaVersion); err != nil {
		return err
	}
	if err := verifyMigrationChecksums(ctx, db); err != nil {
		return err
	}
	return verifyLiveSchemaAtVersion(ctx, db, schemaVersion)
}

func verifyInitializedMigrationMetadata(ctx context.Context, db migrationSchemaQueryer) error {
	var requiredTables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM sqlite_schema
		WHERE type = 'table' AND name IN ('database_metadata', 'schema_migrations')`).Scan(&requiredTables); err != nil {
		return invalidMigrationMetadata("inspect books migration metadata", err)
	}
	if requiredTables != 2 {
		return invalidMigrationMetadata("books database is missing required migration metadata", nil)
	}
	var metadataRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM database_metadata
		WHERE singleton = 1
		  AND length(trim(database_uuid)) > 0
		  AND length(trim(created_at)) > 0
		  AND length(base_currency) = 3
		  AND base_currency = upper(base_currency)`).Scan(&metadataRows); err != nil {
		return invalidMigrationMetadata("inspect books database metadata", err)
	}
	var allMetadataRows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM database_metadata").Scan(&allMetadataRows); err != nil {
		return invalidMigrationMetadata("inspect books database metadata", err)
	}
	if metadataRows != 1 || allMetadataRows != 1 {
		return invalidMigrationMetadata("books database metadata is missing or invalid", nil)
	}
	return nil
}

func invalidMigrationMetadata(message string, err error) error {
	if err != nil {
		return apperr.Wrap(apperr.Integrity, "DATABASE_MIGRATION_METADATA_INVALID", message, err)
	}
	return apperr.New(apperr.Integrity, "DATABASE_MIGRATION_METADATA_INVALID", message)
}

func verifyMigrationLedgerCompleteness(ctx context.Context, db migrationSchemaQueryer, schemaVersion int) error {
	var migrationCount, minimumMigration, maximumMigration int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(version), 0), COALESCE(MAX(version), 0)
	        FROM schema_migrations`).Scan(&migrationCount, &minimumMigration, &maximumMigration); err != nil {
		return invalidMigrationMetadata("inspect books migration ledger", err)
	}
	if migrationCount != schemaVersion || minimumMigration != 1 || maximumMigration != schemaVersion {
		return invalidMigrationMetadata("books migration ledger does not match the recorded schema version", nil)
	}
	return nil
}

func verifyLiveSchema(ctx context.Context, db *sql.DB) error {
	return verifyLiveSchemaAtVersion(ctx, db, CurrentSchemaVersion)
}

func verifyLiveSchemaAtVersion(ctx context.Context, db migrationSchemaQueryer, schemaVersion int) error {
	expectedSchemaObjects, err := expectedSchemaAtVersion(schemaVersion)
	if err != nil {
		return apperr.Wrap(apperr.Integrity, "SCHEMA_MANIFEST_FAILED", "build expected database schema", err)
	}
	actual, err := readSchemaObjects(ctx, db)
	if err != nil {
		return err
	}
	expectedByKey := make(map[string]schemaObject, len(expectedSchemaObjects))
	for _, object := range expectedSchemaObjects {
		expectedByKey[schemaObjectKey(object)] = object
	}
	for _, object := range actual {
		key := schemaObjectKey(object)
		expected, ok := expectedByKey[key]
		if !ok {
			return schemaDriftError("unexpected", object)
		}
		if object.TableName != expected.TableName || object.SQL != expected.SQL {
			return schemaDriftError("changed", object)
		}
		delete(expectedByKey, key)
	}
	if len(expectedByKey) != 0 {
		missing := make([]schemaObject, 0, len(expectedByKey))
		for _, object := range expectedByKey {
			missing = append(missing, object)
		}
		sort.Slice(missing, func(i, j int) bool {
			return schemaObjectKey(missing[i]) < schemaObjectKey(missing[j])
		})
		return schemaDriftError("missing", missing[0])
	}
	return nil
}

func expectedSchemaAtVersion(schemaVersion int) ([]schemaObject, error) {
	entryValue, _ := expectedSchemaCache.LoadOrStore(schemaVersion, &expectedSchemaCacheEntry{})
	entry := entryValue.(*expectedSchemaCacheEntry)
	entry.once.Do(func() {
		entry.objects, entry.err = buildExpectedSchemaAtVersion(schemaVersion)
	})
	return entry.objects, entry.err
}

func buildExpectedSchemaAtVersion(schemaVersion int) ([]schemaObject, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	if schemaVersion < 1 || schemaVersion > len(migrations) {
		return nil, fmt.Errorf("schema version %d is outside the embedded migration bundle", schemaVersion)
	}
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(db)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		return nil, err
	}
	for _, migration := range migrations[:schemaVersion] {
		if _, err := db.ExecContext(ctx, migration.SQL); err != nil {
			return nil, err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations
			(version, name, checksum, applied_at, app_version, sqlite_version)
			VALUES (?, ?, ?, 'manifest', 'manifest', 'manifest')`, migration.Version, migration.Name, migration.Checksum); err != nil {
			return nil, err
		}
	}
	return readSchemaObjects(ctx, db)
}

func readSchemaObjects(ctx context.Context, db migrationSchemaQueryer) ([]schemaObject, error) {
	rows, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_schema
		WHERE type IN ('table', 'index', 'trigger', 'view')
		  AND name NOT GLOB 'sqlite_*'
		ORDER BY type, name, tbl_name`)
	if err != nil {
		return nil, mapSQLiteError("read live database schema", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	var result []schemaObject
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.TableName, &object.SQL); err != nil {
			return nil, err
		}
		object.SQL = canonicalSchemaSQL(object.SQL)
		result = append(result, object)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError("read live database schema", err)
	}
	return result, nil
}

func canonicalSchemaSQL(value string) string {
	var result strings.Builder
	var quote byte
	pendingSpace := false
	for index := 0; index < len(value); index++ {
		current := value[index]
		if quote != 0 {
			result.WriteByte(current)
			if quote == '[' {
				if current == ']' {
					quote = 0
				}
				continue
			}
			if current == quote {
				if index+1 < len(value) && value[index+1] == quote {
					index++
					result.WriteByte(value[index])
				} else {
					quote = 0
				}
			}
			continue
		}
		if current == ' ' || current == '\t' || current == '\n' || current == '\r' || current == '\f' {
			pendingSpace = result.Len() != 0
			continue
		}
		if pendingSpace {
			result.WriteByte(' ')
			pendingSpace = false
		}
		result.WriteByte(current)
		if current == '\'' || current == '"' || current == '`' || current == '[' {
			quote = current
		}
	}
	return strings.TrimSpace(result.String())
}

func schemaObjectKey(object schemaObject) string {
	return object.Type + "\x00" + object.Name
}

func schemaDriftError(condition string, object schemaObject) error {
	return apperr.New(
		apperr.Integrity,
		"DATABASE_SCHEMA_DRIFT",
		fmt.Sprintf("database schema has %s %s %q", condition, object.Type, object.Name),
	)
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid migration version %q: %w", entry.Name(), err)
		}
		contents, err := fs.ReadFile(migrationFiles, "migrations/"+entry.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(contents)
		migrations = append(migrations, migration{Version: v, Name: entry.Name(), SQL: string(contents), Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for i, migration := range migrations {
		if migration.Version != i+1 {
			return nil, fmt.Errorf("migration sequence has gap at %d", i+1)
		}
	}
	return migrations, nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	return applyMigrationsForDatabase(ctx, db, "")
}

func applyMigrationsForDatabase(ctx context.Context, db *sql.DB, expectedDatabaseUUID string) error {
	migrations, err := loadMigrations()
	if err != nil {
		return apperr.Wrap(apperr.Integrity, "MIGRATION_BUNDLE_INVALID", "load migrations", err)
	}
	if _, err := db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		return mapSQLiteError("initialize migration ledger", err)
	}
	if err := verifyMigrationChecksumsAgainst(ctx, db, migrations); err != nil {
		return err
	}
	var recordedVersion, migrationCount int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&recordedVersion); err != nil {
		return mapSQLiteError("read schema version", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		return mapSQLiteError("inspect migration state", err)
	}
	if recordedVersion < 0 || recordedVersion > len(migrations) {
		return apperr.New(apperr.Conflict, "DATABASE_TOO_NEW", "database schema is newer than this CLI")
	}
	if recordedVersion == 0 {
		if migrationCount != 0 {
			return invalidMigrationMetadata("uninitialized database has migration ledger entries", nil)
		}
	} else if err := verifyMigrationSchemaState(ctx, db, recordedVersion, migrations); err != nil {
		return err
	}
	if recordedVersion == len(migrations) {
		return verifyMigrationSchemaState(ctx, db, recordedVersion, migrations)
	}
	return applyMigrationBatchForDatabase(ctx, db, migrations, migrations[recordedVersion:], verifyMigrationTargetState, expectedDatabaseUUID)
}

type migrationTargetVerifier func(context.Context, *sql.Tx, int, []migration) error

func verifyMigrationTargetState(ctx context.Context, tx *sql.Tx, schemaVersion int, migrations []migration) error {
	return verifyMigrationSchemaState(ctx, tx, schemaVersion, migrations)
}

func applyMigrationBatchForDatabase(ctx context.Context, db *sql.DB, migrations, candidates []migration, verifyTarget migrationTargetVerifier, expectedDatabaseUUID string) error {
	if len(candidates) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mapSQLiteError("begin migration", err)
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if candidates[0].Version > 1 {
		if expectedDatabaseUUID != "" {
			var databaseUUID string
			if err := tx.QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
				return invalidMigrationMetadata("recheck books database identity in migration transaction", err)
			}
			if databaseUUID != expectedDatabaseUUID {
				return migrationTargetChanged()
			}
		}
		if err := verifyInitializedMigrationMetadata(ctx, tx); err != nil {
			return err
		}
		if err := verifyMigrationSchemaState(ctx, tx, candidates[0].Version-1, migrations); err != nil {
			return err
		}
	}
	for _, candidate := range candidates {
		if _, err := tx.ExecContext(ctx, candidate.SQL); err != nil {
			return mapSQLiteError("apply migration "+candidate.Name, err)
		}
		var sqliteVersion string
		if err := tx.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&sqliteVersion); err != nil {
			return mapSQLiteError("read SQLite version", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations
			(version, name, checksum, applied_at, app_version, sqlite_version)
			VALUES (?, ?, ?, ?, ?, ?)`, candidate.Version, candidate.Name, candidate.Checksum, UTCNow(), version.Identifier(), sqliteVersion); err != nil {
			return mapSQLiteError("record migration "+candidate.Name, err)
		}
		if err := verifyTarget(ctx, tx, candidate.Version, migrations); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return mapSQLiteError("commit migrations", err)
	}
	return nil
}

func verifyMigrationSchemaState(ctx context.Context, db migrationSchemaQueryer, schemaVersion int, migrations []migration) error {
	var applicationID, recordedVersion int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return mapSQLiteError("read application id", err)
	}
	if applicationID != ApplicationID {
		return apperr.New(apperr.Integrity, "DATABASE_WRONG_APPLICATION", "file is not a books database")
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&recordedVersion); err != nil {
		return mapSQLiteError("read schema version", err)
	}
	if recordedVersion != schemaVersion {
		return invalidMigrationMetadata("books database schema version does not match the migration target", nil)
	}
	if err := verifyMigrationLedgerCompleteness(ctx, db, schemaVersion); err != nil {
		return err
	}
	if err := verifyMigrationChecksumsAgainst(ctx, db, migrations); err != nil {
		return err
	}
	return verifyLiveSchemaAtVersion(ctx, db, schemaVersion)
}

func verifyMigrationChecksums(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	return verifyMigrationChecksumsAgainst(ctx, db, migrations)
}

func verifyMigrationChecksumsAgainst(ctx context.Context, db migrationSchemaQueryer, migrations []migration) error {
	expected := make(map[int]migration, len(migrations))
	for _, migration := range migrations {
		expected[migration.Version] = migration
	}
	rows, err := db.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return mapSQLiteError("verify migration ledger", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	for rows.Next() {
		var versionNumber int
		var name, checksum string
		if err := rows.Scan(&versionNumber, &name, &checksum); err != nil {
			return err
		}
		migration, ok := expected[versionNumber]
		if !ok {
			return apperr.New(apperr.Conflict, "DATABASE_TOO_NEW", "database contains an unknown migration")
		}
		if migration.Name != name || migration.Checksum != checksum {
			return apperr.New(apperr.Integrity, "MIGRATION_CHECKSUM_MISMATCH", "an applied migration differs from the embedded migration")
		}
	}
	return rows.Err()
}
