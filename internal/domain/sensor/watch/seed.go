package watch

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// SeedDemo prepares the demo marina for one alert of every kind and one automatic presence, then runs the
// reconciliation once (m12-plan §17.2, make seed). It picks devices whose berths fit each scenario (free berth,
// transit arriving today, active mooring contracts), records the staff departures and the release the scenarios
// need through the presence and occupancy services with p, and sets the physical states with back-dated state
// changes (only on devices whose history allows it: changes stay ordered, Q41). Scenarios without a fitting device
// are skipped. A marina that already has sensor alerts is left as it is. The marina must have
// presence_effect_enabled for the automatic presence (the caller saves the settings first).
func (s *Service) SeedDemo(ctx context.Context, p *auth.Principal, marinaID string) (ReconcileResult, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return ReconcileResult{}, err
	}
	db := s.d.DB.DB
	page, err := store.ListSensorAlerts(ctx, db, scope, store.SensorAlertFilter{}, "", 1)
	if err != nil || len(page.Items) > 0 {
		return ReconcileResult{}, err
	}
	now := s.now()
	sn, err := s.loadSnapshot(ctx, db, scope, store.SensorWatchDeviceFilter{ActiveOnly: true}, now, nil)
	if err != nil || len(sn.devices) == 0 {
		return ReconcileResult{}, err
	}
	ids := make([]string, 0, len(sn.devices))
	for _, d := range sn.devices {
		ids = append(ids, d.ID)
	}
	changes, err := store.SensorStateChangesOf(ctx, db, scope, ids, time.Unix(0, 0), now.Add(time.Hour))
	if err != nil {
		return ReconcileResult{}, err
	}
	lastChange := map[string]time.Time{}
	for _, c := range changes {
		lastChange[c.DeviceID] = c.At
	}
	plan := &seedPlan{used: map[string]bool{}}
	pick := func(fit func(sub reconcile.Subject) bool, since time.Time) *store.SensorWatchDevice {
		for i := range sn.devices {
			d := &sn.devices[i]
			sub := sn.subjects[d.ID]
			if plan.used[d.ID] || sub.Informational || d.ResourceType == store.SensorResourcePedestal || lastChange[d.ID].After(since) || !fit(sub) {
				continue
			}
			plan.used[d.ID] = true
			return d
		}
		return nil
	}
	today := sn.today
	single := func(sub reconcile.Subject) *reconcile.Stay {
		var out *reconcile.Stay
		n := 0
		for i := range sub.Facts.Today {
			if sub.Facts.Today[i].VesselID != "" {
				out, n = &sub.Facts.Today[i], n+1
			}
		}
		if n != 1 || len(sub.Facts.Releases) > 0 {
			return nil
		}
		return out
	}
	mooring := func(annual bool) func(reconcile.Subject) bool {
		return func(sub reconcile.Subject) bool {
			st := single(sub)
			if st == nil || st.ContractKind != "mooring" || st.Status != "active" || st.AllocationKind != store.AllocKindContract ||
				st.LastNight.Before(today.AddDays(3)) || (annual && st.ContractTerm != "annual") {
				return false
			}
			pr := sub.Facts.Presences[st.VesselID]
			return pr.State == "" || pr.State == presence.StateInPort
		}
	}
	// a. unexpected_occupied on a free berth.
	since := now.Add(-time.Hour)
	if d := pick(func(sub reconcile.Subject) bool { return len(sub.Facts.Today) == 0 && len(sub.Facts.Yesterday) == 0 }, since); d != nil {
		plan.add(d, reconcile.Occupied, since)
	}
	// b. transit_arrival: a confirmed transit arriving today (or yesterday) and the berth occupied since.
	if d := pick(func(sub reconcile.Subject) bool {
		st := single(sub)
		return st != nil && st.ContractKind == "transit" && st.Status == "confirmed" &&
			(st.FirstNight.Equal(today) || st.FirstNight.Equal(today.AddDays(-1)))
	}, now.Add(-30*time.Minute)); d != nil {
		at := now.Add(-30 * time.Minute)
		if mid := single(sn.subjects[d.ID]).FirstNight.Time(sn.loc); at.Before(mid) {
			at = mid.Add(time.Minute)
		}
		plan.add(d, reconcile.Occupied, at)
	}
	// c. release_proposal: an annual vessel away for 4 days (staff departure), berth free since.
	away := now.Add(-4 * 24 * time.Hour)
	if d := pick(mooring(false), away); d != nil {
		st := single(sn.subjects[d.ID])
		if err := s.seedDeparture(ctx, p, marinaID, st, away); err == nil {
			plan.add(d, reconcile.Free, away)
		}
	}
	// d. automatic presence: an annual vessel in port whose berth has been free for 7 hours.
	if d := pick(mooring(true), now.Add(-7*time.Hour)); d != nil {
		plan.add(d, reconcile.Free, now.Add(-7*time.Hour))
	}
	// e. return_detected: a staff departure two days ago, berth occupied for an hour.
	if d := pick(mooring(false), since); d != nil {
		st := single(sn.subjects[d.ID])
		if err := s.seedDeparture(ctx, p, marinaID, st, now.Add(-48*time.Hour)); err == nil {
			plan.add(d, reconcile.Occupied, since)
		}
	}
	// f. early_return: nights released from today, berth occupied for an hour.
	if d := pick(mooring(false), since); d != nil && s.d.Occupancy != nil {
		st := single(sn.subjects[d.ID])
		if _, err := s.d.Occupancy.CreateRelease(ctx, p, marinaID, occupancy.ReleaseInput{
			AllocationID: st.AllocationID, From: today, To: today.AddDays(2), Source: "office",
		}); err == nil {
			plan.add(d, reconcile.Occupied, since)
		}
	}
	// g. device_silent and h. battery_low on two more devices (health only, no state change).
	if d := pick(func(reconcile.Subject) bool { return true }, now.Add(time.Hour)); d != nil {
		c := now.Add(-4 * time.Hour)
		plan.health = append(plan.health, seedHealth{dev: d, contact: &c})
	}
	if d := pick(func(reconcile.Subject) bool { return true }, now.Add(time.Hour)); d != nil {
		b := 12
		plan.health = append(plan.health, seedHealth{dev: d, battery: &b})
	}
	if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error { return s.applySeed(ctx, tx, scope, plan, now) }); err != nil {
		return ReconcileResult{}, err
	}
	sys, err := store.SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return ReconcileResult{}, err
	}
	return s.ReconcileMarina(ctx, sys)
}

