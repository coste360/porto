package yardquote

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// builtin is a built-in variable (vessel or context, m10-plan §3.3).
type builtin struct {
	Name, Source, Kind, Unit string
	It, En                   string
	Enum                     []string
}

// builtins are the vessel variables (read from the vessel) and the context variables of price formulas.
var builtins = []builtin{
	{"loa", formula.SourceVessel, store.FormulaKindNumber, "m", "Lunghezza fuori tutto", "Length overall", nil},
	{"beam", formula.SourceVessel, store.FormulaKindNumber, "m", "Larghezza", "Beam", nil},
	{"draft", formula.SourceVessel, store.FormulaKindNumber, "m", "Pescaggio", "Draft", nil},
	{"air_draft", formula.SourceVessel, store.FormulaKindNumber, "m", "Altezza fuori acqua", "Air draft", nil},
	{"lwl", formula.SourceVessel, store.FormulaKindNumber, "m", "Lunghezza al galleggiamento", "Waterline length", nil},
	{"displacement", formula.SourceVessel, store.FormulaKindNumber, "t", "Dislocamento", "Displacement", nil},
	{"engines", formula.SourceVessel, store.FormulaKindNumber, "pcs", "Numero motori", "Engines", nil},
	{"kw_total", formula.SourceVessel, store.FormulaKindNumber, "kw", "Potenza totale", "Total power", nil},
	{"hull_type", formula.SourceVessel, store.FormulaKindEnum, "none", "Tipo di scafo", "Hull type", hullTypes},
	{"material", formula.SourceVessel, store.FormulaKindEnum, "none", "Materiale dello scafo", "Hull material", nil},
	{"age", formula.SourceVessel, store.FormulaKindNumber, "years", "Età", "Age", nil},
	{formula.VarQty, formula.SourceContext, store.FormulaKindNumber, "none", "Quantità della riga", "Line quantity", nil},
	{formula.VarListPrice, formula.SourceContext, store.FormulaKindNumber, "none", "Prezzo di listino (euro)", "List price (euro)", nil},
}

func isBuiltin(name string) bool {
	for _, b := range builtins {
		if b.Name == name {
			return true
		}
	}
	return false
}

// reservedNames cannot be used for marina variables or tables (keywords, functions, built-ins).
var reservedNames = []string{"and", "or", "not", "true", "false", "if", "min", "max", "round", "ceil", "floor", "band"}

func isReserved(name string) bool {
	for _, r := range reservedNames {
		if r == name {
			return true
		}
	}
	return isBuiltin(name)
}

func kindOf(k string) formula.Kind {
	switch k {
	case store.FormulaKindBool:
		return formula.KindBool
	case store.FormulaKindEnum:
		return formula.KindEnum
	}
	return formula.KindNumber
}

func milliPtr(p *int64) *qty.Milli {
	if p == nil {
		return nil
	}
	v := qty.Milli(*p)
	return &v
}

// catalogEnv is the formula context of a marina: declarations, derived definitions and band tables.
type catalogEnv struct {
	decls   map[string]formula.VarDecl
	vars    map[string]store.FormulaVariable
	derived []formula.Derived
	tables  map[string]formula.Table
	trows   map[string]store.FormulaTable
}

func tableRows(j store.JSON) []formula.Row {
	var raw []struct {
		UpTo  *int64 `json:"up_to_milli"`
		Value int64  `json:"value_milli"`
	}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &raw)
	}
	out := make([]formula.Row, 0, len(raw))
	for _, r := range raw {
		out = append(out, formula.Row{UpTo: milliPtr(r.UpTo), Value: qty.Milli(r.Value)})
	}
	return out
}

