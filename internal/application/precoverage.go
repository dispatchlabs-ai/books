package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/importer"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

type precoverageClosureInputFile struct {
	StatementAccount string                            `json:"statement_account"`
	Identity         ledger.PrecoverageClosureIdentity `json:"identity"`
	ClosedOn         string                            `json:"closed_on"`
	ClosureEvidence  ledger.PrecoverageClosureEvidence `json:"closure_evidence"`
	ZeroEvidence     struct {
		SourceKind       string `json:"source_kind"`
		SourcePath       string `json:"source_path"`
		SourceSHA256     string `json:"source_sha256"`
		Locator          string `json:"locator"`
		PayloadSHA256    string `json:"payload_sha256"`
		ObservedOn       string `json:"observed_on"`
		ProviderStatus   string `json:"provider_status"`
		CurrentBalance   string `json:"current_balance"`
		AvailableBalance string `json:"available_balance"`
	} `json:"zero_evidence"`
	AccountHolder string `json:"account_holder"`
	AccountSuffix string `json:"account_suffix"`
	Reason        string `json:"reason"`
}

func ReadPrecoverageClosureInput(path string, resolve ...func(string) (money.Currency, error)) (ledger.CloseStatementAccountBeforeCoverageInput, error) {
	return ReadPrecoverageClosureInputFS(path, importer.LocalFiles{}, resolve...)
}
func ReadPrecoverageClosureInputFS(path string, files fs.FS, resolve ...func(string) (money.Currency, error)) (ledger.CloseStatementAccountBeforeCoverageInput, error) {
	if path == "" || path == "-" {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input must be an absolute retained JSON file path")
	}
	if !filepath.IsAbs(path) {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.New(apperr.Invalid, "INPUT_PATH_INVALID", "--input must be an absolute retained JSON file path")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "INPUT_PATH_INVALID", "resolve lifecycle input path", err)
	}
	data, err := fs.ReadFile(files, absPath)
	if err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "INPUT_READ_FAILED", "read lifecycle input file", err)
	}
	var file precoverageClosureInputFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode precoverage closure input", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode precoverage closure input", err)
	}
	currency := money.Currency{}
	if len(resolve) > 0 {
		currency, err = resolve[0](file.StatementAccount)
		if err != nil {
			return ledger.CloseStatementAccountBeforeCoverageInput{}, err
		}
	}
	currentBalance, err := currency.Parse(file.ZeroEvidence.CurrentBalance)
	if err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "PRECOVERAGE_BALANCE_INVALID", "parse provider current balance", err)
	}
	availableBalance, err := currency.Parse(file.ZeroEvidence.AvailableBalance)
	if err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "PRECOVERAGE_BALANCE_INVALID", "parse provider available balance", err)
	}
	inputDigest := sha256.Sum256(data)
	input := ledger.CloseStatementAccountBeforeCoverageInput{
		StatementAccount: file.StatementAccount, Identity: file.Identity, ClosedOn: file.ClosedOn,
		ClosureEvidence: file.ClosureEvidence,
		ZeroEvidence: ledger.PrecoverageZeroEvidence{
			SourceKind: file.ZeroEvidence.SourceKind, SourcePath: file.ZeroEvidence.SourcePath,
			SourceSHA256: file.ZeroEvidence.SourceSHA256, Locator: file.ZeroEvidence.Locator,
			PayloadSHA256: file.ZeroEvidence.PayloadSHA256, ObservedOn: file.ZeroEvidence.ObservedOn,
			ProviderStatus:      file.ZeroEvidence.ProviderStatus,
			CurrentBalanceCents: currentBalance, AvailableBalanceCents: availableBalance,
		},
		AccountHolder: file.AccountHolder, AccountSuffix: file.AccountSuffix, Reason: file.Reason,
		InputSourcePath: absPath, InputSourceSHA256: hex.EncodeToString(inputDigest[:]),
	}
	if err := verifyPrecoverageEvidenceFilesFS(input, files, currency); err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, err
	}
	return input, nil
}

func verifyEvidenceDigest(files fs.FS, path, expected, label string) ([]byte, error) {
	data, err := fs.ReadFile(files, path)
	if err != nil {
		return nil, apperr.Wrap(apperr.Input, "EVIDENCE_READ_FAILED", "read "+label, err)
	}
	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	if !strings.EqualFold(strings.TrimSpace(expected), actual) {
		return nil, apperr.New(apperr.Integrity, "EVIDENCE_DIGEST_MISMATCH", label+" SHA-256 does not match the retained source file")
	}
	return data, nil
}

