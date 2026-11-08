package formula

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/qty"
)

var fuzzSeeds = []string{
	"loa * (beam + draft) * 0.85",
	"round(hull_area * 0.15, 1)",
	"ceil(hull_area * layers / 25)",
	"if(hull_type == \"catamaran\", list_price * 1.5, list_price)",
	"band(loa, lift) + min(1, 2, 3)",
	"not keel or loa >= 12 and beam < 5",
	"1 / 0",
	"floor(-3.5) - ceil(2.001)",
	"((((1))))",
	"max(loa, beam, draft) / 3 * 3",
	"",
	"\"monohull_sail\" == hull_type",
}

// FuzzParse: parsing never panics, is deterministic, and a parsed program's checks never panic (INV-29).
func FuzzParse(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		p1, err1 := Parse(src)
		p2, err2 := Parse(src)
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non deterministic parse of %q", src)
		}
		if err1 != nil {
			var se *SyntaxError
			if !errors.As(err1, &se) {
				t.Fatalf("parse error of %q is %T", src, err1)
			}
			return
		}
		if p1.Source() != p2.Source() || len(p1.Idents()) != len(p2.Idents()) {
			t.Fatalf("non deterministic program of %q", src)
		}
		_ = p1.Check(testDecls(), testTables(), []string{VarQty, VarListPrice})
		if len(p1.Idents()) > MaxIdents {
			t.Fatalf("%q: too many identifiers accepted", src)
		}
	})
}

// FuzzEval: evaluation never panics, gives the same result twice, and a program that checks clean never fails
// with type_mismatch at evaluation (INV-29).
func FuzzEval(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s, int64(12000), int64(2000))
	}
	f.Fuzz(func(t *testing.T, src string, loa, layers int64) {
		p, err := Parse(src)
		if err != nil {
			return
		}
		env := testEnv()
		env.Vars["loa"] = Number(qty.Milli(loa))
		env.Vars["layers"] = Number(qty.Milli(layers))
		v1, e1 := p.Eval(env)
		v2, e2 := p.Eval(env)
		if v1 != v2 || (e1 == nil) != (e2 == nil) || (e1 != nil && e1.Error() != e2.Error()) {
			t.Fatalf("non deterministic eval of %q", src)
		}
		if e1 != nil {
			var ee *EvalError
			if !errors.As(e1, &ee) {
				t.Fatalf("eval error of %q is %T", src, e1)
			}
			if len(p.Check(testDecls(), testTables(), []string{VarQty, VarListPrice})) == 0 && ee.Code == CodeTypeMismatch {
				t.Fatalf("%q checks clean but fails with type_mismatch", src)
			}
		}
	})
}
