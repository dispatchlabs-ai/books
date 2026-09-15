package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/operations"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
		companySchema := map[string]any{"type": "string", "enum": allowed}
		if slices.Contains(allowed, "*") {
			delete(companySchema, "enum")
			companySchema["description"] = "Registered company key; operator explicitly granted all companies"
		}
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_company_" + d.ID, Description: toolDescription(d), InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"company", "input"}, "properties": map[string]any{"company": companySchema, "input": wire.OperationInputSchema(d.Input)}}, OutputSchema: resultSchema(d.Output), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: d.Effect == "read", OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Company string          `json:"company"`
				Input   json.RawMessage `json:"input"`
			}
			if err := application.DecodeRequest(req.Params.Arguments, &args); err != nil {
				return failure(err), nil
			}
			grants := operations.CompanyGrants(p.Companies, args.Company)
			if booksconfig.ValidateCompanyKey(args.Company) != nil || !slices.Contains(grants, "read") {
				return failure(apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", "company is not available")), nil
			}

			app, err := application.Open(ctx, p.ConfigPath, args.Company, p.Actor, storesqlite.ReadWrite)
			if err != nil {
				return failure(err), nil
			}
			defer func() { _ = app.Close() }()
			input := op.NewInput()
			if err := wire.DecodeOperation(artifact.Bind(artifact.WithRoot(ctx, p.ArtifactDirectory), p.Actor, "company:"+app.Identity()), args.Input, input); err != nil {
				return failure(err), nil
			}
			value, err := op.Invoke(artifact.WithRoot(ctx, p.ArtifactDirectory), app, operations.CompanyAccess(p.Actor, args.Company, grants), input)
			if err != nil {
				return failure(err), nil
			}
			return operationResult(artifact.Bind(artifact.WithRoot(ctx, p.ArtifactDirectory), p.Actor, "company:"+app.Identity()), value, d.Effect)
		})
	}
}
