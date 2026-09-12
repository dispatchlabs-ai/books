package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type fixture struct {
	store   *storesqlite.Store
	service *ledger.Service
	path    string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := ledger.NewService(store, "test")
	if _, err := service.CreatePeriod(ctx, ledger.CreatePeriodInput{
		Code: "2026-07", StartDate: "2026-07-01", EndDate: "2026-07-31", FiscalYear: 2026, PeriodNumber: 7,
	}); err != nil {
		t.Fatalf("create period: %v", err)
	}
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "ACME", LegalName: "Acme, Inc.", Currency: "USD"}); err != nil {
		t.Fatalf("create entity: %v", err)
	}
	for _, account := range []ledger.CreateAccountInput{
		{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"ACME"}},
		{Code: "4000", Name: "Revenue", Type: "REVENUE", BookCodes: []string{"ACME"}},
	} {
		if _, err := service.CreateAccount(ctx, account); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	return fixture{store: store, service: service, path: path}
}

func (f fixture) createJournal(t *testing.T, debit, credit int64) ledger.Journal {
	t.Helper()
	journal, err := f.service.CreateJournal(context.Background(), ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Test sale",
		Lines: []ledger.JournalLineInput{
			{Account: "1000", DebitCents: debit},
			{Account: "4000", CreditCents: credit},
		},
	})
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}
	return journal
}

func TestInitAndDoctor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	info, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database permissions = %o, want 600", info.Mode().Perm())
	}
	result, err := f.store.Doctor(context.Background())
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !result.OK || result.AuditEvents < 4 {
		t.Fatalf("unexpected doctor result: %+v", result)
	}
}

func TestInitFailureLeavesNoDatabaseAndPreservesParentPermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "books.sqlite")
	if _, err := storesqlite.Init(ctx, path, "USD", "   "); err == nil {
		t.Fatal("init without an actor unexpectedly succeeded")
	} else if appError, ok := apperr.As(err); !ok || appError.Code != "ACTOR_REQUIRED" {
		t.Fatalf("init error = %v, want ACTOR_REQUIRED", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed init left database file behind: %v", err)
	}
	cancelledPath := filepath.Join(directory, "cancelled.sqlite")
	cancelledContext, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := storesqlite.Init(cancelledContext, cancelledPath, "USD", "test"); err == nil {
		t.Fatal("init with a cancelled context unexpectedly succeeded")
	}
	if _, err := os.Stat(cancelledPath); !os.IsNotExist(err) {
		t.Fatalf("failed post-create init left database file behind: %v", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing parent permissions = %o, want 755", info.Mode().Perm())
	}
}

func TestPostedJournalDatabaseInvariants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	journal := f.createJournal(t, 10_000, 10_000)
	posted, err := f.service.PostJournal(ctx, journal.ID)
	if err != nil {
		t.Fatalf("post journal: %v", err)
	}
	if posted.Status != "POSTED" {
		t.Fatalf("status = %s", posted.Status)
	}

	for name, statement := range map[string]string{
		"header update":              "UPDATE journal_entries SET description = 'changed' WHERE id = '" + journal.ID + "'",
		"header delete":              "DELETE FROM journal_entries WHERE id = '" + journal.ID + "'",
		"line update":                "UPDATE journal_lines SET debit_cents = 9999 WHERE id = '" + posted.Lines[0].ID + "'",
		"line delete":                "DELETE FROM journal_lines WHERE id = '" + posted.Lines[0].ID + "'",
		"account type reclassify":    "UPDATE accounts SET account_type = 'EXPENSE' WHERE code = '4000'",
		"account subtype reclassify": "UPDATE accounts SET subtype = 'SUSPENSE' WHERE code = '4000'",
		"account section reclassify": "UPDATE accounts SET statement_section = 'OTHER' WHERE code = '4000'",
	} {
		name, statement := name, statement
		t.Run(name, func(t *testing.T) {
			if _, err := f.store.DB().ExecContext(ctx, statement); err == nil {
				t.Fatalf("direct SQL mutation unexpectedly succeeded")
			}
		})
	}

	unbalanced := f.createJournal(t, 10_000, 9_999)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries SET status = 'POSTED', posted_at = '2026-07-31T00:00:00Z', posted_by = 'direct' WHERE id = ?`, unbalanced.ID); err == nil {
		t.Fatal("direct unbalanced posting unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_lines SET journal_entry_id = ?
        WHERE id = (SELECT id FROM journal_lines WHERE journal_entry_id = ? ORDER BY line_number LIMIT 1)`, journal.ID, unbalanced.ID); err == nil {
		t.Fatal("moving a draft line into a posted journal unexpectedly succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_lines SET debit_cents = 1.5 WHERE journal_entry_id = ? AND line_number = 1`, unbalanced.ID); err == nil {
		t.Fatal("floating-point ledger amount unexpectedly succeeded")
	}
	if err := f.service.AbandonJournal(ctx, unbalanced.ID); err != nil {
		t.Fatalf("abandon draft: %v", err)
	}
	if _, err := f.service.ClosePeriod(ctx, "ACME", "2026-07", false); err != nil {
		t.Fatalf("close period: %v", err)
	}
	closedDraft := f.createJournal(t, 2_000, 2_000)
	if _, err := f.service.PostJournal(ctx, closedDraft.ID); err == nil {
		t.Fatal("posting into a closed period unexpectedly succeeded")
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatalf("doctor after negative tests: %v", err)
	}
}

