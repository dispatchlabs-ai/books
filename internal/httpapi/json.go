package httpapi

import (
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/money"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
)

type envelope struct {
	Schema string     `json:"schema"`
	OK     bool       `json:"ok"`
	Data   any        `json:"data,omitempty"`
	Error  *errorBody `json:"error,omitempty"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Schema: "books.api/v1", OK: true, Data: apiValue(reflect.ValueOf(data))})
}
func writeFailure(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Schema: "books.api/v1", OK: false, Error: &errorBody{Code: code, Message: message}})
}
func writeError(w http.ResponseWriter, e error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "Books could not complete this operation"
	if a, ok := apperr.As(e); ok {
		code = a.Code
		switch a.Kind {
		case apperr.Input, apperr.Invalid:
			status = http.StatusBadRequest
			message = a.Message
		case apperr.NotFound:
			status = http.StatusNotFound
			message = a.Message
		case apperr.Conflict:
			status = http.StatusConflict
			message = a.Message
		case apperr.Validation:
			status = http.StatusUnprocessableEntity
			message = a.Message
		case apperr.Unavailable:
			status = http.StatusServiceUnavailable
			if a.Code == "ACCOUNT_DEFAULTS_PARTIAL" {
				message = a.Message
			}
		case apperr.Integrity:
			status = http.StatusInternalServerError
		}
	}
	if code == "PERMISSION_DENIED" {
		status = http.StatusForbidden
	}
	writeFailure(w, status, code, message)
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return apperr.New(apperr.Input, "CONTENT_TYPE_INVALID", "Content-Type must be application/json")
	}
	data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if e != nil {
		return apperr.New(apperr.Input, "REQUEST_TOO_LARGE", "JSON request exceeds the limit")
	}
	return application.DecodeRequest(data, v)
}
func serveTransactions(w http.ResponseWriter, r *http.Request, app *application.Service) error {
	after := int64(0)
	limit := 100
	var e error
	if v := r.URL.Query().Get("after"); v != "" {
		after, e = strconv.ParseInt(v, 10, 64)
		if e != nil {
			return apperr.New(apperr.Invalid, "PAGE_INVALID", "after must be an integer cursor")
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil {
			return apperr.New(apperr.Invalid, "PAGE_INVALID", "limit must be an integer")
		}
	}
	items, e := app.Transactions(r.Context(), after, limit)
	if e != nil {
		return e
	}
	next := ""
	if len(items) == limit {
		next = strconv.FormatInt(items[len(items)-1].EntryNumber, 10)
	}
	writeData(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	return nil
}

// Integer minor units and int64 cursors cross JSON as strings, so JavaScript
// clients never round accounting values above Number.MAX_SAFE_INTEGER.
func apiValue(v reflect.Value) any { return encodeAPIValue(v, false) }
func encodeAPIValue(v reflect.Value, preserveNil bool) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return encodeAPIValue(v.Elem(), preserveNil)
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		return v.Interface()
	}
	if v.Type() == reflect.TypeFor[money.Currency]() {
		return v.Interface().(money.Currency).Code()
	}
	switch v.Kind() {
	case reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Struct:
		out := map[string]any{}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := f.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			if strings.Contains(options, "omitempty") && v.Field(i).IsZero() {
				continue
			}
			out[name] = encodeAPIValue(v.Field(i), preserveNil)
		}
		return out
	case reflect.Slice, reflect.Array:
		if preserveNil && v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = encodeAPIValue(v.Index(i), preserveNil)
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		it := v.MapRange()
		for it.Next() {
			out[it.Key().String()] = encodeAPIValue(it.Value(), preserveNil)
		}
		return out
	default:
		return v.Interface()
	}
}

// Company scope is bound before this handler; do not accept alternate entity or
// group selectors that could turn a company credential into database access.
func serveGeneralLedger(w http.ResponseWriter, r *http.Request, app *application.Service) error {
	q := r.URL.Query()
	for key, values := range q {
		switch key {
		case "from", "to", "account", "include_zero":
		default:
			return apperr.New(apperr.Invalid, "REPORT_QUERY_INVALID", "unsupported general-ledger query parameter")
		}
		if len(values) != 1 {
			return apperr.New(apperr.Invalid, "REPORT_QUERY_INVALID", "general-ledger query parameters must occur once")
		}
	}
	zero := false
	if values, ok := q["include_zero"]; ok {
		if values[0] != "true" && values[0] != "false" {
			return apperr.New(apperr.Invalid, "REPORT_QUERY_INVALID", "include_zero must be true or false")
		}
		zero = values[0] == "true"
	}
	result, err := app.GeneralLedger(r.Context(), application.GeneralLedgerRequest{
		From: q.Get("from"), To: q.Get("to"), Account: q.Get("account"), IncludeZero: zero,
	})
	if err != nil {
		return err
	}
	writeData(w, http.StatusOK, result)
	return nil
}
