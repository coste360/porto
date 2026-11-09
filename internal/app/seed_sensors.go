package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/domain/sensor/fakesensor"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Demo sensor layout (m12-plan §17.2).
const (
	seedSensorBerths  = 40
	seedSensorHistory = 6 * time.Hour
	seedSensorEvery   = 10 * time.Minute
	seedBackdatedAge  = 5 * 24 * time.Hour
	seedGatewayFrom   = 30 // berth devices 30..37 post through the gateway
	seedSourceFrom    = 38 // berth devices 38..39 are fed by the fake source (gateway when it is not registered)
	seedFlips         = 3  // free berths with an occupied hour in the last 6 h
)

var seedCodeClean = regexp.MustCompile(`[^A-Z0-9_-]+`)

// seedSensorDevice is a device created by the seed with how its readings reach Porto.
type seedSensorDevice struct {
	view   sensor.DeviceView
	key    string
	path   string // own, gateway, source
	state  string // the back-dated physical state
	flip   bool   // occupied between now-3h and now-2h
	silent bool   // no reading in the last 4 hours
	low    bool   // battery 12 %
}

// seedSensors adds the M12 demo of INGEST (m12-plan §17.2): a gateway, the fake source (dev and demo only), 40
// berth devices on the pier with most berths, 2 segment devices on a linear area and 1 pedestal device; a physical
// history back-dated 5 days (so the alert seed finds a 4-day absence), then 6 h of readings every 10 minutes
// through sensor.Ingest (signed device batches, one gateway batch, one source poll) agreeing with it, except three
// free berths occupied for one hour, one device silent for 4 hours and one at 12 % battery. Everything goes
// through the sensor service as the demo owner; a no-op when the marina already has devices.
func (a *App) seedSensors(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Sensors == nil || res.MarinaID == "" {
		return nil
	}
	existing, err := a.Sensors.ListDevices(ctx, p, res.MarinaID, sensor.DeviceFilter{}, "", 1)
	if err != nil || len(existing.Items) > 0 {
		return err
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	berths, areas, peds, err := store.SensorSeedCandidates(ctx, a.DB.DB, scope)
	if err != nil || len(berths) == 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	now := a.Clock.Now()
	occupied, err := store.PublicOccupiedBerths(ctx, a.DB.DB, scope, calendar.In(now, loc), now)
	if err != nil {
		return err
	}

	gw, err := a.Sensors.CreateGateway(ctx, p, res.MarinaID, sensor.GatewayInput{Code: "GW-NORD", Name: "Gateway LoRaWAN nord", Kind: store.SensorGatewayLoRaWAN})
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	var fake *fakesensor.Source
	sourceID := ""
	if f, ok := a.Sensors.Driver(fakesensor.Code).(*fakesensor.Source); ok {
		src, err := a.Sensors.CreateSource(ctx, p, res.MarinaID, sensor.SourceInput{
			Code: fakesensor.Code, Name: "Sorgente demo", Config: map[string]string{"label": "demo"}, PollSeconds: 60,
		})
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}
		fake, sourceID = f, src.ID
	}

	var devs []*seedSensorDevice
	add := func(in sensor.DeviceInput, path, state string) error {
		d, err := a.Sensors.CreateDevice(ctx, p, res.MarinaID, in)
		if err != nil {
			return fmt.Errorf("device %s: %w", in.Code, err)
		}
		devs = append(devs, &seedSensorDevice{view: d.Device, key: d.Key, path: path, state: state})
		return nil
	}
	pier := seedPier(berths, seedSensorBerths)
	flips := 0
	for i, b := range pier {
		in := sensor.DeviceInput{
			ExternalID: fmt.Sprintf("A8175800DE%06X", i+1), Code: seedCode("S-" + b.Code), Kind: store.SensorKindUltrasonic,
			Model: "US-200", Firmware: "2.4.1", ResourceType: store.SensorResourceBerth, ResourceID: b.ID,
		}
		path := "own"
		switch {
		case i >= seedSourceFrom && sourceID != "":
			in.SourceID, path, in.Kind = sourceID, "source", store.SensorKindCamera
		case i >= seedGatewayFrom:
			in.GatewayID, path, in.Kind = gw.Gateway.ID, "gateway", store.SensorKindRadar
		}
		state := store.SensorFree
		if occupied[b.ID] {
			state = store.SensorOccupied
		}
		if err := add(in, path, state); err != nil {
			return err
		}
		d := devs[len(devs)-1]
		if state == store.SensorFree && flips < seedFlips && i%5 == 2 {
			d.flip, flips = true, flips+1
		}
		d.silent, d.low = i == len(pier)-1, i == len(pier)-2
	}
	if len(areas) > 0 && areas[0].ToCm-areas[0].FromCm >= 400 {
		ar := areas[0]
		half := (ar.ToCm - ar.FromCm) / 2
		for i, seg := range [][2]int64{{0, half}, {half, ar.ToCm - ar.FromCm}} {
			from, to := seg[0], seg[1]
			if err := add(sensor.DeviceInput{
				ExternalID: fmt.Sprintf("A8175800AE%06X", i+1), Code: fmt.Sprintf("S-LIN-%d", i+1), Kind: store.SensorKindRadar,
				Model: "RD-10", ResourceType: store.SensorResourceDynamicArea, ResourceID: ar.ID, FromCm: &from, ToCm: &to,
			}, "own", store.SensorFree); err != nil {
				return err
			}
		}
	}
	if len(peds) > 0 {
		if err := add(sensor.DeviceInput{
			ExternalID: "A8175800PE000001", Code: "S-COL-1", Kind: store.SensorKindMagnetometer, Model: "MG-1",
			ResourceType: store.SensorResourcePedestal, ResourceID: peds[0].ID, PedestalSocket: peds[0].Socket,
		}, "own", store.SensorFree); err != nil {
			return err
		}
	}

	states := make(map[string]string, len(devs))
	for _, d := range devs {
		states[d.view.ID] = d.state
	}
	if _, err := a.Sensors.SeedBackdated(ctx, p, res.MarinaID, states, now.Add(-seedBackdatedAge)); err != nil {
		return fmt.Errorf("back-dated history: %w", err)
	}

	// 6 h of readings, oldest first, in one batch per device (own key), per gateway and per source poll.
	start := now.Add(-seedSensorHistory)
	var gwBatch []seedReading
	var srcBatch []ext.SensorReading
	for i, d := range devs {
		var batch []seedReading
		for t := start; !t.After(now.Add(-time.Minute)); t = t.Add(seedSensorEvery) {
			if d.silent && t.After(now.Add(-4*time.Hour)) {
				break
			}
			st := d.state
			if d.flip && !t.Before(now.Add(-3*time.Hour)) && t.Before(now.Add(-2*time.Hour)) {
				st = store.SensorOccupied
			}
			bat := 80 + (i*7)%16
			if d.low {
				bat = 12
			}
			r := seedReading{DeviceID: d.view.ExternalID, ObservedAt: t.UTC().Format(time.RFC3339), State: st, BatteryPct: bat, RSSI: -90 - i%20}
			switch d.path {
			case "gateway":
				gwBatch = append(gwBatch, r)
			case "source":
				b := bat
				srcBatch = append(srcBatch, ext.SensorReading{DeviceExternalID: d.view.ExternalID, ObservedAt: t.UTC(), State: st, BatteryPct: &b})
			default:
				batch = append(batch, r)
			}
		}
		if len(batch) > 0 {
			if err := a.seedIngest(ctx, d.key, batch); err != nil {
				return fmt.Errorf("readings of %s: %w", d.view.Code, err)
			}
		}
	}
	if len(gwBatch) > 0 {
		if err := a.seedIngest(ctx, gw.Key, gwBatch); err != nil {
			return fmt.Errorf("gateway readings: %w", err)
		}
	}
	if fake != nil && len(srcBatch) > 0 {
		fake.Push(res.MarinaID, srcBatch...)
		if _, err := a.Sensors.PollSourceNow(ctx, p, res.MarinaID, sourceID); err != nil {
			return fmt.Errorf("source poll: %w", err)
		}
	}
	return nil
}

