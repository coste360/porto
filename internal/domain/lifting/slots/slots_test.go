package slots

import (
	"testing"
	"time"
)

func TestSlots_FromClockAndBack(t *testing.T) {
	cases := []struct{ h, m, slot int }{{0, 0, 0}, {7, 0, 28}, {7, 14, 28}, {7, 15, 29}, {19, 0, 76}, {23, 59, 95}}
	for _, c := range cases {
		got, err := FromClock(c.h, c.m)
		if err != nil || got != c.slot {
			t.Fatalf("FromClock(%d,%d) = %d, %v; want %d", c.h, c.m, got, err, c.slot)
		}
		h, m, err := Clock(c.slot)
		if err != nil || h != c.h || m != c.m-c.m%15 {
			t.Fatalf("Clock(%d) = %d:%d, %v", c.slot, h, m, err)
		}
	}
	for _, bad := range [][2]int{{-1, 0}, {24, 0}, {0, 60}, {0, -1}} {
		if _, err := FromClock(bad[0], bad[1]); err == nil {
			t.Fatalf("FromClock(%v) accepted", bad)
		}
	}
	if _, _, err := Clock(97); err == nil {
		t.Fatal("Clock(97) accepted")
	}
	if Label(96) != "24:00" || Label(29) != "07:15" || Label(-1) != "" {
		t.Fatal("labels")
	}
}

func TestSlots_Window(t *testing.T) {
	lift := Window{DefaultLiftFrom, DefaultLiftTo}
	if !SafeWindow(lift) {
		t.Fatal("default window must be safe")
	}
	if SafeWindow(Window{0, 96}) || SafeWindow(Window{10, 9}) {
		t.Fatal("window with DST gap or empty accepted")
	}
	cases := []struct {
		w    Window
		want bool
	}{
		{Window{28, 30}, true},
		{Window{74, 76}, true},
		{Window{27, 30}, false},
		{Window{74, 77}, false},
		{Window{30, 30}, false},
		{Window{31, 30}, false},
	}
	for _, c := range cases {
		if got := Within(c.w, lift); got != c.want {
			t.Fatalf("Within(%v) = %v", c.w, got)
		}
	}
	if Within(Window{8, 9}, Window{0, 96}) || Within(Window{11, 13}, Window{0, 96}) {
		t.Fatal("DST gap accepted")
	}
	if got := Range(Window{28, 31}); len(got) != 3 || got[0] != 28 || got[2] != 30 {
		t.Fatalf("Range = %v", got)
	}
	if Range(Window{3, 3}) != nil {
		t.Fatal("empty range")
	}
	if d := Duration(28, 30); d != (Window{28, 30}) {
		t.Fatalf("Duration = %v", d)
	}
	if d := Duration(28, 31); d != (Window{28, 31}) {
		t.Fatalf("Duration rounding = %v", d)
	}
	if !(Window{28, 32}).Overlaps(Window{31, 40}) || (Window{28, 32}).Overlaps(Window{32, 40}) {
		t.Fatal("overlaps")
	}
}

func TestSlots_OfWallClockOnDSTDay(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 2026-03-29 is the spring change in Rome: 09:30 local is still slot 38.
	tm := time.Date(2026, 3, 29, 9, 30, 0, 0, rome)
	if Of(tm) != 38 {
		t.Fatalf("Of = %d", Of(tm))
	}
}
