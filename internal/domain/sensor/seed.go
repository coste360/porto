package sensor

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Driver returns the registered polled source with code (nil when none): the demo seed feeds the fake source.
func (s *Service) Driver(code string) ext.SensorSource { return s.driver(code) }

// SeedBackdated gives devices a physical history older than the ingestion window (demo seed, m12-plan §17.2): for
// each device of states it confirms the state at at (a state change from unknown with reason readings and its
// event) and sets last_observed_at to at. Only devices never read are changed (an existing history stays as it
// is, so the changes keep alternating, Q41). harbor.configure.
func (s *Service) SeedBackdated(ctx context.Context, p *auth.Principal, marinaID string, states map[string]string, at time.Time) (int, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(states))
	for id, st := range states {
		if st != store.SensorOccupied && st != store.SensorFree && st != store.SensorUnknown {
			return 0, errors.New("sensor: bad seed state")
		}
		ids = append(ids, id)
	}
	now := store.UTC(s.d.Clock.Now())
	at = store.UTC(at)
	n := 0
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n = 0
		devices, err := store.SensorDevicesByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		if err := store.EnsureSensorDeviceStates(ctx, tx, scope.ID(), ids, now); err != nil {
			return err
		}
		locked, err := store.LockSensorDeviceStates(ctx, tx, scope.ID(), ids)
		if err != nil {
			return err
		}
		for _, id := range ids {
			d, st := devices[id], locked[id]
			if d == nil || st == nil || st.LastObservedAt != nil || st.Physical != store.SensorUnknown {
				continue
			}
			h := hysteresis.State{Confirmed: states[id], ConfirmedSince: at, LastObservedAt: at}
			fromHysteresis(st, h)
			st.UpdatedAt = now
			if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
				return err
			}
			if states[id] != store.SensorUnknown {
				ch := hysteresis.Change{From: store.SensorUnknown, To: states[id], At: at, Reason: hysteresis.ReasonReadings}
				if err := s.writeChange(ctx, tx, scope, d, ch, now); err != nil {
					return err
				}
			}
			n++
		}
		return nil
	})
	return n, err
}
