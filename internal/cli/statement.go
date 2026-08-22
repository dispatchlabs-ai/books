package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
)

func newStatementAccountCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "statement-account", Short: "Manage bank, credit-card, loan, and investment statement accounts"}
	var code, entity, book, account, name, kind, currency, reconcileFrom string
	var required bool
	create := &cobra.Command{
		Use: "create", Short: "Link a statement account to one GL control account", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.CreateStatementAccountInput{Code: code, Entity: entity, Book: book, GLAccount: account, Name: name, Kind: kind, Currency: currency, RequiredForClose: required, ReconciliationRequiredFrom: reconcileFrom}
			if err := requireCommit(opts, "statement-account create"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).CreateStatementAccount(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, statementAccountHeaders, [][]string{statementAccountRow(result)})
		},
	}
	create.Flags().StringVar(&code, "code", "", "statement-account code")
	create.Flags().StringVar(&entity, "entity", "", "entity code")
	create.Flags().StringVar(&book, "book", "", "actual book code")
	create.Flags().StringVar(&account, "account", "", "GL control account code")
	create.Flags().StringVar(&name, "name", "", "statement-account name")
	create.Flags().StringVar(&kind, "kind", "", "BANK, CREDIT_CARD, LOAN, or INVESTMENT")
	create.Flags().StringVar(&currency, "currency", "USD", "account currency")
	create.Flags().BoolVar(&required, "required-for-close", true, "require reconciliation before period close")
	create.Flags().StringVar(&reconcileFrom, "reconcile-from", "", "first date requiring reconciliation for period close")
	_ = create.MarkFlagRequired("reconcile-from")
	list := &cobra.Command{
		Use: "list", Short: "List statement accounts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			accounts, err := ledger.NewService(store, opts.actor).ListStatementAccounts(cmd.Context(), entity)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(accounts))
			for _, value := range accounts {
				rows = append(rows, statementAccountRow(value))
			}
			return writeResult(cmd, opts.format, accounts, statementAccountHeaders, rows)
		},
	}
	list.Flags().StringVar(&entity, "entity", "", "optional entity code")
	var archiveCode, archiveReason, reconcileThrough string
	var commitArchive bool
	archive := &cobra.Command{
		Use: "archive", Short: "Archive a statement account with an audit reason", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.ArchiveStatementAccountInput{Code: archiveCode, ReconciliationRequiredThrough: reconcileThrough, Reason: archiveReason}
			if !commitArchive || opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
				validated, err := ledger.NewService(store, opts.actor).ValidateStatementAccountArchive(cmd.Context(), input)
				if err != nil {
					return err
				}
				data := map[string]any{"code": validated.Code, "reconciliation_required_through": validated.ReconciliationRequiredThrough, "reason": validated.ArchiveReason, "target_status": validated.Status, "committed": false, "dry_run": opts.dryRun}
				return writeResult(cmd, opts.format, data,
					[]string{"CODE", "RECONCILE THROUGH", "TARGET STATUS", "REASON", "COMMITTED", "DRY RUN"},
					[][]string{{validated.Code, validated.ReconciliationRequiredThrough, validated.Status, validated.ArchiveReason, "false", fmt.Sprint(opts.dryRun)}})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ArchiveStatementAccount(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, statementAccountHeaders, [][]string{statementAccountRow(result)})
		},
	}
	archive.Flags().StringVar(&archiveCode, "code", "", "statement-account code")
	archive.Flags().StringVar(&reconcileThrough, "reconcile-through", "", "last date requiring reconciliation for period close")
	archive.Flags().StringVar(&archiveReason, "reason", "", "required audit reason")
	archive.Flags().BoolVar(&commitArchive, "commit", false, "commit the archive; otherwise preview it")
	_ = archive.MarkFlagRequired("reconcile-through")
	command.AddCommand(create, list, archive, newStatementAccountIdentityCommand(opts), newStatementAccountLifecycleCommand(opts))
	return command
}

