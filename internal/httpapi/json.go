package httpapi

import (
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"io"
	"net/http"
	"reflect"
	"strconv"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/operations"
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
func apiValue(v reflect.Value) any                         { return encodeAPIValue(v, false) }
func encodeAPIValue(v reflect.Value, preserveNil bool) any { return wire.EncodeValue(v, preserveNil) }

// Company scope is bound before this handler; do not accept alternate entity or
// group selectors that could turn a company credential into database access.
func serveGeneralLedger(w http.ResponseWriter, r *http.Request, app *application.Service, access operations.Access) error {
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
	result, err := operations.GeneralLedger().Execute(r.Context(), app, access, application.GeneralLedgerRequest{
		From: q.Get("from"), To: q.Get("to"), Account: q.Get("account"), IncludeZero: zero,
	})
	if err != nil {
		return err
	}
	writeData(w, http.StatusOK, result)
	return nil
}

func serveCompanyReport(w http.ResponseWriter, r *http.Request, app *application.Service, access operations.Access, kind string) error {
	q := r.URL.Query()
	zero := false
	if values, ok := q["include_zero"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			return apperr.New(apperr.Invalid, "REPORT_QUERY_INVALID", "include_zero must occur once and be true or false")
		}
		zero = values[0] == "true"
	}
	var result any
	var err error
	switch kind {
	case "trial-balance":
		result, err = operations.TrialBalance().Execute(r.Context(), app, access, application.AsOfReportRequest{AsOf: q.Get("as_of"), IncludeZero: zero})
	case "balance-sheet":
		result, err = operations.BalanceSheet().Execute(r.Context(), app, access, application.AsOfReportRequest{AsOf: q.Get("as_of"), IncludeZero: zero})
	case "profit-loss":
		result, err = operations.ProfitLoss().Execute(r.Context(), app, access, application.RangeReportRequest{From: q.Get("from"), To: q.Get("to"), IncludeZero: zero})
	default:
		return apperr.New(apperr.NotFound, "REPORT_NOT_FOUND", "report is not available")
	}
	if err != nil {
		return err
	}
	writeData(w, http.StatusOK, result)
	return nil
}
