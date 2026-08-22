package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/money"
)

var decimalPattern = regexp.MustCompile(`^([+-]?)([0-9]*)(?:\.([0-9]*))?$`)

const maxJSONImportBytes int64 = 64 << 20

func parseCents(value string) (int64, error) {
	s := strings.TrimSpace(value)
	match := decimalPattern.FindStringSubmatch(s)
	if match == nil || (match[2] == "" && match[3] == "") {
		return 0, fmt.Errorf("invalid decimal money value %q", value)
	}
	fraction := strings.TrimRight(match[3], "0")
	if len(fraction) > 2 {
		return 0, fmt.Errorf("money value %q has sub-cent precision", value)
	}
	wholeText := match[2]
	if wholeText == "" {
		wholeText = "0"
	}
	whole, err := strconv.ParseUint(wholeText, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("invalid money value %q: %w", value, err)
	}
	fractionText := match[3]
	if len(fractionText) > 2 {
		fractionText = fractionText[:2]
	}
	for len(fractionText) < 2 {
		fractionText += "0"
	}
	var fractional uint64
	if fractionText != "" {
		fractional, err = strconv.ParseUint(fractionText, 10, 7)
		if err != nil {
			return 0, fmt.Errorf("invalid money value %q: %w", value, err)
		}
	}
	const maxInt64 = uint64(^uint64(0) >> 1)
	if whole > maxInt64/100 || whole*100 > maxInt64-fractional {
		return 0, fmt.Errorf("money value %q exceeds int64 cents", value)
	}
	cents := int64(whole*100 + fractional)
	if match[1] == "-" {
		cents = -cents
	}
	return cents, nil
}

func parseDate(value string) (string, error) {
	s := strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("invalid date %q", value)
}

func validateISODate(value, field string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return fmt.Errorf("%s must be an ISO-8601 date", field)
	}
	return nil
}

func periodFor(date string) string { return date[:7] }

func normalizedEntity(value string) string { return normalizeCode(value) }

func normalizeCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if valid {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && out.Len() > 0 {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func normalizeName(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeSubtype(value string) string { return normalizeCode(value) }

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(file)
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func bytesSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func readJSONRows(path string) ([]json.RawMessage, error) {
	data, err := readImportFile(path, maxJSONImportBytes, "JSON import")
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if envelope.Rows == nil {
		return nil, fmt.Errorf("%s does not contain a rows array", path)
	}
	return envelope.Rows, nil
}

func readImportFile(path string, limit int64, label string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(file)
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %s is not a regular file", label, path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s %s exceeds the %d-byte input limit", label, path, limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s %s changed while reading or exceeds the %d-byte input limit", label, path, limit)
	}
	return data, nil
}

func sourceBounds(entity EntityRequest, source Source) (string, string) {
	start := entity.StartDate
	if source.StartDate != "" && (start == "" || source.StartDate > start) {
		start = source.StartDate
	}
	end := entity.CutoffDate
	if source.EndDate != "" && (end == "" || source.EndDate < end) {
		end = source.EndDate
	}
	return start, end
}

func dateIncluded(date string, entity EntityRequest, source Source) bool {
	start, end := sourceBounds(entity, source)
	return (start == "" || date >= start) && (end == "" || date <= end)
}

func validateRequest(request Request) error {
	if len(request.Entities) == 0 {
		return fmt.Errorf("at least one entity request is required")
	}
	seenEntity := map[string]bool{}
	seenBook := map[string]bool{}
	for index := range request.Entities {
		entity := &request.Entities[index]
		entity.EntityCode = normalizedEntity(entity.EntityCode)
		entity.BookCode = normalizeCode(entity.BookCode)
		entity.Currency = strings.ToUpper(strings.TrimSpace(entity.Currency))
		if entity.EntityCode == "" || entity.BookCode == "" {
			return fmt.Errorf("entity and book codes are required")
		}
		if seenEntity[entity.EntityCode] || seenBook[entity.BookCode] {
			return fmt.Errorf("duplicate entity or book code in import request")
		}
		seenEntity[entity.EntityCode] = true
		seenBook[entity.BookCode] = true
		if !money.IsSupportedCurrency(entity.Currency) {
			return fmt.Errorf("entity %s uses unsupported currency %q; this release supports USD only", entity.EntityCode, entity.Currency)
		}
		if err := validateISODate(entity.StartDate, "start date"); err != nil {
			return fmt.Errorf("entity %s: %w", entity.EntityCode, err)
		}
		if err := validateISODate(entity.CutoffDate, "cutoff date"); err != nil {
			return fmt.Errorf("entity %s: %w", entity.EntityCode, err)
		}
		if entity.CutoffDate < entity.StartDate {
			return fmt.Errorf("entity %s cutoff precedes start date", entity.EntityCode)
		}
		if len(entity.Sources) == 0 {
			return fmt.Errorf("entity %s requires at least one bounded source", entity.EntityCode)
		}
		for sourceIndex := range entity.Sources {
			source := &entity.Sources[sourceIndex]
			source.Path = filepath.Clean(source.Path)
			if source.StartDate == "" || source.EndDate == "" {
				return fmt.Errorf("entity %s source %s requires explicit start and end dates", entity.EntityCode, source.Path)
			}
			if err := validateISODate(source.StartDate, "source start date"); err != nil {
				return fmt.Errorf("entity %s source %s: %w", entity.EntityCode, source.Path, err)
			}
			if err := validateISODate(source.EndDate, "source end date"); err != nil {
				return fmt.Errorf("entity %s source %s: %w", entity.EntityCode, source.Path, err)
			}
			if source.EndDate < source.StartDate {
				return fmt.Errorf("entity %s source %s ends before it starts", entity.EntityCode, source.Path)
			}
		}
		if err := validateSourceCoverage(*entity); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceCoverage(entity EntityRequest) error {
	type interval struct{ start, end string }
	intervals := make([]interval, 0, len(entity.Sources))
	for _, source := range entity.Sources {
		start, end := source.StartDate, source.EndDate
		if start < entity.StartDate {
			start = entity.StartDate
		}
		if end > entity.CutoffDate {
			end = entity.CutoffDate
		}
		if start <= end {
			intervals = append(intervals, interval{start: start, end: end})
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].end < intervals[j].end
	})
	cursor := entity.StartDate
	for _, current := range intervals {
		if current.start > cursor {
			return fmt.Errorf("entity %s source coverage gap begins %s", entity.EntityCode, cursor)
		}
		if current.end >= cursor {
			parsed, _ := time.Parse("2006-01-02", current.end)
			cursor = parsed.AddDate(0, 0, 1).Format("2006-01-02")
		}
		if cursor > entity.CutoffDate {
			return nil
		}
	}
	return fmt.Errorf("entity %s source coverage ends before cutoff %s (first uncovered date %s)", entity.EntityCode, entity.CutoffDate, cursor)
}

func appendDiagnostic(target *[]Diagnostic, severity Severity, code string, entity EntityRequest, sourcePath, locator, sourceKey, message string) {
	*target = append(*target, Diagnostic{
		Severity: severity, Code: code, EntityCode: entity.EntityCode,
		SourcePath: sourcePath, Locator: locator, SourceKey: sourceKey, Message: message,
	})
}

func sortDiagnostics(values []Diagnostic) {
	sort.SliceStable(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if a.EntityCode != b.EntityCode {
			return a.EntityCode < b.EntityCode
		}
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		if a.Locator != b.Locator {
			return a.Locator < b.Locator
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

func rawJournalDigest(journal rawJournal) string {
	type digestLine struct {
		Account     string `json:"account"`
		Description string `json:"description"`
		Debit       int64  `json:"debit"`
		Credit      int64  `json:"credit"`
	}
	payload := struct {
		Date        string       `json:"date"`
		Description string       `json:"description"`
		Reference   string       `json:"reference"`
		Lines       []digestLine `json:"lines"`
	}{Date: journal.postingDate, Description: journal.description, Reference: journal.reference}
	for _, line := range journal.lines {
		payload.Lines = append(payload.Lines, digestLine{line.accountKey, line.description, line.debitCents, line.creditCents})
	}
	encoded, _ := json.Marshal(payload)
	return bytesSHA256(encoded)
}

func validateRawJournal(journal rawJournal) error {
	if len(journal.lines) < 2 {
		return fmt.Errorf("journal requires at least two nonzero posting lines")
	}
	var debit, credit int64
	for index, line := range journal.lines {
		if line.accountKey == "" {
			return fmt.Errorf("line %d has no resolved account", index+1)
		}
		if line.debitCents < 0 || line.creditCents < 0 || (line.debitCents > 0) == (line.creditCents > 0) {
			return fmt.Errorf("line %d must contain exactly one positive debit or credit", index+1)
		}
		debit += line.debitCents
		credit += line.creditCents
	}
	if debit == 0 || debit != credit {
		return fmt.Errorf("journal is not balanced: debit=%d credit=%d", debit, credit)
	}
	return nil
}

func addSignedLine(lines *[]rawLine, accountKey, description string, signedCents int64, positiveIsDebit bool) {
	if signedCents == 0 {
		return
	}
	line := rawLine{accountKey: accountKey, description: strings.TrimSpace(description)}
	if (signedCents > 0) == positiveIsDebit {
		if signedCents < 0 {
			line.debitCents = -signedCents
		} else {
			line.debitCents = signedCents
		}
	} else {
		if signedCents < 0 {
			line.creditCents = -signedCents
		} else {
			line.creditCents = signedCents
		}
	}
	*lines = append(*lines, line)
}
