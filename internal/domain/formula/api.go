package formula

import (
	"errors"
	"slices"
	"sort"

	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Idents returns the distinct identifiers (variables) referenced by the program, sorted.
func (p *Program) Idents() []string {
	if p == nil {
		return nil
	}
	return slices.Clone(p.idents)
}

// Tables returns the distinct band tables referenced by the program, sorted.
func (p *Program) Tables() []string {
	if p == nil {
		return nil
	}
	return slices.Clone(p.tables)
}

// ResolveDerived evaluates the derived variables in topological order on top of base and returns the extended
// environment; a cycle is an *EvalError with CodeDerivedCycle. Definitions are visited in name order, so the
// result and the first error never depend on the input order. A derived variable that cannot be evaluated is an
// *EvalError whose Ref names it.
func ResolveDerived(defs []Derived, base Env, decls map[string]VarDecl) (Env, error) {
	_ = decls
	out := Env{Vars: make(map[string]Value, len(base.Vars)+len(defs)), Tables: base.Tables}
	for k, v := range base.Vars {
		out.Vars[k] = v
	}
	sorted := slices.Clone(defs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	progs := make(map[string]*Program, len(sorted))
	for _, d := range sorted {
		if _, dup := progs[d.Name]; dup {
			continue
		}
		p, err := Parse(d.Src)
		if err != nil {
			var se *SyntaxError
			if errors.As(err, &se) {
				return Env{}, &EvalError{Code: se.Code, Pos: se.Pos, Ref: d.Name}
			}
			return Env{}, &EvalError{Code: CodeSyntax, Ref: d.Name}
		}
		progs[d.Name] = p
	}
	order, err := topoOrder(progs)
	if err != nil {
		return Env{}, err
	}
	for _, name := range order {
		v, err := progs[name].Eval(out)
		if err != nil {
			var ee *EvalError
			if errors.As(err, &ee) {
				return Env{}, &EvalError{Code: ee.Code, Pos: ee.Pos, Ref: name}
			}
			return Env{}, err
		}
		out.Vars[name] = v
	}
	return out, nil
}

// DerivedOrder returns the evaluation order of derived definitions (dependencies first, ties by name), or an
// *EvalError derived_cycle (Ref = a variable on the cycle) or the syntax error of a definition.
func DerivedOrder(defs []Derived) ([]string, error) {
	progs := make(map[string]*Program, len(defs))
	for _, d := range defs {
		p, err := Parse(d.Src)
		if err != nil {
			var se *SyntaxError
			if errors.As(err, &se) {
				return nil, &EvalError{Code: se.Code, Pos: se.Pos, Ref: d.Name}
			}
			return nil, err
		}
		progs[d.Name] = p
	}
	return topoOrder(progs)
}

// topoOrder is Kahn's algorithm over the derived definitions, always taking the smallest ready name.
func topoOrder(progs map[string]*Program) ([]string, error) {
	names := make([]string, 0, len(progs))
	for n := range progs {
		names = append(names, n)
	}
	sort.Strings(names)
	deps := make(map[string][]string, len(names))
	indeg := make(map[string]int, len(names))
	users := map[string][]string{}
	for _, n := range names {
		for _, id := range progs[n].idents {
			if _, ok := progs[id]; ok {
				deps[n] = append(deps[n], id)
				users[id] = append(users[id], n)
			}
		}
		indeg[n] = len(deps[n])
	}
	var ready []string
	for _, n := range names {
		if indeg[n] == 0 {
			ready = append(ready, n)
		}
	}
	order := make([]string, 0, len(names))
	for len(ready) > 0 {
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for _, u := range users[n] {
			indeg[u]--
			if indeg[u] == 0 {
				ready = append(ready, u)
			}
		}
	}
	if len(order) < len(names) {
		for _, n := range names {
			if indeg[n] > 0 {
				return nil, &EvalError{Code: CodeDerivedCycle, Ref: n}
			}
		}
	}
	return order, nil
}

// Quantity converts the result of a quantity formula: a number >= 0 (else CodeNegativeQuantity).
func Quantity(v Value) (qty.Milli, error) {
	if v.Kind != KindNumber {
		return 0, &EvalError{Code: CodeTypeMismatch}
	}
	if v.Num < 0 {
		return 0, &EvalError{Code: CodeNegativeQuantity}
	}
	return v.Num, nil
}

// PriceCents converts the result of a price formula (euro with 3 decimals) to cents half-even (v / 10); the
// result is >= 0 (else CodeNegativePrice).
func PriceCents(v Value) (money.Cents, error) {
	if v.Kind != KindNumber {
		return 0, &EvalError{Code: CodeTypeMismatch}
	}
	if v.Num < 0 {
		return 0, &EvalError{Code: CodeNegativePrice}
	}
	c, err := v.Num.DivInt(10)
	if err != nil {
		return 0, &EvalError{Code: CodeOverflow}
	}
	return money.Cents(c), nil
}

// EuroValue is the formula value of an amount in cents (list_price): euro with 3 decimals, exact.
func EuroValue(c money.Cents) (Value, error) {
	v, err := qty.Milli(c).MulInt(10)
	if err != nil {
		return Value{}, &EvalError{Code: CodeOverflow}
	}
	return Number(v), nil
}

// CmValue is the formula value (metres) of a length in centimetres: cm × 10 milli, exact.
func CmValue(cm int64) (Value, error) {
	v, err := qty.Milli(cm).MulInt(10)
	if err != nil {
		return Value{}, &EvalError{Code: CodeOverflow}
	}
	return Number(v), nil
}
