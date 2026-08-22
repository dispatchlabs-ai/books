package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type qboRef struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type qboObject struct {
	ID                  string      `json:"Id"`
	TxnDate             string      `json:"TxnDate"`
	DocNumber           string      `json:"DocNumber"`
	PrivateNote         string      `json:"PrivateNote"`
	TotalAmount         json.Number `json:"TotalAmt"`
	Amount              json.Number `json:"Amount"`
	Credit              bool        `json:"Credit"`
	PaymentType         string      `json:"PaymentType"`
	CurrencyRef         qboRef      `json:"CurrencyRef"`
	EntityRef           qboRef      `json:"EntityRef"`
	CustomerRef         qboRef      `json:"CustomerRef"`
	AccountRef          qboRef      `json:"AccountRef"`
	DepositToAccountRef qboRef      `json:"DepositToAccountRef"`
	FromAccountRef      qboRef      `json:"FromAccountRef"`
	ToAccountRef        qboRef      `json:"ToAccountRef"`
	ARAccountRef        qboRef      `json:"ARAccountRef"`
	APAccountRef        qboRef      `json:"APAccountRef"`
	Lines               []qboLine   `json:"Line"`
}

type qboLine struct {
	ID                            string      `json:"Id"`
	Description                   string      `json:"Description"`
	DetailType                    string      `json:"DetailType"`
	Amount                        json.Number `json:"Amount"`
	AccountBasedExpenseLineDetail struct {
		AccountRef qboRef `json:"AccountRef"`
	} `json:"AccountBasedExpenseLineDetail"`
	DepositLineDetail struct {
		AccountRef qboRef `json:"AccountRef"`
	} `json:"DepositLineDetail"`
	SalesItemLineDetail struct {
		ItemAccountRef qboRef `json:"ItemAccountRef"`
		ItemRef        qboRef `json:"ItemRef"`
	} `json:"SalesItemLineDetail"`
	JournalEntryLineDetail struct {
		AccountRef  qboRef `json:"AccountRef"`
		PostingType string `json:"PostingType"`
	} `json:"JournalEntryLineDetail"`
	LinkedTransactions []struct {
		TransactionID   string `json:"TxnId"`
		TransactionType string `json:"TxnType"`
	} `json:"LinkedTxn"`
}

type objectContentError struct {
	code    string
	message string
}

func (err objectContentError) Error() string { return err.message }

var qboTransactionFiles = []string{
	"Bill", "BillPayment", "CreditMemo", "Deposit", "Invoice", "JournalEntry",
	"Payment", "Purchase", "RefundReceipt", "SalesReceipt", "Transfer", "VendorCredit",
}

func parseQBOObjectDirectory(state *entityState, source Source) error {
	for _, objectType := range qboTransactionFiles {
		path := filepath.Join(source.Path, objectType+".json")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := parseQBOObjectFile(state, source, objectType, path); err != nil {
			return err
		}
	}
	return nil
}

