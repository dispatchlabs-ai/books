package importer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type qboAccount struct {
	ID                 string `json:"Id"`
	Name               string `json:"Name"`
	FullyQualifiedName string `json:"FullyQualifiedName"`
	AccountNum         string `json:"AcctNum"`
	AccountType        string `json:"AccountType"`
	AccountSubtype     string `json:"AccountSubType"`
	Classification     string `json:"Classification"`
	Active             *bool  `json:"Active"`
}

func loadAccountCatalog(entity EntityRequest) (*accountCatalog, error) {
	path := entity.AccountCatalogPath
	if path == "" {
		for _, source := range entity.Sources {
			if source.Kind == SourceQBOObjectDir {
				path = filepath.Join(source.Path, "Account.json")
				break
			}
		}
	}
	if path == "" {
		return nil, fmt.Errorf("entity %s requires a QBO account catalog", entity.EntityCode)
	}
	rows, err := readJSONRows(path)
	if err != nil {
		return nil, fmt.Errorf("load account catalog for %s: %w", entity.EntityCode, err)
	}
	fileDigest, err := fileSHA256(path)
	if err != nil {
		return nil, fmt.Errorf("hash account catalog for %s: %w", entity.EntityCode, err)
	}
	catalog := &accountCatalog{
		entityCode:    entity.EntityCode,
		byID:          map[string]*accountRecord{},
		byInternalKey: map[string]*accountRecord{},
		byNumber:      map[string][]*accountRecord{},
		byName:        map[string][]*accountRecord{},
	}
	for index, raw := range rows {
		var source qboAccount
		if err := json.Unmarshal(raw, &source); err != nil {
			return nil, fmt.Errorf("decode account row %d for %s: %w", index, entity.EntityCode, err)
		}
		if strings.TrimSpace(source.ID) == "" {
			return nil, fmt.Errorf("account row %d for %s has no QBO ID", index, entity.EntityCode)
		}
		record, err := accountFromQBO(entity, source, Evidence{
			SourceKind: SourceQBOObjectDir, SourcePath: path, SourceSHA256: fileDigest,
			Locator: fmt.Sprintf("Account.json#/rows/%d", index), PayloadSHA256: bytesSHA256(raw),
		})
		if err != nil {
			return nil, fmt.Errorf("account %s for %s: %w", source.ID, entity.EntityCode, err)
		}
		if _, exists := catalog.byID[record.externalID]; exists {
			return nil, fmt.Errorf("duplicate QBO account ID %s for %s", record.externalID, entity.EntityCode)
		}
		catalog.add(record)
	}
	return catalog, nil
}

func (catalog *accountCatalog) add(record *accountRecord) {
	catalog.byID[record.externalID] = record
	catalog.byInternalKey[record.key] = record
	if record.accountNum != "" {
		catalog.byNumber[normalizeCode(record.accountNum)] = append(catalog.byNumber[normalizeCode(record.accountNum)], record)
	}
	for _, name := range []string{record.name, record.fullyQualified} {
		if key := normalizeName(name); key != "" {
			catalog.byName[key] = append(catalog.byName[key], record)
		}
	}
}

func accountFromQBO(entity EntityRequest, source qboAccount, evidence Evidence) (*accountRecord, error) {
	typeName, err := canonicalAccountType(source.Classification, source.AccountType)
	if err != nil {
		return nil, err
	}
	active := true
	if source.Active != nil {
		active = *source.Active
	}
	accountNum := strings.TrimSpace(source.AccountNum)
	normalBalance := "CREDIT"
	statementSection := "BALANCE_SHEET"
	if typeName == "ASSET" || typeName == "EXPENSE" {
		normalBalance = "DEBIT"
	}
	if typeName == "REVENUE" || typeName == "EXPENSE" {
		statementSection = "INCOME_STATEMENT"
	}
	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = strings.TrimSpace(source.FullyQualifiedName)
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return &accountRecord{
		key:        entity.EntityCode + "\x00QBO\x00" + strings.TrimSpace(source.ID),
		entityCode: entity.EntityCode, externalID: strings.TrimSpace(source.ID), accountNum: accountNum,
		name: name, fullyQualified: strings.TrimSpace(source.FullyQualifiedName), accountType: typeName,
		subtype: canonicalSubtype(source.AccountType, source.AccountSubtype), normalBalance: normalBalance,
		statementSection: statementSection, active: active, evidence: evidence,
	}, nil
}

