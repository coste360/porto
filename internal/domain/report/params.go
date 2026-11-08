package report

import (
	"net/url"
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
)

// Query parameter enumerations (OpenAPI).
var (
	mapKinds        = []string{"water", "yard", "shed", "parking", "boxes", "other"}
	families        = []string{calc.FamilyBerth, calc.FamilyLand, calc.FamilyLinear, calc.FamilySurface}
	resourceTypes   = []string{calc.TypeBerth, calc.TypeArea, calc.TypeLand}
	channels        = []string{"desk", "online", "api", "external"}
	purposes        = []string{"transit", "contract"}
	contractKinds   = []string{"mooring", "transit", "storage", "charter"}
	chargeOrigins   = []string{"installment", "holder_installment", "transit", "contract_trueup", "counter_sale", "termination", "deposit_retention", "manual", "adjustment", "opening_balance", "utility", "work_order", "rental"}
	paymentStatuses = []string{"open", "partially_paid", "paid", "cancelled"}
	invoicingStates = []string{"to_invoice", "exported", "invoiced"}
	fiscalModes     = []string{"invoice", "receipt"}
	paymentMethods  = []string{"cash", "card_pos", "bank_transfer", "cheque", "other", "card_online", "wallet", "sepa_debit", "credit_note"}
	langs           = []string{"it", "en"}
	formats         = []string{string(FormatJSON), string(FormatCSV), string(FormatXLSX)}
	aligns          = []string{"date", "weekday"}
	granularities   = []string{"day", "week", "month"}
	compares        = []string{"previous_year", "none"}
	// M9 (m9-plan §11).
	meterKinds          = []string{"power", "water", "hours", "cycles"}
	taskStatuses        = []string{"open", "scheduled", "in_progress", "done", "cancelled"}
	consumptionStatuses = []string{"pending", "anomaly", "unassigned", "charged", "excluded"}
	elementTypes        = []string{"berth", "structure", "dynamic_area", "land_resource", "pedestal", "equipment"}
)

var occupancyGroups = []string{"resource", "structure", "group", "category", "map", "month", "week", "night"}

var dimParams = []string{"map_id", "map_kind", "structure_id", "group_id", "category"}

// reportSpec is what a report accepts: its parameters (besides from, to, format, lang, page, page_size) and the
// values of group_by and section (first section = default).
type reportSpec struct {
	params   []string
	groupBy  []string
	sections []string
}

var specs = map[Key]reportSpec{
	calc.KeyOccupancy:  {params: append([]string{"variant", "family", "group_by", "resource_type"}, dimParams...), groupBy: occupancyGroups},
	calc.KeyEfficiency: {params: append([]string{"family", "group_by", "resource_type"}, dimParams...), groupBy: occupancyGroups},
	calc.KeyAllocationWaste: {
		params:  append([]string{"threshold_bp", "suggestions", "group_by"}, dimParams...),
		groupBy: []string{"structure", "group", "category", "map", "kind"},
	},
	calc.KeyLostDemand: {
		params:  []string{"group_by", "channel", "purpose"},
		groupBy: []string{"reason", "month", "week", "length_band", "channel", "source", "purpose"},
	},
	calc.KeyCustomers: {
		params: []string{"group_by", "new", "section"}, groupBy: []string{"category", "nationality", "seniority", "channel", "value_band"},
		sections: []string{"customers", "renewals"},
	},
	calc.KeyProduction: {params: []string{"group_by", "kind", "customer_id"}, groupBy: []string{"kind", "term", "month", "category", "channel", "customer"}},
	calc.KeyAccruals: {
		params: []string{"group_by", "customer_id", "section"}, groupBy: []string{"section", "month", "customer", "origin"},
		sections: []string{"competence", "deferral", "accrual"},
	},
	calc.KeyCharges: {
		params:  []string{"basis", "group_by", "customer_id", "item_id", "origin", "payment_status", "invoicing_status", "fiscal_mode"},
		groupBy: []string{"origin", "customer", "item", "revenue_class", "payment_status", "invoicing_status", "month", "vat_rate"},
	},
	calc.KeyPayments: {
		params: []string{"group_by", "method", "customer_id", "section"}, groupBy: []string{"method", "provider", "source", "month", "customer"},
		sections: []string{"main", "payouts"},
	},
	calc.KeyRevenueClasses: {params: []string{"basis"}},
	calc.KeyReceivables: {
		params: []string{"as_of", "overdue", "group_by", "customer_id", "origin", "section"}, groupBy: []string{"bucket", "customer", "origin"},
		sections: []string{"open", "future", "reminders"},
	},
	calc.KeyPresences: {
		params: []string{"group_by", "section"}, groupBy: []string{"day", "week", "month", "contract_kind"},
		sections: []string{"movements", "absences", "releases"},
	},
	calc.KeyRights: {
		params: []string{"group_by", "section"}, groupBy: []string{"item", "holder", "run_kind", "month"},
		sections: []string{"holders", "charges", "allotments", "mandates"},
	},
	calc.KeyCash:         {params: []string{"group_by", "section"}, groupBy: []string{"register", "day", "month", "vat_rate", "method"}, sections: []string{"sessions", "fiscal"}},
	calc.KeyVessels:      {params: append([]string{"group_by"}, dimParams...), groupBy: []string{"hull_type", "length_band", "flag", "kind"}},
	calc.KeyBerthRevenue: {params: append([]string{"group_by"}, dimParams...), groupBy: []string{"structure", "group", "category", "map"}},
	calc.KeyStorage: {
		params:  []string{"group_by", "map_id", "map_kind", "area_id", "exits_blocked_only"},
		groupBy: []string{"area", "map_kind", "contract_kind", "month"},
	},
	calc.KeyMaintenance: {
		params:  []string{"group_by", "status", "element_type", "overdue_only"},
		groupBy: []string{"status", "priority", "element_type", "intervention", "month"},
	},
	calc.KeyUtilities: {
		params:  []string{"group_by", "kind", "status", "meter_id", "contract_id"},
		groupBy: []string{"kind", "meter", "status", "contract", "month"},
	},
}

