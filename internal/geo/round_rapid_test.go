package geo

import (
	"math"
	"math/big"
	"testing"

	"pgregory.net/rapid"
)

// ratHalfEven is the reference: num/den rounded half-even on exact rationals.
func ratHalfEven(num, den int64) int64 {
	n, d := big.NewInt(num), big.NewInt(den)
	q, m := new(big.Int).DivMod(n, d, new(big.Int)) // floor division, 0 <= m < d
	switch new(big.Int).Lsh(m, 1).Cmp(d) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	return q.Int64()
}

func TestDivRoundHalfEven_MatchesBigRat(t *testing.T) {
	for _, c := range [][3]int64{{5, 2, 2}, {7, 2, 4}, {-5, 2, -2}, {-7, 2, -4}, {1, 3, 0}, {2, 3, 1}, {-2, 3, -1}, {0, 7, 0}} {
		if got := DivRoundHalfEven(c[0], c[1]); got != c[2] {
			t.Errorf("DivRoundHalfEven(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
	rapid.Check(t, func(t *rapid.T) {
		num := rapid.Int64Range(math.MinInt64+1, math.MaxInt64).Draw(t, "num")
		den := rapid.OneOf(rapid.Int64Range(1, 1000), rapid.Int64Range(1, math.MaxInt64)).Draw(t, "den")
		if got, want := DivRoundHalfEven(num, den), ratHalfEven(num, den); got != want {
			t.Fatalf("DivRoundHalfEven(%d, %d) = %d, want %d", num, den, got, want)
		}
	})
}
