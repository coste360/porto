package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// SensorNightFacts are the rows of the physical-night comparison of a set of devices over a range of nights
// (m12-plan §10; RECONCILE): the state changes inside the window with the state before it, the allocations and
// active releases of the compared resources, the presence events of their vessels and the alerts of the devices.
type SensorNightFacts struct {
	Changes      []SensorStateChange
	StatesBefore map[string]string
	Stays        []SensorStay
	Releases     []SensorRelease
	Presences    []SensorPresenceEvent
	Alerts       []SensorAlert
}

// LoadSensorNightFacts reads the facts of the nights [from, to] of the devices compared with resourceIDs; the
// physical window is [winStart, winEnd) (22:00 of the first night to 06:00 after the last, local).
func LoadSensorNightFacts(ctx context.Context, db bun.IDB, s MarinaScope, deviceIDs, resourceIDs []string, from, to calendar.Date,
	winStart, winEnd time.Time,
) (*SensorNightFacts, error) {
	out := &SensorNightFacts{}
	var err error
	if out.Changes, err = SensorStateChangesOf(ctx, db, s, deviceIDs, winStart, winEnd); err != nil {
		return nil, err
	}
	if out.StatesBefore, err = SensorStatesBefore(ctx, db, s, deviceIDs, winStart); err != nil {
		return nil, err
	}
	if out.Stays, err = SensorStays(ctx, db, s, resourceIDs, from, to); err != nil {
		return nil, err
	}
	if out.Releases, err = SensorReleases(ctx, db, s, resourceIDs, from, to); err != nil {
		return nil, err
	}
	var vessels []string
	seen := map[string]bool{}
	for _, st := range out.Stays {
		if st.VesselID != "" && !seen[st.VesselID] {
			seen[st.VesselID] = true
			vessels = append(vessels, st.VesselID)
		}
	}
	if out.Presences, err = SensorPresenceEvents(ctx, db, s, vessels, winEnd); err != nil {
		return nil, err
	}
	if ids := validIDs(deviceIDs); len(ids) > 0 {
		if err := db.NewSelect().Model(&out.Alerts).Where("marina_id = ?", s.ID()).Where("device_id IN (?)", bun.List(ids)).
			Where("opened_at < ?", UTC(winEnd)).Where("(cleared_at IS NULL OR cleared_at >= ?)", UTC(winStart.Add(-22*time.Hour))).
			OrderExpr("opened_at, id").Scan(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SensorReportMaps returns the (Italian, else English) names of the maps of the marina keyed by id.
func SensorReportMaps(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	maps, err := ListMaps(ctx, db, s)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(maps))
	for _, m := range maps {
		var names map[string]string
		_ = json.Unmarshal(m.Name, &names)
		out[m.ID] = names["it"]
		if out[m.ID] == "" {
			out[m.ID] = names["en"]
		}
	}
	return out, nil
}
