package cli

import (
	"fmt"
	"path/filepath"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/dispatchlabs-ai/books/internal/version"

	"github.com/spf13/cobra"
)

func newDBCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "Initialize, inspect, verify, back up, or restore the database"}
	command.AddCommand(newDBInitCommand(opts), newDBMigrateCommand(opts), newDBStatusCommand(opts), newDBDoctorCommand(opts), newDBBackupCommand(opts), newDBRestoreCommand(opts))
	return command
}

func newDBInitCommand(opts *options) *cobra.Command {
	var currency string
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new books database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "db init"); err != nil {
				return err
			}
			database, err := opts.resolveDatabase()
			if err != nil {
				return err
			}
			store, err := storesqlite.Init(cmd.Context(), database, currency, opts.actor)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			var databaseID, createdAt, baseCurrency string
			if err := store.DB().QueryRowContext(cmd.Context(), `SELECT database_uuid, created_at, base_currency FROM database_metadata WHERE singleton = 1`).Scan(&databaseID, &createdAt, &baseCurrency); err != nil {
				return err
			}
			data := map[string]any{"database_id": databaseID, "path": store.Path(), "created_at": createdAt, "base_currency": baseCurrency, "schema_version": storesqlite.CurrentSchemaVersion}
			return writeResult(cmd, opts.format, data, []string{"DATABASE ID", "PATH", "CURRENCY", "SCHEMA"}, [][]string{{databaseID, store.Path(), baseCurrency, fmt.Sprint(storesqlite.CurrentSchemaVersion)}})
		},
	}
	command.Flags().StringVar(&currency, "currency", "USD", "database base currency")
	return command
}

func newDBMigrateCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply forward-only database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := opts.resolveDatabase()
			if err != nil {
				return err
			}
			if opts.dryRun {
				if err := storesqlite.ValidateMigrationTarget(cmd.Context(), database); err != nil {
					return err
				}
				data := map[string]any{"path": database, "target_schema_version": storesqlite.CurrentSchemaVersion, "dry_run": true}
				return writeResult(cmd, opts.format, data, []string{"PATH", "TARGET", "DRY RUN"}, [][]string{{database, fmt.Sprint(storesqlite.CurrentSchemaVersion), "true"}})
			}
			if err := storesqlite.Migrate(cmd.Context(), database); err != nil {
				return err
			}
			data := map[string]any{"path": database, "schema_version": storesqlite.CurrentSchemaVersion}
			return writeResult(cmd, opts.format, data, []string{"PATH", "SCHEMA"}, [][]string{{database, fmt.Sprint(storesqlite.CurrentSchemaVersion)}})
		},
	}
}

func newDBStatusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show database identity and schema status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			var id, createdAt, currency, sqliteVersion string
			var migrationCount int
			if err := store.DB().QueryRowContext(cmd.Context(), `SELECT database_uuid, created_at, base_currency FROM database_metadata WHERE singleton = 1`).Scan(&id, &createdAt, &currency); err != nil {
				return err
			}
			if err := store.DB().QueryRowContext(cmd.Context(), "SELECT sqlite_version()").Scan(&sqliteVersion); err != nil {
				return err
			}
			if err := store.DB().QueryRowContext(cmd.Context(), "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
				return err
			}
			data := map[string]any{"database_id": id, "path": store.Path(), "created_at": createdAt, "base_currency": currency, "schema_version": storesqlite.CurrentSchemaVersion, "migration_count": migrationCount, "sqlite_version": sqliteVersion, "app_version": version.Identifier()}
			return writeResult(cmd, opts.format, data, []string{"DATABASE ID", "PATH", "CURRENCY", "SCHEMA", "SQLITE"}, [][]string{{id, store.Path(), currency, fmt.Sprint(storesqlite.CurrentSchemaVersion), sqliteVersion}})
		},
	}
}

func newDBDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify SQLite, accounting, close-digest, and audit invariants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := store.Doctor(cmd.Context())
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"OK", "INTEGRITY", "FK VIOLATIONS", "UNBALANCED", "AUDIT EVENTS"}, [][]string{{fmt.Sprint(result.OK), result.IntegrityCheck, fmt.Sprint(result.ForeignKeyViolations), fmt.Sprint(result.UnbalancedJournals), fmt.Sprint(result.AuditEvents)}})
		},
	}
}

func newDBBackupCommand(opts *options) *cobra.Command {
	var destination string
	command := &cobra.Command{
		Use:   "backup",
		Short: "Create and verify an online SQLite backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if destination == "" {
				return apperr.New(apperr.Invalid, "BACKUP_PATH_REQUIRED", "--out is required")
			}
			if err := requireCommit(opts, "db backup"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := storesqlite.Backup(cmd.Context(), store, destination, opts.actor)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"BACKUP ID", "PATH", "SHA256"}, [][]string{{result.ID, result.Path, result.SHA256}})
		},
	}
	command.Flags().StringVar(&destination, "out", "", "backup destination (must not exist)")
	return command
}

func newDBRestoreCommand(opts *options) *cobra.Command {
	var source, confirmation string
	command := &cobra.Command{
		Use:   "restore",
		Short: "Validate and atomically restore a database backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if source == "" {
				return apperr.New(apperr.Invalid, "RESTORE_SOURCE_REQUIRED", "--from is required")
			}
			database, err := opts.resolveDatabase()
			if err != nil {
				return err
			}
			targetAbs, _ := filepath.Abs(database)
			expected := storesqlite.RestoreExpectation{}
			var validation storesqlite.RestoreValidation
			var registered booksconfig.ResolvedCompany
			var backfillIdentity bool
			if opts.resolved != nil {
				registered, validation, expected, backfillIdentity, err = validateCompanyRestore(cmd, *opts.resolved, source)
			} else {
				validation, err = storesqlite.ValidateRestore(cmd.Context(), targetAbs, source, expected)
			}
			if err != nil {
				return err
			}
			if opts.dryRun {
				data := map[string]any{
					"source": validation.Source, "target": validation.Target,
					"source_sha256": validation.SourceSHA256, "source_database_uuid": validation.SourceDatabaseUUID,
					"previous_target_database_uuid": validation.PreviousTargetDatabaseUUID, "valid": true, "dry_run": true,
				}
				return writeResult(cmd, opts.format, data,
					[]string{"SOURCE", "TARGET", "SOURCE SHA256", "SOURCE DATABASE UUID", "VALID", "DRY RUN"},
					[][]string{{validation.Source, validation.Target, validation.SourceSHA256, validation.SourceDatabaseUUID, "true", "true"}})
			}
			if confirmation != targetAbs {
				return apperr.New(apperr.Invalid, "RESTORE_CONFIRMATION_REQUIRED", "--confirm must equal the absolute target database path")
			}
			if backfillIdentity {
				registered, err = persistResolvedCompanyDatabaseUUID(opts, registered, expected.DatabaseUUID)
				if err != nil {
					return err
				}
				expected.DatabaseUUID = registered.Company.DatabaseUUID
			}
			result, err := storesqlite.Restore(cmd.Context(), targetAbs, source, opts.actor, expected)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result,
				[]string{"SOURCE", "TARGET", "PRE-RESTORE BACKUP", "SOURCE SHA256", "SOURCE DATABASE UUID"},
				[][]string{{result.Source, result.Target, result.PreRestoreBackup, result.SourceSHA256, result.SourceDatabaseUUID}})
		},
	}
	command.Flags().StringVar(&source, "from", "", "validated backup source")
	command.Flags().StringVar(&confirmation, "confirm", "", "absolute target path confirmation")
	return command
}
