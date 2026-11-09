//go:build integration

package sensor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/sensor/fakesensor"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

const ingestPath = "/api/ingest/v1/readings"

// senv is the backend of the sensor tests: the fixture marina with a water map (berths A1..A4, the 60 m linear area
// L1 on a structure axis, a pedestal C1 with sockets S1, S2 serving A1 and A2), the sensor service with the fake
// source and a captured log.
type senv struct {
	*fixture.Env
	s        *Service
	scope    store.MarinaScope
	harbour  contracttest.Harbour
	pedestal string
	fake     *fakesensor.Source
	logs     *syncBuffer
	wake     chan struct{}
	limiter  *countLimiter
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// countLimiter is a per-key limiter with a fixed budget per key (tests of 429).
type countLimiter struct {
	mu     sync.Mutex
	budget int
	used   map[string]int
}

func (l *countLimiter) Allow(keyID string, _ bool) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.budget <= 0 {
		return true, 0
	}
	l.used[keyID]++
	if l.used[keyID] > l.budget {
		return false, 7 * time.Second
	}
	return true, 0
}

func newSenv(t testing.TB, db *store.DB) *senv {
	t.Helper()
	fe := fixture.New(t, db)
	fe.Clock.Set(time.Date(2027, 6, 12, 8, 0, 0, 0, time.UTC))
	e := &senv{Env: fe, fake: fakesensor.New(), logs: &syncBuffer{}, wake: make(chan struct{}, 1), limiter: &countLimiter{used: map[string]int{}}}
	now := store.UTC(fe.Clock.Now())
	e.harbour = contracttest.InsertHarbour(t, db, fe.MarinaID, now, 4)
	ctx := context.Background()
	// A structure axis for L1 (straight, 80 m along x at y = 3000) and a pedestal serving A1 (S1) and A2 (S2).
	st := &store.HarborStructure{
		ID: store.NewID(), MarinaID: fe.MarinaID, MapID: e.harbour.MapID, Kind: "quay", Code: "Q1",
		Geometry: store.JSON(`{"points":[{"x":1000,"y":3000},{"x":9000,"y":3000}]}`), WidthCm: 300, Attributes: store.JSON(`{}`),
		CreatedAt: now, UpdatedAt: now,
	}
	ped := &store.Pedestal{
		ID: store.NewID(), MarinaID: fe.MarinaID, MapID: e.harbour.MapID, Code: "C1", XCm: 1200, YCm: 1500,
		Sockets: store.JSON(`[{"code":"S1","kind":"power"},{"code":"S2","kind":"power"},{"code":"S3","kind":"power"}]`), Meters: store.JSON(`[]`),
		CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []any{st, ped} {
		if _, err := db.NewInsert().Model(r).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
	e.pedestal = ped.ID
	if _, err := db.NewUpdate().Table("dynamic_areas").Set("structure_id = ?", st.ID).Where("id = ?", e.harbour.Area).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for i, sock := range []string{"S1", "S2"} {
		if _, err := db.NewUpdate().Table("berths").Set("pedestal_id = ?", ped.ID).Set("pedestal_socket = ?", sock).
			Where("id = ?", e.harbour.Berths[i]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	scope, err := store.ResolveMarinaScope(ctx, db.DB, fe.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	e.scope = scope
	e.s = NewService(Deps{
		DB: db, Clock: fe.Clock, Audit: fe.Audit, Outbox: fe.Outbox, Keys: fe.Keys,
		Log:     slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Sources: []ext.SensorSource{e.fake}, RawRetention: 30 * 24 * time.Hour, Limiter: e.limiter, Wake: e.wake,
	})
	return e
}

func i64(v int64) *int64 { return &v }

// device creates a berth device on berth i with code and external id.
func (e *senv) device(t testing.TB, code, ext string, berth int) *DeviceWithKey {
	t.Helper()
	d, err := e.s.CreateDevice(fixture.Ctx(), e.Owner, e.MarinaID, DeviceInput{
		ExternalID: ext, Code: code, Kind: store.SensorKindUltrasonic, ResourceType: store.SensorResourceBerth, ResourceID: e.harbour.Berths[berth],
	})
	if err != nil {
		t.Fatalf("create device %s: %v", code, err)
	}
	return d
}

type reading struct {
	DeviceID   string `json:"device_id"`
	ObservedAt string `json:"observed_at"`
	State      string `json:"state"`
	DistanceCm *int64 `json:"distance_cm,omitempty"`
	BatteryPct *int   `json:"battery_pct,omitempty"`
	RSSI       *int   `json:"rssi,omitempty"`
	Firmware   string `json:"firmware,omitempty"`
}

func body(t testing.TB, rs ...reading) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"readings": rs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// signed is an HMAC request for key over b at the clock's now.
func (e *senv) signed(key string, b []byte) IngestRequest {
	id, _ := ingestauth.KeyIDOf(key)
	t := e.Clock.Now().Unix()
	return IngestRequest{
		Method: "POST", Path: ingestPath, Body: b, PortoKey: id,
		PortoSignature: "t=" + strconv.FormatInt(t, 10) + ",v1=" + ingestauth.Sign(key, t, "POST", ingestPath, b), Secure: true,
	}
}

func (e *senv) post(t testing.TB, key string, rs ...reading) *IngestResult {
	t.Helper()
	res, err := e.s.Ingest(context.Background(), e.signed(key, body(t, rs...)))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return res
}

func (e *senv) state(t testing.TB, deviceID string) *store.SensorDeviceState {
	t.Helper()
	st, err := store.GetSensorDeviceState(context.Background(), e.DB.DB, e.scope, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (e *senv) changes(t testing.TB, deviceID string) []store.SensorStateChange {
	t.Helper()
	cs, err := store.ListSensorStateChangesOf(context.Background(), e.DB.DB, e.scope, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// checkInvariants asserts Q40/Q41 on every marina of the database.
func checkInvariants(t testing.TB, db *store.DB) {
	t.Helper()
	var marinas []string
	if err := db.NewSelect().Table("marinas").Column("id").Scan(context.Background(), &marinas); err != nil {
		t.Fatal(err)
	}
	for _, m := range marinas {
		v, err := store.CheckSensorInvariants(context.Background(), db.DB, m)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) > 0 {
			t.Fatalf("sensor invariants: %+v", v)
		}
	}
}

func isField(err error, field string) bool {
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		return false
	}
	for _, f := range v.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return needle != "" && strings.Contains(haystack, needle)
}

type harbourIDs = contracttest.Harbour

func insertHarbour(t testing.TB, db *store.DB, marinaID string, now time.Time) harbourIDs {
	return contracttest.InsertHarbour(t, db, marinaID, now, 2)
}
