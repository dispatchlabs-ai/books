package banking

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dispatchlabs-ai/books/internal/money"
)

type node struct {
	name, text string
	version    string
	children   []*node
}

func (n *node) all(name string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}
func (n *node) one(name string) (*node, error) {
	found := n.all(name)
	if len(found) != 1 {
		return nil, invalid("OFX_FIELD_INVALID", "expected exactly one "+name)
	}
	return found[0], nil
}
func (n *node) optional(name string) (string, error) {
	f := n.all(name)
	if len(f) > 1 || (len(f) == 1 && len(f[0].children) > 0) {
		return "", invalid("OFX_FIELD_INVALID", "invalid or repeated "+name)
	}
	if len(f) == 0 {
		return "", nil
	}
	return strings.TrimSpace(f[0].text), nil
}
func (n *node) required(name string) (string, error) {
	v, e := n.optional(name)
	if e != nil {
		return "", e
	}
	if v == "" {
		return "", invalid("OFX_FIELD_REQUIRED", name+" is required")
	}
	return v, nil
}
func allowed(n *node, names string) error {
	allow := map[string]bool{}
	for _, s := range strings.Fields(names) {
		allow[s] = true
	}
	for _, c := range n.children {
		if !allow[c.name] {
			return invalid("OFX_FEATURE_UNSUPPORTED", "unsupported OFX element "+c.name)
		}
	}
	return nil
}

var aggregates = map[string]bool{
	"OFX": true, "SIGNONMSGSRSV1": true, "SONRS": true, "STATUS": true, "FI": true,
	"BANKMSGSRSV1": true, "STMTTRNRS": true, "STMTRS": true, "BANKACCTFROM": true,
	"CREDITCARDMSGSRSV1": true, "CCSTMTTRNRS": true, "CCSTMTRS": true, "CCACCTFROM": true,
	"BANKTRANLIST": true, "STMTTRN": true, "LEDGERBAL": true, "AVAILBAL": true,
}

// Parse supports bounded OFX 1 SGML and OFX 2 XML bank/card statement responses.
// Unsupported financial semantics fail closed; original bytes belong to the caller.
func Parse(data []byte) (Document, error) {
	doc := Document{Parser: ParserVersion, Accounts: []Account{}}
	if len(data) == 0 || len(data) > MaxBytes {
		return doc, invalid("IMPORT_SIZE_INVALID", "statement must contain between 1 and 8388608 bytes")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	text := strings.TrimSpace(string(data))
	sgml := strings.HasPrefix(text, "OFXHEADER:")
	if sgml {
		start := strings.Index(text, "<OFX>")
		if start < 0 {
			return doc, invalid("OFX_HEADER_INVALID", "OFX root is missing")
		}
		headers := map[string]string{}
		for _, line := range strings.Split(text[:start], "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok || headers[k] != "" {
				return doc, invalid("OFX_HEADER_INVALID", "invalid or repeated OFX header")
			}
			headers[k] = strings.TrimSpace(v)
		}
		if headers["OFXHEADER"] != "100" || headers["DATA"] != "OFXSGML" || (headers["SECURITY"] != "NONE" && headers["SECURITY"] != "") || (headers["COMPRESSION"] != "NONE" && headers["COMPRESSION"] != "") {
			return doc, invalid("OFX_HEADER_UNSUPPORTED", "unsupported SGML security, compression, or header")
		}
		switch headers["VERSION"] {
		case "102", "103", "160":
		default:
			return doc, invalid("OFX_VERSION_UNSUPPORTED", "supported SGML versions are 102, 103, and 160")
		}
		doc.Format = "OFX"
		doc.Version = headers["VERSION"]
		var err error
		text, err = decodeText([]byte(text[start:]), headers["ENCODING"], headers["CHARSET"])
		if err != nil {
			return doc, err
		}
	} else {
		doc.Format = "OFX"
		doc.Version = "2"
		if !utf8.ValidString(text) {
			return doc, invalid("OFX_ENCODING_UNSUPPORTED", "XML input must be UTF-8")
		}
	}
	root, err := parseTree(text, sgml)
	if err != nil {
		return doc, err
	}
	if root.version != "" {
		doc.Version = root.version
	}
	if root.name != "OFX" {
		return doc, invalid("FORMAT_UNSUPPORTED", "expected an OFX bank or card statement")
	}
	if err = allowed(root, "SIGNONMSGSRSV1 BANKMSGSRSV1 CREDITCARDMSGSRSV1 INTU.BID INTU.USERID"); err != nil {
		return doc, err
	}
	institution := ""
	if signons := root.all("SIGNONMSGSRSV1"); len(signons) > 0 {
		if len(signons) != 1 {
			return doc, invalid("OFX_FIELD_INVALID", "duplicate signon response")
		}
		son, e := signons[0].one("SONRS")
		if e != nil {
			return doc, e
		}
		if e = checkStatus(son); e != nil {
			return doc, e
		}
		fis := son.all("FI")
		if len(fis) > 1 {
			return doc, invalid("OFX_FIELD_INVALID", "duplicate institution")
		}
		if len(fis) == 1 {
			fid, e := fis[0].optional("FID")
			if e != nil {
				return doc, e
			}
			org, e := fis[0].optional("ORG")
			if e != nil {
				return doc, e
			}
			if fid != "" {
				institution = org + ":" + fid
			}
		}
	}
	seen := map[string]bool{}
	total := 0
	for _, family := range []struct{ group, response, statement, kind string }{{"BANKMSGSRSV1", "STMTTRNRS", "STMTRS", "BANK"}, {"CREDITCARDMSGSRSV1", "CCSTMTTRNRS", "CCSTMTRS", "CREDIT_CARD"}} {
		groups := root.all(family.group)
		if len(groups) > 1 {
			return doc, invalid("OFX_FIELD_INVALID", "duplicate message group")
		}
		for _, g := range groups {
			if e := allowed(g, family.response); e != nil {
				return doc, e
			}
			for _, r := range g.children {
				if e := allowed(r, "TRNUID STATUS CLTCOOKIE "+family.statement); e != nil {
					return doc, e
				}
				if e := checkStatus(r); e != nil {
					return doc, e
				}
				st, e := r.one(family.statement)
				if e != nil {
					return doc, e
				}
				a, e := parseAccount(st, family.kind, institution)
				if e != nil {
					return doc, e
				}
				if seen[a.Key] {
					return doc, invalid("OFX_ACCOUNT_REPEATED", "repeated account statements require a supported pagination profile")
				}
				seen[a.Key] = true
				total += len(a.Transactions)
				if total > MaxTransactions || len(doc.Accounts) >= MaxAccounts {
					return doc, invalid("IMPORT_LIMIT_EXCEEDED", "too many accounts or transactions")
				}
				doc.Accounts = append(doc.Accounts, a)
			}
		}
	}
	if len(doc.Accounts) == 0 {
		return doc, invalid("OFX_STATEMENT_MISSING", "file contains no supported bank or card statement")
	}
	return doc, nil
}

