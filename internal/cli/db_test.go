package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func TestDBMigrateDryRunRejectsUnrelatedDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unrelated.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE customer_data (id INTEGER PRIMARY KEY, marker TEXT NOT NULL)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO customer_data(marker) VALUES ('preserve-me')"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	root, _ := newRootCommand()
	root.SetArgs([]string{"--db", path, "--dry-run", "db", "migrate"})
	err = root.Execute()
	if err == nil {
		t.Fatal("dry-run migration of unrelated database unexpectedly succeeded")
	}
	appError, ok := apperr.As(err)
	if !ok || appError.Code != "DATABASE_WRONG_APPLICATION" {
		t.Fatalf("migration error = %v, want DATABASE_WRONG_APPLICATION", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("dry-run migration modified unrelated database")
	}
}
