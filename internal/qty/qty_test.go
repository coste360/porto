package qty

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/coste360/porto/internal/money"
)

var (
	_ sql.Scanner   = (*Milli)(nil)
	_ driver.Valuer = Milli(0)
)

func TestParse_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want Milli
	}{
		{"0", 0},
		{"1", 1000},
		{"1.5", 1500},
		{"1,5", 1500},
		{"-0.125", -125},
		{"+3", 3000},
		{" 2.000 ", 2000},
		{"0.001", 1},
		{"-0", 0},
		{"007", 7000},
		{"999999999999.999", 999_999_999_999_999},
		{"9223372036854775.807", math.MaxInt64},
		{"-9223372036854775.808", math.MinInt64},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestParse_Invalid(t *testing.T) {
	for _, in := range []string{"", " ", "-", "+", ".", "1.", ".5", ",5", "1.2.3", "1,2.3", "1e3", "1 000", "abc", "--1", "+-1", "1_000", "0x1", "１"} {
		if _, err := Parse(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", in, err)
		}
	}
}

func TestParse_TooPrecise(t *testing.T) {
	for _, in := range []string{"1.2345", "0.0001", "-3,1415"} {
		if _, err := Parse(in); !errors.Is(err, ErrTooPrecise) {
			t.Errorf("Parse(%q) err = %v, want ErrTooPrecise", in, err)
		}
	}
}

func TestParse_Overflow(t *testing.T) {
	for _, in := range []string{"9223372036854775.808", "-9223372036854775.809", "99999999999999999999", "18446744073709551616"} {
		if _, err := Parse(in); !errors.Is(err, ErrOverflow) {
			t.Errorf("Parse(%q) err = %v, want ErrOverflow", in, err)
		}
	}
}

func TestMustParse_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	MustParse("x")
}

func TestString_Canonical(t *testing.T) {
	tests := []struct {
		in   Milli
		want string
	}{
		{0, "0"},
		{1000, "1"},
		{12000, "12"},
		{12500, "12.5"},
		{12050, "12.05"},
		{-125, "-0.125"},
		{1, "0.001"},
		{-1, "-0.001"},
		{math.MaxInt64, "9223372036854775.807"},
		{math.MinInt64, "-9223372036854775.808"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Milli(%d).String() = %q, want %q", int64(tt.in), got, tt.want)
		}
	}
}

func TestFormat_FixedDecimals(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		sep      byte
		want     string
	}{
		{"12.5", 2, ',', "12,50"},
		{"12.5", 3, '.', "12.500"},
		{"12.5", 0, '.', "12"}, // tie → even
		{"13.5", 0, '.', "14"},
		{"1.125", 2, '.', "1.12"},
		{"1.135", 2, '.', "1.14"},
		{"-1.125", 2, '.', "-1.12"},
		{"0.4", 0, '.', "0"},
		{"-0.4", 0, '.', "0"}, // no "-0"
		{"-0.6", 0, '.', "-1"},
		{"2", 1, ',', "2,0"},
		{"2.25", 9, '.', "2.250"}, // clamped to 3
		{"2.25", -1, '.', "2"},    // clamped to 0
	}
	for _, tt := range tests {
		m := MustParse(tt.in)
		if got := m.Format(tt.decimals, tt.sep); got != tt.want {
			t.Errorf("%s.Format(%d, %q) = %q, want %q", tt.in, tt.decimals, tt.sep, got, tt.want)
		}
	}
}

func TestRound_HalfEven(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		want     string
	}{
		{"1.125", 2, "1.12"},
		{"1.135", 2, "1.14"},
		{"1.126", 2, "1.13"},
		{"-1.125", 2, "-1.12"},
		{"-1.135", 2, "-1.14"},
		{"2.5", 0, "2"},
		{"3.5", 0, "4"},
		{"-2.5", 0, "-2"},
		{"0.05", 1, "0"},
		{"0.15", 1, "0.2"},
		{"1.234", 3, "1.234"},
	}
	for _, tt := range tests {
		if got := MustParse(tt.in).Round(tt.decimals); got != MustParse(tt.want) {
			t.Errorf("Round(%s, %d) = %s, want %s", tt.in, tt.decimals, got, tt.want)
		}
	}
	// At the int64 edge rounding up would overflow: it rounds towards zero.
	if got := Milli(math.MaxInt64).Round(0); got != math.MaxInt64-807 {
		t.Errorf("Round(MaxInt64, 0) = %d", got)
	}
	if got := Milli(math.MinInt64).Round(0); got != math.MinInt64+808 {
		t.Errorf("Round(MinInt64, 0) = %d", got)
	}
}

