package pricing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os" //nolint:depguard // the test reads the hand-derived corpus of testdata/pricing (not the engine)
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The corpus of hand-derived cases (m2-plan §2.7). Expectations are computed by hand from the semantics of
// doc.go, never by running the engine.
var corpusDir = filepath.Join("..", "..", "..", "testdata", "pricing")

type corpusCase struct {
	Name     string            `json:"name"`
	Spec     string            `json:"spec"`
	Catalog  string            `json:"catalog"`
	Patch    json.RawMessage   `json:"patch,omitempty"`     // RFC 7386 merge patch of the fixture
	AddItems []json.RawMessage `json:"add_items,omitempty"` // appended to items after the patch
	AddRules []json.RawMessage `json:"add_rules,omitempty"` // appended to rules after the patch
	Input    json.RawMessage   `json:"input"`
	Expect   json.RawMessage   `json:"expect"`
	Why      string            `json:"why"`
}

type corpusFile struct {
	Cases []corpusCase `json:"cases"`
}

type loadedCase struct {
	file string
	corpusCase
}

func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func decodeAny(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// mergePatch applies an RFC 7386 JSON merge patch.
func mergePatch(target, patch any) any {
	pm, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	tm, ok := target.(map[string]any)
	if !ok {
		tm = map[string]any{}
	}
	for k, v := range pm {
		if v == nil {
			delete(tm, k)
			continue
		}
		tm[k] = mergePatch(tm[k], v)
	}
	return tm
}

func loadFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir, "catalogs", name+".json"))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func fixtureNames(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(corpusDir, "catalogs", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".json"))
	}
	sort.Strings(out)
	return out
}

func loadCorpus(t testing.TB) []loadedCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(corpusDir, "cases", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var out []loadedCase
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cf corpusFile
		if err := strictUnmarshal(b, &cf); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, c := range cf.Cases {
			out = append(out, loadedCase{file: strings.TrimSuffix(filepath.Base(f), ".json"), corpusCase: c})
		}
	}
	return out
}

// buildCatalog loads the fixture of c and applies its patch and additions.
func buildCatalog(t testing.TB, c corpusCase) Catalog {
	t.Helper()
	doc, err := decodeAny(loadFixture(t, c.Catalog))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Patch) > 0 {
		p, err := decodeAny(c.Patch)
		if err != nil {
			t.Fatal(err)
		}
		doc = mergePatch(doc, p)
	}
	m := doc.(map[string]any)
	for key, add := range map[string][]json.RawMessage{"items": c.AddItems, "rules": c.AddRules} {
		list, _ := m[key].([]any)
		for _, raw := range add {
			v, err := decodeAny(raw)
			if err != nil {
				t.Fatal(err)
			}
			list = append(list, v)
		}
		m[key] = list
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var cat Catalog
	if err := strictUnmarshal(b, &cat); err != nil {
		t.Fatalf("catalog %s: %v", c.Catalog, err)
	}
	return cat
}

// compareSubset checks that every field of want is in got with the same value; arrays must have the same
// length and their elements are compared the same way.
func compareSubset(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want object, got %v", path, got)}
		}
		var out []string
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gv, ok := g[k]
			if !ok {
				if w[k] == nil {
					continue
				}
				out = append(out, fmt.Sprintf("%s.%s: missing", path, k))
				continue
			}
			out = append(out, compareSubset(path+"."+k, w[k], gv)...)
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok {
			if len(w) == 0 && got == nil {
				return nil
			}
			return []string{fmt.Sprintf("%s: want array, got %v", path, got)}
		}
		if len(w) != len(g) {
			gb, _ := json.Marshal(g)
			return []string{fmt.Sprintf("%s: want %d elements, got %d: %s", path, len(w), len(g), gb)}
		}
		var out []string
		for i := range w {
			out = append(out, compareSubset(fmt.Sprintf("%s[%d]", path, i), w[i], g[i])...)
		}
		return out
	default:
		if fmt.Sprint(want) != fmt.Sprint(got) {
			return []string{fmt.Sprintf("%s: want %v, got %v", path, want, got)}
		}
		return nil
	}
}

