package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"

	driver "github.com/mattn/go-sqlite3"
)

type BackupResult struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	CreatedAt   string `json:"created_at"`
	AuditEvents int64  `json:"audit_events"`
}

type RestoreEntityIdentity struct {
	Code      string `json:"code"`
	LegalName string `json:"legal_name"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
}

type RestoreBookIdentity struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	EntityCode string `json:"entity_code,omitempty"`
	GroupCode  string `json:"group_code,omitempty"`
	Basis      string `json:"basis"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
}

type RestoreValidation struct {
	Source                     string                  `json:"source"`
	Target                     string                  `json:"target"`
	SourceSHA256               string                  `json:"source_sha256"`
	SourceDatabaseUUID         string                  `json:"source_database_uuid"`
	PreviousTargetDatabaseUUID string                  `json:"previous_target_database_uuid,omitempty"`
	SourceBaseCurrency         string                  `json:"source_base_currency"`
	SourceEntities             []RestoreEntityIdentity `json:"source_entities"`
	SourceBooks                []RestoreBookIdentity   `json:"source_books"`
}

type RestoreResult struct {
	Source                     string                  `json:"source"`
	Target                     string                  `json:"target"`
	PreRestoreBackup           string                  `json:"pre_restore_backup,omitempty"`
	SourceSHA256               string                  `json:"source_sha256"`
	SourceDatabaseUUID         string                  `json:"source_database_uuid"`
	PreviousTargetDatabaseUUID string                  `json:"previous_target_database_uuid,omitempty"`
	SourceBaseCurrency         string                  `json:"source_base_currency"`
	SourceEntities             []RestoreEntityIdentity `json:"source_entities"`
	SourceBooks                []RestoreBookIdentity   `json:"source_books"`
}

// RestoreExpectation binds a restore to registry identity that remains
// available even when the target database is missing or has been replaced.
// Empty fields preserve the advanced direct-database behavior, which binds an
// existing target to its own database UUID and permits adoption at a new path.
type RestoreExpectation struct {
	DatabaseUUID string
	EntityCode   string
	BookCode     string
}

type restoreDatabaseIdentity struct {
	DatabaseUUID string
	BaseCurrency string
	Entities     []RestoreEntityIdentity
	Books        []RestoreBookIdentity
}

