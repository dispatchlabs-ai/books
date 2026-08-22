package importer

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	maxXLSXCompressedBytes  int64  = 32 << 20
	maxXLSXMemberBytes      uint64 = 32 << 20
	maxXLSXExpandedBytes    uint64 = 64 << 20
	maxXLSXMembers                 = 256
	maxXLSXCompressionRatio uint64 = 1000
	maxXLSXSharedStrings           = 250_000
	maxXLSXSharedTextBytes         = 16 << 20
	maxXLSXSingleTextBytes         = 1 << 20
	maxXLSXRows                    = 250_000
	maxXLSXCells                   = 2_000_000
	maxXLSXCellsPerRow             = 256
)

type xlsxSharedStrings struct {
	Items []xlsxStringItem `xml:"si"`
}

type xlsxStringItem struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (item xlsxStringItem) value() string {
	if item.Text != "" {
		return item.Text
	}
	var result strings.Builder
	for _, run := range item.Runs {
		result.WriteString(run.Text)
	}
	return result.String()
}

type xlsxWorksheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Number int        `xml:"r,attr"`
	Cells  []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Reference string `xml:"r,attr"`
	Type      string `xml:"t,attr"`
	Value     string `xml:"v"`
	Inline    struct {
		Text string `xml:"t"`
	} `xml:"is"`
}

