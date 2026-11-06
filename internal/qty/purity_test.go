package qty

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPurity checks that the package stays pure (AGENTS.md): standard library only plus internal/money,
// no clock, no randomness, no I/O, and no time.Now. Lint enforces the same rules; this test keeps them in
// `go test`.
func TestPurity(t *testing.T) {
	allowedInternal := map[string]bool{"github.com/coste360/porto/internal/money": true}
	forbidden := []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "os", "net", "net/http", "database/sql"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(path, ".") && !allowedInternal[path] {
				t.Errorf("%s imports non-standard package %q", name, path)
			}
			for _, bad := range forbidden {
				if path == bad {
					t.Errorf("%s imports %q", name, path)
				}
			}
		}
		full, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(full, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && sel.Sel.Name == "Now" {
					t.Errorf("%s calls time.Now", fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}
}
