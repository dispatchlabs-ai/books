package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/httpapi"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func readLimitedFile(path string, max int64) ([]byte, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = os.Stdin
	} else {
		var e error
		file, e = os.Open(path)
		if e != nil {
			return nil, apperr.Wrap(apperr.Input, "INPUT_READ_FAILED", "input could not be opened", e)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	data, e := io.ReadAll(io.LimitReader(reader, max+1))
	if e != nil {
		return nil, apperr.Wrap(apperr.Input, "INPUT_READ_FAILED", "input could not be read", e)
	}
	if int64(len(data)) > max {
		return nil, apperr.New(apperr.Input, "INPUT_TOO_LARGE", "input exceeds the supported byte limit")
	}
	return data, nil
}

func newBankImportCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "bank-import", Short: "Upload, preview, and apply bank statement files"}
	var input, name, key, optionsFile string
	upload := &cobra.Command{Use: "upload", Short: "Durably upload and parse a bank/card statement", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		data, e := readLimitedFile(input, banking.MaxBytes)
		if e != nil {
			return e
		}
		var sourceOptions banking.Options
		if optionsFile != "" {
			if input == "-" && optionsFile == "-" {
				return apperr.New(apperr.Input, "INPUT_INVALID", "statement and options cannot both use stdin")
			}
			encoded, e := readLimitedFile(optionsFile, banking.MaxOptionsBytes)
			if e != nil {
				return e
			}
			if e = application.DecodeRequest(encoded, &sourceOptions); e != nil {
				return e
			}
		}
		sourceName := name
		if sourceName == "" {
			sourceName = filepath.Base(input)
			if input == "-" {
				// Preserve the shipped stdin upload name in the idempotency contract.
				sourceName = "statement.ofx"
			}
		}
		app, e := openApplication(cmd, opts, storesqlite.ReadWrite)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		job, e := app.UploadWithOptions(cmd.Context(), key, sourceName, data, sourceOptions)
		if e != nil {
			return e
		}
		job, e = app.Process(cmd.Context(), job.ID)
		if e != nil {
			return e
		}
		return renderBankJob(cmd, opts, job)
	}}
	upload.Flags().StringVar(&input, "input", "", "statement file or - for stdin")
	upload.Flags().StringVar(&optionsFile, "options", "", "JSON format, source identity, and column/date profile")
	upload.Flags().StringVar(&name, "name", "", "source filename without a path")
	upload.Flags().StringVar(&key, "key", "", "stable upload idempotency key")
	_ = upload.MarkFlagRequired("input")
	_ = upload.MarkFlagRequired("key")
	show := &cobra.Command{Use: "show JOB", Short: "Read a durable import job and its parsed accounts", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, e := openApplication(cmd, opts, storesqlite.ReadOnly)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		job, e := app.Job(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return renderBankJob(cmd, opts, job)
	}}
	process := &cobra.Command{Use: "process JOB", Short: "Resume parsing an uploaded job", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, e := openApplication(cmd, opts, storesqlite.ReadWrite)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		job, e := app.Process(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return renderBankJob(cmd, opts, job)
	}}
	var choicesFile, previewKey string
	preview := &cobra.Command{Use: "preview JOB", Short: "Save a validated import preview from explicit mapping choices", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		data, e := readLimitedFile(choicesFile, 2<<20)
		if e != nil {
			return e
		}
		var choices ledger.BankImportChoices
		if e = application.DecodeRequest(data, &choices); e != nil {
			return e
		}
		app, e := openApplication(cmd, opts, storesqlite.ReadWrite)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		plan, e := app.Preview(cmd.Context(), args[0], previewKey, choices)
		if e != nil {
			return e
		}
		return renderBankPlan(cmd, opts, plan)
	}}
	preview.Flags().StringVar(&choicesFile, "input", "", "JSON account mappings and optional classifications")
	preview.Flags().StringVar(&previewKey, "key", "", "stable preview key; use a new key to replan")
	_ = preview.MarkFlagRequired("input")
	_ = preview.MarkFlagRequired("key")
	planShow := &cobra.Command{Use: "plan PLAN", Short: "Read a saved import preview", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, e := openApplication(cmd, opts, storesqlite.ReadOnly)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		plan, e := app.Plan(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return renderBankPlan(cmd, opts, plan)
	}}
	var digest string
	var commit bool
	apply := &cobra.Command{Use: "apply PLAN", Short: "Atomically apply the exact saved preview", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !commit {
			return apperr.New(apperr.Invalid, "COMMIT_REQUIRED", "inspect the preview, then supply --commit and its --digest")
		}
		app, e := openApplication(cmd, opts, storesqlite.ReadWrite)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		receipt, e := app.Apply(cmd.Context(), args[0], digest)
		if e != nil {
			return e
		}
		return writeResult(cmd, opts.format, receipt, []string{"JOB", "PLAN", "NEW TRANSACTIONS", "RETAINED OBSERVATIONS", "SKIPPED", "NEW JOURNALS", "EXISTING JOURNALS"}, [][]string{{receipt.JobID, receipt.PlanID, fmt.Sprint(receipt.Summary.ImportedTransactions), fmt.Sprint(receipt.Summary.RetainedObservations), fmt.Sprint(receipt.Summary.SkippedTransactions), fmt.Sprint(receipt.Summary.NewJournals), fmt.Sprint(receipt.Summary.ExistingJournals)}})
	}}
	apply.Flags().StringVar(&digest, "digest", "", "exact preview SHA-256")
	apply.Flags().BoolVar(&commit, "commit", false, "commit the preview")
	_ = apply.MarkFlagRequired("digest")
	var matchesFile string
	matches := &cobra.Command{Use: "matches JOB", Short: "Review possible existing matches for mapped statement activity", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		data, e := readLimitedFile(matchesFile, 2<<20)
		if e != nil {
			return e
		}
		var choices ledger.BankImportChoices
		if e = application.DecodeRequest(data, &choices); e != nil {
			return e
		}
		app, e := openApplication(cmd, opts, storesqlite.ReadOnly)
		if e != nil {
			return e
		}
		defer func() { _ = app.Close() }()
		result, e := app.Matches(cmd.Context(), args[0], choices)
		if e != nil {
			return e
		}
		rows := [][]string{}
		for _, match := range result.Matches {
			for _, candidate := range match.Candidates {
				rows = append(rows, []string{match.AccountKey, match.TransactionID, match.PostedDate, match.Amount, candidate.SourceRecordID, candidate.Description})
			}
		}
		return writeResult(cmd, opts.format, result, []string{"ACCOUNT KEY", "TRANSACTION", "DATE", "AMOUNT", "EXISTING SOURCE", "DESCRIPTION"}, rows)
	}}
	matches.Flags().StringVar(&matchesFile, "input", "", "JSON account mappings to inspect")
	_ = matches.MarkFlagRequired("input")
	formats := &cobra.Command{Use: "formats", Short: "List statement formats and required source profiles", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		values := banking.Capabilities()
		rows := [][]string{}
		for _, v := range values {
			rows = append(rows, []string{v.Format, v.Profile, strings.Join(v.OptionsRequired, ", ")})
		}
		return writeResult(cmd, opts.format, values, []string{"FORMAT", "PROFILE", "REQUIRED OPTIONS"}, rows)
	}}
	command.AddCommand(upload, show, process, preview, planShow, apply, matches, formats)
	return command
}
func renderBankJob(cmd *cobra.Command, opts *options, job ledger.BankImportJob) error {
	if opts.format != "table" {
		return writeResult(cmd, opts.format, job, []string{"JOB", "STATUS", "SOURCE"}, [][]string{{job.ID, job.Status, job.SourceName}})
	}
	rows := [][]string{{job.ID, job.Status, job.SourceName}}
	if e := writeResult(cmd, opts.format, job, []string{"JOB", "STATUS", "SOURCE"}, rows); e != nil {
		return e
	}
	if job.Error != nil {
		_, e := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", job.Error.Code, job.Error.Message)
		return e
	}
	if job.Document != nil {
		for _, diagnostic := range job.Document.Diagnostics {
			if _, e := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", diagnostic.Code, diagnostic.Message); e != nil {
				return e
			}
		}
		rows = nil
		for _, a := range job.Document.Accounts {
			suffix := a.AccountID
			if len(suffix) > 4 {
				suffix = "…" + suffix[len(suffix)-4:]
			}
			rows = append(rows, []string{a.Key, a.Kind, suffix, a.Currency, fmt.Sprint(len(a.Transactions))})
		}
		return writeResult(cmd, opts.format, job.Document.Accounts, []string{"ACCOUNT KEY", "KIND", "ACCOUNT", "CURRENCY", "TRANSACTIONS"}, rows)
	}
	return nil
}
func renderBankPlan(cmd *cobra.Command, opts *options, plan ledger.BankImportPlan) error {
	return writeResult(cmd, opts.format, plan, []string{"PLAN", "DIGEST", "NEW TRANSACTIONS", "RETAINED OBSERVATIONS", "SKIPPED", "NEW JOURNALS", "EXISTING JOURNALS"}, [][]string{{plan.ID, plan.Digest, fmt.Sprint(plan.Summary.ImportedTransactions), fmt.Sprint(plan.Summary.RetainedObservations), fmt.Sprint(plan.Summary.SkippedTransactions), fmt.Sprint(plan.Summary.NewJournals), fmt.Sprint(plan.Summary.ExistingJournals)}})
}

func newServeCommand(opts *options) *cobra.Command {
	var authFile string
	cmd := &cobra.Command{Use: "serve", Short: "Serve the authenticated client API for explicitly granted companies", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if opts.dryRun {
			return apperr.New(apperr.Invalid, "DRY_RUN_UNSUPPORTED", "serve starts a persistent service; --dry-run is not supported")
		}
		if opts.database != "" || opts.company != "" {
			return apperr.New(apperr.Invalid, "SERVER_SCOPE_CONFIGURED", "serve uses company grants in its server configuration, not --db or --company")
		}
		path, e := opts.resolveConfigPath()
		if e != nil {
			return e
		}
		config, e := httpapi.LoadConfig(authFile)
		if e != nil {
			return e
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		server, e := httpapi.New(ctx, path, config)
		if e != nil {
			return e
		}
		defer func() { _ = server.Close() }()
		return server.Serve(ctx)
	}}
	cmd.Flags().StringVar(&authFile, "server-config", "", "private server configuration with hashed credentials and company grants")
	_ = cmd.MarkFlagRequired("server-config")
	return cmd
}
