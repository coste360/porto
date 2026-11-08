package report

import (
	"context"
	"net/url"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/align"
	"github.com/coste360/porto/internal/domain/report/calc"
)

// tileSpec is a dashboard tile or comparison indicator: the report and the parameters its drill link carries
// (m7-plan §4). The tile value is the Totals of exactly that report call.
type tileSpec struct {
	key       string
	report    Key
	drill     func() Params
	needAll   bool
	dims      bool // the dashboard dimension filters apply
	noPrev    bool // receivables: current residuals only (D13)
	noSeries  bool // not additive by day
	seam      string
	breakdown func(any) []calc.Breakdown
	// period overrides the dashboard period (storage_exits_blocked: the next 30 days); its drill keeps from/to.
	period func(today calendar.Date) (calendar.Date, calendar.Date)
}

// nextDays is the period of the next n days from today (today included).
func nextDays(n int) func(calendar.Date) (calendar.Date, calendar.Date) {
	return func(today calendar.Date) (calendar.Date, calendar.Date) { return today, today.AddDays(n - 1) }
}

var tileSpecs = []tileSpec{
	{key: calc.IndProduction, report: calc.KeyProduction, drill: func() Params { return Params{} }, needAll: true},
	{key: calc.IndCharged, report: calc.KeyCharges, drill: func() Params { return Params{Basis: "issued"} }, needAll: true},
	{key: calc.IndCollected, report: calc.KeyPayments, drill: func() Params { return Params{} }, needAll: true},
	{key: calc.IndReceivablesOpen, report: calc.KeyReceivables, drill: func() Params { return Params{} }, needAll: true, noPrev: true, noSeries: true},
	{key: calc.IndReceivablesOver, report: calc.KeyReceivables, drill: func() Params { return Params{OverdueOnly: true} }, needAll: true, noPrev: true, noSeries: true},
	{key: calc.IndOccupancy, report: calc.KeyOccupancy, drill: func() Params {
		return Params{Variant: "physical", Family: calc.FamilyBerth, Dims: Dims{MapKind: "water"}}
	}, dims: true},
	{key: calc.IndEfficiency, report: calc.KeyEfficiency, drill: func() Params {
		return Params{Family: calc.FamilyBerth, Dims: Dims{MapKind: "water"}}
	}, dims: true},
	{key: calc.IndBerthRevenuePerM, report: calc.KeyBerthRevenue, drill: func() Params { return Params{Dims: Dims{MapKind: "water"}} }, needAll: true, dims: true},
	{key: calc.IndTransitAvgNight, report: calc.KeyProduction, drill: func() Params { return Params{Kind: calc.ContractTransit} }, needAll: true},
	{
		key: calc.IndNewCustomers, report: calc.KeyCustomers, drill: func() Params { return Params{NewOnly: true} }, needAll: true,
		breakdown: func(res any) []calc.Breakdown {
			r, _ := res.(*CustomersResult)
			recs := r.Records
			out := calc.BreakdownOf(recs, "nationality", func(r calc.CustomersRecord) string { return orNone(r.Nationality) }, 5)
			return append(out, calc.BreakdownOf(recs, "channel", func(r calc.CustomersRecord) string { return orNone(r.AcquisitionChannel) }, 0)...)
		},
	},
	{key: calc.IndRenewalRate, report: calc.KeyCustomers, drill: func() Params { return Params{Section: "renewals"} }, needAll: true},
	{
		key: calc.IndVesselsHosted, report: calc.KeyVessels, drill: func() Params { return Params{} }, noSeries: true,
		breakdown: func(res any) []calc.Breakdown {
			r, _ := res.(*VesselsResult)
			recs := r.Records
			out := calc.BreakdownOf(recs, "hull_type", func(r calc.VesselsRecord) string { return r.HullType }, 0)
			out = append(out, calc.BreakdownOf(recs, "length_band", func(r calc.VesselsRecord) string { return r.LengthBand }, 0)...)
			return append(out, calc.BreakdownOf(recs, "flag", func(r calc.VesselsRecord) string { return orNone(r.Flag) }, 5)...)
		},
	},
	{key: calc.IndVesselsAvgLOA, report: calc.KeyVessels, drill: func() Params { return Params{} }, noSeries: true},
	// M9 (m9-plan §11): the occupancy of yard and shed surfaces, the blocked exits of the next 30 days, the overdue
	// maintenance tasks and the unbilled utility consumptions (value at the current price).
	{key: calc.IndStorageOccupancy, report: calc.KeyStorage, drill: func() Params { return Params{} }},
	{
		key: calc.IndStorageExitsBlocked, report: calc.KeyStorage, drill: func() Params { return Params{ExitsBlockedOnly: true} },
		noPrev: true, noSeries: true, period: nextDays(30),
	},
	{
		key: calc.IndMaintenanceOverdue, report: calc.KeyMaintenance, drill: func() Params { return Params{TaskOverdueOnly: true} },
		noPrev: true, noSeries: true,
	},
	{
		key: calc.IndUtilityUnbilled, report: calc.KeyUtilities, drill: func() Params { return Params{Status: "pending"} },
		needAll: true, noPrev: true, noSeries: true,
	},
	{key: string(calc.TileYardOpenOrders), seam: calc.ReasonMilestoneM10},
	{key: string(calc.TileYardHours), seam: calc.ReasonMilestoneM10},
	{key: string(calc.TileYardMargin), seam: calc.ReasonMilestoneM10},
	{key: string(calc.TileRentalUtilisation), seam: calc.ReasonMilestoneM11},
	{key: string(calc.TileRentalRevenue), seam: calc.ReasonMilestoneM11},
}

