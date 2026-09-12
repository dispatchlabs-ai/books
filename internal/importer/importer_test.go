package importer

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseCentsExact(t *testing.T) {
	t.Parallel()
	tests := map[string]int64{
		"0": 0, ".40": 40, "-.58": -58, "12.3": 1230,
		"175.0": 17500, "10000.00": 1000000, "1.2300": 123,
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := parseCents(input)
			if err != nil {
				t.Fatalf("parseCents(%q): %v", input, err)
			}
			if got != want {
				t.Fatalf("parseCents(%q) = %d, want %d", input, got, want)
			}
		})
	}
	for _, input := range []string{"", "1.001", "1e2", "$1.00", "1,000.00"} {
		if _, err := parseCents(input); err == nil {
			t.Fatalf("parseCents(%q) unexpectedly succeeded", input)
		}
	}
}

func TestGeneralLedgerUsesMetadataAndAncestorAccount(t *testing.T) {
	t.Parallel()
	request := Request{Entities: []EntityRequest{{
		EntityCode: "TEST", BookCode: "TEST", Currency: "USD",
		StartDate: "2023-01-01", CutoffDate: "2023-12-31",
		AccountCatalogPath: "testdata/accounts.json",
		Sources:            []Source{{Kind: SourceGeneralLedger, Path: "testdata/general_ledger.json", StartDate: "2023-01-01", EndDate: "2023-12-31"}},
	}}}
	plan, err := Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrorDiagnostics(t, plan.Diagnostics)
	if got := len(plan.Entities[0].Journals); got != 1 {
		t.Fatalf("journal count = %d, want 1; diagnostics=%+v", got, plan.Diagnostics)
	}
	journal := plan.Entities[0].Journals[0]
	if journal.Input.SourceKey != "transaction:T100" {
		t.Fatalf("source key = %q", journal.Input.SourceKey)
	}
	if got, want := journal.Input.Lines[0].CreditCents+journal.Input.Lines[1].CreditCents, int64(1234); got != want {
		t.Fatalf("credits = %d, want %d", got, want)
	}
	if got, want := journal.Input.Lines[0].DebitCents+journal.Input.Lines[1].DebitCents, int64(1234); got != want {
		t.Fatalf("debits = %d, want %d", got, want)
	}
}

func TestSourcePlanningIsIdempotent(t *testing.T) {
	t.Parallel()
	request := Request{Entities: []EntityRequest{{
		EntityCode: "TEST", BookCode: "TEST", Currency: "USD",
		StartDate: "2023-01-01", CutoffDate: "2023-12-31",
		AccountCatalogPath: "testdata/accounts.json",
		Sources: []Source{
			{Kind: SourceGeneralLedger, Path: "testdata/general_ledger.json", StartDate: "2023-01-01", EndDate: "2023-12-31"},
			{Kind: SourceGeneralLedger, Path: "testdata/general_ledger.json", StartDate: "2023-01-01", EndDate: "2023-12-31"},
		},
	}}}
	first, err := Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated planning was not deterministic")
	}
	if len(first.Entities[0].Journals) != 1 {
		t.Fatalf("duplicate source produced %d journals", len(first.Entities[0].Journals))
	}
	found := false
	for _, diagnostic := range first.Diagnostics {
		found = found || diagnostic.Code == "SOURCE_DUPLICATE_IGNORED"
	}
	if !found {
		t.Fatal("duplicate source did not produce an idempotency diagnostic")
	}
}

func TestJournalXLSXExcludesBlankAccountSubtotal(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "Journal.xlsx")
	writeTestWorkbook(t, path)
	request := Request{Entities: []EntityRequest{{
		EntityCode: "TEST", BookCode: "TEST", Currency: "USD",
		StartDate: "2021-01-01", CutoffDate: "2022-12-31",
		AccountCatalogPath: "testdata/accounts.json",
		Sources:            []Source{{Kind: SourceJournalXLSX, Path: path, StartDate: "2021-01-01", EndDate: "2022-12-31"}},
	}}}
	plan, err := Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrorDiagnostics(t, plan.Diagnostics)
	if len(plan.Entities[0].Journals) != 1 {
		t.Fatalf("journal count = %d; diagnostics=%+v", len(plan.Entities[0].Journals), plan.Diagnostics)
	}
	lines := plan.Entities[0].Journals[0].Input.Lines
	if len(lines) != 2 {
		t.Fatalf("posting line count = %d, subtotal became a posting", len(lines))
	}
	if lines[0].DebitCents+lines[1].DebitCents != 1234 || lines[0].CreditCents+lines[1].CreditCents != 1234 {
		t.Fatalf("unexpected exact cents: %+v", lines)
	}
}

func TestQBOObjectPurchaseRule(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	accountData, err := os.ReadFile("testdata/accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Account.json"), accountData, 0o600); err != nil {
		t.Fatal(err)
	}
	purchase := `{"entity":"Purchase","rows":[{"Id":"P1","TxnDate":"2026-01-05","CurrencyRef":{"value":"USD"},"AccountRef":{"value":"A1"},"Credit":false,"PaymentType":"Cash","TotalAmt":12.34,"Line":[{"DetailType":"AccountBasedExpenseLineDetail","Amount":12.34,"Description":"Service","AccountBasedExpenseLineDetail":{"AccountRef":{"value":"E1"}}}]}]}`
	if err := os.WriteFile(filepath.Join(directory, "Purchase.json"), []byte(purchase), 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{Entities: []EntityRequest{{
		EntityCode: "TEST", BookCode: "TEST", Currency: "USD",
		StartDate: "2026-01-01", CutoffDate: "2026-07-31",
		AccountCatalogPath: filepath.Join(directory, "Account.json"),
		Sources:            []Source{{Kind: SourceQBOObjectDir, Path: directory, StartDate: "2026-01-01", EndDate: "2026-07-31"}},
	}}}
	plan, err := Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrorDiagnostics(t, plan.Diagnostics)
	journal := plan.Entities[0].Journals[0].Input
	if journal.SourceKey != "transaction:P1" || len(journal.Lines) != 2 {
		t.Fatalf("unexpected purchase journal: %+v", journal)
	}
	if journal.Lines[0].DebitCents != 1234 || journal.Lines[1].CreditCents != 1234 {
		t.Fatalf("unexpected purchase directions: %+v", journal.Lines)
	}
}