func TestReversalPreservesTaxMetadataAndCannotPredateOriginal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)

	postOriginal := func(description string) ledger.Journal {
		t.Helper()
		journal, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
			Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: description,
			TaxType: "FEDERAL_INCOME_TAX", TaxAccountingPeriod: "2026-Q2",
			Lines: []ledger.JournalLineInput{
				{Account: "1000", DebitCents: 1_000},
				{Account: "4000", CreditCents: 1_000},
			},
		})
		if err != nil {
			t.Fatalf("create original journal: %v", err)
		}
		posted, err := f.service.PostJournal(ctx, journal.ID)
		if err != nil {
			t.Fatalf("post original journal: %v", err)
		}
		return posted
	}

	original := postOriginal("Tax-tagged original")
	_, err := f.service.ReverseJournal(ctx, original.ID, "2026-07-14", "2026-07", "Predated reversal")
	requireAppError(t, err, "REVERSAL_DATE_INVALID", "cannot precede")

	reversal, err := f.service.ReverseJournal(ctx, original.ID, "2026-07-16", "2026-07", "Valid reversal")
	if err != nil {
		t.Fatalf("create reversal: %v", err)
	}
	if reversal.TaxType != original.TaxType || reversal.TaxAccountingPeriod != original.TaxAccountingPeriod {
		t.Fatalf("reversal tax metadata = %q/%q, want %q/%q",
			reversal.TaxType, reversal.TaxAccountingPeriod, original.TaxType, original.TaxAccountingPeriod)
	}
	if _, err := f.service.PostJournal(ctx, reversal.ID); err != nil {
		t.Fatalf("post metadata-preserving reversal: %v", err)
	}

	mismatchedOriginal := postOriginal("Tax metadata trigger original")
	mismatched, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-16", Period: "2026-07", Description: "Mismatched tax reversal",
		ReversalOfID: mismatchedOriginal.ID,
		Lines: []ledger.JournalLineInput{
			{Account: "1000", CreditCents: 1_000},
			{Account: "4000", DebitCents: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("create mismatched reversal draft: %v", err)
	}
	_, err = f.service.PostJournal(ctx, mismatched.ID)
	requireAppError(t, err, "JOURNAL_INVALID", "tax type")
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries
		SET status = 'POSTED', posted_at = '2026-07-16T00:00:00Z', posted_by = 'direct'
		WHERE id = ?`, mismatched.ID); err == nil {
		t.Fatal("direct reversal posting with mismatched tax metadata unexpectedly succeeded")
	}
	if err := f.service.AbandonJournal(ctx, mismatched.ID); err != nil {
		t.Fatalf("abandon mismatched reversal: %v", err)
	}

	predatedOriginal := postOriginal("Predated trigger original")
	predated, err := f.service.CreateJournal(ctx, ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-14", Period: "2026-07", Description: "Predated manual reversal",
		ReversalOfID: predatedOriginal.ID,
		TaxType:      predatedOriginal.TaxType, TaxAccountingPeriod: predatedOriginal.TaxAccountingPeriod,
		Lines: []ledger.JournalLineInput{
			{Account: "1000", CreditCents: 1_000},
			{Account: "4000", DebitCents: 1_000},
		},
	})
	if err != nil {
		t.Fatalf("create predated reversal draft: %v", err)
	}
	_, err = f.service.PostJournal(ctx, predated.ID)
	requireAppError(t, err, "JOURNAL_INVALID", "cannot precede")
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE journal_entries
		SET status = 'POSTED', posted_at = '2026-07-14T00:00:00Z', posted_by = 'direct'
		WHERE id = ?`, predated.ID); err == nil {
		t.Fatal("direct predated reversal posting unexpectedly succeeded")
	}
}

