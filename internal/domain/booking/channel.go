package booking

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// ChannelHoldHours is the hold of an unpaid channel reservation left to the desk (m8-plan §8.2, §16 D14).
const ChannelHoldHours = 24

// ChannelPlan is the read-only part of a channel booking, computed before the write transaction (search and
// price read in their own transactions): a solution with its price, or the reason of a rejection.
type ChannelPlan struct {
	RejectReason string
	solution     occupancy.Solution
	price        *contract.OnlinePrice
}

// PlanChannelBooking checks the rules, searches the mapped category with channel external (quotas apply) and
// prices the best solution, outside any transaction (m8-plan §8.2).
func (s *Service) PlanChannelBooking(ctx context.Context, scope store.MarinaScope, in ChannelBookingInput) (*ChannelPlan, error) {
	db := s.d.DB.DB
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if err := checkVessel(in.Vessel); err != nil || in.Arrival.IsZero() || !in.Departure.After(in.Arrival) {
		return &ChannelPlan{RejectReason: store.ChannelRejectInvalid}, nil
	}
	if reason, err := s.blockReason(ctx, mc, in.Arrival, in.Departure); err != nil {
		return nil, err
	} else if reason != "" {
		return &ChannelPlan{RejectReason: store.ChannelRejectRules}, nil
	}
	cat, err := s.loadCatalogue(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if !contains(cat.categories, in.Category) {
		return &ChannelPlan{RejectReason: store.ChannelRejectUnmappedCategory}, nil
	}
	sols, err := s.search(ctx, scope, occupancy.ChannelExternal, in.Arrival, in.Departure, in.Vessel)
	if err != nil {
		return nil, err
	}
	list := byCategory(cat, sols)[in.Category]
	if len(list) == 0 {
		return &ChannelPlan{RejectReason: store.ChannelRejectNoSpace}, nil
	}
	price, err := s.d.Contracts.PriceOnline(ctx, scope, contract.OnlinePriceInput{
		Channel: contract.ChannelExternal, Arrival: in.Arrival, Departure: in.Departure, Placement: placement(list[0]),
		Vessel: ptr(newVessel(in.Vessel, "")),
	})
	if err != nil {
		return nil, err
	}
	return &ChannelPlan{solution: list[0], price: price}, nil
}

// CreateChannelBookingTx books an inbound channel reservation in tx (m8-plan §8.2): rules, search with channel
// external restricted to the mapped category (quotas apply), contract.CreateOnlineTransitTx, customer and vessel
// from the guest, confirmTx with source channel when paid to the channel, else held 24 h. No space is a rejected
// result with an inquiry declined_no_space, not an error. in.Plan (PlanChannelBooking, before the transaction) is
// required on SQLite, whose write transaction must not wait on other connections. A solution lost to a concurrent booking is
// occupancy.ErrResourceTaken (or ErrQuotaExhausted): the caller rolls the transaction back and calls again (the
// next search no longer proposes the taken resource).
func (s *Service) CreateChannelBookingTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in ChannelBookingInput) (*ChannelBookingResult, error) {
	plan := in.Plan
	if plan == nil {
		var err error
		if plan, err = s.PlanChannelBooking(ctx, scope, in); err != nil {
			return nil, err
		}
	}
	mc, err := s.scoped(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if plan.RejectReason != "" {
		if plan.RejectReason != store.ChannelRejectNoSpace {
			return &ChannelBookingResult{RejectReason: plan.RejectReason}, nil
		}
		inq := &store.Inquiry{
			MarinaID: scope.ID(), Channel: store.OnlineChannelExternal, Source: "channel", LOACm: int64(in.Vessel.LOACm),
			BeamCm: int64(in.Vessel.BeamCm), DraftCm: int64(in.Vessel.DraftCm), Arrival: in.Arrival, Departure: in.Departure,
			Outcome: store.InquiryDeclinedNoSpace,
		}
		if err := store.InsertAnonymousInquiry(ctx, tx, inq, now); err != nil {
			return nil, err
		}
		return &ChannelBookingResult{RejectReason: store.ChannelRejectNoSpace, InquiryID: inq.ID}, nil
	}
	sol, price := plan.solution, plan.price
	id := store.NewID()
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	ref, err := newReference(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	locale := in.Guest.Locale
	if locale != "en" {
		locale = "it"
	}
	who := contract.ChannelActor(in.ChannelCode)
	ot, err := s.d.Contracts.CreateOnlineTransitTx(ctx, tx, scope, who, contract.OnlineTransitInput{
		Channel: contract.ChannelExternal, Vessel: newVessel(in.Vessel, ""), Arrival: in.Arrival, Departure: in.Departure,
		Placement: placement(sol), HoldUntil: now.Add(ChannelHoldHours * time.Hour), Policy: onlinePolicy(mc),
		Price: price, Booking: contract.OnlineBookingRow{ID: id, Reference: ref, TokenNonce: nonce, Locale: locale},
	})
	if err != nil {
		return nil, err
	}
	guest := in.Guest
	guest.FirstName, _ = cleanName(guest.FirstName, 100)
	guest.LastName, _ = cleanName(guest.LastName, 100)
	if guest.LastName == "" {
		guest.LastName = "—"
	}
	if e, ok := customer.NormalizeEmail(guest.Email); ok {
		guest.Email = e
	} else {
		guest.Email = ""
	}
	if p, ok := customer.NormalizePhone(guest.Phone); ok {
		guest.Phone = p
	} else {
		guest.Phone = ""
	}
	guest.Country, guest.Locale = strings.ToUpper(strings.TrimSpace(guest.Country)), locale
	if len(guest.Country) != 2 {
		guest.Country = ""
	}
	customerID, err := s.channelCustomerTx(ctx, tx, scope, who, guest)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(guest.LastName)
	if _, err := s.d.Contracts.SetOnlineCustomerTx(ctx, tx, scope, who, ot.Contract.ID, contract.OnlineCustomer{
		CustomerID: customerID, VesselName: name, HullType: in.Vessel.HullType, DraftCm: int64(in.Vessel.DraftCm),
	}); err != nil {
		return nil, err
	}
	contact, err := json.Marshal(Contact{
		FirstName: guest.FirstName, LastName: guest.LastName, Email: guest.Email, Phone: guest.Phone, Country: guest.Country, Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	ob := ot.Booking
	if ob.ContactEnc, err = s.d.Keys.EncryptString(string(contact), contactAAD(ob.ID)); err != nil {
		return nil, err
	}
	if err := store.UpdateOnlineBooking(ctx, tx, scope, ob, now); err != nil {
		return nil, err
	}
	if in.PaidToChannel {
		// ADR 0013: a verified reservation paid to the channel (merchant of record) is the payment proof.
		if _, err := s.d.Contracts.ConfirmChannelPaidTx(ctx, tx, scope, who, ot.Contract.ID); err != nil {
			return nil, err
		}
	}
	return &ChannelBookingResult{Booked: true, ContractID: ot.Contract.ID, Reference: ref}, nil
}

func contains(cats []Category, code string) bool {
	for _, c := range cats {
		if c.Code == code {
			return true
		}
	}
	return false
}

// channelCustomerTx resolves the guest of a channel reservation: the only live customer with the same email, else
// a new customer (guests without a valid email always get a new customer).
func (s *Service) channelCustomerTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who contract.Who, g CustomerDetails) (string, error) {
	if g.Email != "" {
		return s.resolveCustomerTx(ctx, tx, scope, who.Actor, g)
	}
	now := s.now()
	row := &store.Customer{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: customer.KindPerson, FirstName: g.FirstName, LastName: g.LastName,
		Nationality: g.Country, Locale: g.Locale, Emails: store.JSON("[]"), Phones: store.JSON("[]"),
		BillingAddress: store.JSON("{}"), PaymentTerms: store.JSON("{}"), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if g.Phone != "" {
		row.Phones = store.MustJSON([]customer.Contact{{Value: g.Phone}})
	}
	row.SearchText = customer.SearchText(row)
	if err := store.InsertCustomer(ctx, tx, row); err != nil {
		return "", err
	}
	return row.ID, s.customerCreatedTx(ctx, tx, scope, who, row.ID)
}

// CancelChannelBookingTx cancels the transit of a channel reservation in tx (channel cancel event, actor
// channel:<code>). A transit already ended or arrived is left as it is.
func (s *Service) CancelChannelBookingTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who contract.Who, contractID string) (*store.Contract, error) {
	c, err := store.LockContract(ctx, tx, scope, contractID)
	if err != nil {
		return nil, err
	}
	switch contract.Status(c.Status) {
	case contract.StatusHeld, contract.StatusPendingPayment, contract.StatusAwaitingApproval, contract.StatusConfirmed:
	default:
		return c, nil
	}
	return s.d.Contracts.CancelOnlineTransitTx(ctx, tx, scope, who, contractID, 0, "channel_cancel")
}

// ChannelNight is the availability of one category on one night for a channel push.
type ChannelNight struct {
	Night calendar.Date
	Units int
	Cents money.Cents // price of the night for the reference vessel (withRates only)
}

// ChannelNights computes, for the next days nights from today, the free online solutions per category for the
// reference vessel (channel external: online_sellable resources, quotas applied), and with withRates the price of
// one night on the best solution (m8-plan §8.2).
func (s *Service) ChannelNights(ctx context.Context, scope store.MarinaScope, days int, v Vessel, withRates bool) (map[string][]ChannelNight, error) {
	db := s.d.DB.DB
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	cat, err := s.loadCatalogue(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := map[string][]ChannelNight{}
	today := mc.today(s.d.Clock.Now())
	for i := range days {
		d := today.AddDays(i)
		sols, err := s.search(ctx, scope, occupancy.ChannelExternal, d, d.AddDays(1), v)
		if err != nil {
			return nil, err
		}
		grouped := byCategory(cat, sols)
		for _, c := range cat.categories {
			list := grouped[c.Code]
			seen := map[string]bool{}
			for _, sol := range list {
				seen[sol.Segments[0].ResourceID] = true
			}
			n := ChannelNight{Night: d, Units: len(seen)}
			if withRates && len(list) > 0 {
				if p, err := s.d.Contracts.PriceOnline(ctx, scope, contract.OnlinePriceInput{
					Channel: contract.ChannelExternal, Arrival: d, Departure: d.AddDays(1), Placement: placement(list[0]),
					Vessel: ptr(newVessel(v, "")),
				}); err == nil {
					n.Cents = money.Cents(p.Total())
				}
			}
			out[c.Code] = append(out[c.Code], n)
		}
	}
	return out, nil
}