func parseJournalXLSX(state *entityState, source Source) error {
	info, err := os.Stat(source.Path)
	if err != nil {
		return fmt.Errorf("inspect journal workbook: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("journal workbook is not a regular file")
	}
	if info.Size() > maxXLSXCompressedBytes {
		return fmt.Errorf("journal workbook exceeds the %d-byte compressed input limit", maxXLSXCompressedBytes)
	}
	fileDigest, err := fileSHA256(source.Path)
	if err != nil {
		return fmt.Errorf("hash journal workbook: %w", err)
	}
	reader, err := zip.OpenReader(source.Path)
	if err != nil {
		return fmt.Errorf("open journal workbook: %w", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(reader)
	if err := validateXLSXArchive(reader.File); err != nil {
		return err
	}
	sharedData, err := readZipMember(reader.File, "xl/sharedStrings.xml", maxXLSXMemberBytes)
	if err != nil {
		return err
	}
	worksheetData, err := readZipMember(reader.File, "xl/worksheets/sheet1.xml", maxXLSXMemberBytes)
	if err != nil {
		return err
	}
	var sharedXML xlsxSharedStrings
	if err := xml.Unmarshal(sharedData, &sharedXML); err != nil {
		return fmt.Errorf("decode workbook shared strings: %w", err)
	}
	shared := make([]string, len(sharedXML.Items))
	for index, item := range sharedXML.Items {
		shared[index] = item.value()
	}
	var sheet xlsxWorksheet
	if err := xml.Unmarshal(worksheetData, &sheet); err != nil {
		return fmt.Errorf("decode workbook worksheet: %w", err)
	}
	if err := validateXLSXContent(sharedXML, sheet); err != nil {
		return err
	}
	headerIndex, columns, err := findJournalHeader(sheet.Rows, shared)
	if err != nil {
		return err
	}
	type pendingLine struct {
		row, accountColumn                  int
		account, description, debit, credit string
	}
	type pendingTransaction struct {
		startRow, endRow                          int
		date, transactionType, number, name, memo string
		lines                                     []pendingLine
	}
	var current *pendingTransaction
	finalize := func() {
		if current == nil || len(current.lines) == 0 {
			current = nil
			return
		}
		locator := fmt.Sprintf("sheet1!row:%d-%d", current.startRow, current.endRow)
		date, err := parseDate(current.date)
		if err != nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_DATE_INVALID", state.request, source.Path, locator, "", err.Error())
			current = nil
			return
		}
		if !dateIncluded(date, state.request, source) {
			current = nil
			return
		}
		journal := rawJournal{
			entityCode: state.request.EntityCode, bookCode: state.request.BookCode,
			postingDate: date, period: periodFor(date), sourceSystem: "QBO",
			description: journalDescription(current.transactionType, current.name, current.memo),
			reference:   strings.TrimSpace(current.number),
			evidence:    Evidence{SourceKind: SourceJournalXLSX, SourcePath: source.Path, SourceSHA256: fileDigest, Locator: locator},
		}
		unresolved := false
		for _, sourceLine := range current.lines {
			account, err := state.catalog.resolveHistorical(sourceLine.account)
			if err != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_ACCOUNT_UNRESOLVED", state.request, source.Path,
					fmt.Sprintf("sheet1!row:%d", sourceLine.row), "", err.Error())
				unresolved = true
				continue
			}
			debit, err := parseOptionalCents(sourceLine.debit)
			if err != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_AMOUNT_INVALID", state.request, source.Path,
					fmt.Sprintf("sheet1!row:%d", sourceLine.row), "", "debit: "+err.Error())
				unresolved = true
				continue
			}
			credit, err := parseOptionalCents(sourceLine.credit)
			if err != nil {
				appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_AMOUNT_INVALID", state.request, source.Path,
					fmt.Sprintf("sheet1!row:%d", sourceLine.row), "", "credit: "+err.Error())
				unresolved = true
				continue
			}
			if debit < 0 || credit < 0 || (debit > 0) == (credit > 0) {
				appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_POSTING_INVALID", state.request, source.Path,
					fmt.Sprintf("sheet1!row:%d", sourceLine.row), "", "posting row must contain exactly one positive debit or credit")
				unresolved = true
				continue
			}
			journal.lines = append(journal.lines, rawLine{
				accountKey: account.key, description: strings.TrimSpace(sourceLine.description),
				debitCents: debit, creditCents: credit,
			})
		}
		if unresolved {
			current = nil
			return
		}
		journal.sourceKey = fmt.Sprintf("journal-xlsx:%s:row-%d", fileDigest[:16], current.startRow)
		journal.evidence.PayloadSHA256 = rawJournalDigest(journal)
		if err := validateRawJournal(journal); err != nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_JOURNAL_INVALID", state.request, source.Path, locator, journal.sourceKey, err.Error())
			current = nil
			return
		}
		state.journals = append(state.journals, journal)
		current = nil
	}
	for _, row := range sheet.Rows[headerIndex+1:] {
		values, err := xlsxRowValues(row, shared)
		if err != nil {
			return fmt.Errorf("worksheet row %d: %w", row.Number, err)
		}
		dateValue := strings.TrimSpace(values[columns["Date"]])
		accountValue := strings.TrimSpace(values[columns["Account"]])
		if dateValue != "" {
			finalize()
			current = &pendingTransaction{
				startRow: row.Number, endRow: row.Number, date: dateValue,
				transactionType: values[columns["Transaction Type"]], number: values[columns["Num"]],
				name: values[columns["Name"]], memo: values[columns["Memo/Description"]],
			}
		}
		// QuickBooks writes a total row after each transaction. It repeats both
		// totals but has no Account; it is presentation data, not a posting.
		if accountValue == "" {
			continue
		}
		if current == nil {
			appendDiagnostic(&state.diagnostics, SeverityError, "XLSX_ORPHAN_POSTING", state.request, source.Path,
				fmt.Sprintf("sheet1!row:%d", row.Number), "", "posting row has no preceding transaction date")
			continue
		}
		description := strings.TrimSpace(values[columns["Memo/Description"]])
		if description == "" {
			description = strings.TrimSpace(values[columns["Name"]])
		}
		current.lines = append(current.lines, pendingLine{
			row: row.Number, accountColumn: columns["Account"], account: accountValue, description: description,
			debit: values[columns["Debit"]], credit: values[columns["Credit"]],
		})
		current.endRow = row.Number
	}
	finalize()
	return nil
}

func validateXLSXArchive(files []*zip.File) error {
	if len(files) > maxXLSXMembers {
		return fmt.Errorf("journal workbook contains %d ZIP members; limit is %d", len(files), maxXLSXMembers)
	}
	var expanded uint64
	for _, file := range files {
		if file.UncompressedSize64 > maxXLSXMemberBytes {
			return fmt.Errorf("workbook member %s exceeds the %d-byte expanded member limit", file.Name, maxXLSXMemberBytes)
		}
		if expanded > maxXLSXExpandedBytes-file.UncompressedSize64 {
			return fmt.Errorf("journal workbook exceeds the %d-byte total expanded limit", maxXLSXExpandedBytes)
		}
		expanded += file.UncompressedSize64
		if file.UncompressedSize64 > 1<<20 {
			if file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > maxXLSXCompressionRatio {
				return fmt.Errorf("workbook member %s exceeds the %d:1 compression-ratio limit", file.Name, maxXLSXCompressionRatio)
			}
		}
	}
	return nil
}

