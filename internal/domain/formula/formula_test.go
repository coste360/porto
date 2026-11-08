package formula

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/qty"
)

func m(s string) qty.Milli { return qty.MustParse(s) }

func mp(s string) *qty.Milli {
	v := m(s)
	return &v
}

// testDecls is the 12 m sailing boat of m10-plan §4.4 plus a job variable and an enum.
func testDecls() map[string]VarDecl {
	return map[string]VarDecl{
		"loa":       {Name: "loa", Kind: KindNumber, Source: SourceVessel},
		"beam":      {Name: "beam", Kind: KindNumber, Source: SourceVessel},
		"draft":     {Name: "draft", Kind: KindNumber, Source: SourceVessel},
		"hull_area": {Name: "hull_area", Kind: KindNumber, Source: SourceDerived},
		"layers":    {Name: "layers", Kind: KindNumber, Source: SourceJob, Min: mp("1"), Max: mp("4"), Default: mp("2")},
		"hull_type": {Name: "hull_type", Kind: KindEnum, Source: SourceVessel, Enum: []string{"monohull_sail", "catamaran"}},
		"keel":      {Name: "keel", Kind: KindBool, Source: SourceJob},
	}
}

func testTables() map[string]Table {
	return map[string]Table{
		"lift": {Name: "lift", Rows: []Row{{UpTo: mp("10"), Value: m("100")}, {UpTo: mp("15"), Value: m("150")}, {Value: m("300")}}},
		"shut": {Name: "shut", Rows: []Row{{UpTo: mp("10"), Value: m("1")}}},
	}
}

func testEnv() Env {
	return Env{
		Vars: map[string]Value{
			"loa": Number(m("12")), "beam": Number(m("4")), "draft": Number(m("1.5")), "hull_area": Number(m("56.1")),
			"layers": Number(m("2")), "hull_type": Enum("monohull_sail"), "keel": Bool(true),
			"qty": Number(m("3")), "list_price": Number(m("18")),
		},
		Tables: testTables(),
	}
}

func evalSrc(t *testing.T, src string) (Value, error) {
	t.Helper()
	p, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return p.Eval(testEnv())
}

func TestFormula_Eval(t *testing.T) {
	cases := []struct {
		src  string
		want Value
	}{
		{"1 + 2 * 3", Number(m("7"))},
		{"(1 + 2) * 3", Number(m("9"))},
		{"10 - 4 - 3", Number(m("3"))},
		{"1 / 3 * 3", Number(m("0.999"))}, // half-even at every step, left to right
		{"2 / 3", Number(m("0.667"))},
		{"0.005 * 0.5", Number(m("0.002"))}, // 0.0025 -> half-even 0.002
		{"0.015 * 0.5", Number(m("0.008"))}, // 0.0075 -> 0.008
		{"-loa", Number(m("-12"))},
		{"- -2", Number(m("2"))},
		{"loa * (beam + draft) * 0.85", Number(m("56.1"))},
		{"round(hull_area * 0.15, 1)", Number(m("8.4"))},
		{"round(8.45, 1)", Number(m("8.4"))},
		{"round(8.55, 1)", Number(m("8.6"))},
		{"round(2.5, 0)", Number(m("2"))},
		{"round(1.235, 2)", Number(m("1.24"))},
		{"ceil(hull_area * layers / 25)", Number(m("5"))},
		{"ceil(4)", Number(m("4"))},
		{"ceil(-1.5)", Number(m("-1"))},
		{"floor(1.999)", Number(m("1"))},
		{"floor(-1.001)", Number(m("-2"))},
		{"min(3, 1, 2)", Number(m("1"))},
		{"max(3, 1, 2, 8, 0, 4, 5, 6)", Number(m("8"))},
		{"if(hull_type == \"catamaran\", list_price * 1.5, list_price)", Number(m("18"))},
		{"if(hull_type != \"catamaran\", 1, 2)", Number(m("1"))},
		{"loa > 10 and keel", Bool(true)},
		{"loa < 10 or not keel", Bool(false)},
		{"not loa <= 12", Bool(false)},
		{"loa >= 12", Bool(true)},
		{"loa == 12", Bool(true)},
		{"keel != false", Bool(true)},
		{"band(loa, lift)", Number(m("150"))},
		{"band(10, lift)", Number(m("100"))}, // equal to up_to
		{"band(10.001, lift)", Number(m("150"))},
		{"band(99, lift)", Number(m("300"))}, // open row
		{"qty * list_price", Number(m("54"))},
		{"true", Bool(true)},
	}
	for _, c := range cases {
		got, err := evalSrc(t, c.src)
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		if got != c.want {
			t.Fatalf("%q = %+v, want %+v", c.src, got, c.want)
		}
		p, _ := Parse(c.src)
		if issues := p.Check(testDecls(), testTables(), []string{VarQty, VarListPrice}); len(issues) > 0 {
			t.Fatalf("%q: unexpected issues %+v", c.src, issues)
		}
	}
}

