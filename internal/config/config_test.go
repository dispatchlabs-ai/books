package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathUsesDotBooksInUserHome(t *testing.T) {
	t.Setenv("BOOKS_HOME", "")
	t.Setenv("BOOKS_CONFIG", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".books", "books.toml")
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestConfigRoundTripAndCompanyResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".books", "books.toml")
	value := New()
	company := NewCompany("acme", "Acme, Inc.", "USD", "ACCRUAL")
	company.DatabaseUUID = "01234567-89ab-4cde-8f01-23456789abcd"
	company.FiscalYearEnd = 6
	value.Companies["acme"] = company
	value.DefaultCompany = "acme"
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.Resolve(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Key != "acme" || resolved.Company.DatabaseUUID != company.DatabaseUUID || resolved.Company.FiscalYearEnd != 6 || resolved.Database != filepath.Join(filepath.Dir(path), "companies", "acme", "ledger.sqlite") {
		t.Fatalf("resolved company = %+v", resolved)
	}
}

func TestConfigRejectsNoncanonicalDatabaseUUID(t *testing.T) {
	value := New()
	company := NewCompany("acme", "Acme, Inc.", "USD", "ACCRUAL")
	company.DatabaseUUID = "NOT-A-UUID"
	value.Companies["acme"] = company
	if err := value.Validate(); err == nil {
		t.Fatal("Validate accepted a noncanonical company database UUID")
	}
}

func TestConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "books.toml")
	data := []byte("version = 1\nunknown = true\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown configuration field")
	}
}

func TestConfigRejectsUnsupportedDefaultOutput(t *testing.T) {
	value := New()
	value.Defaults.Output = "yaml"
	if err := value.Validate(); err == nil {
		t.Fatal("Validate accepted an unsupported output format")
	}
}

func TestSaveDoesNotChangeExistingParentPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "shared-config")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	value := New()
	if err := Save(filepath.Join(directory, "books.toml"), value); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing parent permissions changed to %o", info.Mode().Perm())
	}
}

func TestDerivedCompanyKeysFitLedgerCodes(t *testing.T) {
	tests := map[string]string{
		"X":                   "x-co",
		"Acme Services, Inc.": "acme-services-inc",
		"A Very Long Company Name That Exceeds The Limit": "a-very-long-company-name-that-ex",
	}
	for name, want := range tests {
		key := DeriveCompanyKey(name)
		if key != want {
			t.Fatalf("DeriveCompanyKey(%q) = %q, want %q", name, key, want)
		}
		if err := ValidateCompanyKey(key); err != nil {
			t.Fatalf("derived key %q is invalid: %v", key, err)
		}
	}
}
