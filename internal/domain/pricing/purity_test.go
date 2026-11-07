package pricing

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPurity keeps the engine pure (INV-21): standard library plus money, qty, calendar and geo; no store, clock,
// I/O, network or randomness; no time.Now. depguard enforces the same in lint.
func TestPurity(t *testing.T) {
	allowed := map[string]bool{
		"github.com/coste360/porto/internal/money":    true,
		"github.com/coste360/porto/internal/qty":      true,
		"github.com/coste360/porto/internal/calendar": true,
		"github.com/coste360/porto/internal/geo":      true,
	}
	forbidden := []string{"os", "net", "net/http", "math/rand", "math/rand/v2", "crypto/rand", "database/sql", "io/ioutil", "syscall", "unsafe"}
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
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q", name, path)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since" || sel.Sel.Name == "Until") {
					t.Errorf("%s uses time.%s", fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}
