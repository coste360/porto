package qty

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"pgregory.net/rapid"
)

func genMilli(t *rapid.T, label string) Milli { return Milli(rapid.Int64().Draw(t, label)) }

// genSmall draws quantities up to ±1e9 (realistic hours, metres, kWh).
func genSmall(t *rapid.T, label string) Milli {
	return Milli(rapid.Int64Range(-1e12, 1e12).Draw(t, label))
}

func TestRapid_ParseStringRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := genMilli(t, "m")
		got, err := Parse(m.String())
		if err != nil || got != m {
			t.Fatalf("Parse(%q) = %d, %v; want %d", m.String(), got, err, m)
		}
	})
}

func TestRapid_FormatParseRoundTrip3(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := genMilli(t, "m")
		sep := rapid.SampledFrom([]byte{'.', ','}).Draw(t, "sep")
		got, err := Parse(m.Format(3, sep))
		if err != nil || got != m {
			t.Fatalf("Parse(%q) = %d, %v; want %d", m.Format(3, sep), got, err, m)
		}
	})
}

func TestRapid_FormatMatchesRound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := genSmall(t, "m")
		d := rapid.IntRange(0, 3).Draw(t, "d")
		got, err := Parse(m.Format(d, '.'))
		if err != nil || got != m.Round(d) {
			t.Fatalf("Format(%d) of %s = %q, Round = %s", d, m, m.Format(d, '.'), m.Round(d))
		}
	})
}

func TestRapid_MulCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genMilli(t, "a"), genMilli(t, "b")
		x, errX := a.Mul(b)
		y, errY := b.Mul(a)
		if x != y || !errors.Is(errX, errY) {
			t.Fatalf("%s×%s = %s,%v but %s,%v", a, b, x, errX, y, errY)
		}
	})
}

func TestRapid_MulByOneIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMilli(t, "a")
		if got, err := a.Mul(One); err != nil || got != a {
			t.Fatalf("%s × 1 = %s, %v", a, got, err)
		}
		if got, err := a.Div(One); err != nil || got != a {
			t.Fatalf("%s ÷ 1 = %s, %v", a, got, err)
		}
	})
}

func TestRapid_DivMulBound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSmall(t, "a")
		b := Milli(rapid.Int64Range(-1e6, 1e6).Filter(func(v int64) bool { return v != 0 }).Draw(t, "b"))
		p, err := a.Mul(b)
		if err != nil {
			t.Fatal(err)
		}
		q, err := p.Div(b)
		if err != nil {
			t.Fatal(err)
		}
		// a·b is rounded by ≤ 0.5 milli, which the division scales by 1000/|b|; the division then rounds by
		// ≤ 0.5 milli more. For |b| ≥ 1 the result is within 1 milli of a.
		diff := absInt(int64(q - a))
		bound := 500/absInt(int64(b)) + 1
		if diff > bound {
			t.Fatalf("Div(Mul(%s, %s)) = %s, off by %d milli (bound %d)", a, b, q, diff, bound)
		}
	})
}

func absInt(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRapid_AddSubInverse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genMilli(t, "a"), genMilli(t, "b")
		s, err := a.Add(b)
		if err != nil {
			// Overflow is reported exactly when the true sum leaves int64.
			if new(big.Int).Add(big.NewInt(int64(a)), big.NewInt(int64(b))).IsInt64() {
				t.Fatalf("spurious overflow %d + %d", a, b)
			}
			return
		}
		back, err := s.Sub(b)
		if err != nil || back != a {
			t.Fatalf("(%d + %d) - %d = %d, %v", a, b, b, back, err)
		}
	})
}

func TestRapid_RoundIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := genMilli(t, "m")
		d := rapid.IntRange(0, 3).Draw(t, "d")
		r := m.Round(d)
		if r.Round(d) != r {
			t.Fatalf("Round(%d) not idempotent on %d: %d → %d", d, m, r, r.Round(d))
		}
		if int64(r)%pow10[d] != 0 {
			t.Fatalf("Round(%d) of %d = %d has extra digits", d, m, r)
		}
		if diff := absInt(int64(r - m)); diff > pow10[d] {
			t.Fatalf("Round(%d) of %d moved by %d", d, m, diff)
		}
	})
}

func TestRapid_MulMatchesBigRat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := genMilli(t, "a"), genMilli(t, "b")
		if rapid.Bool().Draw(t, "small") {
			a, b = genSmall(t, "sa"), genSmall(t, "sb")
		}
		got, err := a.Mul(b)
		want, ok := refMulDiv(int64(a), int64(b), Scale)
		if !ok {
			if !errors.Is(err, ErrOverflow) {
				t.Fatalf("%d×%d: want overflow, got %d, %v", a, b, got, err)
			}
			return
		}
		if err != nil || int64(got) != want {
			t.Fatalf("%d×%d = %d, %v; want %d", a, b, got, err, want)
		}
	})
}

func TestRapid_DivMatchesBigRat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMilli(t, "a")
		b := genMilli(t, "b")
		if b == 0 {
			b = math.MinInt64
		}
		got, err := a.Div(b)
		want, ok := refMulDiv(int64(a), Scale, int64(b))
		if !ok {
			if !errors.Is(err, ErrOverflow) {
				t.Fatalf("%d÷%d: want overflow, got %d, %v", a, b, got, err)
			}
			return
		}
		if err != nil || int64(got) != want {
			t.Fatalf("%d÷%d = %d, %v; want %d", a, b, got, err, want)
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"0", "1.5", "-0,125", " 2.000 ", "1.2345", "9223372036854775.807", "1e3", "--1", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m, err := Parse(s)
		if err != nil {
			return
		}
		back, err := Parse(m.String())
		if err != nil || back != m {
			t.Fatalf("Parse(%q) = %d but String %q re-parses to %d, %v", s, m, m.String(), back, err)
		}
	})
}
