package banking

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/money"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxOptionsBytes = 32 << 10

const StatementParserVersion = "financial-statements/v1"

// Options supplies facts missing from a source format. Persist this exact value
// with upload evidence; reparsing must never depend on current machine locale.
type Options struct {
	Format        string          `json:"format,omitempty"`
	MTBookingDate string          `json:"mt_booking_date,omitempty"`
	InterimStatus string          `json:"interim_status,omitempty"`
	Institution   string          `json:"institution,omitempty"`
	AccountID     string          `json:"account_id,omitempty"`
	AccountKind   string          `json:"account_kind,omitempty"`
	Currency      string          `json:"currency,omitempty"`
	DateLayout    string          `json:"date_layout,omitempty"`
	YearPivot     *int            `json:"year_pivot,omitempty"`
	Encoding      string          `json:"encoding,omitempty"`
	Tabular       *TabularProfile `json:"tabular,omitempty"`
}
type TabularProfile struct {
	HeaderRow          int               `json:"header_row"`
	Sheet              string            `json:"sheet,omitempty"`
	Delimiter          string            `json:"delimiter,omitempty"`
	IDColumn           string            `json:"id_column,omitempty"`
	DateColumn         string            `json:"date_column"`
	ValueDateColumn    string            `json:"value_date_column,omitempty"`
	AmountColumn       string            `json:"amount_column,omitempty"`
	DebitColumn        string            `json:"debit_column,omitempty"`
	CreditColumn       string            `json:"credit_column,omitempty"`
	DescriptionColumns []string          `json:"description_columns"`
	AccountColumn      string            `json:"account_column,omitempty"`
	CurrencyColumn     string            `json:"currency_column,omitempty"`
	StatusColumn       string            `json:"status_column,omitempty"`
	StatusValues       map[string]string `json:"status_values,omitempty"`
	DecimalSeparator   string            `json:"decimal_separator"`
	ThousandsSeparator string            `json:"thousands_separator,omitempty"`
	InvertSign         bool              `json:"invert_sign,omitempty"`
}
type FormatCapability struct {
	Format          string   `json:"format"`
	Profile         string   `json:"profile"`
	OptionsRequired []string `json:"options_required,omitempty"`
}