func TestSourceJournalIdempotencyAndConflict(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	input := ledger.CreateJournalInput{
		Book: "ACME", PostingDate: "2026-07-15", Period: "2026-07", Description: "Imported sale",
		SourceSystem: "QBO", SourceKey: "invoice:123",
		Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 1000}, {Account: "4000", CreditCents: 1000}},
	}
	first, err := f.service.CreateJournal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.CreateJournal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent source returned %s, want %s", second.ID, first.ID)
	}
	input.Lines[0].DebitCents++
	if _, err := f.service.CreateJournal(ctx, input); err == nil {
		t.Fatal("changed source payload unexpectedly succeeded")
	}
}

func TestBackupAndRestoreRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	journal := f.createJournal(t, 12345, 12345)
	if _, err := f.service.PostJournal(ctx, journal.ID); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "books.backup")
	result, err := storesqlite.Backup(ctx, f.store, backupPath, "test")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.SHA256 == "" {
		t.Fatal("backup did not return a hash")
	}
	backup, err := storesqlite.Open(ctx, backupPath, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Doctor(ctx); err != nil {
		_ = backup.Close()
		t.Fatalf("backup doctor: %v", err)
	}
	_ = backup.Close()
	restoredPath := filepath.Join(t.TempDir(), "restored.sqlite")
	if _, err := storesqlite.Restore(ctx, restoredPath, backupPath, "test", storesqlite.RestoreExpectation{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := storesqlite.Open(ctx, restoredPath, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(restored)
	if _, err := restored.Doctor(ctx); err != nil {
		t.Fatalf("restored doctor: %v", err)
	}
	var count int
	if err := restored.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journal_entries WHERE status = 'POSTED'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("restored posted journals = %d, want 1", count)
	}
}

func TestRestoreRejectsDifferentDatabaseLineageBeforeMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := newFixture(t)
	source := newFixture(t)
	backupPath := filepath.Join(t.TempDir(), "unrelated.sqlite")
	if _, err := storesqlite.Backup(ctx, source.store, backupPath, "test"); err != nil {
		t.Fatal(err)
	}
	var targetUUID string
	var auditEvents int
	if err := target.store.DB().QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&targetUUID); err != nil {
		t.Fatal(err)
	}
	if err := target.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&auditEvents); err != nil {
		t.Fatal(err)
	}
	if _, err := storesqlite.ValidateRestore(ctx, target.path, backupPath, storesqlite.RestoreExpectation{}); err == nil {
		t.Fatal("unrelated restore source passed dry-run validation")
	} else {
		requireAppError(t, err, "RESTORE_DATABASE_MISMATCH", "lineage")
	}
	if _, err := storesqlite.Restore(ctx, target.path, backupPath, "test", storesqlite.RestoreExpectation{}); err == nil {
		t.Fatal("unrelated restore source replaced the target")
	} else {
		requireAppError(t, err, "RESTORE_DATABASE_MISMATCH", "lineage")
	}
	missingTarget := filepath.Join(t.TempDir(), "missing-target.sqlite")
	if _, err := storesqlite.Restore(ctx, missingTarget, backupPath, "test", storesqlite.RestoreExpectation{
		DatabaseUUID: targetUUID,
		EntityCode:   "ACME",
		BookCode:     "ACME",
	}); err == nil {
		t.Fatal("unrelated restore source was adopted at a missing registered target")
	} else {
		requireAppError(t, err, "RESTORE_DATABASE_MISMATCH", "registered company")
	}
	if _, err := os.Stat(missingTarget); !os.IsNotExist(err) {
		t.Fatalf("rejected registered restore created missing target: %v", err)
	}
	var currentUUID string
	var currentAuditEvents int
	if err := target.store.DB().QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&currentUUID); err != nil {
		t.Fatal(err)
	}
	if err := target.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&currentAuditEvents); err != nil {
		t.Fatal(err)
	}
	if currentUUID != targetUUID || currentAuditEvents != auditEvents {
		t.Fatalf("rejected restore mutated target: uuid %q/%q audit events %d/%d", currentUUID, targetUUID, currentAuditEvents, auditEvents)
	}
	preRestoreBackups, err := filepath.Glob(target.path + ".pre-restore-*.backup")
	if err != nil {
		t.Fatal(err)
	}
	if len(preRestoreBackups) != 0 {
		t.Fatalf("lineage rejection created pre-restore backups: %v", preRestoreBackups)
	}
}

