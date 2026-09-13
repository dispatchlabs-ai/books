// Package operations defines typed, authorized backend bindings and records
// CLI, HTTP and MCP coverage. The inventory itself never grants authority.
package operations

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog.json
var catalogJSON []byte

type HTTPBinding struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Operation struct {
	ID   string        `json:"id"`
	CLI  []string      `json:"cli"`
	HTTP []HTTPBinding `json:"http"`
	MCP  []string      `json:"mcp"`
	Gap  string        `json:"gap"`
}

// Catalog returns an independent copy. A listed HTTP route means a binding
// exists; it does not assert complete scope/argument parity with CLI aliases.
func Catalog() []Operation {
	var result []Operation
	if err := json.Unmarshal(catalogJSON, &result); err != nil {
		panic(err)
	}
	return result
}
