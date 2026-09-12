package banking

import (
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/money"
	"math/big"
	"strconv"
	"strings"
)

type baiRecord struct {
	text         string
	first, count int
}

func baiRecords(data []byte, o Options) ([]baiRecord, error) {
	text, e := textInput(data, o)
	if e != nil {
		return nil, e
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var records []baiRecord
	physical := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \r")
		if line == "" {
			continue
		}
		physical++
		if physical > MaxNodes || len(line) > MaxText {
			return nil, invalid("BAI_LIMIT_EXCEEDED", "BAI record limit exceeded")
		}
		if len(line) < 3 || line[2] != ',' {
			return nil, invalid("BAI_RECORD_INVALID", "expected a two-digit record code and comma")
		}
		if strings.HasPrefix(line, "88,") {
			if len(records) == 0 {
				return nil, invalid("BAI_CONTINUATION_INVALID", "orphan continuation")
			}
			prior := &records[len(records)-1]
			if strings.HasSuffix(prior.text, "/") && !baiHasText(prior.text) {
				prior.text = strings.TrimSuffix(prior.text, "/") + "," + line[3:]
			} else {
				prior.text += "\n" + line[3:]
			}
			prior.count++
			if len(prior.text) > MaxText {
				return nil, invalid("BAI_LIMIT_EXCEEDED", "logical record too long")
			}
		} else {
			records = append(records, baiRecord{line, physical, 1})
		}
	}
	return records, nil
}
func baiHasText(s string) bool {
	if !strings.HasPrefix(s, "16,") {
		return false
	}
	f := strings.Split(s, ",")
	if len(f) < 4 {
		return false
	}
	n := 4
	switch f[3] {
	case "S":
		n += 3
	case "V":
		n += 2
	case "D":
		if len(f) < 5 {
			return false
		}
		c, e := strconv.Atoi(f[4])
		if e != nil || c < 0 || c > 1000 {
			return false
		}
		n += 1 + 2*c
	}
	return len(f) > n+2 && f[n+2] != "/"
}
func baiInteger(v string) (*big.Int, error) {
	if strings.HasPrefix(v, "+") || strings.HasPrefix(v, "-") {
		if len(v) < 2 {
			return nil, invalid("BAI_AMOUNT_INVALID", "invalid control amount")
		}
	}
	if !digits(strings.TrimLeft(v, "+-")) || len(v) > 31 {
		return nil, invalid("BAI_AMOUNT_INVALID", "invalid control amount")
	}
	n, ok := new(big.Int).SetString(v, 10)
	if !ok {
		return nil, invalid("BAI_AMOUNT_INVALID", "invalid integer")
	}
	return n, nil
}
func baiCount(v string, n int) bool { x, e := strconv.Atoi(v); return e == nil && x == n && digits(v) }
func currencyScale(ccy string) (int, error) {
	if value, err := money.Lookup(ccy); err == nil {
		return value.Scale(), nil
	}
	return 0, invalid("IMPORT_CURRENCY_SCALE_UNSUPPORTED", "currency has no supported implied-decimal scale")
}

