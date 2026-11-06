package calendar

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var (
	_ sql.Scanner   = (*Date)(nil)
	_ driver.Valuer = Date{}
)

func TestParse_Strict(t *testing.T) {
	valid := map[string]Date{
		"2026-07-01": New(2026, time.July, 1),
		"2024-02-29": New(2024, time.February, 29),
		"1000-01-01": New(1000, time.January, 1),
		"9999-12-31": New(9999, time.December, 31),
	}
	for in, want := range valid {
		got, err := Parse(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
		if got.String() != in {
			t.Errorf("Parse(%q).String() = %q", in, got.String())
		}
	}
	invalid := []string{
		"", "2026-7-1", "2026-07-1", "26-07-01", "2026/07/01", "2026-07-01T00:00:00Z", " 2026-07-01",
		"2026-13-01", "2026-00-10", "2026-02-29", "2026-04-31", "0000-01-01", "0001-01-01", "0999-12-31", "+026-07-01", "2026-0a-01", "２０２６-07-01",
	}
	for _, in := range invalid {
		if _, err := Parse(in); !errors.Is(err, ErrInvalidDate) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalidDate", in, err)
		}
	}
}

func TestMustParse_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	MustParse("nope")
}

func TestDate_Accessors(t *testing.T) {
	d := MustParse("2026-11-06")
	if d.Year() != 2026 || d.Month() != time.November || d.Day() != 6 {
		t.Fatalf("accessors = %d %v %d", d.Year(), d.Month(), d.Day())
	}
	if d.ISOWeekday() != 5 { // Friday
		t.Fatalf("ISOWeekday = %d", d.ISOWeekday())
	}
	if MustParse("2026-11-08").ISOWeekday() != 7 || MustParse("2026-11-09").ISOWeekday() != 1 {
		t.Fatal("ISOWeekday Sunday/Monday")
	}
	if (Date{}).String() != "" || !(Date{}).IsZero() || d.IsZero() {
		t.Fatal("zero date")
	}
}

func TestDate_Arithmetic(t *testing.T) {
	tests := []struct {
		from string
		n    int
		want string
	}{
		{"2026-12-31", 1, "2027-01-01"},
		{"2024-02-28", 1, "2024-02-29"},
		{"2026-02-28", 1, "2026-03-01"},
		{"2026-03-01", -1, "2026-02-28"},
		{"2026-03-29", 1, "2026-03-30"}, // DST change in Europe: irrelevant to dates
		{"2026-01-01", 365, "2027-01-01"},
	}
	for _, tt := range tests {
		got := MustParse(tt.from).AddDays(tt.n)
		if got.String() != tt.want {
			t.Errorf("%s + %d = %s, want %s", tt.from, tt.n, got, tt.want)
		}
		if back := MustParse(tt.from).DaysUntil(got); back != tt.n {
			t.Errorf("DaysUntil = %d, want %d", back, tt.n)
		}
	}
	a, b := MustParse("2026-01-01"), MustParse("2026-01-02")
	if !a.Before(b) || !b.After(a) || a.Equal(b) || a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(MustParse("2026-01-01")) != 0 {
		t.Fatal("comparisons")
	}
}

