package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/dispatchlabs-ai/books/internal/version"

	"github.com/spf13/cobra"
)

type options struct {
	database       string
	format         string
	actor          string
	dryRun         bool
	configPath     string
	company        string
	jsonOutput     bool
	noInput        bool
	formatExplicit bool
	loadedConfig   *booksconfig.Config
	resolvedConfig string
	resolved       *booksconfig.ResolvedCompany
}

type renderedError struct{ err error }

func (e *renderedError) Error() string { return e.err.Error() }
func (e *renderedError) Unwrap() error { return e.err }

func IsRendered(err error) bool {
	var target *renderedError
	return errors.As(err, &target)
}

func databaseOverride() string {
	if value := strings.TrimSpace(os.Getenv("BOOKS_DB")); value != "" {
		return value
	}
	return ""
}

func defaultActor() string {
	if value := strings.TrimSpace(os.Getenv("BOOKS_ACTOR")); value != "" {
		return value
	}
	current, err := user.Current()
	if err == nil && current.Username != "" {
		return current.Username
	}
	return "unknown"
}

func newRootCommand() (*cobra.Command, *options) {
	opts := &options{database: databaseOverride(), format: "table", actor: defaultActor(), noInput: true}
	root := &cobra.Command{
		Use:           "books",
		Short:         "Noninteractive, local-first double-entry accounting",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Identifier(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			opts.formatExplicit = cmd.Root().PersistentFlags().Changed("format") || cmd.Root().PersistentFlags().Changed("json")
			if opts.jsonOutput {
				opts.format = "json"
			}
			if !opts.formatExplicit && strings.TrimSpace(opts.database) == "" {
				path, err := opts.resolveConfigPath()
				if err != nil {
					return err
				}
				if _, err := os.Stat(path); err == nil {
					value, err := booksconfig.Load(path)
					if err != nil {
						if cmd.CommandPath() != "books config path" {
							return apperr.Wrap(apperr.Invalid, "CONFIG_INVALID", "load Books configuration", err)
						}
					} else {
						opts.loadedConfig = &value
						if value.Defaults.Output != "" {
							opts.format = value.Defaults.Output
						}
					}
				} else if !os.IsNotExist(err) {
					return apperr.Wrap(apperr.Unavailable, "CONFIG_STAT_FAILED", "inspect Books configuration", err)
				}
			}
			switch opts.format {
			case "table", "json", "jsonl", "csv":
				return nil
			default:
				return apperr.New(apperr.Invalid, "FORMAT_INVALID", "format must be table, json, jsonl, or csv")
			}
		},
	}
	root.SetVersionTemplate("books {{.Version}}\n")
	root.PersistentFlags().StringVar(&opts.database, "db", opts.database, "explicit SQLite database path (or BOOKS_DB) for advanced direct-database commands")
	root.PersistentFlags().StringVar(&opts.configPath, "config", "", "Books configuration path (default ~/.books/books.toml or BOOKS_CONFIG)")
	root.PersistentFlags().StringVarP(&opts.company, "company", "c", "", "registered company key (defaults to default_company)")
	root.PersistentFlags().StringVar(&opts.format, "format", opts.format, "output format: table, json, jsonl, csv")
	root.PersistentFlags().BoolVar(&opts.jsonOutput, "json", false, "emit stable JSON output (alias for --format json)")
	root.PersistentFlags().StringVar(&opts.actor, "actor", opts.actor, "audit actor (or BOOKS_ACTOR)")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", false, "validate and preview without committing where supported")
	root.PersistentFlags().BoolVar(&opts.noInput, "no-input", true, "never read interactive input (always true; retained for explicit automation contracts)")
	root.AddCommand(
		newInitCommand(opts),
		newCompanyCommand(opts),
		newCompaniesCommand(opts),
		newConfigCommand(opts),
		newDBCommand(opts),
		newEntityCommand(opts),
		newBookCommand(opts),
		newOwnershipCommand(opts),
		newGroupCommand(opts),
		newAccountsCommand(opts),
		newAccountCommand(opts),
		newSpendCommand(opts),
		newReceiveCommand(opts),
		newTransferCommand(opts),
		newTxCommand(opts),
		newCorrectCommand(opts),
		newReverseCommand(opts),
		newUndoCommand(opts),
		newCloseCommand(opts),
		newYearCloseCommand(opts),
		newPeriodsCommand(opts),
		newReopenCommand(opts),
		newBackupCommand(opts),
		newRestoreCommand(opts),
		newDoctorCommand(opts),
		newImportCommand(opts),
		newPeriodCommand(opts),
		newJournalCommand(opts),
		newStatementAccountCommand(opts),
		newStatementCommand(opts),
		newTransactionCommand(opts),
		newSourceCommand(opts),
		newImportBatchCommand(opts),
		newReconcileCommand(opts),
		newReportCommand(opts),
		newAuditCommand(opts),
	)
	root.AddCommand(
		newRootReportAlias(opts, "gl", newGLCommand(opts)),
		newRootReportAlias(opts, "tb", newTBCommand(opts)),
		newRootReportAlias(opts, "pl", newPLCommand(opts)),
		newRootReportAlias(opts, "bs", newBSCommand(opts)),
	)
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return apperr.New(apperr.Invalid, "ARGUMENT_INVALID", "books does not accept positional arguments; run books --help")
		}
		return runDashboard(cmd, opts)
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return apperr.New(apperr.Invalid, "FLAG_INVALID", err.Error())
	})
	return root, opts
}