func Capabilities() []FormatCapability {
	return []FormatCapability{
		{"OFX", "bank/card SGML 102/103/160 and XML 200–230", nil},
		{"QFX", "OFX bank/card with Intuit identifiers", nil}, {"QBO", "Web Connect OFX bank/card", nil},
		{"QIF", "non-investment account registers, categories and splits", []string{"institution", "currency", "date_layout"}},
		{"CSV", "explicit column/date/amount profile", []string{"institution", "currency", "date_layout", "tabular"}},
		{"TSV", "explicit column/date/amount profile", []string{"institution", "currency", "date_layout", "tabular"}},
		{"XLSX", "explicit worksheet and column/date/amount profile", []string{"institution", "currency", "date_layout", "tabular"}},
		{"CAMT", "camt.052/.053/.054 namespaces 001.02 and 001.08", nil},
		{"MT940", "statement tags 20/25/28C/60/61/62/64/65/86", []string{"institution", "year_pivot"}},
		{"MT942", "interim reporting tags 20/25/28C/34F/13D/61/86/90", []string{"institution", "currency", "year_pivot"}},
		{"BAI2", "version 2 records 01/02/03/16/88/49/98/99", []string{"year_pivot"}},
		{"BTRS", "X9.121 version 3 records 01/02/03/16/88/49/98/99", []string{"year_pivot"}},
		{"CODA", "version 2 records, 2.7 layout", []string{"year_pivot"}},
		{"CFONB120", "July 2004 120-byte account statements", []string{"year_pivot"}},
		{"NORMA43", "June 2012 account statements, modes 1/2/3", []string{"year_pivot"}},
	}
}
func SupportedParser(v string) bool { return v == ParserVersion || v == StatementParserVersion }
func NormalizeOptions(o Options) (Options, error) {
	encoded, err := json.Marshal(o)
	if err != nil || len(encoded) > MaxOptionsBytes {
		return o, invalid("IMPORT_OPTIONS_INVALID", "source options exceed the supported byte limit")
	}
	o.Format = strings.ToUpper(strings.TrimSpace(o.Format))
	if o.Format == "AUTO" {
		o.Format = ""
	}
	if o.Format != "" {
		found := false
		for _, c := range Capabilities() {
			if o.Format == c.Format {
				found = true
			}
		}
		if !found {
			return o, invalid("FORMAT_UNSUPPORTED", "unknown statement format")
		}
	}
	for _, v := range []string{o.Institution, o.AccountID, o.AccountKind, o.Currency, o.DateLayout, o.Encoding} {
		if len(v) > 128 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\x00\r\n") {
			return o, invalid("IMPORT_OPTIONS_INVALID", "invalid source profile field")
		}
	}
	if o.Currency != "" && !currencyPattern.MatchString(o.Currency) {
		return o, invalid("IMPORT_CURRENCY_INVALID", "currency must be an uppercase ISO code")
	}
	if o.AccountKind != "" && o.AccountKind != "BANK" && o.AccountKind != "CREDIT_CARD" {
		return o, invalid("IMPORT_OPTIONS_INVALID", "account_kind must be BANK or CREDIT_CARD")
	}
	if o.YearPivot != nil && (*o.YearPivot < 0 || *o.YearPivot > 99) {
		return o, invalid("IMPORT_OPTIONS_INVALID", "year_pivot must be between 0 and 99")
	}
	if o.MTBookingDate != "" && o.MTBookingDate != "statement" && o.MTBookingDate != "value" {
		return o, invalid("IMPORT_OPTIONS_INVALID", "mt_booking_date must be statement or value")
	}
	if o.InterimStatus != "" && o.InterimStatus != "POSTED" && o.InterimStatus != "PENDING" && o.InterimStatus != "REVIEW" {
		return o, invalid("IMPORT_OPTIONS_INVALID", "interim_status must be POSTED, PENDING, or REVIEW")
	}
	switch o.Encoding {
	case "", "UTF-8", "WINDOWS-1252", "ISO-8859-1", "CP850":
	default:
		return o, invalid("IMPORT_ENCODING_UNSUPPORTED", "supported encodings are UTF-8, WINDOWS-1252, ISO-8859-1, and CP850")
	}
	if o.DateLayout != "" {
		switch o.DateLayout {
		case "2006-01-02", "20060102", "01/02/2006", "02/01/2006", "01/02/06", "02/01/06", "02.01.2006", "02-01-2006", "2006/01/02", "EXCEL-1900", "EXCEL-1904":
		default:
			return o, invalid("IMPORT_DATE_LAYOUT_INVALID", "unsupported explicit date layout")
		}
	}
	if o.Tabular != nil {
		p := *o.Tabular
		o.Tabular = &p
		if err := validateTabularProfile(p); err != nil {
			return o, err
		}
	}
	return o, nil
}

