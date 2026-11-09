package app

import (
	"testing"
	"time"
)

// TestSeedHandoverTimes_Clamped: today's demo handovers happen at the booked times (check-out ten minutes before
// the start, return five minutes before the end), never after the seed moment, the return never before the
// check-out.
func TestSeedHandoverTimes_Clamped(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, time.July, 10, h, m, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name              string
		start, end, now   time.Time
		wantOut, wantBack time.Time
	}{
		{"seeded after the end", at(8, 0), at(9, 30), at(13, 0), at(7, 50), at(9, 25)},
		{"seeded during the rental", at(8, 0), at(13, 0), at(10, 0), at(7, 50), at(10, 0)},
		{"seeded before the start", at(8, 0), at(9, 30), at(6, 0), at(6, 0), at(6, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, back := seedHandoverTimes(tc.start, tc.end, tc.now)
			if !out.Equal(tc.wantOut) || !back.Equal(tc.wantBack) {
				t.Fatalf("got %s / %s, want %s / %s", out, back, tc.wantOut, tc.wantBack)
			}
		})
	}
}
