package contract

import (
	"context"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Transit confirmation (INV-09): confirmTx is the only code that sets a transit to confirmed. In M3 it is reached
// through the manual driver (ConfirmTransit with source manual, and the confirm option of createTransit and
// acceptQuote); the M5/M8 payment webhook calls it with source provider (also from awaiting_approval and, for an
// online transit paid late, from expired with confirm_late); a verified paid channel reservation with source
// channel (M8, ADR 0013).

// ConfirmTransit confirms a held transit with the manual driver (transits.desk, If-Match). The hold becomes a
// transit allocation; a lost hold is allocated again or the confirmation fails with 409 resource_taken.
func (s *Service) ConfirmTransit(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, mc ManualConfirmation) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	ref, ok := cleanText(mc.Reference, 200)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("reference", "too_long")
		return nil, v
	}
	mc.Reference = ref
	a := actor{p: p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindTransit, ifMatch)
		if err != nil {
			return err
		}
		if err := s.confirmTx(ctx, tx, scope, a, c, SourceManual, mc); err != nil {
			return err
		}
		return s.commitHook()
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// confirmTx moves a held, pending_payment or awaiting_approval transit (an expired online one with a late provider
// payment) to confirmed in tx: holds converted to transit allocations (or allocated again
// when lost), confirmation fields, audit and transit.confirmed. When the vessel was already recorded in port
// on this transit (held + arrival, m3-plan §13 D13) the transit continues to arrived in the same transaction.
func (s *Service) confirmTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, source string, mc ManualConfirmation) error {
	action := ActionConfirm
	if c.Status == string(StatusExpired) {
		// M8 late payment (m8-plan §5.5): only the provider webhook confirms an expired online transit.
		if source != SourceProvider || c.Channel != ChannelOnline {
			return &TransitionError{From: c.Status, Action: ActionConfirmLate}
		}
		action = ActionConfirmLate
	}
	t, err := Next(TransitTransitions, Status(c.Status), action)
	if err != nil {
		return err
	}
	if c.CustomerID == "" {
		return ErrCustomerRequired
	}
	switch source {
	case SourceManual, SourceProvider:
	case SourceChannel:
		if c.Channel != ChannelExternal {
			return errors.New("contract: channel confirmation of a non-channel transit")
		}
	default:
		return errors.New("contract: unknown confirmation source")
	}
	if action == ActionConfirmLate {
		if err := s.reclaimQuotaTx(ctx, tx, scope, c); err != nil {
			return err
		}
	}
	now := s.now()
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	reallocated, holds, err := s.convertOrReallocate(ctx, tx, scope, a, now, c, allocs, occupancy.KindTransit)
	if err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	c.ConfirmedAt, c.ConfirmedByUserID, c.ConfirmationSource = now, a.userID(), source
	c.ConfirmationRef, c.PayAtDeparture = mc.Reference, mc.PayAtDeparture
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "transit.confirm", entityTransit, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "confirmation_source": source, "pay_at_departure": mc.PayAtDeparture,
		"converted": holds, "reallocated": idsOf(reallocated),
	}); err != nil {
		return err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
		return err
	}
	if err := s.emitAllocated(ctx, tx, scope, reallocated, c.ID); err != nil {
		return err
	}
	if err := s.billing().OnTransitConfirmedTx(ctx, tx, scope, a.audit(), c); err != nil {
		return err
	}
	if IsOnline(c) {
		// M8 (m8-plan §5.4): the inquiry of the booking becomes booked and the booking service queues the
		// confirmation email, in this transaction.
		if err := s.onlineInquiryTx(ctx, tx, scope, c, store.InquiryBooked); err != nil {
			return err
		}
		if h := s.hooks(); h != nil {
			if err := h.OnOnlineConfirmedTx(ctx, tx, scope, c); err != nil {
				return err
			}
		}
	}
	// Held + already arrived: chain confirmed → arrived.
	vp, err := store.GetVesselPresence(ctx, tx, scope, c.VesselID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if vp != nil && vp.State == stateInPort && slices.Contains(idsOf(allocs), vp.AllocationID) {
		return s.arriveTx(ctx, tx, scope, a, c)
	}
	return nil
}
