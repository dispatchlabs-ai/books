package banking

import (
	"fmt"
	"strconv"
	"strings"
)

func codaAmount(v, sign string) (string, error) {
	switch sign {
	case "0":
		sign = "C"
	case "1":
		sign = "D"
	default:
		return "", invalid("CODA_SIGN_INVALID", "invalid CODA amount sign")
	}
	return fixedSigned(v, sign, 3)
}
func codaAccount(v, structure string) (string, string, error) {
	switch structure {
	case "0":
		if !digits(v[:12]) {
			return "", "", invalid("CODA_ACCOUNT_INVALID", "invalid Belgian BBAN")
		}
		return v[:12], strings.TrimSpace(v[13:16]), nil
	case "1", "3":
		return strings.TrimSpace(v[:34]), v[34:37], nil
	case "2":
		return strings.TrimSpace(v[:31]), v[34:37], nil
	default:
		return "", "", invalid("CODA_ACCOUNT_INVALID", "unknown account structure")
	}
}
func codaLink(r, next string) error {
	if len(r) != 128 {
		return invalid("CODA_RECORD_INVALID", "invalid record length")
	}
	kind := r[:1]
	switch kind {
	case "2", "3":
		if (r[125] != '0' && r[125] != '1') || (r[127] != '0' && r[127] != '1') {
			return invalid("CODA_LINK_INVALID", "invalid next/link code")
		}
		continuation := false
		if len(next) > 1 {
			continuation = next[0] == r[0] && next[1] > r[1] && next[1] <= '3' && next[2:10] == r[2:10]
			if kind == "3" && r[1] == '1' && next[1] != '2' {
				continuation = false
			}
		}
		if (r[125] == '1') != continuation {
			return invalid("CODA_LINK_INVALID", "missing or unexpected continuation record")
		}
		if !continuation {
			information := strings.HasPrefix(next, "31")
			if (r[127] == '1') != information {
				return invalid("CODA_LINK_INVALID", "missing or unexpected information record")
			}
		}
	case "8", "4":
		if (r[127] != '0' && r[127] != '1') || ((r[127] == '1') != strings.HasPrefix(next, "4")) {
			return invalid("CODA_LINK_INVALID", "free communication link mismatch")
		}
	}
	return nil
}
func parseCODA(data []byte, o Options) (Document, error) {
	d := Document{Format: "CODA", Version: "2.7", Fields: map[string]string{}}
	lines, e := fixedRecords(data, 128)
	if e != nil {
		return d, e
	}
	var a *Account
	var st Statement
	header, accountHeader := "", ""
	inFile, closed, ended, separate := false, false, false, false
	fileNumber, count := 0, 0
	debit, credit := "0.00", "0.00"
	sequence, detailNumber := 0, 0
	parentType, activeSeq := "", ""
	activeDetail := -1
	infoNumber := 0
	globalLevels := []byte{}
	parentDetailSum := "0.00"
	hasDirectDetails := false
	parentIndex := -1
	subtotal, nestedSum := "", "0.00"
	hasNested := false
	checkNested := func() error {
		if subtotal != "" && hasNested && !sameAmount(subtotal, nestedSum) {
			return invalid("CODA_DETAIL_TOTAL_MISMATCH", "nested detail amounts differ from their subtotal")
		}
		return nil
	}
	checkDetails := func() error {
		if e := checkNested(); e != nil {
			return e
		}
		if a != nil && parentIndex >= 0 && hasDirectDetails && !sameAmount(parentDetailSum, a.Transactions[parentIndex].Amount) {
			return invalid("CODA_DETAIL_TOTAL_MISMATCH", "direct detail amounts differ from their parent movement")
		}
		return nil
	}
	for i, r := range lines {
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		if ended {
			return d, invalid("CODA_STRUCTURE_INVALID", "data follows the final file trailer")
		}
		if e = codaLink(r, next); e != nil {
			return d, e
		}
		switch r[0] {
		case '0':
			if inFile || a != nil || r[1:5] != "0000" || r[14:16] != "05" || r[127] != '2' {
				return d, invalid("CODA_HEADER_INVALID", "invalid CODA v2 header")
			}
			if _, e = shortDate(r[5:11], "DMY", o.YearPivot); e != nil {
				return d, e
			}
			if r[16] != ' ' && r[16] != 'D' {
				return d, invalid("CODA_HEADER_INVALID", "invalid duplicate marker")
			}
			if !digits(r[83:88]) {
				return d, invalid("CODA_HEADER_INVALID", "invalid separate application code")
			}
			header = r
			separate = r[83:88] != "00000"
			inFile = true
			closed = false
			fileNumber++
			count = 0
			debit, credit = "0.00", "0.00"
			d.Fields[fmt.Sprintf("file:%d", fileNumber)], e = fixedText(r, o)
			if e != nil {
				return d, e
			}
		case '1':
			if !inFile || a != nil || closed {
				return d, invalid("CODA_STRUCTURE_INVALID", "invalid old balance position")
			}
			accountHeader = r
			id, ccy, e := codaAccount(r[5:42], r[1:2])
			if e != nil {
				return d, e
			}
			opts := o
			if opts.Institution == "" {
				opts.Institution = strings.TrimSpace(header[60:71])
				if opts.Institution == "" {
					opts.Institution = header[11:14]
				}
			}
			account, e := sourceAccount(opts, "CODA", id, ccy, "")
			if e != nil {
				return d, e
			}
			a = &account
			date, e := shortDate(r[58:64], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			amount, e := codaAmount(r[43:58], r[42:43])
			if e != nil {
				return d, e
			}
			st = Statement{ID: fmt.Sprintf("%s:%s:%s", header[5:11], r[125:128], r[2:5]), From: date, Balances: []Balance{{Kind: "OPENING", Amount: amount, AsOf: date}}, Fields: map[string]string{"separate_application": header[83:88], "duplicate": strings.TrimSpace(header[16:17])}}
			sequence, detailNumber = 0, 0
			parentIndex = -1
			activeDetail = -1
			parentType = ""
			activeSeq = ""
			globalLevels = nil
			hasDirectDetails = false
			parentDetailSum = "0.00"
			subtotal, nestedSum, hasNested = "", "0.00", false
			count++
		case '2':
			if a == nil || closed || r[1] < '1' || r[1] > '3' {
				return d, invalid("CODA_STRUCTURE_INVALID", "movement outside an open account")
			}
			count++
			if r[1] == '1' {
				if !digits(r[2:10]) || !digits(r[53:61]) {
					return d, invalid("CODA_MOVEMENT_INVALID", "invalid sequence or transaction code")
				}
				seq, _ := strconv.Atoi(r[2:6])
				dn, _ := strconv.Atoi(r[6:10])
				typ := r[53:54]
				amount, e := codaAmount(r[32:47], r[31:32])
				if e != nil {
					return d, e
				}
				date, e := shortDate(r[115:121], "DMY", o.YearPivot)
				if e != nil {
					return d, e
				}
				value := ""
				if r[47:53] != "000000" {
					value, e = shortDate(r[47:53], "DMY", o.YearPivot)
					if e != nil {
						return d, e
					}
				}
				if r[61] != '0' && r[61] != '1' {
					return d, invalid("CODA_COMMUNICATION_INVALID", "invalid communication type")
				}
				text, e := fixedText(r[62:115], o)
				if e != nil {
					return d, e
				}
				raw, e := fixedText(r, o)
				if e != nil {
					return d, e
				}
				fields := map[string]string{"record": raw, "bank_reference": strings.TrimSpace(r[10:31]), "sequence": r[2:6], "detail_number": r[6:10], "transaction_type": typ, "communication_type": r[61:62]}
				if dn == 0 {
					if e = checkDetails(); e != nil {
						return d, e
					}
					if seq != (sequence+1)%10000 {
						return d, invalid("CODA_SEQUENCE_INVALID", "movement sequence is not contiguous")
					}
					sequence = seq
					detailNumber = 0
					parentType = typ
					activeSeq = r[2:6]
					if !strings.Contains("0123", typ) && !separate {
						return d, invalid("CODA_HIERARCHY_INVALID", "detail movement has no parent total")
					}
					status := "POSTED"
					if separate || header[16] == 'D' {
						status = "REVIEW"
					}
					a.Transactions = append(a.Transactions, Transaction{Type: r[53:61], PostedDate: date, ValueAt: value, Amount: amount, Description: text, Status: status, Fields: fields})
					parentIndex = len(a.Transactions) - 1
					activeDetail = -1
					hasDirectDetails = false
					parentDetailSum = "0.00"
					subtotal, nestedSum, hasNested = "", "0.00", false
					if typ == "7" {
						subtotal = amount
					}
					if r[31] == '1' {
						debit, e = decimalSum(debit, negate(amount))
					} else {
						credit, e = decimalSum(credit, amount)
					}
					if e != nil {
						return d, e
					}
				} else {
					if parentIndex < 0 || r[2:6] != activeSeq || dn != (detailNumber+1)%10000 {
						return d, invalid("CODA_SEQUENCE_INVALID", "detail sequence does not match its parent")
					}
					detailNumber = dn
					allowed := ""
					switch parentType {
					case "1":
						allowed = "5"
					case "2":
						allowed = "679"
					case "3":
						allowed = "8"
					case "7":
						if separate {
							allowed = "9"
						}
					}
					if !strings.Contains(allowed, typ) || allowed == "" {
						return d, invalid("CODA_HIERARCHY_INVALID", "detail type does not belong to its parent")
					}
					if typ == "9" {
						if subtotal == "" {
							return d, invalid("CODA_HIERARCHY_INVALID", "nested detail lacks an active subtotal")
						}
						hasNested = true
						nestedSum, e = decimalSum(nestedSum, amount)
						if e != nil {
							return d, e
						}
					} else {
						if e = checkNested(); e != nil {
							return d, e
						}
						subtotal, nestedSum, hasNested = "", "0.00", false
						if typ == "7" {
							subtotal = amount
						}
						hasDirectDetails = true
						parentDetailSum, e = decimalSum(parentDetailSum, amount)
						if e != nil {
							return d, e
						}
					}
					t := &a.Transactions[parentIndex]
					t.Details = append(t.Details, Detail{Amount: amount, Currency: a.Currency, Description: text, Fields: fields})
					activeDetail = len(t.Details) - 1
				}
				level := r[124]
				if level < '0' || level > '9' {
					return d, invalid("CODA_HIERARCHY_INVALID", "invalid globalization level")
				}
				if level != '0' {
					if len(globalLevels) > 0 && globalLevels[len(globalLevels)-1] == level {
						globalLevels = globalLevels[:len(globalLevels)-1]
					} else {
						if len(globalLevels) > 0 && level <= globalLevels[len(globalLevels)-1] {
							return d, invalid("CODA_HIERARCHY_INVALID", "unordered globalization nesting")
						}
						globalLevels = append(globalLevels, level)
					}
				}
				infoNumber = 0
			} else {
				if parentIndex < 0 || i == 0 || lines[i-1][0] != '2' || r[2:10] != lines[i-1][2:10] {
					return d, invalid("CODA_LINK_INVALID", "orphan movement continuation")
				}
				text := ""
				if r[1] == '2' {
					text, e = fixedText(r[10:63], o)
				} else {
					text, e = fixedText(r[82:125], o)
				}
				if e != nil {
					return d, e
				}
				raw, e := fixedText(r, o)
				if e != nil {
					return d, e
				}
				t := &a.Transactions[parentIndex]
				if activeDetail >= 0 {
					detail := &t.Details[activeDetail]
					detail.Description = strings.TrimSpace(detail.Description + " " + text)
					detail.Fields["continuation:"+r[1:2]] = raw
				} else {
					t.Description = strings.TrimSpace(t.Description + " " + text)
					t.Fields["continuation:"+r[1:2]] = raw
				}
			}
		case '3':
			if a == nil || closed || parentIndex < 0 || r[1] < '1' || r[1] > '3' || r[2:6] != activeSeq {
				return d, invalid("CODA_LINK_INVALID", "orphan information record")
			}
			count++
			if r[1] == '1' {
				fields := a.Transactions[parentIndex].Fields
				if activeDetail >= 0 {
					fields = a.Transactions[parentIndex].Details[activeDetail].Fields
				}
				if strings.TrimSpace(r[10:31]) != fields["bank_reference"] {
					return d, invalid("CODA_LINK_INVALID", "information bank reference differs from its movement")
				}
				infoNumber++
			} else if i == 0 || lines[i-1][0] != '3' || r[2:10] != lines[i-1][2:10] {
				return d, invalid("CODA_LINK_INVALID", "orphan information continuation")
			}
			raw, e := fixedText(r, o)
			if e != nil {
				return d, e
			}
			t := &a.Transactions[parentIndex]
			key := fmt.Sprintf("information:%d:%c", infoNumber, r[1])
			if activeDetail >= 0 {
				t.Details[activeDetail].Fields[key] = raw
			} else {
				t.Fields[key] = raw
			}
		case '8':
			if a == nil || closed || r[4:41] != accountHeader[5:42] {
				return d, invalid("CODA_ACCOUNT_MISMATCH", "closing account does not match opening account")
			}
			if e = checkDetails(); e != nil {
				return d, e
			}
			if len(globalLevels) != 0 {
				return d, invalid("CODA_HIERARCHY_INVALID", "unclosed globalization")
			}
			date, e := shortDate(r[57:63], "DMY", o.YearPivot)
			if e != nil {
				return d, e
			}
			if date < st.From {
				return d, invalid("CODA_DATE_INVALID", "closing precedes opening")
			}
			amount, e := codaAmount(r[42:57], r[41:42])
			if e != nil {
				return d, e
			}
			expected, e := decimalSum(st.Balances[0].Amount, credit, negate(debit))
			if e != nil {
				return d, e
			}
			if !separate && !sameAmount(amount, expected) {
				return d, invalid("CODA_BALANCE_MISMATCH", "opening balance plus movements differs from closing balance")
			}
			for _, t := range a.Transactions {
				if t.PostedDate < st.From || t.PostedDate > date {
					return d, invalid("CODA_DATE_INVALID", "movement outside statement period")
				}
			}
			st.Through = date
			st.Balances = append(st.Balances, Balance{Kind: "CLOSING", Amount: amount, AsOf: date})
			closed = true
			count++
		case '4':
			if a == nil || !closed {
				return d, invalid("CODA_STRUCTURE_INVALID", "free communication before closing balance")
			}
			text, e := fixedText(r[32:112], o)
			if e != nil {
				return d, e
			}
			st.Fields["communication:"+r[2:10]] = text
		case '9':
			if !inFile || a == nil || !closed {
				return d, invalid("CODA_STRUCTURE_INVALID", "trailer before closing balance")
			}
			db, e := fixedDecimal(r[22:37], 3)
			if e != nil {
				return d, e
			}
			cr, e := fixedDecimal(r[37:52], 3)
			if e != nil {
				return d, e
			}
			if !baiCount(r[16:22], count) || !sameAmount(db, debit) || !sameAmount(cr, credit) {
				return d, invalid("CODA_CONTROL_MISMATCH", "record count or debit/credit total mismatch")
			}
			if r[127] != '1' && r[127] != '2' {
				return d, invalid("CODA_TRAILER_INVALID", "invalid multiple-file code")
			}
			if r[127] == '1' && !strings.HasPrefix(next, "0") {
				return d, invalid("CODA_STRUCTURE_INVALID", "missing next CODA file")
			}
			a.From, a.Through = st.From, st.Through
			a.Balances = st.Balances
			a.Statements = []Statement{st}
			appendFixedAccount(&d, *a)
			a = nil
			inFile = false
			ended = r[127] == '2'
		default:
			return d, invalid("CODA_RECORD_UNSUPPORTED", "unknown CODA record code")
		}
	}
	if !ended {
		return d, invalid("CODA_STRUCTURE_INVALID", "missing final trailer")
	}
	return d, nil
}