// ParseFile detects structure, not filename. Ambiguous text formats require an
// explicit format and profile; no parser guesses locale, ownership, or signs.
func ParseFile(data []byte, o Options) (Document, error) {
	var d Document
	if len(data) == 0 || len(data) > MaxBytes {
		return d, invalid("IMPORT_SIZE_INVALID", "statement exceeds the supported byte limit")
	}
	var err error
	o, err = NormalizeOptions(o)
	if err != nil {
		return d, err
	}
	format := o.Format
	trim := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if format == "" {
		switch {
		case bytes.HasPrefix(trim, []byte("OFXHEADER:")), bytes.Contains(trim, []byte("<OFX>")):
			format = "OFX"
		case bytes.HasPrefix(trim, []byte("!Type:")), bytes.HasPrefix(trim, []byte("!Account")):
			format = "QIF"
		case bytes.Contains(trim, []byte("urn:iso:std:iso:20022:tech:xsd:camt.")):
			format = "CAMT"
		case bytes.HasPrefix(trim, []byte("PK\x03\x04")):
			format = "XLSX"
		case bytes.HasPrefix(trim, []byte(":20:")), bytes.HasPrefix(trim, []byte("{1:")):
			if bytes.Contains(trim, []byte(":60F:")) || bytes.Contains(trim, []byte(":60M:")) {
				format = "MT940"
			} else {
				format = "MT942"
			}
		case bytes.HasPrefix(trim, []byte("01,")):
			first := strings.Split(strings.SplitN(string(trim), "\n", 2)[0], ",")
			if len(first) == 9 && strings.TrimSuffix(strings.TrimSpace(first[8]), "/") == "3" {
				format = "BTRS"
			} else {
				format = "BAI2"
			}
		default:
			line := bytes.SplitN(trim, []byte("\n"), 2)[0]
			line = bytes.TrimSuffix(line, []byte("\r"))
			switch {
			case len(line) == 128 && line[0] == '0':
				format = "CODA"
			case len(line) == 120 && bytes.HasPrefix(line, []byte("01")):
				format = "CFONB120"
			case len(line) == 80 && bytes.HasPrefix(line, []byte("11")):
				format = "NORMA43"
			default:
				return d, invalid("FORMAT_REQUIRED", "select a format and explicit profile for delimited or unrecognized text")
			}
		}
	}
	if format == "OFX" || format == "QFX" || format == "QBO" {
		d, err = Parse(data)
		if err != nil {
			return d, err
		}
		if format != "OFX" {
			d.Format = format
		}
		return d, nil
	}
	switch format {
	case "CSV", "TSV", "XLSX":
		d, err = parseTabular(data, o, format)
	case "QIF":
		d, err = parseQIF(data, o)
	case "CAMT":
		d, err = parseCAMT(data, o)
	case "MT940", "MT942":
		d, err = parseMT(data, o, format)
	case "BAI2", "BTRS":
		d, err = parseBAI(data, o, format)
	case "CODA":
		d, err = parseCODA(data, o)
	case "CFONB120":
		d, err = parseCFONB(data, o)
	case "NORMA43":
		d, err = parseNorma43(data, o)
	default:
		return d, invalid("FORMAT_UNSUPPORTED", "statement format is unsupported")
	}
	if err != nil {
		return Document{}, err
	}
	d.Parser = StatementParserVersion
	if err = finalizeDocument(&d, data); err != nil {
		return Document{}, err
	}
	return d, nil
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var decimalPattern = regexp.MustCompile(`^[+-]?[0-9]{1,30}(\.[0-9]{1,9})?$`)

func decimal(v string) (string, error) {
	if !decimalPattern.MatchString(v) {
		return "", invalid("IMPORT_AMOUNT_INVALID", "amount must be bounded exact decimal text")
	}
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	parts := strings.SplitN(v, ".", 2)
	whole := strings.TrimLeft(parts[0], "0")
	if whole == "" {
		whole = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	out := whole + "." + fraction
	if neg && out != "0.00" {
		out = "-" + out
	}
	return out, nil
}
func fixedDecimal(v string, scale int) (string, error) {
	if scale < 0 || scale > 9 {
		return "", invalid("IMPORT_AMOUNT_INVALID", "unsupported decimal precision")
	}
	negative := strings.HasPrefix(v, "-")
	if strings.HasPrefix(v, "+") || strings.HasPrefix(v, "-") {
		v = v[1:]
	}
	if !digits(v) || len(v) > 30 {
		return "", invalid("IMPORT_AMOUNT_INVALID", "invalid fixed-scale amount")
	}
	for len(v) <= scale {
		v = "0" + v
	}
	if scale > 0 {
		v = v[:len(v)-scale] + "." + v[len(v)-scale:]
	}
	if negative {
		v = "-" + v
	}
	return decimal(v)
}
func signedDecimal(v, sign string) (string, error) {
	if strings.HasPrefix(v, "-") {
		return "", invalid("IMPORT_AMOUNT_INVALID", "signed amount conflicts with debit/credit indicator")
	}
	switch sign {
	case "D", "DBIT", "1", "-":
		v = "-" + strings.TrimPrefix(v, "+")
	case "C", "CRDT", "2", "+":
	default:
		return "", invalid("IMPORT_SIGN_INVALID", "unknown debit/credit indicator")
	}
	return decimal(v)
}
func decimalSum(vs ...string) (string, error) {
	sum := new(big.Rat)
	for _, v := range vs {
		r, ok := new(big.Rat).SetString(v)
		if !ok {
			return "", invalid("IMPORT_AMOUNT_INVALID", "invalid amount in control total")
		}
		sum.Add(sum, r)
	}
	return decimal(sum.FloatString(9))
}
func sameAmount(a, b string) bool {
	ar, ok := new(big.Rat).SetString(a)
	if !ok {
		return false
	}
	br, ok := new(big.Rat).SetString(b)
	return ok && ar.Cmp(br) == 0
}
func negate(v string) string {
	if strings.HasPrefix(v, "-") {
		return v[1:]
	}
	if v == "0.00" {
		return v
	}
	return "-" + v
}
func digits(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func isoDate(v string) (string, error) {
	if len(v) == 10 {
		if _, e := time.Parse("2006-01-02", v); e == nil {
			return v, nil
		}
	}
	if len(v) > 10 {
		if d, e := time.Parse(time.RFC3339Nano, v); e == nil {
			return d.Format("2006-01-02"), nil
		}
	}
	return "", invalid("IMPORT_DATE_INVALID", "invalid ISO date or timestamp")
}
func shortDate(v, order string, pivot *int) (string, error) {
	if len(v) != 6 || !digits(v) {
		return "", invalid("IMPORT_DATE_INVALID", "expected a six-digit date")
	}
	if pivot == nil {
		return "", invalid("IMPORT_YEAR_PIVOT_REQUIRED", "this source uses two-digit years; specify year_pivot explicitly")
	}
	yy, mm, dd := v[:2], v[2:4], v[4:]
	if order == "DMY" {
		dd, mm, yy = v[:2], v[2:4], v[4:]
	}
	y, _ := strconv.Atoi(yy)
	if y >= *pivot {
		y += 1900
	} else {
		y += 2000
	}
	return isoDate(fmt.Sprintf("%04d-%s-%s", y, mm, dd))
}
func sourceDate(v string, o Options) (string, error) {
	if o.DateLayout == "" {
		return "", invalid("IMPORT_DATE_LAYOUT_REQUIRED", "specify the source date layout")
	}
	if strings.HasPrefix(o.DateLayout, "EXCEL-") {
		return excelDate(v, o.DateLayout)
	}
	layout := o.DateLayout
	if layout == "01/02/06" || layout == "02/01/06" {
		parts := strings.Split(v, "/")
		if len(parts) != 3 {
			return "", invalid("IMPORT_DATE_INVALID", "date does not match its profile")
		}
		a, e := strconv.Atoi(strings.TrimSpace(parts[0]))
		if e != nil {
			return "", invalid("IMPORT_DATE_INVALID", "invalid date")
		}
		b, e := strconv.Atoi(strings.TrimSpace(parts[1]))
		if e != nil {
			return "", invalid("IMPORT_DATE_INVALID", "invalid date")
		}
		year := strings.TrimSpace(parts[2])
		if len(year) != 2 {
			return "", invalid("IMPORT_DATE_INVALID", "expected a two-digit year")
		}
		if layout == "01/02/06" {
			a, b = b, a
		}
		return shortDate(fmt.Sprintf("%02d%02d%s", a, b, year), "DMY", o.YearPivot)
	}
	if layout == "01/02/2006" {
		layout = "1/2/2006"
	}
	if layout == "02/01/2006" {
		layout = "2/1/2006"
	}
	d, e := time.Parse(layout, v)
	if e != nil {
		return "", invalid("IMPORT_DATE_INVALID", "date does not match the declared layout")
	}
	return d.Format("2006-01-02"), nil
}
func excelDate(v, layout string) (string, error) {
	// Only integral date cells are accepted; fractional times must use text with an explicit layout.
	n, e := strconv.Atoi(v)
	if e != nil || n < 0 || n > 2958465 {
		return "", invalid("IMPORT_DATE_INVALID", "invalid Excel date serial")
	}
	base := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	if layout == "EXCEL-1904" {
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	} else {
		if n == 60 {
			return "", invalid("IMPORT_DATE_INVALID", "Excel's fictitious leap day is invalid")
		}
		if n > 60 {
			n--
		}
	}
	return base.AddDate(0, 0, n).Format("2006-01-02"), nil
}
func textInput(data []byte, o Options) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	switch o.Encoding {
	case "CP850":
		var b strings.Builder
		for _, c := range data {
			if c < 128 {
				b.WriteByte(c)
			} else {
				b.WriteRune(cp850High[c-128])
			}
		}
		return b.String(), nil
	case "WINDOWS-1252":
		return decodeText(data, "USASCII", "1252")
	case "ISO-8859-1":
		var b strings.Builder
		for _, c := range data {
			b.WriteRune(rune(c))
		}
		return b.String(), nil
	default:
		if !utf8.Valid(data) {
			return "", invalid("IMPORT_ENCODING_INVALID", "source is not UTF-8; declare its legacy encoding")
		}
		return string(data), nil
	}
}
func sourceAccount(o Options, format, id, currency, kind string) (Account, error) {
	if id == "" {
		id = o.AccountID
	}
	if currency == "" {
		currency = o.Currency
	}
	if kind == "" {
		kind = o.AccountKind
	}
	if kind == "" {
		kind = "BANK"
	}
	if id == "" || o.Institution == "" || currency == "" {
		return Account{}, invalid("IMPORT_ACCOUNT_REQUIRED", "source institution, complete account identifier, and currency are required")
	}
	a := Account{Institution: o.Institution, AccountID: id, Currency: currency, Kind: kind, AccountType: kind, Balances: []Balance{}, Transactions: []Transaction{}}
	a.Key = accountKey(format, a)
	return a, nil
}
func accountKey(family string, a Account) string {
	data, _ := json.Marshal([]string{family, a.Institution, a.BankID, a.BranchID, a.AccountID, a.AccountType, a.Kind, a.Currency})
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}
func Family(format string) string {
	switch format {
	case "QFX", "QBO", "OFX":
		return "OFX"
	case "MT940", "MT942":
		return "MT"
	case "CSV", "TSV", "XLSX":
		return "TABULAR"
	}
	if strings.HasPrefix(format, "CAMT") {
		return "CAMT"
	}
	return format
}
func finalizeDocument(d *Document, data []byte) error {
	if len(d.Accounts) < 1 || len(d.Accounts) > MaxAccounts {
		return invalid("IMPORT_ACCOUNT_LIMIT", "source must contain between 1 and 100 accounts")
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	accounts := map[string]bool{}
	total := 0
	weak := 0
	for ai := range d.Accounts {
		a := &d.Accounts[ai]
		if a.Key == "" {
			a.Key = accountKey(Family(d.Format), *a)
		}
		if accounts[a.Key] {
			return invalid("IMPORT_DUPLICATE_ACCOUNT", "source repeats an account block; combine statements explicitly")
		}
		accounts[a.Key] = true
		if !currencyPattern.MatchString(a.Currency) || a.AccountID == "" || len(a.AccountID) > 128 || a.Institution == "" {
			return invalid("IMPORT_ACCOUNT_INVALID", "invalid source account identity or currency")
		}
		ids := map[string]bool{}
		for ti := range a.Transactions {
			t := &a.Transactions[ti]
			total++
			if total > MaxTransactions {
				return invalid("IMPORT_LIMIT_EXCEEDED", "too many transactions")
			}
			if _, e := isoDate(t.PostedDate); e != nil {
				return e
			}
			v, e := decimal(t.Amount)
			if e != nil {
				return e
			}
			t.Amount = v
			if t.ID == "" {
				t.ID = fmt.Sprintf("file:%s:%d:%d", hash, ai+1, ti+1)
				t.Identity = "FILE"
				weak++
			} else if t.Identity == "" {
				t.Identity = "NATIVE"
			}
			if len(t.ID) > MaxTransactionID || ids[t.ID] {
				return invalid("IMPORT_DUPLICATE_ID", "invalid or repeated source transaction identifier")
			}
			ids[t.ID] = true
			if t.Status == "" {
				t.Status = "POSTED"
			}
			if t.Status != "POSTED" && t.Status != "PENDING" && t.Status != "REVIEW" {
				return invalid("IMPORT_STATUS_INVALID", "unknown transaction state")
			}
			if t.PostedAt == "" {
				t.PostedAt = t.PostedDate
			}
			if t.Description == "" {
				t.Description = t.Type
			}
			if len(t.Description) > MaxText {
				return invalid("IMPORT_LIMIT_EXCEEDED", "description exceeds the field limit")
			}
			if t.Fields == nil {
				t.Fields = map[string]string{}
			}
			if a.From == "" || t.PostedDate < a.From {
				a.From = t.PostedDate
			}
			if a.Through == "" || t.PostedDate > a.Through {
				a.Through = t.PostedDate
			}
		}
		if !money.IsSupportedCurrency(a.Currency) {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: "CURRENCY_NOT_POSTABLE", Message: "Source currency is retained; posting requires a matching supported ledger currency", AccountKey: a.Key})
		}
	}
	if weak > 0 {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Code: "FILE_SCOPED_IDENTIFIERS", Message: "Some records have no reliable bank ID; overlapping imports require explicit identity review"})
	}
	sort.Slice(d.Accounts, func(i, j int) bool { return d.Accounts[i].Key < d.Accounts[j].Key })
	return nil
}

// UploadRequest preserves the original OFX upload key contract for empty options.
// Options are otherwise part of immutable source evidence and replay identity.
func UploadRequest(name, sourceHash string, o Options) []byte {
	options, _ := json.Marshal(o)
	parts := []string{name, sourceHash}
	if string(options) != "{}" {
		parts = append(parts, string(options))
	}
	data, _ := json.Marshal(parts)
	return data
}