var statementAccountHeaders = []string{"CODE", "ENTITY", "BOOK", "GL ACCOUNT", "KIND", "CURRENCY", "REQUIRED", "RECONCILE FROM", "RECONCILE THROUGH", "STATUS", "ARCHIVED AT", "ARCHIVED BY", "ARCHIVE REASON", "ID"}

func statementAccountRow(value ledger.StatementAccount) []string {
	return []string{value.Code, value.EntityCode, value.BookCode, value.GLAccountCode, value.Kind,
		value.Currency, fmt.Sprint(value.RequiredForClose),
		value.ReconciliationRequiredFrom, value.ReconciliationRequiredThrough, value.Status, value.ArchivedAt, value.ArchivedBy,
		value.ArchiveReason, value.ID}
}

var statementAccountIdentityHeaders = []string{
	"ENTITY", "STATEMENT ACCOUNT", "SOURCE", "REALM", "EXTERNAL ID", "NUMBER", "NAME", "ACTIVE",
	"SOURCE KIND", "SOURCE PATH", "SOURCE SHA256", "LOCATOR", "PAYLOAD SHA256", "CREATED AT", "CREATED BY", "ID",
}

func statementAccountIdentityRow(value ledger.StatementAccountIdentity) []string {
	return []string{
		value.EntityCode, value.StatementAccount, value.SourceSystem, value.SourceRealm, value.ExternalID,
		value.AccountNumber, value.Name, fmt.Sprint(value.Active), value.Evidence.SourceKind,
		value.Evidence.SourcePath, value.Evidence.SourceSHA256, value.Evidence.Locator,
		value.Evidence.PayloadSHA256, value.CreatedAt, value.CreatedBy, value.ID,
	}
}

func newStatementAccountIdentityCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "identity", Short: "Manage immutable source aliases for statement accounts"}
	var statementAccount, sourceSystem, sourceRealm, externalID, accountNumber, name string
	var sourceKind, sourcePath, sourceSHA256, locator, payloadSHA256 string
	var active, commit bool
	add := &cobra.Command{
		Use: "add", Short: "Record an evidence-backed source alias", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input := ledger.AddStatementAccountIdentityInput{
				StatementAccount: statementAccount, SourceSystem: sourceSystem, SourceRealm: sourceRealm, ExternalID: externalID,
				AccountNumber: accountNumber, Name: name, Active: active,
				Evidence: ledger.StatementAccountIdentityEvidence{
					SourceKind: sourceKind, SourcePath: sourcePath, SourceSHA256: sourceSHA256,
					Locator: locator, PayloadSHA256: payloadSHA256,
				},
			}
			if !commit || opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
				normalized, err := ledger.NewService(store, opts.actor).ValidateStatementAccountIdentity(cmd.Context(), input)
				if err != nil {
					return err
				}
				preview := map[string]any{"input": normalized, "committed": false, "dry_run": opts.dryRun}
				return writeResult(cmd, opts.format, preview,
					[]string{"STATEMENT ACCOUNT", "SOURCE", "REALM", "EXTERNAL ID", "NUMBER", "NAME", "ACTIVE", "SOURCE KIND", "SOURCE PATH", "LOCATOR", "COMMITTED", "DRY RUN"},
					[][]string{{normalized.StatementAccount, normalized.SourceSystem, normalized.SourceRealm, normalized.ExternalID, normalized.AccountNumber, normalized.Name, fmt.Sprint(normalized.Active), normalized.Evidence.SourceKind, normalized.Evidence.SourcePath, normalized.Evidence.Locator, "false", fmt.Sprint(opts.dryRun)}})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).AddStatementAccountIdentity(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, statementAccountIdentityHeaders, [][]string{statementAccountIdentityRow(result)})
		},
	}
	add.Flags().StringVar(&statementAccount, "statement-account", "", "statement-account code")
	add.Flags().StringVar(&sourceSystem, "source-system", "", "source system, such as QBO or PROVIDER")
	add.Flags().StringVar(&sourceRealm, "source-realm", "", "source realm; use GLOBAL only when source ids are globally scoped")
	add.Flags().StringVar(&externalID, "external-id", "", "source-local account id")
	add.Flags().StringVar(&accountNumber, "account-number", "", "account number observed in the source")
	add.Flags().StringVar(&name, "name", "", "account name observed in the source")
	add.Flags().BoolVar(&active, "active", true, "whether the account was active in the source")
	add.Flags().StringVar(&sourceKind, "source-kind", "", "evidence source kind")
	add.Flags().StringVar(&sourcePath, "source-path", "", "path or stable name of the evidence source")
	add.Flags().StringVar(&sourceSHA256, "source-sha256", "", "SHA-256 of the evidence source")
	add.Flags().StringVar(&locator, "locator", "", "location of the account inside the evidence source")
	add.Flags().StringVar(&payloadSHA256, "payload-sha256", "", "optional SHA-256 of the source account payload")
	add.Flags().BoolVar(&commit, "commit", false, "commit the identity; otherwise validate and preview it")
	for _, flag := range []string{"statement-account", "source-system", "source-realm", "external-id", "name", "source-kind", "source-path", "source-sha256", "locator"} {
		_ = add.MarkFlagRequired(flag)
	}

	var listStatementAccount, listEntity, listSourceSystem, listSourceRealm string
	list := &cobra.Command{
		Use: "list", Short: "List statement-account source aliases", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			identities, err := ledger.NewService(store, opts.actor).ListStatementAccountIdentities(cmd.Context(), ledger.StatementAccountIdentityFilter{
				StatementAccount: listStatementAccount, Entity: listEntity, SourceSystem: listSourceSystem, SourceRealm: listSourceRealm,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(identities))
			for _, identity := range identities {
				rows = append(rows, statementAccountIdentityRow(identity))
			}
			return writeResult(cmd, opts.format, identities, statementAccountIdentityHeaders, rows)
		},
	}
	list.Flags().StringVar(&listStatementAccount, "statement-account", "", "optional statement-account code")
	list.Flags().StringVar(&listEntity, "entity", "", "optional entity code")
	list.Flags().StringVar(&listSourceSystem, "source-system", "", "optional source system")
	list.Flags().StringVar(&listSourceRealm, "source-realm", "", "optional source realm")
	command.AddCommand(add, list)
	return command
}

type statementImportFile struct {
	StatementAccount string                     `json:"statement_account"`
	SourceSystem     string                     `json:"source_system"`
	SourceName       string                     `json:"source_name"`
	Transactions     []statementTransactionFile `json:"transactions"`
}

type statementTransactionFile struct {
	ExternalID          string          `json:"external_id"`
	PostedDate          string          `json:"posted_date"`
	Description         string          `json:"description"`
	Amount              string          `json:"amount"`
	Disposition         string          `json:"disposition,omitempty"`
	ExclusionReason     string          `json:"exclusion_reason,omitempty"`
	TaxType             string          `json:"tax_type,omitempty"`
	TaxAccountingPeriod string          `json:"tax_accounting_period,omitempty"`
	RawJSON             json.RawMessage `json:"raw_json,omitempty"`
	ResolutionReason    string          `json:"resolution_reason,omitempty"`
	ResolutionEvidence  json.RawMessage `json:"resolution_evidence,omitempty"`
}

func readInputBytes(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, apperr.Wrap(apperr.Input, "INPUT_READ_FAILED", "read stdin", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperr.Wrap(apperr.Input, "INPUT_READ_FAILED", "read input file", err)
	}
	return data, nil
}

