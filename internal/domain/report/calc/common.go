package calc

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/align"
	"github.com/coste360/porto/internal/money"
)

// Scale factors of the fraction indicators: bp for ratios, 100 for cents per metre (cents × 100 / cm), 1 for the
// averages (cents per night, cm).
const (
	ScaleBP     int64 = 10_000
	ScalePerM   int64 = 100
	ScaleAvg    int64 = 1
	maxOverflow       = int64(1) << 62
)

// Ratio is num / den in basis points, half-even; not OK when den is 0 (D3).
func Ratio(num, den int64) BP {
	v, ok := scaled(num, den, ScaleBP)
	return BP{Value: v, OK: ok}
}

// Scaled is num × scale / den half-even (128-bit intermediate); not OK when den is 0.
func Scaled(num, den, scale int64) Opt[int64] {
	v, ok := scaled(num, den, scale)
	return Opt[int64]{Value: v, OK: ok}
}

func scaled(num, den, scale int64) (int64, bool) {
	if den == 0 {
		return 0, false
	}
	v, err := money.Cents(num).MulRatChecked(scale, den)
	if err != nil {
		return 0, false
	}
	return int64(v), true
}

// Some returns an OK optional.
func Some[T ~int64](v T) Opt[T] { return Opt[T]{Value: v, OK: true} }

// None is a hidden or unknown optional.
func None[T ~int64]() Opt[T] { return Opt[T]{} }

// Dims are the dimension filters of a report (m7-plan §3.0); empty means all.
type Dims struct {
	MapID, MapKind, StructureID, GroupID, Category string
}

// Fact is the contribution of one day to an indicator: Num, and Den for the fractions.
type Fact struct {
	Num, Den int64
}

// Daily are the facts of an indicator by day (m7-plan §5): Σ daily == the tile, by construction.
type Daily map[calendar.Date]Fact

// Add adds a contribution to a day.
func (d Daily) Add(day calendar.Date, num, den int64) {
	f := d[day]
	f.Num += num
	f.Den += den
	d[day] = f
}

// Sum returns the sum of every day.
func (d Daily) Sum() Fact {
	var s Fact
	for _, f := range d {
		s.Num += f.Num
		s.Den += f.Den
	}
	return s
}

// Indicator describes how a tile value is computed from its numerator and denominator.
type Indicator struct {
	Unit     Unit
	Fraction bool  // value = Num × Scale / Den; else value = Num
	Scale    int64 // ScaleBP, ScalePerM, ScaleAvg
	ConstDen bool  // the denominator is not additive by day (it is the same for every bucket: total length)
}

// Value returns the value of a fact for the indicator.
func (ind Indicator) Value(f Fact) TileValue {
	if !ind.Fraction {
		return TileValue{Value: Some(f.Num)}
	}
	return TileValue{Value: Scaled(f.Num, f.Den, ind.Scale), Numerator: f.Num, Denominator: f.Den, HasFraction: true}
}

// PageOf returns page p (1-based) of size n of recs (n ≤ 0: every record) and the page info.
func PageOf[R any](recs []R, page, size int) ([]R, Page) {
	total := int64(len(recs))
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		return recs, Page{Page: 1, PageSize: len(recs), Total: total}
	}
	lo := (page - 1) * size
	if lo >= len(recs) {
		return recs[:0], Page{Page: page, PageSize: size, Total: total}
	}
	hi := min(lo+size, len(recs))
	return recs[lo:hi], Page{Page: page, PageSize: size, Total: total}
}

// grouper accumulates records into groups by key, keeping first-seen labels; Groups sorts by key.
type grouper[R any] struct {
	order  []string
	labels map[string]string
	recs   map[string][]R
}

func newGrouper[R any]() *grouper[R] {
	return &grouper[R]{labels: map[string]string{}, recs: map[string][]R{}}
}

func (g *grouper[R]) add(key, label string, r R) {
	if _, ok := g.recs[key]; !ok {
		g.order = append(g.order, key)
		g.labels[key] = label
		g.recs[key] = nil
	}
	g.recs[key] = append(g.recs[key], r)
}

// groupsOf folds every group with fold, in key order.
func groupsOf[R, T any](g *grouper[R], fold func([]R) T) []Group[T] {
	keys := slices.Clone(g.order)
	slices.Sort(keys)
	out := make([]Group[T], 0, len(keys))
	for _, k := range keys {
		out = append(out, Group[T]{Key: k, Label: g.labels[k], Totals: fold(g.recs[k])})
	}
	return out
}