// seedPier returns up to n berths of the structure with the most berths, completed with other berths by code.
func seedPier(berths []store.SensorSeedBerth, n int) []store.SensorSeedBerth {
	count := map[string]int{}
	best := ""
	for _, b := range berths {
		count[b.StructureID]++
		if b.StructureID != "" && (best == "" || count[b.StructureID] > count[best]) {
			best = b.StructureID
		}
	}
	var out []store.SensorSeedBerth
	for _, b := range berths {
		if b.StructureID == best && len(out) < n {
			out = append(out, b)
		}
	}
	for _, b := range berths {
		if b.StructureID != best && len(out) < n {
			out = append(out, b)
		}
	}
	return out
}

func seedCode(s string) string {
	c := seedCodeClean.ReplaceAllString(strings.ToUpper(s), "-")
	if len(c) > 32 {
		c = c[:32]
	}
	return c
}

type seedReading struct {
	DeviceID   string `json:"device_id"`
	ObservedAt string `json:"observed_at"`
	State      string `json:"state"`
	BatteryPct int    `json:"battery_pct"`
	RSSI       int    `json:"rssi"`
}

// seedIngest signs a batch with key at the app clock and stores it through sensor.Ingest (the same path as the
// devices).
func (a *App) seedIngest(ctx context.Context, key string, rs []seedReading) error {
	body, err := json.Marshal(map[string]any{"readings": rs})
	if err != nil {
		return err
	}
	keyID, _ := ingestauth.KeyIDOf(key)
	t := a.Clock.Now().Unix()
	res, err := a.Sensors.Ingest(ctx, sensor.IngestRequest{
		Method: "POST", Path: "/api/ingest/v1/readings", Body: body, PortoKey: keyID, Secure: true,
		PortoSignature: fmt.Sprintf("t=%d,v1=%s", t, ingestauth.Sign(key, t, "POST", "/api/ingest/v1/readings", body)),
	})
	if err != nil {
		return err
	}
	if len(res.Rejected) > 0 {
		return fmt.Errorf("%d readings rejected (%s)", len(res.Rejected), res.Rejected[0].Code)
	}
	return nil
}
