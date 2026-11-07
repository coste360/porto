package contract

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// UnexpectedTransit creates the held transit of a vessel that arrived without a booking (m3-plan §13 D29) and
// runs then in the same transaction (the presence service records the arrival there). The vessel is already in
// the harbour, so closures do not apply; without a price for the stay the transit is stored unpriced (the
// office prices it with a stay change) instead of refusing the vessel.
func (s *Service) UnexpectedTransit(ctx context.Context, p *auth.Principal, marinaID string, in TransitInput,
	then func(ctx context.Context, tx bun.IDB, c *store.Contract, allocs []store.Allocation) error,
) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return err
	}
	if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(in.FitOverride)); err != nil {
		return err
	}
	if err := s.requireAllocator(); err != nil {
		return err
	}
	st, err := store.ContractOccupancySettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return err
	}
	if err := in.check(1); err != nil {
		return err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, in.VesselID); err != nil {
		return err
	}
	pr := priceRequest{
		contractType: PricingType(KindTransit, ""), arrival: in.Arrival, departure: in.Departure, first: in.Placements[0],
		vesselID: in.VesselID, channel: in.Channel,
	}
	if in.NewVessel != nil {
		pr.vessel = &pricelist.SimVessel{LOA: geo.Cm(in.NewVessel.LOACm), Beam: geo.Cm(in.NewVessel.BeamCm), HullType: in.NewVessel.HullType}
	}
	price, err := s.price(ctx, p, marinaID, pr)
	if err != nil {
		if !errors.Is(err, pricing.ErrNoPrice) && !errors.Is(err, pricelist.ErrNoActiveList) {
			return err
		}
		price = nil
	}
	a := actor{p}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := s.createTransitTx(ctx, tx, scope, a, in, price, st, false, nil)
		if err != nil {
			return err
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		return then(ctx, tx, c, allocs)
	})
}
