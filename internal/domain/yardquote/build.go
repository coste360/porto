package yardquote

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Snapshot building (m10-plan §3.6, §4.2): from the step 1-4 inputs and the current catalogue to a snapshot that
// price.Compute turns into lines. Everything a recompute needs is copied into the snapshot (INV-28).

// buildInput are the inputs of a snapshot.
type buildInput struct {
	CustomerID      string
	VesselID        string
	Selections      []Selection
	JobInputs       map[string]Value
	VesselOverrides map[string]Value
	Overrides       []price.Override
	ManualLines     []ManualLine
	QuotedOn        calendar.Date
	DepositBP       *int64
	DepositCents    *int64
	AllowInactive   bool // a re-pricing keeps packages deactivated after the quote was made
}

// built is a computed snapshot.
type built struct {
	snap   price.Snapshot
	lines  []price.Line
	totals price.Totals
	items  map[string]store.Item
	locale string
}

func validationErr(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

// lineKey identifies a line across re-pricings (overrides follow their line, not its position).
func lineKey(l price.LineSpec, manualIndex int) string {
	switch l.Source {
	case price.SourceManual:
		return "manual|" + strconv.Itoa(manualIndex)
	case price.SourcePackage:
		return fmt.Sprintf("package|%s|%d|%s", l.PackageID, l.PackageLineSeq, l.ItemID)
	}
	return "item|" + l.ItemID
}

func lineKeys(specs []price.LineSpec) map[int]string {
	out := map[int]string{}
	mi := 0
	for _, l := range specs {
		out[l.Seq] = lineKey(l, mi)
		if l.Source == price.SourceManual {
			mi++
		}
	}
	return out
}

// remapOverrides moves the overrides of old lines to the seq of the same line in the new specs; overrides whose
// line is gone are dropped.
func remapOverrides(overrides []price.Override, oldSpecs, newSpecs []price.LineSpec) []price.Override {
	oldKeys := lineKeys(oldSpecs)
	newSeq := map[string]int{}
	for seq, k := range lineKeys(newSpecs) {
		newSeq[k] = seq
	}
	out := []price.Override{}
	for _, o := range overrides {
		if seq, ok := newSeq[oldKeys[o.Seq]]; ok {
			o.Seq = seq
			out = append(out, o)
		}
	}
	return out
}

// manualLinesOf rebuilds the manual lines of a snapshot (their discount lives in the override of their seq).
func manualLinesOf(snap price.Snapshot) []ManualLine {
	disc := map[int]money.Cents{}
	for _, o := range snap.Overrides {
		if o.Discount != nil {
			disc[o.Seq] = *o.Discount
		}
	}
	var out []ManualLine
	for _, l := range snap.Lines {
		if l.Source != price.SourceManual || l.ManualQty == nil || l.ManualUnit == nil {
			continue
		}
		out = append(out, ManualLine{
			ItemID: l.ItemID, WorkTypeID: l.WorkTypeID, Description: l.Description, Qty: *l.ManualQty, Unit: *l.ManualUnit,
			Discount: disc[l.Seq],
		})
	}
	return out
}

// checkValue verifies the kind of a value.
func checkValue(v Value, kind string) bool {
	switch kind {
	case store.FormulaKindBool:
		return v.Kind == price.KindBool
	case store.FormulaKindEnum:
		return v.Kind == price.KindEnum
	}
	return v.Kind == price.KindNumber
}

// jobInputIssues checks the job inputs against the job variables of the quote: known name, kind, range, enum.
func jobInputIssues(vars []price.JobVar, in map[string]Value) []Issue {
	byName := map[string]price.JobVar{}
	for _, v := range vars {
		byName[v.Name] = v
	}
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	var issues []Issue
	for _, n := range names {
		x := in[n]
		field := "job_inputs." + n
		jv, ok := byName[n]
		if !ok {
			issues = append(issues, Issue{Code: formula.CodeUnknownVariable, Field: field, Ref: n})
			continue
		}
		if !checkValue(x, jv.Kind) {
			issues = append(issues, Issue{Code: formula.CodeTypeMismatch, Field: field, Ref: n})
			continue
		}
		if jv.Kind == store.FormulaKindNumber && ((jv.Min != nil && x.Num < *jv.Min) || (jv.Max != nil && x.Num > *jv.Max)) {
			issues = append(issues, Issue{Code: formula.CodeOutOfRange, Field: field, Ref: n})
		}
		if jv.Kind == store.FormulaKindEnum && len(jv.Enum) > 0 && !contains(jv.Enum, x.Enum) {
			issues = append(issues, Issue{Code: formula.CodeEnumValueUnknown, Field: field, Ref: n})
		}
	}
	return issues
}

// vesselOverrideIssues checks the vessel overrides: built-in vessel variables of the right kind.
func vesselOverrideIssues(in map[string]Value) []Issue {
	var issues []Issue
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		field := "vessel_overrides." + n
		var b *builtin
		for i := range builtins {
			if builtins[i].Name == n && builtins[i].Source == formula.SourceVessel {
				b = &builtins[i]
			}
		}
		switch {
		case b == nil:
			issues = append(issues, Issue{Code: formula.CodeUnknownVariable, Field: field, Ref: n})
		case !checkValue(in[n], b.Kind):
			issues = append(issues, Issue{Code: formula.CodeTypeMismatch, Field: field, Ref: n})
		case b.Kind == store.FormulaKindNumber && in[n].Num < 0:
			issues = append(issues, Issue{Code: formula.CodeOutOfRange, Field: field, Ref: n})
		case len(b.Enum) > 0 && !contains(b.Enum, in[n].Enum):
			issues = append(issues, Issue{Code: formula.CodeEnumValueUnknown, Field: field, Ref: n})
		}
	}
	return issues
}

