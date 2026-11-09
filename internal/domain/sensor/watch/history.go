package watch

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/sensor/nights"
	"github.com/coste360/porto/internal/store"
)

// History limits (m12-plan §10.1).
const (
	maxHistoryDays     = 31
	defaultHistoryDays = 7
	maxReadings        = 10000
)

// nightsInput converts the stored facts to the pure night comparison.
func nightsInput(f *store.SensorNightFacts, loc *time.Location, devs []store.SensorWatchDevice, targets map[string]target, from, to calendar.Date) calc.SensorNightsInput {
	in := calc.SensorNightsInput{From: from, To: to, Loc: loc}
	changes := map[string][]nights.Change{}
	for _, c := range f.Changes {
		changes[c.DeviceID] = append(changes[c.DeviceID], nights.Change{To: c.ToState, At: c.At})
	}
	for _, d := range devs {
		t := targets[d.ID]
		in.Subjects = append(in.Subjects, calc.SensorSubject{
			DeviceID: d.ID, DeviceCode: d.Code, ResourceType: d.ResourceType, ResourceID: d.ResourceID, TargetID: t.ResourceID,
			MapID: d.MapID, FromCm: d.FromCm, ToCm: d.ToCm, Informational: t.Informational, StateBefore: f.StatesBefore[d.ID],
			Changes: changes[d.ID],
		})
	}
	for _, st := range f.Stays {
		in.Stays = append(in.Stays, calc.SensorStayRow{
			AllocationID: st.AllocationID, ResourceID: st.ResourceID, VesselID: st.VesselID, AllocationKind: st.Kind,
			ContractKind: st.ContractKind, FirstNight: st.FirstNight, LastNight: st.LastNight, XCm: st.XCm, LoaCm: st.LoaCm,
		})
	}
	for _, r := range f.Releases {
		in.Releases = append(in.Releases, calc.SensorReleaseRow{ID: r.ID, AllocationID: r.AllocationID, ResourceID: r.ResourceID, FromNight: r.FromNight, ToNight: r.ToNight})
	}
	for _, p := range f.Presences {
		in.Presences = append(in.Presences, calc.SensorPresenceRow{VesselID: p.VesselID, Event: p.Event, At: p.At})
	}
	for _, a := range f.Alerts {
		in.Alerts = append(in.Alerts, calc.SensorAlertRow{ID: a.ID, DeviceID: a.DeviceID, OpenedAt: a.OpenedAt, ClearedAt: a.ClearedAt})
	}
	return in
}

// nightRows compares the devices on the nights [from, to].
func (s *Service) nightRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, loc *time.Location, devs []store.SensorWatchDevice,
	targets map[string]target, from, to calendar.Date,
) ([]calc.SensorNight, error) {
	if len(devs) == 0 {
		return nil, nil
	}
	var ids, res []string
	for _, d := range devs {
		ids = append(ids, d.ID)
		if t := targets[d.ID]; t.ResourceID != "" {
			res = append(res, t.ResourceID)
		}
	}
	winStart, _ := nights.Window(from, loc)
	_, winEnd := nights.Window(to, loc)
	f, err := store.LoadSensorNightFacts(ctx, db, scope, ids, res, from, to, winStart, winEnd)
	if err != nil {
		return nil, err
	}
	return calc.SensorNights(nightsInput(f, loc, devs, targets, from, to)), nil
}

// combineNights merges the nights of several devices of one resource: occupied if any is occupied, else free if any
// is free, else unknown; the contractual status and the presence are the resource's.
func combineNights(rows []calc.SensorNight, from, to calendar.Date) []NightRow {
	by := map[calendar.Date][]calc.SensorNight{}
	for _, r := range rows {
		by[r.Night] = append(by[r.Night], r)
	}
	var out []NightRow
	for d := from; !d.After(to); d = d.AddDays(1) {
		n := NightRow{Night: d, Contractual: calc.SensorContractFree, Physical: nights.Unknown}
		for _, r := range by[d] {
			switch {
			case r.Physical == nights.Occupied:
				n.Physical = nights.Occupied
			case r.Physical == nights.Free && n.Physical == nights.Unknown:
				n.Physical = nights.Free
			}
			if r.Contractual != calc.SensorContractFree || n.Contractual == calc.SensorContractFree {
				n.Contractual = r.Contractual
			}
			if r.Presence != "" {
				n.Presence = r.Presence
			}
		}
		n.Discrepancy = calc.SensorDiscrepancyKind(n.Physical, n.Contractual) != ""
		out = append(out, n)
	}
	return out
}

// historyRange applies the defaults (to = today, from = to - 7) and the 31-day limit.
func historyRange(q HistoryQuery, today calendar.Date) (calendar.Date, calendar.Date, error) {
	to := today
	if q.To != nil {
		to = *q.To
	}
	from := to.AddDays(-defaultHistoryDays)
	if q.From != nil {
		from = *q.From
	}
	if to.Before(from) {
		return from, to, validationErr("to", "before_from")
	}
	if from.DaysUntil(to)+1 > maxHistoryDays {
		// ErrRangeTooLong joined with the field error so that the problem is 422 validation_failed on to.
		return from, to, errors.Join(ErrRangeTooLong, validationErr("to", "range_too_long"))
	}
	return from, to, nil
}

func encodeReadingsCursor(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano)))
}

