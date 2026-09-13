package cli

import (
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/spf13/cobra"
)

// Local commands call the same application services as HTTP, without requiring
// a running server. CLI syntax and rendering stay at this boundary.
func openApplication(cmd *cobra.Command, opts *options, mode storesqlite.Mode) (*application.Service, error) {
	if opts.database != "" {
		return nil, apperr.New(apperr.Invalid, "COMPANY_REQUIRED", "client workflows require a registered company rather than --db")
	}
	if _, err := opts.resolveCompany(); err != nil {
		return nil, err
	}
	opener := openRead
	if mode == storesqlite.ReadWrite {
		opener = openWrite
	}
	store, err := opener(cmd, opts)
	if err != nil {
		return nil, err
	}
	resolved, err := opts.resolveCompany()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	app, err := application.Bind(cmd.Context(), store, resolved, opts.actor)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return app, nil
}

func openWorkflow(cmd *cobra.Command, opts *options, write bool) (*application.Service, error) {
	if _, err := opts.resolveCompany(); err != nil {
		return nil, err
	}
	mode := storesqlite.ReadOnly
	if write && !opts.dryRun {
		mode = storesqlite.ReadWrite
	}
	return openApplication(cmd, opts, mode)
}
