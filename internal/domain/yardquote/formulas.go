package yardquote

import (
	"context"
	"regexp"
	"slices"
	"sort"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Formula variables, band tables and the formula test (m10-plan §3.1-§3.5).

var (
	varName   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	enumValue = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	units     = []string{"m", "m2", "t", "kw", "h", "pcs", "l", "pct", "none"}
)

// ListVariables lists the built-in vessel and context variables and the marina's derived and job variables, each
// with its value on the test vessel when it has one (yard.read).
func (s *Service) ListVariables(ctx context.Context, p *auth.Principal, marinaID string) ([]VariableView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ce, err := loadCatalogEnv(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	set, err := s.SettingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	env := ce.testEnv(testVesselOf(set.TestVessel), s.today(ctx, db, scope).Year())
	testValue := func(name string) *Value {
		if v, ok := env.Vars[name]; ok {
			x := price.FromValue(v)
			return &x
		}
		return nil
	}
	out := make([]VariableView, 0, len(builtins)+len(ce.vars))
	for _, b := range builtins {
		en := b.En
		out = append(out, VariableView{
			Name: b.Name, Source: b.Source, Kind: b.Kind, Unit: b.Unit, Label: I18n{It: b.It, En: &en}, Builtin: true, Enum: b.Enum,
			TestValue: testValue(b.Name),
		})
	}
	names := make([]string, 0, len(ce.vars))
	for n := range ce.vars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := ce.vars[n]
		out = append(out, variableView(v, testValue(n)))
	}
	return out, nil
}

func variableView(v store.FormulaVariable, test *Value) VariableView {
	row := v
	return VariableView{
		Row: &row, Name: v.Name, Source: v.Source, Kind: v.Kind, Unit: v.Unit, Label: i18nOf(v.Label), Enum: v.EnumValues, TestValue: test,
	}
}

func int64Ptr(m *qty.Milli) *int64 {
	if m == nil {
		return nil
	}
	v := int64(*m)
	return &v
}

// checkVariable validates the fields of a variable row (not its expression).
func checkVariable(v *auth.ValidationError, r *store.FormulaVariable) {
	if !contains(units, r.Unit) {
		v.Add("unit", "invalid")
	}
	if r.Source == store.FormulaSourceDerived {
		if r.Expression == "" {
			v.Add("expression", "required")
		}
		if r.MinMilli != nil || r.MaxMilli != nil || r.DefaultMilli != nil || r.DefaultEnum != "" || len(r.EnumValues) > 0 {
			v.Add("expression", "derived_has_input_fields")
		}
		if r.Kind == store.FormulaKindEnum {
			v.Add("kind", "invalid")
		}
		return
	}
	if r.Expression != "" {
		v.Add("expression", "job_has_expression")
	}
	switch r.Kind {
	case store.FormulaKindNumber:
		if r.MinMilli != nil && r.MaxMilli != nil && *r.MaxMilli < *r.MinMilli {
			v.Add("max_milli", "invalid")
		}
		if d := r.DefaultMilli; d != nil && ((r.MinMilli != nil && *d < *r.MinMilli) || (r.MaxMilli != nil && *d > *r.MaxMilli)) {
			v.Add("default_milli", formula.CodeOutOfRange)
		}
		if len(r.EnumValues) > 0 || r.DefaultEnum != "" {
			v.Add("enum_values", "invalid")
		}
	case store.FormulaKindBool:
		if r.DefaultMilli != nil && *r.DefaultMilli != 0 && *r.DefaultMilli != 1000 {
			v.Add("default_milli", "invalid")
		}
		if r.MinMilli != nil || r.MaxMilli != nil || len(r.EnumValues) > 0 || r.DefaultEnum != "" {
			v.Add("kind", "invalid")
		}
	case store.FormulaKindEnum:
		if len(r.EnumValues) == 0 || len(r.EnumValues) > 32 {
			v.Add("enum_values", "invalid")
		}
		seen := map[string]bool{}
		for _, e := range r.EnumValues {
			if !enumValue.MatchString(e) || seen[e] {
				v.Add("enum_values", "invalid")
			}
			seen[e] = true
		}
		if r.DefaultEnum != "" && !seen[r.DefaultEnum] {
			v.Add("default_enum", "enum_value_unknown")
		}
		if r.MinMilli != nil || r.MaxMilli != nil || r.DefaultMilli != nil {
			v.Add("kind", "invalid")
		}
	default:
		v.Add("kind", "invalid")
	}
}

// saveVariable validates a variable in the context of the catalogue (its expression on the test vessel and every
// dependent package and item formula), then writes it.
func (s *Service) saveVariable(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, r *store.FormulaVariable, expected int) error {
	ce, err := loadCatalogEnv(ctx, tx, scope)
	if err != nil {
		return err
	}
	ce.setVar(*r)
	val, err := s.newValidator(ctx, tx, scope, ce)
	if err != nil {
		return err
	}
	if r.Source == store.FormulaSourceDerived {
		val.derived("expression", r.Name, r.Expression, kindOf(r.Kind))
		if err := val.err(); err != nil {
			return err
		}
		val.skip = r.Name
	}
	if err := s.revalidate(ctx, tx, scope, val); err != nil {
		return err
	}
	if err := val.err(); err != nil {
		return err
	}
	action := "formula_variable.update"
	if expected == 0 {
		action = "formula_variable.create"
		err = store.InsertFormulaVariable(ctx, tx, scope, r)
	} else {
		err = store.UpdateFormulaVariable(ctx, tx, scope, r, expected, s.now())
	}
	if err != nil {
		return writeErr(err)
	}
	return s.catalogChanged(ctx, tx, scope, p.Actor(), action, "variable", r.ID, map[string]any{
		"name": r.Name, "source": r.Source, "kind": r.Kind, "expression": r.Expression,
	})
}

// CreateVariable creates a derived or job variable (yard.manage).
func (s *Service) CreateVariable(ctx context.Context, p *auth.Principal, marinaID string, in VariableInput) (*VariableView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !varName.MatchString(in.Name) || isReserved(in.Name) {
		v.Add("name", "invalid")
	}
	if in.Source != store.FormulaSourceDerived && in.Source != store.FormulaSourceJob {
		v.Add("source", "invalid")
	}
	unit := in.Unit
	if unit == "" {
		unit = "none"
	}
	label := I18n{It: in.Name}
	if in.Label != nil {
		label = checkI18n(v, "label", *in.Label, 100)
	}
	now := s.now()
	r := &store.FormulaVariable{
		ID: store.NewID(), Name: in.Name, Source: in.Source, Kind: in.Kind, Unit: unit, Expression: in.Expression,
		MinMilli: int64Ptr(in.Min), MaxMilli: int64Ptr(in.Max), DefaultMilli: int64Ptr(in.Default), DefaultEnum: in.DefaultEnum,
		EnumValues: store.Strings(in.EnumValues), Label: i18nJSON(label), CreatedAt: now, UpdatedAt: now,
	}
	if r.EnumValues == nil {
		r.EnumValues = store.Strings{}
	}
	checkVariable(v, r)
	if err := v.Err(); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return s.saveVariable(ctx, tx, scope, p, r, 0)
	})
	if err != nil {
		return nil, err
	}
	out := variableView(*r, nil)
	return &out, nil
}

