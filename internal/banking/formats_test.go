package banking

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func testOptions() Options {
	pivot := 70
	return Options{Institution: "FAKE-BANK", AccountID: "FAKE-ACCOUNT", Currency: "USD", DateLayout: "2006-01-02", YearPivot: &pivot}
}
func testTableProfile() *TabularProfile {
	return &TabularProfile{HeaderRow: 1, DateColumn: "Date", AmountColumn: "Amount", DescriptionColumns: []string{"Description"}, DecimalSeparator: "."}
}
func fixedFixture(width int, fields map[int]string) string {
	b := bytes.Repeat([]byte{' '}, width)
	for start, v := range fields {
		if start < 1 || start+len(v)-1 > width {
			panic("invalid synthetic fixture position")
		}
		copy(b[start-1:], v)
	}
	return string(b)
}
func cfonbFixture() string {
	base := func(code, date, amount string) map[int]string {
		return map[int]string{1: code, 3: "99999", 12: "99999", 17: "USD", 20: "2", 22: "99999999999", 35: date, 91: amount}
	}
	open := fixedFixture(120, base("01", "010726", "0000000000000{"))
	movement := base("04", "150726", "0000000001234E")
	movement[33] = "05"
	movement[43] = "150726"
	movement[49] = "Synthetic sale"
	movement[105] = "FAKE-REFERENCE"
	close := fixedFixture(120, base("07", "310726", "0000000001234E"))
	return strings.Join([]string{open, fixedFixture(120, movement), close}, "\n") + "\n"
}
func normaFixture() string {
	return strings.Join([]string{
		fixedFixture(80, map[int]string{1: "11", 3: "999999999999999999", 21: "260701", 27: "260731", 33: "2", 34: "00000000000000", 48: "840", 51: "3", 52: "Synthetic account"}),
		fixedFixture(80, map[int]string{1: "22", 7: "9999", 11: "260715", 17: "260715", 23: "04000", 28: "2", 29: "00000000012345", 43: "0000000000", 53: "000000000000", 65: "Synthetic sale"}),
		fixedFixture(80, map[int]string{1: "2301", 5: "Synthetic complement", 43: "Payment reference"}),
		fixedFixture(80, map[int]string{1: "33", 3: "999999999999999999", 21: "00000", 26: "00000000000000", 40: "00001", 45: "00000000012345", 59: "2", 60: "00000000012345", 74: "840"}),
		fixedFixture(80, map[int]string{1: "88", 3: strings.Repeat("9", 18), 21: "000004"})}, "\n") + "\n"
}
func codaFixture() string {
	acct := fmt.Sprintf("%-34sUSD", "FAKE-ACCOUNT")
	return strings.Join([]string{
		fixedFixture(128, map[int]string{1: "00000", 6: "310726", 12: "999", 15: "05", 25: "FAKE-FILE", 61: "FAKEBICXXX", 72: "00000000000", 84: "00000", 128: "2"}),
		fixedFixture(128, map[int]string{1: "13001", 6: acct, 43: "0", 44: "000000000000000", 59: "010726", 65: "Synthetic account", 126: "001"}),
		fixedFixture(128, map[int]string{1: "2100010000", 11: "FAKE-REFERENCE", 32: "0", 33: "000000000123450", 48: "150726", 54: "00150000", 62: "0", 63: "Synthetic sale", 116: "150726", 122: "001", 125: "00", 128: "0"}),
		fixedFixture(128, map[int]string{1: "8001", 5: acct, 42: "0", 43: "000000000123450", 58: "310726", 128: "0"}),
		fixedFixture(128, map[int]string{1: "9", 17: "000003", 23: "000000000000000", 38: "000000000123450", 128: "2"})}, "\n") + "\n"
}

const baiFixture = "01,FAKE-BANK,FAKE-RECEIVER,260731,1200,1,,,2/\n02,,FAKE-BANK,1,260715,1200,USD,2/\n03,FAKE-ACCOUNT,USD,010,0,,,015,12345,,/\n16,195,12345,0,FAKE-REFERENCE,,Synthetic sale\n49,24690,3/\n98,24690,1,5/\n99,24690,1,7/\n"
const mtFixture = ":20:FAKE-MESSAGE\n:25:FAKE-ACCOUNT\n:28C:1/1\n:60F:C260701USD0,00\n:61:2607150715C123,45NTRFNONREF//FAKE-TX-1\n:86:Synthetic sale\n:62F:C260731USD123,45\n"
const mtInterim = ":20:FAKE-MESSAGE\n:25:FAKE-ACCOUNT\n:28C:1/1\n:13D:2607151200+0000\n:34F:USD0,00\n:61:2607150715C123,45NTRFNONREF//FAKE-TX-1\n:86:Synthetic sale\n:90D:0USD0,00\n:90C:1USD123,45\n"

