// Package mcpserver exposes the shared Books operations over MCP stdio only.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
	"github.com/dispatchlabs-ai/books/internal/version"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DatabasePolicy struct {
	Path   string   `json:"path"`
	UUID   string   `json:"uuid"`
	Grants []string `json:"grants"`
}
type Policy struct {
	ArtifactDirectory string                    `json:"artifact_directory,omitempty"`
	ConfigPath        string                    `json:"config_path,omitempty"`
	Companies         map[string][]string       `json:"companies,omitempty"`
	Schema            string                    `json:"schema"`
	Actor             string                    `json:"actor"`
	Databases         map[string]DatabasePolicy `json:"databases"`
}

func LoadPolicy(path string) (Policy, error) {
	var p Policy
	f, err := os.Open(path)
	if err != nil {
		return p, apperr.New(apperr.Input, "MCP_POLICY_UNAVAILABLE", "MCP policy could not be opened")
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return p, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return p, apperr.New(apperr.Input, "MCP_POLICY_INVALID", "MCP policy must be a private regular file no larger than 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return p, err
	}
	if err = application.DecodeRequest(data, &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}
func (p Policy) Validate() error {
	bad := func() error {
		return apperr.New(apperr.Invalid, "MCP_POLICY_INVALID", "explicit actor, database identities and read/manage grants are required")
	}
	simple := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if p.ArtifactDirectory != "" && !filepath.IsAbs(p.ArtifactDirectory) {
		return bad()
	}
	if p.Schema != "books.mcp-policy/v1" || !simple.MatchString(p.Actor) || (len(p.Databases) == 0 && len(p.Companies) == 0) {
		return bad()
	}
	if len(p.Companies) > 0 && !filepath.IsAbs(p.ConfigPath) {
		return bad()
	}
	for key, grants := range p.Companies {
		if booksconfig.ValidateCompanyKey(key) != nil {
			return bad()
		}
		seen := map[string]bool{}
		for _, grant := range grants {
			if seen[grant] || (grant != "read" && grant != "post" && grant != "import" && grant != "manage") {
				return bad()
			}
			seen[grant] = true
		}
		if !seen["read"] {
			return bad()
		}
	}
	for key, db := range p.Databases {
		if !simple.MatchString(key) || !filepath.IsAbs(db.Path) || !uuid.MatchString(db.UUID) {
			return bad()
		}
		seen := map[string]bool{}
		for _, grant := range db.Grants {
			if seen[grant] || (grant != "read" && grant != "manage") {
				return bad()
			}
			seen[grant] = true
		}
		if !seen["read"] {
			return bad()
		}
	}
	return nil
}

type Server struct {
	companies map[string]*application.Service
	MCP       *mcp.Server
	databases map[string]*application.Database
}

func New(ctx context.Context, policy Policy) (*Server, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	// Freeze the caller-owned policy; later map/slice edits cannot increase grants.
	copyData, _ := json.Marshal(policy)
	var p Policy
	_ = json.Unmarshal(copyData, &p)
	s := &Server{MCP: mcp.NewServer(&mcp.Implementation{Name: "books", Version: version.Identifier()}, nil), databases: map[string]*application.Database{}, companies: map[string]*application.Service{}}
	for key := range p.Companies {
		app, err := application.Open(ctx, p.ConfigPath, key, p.Actor, storesqlite.ReadWrite)
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		s.companies[key] = app
	}
	s.registerCompanies(p)
	for key, config := range p.Databases {
		db, err := application.OpenDatabase(ctx, key, config.Path, config.UUID)
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		s.databases[key] = db
	}
	for _, op := range operations.DatabaseOperations() {
		descriptor := op.Descriptor()
		allowed := []string{}
		for key, db := range p.Databases {
			if slices.Contains(db.Grants, descriptor.Grant) {
				allowed = append(allowed, key)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		sort.Strings(allowed)
		closed := false
		s.MCP.AddTool(&mcp.Tool{Name: "books_db_" + descriptor.ID, Description: fmt.Sprintf("%s on an explicitly authorized whole database. Requires %s; effect %s. Amounts are exact minor-unit strings. Check validation fields and errors in the result.", descriptor.ID, descriptor.Grant, descriptor.Effect), InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"database", "input"}, "properties": map[string]any{"database": map[string]any{"type": "string", "enum": allowed}, "input": wire.Schema(descriptor.Input)}}, OutputSchema: resultSchema(descriptor.Output), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: descriptor.Effect == "read", OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			value, err := op.Execute(artifact.WithRoot(ctx, p.ArtifactDirectory), s.databases[args.Database], operations.ScopedDatabaseAccess(p.Actor, args.Database, config.Grants), input)
			if err != nil {
				return failure(err), nil
			}
			return operationResult(artifact.Bind(artifact.WithRoot(ctx, p.ArtifactDirectory), p.Actor, "database:"+s.databases[args.Database].Identity()), value, descriptor.Effect)
		})
	}
	return s, nil
}
func failure(err error) *mcp.CallToolResult {
	code, message := "OPERATION_FAILED", "Books operation failed"
	if e, ok := apperr.As(err); ok {
		code, message = e.Code, e.Message
	}
	data, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}
func (s *Server) Close() error {
	var errs []error
	for _, app := range s.companies {
		errs = append(errs, app.Close())
	}
	for _, db := range s.databases {
		errs = append(errs, db.Close())
	}
	return errors.Join(errs...)
}
