package banking

import (
	"bytes"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/money"
	"strconv"
	"strings"
)

// Positions in these bank formats are byte positions, before text decoding.
func fixedRecords(data []byte, width int) ([]string, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var out []string
	if bytes.ContainsAny(data, "\r\n") {
		lines := bytes.Split(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"))
		for i, line := range lines {
			if len(line) == 0 && i == len(lines)-1 {
				continue
			}
			if len(line) != width {
				return nil, invalid("IMPORT_RECORD_WIDTH_INVALID", fmt.Sprintf("record %d must have %d bytes", i+1, width))
			}
			out = append(out, string(line))
		}
	} else {
		if len(data)%width != 0 {
			return nil, invalid("IMPORT_RECORD_WIDTH_INVALID", "truncated fixed-width record")
		}
		for len(data) > 0 {
			out = append(out, string(data[:width]))
			data = data[width:]
		}
	}
	if len(out) == 0 || len(out) > MaxNodes {
		return nil, invalid("IMPORT_RECORD_LIMIT", "invalid fixed-width record count")
	}
	return out, nil
}
func fixedText(v string, o Options) (string, error) {
	s, e := textInput([]byte(v), o)
	return strings.TrimSpace(s), e
}
func overpunch(v string, scale int) (string, error) {
	if len(v) < 2 {
		return "", invalid("CFONB_AMOUNT_INVALID", "truncated overpunched amount")
	}
	last := v[len(v)-1]
	digit := strings.IndexByte("{ABCDEFGHI", last)
	negative := false
	if digit < 0 {
		digit = strings.IndexByte("}JKLMNOPQR", last)
		negative = true
	}
	if digit < 0 {
		return "", invalid("CFONB_AMOUNT_INVALID", "invalid signed final digit")
	}
	amount, e := fixedDecimal(v[:len(v)-1]+strconv.Itoa(digit), scale)
	if e != nil {
		return "", e
	}
	if negative {
		amount = negate(amount)
	}
	return amount, nil
}
func appendFixedAccount(d *Document, a Account) {
	for i := range d.Accounts {
		p := &d.Accounts[i]
		if p.Key == a.Key {
			p.Transactions = append(p.Transactions, a.Transactions...)
			p.Statements = append(p.Statements, a.Statements...)
			p.Balances = append(p.Balances, a.Balances...)
			if a.From < p.From {
				p.From = a.From
			}
			if a.Through > p.Through {
				p.Through = a.Through
			}
			return
		}
	}
	d.Accounts = append(d.Accounts, a)
}
func parseCFONB(data []byte, o Options) (Document, error) {
	d := Document{Format: "CFONB120", Version: "2004-07"}
	lines, e := fixedRecords(data, 120)
	if e != nil {
		return d, e
	}
	var a *Account
	var st Statement
	var header string
	scale := 0
	movement := "0.00"
	for _, r := range lines {
		code := r[:2]
		if code == "01" {
			if a != nil {
				return d, invalid("CFONB_STRUCTURE_INVALID", "opening record before prior closing")
			}
			if !digits(r[2:7]) || !digits(r[11:16]) || !digits(r[19:20]) {
				return d, invalid("CFONB_ACCOUNT_INVALID", "invalid bank, branch, or decimals")
			}
			header = r
			opts := o
			if opts.Institution == "" {
				opts.Institution = r[2:7]
			}
			id := strings.TrimSpace(r[21:32])
			account, e := sourceAccount(opts, "CFONB120", id, r[16:19], "")
			if e != nil {
				return d, e
			}
			account.BankID = r[2:7]
			account.BranchID = r[11:16]
			account.Key = accountKey("CFONB120", account)
			a = &account
			scale, _ = strconv.Atoi(r[19:20])
			date, e := shortDate(r[34:40], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			amount, e := overpunch(r[90:104], scale)
			if e != nil {
				return d, e
			}
			st = Statement{ID: fmt.Sprintf("%s:%s", id, date), From: date, Fields: map[string]string{}}
			st.Balances = []Balance{{Kind: "OPENING", Amount: amount, AsOf: date}}
			movement = "0.00"
			continue
		}
		if a == nil {
			return d, invalid("CFONB_STRUCTURE_INVALID", "record outside an account")
		}
		if r[2:7] != header[2:7] || r[11:20] != header[11:20] || r[21:32] != header[21:32] {
			return d, invalid("CFONB_ACCOUNT_MISMATCH", "record account identity differs from its header")
		}
		switch code {
		case "04":
			date, e := shortDate(r[34:40], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			value, e := shortDate(r[42:48], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			amount, e := overpunch(r[90:104], scale)
			if e != nil {
				return d, e
			}
			description, e := fixedText(r[48:79], o)
			if e != nil {
				return d, e
			}
			reference, e := fixedText(r[104:120], o)
			if e != nil {
				return d, e
			}
			t := Transaction{Type: r[32:34], PostedDate: date, ValueAt: value, Amount: amount, Description: description, Fields: map[string]string{"reference": reference, "entry_number": strings.TrimSpace(r[81:88]), "internal_operation": r[7:11], "booking_date_raw": r[34:40]}}
			a.Transactions = append(a.Transactions, t)
			movement, e = decimalSum(movement, amount)
			if e != nil {
				return d, e
			}
		case "05":
			if len(a.Transactions) == 0 {
				return d, invalid("CFONB_STRUCTURE_INVALID", "orphan supplementary record")
			}
			t := &a.Transactions[len(a.Transactions)-1]
			if r[34:40] != t.Fields["booking_date_raw"] {
				return d, invalid("CFONB_SUPPLEMENT_INVALID", "supplement booking date differs from its movement")
			}
			text, e := fixedText(r[48:118], o)
			if e != nil {
				return d, e
			}
			t.Details = append(t.Details, Detail{Description: text, Fields: map[string]string{"qualifier": r[45:48]}})
			if text != "" {
				t.Description += " " + text
			}
		case "07":
			date, e := shortDate(r[34:40], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			if date < st.From {
				return d, invalid("CFONB_DATE_INVALID", "closing date precedes opening date")
			}
			amount, e := overpunch(r[90:104], scale)
			if e != nil {
				return d, e
			}
			expected, e := decimalSum(st.Balances[0].Amount, movement)
			if e != nil {
				return d, e
			}
			if !sameAmount(amount, expected) {
				return d, invalid("CFONB_BALANCE_MISMATCH", "opening balance plus movements differs from closing balance")
			}
			for _, t := range a.Transactions {
				if t.PostedDate < st.From || t.PostedDate > date {
					return d, invalid("CFONB_DATE_INVALID", "movement outside statement range")
				}
			}
			st.Through = date
			st.Balances = append(st.Balances, Balance{Kind: "CLOSING", Amount: amount, AsOf: date})
			a.From, a.Through = st.From, st.Through
			a.Balances = st.Balances
			a.Statements = []Statement{st}
			appendFixedAccount(&d, *a)
			a = nil
		default:
			return d, invalid("CFONB_RECORD_UNSUPPORTED", "unknown CFONB record code")
		}
	}
	if a != nil {
		return d, invalid("CFONB_STRUCTURE_INVALID", "missing closing record")
	}
	return d, nil
}
func numericCurrency(v string) (string, error) {
	if code, ok := money.CurrencyFromNumeric(v); ok {
		return code, nil
	}
	return "", invalid("NORMA43_CURRENCY_UNSUPPORTED", "unsupported numeric currency code")
}

func fixedSigned(v, sign string, scale int) (string, error) {
	a, e := fixedDecimal(v, scale)
	if e != nil {
		return "", e
	}
	return signedDecimal(a, sign)
}
func parseNorma43(data []byte, o Options) (Document, error) {
	d := Document{Format: "NORMA43", Version: "2012-06"}
	lines, e := fixedRecords(data, 80)
	if e != nil {
		return d, e
	}
	var a *Account
	var st Statement
	header := ""
	debits, credits := "0.00", "0.00"
	nd, nc := 0, 0
	ended := false
	supplements := 0
	fx := false
	for i, r := range lines {
		if ended {
			return d, invalid("NORMA43_STRUCTURE_INVALID", "data follows the file trailer")
		}
		switch r[:2] {
		case "11":
			if a != nil {
				return d, invalid("NORMA43_STRUCTURE_INVALID", "opening before previous account trailer")
			}
			header = r
			if !digits(r[2:20]) || !strings.Contains("123", r[50:51]) {
				return d, invalid("NORMA43_ACCOUNT_INVALID", "invalid account identity or mode")
			}
			ccy, e := numericCurrency(r[47:50])
			if e != nil {
				return d, e
			}
			opts := o
			if opts.Institution == "" {
				opts.Institution = r[2:6]
			}
			account, e := sourceAccount(opts, "NORMA43", r[10:20], ccy, "")
			if e != nil {
				return d, e
			}
			account.BankID = r[2:6]
			account.BranchID = r[6:10]
			account.Key = accountKey("NORMA43", account)
			a = &account
			from, e := shortDate(r[20:26], "YMD", o.YearPivot)
			if e != nil {
				return d, e
			}
			through, e := shortDate(r[26:32], "YMD", o.YearPivot)
			if e != nil {
				return d, e
			}
			if through < from {
				return d, invalid("NORMA43_DATE_INVALID", "reversed statement date range")
			}
			amount, e := fixedSigned(r[33:47], r[32:33], 2)
			if e != nil {
				return d, e
			}
			name, e := fixedText(r[51:77], o)
			if e != nil {
				return d, e
			}
			st = Statement{ID: fmt.Sprintf("%s:%s:%s", r[2:20], from, through), From: from, Through: through, Balances: []Balance{{Kind: "OPENING", Amount: amount, AsOf: from}}, Fields: map[string]string{"mode": r[50:51], "name": name}}
			nd, nc = 0, 0
			debits, credits = "0.00", "0.00"
		case "22":
			if a == nil {
				return d, invalid("NORMA43_STRUCTURE_INVALID", "movement outside an account")
			}
			date, e := shortDate(r[10:16], "YMD", o.YearPivot)
			if e != nil {
				return d, e
			}
			value, e := shortDate(r[16:22], "YMD", o.YearPivot)
			if e != nil {
				return d, e
			}
			if date < st.From || date > st.Through {
				return d, invalid("NORMA43_DATE_INVALID", "movement outside statement date range")
			}
			amount, e := fixedSigned(r[28:42], r[27:28], 2)
			if e != nil {
				return d, e
			}
			ref, e := fixedText(r[64:80], o)
			if e != nil {
				return d, e
			}
			a.Transactions = append(a.Transactions, Transaction{Type: r[22:27], PostedDate: date, ValueAt: value, Amount: amount, Description: ref, Fields: map[string]string{"origin_branch": r[6:10], "document": r[42:52], "reference_1": r[52:64], "reference_2": ref}})
			if r[27] == '1' {
				nd++
				debits, e = decimalSum(debits, negate(amount))
			} else {
				nc++
				credits, e = decimalSum(credits, amount)
			}
			if e != nil {
				return d, e
			}
			supplements = 0
			fx = false
		case "23":
			if a == nil || len(a.Transactions) == 0 || fx || supplements >= 5 || r[2:4] != fmt.Sprintf("%02d", supplements+1) {
				return d, invalid("NORMA43_SUPPLEMENT_INVALID", "orphan or unordered supplementary concept")
			}
			supplements++
			text, e := fixedText(r[4:42], o)
			if e != nil {
				return d, e
			}
			text2, e := fixedText(r[42:80], o)
			if e != nil {
				return d, e
			}
			t := &a.Transactions[len(a.Transactions)-1]
			t.Description = strings.TrimSpace(t.Description + " " + text + " " + text2)
			t.Details = append(t.Details, Detail{Description: strings.TrimSpace(text + " " + text2), Fields: map[string]string{"sequence": r[2:4]}})
		case "24":
			if a == nil || len(a.Transactions) == 0 || fx || r[2:4] != "01" {
				return d, invalid("NORMA43_SUPPLEMENT_INVALID", "orphan or duplicate FX equivalence")
			}
			fx = true
			ccy, e := numericCurrency(r[4:7])
			if e != nil {
				return d, e
			}
			if ccy == a.Currency {
				return d, invalid("NORMA43_SUPPLEMENT_INVALID", "FX equivalence must use another currency")
			}
			amount, e := fixedDecimal(r[7:21], 2)
			if e != nil {
				return d, e
			}
			t := &a.Transactions[len(a.Transactions)-1]
			t.Details = append(t.Details, Detail{Currency: ccy, Amount: amount, Fields: map[string]string{"kind": "FX_EQUIVALENCE"}})
		case "33":
			if a == nil || r[2:20] != header[2:20] || r[73:76] != header[47:50] {
				return d, invalid("NORMA43_ACCOUNT_MISMATCH", "invalid account trailer identity")
			}
			db, e := fixedDecimal(r[25:39], 2)
			if e != nil {
				return d, e
			}
			cr, e := fixedDecimal(r[44:58], 2)
			if e != nil {
				return d, e
			}
			if !baiCount(r[20:25], nd) || !baiCount(r[39:44], nc) || !sameAmount(db, debits) || !sameAmount(cr, credits) {
				return d, invalid("NORMA43_CONTROL_MISMATCH", "movement count or debit/credit total mismatch")
			}
			close, e := fixedSigned(r[59:73], r[58:59], 2)
			if e != nil {
				return d, e
			}
			expected, e := decimalSum(st.Balances[0].Amount, credits, negate(debits))
			if e != nil {
				return d, e
			}
			if !sameAmount(close, expected) {
				return d, invalid("NORMA43_BALANCE_MISMATCH", "opening balance plus movements differs from closing balance")
			}
			st.Balances = append(st.Balances, Balance{Kind: "CLOSING", Amount: close, AsOf: st.Through})
			a.From, a.Through = st.From, st.Through
			a.Balances = st.Balances
			a.Statements = []Statement{st}
			appendFixedAccount(&d, *a)
			a = nil
		case "88":
			if a != nil || i == 0 || r[2:20] != strings.Repeat("9", 18) || !baiCount(r[20:26], i) {
				return d, invalid("NORMA43_CONTROL_MISMATCH", "invalid file trailer or record count")
			}
			ended = true
		default:
			return d, invalid("NORMA43_RECORD_UNSUPPORTED", "unknown record code")
		}
	}
	if !ended {
		return d, invalid("NORMA43_STRUCTURE_INVALID", "missing file trailer")
	}
	return d, nil
}
