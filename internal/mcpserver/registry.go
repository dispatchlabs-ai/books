package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
)

func (s *Server) registerRegistry(p Policy) {
	for _, op := range operations.RegistryOperations() {
		d := op.Descriptor()
		if !slices.Contains(p.Registry, d.Grant) {
			continue
		}
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_registry_" + d.ID, Description: "Operate the explicitly authorized company registry. Company creation uses operator-owned storage. Registry access does not grant posting authority.", InputSchema: map[string]any{"type": "object", "required": []string{"input"}, "additionalProperties": false, "properties": map[string]any{"input": wire.Schema(d.Input)}}, OutputSchema: resultSchema(d.Output), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: d.Effect == "read", OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Input json.RawMessage `json:"input"`
			}
			if err := application.DecodeRequest(req.Params.Arguments, &args); err != nil {
				return failure(err), nil
			}
			input := op.NewInput()
			if err := wire.Decode(args.Input, input); err != nil {
				return failure(err), nil
			}
			out, err := op.Execute(ctx, application.NewRegistry(p.ConfigPath), operations.ScopedRegistryAccess(p.Actor, p.Registry), input)
			if err != nil {
				return failure(err), nil
			}
			return inlineOperationResult(out)
		})
	}
}
