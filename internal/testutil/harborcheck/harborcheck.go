// Package harborcheck is the defence-in-depth check of INV-04 (m1-plan §3.3): it loads every berth of every map
// straight from the database and checks all pairs by brute force, without a spatial index and without
// layout.ValidateMap. Acceptance, integration and concurrency tests call it at the end.
package harborcheck

import (
	"context"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// TB is the part of testing.TB the check needs (also implemented by *rapid.T).
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// AssertNoOverlappingBerths fails the test when two non-buoy berths of the same map overlap: their rectangles,
// each shrunk by 1 cm per side (the spec 8.4 tolerance), have overlapping interiors.
func AssertNoOverlappingBerths(t TB, db *store.DB) {
	t.Helper()
	if n := OverlappingPairs(t, db); n > 0 {
		t.Fatalf("INV-04 violated: %d overlapping berth pairs on saved maps", n)
	}
}

// AssertNoOverlappingBerthsInMap is AssertNoOverlappingBerths restricted to one map. Property tests that create
// a map per iteration call it after every step, so the cost of a step does not grow with the iterations run
// before it; they still call AssertNoOverlappingBerths once at the end.
func AssertNoOverlappingBerthsInMap(t TB, db *store.DB, mapID string) {
	t.Helper()
	if n := overlappingPairs(t, db, mapID); n > 0 {
		t.Fatalf("INV-04 violated: %d overlapping berth pairs on map %s", n, mapID)
	}
}

// OverlappingPairs returns the number of overlapping berth pairs on saved maps.
func OverlappingPairs(t TB, db *store.DB) int {
	t.Helper()
	return overlappingPairs(t, db, "")
}

// overlappingPairs checks the berths of one map, or of every map when mapID is empty.
func overlappingPairs(t TB, db *store.DB, mapID string) int {
	t.Helper()
	var rows []store.Berth
	q := db.NewSelect().Model(&rows).Where("mooring_type <> ?", "buoy")
	if mapID != "" {
		q = q.Where("map_id = ?", mapID)
	}
	if err := q.OrderExpr("map_id, id").Scan(context.Background()); err != nil {
		t.Fatalf("harborcheck: load berths: %v", err)
	}
	byMap := map[string][]geo.Rect{}
	n := 0
	for _, b := range rows {
		if b.WidthCm < int64(layout.MinRectSide) || b.LengthCm < int64(layout.MinRectSide) {
			t.Errorf("harborcheck: map %s: berth %s is %dx%d cm, too thin to check for overlap", b.MapID, b.ID, b.WidthCm, b.LengthCm)
			n++
		}
		r := geo.Rect{
			Anchor: geo.Point{X: geo.Cm(b.AnchorXCm), Y: geo.Cm(b.AnchorYCm)}, Width: geo.Cm(b.WidthCm),
			Length: geo.Cm(b.LengthCm), Rotation: geo.Angle(b.RotationDd),
		}
		byMap[b.MapID] = append(byMap[b.MapID], layout.ShrinkRect(r, 1))
	}
	for mapID, rects := range byMap {
		for i := range rects {
			for j := i + 1; j < len(rects); j++ {
				a, b := rects[i], rects[j]
				if a.Width <= 0 || a.Length <= 0 || b.Width <= 0 || b.Length <= 0 {
					continue
				}
				if geo.RectsOverlap(a, b) {
					t.Errorf("harborcheck: map %s: berths %d and %d overlap", mapID, i, j)
					n++
				}
			}
		}
	}
	return n
}