func checkStatus(n *node) error {
	st, e := n.one("STATUS")
	if e != nil {
		return e
	}
	code, e := st.required("CODE")
	if e != nil {
		return e
	}
	severity, e := st.required("SEVERITY")
	if e != nil {
		return e
	}
	if code != "0" || severity != "INFO" {
		return invalid("OFX_RESPONSE_FAILED", "bank response is not a successful statement")
	}
	return nil
}

func parseAccount(n *node, kind, institution string) (Account, error) {
	a := Account{Kind: kind, Institution: institution, Balances: []Balance{}, Transactions: []Transaction{}}
	acctTag := "BANKACCTFROM"
	if kind == "CREDIT_CARD" {
		acctTag = "CCACCTFROM"
	}
	if e := allowed(n, "CURDEF BANKTRANLIST LEDGERBAL AVAILBAL MKTGINFO "+acctTag); e != nil {
		return a, e
	}
	var e error
	a.Currency, e = n.required("CURDEF")
	if e != nil {
		return a, e
	}
	if !money.IsSupportedCurrency(a.Currency) {
		return a, invalid("CURRENCY_NOT_SUPPORTED", "unsupported monetary currency")
	}
	currency, _ := money.Lookup(a.Currency)
	from, e := n.one(acctTag)
	if e != nil {
		return a, e
	}
	if e = allowed(from, "BANKID BRANCHID ACCTID ACCTTYPE ACCTKEY"); e != nil {
		return a, e
	}
	a.AccountID, e = from.required("ACCTID")
	if e != nil {
		return a, e
	}
	if len(a.AccountID) > 128 {
		return a, invalid("OFX_FIELD_INVALID", "account identifier exceeds the supported length")
	}
	a.BankID, e = from.optional("BANKID")
	if e != nil {
		return a, e
	}
	a.BranchID, e = from.optional("BRANCHID")
	if e != nil {
		return a, e
	}
	a.AccountType, e = from.optional("ACCTTYPE")
	if e != nil {
		return a, e
	}
	if kind == "BANK" {
		if a.BankID == "" || a.AccountType == "" {
			return a, invalid("OFX_ACCOUNT_INVALID", "bank identifier and account type are required")
		}
		a.Institution = "BANK:" + a.BankID
	}
	if a.Institution == "" {
		return a, invalid("OFX_INSTITUTION_REQUIRED", "card statement requires the signon institution FID")
	}
	identity, _ := json.Marshal([]string{a.Institution, a.BankID, a.BranchID, a.AccountID, a.AccountType, a.Kind, a.Currency})
	sum := sha256.Sum256(identity)
	a.Key = hex.EncodeToString(sum[:])
	list, e := n.one("BANKTRANLIST")
	if e != nil {
		return a, e
	}
	if e = allowed(list, "DTSTART DTEND STMTTRN"); e != nil {
		return a, e
	}
	a.From, e = list.required("DTSTART")
	if e != nil {
		return a, e
	}
	a.Through, e = list.required("DTEND")
	if e != nil {
		return a, e
	}
	start, e := ofxDate(a.From)
	if e != nil {
		return a, e
	}
	end, e := ofxDate(a.Through)
	if e != nil {
		return a, e
	}
	if start > end {
		return a, invalid("OFX_DATE_INVALID", "statement range is reversed")
	}
	seen := map[string]bool{}
	for _, tr := range list.all("STMTTRN") {
		t, e := parseTransaction(tr, currency)
		if e != nil {
			return a, e
		}
		if t.PostedDate < start || t.PostedDate > end {
			return a, invalid("OFX_DATE_INVALID", "transaction lies outside statement range")
		}
		if seen[t.ID] {
			return a, invalid("OFX_DUPLICATE_ID", "statement repeats a transaction identifier")
		}
		seen[t.ID] = true
		a.Transactions = append(a.Transactions, t)
		if len(a.Transactions) > MaxTransactions {
			return a, invalid("IMPORT_LIMIT_EXCEEDED", "too many transactions")
		}
	}
	for _, b := range []string{"LEDGERBAL", "AVAILBAL"} {
		entries := n.all(b)
		if len(entries) > 1 {
			return a, invalid("OFX_FIELD_INVALID", "duplicate balance")
		}
		if b == "LEDGERBAL" && len(entries) == 0 {
			return a, invalid("OFX_BALANCE_REQUIRED", "ledger balance is required")
		}
		for _, bn := range entries {
			if e = allowed(bn, "BALAMT DTASOF"); e != nil {
				return a, e
			}
			amount, e := bn.required("BALAMT")
			if e != nil {
				return a, e
			}
			c, e := currency.Parse(amount)
			if e != nil {
				return a, invalid("OFX_AMOUNT_INVALID", "balance is not exact account-currency money")
			}
			at, e := bn.required("DTASOF")
			if e != nil {
				return a, e
			}
			if _, e = ofxDate(at); e != nil {
				return a, e
			}
			a.Balances = append(a.Balances, Balance{Kind: b, Amount: currency.Format(c), AsOf: at})
		}
	}
	return a, nil
}

