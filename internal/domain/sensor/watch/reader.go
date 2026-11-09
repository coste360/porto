package watch

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// SensorFacts implements occupancy.SensorReader for the day view (m12-plan §9.2): the physical facts and
// expectations of the sensors of a map today (empty for another date), keyed by the resource the anomaly is
// reported on (the berth, the dynamic area of a segment, the berth of a pedestal socket). Several devices on one
// resource: a discrepant one wins.
func (s *Service) SensorFacts(ctx context.Context, db bun.IDB, scope store.MarinaScope, mapID string, date calendar.Date, now time.Time) (occupancy.SensorFacts, error) {
	out := occupancy.SensorFacts{ByResource: map[string]engine.SensorFact{}}
	sn, err := s.loadSnapshot(ctx, db, scope, store.SensorWatchDeviceFilter{MapID: mapID, ActiveOnly: true}, store.UTC(now), nil)
	if err != nil {
		return out, err
	}
	out.UnexpectedAfterMinutes = sn.settings.UnexpectedAfterMinutes
	if !date.Equal(sn.today) || len(sn.devices) == 0 {
		return out, nil
	}
	in := sn.input(nil)
	for _, d := range sn.devices {
		t := sn.targets[d.ID]
		sub := sn.subjects[d.ID]
		exp, disc := reconcile.ExpectedPhysical(in, sub)
		key := t.ResourceID
		if t.Informational {
			key = d.ResourceID
		}
		f := engine.SensorFact{Physical: sub.Physical, Expectation: exp, Informational: t.Informational}
		if !sub.PhysicalSince.IsZero() {
			f.StableMinutes = int(sn.now.Sub(sub.PhysicalSince) / time.Minute)
		}
		if prev, ok := out.ByResource[key]; ok {
			prevDisc := !prev.Informational && reconcile.Discrepancy(prev.Physical, time.Duration(prev.StableMinutes)*time.Minute, prev.Expectation, in.Settings)
			if prevDisc || !disc {
				continue
			}
		}
		out.ByResource[key] = f
	}
	return out, nil
}
