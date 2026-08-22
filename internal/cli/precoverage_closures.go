package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
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

type precoverageClosureCommandOutput struct {
	ledger.StatementAccountPrecoverageClosure
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run"`
}

func decodePrecoverageClosureInput(path string) (ledger.CloseStatementAccountBeforeCoverageInput, error) {
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
	data, err := os.ReadFile(absPath)
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
	currentBalance, err := money.Parse(file.ZeroEvidence.CurrentBalance)
	if err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, apperr.Wrap(apperr.Input, "PRECOVERAGE_BALANCE_INVALID", "parse provider current balance", err)
	}
	availableBalance, err := money.Parse(file.ZeroEvidence.AvailableBalance)
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
	if err := verifyPrecoverageEvidenceFiles(input); err != nil {
		return ledger.CloseStatementAccountBeforeCoverageInput{}, err
	}
	return input, nil
}

func verifyEvidenceDigest(path, expected, label string) ([]byte, error) {
	data, err := os.ReadFile(path)
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

func verifyPrecoverageEvidenceFiles(input ledger.CloseStatementAccountBeforeCoverageInput) error {
	if _, err := verifyEvidenceDigest(input.ClosureEvidence.SourcePath, input.ClosureEvidence.SourceSHA256, "provider closure evidence"); err != nil {
		return err
	}
	snapshotData, err := verifyEvidenceDigest(input.ZeroEvidence.SourcePath, input.ZeroEvidence.SourceSHA256, "provider account snapshot")
	if err != nil {
		return err
	}
	return verifyProviderSnapshot(snapshotData, input)
}

func verifyProviderSnapshot(data []byte, input ledger.CloseStatementAccountBeforeCoverageInput) error {
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
	current, err := providerBalanceCents(record["currentBalance"])
	if err != nil || current != input.ZeroEvidence.CurrentBalanceCents {
		return apperr.New(apperr.Integrity, "EVIDENCE_BALANCE_MISMATCH", "provider current balance does not match the lifecycle input")
	}
	available, err := providerBalanceCents(record["availableBalance"])
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

func providerBalanceCents(value any) (int64, error) {
	var amount string
	switch typed := value.(type) {
	case json.Number:
		amount = typed.String()
	case string:
		amount = typed
	default:
		return 0, fmt.Errorf("unsupported balance type %T", value)
	}
	return money.Parse(amount)
}

func newStatementAccountLifecycleCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "lifecycle", Short: "Record audited statement-account lifecycle evidence"}
	var inputPath string
	var commit bool
	closeBeforeCoverage := &cobra.Command{
		Use: "close-before-coverage", Short: "Certify an exact-zero provider closure before required reconciliation coverage", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input, err := decodePrecoverageClosureInput(inputPath)
			if err != nil {
				return err
			}
			if !commit || opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
				result, err := ledger.NewService(store, opts.actor).ValidateStatementAccountPrecoverageClosure(cmd.Context(), input)
				if err != nil {
					return err
				}
				output := precoverageClosureCommandOutput{StatementAccountPrecoverageClosure: result, Committed: false, DryRun: opts.dryRun}
				return writeResult(cmd, opts.format, precoverageClosureMachineOutput(output), precoverageClosureHeaders, [][]string{precoverageClosureRow(output)})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CloseStatementAccountBeforeCoverage(cmd.Context(), input)
			if err != nil {
				return err
			}
			output := precoverageClosureCommandOutput{StatementAccountPrecoverageClosure: result, Committed: true, DryRun: false}
			return writeResult(cmd, opts.format, precoverageClosureMachineOutput(output), precoverageClosureHeaders, [][]string{precoverageClosureRow(output)})
		},
	}
	closeBeforeCoverage.Flags().StringVarP(&inputPath, "input", "i", "", "absolute retained lifecycle JSON input path")
	closeBeforeCoverage.Flags().BoolVar(&commit, "commit", false, "commit the lifecycle evidence and archive; otherwise preview")
	_ = closeBeforeCoverage.MarkFlagRequired("input")

	var statementAccount, entity string
	list := &cobra.Command{
		Use: "list", Short: "List immutable precoverage closure certificates", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListStatementAccountPrecoverageClosures(cmd.Context(), ledger.PrecoverageClosureFilter{StatementAccount: statementAccount, Entity: entity})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(result))
			machine := make([]map[string]any, 0, len(result))
			for _, closure := range result {
				output := precoverageClosureCommandOutput{StatementAccountPrecoverageClosure: closure, Committed: true}
				rows = append(rows, precoverageClosureRow(output))
				machine = append(machine, precoverageClosureMachineOutput(output))
			}
			return writeResult(cmd, opts.format, machine, precoverageClosureHeaders, rows)
		},
	}
	list.Flags().StringVar(&statementAccount, "statement-account", "", "optional statement-account code")
	list.Flags().StringVar(&entity, "entity", "", "optional entity code")
	command.AddCommand(closeBeforeCoverage, list)
	return command
}

