package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/wire"
)

func writeWorkflowData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Saved-plan digests distinguish null from empty arrays. Preserve that
	// representation when serializing a plan for round-trip review and apply.
	_ = json.NewEncoder(w).Encode(envelope{Schema: "books.api/v1", OK: true, Data: encodeAPIValue(reflect.ValueOf(data), true)})
}
func readWorkflowJSON(w http.ResponseWriter, r *http.Request, value any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return apperr.New(apperr.Input, "CONTENT_TYPE_INVALID", "Content-Type must be application/json")
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return apperr.New(apperr.Input, "REQUEST_TOO_LARGE", "JSON request exceeds the limit")
	}
	return wire.Decode(data, value)
}

func readOperationJSON(w http.ResponseWriter, r *http.Request, value any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return apperr.New(apperr.Input, "CONTENT_TYPE_INVALID", "Content-Type must be application/json")
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return apperr.New(apperr.Input, "REQUEST_TOO_LARGE", "JSON request exceeds the limit")
	}
	return wire.DecodeOperation(r.Context(), data, value)
}
