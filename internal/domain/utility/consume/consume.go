package consume

import (
	"math/bits"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

// Anomaly codes (m9-plan §9.2).
const (
	AnomalyNegative      = "negative"
	AnomalyRollover      = "rollover"
	AnomalyOverThreshold = "over_threshold"
	AnomalyOutOfOrder    = "out_of_order"
	AnomalyGap           = "gap"
)

// GapDays is the longest interval without the gap anomaly.
const GapDays = 120

// Reading kinds.
const (
	KindRegular        = "regular"
	KindInitial        = "initial"
	KindReplacementOld = "replacement_old"
	KindReplacementNew = "replacement_new"
)

// Reading is a reading as the pure functions see it.
type Reading struct {
	ID     string
	At     time.Time
	On     calendar.Date
	Value  qty.Milli
	Kind   string // regular, initial, replacement_old, replacement_new
	Status string // valid, anomaly, confirmed (void readings are never passed)
}

// Meter are the meter parameters of the computation.
type Meter struct {
	MultiplierMilli     int64
	RolloverMilli       *int64
	DailyThresholdMilli *int64
}

// Interval is the consumption between two consecutive readings.
type Interval struct {
	FromID, ToID string
	First, Last  calendar.Date // inclusive nights
	Delta        qty.Milli
	Anomalies    []string
}

// Segment is an occupant of the meter's resource for an inclusive night range (Key = allocation id).
type Segment struct {
	Key         string
	First, Last calendar.Date
}

// Share is the part of Delta of one segment (Key = "" for the unassigned nights).
type Share struct {
	Key         string
	First, Last calendar.Date
	Qty         qty.Milli
}

// restarts reports whether cur starts a new index (no interval with the reading before it).
func restarts(cur Reading) bool {
	return cur.Kind == KindInitial || cur.Kind == KindReplacementNew
}

func multiplier(m Meter) int64 {
	if m.MultiplierMilli <= 0 {
		return 1000
	}
	return m.MultiplierMilli
}

// raw returns the index difference of cur after prev (wrapped through the rollover register when negative) and
// whether it wrapped. A negative difference without rollover is returned as is.
func raw(m Meter, prev, cur Reading) (qty.Milli, bool) {
	diff := cur.Value - prev.Value
	if diff < 0 && m.RolloverMilli != nil && *m.RolloverMilli > 0 {
		return qty.Milli(*m.RolloverMilli) - prev.Value + cur.Value, true
	}
	return diff, false
}

// nights returns the inclusive nights of the interval prev → cur: prev.On … cur.On − 1, a same-day pair on prev.On.
func nights(prev, cur Reading) (calendar.Date, calendar.Date) {
	last := cur.On.AddDays(-1)
	if last.Before(prev.On) {
		last = prev.On
	}
	return prev.On, last
}

// delta returns the consumption of the interval after the multiplier (half-even); a negative difference without
// rollover counts as zero (the reading is flagged negative and reviewed by the office).
func delta(m Meter, prev, cur Reading) qty.Milli {
	r, _ := raw(m, prev, cur)
	if r <= 0 {
		return 0
	}
	v, err := r.Mul(qty.Milli(multiplier(m)))
	if err != nil {
		return 0
	}
	return v
}

// Check returns the anomaly codes of cur after prev (lastCharged = read_at of the latest reading used by a charged
// consumption, zero when none). prev is the zero Reading for the first reading of a meter.
func Check(m Meter, prev, cur Reading, lastCharged time.Time) []string {
	out := []string{}
	if !lastCharged.IsZero() && cur.At.Before(lastCharged) {
		out = append(out, AnomalyOutOfOrder)
	}
	if prev.ID == "" || restarts(cur) || prev.Kind == KindReplacementOld {
		return out
	}
	r, wrapped := raw(m, prev, cur)
	switch {
	case wrapped:
		out = append(out, AnomalyRollover)
	case r < 0:
		out = append(out, AnomalyNegative)
	}
	days := prev.On.DaysUntil(cur.On)
	if days > GapDays {
		out = append(out, AnomalyGap)
	}
	if m.DailyThresholdMilli != nil && *m.DailyThresholdMilli > 0 {
		limit := int64(*m.DailyThresholdMilli) * int64(max(days, 1))
		if int64(delta(m, prev, cur)) > limit {
			out = append(out, AnomalyOverThreshold)
		}
	}
	return out
}

// Intervals returns the intervals of consecutive readings ordered by At (replacement pairs and initial readings
// restart the index: no interval ends on them). Anomalies are those of Check without out_of_order.
func Intervals(m Meter, readings []Reading) []Interval {
	rs := slices.Clone(readings)
	slices.SortStableFunc(rs, func(a, b Reading) int { return a.At.Compare(b.At) })
	out := []Interval{}
	for i := 1; i < len(rs); i++ {
		prev, cur := rs[i-1], rs[i]
		if restarts(cur) || prev.Kind == KindReplacementOld {
			continue
		}
		first, last := nights(prev, cur)
		out = append(out, Interval{
			FromID: prev.ID, ToID: cur.ID, First: first, Last: last, Delta: delta(m, prev, cur),
			Anomalies: Check(m, prev, cur, time.Time{}),
		})
	}
	return out
}

// item is a share being computed: its nights and its order for ties.
type item struct {
	key         string
	first, last calendar.Date
	nights      int64
}

// Split shares delta among the segments over the nights [first, last] in proportion to their nights (largest
// remainder in milli, ties to the later share); nights without a segment form the unassigned share (Key ""). Several
// segments of one key (runs of nights) form one share; a night claimed by two keys counts for the first segment
// listed. A share's First and Last are its first and last night (its nights need not be contiguous). Σ shares =
// delta exactly (INV-91). Shares with no night in [first, last] are omitted.
func Split(delta qty.Milli, first, last calendar.Date, segments []Segment) []Share {
	if last.Before(first) {
		return nil
	}
	total := int64(first.DaysUntil(last) + 1)
	owner := make([]int, total) // index+1 into items, 0 = unassigned
	var items []item
	index := map[string]int{}
	for _, s := range segments {
		f, l := s.First, s.Last
		if f.Before(first) {
			f = first
		}
		if l.After(last) {
			l = last
		}
		if l.Before(f) {
			continue
		}
		for i := first.DaysUntil(f); i <= first.DaysUntil(l); i++ {
			if owner[i] != 0 {
				continue
			}
			k, ok := index[s.Key]
			if !ok {
				k = len(items)
				index[s.Key] = k
				items = append(items, item{key: s.Key})
			}
			owner[i] = k + 1
		}
	}
	un := -1
	for i := range owner {
		if owner[i] == 0 {
			if un < 0 {
				un = len(items)
				items = append(items, item{key: ""})
			}
			owner[i] = un + 1
		}
	}
	for i, o := range owner {
		it := &items[o-1]
		d := first.AddDays(i)
		if it.nights == 0 {
			it.first = d
		}
		it.last = d
		it.nights++
	}
	slices.SortStableFunc(items, func(a, b item) int { return a.first.Compare(b.first) })

	neg := delta < 0
	mag := int64(delta)
	if neg {
		mag = -mag
	}
	shares := make([]int64, len(items))
	rems := make([]int64, len(items))
	var assigned int64
	for i, it := range items {
		// mag × nights / total without overflow for realistic magnitudes (mag < 2^63 / 2^16 nights).
		q, r := mulDiv(mag, it.nights, total)
		shares[i], rems[i] = q, r
		assigned += q
	}
	left := mag - assigned
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if rems[a] != rems[b] {
			if rems[a] > rems[b] {
				return -1
			}
			return 1
		}
		return b - a // ties to the later segment
	})
	for k := 0; k < int(left); k++ {
		shares[order[k%len(order)]]++
	}
	out := make([]Share, len(items))
	for i, it := range items {
		v := shares[i]
		if neg {
			v = -v
		}
		out[i] = Share{Key: it.key, First: it.first, Last: it.last, Qty: qty.Milli(v)}
	}
	return out
}

// mulDiv returns floor(a×b/d) and the remainder for non-negative a, b and positive d, with a 128-bit intermediate.
func mulDiv(a, b, d int64) (int64, int64) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	q, r := bits.Div64(hi, lo, uint64(d))
	return int64(q), int64(r)
}
