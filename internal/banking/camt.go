package banking

import (
	"fmt"
	"strings"
)

func camtDate(n *xmlNode) (string, error) {
	if len(n.Children) != 1 || (n.Children[0].Name.Local != "Dt" && n.Children[0].Name.Local != "DtTm") {
		return "", invalid("CAMT_DATE_INVALID", "expected one date or timestamp")
	}
	return isoDate(strings.TrimSpace(n.Children[0].Text))
}
func camtAmount(n *xmlNode, ccy string) (string, error) {
	a, e := n.one("Amt")
	if e != nil {
		return "", e
	}
	if a.attr("Ccy") != ccy {
		return "", invalid("CAMT_CURRENCY_MISMATCH", "entry or balance currency differs from the account currency")
	}
	sign, e := n.value("CdtDbtInd")
	if e != nil {
		return "", e
	}
	return signedDecimal(strings.TrimSpace(a.Text), sign)
}
func camtChoice(n *xmlNode, first, second string) (string, error) {
	if len(n.Children) != 1 {
		return "", invalid("CAMT_CHOICE_INVALID", "expected one identifier choice")
	}
	if n.Children[0].Name.Local == first {
		return strings.TrimSpace(n.Children[0].Text), nil
	}
	if n.Children[0].Name.Local == second {
		return n.Children[0].value("Id")
	}
	return "", invalid("CAMT_CHOICE_INVALID", "unsupported identifier choice")
}
func parseCAMT(data []byte, o Options) (Document, error) {
	d := Document{Format: "CAMT", Fields: map[string]string{}}
	root, e := xmlTree(data)
	if e != nil {
		return d, e
	}
	ns := root.Name.Space
	prefix := "urn:iso:std:iso:20022:tech:xsd:"
	if root.Name.Local != "Document" || !strings.HasPrefix(ns, prefix) || !root.namespace(ns) {
		return d, invalid("CAMT_NAMESPACE_INVALID", "expected a consistent ISO 20022 Document namespace")
	}
	version := strings.TrimPrefix(ns, prefix)
	parts := strings.Split(version, ".")
	if len(parts) != 4 || parts[0] != "camt" || parts[2] != "001" || (parts[3] != "02" && parts[3] != "08") {
		return d, invalid("CAMT_VERSION_UNSUPPORTED", "supported CAMT schemas are 052/053/054.001.02 and .001.08")
	}
	containerName, statementName := "", ""
	switch parts[1] {
	case "052":
		containerName, statementName = "BkToCstmrAcctRpt", "Rpt"
	case "053":
		containerName, statementName = "BkToCstmrStmt", "Stmt"
	case "054":
		containerName, statementName = "BkToCstmrDbtCdtNtfctn", "Ntfctn"
	default:
		return d, invalid("CAMT_VERSION_UNSUPPORTED", "unsupported CAMT message")
	}
	d.Version = version
	if len(root.Children) != 1 {
		return d, invalid("CAMT_STRUCTURE_INVALID", "expected one reporting message")
	}
	body, e := root.one(containerName)
	if e != nil {
		return d, e
	}
	header, e := body.one("GrpHdr")
	if e != nil {
		return d, e
	}
	header.leaves("GrpHdr", d.Fields)
	// Multi-page documents must be assembled before importing: a missing page can
	// carry movements even when all supplied entries are individually valid.
	if e = camtPagination(header, "MsgPgntn"); e != nil {
		return d, e
	}
	blocks := body.all(statementName)
	if len(blocks) == 0 || len(blocks) > MaxAccounts {
		return d, invalid("CAMT_STATEMENT_LIMIT", "invalid statement count")
	}
	indexes := map[string]int{}
	statements := map[string]bool{}
	for _, block := range blocks {
		sid, e := block.value("Id")
		if e != nil {
			return d, e
		}
		if e = camtPagination(block, "StmtPgntn"); e != nil {
			return d, e
		}
		if e = camtPagination(block, "RptPgntn"); e != nil {
			return d, e
		}
		node, e := block.one("Acct")
		if e != nil {
			return d, e
		}
		idnode, e := node.one("Id")
		if e != nil {
			return d, e
		}
		id, e := camtChoice(idnode, "IBAN", "Othr")
		if e != nil {
			return d, e
		}
		ccy, e := node.value("Ccy")
		if e != nil {
			return d, e
		}
		opts := o
		if opts.Institution == "" {
			sv, e := node.optional("Svcr")
			if e != nil {
				return d, e
			}
			if sv != nil {
				fi, e := sv.one("FinInstnId")
				if e != nil {
					return d, e
				}
				opts.Institution, e = fi.optionalValue("BICFI")
				if e != nil {
					return d, e
				}
				if opts.Institution == "" {
					opts.Institution, e = fi.optionalValue("BIC")
					if e != nil {
						return d, e
					}
				}
				if opts.Institution == "" {
					other, e := fi.optional("Othr")
					if e != nil {
						return d, e
					}
					if other != nil {
						opts.Institution, e = other.value("Id")
						if e != nil {
							return d, e
						}
					}
				}
			}
		}
		a, e := sourceAccount(opts, "CAMT", id, ccy, "")
		if e != nil {
			return d, e
		}
		key := a.Key + "/" + sid
		if statements[key] {
			return d, invalid("CAMT_DUPLICATE_STATEMENT", "repeated statement identifier")
		}
		statements[key] = true
		st := Statement{ID: sid, Fields: map[string]string{}}
		for _, ch := range block.Children {
			if ch.Name.Local != "Ntry" {
				ch.leaves(ch.Name.Local, st.Fields)
			}
		}
		rangeNode, e := block.optional("FrToDt")
		if e != nil {
			return d, e
		}
		if rangeNode != nil {
			from, e := rangeNode.value("FrDtTm")
			if e != nil {
				return d, e
			}
			st.From, e = isoDate(from)
			if e != nil {
				return d, e
			}
			through, e := rangeNode.value("ToDtTm")
			if e != nil {
				return d, e
			}
			st.Through, e = isoDate(through)
			if e != nil {
				return d, e
			}
			if st.From > st.Through {
				return d, invalid("CAMT_DATE_INVALID", "statement range is reversed")
			}
		}
		opening, closing := "", ""
		for _, bn := range block.all("Bal") {
			code, e := bn.path("Tp", "CdOrPrtry")
			if e != nil {
				return d, e
			}
			if len(code.Children) != 1 {
				return d, invalid("CAMT_BALANCE_INVALID", "expected one balance type")
			}
			kind := strings.TrimSpace(code.Children[0].Text)
			amt, e := camtAmount(bn, ccy)
			if e != nil {
				return d, e
			}
			dn, e := bn.one("Dt")
			if e != nil {
				return d, e
			}
			date, e := camtDate(dn)
			if e != nil {
				return d, e
			}
			st.Balances = append(st.Balances, Balance{Kind: kind, Amount: amt, AsOf: date})
			if kind == "OPBD" {
				if opening != "" {
					return d, invalid("CAMT_BALANCE_INVALID", "repeated opening booked balance")
				}
				opening = amt
			}
			if kind == "CLBD" {
				if closing != "" {
					return d, invalid("CAMT_BALANCE_INVALID", "repeated closing booked balance")
				}
				closing = amt
			}
		}
		movement := "0.00"
		for _, entry := range block.all("Ntry") {
			t := Transaction{Fields: map[string]string{}, Type: "CAMT"}
			entry.leaves("Ntry", t.Fields)
			t.Amount, e = camtAmount(entry, ccy)
			if e != nil {
				return d, e
			}
			status, e := entry.one("Sts")
			if e != nil {
				return d, e
			}
			sv := strings.TrimSpace(status.Text)
			if len(status.Children) > 0 {
				sv, e = status.value("Cd")
				if e != nil {
					return d, e
				}
			}
			switch sv {
			case "BOOK":
				t.Status = "POSTED"
			case "PDNG":
				t.Status = "PENDING"
			case "INFO", "FUTR":
				t.Status = "REVIEW"
			default:
				return d, invalid("CAMT_STATUS_UNSUPPORTED", "unsupported entry status")
			}
			booking, e := entry.optional("BookgDt")
			if e != nil {
				return d, e
			}
			if booking != nil {
				t.PostedDate, e = camtDate(booking)
				if e != nil {
					return d, e
				}
			} else {
				return d, invalid("CAMT_BOOKING_DATE_REQUIRED", "entry lacks a booking date; provide a report with explicit booking dates")
			}
			if st.From != "" && (t.PostedDate < st.From || t.PostedDate > st.Through) {
				return d, invalid("CAMT_DATE_INVALID", "entry lies outside statement date range")
			}
			value, e := entry.optional("ValDt")
			if e != nil {
				return d, e
			}
			if value != nil {
				t.ValueAt, e = camtDate(value)
				if e != nil {
					return d, e
				}
			}
			// Account-servicer reference is the bank's entry identifier. NtryRef and
			// end-to-end IDs are retained but cannot safely stand in for bank identity.
			t.ID, e = entry.optionalValue("AcctSvcrRef")
			if e != nil {
				return d, e
			}
			if t.ID == "NOTPROVIDED" || t.ID == "NONREF" {
				t.ID = ""
			}
			t.Description, e = entry.optionalValue("AddtlNtryInf")
			if e != nil {
				return d, e
			}
			code, e := entry.optional("BkTxCd")
			if e != nil {
				return d, e
			}
			if code != nil {
				t.Type = "CAMT:" + camtText(code)
			}
			for _, details := range entry.all("NtryDtls") {
				for _, tx := range details.all("TxDtls") {
					detail := Detail{Fields: map[string]string{}}
					tx.leaves("TxDtls", detail.Fields)
					refs, e := tx.optional("Refs")
					if e != nil {
						return d, e
					}
					if refs != nil {
						detail.ID, e = refs.optionalValue("AcctSvcrRef")
						if e != nil {
							return d, e
						}
					}
					detail.Description = camtTextNamed(tx, "RmtInf")
					if t.Description == "" {
						t.Description = detail.Description
					}
					amount, e := tx.optional("Amt")
					if e != nil {
						return d, e
					}
					if amount != nil {
						detail.Amount, e = decimal(strings.TrimSpace(amount.Text))
						if e != nil {
							return d, e
						}
						detail.Currency = amount.attr("Ccy")
					}
					if amount == nil {
						amtDetails, e := tx.optional("AmtDtls")
						if e != nil {
							return d, e
						}
						if amtDetails != nil {
							txAmt, e := amtDetails.optional("TxAmt")
							if e != nil {
								return d, e
							}
							if txAmt != nil {
								an, e := txAmt.one("Amt")
								if e != nil {
									return d, e
								}
								detail.Amount, e = decimal(strings.TrimSpace(an.Text))
								if e != nil {
									return d, e
								}
								detail.Currency = an.attr("Ccy")
							}
						}
					}
					t.Details = append(t.Details, detail)
				}
			}
			// Reversal indicator describes the movement; CdtDbtInd already supplies
			// its direction, so a reversal must never invert this amount a second time.
			rev, e := entry.optionalValue("RvslInd")
			if e != nil {
				return d, e
			}
			if rev != "" && rev != "true" && rev != "false" && rev != "0" && rev != "1" {
				return d, invalid("CAMT_REVERSAL_INVALID", "invalid reversal indicator")
			}
			if t.Status == "POSTED" {
				movement, e = decimalSum(movement, t.Amount)
				if e != nil {
					return d, e
				}
			}
			a.Transactions = append(a.Transactions, t)
		}
		if opening != "" && closing != "" {
			sum, e := decimalSum(opening, movement)
			if e != nil {
				return d, e
			}
			if !sameAmount(sum, closing) {
				return d, invalid("CAMT_BALANCE_MISMATCH", "opening booked balance plus booked entries does not equal closing booked balance")
			}
		}
		a.From, a.Through = st.From, st.Through
		a.Balances = st.Balances
		a.Statements = []Statement{st}
		if index, ok := indexes[a.Key]; ok {
			prior := &d.Accounts[index]
			prior.Transactions = append(prior.Transactions, a.Transactions...)
			prior.Statements = append(prior.Statements, st)
			prior.Balances = append(prior.Balances, st.Balances...)
			if prior.From == "" || a.From < prior.From {
				prior.From = a.From
			}
			if a.Through > prior.Through {
				prior.Through = a.Through
			}
		} else {
			indexes[a.Key] = len(d.Accounts)
			d.Accounts = append(d.Accounts, a)
		}
	}
	return d, nil
}
func camtPagination(n *xmlNode, name string) error {
	p, e := n.optional(name)
	if e != nil || p == nil {
		return e
	}
	number, e := p.value("PgNb")
	if e != nil {
		return e
	}
	last, e := p.value("LastPgInd")
	if e != nil {
		return e
	}
	if number != "1" || (last != "true" && last != "1") {
		return invalid("CAMT_PAGINATION_INCOMPLETE", fmt.Sprintf("%s must describe one complete page; assemble the complete report first", name))
	}
	return nil
}
func camtText(n *xmlNode) string {
	if len(n.Children) == 0 {
		return strings.TrimSpace(n.Text)
	}
	var parts []string
	for _, c := range n.Children {
		v := camtText(c)
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}
func camtTextNamed(n *xmlNode, name string) string {
	var parts []string
	for _, c := range n.all(name) {
		parts = append(parts, camtText(c))
	}
	return strings.Join(parts, " ")
}
