package mcpserver

import (
	"context"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerMetadata() {
	for _, id := range []string{"health", "capabilities"} {
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_" + id, Description: metadataDescriptions[id], InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct{}
			if err := application.DecodeRequest(req.Params.Arguments, &args); err != nil {
				return failure(err), nil
			}
			if id == "health" {
				return operationResult(ctx, map[string]string{"status": "ready"}, "read")
			}
			return operationResult(ctx, application.Capabilities(), "read")
		})
	}
}
