package contract

import (
	"context"
	"encoding/json"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Presence hooks (m3-plan §4.2, §4.4): the presence service records arrivals and departures and calls these in
// its transaction, so the transit state changes still go through this package (INV-30).

const stateInPort = "in_port"

func jsonUnmarshal(raw store.JSON, v any) error { return json.Unmarshal(raw, v) }

// OnArrivalTx applies a recorded arrival to the contract: a confirmed transit becomes arrived; a held transit
// stays held (its later confirmation chains to arrived, §13 D13); other contracts do not change. It returns the
// contract after the change (nil when contractID is empty).
func (s *Service) OnArrivalTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, contractID string) (*store.Contract, error) {
	if contractID == "" {
		return nil, nil
	}
	c, err := lockContract(ctx, tx, scope, contractID, "", 0)
	if err != nil {
		return nil, err
	}
	if c.Kind != KindTransit || c.Status != string(StatusConfirmed) {
		return c, nil
	}
	if err := s.arriveTx(ctx, tx, scope, actor{p: p}, c); err != nil {
		return nil, err
	}
	return c, nil
}

// arriveTx moves a confirmed transit to arrived.
func (s *Service) arriveTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract) error {
	t, err := Next(TransitTransitions, Status(c.Status), ActionArrive)
	if err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "transit.arrive", entityTransit, c.ID, map[string]any{"from_status": from, "status": c.Status}); err != nil {
		return err
	}
	return s.emitTransition(ctx, tx, scope, t, subject{contract: c})
}

// OnDepartureTx applies a departure recorded on day d: an arrived transit becomes departed and, when it leaves
// before its last night, the nights from d are freed (the price stays as it is in M3). Other contracts do not
// change.
func (s *Service) OnDepartureTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, contractID string, d calendar.Date) (*store.Contract, error) {
	if contractID == "" {
		return nil, nil
	}
	c, err := lockContract(ctx, tx, scope, contractID, "", 0)
	if err != nil {
		return nil, err
	}
	if c.Kind != KindTransit || c.Status != string(StatusArrived) {
		return c, nil
	}
	t, err := Next(TransitTransitions, Status(c.Status), ActionDepart)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, err
	}
	var freed []string
	for _, al := range activeOnly(allocs) {
		if al.LastNight.Before(d) {
			continue
		}
		if !al.FirstNight.Before(d) {
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusCancelled); err != nil {
				return nil, err
			}
		} else if err := s.alloc.TruncateTx(ctx, tx, scope, al.ID, d); err != nil {
			return nil, err
		}
		freed = append(freed, al.ID)
		from := d
		if al.FirstNight.After(d) {
			from = al.FirstNight
		}
		if err := s.emit(ctx, tx, scope, event.OccupancyReleased, event.OccupancyReleasedData{
			AllocationIDs: []string{al.ID}, ResourceIDs: []string{al.ResourceID}, MapID: al.MapID,
			From: from, To: al.LastNight, Reason: event.ReleasedTruncate,
		}); err != nil {
			return nil, err
		}
	}
	prev := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, actor{p: p}, "transit.depart", entityTransit, c.ID, map[string]any{
		"from_status": prev, "status": c.Status, "night": d, "freed": freed,
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return nil, err
	}
	return c, nil
}