func parseTransaction(n *node, currency money.Currency) (Transaction, error) {
	t := Transaction{Fields: map[string]string{}}
	if e := allowed(n, "TRNTYPE DTPOSTED DTUSER DTAVAIL TRNAMT FITID SRVRTID CHECKNUM REFNUM SIC PAYEEID NAME EXTDNAME MEMO"); e != nil {
		return t, e
	}
	for _, c := range n.children {
		v, e := n.optional(c.name)
		if e != nil {
			return t, e
		}
		t.Fields[c.name] = v
	}
	var e error
	t.ID, e = n.required("FITID")
	if e != nil {
		return t, e
	}
	if len(t.ID) > MaxTransactionID {
		return t, invalid("OFX_FIELD_INVALID", "transaction identifier exceeds supported length")
	}
	t.Type, e = n.required("TRNTYPE")
	if e != nil {
		return t, e
	}
	switch t.Type {
	case "CREDIT", "DEBIT", "INT", "DIV", "FEE", "SRVCHG", "DEP", "ATM", "POS", "XFER", "CHECK", "PAYMENT", "CASH", "DIRECTDEP", "DIRECTDEBIT", "REPEATPMT", "OTHER":
	default:
		return t, invalid("OFX_TRANSACTION_UNSUPPORTED", "unsupported transaction type")
	}
	t.PostedAt, e = n.required("DTPOSTED")
	if e != nil {
		return t, e
	}
	t.PostedDate, e = ofxDate(t.PostedAt)
	if e != nil {
		return t, e
	}
	for _, k := range []string{"DTUSER", "DTAVAIL"} {
		if v := t.Fields[k]; v != "" {
			if _, e = ofxDate(v); e != nil {
				return t, e
			}
		}
	}
	t.ValueAt = t.Fields["DTAVAIL"]
	amount, e := n.required("TRNAMT")
	if e != nil {
		return t, e
	}
	c, e := currency.Parse(amount)
	if e != nil {
		return t, invalid("OFX_AMOUNT_INVALID", "transaction amount is not exact account-currency money")
	}
	t.Amount = currency.Format(c)
	t.Description = t.Fields["NAME"]
	if v := t.Fields["EXTDNAME"]; v != "" {
		t.Description = v
	}
	if v := t.Fields["MEMO"]; v != "" {
		if t.Description != "" {
			t.Description += " — "
		}
		t.Description += v
	}
	if t.Description == "" {
		t.Description = t.Type
	}
	return t, nil
}

