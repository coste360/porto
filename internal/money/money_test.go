package money

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"pgregory.net/rapid"
)

func TestCents_String(t *testing.T) {
	tests := []struct {
		in   Cents
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{-5, "-0.05"},
		{100, "1.00"},
		{1234, "12.34"},
		{-1234, "-12.34"},
		{math.MaxInt64, "92233720368547758.07"},
		{math.MinInt64, "-92233720368547758.08"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Cents(%d).String() = %q, want %q", int64(tt.in), got, tt.want)
		}
	}
}

func TestParseCents(t *testing.T) {
	valid := []struct {
		in   string
		want Cents
	}{
		{"0", 0},
		{"12", 1200},
		{"12.3", 1230},
		{"12,30", 1230},
		{"-0.05", -5},
		{"+4", 400},
		{" 7.5 ", 750},
		{"0.00", 0},
		{"-0", 0},
		{"92233720368547758.07", math.MaxInt64},
		{"-92233720368547758.08", math.MinInt64},
	}
	for _, tt := range valid {
		got, err := ParseCents(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
	invalid := []string{"", " ", "-", "+", ".", "1.", ".5", ",5", "1.234", "1.2.3", "1,2.3", "1e3", "1 000", "1.000,00", "abc", "--1", "+-1", "0x10"}
	for _, in := range invalid {
		if _, err := ParseCents(in); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("ParseCents(%q) err = %v, want ErrInvalidAmount", in, err)
		}
	}
	overflow := []string{"92233720368547758.08", "-92233720368547758.09", "99999999999999999999999"}
	for _, in := range overflow {
		if _, err := ParseCents(in); !errors.Is(err, ErrOverflow) {
			t.Errorf("ParseCents(%q) err = %v, want ErrOverflow", in, err)
		}
	}
}

func TestDivRoundHalfEven(t *testing.T) {
	tests := []struct{ num, den, want int64 }{
		{10, 2, 5},
		{5, 2, 2},   // 2.5 -> 2
		{7, 2, 4},   // 3.5 -> 4
		{-5, 2, -2}, // -2.5 -> -2
		{-7, 2, -4}, // -3.5 -> -4
		{5, -2, -2},
		{-5, -2, 2},
		{1, 3, 0},
		{2, 3, 1},
		{-2, 3, -1},
		{0, 7, 0},
		{math.MaxInt64, 1, math.MaxInt64},
		{math.MinInt64, 1, math.MinInt64},
		{math.MinInt64, 2, math.MinInt64 / 2},
	}
	for _, tt := range tests {
		if got := DivRoundHalfEven(tt.num, tt.den); got != tt.want {
			t.Errorf("DivRoundHalfEven(%d, %d) = %d, want %d", tt.num, tt.den, got, tt.want)
		}
	}
	mustPanic(t, "den zero", func() { DivRoundHalfEven(1, 0) })
	mustPanic(t, "MinInt64/-1", func() { DivRoundHalfEven(math.MinInt64, -1) })
}

func TestMulRat(t *testing.T) {
	tests := []struct {
		c        Cents
		num, den int64
		want     Cents
	}{
		{1000, 1, 3, 333},
		{1000, 2, 3, 667},
		{250, 1, 100, 2}, // 2.5 -> 2
		{350, 1, 100, 4}, // 3.5 -> 4
		{-350, 1, 100, -4},
		{math.MaxInt64, 3, 3, math.MaxInt64}, // needs the 128-bit intermediate
		{math.MaxInt64 / 2, 2, 1, math.MaxInt64 - 1},
	}
	for _, tt := range tests {
		if got := tt.c.MulRat(tt.num, tt.den); got != tt.want {
			t.Errorf("%d.MulRat(%d, %d) = %d, want %d", tt.c, tt.num, tt.den, got, tt.want)
		}
	}
	if _, err := Cents(math.MaxInt64).MulRatChecked(2, 1); !errors.Is(err, ErrOverflow) {
		t.Errorf("overflow err = %v", err)
	}
	mustPanic(t, "MulRat overflow", func() { Cents(math.MaxInt64).MulRat(2, 1) })
}

func TestPercentBP(t *testing.T) {
	tests := []struct {
		c    Cents
		bp   int64
		want Cents
	}{
		{10000, 2200, 2200},
		{1050, 1000, 105},
		{25, 1000, 2}, // 2.5 -> 2
		{35, 1000, 4}, // 3.5 -> 4
		{12345, 0, 0},
		{-10000, 500, -500},
	}
	for _, tt := range tests {
		if got := tt.c.PercentBP(tt.bp); got != tt.want {
			t.Errorf("%d.PercentBP(%d) = %d, want %d", tt.c, tt.bp, got, tt.want)
		}
	}
}

func TestVATSplit(t *testing.T) {
	tests := []struct {
		gross    Cents
		rate     int64
		net, vat Cents
	}{
		{12200, 2200, 10000, 2200},
		{100, 2200, 82, 18}, // 81.967 -> 82
		{0, 2200, 0, 0},
		{1000, 0, 1000, 0},
		{11000, 1000, 10000, 1000},
		{-12200, 2200, -10000, -2200},
	}
	for _, tt := range tests {
		net, vat := VATSplit(tt.gross, tt.rate)
		if net != tt.net || vat != tt.vat {
			t.Errorf("VATSplit(%d, %d) = %d, %d; want %d, %d", tt.gross, tt.rate, net, vat, tt.net, tt.vat)
		}
	}
	mustPanic(t, "negative rate", func() { VATSplit(100, -1) })
}

func TestVATSplit_SumsToGross(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		gross := Cents(rapid.Int64Range(-1e15, 1e15).Draw(t, "gross"))
		rate := rapid.Int64Range(0, 10_000).Draw(t, "rate")
		net, vat := VATSplit(gross, rate)
		if net+vat != gross {
			t.Fatalf("net %d + vat %d != gross %d", net, vat, gross)
		}
		// net is the half-even rounding of gross*10000/(10000+rate): within half a cent.
		exact := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(int64(gross)), big.NewInt(10_000)), big.NewInt(10_000+rate))
		diff := new(big.Rat).Sub(exact, new(big.Rat).SetInt64(int64(net)))
		if diff.Abs(diff).Cmp(big.NewRat(1, 2)) > 0 {
			t.Fatalf("net %d too far from %s", net, exact.FloatString(4))
		}
	})
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name    string
		total   Cents
		weights []int64
		want    []Cents
	}{
		{"even", 300, []int64{1, 1, 1}, []Cents{100, 100, 100}},
		{"remainder to first on tie", 100, []int64{1, 1, 1}, []Cents{34, 33, 33}},
		{"two remainders", 200, []int64{1, 1, 1}, []Cents{67, 67, 66}},
		{"largest remainder wins", 100, []int64{1, 2}, []Cents{33, 67}},
		{"millesimi", 1000, []int64{333, 333, 334}, []Cents{333, 333, 334}},
		{"zero weight", 100, []int64{0, 1}, []Cents{0, 100}},
		{"negative total", -100, []int64{1, 1, 1}, []Cents{-34, -33, -33}},
		{"zero total", 0, []int64{5, 7}, []Cents{0, 0}},
		{"single", 12345, []int64{9}, []Cents{12345}},
		{"huge", math.MaxInt64, []int64{1, 1}, []Cents{math.MaxInt64/2 + 1, math.MaxInt64 / 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Allocate(tt.total, tt.weights)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
	for _, w := range [][]int64{nil, {}, {0, 0}, {1, -1}} {
		if _, err := Allocate(100, w); !errors.Is(err, ErrInvalidWeights) {
			t.Errorf("Allocate(100, %v) err = %v, want ErrInvalidWeights", w, err)
		}
	}
	if _, err := Allocate(100, []int64{math.MaxInt64, 1}); !errors.Is(err, ErrOverflow) {
		t.Errorf("weight sum overflow err = %v", err)
	}
}

func TestAllocate_SumsExactly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := Cents(rapid.Int64Range(-1e12, 1e12).Draw(t, "total"))
		weights := rapid.SliceOfN(rapid.Int64Range(0, 1e6), 1, 30).Draw(t, "weights")
		var wsum int64
		for _, w := range weights {
			wsum += w
		}
		got, err := Allocate(total, weights)
		if wsum == 0 {
			if !errors.Is(err, ErrInvalidWeights) {
				t.Fatalf("err = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		var sum Cents
		for i, s := range got {
			sum += s
			// Each share is within one cent of the exact proportional share.
			exact := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(int64(total)), big.NewInt(weights[i])), big.NewInt(wsum))
			diff := new(big.Rat).Sub(exact, new(big.Rat).SetInt64(int64(s)))
			if diff.Abs(diff).Cmp(big.NewRat(1, 1)) >= 0 {
				t.Fatalf("share %d = %d, exact %s", i, s, exact.FloatString(3))
			}
			if weights[i] == 0 && s != 0 {
				t.Fatalf("zero weight got %d", s)
			}
		}
		if sum != total {
			t.Fatalf("sum %d != total %d", sum, total)
		}
	})
}

