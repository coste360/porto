package report

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/sensor/nights"
	"github.com/coste360/porto/internal/store"
)

// R-sensors (M12, m12-plan §10.3): one row per (resource, night) with a physical vs contractual discrepancy, on the
// M7 machinery (reports.operational or reports.all): one read transaction, the pure calc.SensorNights and
// calc.BuildSensors, Totals = Σ rows (INV-62). Rows carry codes only, never vessel names.

// SensorsResult is the sensors report.
type SensorsResult = calc.SensorsResult

// MaxSensorsNights is the longest period of the sensors report (m12-plan §10.3).
const MaxSensorsNights = 366

// authorizeSensors checks reports.all, else reports.operational. Another marina is auth.ErrNotFound.
func (s *Service) authorizeSensors(ctx context.Context, p *auth.Principal, marinaID string) (access, error) {
	scope, perm, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermReportsAll, auth.PermReportsOperational)
	if err != nil {
		return access{}, err
	}
	return access{Scope: scope, Full: perm == auth.PermReportsAll}, nil
}

// CheckSensors authorises p for the sensors report (handlers call it before answering a parse error).
func (s *Service) CheckSensors(ctx context.Context, p *auth.Principal, marinaID string) error {
	_, err := s.authorizeSensors(ctx, p, marinaID)
	return err
}

// Sensors returns the sensors report, view discrepancies (reports.operational or reports.all).
func (s *Service) Sensors(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*SensorsResult, error) {
	acc, err := s.authorizeSensors(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if q.Report != "" && q.Report != calc.KeySensors {
		return nil, filterErr("report")
	}
	q.Report = calc.KeySensors
	if q.View != "" && q.View != calc.SensorsViewDiscrepancies {
		return nil, filterErr("view")
	}
	if err := checkPeriod(q.From, q.To); err != nil {
		return nil, err
	}
	var out *SensorsResult
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		var err error
		if q.From, q.To, err = r.period(q.From, q.To); err != nil {
			return err
		}
		if q.From.DaysUntil(q.To)+1 > MaxSensorsNights {
			return ErrInvalidPeriod
		}
		res, err := r.runSensors(q)
		if err != nil {
			return err
		}
		out = res
		return finish(res, q, r.today)
	})
	return out, err
}

func (r *run) runSensors(q Params) (*SensorsResult, error) {
	sc := r.scope()
	devs, err := store.SensorWatchDevices(r.ctx, r.tx, sc, store.SensorWatchDeviceFilter{MapID: q.Dims.MapID})
	if err != nil {
		return nil, err
	}
	in := calc.SensorsInput{GroupBy: q.GroupBy, MapID: q.Dims.MapID, GroupID: q.Dims.GroupID, Kind: q.Kind}
	in.From, in.To, in.Loc = q.From, q.To, r.loc
	if len(devs) == 0 {
		res := calc.BuildSensors(in)
		return &res, nil
	}
	// Facts resource of each device: the berth, the area of a segment, the berth of a pedestal socket (or none).
	var pedestals []string
	for _, d := range devs {
		if d.ResourceType == store.SensorResourcePedestal {
			pedestals = append(pedestals, d.ResourceID)
		}
	}
	pb, err := store.SensorPedestalBerths(r.ctx, r.tx, sc, pedestals)
	if err != nil {
		return nil, err
	}
	targetOf := map[string]string{}
	var ids, resIDs, codeIDs []string
	for _, d := range devs {
		t := d.ResourceID
		if d.ResourceType == store.SensorResourcePedestal {
			t = ""
			n := 0
			for _, b := range pb {
				if b.PedestalID == d.ResourceID && (d.PedestalSocket == "" || b.Socket == d.PedestalSocket) {
					t, n = b.BerthID, n+1
				}
			}
			if n != 1 {
				t = ""
			}
		}
		targetOf[d.ID] = t
		ids = append(ids, d.ID)
		codeIDs = append(codeIDs, d.ResourceID)
		if t != "" {
			resIDs = append(resIDs, t)
		}
	}
	winStart, _ := nights.Window(q.From, r.loc)
	_, winEnd := nights.Window(q.To, r.loc)
	f, err := store.LoadSensorNightFacts(r.ctx, r.tx, sc, ids, resIDs, q.From, q.To, winStart, winEnd)
	if err != nil {
		return nil, err
	}
	codes, err := store.SensorResourceCodesOf(r.ctx, r.tx, sc, codeIDs)
	if err != nil {
		return nil, err
	}
	maps, err := store.SensorReportMaps(r.ctx, r.tx, sc)
	if err != nil {
		return nil, err
	}
	groups, err := store.GroupsOfResources(r.ctx, r.tx, sc, q.Dims.MapID)
	if err != nil {
		return nil, err
	}
	changes := map[string][]nights.Change{}
	for _, c := range f.Changes {
		changes[c.DeviceID] = append(changes[c.DeviceID], nights.Change{To: c.ToState, At: c.At})
	}
	for _, d := range devs {
		t := targetOf[d.ID]
		g := groups[t]
		if g == nil {
			g = groups[d.ResourceID]
		}
		in.Subjects = append(in.Subjects, calc.SensorSubject{
			DeviceID: d.ID, DeviceCode: d.Code, ResourceType: d.ResourceType, ResourceID: d.ResourceID, ResourceCode: codes[d.ResourceID],
			TargetID: t, MapID: d.MapID, MapName: maps[d.MapID], FromCm: d.FromCm, ToCm: d.ToCm, Groups: g, Informational: t == "",
			StateBefore: f.StatesBefore[d.ID], Changes: changes[d.ID],
		})
	}
	for _, st := range f.Stays {
		in.Stays = append(in.Stays, calc.SensorStayRow{
			AllocationID: st.AllocationID, ResourceID: st.ResourceID, VesselID: st.VesselID, AllocationKind: st.Kind,
			ContractKind: st.ContractKind, FirstNight: st.FirstNight, LastNight: st.LastNight, XCm: st.XCm, LoaCm: st.LoaCm,
		})
	}
	for _, x := range f.Releases {
		in.Releases = append(in.Releases, calc.SensorReleaseRow{ID: x.ID, AllocationID: x.AllocationID, ResourceID: x.ResourceID, FromNight: x.FromNight, ToNight: x.ToNight})
	}
	for _, p := range f.Presences {
		in.Presences = append(in.Presences, calc.SensorPresenceRow{VesselID: p.VesselID, Event: p.Event, At: p.At})
	}
	for _, a := range f.Alerts {
		in.Alerts = append(in.Alerts, calc.SensorAlertRow{ID: a.ID, DeviceID: a.DeviceID, OpenedAt: a.OpenedAt, ClearedAt: a.ClearedAt})
	}
	res := calc.BuildSensors(in)
	return &res, nil
}