func TestRange(t *testing.T) {
	got := Range(MustParse("2026-02-27"), MustParse("2026-03-02"))
	want := []string{"2026-02-27", "2026-02-28", "2026-03-01", "2026-03-02"}
	if len(got) != len(want) {
		t.Fatalf("Range = %v", got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("Range[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	if r := Range(MustParse("2026-01-02"), MustParse("2026-01-01")); r != nil {
		t.Fatalf("reversed Range = %v", r)
	}
	if r := Range(MustParse("2026-01-01"), MustParse("2026-01-01")); len(r) != 1 {
		t.Fatalf("single Range = %v", r)
	}
}

func TestNightsBetween(t *testing.T) {
	tests := []struct {
		arrival, departure string
		first, last        string
		count              int
		wantErr            bool
	}{
		{"2026-07-01", "2026-07-02", "2026-07-01", "2026-07-01", 1, false},
		{"2026-07-01", "2026-07-08", "2026-07-01", "2026-07-07", 7, false},
		{"2026-12-30", "2027-01-02", "2026-12-30", "2027-01-01", 3, false},
		{"2026-07-01", "2026-07-01", "", "", 0, true},
		{"2026-07-02", "2026-07-01", "", "", 0, true},
	}
	for _, tt := range tests {
		n, err := NightsBetween(MustParse(tt.arrival), MustParse(tt.departure))
		if tt.wantErr {
			if !errors.Is(err, ErrInvalidRange) {
				t.Errorf("NightsBetween(%s, %s) err = %v", tt.arrival, tt.departure, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if n.First.String() != tt.first || n.Last.String() != tt.last || n.Count() != tt.count {
			t.Errorf("NightsBetween(%s, %s) = %+v count %d", tt.arrival, tt.departure, n, n.Count())
		}
		if len(n.Each()) != tt.count || !n.Departure().Equal(MustParse(tt.departure)) {
			t.Errorf("Each/Departure mismatch for %s..%s", tt.arrival, tt.departure)
		}
		if !n.Contains(MustParse(tt.arrival)) || n.Contains(MustParse(tt.departure)) {
			t.Errorf("Contains: arrival night in, departure day out (%s..%s)", tt.arrival, tt.departure)
		}
	}
	if _, err := NightsBetween(Date{}, MustParse("2026-01-01")); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("zero arrival err = %v", err)
	}
}

func TestNights_Overlaps(t *testing.T) {
	mk := func(a, d string) Nights {
		n, err := NightsBetween(MustParse(a), MustParse(d))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	tests := []struct {
		name string
		a, b Nights
		want bool
	}{
		{"same", mk("2026-07-01", "2026-07-05"), mk("2026-07-01", "2026-07-05"), true},
		{"back to back (departure = next arrival)", mk("2026-07-01", "2026-07-05"), mk("2026-07-05", "2026-07-08"), false},
		{"one night overlap", mk("2026-07-01", "2026-07-05"), mk("2026-07-04", "2026-07-08"), true},
		{"nested", mk("2026-07-01", "2026-07-10"), mk("2026-07-03", "2026-07-04"), true},
		{"disjoint", mk("2026-07-01", "2026-07-02"), mk("2026-08-01", "2026-08-02"), false},
	}
	for _, tt := range tests {
		if got := tt.a.Overlaps(tt.b); got != tt.want {
			t.Errorf("%s: Overlaps = %v", tt.name, got)
		}
		if got := tt.b.Overlaps(tt.a); got != tt.want {
			t.Errorf("%s: Overlaps not symmetric", tt.name)
		}
	}
}

func TestRapid_NightsOverlapMatchesEach(t *testing.T) {
	base := MustParse("2026-01-01")
	rapid.Check(t, func(t *rapid.T) {
		draw := func(label string) Nights {
			a := rapid.IntRange(0, 60).Draw(t, label+"_a")
			l := rapid.IntRange(1, 20).Draw(t, label+"_len")
			n, _ := NightsBetween(base.AddDays(a), base.AddDays(a+l))
			return n
		}
		x, y := draw("x"), draw("y")
		shared := false
		for _, d := range x.Each() {
			if y.Contains(d) {
				shared = true
			}
		}
		if x.Overlaps(y) != shared {
			t.Fatalf("Overlaps(%+v, %+v) = %v, shared = %v", x, y, x.Overlaps(y), shared)
		}
	})
}

func TestIn_Timezone(t *testing.T) {
	rome, err := LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		instant string
		want    string
	}{
		{"2026-07-01T21:59:59Z", "2026-07-01"}, // 23:59:59 CEST
		{"2026-07-01T22:00:00Z", "2026-07-02"}, // 00:00 CEST
		{"2026-01-01T22:59:59Z", "2026-01-01"}, // 23:59:59 CET
		{"2026-01-01T23:00:00Z", "2026-01-02"},
		// DST start 2026-03-29 02:00 CET -> 03:00 CEST.
		{"2026-03-28T23:00:00Z", "2026-03-29"},
		{"2026-03-29T21:59:59Z", "2026-03-29"},
		{"2026-03-29T22:00:00Z", "2026-03-30"},
		// DST end 2026-10-25 03:00 CEST -> 02:00 CET.
		{"2026-10-24T22:00:00Z", "2026-10-25"},
		{"2026-10-25T22:59:59Z", "2026-10-25"},
		{"2026-10-25T23:00:00Z", "2026-10-26"},
	}
	for _, tt := range tests {
		inst, _ := time.Parse(time.RFC3339, tt.instant)
		if got := In(inst, rome).String(); got != tt.want {
			t.Errorf("In(%s, Rome) = %s, want %s", tt.instant, got, tt.want)
		}
	}
	inst, _ := time.Parse(time.RFC3339, "2026-07-01T23:30:00Z")
	if got := In(inst, nil).String(); got != "2026-07-01" {
		t.Errorf("In(nil loc) = %s", got)
	}
	if got := MustParse("2026-03-29").Time(rome); got.Hour() != 0 || got.Day() != 29 {
		t.Errorf("Time(rome) = %v", got)
	}
}

func TestLoadLocation(t *testing.T) {
	for _, ok := range []string{"Europe/Rome", "UTC", "America/New_York"} {
		if _, err := LoadLocation(ok); err != nil {
			t.Errorf("LoadLocation(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Local", "Mars/Olympus", "../etc/passwd"} {
		if _, err := LoadLocation(bad); !errors.Is(err, ErrInvalidLocation) {
			t.Errorf("LoadLocation(%q) err = %v", bad, err)
		}
	}
}

func TestDate_JSONRoundTrip(t *testing.T) {
	type payload struct {
		D Date  `json:"d"`
		P *Date `json:"p"`
	}
	d := MustParse("2026-08-15")
	b, err := json.Marshal(payload{D: d})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"d":"2026-08-15","p":null}` {
		t.Fatalf("json = %s", b)
	}
	var back payload
	if err := json.Unmarshal(b, &back); err != nil || !back.D.Equal(d) || back.P != nil {
		t.Fatalf("unmarshal = %+v, %v", back, err)
	}
	if b, _ := json.Marshal(Date{}); string(b) != "null" {
		t.Fatalf("zero json = %s", b)
	}
	var z Date
	if err := json.Unmarshal([]byte("null"), &z); err != nil || !z.IsZero() {
		t.Fatal("null -> zero")
	}
	for _, bad := range []string{`"2026-8-15"`, `20260815`, `"x"`} {
		var x Date
		if err := json.Unmarshal([]byte(bad), &x); err == nil {
			t.Errorf("Unmarshal(%s) accepted", bad)
		}
	}
	// Text form (query parameters, map keys).
	var tx Date
	if err := tx.UnmarshalText([]byte("2026-08-15")); err != nil || !tx.Equal(d) {
		t.Fatal("UnmarshalText")
	}
	if err := tx.UnmarshalText(nil); err != nil || !tx.IsZero() {
		t.Fatal("UnmarshalText empty")
	}
	if b, _ := d.MarshalText(); string(b) != "2026-08-15" {
		t.Fatal("MarshalText")
	}
}

func TestDate_SQLRoundTrip(t *testing.T) {
	d := MustParse("2026-08-15")
	v, err := d.Value()
	if err != nil || v != "2026-08-15" {
		t.Fatalf("Value = %v, %v", v, err)
	}
	if v, _ := (Date{}).Value(); v != nil {
		t.Fatalf("zero Value = %v", v)
	}
	rome, _ := LoadLocation("Europe/Rome")
	sources := []any{
		"2026-08-15",
		[]byte("2026-08-15"),
		"2026-08-15T00:00:00Z",
		"2026-08-15 00:00:00+00:00",
		time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 15, 0, 0, 0, 0, rome), // a driver using the session timezone
	}
	for _, src := range sources {
		var got Date
		if err := got.Scan(src); err != nil || !got.Equal(d) {
			t.Errorf("Scan(%#v) = %v, %v", src, got, err)
		}
	}
	var n Date
	if err := n.Scan(nil); err != nil || !n.IsZero() {
		t.Fatal("Scan(nil)")
	}
	for _, bad := range []any{42, "2026-13-01", []byte("x")} {
		var x Date
		if err := x.Scan(bad); err == nil {
			t.Errorf("Scan(%#v) accepted", bad)
		}
	}
}

func TestRapid_DateRoundTrips(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MustParse("1970-01-01").AddDays(rapid.IntRange(-300_000, 2_900_000).Draw(t, "days"))
		if got, err := Parse(d.String()); err != nil || !got.Equal(d) {
			t.Fatalf("Parse(%s) = %v, %v", d, got, err)
		}
		v, _ := d.Value()
		var s Date
		if err := s.Scan(v); err != nil || !s.Equal(d) {
			t.Fatalf("Scan(Value(%s)) = %v, %v", d, s, err)
		}
		n := rapid.IntRange(-1000, 1000).Draw(t, "n")
		if d.DaysUntil(d.AddDays(n)) != n {
			t.Fatalf("DaysUntil(AddDays(%d)) mismatch", n)
		}
	})
}