// GroupRecords groups records by the keys keyOf returns (a record may belong to several groups, or none) and
// folds each group with fold.
func GroupRecords[R, T any](recs []R, keyOf func(R) []string, fold func([]R) T) []Group[T] {
	g := newGrouper[R]()
	for _, r := range recs {
		for _, k := range keyOf(r) {
			g.add(k, k, r)
		}
	}
	return groupsOf(g, fold)
}

// MonthKey is YYYY-MM.
func MonthKey(d calendar.Date) string {
	return strconv.Itoa(d.Year()) + "-" + twoDigits(int(d.Month()))
}

// WeekKey is the Monday of the ISO week (YYYY-MM-DD).
func WeekKey(d calendar.Date) string { return align.Bucket(d, align.Week).String() }

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// overlapDays returns the number of days of [a, b] ∩ [from, to] (0 when disjoint).
func overlapDays(a, b, from, to calendar.Date) int64 {
	lo, hi := a, b
	if from.After(lo) {
		lo = from
	}
	if to.Before(hi) {
		hi = to
	}
	if hi.Before(lo) {
		return 0
	}
	return int64(lo.DaysUntil(hi) + 1)
}

// inPeriod reports whether d is inside [from, to].
func inPeriod(d, from, to calendar.Date) bool { return !d.Before(from) && !d.After(to) }

// cmpDate orders dates (zero first).
func cmpDate(a, b calendar.Date) int { return a.Compare(b) }

// LengthBand is the LOA band key of a length in cm (D8).
func LengthBand(loa int64) string {
	switch {
	case loa < 800:
		return "lt8"
	case loa < 1000:
		return "m8_10"
	case loa < 1200:
		return "m10_12"
	case loa < 1500:
		return "m12_15"
	case loa < 2000:
		return "m15_20"
	case loa < 2500:
		return "m20_25"
	default:
		return "ge25"
	}
}

// compareKeys is a multi-key string comparison helper.
func compareKeys(a, b []string) int {
	for i, x := range a {
		if i >= len(b) {
			break
		}
		if c := cmp.Compare(x, b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// monthOf is the first day of the month of d.
func monthOf(d calendar.Date) calendar.Date { return calendar.New(d.Year(), d.Month(), 1) }

// Indicator keys: the dashboard tiles and the comparison indicators (OpenAPI TileKey, ComparisonIndicator).
const (
	IndProduction       = "production"
	IndCharged          = "charged"
	IndCollected        = "collected"
	IndReceivablesOpen  = "receivables_open"
	IndReceivablesOver  = "receivables_overdue"
	IndOccupancy        = "occupancy"
	IndEfficiency       = "efficiency"
	IndBerthRevenuePerM = "berth_revenue_per_m"
	IndTransitAvgNight  = "transit_avg_night"
	IndNewCustomers     = "new_customers"
	IndRenewalRate      = "renewal_rate"
	IndVesselsHosted    = "vessels_hosted"
	IndVesselsAvgLOA    = "vessels_avg_loa"
	IndArrivals         = "arrivals"
	IndDepartures       = "departures"
	IndLostDemandCount  = "lost_demand_count"
	IndLostDemandValue  = "lost_demand_value"
)

// Facts are the daily facts of the indicators a report computes, by indicator key.
type Facts map[string]Daily

// Indicators describes every indicator (unit and how its value derives from Σ num / Σ den).
var Indicators = map[string]Indicator{
	IndProduction:       {Unit: UnitCents},
	IndCharged:          {Unit: UnitCents},
	IndCollected:        {Unit: UnitCents},
	IndReceivablesOpen:  {Unit: UnitCents},
	IndReceivablesOver:  {Unit: UnitCents},
	IndOccupancy:        {Unit: UnitBP, Fraction: true, Scale: ScaleBP},
	IndEfficiency:       {Unit: UnitBP, Fraction: true, Scale: ScaleBP},
	IndBerthRevenuePerM: {Unit: UnitCentsPerM, Fraction: true, Scale: ScalePerM, ConstDen: true},
	IndTransitAvgNight:  {Unit: UnitCentsPerNight, Fraction: true, Scale: ScaleAvg},
	IndNewCustomers:     {Unit: UnitCount},
	IndRenewalRate:      {Unit: UnitBP, Fraction: true, Scale: ScaleBP},
	IndVesselsHosted:    {Unit: UnitCount},
	IndVesselsAvgLOA:    {Unit: UnitCm, Fraction: true, Scale: ScaleAvg},
	IndArrivals:         {Unit: UnitCount},
	IndDepartures:       {Unit: UnitCount},
	IndLostDemandCount:  {Unit: UnitCount},
	IndLostDemandValue:  {Unit: UnitCents},
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
