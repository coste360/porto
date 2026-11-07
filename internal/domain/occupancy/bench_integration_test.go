//go:build integration

package occupancy

import (
	"fmt"
	"testing"

	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// bigHarbour adds n berths and k linear areas of 100 m to h, and occupies about half of the berths for the
// next 30 nights.
func bigHarbour(b *testing.B, h *harbour, n, k int) {
	b.Helper()
	ctx := ctxT()
	now := store.UTC(h.Clock.Now())
	var berths []store.Berth
	for i := range n {
		berths = append(berths, store.Berth{
			ID: store.NewID(), MarinaID: h.MarinaID, MapID: h.mapID, Code: fmt.Sprintf("Z-%04d", i), Side: "none", AnchorXCm: int64(i * 500),
			AnchorYCm: 50000, WidthCm: 450, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 400, DepthCm: 300, MooringType: "mooring_line",
			Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		})
	}
	for start := 0; start < len(berths); start += 200 {
		part := berths[start:min(len(berths), start+200)]
		if _, err := h.DB.NewInsert().Model(&part).Exec(ctx); err != nil {
			b.Fatal(err)
		}
	}
	for i := range k {
		from, to := int64(0), int64(10000)
		a := &store.DynamicArea{
			ID: store.NewID(), MarinaID: h.MarinaID, MapID: h.mapID, Kind: "linear", Code: fmt.Sprintf("Q-%d", i), Side: "left", FromCm: &from,
			ToCm: &to, MooringMode: "stern_to", CellCm: 50, MarginCm: 50, Supports: store.Strings{}, DepthProfile: store.MustJSON([]any{}),
			BlockedCells: store.MustJSON([]int{}), Exits: store.MustJSON([]int{}), CellCount: 200, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := h.DB.NewInsert().Model(a).Exec(ctx); err != nil {
			b.Fatal(err)
		}
		for j := range 15 {
			h.mustAllocate(b, transit(h.vessel(b, 1000, 350), areaSpec(a.ID, geo.Cm(30+j*600), h.today, h.today.AddDays(29))))
		}
	}
	v := h.vessel(b, 1000, 350)
	for i := 0; i < n; i += 2 {
		h.mustAllocate(b, NewAllocation{VesselID: &v, Kind: KindContract, Spec: berthSpec(berths[i].ID, h.today.AddDays(i%7), h.today.AddDays(29))})
	}
}

// 800 berths and 1 km of linear areas, 30 nights (m3-plan §3.6 budget: ≤ 150 ms on SQLite).
func BenchmarkSearch_Demo(b *testing.B) {
	db := benchDB(b)
	h := newHarbour(b, db)
	bigHarbour(b, h, 800, 10)
	v := h.vessel(b, 1100, 380)
	in := SearchInput{Purpose: engine.PurposeTransit, VesselID: v, Arrival: h.today, Departure: h.today.AddDays(30), Channel: "desk"}
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.s.Search(ctxT(), h.Owner, h.MarinaID, in); err != nil {
			b.Fatal(err)
		}
	}
}

// 3 000 elements, about 2 000 occupied (m3-plan §8.3 budget: ≤ 250 ms on SQLite).
func BenchmarkDayView_3000(b *testing.B) {
	db := benchDB(b)
	h := newHarbour(b, db)
	bigHarbour(b, h, 2990, 0)
	date := h.today.AddDays(10)
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.s.GetDay(ctxT(), h.Owner, h.MarinaID, h.mapID, &date); err != nil {
			b.Fatal(err)
		}
	}
}

// An annual contract in a linear area: 365 nights × 10 cells.
func BenchmarkAllocate_Annual(b *testing.B) {
	db := benchDB(b)
	h := newHarbour(b, db)
	v := h.vessel(b, 1000, 400)
	b.ResetTimer()
	i := 0
	for b.Loop() {
		start := h.today.AddDays(400 * i)
		i++
		if _, err := h.allocate(ctxT(), NewAllocation{VesselID: &v, Kind: KindContract, Spec: areaSpec(h.area, 100, start, start.AddDays(364))}); err != nil {
			b.Fatal(err)
		}
	}
}

// benchDB opens a fresh migrated in-memory SQLite database (testdb takes a *testing.T).
func benchDB(b *testing.B) *store.DB {
	b.Helper()
	db, err := store.Open(ctxT(), store.Options{SQLiteMemory: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctxT()); err != nil {
		b.Fatal(err)
	}
	return db
}
