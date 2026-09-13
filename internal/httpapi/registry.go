package httpapi

import (
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"net/http"
	"strings"
)

func (s *Server) serveRegistry(w http.ResponseWriter, r *http.Request, p Principal) error {
	if len(p.Registry) == 0 {
		return apperr.New(apperr.NotFound, "REGISTRY_NOT_AVAILABLE", "registry is not available to this principal")
	}
	if r.Method != http.MethodPost {
		return apperr.New(apperr.NotFound, "ROUTE_NOT_FOUND", "API route was not found")
	}
	if r.URL.RawQuery != "" {
		return apperr.New(apperr.Invalid, "OPERATION_QUERY_INVALID", "operation parameters belong in the JSON body")
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/admin/registry/operations/")
	op, ok := operations.LookupRegistryOperation(id)
	if !ok {
		return apperr.New(apperr.NotFound, "OPERATION_NOT_FOUND", "operation was not found")
	}
	input := op.NewInput()
	if err := readWorkflowJSON(w, r, input); err != nil {
		return err
	}
	out, err := op.Execute(r.Context(), application.NewRegistry(s.booksConfig), operations.ScopedRegistryAccess(p.ID, p.Registry), input)
	if err != nil {
		return err
	}
	writeWorkflowData(w, http.StatusOK, out)
	return nil
}
