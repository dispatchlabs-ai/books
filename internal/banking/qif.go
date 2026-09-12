package banking

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func strconvItoa(v int) string { return strconv.Itoa(v) }
func parseQIF(data []byte, o Options) (Document, error) {
	doc := Document{Format: "QIF", Version: "register", Fields: map[string]string{}}
	text, e := textInput(data, o)
	if e != nil {
		return doc, e
	}
	var section string
	var record []string
	currentName := o.AccountID
	kind := "BANK"
	accountIndex := map[string]int{}
	metadata := 0
	addRecord := func() error {
		if len(record) == 0 {
			return invalid("QIF_RECORD_INVALID", "empty QIF record")
		}
		fields := map[byte][]string{}
		for _, line := range record {
			if line == "" {
				return invalid("QIF_RECORD_INVALID", "empty field")
			}
			fields[line[0]] = append(fields[line[0]], line[1:])
		}
		single := func(key byte) (string, error) {
			v := fields[key]
			if len(v) > 1 {
				return "", invalid("QIF_FIELD_INVALID", "repeated singleton field")
			}
			if len(v) == 0 {
				return "", nil
			}
			return strings.TrimSpace(v[0]), nil
		}
		if section == "Account" {
			name, err := single('N')
			if err != nil || name == "" {
				return invalid("QIF_ACCOUNT_INVALID", "QIF account requires one name")
			}
			currentName = name
			typ, err := single('T')
			if err != nil {
				return err
			}
			switch typ {
			case "Bank", "Cash", "Oth A", "":
				kind = "BANK"
			case "CCard":
				kind = "CREDIT_CARD"
			default:
				return invalid("QIF_ACCOUNT_UNSUPPORTED", "only bank, cash, other-asset, and credit-card registers are supported")
			}
			metadata++
			v, _ := json.Marshal(record)
			doc.Fields[fmt.Sprintf("account:%d", metadata)] = string(v)
			return nil
		}
		if section == "Cat" || section == "Class" {
			metadata++
			v, _ := json.Marshal(record)
			doc.Fields[fmt.Sprintf("%s:%d", section, metadata)] = string(v)
			return nil
		}
		switch section {
		case "Bank", "Cash", "Oth A":
			kind = "BANK"
		case "CCard":
			kind = "CREDIT_CARD"
		default:
			return invalid("QIF_SECTION_UNSUPPORTED", "only account registers, account lists, categories, and classes are supported")
		}
		for k := range fields {
			if !strings.ContainsRune("DTUCNPMALSE$%", rune(k)) {
				return invalid("QIF_FIELD_UNSUPPORTED", "unknown register field")
			}
		}
		rawDate, err := single('D')
		if err != nil {
			return err
		}
		rawDate = strings.ReplaceAll(strings.ReplaceAll(rawDate, "'", "/"), " ", "")
		date, err := sourceDate(rawDate, o)
		if err != nil {
			return err
		}
		rawAmount, err := single('T')
		if err != nil {
			return err
		}
		u, err := single('U')
		if err != nil {
			return err
		}
		if rawAmount == "" {
			rawAmount = u
		}
		amount, err := profileAmount(rawAmount, ".", ",")
		if err != nil {
			return err
		}
		if u != "" {
			other, err := profileAmount(u, ".", ",")
			if err != nil || !sameAmount(amount, other) {
				return invalid("QIF_AMOUNT_CONFLICT", "T and U amounts disagree")
			}
		}
		a, err := sourceAccount(o, "QIF", currentName, o.Currency, kind)
		if err != nil {
			return err
		}
		index, ok := accountIndex[a.Key]
		if !ok {
			if len(doc.Accounts) >= MaxAccounts {
				return invalid("IMPORT_LIMIT_EXCEEDED", "too many QIF accounts")
			}
			index = len(doc.Accounts)
			accountIndex[a.Key] = index
			doc.Accounts = append(doc.Accounts, a)
		}
		t := Transaction{Type: "QIF", PostedDate: date, PostedAt: rawDate, Amount: amount, Fields: map[string]string{}}
		for _, key := range []byte{'C', 'N', 'P', 'M', 'L'} {
			v, err := single(key)
			if err != nil {
				return err
			}
			if v != "" {
				t.Fields[string(key)] = v
			}
		}
		t.Description = strings.TrimSpace(strings.Join([]string{t.Fields["P"], t.Fields["M"]}, " "))
		if len(fields['A']) > 6 {
			return invalid("QIF_FIELD_INVALID", "too many address lines")
		}
		if len(fields['A']) > 0 {
			t.Fields["A"] = strings.Join(fields['A'], "\n")
		}
		var split *Detail
		var sum []string
		flush := func() error {
			if split == nil {
				return nil
			}
			if split.Amount == "" {
				return invalid("QIF_SPLIT_INVALID", "every split requires an amount")
			}
			t.Details = append(t.Details, *split)
			sum = append(sum, split.Amount)
			split = nil
			return nil
		}
		for _, line := range record {
			switch line[0] {
			case 'S':
				if err := flush(); err != nil {
					return err
				}
				split = &Detail{Fields: map[string]string{"category": line[1:]}}
			case 'E':
				if split == nil || split.Description != "" {
					return invalid("QIF_SPLIT_INVALID", "orphaned or repeated split memo")
				}
				split.Description = line[1:]
			case '$':
				if split == nil || split.Amount != "" {
					return invalid("QIF_SPLIT_INVALID", "orphaned or repeated split amount")
				}
				split.Amount, err = profileAmount(line[1:], ".", ",")
				if err != nil {
					return err
				}
			case '%':
				return invalid("QIF_SPLIT_UNSUPPORTED", "percentage-only splits require explicit monetary amounts")
			}
		}
		if err := flush(); err != nil {
			return err
		}
		if len(sum) > 0 {
			total, err := decimalSum(sum...)
			if err != nil || !sameAmount(total, amount) {
				return invalid("QIF_SPLIT_TOTAL_MISMATCH", "split amounts do not equal the register amount")
			}
		}
		doc.Accounts[index].Transactions = append(doc.Accounts[index].Transactions, t)
		if len(doc.Accounts[index].Transactions) > MaxTransactions {
			return invalid("IMPORT_LIMIT_EXCEEDED", "too many QIF transactions")
		}
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > MaxNodes {
		return doc, invalid("IMPORT_LIMIT_EXCEEDED", "too many QIF lines")
	}
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if len(line) > MaxText {
			return doc, invalid("IMPORT_LIMIT_EXCEEDED", "QIF field exceeds the text limit")
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "!") {
			if len(record) > 0 {
				return doc, invalid("QIF_TRUNCATED", "record is missing its terminator")
			}
			switch {
			case line == "!Account":
				section = "Account"
			case strings.HasPrefix(line, "!Type:"):
				section = strings.TrimPrefix(line, "!Type:")
			case line == "!Option:AllXfr", line == "!Option:AutoSwitch", line == "!Clear:AutoSwitch":
			default:
				return doc, invalid("QIF_HEADER_UNSUPPORTED", "unknown QIF section or option")
			}
			continue
		}
		if line == "^" {
			if e = addRecord(); e != nil {
				return Document{}, e
			}
			record = nil
		} else {
			record = append(record, line)
		}
	}
	if len(record) > 0 {
		return doc, invalid("QIF_TRUNCATED", "QIF record is missing its terminator")
	}
	return doc, nil
}
