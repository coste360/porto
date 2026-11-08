package yardquote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// IssueTestCaseMismatch is the issue of a per-line test case whose expected quantity or price differs.
const IssueTestCaseMismatch = "test_case_mismatch"

// validator checks formulas at save (m10-plan §3.5): parse, type check against the current declarations, then
// evaluation on the test vessel with job defaults and the optional test case.
type validator struct {
	ce     *catalogEnv
	env    formula.Env
	issues []Issue
	ref    string // package code, item code or variable name (re-validation)
	skip   string // derived variable already checked by the caller
}

func (s *Service) newValidator(ctx context.Context, db bun.IDB, scope store.MarinaScope, ce *catalogEnv) (*validator, error) {
	set, err := s.SettingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return &validator{ce: ce, env: ce.testEnv(testVesselOf(set.TestVessel), s.today(ctx, db, scope).Year())}, nil
}

func (v *validator) add(is Issue) {
	if is.Ref == "" {
		is.Ref = v.ref
	}
	v.issues = append(v.issues, is)
}

func (v *validator) err() error {
	if len(v.issues) == 0 {
		return nil
	}
	return &FormulaError{Issues: v.issues}
}

func issueFrom(err error, field string) Issue {
	var se *formula.SyntaxError
	if errors.As(err, &se) {
		return Issue{Code: se.Code, Field: field, Pos: se.Pos}
	}
	var ee *formula.EvalError
	if errors.As(err, &ee) {
		return Issue{Code: ee.Code, Field: field, Pos: ee.Pos, Ref: ee.Ref}
	}
	return Issue{Code: formula.CodeSyntax, Field: field}
}

// check parses and type-checks src for a result kind; it returns the program when it is well typed.
func (v *validator) check(field, src string, want formula.Kind, context []string) *formula.Program {
	p, err := formula.Parse(src)
	if err != nil {
		v.add(issueFrom(err, field))
		return nil
	}
	issues := p.Check(v.ce.decls, v.ce.tables, context)
	for _, is := range issues {
		v.add(Issue{Code: is.Code, Field: field, Pos: is.Pos, Ref: is.Ref, Params: is.Params})
	}
	if len(issues) > 0 {
		return nil
	}
	if k := p.ResultKind(v.ce.decls, v.ce.tables, context); k != want {
		v.add(Issue{Code: formula.CodeTypeMismatch, Field: field, Params: map[string]string{"want": kindName(want)}})
		return nil
	}
	return p
}

func kindName(k formula.Kind) string {
	switch k {
	case formula.KindBool:
		return store.FormulaKindBool
	case formula.KindEnum:
		return store.FormulaKindEnum
	}
	return store.FormulaKindNumber
}

// eval evaluates p on the test environment plus extra values; a missing variable is not an issue at save (a job
// variable without default, a dimension the test vessel lacks): the quote asks for it.
func (v *validator) eval(field string, p *formula.Program, extra map[string]formula.Value) (formula.Value, bool) {
	env := v.env
	if len(extra) > 0 {
		env = formula.Env{Vars: make(map[string]formula.Value, len(v.env.Vars)+len(extra)), Tables: v.env.Tables}
		for k, x := range v.env.Vars {
			env.Vars[k] = x
		}
		for k, x := range extra {
			env.Vars[k] = x
		}
	}
	val, err := p.Eval(env)
	if err != nil {
		var ee *formula.EvalError
		if errors.As(err, &ee) && ee.Code == formula.CodeMissingVariable {
			return formula.Value{}, false
		}
		v.add(issueFrom(err, field))
		return formula.Value{}, false
	}
	return val, true
}

func testVars(tc *TestCase) map[string]formula.Value {
	if tc == nil {
		return nil
	}
	out := map[string]formula.Value{}
	for k, x := range tc.Vars {
		out[k] = price.ToValue(x)
	}
	return out
}

// lineFormulas validates the quantity and price formulas of a line (package line or item defaults). list is the
// catalogue unit price used as list_price on the test vessel (nil = unknown).
func (v *validator) lineFormulas(prefix, qtySrc, priceSrc string, tc *TestCase, list *money.Cents) {
	extra := testVars(tc)
	q := qty.One
	qtyKnown := qtySrc == ""
	if qtySrc != "" {
		if p := v.check(prefix+"qty_formula", qtySrc, formula.KindNumber, nil); p != nil {
			if val, ok := v.eval(prefix+"qty_formula", p, extra); ok {
				m, err := formula.Quantity(val)
				if err != nil {
					v.add(issueFrom(err, prefix+"qty_formula"))
				} else {
					q, qtyKnown = m, true
				}
			}
		}
	}
	if tc != nil && tc.ExpectQty != nil && qtyKnown && q != *tc.ExpectQty {
		v.add(Issue{Code: IssueTestCaseMismatch, Field: prefix + "qty_formula", Params: map[string]string{
			"expected": tc.ExpectQty.String(), "got": q.String(),
		}})
	}
	if priceSrc == "" {
		return
	}
	p := v.check(prefix+"price_formula", priceSrc, formula.KindNumber, []string{formula.VarQty, formula.VarListPrice})
	if p == nil {
		return
	}
	pe := map[string]formula.Value{formula.VarQty: formula.Number(q)}
	for k, x := range extra {
		pe[k] = x
	}
	if list != nil {
		lp, err := formula.EuroValue(*list)
		if err == nil {
			pe[formula.VarListPrice] = lp
		}
	} else if _, ok := pe[formula.VarListPrice]; !ok {
		pe[formula.VarListPrice] = formula.Number(0)
	}
	val, ok := v.eval(prefix+"price_formula", p, pe)
	if !ok {
		return
	}
	c, err := formula.PriceCents(val)
	if err != nil {
		v.add(issueFrom(err, prefix+"price_formula"))
		return
	}
	if tc != nil && tc.ExpectPrice != nil && c != *tc.ExpectPrice {
		v.add(Issue{Code: IssueTestCaseMismatch, Field: prefix + "price_formula", Params: map[string]string{
			"expected": strconv.FormatInt(int64(*tc.ExpectPrice), 10), "got": strconv.FormatInt(int64(c), 10),
		}})
	}
}

