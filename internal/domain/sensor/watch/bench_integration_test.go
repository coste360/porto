//go:build integration

package watch

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// BenchmarkSensorMap_1000 measures getSensorMap for a map with 1,000 berth devices on a SQLite file database
// (m12-plan §9.2: ≤ 3 queries, ≤ 100 ms).
func BenchmarkSensorMap_1000(b *testing.B) {
	db := testdb.SQLite(b)
	e := newWenv(b, db)
	ctx := context.Background()
	now := e.now()
	berths := make([]store.Berth, 0, 1000)
	devices := make([]store.SensorDevice, 0, 1000)
	states := make([]store.SensorDeviceState, 0, 1000)
	phys := []string{store.SensorOccupied, store.SensorFree, store.SensorUnknown}
	for i := range 1000 {
		br := store.Berth{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: e.MapID, Code: "B" + strconv.Itoa(i+1), Side: "none",
			AnchorXCm: int64(1000 + 100*(i%900)), AnchorYCm: int64(5000 + 1500*(i/900)), WidthCm: 400, LengthCm: 1200,
			MaxLOACm: 1200, MaxBeamCm: 400, MooringType: "mooring_line", PriceClass: "L12", Attributes: store.Strings{},
			Active: true, CreatedAt: now, UpdatedAt: now,
		}
		berths = append(berths, br)
		d := store.SensorDevice{
			ID: store.NewID(), MarinaID: e.MarinaID, ExternalID: "BM" + strconv.Itoa(i), Code: "BM" + strconv.Itoa(i),
			Kind: store.SensorKindUltrasonic, ResourceType: store.SensorResourceBerth, ResourceID: br.ID, MapID: e.MapID,
			XCm: br.AnchorXCm, YCm: br.AnchorYCm, Active: true, Version: 1, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
		}
		devices = append(devices, d)
		since := now.Add(-time.Duration(i%120) * time.Minute)
		states = append(states, store.SensorDeviceState{
			DeviceID: d.ID, MarinaID: e.MarinaID, Physical: phys[i%3], PhysicalSince: &since, LastObservedAt: &now,
			LastContactAt: &now, UpdatedAt: now,
		})
	}
	for start := 0; start < 1000; start += 200 {
		bs, ds, ss := berths[start:start+200], devices[start:start+200], states[start:start+200]
		for _, m := range []any{&bs, &ds, &ss} {
			if _, err := db.NewInsert().Model(m).Exec(ctx); err != nil {
				b.Fatal(err)
			}
		}
	}
	pctx := fixture.Ctx()
	b.ResetTimer()
	for b.Loop() {
		m, err := e.w.Map(pctx, e.Owner, e.MarinaID, e.MapID, nil)
		if err != nil || len(m.Entries) < 1000 {
			b.Fatalf("map %v %d", err, len(m.Entries))
		}
	}
}
