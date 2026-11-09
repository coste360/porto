package presence

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// ErrSensorNotAllowed: the automatic presence of a sensor was asked for anything but an active annual mooring
// contract (a transit, a seasonal or monthly mooring, an allocation without a contract), an event other than an
// arrival or a departure, or from an offline replay (m12-plan §8.2 R3/R3b, D12). The reconcile job treats it as "no
// action".
var ErrSensorNotAllowed = errors.New("presence: sensor presence not allowed")

// RecordSensorTx records the automatic arrival or departure of the M12 reconciliation (rules R3 and R3b, opt-in per
// marina) inside the caller's transaction: presences.source = sensor, audit actor system, presence event in the
// outbox, never the contract hooks (sensors never change contracts, INV-100). It re-reads the vessel presence under
// a lock (PostgreSQL FOR UPDATE) and answers ErrInvalidTransition when another writer already moved the vessel to
// the target state, which the caller ignores. Only allocations of an active annual mooring contract qualify
// (INV-100); everything else is ErrSensorNotAllowed (Q43).
func (s *Service) RecordSensorTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, vesselID, allocationID, ev string,
	at time.Time,
) (*store.Presence, error) {
	if _, offline := reqctx.OfflineReplay(ctx); offline || (ev != EventArrived && ev != EventDeparted) || allocationID == "" {
		return nil, ErrSensorNotAllowed
	}
	cid, err := store.ContractOfAllocation(ctx, tx, scope, allocationID)
	if err != nil {
		return nil, notFound(err)
	}
	if cid == "" {
		return nil, ErrSensorNotAllowed
	}
	c, err := store.GetContract(ctx, tx, scope, cid)
	if err != nil {
		return nil, notFound(err)
	}
	if c.Kind != contract.KindMooring || c.Term != contract.TermAnnual || c.Status != string(contract.StatusActive) {
		return nil, ErrSensorNotAllowed
	}
	if _, err := store.LockVesselPresence(ctx, tx, scope, vesselID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	t := store.UTC(at)
	pr, _, err := s.recordTx(ctx, tx, scope, nil, Input{VesselID: vesselID, Event: ev, At: &t, AllocationID: allocationID}, SourceSensor)
	if err != nil {
		return nil, err
	}
	return pr, nil
}
