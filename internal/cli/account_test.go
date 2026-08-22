package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

func accountIdentityCLIDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "books.sqlite")
	store, err := storesqlite.Init(ctx, path, "USD", "test")
	if err != nil {
		t.Fatal(err)
	}
	service := ledger.NewService(store, "test")
	if _, err := service.CreateEntity(ctx, ledger.CreateEntityInput{Code: "ACME", LegalName: "Acme, Inc.", Currency: "USD"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := service.CreateAccount(ctx, ledger.CreateAccountInput{Code: "1000", Name: "Cash", Type: "ASSET", BookCodes: []string{"ACME"}}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeJSONCommand(t *testing.T, args ...string) map[string]any {
	t.Helper()
	root, _ := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, output.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	return result
}

func TestAccountIdentityAddAndListCLI(t *testing.T) {
	t.Parallel()
	path := accountIdentityCLIDatabase(t)
	digest := strings.Repeat("a", 64)
	added := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json",
		"account", "identity", "add",
		"--entity", "ACME", "--account", "1000", "--source-system", "QBO",
		"--external-id", "283", "--account-number", "10100", "--name", "Sample Bank Operating",
		"--source-kind", "QBO_OBJECT_DIRECTORY", "--source-path", "/evidence/Account.json",
		"--source-sha256", digest, "--locator", "Account.json#/rows/4",
	)
	data := added["data"].(map[string]any)
	if data["entity_code"] != "ACME" || data["external_id"] != "283" || data["active"] != true {
		t.Fatalf("unexpected add output: %#v", data)
	}
	listed := executeJSONCommand(t,
		"--db", path, "--actor", "test", "--format", "json",
		"account", "identity", "list", "--entity", "acme", "--source-system", "qbo",
	)
	rows := listed["data"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["account_code"] != "1000" {
		t.Fatalf("unexpected list output: %#v", rows)
	}
}