func Execute() error {
	return executeArgs(os.Args[1:], os.Stdout, os.Stderr)
}

func executeArgs(args []string, stdout, stderr io.Writer) error {
	root, opts := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	selectEarlyOutput(opts, args)
	err := root.Execute()
	err = normalizeInvocationError(err)
	if err != nil && (opts.format == "json" || opts.format == "jsonl") {
		_ = writeError(root.ErrOrStderr(), err)
		return &renderedError{err: err}
	}
	return err
}

func selectEarlyOutput(opts *options, args []string) {
	var explicit bool
	// Detect the JSON alias independently of flag-value parsing so even a
	// malformed earlier flag cannot hide the caller's machine-output request.
	for _, argument := range args {
		if argument == "--" {
			break
		}
		if argument == "--json" {
			opts.jsonOutput = true
			opts.format = "json"
			explicit = true
			continue
		}
		if strings.HasPrefix(argument, "--json=") {
			value, err := strconv.ParseBool(strings.TrimPrefix(argument, "--json="))
			if err == nil {
				opts.jsonOutput = value
				if value {
					opts.format = "json"
				}
			}
			explicit = true
		}
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			break
		}
		switch {
		case argument == "--json":
			opts.jsonOutput = true
			opts.format = "json"
			explicit = true
		case strings.HasPrefix(argument, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(argument, "--json="))
			if err == nil {
				opts.jsonOutput = value
				if value {
					opts.format = "json"
				}
			}
			explicit = true
		case argument == "--format" && index+1 < len(args):
			opts.format = strings.ToLower(strings.TrimSpace(args[index+1]))
			explicit = true
			index++
		case strings.HasPrefix(argument, "--format="):
			opts.format = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(argument, "--format=")))
			explicit = true
		case argument == "--config" && index+1 < len(args):
			opts.configPath = args[index+1]
			index++
		case strings.HasPrefix(argument, "--config="):
			opts.configPath = strings.TrimPrefix(argument, "--config=")
		case argument == "--db" && index+1 < len(args):
			opts.database = args[index+1]
			index++
		case strings.HasPrefix(argument, "--db="):
			opts.database = strings.TrimPrefix(argument, "--db=")
		}
	}
	if opts.jsonOutput {
		opts.format = "json"
		explicit = true
	}
	opts.formatExplicit = explicit
	if explicit || strings.TrimSpace(opts.database) != "" {
		return
	}
	path, err := booksconfig.DefaultPath(opts.configPath)
	if err != nil {
		return
	}
	value, err := booksconfig.Load(path)
	if err == nil && value.Defaults.Output != "" {
		opts.loadedConfig = &value
		opts.resolvedConfig = path
		opts.format = value.Defaults.Output
	}
}

