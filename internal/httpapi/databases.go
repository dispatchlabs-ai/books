package httpapi

import (
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/artifact"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"net/http"
	"strings"
)

func (s *Server) serveDatabase(w http.ResponseWriter, r *http.Request, p Principal, path []string) error {
	if len(path) != 3 || path[1] != "operations" || r.Method != http.MethodPost {
		return apperr.New(apperr.NotFound, "ROUTE_NOT_FOUND", "API route was not found")
	}
	config, ok := s.config.Databases[path[0]]
	// Fail before decoding input; company grants never imply database authority.
	if !ok || len(p.Databases[path[0]]) == 0 {
		return apperr.New(apperr.NotFound, "DATABASE_NOT_FOUND", "database is not available to this principal")
	}
	target := application.NewDatabaseTarget(path[0], config.Path, config.UUID)
	if op, ok := operations.LookupMaintenanceOperation(path[2]); ok {
		if r.URL.RawQuery != "" {
			return apperr.New(apperr.Invalid, "OPERATION_QUERY_INVALID", "operation parameters belong in the JSON body")
		}
		input := op.NewInput()
		if err := readWorkflowJSON(w, r, input); err != nil {
			return err
		}
		output, err := op.Execute(artifact.WithRoot(r.Context(), s.config.ArtifactDirectory), target, operations.ScopedDatabaseAccess(p.ID, path[0], p.Databases[path[0]]), input)
		if err != nil {
			return err
		}
		writeWorkflowData(w, http.StatusOK, output)
		return nil
	}
	var db *application.Database
	var err error
	if strings.HasPrefix(path[2], "artifact_") {
		db, err = target.OpenArtifactScope(r.Context())
	} else {
		db, err = target.Open(r.Context())
	}
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	op, ok := operations.LookupDatabaseOperation(path[2])
	if !ok {
		return apperr.New(apperr.NotFound, "OPERATION_NOT_FOUND", "operation was not found")
	}
	if r.URL.RawQuery != "" {
		return apperr.New(apperr.Invalid, "OPERATION_QUERY_INVALID", "operation parameters belong in the JSON body")
	}
	input := op.NewInput()
	if err := readOperationJSON(w, r.WithContext(artifact.Bind(artifact.WithRoot(r.Context(), s.config.ArtifactDirectory), p.ID, "database:"+db.Identity())), input); err != nil {
		return err
	}
	output, err := op.Execute(artifact.WithRoot(r.Context(), s.config.ArtifactDirectory), db, operations.ScopedDatabaseAccess(p.ID, path[0], p.Databases[path[0]]), input)
	if err != nil {
		return err
	}
	writeWorkflowData(w, http.StatusOK, output)
	return nil
}
