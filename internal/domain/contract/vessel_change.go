package contract

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Change of vessel (spec 13.4; m4-plan §3.5, §15 D8): every live allocation of the contract from effectiveOn is
// truncated and the same line allocated again with the new vessel on the same placement and nights (no
// moved_from_id: INV-11 requires the same vessel on a chain). The fit of the new vessel is checked by the
// occupancy engine on each allocation (422 fit_blocked unless overridden). The same primitive rotates charter
// vessels (charter.go).

// reallocateVesselTx moves one allocation to vesselID from night from (kind and hold expiry kept).
func (s *Service) reallocateVesselTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, al store.Allocation,
	from calendar.Date, vesselID string, fit *occupancy.FitOverride,
) (*store.Allocation, error) {
	first := al.FirstNight
	if first.Before(from) {
		if err := s.alloc.TruncateTx(ctx, tx, scope, al.ID, from); err != nil {
			return nil, err
		}
		first = from
	} else if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusCancelled); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.OccupancyReleased, event.OccupancyReleasedData{
		AllocationIDs: []string{al.ID}, ResourceIDs: []string{al.ResourceID}, MapID: al.MapID,
		From: first, To: al.LastNight, Reason: event.ReleasedTruncate,
	}); err != nil {
		return nil, err
	}
	seg := segmentOf(&al)
	seg.First = first
	var exp *time.Time
	if al.Kind == store.AllocKindHold {
		e := al.ExpiresAt
		exp = &e
	}
	return s.allocateLine(ctx, tx, scope, a, s.now(), allocation{
		lineID: al.ContractLineID, vesselID: vesselID, kind: occupancy.Kind(al.Kind), seg: seg, expiresAt: exp, fit: fit,
	})
}

// changeVesselTx applies a vessel change to the locked contract from effectiveOn.
func (s *Service) changeVesselTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract,
	effectiveOn calendar.Date, vesselID, fitReason string,
) error {
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	var fit *occupancy.FitOverride
	if fitReason != "" {
		fit = occupancy.NewFitOverride(a.p, fitReason)
	}
	var created []store.Allocation
	for _, al := range activeOnly(allocs) {
		if al.LastNight.Before(effectiveOn) || al.ContractLineID == "" {
			continue
		}
		n, err := s.reallocateVesselTx(ctx, tx, scope, a, al, effectiveOn, vesselID, fit)
		if err != nil {
			return err
		}
		created = append(created, *n)
	}
	c.VesselID = vesselID
	return s.emitAllocated(ctx, tx, scope, created, c.ID)
}