func normalizeInvocationError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "unknown command "):
		return apperr.New(apperr.Invalid, "COMMAND_NOT_FOUND", message)
	case strings.Contains(message, "unknown flag"), strings.Contains(message, "unknown shorthand flag"),
		strings.Contains(message, "flag needs an argument"), strings.Contains(message, "invalid argument") && strings.Contains(message, "for \"--"):
		return apperr.New(apperr.Invalid, "FLAG_INVALID", message)
	case strings.Contains(message, "arg(s)"), strings.Contains(message, "required flag"):
		return apperr.New(apperr.Invalid, "ARGUMENT_INVALID", message)
	default:
		return err
	}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	appError, ok := apperr.As(err)
	if !ok {
		return 1
	}
	switch appError.Kind {
	case apperr.Invalid:
		return 2
	case apperr.NotFound:
		return 3
	case apperr.Validation:
		return 4
	case apperr.Conflict:
		return 5
	case apperr.Integrity:
		return 6
	case apperr.Input:
		return 7
	case apperr.Unavailable:
		return 8
	default:
		return 1
	}
}

func openRead(cmd *cobra.Command, opts *options) (*storesqlite.Store, error) {
	database, err := opts.resolveDatabase()
	if err != nil {
		return nil, err
	}
	store, err := storesqlite.Open(cmd.Context(), database, storesqlite.ReadOnly)
	if err != nil {
		return nil, err
	}
	if err := store.VerifySchema(cmd.Context()); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := bindResolvedCompanyDatabase(cmd, opts, store); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func openWrite(cmd *cobra.Command, opts *options) (*storesqlite.Store, error) {
	if err := requireCommit(opts, cmd.CommandPath()); err != nil {
		return nil, err
	}
	database, err := opts.resolveDatabase()
	if err != nil {
		return nil, err
	}
	store, err := storesqlite.Open(cmd.Context(), database, storesqlite.ReadWrite)
	if err != nil {
		return nil, err
	}
	if err := store.VerifySchema(cmd.Context()); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := bindResolvedCompanyDatabase(cmd, opts, store); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func bindResolvedCompanyDatabase(cmd *cobra.Command, opts *options, store *storesqlite.Store) error {
	if opts.resolved == nil || strings.TrimSpace(opts.database) != "" {
		return nil
	}
	resolved := *opts.resolved
	var databaseUUID string
	if err := store.DB().QueryRowContext(cmd.Context(), `SELECT database_uuid
		FROM database_metadata WHERE singleton = 1`).Scan(&databaseUUID); err != nil {
		return storesqlite.MapError("read registered company database identity", err)
	}
	var companyMatches int
	if err := store.DB().QueryRowContext(cmd.Context(), `SELECT COUNT(*)
		FROM books book JOIN entities entity ON entity.id = book.entity_id
		WHERE book.code = ? AND entity.code = ?`, resolved.Company.BookCode, resolved.Company.EntityCode).Scan(&companyMatches); err != nil {
		return storesqlite.MapError("verify registered company database identity", err)
	}
	if companyMatches != 1 || (resolved.Company.DatabaseUUID != "" && resolved.Company.DatabaseUUID != databaseUUID) {
		return apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "registered company database identity does not match books.toml")
	}
	if resolved.Company.DatabaseUUID != "" {
		return nil
	}
	_, err := persistResolvedCompanyDatabaseUUID(opts, resolved, databaseUUID)
	return err
}

func persistResolvedCompanyDatabaseUUID(opts *options, resolved booksconfig.ResolvedCompany, databaseUUID string) (booksconfig.ResolvedCompany, error) {
	updated, err := booksconfig.Update(resolved.ConfigPath, nil, func(current *booksconfig.Config, _ bool) error {
		company, ok := current.Companies[resolved.Key]
		if !ok {
			return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is no longer registered", resolved.Key))
		}
		currentResolved, err := current.Resolve(resolved.ConfigPath, resolved.Key)
		if err != nil {
			return apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve company while binding database identity", err)
		}
		if currentResolved.Database != resolved.Database {
			return apperr.New(apperr.Conflict, "COMPANY_CONFIG_CHANGED", "registered company database path changed while its identity was being bound")
		}
		if company.DatabaseUUID != "" && company.DatabaseUUID != databaseUUID {
			return apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "registered company database identity changed while books.toml was being updated")
		}
		company.DatabaseUUID = databaseUUID
		current.Companies[resolved.Key] = company
		return nil
	})
	if err != nil {
		return booksconfig.ResolvedCompany{}, configMutationError("bind registered company database identity", err)
	}
	updatedResolved, err := updated.Resolve(resolved.ConfigPath, resolved.Key)
	if err != nil {
		return booksconfig.ResolvedCompany{}, apperr.Wrap(apperr.Invalid, "COMPANY_CONFIG_INVALID", "resolve identity-bound company", err)
	}
	opts.loadedConfig = &updated
	opts.resolved = &updatedResolved
	return updatedResolved, nil
}

