package booking

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// maxAttempts bounds the solutions tried by one creation (m8-plan §4.3).
const maxAttempts = 3

// onlinePolicy is the online policy snapshot of a new booking.
func onlinePolicy(mc *marinaCtx) store.JSON {
	op := contract.OnlinePolicy{
		PaymentMode: mc.set.PaymentMode, DepositBP: mc.set.DepositBP, RefundAs: mc.set.RefundAs, RevealBerth: mc.set.RevealBerth,
		CancellationTiers: []contract.OnlineTier{},
	}
	for _, t := range tiersOf(mc.set) {
		op.CancellationTiers = append(op.CancellationTiers, contract.OnlineTier{MinDaysBefore: t.MinDaysBefore, RefundBP: t.RefundBP})
	}
	return store.MustJSON(op)
}

// snapshotPolicy returns the online policy snapshot of a contract (nil when it has none).
func snapshotPolicy(c *store.Contract) *contract.OnlinePolicy {
	var p struct {
		Online *contract.OnlinePolicy `json:"online"`
	}
	if json.Unmarshal(c.Policy, &p) != nil {
		return nil
	}
	return p.Online
}

func snapshotTiers(c *store.Contract) []policy.Tier {
	op := snapshotPolicy(c)
	if op == nil {
		return nil
	}
	out := make([]policy.Tier, 0, len(op.CancellationTiers))
	for _, t := range op.CancellationTiers {
		out = append(out, policy.Tier{MinDaysBefore: t.MinDaysBefore, RefundBP: t.RefundBP})
	}
	return out
}

// dueNow is the advance payment of a contract per its policy snapshot.
func dueNow(c *store.Contract) money.Cents {
	op := snapshotPolicy(c)
	mode, bp := "full", 0
	if op != nil {
		mode, bp = op.PaymentMode, op.DepositBP
	}
	return policy.Deposit(policy.DepositInput{Mode: mode, BP: bp, Total: money.Cents(c.TotalCents), PriceListDeposit: money.Cents(c.DepositCents)})
}