// comparisonSpecs are the extra comparison indicators (not tiles).
var comparisonSpecs = []tileSpec{
	{key: calc.IndArrivals, report: calc.KeyPresences, drill: func() Params { return Params{} }},
	{key: calc.IndDepartures, report: calc.KeyPresences, drill: func() Params { return Params{} }},
	{key: calc.IndLostDemandCount, report: calc.KeyLostDemand, drill: func() Params { return Params{} }},
	{key: calc.IndLostDemandValue, report: calc.KeyLostDemand, drill: func() Params { return Params{} }, needAll: true},
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func specOf(key string) (tileSpec, bool) {
	for _, list := range [][]tileSpec{tileSpecs, comparisonSpecs} {
		for _, t := range list {
			if t.key == key && t.seam == "" {
				return t, true
			}
		}
	}
	return tileSpec{}, false
}

// ParamsOf returns the drill parameters of a tile with the dashboard dimension filters applied (only for the
// tiles they apply to): the same Params a report request with Drill.Params parses to.
func ParamsOf(t tileSpec, dims Dims) Params {
	q := t.drill()
	q.Report = t.report
	if t.dims {
		if dims.MapID != "" {
			q.Dims.MapID = dims.MapID
		}
		if dims.MapKind != "" {
			q.Dims.MapKind = dims.MapKind
		}
		if dims.StructureID != "" {
			q.Dims.StructureID = dims.StructureID
		}
		if dims.GroupID != "" {
			q.Dims.GroupID = dims.GroupID
		}
		if dims.Category != "" {
			q.Dims.Category = dims.Category
		}
	}
	return q
}

// drillOf is the drill of a tile: its parameters without from and to.
func drillOf(q Params) *calc.Drill {
	v := q.Values()
	v.Del("from")
	v.Del("to")
	m := map[string]string{}
	for k := range v {
		m[k] = joinValues(v[k])
	}
	return &calc.Drill{Report: q.Report, Params: m}
}

// drillWithPeriod is the drill of a tile with its own period: its parameters with from and to.
func drillWithPeriod(q Params) *calc.Drill {
	v := q.Values()
	m := map[string]string{}
	for k := range v {
		m[k] = joinValues(v[k])
	}
	return &calc.Drill{Report: q.Report, Params: m}
}

// tileRun is a report run for a tile: the result (for breakdowns), its totals and its daily facts.
type tileRun struct {
	res    any
	totals any
	facts  calc.Facts
}

// runReport runs the report of q (from/to resolved) and memoises it by its canonical query.
func (r *run) runReport(q Params, memo map[string]*tileRun) (*tileRun, error) {
	k := string(q.Report) + "?" + q.Values().Encode()
	if t, ok := memo[k]; ok {
		return t, nil
	}
	t := &tileRun{}
	var err error
	switch q.Report {
	case calc.KeyProduction:
		var x *ProductionResult
		x, t.facts, err = r.runProduction(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyCharges:
		var x *ChargesResult
		x, t.facts, err = r.runCharges(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyPayments:
		var x *PaymentsResult
		x, t.facts, err = r.runPayments(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyReceivables:
		if q.AsOf.IsZero() {
			q.AsOf = r.today
		}
		var x *ReceivablesResult
		x, err = r.runReceivables(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyOccupancy:
		var x *OccupancyResult
		x, t.facts, err = r.runOccupancy(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyEfficiency:
		var x *EfficiencyResult
		x, t.facts, err = r.runEfficiency(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyBerthRevenue:
		var x *BerthRevenueResult
		x, t.facts, err = r.runBerthRevenue(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyCustomers:
		var x *CustomersResult
		x, t.facts, err = r.runCustomers(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyVessels:
		var x *VesselsResult
		x, t.facts, err = r.runVessels(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyPresences:
		var x *PresencesResult
		x, t.facts, err = r.runPresences(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyStorage:
		var x *StorageResult
		x, t.facts, err = r.runStorage(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyMaintenance:
		var x *MaintenanceResult
		x, t.facts, err = r.runMaintenance(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyUtilities:
		var x *UtilitiesResult
		x, t.facts, err = r.runUtilities(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	case calc.KeyLostDemand:
		var x *LostDemandResult
		x, t.facts, err = r.runLostDemand(q)
		if x != nil {
			t.res, t.totals = x, x.Totals
		}
	default:
		return nil, ErrUnknownIndicator
	}
	if err != nil {
		return nil, err
	}
	memo[k] = t
	return t, nil
}

// Dashboard returns the tiles of the manager dashboard the principal may see (reports.operational or reports.all;
// neither: auth.ErrForbidden), each with the previous period, trend, series and drill (m7-plan §4).
func (s *Service) Dashboard(ctx context.Context, p *auth.Principal, marinaID string, q DashboardParams) (*Dashboard, error) {
	acc, err := s.authorize(ctx, p, marinaID, false)
	if err != nil {
		return nil, err
	}
	if err := checkPeriod(q.From, q.To); err != nil {
		return nil, err
	}
	out := &Dashboard{Compare: q.Compare, Align: q.Align}
	if out.Compare == "" {
		out.Compare = "previous_year"
	}
	if out.Align == "" {
		out.Align = align.ByDate
	}
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		from, to, err := r.period(q.From, q.To)
		if err != nil {
			return err
		}
		out.From, out.To, out.Today = from, to, r.today
		out.Granularity = align.DefaultGranularity(from, to)
		out.Filters = dimFilters(q.Dims)
		withPrev := out.Compare != "none"
		var pf, pt calendar.Date
		if withPrev {
			pf, pt = align.Previous(from, to, 1, out.Align)
			out.PrevFrom, out.PrevTo = pf, pt
		}
		memo := map[string]*tileRun{}
		for _, t := range tileSpecs {
			if t.seam != "" {
				out.Tiles = append(out.Tiles, calc.Tile{
					Key: calc.TileKey(t.key), Unit: calc.UnitCount, Reason: t.seam,
					Trend: "none", Series: []calc.SeriesPoint{}, Breakdown: []calc.Breakdown{},
				})
				continue
			}
			if t.needAll && !acc.Full {
				continue
			}
			tile, err := r.tile(t, q.Dims, from, to, pf, pt, withPrev && !t.noPrev, out.Align, out.Granularity, memo)
			if err != nil {
				return err
			}
			out.Tiles = append(out.Tiles, tile)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func dimFilters(d Dims) map[string]string {
	v := url.Values{}
	for k, x := range map[string]string{"map_id": d.MapID, "map_kind": d.MapKind, "structure_id": d.StructureID, "group_id": d.GroupID, "category": d.Category} {
		if x != "" {
			v.Set(k, x)
		}
	}
	m := map[string]string{}
	for k := range v {
		m[k] = v.Get(k)
	}
	return m
}

// tile builds one tile from the run of its drill report on the period and, with a comparison, on the previous
// period.
func (r *run) tile(t tileSpec, dims Dims, from, to, pf, pt calendar.Date, withPrev bool, alignment, granularity string,
	memo map[string]*tileRun,
) (calc.Tile, error) {
	q := ParamsOf(t, dims)
	ind := calc.Indicators[t.key]
	drill := drillOf(q)
	if t.period != nil {
		from, to = t.period(r.today)
		withPrev = false
		q.From, q.To = from, to
		drill = drillWithPeriod(q)
	}
	tile := calc.Tile{
		Key: calc.TileKey(t.key), Unit: ind.Unit, Available: true, DimsApplied: t.dims, Drill: drill,
		Trend: "none", Series: []calc.SeriesPoint{}, Breakdown: []calc.Breakdown{},
	}
	cq := q
	cq.From, cq.To = from, to
	cur, err := r.runReport(cq, memo)
	if err != nil {
		return tile, err
	}
	tile.Current = calc.TileValueOf(t.key, cur.totals)
	var prev *tileRun
	if withPrev {
		pq := q
		pq.From, pq.To = pf, pt
		if prev, err = r.runReport(pq, memo); err != nil {
			return tile, err
		}
		pv := calc.TileValueOf(t.key, prev.totals)
		tile.Previous = &pv
	}
	tile.Delta, tile.DeltaBP, tile.Trend = calc.Delta(tile.Current, tile.Previous)
	if !t.noSeries {
		var pd calc.Daily
		var constDen [2]int64
		constDen[0] = tile.Current.Denominator
		if prev != nil {
			pd = prev.facts[t.key]
			constDen[1] = tile.Previous.Denominator
		}
		pts := calc.Aligned(t.key, cur.facts[t.key], pd, from, to, 1, alignment, granularity, constDen)
		tile.Series = calc.SeriesOf(pts, prev != nil)
	}
	if t.breakdown != nil {
		tile.Breakdown = t.breakdown(cur.res)
	}
	return tile, nil
}

// Compare returns an indicator compared with q.YearsBack previous periods aligned by date or weekday (R11,
// m7-plan §5); the permission is the one of the indicator's report.
func (s *Service) Compare(ctx context.Context, p *auth.Principal, marinaID string, q ComparisonParams) (*Comparison, error) {
	if _, err := s.authorize(ctx, p, marinaID, false); err != nil {
		return nil, err
	}
	t, ok := specOf(q.Indicator)
	if !ok || t.noSeries {
		return nil, ErrUnknownIndicator
	}
	acc, err := s.authorize(ctx, p, marinaID, t.needAll)
	if err != nil {
		return nil, err
	}
	if err := checkPeriod(q.From, q.To); err != nil {
		return nil, err
	}
	k := q.YearsBack
	if k <= 0 {
		k = 1
	}
	if k > MaxYearsBack {
		return nil, filterErr("years_back")
	}
	out := &Comparison{Indicator: q.Indicator, Unit: calc.Indicators[t.key].Unit, Align: q.Align, Granularity: q.Granularity, YearsBack: k}
	if out.Align == "" {
		out.Align = align.ByDate
	}
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		from, to, err := r.period(q.From, q.To)
		if err != nil {
			return err
		}
		if out.Granularity == "" {
			out.Granularity = align.DefaultGranularity(from, to)
		}
		dq := ParamsOf(t, q.Dims)
		out.Filters = drillOf(dq).Params
		memo := map[string]*tileRun{}
		cq := dq
		cq.From, cq.To = from, to
		cur, err := r.runReport(cq, memo)
		if err != nil {
			return err
		}
		pf, pt := align.Previous(from, to, k, out.Align)
		pq := dq
		pq.From, pq.To = pf, pt
		prev, err := r.runReport(pq, memo)
		if err != nil {
			return err
		}
		cv, pv := calc.TileValueOf(t.key, cur.totals), calc.TileValueOf(t.key, prev.totals)
		out.Current = calc.ComparisonPeriod{From: from, To: to, Value: cv.Value, Numerator: cv.Numerator, Denominator: cv.Denominator, HasFraction: cv.HasFraction}
		out.Previous = calc.ComparisonPeriod{From: pf, To: pt, Value: pv.Value, Numerator: pv.Numerator, Denominator: pv.Denominator, HasFraction: pv.HasFraction}
		out.Delta, out.DeltaBP, _ = calc.Delta(cv, &pv)
		out.Series = calc.Aligned(t.key, cur.facts[t.key], prev.facts[t.key], from, to, k, out.Align, out.Granularity,
			[2]int64{cv.Denominator, pv.Denominator})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
