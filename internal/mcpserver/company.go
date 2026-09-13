package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
	"slices"
	"sort"
	"strings"
)

func (s *Server) registerCompanies(p Policy) {
	for _, op := range operations.CompanyOperations() {
		d := op.Descriptor()
		allowed := []string{}
		for key, grants := range p.Companies {
			ok := true
			for _, grant := range strings.Split(d.Grant, "+") {
				if !slices.Contains(grants, grant) {
					ok = false
				}
			}
			if ok {
				allowed = append(allowed, key)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		sort.Strings(allowed)
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_company_" + d.ID, Description: fmt.Sprintf("%s for a registered company. Requires %s. Amounts use the schema's exact string representation; inspect validation results.", d.ID, d.Grant), InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"company", "input"}, "properties": map[string]any{"company": map[string]any{"type": "string", "enum": allowed}, "input": wire.Schema(d.Input)}}, OutputSchema: map[string]any{"type": "object", "required": []string{"result"}, "properties": map[string]any{"result": wire.Schema(d.Output)}, "additionalProperties": false}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: d.Effect == "read", OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Company string          `json:"company"`
				Input   json.RawMessage `json:"input"`
			}
			if err := application.DecodeRequest(req.Params.Arguments, &args); err != nil {
				return failure(err), nil
			}
			app, ok := s.companies[args.Company]
			if !ok {
				return failure(apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company is not available")), nil
			}
			input := op.NewInput()
			if err := wire.Decode(args.Input, input); err != nil {
				return failure(err), nil
			}
			value, err := op.Invoke(ctx, app, operations.CompanyAccess(p.Actor, args.Company, p.Companies[args.Company]), input)
			if err != nil {
				return failure(err), nil
			}
			result := map[string]any{"result": wire.EncodeValue(reflect.ValueOf(value), true)}
			data, err := json.Marshal(result)
			if err != nil {
				return failure(err), nil
			}
			return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
		})
	}
}
