package due

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }
func dp(s string) *calendar.Date {
	x := d(s)
	return &x
}

func TestNext_Scenario(t *testing.T) {
	from := d("2026-03-10")
	cases := []struct {
		name string
		p    Periodicity
		s    State
		want string // "" = not computable
	}{
		{"months from last done", Periodicity{Kind: KindMonths, Every: 6}, State{LastDone: dp("2025-11-15")}, "2026-05-15"},
		{"months end of month clamp", Periodicity{Kind: KindMonths, Every: 1}, State{LastDone: dp("2026-01-31")}, "2026-02-28"},
		{"months leap clamp", Periodicity{Kind: KindMonths, Every: 1}, State{LastDone: dp("2028-01-31")}, "2028-02-29"},
		{"months across years", Periodicity{Kind: KindMonths, Every: 14}, State{LastDone: dp("2025-12-31")}, "2027-02-28"},
		{"months from start", Periodicity{Kind: KindMonths, Every: 3, StartsOn: dp("2026-01-01")}, State{}, "2026-04-01"},
		{"months from creation", Periodicity{Kind: KindMonths, Every: 12}, State{}, "2027-03-10"},
		{"months overdue", Periodicity{Kind: KindMonths, Every: 1}, State{LastDone: dp("2025-01-10")}, "2025-02-10"},
		{"fixed date this year", Periodicity{Kind: KindFixedDate, MonthDay: "04-30"}, State{}, "2026-04-30"},
		{"fixed date next year after done", Periodicity{Kind: KindFixedDate, MonthDay: "04-30"}, State{LastDone: dp("2026-04-30")}, "2027-04-30"},
		{"fixed date done early", Periodicity{Kind: KindFixedDate, MonthDay: "04-30"}, State{LastDone: dp("2026-04-20")}, "2026-04-30"},
		{"fixed date passed from start", Periodicity{Kind: KindFixedDate, MonthDay: "01-15", StartsOn: dp("2026-02-01")}, State{}, "2027-01-15"},
		{"fixed leap day common year", Periodicity{Kind: KindFixedDate, MonthDay: "02-29"}, State{LastDone: dp("2026-03-01")}, "2027-02-28"},
		{"fixed leap day leap year", Periodicity{Kind: KindFixedDate, MonthDay: "02-29"}, State{LastDone: dp("2027-03-01")}, "2028-02-29"},
		{"cycles reached", Periodicity{Kind: KindCycles, Every: 250_000}, State{LastCycles: 1000_000, CurrentCycles: 1250_000}, "2026-03-10"},
		{"cycles not reached", Periodicity{Kind: KindCycles, Every: 250_000}, State{LastCycles: 1000_000, CurrentCycles: 1249_999}, ""},
		{"invalid months", Periodicity{Kind: KindMonths, Every: 0}, State{}, ""},
		{"invalid fixed", Periodicity{Kind: KindFixedDate, MonthDay: "02-30"}, State{}, ""},
		{"invalid kind", Periodicity{Kind: "weeks", Every: 1}, State{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Next(c.p, c.s, from)
			if c.want == "" {
				if ok {
					t.Fatalf("got %s, want not computable", got)
				}
				return
			}
			if !ok || got.String() != c.want {
				t.Fatalf("got %s (%v), want %s", got, ok, c.want)
			}
		})
	}
}

func TestParseMonthDay_Scenario(t *testing.T) {
	for s, ok := range map[string]bool{"01-01": true, "02-29": true, "12-31": true, "13-01": false, "04-31": false, "4-1": false, "aa-bb": false} {
		if _, _, got := ParseMonthDay(s); got != ok {
			t.Errorf("%s: got %v want %v", s, got, ok)
		}
	}
}

// TestRapid_DueNotBeforeLastDone: months and fixed-date plans are never due before (months: on) the last done.
func TestRapid_DueNotBeforeLastDone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := d("2020-01-01")
		last := base.AddDays(rapid.IntRange(0, 4000).Draw(t, "last"))
		from := base.AddDays(rapid.IntRange(0, 5000).Draw(t, "from"))
		var p Periodicity
		if rapid.Bool().Draw(t, "months") {
			p = Periodicity{Kind: KindMonths, Every: int64(rapid.IntRange(1, MaxEveryMonths).Draw(t, "every"))}
		} else {
			m := rapid.IntRange(1, 12).Draw(t, "m")
			day := rapid.IntRange(1, 28).Draw(t, "day")
			if rapid.Bool().Draw(t, "leap") {
				m, day = 2, 29
			}
			p = Periodicity{Kind: KindFixedDate, MonthDay: twoDigits(m) + "-" + twoDigits(day)}
		}
		got, ok := Next(p, State{LastDone: &last}, from)
		if !ok {
			t.Fatalf("not computable: %+v", p)
		}
		if !got.After(last) {
			t.Fatalf("due %s not after last done %s (%+v)", got, last, p)
		}
		if p.Kind == KindFixedDate && last.DaysUntil(got) > 366 {
			t.Fatalf("fixed date %s more than a year after %s", got, last)
		}
	})
}

func TestRapid_CyclesThreshold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		every := rapid.Int64Range(1, 1_000_000).Draw(t, "every")
		last := qty.Milli(rapid.Int64Range(0, 1_000_000_000).Draw(t, "last"))
		adv := rapid.Int64Range(0, 2_000_000).Draw(t, "adv")
		_, ok := Next(Periodicity{Kind: KindCycles, Every: every}, State{LastCycles: last, CurrentCycles: last + qty.Milli(adv)}, d("2026-01-01"))
		if ok != (adv >= every) {
			t.Fatalf("every %d advanced %d: ok %v", every, adv, ok)
		}
	})
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
