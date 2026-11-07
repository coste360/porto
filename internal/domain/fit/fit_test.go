package fit_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os" //nolint:depguard // the test reads the shared parity vectors of testdata/fit (not the engine)
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/fit/internal/vectors"
	"github.com/coste360/porto/internal/geo"
)

// The vectors are shared with web/shared/src/map/fit.parity.test.ts (m3-plan §2.1).
var vectorFile = filepath.Join("..", "..", "..", "testdata", "fit", "cases.json")

func TestFit_Scenario(t *testing.T) {
	hand := vectors.Handwritten()
	if len(hand) < 60 {
		t.Fatalf("only %d hand-written cases", len(hand))
	}
	seen := map[string]bool{}
	for _, c := range hand {
		t.Run(c.Name, func(t *testing.T) {
			got := vectors.Run(c)
			if !reflect.DeepEqual(got, c.Expect) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(c.Expect)
				t.Fatalf("got %s, want %s", gb, wb)
			}
		})
		for _, code := range append(append([]string{}, c.Expect.Blocking...), c.Expect.Warnings...) {
			seen[code] = true
		}
	}
	for _, code := range append(fit.BlockingCodes(), fit.WarningCodes()...) {
		if !seen[code] {
			t.Errorf("no hand case covers %s", code)
		}
	}
}

func TestFit_LimitsAndOverride(t *testing.T) {
	draft := geo.Cm(290)
	depth := geo.Cm(300)
	res := fit.Check(fit.Vessel{LOA: 1300, Beam: 420, Draft: &draft, Hull: fit.HullCatamaran},
		fit.Target{Kind: fit.TargetBerth, MaxLOA: 1200, MaxBeam: 400, Length: 1200, Width: 450, Depth: &depth},
		calendar.MustParse("2027-07-01"), fit.Rules{LOATolerance: 50, SafetyClearance: 30, DepthWarnMargin: 10, WasteThresholdBP: 6000})
	if res.OK || !res.Overridable {
		t.Fatalf("ok %v overridable %v", res.OK, res.Overridable)
	}
	want := []fit.Issue{
		{Code: fit.CodeBeamExceeds, Limit: 400, Actual: 420},
		{Code: fit.CodeDraftExceeds, Limit: 300, Actual: 320},
		{Code: fit.CodeLOAExceeds, Limit: 1250, Actual: 1300},
		{Code: fit.CodeMultihullNotAllowed},
	}
	if !reflect.DeepEqual(res.Blocking, want) {
		t.Fatalf("blocking %+v", res.Blocking)
	}
	inv := fit.Check(fit.Vessel{LOA: 0, Beam: 300}, fit.Target{Kind: fit.TargetBerth}, calendar.MustParse("2027-07-01"), fit.Rules{})
	if inv.OK || inv.Overridable {
		t.Fatalf("vessel_invalid must not be overridable: %+v", inv)
	}
}

func readVectors(t *testing.T) vectors.File {
	t.Helper()
	b, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Fatal(err)
	}
	var f vectors.File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFit_ParityVectors(t *testing.T) {
	f := readVectors(t)
	if f.Version != 1 || len(f.Cases) < 360 {
		t.Fatalf("version %d, %d cases", f.Version, len(f.Cases))
	}
	for _, c := range f.Cases {
		got := vectors.Run(c)
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(c.Expect)
		if !bytes.Equal(gb, wb) {
			t.Errorf("%s: got %s, want %s", c.Name, gb, wb)
		}
	}
}

func TestFit_VectorsUpToDate(t *testing.T) {
	want, err := vectors.Encode(vectors.Cases(1, 300))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("testdata/fit/cases.json is stale: run go run ./internal/domain/fit/internal/genvectors -seed 1 -n 300")
	}
}

// Shrinking the vessel (any dimension) never adds a blocking code.
func TestRapid_FitMonotone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		opt := func(name string, lo, hi int64) *geo.Cm {
			if !rapid.Bool().Draw(t, name+"_set") {
				return nil
			}
			v := geo.Cm(rapid.Int64Range(lo, hi).Draw(t, name))
			return &v
		}
		v := fit.Vessel{
			LOA: geo.Cm(rapid.Int64Range(1, 3000).Draw(t, "loa")), Beam: geo.Cm(rapid.Int64Range(1, 900).Draw(t, "beam")),
			Draft: opt("draft", 1, 500), AirDraft: opt("air", 1, 3000),
			Hull: rapid.SampledFrom([]string{"monohull_sail", "catamaran", "rib"}).Draw(t, "hull"),
		}
		tg := fit.Target{
			Kind:   fit.TargetKind(rapid.SampledFrom([]string{"berth", "linear_area", "surface_area", "land"}).Draw(t, "kind")),
			MaxLOA: geo.Cm(rapid.Int64Range(0, 3000).Draw(t, "max_loa")), MaxBeam: geo.Cm(rapid.Int64Range(0, 900).Draw(t, "max_beam")),
			Length: geo.Cm(rapid.Int64Range(0, 3000).Draw(t, "len")), Width: geo.Cm(rapid.Int64Range(0, 900).Draw(t, "width")),
			Depth: opt("depth", 1, 600), MaxAirDraft: opt("max_air", 1, 3000), MultihullOK: rapid.Bool().Draw(t, "multi"),
			LandKind: rapid.SampledFrom([]string{"parking", "cradle", "trailer_spot"}).Draw(t, "land"),
		}
		r := fit.Rules{LOATolerance: geo.Cm(rapid.Int64Range(0, 100).Draw(t, "tol")), SafetyClearance: geo.Cm(rapid.Int64Range(0, 60).Draw(t, "clr"))}
		night := calendar.MustParse("2027-07-01")
		big := fit.Check(v, tg, night, r)
		small := v
		small.LOA -= geo.Cm(rapid.Int64Range(0, int64(v.LOA)-1).Draw(t, "dloa"))
		small.Beam -= geo.Cm(rapid.Int64Range(0, int64(v.Beam)-1).Draw(t, "dbeam"))
		if v.Draft != nil {
			d := *v.Draft - geo.Cm(rapid.Int64Range(0, int64(*v.Draft)-1).Draw(t, "ddraft"))
			small.Draft = &d
		}
		if v.AirDraft != nil {
			d := *v.AirDraft - geo.Cm(rapid.Int64Range(0, int64(*v.AirDraft)-1).Draw(t, "dair"))
			small.AirDraft = &d
		}
		had := map[string]bool{}
		for _, c := range big.BlockingCodeList() {
			had[c] = true
		}
		for _, c := range fit.Check(small, tg, night, r).BlockingCodeList() {
			if !had[c] {
				t.Fatalf("shrinking added %s", c)
			}
		}
	})
}

// TestFit_Purity checks the package stays pure: standard library plus calendar and geo, no clock, no randomness.
func TestFit_Purity(t *testing.T) {
	checkPurity(t, ".", map[string]bool{"github.com/coste360/porto/internal/geo": true, "github.com/coste360/porto/internal/calendar": true})
}

func checkPurity(t *testing.T, dir string, allowed map[string]bool) {
	t.Helper()
	forbidden := []string{"time", "math/rand", "math/rand/v2", "crypto/rand", "os", "net", "net/http", "database/sql", "io/fs"}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
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
			if (strings.Contains(path, ".") && !allowed[path]) || containsStr(forbidden, path) {
				t.Errorf("%s imports %q", name, path)
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

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