func TestFormula_LazyAndShortCircuit(t *testing.T) {
	for _, src := range []string{
		"if(true, 1, 1 / 0)",
		"if(false, 1 / 0, 2)",
		"false and 1 / 0 > 1",
		"true or 1 / 0 > 1",
		"if(loa > 5, 1, missing_var)",
	} {
		if _, err := evalSrc(t, src); err != nil {
			t.Fatalf("%q: %v", src, err)
		}
	}
}

func TestFormula_EvalErrors(t *testing.T) {
	cases := map[string]string{
		"1 / 0":                      CodeDivByZero,
		"1 / (loa - 12)":             CodeDivByZero,
		"nope + 1":                   CodeMissingVariable,
		"band(20, shut)":             CodeBandNoRow,
		"band(1, nothere)":           CodeUnknownTable,
		"9223372036854775 * 1000":    CodeOverflow,
		"9223372036854775 + 1":       CodeOverflow,
		"-9223372036854775 - 1":      CodeOverflow,
		"ceil(9223372036854775.807)": CodeOverflow,
		"foo(1)":                     CodeUnknownFunction,
		"loa + keel":                 CodeTypeMismatch,
	}
	for src, code := range cases {
		_, err := evalSrc(t, src)
		var ee *EvalError
		if !errors.As(err, &ee) || ee.Code != code {
			t.Fatalf("%q: got %v, want %s", src, err, code)
		}
	}
}

func TestFormula_SyntaxErrors(t *testing.T) {
	cases := map[string]string{
		"":                              CodeSyntax,
		"1 +":                           CodeSyntax,
		"(1":                            CodeSyntax,
		"1.2345":                        CodeSyntax,
		"1.":                            CodeSyntax,
		"1a":                            CodeSyntax,
		"Loa":                           CodeSyntax,
		"lOa":                           CodeSyntax,
		"a < b < c":                     CodeSyntax,
		"\"x":                           CodeSyntax,
		"\"A\"":                         CodeSyntax,
		"1 % 2":                         CodeSyntax,
		"min(1,)":                       CodeSyntax,
		"99999999999999999999":          CodeSyntax,
		strings.Repeat("1+", 300) + "1": CodeTooComplex, // > 500 bytes
		strings.Repeat("(", 40) + "1" + strings.Repeat(")", 40): CodeTooComplex,
		strings.Repeat("-", 40) + "1":                           CodeTooComplex,
		strings.Repeat("1+", 200) + "1":                         CodeTooComplex, // > 256 nodes
	}
	idents := make([]string, 33)
	for i := range idents {
		idents[i] = "v" + strconv.Itoa(i)
	}
	cases[strings.Join(idents, "+")] = CodeTooComplex
	for src, code := range cases {
		_, err := Parse(src)
		var se *SyntaxError
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("%q: got %v, want %s", src, err, code)
		}
	}
}

func TestFormula_Check(t *testing.T) {
	cases := map[string]string{
		"loa + nope":                          CodeUnknownVariable,
		"qty * 2":                             CodeUnknownVariable, // context only inside price formulas
		"foo(1)":                              CodeUnknownFunction,
		"band(loa, nothere)":                  CodeUnknownTable,
		"band(loa, 3)":                        CodeTypeMismatch,
		"min(1)":                              CodeArity,
		"max(1,2,3,4,5,6,7,8,9)":              CodeArity,
		"if(true, 1)":                         CodeArity,
		"round(loa)":                          CodeArity,
		"round(loa, layers)":                  CodeTypeMismatch,
		"round(loa, 4)":                       CodeTypeMismatch,
		"round(loa, 1.5)":                     CodeTypeMismatch,
		"ceil(1, 2)":                          CodeArity,
		"loa + keel":                          CodeTypeMismatch,
		"keel and 1":                          CodeTypeMismatch,
		"not loa":                             CodeTypeMismatch,
		"-keel":                               CodeTypeMismatch,
		"hull_type < \"catamaran\"":           CodeTypeMismatch,
		"hull_type == \"submarine\"":          CodeEnumValueUnknown,
		"\"a\" == \"a\"":                      CodeTypeMismatch,
		"\"a\"":                               CodeTypeMismatch,
		"loa == keel":                         CodeTypeMismatch,
		"if(loa, 1, 2)":                       CodeTypeMismatch,
		"if(keel, 1, keel)":                   CodeTypeMismatch,
		"if(keel, \"a\", \"b\") == hull_type": CodeTypeMismatch,
	}
	for src, code := range cases {
		p, err := Parse(src)
		if err != nil {
			t.Fatalf("%q: parse %v", src, err)
		}
		issues := p.Check(testDecls(), testTables(), nil)
		found := false
		for _, is := range issues {
			found = found || is.Code == code
		}
		if !found {
			t.Fatalf("%q: issues %+v, want %s", src, issues, code)
		}
	}
	p, _ := Parse("loa * 2")
	if k := p.ResultKind(testDecls(), nil, nil); k != KindNumber {
		t.Fatalf("result kind %v", k)
	}
	p, _ = Parse("loa > 2")
	if k := p.ResultKind(testDecls(), nil, nil); k != KindBool {
		t.Fatalf("result kind %v", k)
	}
}