func camtFixture(message, version string) string {
	container, block := "BkToCstmrStmt", "Stmt"
	switch message {
	case "052":
		container, block = "BkToCstmrAcctRpt", "Rpt"
	case "054":
		container, block = "BkToCstmrDbtCdtNtfctn", "Ntfctn"
	}
	status := "BOOK"
	if version == "08" {
		status = "<Cd>BOOK</Cd>"
	}
	return fmt.Sprintf(`<Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.%s.001.%s"><%s><GrpHdr><MsgId>FAKE-MESSAGE</MsgId><CreDtTm>2026-07-31T12:00:00Z</CreDtTm><MsgPgntn><PgNb>1</PgNb><LastPgInd>true</LastPgInd></MsgPgntn></GrpHdr><%s><Id>FAKE-STATEMENT</Id><Acct><Id><Othr><Id>FAKE-ACCOUNT</Id></Othr></Id><Ccy>USD</Ccy><Svcr><FinInstnId><BICFI>FAKEBICXXX</BICFI></FinInstnId></Svcr></Acct><Bal><Tp><CdOrPrtry><Cd>OPBD</Cd></CdOrPrtry></Tp><Amt Ccy="USD">0.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><Dt><Dt>2026-07-01</Dt></Dt></Bal><Bal><Tp><CdOrPrtry><Cd>CLBD</Cd></CdOrPrtry></Tp><Amt Ccy="USD">123.45</Amt><CdtDbtInd>CRDT</CdtDbtInd><Dt><Dt>2026-07-31</Dt></Dt></Bal><Ntry><Amt Ccy="USD">123.45</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>%s</Sts><BookgDt><Dt>2026-07-15</Dt></BookgDt><ValDt><Dt>2026-07-16</Dt></ValDt><AcctSvcrRef>FAKE-TX-1</AcctSvcrRef><BkTxCd><Prtry><Cd>SALE</Cd></Prtry></BkTxCd><NtryDtls><TxDtls><Amt Ccy="USD">60.00</Amt><RmtInf><Ustrd>Synthetic sale</Ustrd></RmtInf></TxDtls><TxDtls><Amt Ccy="USD">63.45</Amt><RmtInf><Ustrd>Second component</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry></%s></%s></Document>`, message, version, container, block, status, block, container)
}
func xlsxFixture(t *testing.T, formula bool) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	const ns = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	var rows strings.Builder
	for i, row := range [][]string{{"Date", "Amount", "Description"}, {"2026-07-15", "123.45", "Synthetic sale"}} {
		fmt.Fprintf(&rows, `<row r="%d">`, i+1)
		for j, value := range row {
			var escaped bytes.Buffer
			if e := xml.EscapeText(&escaped, []byte(value)); e != nil {
				t.Fatal(e)
			}
			if formula && i == 1 && j == 1 {
				fmt.Fprintf(&rows, `<c r="%c%d"><f>100+23.45</f><v>123.45</v></c>`, rune('A'+j), i+1)
			} else {
				fmt.Fprintf(&rows, `<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, rune('A'+j), i+1, escaped.String())
			}
		}
		rows.WriteString("</row>")
	}
	files := map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns="` + ns + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Statement" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="` + ns + `"><sheetData>` + rows.String() + `</sheetData></worksheet>`,
	}
	for name, text := range files {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(text)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func requireFormatError(t *testing.T, data []byte, o Options, code string) {
	t.Helper()
	_, e := ParseFile(data, o)
	a, ok := apperr.As(e)
	if !ok || a.Code != code {
		t.Fatalf("error=%v; want %s", e, code)
	}
}
func TestStatementFormatFamilies(t *testing.T) {
	o := testOptions()
	btrs := strings.Replace(strings.Replace(baiFixture, ",1,,,2/", ",1,,,3/", 1), "1200,USD,2/", "1200,,2/", 1)
	cases := []struct {
		name, input string
		weak        bool
		status      string
	}{
		{"CSV", "Date,Amount,Description\n2026-07-15,123.45,Synthetic sale\n", true, "POSTED"},
		{"TSV", "Date\tAmount\tDescription\n2026-07-15\t123.45\tSynthetic sale\n", true, "POSTED"},
		{"QIF", "!Type:Bank\nD7/15/2026\nT123.45\nPSynthetic sale\n^\n", true, "POSTED"},
		{"MT940", mtFixture, false, "POSTED"}, {"MT942", mtInterim, false, "REVIEW"},
		{"BAI2", baiFixture, true, "POSTED"}, {"BTRS", btrs, true, "POSTED"},
		{"CFONB120", cfonbFixture(), true, "POSTED"}, {"NORMA43", normaFixture(), true, "POSTED"}, {"CODA", codaFixture(), true, "POSTED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := o
			opts.Format = c.name
			if c.name == "CSV" || c.name == "TSV" {
				opts.Tabular = testTableProfile()
			}
			if c.name == "QIF" {
				opts.DateLayout = "01/02/2006"
			}
			d, e := ParseFile([]byte(c.input), opts)
			if e != nil {
				t.Fatal(e)
			}
			if len(d.Accounts) != 1 || len(d.Accounts[0].Transactions) != 1 {
				t.Fatalf("document=%+v", d)
			}
			tr := d.Accounts[0].Transactions[0]
			if d.Parser != StatementParserVersion || tr.Amount != "123.45" || tr.PostedDate != "2026-07-15" || tr.Status != c.status || !strings.Contains(tr.Description, "Synthetic") {
				t.Fatalf("semantics=%+v", tr)
			}
			if (tr.Identity == "FILE") != c.weak {
				t.Fatalf("identity=%+v", tr)
			}
			again, e := ParseFile([]byte(c.input), opts)
			if e != nil || again.Accounts[0].Transactions[0].ID != tr.ID {
				t.Fatal("file replay changed identity")
			}
			if c.name != "CSV" && c.name != "TSV" {
				opts.Format = ""
				if _, e = ParseFile([]byte(c.input), opts); e != nil {
					t.Fatalf("detection: %v", e)
				}
			}
		})
	}
	for _, family := range []string{"052", "053", "054"} {
		for _, version := range []string{"02", "08"} {
			t.Run("CAMT/"+family+"/"+version, func(t *testing.T) {
				d, e := ParseFile([]byte(camtFixture(family, version)), Options{})
				if e != nil {
					t.Fatal(e)
				}
				tr := d.Accounts[0].Transactions[0]
				if tr.Amount != "123.45" || tr.Identity != "NATIVE" || len(tr.Details) != 2 || tr.ValueAt != "2026-07-16" {
					t.Fatalf("CAMT semantics=%+v", tr)
				}
			})
		}
	}
	t.Run("XLSX", func(t *testing.T) {
		o.Tabular = testTableProfile()
		d, e := ParseFile(xlsxFixture(t, false), o)
		if e != nil {
			t.Fatal(e)
		}
		if d.Format != "XLSX" || d.Accounts[0].Transactions[0].Amount != "123.45" {
			t.Fatalf("xlsx=%+v", d)
		}
		requireFormatError(t, xlsxFixture(t, true), o, "IMPORT_XLSX_FORMULA")
	})
}
func TestWebConnectVariantsKeepOFXIdentity(t *testing.T) {
	base, e := Parse([]byte(sampleXML))
	if e != nil {
		t.Fatal(e)
	}
	for _, format := range []string{"QFX", "QBO"} {
		for _, sgml := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sgml=%v", format, sgml), func(t *testing.T) {
				text := strings.Replace(sampleXML, "</OFX>", "<INTU.BID>99999</INTU.BID><INTU.USERID>FAKE-USER</INTU.USERID></OFX>", 1)
				if sgml {
					text = "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:NONE\nCOMPRESSION:NONE\n\n" + text[strings.Index(text, "<OFX>"):]
					text = strings.ReplaceAll(strings.ReplaceAll(text, "</INTU.BID>", ""), "</INTU.USERID>", "")
				}
				d, e := ParseFile([]byte(text), Options{Format: format})
				if e != nil {
					t.Fatal(e)
				}
				if d.Format != format || d.Accounts[0].Key != base.Accounts[0].Key || d.Accounts[0].Transactions[0].ID != base.Accounts[0].Transactions[0].ID {
					t.Fatal("Web Connect changed OFX account or transaction identity")
				}
			})
		}
	}
}
func TestTabularProfilesAreExplicitAndExact(t *testing.T) {
	o := testOptions()
	o.Format = "CSV"
	input := []byte("Date;Amount;Description\n15/07/2026;1.234,56;Synthetic sale\n")
	requireFormatError(t, input, o, "IMPORT_PROFILE_REQUIRED")
	o.Tabular = testTableProfile()
	o.Tabular.Delimiter = ";"
	o.DateLayout = "02/01/2006"
	requireFormatError(t, input, o, "IMPORT_AMOUNT_INVALID")
	o.Tabular.DecimalSeparator = ","
	o.Tabular.ThousandsSeparator = "."
	d, e := ParseFile(input, o)
	if e != nil || d.Accounts[0].Transactions[0].Amount != "1234.56" {
		t.Fatalf("locale parse: %+v %v", d, e)
	}
	requireFormatError(t, bytes.Replace(input, []byte("1.234,56"), []byte("1.23,456"), 1), o, "IMPORT_AMOUNT_INVALID")
	o.Tabular.AmountColumn = ""
	o.Tabular.DebitColumn = "Debit"
	o.Tabular.CreditColumn = "Credit"
	input = []byte("Date;Debit;Credit;Description\n15/07/2026;123,45;;Synthetic expense\n")
	d, e = ParseFile(input, o)
	if e != nil || d.Accounts[0].Transactions[0].Amount != "-123.45" {
		t.Fatalf("debit parse: %+v %v", d, e)
	}
	requireFormatError(t, bytes.Replace(input, []byte("123,45;;"), []byte("123,45;2,00;"), 1), o, "IMPORT_SIGN_INVALID")
	o.DateLayout = "01/02/06"
	o.YearPivot = nil
	requireFormatError(t, []byte("Date;Debit;Credit;Description\n07/15/26;123,45;;Synthetic expense\n"), o, "IMPORT_YEAR_PIVOT_REQUIRED")
	o = testOptions()
	o.Format = "CSV"
	o.Tabular = testTableProfile()
	o.Tabular.StatusColumn = "State"
	o.Tabular.StatusValues = map[string]string{"waiting": "PENDING", "settled": "POSTED"}
	input = []byte("Date,Amount,Description,State\n2026-07-15,0.000000001,Synthetic,waiting\n")
	d, e = ParseFile(input, o)
	if e != nil || d.Accounts[0].Transactions[0].Status != "PENDING" || d.Accounts[0].Transactions[0].Amount != "0.000000001" {
		t.Fatalf("exact pending: %+v %v", d, e)
	}
	requireFormatError(t, bytes.Replace(input, []byte("waiting"), []byte("unknown"), 1), o, "IMPORT_STATUS_UNKNOWN")
}
func TestQIFSplitsAndWeakIdentity(t *testing.T) {
	o := testOptions()
	o.Format = "QIF"
	o.DateLayout = "01/02/2006"
	input := "!Account\nNFAKE-ACCOUNT\nTBank\n^\n!Type:Bank\nD7/15/2026\nT-123.45\nPSynthetic supplier\nSOffice\n$-100.00\nSShipping\n$-23.45\n^\n"
	d, e := ParseFile([]byte(input), o)
	if e != nil {
		t.Fatal(e)
	}
	tr := d.Accounts[0].Transactions[0]
	if tr.Amount != "-123.45" || len(tr.Details) != 2 || tr.Identity != "FILE" {
		t.Fatalf("splits=%+v", tr)
	}
	requireFormatError(t, []byte(strings.Replace(input, "$-23.45", "$-23.46", 1)), o, "QIF_SPLIT_TOTAL_MISMATCH")
	requireFormatError(t, []byte(strings.TrimSuffix(input, "^\n")), o, "QIF_TRUNCATED")
	requireFormatError(t, []byte(strings.Replace(input, "TBank", "TOth L", 1)), o, "QIF_ACCOUNT_UNSUPPORTED")
}
func TestStatementControlFailures(t *testing.T) {
	o := testOptions()
	cases := []struct{ format, input, code string }{
		{"MT940", strings.Replace(mtFixture, ":62F:C260731USD123,45", ":62F:C260731USD123,46", 1), "IMPORT_BALANCE_MISMATCH"},
		{"MT942", strings.Replace(mtInterim, ":90C:1USD", ":90C:2USD", 1), "IMPORT_COUNT_MISMATCH"},
		{"BAI2", strings.Replace(baiFixture, "49,24690,3/", "49,24691,3/", 1), "BAI_CONTROL_MISMATCH"},
		{"BAI2", strings.Replace(baiFixture, "99,24690,1,7/", "99,24690,1,6/", 1), "BAI_CONTROL_MISMATCH"},
		{"BAI2", strings.Replace(baiFixture, "16,195", "16,190", 1), "BAI_CODE_UNSUPPORTED"},
		{"BAI2", strings.Replace(baiFixture, "FAKE-BANK,1,260715", "FAKE-BANK,3,260715", 1), "BAI_GROUP_STATUS_UNSUPPORTED"},
		{"CFONB120", strings.TrimSuffix(cfonbFixture(), "\n")[:120], "CFONB_STRUCTURE_INVALID"},
		{"NORMA43", strings.Replace(normaFixture(), "000004", "000003", 1), "NORMA43_CONTROL_MISMATCH"},
		{"CODA", strings.Replace(codaFixture(), "\n9               000003", "\n9               000004", 1), "CODA_CONTROL_MISMATCH"},
		{"CAMT", strings.Replace(camtFixture("053", "08"), "<PgNb>1</PgNb>", "<PgNb>2</PgNb>", 1), "CAMT_PAGINATION_INCOMPLETE"},
		{"CAMT", strings.Replace(camtFixture("053", "08"), "<Cd>BOOK</Cd>", "<Cd>PDNG</Cd>", 1), "CAMT_BALANCE_MISMATCH"},
		{"CAMT", strings.Replace(camtFixture("053", "08"), `<Ntry><Amt Ccy="USD">`, `<Ntry><Amt Ccy="EUR">`, 1), "CAMT_CURRENCY_MISMATCH"},
	}
	for _, c := range cases {
		t.Run(c.format+"/"+c.code, func(t *testing.T) {
			opts := o
			opts.Format = c.format
			requireFormatError(t, []byte(c.input), opts, c.code)
		})
	}
}
func TestBAIContinuationsFundsAndInterim(t *testing.T) {
	o := testOptions()
	input := strings.Replace(baiFixture, "16,195,12345,0,FAKE-REFERENCE,,Synthetic sale", "16,195,12345,S,6000,6000,345/\n88,FAKE-REFERENCE,,Synthetic sale\n88, continued text / with slash", 1)
	input = strings.NewReplacer("49,24690,3/", "49,24690,5/", "98,24690,1,5/", "98,24690,1,7/", "99,24690,1,7/", "99,24690,1,9/").Replace(input)
	d, e := ParseFile([]byte(input), o)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(d.Accounts[0].Transactions[0].Description, "continued text / with slash") {
		t.Fatal("continuation lost")
	}
	input = strings.Replace(baiFixture, "1200,USD,2/", "1200,USD,3/", 1)
	d, e = ParseFile([]byte(input), o)
	if e != nil || d.Accounts[0].Transactions[0].Status != "REVIEW" {
		t.Fatalf("interim=%+v %v", d, e)
	}
}
func TestRegionalDebitAndEncoding(t *testing.T) {
	o := testOptions()
	input := strings.ReplaceAll(cfonbFixture(), "0000000001234E", "0000000001234N")
	d, e := ParseFile([]byte(input), o)
	if e != nil || d.Accounts[0].Transactions[0].Amount != "-123.45" {
		t.Fatalf("overpunch debit=%+v %v", d, e)
	}
	lines := strings.Split(codaFixture(), "\n")
	move := []byte(lines[2])
	move[31] = '1'
	lines[2] = string(move)
	close := []byte(lines[3])
	close[41] = '1'
	lines[3] = string(close)
	trailer := []byte(lines[4])
	copy(trailer[22:37], "000000000123450")
	copy(trailer[37:52], "000000000000000")
	lines[4] = string(trailer)
	d, e = ParseFile([]byte(strings.Join(lines, "\n")), o)
	if e != nil || d.Accounts[0].Transactions[0].Amount != "-123.45" {
		t.Fatalf("CODA debit=%+v %v", d, e)
	}
	raw := []byte(cfonbFixture())
	idx := bytes.Index(raw, []byte("Synthetic sale"))
	raw[idx] = 0xe9
	o.Encoding = "WINDOWS-1252"
	d, e = ParseFile(raw, o)
	if e != nil || !strings.HasPrefix(d.Accounts[0].Transactions[0].Description, "é") {
		t.Fatalf("byte-based legacy text=%+v %v", d, e)
	}
}
func FuzzStatementParsers(f *testing.F) {
	for _, input := range []string{baiFixture, mtFixture, mtInterim, cfonbFixture(), codaFixture(), normaFixture(), camtFixture("053", "08"), "!Type:Bank\nD2026-07-15\nT1\nPX\n^\n"} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > MaxBytes {
			return
		}
		_, _ = ParseFile(input, testOptions())
	})
}