var datePattern = regexp.MustCompile(`^([0-9]{8})([0-9]{6}(\.[0-9]{1,3})?)?(\[([+-]?[0-9]{1,2})(\.[0-9]+)?(:[A-Za-z0-9 +_-]{1,16})?\])?$`)

func ofxDate(v string) (string, error) {
	m := datePattern.FindStringSubmatch(v)
	if m == nil {
		return "", invalid("OFX_DATE_INVALID", "invalid OFX date")
	}
	if m[4] != "" {
		offset, err := strconv.ParseFloat(m[5]+m[6], 64)
		if err != nil || offset < -12 || offset > 14 {
			return "", invalid("OFX_DATE_INVALID", "timezone offset must be between -12 and +14 hours")
		}
	}
	layout := "20060102"
	base := m[1]
	if m[2] != "" {
		layout = "20060102150405"
		base += strings.Split(m[2], ".")[0]
	}
	d, e := time.Parse(layout, base)
	if e != nil {
		return "", invalid("OFX_DATE_INVALID", "invalid calendar date or time")
	}
	return d.Format("2006-01-02"), nil
}

func decodeText(b []byte, encoding, charset string) (string, error) {
	if encoding == "UTF-8" || encoding == "UNICODE" {
		if !utf8.Valid(b) {
			return "", invalid("OFX_ENCODING_UNSUPPORTED", "invalid UTF-8 input")
		}
		return string(b), nil
	}
	if encoding != "USASCII" {
		return "", invalid("OFX_ENCODING_UNSUPPORTED", "supported SGML encodings are USASCII and UTF-8")
	}
	if charset == "NONE" || charset == "" {
		for _, c := range b {
			if c > 127 {
				return "", invalid("OFX_ENCODING_UNSUPPORTED", "non-ASCII byte without charset")
			}
		}
		return string(b), nil
	}
	if charset != "1252" {
		return "", invalid("OFX_ENCODING_UNSUPPORTED", "supported legacy charset is 1252")
	}
	const cp1252 = "€\u0081‚ƒ„…†‡ˆ‰Š‹Œ\u008dŽ\u008f\u0090‘’“”•–—˜™š›œ\u009džŸ"
	table := []rune(cp1252)
	var out strings.Builder
	for _, c := range b {
		r := rune(c)
		if c >= 0x80 && c < 0xa0 {
			r = table[int(c)-0x80]
		}
		out.WriteRune(r)
	}
	return out.String(), nil
}

func parseTree(input string, sgml bool) (*node, error) {
	if sgml {
		var e error
		input, e = closeSGMLLeaves(input)
		if e != nil {
			return nil, e
		}
	}
	d := xml.NewDecoder(strings.NewReader(input))
	d.Strict = true
	var root *node
	var stack []*node
	count := 0
	xmlVersion := ""
	for {
		tok, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, invalid("OFX_MALFORMED", "malformed OFX markup")
		}
		switch v := tok.(type) {
		case xml.StartElement:
			count++
			if count > MaxNodes || len(stack) >= MaxDepth {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "OFX exceeds structural limits")
			}
			if v.Name.Space != "" || len(v.Attr) != 0 || len(v.Name.Local) > 64 {
				return nil, invalid("OFX_FEATURE_UNSUPPORTED", "namespaces and element attributes are unsupported")
			}
			n := &node{name: v.Name.Local}
			if len(stack) == 0 {
				if root != nil {
					return nil, invalid("OFX_MALFORMED", "multiple document roots")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, invalid("OFX_MALFORMED", "unexpected closing tag")
			}
			n := stack[len(stack)-1]
			if len(n.children) > 0 && strings.TrimSpace(n.text) != "" {
				return nil, invalid("OFX_MALFORMED", "mixed aggregate text is unsupported")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, invalid("OFX_MALFORMED", "text outside document")
				}
				continue
			}
			n := stack[len(stack)-1]
			if len(n.text)+len(v) > MaxText {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "OFX field exceeds text limit")
			}
			n.text += string(v)
		case xml.Directive:
			return nil, invalid("OFX_FEATURE_UNSUPPORTED", "DTDs and directives are not supported")
		case xml.ProcInst:
			if root != nil || sgml {
				return nil, invalid("OFX_HEADER_INVALID", "processing instructions must precede the XML root")
			}
			if v.Target == "OFX" {
				if xmlVersion != "" {
					return nil, invalid("OFX_HEADER_INVALID", "repeated OFX header")
				}
				var err error
				xmlVersion, err = xmlHeader(v.Inst)
				if err != nil {
					return nil, err
				}
			}
			if v.Target != "xml" && v.Target != "OFX" {
				return nil, invalid("OFX_FEATURE_UNSUPPORTED", "unsupported processing instruction")
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, invalid("OFX_MALFORMED", "incomplete document")
	}
	root.version = xmlVersion
	return root, nil
}