func baiFunds(f []string, index int, o Options, format string) (int, string, error) {
	if index >= len(f) {
		return index, "", invalid("BAI_FUNDS_INVALID", "missing funds type")
	}
	kind := f[index]
	next := index + 1
	value := ""
	count := 0
	switch kind {
	case "", "0", "1", "2", "Z":
	case "S":
		count = 3
	case "V":
		count = 2
	case "D":
		if format == "BTRS" {
			return next, "", invalid("BAI_FUNDS_UNSUPPORTED", "BTRS retires funds type D")
		}
		if next >= len(f) {
			return next, "", invalid("BAI_FUNDS_INVALID", "missing distribution count")
		}
		n, e := strconv.Atoi(f[next])
		if e != nil || n < 1 || n > 1000 {
			return next, "", invalid("BAI_FUNDS_INVALID", "invalid distribution count")
		}
		next++
		count = n * 2
	default:
		return next, "", invalid("BAI_FUNDS_INVALID", "unknown funds type")
	}
	if next+count > len(f) {
		return next, "", invalid("BAI_FUNDS_INVALID", "truncated funds distribution")
	}
	for i := 0; i < count; i++ {
		v := f[next+i]
		if kind == "V" {
			if i == 0 {
				var e error
				value, e = shortDate(v, "YMD", o.YearPivot)
				if e != nil {
					return next, "", e
				}
			} else if !baiTime(v, true) {
				return next, "", invalid("BAI_TIME_INVALID", "invalid funds value time")
			}
		} else if _, e := baiInteger(v); e != nil {
			return next, "", e
		}
	}
	return next + count, value, nil
}
func baiTime(v string, optional bool) bool {
	if v == "" {
		return optional
	}
	if v == "2400" || v == "9999" {
		return true
	}
	if len(v) != 4 || !digits(v) {
		return false
	}
	h, _ := strconv.Atoi(v[:2])
	m, _ := strconv.Atoi(v[2:])
	return h < 24 && m < 60
}
func parseBAI(data []byte, o Options, format string) (Document, error) {
	d := Document{Format: format, Fields: map[string]string{}}
	records, e := baiRecords(data, o)
	if e != nil {
		return d, e
	}
	if len(records) < 5 {
		return d, invalid("BAI_STRUCTURE_INVALID", "incomplete BAI file")
	}
	fileSum, groupSum, acctSum := new(big.Int), new(big.Int), new(big.Int)
	groupStart, accountStart, groups, accounts := 0, 0, 0, 0
	var account *Account
	var statement Statement
	groupDate, groupCCY, institution, status := "", "", "", ""
	seenFile, ended, inGroup := false, false, false
	indexes := map[string]int{}
	for _, r := range records {
		if ended {
			return d, invalid("BAI_STRUCTURE_INVALID", "records after file trailer")
		}
		isText := baiHasText(r.text)
		if !isText && !strings.HasSuffix(r.text, "/") {
			return d, invalid("BAI_RECORD_INVALID", "non-text record lacks its slash terminator")
		}
		raw := r.text
		if !isText {
			raw = strings.TrimSuffix(raw, "/")
		}
		f := strings.Split(raw, ",")
		code := f[0]
		switch code {
		case "01":
			if seenFile || r.first != 1 || len(f) != 9 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid file header")
			}
			seenFile = true
			want := "2"
			if format == "BTRS" {
				want = "3"
			}
			if f[8] != want {
				return d, invalid("BAI_VERSION_UNSUPPORTED", "file version differs from selected format")
			}
			d.Version = want
			if f[1] == "" || f[2] == "" || f[5] == "" {
				return d, invalid("BAI_HEADER_INVALID", "missing sender, receiver, or file identifier")
			}
			if _, e = shortDate(f[3], "YMD", o.YearPivot); e != nil {
				return d, e
			}
			if !baiTime(f[4], false) {
				return d, invalid("BAI_TIME_INVALID", "invalid creation time")
			}
			for _, v := range f[6:8] {
				if v != "" && !digits(v) {
					return d, invalid("BAI_HEADER_INVALID", "invalid physical record/block length")
				}
			}
			d.Fields["file_header"] = raw
		case "02":
			if !seenFile || inGroup || account != nil || len(f) != 8 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid group header")
			}
			if f[3] != "1" {
				return d, invalid("BAI_GROUP_STATUS_UNSUPPORTED", "only update groups can be imported; deletion, correction, and test groups require a separate correction workflow")
			}
			if f[2] == "" {
				return d, invalid("BAI_HEADER_INVALID", "missing group originator")
			}
			groupDate, e = shortDate(f[4], "YMD", o.YearPivot)
			if e != nil {
				return d, e
			}
			if !baiTime(f[5], true) {
				return d, invalid("BAI_TIME_INVALID", "invalid group time")
			}
			groupCCY = f[6]
			if format == "BTRS" && groupCCY != "" {
				return d, invalid("BAI_CURRENCY_INVALID", "BTRS currency belongs on account records")
			}
			if groupCCY == "" {
				groupCCY = "USD"
			}
			institution = f[2]
			if o.Institution != "" {
				institution = o.Institution
			}
			status = "REVIEW"
			if f[7] == "2" {
				status = "POSTED"
			} else if o.InterimStatus != "" {
				status = o.InterimStatus
			}
			if f[7] != "" && f[7] != "1" && f[7] != "2" && f[7] != "3" && f[7] != "4" {
				return d, invalid("BAI_HEADER_INVALID", "invalid as-of modifier")
			}
			inGroup = true
			groupStart = r.first
			groupSum.SetInt64(0)
			accounts = 0
			groups++
			d.Fields[fmt.Sprintf("group:%d", groups)] = raw
		case "03":
			if !inGroup || account != nil || len(f) < 3 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid account header")
			}
			ccy := f[2]
			if ccy == "" {
				if format == "BTRS" {
					return d, invalid("BAI_CURRENCY_INVALID", "BTRS account currency is required")
				}
				ccy = groupCCY
			}
			scale, e := currencyScale(ccy)
			if e != nil {
				return d, e
			}
			opts := o
			opts.Institution = institution
			a, e := sourceAccount(opts, format, f[1], ccy, "")
			if e != nil {
				return d, e
			}
			account = &a
			accountStart = r.first
			acctSum.SetInt64(0)
			statement = Statement{ID: fmt.Sprintf("%s:%d:%s", d.Fields["file_header"], groups, f[1]), From: groupDate, Through: groupDate, Fields: map[string]string{"account_header": raw}}
			for i := 3; i < len(f); {
				if len(f)-i < 4 {
					return d, invalid("BAI_SUMMARY_INVALID", "incomplete status or summary tuple")
				}
				tc, amount, items := f[i], f[i+1], f[i+2]
				if tc == "" && amount == "" && items == "" && f[i+3] == "" {
					i += 4
					continue
				}
				isStatus := strings.Contains(baiStatusCodes, " "+tc+" ")
				if !isStatus && !strings.Contains(baiSummaryCodes, " "+tc+" ") {
					return d, invalid("BAI_CODE_UNSUPPORTED", "unrecognized status or summary code "+tc)
				}
				n, e := baiInteger(amount)
				if e != nil {
					return d, e
				}
				if !isStatus && n.Sign() < 0 {
					return d, invalid("BAI_AMOUNT_INVALID", "summary amounts must be unsigned")
				}
				if items != "" && !digits(items) {
					return d, invalid("BAI_SUMMARY_INVALID", "invalid item count")
				}
				if format == "BTRS" && isStatus && (items != "" || f[i+3] != "") {
					return d, invalid("BAI_SUMMARY_INVALID", "BTRS status records cannot include count or funds type")
				}
				acctSum.Add(acctSum, n)
				normalized, e := fixedDecimal(amount, scale)
				if e != nil {
					return d, e
				}
				if isStatus {
					statement.Balances = append(statement.Balances, Balance{Kind: tc, Amount: normalized, AsOf: groupDate})
				}
				i, _, e = baiFunds(f, i+3, o, format)
				if e != nil {
					return d, e
				}
			}
		case "16":
			if account == nil || len(f) < 4 {
				return d, invalid("BAI_STRUCTURE_INVALID", "detail outside an account")
			}
			tc := f[1]
			next, value, e := baiFunds(f, 3, o, format)
			if e != nil {
				return d, e
			}
			if len(f) < next+3 {
				return d, invalid("BAI_DETAIL_INVALID", "missing reference/text delimiters")
			}
			bankRef, customerRef, description := f[next], f[next+1], strings.Join(f[next+2:], ",")
			if description == "/" {
				description = ""
			}
			if tc == "890" {
				if f[2] != "" || f[3] != "" {
					return d, invalid("BAI_DETAIL_INVALID", "non-monetary code has an amount or funds type")
				}
				statement.Fields[fmt.Sprintf("message:%d", r.first)] = description
				continue
			}
			if !strings.Contains(baiDetailCodes, " "+tc+" ") {
				return d, invalid("BAI_CODE_UNSUPPORTED", "unrecognized or custom detail code "+tc)
			}
			n, e := baiInteger(f[2])
			if e != nil {
				return d, e
			}
			if n.Sign() < 0 {
				return d, invalid("BAI_AMOUNT_INVALID", "detail amount must be unsigned")
			}
			acctSum.Add(acctSum, n)
			scale, e := currencyScale(account.Currency)
			if e != nil {
				return d, e
			}
			amount, e := fixedDecimal(f[2], scale)
			if e != nil {
				return d, e
			}
			codeNumber, _ := strconv.Atoi(tc)
			if codeNumber >= 400 {
				amount = negate(amount)
			}
			// References have originator-defined meaning in BAI2; preserve them as
			// evidence but use file identity rather than assume global uniqueness.
			t := Transaction{Type: tc, PostedDate: groupDate, ValueAt: value, Amount: amount, Description: description, Status: status, Fields: map[string]string{"record": raw, "bank_reference": bankRef, "customer_reference": customerRef}}
			account.Transactions = append(account.Transactions, t)
		case "49":
			if account == nil || len(f) != 3 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid account trailer")
			}
			sum, e := baiInteger(f[1])
			if e != nil {
				return d, e
			}
			if sum.Cmp(acctSum) != 0 || !baiCount(f[2], r.first+r.count-accountStart) {
				return d, invalid("BAI_CONTROL_MISMATCH", "account control total or physical record count mismatch")
			}
			account.From, account.Through = groupDate, groupDate
			account.Balances = statement.Balances
			account.Statements = []Statement{statement}
			if index, ok := indexes[account.Key]; ok {
				prior := &d.Accounts[index]
				prior.Transactions = append(prior.Transactions, account.Transactions...)
				prior.Statements = append(prior.Statements, statement)
				prior.Balances = append(prior.Balances, statement.Balances...)
			} else {
				indexes[account.Key] = len(d.Accounts)
				d.Accounts = append(d.Accounts, *account)
			}
			groupSum.Add(groupSum, acctSum)
			account = nil
			accounts++
		case "98":
			if !inGroup || account != nil || len(f) != 4 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid group trailer")
			}
			sum, e := baiInteger(f[1])
			if e != nil {
				return d, e
			}
			if sum.Cmp(groupSum) != 0 || !baiCount(f[2], accounts) || !baiCount(f[3], r.first+r.count-groupStart) {
				return d, invalid("BAI_CONTROL_MISMATCH", "group control total or count mismatch")
			}
			fileSum.Add(fileSum, groupSum)
			inGroup = false
		case "99":
			if inGroup || account != nil || !seenFile || len(f) != 4 {
				return d, invalid("BAI_STRUCTURE_INVALID", "invalid file trailer")
			}
			sum, e := baiInteger(f[1])
			if e != nil {
				return d, e
			}
			if sum.Cmp(fileSum) != 0 || !baiCount(f[2], groups) || !baiCount(f[3], r.first+r.count-1) {
				return d, invalid("BAI_CONTROL_MISMATCH", "file control total or count mismatch")
			}
			ended = true
		default:
			return d, invalid("BAI_RECORD_UNSUPPORTED", "unsupported record code")
		}
	}
	if !ended {
		return d, invalid("BAI_STRUCTURE_INVALID", "missing file trailer")
	}
	return d, nil
}
