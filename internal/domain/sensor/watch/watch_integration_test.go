//go:build integration

package watch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestReconcile_UnexpectedOccupiedAndDismiss(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorOccupied, e.now().Add(-14*time.Minute))
		if r := e.reconcile(t); r.Opened != 0 {
			t.Fatalf("before the threshold: %+v", r)
		}
		e.Clock.Advance(time.Minute)
		e.set(t, d, store.SensorOccupied, e.now().Add(-15*time.Minute))
		if r := e.reconcile(t); r.Opened != 1 {
			t.Fatalf("at the threshold: %+v", r)
		}
		a := e.openAlert(t, d.ID, store.SensorAlertUnexpectedOccupied)
		evs := e.Events(t, event.SensorAnomaly)
		if len(evs) != 1 || e.AuditCount(t, "sensor_alert.open") != 1 {
			t.Fatalf("event %d audit %d", len(evs), e.AuditCount(t, "sensor_alert.open"))
		}
		// A second run opens nothing (INV-104).
		if r := e.reconcile(t); r.Opened != 0 {
			t.Fatalf("second run: %+v", r)
		}
		// List and get.
		page, err := e.w.ListAlerts(ctx, e.Owner, e.MarinaID, AlertFilter{}, "", 0)
		if err != nil || len(page.Items) != 1 || page.Items[0].ResourceCode != "A1" || !slices.Contains(page.Items[0].AllowedActions, ActionDismiss) {
			t.Fatalf("list %v %+v", err, page)
		}
		// Wrong version, wrong action, then dismiss.
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version+1, ActionInput{Action: ActionDismiss}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("version: %v", err)
		}
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionCreateTask}); !errors.Is(err, ErrActionNotAllowed) {
			t.Fatalf("not allowed: %v", err)
		}
		v, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionDismiss, Note: "barca del cantiere"})
		if err != nil || v.Status != store.SensorAlertDismissed || v.DecidedByUserID == "" || len(v.AllowedActions) != 0 {
			t.Fatalf("dismiss %v %+v", err, v)
		}
		// Dismissed: suppressed until the condition clears.
		e.Clock.Advance(10 * time.Minute)
		if r := e.reconcile(t); r.Opened != 0 {
			t.Fatalf("dismissed reopened: %+v", r)
		}
		e.set(t, d, store.SensorFree, e.now())
		e.reconcile(t)
		if e.countAlerts(t, "cleared_at IS NULL") != 0 {
			t.Fatal("free berth: condition cleared")
		}
		e.Clock.Advance(time.Hour)
		e.set(t, d, store.SensorOccupied, e.now().Add(-20*time.Minute))
		if r := e.reconcile(t); r.Opened != 1 {
			t.Fatalf("new occupation: %+v", r)
		}
		e.invariants(t)
		// Another marina's alert is 404; a role without presence.write is 403.
		_, _, other := e.OtherOrganization(t)
		if _, err := e.w.GetAlert(ctx, other, e.MarinaID, a.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		for _, ev := range e.Events(t, event.SensorAnomaly) {
			if len(ev.Payload) == 0 || bytesContains(ev.Payload, "barca") {
				t.Fatal("event data carries the note")
			}
		}
	})
}

func TestReconcile_TransitArrivalConfirm(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		tr := e.Transit(t, e.Berths[1], "2027-06-02", "2027-06-04", true)
		d := e.device(t, store.SensorResourceBerth, e.Berths[1], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorOccupied, e.now().Add(-30*time.Minute))
		e.reconcile(t)
		a := e.openAlert(t, d.ID, store.SensorAlertTransitArrival)
		if e.countAlerts(t, "kind = ?", store.SensorAlertUnexpectedOccupied) != 0 {
			t.Fatal("arrival day is never unexpected")
		}
		v, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmArrival})
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if v.Status != store.SensorAlertConfirmed || v.OutcomeRef == "" {
			t.Fatalf("alert %+v", v)
		}
		c, err := store.GetContract(context.Background(), db.DB, e.scope, tr.Row.ID)
		if err != nil || c.Status != string(contract.StatusArrived) {
			t.Fatalf("transit %v %+v", err, c)
		}
		pr, err := store.GetPresence(context.Background(), db.DB, e.scope, v.OutcomeRef)
		if err != nil || pr.Source != presence.SourceStaff || pr.Event != presence.EventArrived {
			t.Fatalf("presence %v %+v", err, pr)
		}
		// Decided: a second confirm is a version conflict, the next run clears the condition.
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmArrival}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("second confirm: %v", err)
		}
		e.reconcile(t)
		if e.countAlerts(t, "cleared_at IS NULL") != 0 {
			t.Fatal("arrived transit: condition cleared")
		}
		e.invariants(t)
	})
}

