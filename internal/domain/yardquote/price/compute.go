package price

import (
	"errors"
	"slices"
	"sort"

	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// ItemTypeLabour is the item type whose lines carry estimated hours (= quantity) and create a task.
const ItemTypeLabour = "labour"

// Value kinds of VariableValue.
const (
	KindNumber = "number"
	KindBool   = "bool"
	KindEnum   = "enum"
)

// ErrCycle means the derived definitions of a snapshot form a cycle (never stored: saves are validated).
var ErrCycle = errors.New("yardquote/price: derived cycle")

// ToValue converts a snapshot value to a formula value.
func ToValue(v VariableValue) formula.Value {
	switch v.Kind {
	case KindBool:
		return formula.Bool(v.Bool)
	case KindEnum:
		return formula.Enum(v.Enum)
	}
	return formula.Number(v.Num)
}

// FromValue converts a formula value to a snapshot value.
func FromValue(v formula.Value) VariableValue {
	switch v.Kind {
	case formula.KindBool:
		return VariableValue{Kind: KindBool, Bool: v.Bool}
	case formula.KindEnum:
		return VariableValue{Kind: KindEnum, Enum: v.Enum}
	}
	return VariableValue{Kind: KindNumber, Num: v.Num}
}

// Env builds the formula environment of a snapshot: vessel ∪ overrides ∪ derived ∪ job inputs (later sources win)
// and the band tables. A derived variable that cannot be evaluated (missing vessel data) is left out, so the lines
// that use it report missing_variable.
func Env(s Snapshot) (formula.Env, error) {
	env := formula.Env{Vars: map[string]formula.Value{}, Tables: map[string]formula.Table{}}
	for _, t := range s.Tables {
		ft := formula.Table{Name: t.Name}
		for _, r := range t.Rows {
			ft.Rows = append(ft.Rows, formula.Row{UpTo: r.UpTo, Value: r.Value})
		}
		env.Tables[t.Name] = ft
	}
	for k, v := range s.Vessel {
		env.Vars[k] = ToValue(v)
	}
	for k, v := range s.VesselOverrides {
		env.Vars[k] = ToValue(v)
	}
	defs := make([]formula.Derived, 0, len(s.Derived))
	srcs := map[string]string{}
	for _, d := range s.Derived {
		defs = append(defs, formula.Derived{Name: d.Name, Src: d.Src})
		srcs[d.Name] = d.Src
	}
	order, err := formula.DerivedOrder(defs)
	if err != nil {
		var ee *formula.EvalError
		if errors.As(err, &ee) && ee.Code == formula.CodeDerivedCycle {
			return env, ErrCycle
		}
		order = nil // a definition that does not parse is simply absent
		for _, d := range defs {
			if _, perr := formula.Parse(d.Src); perr == nil {
				order = append(order, d.Name)
			}
		}
		sort.Strings(order)
	}
	for _, name := range order {
		p, err := formula.Parse(srcs[name])
		if err != nil {
			continue
		}
		if v, err := p.Eval(env); err == nil {
			env.Vars[name] = v
		}
	}
	for k, v := range s.JobInputs {
		env.Vars[k] = ToValue(v)
	}
	return env, nil
}

// issueOf converts a formula error to a line issue of field.
func issueOf(err error, field string) LineIssue {
	var ee *formula.EvalError
	if errors.As(err, &ee) {
		return LineIssue{Code: ee.Code, Field: field, Pos: ee.Pos, Ref: ee.Ref}
	}
	var se *formula.SyntaxError
	if errors.As(err, &se) {
		return LineIssue{Code: se.Code, Field: field, Pos: se.Pos}
	}
	return LineIssue{Code: formula.CodeSyntax, Field: field}
}

func evalFormula(src string, env formula.Env) (formula.Value, error) {
	p, err := formula.Parse(src)
	if err != nil {
		return formula.Value{}, err
	}
	return p.Eval(env)
}

// Compute prices the lines of the snapshot (formulas, catalogue prices, overrides, VAT) and the totals. It is
// deterministic: the same snapshot always gives the same lines (INV-28).
func Compute(s Snapshot) ([]Line, Totals, error) {
	env, err := Env(s)
	if err != nil {
		return nil, Totals{}, err
	}
	overrides := map[int]Override{}
	for _, o := range s.Overrides {
		overrides[o.Seq] = o
	}
	specs := append([]LineSpec(nil), s.Lines...)
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].Seq < specs[j].Seq })
	lines := make([]Line, 0, len(specs))
	for _, spec := range specs {
		o, hasOverride := overrides[spec.Seq]
		if hasOverride && o.Remove {
			continue
		}
		l, err := computeLine(spec, s, env, o, hasOverride)
		if err != nil {
			return nil, Totals{}, err
		}
		lines = append(lines, l)
	}
	t, err := totals(lines, s)
	return lines, t, err
}

// Recompute reproduces the stored lines of a snapshot exactly (INV-28): same as Compute, kept separate so that a
// future LanguageVersion keeps the old evaluator path.
func Recompute(s Snapshot) ([]Line, Totals, error) { return Compute(s) }

