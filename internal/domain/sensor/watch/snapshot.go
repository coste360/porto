package watch

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// target is the resource whose contractual facts a device is compared with: the berth, the dynamic area (segment)
// or the berth of a pedestal socket; informational when a pedestal device maps to no single berth (m12-plan §8.1).
type target struct {
	ResourceType  string // berth, dynamic_area
	ResourceID    string
	Informational bool
}

// snapshot is everything the rules, the map and the day-view facts read for a set of devices at now.
type snapshot struct {
	now      time.Time
	loc      *time.Location
	today    calendar.Date
	settings *store.SensorSettings
	devices  []store.SensorWatchDevice
	targets  map[string]target
	subjects map[string]reconcile.Subject
}

func settingsOf(st *store.SensorSettings) reconcile.Settings {
	return reconcile.Settings{
		UnexpectedAfterMinutes: st.UnexpectedAfterMinutes, DepartureCutoffMinutes: st.DepartureCutoffMinutes,
		SilentAlertMinutes: st.SilentAlertMinutes, BatteryLowPct: st.BatteryLowPct, PresenceEffectEnabled: st.PresenceEffectEnabled,
		PresenceOutAfterHours: st.PresenceOutAfterHours, ReleaseProposalAfterDays: st.ReleaseProposalAfterDays,
		ReleaseProposalNights: st.ReleaseProposalNights,
	}
}

func (sn *snapshot) input(alerts []reconcile.AlertKey) reconcile.Input {
	subs := make([]reconcile.Subject, 0, len(sn.devices))
	for _, d := range sn.devices {
		subs = append(subs, sn.subjects[d.ID])
	}
	return reconcile.Input{Now: sn.now, Loc: sn.loc, Settings: settingsOf(sn.settings), Subjects: subs, Alerts: alerts}
}

func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// resolveTargets maps every device to its facts resource (pedestal sockets through the berths they serve).
func resolveTargets(ctx context.Context, db bun.IDB, scope store.MarinaScope, devs []store.SensorWatchDevice) (map[string]target, error) {
	out := make(map[string]target, len(devs))
	var pedestals []string
	for _, d := range devs {
		switch d.ResourceType {
		case store.SensorResourcePedestal:
			pedestals = append(pedestals, d.ResourceID)
		default:
			out[d.ID] = target{ResourceType: d.ResourceType, ResourceID: d.ResourceID}
		}
	}
	if len(pedestals) == 0 {
		return out, nil
	}
	berths, err := store.SensorPedestalBerths(ctx, db, scope, pedestals)
	if err != nil {
		return nil, err
	}
	byPedestal := map[string][]store.SensorPedestalBerth{}
	for _, b := range berths {
		byPedestal[b.PedestalID] = append(byPedestal[b.PedestalID], b)
	}
	for _, d := range devs {
		if d.ResourceType != store.SensorResourcePedestal {
			continue
		}
		var match []string
		for _, b := range byPedestal[d.ResourceID] {
			if d.PedestalSocket == "" || b.Socket == d.PedestalSocket {
				match = append(match, b.BerthID)
			}
		}
		if len(match) == 1 {
			out[d.ID] = target{ResourceType: store.SensorResourceBerth, ResourceID: match[0]}
		} else {
			out[d.ID] = target{Informational: true}
		}
	}
	return out, nil
}

func segmentHit(st store.SensorStay, from, to *int64) bool {
	if from == nil || to == nil || st.XCm == nil {
		return true
	}
	return *st.XCm < *to && *st.XCm+st.LoaCm > *from
}

func stayOf(st store.SensorStay, policy string) reconcile.Stay {
	x := int64(0)
	if st.XCm != nil {
		x = *st.XCm
	}
	return reconcile.Stay{
		AllocationID: st.AllocationID, ContractID: st.ContractID, ContractKind: st.ContractKind, ContractTerm: st.ContractTerm,
		Status: st.ContractStatus, VesselID: st.VesselID, FirstNight: st.FirstNight, LastNight: st.LastNight,
		AllocationKind: st.Kind, ReleasePolicy: policy, XCm: x, LoaCm: st.LoaCm,
	}
}

func covers(first, last, d calendar.Date) bool { return !d.Before(first) && !d.After(last) }

