package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
)

type journalFile struct {
	Book                string            `json:"book"`
	Kind                string            `json:"kind,omitempty"`
	PostingDate         string            `json:"posting_date"`
	Period              string            `json:"period"`
	Description         string            `json:"description"`
	Reference           string            `json:"reference,omitempty"`
	SourceSystem        string            `json:"source_system,omitempty"`
	SourceKey           string            `json:"source_key,omitempty"`
	TaxType             string            `json:"tax_type,omitempty"`
	TaxAccountingPeriod string            `json:"tax_accounting_period,omitempty"`
	Lines               []journalLineFile `json:"lines"`
}

type journalLineFile struct {
	Account            string `json:"account"`
	Description        string `json:"description,omitempty"`
	Debit              string `json:"debit,omitempty"`
	Credit             string `json:"credit,omitempty"`
	CounterpartyEntity string `json:"counterparty_entity,omitempty"`
	IntercompanyKey    string `json:"intercompany_key,omitempty"`
}

func (input journalFile) ledgerInput() (ledger.CreateJournalInput, error) {
	result := ledger.CreateJournalInput{
		Book: input.Book, Kind: input.Kind, PostingDate: input.PostingDate, Period: input.Period, Description: input.Description,
		Reference: input.Reference, SourceSystem: input.SourceSystem, SourceKey: input.SourceKey,
		TaxType: input.TaxType, TaxAccountingPeriod: input.TaxAccountingPeriod,
	}
	for i, line := range input.Lines {
		var debit, credit int64
		var err error
		if strings.TrimSpace(line.Debit) != "" {
			debit, err = money.Parse(line.Debit)
			if err != nil {
				return result, apperr.Wrap(apperr.Input, "JOURNAL_AMOUNT_INVALID", fmt.Sprintf("line %d debit is invalid", i+1), err)
			}
		}
		if strings.TrimSpace(line.Credit) != "" {
			credit, err = money.Parse(line.Credit)
			if err != nil {
				return result, apperr.Wrap(apperr.Input, "JOURNAL_AMOUNT_INVALID", fmt.Sprintf("line %d credit is invalid", i+1), err)
			}
		}
		result.Lines = append(result.Lines, ledger.JournalLineInput{Account: line.Account, Description: line.Description, DebitCents: debit, CreditCents: credit, CounterpartyEntity: line.CounterpartyEntity, IntercompanyKey: line.IntercompanyKey})
	}
	return result, nil
}

func journalRows(journal ledger.Journal) [][]string {
	rows := make([][]string, 0, len(journal.Lines))
	for _, line := range journal.Lines {
		rows = append(rows, []string{journal.BookCode, fmt.Sprint(journal.EntryNumber), journal.PostingDate, journal.Status, fmt.Sprint(line.LineNumber), line.AccountCode, line.Description, money.Format(line.DebitCents), money.Format(line.CreditCents), line.ID})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{journal.BookCode, fmt.Sprint(journal.EntryNumber), journal.PostingDate, journal.Status, "", "", journal.Description, "0.00", "0.00", journal.ID})
	}
	return rows
}

func newJournalCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "journal", Short: "Create, validate, post, reverse, and inspect journal entries"}
	var inputPath string
	create := &cobra.Command{
		Use: "create", Short: "Create a draft journal from JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required")
			}
			var file journalFile
			if err := readJSONInput(inputPath, &file); err != nil {
				return err
			}
			input, err := file.ledgerInput()
			if err != nil {
				return err
			}
			if err := requireCommit(opts, "journal create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journal, err := ledger.NewService(store, opts.actor).CreateJournal(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, journal, []string{"BOOK", "NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "DESCRIPTION", "DEBIT", "CREDIT", "ID"}, journalRows(journal))
		},
	}
	create.Flags().StringVarP(&inputPath, "input", "i", "", "JSON input file or - for stdin")
	var editID string
	edit := &cobra.Command{
		Use: "edit", Short: "Replace a draft journal from JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if editID == "" || inputPath == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--id and --input are required")
			}
			var file journalFile
			if err := readJSONInput(inputPath, &file); err != nil {
				return err
			}
			input, err := file.ledgerInput()
			if err != nil {
				return err
			}
			if err := requireCommit(opts, "journal edit"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journal, err := ledger.NewService(store, opts.actor).ReplaceDraft(cmd.Context(), editID, input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, journal, []string{"BOOK", "NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "DESCRIPTION", "DEBIT", "CREDIT", "ID"}, journalRows(journal))
		},
	}
	edit.Flags().StringVar(&editID, "id", "", "draft journal id")
	edit.Flags().StringVarP(&inputPath, "input", "i", "", "JSON input file or - for stdin")
	var journalID string
	show := &cobra.Command{
		Use: "show", Short: "Show a journal and its lines", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journal, err := ledger.NewService(store, opts.actor).GetJournal(cmd.Context(), journalID)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, journal, []string{"BOOK", "NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "DESCRIPTION", "DEBIT", "CREDIT", "ID"}, journalRows(journal))
		},
	}
	show.Flags().StringVar(&journalID, "id", "", "journal id")
	validate := &cobra.Command{
		Use: "validate", Short: "Validate a draft without posting", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ValidateJournal(cmd.Context(), journalID)
			if err != nil {
				return err
			}
			errorsText := strings.Join(result.Errors, "; ")
			if err := writeResult(cmd, opts.format, result, []string{"ID", "VALID", "DEBITS", "CREDITS", "ERRORS"}, [][]string{{result.JournalID, fmt.Sprint(result.Valid), money.Format(result.DebitCents), money.Format(result.CreditCents), errorsText}}); err != nil {
				return err
			}
			if !result.Valid {
				if errorsText == "" {
					errorsText = "journal validation failed"
				}
				return apperr.New(apperr.Validation, "JOURNAL_INVALID", errorsText)
			}
			return nil
		},
	}
	validate.Flags().StringVar(&journalID, "id", "", "journal id")
	post := &cobra.Command{
		Use: "post", Short: "Atomically post a valid draft", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
				result, err := ledger.NewService(store, opts.actor).ValidateJournal(cmd.Context(), journalID)
				if err != nil {
					return err
				}
				if !result.Valid {
					return apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(result.Errors, "; "))
				}
				return writeResult(cmd, opts.format, map[string]any{"validation": result, "dry_run": true}, []string{"ID", "VALID", "DEBITS", "CREDITS", "DRY RUN"}, [][]string{{journalID, "true", money.Format(result.DebitCents), money.Format(result.CreditCents), "true"}})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journal, err := ledger.NewService(store, opts.actor).PostJournal(cmd.Context(), journalID)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, journal, []string{"BOOK", "NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "DESCRIPTION", "DEBIT", "CREDIT", "ID"}, journalRows(journal))
		},
	}
	post.Flags().StringVar(&journalID, "id", "", "draft journal id")
	var reverseDate, reversePeriod, reverseDescription string
	reverse := &cobra.Command{
		Use: "reverse", Short: "Create a linked draft reversal", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "journal reverse"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journal, err := ledger.NewService(store, opts.actor).ReverseJournal(cmd.Context(), journalID, reverseDate, reversePeriod, reverseDescription)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, journal, []string{"BOOK", "NUMBER", "DATE", "STATUS", "LINE", "ACCOUNT", "DESCRIPTION", "DEBIT", "CREDIT", "ID"}, journalRows(journal))
		},
	}
	reverse.Flags().StringVar(&journalID, "id", "", "posted journal id")
	reverse.Flags().StringVar(&reverseDate, "date", "", "reversal posting date")
	reverse.Flags().StringVar(&reversePeriod, "period", "", "reversal period")
	reverse.Flags().StringVar(&reverseDescription, "description", "", "reversal description")
	abandon := &cobra.Command{
		Use: "abandon", Short: "Abandon an unused draft", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "journal abandon"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).AbandonJournal(cmd.Context(), journalID); err != nil {
				return err
			}
			return writeResult(cmd, opts.format, map[string]any{"id": journalID, "status": "ABANDONED"}, []string{"ID", "STATUS"}, [][]string{{journalID, "ABANDONED"}})
		},
	}
	abandon.Flags().StringVar(&journalID, "id", "", "draft journal id")
	var book, from, to, status string
	list := &cobra.Command{
		Use: "list", Short: "List journals", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			journals, err := ledger.NewService(store, opts.actor).ListJournals(cmd.Context(), book, from, to, status)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(journals))
			for _, value := range journals {
				rows = append(rows, []string{value.BookCode, fmt.Sprint(value.EntryNumber), value.PostingDate, value.Status, value.Description, money.Format(value.TotalDebitCents), value.ID})
			}
			return writeResult(cmd, opts.format, journals, []string{"BOOK", "NUMBER", "DATE", "STATUS", "DESCRIPTION", "TOTAL", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&book, "book", "", "optional book code")
	list.Flags().StringVar(&from, "from", "", "optional start date")
	list.Flags().StringVar(&to, "to", "", "optional end date")
	list.Flags().StringVar(&status, "status", "", "optional DRAFT, POSTED, or ABANDONED")
	var batchInputPath string
	importCommand := &cobra.Command{
		Use: "import", Short: "Atomically import an idempotent batch of journal drafts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if batchInputPath == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required")
			}
			raw, err := readInputBytes(batchInputPath)
			if err != nil {
				return err
			}
			input, err := parseJournalImport(raw)
			if err != nil {
				return err
			}
			if err := requireCommit(opts, "journal import"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ImportJournals(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"BATCH", "CHANGED", "CREATED", "SKIPPED"}, [][]string{{result.BatchID, fmt.Sprint(result.Changed), fmt.Sprint(result.CreatedCount), fmt.Sprint(result.SkippedCount)}})
		},
	}
	importCommand.Flags().StringVarP(&batchInputPath, "input", "i", "", "journal import JSON file or - for stdin")
	var batchID string
	postBatch := &cobra.Command{
		Use: "post-batch", Short: "Validate and atomically post every draft in an import batch", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var opener = openWrite
			if opts.dryRun {
				opener = openRead
			}
			store, err := opener(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).PostImportBatch(cmd.Context(), batchID, opts.dryRun)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"BATCH", "CHANGED", "POSTED", "ALREADY POSTED", "DRY RUN"}, [][]string{{result.BatchID, fmt.Sprint(result.Changed), fmt.Sprint(result.PostedCount), fmt.Sprint(result.AlreadyPosted), fmt.Sprint(opts.dryRun)}})
		},
	}
	postBatch.Flags().StringVar(&batchID, "batch", "", "journal import batch id")
	command.AddCommand(newHumanJournalAddCommand(opts), create, edit, show, validate, post, reverse, abandon, list, importCommand, postBatch)
	return command
}

type journalImportFile struct {
	SourceSystem string                    `json:"source_system"`
	SourceName   string                    `json:"source_name"`
	Entity       string                    `json:"entity,omitempty"`
	Records      []journalImportRecordFile `json:"records"`
}

type journalImportRecordFile struct {
	Journal journalFile     `json:"journal"`
	RawJSON json.RawMessage `json:"raw_json,omitempty"`
}

func parseJournalImport(raw []byte) (ledger.JournalImportInput, error) {
	var file journalImportFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return ledger.JournalImportInput{}, apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode journal import", err)
	}
	fileHash := sha256.Sum256(raw)
	result := ledger.JournalImportInput{SourceSystem: file.SourceSystem, SourceName: file.SourceName, FileSHA256: hex.EncodeToString(fileHash[:]), Entity: file.Entity}
	for i, record := range file.Records {
		input, err := record.Journal.ledgerInput()
		if err != nil {
			return result, apperr.Wrap(apperr.Input, "JOURNAL_IMPORT_RECORD_INVALID", fmt.Sprintf("record %d is invalid", i+1), err)
		}
		result.Records = append(result.Records, ledger.JournalImportRecord{Journal: input, RawJSON: record.RawJSON})
	}
	return result, nil
}