func TestRestoreRecordsImmutableSourceIdentityEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	var databaseUUID string
	if err := f.store.DB().QueryRowContext(ctx, `SELECT database_uuid FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "same-lineage.sqlite")
	backup, err := storesqlite.Backup(ctx, f.store, backupPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	validation, err := storesqlite.ValidateRestore(ctx, f.path, backupPath, storesqlite.RestoreExpectation{})
	if err != nil {
		t.Fatal(err)
	}
	if validation.SourceSHA256 != backup.SHA256 || validation.SourceDatabaseUUID != databaseUUID || validation.PreviousTargetDatabaseUUID != databaseUUID {
		t.Fatalf("restore validation identity = %+v, backup = %+v", validation, backup)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := storesqlite.Restore(ctx, f.path, backupPath, "test", storesqlite.RestoreExpectation{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceSHA256 != backup.SHA256 || result.SourceDatabaseUUID != databaseUUID || result.PreviousTargetDatabaseUUID != databaseUUID {
		t.Fatalf("restore result identity = %+v, backup = %+v", result, backup)
	}
	if len(result.SourceEntities) != 1 || result.SourceEntities[0].Code != "ACME" || len(result.SourceBooks) != 1 || result.SourceBooks[0].Code != "ACME" {
		t.Fatalf("restore result legal identity = %+v", result)
	}
	if _, err := os.Stat(result.PreRestoreBackup); err != nil {
		t.Fatalf("pre-restore backup: %v", err)
	}
	restored, err := storesqlite.Open(ctx, f.path, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(restored)
	var payloadJSON string
	if err := restored.DB().QueryRowContext(ctx, `SELECT payload_json FROM audit_events
		WHERE command = 'db restore' ORDER BY sequence DESC LIMIT 1`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["source_sha256"] != backup.SHA256 || payload["source_database_uuid"] != databaseUUID || payload["previous_target_database_uuid"] != databaseUUID {
		t.Fatalf("restore audit identity evidence = %#v", payload)
	}
	if entities, ok := payload["source_entities"].([]any); !ok || len(entities) != 1 {
		t.Fatalf("restore audit entity evidence = %#v", payload["source_entities"])
	}
	if books, ok := payload["source_books"].([]any); !ok || len(books) != 1 {
		t.Fatalf("restore audit book evidence = %#v", payload["source_books"])
	}
	if _, err := restored.Doctor(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBackupPublicationNeverReplacesExistingDirectoryEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	directory := t.TempDir()

	destination := filepath.Join(directory, "dangling-destination.backup")
	danglingTarget := filepath.Join(directory, "must-not-be-created")
	if err := os.Symlink(danglingTarget, destination); err != nil {
		t.Fatal(err)
	}
	_, err := storesqlite.Backup(ctx, f.store, destination, "test")
	requireAppError(t, err, "BACKUP_EXISTS", "already exists")
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("destination symlink was replaced: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(danglingTarget); !os.IsNotExist(err) {
		t.Fatalf("backup followed dangling destination symlink: %v", err)
	}

	secondDestination := filepath.Join(directory, "exclusive-temp.backup")
	temporary := secondDestination + ".tmp"
	secondDanglingTarget := filepath.Join(directory, "temp-target-must-not-be-created")
	if err := os.Symlink(secondDanglingTarget, temporary); err != nil {
		t.Fatal(err)
	}
	_, err = storesqlite.Backup(ctx, f.store, secondDestination, "test")
	requireAppError(t, err, "BACKUP_TEMP_EXISTS", "temporary file already exists")
	if info, err := os.Lstat(temporary); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("temporary symlink was replaced: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(secondDanglingTarget); !os.IsNotExist(err) {
		t.Fatalf("backup followed dangling temporary symlink: %v", err)
	}

	var backupRecords int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM backup_records").Scan(&backupRecords); err != nil {
		t.Fatal(err)
	}
	if backupRecords != 0 {
		t.Fatalf("failed no-clobber backups recorded %d successful backups", backupRecords)
	}
}

func TestBackupFailureRemovesPublishedDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'audit_events'`, int64(9223372036854775807)); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "failed.backup")
	if _, err := storesqlite.Backup(ctx, f.store, destination, "test"); err == nil {
		t.Fatal("backup with rejected metadata unexpectedly succeeded")
	}
	for _, path := range []string{destination, destination + ".tmp"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed backup left %s behind: %v", path, err)
		}
	}
	var records int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM backup_records").Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("failed backup recorded %d metadata rows, want 0", records)
	}
}