// runCase quotes one case and returns the mismatches.
func runCase(t testing.TB, c corpusCase) []string {
	t.Helper()
	cat := buildCatalog(t, c)
	var in Input
	if err := strictUnmarshal(c.Input, &in); err != nil {
		t.Fatalf("input: %v", err)
	}
	wantAny, err := decodeAny(c.Expect)
	if err != nil {
		t.Fatal(err)
	}
	want := wantAny.(map[string]any)
	bd, qerr := Quote(in, cat)
	if we, ok := want["error"]; ok {
		if qerr == nil {
			return []string{fmt.Sprintf("want error %v, got total %d", we, bd.Total)}
		}
		if got := ErrorCode(qerr); got != we {
			return []string{fmt.Sprintf("want error %v, got %s (%v)", we, got, qerr)}
		}
		return nil
	}
	if qerr != nil {
		return []string{fmt.Sprintf("unexpected error %s: %v", ErrorCode(qerr), qerr)}
	}
	if _, ok := want["total_cents"]; !ok {
		t.Fatalf("expect has no total_cents")
	}
	var out []string
	if tc, ok := want["trace_codes"]; ok {
		delete(want, "trace_codes")
		codes := tc.([]any)
		i := 0
		for _, st := range bd.Trace {
			if i < len(codes) && st.Code == codes[i] {
				i++
			}
		}
		if i < len(codes) {
			out = append(out, fmt.Sprintf("trace_codes: %v not a subsequence (stopped at %v)", codes, codes[i]))
		}
	}
	gb, err := json.Marshal(bd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAny(gb)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, compareSubset("", want, got)...)
}

func TestQuote_Corpus(t *testing.T) {
	for _, c := range loadCorpus(t) {
		t.Run(c.file+"/"+c.Name, func(t *testing.T) {
			for _, m := range runCase(t, c.corpusCase) {
				t.Error(m)
			}
			if t.Failed() {
				t.Logf("why: %s", c.Why)
			}
		})
	}
}

// corpusQuotas are the minimum cases per file prefix (m2-plan §2.7).
var corpusQuotas = map[string]int{
	"01": 12, "02": 20, "03": 20, "04": 14, "05": 8, "06": 14, "07": 10, "08": 10, "09": 12,
	"10": 10, "11": 14, "12": 20, "13": 24, "14": 10, "15": 16, "16": 10, "17": 8, "18": 14,
}

func TestCorpus_AtLeast200(t *testing.T) {
	cases := loadCorpus(t)
	if len(cases) < 200 {
		t.Errorf("corpus has %d cases, want >= 200", len(cases))
	}
	per := map[string]int{}
	for _, c := range cases {
		per[c.file[:2]]++
		if strings.TrimSpace(c.Why) == "" {
			t.Errorf("%s/%s has no why", c.file, c.Name)
		}
		if strings.TrimSpace(c.Spec) == "" {
			t.Errorf("%s/%s has no spec reference", c.file, c.Name)
		}
	}
	for g, minimum := range corpusQuotas {
		if per[g] < minimum {
			t.Errorf("group %s has %d cases, want >= %d", g, per[g], minimum)
		}
	}
}

func TestCorpus_NamesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, c := range loadCorpus(t) {
		if prev, ok := seen[c.Name]; ok {
			t.Errorf("case %q in %s and %s", c.Name, prev, c.file)
		}
		seen[c.Name] = c.file
	}
}

func TestCorpus_CatalogsValid(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			cat := buildCatalog(t, corpusCase{Catalog: name})
			issues := Validate(cat)
			if strings.HasPrefix(name, "invalid-") {
				if !HasErrors(issues) {
					t.Errorf("invalid fixture has no errors")
				}
				return
			}
			for _, is := range issues {
				if is.Severity == SeverityError {
					t.Errorf("%+v", is)
				}
			}
		})
	}
}