// UpdateVariable changes a variable (yard.manage, If-Match); dependents are re-validated (D6).
func (s *Service) UpdateVariable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in VariablePatch) (*VariableView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var out VariableView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetFormulaVariable(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if in.Unit != nil {
			r.Unit = *in.Unit
		}
		if in.Expression != nil {
			r.Expression = *in.Expression
		}
		if in.Min.Set {
			r.MinMilli = int64Ptr(in.Min.V)
		}
		if in.Max.Set {
			r.MaxMilli = int64Ptr(in.Max.V)
		}
		if in.Default.Set {
			r.DefaultMilli = int64Ptr(in.Default.V)
		}
		if in.DefaultEnum.Set {
			r.DefaultEnum = in.DefaultEnum.V
		}
		if in.EnumValues != nil {
			r.EnumValues = store.Strings(*in.EnumValues)
		}
		if in.Label != nil {
			r.Label = i18nJSON(checkI18n(v, "label", *in.Label, 100))
		}
		checkVariable(v, r)
		if err := v.Err(); err != nil {
			return err
		}
		if err := s.saveVariable(ctx, tx, scope, p, r, ifMatch); err != nil {
			return err
		}
		out = variableView(*r, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// references returns the packages (codes), items (codes) and derived variables whose current formulas reference
// name, as a variable or as a table.
func (s *Service) references(ctx context.Context, db bun.IDB, scope store.MarinaScope, ce *catalogEnv, name string, table bool) ([]string, error) {
	uses := func(src string) bool {
		pr, err := formula.Parse(src)
		if err != nil {
			return false
		}
		if table {
			return slices.Contains(pr.Tables(), name)
		}
		return slices.Contains(pr.Idents(), name)
	}
	var refs []string
	for _, d := range ce.derived {
		if d.Name != name && uses(d.Src) {
			refs = append(refs, d.Name)
		}
	}
	pkgs, err := store.AllWorkPackages(ctx, db, scope, false)
	if err != nil {
		return nil, err
	}
	for _, pk := range pkgs {
		if pk.CurrentVersion == 0 {
			continue
		}
		pv, err := store.GetWorkPackageVersion(ctx, db, scope, pk.ID, pk.CurrentVersion)
		if err != nil {
			return nil, err
		}
		used := !table && slices.Contains([]string(pv.JobVars), name)
		for _, l := range decodeLines(pv.Lines) {
			used = used || uses(l.QtyFormula) || uses(l.PriceFormula)
		}
		if used {
			refs = append(refs, pk.Code)
		}
	}
	items, err := store.AllYardItemSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if uses(it.QtyFormula) || uses(it.PriceFormula) {
			if row, err := store.GetItem(ctx, db, scope, it.ItemID); err == nil {
				refs = append(refs, row.Code)
			}
		}
	}
	return refs, nil
}

// DeleteVariable deletes a variable that nothing references (yard.manage, If-Match; 409 when in use).
func (s *Service) DeleteVariable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetFormulaVariable(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		ce, err := loadCatalogEnv(ctx, tx, scope)
		if err != nil {
			return err
		}
		refs, err := s.references(ctx, tx, scope, ce, r.Name, false)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			return ErrInUse
		}
		if err := store.DeleteFormulaVariable(ctx, tx, scope, id, ifMatch); err != nil {
			return versionErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "formula_variable.delete", "variable", id, map[string]any{"name": r.Name})
	})
}