func canonicalSubtype(accountType, accountSubtype string) string {
	switch normalizeName(accountType) {
	case "BANK":
		return "BANK"
	case "ACCOUNTS RECEIVABLE":
		return "ACCOUNTS-RECEIVABLE"
	case "ACCOUNTS PAYABLE":
		return "ACCOUNTS-PAYABLE"
	case "CREDIT CARD":
		return "CREDIT-CARD"
	default:
		return normalizeSubtype(accountSubtype)
	}
}

func canonicalAccountType(classification, accountType string) (string, error) {
	switch normalizeName(classification) {
	case "ASSET":
		return "ASSET", nil
	case "LIABILITY":
		return "LIABILITY", nil
	case "EQUITY":
		return "EQUITY", nil
	case "REVENUE":
		return "REVENUE", nil
	case "EXPENSE":
		return "EXPENSE", nil
	}
	switch normalizeName(accountType) {
	case "BANK", "ACCOUNTS RECEIVABLE", "OTHER CURRENT ASSET", "FIXED ASSET", "OTHER ASSET":
		return "ASSET", nil
	case "ACCOUNTS PAYABLE", "CREDIT CARD", "OTHER CURRENT LIABILITY", "LONG TERM LIABILITY":
		return "LIABILITY", nil
	case "EQUITY":
		return "EQUITY", nil
	case "INCOME", "OTHER INCOME":
		return "REVENUE", nil
	case "EXPENSE", "OTHER EXPENSE", "COST OF GOODS SOLD":
		return "EXPENSE", nil
	default:
		return "", fmt.Errorf("unsupported QBO classification %q and account type %q", classification, accountType)
	}
}

func (catalog *accountCatalog) resolveID(id string) (*accountRecord, error) {
	id = strings.TrimSpace(id)
	if record := catalog.byID[id]; record != nil {
		return record, nil
	}
	return nil, fmt.Errorf("QBO account ID %q is absent from the account catalog", id)
}

func (catalog *accountCatalog) resolveHistorical(value string) (*accountRecord, error) {
	name := strings.TrimSpace(value)
	if number := leadingAccountNumber(name); number != "" {
		matches := catalog.byNumber[number]
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("account number %s matches %d QBO accounts", number, len(matches))
		}
	}
	matches := catalog.byName[normalizeName(name)]
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("account name %q matches %d QBO accounts", name, len(matches))
	}
	return nil, fmt.Errorf("historical account %q is absent from the QBO account catalog", name)
}

func leadingAccountNumber(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 || len(fields[0]) < 4 || len(fields[0]) > 12 {
		return ""
	}
	for _, r := range fields[0] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return fields[0]
}

func semanticSignature(record *accountRecord) string {
	return strings.Join([]string{record.accountType, record.subtype, normalizeName(record.name)}, "\x00")
}

type accountUsage struct {
	record   *accountRecord
	bookCode string
	first    string
	last     string
}

