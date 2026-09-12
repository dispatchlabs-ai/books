package banking

import (
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="230" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX><SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><DTSERVER>20260731120000</DTSERVER><LANGUAGE>ENG</LANGUAGE><FI><ORG>Example</ORG><FID>FAKE-FI</FID></FI></SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1><STMTTRNRS><TRNUID>1</TRNUID><STATUS><CODE>0</CODE><SEVERITY>INFO</SEVERITY></STATUS><STMTRS><CURDEF>USD</CURDEF><BANKACCTFROM><BANKID>FAKE-BANK</BANKID><ACCTID>FAKE-ACCOUNT-1234</ACCTID><ACCTTYPE>CHECKING</ACCTTYPE></BANKACCTFROM><BANKTRANLIST><DTSTART>20260701</DTSTART><DTEND>20260731</DTEND><STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260715120000.000[-5:EST]</DTPOSTED><TRNAMT>-12.34</TRNAMT><FITID>fake-tx-1</FITID><NAME>Example &amp; Co</NAME><MEMO>Office supplies</MEMO></STMTTRN></BANKTRANLIST><LEDGERBAL><BALAMT>987.66</BALAMT><DTASOF>20260731</DTASOF></LEDGERBAL><AVAILBAL><BALAMT>980.00</BALAMT><DTASOF>20260731</DTASOF></AVAILBAL></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`

func TestOFXFormatsPreserveIdentityDatesAndBalances(t *testing.T) {
	xmlDoc, e := Parse([]byte(sampleXML))
	if e != nil {
		t.Fatal(e)
	}
	body := sampleXML[strings.Index(sampleXML, "<OFX>"):]
	for _, name := range []string{"CODE", "SEVERITY", "DTSERVER", "LANGUAGE", "ORG", "FID", "TRNUID", "CURDEF", "BANKID", "ACCTID", "ACCTTYPE", "DTSTART", "DTEND", "TRNTYPE", "DTPOSTED", "TRNAMT", "FITID", "NAME", "MEMO", "BALAMT", "DTASOF"} {
		body = strings.ReplaceAll(body, "</"+name+">", "")
	}
	sgml := "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:NONE\nCOMPRESSION:NONE\n\n" + body
	sgmlDoc, e := Parse([]byte(sgml))
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []Document{xmlDoc, sgmlDoc} {
		a := d.Accounts[0]
		tr := a.Transactions[0]
		if a.Key != xmlDoc.Accounts[0].Key || tr.Amount != "-12.34" || tr.PostedDate != "2026-07-15" || tr.Description != "Example & Co — Office supplies" || tr.Fields["MEMO"] != "Office supplies" || a.Balances[0].Amount != "987.66" || a.Balances[1].Kind != "AVAILBAL" {
			t.Fatalf("lost semantics: %+v", d)
		}
	}
}
func TestOFXRejectsUnsupportedAndMalformedFinancialContent(t *testing.T) {
	cases := map[string]string{
		"correction":      strings.Replace(sampleXML, "</STMTTRN>", "<CORRECTFITID>old</CORRECTFITID></STMTTRN>", 1),
		"currency":        strings.ReplaceAll(sampleXML, "USD", "XXX"),
		"fraction":        strings.Replace(sampleXML, "-12.34", "-12.345", 1),
		"duplicate-field": strings.Replace(sampleXML, "<TRNAMT>-12.34</TRNAMT>", "<TRNAMT>-12.34</TRNAMT><TRNAMT>1</TRNAMT>", 1),
		"duplicate-id":    strings.Replace(sampleXML, "</BANKTRANLIST>", sampleXML[strings.Index(sampleXML, "<STMTTRN>"):strings.Index(sampleXML, "</STMTTRN>")+len("</STMTTRN>")]+"</BANKTRANLIST>", 1),
		"bad-date":        strings.Replace(sampleXML, "20260715120000.000[-5:EST]", "20260230120000", 1),
		"out-of-period":   strings.Replace(sampleXML, "20260715120000.000[-5:EST]", "20260801", 1),
		"status":          strings.Replace(sampleXML, "<CODE>0</CODE>", "<CODE>2000</CODE>", 1),
		"xxe":             `<!DOCTYPE OFX [<!ENTITY e SYSTEM "file:///fake-secret">]>` + sampleXML[strings.Index(sampleXML, "<OFX>"):],
		"truncated":       sampleXML[:len(sampleXML)-6],
		"second-root":     sampleXML + "<OFX/>",
		"unknown-message": strings.Replace(sampleXML, "</OFX>", "<INVSTMTMSGSRSV1/></OFX>", 1),
		"pending":         strings.Replace(sampleXML, "<STMTTRN>", "<STMTTRNP>", 1),
		"overflow":        strings.Replace(sampleXML, "-12.34", "92233720368547758.08", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := Parse([]byte(input)); e == nil {
				t.Fatal("unsafe input was accepted")
			}
		})
	}
}
func TestOFXCardSignsAndAccountNamespace(t *testing.T) {
	card := strings.NewReplacer("BANKMSGSRSV1", "CREDITCARDMSGSRSV1", "STMTTRNRS", "CCSTMTTRNRS", "STMTRS", "CCSTMTRS", "BANKACCTFROM", "CCACCTFROM", "<BANKID>FAKE-BANK</BANKID>", "", "<ACCTTYPE>CHECKING</ACCTTYPE>", "").Replace(sampleXML)
	d, e := Parse([]byte(card))
	if e != nil {
		t.Fatal(e)
	}
	a := d.Accounts[0]
	if a.Kind != "CREDIT_CARD" || a.Transactions[0].Amount != "-12.34" {
		t.Fatalf("card semantics: %+v", a)
	}
	noFID := strings.Replace(card, "<FID>FAKE-FI</FID>", "", 1)
	if _, e = Parse([]byte(noFID)); e == nil {
		t.Fatal("accepted unscoped card identity")
	}
	changed := strings.Replace(sampleXML, "FAKE-BANK", "OTHER-FAKE-BANK", 1)
	bank1, e := Parse([]byte(sampleXML))
	if e != nil {
		t.Fatal(e)
	}
	bank2, e := Parse([]byte(changed))
	if e != nil {
		t.Fatal(e)
	}
	if bank1.Accounts[0].Key == bank2.Accounts[0].Key {
		t.Fatal("institution namespace lost")
	}
}
func TestOFXResourceBudgets(t *testing.T) {
	for _, input := range []string{strings.Repeat("x", MaxBytes+1), strings.Replace(sampleXML, "Office supplies", strings.Repeat("x", MaxText+1), 1), "<OFX>" + strings.Repeat("<X>", MaxDepth+1) + strings.Repeat("</X>", MaxDepth+1) + "</OFX>"} {
		_, e := Parse([]byte(input))
		a, ok := apperr.As(e)
		if !ok || (a.Code != "IMPORT_SIZE_INVALID" && a.Code != "IMPORT_LIMIT_EXCEEDED") {
			t.Fatalf("budget error: %v", e)
		}
	}
}
func TestOFXLegacyEncoding(t *testing.T) {
	v, e := decodeText([]byte{'A', 0x80, 0x92}, "USASCII", "1252")
	if e != nil || v != "A€’" {
		t.Fatalf("decode: %q %v", v, e)
	}
	if _, e = decodeText([]byte{0xff}, "USASCII", "NONE"); e == nil {
		t.Fatal("invalid ASCII accepted")
	}
}
func FuzzParseOFX(f *testing.F) {
	f.Add([]byte(sampleXML))
	f.Add([]byte("OFXHEADER:100\n<OFX>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxBytes {
			return
		}
		_, _ = Parse(data)
	})
}

func TestOFXXMLHeaderAndOffsets(t *testing.T) {
	good, err := Parse([]byte(sampleXML))
	if err != nil || good.Version != "230" {
		t.Fatalf("version=%s %v", good.Version, err)
	}
	for name, input := range map[string]string{
		"version":            strings.Replace(sampleXML, `VERSION="230"`, `VERSION="999"`, 1),
		"security":           strings.Replace(sampleXML, `SECURITY="NONE"`, `SECURITY="TYPE1"`, 1),
		"transaction-offset": strings.Replace(sampleXML, "[-5:EST]", "[99:FAKE]", 1),
		"range-offset":       strings.Replace(sampleXML, "<DTSTART>20260701", "<DTSTART>20260701[-13]", 1),
		"balance-offset":     strings.Replace(sampleXML, "<DTASOF>20260731", "<DTASOF>20260731[14.5]", 1),
		"mixed-text":         strings.Replace(sampleXML, "<BANKTRANLIST>", "<BANKTRANLIST>ignored", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
}

func TestOFXCurrencyPrecision(t *testing.T) {
	for _, tc := range []struct{ code, amount, balance, available, invalid string }{
		{"EUR", "-12.34", "987.66", "980.00", "1.001"},
		{"JPY", "-12", "988", "980", "1.1"},
		{"KWD", "-12.345", "987.655", "980.001", "1.0001"},
		{"CLF", "-12.3456", "987.6544", "980.0001", "1.00001"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			source := strings.NewReplacer("USD", tc.code, "-12.34", tc.amount, "987.66", tc.balance, "980.00", tc.available).Replace(sampleXML)
			doc, e := Parse([]byte(source))
			if e != nil {
				t.Fatal(e)
			}
			a := doc.Accounts[0]
			if a.Currency != tc.code || a.Transactions[0].Amount != tc.amount || a.Balances[0].Amount != tc.balance || a.Balances[1].Amount != tc.available {
				t.Fatal(a)
			}
			if _, e = Parse([]byte(strings.Replace(source, "<TRNAMT>"+tc.amount+"</TRNAMT>", "<TRNAMT>"+tc.invalid+"</TRNAMT>", 1))); e == nil {
				t.Fatal("fractional minor unit accepted")
			}
		})
	}
}