func (opts *options) resolveConfigPath() (string, error) {
	if opts.resolvedConfig != "" {
		return opts.resolvedConfig, nil
	}
	path, err := booksconfig.DefaultPath(opts.configPath)
	if err != nil {
		return "", apperr.Wrap(apperr.Invalid, "CONFIG_PATH_INVALID", "resolve Books configuration path", err)
	}
	opts.resolvedConfig = path
	return path, nil
}

func (opts *options) loadConfig() (booksconfig.Config, string, error) {
	path, err := opts.resolveConfigPath()
	if err != nil {
		return booksconfig.Config{}, "", err
	}
	if opts.loadedConfig != nil {
		return *opts.loadedConfig, path, nil
	}
	value, err := booksconfig.Load(path)
	if err != nil {
		return booksconfig.Config{}, path, apperr.Wrap(apperr.NotFound, "CONFIG_NOT_FOUND", "load Books configuration", err)
	}
	if !opts.formatExplicit && value.Defaults.Output != "" {
		opts.format = value.Defaults.Output
	}
	opts.loadedConfig = &value
	return value, path, nil
}

func (opts *options) resolveCompany() (booksconfig.ResolvedCompany, error) {
	if strings.TrimSpace(opts.database) != "" {
		return booksconfig.ResolvedCompany{}, apperr.New(apperr.Invalid, "DATABASE_OVERRIDE_CONFLICT", "--db and BOOKS_DB cannot be used with registered-company commands; remove the database override or use the advanced direct-database command")
	}
	if opts.resolved != nil {
		return *opts.resolved, nil
	}
	value, path, err := opts.loadConfig()
	if err != nil {
		return booksconfig.ResolvedCompany{}, err
	}
	resolved, err := value.Resolve(path, opts.company)
	if err != nil {
		return booksconfig.ResolvedCompany{}, apperr.Wrap(apperr.Invalid, "COMPANY_NOT_SELECTED", "resolve company", err)
	}
	opts.resolved = &resolved
	return resolved, nil
}

func (opts *options) resolveDatabase() (string, error) {
	if strings.TrimSpace(opts.database) != "" {
		path, err := filepath.Abs(filepath.Clean(opts.database))
		if err != nil {
			return "", apperr.Wrap(apperr.Invalid, "DATABASE_PATH_INVALID", "resolve database path", err)
		}
		return path, nil
	}
	resolved, err := opts.resolveCompany()
	if err != nil {
		return "", err
	}
	return resolved.Database, nil
}

func requireCommit(opts *options, command string) error {
	if opts.dryRun {
		return apperr.New(apperr.Validation, "DRY_RUN_UNSUPPORTED", fmt.Sprintf("%s does not yet support dry-run", command))
	}
	return nil
}