// derived validates a derived variable: acyclic, reads only vessel and derived variables, of the declared kind.
func (v *validator) derived(field string, name, src string, kind formula.Kind) {
	p := v.check(field, src, kind, nil)
	if p == nil {
		return
	}
	for _, id := range p.Idents() {
		d, ok := v.ce.decls[id]
		if ok && d.Source != formula.SourceVessel && d.Source != formula.SourceDerived {
			v.add(Issue{Code: formula.CodeUnknownVariable, Field: field, Ref: id, Params: map[string]string{"reason": "not_vessel_or_derived"}})
		}
	}
	if _, err := formula.DerivedOrder(v.ce.derived); err != nil {
		is := issueFrom(err, field)
		if is.Ref == "" {
			is.Ref = name
		}
		v.add(is)
		return
	}
	v.eval(field, p, nil)
}

// decodeLines reads the lines of a package version.
func decodeLines(j store.JSON) []PackageLine {
	var out []PackageLine
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

func decodeTestCase(j store.JSON) *TestCase {
	if len(j) == 0 || string(j) == "null" {
		return nil
	}
	var tc TestCase
	if json.Unmarshal(j, &tc) != nil {
		return nil
	}
	return &tc
}

// listPrices returns the catalogue unit prices of items today (test list_price).
func (s *Service) listPrices(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) map[string]money.Cents {
	out := map[string]money.Cents{}
	if s.d.Prices == nil || len(ids) == 0 {
		return out
	}
	m, err := s.d.Prices.UnitPricesTx(ctx, db, scope, ids, s.today(ctx, db, scope), "")
	if err != nil {
		return out
	}
	for id, p := range m {
		if p.Found {
			out[id] = p.UnitCents
		}
	}
	return out
}

func centsPtr(m map[string]money.Cents, id string) *money.Cents {
	if c, ok := m[id]; ok {
		return &c
	}
	return nil
}

// revalidate re-checks every derived variable, the current version of every active package and every item
// formula against the context (after a change of a variable or table, D6). Failures carry Ref = the package code,
// the item code or the variable name.
func (s *Service) revalidate(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *validator) error {
	for _, d := range v.ce.derived {
		if d.Name == v.skip {
			continue
		}
		v.ref = d.Name
		v.derived("variables."+d.Name, d.Name, d.Src, v.ce.decls[d.Name].Kind)
	}
	pkgs, err := store.AllWorkPackages(ctx, db, scope, true)
	if err != nil {
		return err
	}
	var versions []*store.WorkPackageVersion
	ids := []string{}
	for _, pk := range pkgs {
		if pk.CurrentVersion == 0 {
			versions = append(versions, nil)
			continue
		}
		pv, err := store.GetWorkPackageVersion(ctx, db, scope, pk.ID, pk.CurrentVersion)
		if err != nil {
			return err
		}
		versions = append(versions, pv)
		for _, l := range decodeLines(pv.Lines) {
			ids = append(ids, l.ItemID)
		}
	}
	items, err := store.AllYardItemSettings(ctx, db, scope)
	if err != nil {
		return err
	}
	for _, it := range items {
		ids = append(ids, it.ItemID)
	}
	prices := s.listPrices(ctx, db, scope, ids)
	for i, pk := range pkgs {
		if versions[i] == nil {
			continue
		}
		v.ref = pk.Code
		for j, l := range decodeLines(versions[i].Lines) {
			v.lineFormulas(fmt.Sprintf("packages.%s.lines[%d].", pk.Code, j), l.QtyFormula, l.PriceFormula, l.TestCase, centsPtr(prices, l.ItemID))
		}
	}
	codes := map[string]string{}
	if len(items) > 0 {
		iids := make([]string, 0, len(items))
		for _, it := range items {
			iids = append(iids, it.ItemID)
		}
		rows, err := store.ItemsByIDs(ctx, db, scope, iids)
		if err != nil {
			return err
		}
		for _, r := range rows {
			codes[r.ID] = r.Code
		}
	}
	for _, it := range items {
		v.ref = codes[it.ItemID]
		v.lineFormulas("items."+codes[it.ItemID]+".", it.QtyFormula, it.PriceFormula, decodeTestCase(it.TestCase), centsPtr(prices, it.ItemID))
	}
	v.ref = ""
	return nil
}
