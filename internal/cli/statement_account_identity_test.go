package cli

import (
	"strings"
	"testing"
)

func statementIdentityCLIArgs(path string) []string {
	return []string{
		"--db", path, "--actor", "identity-test", "--format", "json",
		"statement-account", "identity", "add",
		"--statement-account", "acme-cash", "--source-system", "finances",
		"--source-realm", "global",
		"--external-id", "cash-old-id", "--account-number", "0110", "--name", "Example Bank Base",
		"--source-kind", "sqlite_account_registry", "--source-path", "/evidence/finance.sqlite",
		"--source-sha256", strings.Repeat("a", 64), "--locator", "accounts#cash-old-id",
	}
}

func TestStatementAccountIdentityCLIRequiresCommitAndSupportsAliases(t *testing.T) {
	t.Parallel()
	path := accountIdentityCLIDatabase(t)
	executeJSONCommand(t, "--db", path, "--actor", "test", "--format", "json",
		"statement-account", "create", "--code", "acme-cash", "--entity", "acme",
		"--book", "acme", "--account", "1000", "--name", "Acme Cash", "--kind", "BANK",
		"--reconcile-from", "2026-01-01", "--required-for-close=false")

	preview := executeJSONCommand(t, statementIdentityCLIArgs(path)...)
	previewData := preview["data"].(map[string]any)
	if previewData["committed"] != false {
		t.Fatalf("identity preview = %#v", previewData)
	}
	previewInput := previewData["input"].(map[string]any)
	if previewInput["statement_account"] != "ACME-CASH" || previewInput["source_system"] != "FINANCES" || previewInput["source_realm"] != "GLOBAL" {
		t.Fatalf("identity preview was not normalized: %#v", previewInput)
	}
	listed := executeJSONCommand(t, "--db", path, "--format", "json",
		"statement-account", "identity", "list", "--statement-account", "acme-cash")
	if len(listed["data"].([]any)) != 0 {
		t.Fatalf("identity preview wrote rows: %#v", listed)
	}

	commitArgs := append(statementIdentityCLIArgs(path), "--commit")
	added := executeJSONCommand(t, commitArgs...)
	addedData := added["data"].(map[string]any)
	if addedData["statement_account"] != "ACME-CASH" || addedData["source_system"] != "FINANCES" || addedData["source_realm"] != "GLOBAL" ||
		addedData["created_by"] != "identity-test" {
		t.Fatalf("committed identity = %#v", addedData)
	}

	aliasArgs := statementIdentityCLIArgs(path)
	for index := range aliasArgs {
		switch aliasArgs[index] {
		case "finances":
			aliasArgs[index] = "provider"
		case "cash-old-id":
			aliasArgs[index] = "provider-account-0110"
		case "accounts#cash-old-id":
			aliasArgs[index] = "accounts/provider-account-0110"
		}
	}
	aliasArgs = append(aliasArgs, "--commit")
	executeJSONCommand(t, aliasArgs...)
	listed = executeJSONCommand(t, "--db", path, "--format", "json",
		"statement-account", "identity", "list", "--entity", "acme", "--source-realm", "global")
	identities := listed["data"].([]any)
	if len(identities) != 2 {
		t.Fatalf("statement account aliases = %#v", identities)
	}
}

func TestStatementAccountIdentityCLIRequiresEvidenceFlags(t *testing.T) {
	t.Parallel()
	root, _ := newRootCommand()
	root.SetArgs([]string{
		"statement-account", "identity", "add", "--statement-account", "CASH",
		"--source-system", "BANK", "--source-realm", "GLOBAL", "--external-id", "1", "--name", "Cash",
		"--source-kind", "CSV", "--source-path", "statement.csv", "--locator", "row=1",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "source-sha256") {
		t.Fatalf("missing evidence SHA flag error = %v", err)
	}
}

func TestStatementAccountIdentityCLIRequiresSourceRealm(t *testing.T) {
	t.Parallel()
	root, _ := newRootCommand()
	root.SetArgs([]string{
		"statement-account", "identity", "add", "--statement-account", "CASH",
		"--source-system", "QBO", "--external-id", "1", "--name", "Cash",
		"--source-kind", "JSON", "--source-path", "Account.json",
		"--source-sha256", strings.Repeat("a", 64), "--locator", "Account.json#1",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "source-realm") {
		t.Fatalf("missing source realm flag error = %v", err)
	}
}
