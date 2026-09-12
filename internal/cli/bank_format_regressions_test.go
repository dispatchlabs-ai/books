package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBankPriorStdinUploadReplay(t *testing.T) {
	home := setupHumanCLIHome(t, "2026-01-01", "starter")
	path := filepath.Join(home, "synthetic.ofx")
	if e := os.WriteFile(path, []byte(syntheticOFX), 0600); e != nil {
		t.Fatal(e)
	}
	// Reproduce the filename persisted by the original stdin-upload contract.
	executeHumanJSON(t, "bank-import", "upload", "--input", path, "--name", "statement.ofx", "--key", "stable-key")
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; _ = f.Close() })
	executeHumanJSON(t, "bank-import", "upload", "--input", "-", "--key", "stable-key")
}
