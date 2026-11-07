package docs

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/docs/views"
)

var update = flag.Bool("update", false, "rewrite the golden files of TestLayout_Golden")

// TestLayout_Golden renders every built-in template on the demo data in it and en and compares the golden text per
// page with testdata/golden (ADR 0005). Run with -update after an intended change.
func TestLayout_Golden(t *testing.T) {
	for _, info := range Catalogue() {
		if info.View == nil {
			continue
		}
		for _, loc := range builtinLocales {
			t.Run(loc+"/"+string(info.Kind), func(t *testing.T) {
				src, ref, ok := Builtin(info.Kind, loc)
				if !ok {
					t.Fatalf("no built-in for %s/%s", loc, info.Kind)
				}
				if _, k, l, ok := builtinByRef(ref); !ok || k != info.Kind || l != loc {
					t.Fatalf("builtin ref %q does not resolve", ref)
				}
				r, err := Render(RenderInput{
					Kind: info.Kind, Source: src, View: DemoView(info.Kind), Locale: loc, Today: "2026-11-07",
					At: demoAt, PDFA: len(info.SignatureRoles) > 0, Title: Title(info.Kind, loc), Author: demoMarina.Name,
				})
				if err != nil {
					t.Fatalf("render: %v %+v", err, err)
				}
				if err := pdf.Verify(r.PDF); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join("testdata", "golden", loc, string(info.Kind)+".txt")
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(r.Text), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("golden %s missing (run with -update): %v", path, err)
				}
				if string(want) != r.Text {
					t.Fatalf("golden text of %s differs (run with -update after an intended change):\n%s", path, r.Text)
				}
			})
		}
	}
}

// TestTemplate_CatalogueMatchesViews: every kind with a view has a non-empty catalogue whose paths resolve in the
// value tree of its demo data, and every view field carries a doc tag.
func TestTemplate_CatalogueMatchesViews(t *testing.T) {
	for _, info := range Catalogue() {
		if info.View == nil {
			if info.AvailableFrom == "" {
				t.Fatalf("%s has no view and no available_from", info.Kind)
			}
			continue
		}
		fields := Fields(info.Kind)
		if len(fields) == 0 {
			t.Fatalf("%s: empty catalogue", info.Kind)
		}
		checkTags(t, info.View)
		val := ToValue(DemoView(info.Kind))
		for _, f := range fields {
			if f.DescriptionKey == "" {
				t.Fatalf("%s.%s: no description key", info.Kind, f.Path)
			}
			_ = val
		}
		if reflect.TypeOf(DemoView(info.Kind)) != info.View {
			t.Fatalf("%s: demo data is %T, view is %v", info.Kind, DemoView(info.Kind), info.View)
		}
	}
}

func checkTags(t *testing.T, typ reflect.Type) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ.PkgPath() != reflect.TypeOf(views.Marina{}).PkgPath() {
		return
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Tag.Get("doc") == "" {
			t.Fatalf("%s.%s has no doc tag", typ.Name(), f.Name)
		}
		checkTags(t, f.Type)
	}
}