func TestAddSub(t *testing.T) {
	if s, err := MustParse("1.5").Add(MustParse("2.25")); err != nil || s != MustParse("3.75") {
		t.Errorf("Add = %s, %v", s, err)
	}
	if d, err := MustParse("1.5").Sub(MustParse("2.25")); err != nil || d != MustParse("-0.75") {
		t.Errorf("Sub = %s, %v", d, err)
	}
	overflows := []func() (Milli, error){
		func() (Milli, error) { return Milli(math.MaxInt64).Add(1) },
		func() (Milli, error) { return Milli(math.MinInt64).Add(-1) },
		func() (Milli, error) { return Milli(math.MinInt64).Sub(1) },
		func() (Milli, error) { return Milli(math.MaxInt64).Sub(-1) },
		func() (Milli, error) { return Milli(0).Sub(math.MinInt64) },
	}
	for i, f := range overflows {
		if _, err := f(); !errors.Is(err, ErrOverflow) {
			t.Errorf("case %d: err = %v, want ErrOverflow", i, err)
		}
	}
}

func TestMul_HalfEven(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"0.001", "0.5", "0"},     // 0.0005 → tie → even 0
		{"0.003", "0.5", "0.002"}, // 0.0015 → 0.002
		{"0.001", "1.5", "0.002"}, // 0.0015 → 0.002
		{"0.005", "0.5", "0.002"}, // 0.0025 → 0.002
		{"1.5", "1.5", "2.25"},
		{"-1.5", "1.5", "-2.25"},
		{"-0.003", "0.5", "-0.002"},
		{"12", "0", "0"},
		{"1000000", "1000000", "1000000000000"},
	}
	for _, tt := range tests {
		got, err := MustParse(tt.a).Mul(MustParse(tt.b))
		if err != nil || got != MustParse(tt.want) {
			t.Errorf("%s × %s = %s, %v; want %s", tt.a, tt.b, got, err, tt.want)
		}
	}
}

func TestMul_Overflow(t *testing.T) {
	if _, err := Milli(math.MaxInt64).Mul(MustParse("2")); !errors.Is(err, ErrOverflow) {
		t.Errorf("err = %v", err)
	}
	if _, err := MustParse("10000000000").Mul(MustParse("10000000000")); !errors.Is(err, ErrOverflow) {
		t.Errorf("err = %v", err)
	}
	// Needs the 128-bit intermediate but fits.
	if got, err := Milli(math.MaxInt64).Mul(One); err != nil || got != math.MaxInt64 {
		t.Errorf("MaxInt64 × 1 = %d, %v", got, err)
	}
	if _, err := Milli(5).MulInt(math.MaxInt64); !errors.Is(err, ErrOverflow) {
		t.Errorf("MulInt err = %v", err)
	}
}

func TestDiv_HalfEven(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"1", "3", "0.333"},
		{"2", "3", "0.667"},
		{"1", "8", "0.125"},
		{"0.001", "2", "0"}, // 0.0005 → tie → 0
		{"0.003", "2", "0.002"},
		{"-1", "3", "-0.333"},
		{"1", "-8", "-0.125"},
		{"10", "0.5", "20"},
	}
	for _, tt := range tests {
		got, err := MustParse(tt.a).Div(MustParse(tt.b))
		if err != nil || got != MustParse(tt.want) {
			t.Errorf("%s ÷ %s = %s, %v; want %s", tt.a, tt.b, got, err, tt.want)
		}
	}
	if got, err := MustParse("1").DivInt(3); err != nil || got != MustParse("0.333") {
		t.Errorf("DivInt = %s, %v", got, err)
	}
	if got, err := MustParse("1.5").MulInt(3); err != nil || got != MustParse("4.5") {
		t.Errorf("MulInt = %s, %v", got, err)
	}
}

func TestDiv_ByZero(t *testing.T) {
	if _, err := One.Div(0); !errors.Is(err, ErrDivByZero) {
		t.Errorf("Div err = %v", err)
	}
	if _, err := One.DivInt(0); !errors.Is(err, ErrDivByZero) {
		t.Errorf("DivInt err = %v", err)
	}
	if _, err := FromFraction(1, 0); !errors.Is(err, ErrDivByZero) {
		t.Errorf("FromFraction err = %v", err)
	}
}

