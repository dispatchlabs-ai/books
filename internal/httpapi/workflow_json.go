package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
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
	// Validate duplicate keys, nesting and trailing content before materializing
	// a map; otherwise JSON decoding would silently discard ambiguous fields.
	var raw json.RawMessage
	if err = application.DecodeRequest(data, &raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tree any
	if err = decoder.Decode(&tree); err != nil {
		return err
	}
	typ := reflect.TypeOf(value)
	converted, err := decodeAPIValue(tree, typ.Elem())
	if err != nil {
		return err
	}
	data, err = json.Marshal(converted)
	if err != nil {
		return err
	}
	return application.DecodeRequest(data, value)
}
func decodeAPIValue(value any, typ reflect.Type) (any, error) {
	bad := func() (any, error) {
		return nil, apperr.New(apperr.Input, "REQUEST_JSON_INVALID", "request fields must match the workflow schema; 64-bit integers must be canonical decimal strings")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if value == nil {
		return nil, nil
	}
	switch typ.Kind() {
	case reflect.Int64:
		text, ok := value.(string)
		if !ok {
			return bad()
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil || strconv.FormatInt(number, 10) != text {
			return bad()
		}
		return json.Number(text), nil
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
		for name, child := range object {
			field, ok := fields[name]
			if !ok {
				return bad()
			}
			converted, err := decodeAPIValue(child, field)
			if err != nil {
				return nil, err
			}
			object[name] = converted
		}
		return object, nil
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return bad()
		}
		for i, child := range items {
			converted, err := decodeAPIValue(child, typ.Elem())
			if err != nil {
				return nil, err
			}
			items[i] = converted
		}
		return items, nil
	default:
		return value, nil
	}
}
