package banking

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestStatementRegressionXLSXNumberLocale(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Statement" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Date</t></is></c><c r="B1" t="inlineStr"><is><t>Amount</t></is></c><c r="C1" t="inlineStr"><is><t>Description</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>2026-07-01</t></is></c><c r="B2" t="n"><v>1.000</v></c><c r="C2" t="inlineStr"><is><t>Synthetic sale</t></is></c></row></sheetData></worksheet>`,
	}
	for name, data := range files {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(data))
	}
	_ = z.Close()
	d, e := ParseFile(b.Bytes(), Options{Format: "XLSX", Institution: "FAKE", AccountID: "FAKE", Currency: "USD", DateLayout: "2006-01-02", Tabular: &TabularProfile{HeaderRow: 1, DateColumn: "Date", AmountColumn: "Amount", DescriptionColumns: []string{"Description"}, DecimalSeparator: ",", ThousandsSeparator: "."}})
	if e != nil {
		t.Fatal(e)
	}
	if d.Accounts[0].Transactions[0].Amount != "1.00" {
		t.Fatalf("numeric 1.000 misread as %s", d.Accounts[0].Transactions[0].Amount)
	}
}

const regressionMT = `:20:FAKE-REF
:25:FAKE-ACCT
:28C:001/1
:60F:C260701USD0,
:61:2607010701C1,NTRFNONREF
:62F:C260701USD1,
`

func TestStatementRegressionMTDuplicateStatement(t *testing.T) {
	p := 70
	d, e := ParseFile([]byte(regressionMT+regressionMT), Options{Format: "MT940", Institution: "FAKE", YearPivot: &p})
	if e == nil {
		t.Fatalf("duplicate statement accepted: tx=%d ids=%s,%s", len(d.Accounts[0].Transactions), d.Accounts[0].Transactions[0].ID, d.Accounts[0].Transactions[1].ID)
	}
}
func TestStatementRegressionMTLaterMissingFirstPage(t *testing.T) {
	p := 70
	second := strings.Replace(strings.Replace(regressionMT, "001/1", "002/2", 1), "FAKE-REF", "FAKE-REF2", 1)
	d, e := ParseFile([]byte(regressionMT+second), Options{Format: "MT940", Institution: "FAKE", YearPivot: &p})
	if e == nil {
		t.Fatalf("statement starts on page2 accepted: %+v", d.Accounts[0].Statements)
	}
}

func TestStatementRegressionCODAInformationLinks(t *testing.T) {
	for _, bad := range []string{"skipped32", "wrong-reference"} {
		t.Run(bad, func(t *testing.T) {
			lines := strings.Split(strings.TrimSuffix(codaFixture(), "\n"), "\n")
			movement := []byte(lines[2])
			movement[127] = '1'
			lines[2] = string(movement)
			ref := "FAKE-REFERENCE"
			next := "0"
			count := "000004"
			if bad == "wrong-reference" {
				ref = "OTHER-REFERENCE"
			} else {
				next = "1"
				count = "000005"
			}
			info := fixedFixture(128, map[int]string{1: "3100010000", 11: ref, 32: "00150000", 40: "0", 41: "Synthetic info", 126: next, 128: "0"})
			records := append([]string{}, lines[:3]...)
			records = append(records, info)
			if bad == "skipped32" {
				records = append(records, fixedFixture(128, map[int]string{1: "3300010000", 11: "Continuation", 126: "0", 128: "0"}))
			}
			trailer := []byte(lines[4])
			copy(trailer[16:22], count)
			records = append(records, lines[3], string(trailer))
			d, e := ParseFile([]byte(strings.Join(records, "\n")+"\n"), testOptions())
			if e == nil {
				t.Fatalf("invalid CODA information chain accepted: %+v", d.Accounts[0].Transactions[0].Fields)
			}
		})
	}
}

func TestStatementRegressionCAMTCurrencyNamespace(t *testing.T) {
	input := strings.Replace(camtFixture("053", "08"), `<Ntry><Amt Ccy="USD">`, `<Ntry><Amt xmlns:evil="urn:fake" evil:Ccy="USD" Ccy="EUR">`, 1)
	d, e := ParseFile([]byte(input), Options{Format: "CAMT"})
	if e == nil {
		t.Fatalf("foreign currency spoof accepted: account=%s amount=%s actual=%s", d.Accounts[0].Currency, d.Accounts[0].Transactions[0].Amount, d.Accounts[0].Transactions[0].Fields["Ntry/Amt/@Ccy"])
	}
}
func TestStatementRegressionBlankDebitCredit(t *testing.T) {
	o := testOptions()
	o.Format = "CSV"
	o.Tabular = &TabularProfile{HeaderRow: 1, DateColumn: "Date", DebitColumn: "Debit", CreditColumn: "Credit", DescriptionColumns: []string{"Description"}, DecimalSeparator: "."}
	d, e := ParseFile([]byte("Date,Debit,Credit,Description\n2026-07-15,,,Missing amount\n"), o)
	if e == nil {
		t.Fatalf("missing amount becomes %s", d.Accounts[0].Transactions[0].Amount)
	}
}

func TestStatementRegressionCODANestedTotal(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(codaFixture(), "\n"), "\n")
	main := []byte(lines[2])
	main[53] = '2'
	lines[2] = string(main)
	subtotal := append([]byte{}, main...)
	subtotal[53] = '7'
	copy(subtotal[6:10], "0001")
	detail := append([]byte{}, main...)
	detail[53] = '9'
	copy(detail[6:10], "0002")
	copy(detail[32:47], "000000000999990")
	trailer := []byte(lines[4])
	copy(trailer[16:22], "000005")
	records := append([]string{}, lines[:3]...)
	records = append(records, string(subtotal), string(detail), lines[3], string(trailer))
	d, e := ParseFile([]byte(strings.Join(records, "\n")+"\n"), testOptions())
	if e == nil {
		t.Fatalf("nested detail999.99 accepted under subtotal%s", d.Accounts[0].Transactions[0].Details[0].Amount)
	}
}
