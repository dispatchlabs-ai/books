package wire

import (
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/money"
	"reflect"
	"strings"
)

// Schema mirrors the shared JSON representation. Input omissions are allowed
// because the domain validates zero values and operation-specific requirements.
func Schema(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[json.RawMessage]() {
		return map[string]any{}
	}
	if t == reflect.TypeFor[money.Currency]() {
		return map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{Schema(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int64:
		return map[string]any{"type": "string", "pattern": "^(0|-?[1-9][0-9]*)$", "description": "Exact signed int64; monetary amounts use owning currency minor units."}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": []string{"array", "null"}, "items": Schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": Schema(t.Elem())}
	case reflect.Struct:
		properties := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			properties[name] = Schema(f.Type)
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	default:
		return map[string]any{}
	}
}
