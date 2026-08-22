package importer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type qboReport struct {
	Header struct {
		ReportName  string `json:"ReportName"`
		ReportBasis string `json:"ReportBasis"`
		StartPeriod string `json:"StartPeriod"`
		EndPeriod   string `json:"EndPeriod"`
		Currency    string `json:"Currency"`
	} `json:"Header"`
	Columns struct {
		Columns []qboReportColumn `json:"Column"`
	} `json:"Columns"`
	Rows qboReportRows `json:"Rows"`
}

type qboReportColumn struct {
	Title    string `json:"ColTitle"`
	Type     string `json:"ColType"`
	Metadata []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"MetaData"`
}

type qboReportRows struct {
	Rows []qboReportRow `json:"Row"`
}

type qboReportRow struct {
	Header *struct {
		Cells []qboReportCell `json:"ColData"`
	} `json:"Header"`
	Rows  *qboReportRows  `json:"Rows"`
	Cells []qboReportCell `json:"ColData"`
	Type  string          `json:"type"`
}

type qboReportCell struct {
	Value string `json:"value"`
	ID    string `json:"id"`
}

type generalLedgerPosting struct {
	date, transactionType, number, name, memo     string
	transactionID, accountID, accountName, amount string
	ordinal                                       int
}

func parseGeneralLedger(state *entityState, source Source) error {
	data, err := readImportFile(source.Path, maxJSONImportBytes, "GeneralLedger JSON")
	if err != nil {
		return fmt.Errorf("read general ledger: %w", err)
	}
	fileDigest := bytesSHA256(data)
	var report qboReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode general ledger: %w", err)
	}
	if report.Header.ReportName != "GeneralLedger" {
		return fmt.Errorf("%s is %q, not a GeneralLedger report", source.Path, report.Header.ReportName)
	}
	if !strings.EqualFold(report.Header.ReportBasis, "Accrual") {
		return fmt.Errorf("%s uses %q basis; only Accrual is supported", source.Path, report.Header.ReportBasis)
	}
	if !strings.EqualFold(report.Header.Currency, state.request.Currency) {
		return fmt.Errorf("%s currency %q does not match entity currency %q", source.Path, report.Header.Currency, state.request.Currency)
	}
	if err := validateISODate(report.Header.StartPeriod, "report start period"); err != nil {
		return fmt.Errorf("%s: %w", source.Path, err)
	}
	if err := validateISODate(report.Header.EndPeriod, "report end period"); err != nil {
		return fmt.Errorf("%s: %w", source.Path, err)
	}
	if report.Header.StartPeriod > source.StartDate || report.Header.EndPeriod < source.EndDate {
		return fmt.Errorf("%s report interval %s through %s does not cover declared source interval %s through %s",
			source.Path, report.Header.StartPeriod, report.Header.EndPeriod, source.StartDate, source.EndDate)
	}
	columnIndex, err := generalLedgerColumns(report.Columns.Columns)
	if err != nil {
		return fmt.Errorf("%s: %w", source.Path, err)
	}
	groups := map[string][]generalLedgerPosting{}
	var ordinal int
	var walk func(rows []qboReportRow, ancestors []qboReportCell)
	walk = func(rows []qboReportRow, ancestors []qboReportCell) {
		for _, row := range rows {
			if row.Header != nil {
				next := ancestors
				if len(row.Header.Cells) > 0 {
					next = append(append([]qboReportCell(nil), ancestors...), row.Header.Cells[0])
				}
				if row.Rows != nil {
					walk(row.Rows.Rows, next)
				}
				continue
			}
			if row.Rows != nil && len(row.Cells) == 0 {
				walk(row.Rows.Rows, ancestors)
				continue
			}
			if len(row.Cells) == 0 {
				continue
			}
			ordinal++
			cells := paddedReportCells(row.Cells, len(report.Columns.Columns))
			dateValue := cellValue(cells, columnIndex["tx_date"])
			transactionID := cellID(cells, columnIndex["txn_type"])
			if transactionID == "" {
				// Beginning balances and report subtotals have no transaction metadata.
				continue
			}
			date, dateErr := parseDate(dateValue)
			locator := fmt.Sprintf("GeneralLedger#/data/%d", ordinal)
			if dateErr != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_DATE_INVALID", state.request, source.Path, locator,
					"transaction:"+transactionID, dateErr.Error())
				continue
			}
			if date < report.Header.StartPeriod || date > report.Header.EndPeriod {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_DATE_OUTSIDE_REPORT", state.request, source.Path, locator,
					"transaction:"+transactionID, fmt.Sprintf("posting date %s is outside report interval %s through %s", date, report.Header.StartPeriod, report.Header.EndPeriod))
				continue
			}
			if !dateIncluded(date, state.request, source) {
				continue
			}
			account := deepestAccountAncestor(ancestors)
			if account.ID == "" {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_ACCOUNT_METADATA_MISSING", state.request, source.Path, locator,
					"transaction:"+transactionID, "transaction row has no account ID in its ancestor headers")
				continue
			}
			amount := cellValue(cells, columnIndex["subt_nat_amount"])
			if strings.TrimSpace(amount) == "" {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_AMOUNT_MISSING", state.request, source.Path, locator,
					"transaction:"+transactionID, "transaction row has no natural-balance amount")
				continue
			}
			groups[transactionID] = append(groups[transactionID], generalLedgerPosting{
				date: date, transactionType: cellValue(cells, columnIndex["txn_type"]),
				number: cellValue(cells, columnIndex["doc_num"]), name: cellValue(cells, columnIndex["name"]),
				memo: cellValue(cells, columnIndex["memo"]), transactionID: transactionID,
				accountID: account.ID, accountName: account.Value, amount: amount, ordinal: ordinal,
			})
		}
	}
	walk(report.Rows.Rows, nil)

	transactionIDs := make([]string, 0, len(groups))
	for transactionID := range groups {
		transactionIDs = append(transactionIDs, transactionID)
	}
	sort.Strings(transactionIDs)
	for _, transactionID := range transactionIDs {
		postings := groups[transactionID]
		journal := rawJournal{
			entityCode: state.request.EntityCode, bookCode: state.request.BookCode,
			postingDate: postings[0].date, period: periodFor(postings[0].date),
			description: journalDescription(postings[0].transactionType, postings[0].name, postings[0].memo),
			reference:   strings.TrimSpace(postings[0].number), sourceSystem: "QBO", sourceKey: "transaction:" + transactionID,
			evidence: Evidence{SourceKind: SourceGeneralLedger, SourcePath: source.Path, SourceSHA256: fileDigest,
				Locator: fmt.Sprintf("GeneralLedger#/transaction-metadata/%s", transactionID)},
		}
		invalid := false
		for _, posting := range postings {
			locator := fmt.Sprintf("GeneralLedger#/data/%d", posting.ordinal)
			if posting.date != journal.postingDate {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_TRANSACTION_DATE_CONFLICT", state.request, source.Path, locator,
					journal.sourceKey, fmt.Sprintf("QBO transaction ID has dates %s and %s", journal.postingDate, posting.date))
				invalid = true
				continue
			}
			account, err := state.catalog.resolveID(posting.accountID)
			if err != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_ACCOUNT_UNRESOLVED", state.request, source.Path, locator, journal.sourceKey,
					fmt.Sprintf("ancestor account %q (%s): %v", posting.accountName, posting.accountID, err))
				invalid = true
				continue
			}
			amount, err := parseCents(posting.amount)
			if err != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "GL_AMOUNT_INVALID", state.request, source.Path, locator, journal.sourceKey, err.Error())
				invalid = true
				continue
			}
			if amount == 0 {
				continue
			}
			addSignedLine(&journal.lines, account.key, posting.memo, amount, account.normalBalance == "DEBIT")
		}
		if invalid {
			continue
		}
		journal.evidence.PayloadSHA256 = rawJournalDigest(journal)
		if len(journal.lines) == 0 {
			appendDiagnostic(&state.diagnostics, SeverityInfo, "GL_ZERO_TRANSACTION_SKIPPED", state.request, source.Path,
				journal.evidence.Locator, journal.sourceKey, "zero-value or voided transaction has no ledger posting")
			continue
		}
		if err := validateRawJournal(journal); err != nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "GL_JOURNAL_INVALID", state.request, source.Path,
				journal.evidence.Locator, journal.sourceKey, err.Error())
			continue
		}
		state.journals = append(state.journals, journal)
	}
	return nil
}

func generalLedgerColumns(columns []qboReportColumn) (map[string]int, error) {
	wanted := []string{"tx_date", "txn_type", "doc_num", "name", "memo", "subt_nat_amount"}
	result := map[string]int{}
	for index, column := range columns {
		for _, metadata := range column.Metadata {
			if metadata.Name == "ColKey" {
				result[metadata.Value] = index
			}
		}
	}
	for _, key := range wanted {
		if _, ok := result[key]; !ok {
			return nil, fmt.Errorf("GeneralLedger column metadata %q is missing", key)
		}
	}
	return result, nil
}

func paddedReportCells(cells []qboReportCell, length int) []qboReportCell {
	if len(cells) >= length {
		return cells
	}
	result := make([]qboReportCell, length)
	copy(result, cells)
	return result
}

func cellValue(cells []qboReportCell, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index].Value
}

func cellID(cells []qboReportCell, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index].ID
}

func deepestAccountAncestor(ancestors []qboReportCell) qboReportCell {
	for index := len(ancestors) - 1; index >= 0; index-- {
		if strings.TrimSpace(ancestors[index].ID) != "" {
			return ancestors[index]
		}
	}
	return qboReportCell{}
}