var precoverageClosureHeaders = []string{
	"ACCOUNT", "ENTITY", "CLOSED ON", "DISPOSITION", "STATUS", "CONTROL AT CLOSURE",
	"CURRENT CONTROL", "POST-CLOSE LINES", "DRAFT LINES", "ACTIVE IDENTITIES", "IDENTITY DIGEST",
	"INPUT SHA256", "CHANGED", "COMMITTED", "ID",
}

func precoverageClosureRow(value precoverageClosureCommandOutput) []string {
	return []string{
		value.StatementAccount, value.EntityCode, value.ClosedOn, value.CoverageDisposition, value.Status,
		money.Format(value.ControlBalanceAtClosureCents), money.Format(value.CurrentControlBalanceCents),
		fmt.Sprint(value.PostClosureControlLineCount), fmt.Sprint(value.DraftControlLineCount),
		fmt.Sprint(value.ActiveIdentityCount), value.ActiveIdentityDigest, value.InputSourceSHA256, fmt.Sprint(value.Changed),
		fmt.Sprint(value.Committed), value.ID,
	}
}

func precoverageClosureMachineOutput(value precoverageClosureCommandOutput) map[string]any {
	closure := value.StatementAccountPrecoverageClosure
	closureEvidence := map[string]any{
		"source_kind":   closure.ClosureEvidence.SourceKind,
		"source_path":   closure.ClosureEvidence.SourcePath,
		"source_sha256": closure.ClosureEvidence.SourceSHA256,
		"locator":       "official provider closure evidence",
	}
	zeroEvidence := map[string]any{
		"source_kind":             closure.ZeroEvidence.SourceKind,
		"source_path":             closure.ZeroEvidence.SourcePath,
		"source_sha256":           closure.ZeroEvidence.SourceSHA256,
		"locator":                 "exact provider account object",
		"payload_sha256":          closure.ZeroEvidence.PayloadSHA256,
		"observed_on":             closure.ZeroEvidence.ObservedOn,
		"provider_status":         closure.ZeroEvidence.ProviderStatus,
		"current_balance_cents":   closure.ZeroEvidence.CurrentBalanceCents,
		"available_balance_cents": closure.ZeroEvidence.AvailableBalanceCents,
	}
	return map[string]any{
		"id": closure.ID, "statement_account_id": closure.StatementAccountID,
		"statement_account":             closure.StatementAccount,
		"statement_account_identity_id": closure.StatementAccountIdentityID,
		"active_identity_count":         closure.ActiveIdentityCount,
		"active_identity_digest":        closure.ActiveIdentityDigest,
		"entity":                        closure.EntityCode, "book": closure.BookCode, "gl_account": closure.GLAccountCode,
		"reconciliation_required_from":    closure.ReconciliationRequiredFrom,
		"reconciliation_required_through": closure.ReconciliationRequiredThrough,
		"coverage_disposition":            closure.CoverageDisposition, "closed_on": closure.ClosedOn,
		"closure_evidence": closureEvidence, "zero_evidence": zeroEvidence,
		"account_holder": closure.AccountHolder, "account_suffix": closure.AccountSuffix,
		"reason": closure.Reason, "input_source_path": closure.InputSourcePath,
		"input_source_sha256":              closure.InputSourceSHA256,
		"control_balance_at_closure_cents": closure.ControlBalanceAtClosureCents,
		"current_control_balance_cents":    closure.CurrentControlBalanceCents,
		"post_closure_control_line_count":  closure.PostClosureControlLineCount,
		"draft_control_line_count":         closure.DraftControlLineCount,
		"status":                           closure.Status, "archived_at": closure.ArchivedAt, "archived_by": closure.ArchivedBy,
		"created_at": closure.CreatedAt, "created_by": closure.CreatedBy,
		"changed": closure.Changed, "committed": value.Committed, "dry_run": value.DryRun,
	}
}