func parseQBOObjectFile(state *entityState, source Source, objectType, path string) error {
	rows, err := readJSONRows(path)
	if err != nil {
		return err
	}
	fileDigest, err := fileSHA256(path)
	if err != nil {
		return err
	}
	for index, raw := range rows {
		var object qboObject
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("decode %s row %d: %w", objectType, index, err)
		}
		locator := fmt.Sprintf("%s.json#/rows/%d", objectType, index)
		date, err := parseDate(object.TxnDate)
		if err != nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "QBO_OBJECT_DATE_INVALID", state.request, path, locator, "", err.Error())
			continue
		}
		if !dateIncluded(date, state.request, source) {
			continue
		}
		sourceKey := "transaction:" + strings.TrimSpace(object.ID)
		if strings.TrimSpace(object.ID) == "" {
			appendDiagnostic(&state.diagnostics, SeverityError, "QBO_OBJECT_ID_MISSING", state.request, path, locator, "", "QBO object has no Id")
			continue
		}
		if !strings.EqualFold(object.CurrencyRef.Value, state.request.Currency) {
			appendDiagnostic(&state.diagnostics, SeverityError, "QBO_OBJECT_CURRENCY_UNSUPPORTED", state.request, path, locator, sourceKey,
				fmt.Sprintf("object currency %q does not match entity currency %q", object.CurrencyRef.Value, state.request.Currency))
			continue
		}
		if !supportedQBOObject(objectType) {
			appendDiagnostic(&state.diagnostics, SeverityError, "QBO_OBJECT_UNSUPPORTED", state.request, path, locator, sourceKey,
				fmt.Sprintf("%s has no reviewed deterministic posting rule", objectType))
			continue
		}
		journal := rawJournal{
			entityCode: state.request.EntityCode, bookCode: state.request.BookCode,
			postingDate: date, period: periodFor(date), sourceSystem: "QBO", sourceKey: sourceKey,
			description: qboObjectDescription(objectType, object), reference: strings.TrimSpace(object.DocNumber),
			evidence: Evidence{SourceKind: SourceQBOObjectDir, SourcePath: path, SourceSHA256: fileDigest,
				Locator: locator, PayloadSHA256: bytesSHA256(raw)}, payload: raw,
		}
		zero, err := populateQBOJournal(state.catalog, objectType, object, &journal)
		if err != nil {
			contentError, ok := err.(objectContentError)
			code := "QBO_OBJECT_INVALID"
			if ok {
				code = contentError.code
			}
			appendDiagnostic(&state.diagnostics, SeverityError, code, state.request, path, locator, sourceKey, err.Error())
			continue
		}
		if zero {
			appendDiagnostic(&state.diagnostics, SeverityInfo, "QBO_ZERO_OBJECT_SKIPPED", state.request, path, locator, sourceKey,
				"zero-value or voided object has no ledger posting")
			continue
		}
		if err := validateRawJournal(journal); err != nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "QBO_JOURNAL_INVALID", state.request, path, locator, sourceKey, err.Error())
			continue
		}
		state.journals = append(state.journals, journal)
	}
	return nil
}

func supportedQBOObject(objectType string) bool {
	switch objectType {
	case "Deposit", "Invoice", "JournalEntry", "Payment", "Purchase", "Transfer":
		return true
	default:
		return false
	}
}

func populateQBOJournal(catalog *accountCatalog, objectType string, object qboObject, journal *rawJournal) (bool, error) {
	switch objectType {
	case "Purchase":
		return populatePurchase(catalog, object, journal)
	case "Deposit":
		return populateDeposit(catalog, object, journal)
	case "Invoice":
		return populateInvoice(catalog, object, journal)
	case "Payment":
		return populatePayment(catalog, object, journal)
	case "JournalEntry":
		return populateJournalEntry(catalog, object, journal)
	case "Transfer":
		return populateTransfer(catalog, object, journal)
	default:
		return false, objectContentError{"QBO_OBJECT_UNSUPPORTED", objectType + " has no posting rule"}
	}
}

func populatePurchase(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	control, err := resolveObjectAccount(catalog, object.AccountRef, "purchase AccountRef")
	if err != nil {
		return false, err
	}
	total, err := numberCents(object.TotalAmount, "purchase TotalAmt")
	if err != nil {
		return false, err
	}
	var lineTotal int64
	for index, line := range object.Lines {
		if line.DetailType != "AccountBasedExpenseLineDetail" {
			return false, objectContentError{"QBO_LINE_TYPE_UNSUPPORTED", fmt.Sprintf("purchase line %d has unsupported DetailType %q", index+1, line.DetailType)}
		}
		account, err := resolveObjectAccount(catalog, line.AccountBasedExpenseLineDetail.AccountRef, fmt.Sprintf("purchase line %d AccountRef", index+1))
		if err != nil {
			return false, err
		}
		amount, err := numberCents(line.Amount, fmt.Sprintf("purchase line %d Amount", index+1))
		if err != nil {
			return false, err
		}
		if amount < 0 {
			return false, objectContentError{"QBO_AMOUNT_DIRECTION_UNSUPPORTED", fmt.Sprintf("purchase line %d has negative Amount", index+1)}
		}
		lineTotal += amount
		addSignedLine(&journal.lines, account.key, line.Description, amount, !object.Credit)
	}
	if lineTotal != total {
		return false, objectContentError{"QBO_TOTAL_MISMATCH", fmt.Sprintf("purchase line total %d does not equal TotalAmt %d cents", lineTotal, total)}
	}
	if total == 0 {
		return true, nil
	}
	addSignedLine(&journal.lines, control.key, journal.description, total, object.Credit)
	return false, nil
}

