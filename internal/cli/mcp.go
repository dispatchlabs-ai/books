package cli

import (
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	var policy string
	cmd := &cobra.Command{Use: "mcp", Short: "Serve Books tools over stdio with explicit policy", Args: cobra.NoArgs, PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		for _, name := range []string{"db", "config", "company", "actor", "dry-run", "format", "json"} {
			if cmd.Flags().Changed(name) {
				return apperr.New(apperr.Invalid, "MCP_FLAG_INVALID", "MCP scope and actor come from --policy; output is stdio protocol")
			}
		}
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		if policy == "" {
			return apperr.New(apperr.Invalid, "MCP_POLICY_REQUIRED", "--policy is required")
		}
		p, err := mcpserver.LoadPolicy(policy)
		if err != nil {
			return err
		}
		server, err := mcpserver.New(cmd.Context(), p)
		if err != nil {
			return err
		}
		defer func() { _ = server.Close() }()
		return server.Run(cmd.Context())
	}}
	cmd.Flags().StringVar(&policy, "policy", "", "private MCP access policy path")
	return cmd
}