// Create holds a berth of the category (m8-plan §4.3): domain idempotency on IdempotencyKey, rules, live-hold
// limit, search, then up to 3 attempts of contract.CreateOnlineTransitTx on the next solution.
func (s *Service) Create(ctx context.Context, slug string, in CreateInput) (*Created, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if len(key) < 8 || len(key) > 128 {
		return nil, &RulesError{Field: "idempotency_key", Code: "invalid"}
	}
	idem := sha256Hex(mc.scope.ID() + "|" + key)
	if replay, err := s.replay(ctx, mc, idem, in.BodySHA256); replay != nil || err != nil {
		return replay, err
	}
	v := in.Vessel
	if in.VesselID != "" {
		if v, _, err = s.sessionVessel(ctx, mc, slug, in.VesselID, in.SessionToken); err != nil {
			return nil, err
		}
	}
	if err := checkVessel(v); err != nil {
		return nil, err
	}
	if reason, err := s.blockReason(ctx, mc, in.Arrival, in.Departure); err != nil {
		return nil, err
	} else if reason != "" {
		return nil, &RulesError{Field: "arrival", Code: reason}
	}
	if s.d.BotProtection != nil {
		if ok, err := s.d.BotProtection.Verify(ctx, in.BotToken, in.RemoteIP); err != nil || !ok {
			return nil, &RulesError{Field: "bot_token", Code: "invalid"}
		}
	}
	db := s.d.DB.DB
	if !mc.set.AllowWithoutProvider {
		if _, err := s.d.Collect.OnlineCheckoutProvider(ctx, db, mc.scope); err != nil {
			return nil, ErrProviderDisabled
		}
	}
	if n, err := store.CountLiveOnlineHolds(ctx, db, mc.scope); err != nil {
		return nil, err
	} else if n >= mc.set.MaxLiveHolds {
		return nil, ErrBusy
	}
	services, err := s.checkServices(ctx, mc.scope, in.Services)
	if err != nil {
		return nil, err
	}
	locale := in.Locale
	if locale != "en" {
		locale = "it"
	}
	cat, err := s.loadCatalogue(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	sols, err := s.search(ctx, mc.scope, occupancy.ChannelOnline, in.Arrival, in.Departure, v)
	if err != nil {
		return nil, err
	}
	grouped := byCategory(cat, sols)
	list := grouped[in.Category]
	if len(list) == 0 {
		return nil, unavailableErr(cat, grouped)
	}
	groups, err := store.GroupsOfResources(ctx, db, mc.scope, "")
	if err != nil {
		return nil, err
	}
	origin := ""
	if o := strings.TrimSpace(in.EmbedParentOrigin); o != "" && slices.Contains([]string(mc.m.AllowedEmbedOrigins), o) {
		origin = o
	}
	attempts := 0
	for _, sol := range list {
		if attempts == maxAttempts {
			break
		}
		attempts++
		q, price, err := s.quoteOf(ctx, mc, contract.ChannelOnline, in.Arrival, in.Departure, v, sol, in.Category, services, in.PromoCode)
		if err != nil {
			return nil, err
		}
		ap := policy.Approval(approvalRules(mc.set), policy.ApprovalInput{LOACm: v.LOACm, GroupIDs: groups[sol.Segments[0].ResourceID]})
		q.ApprovalRequired = ap.Required
		out, err := s.createAttempt(ctx, mc, in, v, sol, price, services, idem, locale, origin, attempts)
		switch {
		case err == nil:
			out.Quote, out.ApprovalRequired = *q, ap.Required
			return out, nil
		case errors.Is(err, occupancy.ErrResourceTaken), errors.Is(err, occupancy.ErrQuotaExhausted), errors.Is(err, occupancy.ErrClosed):
			continue
		case store.IsUniqueViolation(err):
			// A concurrent request with the same Idempotency-Key won: answer as its replay.
			if replay, rerr := s.replay(ctx, mc, idem, in.BodySHA256); replay != nil || rerr != nil {
				return replay, rerr
			}
			continue
		default:
			return nil, err
		}
	}
	// Every tried solution was taken: answer with what is still available (another read).
	sols, err = s.search(ctx, mc.scope, occupancy.ChannelOnline, in.Arrival, in.Departure, v)
	if err != nil {
		return nil, err
	}
	return nil, unavailableErr(cat, byCategory(cat, sols))
}

// createAttempt is one transaction of Create on one solution.
func (s *Service) createAttempt(ctx context.Context, mc *marinaCtx, in CreateInput, v Vessel, sol occupancy.Solution,
	price *contract.OnlinePrice, services []ServiceRequest, idem, locale, origin string, attempt int,
) (*Created, error) {
	id := store.NewID()
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	now := s.now()
	holdUntil := now.Add(s.d.HoldTTL)
	var out *Created
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ref, err := newReference(ctx, tx, mc.scope)
		if err != nil {
			return err
		}
		inq := &store.Inquiry{
			MarinaID: mc.scope.ID(), Channel: store.OnlineChannelOnline, Source: "public", LOACm: int64(v.LOACm),
			BeamCm: int64(v.BeamCm), DraftCm: int64(v.DraftCm), Arrival: in.Arrival, Departure: in.Departure, Outcome: store.InquiryOpen,
		}
		if err := store.InsertAnonymousInquiry(ctx, tx, inq, now); err != nil {
			return err
		}
		ot, err := s.d.Contracts.CreateOnlineTransitTx(ctx, tx, mc.scope, contract.PublicActor(), contract.OnlineTransitInput{
			Channel: contract.ChannelOnline, Vessel: newVessel(v, ""), Arrival: in.Arrival, Departure: in.Departure,
			Placement: placement(sol), Services: contractServices(services), PromoCode: in.PromoCode, HoldUntil: holdUntil,
			Policy: onlinePolicy(mc), Price: price,
			Booking: contract.OnlineBookingRow{
				ID: id, Reference: ref, TokenNonce: nonce, IdempotencyHash: idem, BodySHA256: in.BodySHA256, Locale: locale,
				EmbedParentOrigin: origin, InquiryID: inq.ID,
			},
		})
		if err != nil {
			return err
		}
		inq.ContractID = ot.Contract.ID
		inq.UpdatedAt = now
		if err := store.UpdateInquiry(ctx, tx, mc.scope, inq.Version, inq); err != nil {
			return err
		}
		if s.beforeCommit != nil {
			if err := s.beforeCommit(attempt); err != nil {
				return err
			}
		}
		tok, err := s.bookingToken(id, nonce)
		if err != nil {
			return err
		}
		out = &Created{
			BookingID: id, Reference: ref, BookingToken: tok, Status: ot.Contract.Status, HoldExpiresAt: holdUntil,
			AmountDueNow: dueNow(ot.Contract),
		}
		return nil
	})
	return out, err
}

