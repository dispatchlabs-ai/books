package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dispatchlabs-ai/books/internal/operations"
	"github.com/spf13/cobra"
)

func TestOperationInventoryCoversCLIAndOpenAPI(t *testing.T) {
	root, _ := newRootCommand()
	commands := map[string]bool{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c != root && (c.Run != nil || c.RunE != nil) {
			commands[strings.TrimPrefix(c.CommandPath(), "books ")] = true
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	delete(commands, "serve") // Process lifecycle, not a backend operation.
	_, source, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../docs/schemas/books-api-v7.openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for path, methods := range spec.Paths {
		for method := range methods {
			switch method {
			case "get", "post", "put", "patch", "delete":
				routes[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	ids := map[string]bool{}
	for _, op := range operations.Catalog() {
		if op.ID == "" || ids[op.ID] {
			t.Fatalf("invalid or duplicate operation %q", op.ID)
		}
		ids[op.ID] = true
		if op.Gap == "" {
			t.Fatalf("operation %s must record its current parity gaps", op.ID)
		}
		for _, cmd := range op.CLI {
			if !commands[cmd] {
				t.Errorf("unknown or duplicate CLI binding %s", cmd)
			}
			delete(commands, cmd)
		}
		for _, route := range op.HTTP {
			key := route.Method + " " + route.Path
			if !routes[key] {
				t.Errorf("unknown or duplicate HTTP binding %s", key)
			}
			delete(routes, key)
		}
	}
	for cmd := range commands {
		t.Errorf("CLI operation missing from inventory: %s", cmd)
	}
	for route := range routes {
		t.Errorf("HTTP operation missing from inventory: %s", route)
	}
}