// Seedable reports whether SeedDemo would do something: the marina has active devices and no sensor alert yet.
func (s *Service) Seedable(ctx context.Context, p *auth.Principal, marinaID string) (bool, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return false, err
	}
	db := s.d.DB.DB
	page, err := store.ListSensorAlerts(ctx, db, scope, store.SensorAlertFilter{}, "", 1)
	if err != nil || len(page.Items) > 0 {
		return false, err
	}
	devs, err := store.SensorWatchDevices(ctx, db, scope, store.SensorWatchDeviceFilter{ActiveOnly: true})
	return len(devs) > 0, err
}

type seedState struct {
	dev   *store.SensorWatchDevice
	state string
	since time.Time
}

type seedHealth struct {
	dev     *store.SensorWatchDevice
	contact *time.Time
	battery *int
}

type seedPlan struct {
	used   map[string]bool
	states []seedState
	health []seedHealth
}

func (p *seedPlan) add(d *store.SensorWatchDevice, state string, since time.Time) {
	p.states = append(p.states, seedState{dev: d, state: state, since: store.UTC(since)})
}

// seedDeparture records the staff departure of the vessel of a stay at at.
func (s *Service) seedDeparture(ctx context.Context, p *auth.Principal, marinaID string, st *reconcile.Stay, at time.Time) error {
	if s.d.Presence == nil || st == nil {
		return ErrActionNotAllowed
	}
	ret := calendar.In(at, time.UTC).AddDays(30)
	_, err := s.d.Presence.Record(ctx, p, marinaID, presence.Input{
		VesselID: st.VesselID, Event: presence.EventDeparted, At: &at, AllocationID: st.AllocationID, ExpectedReturnOn: &ret,
	})
	return err
}

// applySeed writes the planned physical states (state change + event + state row) and health values.
func (s *Service) applySeed(ctx context.Context, tx bun.Tx, scope store.MarinaScope, plan *seedPlan, now time.Time) error {
	for _, x := range plan.states {
		st, err := store.GetSensorDeviceState(ctx, tx, scope, x.dev.ID)
		if err != nil {
			st = &store.SensorDeviceState{DeviceID: x.dev.ID, MarinaID: scope.ID(), Physical: store.SensorUnknown}
		}
		if st.Physical != x.state {
			c := &store.SensorStateChange{
				MarinaID: scope.ID(), DeviceID: x.dev.ID, ResourceType: x.dev.ResourceType, ResourceID: x.dev.ResourceID, MapID: x.dev.MapID,
				FromCm: x.dev.FromCm, ToCm: x.dev.ToCm, FromState: st.Physical, ToState: x.state, At: x.since, Reason: store.SensorReasonReadings,
				CreatedAt: now,
			}
			if err := store.InsertSensorStateChange(ctx, tx, c); err != nil {
				return err
			}
			if s.d.Outbox != nil {
				if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.SensorStateChanged, event.SensorStateChangedData{
					DeviceID: c.DeviceID, ResourceType: c.ResourceType, ResourceID: c.ResourceID, ResourceIDs: []string{c.ResourceID},
					MapID: c.MapID, FromCm: c.FromCm, ToCm: c.ToCm, From: c.FromState, To: c.ToState, At: c.At.Format(time.RFC3339), Reason: c.Reason,
				}); err != nil {
					return err
				}
			}
		}
		since := x.since
		st.Physical, st.PhysicalSince, st.Candidate, st.CandidateSince, st.CandidateCount = x.state, &since, "", nil, 0
		obs := now.Add(-time.Minute)
		if st.LastObservedAt == nil || st.LastObservedAt.Before(obs) {
			st.LastObservedAt = &obs
		}
		st.LastContactAt, st.UpdatedAt = &obs, now
		if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
			return err
		}
	}
	for _, h := range plan.health {
		st, err := store.GetSensorDeviceState(ctx, tx, scope, h.dev.ID)
		if err != nil {
			st = &store.SensorDeviceState{DeviceID: h.dev.ID, MarinaID: scope.ID(), Physical: store.SensorUnknown}
		}
		if h.contact != nil {
			c := store.UTC(*h.contact)
			st.LastContactAt = &c
		}
		if h.battery != nil {
			b := *h.battery
			st.BatteryPct = &b
		}
		st.UpdatedAt = now
		if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
			return err
		}
	}
	return nil
}
