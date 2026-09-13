package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
	"sort"
)

func (s *Server) registerMaintenance(p Policy) {
	for _, op := range operations.MaintenanceOperations() {
		d := op.Descriptor()
		allowed := []string{}
		for key, db := range p.Databases {
			if slices.Contains(db.Grants, "admin") {
				allowed = append(allowed, key)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		sort.Strings(allowed)
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_db_" + d.ID, Description: "Administer an explicitly configured whole database. Restore requires exact handle confirmation. Close other active Books connections before maintenance.", InputSchema: map[string]any{"type": "object", "required": []string{"database", "input"}, "additionalProperties": false, "properties": map[string]any{"database": map[string]any{"type": "string", "enum": allowed}, "input": wire.Schema(d.Input)}}, OutputSchema: resultSchema(d.Output), Annotations: &mcp.ToolAnnotations{OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Database string          `json:"database"`
				Input    json.RawMessage `json:"input"`
			}
			if err := application.DecodeRequest(req.Params.Arguments, &args); err != nil {
				return failure(err), nil
			}
			config, ok := p.Databases[args.Database]
			if !ok {
				return failure(apperr.New(apperr.NotFound, "DATABASE_NOT_FOUND", "database is not available")), nil
			}
			input := op.NewInput()
			if err := wire.Decode(args.Input, input); err != nil {
				return failure(err), nil
			}
			target := application.NewDatabaseTarget(args.Database, config.Path, config.UUID)
			ctx = artifact.WithRoot(ctx, p.ArtifactDirectory)
			value, err := op.Execute(ctx, target, operations.ScopedDatabaseAccess(p.Actor, args.Database, config.Grants), input)
			if err != nil {
				return failure(err), nil
			}
			identity, err := target.ArtifactIdentity(ctx)
			if err != nil {
				return failure(err), nil
			}
			return operationResult(artifact.Bind(ctx, p.Actor, "database:"+identity), value, d.Effect)
		})
	}
}
