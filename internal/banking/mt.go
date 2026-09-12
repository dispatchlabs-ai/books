package banking

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type mtField struct{ tag, value string }

var mtTag = regexp.MustCompile(`^:([0-9]{2}[A-Z]?):(.*)$`)
var mtBalance = regexp.MustCompile(`^([CD])([0-9]{6})([A-Z]{3})([0-9]{1,15},[0-9]{0,9})$`)
var mtLine = regexp.MustCompile(`^([0-9]{6})([0-9]{4})?(RC|RD|C|D)([A-Z])?([0-9]{1,15},[0-9]{0,9})([A-Z][A-Z0-9]{3})([^\r\n]*)$`)
var mtTimestamp = regexp.MustCompile(`^[0-9]{10}[+-][0-9]{4}$`)
var mtFloor = regexp.MustCompile(`^([A-Z]{3})([CD])?([0-9]{1,15},[0-9]{0,9})$`)

var mtTotal = regexp.MustCompile(`^([0-9]{0,5})([A-Z]{3})([0-9]{1,15},[0-9]{0,9})$`)

func mtAmount(v string) (string, error) {
	if strings.HasSuffix(v, ",") {
		v += "0"
	}
	return decimal(strings.ReplaceAll(v, ",", "."))
}
func parseMT(data []byte, o Options, format string) (Document, error) {
	doc := Document{Format: format, Version: "SWIFT-customer-report"}
	text, e := textInput(data, o)
	if e != nil {
		return doc, e
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{1:") {
		start := strings.Index(text, "{4:")
		if start < 0 {
			return doc, invalid("MT_ENVELOPE_INVALID", "missing SWIFT text block")
		}
		end := strings.Index(text[start+3:], "-}")
		if end < 0 {
			return doc, invalid("MT_ENVELOPE_INVALID", "incomplete SWIFT text block")
		}
		rest := strings.TrimSpace(text[start+3+end+2:])
		if rest != "" && !strings.HasPrefix(rest, "{5:") {
			return doc, invalid("MT_ENVELOPE_UNSUPPORTED", "multiple or unsupported SWIFT envelopes")
		}
		if strings.Contains(rest, "{4:") {
			return doc, invalid("MT_ENVELOPE_UNSUPPORTED", "upload one SWIFT envelope at a time")
		}
		text = text[start+3 : start+3+end]
	}
	var fields []mtField
	var messages [][]mtField
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if line == ":940:" || line == ":942:" {
			if line != ":"+strings.TrimPrefix(format, "MT")+":" {
				return doc, invalid("MT_FORMAT_MISMATCH", "message marker differs from selected format")
			}
			continue
		}
		m := mtTag.FindStringSubmatch(line)
		if m != nil {
			if m[1] == "20" && len(fields) > 0 {
				messages = append(messages, fields)
				fields = nil
			}
			fields = append(fields, mtField{m[1], m[2]})
		} else {
			if len(fields) == 0 {
				return doc, invalid("MT_FIELD_INVALID", "text outside a tagged record")
			}
			p := &fields[len(fields)-1]
			if p.tag != "61" && p.tag != "86" {
				return doc, invalid("MT_FIELD_INVALID", "unexpected continuation")
			}
			p.value += "\n" + line
		}
		if len(fields) > MaxTransactions*2+20 {
			return doc, invalid("IMPORT_LIMIT_EXCEEDED", "too many MT records")
		}
		if len(fields) > 0 && len(fields[len(fields)-1].value) > MaxText {
			return doc, invalid("IMPORT_LIMIT_EXCEEDED", "MT record exceeds text limit")
		}
	}
	if len(fields) > 0 {
		messages = append(messages, fields)
	}
	if len(messages) == 0 || len(messages) > MaxAccounts*20 {
		return doc, invalid("MT_MESSAGE_INVALID", "invalid MT message count")
	}
	byAccount := map[string]int{}
	openPage := map[string]bool{}
	seenStatements := map[string]bool{}
	for _, message := range messages {
		a, statement, e := parseMTMessage(message, o, format)
		if e != nil {
			return Document{}, e
		}
		statementKey := a.Key + "\n" + statement.ID + "\n" + statement.From + "\n" + statement.Through
		if seenStatements[statementKey] {
			return doc, invalid("MT_DUPLICATE_STATEMENT", "repeated statement identifier and period")
		}
		seenStatements[statementKey] = true
		if statement.Fields["partial_report"] == "true" {
			doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Code: "PARTIAL_REPORT", Message: "A nonzero MT942 floor limit omits smaller movements from this report", AccountKey: a.Key})
		}
		index, exists := byAccount[a.Key]
		continuation := statement.Fields["opening_kind"] == "60M"
		intermediate := statement.Fields["closing_kind"] == "62M"
		if continuation && (!exists || !openPage[a.Key]) {
			return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT continuation is missing its first page")
		}
		if !continuation {
			if page, e := mtPage(statement.ID); e != nil || page != 1 {
				return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT statement does not start at page one")
			}
		}
		if exists {
			previous := &doc.Accounts[index]
			last := previous.Statements[len(previous.Statements)-1]
			if openPage[a.Key] {
				if !continuation || strings.Split(last.ID, "/")[0] != strings.Split(statement.ID, "/")[0] {
					return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT continuation belongs to a different statement")
				}
				lp, err := mtPage(last.ID)
				if err != nil {
					return doc, err
				}
				np, err := mtPage(statement.ID)
				if err != nil || np != lp+1 {
					return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT page sequence is not contiguous")
				}
				if len(last.Balances) < 2 || len(statement.Balances) < 2 || !sameAmount(mtBalanceByKind(last.Balances, last.Fields["closing_kind"]).Amount, mtBalanceByKind(statement.Balances, statement.Fields["opening_kind"]).Amount) {
					return doc, invalid("IMPORT_BALANCE_MISMATCH", "MT page balances do not connect")
				}
			}
			previous.Transactions = append(previous.Transactions, a.Transactions...)
			previous.Statements = append(previous.Statements, statement)
			previous.Balances = append(previous.Balances, a.Balances...)
		} else {
			if page, err := mtPage(statement.ID); err != nil || page != 1 {
				return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT statement does not start at page one")
			}
			if len(doc.Accounts) >= MaxAccounts {
				return doc, invalid("IMPORT_LIMIT_EXCEEDED", "too many MT accounts")
			}
			a.Statements = []Statement{statement}
			byAccount[a.Key] = len(doc.Accounts)
			doc.Accounts = append(doc.Accounts, a)
		}
		openPage[a.Key] = intermediate
	}
	for _, open := range openPage {
		if open {
			return doc, invalid("IMPORT_INCOMPLETE_STATEMENT", "MT statement is missing its final page")
		}
	}
	return doc, nil
}
func mtPage(id string) (int, error) {
	parts := strings.Split(id, "/")
	if len(parts) == 1 {
		return 1, nil
	}
	if len(parts) != 2 {
		return 0, invalid("MT_SEQUENCE_INVALID", "invalid statement/page number")
	}
	p, e := strconv.Atoi(parts[1])
	if e != nil || p < 1 {
		return 0, invalid("MT_SEQUENCE_INVALID", "invalid page number")
	}
	return p, nil
}
func parseMTMessage(fields []mtField, o Options, format string) (Account, Statement, error) {
	var a Account
	s := Statement{Fields: map[string]string{}}
	seen := map[string]bool{}
	var lines []mtField
	currency := o.Currency
	accountID := ""
	asof := ""
	var opening, closing *Balance
	var debitTotal, creditTotal string
	debitCount, creditCount := -1, -1
	for _, f := range fields {
		if f.tag != "61" && f.tag != "86" && f.tag != "34F" && f.tag != "65" {
			if seen[f.tag] {
				return a, s, invalid("MT_FIELD_INVALID", "duplicate singleton MT field")
			}
			seen[f.tag] = true
		}
		switch f.tag {
		case "20", "21":
			if f.value == "" || len(f.value) > 16 {
				return a, s, invalid("MT_REFERENCE_INVALID", "invalid message reference")
			}
			s.Fields[f.tag] = f.value
		case "25":
			accountID = strings.TrimSpace(f.value)
		case "28C", "28":
			s.ID = f.value
		case "60F", "60M", "62F", "62M", "64", "65":
			m := mtBalance.FindStringSubmatch(f.value)
			if m == nil {
				return a, s, invalid("MT_BALANCE_INVALID", "invalid balance record")
			}
			if currency != "" && currency != m[3] {
				return a, s, invalid("IMPORT_CURRENCY_MISMATCH", "MT balance currencies differ")
			}
			currency = m[3]
			date, e := shortDate(m[2], "YMD", o.YearPivot)
			if e != nil {
				return a, s, e
			}
			amount, e := mtAmount(m[4])
			if e != nil {
				return a, s, e
			}
			amount, e = signedDecimal(amount, m[1])
			if e != nil {
				return a, s, e
			}
			b := Balance{Kind: f.tag, Amount: amount, AsOf: date}
			s.Balances = append(s.Balances, b)
			switch f.tag {
			case "60F", "60M":
				if opening != nil {
					return a, s, invalid("MT_BALANCE_INVALID", "multiple opening balances")
				}
				opening = &b
				s.From = date
				s.Fields["opening_kind"] = f.tag
			case "62F", "62M":
				if closing != nil {
					return a, s, invalid("MT_BALANCE_INVALID", "multiple closing balances")
				}
				closing = &b
				s.Through = date
				asof = date
				s.Fields["closing_kind"] = f.tag
			}
		case "13D":
			if !mtTimestamp.MatchString(f.value) || !mtClock(f.value[6:10], false) || !mtClock(f.value[11:], true) {
				return a, s, invalid("MT_DATE_INVALID", "missing report timestamp")
			}
			date, e := shortDate(f.value[:6], "YMD", o.YearPivot)
			if e != nil {
				return a, s, e
			}
			asof = date
			s.Fields[f.tag] = f.value
		case "34F":
			m := mtFloor.FindStringSubmatch(f.value)
			if m == nil {
				return a, s, invalid("MT_FLOOR_INVALID", "invalid floor-limit indicator")
			}
			if currency != "" && currency != m[1] {
				return a, s, invalid("IMPORT_CURRENCY_MISMATCH", "floor-limit currency differs")
			}
			currency = m[1]
			key := "floor:" + m[2]
			if _, ok := s.Fields[key]; ok {
				return a, s, invalid("MT_FLOOR_INVALID", "repeated floor limit")
			}
			amount, e := mtAmount(m[3])
			if e != nil {
				return a, s, e
			}
			s.Fields[key] = amount
			if amount != "0.00" {
				s.Fields["partial_report"] = "true"
			}
			s.Fields[fmt.Sprintf("34F:%d", len(s.Fields))] = f.value
		case "90D", "90C":
			m := mtTotal.FindStringSubmatch(f.value)
			if m == nil {
				return a, s, invalid("MT_TOTAL_INVALID", "invalid report total")
			}
			if currency != "" && currency != m[2] {
				return a, s, invalid("IMPORT_CURRENCY_MISMATCH", "MT total currency differs")
			}
			currency = m[2]
			v, e := mtAmount(m[3])
			if e != nil {
				return a, s, e
			}
			count := -1
			if m[1] != "" {
				count, e = strconv.Atoi(m[1])
				if e != nil {
					return a, s, e
				}
			}
			if f.tag == "90D" {
				debitTotal, debitCount = v, count
			} else {
				creditTotal, creditCount = v, count
			}
		case "61":
			lines = append(lines, f)
		case "86":
			if len(lines) == 0 {
				return a, s, invalid("MT_ORPHAN_DETAIL", "description has no transaction")
			}
			lines[len(lines)-1].value += "\n@86:" + f.value
		default:
			return a, s, invalid("MT_TAG_UNSUPPORTED", "unsupported MT tag")
		}
	}
	if !seen["20"] || accountID == "" || s.ID == "" {
		return a, s, invalid("MT_HEADER_INVALID", "message reference, account, and statement sequence are required")
	}
	if format == "MT940" {
		if opening == nil || closing == nil || opening.AsOf > closing.AsOf {
			return a, s, invalid("MT_BALANCE_REQUIRED", "MT940 requires ordered opening and closing balances")
		}
	} else {
		s.From, s.Through = asof, asof
		if opening != nil || closing != nil || asof == "" {
			return a, s, invalid("MT_REPORT_INVALID", "MT942 requires report date and no statement balances")
		}
	}
	var e error
	a, e = sourceAccount(o, "MT", accountID, currency, "")
	if e != nil {
		return a, s, e
	}
	a.Balances = s.Balances
	a.From = s.From
	a.Through = s.Through
	var movements, debits, credits []string
	nd, nc := 0, 0
	for _, line := range lines {
		first, extra, _ := strings.Cut(line.value, "\n")
		m := mtLine.FindStringSubmatch(first)
		if m == nil {
			return a, s, invalid("MT_TRANSACTION_INVALID", "invalid transaction record")
		}
		valueDate, e := shortDate(m[1], "YMD", o.YearPivot)
		if e != nil {
			return a, s, e
		}
		booking := asof
		if m[2] != "" {
			booking, e = mtEntryDate(m[2], asof)
			if e != nil {
				return a, s, e
			}
		} else {
			switch o.MTBookingDate {
			case "value":
				booking = valueDate
			case "statement":
			default:
				return a, s, invalid("MT_BOOKING_DATE_REQUIRED", "entry date is absent; declare mt_booking_date as statement or value")
			}
		}
		if format == "MT940" && (booking < opening.AsOf || booking > closing.AsOf) {
			return a, s, invalid("IMPORT_DATE_INVALID", "MT entry lies outside its balance interval")
		}
		amount, e := mtAmount(m[5])
		if e != nil {
			return a, s, e
		}
		sign := m[3]
		switch sign {
		case "RC":
			sign = "D"
		case "RD":
			sign = "C"
		}
		amount, e = signedDecimal(amount, sign)
		if e != nil {
			return a, s, e
		}
		if m[4] != "" && m[4] != currency[2:] {
			return a, s, invalid("MT_CURRENCY_UNSUPPORTED", "funds code differs from account currency")
		}
		owner, bank, hasBank := strings.Cut(m[7], "//")
		if len(owner) > 16 || hasBank && len(bank) > 16 {
			return a, s, invalid("MT_REFERENCE_INVALID", "transaction reference exceeds profile limit")
		}
		t := Transaction{Type: m[6], PostedDate: booking, PostedAt: booking, ValueAt: valueDate, Amount: amount, Fields: map[string]string{"debit_credit": m[3], "owner_reference": owner, "bank_reference": bank, "supplement": extra}}
		if bank != "" && bank != "NONREF" {
			t.ID = bank
		}
		t.Description = strings.TrimSpace(strings.ReplaceAll(extra, "@86:", ""))
		if t.Description == "" {
			t.Description = owner
		}
		if strings.HasPrefix(m[3], "R") {
			t.Fields["reversal"] = "true"
		}
		if format == "MT942" {
			t.Status = o.InterimStatus
			if t.Status == "" {
				t.Status = "REVIEW"
			}
		}
		a.Transactions = append(a.Transactions, t)
		movements = append(movements, amount)
		if strings.HasPrefix(amount, "-") {
			nd++
			debits = append(debits, negate(amount))
		} else {
			nc++
			credits = append(credits, amount)
		}
	}
	if opening != nil {
		parts := append([]string{opening.Amount}, movements...)
		sum, e := decimalSum(parts...)
		if e != nil || !sameAmount(sum, closing.Amount) {
			return a, s, invalid("IMPORT_BALANCE_MISMATCH", "opening balance plus movements does not equal closing balance")
		}
	}
	for _, check := range []struct {
		values    []string
		total     string
		want, got int
	}{{debits, debitTotal, debitCount, nd}, {credits, creditTotal, creditCount, nc}} {
		if check.want >= 0 && check.want != check.got {
			return a, s, invalid("IMPORT_COUNT_MISMATCH", "MT report count differs from detail")
		}
		if check.total != "" {
			sum, e := decimalSum(check.values...)
			if e != nil || !sameAmount(sum, check.total) {
				return a, s, invalid("IMPORT_TOTAL_MISMATCH", "MT report total differs from detail")
			}
		}
	}
	return a, s, nil
}
func mtEntryDate(mmdd, asof string) (string, error) {
	ref, e := time.Parse("2006-01-02", asof)
	if e != nil {
		return "", invalid("MT_DATE_INVALID", "missing reference date")
	}
	best := ""
	distance := int64(1 << 62)
	for _, year := range []int{ref.Year() - 1, ref.Year(), ref.Year() + 1} {
		v := fmt.Sprintf("%04d-%s-%s", year, mmdd[:2], mmdd[2:])
		d, e := time.Parse("2006-01-02", v)
		if e != nil {
			continue
		}
		delta := d.Unix() - ref.Unix()
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			distance = delta
			best = v
		}
	}
	if best == "" || distance > 183*24*3600 {
		return "", invalid("MT_DATE_INVALID", "ambiguous or invalid entry date")
	}
	return best, nil
}

func mtBalanceByKind(balances []Balance, kind string) Balance {
	for _, b := range balances {
		if b.Kind == kind {
			return b
		}
	}
	return Balance{}
}

func mtClock(v string, offset bool) bool {
	if len(v) != 4 || !digits(v) {
		return false
	}
	h, _ := strconv.Atoi(v[:2])
	m, _ := strconv.Atoi(v[2:])
	if offset {
		return h <= 14 && m < 60 && (h != 14 || m == 0)
	}
	return h < 24 && m < 60
}
