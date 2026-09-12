package banking

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

func validateTabularProfile(p TabularProfile) error {
	if p.HeaderRow < 1 || p.HeaderRow > 100 {
		return invalid("IMPORT_PROFILE_INVALID", "header_row must be between 1 and 100")
	}
	if p.DateColumn == "" || len(p.DescriptionColumns) < 1 || len(p.DescriptionColumns) > 10 {
		return invalid("IMPORT_PROFILE_INVALID", "date and description columns are required")
	}
	if (p.AmountColumn == "") == (p.DebitColumn == "" || p.CreditColumn == "") {
		return invalid("IMPORT_PROFILE_INVALID", "choose either amount_column or both debit_column and credit_column")
	}
	if p.AmountColumn != "" && (p.DebitColumn != "" || p.CreditColumn != "") {
		return invalid("IMPORT_PROFILE_INVALID", "amount and debit/credit columns cannot be combined")
	}
	if p.DecimalSeparator != "." && p.DecimalSeparator != "," {
		return invalid("IMPORT_PROFILE_INVALID", "declare decimal_separator as dot or comma")
	}
	if p.ThousandsSeparator != "" && p.ThousandsSeparator != "," && p.ThousandsSeparator != "." && p.ThousandsSeparator != " " {
		return invalid("IMPORT_PROFILE_INVALID", "unsupported thousands separator")
	}
	if p.DecimalSeparator == p.ThousandsSeparator {
		return invalid("IMPORT_PROFILE_INVALID", "decimal and thousands separators must differ")
	}
	if p.Delimiter != "" && p.Delimiter != "," && p.Delimiter != ";" && p.Delimiter != "\t" && p.Delimiter != "|" {
		return invalid("IMPORT_PROFILE_INVALID", "unsupported delimiter")
	}
	if len(p.Sheet) > 128 {
		return invalid("IMPORT_PROFILE_INVALID", "sheet name is too long")
	}
	cols := []string{p.IDColumn, p.DateColumn, p.ValueDateColumn, p.AmountColumn, p.DebitColumn, p.CreditColumn, p.AccountColumn, p.CurrencyColumn, p.StatusColumn}
	cols = append(cols, p.DescriptionColumns...)
	for _, c := range cols {
		if len(c) > 128 || strings.ContainsAny(c, "\x00\r\n") {
			return invalid("IMPORT_PROFILE_INVALID", "invalid column name")
		}
	}
	if len(p.StatusValues) > 20 {
		return invalid("IMPORT_PROFILE_INVALID", "too many status mappings")
	}
	for k, v := range p.StatusValues {
		if len(k) > 64 || (v != "POSTED" && v != "PENDING" && v != "REVIEW") {
			return invalid("IMPORT_PROFILE_INVALID", "status values must map to POSTED, PENDING, or REVIEW")
		}
	}
	if (p.StatusColumn == "") != (len(p.StatusValues) == 0) {
		return invalid("IMPORT_PROFILE_INVALID", "status_column and status_values must be declared together")
	}
	return nil
}
func profileAmount(v, decimalSeparator, thousands string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", invalid("IMPORT_AMOUNT_INVALID", "amount is missing")
	}
	negative := false
	if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
		negative = true
		v = v[1 : len(v)-1]
		if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
			return "", invalid("IMPORT_AMOUNT_INVALID", "ambiguous amount sign")
		}
	}
	sign := ""
	if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
		sign = v[:1]
		v = v[1:]
	}
	parts := strings.Split(v, decimalSeparator)
	if len(parts) > 2 {
		return "", invalid("IMPORT_AMOUNT_INVALID", "multiple decimal separators")
	}
	if thousands != "" && strings.Contains(parts[0], thousands) {
		groups := strings.Split(parts[0], thousands)
		if len(groups[0]) < 1 || len(groups[0]) > 3 || !digits(groups[0]) {
			return "", invalid("IMPORT_AMOUNT_INVALID", "invalid thousands grouping")
		}
		for _, g := range groups[1:] {
			if len(g) != 3 || !digits(g) {
				return "", invalid("IMPORT_AMOUNT_INVALID", "invalid thousands grouping")
			}
		}
		parts[0] = strings.Join(groups, "")
	}
	if !digits(parts[0]) || len(parts) == 2 && !digits(parts[1]) {
		return "", invalid("IMPORT_AMOUNT_INVALID", "amount does not match its locale profile")
	}
	result := sign + strings.Join(parts, ".")
	if negative {
		result = "-" + result
	}
	return decimal(result)
}
func parseTabular(data []byte, o Options, format string) (Document, error) {
	d := Document{Format: format, Version: "profile/v1"}
	if o.Tabular == nil {
		return d, invalid("IMPORT_PROFILE_REQUIRED", "provide an explicit tabular profile")
	}
	p := *o.Tabular
	var rows [][]string
	numeric := map[cellPosition]string{}
	var e error
	if format == "XLSX" {
		rows, e = readStatementXLSX(data, p, o.DateLayout, numeric)
	} else {
		text, err := textInput(data, o)
		if err != nil {
			return d, err
		}
		reader := csv.NewReader(strings.NewReader(text))
		delimiter := p.Delimiter
		if delimiter == "" {
			delimiter = ","
			if format == "TSV" {
				delimiter = "\t"
			}
		}
		reader.Comma = []rune(delimiter)[0]
		reader.FieldsPerRecord = -1
		reader.ReuseRecord = false
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return d, invalid("IMPORT_TABLE_INVALID", "malformed delimited record")
			}
			if len(row) > 256 || len(rows) >= MaxTransactions+100 {
				return d, invalid("IMPORT_LIMIT_EXCEEDED", "table exceeds row or column limits")
			}
			for _, v := range row {
				if len(v) > MaxText {
					return d, invalid("IMPORT_LIMIT_EXCEEDED", "table cell exceeds text limit")
				}
			}
			rows = append(rows, row)
		}
	}
	if e != nil {
		return d, e
	}
	if len(rows) < p.HeaderRow {
		return d, invalid("IMPORT_HEADER_MISSING", "declared header row is absent")
	}
	header := rows[p.HeaderRow-1]
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := columns[name]; ok {
			return d, invalid("IMPORT_HEADER_AMBIGUOUS", "table repeats a column name")
		}
		columns[name] = i
	}
	configured := []string{p.DateColumn, p.IDColumn, p.ValueDateColumn, p.AmountColumn, p.DebitColumn, p.CreditColumn, p.AccountColumn, p.CurrencyColumn, p.StatusColumn}
	configured = append(configured, p.DescriptionColumns...)
	for _, name := range configured {
		if name != "" {
			if _, ok := columns[name]; !ok {
				return d, invalid("IMPORT_COLUMN_MISSING", "a configured column is missing from the header")
			}
		}
	}
	accounts := map[string]int{}
	for ri, row := range rows[p.HeaderRow:] {
		empty := true
		for _, v := range row {
			if strings.TrimSpace(v) != "" {
				empty = false
			}
		}
		if empty {
			continue
		}
		if len(row) > len(header) {
			return d, invalid("IMPORT_ROW_INVALID", "row has more fields than the header")
		}
		get := func(name string) string {
			if name == "" {
				return ""
			}
			i := columns[name]
			if i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		number := func(name string) (string, bool) {
			if name == "" {
				return "", false
			}
			value, ok := numeric[cellPosition{ri + p.HeaderRow, columns[name]}]
			return value, ok
		}
		amountCell := func(name string) (string, error) {
			if value, ok := number(name); ok {
				return decimal(value)
			}
			return profileAmount(get(name), p.DecimalSeparator, p.ThousandsSeparator)
		}
		dateCell := func(name string) (string, error) {
			value := get(name)
			if strings.HasPrefix(o.DateLayout, "EXCEL-") {
				if normalized, ok := number(name); ok {
					value = normalized
				}
			}
			return sourceDate(value, o)
		}
		date, err := dateCell(p.DateColumn)
		if err != nil {
			return d, err
		}
		amount := ""
		if p.AmountColumn != "" {
			amount, err = amountCell(p.AmountColumn)
		} else {
			debit, credit := get(p.DebitColumn), get(p.CreditColumn)
			if debit == "" && credit == "" {
				return d, invalid("IMPORT_AMOUNT_INVALID", "at least one debit or credit amount is required")
			}
			if debit == "" {
				debit = "0"
			}
			if credit == "" {
				credit = "0"
			}
			if value, ok := number(p.DebitColumn); ok {
				debit, err = decimal(value)
			} else {
				debit, err = profileAmount(debit, p.DecimalSeparator, p.ThousandsSeparator)
			}
			if err != nil {
				return d, err
			}
			if value, ok := number(p.CreditColumn); ok {
				credit, err = decimal(value)
			} else {
				credit, err = profileAmount(credit, p.DecimalSeparator, p.ThousandsSeparator)
			}
			if err != nil {
				return d, err
			}
			if strings.HasPrefix(debit, "-") || strings.HasPrefix(credit, "-") || (debit != "0.00" && credit != "0.00") {
				return d, invalid("IMPORT_SIGN_INVALID", "debit and credit columns require nonnegative amounts on at most one side")
			}
			amount, err = decimalSum(credit, negate(debit))
		}
		if err != nil {
			return d, err
		}
		if p.InvertSign {
			amount = negate(amount)
		}
		accountID := get(p.AccountColumn)
		currency := get(p.CurrencyColumn)
		if p.AccountColumn != "" && accountID == "" || p.CurrencyColumn != "" && currency == "" {
			return d, invalid("IMPORT_ACCOUNT_REQUIRED", "configured account and currency columns cannot be empty")
		}
		a, err := sourceAccount(o, "TABULAR", accountID, currency, "")
		if err != nil {
			return d, err
		}
		index, ok := accounts[a.Key]
		if !ok {
			if len(d.Accounts) >= MaxAccounts {
				return d, invalid("IMPORT_LIMIT_EXCEEDED", "too many accounts")
			}
			index = len(d.Accounts)
			accounts[a.Key] = index
			d.Accounts = append(d.Accounts, a)
		}
		t := Transaction{ID: get(p.IDColumn), Type: format, PostedDate: date, PostedAt: get(p.DateColumn), Amount: amount, Fields: map[string]string{"row": fmt.Sprint(ri + p.HeaderRow + 1)}}
		if p.IDColumn != "" && t.ID == "" {
			return d, invalid("IMPORT_ID_REQUIRED", "configured ID column contains an empty identifier")
		}
		for _, c := range p.DescriptionColumns {
			if v := get(c); v != "" {
				if t.Description != "" {
					t.Description += " — "
				}
				t.Description += v
			}
		}
		if p.ValueDateColumn != "" && get(p.ValueDateColumn) != "" {
			t.ValueAt, err = dateCell(p.ValueDateColumn)
			if err != nil {
				return d, err
			}
			t.Fields["value_date_raw"] = get(p.ValueDateColumn)
		}
		if p.StatusColumn != "" {
			t.Status = p.StatusValues[get(p.StatusColumn)]
			if t.Status == "" {
				return d, invalid("IMPORT_STATUS_UNKNOWN", "source status has no explicit mapping")
			}
		}
		for i, v := range row {
			name := header[i]
			if name == "" {
				name = fmt.Sprintf("column_%d", i+1)
			}
			t.Fields["column:"+name] = v
		}
		d.Accounts[index].Transactions = append(d.Accounts[index].Transactions, t)
	}
	return d, nil
}
