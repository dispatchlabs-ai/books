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
	base := flag.String("base", "docs/schemas/books-api-v11.openapi.json", "base snapshot")
	out := flag.String("out", "docs/schemas/books-api-v12.openapi.json", "output snapshot")
	flag.Parse()
	data, err := os.ReadFile(*base)
	if err != nil {
		panic(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		panic(err)
	}
	spec["info"].(map[string]any)["version"] = "1.11.0"
	paths := spec["paths"].(map[string]any)
	descriptors := []operations.Descriptor{}
	for _, op := range operations.CompanyOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.DatabaseOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.MaintenanceOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, op := range operations.RegistryOperations() {
		descriptors = append(descriptors, op.Descriptor())
	}
	for _, d := range descriptors {
		scope, plural := "company", "companies"
		if d.Scope == "database" {
			scope, plural = "database", "databases"
		}
		path := "/v1/" + plural + "/{" + scope + "}/operations/" + d.ID
		parameters := []any{map[string]any{"name": scope, "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}
		inputSchema := wire.OperationInputSchema(d.Input)
		if d.Scope == "registry" {
			scope = "registry"
			path = "/v1/admin/registry/operations/" + d.ID
			parameters = []any{}
			inputSchema = wire.Schema(d.Input)
		}
		if d.Grant == "admin" {
			inputSchema = wire.Schema(d.Input)
		}
		paths[path] = map[string]any{"post": map[string]any{"operationId": scope + "_" + d.ID, "summary": d.ID, "description": fmt.Sprintf("Requires scope read and %s grants. Effect: %s. Inputs use exact int64 minor-unit strings; domain decimal amount fields remain strings.", d.Grant, d.Effect), "parameters": parameters, "requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": inputSchema}}}, "responses": map[string]any{"200": map[string]any{"description": "Operation result", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": []string{"schema", "ok", "data"}, "properties": map[string]any{"schema": map[string]any{"const": "books.api/v1"}, "ok": map[string]any{"const": true}, "data": wire.Schema(d.Output)}, "additionalProperties": false}}}}, "default": map[string]any{"description": "Stable Books error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}}}}}
	}
	updateCatalog(descriptors)
	data, err = json.MarshalIndent(spec, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*out, append(data, '\n'), 0644); err != nil {
		panic(err)
	}
}

func updateCatalog(descriptors []operations.Descriptor) {
	catalog := operations.Catalog()
	for i := range catalog {
		op := &catalog[i]
		for _, d := range descriptors {
			if d.ID != op.ID {
				continue
			}
			prefix, path := "books_company_", "/v1/companies/{company}/operations/"+d.ID
			if d.Scope == "database" {
				prefix = "books_db_"
				path = "/v1/databases/{database}/operations/" + d.ID
			}
			if d.Scope == "registry" {
				prefix = "books_registry_"
				path = "/v1/admin/registry/operations/" + d.ID
			}
			name := prefix + d.ID
			found := false
			for _, v := range op.MCP {
				if v == name {
					found = true
				}
			}
			if !found {
				op.MCP = append(op.MCP, name)
			}
			found = false
			for _, v := range op.HTTP {
				if v.Method == "POST" && v.Path == path {
					found = true
				}
			}
			if !found {
				op.HTTP = append(op.HTTP, operations.HTTPBinding{Method: "POST", Path: path})
			}
		}
		if op.ID == "health" || op.ID == "capabilities" {
			op.MCP = []string{"books_" + op.ID}
		}
		if len(op.HTTP) > 0 && len(op.MCP) > 0 {
			op.Gap = ""
		}
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile("internal/operations/catalog.json", append(data, '\n'), 0644); err != nil {
		panic(err)
	}
}