func populateDeposit(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	control, err := resolveObjectAccount(catalog, object.DepositToAccountRef, "deposit DepositToAccountRef")
	if err != nil {
		return false, err
	}
	total, err := numberCents(object.TotalAmount, "deposit TotalAmt")
	if err != nil {
		return false, err
	}
	var lineTotal int64
	for index, line := range object.Lines {
		if line.DetailType != "DepositLineDetail" {
			return false, objectContentError{"QBO_LINE_TYPE_UNSUPPORTED", fmt.Sprintf("deposit line %d has unsupported DetailType %q", index+1, line.DetailType)}
		}
		account, err := resolveObjectAccount(catalog, line.DepositLineDetail.AccountRef, fmt.Sprintf("deposit line %d AccountRef", index+1))
		if err != nil {
			return false, err
		}
		amount, err := numberCents(line.Amount, fmt.Sprintf("deposit line %d Amount", index+1))
		if err != nil {
			return false, err
		}
		lineTotal += amount
		addSignedLine(&journal.lines, account.key, line.Description, amount, false)
	}
	if lineTotal != total {
		return false, objectContentError{"QBO_TOTAL_MISMATCH", fmt.Sprintf("deposit line total %d does not equal TotalAmt %d cents", lineTotal, total)}
	}
	if total == 0 {
		return true, nil
	}
	addSignedLine(&journal.lines, control.key, journal.description, total, true)
	return false, nil
}

func populateInvoice(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	receivable, err := resolveControlAccount(catalog, object.ARAccountRef, "ACCOUNTS-RECEIVABLE", "invoice ARAccountRef")
	if err != nil {
		return false, err
	}
	total, err := numberCents(object.TotalAmount, "invoice TotalAmt")
	if err != nil {
		return false, err
	}
	var lineTotal int64
	for index, line := range object.Lines {
		if line.DetailType == "SubTotalLineDetail" {
			continue
		}
		if line.DetailType != "SalesItemLineDetail" {
			return false, objectContentError{"QBO_LINE_TYPE_UNSUPPORTED", fmt.Sprintf("invoice line %d has unsupported DetailType %q", index+1, line.DetailType)}
		}
		account, err := resolveObjectAccount(catalog, line.SalesItemLineDetail.ItemAccountRef, fmt.Sprintf("invoice line %d ItemAccountRef", index+1))
		if err != nil {
			return false, err
		}
		amount, err := numberCents(line.Amount, fmt.Sprintf("invoice line %d Amount", index+1))
		if err != nil {
			return false, err
		}
		lineTotal += amount
		addSignedLine(&journal.lines, account.key, line.Description, amount, false)
	}
	if lineTotal != total {
		return false, objectContentError{"QBO_TOTAL_MISMATCH", fmt.Sprintf("invoice sales-line total %d does not equal TotalAmt %d cents; tax, discount, or unsupported detail may be present", lineTotal, total)}
	}
	if total == 0 {
		return true, nil
	}
	addSignedLine(&journal.lines, receivable.key, journal.description, total, true)
	return false, nil
}

func populatePayment(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	deposit, err := resolveObjectAccount(catalog, object.DepositToAccountRef, "payment DepositToAccountRef")
	if err != nil {
		return false, err
	}
	receivable, err := resolveControlAccount(catalog, object.ARAccountRef, "ACCOUNTS-RECEIVABLE", "payment ARAccountRef")
	if err != nil {
		return false, err
	}
	total, err := numberCents(object.TotalAmount, "payment TotalAmt")
	if err != nil {
		return false, err
	}
	var allocationTotal int64
	for index, line := range object.Lines {
		amount, err := numberCents(line.Amount, fmt.Sprintf("payment line %d Amount", index+1))
		if err != nil {
			return false, err
		}
		allocationTotal += amount
	}
	if len(object.Lines) > 0 && allocationTotal != total {
		return false, objectContentError{"QBO_TOTAL_MISMATCH", fmt.Sprintf("payment allocation total %d does not equal TotalAmt %d cents", allocationTotal, total)}
	}
	if total == 0 {
		return true, nil
	}
	if total < 0 {
		return false, objectContentError{"QBO_AMOUNT_DIRECTION_UNSUPPORTED", "payment TotalAmt is negative"}
	}
	addSignedLine(&journal.lines, deposit.key, journal.description, total, true)
	addSignedLine(&journal.lines, receivable.key, journal.description, total, false)
	return false, nil
}

