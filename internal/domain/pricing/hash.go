package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// canonical returns a copy of cat in canonical order: rules by (stage, seq, id), periods by code, items by code;
// bands keep their order (it is meaningful). encoding/json writes map keys sorted, so the JSON is canonical.
func canonical(cat Catalog) Catalog {
	c := cat
	c.Rules = slices.Clone(cat.Rules)
	slices.SortStableFunc(c.Rules, func(a, b Rule) int {
		if x := strings.Compare(string(a.Stage), string(b.Stage)); x != 0 {
			return x
		}
		if a.Seq != b.Seq {
			if a.Seq < b.Seq {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	c.Items = slices.Clone(cat.Items)
	slices.SortStableFunc(c.Items, func(a, b Item) int {
		if x := strings.Compare(a.Code, b.Code); x != 0 {
			return x
		}
		return strings.Compare(a.ID, b.ID)
	})
	c.PriceList.Periods = slices.Clone(cat.PriceList.Periods)
	slices.SortStableFunc(c.PriceList.Periods, func(a, b RatePeriod) int { return strings.Compare(a.Code, b.Code) })
	return c
}

// catalogHash is the sha256 (hex) of the canonical JSON of cat.
func catalogHash(cat Catalog) string {
	b, err := json.Marshal(canonical(cat))
	if err != nil {
		// Every field of Catalog is a plain value; Marshal cannot fail.
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