func Backup(ctx context.Context, source *Store, destination, actor string) (result BackupResult, returnErr error) {
	if source.mode == ReadOnly {
		return BackupResult{}, apperr.New(apperr.Conflict, "DATABASE_READ_ONLY", "backup requires a writable source so it can record the backup")
	}
	actor, err := normalizeMutationActor(actor)
	if err != nil {
		return BackupResult{}, err
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return BackupResult{}, err
	}
	if _, err := os.Stat(abs); err == nil {
		return BackupResult{}, apperr.New(apperr.Conflict, "BACKUP_EXISTS", "backup destination already exists")
	} else if !os.IsNotExist(err) {
		return BackupResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return BackupResult{}, err
	}
	temporary := abs + ".tmp"
	temporaryFile, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return BackupResult{}, apperr.New(apperr.Conflict, "BACKUP_TEMP_EXISTS", "backup temporary file already exists")
	}
	if err != nil {
		return BackupResult{}, err
	}
	if err := temporaryFile.Close(); err != nil {
		_ = removeDatabaseFiles(temporary)
		return BackupResult{}, err
	}
	defer func() { _ = removeDatabaseFiles(temporary) }()
	published := false
	defer func() {
		if returnErr == nil || !published {
			return
		}
		cleanupErr := removeDatabaseFiles(abs)
		if syncErr := fsyncDir(filepath.Dir(abs)); syncErr != nil {
			cleanupErr = errors.Join(cleanupErr, syncErr)
		}
		if cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back published backup: %w", cleanupErr))
		}
	}()
	destinationDB, err := sql.Open("sqlite3", "file:"+temporary+"?mode=rw&_foreign_keys=1&_journal_mode=DELETE&_synchronous=EXTRA")
	if err != nil {
		return BackupResult{}, err
	}
	destinationDB.SetMaxOpenConns(1)
	sourceConn, err := source.db.Conn(ctx)
	if err != nil {
		return BackupResult{}, errors.Join(err, destinationDB.Close())
	}
	destinationConn, err := destinationDB.Conn(ctx)
	if err != nil {
		return BackupResult{}, errors.Join(err, sourceConn.Close(), destinationDB.Close())
	}
	backupErr := destinationConn.Raw(func(destRaw any) error {
		return sourceConn.Raw(func(sourceRaw any) error {
			dest, ok := destRaw.(*driver.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected destination SQLite driver %T", destRaw)
			}
			src, ok := sourceRaw.(*driver.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected source SQLite driver %T", sourceRaw)
			}
			backup, err := dest.Backup("main", src, "main")
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
	backupErr = errors.Join(backupErr, destinationConn.Close(), sourceConn.Close(), destinationDB.Close())
	if backupErr != nil {
		return BackupResult{}, MapError("create online backup", backupErr)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return BackupResult{}, err
	}
	if err := fsyncFile(temporary); err != nil {
		return BackupResult{}, err
	}
	check, err := Open(ctx, temporary, ReadOnly)
	if err != nil {
		return BackupResult{}, err
	}
	doctor, err := check.Doctor(ctx)
	err = errors.Join(err, check.Close())
	if err != nil {
		return BackupResult{}, apperr.Wrap(apperr.Integrity, "BACKUP_INVALID", "backup verification failed", err)
	}
	digest, err := hashFile(temporary)
	if err != nil {
		return BackupResult{}, err
	}
	// The temporary file is in the destination directory, so a hard link gives
	// us portable atomic no-replace publication on macOS and Linux. Unlike
	// rename, link fails if any destination directory entry already exists.
	if err := os.Link(temporary, abs); errors.Is(err, os.ErrExist) {
		return BackupResult{}, apperr.New(apperr.Conflict, "BACKUP_EXISTS", "backup destination already exists")
	} else if err != nil {
		return BackupResult{}, err
	}
	published = true
	if err := os.Remove(temporary); err != nil {
		return BackupResult{}, err
	}
	if err := fsyncDir(filepath.Dir(abs)); err != nil {
		return BackupResult{}, err
	}
	id, err := NewID()
	if err != nil {
		return BackupResult{}, err
	}
	createdAt := UTCNow()
	tx, err := source.Begin(ctx)
	if err != nil {
		return BackupResult{}, err
	}
	defer func(transaction interface{ Rollback() error }) { _ = transaction.Rollback() }(tx)
	if _, err := tx.ExecContext(ctx, `INSERT INTO backup_records
        (id, created_at, created_by, destination, file_sha256, integrity_status)
        VALUES (?, ?, ?, ?, ?, 'OK')`, id, createdAt, actor, abs, digest); err != nil {
		return BackupResult{}, MapError("record backup", err)
	}
	if _, err := AppendAudit(ctx, tx, AuditInput{
		Actor: actor, Command: "db backup", AggregateType: "backup", AggregateID: id,
		Payload: map[string]any{"destination": abs, "sha256": digest},
	}); err != nil {
		return BackupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return BackupResult{}, MapError("commit backup record", err)
	}
	published = false
	return BackupResult{ID: id, Path: abs, SHA256: digest, CreatedAt: createdAt, AuditEvents: doctor.AuditEvents}, nil
}

func ValidateRestore(ctx context.Context, target, backup string, expected RestoreExpectation) (RestoreValidation, error) {
	targetAbs, backupAbs, targetInfo, targetExists, err := resolveRestorePaths(target, backup)
	if err != nil {
		return RestoreValidation{}, err
	}
	sourceIdentity, err := inspectRestoreDatabasePath(ctx, backupAbs, true)
	if err != nil {
		return RestoreValidation{}, apperr.Wrap(apperr.Integrity, "RESTORE_SOURCE_INVALID", "restore source failed verification", err)
	}
	digest, err := hashFile(backupAbs)
	if err != nil {
		return RestoreValidation{}, err
	}
	validation := RestoreValidation{
		Source:             backupAbs,
		Target:             targetAbs,
		SourceSHA256:       digest,
		SourceDatabaseUUID: sourceIdentity.DatabaseUUID,
		SourceBaseCurrency: sourceIdentity.BaseCurrency,
		SourceEntities:     sourceIdentity.Entities,
		SourceBooks:        sourceIdentity.Books,
	}
	if err := requireExpectedRestoreIdentity(sourceIdentity, expected); err != nil {
		return RestoreValidation{}, err
	}
	if !targetExists {
		return validation, nil
	}
	currentTargetInfo, err := os.Stat(targetAbs)
	if err != nil {
		return RestoreValidation{}, err
	}
	if !os.SameFile(targetInfo, currentTargetInfo) {
		return RestoreValidation{}, apperr.New(apperr.Conflict, "RESTORE_TARGET_CHANGED", "restore target changed while it was being validated")
	}
	targetIdentity, err := inspectRestoreDatabasePath(ctx, targetAbs, false)
	if err != nil {
		return RestoreValidation{}, err
	}
	validation.PreviousTargetDatabaseUUID = targetIdentity.DatabaseUUID
	if expected.DatabaseUUID == "" {
		if err := requireMatchingRestoreLineage(sourceIdentity.DatabaseUUID, targetIdentity.DatabaseUUID); err != nil {
			return RestoreValidation{}, err
		}
	}
	return validation, nil
}

func requireExpectedRestoreIdentity(source restoreDatabaseIdentity, expected RestoreExpectation) error {
	if expected.DatabaseUUID != "" && source.DatabaseUUID != expected.DatabaseUUID {
		return apperr.New(apperr.Conflict, "RESTORE_DATABASE_MISMATCH", "backup database lineage does not match the registered company")
	}
	if expected.EntityCode == "" && expected.BookCode == "" {
		return nil
	}
	if expected.EntityCode == "" || expected.BookCode == "" {
		return apperr.New(apperr.Invalid, "RESTORE_EXPECTATION_INVALID", "restore identity requires both entity and book codes")
	}
	var matches int
	for _, book := range source.Books {
		if book.Code == expected.BookCode && book.EntityCode == expected.EntityCode {
			matches++
		}
	}
	if matches != 1 {
		return apperr.New(apperr.Conflict, "RESTORE_COMPANY_MISMATCH", fmt.Sprintf("backup does not contain the selected company entity/book %s", expected.BookCode))
	}
	return nil
}

func Restore(ctx context.Context, target, backup, actor string, expected RestoreExpectation) (result RestoreResult, returnErr error) {
	actor, err := normalizeMutationActor(actor)
	if err != nil {
		return RestoreResult{}, err
	}
	targetAbs, backupAbs, targetInfo, targetExists, err := resolveRestorePaths(target, backup)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o700); err != nil {
		return RestoreResult{}, err
	}
	temporary := targetAbs + ".restore.tmp"
	defer func() { _ = removeDatabaseFiles(temporary) }()
	if err := copyFileExclusive(backupAbs, temporary); errors.Is(err, os.ErrExist) {
		return RestoreResult{}, apperr.New(apperr.Conflict, "RESTORE_TEMP_EXISTS", "restore temporary file already exists")
	} else if err != nil {
		return RestoreResult{}, err
	}
	sourceIdentity, err := inspectRestoreDatabasePath(ctx, temporary, true)
	if err != nil {
		return RestoreResult{}, apperr.Wrap(apperr.Integrity, "RESTORE_SOURCE_INVALID", "restore source failed verification", err)
	}
	if err := requireExpectedRestoreIdentity(sourceIdentity, expected); err != nil {
		return RestoreResult{}, err
	}
	digest, err := hashFile(temporary)
	if err != nil {
		return RestoreResult{}, err
	}
	result = RestoreResult{
		Source:             backupAbs,
		Target:             targetAbs,
		SourceSHA256:       digest,
		SourceDatabaseUUID: sourceIdentity.DatabaseUUID,
		SourceBaseCurrency: sourceIdentity.BaseCurrency,
		SourceEntities:     sourceIdentity.Entities,
		SourceBooks:        sourceIdentity.Books,
	}
	if targetExists {
		currentTargetInfo, err := os.Stat(targetAbs)
		if err != nil {
			return RestoreResult{}, err
		}
		if !os.SameFile(targetInfo, currentTargetInfo) {
			return RestoreResult{}, apperr.New(apperr.Conflict, "RESTORE_TARGET_CHANGED", "restore target changed before replacement")
		}
		targetStore, err := Open(ctx, targetAbs, ReadWrite)
		if err != nil {
			return RestoreResult{}, err
		}
		targetIdentity, identityErr := inspectRestoreDatabase(ctx, targetStore)
		if identityErr == nil {
			result.PreviousTargetDatabaseUUID = targetIdentity.DatabaseUUID
			if expected.DatabaseUUID == "" {
				identityErr = requireMatchingRestoreLineage(sourceIdentity.DatabaseUUID, targetIdentity.DatabaseUUID)
			}
		}
		if identityErr != nil {
			_ = targetStore.Close()
			return RestoreResult{}, identityErr
		}
		result.PreRestoreBackup = fmt.Sprintf("%s.pre-restore-%s.backup", targetAbs, time.Now().UTC().Format("20060102T150405.000000000Z"))
		_, err = Backup(ctx, targetStore, result.PreRestoreBackup, actor)
		closeErr := targetStore.Close()
		if err != nil {
			return RestoreResult{}, err
		}
		if closeErr != nil {
			return RestoreResult{}, closeErr
		}
	}
	replaced := targetAbs + ".replaced.tmp"
	if _, err := os.Stat(replaced); err == nil {
		return RestoreResult{}, apperr.New(apperr.Conflict, "RESTORE_TEMP_EXISTS", "restore replacement file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
	}
	if targetExists {
		currentTargetInfo, err := os.Stat(targetAbs)
		if err != nil {
			return RestoreResult{}, err
		}
		if !os.SameFile(targetInfo, currentTargetInfo) {
			return RestoreResult{}, apperr.New(apperr.Conflict, "RESTORE_TARGET_CHANGED", "restore target changed before replacement")
		}
		if err := os.Rename(targetAbs, replaced); err != nil {
			return RestoreResult{}, err
		}
	} else if _, err := os.Stat(targetAbs); err == nil {
		return RestoreResult{}, apperr.New(apperr.Conflict, "RESTORE_TARGET_CHANGED", "a restore target appeared after validation")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
	}
	if err := os.Rename(temporary, targetAbs); err != nil {
		if targetExists {
			_ = os.Rename(replaced, targetAbs)
		}
		return RestoreResult{}, err
	}
	swapped := true
	committed := false
	defer func() {
		if !swapped || committed {
			return
		}
		if rollbackErr := rollbackRestore(targetAbs, replaced, targetExists); rollbackErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("roll back failed restore: %w", rollbackErr))
		}
	}()
	if err := fsyncDir(filepath.Dir(targetAbs)); err != nil {
		return RestoreResult{}, err
	}
	if err := func() (operationErr error) {
		restored, err := Open(ctx, targetAbs, ReadWrite)
		if err != nil {
			return err
		}
		defer func() {
			operationErr = errors.Join(operationErr, restored.Close())
		}()
		tx, err := restored.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := AppendAudit(ctx, tx, AuditInput{Actor: actor, Command: "db restore", AggregateType: "database", AggregateID: targetAbs, Payload: map[string]any{
			"source":                        result.Source,
			"pre_restore_backup":            result.PreRestoreBackup,
			"source_sha256":                 result.SourceSHA256,
			"source_database_uuid":          result.SourceDatabaseUUID,
			"previous_target_database_uuid": result.PreviousTargetDatabaseUUID,
			"source_base_currency":          result.SourceBaseCurrency,
			"source_entities":               result.SourceEntities,
			"source_books":                  result.SourceBooks,
		}}); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return MapError("commit restore audit", err)
		}
		_, err = restored.Doctor(ctx)
		return err
	}(); err != nil {
		return RestoreResult{}, err
	}
	if err := fsyncFile(targetAbs); err != nil {
		return RestoreResult{}, err
	}
	if err := fsyncDir(filepath.Dir(targetAbs)); err != nil {
		return RestoreResult{}, err
	}
	committed = true
	if targetExists {
		_ = os.Remove(replaced)
		_ = fsyncDir(filepath.Dir(targetAbs))
	}
	return result, nil
}

