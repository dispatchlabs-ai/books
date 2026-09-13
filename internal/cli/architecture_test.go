package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This boundary is the reason local and remote frontends can share workflows.
// Transport packages may parse/render, but cannot gain their own SQL policy.
func TestFrontendOwnershipBoundary(t *testing.T) {
	for _, dir := range []string{".", "../httpapi", "../application", "../ledger"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				if (dir == "." || dir == "../httpapi") && name == "database/sql" {
					t.Errorf("SQL transport dependency in %s", path)
				}
				if (dir == "../application" || dir == "../ledger") && (name == "github.com/spf13/cobra" || name == "net/http" || strings.HasSuffix(name, "/internal/cli") || strings.HasSuffix(name, "/internal/httpapi")) {
					t.Errorf("domain transport dependency %s in %s", name, path)
				}
			}
			if dir != "." && dir != "../httpapi" {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if receiver, ok := sel.X.(*ast.SelectorExpr); ok && receiver.Sel.Name == "URL" && sel.Sel.Name == "Query" {
					return true
				}
				switch sel.Sel.Name {
				case "QueryContext", "QueryRowContext", "ExecContext", "QueryRow", "Query", "Exec":
					t.Errorf("SQL execution in %s: %s", path, sel.Sel.Name)
				}
				return true
			})
		}
	}
}
