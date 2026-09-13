// Command contractgen appends the backend's database contracts to an OpenAPI
// snapshot. It never reads a Books database or user configuration.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/dispatchlabs-ai/books/internal/wire"
	"os"
)

func main() {
	base := flag.String("base", "docs/schemas/books-api-v7.openapi.json", "base snapshot")
	out := flag.String("out", "docs/schemas/books-api-v8.openapi.json", "output snapshot")
	flag.Parse()
	data, err := os.ReadFile(*base)
	if err != nil {
		panic(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		panic(err)
	}
	spec["info"].(map[string]any)["version"] = "1.7.0"
	paths := spec["paths"].(map[string]any)
	for _, op := range operations.DatabaseOperations() {
		d := op.Descriptor()
		path := "/v1/databases/{database}/operations/" + d.ID
		paths[path] = map[string]any{"post": map[string]any{"operationId": "database_" + d.ID, "summary": d.ID, "description": fmt.Sprintf("Requires explicit whole-database read and %s grants. Effect: %s. Inputs use exact int64 minor-unit strings. Company grants do not apply.", d.Grant, d.Effect), "parameters": []any{map[string]any{"name": "database", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}, "requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": wire.Schema(d.Input)}}}, "responses": map[string]any{"200": map[string]any{"description": "Operation result", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": []string{"schema", "ok", "data"}, "properties": map[string]any{"schema": map[string]any{"const": "books.api/v1"}, "ok": map[string]any{"const": true}, "data": wire.Schema(d.Output)}, "additionalProperties": false}}}}, "default": map[string]any{"description": "Stable Books error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}}}}}
	}
	data, err = json.MarshalIndent(spec, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*out, append(data, '\n'), 0644); err != nil {
		panic(err)
	}
}
