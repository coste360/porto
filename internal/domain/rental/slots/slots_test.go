package slots

import (
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
)

func defaults() Settings {
	return Settings{OpenFrom: 16, OpenTo: 38, Buffer: 1, Bands: []Band{{"morning", 16, 26}, {"afternoon", 28, 38}}}
}

func pt(d string, s int) Point { return Point{Date: calendar.MustParse(d), Slot: s} }

func rng(d0 string, s0 int, d1 string, s1 int) Range {
	return Range{Start: pt(d0, s0), End: pt(d1, s1)}
}

func TestSlots_Clock(t *testing.T) {
	cases := []struct{ h, m, slot int }{{0, 0, 0}, {8, 0, 16}, {9, 29, 18}, {9, 30, 19}, {13, 0, 26}, {23, 59, 47}}
	for _, c := range cases {
		if got := FromClock(c.h, c.m); got != c.slot {
			t.Fatalf("FromClock(%d,%d) = %d, want %d", c.h, c.m, got, c.slot)
		}
		h, m := Clock(c.slot)
		if h != c.h || m != c.m-c.m%30 {
			t.Fatalf("Clock(%d) = %d:%d", c.slot, h, m)
		}
	}
	if Label(48) != "24:00" || Label(19) != "09:30" || Label(0) != "00:00" {
		t.Fatal("labels")
	}
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	got := Instant(calendar.MustParse("2027-06-12"), 20, rome)
	if !got.Equal(time.Date(2027, 6, 12, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("Instant = %v", got)
	}
	if end := Instant(calendar.MustParse("2027-06-12"), 48, rome); !end.Equal(time.Date(2027, 6, 12, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("Instant 48 = %v", end)
	}
	// Up rounds up to the next slot and keeps an exact slot.
	cases2 := []struct {
		at   time.Time
		want Point
	}{
		{time.Date(2027, 6, 12, 11, 0, 0, 0, time.UTC), pt("2027-06-12", 26)},
		{time.Date(2027, 6, 12, 11, 0, 1, 0, time.UTC), pt("2027-06-12", 27)},
		{time.Date(2027, 6, 12, 11, 16, 0, 0, time.UTC), pt("2027-06-12", 27)},
		{time.Date(2027, 6, 12, 21, 45, 0, 0, time.UTC), pt("2027-06-12", 48)},
	}
	for _, c := range cases2 {
		if got := Up(c.at, rome); !got.Date.Equal(c.want.Date) || got.Slot != c.want.Slot {
			t.Fatalf("Up(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestValidateSettings_Table(t *testing.T) {
	ok := defaults()
	if err := ValidateSettings(ok); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	cases := []struct {
		name string
		mod  func(*Settings)
		code string
	}{
		{"window empty", func(s *Settings) { s.OpenTo = s.OpenFrom }, CodeOutsideRentalHours},
		{"window over DST", func(s *Settings) { s.OpenFrom = 2; s.Bands = []Band{{"x", 16, 20}} }, CodeOutsideRentalHours},
		{"window after 48", func(s *Settings) { s.OpenTo = 49 }, CodeOutsideRentalHours},
		{"buffer", func(s *Settings) { s.Buffer = 9 }, CodeOutsideRentalHours},
		{"no bands", func(s *Settings) { s.Bands = nil }, CodeOutsideRentalHours},
		{"band outside", func(s *Settings) { s.Bands[1].To = 40 }, CodeOutsideRentalHours},
		{"duplicate band", func(s *Settings) { s.Bands[1].Code = "morning" }, CodeOutsideRentalHours},
		{"gap below buffer", func(s *Settings) { s.Bands[1].From = 26 }, CodeBandGapLtBuffer},
		{"overlap", func(s *Settings) { s.Bands[1].From = 24 }, CodeBandGapLtBuffer},
	}
	for _, c := range cases {
		s := defaults()
		s.Bands = append([]Band(nil), s.Bands...)
		c.mod(&s)
		var re *RangeError
		if err := ValidateSettings(s); !errors.As(err, &re) || re.Code != c.code {
			t.Fatalf("%s: got %v, want %s", c.name, err, c.code)
		}
	}
	zero := defaults()
	zero.Buffer, zero.Bands[1].From = 0, 26
	if err := ValidateSettings(zero); err != nil {
		t.Fatalf("adjacent bands with buffer 0: %v", err)
	}
}

func TestValidateRange_Table(t *testing.T) {
	s := defaults()
	hourly := Formula{Kind: KindHourly, MinSlots: 2}
	morning := Formula{Kind: KindHalfDay, Band: "morning"}
	evening := Formula{Kind: KindHalfDay, Band: "evening"}
	day := Formula{Kind: KindDay}
	multi := Formula{Kind: KindMultiDay, MinDays: 2, MaxDays: 30}
	week := Formula{Kind: KindWeek, MinDays: 7, MaxDays: 30}
	cases := []struct {
		name string
		f    Formula
		r    Range
		want Quantity
		code string
	}{
		{"hourly E1", hourly, rng("2027-06-12", 20, "2027-06-12", 26), Quantity{Slots: 6, Days: 1}, ""},
		{"hourly E2", hourly, rng("2027-07-10", 19, "2027-07-10", 24), Quantity{Slots: 5, Days: 1}, ""},
		{"hourly at window edges", hourly, rng("2027-06-12", 16, "2027-06-12", 38), Quantity{Slots: 22, Days: 1}, ""},
		{"hourly too short", hourly, rng("2027-06-12", 20, "2027-06-12", 21), Quantity{}, CodeFormulaRangeMismatch},
		{"hourly max", Formula{Kind: KindHourly, MinSlots: 2, MaxSlots: 4}, rng("2027-06-12", 20, "2027-06-12", 25), Quantity{}, CodeFormulaRangeMismatch},
		{"hourly before open", hourly, rng("2027-06-12", 15, "2027-06-12", 20), Quantity{}, CodeOutsideRentalHours},
		{"hourly after close", hourly, rng("2027-06-12", 30, "2027-06-12", 39), Quantity{}, CodeOutsideRentalHours},
		{"hourly DST label", hourly, rng("2027-03-28", 4, "2027-03-28", 6), Quantity{}, CodeOutsideRentalHours},
		{"hourly two dates", hourly, rng("2027-06-12", 30, "2027-06-13", 20), Quantity{}, CodeFormulaRangeMismatch},
		{"reversed", hourly, rng("2027-06-12", 26, "2027-06-12", 20), Quantity{}, CodeFormulaRangeMismatch},
		{"empty", hourly, rng("2027-06-12", 20, "2027-06-12", 20), Quantity{}, CodeFormulaRangeMismatch},
		{"half day exact", morning, rng("2027-06-12", 16, "2027-06-12", 26), Quantity{Slots: 10, Days: 1, Band: "morning"}, ""},
		{"half day shifted", morning, rng("2027-06-12", 17, "2027-06-12", 26), Quantity{}, CodeFormulaRangeMismatch},
		{"half day unknown band", evening, rng("2027-06-12", 16, "2027-06-12", 26), Quantity{}, CodeFormulaRangeMismatch},
		{"day exact", day, rng("2027-06-12", 16, "2027-06-12", 38), Quantity{Slots: 22, Days: 1}, ""},
		{"day short", day, rng("2027-06-12", 16, "2027-06-12", 37), Quantity{}, CodeFormulaRangeMismatch},
		{"multi 3 dates", multi, rng("2027-06-14", 18, "2027-06-16", 38), Quantity{Slots: 116, Days: 3}, ""},
		{"multi one date", multi, rng("2027-06-14", 18, "2027-06-14", 38), Quantity{}, CodeFormulaRangeMismatch},
		{"multi end at open", multi, rng("2027-06-14", 18, "2027-06-16", 16), Quantity{}, CodeOutsideRentalHours},
		{"multi start at close", multi, rng("2027-06-14", 38, "2027-06-16", 20), Quantity{}, CodeOutsideRentalHours},
		{"multi too long", multi, rng("2027-06-01", 18, "2027-07-01", 20), Quantity{}, CodeFormulaRangeMismatch},
		{"multi over DST", multi, rng("2027-03-27", 18, "2027-03-29", 20), Quantity{Slots: 98, Days: 3}, ""},
		{"week 9 days", week, rng("2027-06-05", 16, "2027-06-13", 38), Quantity{Slots: 8*48 + 22, Days: 9}, ""},
		{"week 6 days", week, rng("2027-06-05", 16, "2027-06-10", 38), Quantity{}, CodeFormulaRangeMismatch},
		{"week min forced to 7", Formula{Kind: KindWeek, MinDays: 2}, rng("2027-06-05", 16, "2027-06-10", 38), Quantity{}, CodeFormulaRangeMismatch},
		{"unknown kind", Formula{Kind: "fortnight"}, rng("2027-06-05", 16, "2027-06-10", 38), Quantity{}, CodeFormulaRangeMismatch},
	}
	for _, c := range cases {
		got, err := ValidateRange(c.f, s, c.r)
		if c.code == "" {
			if err != nil || got != c.want {
				t.Fatalf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
			}
			continue
		}
		var re *RangeError
		if !errors.As(err, &re) || re.Code != c.code {
			t.Fatalf("%s: got %v, want %s", c.name, err, c.code)
		}
		if re.Kind != c.f.Kind {
			t.Fatalf("%s: kind %q", c.name, re.Kind)
		}
	}
}

func TestExpand_Examples(t *testing.T) {
	// m11-plan §4.3 examples with buffer 1.
	if n := len(Expand(rng("2027-06-12", 20, "2027-06-12", 26), 1)); n != 7 {
		t.Fatalf("hourly rows = %d", n)
	}
	if n := len(Expand(rng("2027-06-12", 16, "2027-06-12", 26), 1)); n != 11 {
		t.Fatalf("half day rows = %d", n)
	}
	keys := Expand(rng("2027-06-14", 18, "2027-06-16", 38), 1)
	if len(keys) != 117 {
		t.Fatalf("multi-day rows = %d", len(keys))
	}
	last := keys[len(keys)-1]
	if last.Date.String() != "2027-06-16" || last.Slot != 38 {
		t.Fatalf("buffer row = %v", last)
	}
	// End slot 48 + buffer 1 carries to slot 0 of the next date.
	k := Expand(rng("2027-06-14", 40, "2027-06-14", 48), 1)
	if b := k[len(k)-1]; b.Date.String() != "2027-06-15" || b.Slot != 0 {
		t.Fatalf("carried buffer = %v", b)
	}
	// 10:00-12:00 then 12:00-13:00: the first buffer row 24 is the second's first slot.
	a := Expand(rng("2027-06-12", 20, "2027-06-12", 24), 1)
	b := Expand(rng("2027-06-12", 24, "2027-06-12", 26), 1)
	if a[len(a)-1] != b[0] {
		t.Fatal("buffer must collide with an adjacent start")
	}
	if Days(rng("2027-06-14", 18, "2027-06-16", 38)) != 3 || Count(rng("2027-06-14", 18, "2027-06-16", 38)) != 116 {
		t.Fatal("days/count")
	}
}

func genRange(t *rapid.T, label string) Range {
	base := calendar.MustParse("2027-03-25") // spans the spring DST change of 2027-03-28
	d0 := base.AddDays(rapid.IntRange(0, 10).Draw(t, label+"_d0"))
	s0 := rapid.IntRange(0, 47).Draw(t, label+"_s0")
	n := rapid.IntRange(1, 48*4).Draw(t, label+"_n")
	return Range{Start: Point{Date: d0, Slot: s0}, End: After(Point{Date: d0, Slot: s0}, n)}
}

func TestRapid_RentalSlotsExpand(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := genRange(t, "r")
		buf := rapid.IntRange(0, MaxBuffer).Draw(t, "buf")
		keys := Expand(r, buf)
		if len(keys) != Count(r)+buf {
			t.Fatalf("count %d, want %d", len(keys), Count(r)+buf)
		}
		if keys[0].Date != r.Start.Date || keys[0].Slot != r.Start.Slot {
			t.Fatalf("first key %v", keys[0])
		}
		perDate := map[calendar.Date]int{}
		for i, k := range keys {
			if k.Slot < 0 || k.Slot >= PerDay {
				t.Fatalf("slot %d", k.Slot)
			}
			perDate[k.Date]++
			if i > 0 {
				p := keys[i-1]
				contiguous := (p.Date == k.Date && p.Slot+1 == k.Slot) || (p.Slot == PerDay-1 && k.Slot == 0 && p.Date.AddDays(1) == k.Date)
				if !contiguous {
					t.Fatalf("gap between %v and %v", p, k)
				}
			}
		}
		for d, n := range perDate {
			if n > PerDay {
				t.Fatalf("%v has %d rows", d, n)
			}
		}
		// Every inner date of the range (DST dates included) has exactly 48 labels.
		for d := r.Start.Date.AddDays(1); d.Before(r.End.Date); d = d.AddDays(1) {
			if perDate[d] != PerDay {
				t.Fatalf("inner date %v has %d rows", d, perDate[d])
			}
		}
		// A second range starting gap slots after the end: disjoint iff gap >= buffer.
		gap := rapid.IntRange(0, 10).Draw(t, "gap")
		start2 := After(r.End, gap)
		r2 := Range{Start: AsStart(start2), End: After(AsStart(start2), rapid.IntRange(1, 20).Draw(t, "n2"))}
		set := map[Key]bool{}
		for _, k := range keys {
			set[k] = true
		}
		overlap := false
		for _, k := range Expand(r2, buf) {
			if set[k] {
				overlap = true
			}
		}
		if overlap != (gap < buf) {
			t.Fatalf("gap %d buffer %d overlap %v", gap, buf, overlap)
		}
	})
}
