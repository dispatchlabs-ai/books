package banking

import (
	"archive/zip"
	"bytes"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var cellReference = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]*)$`)

type cellPosition struct{ Row, Column int }

func readStatementXLSX(data []byte, p TabularProfile, dateLayout string, numeric map[cellPosition]string) ([][]string, error) {
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return nil, invalid("IMPORT_XLSX_INVALID", "invalid XLSX archive")
	}
	if len(z.File) > 256 {
		return nil, invalid("IMPORT_LIMIT_EXCEEDED", "too many XLSX members")
	}
	members := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		if f.Name == "" || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") || strings.Contains(f.Name, "../") || members[f.Name] != nil {
			return nil, invalid("IMPORT_XLSX_INVALID", "invalid or duplicate archive member")
		}
		if f.UncompressedSize64 > 32<<20 || f.UncompressedSize64 > 1000*(f.CompressedSize64+1) {
			return nil, invalid("IMPORT_LIMIT_EXCEEDED", "XLSX member exceeds expansion limits")
		}
		total += f.UncompressedSize64
		if total > 64<<20 {
			return nil, invalid("IMPORT_LIMIT_EXCEEDED", "XLSX exceeds total expansion limit")
		}
		members[f.Name] = f
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, "vbaproject.bin") || strings.Contains(lower, "externallinks/") {
			return nil, invalid("IMPORT_XLSX_UNSUPPORTED", "macros and external workbook links are unsupported")
		}
	}
	read := func(name string, optional bool) (*xmlNode, error) {
		f := members[name]
		if f == nil {
			if optional {
				return nil, nil
			}
			return nil, invalid("IMPORT_XLSX_INVALID", "required workbook member is missing")
		}
		r, err := f.Open()
		if err != nil {
			return nil, invalid("IMPORT_XLSX_INVALID", "could not read workbook member")
		}
		defer func() { _ = r.Close() }()
		b, err := io.ReadAll(io.LimitReader(r, 32<<20+1))
		if err != nil || len(b) > 32<<20 {
			return nil, invalid("IMPORT_LIMIT_EXCEEDED", "workbook member exceeds read limit")
		}
		n, err := xmlTree(b)
		if err != nil {
			return nil, err
		}
		space := "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
		if strings.HasSuffix(name, ".rels") {
			space = "http://schemas.openxmlformats.org/package/2006/relationships"
		}
		if n.Name.Space != space && (space != "http://schemas.openxmlformats.org/spreadsheetml/2006/main" || n.Name.Space != "http://purl.oclc.org/ooxml/spreadsheetml/main") {
			return nil, invalid("IMPORT_XLSX_UNSUPPORTED", "unsupported workbook XML namespace")
		}
		if !n.namespace(n.Name.Space) {
			return nil, invalid("IMPORT_XLSX_UNSUPPORTED", "mixed worksheet namespaces")
		}
		return n, nil
	}
	workbook, e := read("xl/workbook.xml", false)
	if e != nil {
		return nil, e
	}
	if workbook.Name.Local != "workbook" {
		return nil, invalid("IMPORT_XLSX_INVALID", "invalid workbook root")
	}
	props, e := workbook.optional("workbookPr")
	if e != nil {
		return nil, e
	}
	date1904 := false
	if props != nil {
		switch props.attr("date1904") {
		case "1", "true":
			date1904 = true
		case "", "0", "false":
		default:
			return nil, invalid("IMPORT_XLSX_INVALID", "invalid workbook date system")
		}
	}
	if dateLayout == "EXCEL-1904" && !date1904 || dateLayout == "EXCEL-1900" && date1904 {
		return nil, invalid("IMPORT_DATE_LAYOUT_INVALID", "profile date system differs from workbook")
	}
	sheets, e := workbook.one("sheets")
	if e != nil {
		return nil, e
	}
	entries := sheets.all("sheet")
	if len(entries) == 0 || len(entries) > 100 {
		return nil, invalid("IMPORT_XLSX_INVALID", "invalid worksheet count")
	}
	var selected *xmlNode
	names := map[string]bool{}
	for _, sheet := range entries {
		name := sheet.attr("name")
		if name == "" || names[name] {
			return nil, invalid("IMPORT_XLSX_INVALID", "invalid or duplicate worksheet name")
		}
		names[name] = true
		if name == p.Sheet || p.Sheet == "" && len(entries) == 1 {
			selected = sheet
		}
	}
	if selected == nil {
		return nil, invalid("IMPORT_SHEET_REQUIRED", "select exactly one existing worksheet by name")
	}
	rels, e := read("xl/_rels/workbook.xml.rels", false)
	if e != nil {
		return nil, e
	}
	target := ""
	seenRels := map[string]bool{}
	for _, rel := range rels.all("Relationship") {
		id := rel.attr("Id")
		if id == "" || seenRels[id] {
			return nil, invalid("IMPORT_XLSX_INVALID", "invalid or duplicate relationship")
		}
		seenRels[id] = true
		if rel.attr("TargetMode") == "External" {
			return nil, invalid("IMPORT_XLSX_UNSUPPORTED", "external workbook relationships are unsupported")
		}
		sheetID := selected.attrNS("http://schemas.openxmlformats.org/officeDocument/2006/relationships", "id")
		if sheetID == "" {
			sheetID = selected.attrNS("http://purl.oclc.org/ooxml/officeDocument/relationships", "id")
		}
		if id == sheetID {
			if !strings.HasSuffix(rel.attr("Type"), "/worksheet") {
				return nil, invalid("IMPORT_XLSX_INVALID", "selected sheet is not a worksheet")
			}
			target = rel.attr("Target")
		}
	}
	if target == "" || strings.Contains(target, "..") || strings.Contains(target, "\\") || strings.Contains(target, ":") {
		return nil, invalid("IMPORT_XLSX_INVALID", "invalid worksheet target")
	}
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	} else {
		target = path.Join("xl", target)
	}
	if !strings.HasPrefix(target, "xl/worksheets/") {
		return nil, invalid("IMPORT_XLSX_INVALID", "worksheet target is outside the worksheet directory")
	}
	shared := []string{}
	sst, e := read("xl/sharedStrings.xml", true)
	if e != nil {
		return nil, e
	}
	if sst != nil {
		for _, item := range sst.all("si") {
			v, err := xlsxText(item)
			if err != nil {
				return nil, err
			}
			shared = append(shared, v)
			if len(shared) > 100000 {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "too many shared strings")
			}
		}
	}
	sheet, e := read(target, false)
	if e != nil {
		return nil, e
	}
	sheetData, e := sheet.one("sheetData")
	if e != nil {
		return nil, e
	}
	rows := [][]string{}
	lastRow := 0
	cells := 0
	for _, row := range sheetData.all("row") {
		number, err := strconv.Atoi(row.attr("r"))
		if err != nil || number <= lastRow || number > MaxTransactions+100 {
			return nil, invalid("IMPORT_XLSX_INVALID", "invalid row order or row limit")
		}
		lastRow = number
		for len(rows) < number {
			rows = append(rows, nil)
		}
		seen := map[int]bool{}
		for _, cell := range row.all("c") {
			cells++
			if cells > MaxNodes {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "too many workbook cells")
			}
			if len(cell.all("f")) != 0 {
				return nil, invalid("IMPORT_XLSX_FORMULA", "formula cells are unsupported; export literal values")
			}
			m := cellReference.FindStringSubmatch(cell.attr("r"))
			if m == nil || m[2] != strconv.Itoa(number) {
				return nil, invalid("IMPORT_XLSX_INVALID", "invalid cell reference")
			}
			col := 0
			for _, c := range m[1] {
				col = col*26 + int(c-'A'+1)
			}
			if col > 256 || seen[col] {
				return nil, invalid("IMPORT_XLSX_INVALID", "invalid or duplicate column")
			}
			seen[col] = true
			for len(rows[number-1]) < col {
				rows[number-1] = append(rows[number-1], "")
			}
			value, err := cell.optionalValue("v")
			if err != nil {
				return nil, err
			}
			switch cell.attr("t") {
			case "s":
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(shared) {
					return nil, invalid("IMPORT_XLSX_INVALID", "shared string index is invalid")
				}
				value = shared[index]
			case "inlineStr":
				inline, err := cell.one("is")
				if err != nil {
					return nil, err
				}
				value, err = xlsxText(inline)
				if err != nil {
					return nil, err
				}
			case "", "n":
				if value != "" {
					normalized, err := excelNumber(value)
					if err != nil {
						return nil, err
					}
					numeric[cellPosition{number - 1, col - 1}] = normalized
				}
			case "str", "d":
			default:
				return nil, invalid("IMPORT_XLSX_CELL_UNSUPPORTED", "boolean/error cell types are unsupported in statement tables")
			}
			rows[number-1][col-1] = value
		}
	}
	return rows, nil
}
func xlsxText(n *xmlNode) (string, error) {
	var b strings.Builder
	for _, c := range n.Children {
		switch c.Name.Local {
		case "t":
			b.WriteString(c.Text)
		case "r":
			for _, t := range c.all("t") {
				b.WriteString(t.Text)
			}
		case "phoneticPr", "rPh":
		default:
			return "", invalid("IMPORT_XLSX_INVALID", "unsupported string structure")
		}
		if b.Len() > MaxText {
			return "", invalid("IMPORT_LIMIT_EXCEEDED", "workbook string exceeds limit")
		}
	}
	return b.String(), nil
}

// XLSX numeric cells use XML decimal/scientific notation, independently of
// display formatting or the locale declared for text cells. Expand exactly.
var excelNumberPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func excelNumber(v string) (string, error) {
	if !excelNumberPattern.MatchString(v) {
		return "", invalid("IMPORT_XLSX_NUMBER_INVALID", "invalid numeric cell notation")
	}
	if len(v) > 64 {
		return "", invalid("IMPORT_XLSX_NUMBER_INVALID", "numeric cell is too long")
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == 'e' || r == 'E' })
	if len(parts) < 1 || len(parts) > 2 {
		return "", invalid("IMPORT_XLSX_NUMBER_INVALID", "invalid numeric cell")
	}
	exp := 0
	var e error
	if len(parts) == 2 {
		exp, e = strconv.Atoi(parts[1])
		if e != nil || exp > 40 || exp < -40 {
			return "", invalid("IMPORT_XLSX_NUMBER_INVALID", "numeric cell exponent exceeds exact decimal limits")
		}
	}
	mantissa := parts[0]
	sign := ""
	if strings.HasPrefix(mantissa, "-") || strings.HasPrefix(mantissa, "+") {
		sign = mantissa[:1]
		mantissa = mantissa[1:]
	}
	whole, fraction, hasDot := strings.Cut(mantissa, ".")
	if whole == "" {
		whole = "0"
	}
	if !digits(whole) || (hasDot && !digits(fraction)) {
		return "", invalid("IMPORT_XLSX_NUMBER_INVALID", "invalid numeric cell mantissa")
	}
	number := whole + fraction
	point := len(whole) + exp
	if point < 0 {
		number = strings.Repeat("0", -point) + number
		point = 0
	}
	if point > len(number) {
		number += strings.Repeat("0", point-len(number))
	}
	if point == 0 {
		number = "0." + number
	} else if point < len(number) {
		number = number[:point] + "." + number[point:]
	}
	normalized, e := decimal(sign + number)
	if e != nil {
		return "", e
	}
	normalized = strings.TrimSuffix(normalized, ".00")
	return normalized, nil
}