func resolveRestorePaths(target, backup string) (targetAbs, backupAbs string, targetInfo os.FileInfo, targetExists bool, returnErr error) {
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", "", nil, false, err
	}
	backupAbs, err = filepath.Abs(backup)
	if err != nil {
		return "", "", nil, false, err
	}
	if targetAbs == backupAbs {
		return "", "", nil, false, apperr.New(apperr.Invalid, "RESTORE_SOURCE_IS_TARGET", "restore source must be a separate backup file")
	}
	sourceInfo, err := os.Stat(backupAbs)
	if err != nil {
		return "", "", nil, false, err
	}
	targetInfo, err = os.Stat(targetAbs)
	if err == nil {
		if os.SameFile(sourceInfo, targetInfo) {
			return "", "", nil, false, apperr.New(apperr.Invalid, "RESTORE_SOURCE_IS_TARGET", "restore source must be a separate backup file")
		}
		return targetAbs, backupAbs, targetInfo, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", nil, false, err
	}
	return targetAbs, backupAbs, nil, false, nil
}

func requireMatchingRestoreLineage(sourceUUID, targetUUID string) error {
	if sourceUUID != targetUUID {
		return apperr.New(apperr.Conflict, "RESTORE_DATABASE_MISMATCH", "backup database lineage does not match the restore target")
	}
	return nil
}