func i18nMap(t I18n) map[string]string {
	m := map[string]string{"it": t.It}
	if t.En != nil {
		m["en"] = *t.En
	}
	return m
}

func i18nFromMap(m map[string]string) I18n {
	t := I18n{It: m["it"]}
	if en, ok := m["en"]; ok && en != "" {
		t.En = &en
	}
	return t
}

// build computes the snapshot of a quote input with the current catalogue and price lists.
func (s *Service) build(ctx context.Context, db bun.IDB, scope store.MarinaScope, in buildInput) (*built, error) {
	v := &auth.ValidationError{}
	cust, err := store.GetCustomer(ctx, db, scope, in.CustomerID)
	if err != nil {
		v.Add("customer_id", "not_found")
	}
	vessel, err := store.GetVessel(ctx, db, scope, in.VesselID)
	if err != nil {
		v.Add("vessel_id", "not_found")
	}
	if len(in.Selections)+len(in.ManualLines) == 0 {
		v.Add("selections", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	locale := cust.Locale
	ce, err := loadCatalogEnv(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var specs []price.LineSpec
	jobSet := map[string]bool{}
	defaultWT := ""
	labourTask := map[int]bool{} // spec index -> needs a default task title from the item
	for i, sel := range in.Selections {
		f := fmt.Sprintf("selections[%d]", i)
		switch {
		case sel.PackageID != "" && sel.ItemID == "":
			pk, err := store.GetWorkPackage(ctx, db, scope, sel.PackageID, false)
			if err != nil || pk.CurrentVersion == 0 {
				v.Add(f+".package_id", "not_found")
				continue
			}
			if !pk.Active && !in.AllowInactive {
				v.Add(f+".package_id", "inactive")
				continue
			}
			ver, err := store.GetWorkPackageVersion(ctx, db, scope, pk.ID, pk.CurrentVersion)
			if err != nil {
				return nil, err
			}
			if defaultWT == "" {
				defaultWT = pk.WorkTypeID
			}
			for _, j := range ver.JobVars {
				jobSet[j] = true
			}
			for _, l := range decodeLines(ver.Lines) {
				wt := l.WorkTypeID
				if wt == "" {
					wt = pk.WorkTypeID
				}
				spec := price.LineSpec{
					Source: price.SourcePackage, PackageID: pk.ID, PackageVersion: ver.Version, PackageLineSeq: l.Seq, ItemID: l.ItemID,
					WorkTypeID: wt, QtyFormula: l.QtyFormula, PriceFormula: l.PriceFormula,
				}
				if l.Task != nil {
					spec.Task = &price.TaskSpec{Title: i18nMap(l.Task.Title), Checklist: l.Task.Checklist}
				} else {
					labourTask[len(specs)] = true
				}
				specs = append(specs, spec)
			}
		case sel.ItemID != "" && sel.PackageID == "":
			st, err := store.GetYardItemSettings(ctx, db, scope, sel.ItemID)
			if err != nil || !st.YardEnabled {
				v.Add(f+".item_id", "not_yard_item")
				continue
			}
			labourTask[len(specs)] = true
			specs = append(specs, price.LineSpec{Source: price.SourceItem, ItemID: sel.ItemID, QtyFormula: st.QtyFormula, PriceFormula: st.PriceFormula})
		default:
			v.Add(f, "invalid")
		}
	}
	for i, m := range in.ManualLines {
		f := fmt.Sprintf("manual_lines[%d]", i)
		if m.Qty < 0 || m.Unit < 0 || m.Discount < 0 {
			v.Add(f, "invalid")
		}
		d, ok := cleanText(m.Description, 500)
		if !ok {
			v.Add(f+".description", "invalid")
		}
		mq, mu := m.Qty, m.Unit
		specs = append(specs, price.LineSpec{
			Source: price.SourceManual, ItemID: m.ItemID, WorkTypeID: m.WorkTypeID, Description: d, ManualQty: &mq, ManualUnit: &mu,
		})
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if defaultWT == "" {
		wts, err := store.ListWorkTypes(ctx, db, scope, nil)
		if err != nil {
			return nil, err
		}
		for _, w := range wts {
			if w.Active {
				defaultWT = w.ID
				break
			}
		}
	}
	// Items, yard settings and qualifications.
	ids := make([]string, 0, len(specs))
	for _, l := range specs {
		ids = append(ids, l.ItemID)
	}
	itemRows, err := store.ItemsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	items := map[string]store.Item{}
	for _, it := range itemRows {
		items[it.ID] = it
	}
	settings, err := store.YardItemSettingsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	quals, err := store.ListYardQualifications(ctx, db, scope, nil)
	if err != nil {
		return nil, err
	}
	qualByItem := map[string]store.YardQualification{}
	for _, q := range quals {
		qualByItem[q.LabourItemID] = q
	}
	wts, err := store.ListWorkTypes(ctx, db, scope, nil)
	if err != nil {
		return nil, err
	}
	wtIDs := map[string]bool{}
	for _, w := range wts {
		wtIDs[w.ID] = true
	}
	snapItems := map[string]price.ItemData{}
	for i := range specs {
		l := &specs[i]
		l.Seq = i + 1
		it, ok := items[l.ItemID]
		f := fmt.Sprintf("lines[%d]", i)
		if !ok || !contains(yardItemTypes, it.ItemType) {
			v.Add(f+".item_id", "not_found")
			continue
		}
		if l.WorkTypeID == "" {
			l.WorkTypeID = defaultWT
		}
		if l.WorkTypeID == "" || !wtIDs[l.WorkTypeID] {
			v.Add(f+".work_type_id", "required")
		}
		name := i18nOf(it.Name)
		if l.Description == "" {
			l.Description = localized(name, locale)
		}
		if it.ItemType == price.ItemTypeLabour && l.Task == nil && labourTask[i] {
			l.Task = &price.TaskSpec{Title: i18nMap(name)}
		}
		if _, done := snapItems[it.ID]; done {
			continue
		}
		d := price.ItemData{
			ItemID: it.ID, Code: it.Code, ItemType: it.ItemType, Unit: it.Unit,
			VAT: price.ItemVAT{RateBP: it.VATRateBP, Nature: it.VATNature, PriceIncludesVAT: it.PriceIncludesVAT},
		}
		if st, ok := settings[it.ID]; ok {
			d.CostCents = money.Cents(st.CostCents)
		}
		if q, ok := qualByItem[it.ID]; ok && it.ItemType == price.ItemTypeLabour {
			d.QualificationID, d.CostCents = q.ID, money.Cents(q.DefaultCostCents)
		}
		snapItems[it.ID] = d
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	// Formulas: variables and tables used.
	srcs := make([]string, 0, 2*len(specs))
	for _, l := range specs {
		srcs = append(srcs, l.QtyFormula, l.PriceFormula)
	}
	used, usedTables := ce.closure(srcs)
	snap := price.Snapshot{
		LanguageVersion: formula.LanguageVersion, QuotedOn: in.QuotedOn.String(), Lines: specs, Items: snapItems,
		Vessel: vesselVars(vesselDataOf(vessel), in.QuotedOn.Year()), CustomerCategory: cust.Category,
		Selections: append([]Selection{}, in.Selections...), Prices: map[string]price.UnitPrice{},
	}
	for _, d := range ce.derived {
		if used[d.Name] {
			snap.Derived = append(snap.Derived, price.DerivedSnapshot{Name: d.Name, Src: d.Src})
		}
	}
	tnames := make([]string, 0, len(usedTables))
	for t := range usedTables {
		tnames = append(tnames, t)
	}
	sort.Strings(tnames)
	for _, t := range tnames {
		ft, ok := ce.tables[t]
		if !ok {
			continue
		}
		ts := price.TableSnapshot{Name: t, Rows: []price.TableRowSnapshot{}}
		for _, r := range ft.Rows {
			ts.Rows = append(ts.Rows, price.TableRowSnapshot{UpTo: r.UpTo, Value: r.Value})
		}
		snap.Tables = append(snap.Tables, ts)
	}
	for name := range used {
		if d, ok := ce.vars[name]; ok && d.Source == store.FormulaSourceJob {
			jobSet[name] = true
		}
	}
	jnames := make([]string, 0, len(jobSet))
	for j := range jobSet {
		jnames = append(jnames, j)
	}
	sort.Strings(jnames)
	for _, j := range jnames {
		d, ok := ce.vars[j]
		if !ok {
			continue
		}
		snap.JobVars = append(snap.JobVars, price.JobVar{
			Name: d.Name, Kind: d.Kind, Unit: d.Unit, Label: i18nMap(i18nOf(d.Label)), Min: milliPtr(d.MinMilli), Max: milliPtr(d.MaxMilli),
			Default: jobDefault(d), Enum: d.EnumValues,
		})
	}
	issues := append(jobInputIssues(snap.JobVars, in.JobInputs), vesselOverrideIssues(in.VesselOverrides)...)
	if len(issues) > 0 {
		return nil, &FormulaError{Issues: issues}
	}
	snap.JobInputs = map[string]Value{}
	for _, jv := range snap.JobVars {
		if x, ok := in.JobInputs[jv.Name]; ok {
			snap.JobInputs[jv.Name] = x
		} else if jv.Default != nil {
			snap.JobInputs[jv.Name] = *jv.Default
		}
	}
	if len(in.VesselOverrides) > 0 {
		snap.VesselOverrides = map[string]Value{}
		for k, x := range in.VesselOverrides {
			snap.VesselOverrides[k] = x
		}
	}
	// Overrides (and the discounts of manual lines).
	seqs := map[int]bool{}
	for _, l := range specs {
		seqs[l.Seq] = true
	}
	for i, o := range in.Overrides {
		if !seqs[o.Seq] {
			v.Add(fmt.Sprintf("overrides[%d].seq", i), "not_found")
		}
		if (o.Qty != nil && *o.Qty < 0) || (o.Unit != nil && *o.Unit < 0) || (o.Discount != nil && *o.Discount < 0) {
			v.Add(fmt.Sprintf("overrides[%d]", i), "invalid")
		}
		if o.Description != nil {
			if _, ok := cleanText(*o.Description, 500); !ok {
				v.Add(fmt.Sprintf("overrides[%d].description", i), "invalid")
			}
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	snap.Overrides = append([]price.Override{}, in.Overrides...)
	mi := 0
	for _, l := range specs {
		if l.Source != price.SourceManual {
			continue
		}
		if d := in.ManualLines[mi].Discount; d > 0 {
			dd := d
			snap.Overrides = append(snap.Overrides, price.Override{Seq: l.Seq, Discount: &dd})
		}
		mi++
	}
	if in.DepositCents != nil {
		c := money.Cents(*in.DepositCents)
		snap.DepositCents = &c
	} else if in.DepositBP != nil {
		bp := *in.DepositBP
		snap.DepositBP = &bp
	}
	// Prices.
	if s.d.Prices != nil {
		pm, err := s.d.Prices.UnitPricesTx(ctx, db, scope, ids, in.QuotedOn, cust.Category)
		if err != nil {
			return nil, err
		}
		for id, p := range pm {
			if p.Found {
				snap.Prices[id] = price.UnitPrice{
					ItemID: id, UnitCents: p.UnitCents, PriceListID: p.PriceListID, RuleID: p.RuleID, PriceIncludesVAT: p.PriceIncludesVAT, Found: true,
				}
			}
		}
	}
	b, err := computeBuilt(snap)
	if err != nil {
		return nil, err
	}
	b.items, b.locale = items, locale
	return b, nil
}

// computeBuilt prices a snapshot and fills the values of its derived variables.
func computeBuilt(snap price.Snapshot) (*built, error) {
	if env, err := price.Env(snap); err == nil {
		for i, d := range snap.Derived {
			if x, ok := env.Vars[d.Name]; ok {
				snap.Derived[i].Value = price.FromValue(x)
			} else {
				snap.Derived[i].Value = Value{}
			}
		}
	}
	lines, totals, err := price.Compute(snap)
	if err != nil {
		if errors.Is(err, price.ErrCycle) {
			return nil, &FormulaError{Issues: []Issue{{Code: formula.CodeDerivedCycle, Field: "derived"}}}
		}
		return nil, err
	}
	return &built{snap: snap, lines: lines, totals: totals}, nil
}

// asksOf lists the variables of step 3 from a snapshot: its job variables and the vessel data its formulas need
// but the vessel lacks.
func asksOf(snap price.Snapshot) []VariableAsk {
	out := []VariableAsk{}
	for _, jv := range snap.JobVars {
		a := VariableAsk{
			Name: jv.Name, Source: formula.SourceJob, Kind: jv.Kind, Unit: jv.Unit, Label: i18nFromMap(jv.Label), Min: jv.Min, Max: jv.Max,
			Default: jv.Default, Enum: jv.Enum,
		}
		if x, ok := snap.JobInputs[jv.Name]; ok {
			xx := x
			a.Value = &xx
		}
		a.Required = a.Value == nil
		out = append(out, a)
	}
	need := map[string]bool{}
	addIdents := func(src string) {
		if src == "" {
			return
		}
		if p, err := formula.Parse(src); err == nil {
			for _, id := range p.Idents() {
				need[id] = true
			}
		}
	}
	for _, l := range snap.Lines {
		addIdents(l.QtyFormula)
		addIdents(l.PriceFormula)
	}
	for _, d := range snap.Derived {
		addIdents(d.Src)
	}
	for _, b := range builtins {
		if b.Source != formula.SourceVessel || !need[b.Name] {
			continue
		}
		if _, ok := snap.Vessel[b.Name]; ok {
			continue
		}
		en := b.En
		a := VariableAsk{Name: b.Name, Source: formula.SourceVessel, Kind: b.Kind, Unit: b.Unit, Label: I18n{It: b.It, En: &en}, Enum: b.Enum}
		if x, ok := snap.VesselOverrides[b.Name]; ok {
			xx := x
			a.Value = &xx
		}
		a.Required = a.Value == nil
		out = append(out, a)
	}
	return out
}

// previewOf turns a computed snapshot into the client view.
func previewOf(snap price.Snapshot, lines []price.Line, totals price.Totals, quals map[string]store.YardQualification) Preview {
	pv := Preview{Lines: make([]LineView, 0, len(lines)), Totals: totals, Variables: asksOf(snap), Issues: []price.LineIssue{}, Blocking: totals.Blocking}
	for _, l := range lines {
		it := snap.Items[l.Spec.ItemID]
		pv.Lines = append(pv.Lines, LineView{Line: l, ItemCode: it.Code, ItemType: it.ItemType, ItemUnit: it.Unit})
		pv.Issues = append(pv.Issues, l.Issues...)
	}
	for _, a := range pv.Variables {
		if a.Required {
			pv.Blocking = true
		}
	}
	qids := make([]string, 0, len(totals.EstimatedHours))
	for q := range totals.EstimatedHours {
		qids = append(qids, q)
	}
	sort.Strings(qids)
	for _, q := range qids {
		h := totals.EstimatedHours[q]
		cost, _ := h.MulCents(money.Cents(quals[q].DefaultCostCents))
		pv.Hours = append(pv.Hours, HoursView{QualificationID: q, Hours: h, Cost: cost})
	}
	return pv
}

func (s *Service) qualMap(ctx context.Context, db bun.IDB, scope store.MarinaScope) (map[string]store.YardQualification, error) {
	qs, err := store.ListYardQualifications(ctx, db, scope, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]store.YardQualification{}
	for _, q := range qs {
		out[q.ID] = q
	}
	return out, nil
}