// Only known OFX aggregates may omit a leaf end tag. Structural failures are
// rejected, never repaired by an XML decoder's permissive auto-close mode.
func closeSGMLLeaves(s string) (string, error) {
	var out strings.Builder
	leaf := ""
	nodes := 0
	for len(s) > 0 {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:i])
		s = s[i:]
		j := strings.IndexByte(s, '>')
		if j < 0 {
			return "", invalid("OFX_MALFORMED", "unterminated SGML tag")
		}
		tag := s[1:j]
		s = s[j+1:]
		nodes++
		if nodes > MaxNodes*2 {
			return "", invalid("IMPORT_LIMIT_EXCEEDED", "too many SGML tags")
		}
		end := strings.HasPrefix(tag, "/")
		name := strings.TrimPrefix(tag, "/")
		if name == "" || strings.ContainsAny(name, " \t\r\n!?/\"'") {
			return "", invalid("OFX_MALFORMED", "invalid SGML tag")
		}
		for _, c := range name {
			if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && (c != '.' || (name != "INTU.BID" && name != "INTU.USERID")) {
				return "", invalid("OFX_MALFORMED", "invalid SGML tag name")
			}
		}
		if leaf != "" {
			out.WriteString("</" + leaf + ">")
			if end && name == leaf {
				leaf = ""
				continue
			}
			leaf = ""
		}
		out.WriteString("<" + tag + ">")
		if !end && !aggregates[name] {
			leaf = name
		}
	}
	if leaf != "" {
		return "", invalid("OFX_MALFORMED", fmt.Sprintf("unclosed SGML field %s", leaf))
	}
	return out.String(), nil
}

// xmlHeader validates the supplied declaration using XML attribute parsing.
func xmlHeader(data []byte) (string, error) {
	d := xml.NewDecoder(strings.NewReader("<HEADER " + string(data) + "/>"))
	token, err := d.Token()
	if err != nil {
		return "", invalid("OFX_HEADER_INVALID", "malformed OFX XML header")
	}
	start, ok := token.(xml.StartElement)
	if !ok {
		return "", invalid("OFX_HEADER_INVALID", "malformed OFX XML header")
	}
	attrs := map[string]string{}
	for _, a := range start.Attr {
		if a.Name.Space != "" || attrs[a.Name.Local] != "" {
			return "", invalid("OFX_HEADER_INVALID", "invalid or repeated header attribute")
		}
		switch a.Name.Local {
		case "OFXHEADER", "VERSION", "SECURITY", "OLDFILEUID", "NEWFILEUID":
		default:
			return "", invalid("OFX_HEADER_UNSUPPORTED", "unsupported OFX header attribute")
		}
		attrs[a.Name.Local] = a.Value
	}
	if attrs["OFXHEADER"] != "200" || attrs["SECURITY"] != "NONE" {
		return "", invalid("OFX_HEADER_UNSUPPORTED", "unsupported XML security or header")
	}
	switch attrs["VERSION"] {
	case "200", "201", "202", "203", "210", "211", "220", "230":
	default:
		return "", invalid("OFX_VERSION_UNSUPPORTED", "unsupported OFX XML version")
	}
	if token, err = d.Token(); err != nil {
		return "", invalid("OFX_HEADER_INVALID", "malformed XML header")
	}
	if _, ok = token.(xml.EndElement); !ok {
		return "", invalid("OFX_HEADER_INVALID", "malformed XML header")
	}
	if _, err = d.Token(); err != io.EOF {
		return "", invalid("OFX_HEADER_INVALID", "trailing XML header content")
	}
	return attrs["VERSION"], nil
}
