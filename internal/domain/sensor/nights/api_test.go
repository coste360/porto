package nights

import (
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
)

func TestNights_Table(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, rome)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	d := calendar.MustParse("2027-06-12")
	cases := []struct {
		name    string
		before  string
		changes []Change
		night   calendar.Date
		want    string
	}{
		{"all night occupied", Occupied, nil, d, Occupied},
		{"all night free", Free, nil, d, Free},
		{"unknown all night", Unknown, nil, d, Unknown},
		{"empty before is unknown", "", nil, d, Unknown},
		{"tie 240/240 is occupied", Free, []Change{{Occupied, at("2027-06-13 02:00")}}, d, Occupied},
		{"occupied 239 is free", Free, []Change{{Occupied, at("2027-06-13 02:01")}}, d, Free},
		{"free 241 occupied 239", Occupied, []Change{{Free, at("2027-06-12 23:59")}}, d, Free},
		{"occupied exactly 240 then unknown", Unknown, []Change{{Occupied, at("2027-06-12 22:00")}, {Unknown, at("2027-06-13 02:00")}}, d, Occupied},
		{"occupied 239 then unknown", Unknown, []Change{{Occupied, at("2027-06-12 22:00")}, {Unknown, at("2027-06-13 01:59")}}, d, Unknown},
		{"gap of unknown splits the majority", Occupied, []Change{{Unknown, at("2027-06-12 23:00")}, {Free, at("2027-06-13 03:00")}}, d, Unknown},
		{"changes outside the window", Free, []Change{{Occupied, at("2027-06-12 10:00")}, {Free, at("2027-06-13 07:00")}}, d, Occupied},
		{"change at window end ignored", Free, []Change{{Occupied, at("2027-06-13 06:00")}}, d, Free},
		// 2027-03-28: clocks go forward at 02:00, the window is 420 real minutes.
		{"DST forward night 180 occupied 240 free", Free, []Change{{Occupied, at("2027-03-27 22:00")}, {Free, at("2027-03-28 01:00")}}, calendar.MustParse("2027-03-27"), Free},
		{"DST forward night 240 real occupied", Free, []Change{{Occupied, at("2027-03-27 22:00")}, {Free, at("2027-03-28 03:00")}}, calendar.MustParse("2027-03-27"), Occupied},
		// 2027-10-31: clocks go back at 03:00, the window is 540 real minutes.
		{"DST long night 270/270 tie occupied", Free, []Change{{Occupied, at("2027-10-30 22:00")}, {Free, time.Date(2027, 10, 31, 0, 30, 0, 0, time.UTC)}}, calendar.MustParse("2027-10-30"), Occupied},
		{"DST long night occupied 239 free 301", Occupied, []Change{{Free, at("2027-10-30 22:00").Add(239 * time.Minute)}}, calendar.MustParse("2027-10-30"), Free},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.before, c.changes, c.night, c.night, rome)
			if got[c.night] != c.want {
				occ, free := Minutes(c.before, c.changes, func() time.Time { s, _ := Window(c.night, rome); return s }(), func() time.Time { _, e := Window(c.night, rome); return e }())
				t.Fatalf("got %s want %s (occ %d free %d)", got[c.night], c.want, occ, free)
			}
		})
	}
}

func TestNights_WindowRealMinutes(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	for night, want := range map[string]time.Duration{"2027-06-12": 8 * time.Hour, "2027-03-27": 7 * time.Hour, "2027-10-30": 9 * time.Hour} {
		s, e := Window(calendar.MustParse(night), rome)
		if e.Sub(s) != want {
			t.Fatalf("%s: window %s want %s", night, e.Sub(s), want)
		}
	}
}

func TestNights_RangeAndStateAt(t *testing.T) {
	from, to := calendar.MustParse("2027-06-01"), calendar.MustParse("2027-06-03")
	ch := []Change{{Occupied, time.Date(2027, 6, 2, 12, 0, 0, 0, time.UTC)}}
	got := Classify(Free, ch, from, to, time.UTC)
	if len(got) != 3 || got[from] != Free || got[from.AddDays(1)] != Occupied || got[to] != Occupied {
		t.Fatalf("got %v", got)
	}
	if StateAt(Free, ch, time.Date(2027, 6, 2, 11, 59, 0, 0, time.UTC)) != Free || StateAt(Free, ch, ch[0].At) != Occupied {
		t.Fatal("StateAt")
	}
}