func TestRestoreFailureRollsBackFilesystemSwap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	malicious := newFixture(t)
	var maliciousDatabaseID string
	if err := malicious.store.DB().QueryRowContext(ctx, "SELECT database_uuid FROM database_metadata WHERE singleton = 1").Scan(&maliciousDatabaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := malicious.store.DB().ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'audit_events'`, int64(9223372036854775807)); err != nil {
		t.Fatal(err)
	}
	if err := malicious.store.Close(); err != nil {
		t.Fatal(err)
	}

	t.Run("new target is removed", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "restored.sqlite")
		if _, err := storesqlite.Restore(ctx, target, malicious.path, "test", storesqlite.RestoreExpectation{}); err == nil {
			t.Fatal("restore with rejected audit unexpectedly succeeded")
		}
		for _, path := range []string{target, target + ".restore.tmp", target + ".replaced.tmp"} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed restore left %s behind: %v", path, err)
			}
		}
	})

	t.Run("existing target is restored", func(t *testing.T) {
		target := newFixture(t)
		var databaseID string
		if err := target.store.DB().QueryRowContext(ctx, "SELECT database_uuid FROM database_metadata WHERE singleton = 1").Scan(&databaseID); err != nil {
			t.Fatal(err)
		}
		if err := target.store.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := storesqlite.Restore(ctx, target.path, malicious.path, "test", storesqlite.RestoreExpectation{
			DatabaseUUID: maliciousDatabaseID,
			EntityCode:   "ACME",
			BookCode:     "ACME",
		}); err == nil {
			t.Fatal("restore with rejected audit unexpectedly succeeded")
		}
		for _, path := range []string{target.path + ".restore.tmp", target.path + ".replaced.tmp"} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed restore left %s behind: %v", path, err)
			}
		}
		reopened, err := storesqlite.Open(ctx, target.path, storesqlite.ReadOnly)
		if err != nil {
			t.Fatal(err)
		}
		defer func(closer interface{ Close() error }) { _ = closer.Close() }(reopened)
		var restoredID string
		if err := reopened.DB().QueryRowContext(ctx, "SELECT database_uuid FROM database_metadata WHERE singleton = 1").Scan(&restoredID); err != nil {
			t.Fatal(err)
		}
		if restoredID != databaseID {
			t.Fatalf("failed restore left database %s live, want original %s", restoredID, databaseID)
		}
	})
}

func TestJournalBatchImportAndAtomicPost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	input := ledger.JournalImportInput{
		SourceSystem: "QBO", SourceName: "acme-july.json", FileSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Entity: "ACME",
		Records: []ledger.JournalImportRecord{
			{Journal: ledger.CreateJournalInput{Book: "ACME", PostingDate: "2026-07-10", Period: "2026-07", Description: "Imported one", SourceSystem: "QBO", SourceKey: "1", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 100}, {Account: "4000", CreditCents: 100}}}, RawJSON: json.RawMessage(`{"id":"1"}`)},
			{Journal: ledger.CreateJournalInput{Book: "ACME", PostingDate: "2026-07-11", Period: "2026-07", Description: "Imported two", SourceSystem: "QBO", SourceKey: "2", Lines: []ledger.JournalLineInput{{Account: "1000", DebitCents: 200}, {Account: "4000", CreditCents: 200}}}, RawJSON: json.RawMessage(`{"id":"2"}`)},
		},
	}
	result, err := f.service.ImportJournals(ctx, input)
	if err != nil {
		t.Fatalf("import journals: %v", err)
	}
	if result.CreatedCount != 2 || len(result.JournalIDs) != 2 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	duplicate, err := f.service.ImportJournals(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Changed || duplicate.BatchID != result.BatchID {
		t.Fatalf("exact reimport was not idempotent: %+v", duplicate)
	}
	dryRun, err := f.service.PostImportBatch(ctx, result.BatchID, true)
	if err != nil || dryRun.PostedCount != 2 || !dryRun.Changed {
		t.Fatalf("post batch dry-run: %+v, %v", dryRun, err)
	}
	posted, err := f.service.PostImportBatch(ctx, result.BatchID, false)
	if err != nil {
		t.Fatal(err)
	}
	if posted.PostedCount != 2 || !posted.Changed {
		t.Fatalf("unexpected post result: %+v", posted)
	}
	var postedCount, sourceCount int
	if err := f.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM journal_entries WHERE status = 'POSTED'").Scan(&postedCount); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM source_records sr
        JOIN source_record_journals srj ON srj.source_record_id = sr.id
        WHERE sr.import_batch_id = ? AND srj.link_role = 'PRIMARY'`, result.BatchID).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if postedCount != 2 || sourceCount != 2 {
		t.Fatalf("posted=%d source=%d, want 2/2", postedCount, sourceCount)
	}
	if _, err := f.store.Doctor(ctx); err != nil {
		t.Fatal(err)
	}
}