func parseStatementImport(data []byte) (ledger.StatementImportInput, error) {
	var file statementImportFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return ledger.StatementImportInput{}, apperr.Wrap(apperr.Input, "INPUT_JSON_INVALID", "decode statement import", err)
	}
	sum := sha256.Sum256(data)
	result := ledger.StatementImportInput{StatementAccount: file.StatementAccount, SourceSystem: file.SourceSystem, SourceName: file.SourceName, FileSHA256: hex.EncodeToString(sum[:])}
	for i, value := range file.Transactions {
		amount, err := money.Parse(value.Amount)
		if err != nil {
			return result, apperr.Wrap(apperr.Input, "STATEMENT_AMOUNT_INVALID", fmt.Sprintf("transaction %d amount is invalid", i+1), err)
		}
		raw := value.RawJSON
		if len(raw) == 0 {
			raw, _ = json.Marshal(value)
		}
		result.Transactions = append(result.Transactions, ledger.StatementTransactionInput{
			ExternalID: value.ExternalID, PostedDate: value.PostedDate, Description: value.Description,
			AmountCents: amount, Disposition: value.Disposition, ExclusionReason: value.ExclusionReason,
			TaxType: value.TaxType, TaxAccountingPeriod: value.TaxAccountingPeriod, RawJSON: raw,
			ResolutionReason: value.ResolutionReason, ResolutionEvidence: value.ResolutionEvidence,
		})
	}
	return result, nil
}

func newStatementCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "statement", Short: "Import immutable statement transactions"}
	var inputPath string
	importCommand := &cobra.Command{
		Use: "import", Short: "Atomically import statement transactions from JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" {
				return apperr.New(apperr.Invalid, "INPUT_REQUIRED", "--input is required")
			}
			if err := requireCommit(opts, "statement import"); err != nil {
				return err
			}
			data, err := readInputBytes(inputPath)
			if err != nil {
				return err
			}
			input, err := parseStatementImport(data)
			if err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ImportStatementTransactions(cmd.Context(), input)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result, []string{"BATCH", "CHANGED", "RECORDS", "STATEMENT TRANSACTIONS", "SOURCE ONLY", "SKIPPED"}, [][]string{{result.BatchID, fmt.Sprint(result.Changed), fmt.Sprint(result.RecordCount), fmt.Sprint(result.StatementTransactionCount), fmt.Sprint(result.SourceOnlyCount), fmt.Sprint(result.SkippedCount)}})
		},
	}
	importCommand.Flags().StringVarP(&inputPath, "input", "i", "", "JSON input file or - for stdin")
	command.AddCommand(importCommand)
	return command
}

func newTransactionCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "transaction", Short: "Inspect immutable statement transactions"}
	var account, from, to string
	var unallocated bool
	list := &cobra.Command{
		Use: "list", Short: "List statement transactions and allocation state", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			query := `SELECT st.id, sa.code, si.external_id, st.posted_date, st.description, st.amount_cents,
	                COALESCE(sr.disposition, ''), COALESCE(sr.exclusion_reason, ''),
	                COALESCE(SUM(ri.allocated_amount_cents), 0), COUNT(ri.id) FROM statement_transactions st
	                JOIN statement_accounts sa ON sa.id = st.statement_account_id
	                JOIN source_identities si ON si.id = st.source_identity_id
	                LEFT JOIN source_records sr ON sr.id = st.source_record_id
                LEFT JOIN reconciliation_allocations ri ON ri.statement_transaction_id = st.id WHERE 1=1`
			var queryArgs []any
			if account != "" {
				query += " AND sa.code = ?"
				queryArgs = append(queryArgs, strings.ToUpper(account))
			}
			if from != "" {
				query += " AND st.posted_date >= ?"
				queryArgs = append(queryArgs, from)
			}
			if to != "" {
				query += " AND st.posted_date <= ?"
				queryArgs = append(queryArgs, to)
			}
			query += ` GROUP BY st.id, sa.code, si.external_id, st.posted_date, st.description, st.amount_cents, sr.disposition, sr.exclusion_reason`
			if unallocated {
				query += " HAVING COALESCE(SUM(ri.allocated_amount_cents), 0) <> st.amount_cents"
			}
			query += " ORDER BY st.posted_date, sa.code, si.external_id"
			rowsDB, err := store.DB().QueryContext(cmd.Context(), query, queryArgs...)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)
			type item struct {
				ID               string `json:"id"`
				StatementAccount string `json:"statement_account"`
				ExternalID       string `json:"external_id"`
				PostedDate       string `json:"posted_date"`
				Description      string `json:"description"`
				AmountCents      int64  `json:"amount_cents"`
				Disposition      string `json:"disposition"`
				ExclusionReason  string `json:"exclusion_reason,omitempty"`
				AllocatedCents   int64  `json:"allocated_cents"`
				RemainingCents   int64  `json:"remaining_cents"`
				AllocationCount  int    `json:"allocation_count"`
			}
			var data []item
			var rows [][]string
			for rowsDB.Next() {
				var value item
				if err := rowsDB.Scan(&value.ID, &value.StatementAccount, &value.ExternalID, &value.PostedDate, &value.Description, &value.AmountCents, &value.Disposition, &value.ExclusionReason, &value.AllocatedCents, &value.AllocationCount); err != nil {
					return err
				}
				value.RemainingCents = value.AmountCents - value.AllocatedCents
				data = append(data, value)
				rows = append(rows, []string{value.PostedDate, value.StatementAccount, value.Description, money.Format(value.AmountCents), value.Disposition, money.Format(value.AllocatedCents), money.Format(value.RemainingCents), fmt.Sprint(value.AllocationCount), value.ExternalID, value.ID})
			}
			return writeResult(cmd, opts.format, data, []string{"DATE", "ACCOUNT", "DESCRIPTION", "STATEMENT AMOUNT", "DISPOSITION", "ALLOCATED", "REMAINING", "ALLOCATIONS", "EXTERNAL ID", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&account, "account", "", "statement-account code")
	list.Flags().StringVar(&from, "from", "", "start date")
	list.Flags().StringVar(&to, "to", "", "end date")
	list.Flags().BoolVar(&unallocated, "unallocated", false, "show only transactions that are not fully allocated")
	command.AddCommand(list)
	return command
}

func newSourceCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "source", Short: "Inspect immutable imported source evidence and journal links"}
	var account, disposition, from, to string
	var includeHistory bool
	list := &cobra.Command{
		Use: "list", Short: "List source records including non-postable dispositions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListSourceRecords(cmd.Context(), ledger.SourceRecordFilter{
				SourceAccount: account, Disposition: disposition, FromDate: from, ToDate: to,
				IncludeHistory: includeHistory,
			})
			if err != nil {
				return err
			}
			var rows [][]string
			for _, record := range result {
				amount := ""
				if record.AmountCents != nil {
					amount = money.Format(*record.AmountCents)
				}
				rows = append(rows, []string{record.TransactionDate, record.SourceSystem, record.SourceAccount,
					record.ExternalID, fmt.Sprint(record.Revision), fmt.Sprint(record.Current), amount,
					record.Disposition, record.ExclusionReason,
					record.StatementTransactionID, fmt.Sprint(record.JournalLinkCount), record.ID})
			}
			return writeResult(cmd, opts.format, result,
				[]string{"DATE", "SYSTEM", "SOURCE ACCOUNT", "EXTERNAL ID", "REVISION", "CURRENT", "AMOUNT", "DISPOSITION", "EXCLUSION", "STATEMENT TRANSACTION", "JOURNALS", "ID"}, rows)
		},
	}
	list.Flags().StringVar(&account, "account", "", "optional source or statement-account code")
	list.Flags().StringVar(&disposition, "disposition", "", "optional POSTED, PENDING, NEEDS_REVIEW, or SOURCE_ONLY")
	list.Flags().StringVar(&from, "from", "", "optional start date")
	list.Flags().StringVar(&to, "to", "", "optional end date")
	list.Flags().BoolVar(&includeHistory, "history", false, "include superseded source observations")

	var sourceRecordID, journalID, role string
	show := &cobra.Command{
		Use: "show", Short: "Show immutable source metadata without raw JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).GetSourceRecord(cmd.Context(), sourceRecordID)
			if err != nil {
				return err
			}
			amount := ""
			if result.AmountCents != nil {
				amount = money.Format(*result.AmountCents)
			}
			return writeResult(cmd, opts.format, result,
				[]string{"ID", "SOURCE IDENTITY", "REVISION", "CURRENT", "SUPERSEDES", "OBSERVATION", "BATCH", "IMPORT NAME", "IMPORT SHA256", "ENTITY", "BOOK", "LOCATOR",
					"DATE", "DESCRIPTION", "AMOUNT", "TAX TYPE", "TAX PERIOD", "DISPOSITION", "EXCLUSION",
					"RAW JSON SHA256", "RESOLUTION", "RESOLUTION EVIDENCE SHA256", "STATEMENT TRANSACTION", "JOURNALS", "CREATED", "ACTOR"},
				[][]string{{result.ID, result.SourceIdentityID, fmt.Sprint(result.Revision), fmt.Sprint(result.Current),
					result.SupersedesSourceRecordID, result.ObservationKind, result.ImportBatchID, result.ImportSourceName, result.ImportFileSHA256,
					result.EntityCode, result.BookCode, result.SourceLocator, result.TransactionDate, result.Description, amount,
					result.TaxType, result.TaxAccountingPeriod, result.Disposition, result.ExclusionReason,
					result.RawJSONSHA256, result.ResolutionReason, result.ResolutionEvidenceSHA,
					result.StatementTransactionID, fmt.Sprint(result.JournalLinkCount), result.CreatedAt, result.CreatedBy}})
		},
	}
	show.Flags().StringVar(&sourceRecordID, "id", "", "source-record id")
	link := &cobra.Command{
		Use: "link-journal", Short: "Create an immutable evidence link from a source record to a journal", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireCommit(opts, "source link-journal"); err != nil {
				return err
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).LinkSourceRecordJournal(cmd.Context(), sourceRecordID, journalID, role)
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result,
				[]string{"SOURCE RECORD", "JOURNAL", "BOOK", "ENTRY", "ROLE", "CHANGED"},
				[][]string{{result.SourceRecordID, result.JournalID, result.BookCode, fmt.Sprint(result.EntryNumber), result.LinkRole, fmt.Sprint(result.Changed)}})
		},
	}
	link.Flags().StringVar(&sourceRecordID, "source-record", "", "source-record id")
	link.Flags().StringVar(&journalID, "journal", "", "journal id")
	link.Flags().StringVar(&role, "role", "EVIDENCE", "PRIMARY, EVIDENCE, MIRROR, or ELIMINATION")

	links := &cobra.Command{
		Use: "links", Short: "List journal links for a source record", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := ledger.NewService(store, opts.actor).ListSourceJournalLinks(cmd.Context(), sourceRecordID)
			if err != nil {
				return err
			}
			var rows [][]string
			for _, value := range result {
				rows = append(rows, []string{value.SourceRecordID, value.JournalID, value.BookCode,
					fmt.Sprint(value.EntryNumber), value.LinkRole, value.CreatedAt, value.CreatedBy})
			}
			return writeResult(cmd, opts.format, result,
				[]string{"SOURCE RECORD", "JOURNAL", "BOOK", "ENTRY", "ROLE", "CREATED", "ACTOR"}, rows)
		},
	}
	links.Flags().StringVar(&sourceRecordID, "source-record", "", "source-record id")
	command.AddCommand(list, show, link, links)
	return command
}