func TestReconcile_AnnualAwayReleaseAndReturn(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		bg := context.Background()
		e.enableEffect(t)
		c := e.mooring(t, e.Berths[2], contract.TermAnnual)
		vessel := c.Row.VesselID
		d := e.device(t, store.SensorResourceBerth, e.Berths[2], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorFree, e.now().Add(-6*time.Hour+time.Minute))
		if r := e.reconcile(t); r.Presences != 0 {
			t.Fatalf("before 6 h: %+v", r)
		}
		e.Clock.Advance(time.Minute)
		e.set(t, d, store.SensorFree, e.now().Add(-6*time.Hour))
		if r := e.reconcile(t); r.Presences != 1 {
			t.Fatalf("after 6 h: %+v", r)
		}
		vp, err := store.GetVesselPresence(bg, db.DB, e.scope, vessel)
		if err != nil || vp.State != presence.StateOut {
			t.Fatalf("vessel presence %v %+v", err, vp)
		}
		last, _ := store.GetPresence(bg, db.DB, e.scope, vp.LastPresenceID)
		if last.Source != presence.SourceSensor || last.Event != presence.EventDeparted || !last.At.Equal(e.now().Add(-6*time.Hour)) || last.UserID != "" {
			t.Fatalf("sensor presence %+v", last)
		}
		// 3 days away: release proposal for [D+1, D+7].
		e.Clock.Advance(3*24*time.Hour - 6*time.Hour)
		e.battery(t, d, 80)
		e.reconcile(t)
		a := e.openAlert(t, d.ID, store.SensorAlertReleaseProposal)
		refs := parseRefs(a.Refs)
		today := e.today()
		if refs.ProposedFrom != today.AddDays(1).String() || refs.ProposedTo != today.AddDays(7).String() || refs.AllocationID != c.Allocations[0].ID {
			t.Fatalf("refs %+v", refs)
		}
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmRelease}); !errors.Is(err, ErrConsentRequired) {
			t.Fatalf("consent: %v", err)
		}
		far := today.AddDays(9)
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmRelease, ConsentConfirmed: true, To: &far}); err == nil {
			t.Fatal("range outside the proposal accepted")
		}
		to := today.AddDays(2)
		v, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmRelease, ConsentConfirmed: true, To: &to})
		if err != nil || v.Status != store.SensorAlertConfirmed {
			t.Fatalf("confirm release %v %+v", err, v)
		}
		rel, err := store.GetRelease(bg, db.DB, e.scope, v.OutcomeRef)
		if err != nil || rel.FromNight != today.AddDays(1) || rel.ToNight != to || rel.Source != "office" {
			t.Fatalf("release %v %+v", err, rel)
		}
		// Next day, the berth is occupied again on a released night: early return (not resold) → revoke.
		e.Clock.Advance(24 * time.Hour)
		e.set(t, d, store.SensorOccupied, e.now().Add(-20*time.Minute))
		e.reconcile(t)
		er := e.openAlert(t, d.ID, store.SensorAlertEarlyReturn)
		if r := parseRefs(er.Refs); r.Resold == nil || *r.Resold || r.ReleaseID != rel.ID {
			t.Fatalf("early return refs %+v", r)
		}
		if e.countAlerts(t, "kind = ? AND status = 'open'", store.SensorAlertUnexpectedOccupied) != 0 {
			t.Fatal("R6 wins over R1")
		}
		v, err = e.w.Act(ctx, e.Owner, e.MarinaID, er.ID, er.Version, ActionInput{Action: ActionRevokeRelease})
		if err != nil || v.Status != store.SensorAlertConfirmed {
			t.Fatalf("revoke %v %+v", err, v)
		}
		rel, _ = store.GetRelease(bg, db.DB, e.scope, rel.ID)
		if rel.Status != store.ReleaseRevoked {
			t.Fatalf("release after revoke %+v", rel)
		}
		// The release is gone: return after a sensor departure → automatic arrival.
		if r := e.reconcile(t); r.Presences != 1 {
			t.Fatalf("automatic return: %+v", r)
		}
		vp, _ = store.GetVesselPresence(bg, db.DB, e.scope, vessel)
		if vp.State != presence.StateInPort {
			t.Fatalf("vessel back %+v", vp)
		}
		e.invariants(t)
	})
}

