package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/money"
	"github.com/dispatchlabs-ai/books/internal/version"

	"github.com/spf13/cobra"
)

type envelope struct {
	Schema   string      `json:"schema"`
	OK       bool        `json:"ok"`
	Data     any         `json:"data,omitempty"`
	Error    *errorBody  `json:"error,omitempty"`
	Warnings []string    `json:"warnings"`
	Meta     interface{} `json:"meta"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func metadata() map[string]any {
	return map[string]any{
		"app_version":  version.Identifier(),
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func writeJSON(writer io.Writer, data any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope{Schema: "books.cli/v1", OK: true, Data: machineValue(reflect.ValueOf(data), ""), Warnings: []string{}, Meta: metadata()})
}

func writeJSONL(writer io.Writer, data any) error {
	value := reflect.ValueOf(data)
	encoder := json.NewEncoder(writer)
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		for i := 0; i < value.Len(); i++ {
			if err := encoder.Encode(machineValue(value.Index(i), "")); err != nil {
				return err
			}
		}
		return nil
	}
	return encoder.Encode(machineValue(value, ""))
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func machineValue(value reflect.Value, jsonName string) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return machineValue(value.Elem(), jsonName)
	}
	if value.Type() == rawMessageType {
		return value.Interface()
	}
	if isMoneyName(jsonName) && value.Kind() >= reflect.Int && value.Kind() <= reflect.Int64 {
		return money.Format(value.Int())
	}
	switch value.Kind() {
	case reflect.Struct:
		result := map[string]any{}
		typeInfo := value.Type()
		for i := 0; i < value.NumField(); i++ {
			fieldInfo := typeInfo.Field(i)
			if fieldInfo.PkgPath != "" {
				continue
			}
			name := fieldInfo.Name
			tag := fieldInfo.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if tag != "" {
				if comma := indexComma(tag); comma >= 0 {
					name = tag[:comma]
				} else {
					name = tag
				}
				if name == "" {
					name = fieldInfo.Name
				}
			}
			result[name] = machineValue(value.Field(i), name)
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for i := 0; i < value.Len(); i++ {
			result[i] = machineValue(value.Index(i), jsonName)
		}
		return result
	case reflect.Map:
		result := map[string]any{}
		iterator := value.MapRange()
		for iterator.Next() {
			key := fmt.Sprint(iterator.Key().Interface())
			result[key] = machineValue(iterator.Value(), key)
		}
		return result
	default:
		return value.Interface()
	}
}

func indexComma(value string) int {
	for i, r := range value {
		if r == ',' {
			return i
		}
	}
	return -1
}

func isMoneyName(name string) bool {
	return name == "cents" || (len(name) > 6 && name[len(name)-6:] == "_cents")
}

func writeError(writer io.Writer, err error) error {
	body := &errorBody{Code: "INTERNAL_ERROR", Message: err.Error()}
	if appError, ok := apperr.As(err); ok {
		body.Code = appError.Code
		body.Message = appError.Message
		body.Hint = appError.Hint
	}
	return json.NewEncoder(writer).Encode(envelope{Schema: "books.cli/v1", OK: false, Error: body, Warnings: []string{}, Meta: metadata()})
}

func writeResult(cmd *cobra.Command, format string, data any, headers []string, rows [][]string) error {
	switch format {
	case "json":
		return writeJSON(cmd.OutOrStdout(), data)
	case "jsonl":
		return writeJSONL(cmd.OutOrStdout(), data)
	case "csv":
		writer := csv.NewWriter(cmd.OutOrStdout())
		if len(headers) > 0 {
			if err := writer.Write(headers); err != nil {
				return err
			}
		}
		if err := writer.WriteAll(rows); err != nil {
			return err
		}
		writer.Flush()
		return writer.Error()
	case "table":
		writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if len(headers) > 0 {
			if _, err := fmt.Fprintln(writer, joinTabs(headers)); err != nil {
				return err
			}
		}
		for _, row := range rows {
			if _, err := fmt.Fprintln(writer, joinTabs(row)); err != nil {
				return err
			}
		}
		return writer.Flush()
	default:
		return apperr.New(apperr.Invalid, "FORMAT_INVALID", "invalid output format")
	}
}

func joinTabs(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += "\t"
		}
		result += value
	}
	return result
}