func rowsJSON(rows []formula.Row) store.JSON {
	type row struct {
		UpTo  *int64 `json:"up_to_milli"`
		Value int64  `json:"value_milli"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		var up *int64
		if r.UpTo != nil {
			v := int64(*r.UpTo)
			up = &v
		}
		out = append(out, row{UpTo: up, Value: int64(r.Value)})
	}
	return store.MustJSON(out)
}

// loadCatalogEnv reads the variables and tables of the marina.
func loadCatalogEnv(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*catalogEnv, error) {
	vars, err := store.ListFormulaVariables(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	tables, err := store.ListFormulaTables(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	ce := &catalogEnv{
		decls: map[string]formula.VarDecl{}, vars: map[string]store.FormulaVariable{}, tables: map[string]formula.Table{},
		trows: map[string]store.FormulaTable{},
	}
	for _, b := range builtins {
		if b.Source == formula.SourceVessel {
			ce.decls[b.Name] = formula.VarDecl{Name: b.Name, Kind: kindOf(b.Kind), Unit: b.Unit, Source: b.Source, Enum: b.Enum}
		}
	}
	for _, v := range vars {
		ce.setVar(v)
	}
	for _, t := range tables {
		ce.tables[t.Name] = formula.Table{Name: t.Name, Rows: tableRows(t.BandRows)}
		ce.trows[t.Name] = t
	}
	return ce, nil
}

// setVar adds or replaces a stored variable in the context.
func (ce *catalogEnv) setVar(v store.FormulaVariable) {
	ce.vars[v.Name] = v
	ce.decls[v.Name] = formula.VarDecl{
		Name: v.Name, Kind: kindOf(v.Kind), Unit: v.Unit, Source: v.Source, Min: milliPtr(v.MinMilli), Max: milliPtr(v.MaxMilli),
		Default: milliPtr(v.DefaultMilli), Enum: v.EnumValues,
	}
	ce.rebuildDerived()
}

func (ce *catalogEnv) rebuildDerived() {
	ce.derived = ce.derived[:0]
	names := make([]string, 0, len(ce.vars))
	for n := range ce.vars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if v := ce.vars[n]; v.Source == store.FormulaSourceDerived {
			ce.derived = append(ce.derived, formula.Derived{Name: n, Src: v.Expression})
		}
	}
}

// jobDefault is the default value of a job variable (nil when it has none).
func jobDefault(v store.FormulaVariable) *Value {
	switch v.Kind {
	case store.FormulaKindEnum:
		if v.DefaultEnum != "" {
			return &Value{Kind: price.KindEnum, Enum: v.DefaultEnum}
		}
	case store.FormulaKindBool:
		if v.DefaultMilli != nil {
			return &Value{Kind: price.KindBool, Bool: *v.DefaultMilli != 0}
		}
	default:
		if v.DefaultMilli != nil {
			return &Value{Kind: price.KindNumber, Num: qty.Milli(*v.DefaultMilli)}
		}
	}
	return nil
}

func numVal(m qty.Milli) Value { return Value{Kind: price.KindNumber, Num: m} }

// foldMaterial lower-cases a hull material and folds it to [a-z0-9_] (m10-plan §3.3).
func foldMaterial(s string) string {
	var b strings.Builder
	last := byte('_')
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		c := byte('_')
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			c = byte(r)
		case strings.ContainsRune("àáâä", r):
			c = 'a'
		case strings.ContainsRune("èéêë", r):
			c = 'e'
		case strings.ContainsRune("ìíîï", r):
			c = 'i'
		case strings.ContainsRune("òóôö", r):
			c = 'o'
		case strings.ContainsRune("ùúûü", r):
			c = 'u'
		}
		if c == '_' && last == '_' {
			continue
		}
		b.WriteByte(c)
		last = c
	}
	out := strings.TrimRight(b.String(), "_")
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// vesselData is what the vessel variables are computed from.
type vesselData struct {
	LOA, Beam, Draft, AirDraft, LWL int64 // effective cm, 0 = unknown
	DisplacementKg                  int64
	EnginesKW                       []int64
	EnginesCount                    int
	HullType, Material              string
	YearBuilt                       int
}

func vesselDataOf(v *store.Vessel) vesselData {
	d := vesselData{
		LOA: v.LOACm, Beam: v.BeamCm, Draft: v.DraftCm, AirDraft: v.AirDraftCm, LWL: v.LWLCm, DisplacementKg: v.DisplacementKg,
		HullType: v.HullType, Material: v.HullMaterial, YearBuilt: int(v.YearBuilt),
	}
	if v.DimensionsSource == "measured" {
		for _, m := range []struct {
			dst *int64
			v   int64
		}{
			{&d.LOA, v.MeasuredLOACm}, {&d.Beam, v.MeasuredBeamCm}, {&d.Draft, v.MeasuredDraftCm}, {&d.AirDraft, v.MeasuredAirDraftCm},
		} {
			if m.v > 0 {
				*m.dst = m.v
			}
		}
	}
	var engines []struct {
		PowerKW *int64 `json:"power_kw"`
	}
	if len(v.Engines) > 0 {
		_ = json.Unmarshal(v.Engines, &engines)
	}
	d.EnginesCount = len(engines)
	for _, e := range engines {
		if e.PowerKW != nil {
			d.EnginesKW = append(d.EnginesKW, *e.PowerKW)
		}
	}
	return d
}

func testVesselData(tv TestVessel) vesselData {
	return vesselData{
		LOA: tv.LOACm, Beam: tv.BeamCm, Draft: tv.DraftCm, AirDraft: tv.AirDraftCm, LWL: tv.LWLCm, DisplacementKg: tv.DisplacementKg,
		EnginesKW: tv.EnginesKW, EnginesCount: len(tv.EnginesKW), HullType: tv.HullType, Material: tv.HullMaterial,
		YearBuilt: tv.YearBuilt,
	}
}

// vesselVars are the vessel variables with a value (unknown dimensions are absent: they become step-3 inputs).
func vesselVars(d vesselData, year int) map[string]Value {
	out := map[string]Value{}
	for name, cm := range map[string]int64{"loa": d.LOA, "beam": d.Beam, "draft": d.Draft, "air_draft": d.AirDraft, "lwl": d.LWL} {
		if cm > 0 {
			out[name] = numVal(qty.Milli(cm * 10))
		}
	}
	if d.DisplacementKg > 0 {
		out["displacement"] = numVal(qty.Milli(d.DisplacementKg))
	}
	out["engines"] = numVal(qty.Milli(int64(d.EnginesCount) * 1000))
	var kw int64
	for _, k := range d.EnginesKW {
		kw += k
	}
	out["kw_total"] = numVal(qty.Milli(kw * 1000))
	ht := d.HullType
	if ht == "" {
		ht = "other"
	}
	out["hull_type"] = Value{Kind: price.KindEnum, Enum: ht}
	out["material"] = Value{Kind: price.KindEnum, Enum: foldMaterial(d.Material)}
	if d.YearBuilt > 0 {
		age := year - d.YearBuilt
		if age < 0 {
			age = 0
		}
		out["age"] = numVal(qty.Milli(int64(age) * 1000))
	}
	return out
}

// testEnv is the evaluation environment of the save validation: the test vessel, the derived variables that can
// be computed on it and the job defaults (m10-plan §3.5).
func (ce *catalogEnv) testEnv(tv TestVessel, year int) formula.Env {
	env := formula.Env{Vars: map[string]formula.Value{}, Tables: ce.tables}
	for k, v := range vesselVars(testVesselData(tv), year) {
		env.Vars[k] = price.ToValue(v)
	}
	ce.addDerived(&env)
	for _, v := range ce.vars {
		if v.Source != store.FormulaSourceJob {
			continue
		}
		if d := jobDefault(v); d != nil {
			env.Vars[v.Name] = price.ToValue(*d)
		}
	}
	return env
}

// addDerived evaluates the derived variables that can be computed in env (dependencies first).
func (ce *catalogEnv) addDerived(env *formula.Env) {
	order, err := formula.DerivedOrder(ce.derived)
	if err != nil {
		return
	}
	src := map[string]string{}
	for _, d := range ce.derived {
		src[d.Name] = d.Src
	}
	for _, n := range order {
		p, err := formula.Parse(src[n])
		if err != nil {
			continue
		}
		if v, err := p.Eval(*env); err == nil {
			env.Vars[n] = v
		}
	}
}

// closure returns the variable names a set of formulas needs, expanding derived variables into what they read.
func (ce *catalogEnv) closure(srcs []string) (vars, tables map[string]bool) {
	vars, tables = map[string]bool{}, map[string]bool{}
	var visit func(src string)
	seen := map[string]bool{}
	visit = func(src string) {
		p, err := formula.Parse(src)
		if err != nil {
			return
		}
		for _, t := range p.Tables() {
			tables[t] = true
		}
		for _, id := range p.Idents() {
			vars[id] = true
			if v, ok := ce.vars[id]; ok && v.Source == store.FormulaSourceDerived && !seen[id] {
				seen[id] = true
				visit(v.Expression)
			}
		}
	}
	for _, s := range srcs {
		if s != "" {
			visit(s)
		}
	}
	return vars, tables
}