func TestReconcile_ReturnDetectedAfterStaffDeparture(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		e.enableEffect(t)
		c := e.mooring(t, e.Berths[0], contract.TermAnnual)
		at := e.now().Add(-48 * time.Hour)
		if _, err := e.Presence.Record(ctx, e.Owner, e.MarinaID, presence.Input{VesselID: c.Row.VesselID, Event: presence.EventDeparted, At: &at}); err != nil {
			t.Fatal(err)
		}
		d := e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorOccupied, e.now().Add(-20*time.Minute))
		if r := e.reconcile(t); r.Presences != 0 {
			t.Fatalf("staff departure: no automatic return %+v", r)
		}
		a := e.openAlert(t, d.ID, store.SensorAlertReturnDetected)
		v, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionConfirmArrival})
		if err != nil || v.Status != store.SensorAlertConfirmed {
			t.Fatalf("confirm %v %+v", err, v)
		}
		vp, _ := store.GetVesselPresence(context.Background(), db.DB, e.scope, c.Row.VesselID)
		if vp.State != presence.StateInPort {
			t.Fatalf("presence %+v", vp)
		}
		e.invariants(t)
	})
}

func TestReconcile_SilentBatteryTasks(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		silent := e.device(t, store.SensorResourceBerth, e.Berths[3], nil, nil) // never contacted
		low := e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil)
		e.battery(t, low, 12)
		e.reconcile(t)
		a := e.openAlert(t, silent.ID, store.SensorAlertDeviceSilent)
		b := e.openAlert(t, low.ID, store.SensorAlertBatteryLow)
		if a.ConditionKey != "silent:never" {
			t.Fatalf("key %s", a.ConditionKey)
		}
		v, err := e.w.Act(ctx, e.Owner, e.MarinaID, a.ID, a.Version, ActionInput{Action: ActionCreateTask, Priority: "high"})
		if err != nil || v.Status != store.SensorAlertConfirmed || v.OutcomeRef == "" {
			t.Fatalf("task %v %+v", err, v)
		}
		task, err := store.GetMaintenanceTask(context.Background(), db.DB, e.scope, v.OutcomeRef)
		if err != nil || task.ElementType != store.ElementSensor || task.ElementID != silent.ID || task.Source != store.TaskSourceSensor || task.Priority != "high" {
			t.Fatalf("task %v %+v", err, task)
		}
		// The battery recovered past the band: the alert is stale (409) and resolved in the same call.
		e.battery(t, low, 30)
		if _, err := e.w.Act(ctx, e.Owner, e.MarinaID, b.ID, b.Version, ActionInput{Action: ActionCreateTask}); !errors.Is(err, ErrAlertStale) {
			t.Fatalf("stale: %v", err)
		}
		got, _ := store.GetSensorAlert(context.Background(), db.DB, e.scope, b.ID)
		if got.Status != store.SensorAlertResolved || got.ClearedAt == nil {
			t.Fatalf("stale alert %+v", got)
		}
		// A contact clears the silent condition.
		e.battery(t, silent, 90)
		e.reconcile(t)
		if e.countAlerts(t, "cleared_at IS NULL") != 0 {
			t.Fatal("conditions cleared")
		}
		e.invariants(t)
	})
}