func TestAccountNumberSemanticConflictIsPrefixed(t *testing.T) {
	t.Parallel()
	first := &accountRecord{key: "A", entityCode: "NORTHSTAR", externalID: "1", accountNum: "10000", name: "Operating Cash", accountType: "ASSET", subtype: "CHECKING", normalBalance: "DEBIT", statementSection: "BALANCE_SHEET", active: true}
	second := &accountRecord{key: "B", entityCode: "ACME", externalID: "2", accountNum: "10000", name: "Customer Trust Cash", accountType: "ASSET", subtype: "CHECKING", normalBalance: "DEBIT", statementSection: "BALANCE_SHEET", active: true}
	states := []*entityState{
		{request: EntityRequest{EntityCode: "NORTHSTAR", BookCode: "NORTHSTAR"}, catalog: catalogFor(first), journals: []rawJournal{{postingDate: "2023-01-01", lines: []rawLine{{accountKey: "A", debitCents: 1}}}}},
		{request: EntityRequest{EntityCode: "ACME", BookCode: "ACME"}, catalog: catalogFor(second), journals: []rawJournal{{postingDate: "2023-01-01", lines: []rawLine{{accountKey: "B", debitCents: 1}}}}},
	}
	accounts, codes, diagnostics, err := reconcileAccounts(states)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || codes["A"] != "NORTHSTAR-10000" || codes["B"] != "ACME-10000" {
		t.Fatalf("conflicting accounts were silently merged: accounts=%+v codes=%+v", accounts, codes)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "ACCOUNT_NUMBER_CONFLICT" {
		t.Fatalf("conflict diagnostic missing: %+v", diagnostics)
	}
}

func TestImportFileRejectsOversizedJSONBeforeReading(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxJSONImportBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readImportFile(path, maxJSONImportBytes, "test JSON"); err == nil || !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("oversized JSON error = %v", err)
	}
}

func TestWorkbookRejectsOversizedExpandedMember(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "oversized.xlsx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	member, err := archive.Create("xl/sharedStrings.xml")
	if err != nil {
		t.Fatal(err)
	}
	chunk := []byte(strings.Repeat("x", 1<<20))
	remaining := int64(maxXLSXMemberBytes + 1)
	for remaining > 0 {
		write := int64(len(chunk))
		if write > remaining {
			write = remaining
		}
		if _, err := member.Write(chunk[:write]); err != nil {
			t.Fatal(err)
		}
		remaining -= write
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(reader)
	if err := validateXLSXArchive(reader.File); err == nil || !strings.Contains(err.Error(), "expanded member limit") {
		t.Fatalf("oversized workbook error = %v", err)
	}
}

func TestWorkbookRejectsMemberFlood(t *testing.T) {
	t.Parallel()
	files := make([]*zip.File, maxXLSXMembers+1)
	for index := range files {
		files[index] = &zip.File{FileHeader: zip.FileHeader{Name: "member"}}
	}
	if err := validateXLSXArchive(files); err == nil || !strings.Contains(err.Error(), "ZIP members") {
		t.Fatalf("member flood error = %v", err)
	}
}

func catalogFor(records ...*accountRecord) *accountCatalog {
	catalog := &accountCatalog{byID: map[string]*accountRecord{}, byInternalKey: map[string]*accountRecord{}, byNumber: map[string][]*accountRecord{}, byName: map[string][]*accountRecord{}}
	for _, record := range records {
		catalog.add(record)
	}
	return catalog
}

func assertNoErrorDiagnostics(t *testing.T, diagnostics []Diagnostic) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			t.Fatalf("unexpected error diagnostic: %+v", diagnostic)
		}
	}
}

func writeTestWorkbook(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	shared := []string{"Date", "Transaction Type", "Num", "Name", "Memo/Description", "Account", "Debit", "Credit", "01/02/2022", "Expense", "Vendor", "Service", "65010 Software Services", "10000 Operating Checking"}
	var sharedXML strings.Builder
	sharedXML.WriteString(`<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, value := range shared {
		sharedXML.WriteString("<si><t>")
		sharedXML.WriteString(value)
		sharedXML.WriteString("</t></si>")
	}
	sharedXML.WriteString("</sst>")
	sheet := `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="B1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c><c r="D1" t="s"><v>2</v></c><c r="E1" t="s"><v>3</v></c><c r="F1" t="s"><v>4</v></c><c r="G1" t="s"><v>5</v></c><c r="H1" t="s"><v>6</v></c><c r="I1" t="s"><v>7</v></c></row>
<row r="2"><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="E2" t="s"><v>10</v></c><c r="F2" t="s"><v>11</v></c><c r="G2" t="s"><v>12</v></c><c r="H2" t="n"><v>12.34</v></c></row>
<row r="3"><c r="G3" t="s"><v>13</v></c><c r="I3" t="n"><v>12.34</v></c></row>
<row r="4"><c r="H4" t="n"><v>12.34</v></c><c r="I4" t="n"><v>12.34</v></c></row>
</sheetData></worksheet>`
	for name, content := range map[string]string{"xl/sharedStrings.xml": sharedXML.String(), "xl/worksheets/sheet1.xml": sheet} {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