// replay returns the booking created earlier with the same idempotency hash (nil when none); another body is
// ErrIdempotencyMismatch.
func (s *Service) replay(ctx context.Context, mc *marinaCtx, idem, body string) (*Created, error) {
	db := s.d.DB.DB
	ob, err := store.OnlineBookingByIdempotency(ctx, db, mc.scope, idem)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ob.BodySHA256 != body {
		return nil, ErrIdempotencyMismatch
	}
	c, err := store.GetContract(ctx, db, mc.scope, ob.ContractID)
	if err != nil {
		return nil, err
	}
	b, err := s.view(ctx, db, mc, ob, c)
	if err != nil {
		return nil, err
	}
	tok, err := s.bookingToken(ob.ID, ob.TokenNonce)
	if err != nil {
		return nil, err
	}
	return &Created{
		BookingID: ob.ID, Reference: ob.Reference, BookingToken: tok, Status: c.Status, HoldExpiresAt: b.HoldExpiresAt,
		Quote: b.Quote, ApprovalRequired: b.ApprovalRequired, AmountDueNow: b.AmountDueNow,
	}, nil
}

// loadBooking resolves a booking of the marina for its token holder: an unknown id, a foreign or wrong token or a
// token past its validity are all ErrLinkInvalid (INV-88).
func (s *Service) loadBooking(ctx context.Context, db bun.IDB, mc *marinaCtx, id string, t Tokens, allowStatus bool) (*store.OnlineBooking, *store.Contract, error) {
	ob, err := store.GetOnlineBooking(ctx, db, mc.scope, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrLinkInvalid
		}
		return nil, nil, err
	}
	ok := s.checkBookingToken(ob, t.Booking)
	if !ok && allowStatus && t.Status != "" {
		ok = s.checkStatusToken(ob.ID, t.Status, s.d.Clock.Now())
	}
	if !ok || ob.Channel != store.OnlineChannelOnline {
		return nil, nil, ErrLinkInvalid
	}
	c, err := store.GetContract(ctx, db, mc.scope, ob.ContractID)
	if err != nil {
		return nil, nil, ErrLinkInvalid
	}
	if !s.d.Clock.Now().Before(tokenValidUntil(c.EndsOn, mc.loc)) {
		return nil, nil, ErrLinkInvalid
	}
	return ob, c, nil
}

// holdExpiry returns the latest expiry of the live holds of a contract (zero when none).
func holdExpiry(ctx context.Context, db bun.IDB, scope store.MarinaScope, contractID string, now time.Time) (time.Time, *store.Allocation, error) {
	rows, err := store.AllocationsOfContracts(ctx, db, scope, []string{contractID})
	if err != nil {
		return time.Time{}, nil, err
	}
	var exp time.Time
	var first *store.Allocation
	for i := range rows {
		a := rows[i].Allocation
		if a.Status != store.AllocActive {
			continue
		}
		if first == nil {
			al := a
			first = &al
		}
		if a.Kind == store.AllocKindHold && a.ExpiresAt.After(now) && a.ExpiresAt.After(exp) {
			exp = a.ExpiresAt
		}
	}
	return exp, first, nil
}

