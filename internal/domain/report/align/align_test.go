package align

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

func TestAlign_DateAndWeekday(t *testing.T) {
	shifts := map[int]int{1: 364, 2: 728, 3: 1099, 4: 1463, 5: 1827}
	for k, want := range shifts {
		if got := WeekdayShift(k); got != want {
			t.Errorf("WeekdayShift(%d) = %d, want %d", k, got, want)
		}
	}
	cases := []struct {
		name             string
		from, to         string
		k                int
		align            string
		wantFrom, wantTo string
	}{
		{"date one year", "2026-03-01", "2026-03-31", 1, ByDate, "2025-03-01", "2025-03-31"},
		{"date leap day to non leap", "2028-02-29", "2028-03-01", 1, ByDate, "2027-02-28", "2027-03-01"},
		{"date four years keeps leap day", "2028-02-29", "2028-02-29", 4, ByDate, "2024-02-29", "2024-02-29"},
		{"weekday one year", "2026-11-02", "2026-11-08", 1, ByWeekday, "2025-11-03", "2025-11-09"},
		{"weekday five years", "2026-01-01", "2026-01-31", 5, ByWeekday, "2020-12-31", "2021-01-30"},
	}
	for _, c := range cases {
		f, to := Previous(d(c.from), d(c.to), c.k, c.align)
		if f != d(c.wantFrom) || to != d(c.wantTo) {
			t.Errorf("%s: Previous = %s..%s, want %s..%s", c.name, f, to, c.wantFrom, c.wantTo)
		}
		if c.align == ByWeekday && f.ISOWeekday() != d(c.from).ISOWeekday() {
			t.Errorf("%s: weekday not kept", c.name)
		}
	}
	// Mapping back: never by index; date maps a previous Feb 29 onto Feb 28, weekday keeps the weekday.
	if got := MapToCurrent(d("2024-02-29"), 1, ByDate); got != d("2025-02-28") {
		t.Errorf("MapToCurrent leap = %s", got)
	}
	if got := MapToCurrent(d("2024-02-28"), 1, ByDate); got != d("2025-02-28") {
		t.Errorf("MapToCurrent Feb 28 = %s", got)
	}
	for k := 1; k <= 5; k++ {
		p := d("2021-06-15")
		c := MapToCurrent(p, k, ByWeekday)
		if c.ISOWeekday() != p.ISOWeekday() || p.DaysUntil(c) != WeekdayShift(k) {
			t.Errorf("weekday map k=%d: %s", k, c)
		}
		if got := MapToCurrent(AddYears(d("2026-07-10"), -k), k, ByDate); got != d("2026-07-10") {
			t.Errorf("date map k=%d: %s", k, got)
		}
	}
}

func TestAlign_Buckets(t *testing.T) {
	if g := DefaultGranularity(d("2026-01-01"), d("2026-01-31")); g != Day {
		t.Errorf("31 days: %s", g)
	}
	if g := DefaultGranularity(d("2026-01-01"), d("2026-04-30")); g != Week {
		t.Errorf("120 days: %s", g)
	}
	if g := DefaultGranularity(d("2026-01-01"), d("2026-05-01")); g != Month {
		t.Errorf("121 days: %s", g)
	}
	// 2026-11-08 is a Sunday: its ISO week starts on Monday 2026-11-02.
	if b := Bucket(d("2026-11-08"), Week); b != d("2026-11-02") {
		t.Errorf("week bucket %s", b)
	}
	if b := Bucket(d("2026-11-08"), Month); b != d("2026-11-01") {
		t.Errorf("month bucket %s", b)
	}
	bs := Buckets(d("2026-01-15"), d("2026-04-02"), Month)
	if len(bs) != 4 || bs[0] != d("2026-01-01") || bs[3] != d("2026-04-01") {
		t.Errorf("month buckets %v", bs)
	}
	ws := Buckets(d("2026-11-04"), d("2026-11-16"), Week)
	if len(ws) != 3 || ws[0] != d("2026-11-02") || ws[2] != d("2026-11-16") {
		t.Errorf("week buckets %v", ws)
	}
	ds := Buckets(d("2026-12-30"), d("2027-01-02"), Day)
	if len(ds) != 4 {
		t.Errorf("day buckets %v", ds)
	}
	if Clamp(d("2026-01-01"), d("2026-02-01"), d("2026-03-01")) != d("2026-02-01") {
		t.Error("clamp")
	}
}

// TestAlign_MappingStaysInPeriod (property): every day of the previous period maps onto the current period (by
// date: Feb 29 lands on Feb 28; by weekday: same weekday), and the mapping is monotonic.
func TestAlign_MappingStaysInPeriod(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		from := d("2020-01-01").AddDays(rapid.IntRange(0, 3000).Draw(t, "from"))
		to := from.AddDays(rapid.IntRange(0, 399).Draw(t, "len"))
		k := rapid.IntRange(1, 5).Draw(t, "k")
		al := rapid.SampledFrom([]string{ByDate, ByWeekday}).Draw(t, "align")
		pf, pt := Previous(from, to, k, al)
		var last calendar.Date
		for p := pf; !p.After(pt); p = p.AddDays(1) {
			c := MapToCurrent(p, k, al)
			if al == ByWeekday && (c.Before(from) || c.After(to) || c.ISOWeekday() != p.ISOWeekday()) {
				t.Fatalf("weekday %s → %s outside %s..%s", p, c, from, to)
			}
			if al == ByDate && (Clamp(c, from, to).DaysUntil(c) > 1 || c.DaysUntil(Clamp(c, from, to)) > 1) {
				t.Fatalf("date %s → %s far from %s..%s", p, c, from, to)
			}
			if !last.IsZero() && c.Before(last) {
				t.Fatalf("not monotonic at %s", p)
			}
			last = c
		}
	})
}
