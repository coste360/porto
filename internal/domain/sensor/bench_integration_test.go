//go:build integration

package sensor

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// insertBerths adds n active berths to the map of the harbour and returns their ids.
func insertBerths(tb testing.TB, db *store.DB, marinaID, mapID string, n int, now time.Time) []string {
	tb.Helper()
	rows := make([]store.Berth, 0, n)
	ids := make([]string, 0, n)
	for i := range n {
		b := store.Berth{
			ID: store.NewID(), MarinaID: marinaID, MapID: mapID, Code: "B" + strconv.Itoa(i+1), Side: "none",
			AnchorXCm: int64(1000 + 100*(i%900)), AnchorYCm: int64(5000 + 1500*(i/900)), WidthCm: 400, LengthCm: 1200,
			MaxLOACm: 1200, MaxBeamCm: 400, MooringType: "mooring_line", PriceClass: "L12", Attributes: store.Strings{},
			Active: true, CreatedAt: now, UpdatedAt: now,
		}
		rows = append(rows, b)
		ids = append(ids, b.ID)
	}
	for start := 0; start < len(rows); start += 200 {
		chunk := rows[start:min(start+200, len(rows))]
		if _, err := db.NewInsert().Model(&chunk).Exec(context.Background()); err != nil {
			tb.Fatal(err)
		}
	}
	return ids
}

// BenchmarkIngest_Batch500 measures one ingest transaction of a full gateway batch (500 devices, one reading each)
// on a SQLite file database (m12-plan §14.2, §14.3 write cost).
func BenchmarkIngest_Batch500(b *testing.B) {
	db := testdb.SQLite(b)
	e := newSenv(b, db)
	ctx := fixture.Ctx()
	g, err := e.s.CreateGateway(ctx, e.Owner, e.MarinaID, GatewayInput{Code: "GW", Name: "Gw", Kind: store.SensorGatewayLoRaWAN})
	if err != nil {
		b.Fatal(err)
	}
	berths := insertBerths(b, db, e.MarinaID, e.harbour.MapID, MaxBatch, store.UTC(e.Clock.Now()))
	for i, berth := range berths {
		if _, err := e.s.CreateDevice(ctx, e.Owner, e.MarinaID, DeviceInput{
			ExternalID: "BD" + strconv.Itoa(i), Code: "BD" + strconv.Itoa(i), Kind: store.SensorKindUltrasonic,
			ResourceType: store.SensorResourceBerth, ResourceID: berth, GatewayID: g.Gateway.ID,
		}); err != nil {
			b.Fatal(err)
		}
	}
	states := []string{store.SensorOccupied, store.SensorFree}
	b.ResetTimer()
	n := 0
	for b.Loop() {
		b.StopTimer()
		e.Clock.Advance(time.Minute)
		now := e.Clock.Now()
		rs := make([]reading, 0, MaxBatch)
		for i := range MaxBatch {
			rs = append(rs, reading{DeviceID: "BD" + strconv.Itoa(i), ObservedAt: ts(now), State: states[(n/3+i)%2]})
		}
		req := e.signed(g.Key, body(b, rs...))
		b.StartTimer()
		res, err := e.s.Ingest(context.Background(), req)
		if err != nil || res.Accepted != MaxBatch {
			b.Fatalf("ingest %v %+v", err, res)
		}
		n++
	}
}
