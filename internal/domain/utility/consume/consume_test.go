package consume

import (
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

func day(s string) calendar.Date { return calendar.MustParse(s) }

func rd(id, on string, v int64, kind string) Reading {
	d := day(on)
	return Reading{ID: id, On: d, At: d.Time(time.UTC).Add(9 * time.Hour), Value: qty.Milli(v), Kind: kind, Status: "valid"}
}

func ptr(v int64) *int64 { return &v }

func TestIntervals_Scenario(t *testing.T) {
	m := Meter{MultiplierMilli: 1000}
	rs := []Reading{
		rd("c", "2026-06-30", 112_345, KindRegular),
		rd("a", "2026-06-01", 100_000, KindInitial),
		rd("b", "2026-06-10", 100_000, KindRegular), // zero consumption
		rd("d", "2026-07-05", 112_000, KindReplacementOld),
		rd("e", "2026-07-05", 0, KindReplacementNew).withAt(time.Hour),
		rd("f", "2026-07-15", 5_500, KindRegular),
	}
	got := Intervals(m, rs)
	want := []Interval{
		{FromID: "a", ToID: "b", First: day("2026-06-01"), Last: day("2026-06-09"), Delta: 0, Anomalies: []string{}},
		{FromID: "b", ToID: "c", First: day("2026-06-10"), Last: day("2026-06-29"), Delta: 12_345, Anomalies: []string{}},
		{FromID: "c", ToID: "d", First: day("2026-06-30"), Last: day("2026-07-04"), Delta: 0, Anomalies: []string{AnomalyNegative}},
		{FromID: "e", ToID: "f", First: day("2026-07-05"), Last: day("2026-07-14"), Delta: 5_500, Anomalies: []string{}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d intervals: %+v", len(got), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.FromID != w.FromID || g.ToID != w.ToID || !g.First.Equal(w.First) || !g.Last.Equal(w.Last) || g.Delta != w.Delta ||
			!slices.Equal(g.Anomalies, w.Anomalies) {
			t.Errorf("interval %d: got %+v want %+v", i, g, w)
		}
	}
}

func (r Reading) withAt(d time.Duration) Reading {
	r.At = r.At.Add(d)
	return r
}

func TestIntervals_MultiplierAndSameDay(t *testing.T) {
	m := Meter{MultiplierMilli: 40_000} // CT ratio 40
	got := Intervals(m, []Reading{rd("a", "2026-01-10", 1_000, KindRegular), rd("b", "2026-01-10", 1_250, KindRegular).withAt(time.Hour)})
	if len(got) != 1 || got[0].Delta != 10_000 || !got[0].First.Equal(day("2026-01-10")) || !got[0].Last.Equal(day("2026-01-10")) {
		t.Fatalf("got %+v", got)
	}
	// Half-even on the multiplier: 0.001 × 1.5 = 0.0015 → 0.002.
	got = Intervals(Meter{MultiplierMilli: 1500}, []Reading{rd("a", "2026-01-10", 0, KindRegular), rd("b", "2026-01-11", 1, KindRegular)})
	if got[0].Delta != 2 {
		t.Fatalf("half-even: got %d", got[0].Delta)
	}
}

func TestCheck_Anomalies(t *testing.T) {
	charged := day("2026-05-01").Time(time.UTC).Add(12 * time.Hour)
	cases := []struct {
		name      string
		m         Meter
		prev, cur Reading
		last      time.Time
		want      []string
	}{
		{"none", Meter{}, rd("a", "2026-06-01", 100, KindRegular), rd("b", "2026-06-02", 200, KindRegular), time.Time{}, []string{}},
		{"first reading", Meter{}, Reading{}, rd("b", "2026-06-02", 200, KindRegular), time.Time{}, []string{}},
		{"negative", Meter{}, rd("a", "2026-06-01", 500, KindRegular), rd("b", "2026-06-02", 200, KindRegular), time.Time{}, []string{AnomalyNegative}},
		{"rollover", Meter{RolloverMilli: ptr(100_000)}, rd("a", "2026-06-01", 99_000, KindRegular), rd("b", "2026-06-02", 500, KindRegular), time.Time{}, []string{AnomalyRollover}},
		{"replacement restarts", Meter{}, rd("a", "2026-06-01", 99_000, KindReplacementOld), rd("b", "2026-06-01", 0, KindReplacementNew), time.Time{}, []string{}},
		{"over threshold", Meter{DailyThresholdMilli: ptr(10_000)}, rd("a", "2026-06-01", 0, KindRegular), rd("b", "2026-06-03", 20_001, KindRegular), time.Time{}, []string{AnomalyOverThreshold}},
		{"at threshold", Meter{DailyThresholdMilli: ptr(10_000)}, rd("a", "2026-06-01", 0, KindRegular), rd("b", "2026-06-03", 20_000, KindRegular), time.Time{}, []string{}},
		{"same day threshold", Meter{DailyThresholdMilli: ptr(10_000)}, rd("a", "2026-06-01", 0, KindRegular), rd("b", "2026-06-01", 10_001, KindRegular).withAt(time.Hour), time.Time{}, []string{AnomalyOverThreshold}},
		{"gap", Meter{}, rd("a", "2026-01-01", 0, KindRegular), rd("b", "2026-05-02", 10, KindRegular), time.Time{}, []string{AnomalyGap}},
		{"no gap at 120", Meter{}, rd("a", "2026-01-01", 0, KindRegular), rd("b", "2026-05-01", 10, KindRegular), time.Time{}, []string{}},
		{"out of order", Meter{}, rd("a", "2026-04-01", 0, KindRegular), rd("b", "2026-04-20", 10, KindRegular), charged, []string{AnomalyOutOfOrder}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Check(c.m, c.prev, c.cur, c.last); !slices.Equal(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestSplit_Scenario(t *testing.T) {
	first, last := day("2026-06-01"), day("2026-06-30") // 30 nights
	got := Split(10_000, first, last, []Segment{
		{Key: "x", First: day("2026-05-20"), Last: day("2026-06-10")}, // 10 nights
		{Key: "y", First: day("2026-06-16"), Last: day("2026-07-10")}, // 15 nights
	})
	want := []Share{
		{Key: "x", First: day("2026-06-01"), Last: day("2026-06-10"), Qty: 3_333},
		{Key: "", First: day("2026-06-11"), Last: day("2026-06-15"), Qty: 1_667},
		{Key: "y", First: day("2026-06-16"), Last: day("2026-06-30"), Qty: 5_000},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("share %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	// Ties go to the later segment: 1 milli over two equal halves.
	got = Split(1, day("2026-06-01"), day("2026-06-02"), []Segment{
		{Key: "a", First: day("2026-06-01"), Last: day("2026-06-01")}, {Key: "b", First: day("2026-06-02"), Last: day("2026-06-02")},
	})
	if got[0].Qty != 0 || got[1].Qty != 1 {
		t.Fatalf("tie: %+v", got)
	}
	// Negative correction deltas split with the same rule.
	got = Split(-10, day("2026-06-01"), day("2026-06-03"), nil)
	if len(got) != 1 || got[0].Key != "" || got[0].Qty != -10 {
		t.Fatalf("negative unassigned: %+v", got)
	}
	// Runs of one key form one share; a resold night inside an owner's range counts for the segment listed first.
	got = Split(900, day("2026-06-01"), day("2026-06-09"), []Segment{
		{Key: "res", First: day("2026-06-04"), Last: day("2026-06-06")},
		{Key: "own", First: day("2026-06-01"), Last: day("2026-06-03")},
		{Key: "own", First: day("2026-06-07"), Last: day("2026-06-09")},
	})
	if len(got) != 2 || got[0] != (Share{Key: "own", First: day("2026-06-01"), Last: day("2026-06-09"), Qty: 600}) ||
		got[1] != (Share{Key: "res", First: day("2026-06-04"), Last: day("2026-06-06"), Qty: 300}) {
		t.Fatalf("runs: %+v", got)
	}
	if Split(5, day("2026-06-02"), day("2026-06-01"), nil) != nil {
		t.Fatal("empty range must give no share")
	}
}

// TestRapid_SplitSums: Σ shares = Δ and each share is within 1 milli of its exact proportion (INV-91).
func TestRapid_SplitSums(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		first := day("2026-01-01").AddDays(rapid.IntRange(0, 365).Draw(t, "first"))
		n := rapid.IntRange(1, 400).Draw(t, "nights")
		last := first.AddDays(n - 1)
		delta := qty.Milli(rapid.Int64Range(-1_000_000_000, 1_000_000_000_000).Draw(t, "delta"))
		// Disjoint segments: cut points inside a window wider than the interval.
		var segs []Segment
		cursor := first.AddDays(-rapid.IntRange(0, 20).Draw(t, "lead"))
		for i, k := 0, rapid.IntRange(0, 8).Draw(t, "k"); i < k; i++ {
			cursor = cursor.AddDays(rapid.IntRange(0, 30).Draw(t, "gap"))
			end := cursor.AddDays(rapid.IntRange(0, 60).Draw(t, "len"))
			segs = append(segs, Segment{Key: string(rune('a' + i)), First: cursor, Last: end})
			cursor = end.AddDays(1)
		}
		shares := Split(delta, first, last, segs)
		var sum qty.Milli
		var nightsSum int64
		for _, s := range shares {
			sum += s.Qty
			ns := int64(s.First.DaysUntil(s.Last) + 1)
			nightsSum += ns
			if s.First.Before(first) || s.Last.After(last) {
				t.Fatalf("share outside the interval: %+v", s)
			}
			_ = ns
		}
		if sum != delta {
			t.Fatalf("Σ %d ≠ Δ %d (%+v)", sum, delta, shares)
		}
		total := int64(n)
		for _, s := range shares {
			nights := countNights(s, first, last, segs)
			exact := int64(delta) * nights // fits: |Δ| ≤ 1e12, nights ≤ 400
			diff := int64(s.Qty)*total - exact
			if diff < 0 {
				diff = -diff
			}
			if diff >= total {
				t.Fatalf("share %+v off by more than 1 milli (nights %d of %d, Δ %d)", s, nights, total, delta)
			}
		}
	})
}

// countNights counts the nights of a share: the segment's clipped nights, or the uncovered nights for "".
func countNights(s Share, first, last calendar.Date, segs []Segment) int64 {
	var n int64
	for d := first; !d.After(last); d = d.AddDays(1) {
		owner := ""
		for _, g := range segs {
			if !d.Before(g.First) && !d.After(g.Last) {
				owner = g.Key
				break
			}
		}
		if owner == s.Key {
			n++
		}
	}
	return n
}
