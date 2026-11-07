package levels

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
)

func TestLevels_Next(t *testing.T) {
	due := calendar.MustParse("2027-03-31")
	p := DefaultPolicy()
	cases := []struct {
		today string
		last  string
		want  string
	}{
		{"2027-03-20", "", ""},
		{"2027-03-24", "", PreDue},
		{"2027-03-30", "", PreDue},
		{"2027-03-30", PreDue, ""},
		{"2027-03-31", PreDue, Due},
		{"2027-03-31", "", Due},
		{"2027-04-06", Due, ""},
		{"2027-04-07", Due, Courteous},
		{"2027-04-21", Courteous, Formal},
		{"2027-05-15", Formal, FinalNotice},
		{"2027-05-15", "", FinalNotice}, // job missed: the highest reached level only
		{"2027-06-30", FinalNotice, ""},
	}
	for _, c := range cases {
		got, ok := Next(due, calendar.MustParse(c.today), p, c.last)
		if got != c.want || ok != (c.want != "") {
			t.Fatalf("today %s last %q: got %q", c.today, c.last, got)
		}
	}
	// Disabled levels are skipped.
	p.Steps[2].Enabled = false
	if got, _ := Next(due, calendar.MustParse("2027-04-10"), p, Due); got != "" {
		t.Fatalf("disabled courteous: %q", got)
	}
	if got, _ := NextAfter(due, calendar.MustParse("2027-04-01"), p, Due); got != Formal {
		t.Fatalf("next after: %q", got)
	}
	if !NeedsApproval(FinalNotice) || NeedsApproval(Formal) {
		t.Fatal("approval")
	}
}

func TestLevels_ParsePolicy(t *testing.T) {
	if p := ParsePolicy(nil); p.PreDueDays != 7 || len(p.Steps) != 5 {
		t.Fatalf("default %+v", p)
	}
	p := ParsePolicy([]byte(`{"pre_due_days":3,"levels":[{"level":"formal","days":5,"enabled":true},{"level":"courteous","days":10,"enabled":false},{"level":"x","days":1}]}`))
	if p.PreDueDays != 3 || p.Steps[0].Days != 3 || p.Steps[2].Enabled || p.Steps[2].Days != 10 || p.Steps[3].Days != 10 {
		t.Fatalf("parsed %+v", p)
	}
	if p := ParsePolicy([]byte(`{"levels":[{"level":"pre_due","days":-5,"enabled":true}]}`)); p.PreDueDays != 5 || p.Steps[0].Days != 5 {
		t.Fatalf("negative pre_due %+v", p)
	}
	if p := ParsePolicy([]byte(`not json`)); p.PreDueDays != 7 {
		t.Fatal("invalid json")
	}
}

// TestLevels_NextProperty: simulating the daily job, the levels sent for a charge are strictly increasing, never
// repeated, pre-due only before the due date, and each sent level has been reached.
func TestLevels_NextProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pol := DefaultPolicy()
		pol.PreDueDays = rapid.IntRange(0, 30).Draw(t, "pre")
		pol.Steps[0].Days = pol.PreDueDays
		prev := 0
		for i := 2; i < 5; i++ {
			prev += rapid.IntRange(0, 40).Draw(t, "d")
			pol.Steps[i].Days = prev
		}
		for i := range pol.Steps {
			pol.Steps[i].Enabled = rapid.Bool().Draw(t, "en")
		}
		due := calendar.MustParse("2027-06-15").AddDays(rapid.IntRange(-200, 200).Draw(t, "shift"))
		start := due.AddDays(-60)
		last := ""
		seen := map[string]bool{}
		for d := 0; d < 200; d++ {
			if rapid.IntRange(0, 4).Draw(t, "skip") == 0 {
				continue // the job did not run that day
			}
			today := start.AddDays(d)
			l, ok := Next(due, today, pol, last)
			if !ok {
				continue
			}
			if Rank(l) <= Rank(last) || seen[l] {
				t.Fatalf("level %s after %s", l, last)
			}
			if l == PreDue && !today.Before(due) {
				t.Fatal("pre_due after due date")
			}
			if pol.TriggerOn(l, due).After(today) {
				t.Fatalf("%s before its trigger", l)
			}
			seen[l], last = true, l
		}
	})
}