func decodeReadingsCursor(c string) (*time.Time, error) {
	if c == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, store.ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, string(b))
	if err != nil {
		return nil, store.ErrInvalidCursor
	}
	return &t, nil
}

// History returns the physical history of a resource (map.view): the state changes recorded on it with the state
// at from, the alerts touching it, the contractual and physical status of each night and, with include_readings and
// a range inside the retention, the raw readings of its devices (newest first, at most 10,000 per page).
func (s *Service) History(ctx context.Context, p *auth.Principal, marinaID string, q HistoryQuery) (*History, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	switch q.ResourceType {
	case store.SensorResourceBerth, store.SensorResourceDynamicArea, store.SensorResourcePedestal:
	case "":
		return nil, validationErr("resource_type", "required")
	default:
		return nil, validationErr("resource_type", "invalid")
	}
	if q.ResourceID == "" {
		return nil, validationErr("resource_id", "required")
	}
	return s.history(ctx, scope, p, q)
}

// DeviceHistory returns the physical history of a device (map.view) on its current resource.
func (s *Service) DeviceHistory(ctx context.Context, p *auth.Principal, marinaID, deviceID string, q HistoryQuery) (*History, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView, store.SensorTableDevices, deviceID)
	if err != nil {
		return nil, err
	}
	dev, err := store.GetSensorDevice(ctx, s.d.DB.DB, scope, deviceID)
	if err != nil {
		return nil, notFound(err)
	}
	q.ResourceType, q.ResourceID, q.DeviceID = dev.ResourceType, dev.ResourceID, dev.ID
	return s.history(ctx, scope, p, q)
}

func (s *Service) history(ctx context.Context, scope store.MarinaScope, p *auth.Principal, q HistoryQuery) (*History, error) {
	before, err := decodeReadingsCursor(q.Cursor)
	if err != nil {
		return nil, err
	}
	var out *History
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		codes, err := store.SensorResourceCodesOf(ctx, tx, scope, []string{q.ResourceID})
		if err != nil {
			return err
		}
		if _, ok := codes[q.ResourceID]; !ok {
			return auth.ErrNotFound
		}
		now := s.now()
		loc := s.location(ctx, tx, scope)
		from, to, err := historyRange(q, calendar.In(now, loc))
		if err != nil {
			return err
		}
		start, end := from.Time(loc), to.AddDays(1).Time(loc)
		var devIDs []string
		if q.DeviceID != "" {
			devIDs = []string{q.DeviceID}
		} else if devIDs, err = store.SensorDevicesOnResource(ctx, tx, scope, q.ResourceID, end); err != nil {
			return err
		}
		out = &History{
			ResourceType: q.ResourceType, ResourceID: q.ResourceID, DeviceIDs: devIDs, From: from, To: to,
			StateAtFrom: nights.Unknown, Changes: []store.SensorStateChange{}, Alerts: []AlertView{}, Nights: []NightRow{},
		}
		if len(devIDs) == 0 {
			out.Nights = combineNights(nil, from, to)
			return nil
		}
		var changes []store.SensorStateChange
		if q.DeviceID != "" {
			changes, err = store.SensorStateChangesOf(ctx, tx, scope, devIDs, start, end)
		} else {
			changes, err = store.SensorStateChangesOnResource(ctx, tx, scope, q.ResourceID, start, end)
		}
		if err != nil {
			return err
		}
		out.Changes = append(out.Changes, changes...)
		states, err := store.SensorStatesBefore(ctx, tx, scope, devIDs, start)
		if err != nil {
			return err
		}
		for _, st := range states {
			switch {
			case st == nights.Occupied:
				out.StateAtFrom = nights.Occupied
			case st == nights.Free && out.StateAtFrom == nights.Unknown:
				out.StateAtFrom = nights.Free
			}
		}
		alerts, err := store.SensorAlertsTouching(ctx, tx, scope, []string{q.ResourceID}, start, end)
		if err != nil {
			return err
		}
		if q.DeviceID != "" {
			kept := alerts[:0]
			for _, a := range alerts {
				if a.DeviceID == q.DeviceID {
					kept = append(kept, a)
				}
			}
			alerts = kept
		}
		if out.Alerts, err = s.views(ctx, tx, scope, p, alerts); err != nil {
			return err
		}
		devs, err := store.SensorWatchDevices(ctx, tx, scope, store.SensorWatchDeviceFilter{DeviceIDs: devIDs})
		if err != nil {
			return err
		}
		targets, err := resolveTargets(ctx, tx, scope, devs)
		if err != nil {
			return err
		}
		rows, err := s.nightRows(ctx, tx, scope, loc, devs, targets, from, to)
		if err != nil {
			return err
		}
		out.Nights = combineNights(rows, from, to)
		if q.IncludeReadings {
			kept := now.Add(-s.d.RawRetention)
			out.ReadingsFrom = &kept
			rFrom := start
			if rFrom.Before(kept) {
				rFrom = kept
			}
			if rFrom.Before(end) {
				rs, err := store.SensorReadingsOf(ctx, tx, scope, devIDs, rFrom, end, before, maxReadings+1)
				if err != nil {
					return err
				}
				if len(rs) > maxReadings {
					rs = rs[:maxReadings]
					out.NextCursor = encodeReadingsCursor(rs[len(rs)-1].ObservedAt)
				}
				out.Readings = rs
			}
			if out.Readings == nil {
				out.Readings = []store.SensorReading{}
			}
		}
		return nil
	})
	return out, err
}