func (sp reportSpec) accepts(name string) bool { return slices.Contains(sp.params, name) }

// filterErr is the *FilterError of a parameter.
func filterErr(name string) error { return &FilterError{Param: name} }

type parser struct {
	q   url.Values
	err error
}

func (p *parser) str(name string, maxLen int) string {
	v := p.q.Get(name)
	if len(v) > maxLen && p.err == nil {
		p.err = filterErr(name)
	}
	return v
}

func (p *parser) enum(name string, allowed []string) string {
	v := p.q.Get(name)
	if v != "" && !slices.Contains(allowed, v) && p.err == nil {
		p.err = filterErr(name)
	}
	return v
}

func (p *parser) boolean(name string) (bool, bool) {
	v := p.q.Get(name)
	switch v {
	case "":
		return false, false
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	if p.err == nil {
		p.err = filterErr(name)
	}
	return false, false
}

func (p *parser) integer(name string, lo, hi int64) (int64, bool) {
	v := p.q.Get(name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < lo || n > hi {
		if p.err == nil {
			p.err = filterErr(name)
		}
		return 0, false
	}
	return n, true
}

func (p *parser) date(name string) calendar.Date {
	v := p.q.Get(name)
	if v == "" {
		return calendar.Date{}
	}
	d, err := calendar.Parse(v)
	if err != nil && p.err == nil {
		p.err = ErrInvalidPeriod
	}
	return d
}

func (p *parser) period() (calendar.Date, calendar.Date) {
	from, to := p.date("from"), p.date("to")
	if p.err == nil {
		p.err = checkPeriod(from, to)
	}
	return from, to
}

// checkPeriod validates a period whose bounds are both set (inclusive, at most MaxPeriodDays).
func checkPeriod(from, to calendar.Date) error {
	if from.IsZero() || to.IsZero() {
		return nil
	}
	if to.Before(from) || from.DaysUntil(to)+1 > MaxPeriodDays {
		return ErrInvalidPeriod
	}
	return nil
}

func (p *parser) dims() Dims {
	return Dims{
		MapID: p.str("map_id", 36), MapKind: p.enum("map_kind", mapKinds), StructureID: p.str("structure_id", 36),
		GroupID: p.str("group_id", 36), Category: p.str("category", 64),
	}
}

// ParseParams parses and validates the query string of GET .../reports/{key} (m7-plan §3, §6.3): invalid dates or
// period → ErrInvalidPeriod; an unknown enumerated value → *FilterError. Unknown parameter names are ignored;
// known parameters the report does not use are kept (the result lists them in ignored_filters) and validated
// only by their type.
func ParseParams(key Key, q url.Values) (Params, error) {
	sp, ok := specs[key]
	if !ok {
		return Params{}, filterErr("report")
	}
	p := &parser{q: q}
	out := Params{Report: key}
	out.From, out.To = p.period()
	out.Format = Format(p.enum("format", formats))
	out.Lang = p.enum("lang", langs)
	if n, ok := p.integer("page", 1, 1_000_000); ok {
		out.Page = int(n)
	}
	if n, ok := p.integer("page_size", 1, MaxPageSize); ok {
		out.PageSize = int(n)
	}
	if sp.accepts("group_by") {
		out.GroupBy = p.enum("group_by", sp.groupBy)
	} else {
		out.GroupBy = p.str("group_by", 32)
	}
	if sp.accepts("section") {
		out.Section = p.enum("section", sp.sections)
	} else {
		out.Section = p.str("section", 32)
	}
	out.Dims = p.dims()
	out.Variant = p.enum("variant", []string{"physical", "contractual"})
	out.Family = p.enum("family", families)
	out.ResourceType = p.enum("resource_type", resourceTypes)
	if n, ok := p.integer("threshold_bp", 0, 10_000); ok {
		out.ThresholdBP = &n
	}
	if v, ok := p.boolean("suggestions"); ok && !v {
		out.NoSuggestions = true
	}
	out.Channel = p.enum("channel", channels)
	out.Purpose = p.enum("purpose", purposes)
	out.NewOnly, _ = p.boolean("new")
	if key == calc.KeyUtilities {
		out.Kind = p.enum("kind", meterKinds)
	} else {
		out.Kind = p.enum("kind", contractKinds)
	}
	switch key {
	case calc.KeyMaintenance:
		out.Status = p.enum("status", taskStatuses)
	case calc.KeyUtilities:
		out.Status = p.enum("status", consumptionStatuses)
	default:
		out.Status = p.str("status", 32)
	}
	out.AreaID = p.str("area_id", 36)
	out.ExitsBlockedOnly, _ = p.boolean("exits_blocked_only")
	out.ElementType = p.enum("element_type", elementTypes)
	out.TaskOverdueOnly, _ = p.boolean("overdue_only")
	out.MeterID = p.str("meter_id", 36)
	out.ContractID = p.str("contract_id", 36)
	out.CustomerID = p.str("customer_id", 36)
	out.ItemID = p.str("item_id", 36)
	if key == calc.KeyCharges {
		for _, o := range q["origin"] {
			if !slices.Contains(chargeOrigins, o) && p.err == nil {
				p.err = filterErr("origin")
			}
		}
		if len(q["origin"]) > len(chargeOrigins) && p.err == nil {
			p.err = filterErr("origin")
		}
		if len(q["origin"]) > 0 {
			out.Origins = slices.Clone(q["origin"])
		}
	} else {
		if len(q["origin"]) > 1 && p.err == nil {
			p.err = filterErr("origin")
		}
		out.Origin = p.enum("origin", chargeOrigins)
	}
	out.PaymentStatus = p.enum("payment_status", paymentStatuses)
	out.InvoicingStatus = p.enum("invoicing_status", invoicingStates)
	out.FiscalMode = p.enum("fiscal_mode", fiscalModes)
	out.Basis = p.enum("basis", []string{"issued", "competence"})
	out.Method = p.enum("method", paymentMethods)
	out.AsOf = p.date("as_of")
	out.OverdueOnly, _ = p.boolean("overdue")
	if p.err != nil {
		return Params{}, p.err
	}
	return out, nil
}

// Values is the inverse of ParseParams: the canonical query string of p (from/to included when set, defaults
// omitted). Tile drill parameters are Values() without from and to.
func (p Params) Values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	setDate := func(k string, d calendar.Date) {
		if !d.IsZero() {
			v.Set(k, d.String())
		}
	}
	setBool := func(k string, b bool) {
		if b {
			v.Set(k, "true")
		}
	}
	setDate("from", p.From)
	setDate("to", p.To)
	set("format", string(p.Format))
	set("lang", p.Lang)
	if p.Page > 0 {
		v.Set("page", strconv.Itoa(p.Page))
	}
	if p.PageSize > 0 {
		v.Set("page_size", strconv.Itoa(p.PageSize))
	}
	set("group_by", p.GroupBy)
	set("section", p.Section)
	set("map_id", p.Dims.MapID)
	set("map_kind", p.Dims.MapKind)
	set("structure_id", p.Dims.StructureID)
	set("group_id", p.Dims.GroupID)
	set("category", p.Dims.Category)
	set("variant", p.Variant)
	set("family", p.Family)
	set("resource_type", p.ResourceType)
	if p.ThresholdBP != nil {
		v.Set("threshold_bp", strconv.FormatInt(*p.ThresholdBP, 10))
	}
	if p.NoSuggestions {
		v.Set("suggestions", "false")
	}
	set("channel", p.Channel)
	set("purpose", p.Purpose)
	setBool("new", p.NewOnly)
	set("kind", p.Kind)
	set("customer_id", p.CustomerID)
	set("item_id", p.ItemID)
	for _, o := range p.Origins {
		v.Add("origin", o)
	}
	set("origin", p.Origin)
	set("payment_status", p.PaymentStatus)
	set("invoicing_status", p.InvoicingStatus)
	set("fiscal_mode", p.FiscalMode)
	set("basis", p.Basis)
	set("method", p.Method)
	setDate("as_of", p.AsOf)
	setBool("overdue", p.OverdueOnly)
	set("area_id", p.AreaID)
	setBool("exits_blocked_only", p.ExitsBlockedOnly)
	set("status", p.Status)
	set("element_type", p.ElementType)
	setBool("overdue_only", p.TaskOverdueOnly)
	set("meter_id", p.MeterID)
	set("contract_id", p.ContractID)
	return v
}

// filters returns the applied filters (every set parameter but paging and format) and the ones the report
// ignores.
func (p Params) filters() (map[string]string, []string) {
	v := p.Values()
	for _, k := range []string{"format", "lang", "page", "page_size"} {
		v.Del(k)
	}
	sp := specs[p.Report]
	out := map[string]string{}
	var ignored []string
	for k := range v {
		if k == "from" || k == "to" {
			continue
		}
		if !sp.accepts(k) {
			ignored = append(ignored, k)
			continue
		}
		out[k] = joinValues(v[k])
	}
	slices.Sort(ignored)
	return out, ignored
}

func joinValues(vs []string) string {
	s := ""
	for i, x := range vs {
		if i > 0 {
			s += ","
		}
		s += x
	}
	return s
}

// ParseDashboardParams parses the query string of GET .../dashboard.
func ParseDashboardParams(q url.Values) (DashboardParams, error) {
	p := &parser{q: q}
	out := DashboardParams{}
	out.From, out.To = p.period()
	out.Compare = p.enum("compare", compares)
	out.Align = p.enum("align", aligns)
	out.Dims = p.dims()
	if p.err != nil {
		return DashboardParams{}, p.err
	}
	return out, nil
}

// comparableIndicators are the indicators of GET .../reports/comparison (m7-plan §5).
var comparableIndicators = []string{
	calc.IndProduction, calc.IndCharged, calc.IndCollected, calc.IndOccupancy, calc.IndEfficiency, calc.IndBerthRevenuePerM,
	calc.IndTransitAvgNight, calc.IndNewCustomers, calc.IndRenewalRate, calc.IndArrivals, calc.IndDepartures,
	calc.IndLostDemandCount, calc.IndLostDemandValue,
}

// ParseComparisonParams parses the query string of GET .../reports/comparison (unknown indicator:
// ErrUnknownIndicator).
func ParseComparisonParams(q url.Values) (ComparisonParams, error) {
	p := &parser{q: q}
	out := ComparisonParams{Indicator: q.Get("indicator")}
	out.From, out.To = p.period()
	if n, ok := p.integer("years_back", 1, MaxYearsBack); ok {
		out.YearsBack = int(n)
	}
	out.Align = p.enum("align", aligns)
	out.Granularity = p.enum("granularity", granularities)
	out.Format = Format(p.enum("format", formats))
	out.Lang = p.enum("lang", langs)
	out.Dims = p.dims()
	if p.err != nil {
		return ComparisonParams{}, p.err
	}
	if !slices.Contains(comparableIndicators, out.Indicator) {
		return ComparisonParams{}, ErrUnknownIndicator
	}
	return out, nil
}