func reconcileAccounts(states []*entityState) ([]MasterAccount, map[string]string, []Diagnostic, error) {
	usageByKeyBook := map[string]*accountUsage{}
	records := map[string]*accountRecord{}
	for _, state := range states {
		// The supplied account catalog is part of the reviewed initial setup, not
		// merely a lookup table for journal lines. Preserve every active account,
		// including operational accounts with zero activity in the selected GL
		// interval.
		for _, record := range state.catalog.byInternalKey {
			if !record.active {
				continue
			}
			records[record.key] = record
			usageKey := record.key + "\x00" + state.request.BookCode
			usageByKeyBook[usageKey] = &accountUsage{
				record: record, bookCode: state.request.BookCode,
				first: state.request.StartDate, last: state.request.CutoffDate,
			}
		}
		for _, journal := range state.journals {
			for _, line := range journal.lines {
				record := state.catalog.byKey(line.accountKey)
				if record == nil {
					return nil, nil, nil, fmt.Errorf("internal account key %q is missing", line.accountKey)
				}
				records[record.key] = record
				usageKey := record.key + "\x00" + state.request.BookCode
				usage := usageByKeyBook[usageKey]
				if usage == nil {
					usage = &accountUsage{record: record, bookCode: state.request.BookCode, first: journal.postingDate, last: journal.postingDate}
					usageByKeyBook[usageKey] = usage
				}
				if journal.postingDate < usage.first {
					usage.first = journal.postingDate
				}
				if journal.postingDate > usage.last {
					usage.last = journal.postingDate
				}
			}
		}
	}

	groups := map[string][]*accountRecord{}
	for _, record := range records {
		groupKey := "NO_NUMBER\x00" + record.key
		if record.accountNum != "" {
			groupKey = "NUMBER\x00" + normalizeCode(record.accountNum)
		}
		groups[groupKey] = append(groups[groupKey], record)
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	codeByKey := map[string]string{}
	var diagnostics []Diagnostic
	usedCodes := map[string]bool{}
	for _, groupKey := range groupKeys {
		group := groups[groupKey]
		sort.Slice(group, func(i, j int) bool { return group[i].key < group[j].key })
		if strings.HasPrefix(groupKey, "NO_NUMBER\x00") {
			record := group[0]
			codeByKey[record.key] = uniqueAccountCode(record.entityCode+"-QBO-"+record.externalID, record, usedCodes)
			continue
		}
		number := strings.TrimPrefix(groupKey, "NUMBER\x00")
		canUnify := true
		seenEntity := map[string]bool{}
		signature := semanticSignature(group[0])
		for _, record := range group {
			if semanticSignature(record) != signature || seenEntity[record.entityCode] {
				canUnify = false
			}
			seenEntity[record.entityCode] = true
		}
		if canUnify {
			code := uniqueAccountCode(number, group[0], usedCodes)
			for _, record := range group {
				codeByKey[record.key] = code
			}
			continue
		}
		entities := make([]string, 0, len(group))
		for _, record := range group {
			entities = append(entities, record.entityCode+":"+record.externalID)
		}
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityWarning, Code: "ACCOUNT_NUMBER_CONFLICT",
			Message: fmt.Sprintf("account number %s has conflicting or duplicate realm semantics (%s); entity-prefixed master codes were assigned", number, strings.Join(entities, ", ")),
		})
		seenBase := map[string]bool{}
		for _, record := range group {
			base := record.entityCode + "-" + number
			if seenBase[base] {
				base += "-QBO-" + record.externalID
			}
			seenBase[record.entityCode+"-"+number] = true
			codeByKey[record.key] = uniqueAccountCode(base, record, usedCodes)
		}
	}

	masterByCode := map[string]*MasterAccount{}
	for key, record := range records {
		code := codeByKey[key]
		master := masterByCode[code]
		if master == nil {
			master = &MasterAccount{
				Code: code, Name: record.name, Type: record.accountType, Subtype: record.subtype,
				NormalBalance: record.normalBalance, StatementSection: record.statementSection,
			}
			masterByCode[code] = master
		}
		master.Identities = append(master.Identities, ExternalIdentity{
			EntityCode: record.entityCode, SourceSystem: "QBO", ExternalID: record.externalID,
			AccountNum: record.accountNum, Name: record.name, Active: record.active, Evidence: record.evidence,
		})
	}
	for _, usage := range usageByKeyBook {
		master := masterByCode[codeByKey[usage.record.key]]
		activation := BookActivation{BookCode: usage.bookCode, ActiveFrom: usage.first, PostingEnabled: true}
		if !usage.record.active {
			activation.ActiveTo = usage.last
		}
		master.Activations = append(master.Activations, activation)
	}
	accounts := make([]MasterAccount, 0, len(masterByCode))
	for _, master := range masterByCode {
		sort.Slice(master.Identities, func(i, j int) bool {
			a, b := master.Identities[i], master.Identities[j]
			if a.EntityCode != b.EntityCode {
				return a.EntityCode < b.EntityCode
			}
			return a.ExternalID < b.ExternalID
		})
		sort.Slice(master.Activations, func(i, j int) bool { return master.Activations[i].BookCode < master.Activations[j].BookCode })
		accounts = append(accounts, *master)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Code < accounts[j].Code })
	return accounts, codeByKey, diagnostics, nil
}

func (catalog *accountCatalog) byKey(key string) *accountRecord {
	return catalog.byInternalKey[key]
}

func uniqueAccountCode(base string, record *accountRecord, used map[string]bool) string {
	code := normalizeCode(base)
	if len(code) > 64 {
		code = code[:55] + "-" + bytesSHA256([]byte(record.key))[:8]
	}
	if !used[code] {
		used[code] = true
		return code
	}
	suffix := bytesSHA256([]byte(record.key))[:8]
	if len(code) > 55 {
		code = code[:55]
	}
	code += "-" + suffix
	used[code] = true
	return code
}