// ---- band tables ----

func tableView(r store.FormulaTable) TableView { return TableView{Row: r, Rows: tableRows(r.BandRows)} }

// ListTables lists the band tables (yard.read).
func (s *Service) ListTables(ctx context.Context, p *auth.Principal, marinaID string) ([]TableView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListFormulaTables(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]TableView, 0, len(rows))
	for _, r := range rows {
		out = append(out, tableView(r))
	}
	return out, nil
}

// checkRows: 1..50 rows, strictly ascending up_to, only the last may be open.
func checkRows(v *auth.ValidationError, rows []formula.Row) {
	if len(rows) == 0 || len(rows) > formula.MaxTableRows {
		v.Add("rows", "invalid")
		return
	}
	for i, r := range rows {
		if r.UpTo == nil {
			if i != len(rows)-1 {
				v.Add("rows", "open_row_not_last")
			}
			continue
		}
		if i > 0 && rows[i-1].UpTo != nil && *r.UpTo <= *rows[i-1].UpTo {
			v.Add("rows", "not_ascending")
		}
	}
}

func (s *Service) saveTable(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, r *store.FormulaTable, rows []formula.Row, expected int) error {
	ce, err := loadCatalogEnv(ctx, tx, scope)
	if err != nil {
		return err
	}
	ce.tables[r.Name] = formula.Table{Name: r.Name, Rows: rows}
	val, err := s.newValidator(ctx, tx, scope, ce)
	if err != nil {
		return err
	}
	if err := s.revalidate(ctx, tx, scope, val); err != nil {
		return err
	}
	if err := val.err(); err != nil {
		return err
	}
	action := "formula_table.update"
	if expected == 0 {
		action = "formula_table.create"
		err = store.InsertFormulaTable(ctx, tx, scope, r)
	} else {
		err = store.UpdateFormulaTable(ctx, tx, scope, r, expected, s.now())
	}
	if err != nil {
		return writeErr(err)
	}
	return s.catalogChanged(ctx, tx, scope, p.Actor(), action, "table", r.ID, map[string]any{"name": r.Name, "rows": len(rows)})
}

// CreateTable creates a band table (yard.manage).
func (s *Service) CreateTable(ctx context.Context, p *auth.Principal, marinaID string, in TableInput) (*TableView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !varName.MatchString(in.Name) || isReserved(in.Name) {
		v.Add("name", "invalid")
	}
	checkRows(v, in.Rows)
	label := I18n{It: in.Name}
	if in.Label != nil {
		label = checkI18n(v, "label", *in.Label, 100)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	r := &store.FormulaTable{ID: store.NewID(), Name: in.Name, Label: i18nJSON(label), BandRows: rowsJSON(in.Rows), CreatedAt: now, UpdatedAt: now}
	if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return s.saveTable(ctx, tx, scope, p, r, in.Rows, 0)
	}); err != nil {
		return nil, err
	}
	out := tableView(*r)
	return &out, nil
}