func TestFormula_IdentsAndTables(t *testing.T) {
	p, err := Parse("band(loa * layers, lift) + loa + beam")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Idents(); !reflect.DeepEqual(got, []string{"beam", "layers", "loa"}) {
		t.Fatalf("idents %v", got)
	}
	if got := p.Tables(); !reflect.DeepEqual(got, []string{"lift"}) {
		t.Fatalf("tables %v", got)
	}
	if p.Source() != "band(loa * layers, lift) + loa + beam" {
		t.Fatal("source")
	}
}

// TestFormula_HullArea checks the worked example of m10-plan §4.4.
func TestFormula_HullArea(t *testing.T) {
	base := Env{Vars: map[string]Value{"loa": Number(m("12")), "beam": Number(m("4")), "draft": Number(m("1.5"))}}
	env, err := ResolveDerived([]Derived{
		{Name: "paint_area", Src: "hull_area * 1.1"},
		{Name: "hull_area", Src: "loa * (beam + draft) * 0.85"},
	}, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := env.Vars["hull_area"]; got != Number(m("56.1")) {
		t.Fatalf("hull_area = %v", got)
	}
	if got := env.Vars["paint_area"]; got != Number(m("61.71")) {
		t.Fatalf("paint_area = %v", got)
	}
	if _, ok := base.Vars["hull_area"]; ok {
		t.Fatal("base env modified")
	}
	cases := map[string]qty.Milli{
		"round(hull_area, 1)":                 m("56.1"),
		"round(hull_area * 0.15, 1)":          m("8.4"),
		"round(hull_area * layers * 0.05, 1)": m("5.6"),
		"ceil(hull_area * layers / 25)":       m("5"),
	}
	env.Vars["layers"] = Number(m("2"))
	for src, want := range cases {
		p, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		v, err := p.Eval(env)
		if err != nil {
			t.Fatal(err)
		}
		if q, _ := Quantity(v); q != want {
			t.Fatalf("%s = %v, want %v", src, q, want)
		}
	}
}

func TestFormula_DerivedErrors(t *testing.T) {
	_, err := ResolveDerived([]Derived{{Name: "a", Src: "b + 1"}, {Name: "b", Src: "a * 2"}}, Env{}, nil)
	var ee *EvalError
	if !errors.As(err, &ee) || ee.Code != CodeDerivedCycle || ee.Ref != "a" {
		t.Fatalf("cycle: %v", err)
	}
	_, err = ResolveDerived([]Derived{{Name: "a", Src: "a + 1"}}, Env{}, nil)
	if !errors.As(err, &ee) || ee.Code != CodeDerivedCycle {
		t.Fatalf("self cycle: %v", err)
	}
	_, err = ResolveDerived([]Derived{{Name: "a", Src: "1 +"}}, Env{}, nil)
	if !errors.As(err, &ee) || ee.Code != CodeSyntax || ee.Ref != "a" {
		t.Fatalf("syntax: %v", err)
	}
	_, err = ResolveDerived([]Derived{{Name: "a", Src: "draft * 2"}}, Env{}, nil)
	if !errors.As(err, &ee) || ee.Code != CodeMissingVariable || ee.Ref != "a" {
		t.Fatalf("missing: %v", err)
	}
	order, err := DerivedOrder([]Derived{{Name: "c", Src: "b"}, {Name: "b", Src: "a"}, {Name: "a", Src: "1"}, {Name: "d", Src: "2"}})
	if err != nil || !reflect.DeepEqual(order, []string{"a", "b", "c", "d"}) {
		t.Fatalf("order %v %v", order, err)
	}
}

func TestFormula_Conversions(t *testing.T) {
	if q, err := Quantity(Number(m("1.5"))); err != nil || q != m("1.5") {
		t.Fatal(q, err)
	}
	var ee *EvalError
	if _, err := Quantity(Number(m("-1"))); !errors.As(err, &ee) || ee.Code != CodeNegativeQuantity {
		t.Fatal(err)
	}
	if _, err := Quantity(Bool(true)); !errors.As(err, &ee) || ee.Code != CodeTypeMismatch {
		t.Fatal(err)
	}
	cases := map[string]int64{"27": 2700, "18.005": 1800, "18.015": 1802, "0.004": 0, "0.006": 1, "12.345": 1234}
	for s, want := range cases {
		c, err := PriceCents(Number(m(s)))
		if err != nil || int64(c) != want {
			t.Fatalf("price %s = %d %v, want %d", s, c, err, want)
		}
	}
	if _, err := PriceCents(Number(m("-0.01"))); !errors.As(err, &ee) || ee.Code != CodeNegativePrice {
		t.Fatal(err)
	}
	if v, _ := EuroValue(1800); v != Number(m("18")) {
		t.Fatal(v)
	}
	if v, _ := CmValue(1234); v != Number(m("12.34")) {
		t.Fatal(v)
	}
}
