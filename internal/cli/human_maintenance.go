package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"

	"github.com/spf13/cobra"
)

type humanPeriod struct {
	Code         string `json:"code"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	FiscalYear   int    `json:"fiscal_year"`
	PeriodNumber int    `json:"period_number"`
	YearEnd      bool   `json:"year_end"`
	Status       string `json:"status"`
	CloseDigest  string `json:"close_digest,omitempty"`
}

func newDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use: "doctor", Short: "Verify the selected company's database and accounting invariants", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := store.Doctor(cmd.Context())
			if err != nil {
				return err
			}
			return writeResult(cmd, opts.format, result,
				[]string{"OK", "INTEGRITY", "FOREIGN KEYS", "UNBALANCED JOURNALS", "AUDIT EVENTS"},
				[][]string{{fmt.Sprint(result.OK), result.IntegrityCheck, strconv.Itoa(result.ForeignKeyViolations), strconv.Itoa(result.UnbalancedJournals), strconv.FormatInt(result.AuditEvents, 10)}})
		},
	}
}

func newBackupCommand(opts *options) *cobra.Command {
	var destination string
	command := &cobra.Command{
		Use: "backup", Short: "Create a verified backup in the company backup directory", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			if destination == "" {
				destination = filepath.Join(resolved.Backups, fmt.Sprintf("%s-%s.sqlite", resolved.Key, time.Now().UTC().Format("20060102T150405.000000000Z")))
			}
			destination, err = filepath.Abs(filepath.Clean(destination))
			if err != nil {
				return apperr.Wrap(apperr.Invalid, "BACKUP_PATH_INVALID", "resolve backup destination", err)
			}
			if opts.dryRun {
				data := map[string]any{"company": resolved.Key, "source": resolved.Database, "destination": destination, "dry_run": true}
				return writeResult(cmd, opts.format, data, []string{"COMPANY", "SOURCE", "DESTINATION", "DRY RUN"}, [][]string{{resolved.Key, resolved.Database, destination, "true"}})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			result, err := storesqlite.Backup(cmd.Context(), store, destination, opts.actor)
			if err != nil {
				return err
			}
			data := map[string]any{"company": resolved.Key, "path": result.Path, "sha256": result.SHA256}
			return writeResult(cmd, opts.format, data, []string{"COMPANY", "PATH", "SHA256"}, [][]string{{resolved.Key, result.Path, result.SHA256}})
		},
	}
	command.Flags().StringVarP(&destination, "out", "o", "", "backup path (defaults to a timestamped company backup)")
	return command
}

func newRestoreCommand(opts *options) *cobra.Command {
	var source, confirmation string
	command := &cobra.Command{
		Use: "restore", Short: "Validate and atomically restore a selected company's backup", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if source == "" {
				return apperr.New(apperr.Invalid, "RESTORE_SOURCE_REQUIRED", "--from is required")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			source, err = filepath.Abs(filepath.Clean(source))
			if err != nil {
				return apperr.Wrap(apperr.Invalid, "RESTORE_SOURCE_INVALID", "resolve restore source", err)
			}
			if source == filepath.Clean(resolved.Database) {
				return apperr.New(apperr.Invalid, "RESTORE_SOURCE_IS_TARGET", "restore source must be a separate backup file")
			}
			resolved, validation, expected, backfillIdentity, err := validateCompanyRestore(cmd, resolved, source)
			if err != nil {
				return err
			}
			if opts.dryRun {
				data := map[string]any{
					"company": resolved.Key, "source": validation.Source, "target": validation.Target,
					"source_sha256": validation.SourceSHA256, "source_database_uuid": validation.SourceDatabaseUUID,
					"previous_target_database_uuid": validation.PreviousTargetDatabaseUUID, "valid": true, "dry_run": true,
				}
				return writeResult(cmd, opts.format, data,
					[]string{"COMPANY", "SOURCE", "TARGET", "SOURCE SHA256", "SOURCE DATABASE UUID", "VALID", "DRY RUN"},
					[][]string{{resolved.Key, validation.Source, validation.Target, validation.SourceSHA256, validation.SourceDatabaseUUID, "true", "true"}})
			}
			if confirmation != resolved.Key {
				return apperr.New(apperr.Invalid, "RESTORE_CONFIRMATION_REQUIRED", fmt.Sprintf("--confirm must exactly equal the selected company key %q", resolved.Key))
			}
			if backfillIdentity {
				resolved, err = persistResolvedCompanyDatabaseUUID(opts, resolved, expected.DatabaseUUID)
				if err != nil {
					return err
				}
				expected.DatabaseUUID = resolved.Company.DatabaseUUID
			}
			result, err := storesqlite.Restore(cmd.Context(), resolved.Database, source, opts.actor, expected)
			if err != nil {
				return err
			}
			data := map[string]any{
				"company": resolved.Key, "source": result.Source, "target": result.Target,
				"pre_restore_backup": result.PreRestoreBackup, "source_sha256": result.SourceSHA256,
				"source_database_uuid":          result.SourceDatabaseUUID,
				"previous_target_database_uuid": result.PreviousTargetDatabaseUUID, "restored": true,
			}
			return writeResult(cmd, opts.format, data,
				[]string{"COMPANY", "SOURCE", "TARGET", "PRE-RESTORE BACKUP", "SOURCE SHA256", "SOURCE DATABASE UUID", "RESTORED"},
				[][]string{{resolved.Key, result.Source, result.Target, result.PreRestoreBackup, result.SourceSHA256, result.SourceDatabaseUUID, "true"}})
		},
	}
	command.Flags().StringVar(&source, "from", "", "backup SQLite file")
	command.Flags().StringVar(&confirmation, "confirm", "", "selected company key required to authorize replacement")
	return command
}

func validateCompanyRestore(cmd *cobra.Command, resolved booksconfig.ResolvedCompany, source string) (
	booksconfig.ResolvedCompany, storesqlite.RestoreValidation, storesqlite.RestoreExpectation, bool, error,
) {
	expected := storesqlite.RestoreExpectation{
		DatabaseUUID: resolved.Company.DatabaseUUID,
		EntityCode:   resolved.Company.EntityCode,
		BookCode:     resolved.Company.BookCode,
	}
	validation, err := storesqlite.ValidateRestore(cmd.Context(), resolved.Database, source, expected)
	if err != nil {
		return booksconfig.ResolvedCompany{}, storesqlite.RestoreValidation{}, storesqlite.RestoreExpectation{}, false, err
	}
	backfillIdentity := false
	if expected.DatabaseUUID == "" {
		if validation.PreviousTargetDatabaseUUID == "" {
			return booksconfig.ResolvedCompany{}, storesqlite.RestoreValidation{}, storesqlite.RestoreExpectation{}, false,
				apperr.New(apperr.Conflict, "RESTORE_DATABASE_IDENTITY_MISSING", "books.toml has no database UUID for this company; restore cannot safely adopt a backup while the registered database is missing")
		}
		expected.DatabaseUUID = validation.PreviousTargetDatabaseUUID
		backfillIdentity = true
	}
	return resolved, validation, expected, backfillIdentity, nil
}

func newPeriodsCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use: "periods", Short: "List the selected company's fiscal periods", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			periods, err := ledger.NewService(store, opts.actor).ListPeriods(cmd.Context(), resolved.Company.BookCode)
			if err != nil {
				return err
			}
			values := make([]humanPeriod, 0, len(periods))
			rows := make([][]string, 0, len(periods))
			for _, period := range periods {
				values = append(values, humanPeriod{
					Code: period.Code, StartDate: period.StartDate, EndDate: period.EndDate,
					FiscalYear: period.FiscalYear, PeriodNumber: period.PeriodNumber, YearEnd: period.YearEnd,
					Status: period.BookStatus, CloseDigest: period.CloseDigest,
				})
				rows = append(rows, []string{period.Code, period.StartDate, period.EndDate, strconv.Itoa(period.FiscalYear), strconv.Itoa(period.PeriodNumber), fmt.Sprint(period.YearEnd), period.BookStatus})
			}
			return writeResult(cmd, opts.format, values, []string{"PERIOD", "START", "END", "FISCAL YEAR", "NUMBER", "YEAR END", "STATUS"}, rows)
		},
	}
	command.AddCommand(newAddFiscalYearCommand(opts))
	return command
}

func newAddFiscalYearCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use: "add YEAR", Short: "Add any missing monthly periods for one fiscal year", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			year, err := strconv.Atoi(args[0])
			if err != nil || year < 1900 {
				return apperr.New(apperr.Invalid, "FISCAL_YEAR_INVALID", "YEAR must be a four-digit fiscal year")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			endMonth := time.Month(resolved.Company.FiscalYearEnd)
			startMonth := endMonth%12 + 1
			startYear := year
			if startMonth != time.January {
				startYear--
			}
			planned := monthlyPeriods(time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.Local), endMonth)
			var periodsCreated int
			if opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				periodsCreated, err = ledger.NewService(store, opts.actor).PreviewMissingPeriods(cmd.Context(), planned)
				closeErr := store.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
			} else {
				store, err := openWrite(cmd, opts)
				if err != nil {
					return err
				}
				periodsCreated, err = ledger.NewService(store, opts.actor).CreateMissingPeriods(cmd.Context(), planned)
				closeErr := store.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
			}
			data := map[string]any{"company": resolved.Key, "fiscal_year": year, "periods_created": periodsCreated, "dry_run": opts.dryRun}
			return writeResult(cmd, opts.format, data, []string{"COMPANY", "FISCAL YEAR", "PERIODS CREATED", "DRY RUN"}, [][]string{{resolved.Key, strconv.Itoa(year), strconv.Itoa(periodsCreated), fmt.Sprint(opts.dryRun)}})
		},
	}
}

func newReopenCommand(opts *options) *cobra.Command {
	var reason string
	command := &cobra.Command{
		Use: "reopen PERIOD", Short: "Reopen a closed period with an audit reason", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return apperr.New(apperr.Invalid, "REOPEN_REASON_REQUIRED", "--reason is required")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			periodCode := strings.ToUpper(strings.TrimSpace(args[0]))
			if opts.dryRun {
				store, err := openRead(cmd, opts)
				if err != nil {
					return err
				}
				periods, err := ledger.NewService(store, opts.actor).ListPeriods(cmd.Context(), resolved.Company.BookCode)
				_ = store.Close()
				if err != nil {
					return err
				}
				found := false
				for _, period := range periods {
					if period.Code != periodCode {
						continue
					}
					found = true
					if period.BookStatus != "CLOSED" {
						return apperr.New(apperr.Conflict, "PERIOD_NOT_CLOSED", "book period is not closed")
					}
					break
				}
				if !found {
					return apperr.New(apperr.NotFound, "BOOK_PERIOD_NOT_FOUND", "period is not configured for this book")
				}
				data := map[string]any{"company": resolved.Key, "period": periodCode, "reason": reason, "status": "OPEN (PREVIEW)", "dry_run": true}
				return writeResult(cmd, opts.format, data, []string{"COMPANY", "PERIOD", "STATUS", "REASON", "DRY RUN"}, [][]string{{resolved.Key, periodCode, "OPEN (PREVIEW)", reason, "true"}})
			}
			store, err := openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			if err := ledger.NewService(store, opts.actor).ReopenPeriod(cmd.Context(), resolved.Company.BookCode, periodCode, reason); err != nil {
				return err
			}
			data := map[string]any{"company": resolved.Key, "period": periodCode, "reason": reason, "status": "OPEN"}
			return writeResult(cmd, opts.format, data, []string{"COMPANY", "PERIOD", "STATUS", "REASON"}, [][]string{{resolved.Key, periodCode, "OPEN", reason}})
		},
	}
	command.Flags().StringVar(&reason, "reason", "", "required audit reason")
	return command
}