// UpdateTable changes a band table (yard.manage, If-Match); dependents are re-validated (D6).
func (s *Service) UpdateTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in TablePatch) (*TableView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var out TableView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetFormulaTable(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		rows := tableRows(r.BandRows)
		if in.Rows != nil {
			rows = *in.Rows
			checkRows(v, rows)
			r.BandRows = rowsJSON(rows)
		}
		if in.Label != nil {
			r.Label = i18nJSON(checkI18n(v, "label", *in.Label, 100))
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := s.saveTable(ctx, tx, scope, p, r, rows, ifMatch); err != nil {
			return err
		}
		out = tableView(*r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- formula test ----

// ValidateFormula parses, checks and evaluates a formula on the test vessel or a vessel of the marina, with job
// defaults and the given values (yard.read, nothing written).
func (s *Service) ValidateFormula(ctx context.Context, p *auth.Principal, marinaID string, in ValidateInput) (*ValidateResult, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ce, err := loadCatalogEnv(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	val, err := s.newValidator(ctx, db, scope, ce)
	if err != nil {
		return nil, err
	}
	if in.VesselID != "" {
		vs, err := store.GetVessel(ctx, db, scope, in.VesselID)
		if err != nil {
			return nil, notFound(err)
		}
		env := formula.Env{Vars: map[string]formula.Value{}, Tables: ce.tables}
		for k, x := range vesselVars(vesselDataOf(vs), s.today(ctx, db, scope).Year()) {
			env.Vars[k] = price.ToValue(x)
		}
		for k, x := range val.env.Vars {
			if d, ok := ce.vars[k]; ok && d.Source == store.FormulaSourceJob {
				env.Vars[k] = x
			}
		}
		ce.addDerived(&env)
		val.env = env
	}
	purpose := in.Purpose
	if purpose == "" {
		purpose = PurposeQuantity
	}
	res := &ValidateResult{Issues: []Issue{}, Idents: []string{}}
	if pr, err := formula.Parse(in.Formula); err == nil {
		res.Idents = pr.Idents()
	}
	want, context := formula.KindNumber, []string(nil)
	switch purpose {
	case PurposeBoolean:
		want = formula.KindBool
	case PurposePrice:
		context = []string{formula.VarQty, formula.VarListPrice}
	case PurposeQuantity, PurposeDerived:
	default:
		v := &auth.ValidationError{}
		v.Add("purpose", "invalid")
		return nil, v
	}
	prog := val.check("formula", in.Formula, want, context)
	if prog != nil {
		extra := map[string]formula.Value{}
		for k, x := range in.Vars {
			extra[k] = price.ToValue(x)
		}
		if purpose == PurposePrice {
			q := qty.One
			if in.Qty != nil {
				q = *in.Qty
			}
			extra[formula.VarQty] = formula.Number(q)
			if _, ok := extra[formula.VarListPrice]; !ok && in.ItemID != "" {
				if c := centsPtr(s.listPrices(ctx, db, scope, []string{in.ItemID}), in.ItemID); c != nil {
					extra[formula.VarListPrice], _ = formula.EuroValue(*c)
				}
			}
		}
		x, err := prog.Eval(formula.Env{Vars: mergeVars(val.env.Vars, extra), Tables: val.env.Tables})
		if err != nil {
			val.add(issueFrom(err, "formula"))
		} else {
			fv := price.FromValue(x)
			res.Value = &fv
			switch purpose {
			case PurposeQuantity:
				if q, err := formula.Quantity(x); err != nil {
					val.add(issueFrom(err, "formula"))
				} else {
					res.Qty = &q
				}
			case PurposePrice:
				if c, err := formula.PriceCents(x); err != nil {
					val.add(issueFrom(err, "formula"))
				} else {
					res.Price = &c
				}
			}
		}
	}
	res.Issues = append(res.Issues, val.issues...)
	res.Valid = len(res.Issues) == 0
	return res, nil
}

func mergeVars(a, b map[string]formula.Value) map[string]formula.Value {
	out := make(map[string]formula.Value, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