func verifyPrecoverageEvidenceFilesFS(input ledger.CloseStatementAccountBeforeCoverageInput, files fs.FS, currencies ...money.Currency) error {
	if _, err := verifyEvidenceDigest(files, input.ClosureEvidence.SourcePath, input.ClosureEvidence.SourceSHA256, "provider closure evidence"); err != nil {
		return err
	}
	snapshotData, err := verifyEvidenceDigest(files, input.ZeroEvidence.SourcePath, input.ZeroEvidence.SourceSHA256, "provider account snapshot")
	if err != nil {
		return err
	}
	return verifyProviderSnapshot(snapshotData, input, currencies...)
}

func verifyProviderSnapshot(data []byte, input ledger.CloseStatementAccountBeforeCoverageInput, currencies ...money.Currency) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return apperr.Wrap(apperr.Input, "EVIDENCE_JSON_INVALID", "decode provider account snapshot", err)
	}
	var matches []map[string]any
	findProviderAccountObjects(document, input.Identity.ExternalID, &matches)
	if len(matches) != 1 {
		return apperr.New(apperr.Integrity, "EVIDENCE_IDENTITY_MISMATCH", "provider account snapshot must contain exactly one object for the exact source identity")
	}
	record := matches[0]
	canonical, err := json.Marshal(record)
	if err != nil {
		return apperr.Wrap(apperr.Input, "EVIDENCE_JSON_INVALID", "canonicalize provider account snapshot record", err)
	}
	payloadDigest := sha256.Sum256(canonical)
	if hex.EncodeToString(payloadDigest[:]) != strings.ToLower(strings.TrimSpace(input.ZeroEvidence.PayloadSHA256)) {
		return apperr.New(apperr.Integrity, "EVIDENCE_PAYLOAD_DIGEST_MISMATCH", "provider account snapshot payload SHA-256 does not match the exact account object")
	}
	status, ok := record["status"].(string)
	if !ok || !strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(input.ZeroEvidence.ProviderStatus)) {
		return apperr.New(apperr.Integrity, "EVIDENCE_STATUS_MISMATCH", "provider account snapshot status does not match the lifecycle input")
	}
	holder, ok := record["legalBusinessName"].(string)
	if !ok || holder != input.AccountHolder {
		return apperr.New(apperr.Integrity, "EVIDENCE_HOLDER_MISMATCH", "provider account snapshot holder does not exactly match the lifecycle input")
	}
	accountNumber, ok := record["accountNumber"].(string)
	if !ok || input.AccountSuffix == "" || !strings.HasSuffix(accountNumber, input.AccountSuffix) {
		return apperr.New(apperr.Integrity, "EVIDENCE_ACCOUNT_MISMATCH", "provider account snapshot number does not match the lifecycle suffix")
	}
	current, err := providerBalanceCents(record["currentBalance"], currencies...)
	if err != nil || current != input.ZeroEvidence.CurrentBalanceCents {
		return apperr.New(apperr.Integrity, "EVIDENCE_BALANCE_MISMATCH", "provider current balance does not match the lifecycle input")
	}
	available, err := providerBalanceCents(record["availableBalance"], currencies...)
	if err != nil || available != input.ZeroEvidence.AvailableBalanceCents {
		return apperr.New(apperr.Integrity, "EVIDENCE_BALANCE_MISMATCH", "provider available balance does not match the lifecycle input")
	}
	return nil
}

func findProviderAccountObjects(value any, externalID string, matches *[]map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if id, ok := typed["id"].(string); ok && id == externalID {
			*matches = append(*matches, typed)
		}
		for _, child := range typed {
			findProviderAccountObjects(child, externalID, matches)
		}
	case []any:
		for _, child := range typed {
			findProviderAccountObjects(child, externalID, matches)
		}
	}
}

func providerBalanceCents(value any, currencies ...money.Currency) (int64, error) {
	var amount string
	switch typed := value.(type) {
	case json.Number:
		amount = typed.String()
	case string:
		amount = typed
	default:
		return 0, fmt.Errorf("unsupported balance type %T", value)
	}
	currency := money.Currency{}
	if len(currencies) > 0 {
		currency = currencies[0]
	}
	return currency.Parse(amount)
}