func computeLine(spec LineSpec, s Snapshot, env formula.Env, o Override, hasOverride bool) (Line, error) {
	item := s.Items[spec.ItemID]
	pr := s.Prices[spec.ItemID]
	l := Line{Spec: spec, QualificationID: item.QualificationID, UnitCost: item.CostCents, VATData: item.VAT}
	if pr.Found {
		l.VATData.PriceIncludesVAT = pr.PriceIncludesVAT
		l.PriceListID, l.RuleID = pr.PriceListID, pr.RuleID
	}
	var qtyIssues, priceIssues []LineIssue
	// Quantity.
	switch {
	case spec.ManualQty != nil:
		l.Qty = *spec.ManualQty
	case spec.QtyFormula == "":
		l.Qty = qty.One
	default:
		v, err := evalFormula(spec.QtyFormula, env)
		if err == nil {
			l.Qty, err = formula.Quantity(v)
		}
		if err != nil {
			qtyIssues = append(qtyIssues, issueOf(err, "qty_formula"))
		}
	}
	// Unit price.
	switch {
	case spec.ManualUnit != nil:
		l.Unit = *spec.ManualUnit
	case spec.PriceFormula != "":
		penv := formula.Env{Vars: make(map[string]formula.Value, len(env.Vars)+2), Tables: env.Tables}
		for k, v := range env.Vars {
			penv.Vars[k] = v
		}
		penv.Vars[formula.VarQty] = formula.Number(l.Qty)
		if pr.Found {
			lp, err := formula.EuroValue(pr.UnitCents)
			if err != nil {
				return Line{}, err
			}
			penv.Vars[formula.VarListPrice] = lp
		}
		v, err := evalFormula(spec.PriceFormula, penv)
		if err == nil {
			l.Unit, err = formula.PriceCents(v)
		}
		if err != nil {
			is := issueOf(err, "price_formula")
			if !pr.Found && is.Code == formula.CodeMissingVariable && is.Ref == formula.VarListPrice {
				is = LineIssue{Code: IssueNoPrice, Field: "unit_cents", Ref: spec.ItemID}
			}
			priceIssues = append(priceIssues, is)
		}
	case pr.Found:
		l.Unit = pr.UnitCents
	default:
		priceIssues = append(priceIssues, LineIssue{Code: IssueNoPrice, Field: "unit_cents", Ref: spec.ItemID})
	}
	// Overrides (marked).
	if hasOverride {
		if o.Qty != nil {
			l.Qty, l.QtyOverridden, qtyIssues = *o.Qty, true, nil
		}
		if o.Unit != nil {
			l.Unit, l.PriceOverridden, priceIssues = *o.Unit, true, nil
		}
		if o.Discount != nil && *o.Discount > 0 {
			l.Discount, l.PriceOverridden = *o.Discount, true
		}
		if o.Description != nil {
			l.Spec.Description = *o.Description
		}
	}
	l.Issues = slices.Concat(qtyIssues, priceIssues)
	if item.ItemType == ItemTypeLabour {
		l.EstimatedHours = l.Qty
	}
	amount, err := l.Qty.MulCents(l.Unit)
	if err != nil {
		return Line{}, err
	}
	if l.Discount > amount {
		l.Discount = amount
	}
	amount -= l.Discount
	rate := l.VATData.RateBP
	if l.VATData.PriceIncludesVAT {
		l.Gross = amount
		l.Net, l.VAT = money.VATSplit(amount, rate)
	} else {
		l.Net = amount
		l.VAT = amount.PercentBP(rate)
		l.Gross = l.Net + l.VAT
	}
	return l, nil
}

func totals(lines []Line, s Snapshot) (Totals, error) {
	t := Totals{EstimatedHours: map[string]qty.Milli{}, Buckets: []VATBucket{}}
	type key struct {
		rate   int64
		nature string
	}
	buckets := map[key]*VATBucket{}
	var keys []key
	for i := range lines {
		l := &lines[i]
		t.Net += l.Net
		t.VAT += l.VAT
		t.Gross += l.Gross
		if len(l.Issues) > 0 {
			t.Blocking = true
		}
		k := key{l.VATData.RateBP, l.VATData.Nature}
		b, ok := buckets[k]
		if !ok {
			b = &VATBucket{RateBP: k.rate, Nature: k.nature}
			buckets[k] = b
			keys = append(keys, k)
		}
		b.Net += l.Net
		b.VAT += l.VAT
		b.Gross += l.Gross
		if l.EstimatedHours > 0 && l.QualificationID != "" {
			h, err := t.EstimatedHours[l.QualificationID].Add(l.EstimatedHours)
			if err != nil {
				return Totals{}, err
			}
			t.EstimatedHours[l.QualificationID] = h
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].rate != keys[j].rate {
			return keys[i].rate > keys[j].rate
		}
		return keys[i].nature < keys[j].nature
	})
	for _, k := range keys {
		t.Buckets = append(t.Buckets, *buckets[k])
	}
	t.Deposit = Deposit(t.Gross, s.DepositBP, s.DepositCents)
	return t, nil
}

// Deposit is gross.PercentBP(bp) or the fixed amount capped at gross (0 when neither is set or gross <= 0).
func Deposit(gross money.Cents, bp *int64, fixed *money.Cents) money.Cents {
	if gross <= 0 {
		return 0
	}
	switch {
	case fixed != nil:
		return min(max(*fixed, 0), gross)
	case bp != nil:
		return gross.PercentBP(min(max(*bp, 0), 10_000))
	}
	return 0
}
