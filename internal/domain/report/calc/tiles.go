package calc

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/align"
)

// TileValueOf reads the value of the tile key from the Totals of its drill report (INV-62: a tile never has its
// own code path; it is the fold of the drill-down records).
func TileValueOf(key string, totals any) TileValue {
	ind := Indicators[key]
	f := func(num, den int64) TileValue { return ind.Value(Fact{Num: num, Den: den}) }
	switch t := totals.(type) {
	case ProductionTotals:
		if key == IndTransitAvgNight {
			return f(int64(t.TransitCompetence), t.TransitNights)
		}
		return f(int64(t.Competence), 0)
	case ChargesTotals:
		return f(int64(t.Charged), 0)
	case PaymentsTotals:
		return f(int64(t.Collected), 0)
	case ReceivablesTotals:
		if key == IndReceivablesOver {
			return f(int64(t.Overdue), 0)
		}
		return f(int64(t.Open), 0)
	case OccupancyTotals:
		return f(t.OccupiedUnits, t.AvailableUnits)
	case EfficiencyTotals:
		return f(t.VesselUnits, t.ResourceUnits)
	case BerthRevenueTotals:
		return f(int64(t.Revenue), int64(t.Length))
	case CustomersTotals:
		if key == IndRenewalRate {
			return f(t.RenewalRenewed, t.RenewalEligible)
		}
		return f(t.NewCustomers, 0)
	case VesselsTotals:
		if key == IndVesselsAvgLOA {
			return f(int64(t.LOASum), t.Vessels)
		}
		return f(t.Vessels, 0)
	case PresencesTotals:
		if key == IndDepartures {
			return f(t.Departures, 0)
		}
		return f(t.Arrivals, 0)
	case StorageTotals:
		if key == IndStorageExitsBlocked {
			return f(t.ExitsBlocked, 0)
		}
		return f(t.OccupiedM2Milli, t.AvailableM2Milli)
	case MaintenanceTotals:
		return f(t.Overdue, 0)
	case UtilitiesTotals:
		return f(int64(t.PendingValue.Value), 0)
	case YardTotals:
		switch key {
		case IndYardHours:
			return f(t.HoursMilli, 0)
		case IndYardMargin:
			return f(int64(t.Margin.Value), 0)
		}
		return f(t.Records, 0)
	case RentalTotals:
		if key == IndRentalRevenue {
			return f(int64(t.Net.Value), 0)
		}
		return f(t.RentedSlots.Value, t.AvailableSlots.Value)
	case LostDemandTotals:
		if key == IndLostDemandValue {
			return f(int64(t.Value.Value), 0)
		}
		return f(t.Count, 0)
	}
	return TileValue{}
}

// Fact returns the numerator and denominator of a tile value.
func (v TileValue) Fact() Fact { return Fact{Num: v.numOrValue(), Den: v.Denominator} }

func (v TileValue) numOrValue() int64 {
	if v.HasFraction {
		return v.Numerator
	}
	return v.Value.Value
}

// Delta returns current − previous, its ratio over |previous| in bp and the trend (up, down, flat; none without
// a previous value).
func Delta(cur TileValue, prev *TileValue) (Opt[int64], BP, string) {
	if prev == nil || !cur.Value.OK || !prev.Value.OK {
		return None[int64](), BP{}, "none"
	}
	d := cur.Value.Value - prev.Value.Value
	p := prev.Value.Value
	if p < 0 {
		p = -p
	}
	trend := "flat"
	switch {
	case d > 0:
		trend = "up"
	case d < 0:
		trend = "down"
	}
	return Some(d), Ratio(d, p), trend
}

// Aligned compares the daily facts of an indicator on [from, to] with the facts of the period k years back
// (prev), mapping every previous day onto the current period by date or weekday (never by index), in buckets of
// the given granularity. constDen is the denominator of every bucket for ConstDen indicators (current, previous).
func Aligned(key string, cur, prev Daily, from, to calendar.Date, k int, alignment, granularity string, constDen [2]int64) []ComparisonPoint {
	ind := Indicators[key]
	buckets := align.Buckets(from, to, granularity)
	pos := make(map[calendar.Date]int, len(buckets))
	for i, b := range buckets {
		pos[b] = i
	}
	type nd struct{ cn, cd, pn, pd int64 }
	acc := make([]nd, len(buckets))
	for d, f := range cur {
		if !inPeriod(d, from, to) {
			continue
		}
		i := pos[align.Bucket(d, granularity)]
		acc[i].cn += f.Num
		acc[i].cd += f.Den
	}
	for p, f := range prev {
		d := align.Clamp(align.MapToCurrent(p, k, alignment), from, to)
		i := pos[align.Bucket(d, granularity)]
		acc[i].pn += f.Num
		acc[i].pd += f.Den
	}
	out := make([]ComparisonPoint, len(buckets))
	for i, b := range buckets {
		a := acc[i]
		if ind.ConstDen {
			a.cd, a.pd = constDen[0], constDen[1]
		}
		c, p := ind.Value(Fact{Num: a.cn, Den: a.cd}), ind.Value(Fact{Num: a.pn, Den: a.pd})
		out[i] = ComparisonPoint{
			Bucket: b, Current: c.Value, Previous: p.Value, CurrentNumerator: a.cn, CurrentDenominator: a.cd,
			PreviousNumerator: a.pn, PreviousDenominator: a.pd, HasFraction: ind.Fraction,
		}
	}
	return out
}

// SeriesOf turns comparison points into tile series points (previous unset without a comparison).
func SeriesOf(points []ComparisonPoint, withPrevious bool) []SeriesPoint {
	out := make([]SeriesPoint, len(points))
	for i, p := range points {
		out[i] = SeriesPoint{Bucket: p.Bucket, Value: p.Current}
		if withPrevious {
			out[i].Previous = p.Previous
		}
	}
	return out
}

// BreakdownOf counts records by key, largest first (ties by key), keeping the first n (n ≤ 0: all); keys are
// prefixed with the dimension ("nationality:IT").
func BreakdownOf[R any](recs []R, dim string, key func(R) string, n int) []Breakdown {
	counts := map[string]int64{}
	for _, r := range recs {
		counts[key(r)]++
	}
	out := make([]Breakdown, 0, len(counts))
	for k, v := range counts {
		out = append(out, Breakdown{Key: dim + ":" + k, Label: k, Value: v})
	}
	slices.SortFunc(out, func(a, b Breakdown) int {
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