// view builds the token holder's view of a booking.
func (s *Service) view(ctx context.Context, db bun.IDB, mc *marinaCtx, ob *store.OnlineBooking, c *store.Contract) (*Booking, error) {
	now := s.now()
	exp, al, err := holdExpiry(ctx, db, mc.scope, c.ID, now)
	if err != nil {
		return nil, err
	}
	b := &Booking{
		BookingID: ob.ID, Reference: ob.Reference, Status: c.Status, Arrival: c.StartsOn, Departure: c.EndsOn,
		HoldExpiresAt: exp, ApprovalDeadline: ob.ApprovalDeadline, AmountDueNow: dueNow(c), DetailsComplete: c.CustomerID != "",
		Documents: []Document{}, MissingDocuments: []string{}, Category: CategoryStandard,
	}
	if snap := contract.DecodeSnapshot(c.PriceSnapshot); snap != nil {
		bd := snap.Breakdown
		b.Quote = Quote{
			Nights: bd.Nights, TotalCents: bd.Total, NetCents: bd.Net, VATCents: bd.VAT, DueNowCents: b.AmountDueNow,
			Currency: mc.m.Currency, Lines: []PriceLine{}, PromoApplied: snap.Request.PromoCode != "" && bd.DiscountTotal != 0,
		}
		for _, l := range bd.Lines {
			kind := "service"
			if l.Kind == "berth" {
				kind = "berth"
			}
			b.Quote.Lines = append(b.Quote.Lines, PriceLine{Kind: kind, ItemID: l.ItemID, QtyMilli: int64(l.Qty), TotalCents: l.Gross})
		}
		if snap.Resolved.Category != "" {
			b.Category = snap.Resolved.Category
		}
	}
	if op := snapshotPolicy(c); op != nil {
		b.Quote.PaymentMode = op.PaymentMode
		for _, t := range op.CancellationTiers {
			b.Quote.CancellationTiers = append(b.Quote.CancellationTiers, onlineTier(t))
		}
	}
	b.Quote.Category = b.Category
	if al != nil {
		if pc, err := store.ResourcePriceClass(ctx, db, mc.scope, al.ResourceType, al.ResourceID); err == nil && pc != "" {
			b.Category, b.Quote.Category = pc, pc
		}
	}
	b.ApprovalRequired = ob.ApprovalReason != "" || c.Status == "awaiting_approval"
	if vs, err := store.GetVessel(ctx, db, mc.scope, c.VesselID); err == nil {
		b.Vessel = VesselView{Name: vs.Name, LOACm: geo.Cm(vs.LOACm), BeamCm: geo.Cm(vs.BeamCm), DraftCm: geo.Cm(vs.DraftCm), HullType: vs.HullType}
		if b.Vessel.Name == "—" {
			b.Vessel.Name = ""
		}
	}
	docs, missing, err := s.bookingDocuments(ctx, db, mc, c, ob.ID)
	if err != nil {
		return nil, err
	}
	b.Documents, b.MissingDocuments = docs, missing
	return b, nil
}

// Get returns the booking of the token holder.
func (s *Service) Get(ctx context.Context, slug, bookingID string, t Tokens) (*Booking, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ob, c, err := s.loadBooking(ctx, db, mc, bookingID, t, false)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, db, mc, ob, c)
}

// Abandon cancels a held booking (actor public, reason abandoned).
func (s *Service) Abandon(ctx context.Context, slug, bookingID string, t Tokens) error {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return err
	}
	_, c, err := s.loadBooking(ctx, s.d.DB.DB, mc, bookingID, t, false)
	if err != nil {
		return err
	}
	if c.Status != string(contract.StatusHeld) {
		return &contract.TransitionError{From: c.Status, Action: contract.ActionCancel}
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.LockContract(ctx, tx, mc.scope, c.ID)
		if err != nil {
			return err
		}
		if c.Status != string(contract.StatusHeld) {
			return &contract.TransitionError{From: c.Status, Action: contract.ActionCancel}
		}
		_, err = s.d.Contracts.CancelOnlineTransitTx(ctx, tx, mc.scope, contract.PublicActor(), c.ID, 0, "abandoned")
		return err
	})
}

// Status returns the PII-free status for the return page (booking token or status token).
func (s *Service) Status(ctx context.Context, slug, bookingID string, t Tokens) (*StatusView, error) {
	mc, err := s.resolve(ctx, slug, true)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ob, c, err := s.loadBooking(ctx, db, mc, bookingID, t, true)
	if err != nil {
		return nil, err
	}
	exp, _, err := holdExpiry(ctx, db, mc.scope, c.ID, s.now())
	if err != nil {
		return nil, err
	}
	out := &StatusView{BookingID: ob.ID, Reference: ob.Reference, Status: c.Status, HoldExpiresAt: exp}
	if c.Status == string(contract.StatusPendingPayment) {
		if in, err := store.LiveTransitIntent(ctx, db, mc.scope, c.ID); err == nil && in.Status == store.IntentProcessing {
			out.Verifying = true
		}
	}
	return out, nil
}

// arrivalDayEnd is 23:59 of the arrival day in the marina time zone.
func arrivalDayEnd(arrival calendar.Date, loc *time.Location) time.Time {
	return arrival.AddDays(1).Time(loc).Add(-time.Minute)
}