func TestFromIntAndFraction(t *testing.T) {
	if m, err := FromInt(7); err != nil || m != 7000 {
		t.Errorf("FromInt = %d, %v", m, err)
	}
	if _, err := FromInt(math.MaxInt64 / 100); !errors.Is(err, ErrOverflow) {
		t.Errorf("FromInt overflow err = %v", err)
	}
	tests := []struct {
		num, den int64
		want     string
	}{
		{90, 60, "1.5"},
		{1, 3, "0.333"},
		{2, 3, "0.667"},
		{-45, 60, "-0.75"},
		{1, 2000, "0"}, // 0.0005 → 0
		{3, 2000, "0.002"},
	}
	for _, tt := range tests {
		if got, err := FromFraction(tt.num, tt.den); err != nil || got != MustParse(tt.want) {
			t.Errorf("FromFraction(%d, %d) = %s, %v; want %s", tt.num, tt.den, got, err, tt.want)
		}
	}
}

func TestMulCents(t *testing.T) {
	tests := []struct {
		q    string
		c    money.Cents
		want money.Cents
	}{
		{"3.5", 4000, 14000},
		{"0.333", 100, 33}, // 33.3
		{"0.005", 100, 0},  // 0.5 → 0
		{"0.015", 100, 2},  // 1.5 → 2
		{"0.025", 100, 2},  // 2.5 → 2
		{"-2", 150, -300},
		{"1", -99, -99},
	}
	for _, tt := range tests {
		got, err := MustParse(tt.q).MulCents(tt.c)
		if err != nil || got != tt.want {
			t.Errorf("%s × %d = %d, %v; want %d", tt.q, tt.c, got, err, tt.want)
		}
	}
	if _, err := MustParse("1000000").MulCents(money.Cents(math.MaxInt64 / 10)); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("overflow err = %v", err)
	}
}

func TestSignCmp(t *testing.T) {
	if MustParse("-1").Sign() != -1 || Milli(0).Sign() != 0 || One.Sign() != 1 {
		t.Error("Sign")
	}
	if !Milli(0).IsZero() || One.IsZero() {
		t.Error("IsZero")
	}
	if One.Cmp(2*One) != -1 || One.Cmp(MustParse("1")) != 0 || (2*One).Cmp(One) != 1 {
		t.Error("Cmp")
	}
}

func TestJSON_RoundTrip(t *testing.T) {
	type payload struct {
		Hours Milli            `json:"hours"`
		ByKey map[Milli]string `json:"by_key"`
	}
	in := payload{Hours: MustParse("1.5"), ByKey: map[Milli]string{MustParse("2.25"): "x"}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"hours":1500,"by_key":{"2.25":"x"}}` {
		t.Fatalf("json = %s", b)
	}
	var out payload
	if err := json.Unmarshal(b, &out); err != nil || out.Hours != in.Hours || out.ByKey[MustParse("2.25")] != "x" {
		t.Fatalf("unmarshal = %+v, %v", out, err)
	}
	for _, bad := range []string{`"1.5"`, `1.5`, `1e3`, `null`, `true`} {
		var m Milli
		if err := json.Unmarshal([]byte(bad), &m); err == nil && bad != "null" {
			t.Errorf("Unmarshal(%s) accepted", bad)
		}
	}
}

func TestText_RoundTrip(t *testing.T) {
	m := MustParse("-12.05")
	b, _ := m.MarshalText()
	var back Milli
	if err := back.UnmarshalText(b); err != nil || back != m {
		t.Fatalf("text round trip %s → %s, %v", m, back, err)
	}
	if err := back.UnmarshalText([]byte("1.2345")); !errors.Is(err, ErrTooPrecise) {
		t.Fatalf("UnmarshalText err = %v", err)
	}
}

func TestSQL_ScanValue(t *testing.T) {
	m := MustParse("12.345")
	v, err := m.Value()
	if err != nil || v != int64(12345) {
		t.Fatalf("Value = %v, %v", v, err)
	}
	for _, src := range []any{int64(12345), "12345", []byte("12345")} {
		var got Milli
		if err := got.Scan(src); err != nil || got != m {
			t.Errorf("Scan(%#v) = %d, %v", src, got, err)
		}
	}
	for _, bad := range []any{nil, 1.5, "12.345", []byte("x")} {
		var got Milli
		if err := got.Scan(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Scan(%#v) err = %v", bad, err)
		}
	}
}

// refMulDiv is the math/big reference for a*b/d rounded half-even (test only).
func refMulDiv(a, b, d int64) (int64, bool) {
	r := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(a), big.NewInt(b)), big.NewInt(d))
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	if c := twice.Cmp(r.Denom()); c > 0 || (c == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(int64(r.Sign())))
	}
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}
