package harbor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestHarbor_SingleWritePath enforces the single write path of the map elements (INV-04, m1-plan §3.3): in the
// whole repository, store.SaveMapElements is called only by harbor.(*Service).applyDocument.
func TestHarbor_SingleWritePath(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	calls := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "web", "e2e", "tmp", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				}
				if name != "SaveMapElements" {
					return true
				}
				calls++
				inHarbor := filepath.ToSlash(filepath.Dir(path)) == filepath.ToSlash(filepath.Join(root, "internal", "domain", "harbor"))
				if !inHarbor || fn.Name.Name != "applyDocument" || fn.Recv == nil {
					t.Errorf("%s: %s calls SaveMapElements; only harbor.(*Service).applyDocument may", fset.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("SaveMapElements is called %d times, want exactly once (in applyDocument)", calls)
	}
}
