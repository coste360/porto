package booking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Customer-area operations on transits (m8-plan §7.2), called by portal with the session's scope and customer.
// Any transit that is not the customer's is ErrLinkInvalid (INV-88).

// customerTransit loads a transit of the customer (ErrLinkInvalid otherwise).
func (s *Service) customerTransit(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID, contractID string) (*store.Contract, error) {
	if !store.ValidID(contractID) || customerID == "" {
		return nil, ErrLinkInvalid
	}
	c, err := store.GetContract(ctx, db, scope, contractID)
	if err != nil || c.Kind != contract.KindTransit || c.CustomerID != customerID {
		return nil, ErrLinkInvalid
	}
	return c, nil
}

// money of a transit: total, paid and residual from its live charges (before confirmation there is no charge).
func transitMoney(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Contract) (paid, residual money.Cents, charges []store.Charge, err error) {
	charges, err = store.ChargesOfContract(ctx, db, scope, c.ID)
	if err != nil {
		return 0, 0, nil, err
	}
	live := charges[:0:0]
	for i := range charges {
		ch := charges[i]
		if ch.PaymentStatus == store.ChargeCancelled {
			continue
		}
		live = append(live, ch)
		paid += money.Cents(ch.PaidAmountCents)
		residual += money.Cents(ch.ResidualCents())
	}
	if len(live) == 0 {
		residual = 0
	}
	return paid, residual, live, nil
}

func (s *Service) customerSummary(ctx context.Context, db bun.IDB, mc *marinaCtx, c *store.Contract, ob *store.OnlineBooking) (*CustomerBooking, error) {
	paid, residual, _, err := transitMoney(ctx, db, mc.scope, c)
	if err != nil {
		return nil, err
	}
	cb := &CustomerBooking{
		ContractID: c.ID, Code: contract.Code(c), Status: c.Status, Channel: c.Channel, Arrival: c.StartsOn, Departure: c.EndsOn,
		Total: money.Cents(c.TotalCents), Paid: paid, Residual: residual,
		Documents: []Document{}, MissingDocuments: []string{}, Receipts: []Receipt{},
	}
	if ob != nil {
		cb.Reference = ob.Reference
	}
	cb.ApprovalPending = c.Status == string(contract.StatusAwaitingApproval)
	if v, err := store.GetVessel(ctx, db, mc.scope, c.VesselID); err == nil {
		cb.VesselName = v.Name
	}
	_, al, err := holdExpiry(ctx, db, mc.scope, c.ID, s.now())
	if err != nil {
		return nil, err
	}
	if al != nil {
		if pc, err := store.ResourcePriceClass(ctx, db, mc.scope, al.ResourceType, al.ResourceID); err == nil {
			code := pc
			if code == "" {
				code = CategoryStandard
			}
			cat, err := s.loadCatalogue(ctx, db, mc.scope)
			if err == nil {
				for _, x := range cat.categories {
					if x.Code == code {
						n := x.Name
						cb.Category = &n
					}
				}
			}
		}
		revealed := c.Status == string(contract.StatusConfirmed) || c.Status == string(contract.StatusArrived)
		if op := snapshotPolicy(c); op != nil && op.RevealBerth == onlineconf.RevealNever {
			revealed = false
		}
		if revealed {
			if code, err := store.ResourceCode(ctx, db, mc.scope, al.ResourceType, al.ResourceID); err == nil {
				cb.BerthCode = code
			}
		}
	}
	return cb, nil
}