func TestRapid_ParseStringRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := Cents(rapid.Int64().Draw(t, "c"))
		got, err := ParseCents(c.String())
		if err != nil || got != c {
			t.Fatalf("ParseCents(%q) = %d, %v; want %d", c.String(), got, err, c)
		}
	})
}

func TestRapid_MulRatMatchesBigRat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := rapid.Int64().Draw(t, "c")
		num := rapid.Int64Range(-1e9, 1e9).Draw(t, "num")
		den := rapid.Int64Range(-1e9, 1e9).Filter(func(v int64) bool { return v != 0 }).Draw(t, "den")
		got, err := Cents(c).MulRatChecked(num, den)
		want, ok := halfEvenRat(new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(c), big.NewInt(num)), big.NewInt(den)))
		if !ok {
			if !errors.Is(err, ErrOverflow) {
				t.Fatalf("want overflow, got %d, %v", got, err)
			}
			return
		}
		if err != nil || int64(got) != want {
			t.Fatalf("MulRat(%d, %d, %d) = %d, %v; want %d", c, num, den, got, err, want)
		}
	})
}

// halfEvenRat rounds r half-even to an int64 (test-only reference implementation).
func halfEvenRat(r *big.Rat) (int64, bool) {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int)) // truncated toward zero
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	switch twice.Cmp(r.Denom()) {
	case 1:
		q.Add(q, big.NewInt(int64(r.Sign())))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(int64(r.Sign())))
		}
	}
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	f()
}
