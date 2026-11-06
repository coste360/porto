package layout

import "github.com/coste360/porto/internal/geo"

// PriceClassRule proposes the price class of a berth from its width (spec 7.3, 8.6; m1-plan §11 D7). Bands are
// sorted by MaxCm; the first band whose MaxCm is at least the width wins, else Fallback.
type PriceClassRule struct {
	Basis    string      `json:"basis"` // width
	Bands    []PriceBand `json:"bands"`
	Fallback string      `json:"fallback"`
}

// PriceBand maps widths up to MaxCm (inclusive) to a class.
type PriceBand struct {
	MaxCm geo.Cm `json:"max_cm"`
	Class string `json:"class"`
}

// PriceClassFor returns the class proposed for a width.
func PriceClassFor(width geo.Cm, r PriceClassRule) string {
	for _, b := range r.Bands {
		if width <= b.MaxCm {
			return b.Class
		}
	}
	return r.Fallback
}

// DefaultPriceClassRule maps typical berth widths to the boat-length classes L8…L25.
func DefaultPriceClassRule() PriceClassRule {
	return PriceClassRule{
		Basis: "width",
		Bands: []PriceBand{
			{300, "L8"}, {350, "L10"}, {400, "L12"}, {450, "L14"}, {500, "L16"}, {550, "L18"}, {650, "L20"},
		},
		Fallback: "L25",
	}
}
