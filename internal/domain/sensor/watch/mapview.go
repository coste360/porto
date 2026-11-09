package watch

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// Health values of a map entry (OpenAPI SensorHealth).
const (
	HealthOK         = "ok"
	HealthOffline    = "offline"
	HealthBatteryLow = "battery_low"
	HealthSilent     = "silent"
)

// health is the device health at now: silent (no contact for silent_alert_minutes, never contacted counted from
// created_at) wins over offline (no reading for offline_after_minutes), which wins over battery_low.
func health(d store.SensorWatchDevice, st *store.SensorSettings, now time.Time) string {
	contact := d.CreatedAt
	if d.LastContactAt != nil {
		contact = *d.LastContactAt
	}
	if now.Sub(contact) >= time.Duration(st.SilentAlertMinutes)*time.Minute {
		return HealthSilent
	}
	if d.LastObservedAt == nil || now.Sub(*d.LastObservedAt) >= time.Duration(st.OfflineAfterMinutes)*time.Minute {
		return HealthOffline
	}
	if d.BatteryPct != nil && *d.BatteryPct < st.BatteryLowPct {
		return HealthBatteryLow
	}
	return HealthOK
}

// Map returns the Sensori context of a map (map.view; default the first water map). For a past date the physical
// state is the classification of that night from the history (§10.2), the expectation the contractual status of
// the night, and health is omitted.
func (s *Service) Map(ctx context.Context, p *auth.Principal, marinaID, mapID string, date *calendar.Date) (*Map, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out *Map
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if mapID == "" {
			id, err := store.FirstWaterMap(ctx, tx, scope)
			if err != nil {
				return notFound(err)
			}
			mapID = id
		} else if _, err := store.GetMap(ctx, tx, scope, mapID); err != nil {
			return notFound(err)
		}
		now := s.now()
		sn, err := s.loadSnapshot(ctx, tx, scope, store.SensorWatchDeviceFilter{MapID: mapID, ActiveOnly: true}, now, nil)
		if err != nil {
			return err
		}
		out = &Map{MapID: mapID, GeneratedAt: now, Entries: []MapEntry{}}
		if date != nil && date.Before(sn.today) {
			d := *date
			out.Date = &d
			out.Entries, err = s.pastEntries(ctx, tx, scope, sn, d)
			return err
		}
		ids := make([]string, 0, len(sn.devices))
		for _, d := range sn.devices {
			ids = append(ids, d.ID)
		}
		open, err := store.OpenSensorAlertsOf(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		alerts := map[string][]string{}
		for _, a := range open {
			alerts[a.DeviceID] = append(alerts[a.DeviceID], a.Kind)
		}
		in := sn.input(nil)
		for _, d := range sn.devices {
			sub := sn.subjects[d.ID]
			exp, disc := reconcile.ExpectedPhysical(in, sub)
			e := entryOf(d, sn.targets[d.ID].Informational)
			e.Physical, e.PhysicalSince, e.Expectation, e.Discrepancy = sub.Physical, d.PhysicalSince, exp, disc
			e.Health = health(d, sn.settings, now)
			e.OpenAlerts = alerts[d.ID]
			if e.OpenAlerts == nil {
				e.OpenAlerts = []string{}
			}
			out.Entries = append(out.Entries, e)
		}
		return nil
	})
	return out, err
}

func entryOf(d store.SensorWatchDevice, informational bool) MapEntry {
	return MapEntry{
		DeviceID: d.ID, Code: d.Code, ResourceType: d.ResourceType, ResourceID: d.ResourceID, FromCm: d.FromCm, ToCm: d.ToCm,
		XCm: d.XCm, YCm: d.YCm, Informational: informational, OpenAlerts: []string{},
	}
}

// expectationOfContractual maps the contractual status of a past night to an expectation.
func expectationOfContractual(c string) string {
	switch c {
	case calc.SensorContractContract, calc.SensorContractTransit, calc.SensorContractReleasedResold:
		return reconcile.ExpectOccupied
	default:
		return reconcile.ExpectFree
	}
}

// pastEntries classifies the devices of a snapshot on a past night.
func (s *Service) pastEntries(ctx context.Context, db bun.IDB, scope store.MarinaScope, sn *snapshot, d calendar.Date) ([]MapEntry, error) {
	nights, err := s.nightRows(ctx, db, scope, sn.loc, sn.devices, sn.targets, d, d)
	if err != nil {
		return nil, err
	}
	byDev := map[string]calc.SensorNight{}
	for _, n := range nights {
		byDev[n.DeviceID] = n
	}
	out := make([]MapEntry, 0, len(sn.devices))
	for _, dev := range sn.devices {
		t := sn.targets[dev.ID]
		n := byDev[dev.ID]
		e := entryOf(dev, t.Informational)
		e.Physical = n.Physical
		if e.Physical == "" {
			e.Physical = reconcile.Unknown
		}
		e.Expectation = reconcile.ExpectNone
		if !t.Informational {
			e.Expectation = expectationOfContractual(n.Contractual)
			e.Discrepancy = n.Kind != ""
		}
		out = append(out, e)
	}
	return out, nil
}