// CustomerBookings returns the customer's transits of the marina (any channel), newest first.
func (s *Service) CustomerBookings(ctx context.Context, scope store.MarinaScope, customerID string) ([]CustomerBooking, error) {
	db := s.d.DB.DB
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	rows, err := store.TransitsOfCustomer(ctx, db, scope, customerID, store.MaxLimit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, c := range rows {
		ids = append(ids, c.ID)
	}
	obs, err := store.OnlineBookingsByContracts(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := []CustomerBooking{}
	for i := range rows {
		c := &rows[i]
		var ob *store.OnlineBooking
		if x, ok := obs[c.ID]; ok {
			ob = &x
		}
		cb, err := s.customerSummary(ctx, db, mc, c, ob)
		if err != nil {
			return nil, err
		}
		out = append(out, *cb)
	}
	return out, nil
}

// CustomerBooking returns one transit of the customer with documents, receipts and the cancellation preview.
func (s *Service) CustomerBooking(ctx context.Context, scope store.MarinaScope, customerID, contractID string) (*CustomerBooking, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var ob *store.OnlineBooking
	if x, err := store.OnlineBookingByContract(ctx, db, scope, c.ID); err == nil {
		ob = x
	}
	cb, err := s.customerSummary(ctx, db, mc, c, ob)
	if err != nil {
		return nil, err
	}
	if c.Status == string(contract.StatusConfirmed) || c.Status == string(contract.StatusArrived) {
		cb.QRPayload = "PT1:" + c.ID
		info := mc.set.ArrivalInfo
		cb.ArrivalInfo = &info
	}
	if v, err := store.GetVessel(ctx, db, scope, c.VesselID); err == nil {
		cb.Vessel = &VesselView{Name: v.Name, LOACm: geo.Cm(v.LOACm), BeamCm: geo.Cm(v.BeamCm), DraftCm: geo.Cm(v.DraftCm), HullType: v.HullType}
	}
	if cb.Documents, cb.MissingDocuments, err = s.bookingDocuments(ctx, db, mc, c, ""); err != nil {
		return nil, err
	}
	if cb.Receipts, err = receiptsOf(ctx, db, scope, c); err != nil {
		return nil, err
	}
	prev, err := s.preview(ctx, db, mc, c)
	if err != nil {
		return nil, err
	}
	cb.Cancellation, cb.CanCancel = prev, prev.Allowed && IsOnlineChannel(c.Channel)
	today := mc.today(s.d.Clock.Now())
	cb.CanChangeDates = IsOnlineChannel(c.Channel) && c.Status == string(contract.StatusConfirmed) && today.Before(c.StartsOn)
	cb.CanPayBalance = cb.Residual > 0
	return cb, nil
}

// IsOnlineChannel reports whether a transit channel is the public page (customer-area changes apply to it only).
func IsOnlineChannel(ch string) bool { return ch == contract.ChannelOnline }

// receiptsOf returns the payments matched to the transit's charges.
func receiptsOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Contract) ([]Receipt, error) {
	charges, err := store.ChargesOfContract(ctx, db, scope, c.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(charges))
	for _, ch := range charges {
		ids = append(ids, ch.ID)
	}
	matches, err := store.MatchesOfCharges(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	var payIDs []string
	for _, m := range matches {
		if !slices.Contains(payIDs, m.PaymentID) {
			payIDs = append(payIDs, m.PaymentID)
		}
	}
	pays, err := store.PaymentsByIDs(ctx, db, scope, payIDs)
	if err != nil {
		return nil, err
	}
	out := []Receipt{}
	for _, p := range pays {
		out = append(out, Receipt{PaymentID: p.ID, ReceivedOn: p.ReceivedOn, Amount: money.Cents(p.AmountCents), Method: p.Method})
	}
	return out, nil
}

// paidOnline is the money paid for a transit: matched to its charges, or (before the charge exists) its succeeded
// intents.
func paidOnline(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Contract) (money.Cents, error) {
	paid, _, charges, err := transitMoney(ctx, db, scope, c)
	if err != nil || len(charges) > 0 {
		return paid, err
	}
	return 0, nil
}

// preview computes the cancellation refund from the contract's policy snapshot (never the current settings).
func (s *Service) preview(ctx context.Context, db bun.IDB, mc *marinaCtx, c *store.Contract) (*CancellationPreview, error) {
	out := &CancellationPreview{RefundAs: onlineconf.RefundAsRefund}
	if op := snapshotPolicy(c); op != nil && op.RefundAs != "" {
		out.RefundAs = op.RefundAs
	}
	switch contract.Status(c.Status) {
	case contract.StatusHeld, contract.StatusPendingPayment, contract.StatusAwaitingApproval, contract.StatusConfirmed:
	default:
		out.Reason = "status"
		return out, nil
	}
	paid, err := paidOnline(ctx, db, mc.scope, c)
	if err != nil {
		return nil, err
	}
	out.Paid = paid
	r := policy.Refund(policy.RefundInput{Tiers: snapshotTiers(c), Paid: paid, Arrival: c.StartsOn, Today: mc.today(s.d.Clock.Now())})
	if !r.Allowed {
		out.Reason = "arrival_passed"
		return out, nil
	}
	out.Allowed, out.Refund = true, r.Refund
	if r.Tier != nil {
		out.Tier = &onlineconf.Tier{MinDaysBefore: r.Tier.MinDaysBefore, RefundBP: r.Tier.RefundBP}
	}
	return out, nil
}

// AttachCustomerDocument uploads a document for one of the customer's transits.
func (s *Service) AttachCustomerDocument(ctx context.Context, scope store.MarinaScope, customerID, contractID string, up DocumentUpload) (*Document, error) {
	c, err := s.customerTransit(ctx, s.d.DB.DB, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	return s.attachDocument(ctx, scope, c, up)
}

// PreviewCancellation computes the refund from the contract's policy snapshot (never the current settings).
func (s *Service) PreviewCancellation(ctx context.Context, scope store.MarinaScope, customerID, contractID string) (*CancellationPreview, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return s.preview(ctx, db, mc, c)
}

// CancelByCustomer cancels the transit when the refund equals expectedRefund (ErrRefundChanged otherwise) and, in
// the same transaction, writes the refund (or the credit): either both are committed or neither, so a refund can
// never be lost after the cancellation. Only the provider call runs after commit (refund pending, retried by the
// payment refund path). HTTP replays are answered by the idempotency middleware; a replay on a cancelled transit
// returns the same result.
func (s *Service) CancelByCustomer(ctx context.Context, scope store.MarinaScope, customerID, contractID string, expectedRefund int64) (*CancelResult, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	if !IsOnlineChannel(c.Channel) {
		return nil, ErrCancellationNotAllowed
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	prev, err := s.preview(ctx, db, mc, c)
	if err != nil {
		return nil, err
	}
	if c.Status == string(contract.StatusCancelled) && c.CancelReason == "customer" {
		return &CancelResult{Status: c.Status, RefundAs: prev.RefundAs}, nil // replay
	}
	if !prev.Allowed {
		return nil, ErrCancellationNotAllowed
	}
	if int64(prev.Refund) != expectedRefund {
		return nil, ErrRefundChanged
	}
	who := contract.CustomerActor(customerID)
	var void string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if in := authorizedIntent(ctx, tx, scope, c.ID); in != nil {
			void = in.ID
		}
		cur, err := s.d.Contracts.CancelOnlineTransitTx(ctx, tx, scope, who, c.ID, 0, "customer")
		if err != nil {
			return err
		}
		if err := s.refundTransitTx(ctx, tx, scope, c, prev.Refund, prev.RefundAs == onlineconf.RefundAsCredit, "cancel:"+c.ID); err != nil {
			return fmt.Errorf("refunding cancelled transit: %w", err)
		}
		mc2, ob, err := s.bookingFor(ctx, tx, scope, cur.ID)
		if err != nil || ob == nil {
			return err
		}
		return s.queueBookingMail(ctx, tx, mc2, ob, cur, notify.TemplateBookingCancelled, "booking-cancelled:"+cur.ID, map[string]string{
			"refund": euro(int64(prev.Refund)),
		})
	})
	if err != nil {
		return nil, err
	}
	if void != "" {
		if err := s.d.Collect.VoidIntent(context.WithoutCancel(ctx), scope, void); err != nil {
			s.d.Log.WarnContext(ctx, "booking void failed", "marina_id", scope.ID(), "contract_id", c.ID, "err", err)
		}
	}
	return &CancelResult{Status: string(contract.StatusCancelled), Refund: prev.Refund, RefundAs: prev.RefundAs}, nil
}

// refundTransitTx gives back amount of the money matched to a transit's charges, newest payment first, in the
// caller's transaction: provider payments are refunded through the provider (the provider call is sent after
// commit; a failed send stays a pending refund for the payment refund path), others are refunded by bank transfer;
// with credit the matches are only reversed and the money stays as customer credit. Any error aborts the caller's
// transaction, so the state change that caused the refund is never committed without it.
func (s *Service) refundTransitTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, amount money.Cents, credit bool, key string) error {
	if amount <= 0 {
		return nil
	}
	if s.d.Payments == nil {
		return errors.New("booking: refund without payment service")
	}
	actor := audit.Actor{Type: audit.ActorSystem, ID: "customer:" + c.CustomerID}
	{
		charges, err := store.ChargesOfContract(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(charges))
		for _, ch := range charges {
			ids = append(ids, ch.ID)
		}
		matches, err := store.MatchesOfCharges(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		// Net matched amount per match (reversals have negative amounts and point to the match they reverse).
		net := map[string]int64{}
		order := []string{}
		pays := map[string]string{}
		for _, m := range matches {
			if m.ReversesMatchID != "" {
				net[m.ReversesMatchID] += m.AmountCents
				continue
			}
			net[m.ID] += m.AmountCents
			order = append(order, m.ID)
			pays[m.ID] = m.PaymentID
		}
		left := int64(amount)
		for i := len(order) - 1; i >= 0 && left > 0; i-- {
			id := order[i]
			n := min(net[id], left)
			if n <= 0 {
				continue
			}
			if credit {
				if _, err := s.d.Payments.ReverseMatchTx(ctx, tx, scope, actor, id, money.Cents(n)); err != nil {
					return err
				}
			} else {
				pay, err := store.GetPayment(ctx, tx, scope, pays[id])
				if err != nil {
					return err
				}
				in := payment.RefundInput{
					Amount: money.Cents(n), Reason: "online_cancellation", IdempotencyKey: key + ":" + id,
					Unmatch: []payment.UnmatchInput{{MatchID: id, Amount: money.Cents(n)}},
				}
				if pay.Source == store.PaymentSourceProvider && s.d.Collect.RefundSupported(ctx, scope, pay.ProviderCode) {
					in.ViaProvider = true
				} else {
					in.Method = store.PaymentMethodBankTransfer
				}
				r, err := s.d.Payments.RefundTx(ctx, tx, scope, actor, pay.ID, in)
				if err != nil {
					return err
				}
				if r.Status == store.RefundPending {
					rid := r.ID
					store.AfterCommit(ctx, func() { s.d.Collect.SendRefund(context.WithoutCancel(ctx), scope, rid) })
				}
			}
			left -= n
		}
		if left > 0 {
			return fmt.Errorf("refund of %d cents exceeds the matched money by %d", amount, left)
		}
		return nil
	}
}

// dateChange computes a date change: rules, availability (same resource preferred, else the same category) and
// the new price.
func (s *Service) dateChange(ctx context.Context, db bun.IDB, mc *marinaCtx, c *store.Contract, in DateChange) (*DateChangePreview, *contract.OnlinePrice, contract.Segment, error) {
	out := &DateChangePreview{OldTotal: money.Cents(c.TotalCents)}
	today := mc.today(s.d.Clock.Now())
	if !IsOnlineChannel(c.Channel) || c.Status != string(contract.StatusConfirmed) || !today.Before(c.StartsOn) {
		return nil, nil, contract.Segment{}, ErrCancellationNotAllowed
	}
	if reason, err := s.blockReason(ctx, mc, in.Arrival, in.Departure); err != nil {
		return nil, nil, contract.Segment{}, err
	} else if reason != "" {
		out.Reason = reason
		return out, nil, contract.Segment{}, nil
	}
	rows, err := store.AllocationsOfContracts(ctx, db, mc.scope, []string{c.ID})
	if err != nil {
		return nil, nil, contract.Segment{}, err
	}
	var cur *store.Allocation
	var own []string
	for i := range rows {
		if rows[i].Status == store.AllocActive {
			own = append(own, rows[i].ID)
			if cur == nil {
				a := rows[i].Allocation
				cur = &a
			}
		}
	}
	if cur == nil {
		return nil, nil, contract.Segment{}, ErrHoldExpired
	}
	vs, err := store.GetVessel(ctx, db, mc.scope, c.VesselID)
	if err != nil {
		return nil, nil, contract.Segment{}, err
	}
	v := Vessel{LOACm: geo.Cm(vs.LOACm), BeamCm: geo.Cm(vs.BeamCm), DraftCm: geo.Cm(vs.DraftCm), HullType: vs.HullType}
	var seg *contract.Segment
	if cur.ResourceType == store.ResourceBerth {
		owners, err := store.OccupancyOwners(ctx, db, mc.scope, cur.ResourceType, cur.ResourceID, in.Arrival, in.Departure.AddDays(-1), nil, own)
		if err != nil {
			return nil, nil, contract.Segment{}, err
		}
		now := s.now()
		free := true
		for _, o := range owners {
			if o.Kind == store.AllocKindHold && !o.ExpiresAt.IsZero() && o.ExpiresAt.Before(now) {
				continue
			}
			free = false
		}
		if free {
			seg = &contract.Segment{ResourceType: cur.ResourceType, ResourceID: cur.ResourceID, First: in.Arrival, Last: in.Departure.AddDays(-1)}
		}
	}
	if seg == nil {
		cat, err := s.loadCatalogue(ctx, db, mc.scope)
		if err != nil {
			return nil, nil, contract.Segment{}, err
		}
		pc, err := store.ResourcePriceClass(ctx, db, mc.scope, cur.ResourceType, cur.ResourceID)
		if err != nil {
			return nil, nil, contract.Segment{}, err
		}
		if pc == "" {
			pc = CategoryStandard
		}
		sols, err := s.search(ctx, mc.scope, occupancy.ChannelOnline, in.Arrival, in.Departure, v)
		if err != nil {
			return nil, nil, contract.Segment{}, err
		}
		if list := byCategory(cat, sols)[pc]; len(list) > 0 {
			g := placement(list[0])
			seg = &g
		}
	}
	if seg == nil {
		out.Reason = "unavailable"
		return out, nil, contract.Segment{}, nil
	}
	var services []contract.ServiceRequest
	promo := ""
	if snap := contract.DecodeSnapshot(c.PriceSnapshot); snap != nil {
		for _, sv := range snap.Request.Services {
			services = append(services, contract.ServiceRequest{ItemID: sv.ItemID, QtyMilli: int64(sv.Qty)})
		}
		promo = snap.Request.PromoCode
	}
	p, err := s.d.Contracts.PriceOnline(ctx, mc.scope, contract.OnlinePriceInput{
		Channel: c.Channel, Arrival: in.Arrival, Departure: in.Departure, Placement: *seg, VesselID: c.VesselID,
		CustomerID: c.CustomerID, Services: services, PromoCode: promo,
	})
	if err != nil {
		return nil, nil, contract.Segment{}, err
	}
	out.Available, out.NewTotal = true, money.Cents(p.Total())
	out.Delta = out.NewTotal - out.OldTotal
	if out.Delta > 0 {
		out.Supplement = out.Delta
	} else if out.Delta < 0 {
		paid, err := paidOnline(ctx, db, mc.scope, c)
		if err != nil {
			return nil, nil, contract.Segment{}, err
		}
		r := policy.Refund(policy.RefundInput{Tiers: snapshotTiers(c), Paid: min(-out.Delta, paid), Arrival: c.StartsOn, Today: today})
		out.Refund = r.Refund
	}
	return out, p, *seg, nil
}

// PreviewDateChange computes availability and price effect of new dates (writes nothing).
func (s *Service) PreviewDateChange(ctx context.Context, scope store.MarinaScope, customerID, contractID string, in DateChange) (*DateChangePreview, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	prev, _, _, err := s.dateChange(ctx, db, mc, c, in)
	return prev, err
}

// ChangeDates changes the dates of a confirmed transit before arrival (ErrPriceChanged on a different total).
func (s *Service) ChangeDates(ctx context.Context, scope store.MarinaScope, customerID, contractID string, in DateChange) (*DateChangeResult, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	if in.ExpectedTotalCents == nil {
		return nil, &RulesError{Field: "expected_total_cents", Code: "missing"}
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if c.StartsOn.Equal(in.Arrival) && c.EndsOn.Equal(in.Departure) {
		return &DateChangeResult{Status: c.Status, Arrival: c.StartsOn, Departure: c.EndsOn, NewTotal: money.Cents(c.TotalCents)}, nil
	}
	prev, price, seg, err := s.dateChange(ctx, db, mc, c, in)
	if err != nil {
		return nil, err
	}
	if !prev.Available {
		if prev.Reason != "" && prev.Reason != "unavailable" {
			return nil, &RulesError{Field: "arrival", Code: prev.Reason}
		}
		return nil, &UnavailableError{AvailableCategories: []string{}}
	}
	if prev.NewTotal != *in.ExpectedTotalCents {
		return nil, ErrPriceChanged
	}
	who := contract.CustomerActor(customerID)
	status := c.Status
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, _, err := s.d.Contracts.ChangeOnlineStayTx(ctx, tx, scope, who, c.ID, 0, contract.OnlineStayChange{
			Arrival: in.Arrival, Departure: in.Departure, Placement: seg, ExpectedTotalCents: int64(prev.NewTotal), Price: price,
		})
		if cur != nil {
			status = cur.Status
		}
		if err != nil {
			return err
		}
		// The refund of a shorter or cheaper stay is written with the change (all or nothing).
		return s.refundTransitTx(ctx, tx, scope, c, prev.Refund,
			snapshotPolicy(c) != nil && snapshotPolicy(c).RefundAs == onlineconf.RefundAsCredit,
			// The contract version makes the key unique per change: going back to dates the stay had before
			// (A→B→C→B) is a new refund, never the replay of the first one.
			"dates:"+c.ID+":v"+strconv.Itoa(c.Version)+":"+in.Arrival.String()+":"+in.Departure.String())
	})
	switch {
	case errors.Is(err, contract.ErrOnlinePriceChanged):
		return nil, ErrPriceChanged
	case errors.Is(err, occupancy.ErrResourceTaken), errors.Is(err, occupancy.ErrQuotaExhausted):
		return nil, &UnavailableError{AvailableCategories: []string{}}
	case err != nil:
		return nil, err
	}
	return &DateChangeResult{
		Status: status, Arrival: in.Arrival, Departure: in.Departure, NewTotal: prev.NewTotal,
		Delta: prev.Delta, Refund: prev.Refund, Supplement: prev.Supplement,
	}, nil
}

// PayBalance creates a payment request for the transit's open charges and returns its pay page URL.
func (s *Service) PayBalance(ctx context.Context, scope store.MarinaScope, customerID, contractID string) (*PayBalanceResult, error) {
	db := s.d.DB.DB
	c, err := s.customerTransit(ctx, db, scope, customerID, contractID)
	if err != nil {
		return nil, err
	}
	_, residual, charges, err := transitMoney(ctx, db, scope, c)
	if err != nil {
		return nil, err
	}
	if residual <= 0 {
		return nil, ErrNothingDue
	}
	ids := make([]string, 0, len(charges))
	for _, ch := range charges {
		if ch.ResidualCents() > 0 {
			ids = append(ids, ch.ID)
		}
	}
	url, amount, err := s.d.Collect.BalanceLink(ctx, scope, audit.Actor{Type: audit.ActorSystem, ID: "customer:" + customerID}, customerID, ids)
	if errors.Is(err, collect.ErrChargesNotOpen) {
		return nil, ErrNothingDue
	}
	if err != nil {
		return nil, err
	}
	return &PayBalanceResult{CheckoutURL: url, Amount: money.Cents(amount)}, nil
}
