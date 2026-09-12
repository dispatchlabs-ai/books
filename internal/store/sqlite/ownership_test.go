package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func TestOwnershipHasOneEffectiveOwnerPerChildAndNoCycles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storesqlite.Init(ctx, filepath.Join(t.TempDir(), "books.sqlite"), "USD", "test")
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
	service := ledger.NewService(store, "test")
	for _, input := range []ledger.CreateEntityInput{
		{Code: "PARENT-A", LegalName: "Parent A", Currency: "USD"},
		{Code: "PARENT-B", LegalName: "Parent B", Currency: "USD"},
		{Code: "CHILD", LegalName: "Child", Currency: "USD"},
	} {
		if _, err := service.CreateEntity(ctx, input); err != nil {
			t.Fatalf("create %s: %v", input.Code, err)
		}
	}
	if _, err := service.AddOwnership(ctx, "PARENT-A", "CHILD", "2026-01-01", "2026-06-30"); err != nil {
		t.Fatalf("record first owner: %v", err)
	}
	if _, err := service.AddOwnership(ctx, "PARENT-B", "CHILD", "2026-06-30", ""); err == nil {
		t.Fatal("overlapping ownership by a different parent unexpectedly succeeded")
	}
	if _, err := service.AddOwnership(ctx, "PARENT-B", "CHILD", "2026-07-01", ""); err != nil {
		t.Fatalf("record non-overlapping successor owner: %v", err)
	}
	if _, err := service.AddOwnership(ctx, "CHILD", "PARENT-A", "2027-01-01", ""); err == nil {
		t.Fatal("ownership cycle unexpectedly succeeded")
	}

	var groupMemberTables int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'group_members'`).Scan(&groupMemberTables); err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if groupMemberTables != 0 {
		t.Fatal("obsolete group_members table still exists")
	}
}
