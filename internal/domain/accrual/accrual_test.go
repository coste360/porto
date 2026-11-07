package accrual

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

// TestAccrual_SumsToAmount is INV-60: the day quotas of a line sum to its amount, the remainder is on the last day,
// quotas outside the reporting period are not reported and Within agrees with Allocate.
func TestAccrual_SumsToAmount(t *testing.T) {
	base := d("2020-01-01")
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 4).Draw(t, "lines")
		var lines []Line
		for i := range n {
			days := rapid.IntRange(1, MaxDays).Draw(t, "days")
			from := base.AddDays(rapid.IntRange(0, 2000).Draw(t, "start"))
			amount := money.Cents(rapid.Int64Range(-1_000_000_000, 1_000_000_000).Draw(t, "amount"))
			lines = append(lines, Line{ID: strconv.Itoa(i), Amount: amount, From: from, To: from.AddDays(days - 1)})
		}
		// whole period: every quota of every line
		full, err := Allocate(lines, base, base.AddDays(2000+MaxDays))
		if err != nil {
			t.Fatal(err)
		}
		var total money.Cents
		for _, l := range lines {
			if full.PerLine[l.ID] != l.Amount {
				t.Fatalf("line %s: Σ %d, amount %d", l.ID, full.PerLine[l.ID], l.Amount)
			}
			total += l.Amount
			days := l.Days()
			q := DayQuota(l.Amount, days, 0)
			last := DayQuota(l.Amount, days, days-1)
			if days > 1 && last-q != money.Cents(int64(l.Amount)-int64(q)*int64(days)) {
				t.Fatalf("remainder not on the last day: first %d last %d", q, last)
			}
		}
		if full.Total != total {
			t.Fatalf("total %d, want %d", full.Total, total)
		}
		// a sub-period: quotas inside only, Within = PerLine
		from := base.AddDays(rapid.IntRange(0, 3000).Draw(t, "from"))
		to := from.AddDays(rapid.IntRange(0, 800).Draw(t, "len"))
		part, err := Allocate(lines, from, to)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range part.Quotas {
			if q.Day.Before(from) || q.Day.After(to) {
				t.Fatalf("quota outside the period: %s", q.Day)
			}
		}
		for _, l := range lines {
			w, err := Within(l, from, to)
			if err != nil {
				t.Fatal(err)
			}
			if w != part.PerLine[l.ID] {
				t.Fatalf("Within %d, Allocate %d", w, part.PerLine[l.ID])
			}
		}
	})
}

func TestAccrual_Scenario(t *testing.T) {
	tests := []struct {
		name     string
		line     Line
		from, to string
		want     money.Cents
		quotas   int
	}{
		{"100 over 3 days, all", Line{ID: "a", Amount: 100, From: d("2027-07-01"), To: d("2027-07-03")}, "2027-07-01", "2027-07-31", 100, 3},
		{"100 over 3 days, last day only", Line{ID: "a", Amount: 100, From: d("2027-07-01"), To: d("2027-07-03")}, "2027-07-03", "2027-07-03", 34, 1},
		{"100 over 3 days, first two", Line{ID: "a", Amount: 100, From: d("2027-07-01"), To: d("2027-07-03")}, "2027-06-01", "2027-07-02", 66, 2},
		{"disjoint", Line{ID: "a", Amount: 100, From: d("2027-07-01"), To: d("2027-07-03")}, "2027-08-01", "2027-08-02", 0, 0},
		{"negative", Line{ID: "a", Amount: -10, From: d("2027-07-01"), To: d("2027-07-03")}, "2027-07-03", "2027-07-03", -4, 1},
		{"zero", Line{ID: "a", Amount: 0, From: d("2027-07-01"), To: d("2027-07-01")}, "2027-07-01", "2027-07-01", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Allocate([]Line{tt.line}, d(tt.from), d(tt.to))
			if err != nil {
				t.Fatal(err)
			}
			if r.PerLine["a"] != tt.want || len(r.Quotas) != tt.quotas {
				t.Fatalf("got %d in %d quotas, want %d in %d", r.PerLine["a"], len(r.Quotas), tt.want, tt.quotas)
			}
		})
	}
	if _, err := Allocate([]Line{{ID: "x", Amount: 1, From: d("2027-07-02"), To: d("2027-07-01")}}, d("2027-07-01"), d("2027-07-02")); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("reversed line: %v", err)
	}
	if _, err := Allocate(nil, d("2027-07-02"), d("2027-07-01")); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("reversed period: %v", err)
	}
}

// TestAccrual_Purity checks the package stays pure (no clock, no randomness, no I/O).
func TestAccrual_Purity(t *testing.T) {
	allowed := map[string]bool{"github.com/coste360/porto/internal/calendar": true, "github.com/coste360/porto/internal/money": true}
	checkPurity(t, allowed)
}

func checkPurity(t *testing.T, allowed map[string]bool) {
	t.Helper()
	forbidden := map[string]bool{"time": true, "math/rand": true, "math/rand/v2": true, "crypto/rand": true, "os": true, "net": true, "net/http": true, "database/sql": true, "io/fs": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if (strings.Contains(path, ".") && !allowed[path]) || forbidden[path] {
				t.Errorf("%s imports %q", name, path)
			}
		}
	}
}
