package operations

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"path/filepath"
	"testing"
)

func TestUnsupportedReopenPreviewDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("BOOKS_HOME", home)
	t.Setenv("BOOKS_CONFIG", filepath.Join(home, "books.toml"))
	t.Setenv("BOOKS_DB", "")
	t.Setenv("BOOKS_ACTOR", "preview-test")
	store, err := storesqlite.Init(ctx, filepath.Join(home, "books.sqlite"), "USD", "preview-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	db, err := application.BindDatabase(ctx, "example", store, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for id, input := range map[string]any{"period_reopen": &PeriodRequest{DryRun: true}, "reconcile_reopen": &IDModeRequest{DryRun: true}} {
		op, _ := LookupDatabaseOperation(id)
		_, err := op.Execute(ctx, db, ScopedDatabaseAccess("preview-test", "example", []string{"read", "manage"}), input)
		e, ok := apperr.As(err)
		if !ok || e.Code != "DRY_RUN_UNSUPPORTED" {
			t.Fatal(id, err)
		}
	}
	after, err := store.Doctor(ctx)
	if err != nil || after.AuditEvents != before.AuditEvents {
		t.Fatal("preview changed audit", err)
	}
}