func inspectRestoreDatabasePath(ctx context.Context, path string, verify bool) (identity restoreDatabaseIdentity, returnErr error) {
	store, err := Open(ctx, path, ReadOnly)
	if err != nil {
		return restoreDatabaseIdentity{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, store.Close())
	}()
	if verify {
		if _, err := store.Doctor(ctx); err != nil {
			return restoreDatabaseIdentity{}, err
		}
	}
	return inspectRestoreDatabase(ctx, store)
}

func inspectRestoreDatabase(ctx context.Context, store *Store) (restoreDatabaseIdentity, error) {
	var identity restoreDatabaseIdentity
	if err := store.DB().QueryRowContext(ctx, `SELECT database_uuid, base_currency
		FROM database_metadata WHERE singleton = 1`).Scan(&identity.DatabaseUUID, &identity.BaseCurrency); err != nil {
		return restoreDatabaseIdentity{}, MapError("read database restore identity", err)
	}
	entityRows, err := store.DB().QueryContext(ctx, `SELECT code, legal_name, functional_currency, status
		FROM entities ORDER BY code`)
	if err != nil {
		return restoreDatabaseIdentity{}, MapError("read restore entity identities", err)
	}
	for entityRows.Next() {
		var entity RestoreEntityIdentity
		if err := entityRows.Scan(&entity.Code, &entity.LegalName, &entity.Currency, &entity.Status); err != nil {
			_ = entityRows.Close()
			return restoreDatabaseIdentity{}, err
		}
		identity.Entities = append(identity.Entities, entity)
	}
	if err := entityRows.Err(); err != nil {
		_ = entityRows.Close()
		return restoreDatabaseIdentity{}, err
	}
	if err := entityRows.Close(); err != nil {
		return restoreDatabaseIdentity{}, err
	}
	bookRows, err := store.DB().QueryContext(ctx, `SELECT b.code, b.name, b.kind,
		COALESCE(e.code, ''), COALESCE(g.code, ''), b.accounting_basis, b.currency, b.status
		FROM books b
		LEFT JOIN entities e ON e.id = b.entity_id
		LEFT JOIN consolidation_groups g ON g.id = b.group_id
		ORDER BY b.code`)
	if err != nil {
		return restoreDatabaseIdentity{}, MapError("read restore book identities", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(bookRows)
	for bookRows.Next() {
		var book RestoreBookIdentity
		if err := bookRows.Scan(&book.Code, &book.Name, &book.Kind, &book.EntityCode, &book.GroupCode, &book.Basis, &book.Currency, &book.Status); err != nil {
			return restoreDatabaseIdentity{}, err
		}
		identity.Books = append(identity.Books, book)
	}
	if err := bookRows.Err(); err != nil {
		return restoreDatabaseIdentity{}, err
	}
	return identity, nil
}

func rollbackRestore(target, replaced string, hadTarget bool) error {
	if err := removeDatabaseFiles(target); err != nil {
		return err
	}
	if hadTarget {
		if err := os.Rename(replaced, target); err != nil {
			return err
		}
	}
	return fsyncDir(filepath.Dir(target))
}

func hashFile(path string) (digest string, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFileExclusive(source, destination string) (returnErr error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, in.Close()) }()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, out.Close())
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	closed = true
	return out.Close()
}

func fsyncFile(path string) (returnErr error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	return file.Sync()
}

func fsyncDir(path string) (returnErr error) {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, directory.Close()) }()
	return directory.Sync()
}
