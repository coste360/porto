package layout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPurity checks the package stays pure (INV-21): standard library plus internal/geo and internal/qty, no
// clock, no randomness, no I/O, no time.Now. Lint enforces the same rules.
func TestPurity(t *testing.T) {
	allowed := map[string]bool{"github.com/coste360/porto/internal/geo": true, "github.com/coste360/porto/internal/qty": true}
	forbidden := []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "os", "net", "net/http", "database/sql", "io/fs"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(path, ".") && !allowed[path] {
				t.Errorf("%s imports %q", name, path)
			}
			for _, bad := range forbidden {
				if path == bad {
					t.Errorf("%s imports %q", name, path)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && sel.Sel.Name == "Now" {
					t.Errorf("%s calls time.Now", fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}
}