func TestReconcile_EffectDisabledNoPresence(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		e.mooring(t, e.Berths[2], contract.TermAnnual)
		d := e.device(t, store.SensorResourceBerth, e.Berths[2], nil, nil)
		e.battery(t, d, 80)
		e.set(t, d, store.SensorFree, e.now().Add(-10*time.Hour))
		if r := e.reconcile(t); r.Presences != 0 {
			t.Fatalf("effect disabled: %+v", r)
		}
		n, _ := db.NewSelect().Table("presences").Where("source = 'sensor'").Count(context.Background())
		if n != 0 {
			t.Fatal("sensor presence written")
		}
	})
}

func TestReconcile_NeverChangesOccupancy(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		e.enableEffect(t)
		e.Transit(t, e.Berths[1], "2027-06-02", "2027-06-04", true)
		e.mooring(t, e.Berths[2], contract.TermAnnual)
		e.mooring(t, e.Berths[3], contract.TermAnnual)
		devs := []*store.SensorDevice{
			e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil),
			e.device(t, store.SensorResourceBerth, e.Berths[1], nil, nil),
			e.device(t, store.SensorResourceBerth, e.Berths[2], nil, nil),
			e.device(t, store.SensorResourceBerth, e.Berths[3], nil, nil),
			e.device(t, store.SensorResourceDynamicArea, e.Area, i64(0), i64(1000)),
		}
		e.set(t, devs[0], store.SensorOccupied, e.now().Add(-time.Hour))
		e.set(t, devs[1], store.SensorOccupied, e.now().Add(-time.Hour))
		e.set(t, devs[2], store.SensorFree, e.now().Add(-80*time.Hour))
		e.battery(t, devs[3], 5)
		e.set(t, devs[4], store.SensorOccupied, e.now().Add(-2*time.Hour))
		before := e.occupancyHash(t)
		for range 4 {
			e.reconcile(t)
			e.Clock.Advance(26 * time.Hour)
		}
		if after := e.occupancyHash(t); after != before {
			t.Fatal("sensors changed occupancy, releases, contracts or charges (INV-100)")
		}
		if e.countAlerts(t, "1 = 1") == 0 {
			t.Fatal("scenario opened no alert")
		}
		e.invariants(t)
	})
}

