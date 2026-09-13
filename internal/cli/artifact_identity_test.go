package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactDatabaseIdentityBinding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ctx = artifact.WithRoot(ctx, root)
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	first, err := application.Open(ctx, filepath.Join(home, "books.toml"), "acme", "identity-test", storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	home2 := setupHumanCLIHome(t, "2026-01-01", "starter")
	second, err := application.Open(ctx, filepath.Join(home2, "books.toml"), "acme", "identity-test", storesqlite.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	access := operations.CompanyAccess("identity-test", "acme", []string{"read", "import"})
	begin, _ := operations.LookupCompanyOperation("artifact_begin")
	sum := sha256.Sum256(nil)
	value, err := begin.Invoke(ctx, first, access, &artifact.BeginRequest{Key: "identity", Name: "empty", SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	ref := value.(artifact.Reference)
	finish, _ := operations.LookupCompanyOperation("artifact_finish")
	if _, err = finish.Invoke(ctx, first, access, &operations.IDRequest{ID: ref.ID}); err != nil {
		t.Fatal(err)
	}
	read, _ := operations.LookupCompanyOperation("artifact_read")
	if _, err = read.Invoke(ctx, second, access, &artifact.ReadRequest{ID: ref.ID}); err == nil {
		t.Fatal("reused alias exposed old database artifact")
	}
	if _, err = read.Invoke(ctx, first, access, &artifact.ReadRequest{ID: ref.ID}); err != nil {
		t.Fatal("original identity lost access", err)
	}
	discard, _ := operations.LookupCompanyOperation("artifact_discard")
	if _, err = discard.Invoke(ctx, first, operations.CompanyAccess("identity-test", "acme", []string{"read"}), &operations.IDRequest{ID: ref.ID}); err != nil {
		t.Fatal("read-only owner cannot clean up", err)
	}

}
