// Package layout is the pure geometric model of the harbour map (spec 07, 08): elements, ValidateMap, Footprint
// and area cells, generators, georeferencing and the illustration kit asset ids. No database, no clock, no
// randomness, no I/O (INV-21); lengths are integer centimetres (geo.Cm).
package layout

//go:generate go run ./internal/genkit ../../../assets/harbor-kits/porto-basic/manifest.json kitassets_gen.go

// ValidAsset reports whether id is an asset of the porto-basic illustration kit (assets/harbor-kits/porto-basic,
// spec 7.7). The list is generated from the kit manifest.
func ValidAsset(id string) bool {
	_, ok := kitAssets[id]
	return ok
}

// KitAssets returns the asset ids of the illustration kit in manifest order.
func KitAssets() []string { return append([]string(nil), kitAssetOrder...) }