func readZipMember(files []*zip.File, name string, limit uint64) ([]byte, error) {
	for _, file := range files {
		if file.Name != name {
			continue
		}
		if file.UncompressedSize64 > limit {
			return nil, fmt.Errorf("workbook member %s exceeds the %d-byte expanded member limit", name, limit)
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func(closer interface{ Close() error }) { _ = closer.Close() }(reader)
		data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		if err != nil {
			return nil, err
		}
		if uint64(len(data)) > limit {
			return nil, fmt.Errorf("workbook member %s exceeds the %d-byte expanded member limit", name, limit)
		}
		return data, nil
	}
	return nil, fmt.Errorf("workbook member %s is missing", name)
}

func validateXLSXContent(shared xlsxSharedStrings, sheet xlsxWorksheet) error {
	if len(shared.Items) > maxXLSXSharedStrings {
		return fmt.Errorf("journal workbook contains %d shared strings; limit is %d", len(shared.Items), maxXLSXSharedStrings)
	}
	sharedBytes := 0
	for _, item := range shared.Items {
		itemBytes := len(item.Text)
		for _, run := range item.Runs {
			itemBytes += len(run.Text)
		}
		if itemBytes > maxXLSXSingleTextBytes {
			return fmt.Errorf("journal workbook contains a shared string larger than %d bytes", maxXLSXSingleTextBytes)
		}
		sharedBytes += itemBytes
		if sharedBytes > maxXLSXSharedTextBytes {
			return fmt.Errorf("journal workbook shared strings exceed %d bytes", maxXLSXSharedTextBytes)
		}
	}
	if len(sheet.Rows) > maxXLSXRows {
		return fmt.Errorf("journal workbook contains %d rows; limit is %d", len(sheet.Rows), maxXLSXRows)
	}
	cells := 0
	for _, row := range sheet.Rows {
		if len(row.Cells) > maxXLSXCellsPerRow {
			return fmt.Errorf("journal workbook row %d contains %d cells; limit is %d", row.Number, len(row.Cells), maxXLSXCellsPerRow)
		}
		cells += len(row.Cells)
		if cells > maxXLSXCells {
			return fmt.Errorf("journal workbook contains more than %d cells", maxXLSXCells)
		}
		for _, cell := range row.Cells {
			if len(cell.Value) > maxXLSXSingleTextBytes || len(cell.Inline.Text) > maxXLSXSingleTextBytes {
				return fmt.Errorf("journal workbook cell %s exceeds the %d-byte text limit", cell.Reference, maxXLSXSingleTextBytes)
			}
		}
	}
	return nil
}

func findJournalHeader(rows []xlsxRow, shared []string) (int, map[string]int, error) {
	required := []string{"Date", "Transaction Type", "Num", "Name", "Memo/Description", "Account", "Debit", "Credit"}
	for index, row := range rows {
		values, err := xlsxRowValues(row, shared)
		if err != nil {
			return 0, nil, err
		}
		columns := map[string]int{}
		for column, value := range values {
			columns[strings.TrimSpace(value)] = column
		}
		found := true
		for _, header := range required {
			if _, ok := columns[header]; !ok {
				found = false
				break
			}
		}
		if found {
			return index, columns, nil
		}
	}
	return 0, nil, fmt.Errorf("journal workbook header was not found")
}

func xlsxRowValues(row xlsxRow, shared []string) (map[int]string, error) {
	values := map[int]string{}
	for _, cell := range row.Cells {
		column, err := xlsxColumn(cell.Reference)
		if err != nil {
			return nil, err
		}
		value := cell.Value
		switch cell.Type {
		case "s":
			index, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || index < 0 || index >= len(shared) {
				return nil, fmt.Errorf("cell %s has invalid shared string index %q", cell.Reference, value)
			}
			value = shared[index]
		case "inlineStr":
			value = cell.Inline.Text
		}
		values[column] = value
	}
	return values, nil
}

func xlsxColumn(reference string) (int, error) {
	column := 0
	seen := false
	for _, r := range reference {
		if r < 'A' || r > 'Z' {
			break
		}
		seen = true
		column = column*26 + int(r-'A'+1)
	}
	if !seen {
		return 0, fmt.Errorf("invalid cell reference %q", reference)
	}
	return column - 1, nil
}

func parseOptionalCents(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return parseCents(value)
}

func journalDescription(transactionType, name, memo string) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{transactionType, name, memo} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		duplicate := false
		for _, part := range parts {
			if normalizeName(part) == normalizeName(value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "Imported journal transaction"
	}
	return strings.Join(parts, " - ")
}
