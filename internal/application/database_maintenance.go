package application

import (
	"context"
	"database/sql"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// DatabaseTarget is selected only from trusted operator configuration.
type DatabaseTarget struct{ key, path, uuid string }

func NewDatabaseTarget(key, path, uuid string) *DatabaseTarget {
	return &DatabaseTarget{key, path, uuid}
}
func (t *DatabaseTarget) Key() string { return t.key }
func (t *DatabaseTarget) Open(ctx context.Context) (*Database, error) {
	store, err := storesqlite.Open(ctx, t.path, storesqlite.ReadWrite)
	if err != nil {
		return nil, err
	}
	db, err := BindDatabase(ctx, t.key, store, t.uuid)
	if err != nil {
		_ = store.Close()
	}
	return db, err
}
func (t *DatabaseTarget) check(ctx context.Context) error {
	store, err := storesqlite.Open(ctx, t.path, storesqlite.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	info, err := store.DatabaseInfo(ctx)
	if err != nil {
		return err
	}
	if t.uuid != "" && t.uuid != info.DatabaseID {
		return apperr.New(apperr.Conflict, "DATABASE_IDENTITY_MISMATCH", "configured database identity does not match")
	}
	return nil
}

type MaintenanceStatus struct {
	Database   string `json:"database"`
	DatabaseID string `json:"database_id,omitempty"`
	Schema     int    `json:"schema"`
	DryRun     bool   `json:"dry_run"`
}

func (t *DatabaseTarget) Initialize(ctx context.Context, actor, currency string) (MaintenanceStatus, error) {
	if t.uuid != "" {
		return MaintenanceStatus{}, apperr.New(apperr.Conflict, "DATABASE_IDENTITY_PINNED", "initialization requires an uninitialized operator-configured target")
	}
	store, err := storesqlite.Init(ctx, t.path, currency, actor)
	if err != nil {
		return MaintenanceStatus{}, err
	}
	defer func() { _ = store.Close() }()
	info, err := store.DatabaseInfo(ctx)
	return MaintenanceStatus{Database: t.key, DatabaseID: info.DatabaseID, Schema: storesqlite.CurrentSchemaVersion}, err
}
func (t *DatabaseTarget) Migrate(ctx context.Context, dryRun bool) (MaintenanceStatus, error) {
	result := MaintenanceStatus{Database: t.key, Schema: storesqlite.CurrentSchemaVersion, DryRun: dryRun}
	if dryRun {
		if err := t.check(ctx); err != nil {
			return result, err
		}
		return result, storesqlite.ValidateMigrationTarget(ctx, t.path)
	}
	err := storesqlite.WithMaintenance(ctx, t.path, func(ctx context.Context) error {
		if err := t.check(ctx); err != nil {
			return err
		}
		return storesqlite.Migrate(ctx, t.path)
	})
	return result, err
}

type DatabaseBackup struct {
	Backup   storesqlite.BackupResult `json:"backup"`
	Artifact artifact.Reference       `json:"artifact"`
}

func (t *DatabaseTarget) Backup(ctx context.Context, actor, key string) (DatabaseBackup, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`).MatchString(key) {
		return DatabaseBackup{}, apperr.New(apperr.Invalid, "BACKUP_KEY_INVALID", "backup requires a stable simple key")
	}
	db, err := t.Open(ctx)
	if err != nil {
		return DatabaseBackup{}, err
	}
	defer func() { _ = db.Close() }()
	root := filepath.Join(filepath.Dir(t.path), "backups", db.Identity())
	if err = os.MkdirAll(root, 0700); err != nil {
		return DatabaseBackup{}, err
	}
	destination := filepath.Join(root, key+".backup")
	result, err := storesqlite.Backup(ctx, db.store, destination, actor)
	if e, ok := apperr.As(err); ok && e.Code == "BACKUP_EXISTS" {
		err = db.store.DB().QueryRowContext(ctx, `SELECT id,created_at,file_sha256 FROM backup_records WHERE destination=?`, destination).Scan(&result.ID, &result.CreatedAt, &result.SHA256)
		if err == sql.ErrNoRows {
			return DatabaseBackup{}, apperr.New(apperr.Conflict, "BACKUP_EXISTS", "backup destination exists without a matching receipt")
		}
		result.Path = destination
		if err == nil {
			check, e := storesqlite.Open(ctx, destination, storesqlite.ReadOnly)
			if e != nil {
				return DatabaseBackup{}, e
			}
			doctor, e := check.Doctor(ctx)
			_ = check.Close()
			if e != nil {
				return DatabaseBackup{}, e
			}
			result.AuditEvents = doctor.AuditEvents
		}
	}
	if err != nil {
		return DatabaseBackup{}, err
	}
	data, err := boundedFile(destination, artifact.MaxFile)
	if err != nil {
		return DatabaseBackup{}, err
	}
	ref, err := artifact.Put(artifact.Bind(ctx, actor, "database:"+db.Identity()), key+".backup", data)
	if err != nil {
		return DatabaseBackup{}, err
	}
	if ref.SHA256 != result.SHA256 {
		return DatabaseBackup{}, apperr.New(apperr.Integrity, "BACKUP_CHANGED", "backup no longer matches its recorded digest")
	}
	result.Path = "backup:" + key
	return DatabaseBackup{Backup: result, Artifact: ref}, nil
}
func boundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, apperr.New(apperr.Invalid, "FILE_TOO_LARGE", "file exceeds the transfer limit")
	}
	return data, nil
}

type DatabaseRestore struct {
	Validation       *storesqlite.RestoreValidation `json:"validation,omitempty"`
	Result           *storesqlite.RestoreResult     `json:"result,omitempty"`
	DryRun           bool                           `json:"dry_run"`
	RecoveryArtifact *artifact.Reference            `json:"recovery_artifact,omitempty"`
	Warning          string                         `json:"warning,omitempty"`
}

func (t *DatabaseTarget) Restore(ctx context.Context, actor, id, confirm string, dryRun bool) (DatabaseRestore, error) {
	output := DatabaseRestore{DryRun: dryRun}
	if !dryRun && confirm != t.key {
		return output, apperr.New(apperr.Invalid, "RESTORE_CONFIRMATION_REQUIRED", "confirm the exact configured database handle")
	}
	data, err := artifact.Bytes(ctx, id, artifact.MaxFile)
	if err != nil {
		return output, err
	}
	directory, err := os.MkdirTemp("", "books-restore-")
	if err != nil {
		return output, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	source := filepath.Join(directory, "source.backup")
	if err = os.WriteFile(source, data, 0600); err != nil {
		return output, err
	}
	expected := storesqlite.RestoreExpectation{DatabaseUUID: t.uuid}
	if dryRun {
		value, err := storesqlite.ValidateRestore(ctx, t.path, source, expected)
		value.Source = id
		value.Target = t.key
		output.Validation = &value
		return output, err
	}
	value, err := storesqlite.Restore(ctx, t.path, source, actor, expected)
	if err != nil {
		return output, err
	}
	value.Source = id
	value.Target = t.key
	if value.PreRestoreBackup != "" {
		backup, err := boundedFile(value.PreRestoreBackup, artifact.MaxFile)
		if err == nil {
			ref, e := artifact.Put(ctx, "pre-restore.backup", backup)
			err = e
			if err == nil {
				output.RecoveryArtifact = &ref
			}
		}
		if err != nil {
			output.Warning = "Restore succeeded; the pre-restore backup remains on the server but artifact delivery failed."
		}
	}
	output.Result = &value
	return output, nil
}

func (t *DatabaseTarget) ArtifactIdentity(ctx context.Context) (string, error) {
	if t.uuid != "" {
		return t.uuid, nil
	}
	if _, err := os.Stat(t.path); os.IsNotExist(err) {
		return "uninitialized:" + t.key, nil
	}
	store, err := storesqlite.Open(ctx, t.path, storesqlite.ReadOnly)
	if err != nil {
		return "", err
	}
	defer func() { _ = store.Close() }()
	info, err := store.DatabaseInfo(ctx)
	return info.DatabaseID, err
}

// PendingDatabase allows an explicitly authorized maintenance process to start
// so it can initialize a missing target or migrate an older supported schema.
func PendingDatabase(path string, err error) bool {
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return true
	}
	e, ok := apperr.As(err)
	return ok && e.Code == "MIGRATION_REQUIRED"
}

// OpenArtifactScope also permits uploading a backup for a missing identity-pinned
// target. Its absent store is never used for accounting operations.
func (t *DatabaseTarget) OpenArtifactScope(ctx context.Context) (*Database, error) {
	db, err := t.Open(ctx)
	if err == nil {
		return db, nil
	}
	if t.uuid != "" {
		if _, e := os.Stat(t.path); os.IsNotExist(e) {
			return &Database{key: t.key, identity: t.uuid}, nil
		}
	}
	return nil, err
}
