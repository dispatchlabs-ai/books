// Package wire defines lossless, bounded JSON contracts shared by remote adapters.
package wire

import (
	"bytes"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/money"
	"reflect"
	"strconv"
	"strings"
)

func Decode(data []byte, value any) error {
	var err error
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
	converted, err := decodeValue(tree, typ.Elem())
	if err != nil {
		return err
	}
	data, err = json.Marshal(converted)
	if err != nil {
		return err
	}
	return application.DecodeRequest(data, value)
}
func decodeValue(value any, typ reflect.Type) (any, error) {
	bad := func() (any, error) {
		return nil, apperr.New(apperr.Input, "REQUEST_JSON_INVALID", "request fields must match the workflow schema; 64-bit integers must be canonical decimal strings")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		return value, nil
	}
	if typ == reflect.TypeFor[money.Currency]() {
		return value, nil
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
			converted, err := decodeValue(child, field)
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
			converted, err := decodeValue(child, typ.Elem())
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

func EncodeValue(v reflect.Value, preserveNil bool) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return EncodeValue(v.Elem(), preserveNil)
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
			out[name] = EncodeValue(v.Field(i), preserveNil)
		}
		return out
	case reflect.Slice, reflect.Array:
		if preserveNil && v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = EncodeValue(v.Index(i), preserveNil)
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		it := v.MapRange()
		for it.Next() {
			out[it.Key().String()] = EncodeValue(it.Value(), preserveNil)
		}
		return out
	default:
		return v.Interface()
	}
}