// loadSnapshot reads the devices of f with their states and the facts of D - 1, D and D + 1 (≤ 8 queries).
func (s *Service) loadSnapshot(ctx context.Context, db bun.IDB, scope store.MarinaScope, f store.SensorWatchDeviceFilter,
	now time.Time, settings *store.SensorSettings,
) (*snapshot, error) {
	loc := s.location(ctx, db, scope)
	sn := &snapshot{now: now, loc: loc, today: calendar.In(now, loc), settings: settings, subjects: map[string]reconcile.Subject{}}
	if sn.settings == nil {
		st, err := store.LoadSensorSettings(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		sn.settings = st
	}
	devs, err := store.SensorWatchDevices(ctx, db, scope, f)
	if err != nil {
		return nil, err
	}
	sn.devices = devs
	if len(devs) == 0 {
		return sn, nil
	}
	if sn.targets, err = resolveTargets(ctx, db, scope, devs); err != nil {
		return nil, err
	}
	var resIDs []string
	seenRes := map[string]bool{}
	for _, t := range sn.targets {
		if t.ResourceID != "" && !seenRes[t.ResourceID] {
			seenRes[t.ResourceID] = true
			resIDs = append(resIDs, t.ResourceID)
		}
	}
	d := sn.today
	stays, err := store.SensorStays(ctx, db, scope, resIDs, d.AddDays(-1), d.AddDays(1))
	if err != nil {
		return nil, err
	}
	rels, err := store.SensorReleases(ctx, db, scope, resIDs, d.AddDays(-1), d.AddDays(1))
	if err != nil {
		return nil, err
	}
	policy := "with_consent"
	if occ, err := store.GetOccupancySettings(ctx, db, scope); err == nil {
		policy = occ.ReleasePolicy
	}
	var vessels []string
	for _, st := range stays {
		if st.VesselID != "" {
			vessels = append(vessels, st.VesselID)
		}
	}
	vps, err := store.SensorVesselPresences(ctx, db, scope, vessels)
	if err != nil {
		return nil, err
	}
	staysOf := map[string][]store.SensorStay{}
	byAlloc := map[string]store.SensorStay{}
	for _, st := range stays {
		staysOf[st.ResourceID] = append(staysOf[st.ResourceID], st)
		byAlloc[st.AllocationID] = st
	}
	relsOf := map[string][]store.SensorRelease{}
	for _, r := range rels {
		relsOf[r.ResourceID] = append(relsOf[r.ResourceID], r)
	}
	for _, dev := range devs {
		t := sn.targets[dev.ID]
		sub := reconcile.Subject{
			DeviceID: dev.ID, ResourceType: dev.ResourceType, ResourceID: dev.ResourceID, MapID: dev.MapID,
			Informational: t.Informational, Active: dev.Active, CreatedAt: dev.CreatedAt, Physical: dev.Physical,
			LastContactAt: dev.LastContactAt, BatteryPct: dev.BatteryPct,
			Facts: reconcile.Facts{Presences: map[string]reconcile.Presence{}},
		}
		if dev.PhysicalSince != nil {
			sub.PhysicalSince = *dev.PhysicalSince
		}
		if sub.Physical == "" {
			sub.Physical = reconcile.Unknown
		}
		if !t.Informational {
			allocs := map[string]bool{}
			for _, st := range staysOf[t.ResourceID] {
				if !segmentHit(st, dev.FromCm, dev.ToCm) {
					continue
				}
				if covers(st.FirstNight, st.LastNight, d) {
					sub.Facts.Today = append(sub.Facts.Today, stayOf(st, policy))
					allocs[st.AllocationID] = true
				}
				if covers(st.FirstNight, st.LastNight, d.AddDays(-1)) {
					sub.Facts.Yesterday = append(sub.Facts.Yesterday, stayOf(st, policy))
					allocs[st.AllocationID] = true
				}
				if st.VesselID != "" {
					if vp, ok := vps[st.VesselID]; ok {
						p := reconcile.Presence{
							VesselID: st.VesselID, State: vp.State, LastSource: vp.LastSource, PresenceID: vp.LastPresenceID, Since: vp.Since,
						}
						if !vp.ExpectedReturnOn.IsZero() {
							e := vp.ExpectedReturnOn
							p.ExpectedReturnOn = &e
						}
						sub.Facts.Presences[st.VesselID] = p
					}
				}
			}
			for _, r := range relsOf[t.ResourceID] {
				if !allocs[r.AllocationID] || r.ToNight.Before(d) || r.FromNight.After(d.AddDays(1)) {
					continue
				}
				rel := reconcile.Release{
					ID: r.ID, AllocationID: r.AllocationID, FromNight: r.FromNight, ToNight: r.ToNight,
					Resold: r.ResoldAllocationID != "", ResaleStatus: r.ResaleStatus,
				}
				if rs, ok := byAlloc[r.ResoldAllocationID]; ok {
					stay := stayOf(rs, policy)
					rel.ResaleStay = &stay
				}
				sub.Facts.Releases = append(sub.Facts.Releases, rel)
			}
		}
		sn.subjects[dev.ID] = sub
	}
	return sn, nil
}

// alertKeys converts uncleared alert rows to the rules' keys.
func alertKeys(rows []store.SensorAlert) []reconcile.AlertKey {
	out := make([]reconcile.AlertKey, 0, len(rows))
	for _, a := range rows {
		out = append(out, reconcile.AlertKey{DeviceID: a.DeviceID, Kind: a.Kind, ConditionKey: a.ConditionKey, Open: a.Status == store.SensorAlertOpen})
	}
	return out
}
