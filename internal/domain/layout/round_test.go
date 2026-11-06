package layout

import (
	"math/big"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/geo"
)

// TestRoundRat_MatchesDivRoundHalfEven keeps the big.Rat rounding of the georeference identical to the single
// integer helper geo.DivRoundHalfEven, so that the two cannot drift apart.
func TestRoundRat_MatchesDivRoundHalfEven(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		num := rapid.Int64Range(-1<<62, 1<<62).Draw(t, "num")
		den := rapid.OneOf(rapid.Int64Range(1, 1000), rapid.Int64Range(1, 1<<62)).Draw(t, "den")
		if got, want := roundRat(big.NewRat(num, den)), geo.DivRoundHalfEven(num, den); got != want {
			t.Fatalf("roundRat(%d/%d) = %d, geo.DivRoundHalfEven = %d", num, den, got, want)
		}
	})
}
