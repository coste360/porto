package formula_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestFormula_Purity keeps the language pure (INV-29): standard library plus money and qty; no store, clock,
// I/O, network, randomness or float.
func TestFormula_Purity(t *testing.T) {
	allowed := map[string]bool{
		"github.com/coste360/porto/internal/money": true,
		"github.com/coste360/porto/internal/qty":   true,
	}
	forbidden := []string{"os", "net", "math/rand", "math/rand/v2", "crypto/rand", "database/sql", "io", "syscall", "unsafe", "time", "math"}
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
			if id, ok := n.(*ast.Ident); ok && (id.Name == "float64" || id.Name == "float32") {
				t.Errorf("%s uses %s", fset.Position(id.Pos()), id.Name)
			}
			return true
		})
	}
}