func populateJournalEntry(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	for index, line := range object.Lines {
		if line.DetailType != "JournalEntryLineDetail" {
			return false, objectContentError{"QBO_LINE_TYPE_UNSUPPORTED", fmt.Sprintf("journal-entry line %d has unsupported DetailType %q", index+1, line.DetailType)}
		}
		account, err := resolveObjectAccount(catalog, line.JournalEntryLineDetail.AccountRef, fmt.Sprintf("journal-entry line %d AccountRef", index+1))
		if err != nil {
			return false, err
		}
		amount, err := numberCents(line.Amount, fmt.Sprintf("journal-entry line %d Amount", index+1))
		if err != nil {
			return false, err
		}
		if amount < 0 {
			return false, objectContentError{"QBO_AMOUNT_DIRECTION_UNSUPPORTED", fmt.Sprintf("journal-entry line %d has negative Amount", index+1)}
		}
		switch strings.ToUpper(strings.TrimSpace(line.JournalEntryLineDetail.PostingType)) {
		case "DEBIT":
			addSignedLine(&journal.lines, account.key, line.Description, amount, true)
		case "CREDIT":
			addSignedLine(&journal.lines, account.key, line.Description, amount, false)
		default:
			return false, objectContentError{"QBO_POSTING_TYPE_UNSUPPORTED", fmt.Sprintf("journal-entry line %d has PostingType %q", index+1, line.JournalEntryLineDetail.PostingType)}
		}
	}
	if len(journal.lines) == 0 {
		return true, nil
	}
	return false, nil
}

func populateTransfer(catalog *accountCatalog, object qboObject, journal *rawJournal) (bool, error) {
	from, err := resolveObjectAccount(catalog, object.FromAccountRef, "transfer FromAccountRef")
	if err != nil {
		return false, err
	}
	to, err := resolveObjectAccount(catalog, object.ToAccountRef, "transfer ToAccountRef")
	if err != nil {
		return false, err
	}
	amount, err := numberCents(object.Amount, "transfer Amount")
	if err != nil {
		return false, err
	}
	if amount == 0 {
		return true, nil
	}
	if amount < 0 {
		return false, objectContentError{"QBO_AMOUNT_DIRECTION_UNSUPPORTED", "transfer Amount is negative"}
	}
	addSignedLine(&journal.lines, to.key, journal.description, amount, true)
	addSignedLine(&journal.lines, from.key, journal.description, amount, false)
	return false, nil
}

func resolveObjectAccount(catalog *accountCatalog, ref qboRef, field string) (*accountRecord, error) {
	if strings.TrimSpace(ref.Value) == "" {
		return nil, objectContentError{"QBO_ACCOUNT_REF_MISSING", field + " is missing"}
	}
	account, err := catalog.resolveID(ref.Value)
	if err != nil {
		return nil, objectContentError{"QBO_ACCOUNT_UNRESOLVED", field + ": " + err.Error()}
	}
	return account, nil
}

func resolveControlAccount(catalog *accountCatalog, ref qboRef, subtype, field string) (*accountRecord, error) {
	if strings.TrimSpace(ref.Value) != "" {
		return resolveObjectAccount(catalog, ref, field)
	}
	var matches []*accountRecord
	for _, account := range catalog.byID {
		if account.subtype == subtype {
			matches = append(matches, account)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].externalID < matches[j].externalID })
	if len(matches) != 1 {
		return nil, objectContentError{"QBO_CONTROL_ACCOUNT_AMBIGUOUS", fmt.Sprintf("%s is missing and catalog contains %d %s accounts", field, len(matches), subtype)}
	}
	return matches[0], nil
}

func numberCents(number json.Number, field string) (int64, error) {
	if strings.TrimSpace(number.String()) == "" {
		return 0, objectContentError{"QBO_AMOUNT_MISSING", field + " is missing"}
	}
	cents, err := parseCents(number.String())
	if err != nil {
		return 0, objectContentError{"QBO_AMOUNT_INVALID", field + ": " + err.Error()}
	}
	return cents, nil
}

func qboObjectDescription(objectType string, object qboObject) string {
	name := object.EntityRef.Name
	if name == "" {
		name = object.CustomerRef.Name
	}
	description := journalDescription(objectType, name, object.PrivateNote)
	if description == objectType && object.DocNumber != "" {
		description += " - " + strings.TrimSpace(object.DocNumber)
	}
	if description == objectType {
		description += " - " + object.ID
	}
	return description
}
