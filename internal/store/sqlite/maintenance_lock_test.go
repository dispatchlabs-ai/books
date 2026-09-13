package sqlite_test

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceRejectsLiveConnections(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "maintenance-test")
	if err != nil {
		t.Fatal(err)
	}
	err = storesqlite.Migrate(ctx, path)
	e, ok := apperr.As(err)
	if !ok || e.Code != "DATABASE_BUSY" {
		_ = store.Close()
		t.Fatal("live connection was not protected", err)
	}
	alias := filepath.Join(root, "alias.sqlite")
	if err = os.Symlink(path, alias); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	err = storesqlite.WithMaintenance(ctx, alias, func(context.Context) error { t.Fatal("alias bypassed connection lock"); return nil })
	e, ok = apperr.As(err)
	if !ok || e.Code != "DATABASE_BUSY" {
		_ = store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = storesqlite.Migrate(ctx, path); err != nil {
		t.Fatal("maintenance after close", err)
	}
	if err = storesqlite.WithMaintenance(ctx, path, func(inner context.Context) error {
		if other, err := storesqlite.Open(ctx, path, storesqlite.ReadOnly); err == nil {
			_ = other.Close()
			t.Fatal("new connection bypassed maintenance")
		}
		owned, err := storesqlite.Open(inner, path, storesqlite.ReadOnly)
		if err != nil {
			return err
		}
		return owned.Close()
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceReadOnlyAndNewRestoreDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "source.sqlite")
	s, err := storesqlite.Init(ctx, path, "USD", "maintenance-test")
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "snapshot.backup")
	if _, err = storesqlite.Backup(ctx, s, backup, "maintenance-test"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	readonly := filepath.Join(root, "readonly")
	if err = os.Mkdir(readonly, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(readonly, "copy.backup")
	if err = os.WriteFile(copyPath, data, 0400); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(readonly, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0700) })
	read, err := storesqlite.Open(ctx, copyPath, storesqlite.ReadOnly)
	if err != nil {
		t.Fatal("read-only directory", err)
	}
	if _, err = read.Doctor(ctx); err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	entries, err := os.ReadDir(readonly)
	if err != nil || len(entries) != 1 {
		t.Fatal("read-only inspection wrote adjacent files", err, entries)
	}
	target := filepath.Join(root, "new", "nested", "restored.sqlite")
	if _, err = storesqlite.Restore(ctx, target, backup, "maintenance-test", storesqlite.RestoreExpectation{}); err != nil {
		t.Fatal("restore into missing directory tree", err)
	}
}