func TestWatch_MapHistoryDayViewReport(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newWenv(t, db)
		ctx := fixture.Ctx()
		d := e.device(t, store.SensorResourceBerth, e.Berths[0], nil, nil)
		info := e.device(t, store.SensorResourcePedestal, store.NewID(), nil, nil) // a pedestal with no berth: informational
		_ = info
		today := e.today()
		loc := e.loc()
		// Occupied from 20:00 two nights ago, a free berth in the contracts: two discrepant nights so far.
		since := time.Date(today.AddDays(-2).Year(), today.AddDays(-2).Month(), today.AddDays(-2).Day(), 20, 0, 0, 0, loc)
		e.set(t, d, store.SensorOccupied, since)
		m, err := e.w.Map(ctx, e.Owner, e.MarinaID, "", nil)
		if err != nil || m.MapID != e.MapID {
			t.Fatalf("map %v %+v", err, m)
		}
		var entry *MapEntry
		for i := range m.Entries {
			if m.Entries[i].DeviceID == d.ID {
				entry = &m.Entries[i]
			}
		}
		if entry == nil || entry.Physical != store.SensorOccupied || entry.Expectation != "free" || !entry.Discrepancy || entry.Health != HealthOK {
			t.Fatalf("entry %+v", entry)
		}
		past := today.AddDays(-2)
		pm, err := e.w.Map(ctx, e.Owner, e.MarinaID, e.MapID, &past)
		if err != nil || pm.Date == nil || pm.Entries[0].Health != "" {
			t.Fatalf("past map %v %+v", err, pm)
		}
		for _, x := range pm.Entries {
			if x.DeviceID == d.ID && (x.Physical != store.SensorOccupied || !x.Discrepancy) {
				t.Fatalf("past entry %+v", x)
			}
		}
		// Day view: sensor_discrepancy on A1.
		dv, err := e.Occupancy.GetDay(ctx, e.Owner, e.MarinaID, e.MapID, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, el := range dv.Elements {
			if el.ResourceID == e.Berths[0] && slices.Contains(el.Anomalies, engine.AnomalySensorDiscrepancy) {
				found = true
			}
		}
		if !found {
			t.Fatal("day view anomaly sensor_discrepancy missing")
		}
		// History: state at from, the change, the nights.
		from, to := today.AddDays(-3), today.AddDays(-1)
		h, err := e.w.History(ctx, e.Owner, e.MarinaID, HistoryQuery{ResourceType: store.SensorResourceBerth, ResourceID: e.Berths[0], From: &from, To: &to, IncludeReadings: true})
		if err != nil {
			t.Fatal(err)
		}
		if h.StateAtFrom != store.SensorUnknown || len(h.Changes) != 1 || len(h.Nights) != 3 || h.ReadingsFrom == nil || h.Readings == nil {
			t.Fatalf("history %+v", h)
		}
		if h.Nights[0].Physical != "unknown" || h.Nights[1].Physical != "occupied" || !h.Nights[1].Discrepancy || h.Nights[2].Physical != "occupied" {
			t.Fatalf("nights %+v", h.Nights)
		}
		long := today.AddDays(-40)
		if _, err := e.w.History(ctx, e.Owner, e.MarinaID, HistoryQuery{ResourceType: "berth", ResourceID: e.Berths[0], From: &long}); !errors.Is(err, ErrRangeTooLong) {
			t.Fatalf("range: %v", err)
		}
		if _, err := e.w.History(ctx, e.Owner, e.MarinaID, HistoryQuery{ResourceType: "berth", ResourceID: store.NewID()}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown berth: %v", err)
		}
		dh, err := e.w.DeviceHistory(ctx, e.Owner, e.MarinaID, d.ID, HistoryQuery{From: &from, To: &to})
		if err != nil || len(dh.Changes) != 1 || len(dh.DeviceIDs) != 1 {
			t.Fatalf("device history %v %+v", err, dh)
		}
		// Report: rows = the discrepant nights of the drill; totals = Σ rows (INV-62).
		reports := report.NewService(report.Deps{DB: db, Clock: e.Clock, Prices: e.Prices, Log: e.Log})
		res, err := reports.Sensors(ctx, e.Owner, e.MarinaID, report.Params{From: from, To: to})
		if err != nil {
			t.Fatal(err)
		}
		var nights []calendar.Date
		for _, r := range res.Records {
			nights = append(nights, r.Night)
			if r.Kind != "occupied_unexpected" || r.ResourceCode != "A1" || r.Contractual != "free" {
				t.Fatalf("row %+v", r)
			}
		}
		if len(nights) != 2 || res.Totals.Records != 2 || res.Totals.OccupiedUnexpected.Value != 2 || res.Totals.NightsCompared.Value != 3 {
			t.Fatalf("report %+v %+v", res.Totals, nights)
		}
		drill := 0
		for _, n := range h.Nights {
			if n.Discrepancy {
				drill++
			}
		}
		if int64(drill) != res.Totals.Records {
			t.Fatalf("drill %d report %d", drill, res.Totals.Records)
		}
		if _, err := reports.Sensors(ctx, e.Owner, e.MarinaID, report.Params{From: today.AddDays(-380), To: today}); !errors.Is(err, report.ErrInvalidPeriod) {
			t.Fatalf("period: %v", err)
		}
	})
}

func bytesContains(b []byte, s string) bool { return strings.Contains(string(b), s) }
