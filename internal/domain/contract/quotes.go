package contract

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Quotes with options (spec 13.2; m3-plan §4.3, §13 D28): 1–3 alternatives of 1–2 segments, each priced, each
// optionally holding its resources until the end of valid_until. Acceptance runs 13.3 steps 1–2 only.

// OptionInput is one alternative of a quote.
type OptionInput struct {
	Label       string
	Hold        bool
	Placements  []Segment
	Services    []ServiceRequest
	FitOverride *FitOverrideInput
}

// QuoteInput creates or replaces a quote.
type QuoteInput struct {
	Kind       string // mooring, transit
	Term       string // mooring only
	CustomerID string
	VesselID   string
	StartsOn   calendar.Date
	EndsOn     calendar.Date // exclusive
	ValidUntil *calendar.Date
	Channel    string
	Policy     *PolicyInput
	Notes      string
	InquiryID  string
	Options    []OptionInput
}

// OptionDetail is a quote option with its placements and price.
type OptionDetail struct {
	Row        store.QuoteOption
	Placements []store.QuoteOptionPlacement
	Snapshot   *PriceSnapshot
}

// QuoteDetail is a quote with its options.
type QuoteDetail struct {
	Row       store.Quote
	Code      string
	Options   []OptionDetail
	Vessel    store.Vessel
	Customer  *Party
	Resources map[string]store.ResourceRef
	Maps      map[string]string // allocation id → map id of the option holds
	ShowNotes bool
}

// QuoteCode returns the display code of a quote.
func QuoteCode(q *store.Quote) string { return ContractCode(SeriesQuote, q.Year, q.Number) }

// pricedOption is an option priced before the write transaction.
type pricedOption struct {
	in    OptionInput
	price *priced
}

func (s *Service) checkQuote(in *QuoteInput, today calendar.Date, st *store.OccupancySettings) error {
	v := &auth.ValidationError{}
	switch in.Kind {
	case KindTransit:
		if in.Term != "" {
			v.Add("term", "not_allowed")
		}
	case KindMooring:
		if !slices.Contains(terms, in.Term) {
			v.Add("term", "required")
		}
	default:
		v.Add("kind", "invalid")
	}
	if in.Channel == "" {
		in.Channel = ChannelDesk
	}
	if in.Channel != ChannelDesk && in.Channel != ChannelAPI {
		v.Add("channel", "invalid")
	}
	if in.CustomerID == "" {
		v.Add("customer_id", "required")
	}
	if in.VesselID == "" {
		v.Add("vessel_id", "required")
	}
	checkStay(v, in.StartsOn, in.EndsOn, "starts_on", "ends_on", maxContractNights)
	if in.ValidUntil == nil {
		d := today.AddDays(st.QuoteValidityDays)
		in.ValidUntil = &d
	} else if in.ValidUntil.Before(today) {
		v.Add("valid_until", "in_the_past")
	}
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	in.Notes = notes
	if len(in.Options) == 0 {
		v.Add("options", "required")
	}
	if len(in.Options) > 3 {
		v.Add("options", "too_many")
	}
	for i := range in.Options {
		o := &in.Options[i]
		f := "options[" + strconv.Itoa(i) + "]"
		label, ok := cleanText(o.Label, 120)
		if !ok {
			v.Add(f+".label", "too_long")
		}
		o.Label = label
		if !in.StartsOn.IsZero() && in.EndsOn.After(in.StartsOn) {
			checkSegments(v, f+".placements", o.Placements, in.StartsOn, in.EndsOn, 2)
		}
		if o.FitOverride != nil && cleanReason(o.FitOverride) == "" {
			v.Add(f+".fit_override.reason", "required")
		}
	}
	return v.Err()
}

// prepareQuote validates and prices a quote input (before the write transaction).
func (s *Service) prepareQuote(ctx context.Context, p *auth.Principal, marinaID string, scope store.MarinaScope, in *QuoteInput) ([]pricedOption, *store.OccupancySettings, error) {
	for _, o := range in.Options {
		if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(o.FitOverride)); err != nil {
			return nil, nil, err
		}
	}
	st, err := store.ContractOccupancySettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkQuote(in, s.today(ctx, s.d.DB.DB, scope), st); err != nil {
		return nil, nil, err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, in.VesselID, in.CustomerID); err != nil {
		return nil, nil, err
	}
	out := make([]pricedOption, 0, len(in.Options))
	for _, o := range in.Options {
		pr, err := s.price(ctx, p, marinaID, priceRequest{
			contractType: PricingType(in.Kind, in.Term), arrival: in.StartsOn, departure: in.EndsOn, first: o.Placements[0],
			vesselID: in.VesselID, customerID: in.CustomerID, channel: in.Channel, services: o.Services,
		})
		if err != nil {
			return nil, nil, err
		}
		out = append(out, pricedOption{in: o, price: pr})
	}
	return out, st, nil
}

// writeOptionsTx inserts the options of q with their placements and holds; it returns the hold allocations.
func (s *Service) writeOptionsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, q *store.Quote, opts []pricedOption) ([]store.Allocation, error) {
	now := s.now()
	var holds []store.Allocation
	rows := make([]store.QuoteOption, 0, len(opts))
	var placements []store.QuoteOptionPlacement
	for i, o := range opts {
		if err := checkListStillActive(ctx, tx, scope, o.price); err != nil {
			return nil, err
		}
		row := store.QuoteOption{
			ID: store.NewID(), MarinaID: scope.ID(), QuoteID: q.ID, Seq: i + 1, Label: o.in.Label, Hold: o.in.Hold,
			PriceListID: o.price.listID(), PriceSnapshot: o.price.raw, EngineVersion: o.price.engineVersion(),
			TotalCents: o.price.total(), DepositCents: o.price.deposit(), CreatedAt: now, UpdatedAt: now,
		}
		fit := fitOverride(a, o.in.FitOverride)
		if fit != nil {
			row.FitOverride = store.MustJSON(fit)
		}
		rows = append(rows, row)
		for j, g := range o.in.Placements {
			pl := store.QuoteOptionPlacement{
				ID: store.NewID(), MarinaID: scope.ID(), OptionID: row.ID, Seq: j + 1, ResourceType: g.ResourceType,
				ResourceID: g.ResourceID, XCm: g.X, YCm: g.Y, RotationDd: g.RotationDd, FirstNight: g.First, LastNight: g.Last,
				CreatedAt: now, UpdatedAt: now,
			}
			if o.in.Hold {
				exp := q.ExpiresAt
				al, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
					vesselID: q.VesselID, kind: occupancy.KindHold, seg: g, expiresAt: &exp, fit: fit, arrival: j == 0,
				})
				if err != nil {
					return nil, err
				}
				pl.AllocationID = al.ID
				holds = append(holds, *al)
			}
			placements = append(placements, pl)
		}
	}
	if err := store.InsertQuoteOptions(ctx, tx, rows); err != nil {
		return nil, err
	}
	if err := store.InsertQuotePlacements(ctx, tx, placements); err != nil {
		return nil, err
	}
	return holds, nil
}

// CreateQuote creates a draft quote with its options and holds (contracts.write).
func (s *Service) CreateQuote(ctx context.Context, p *auth.Principal, marinaID string, in QuoteInput) (*QuoteDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	opts, st, err := s.prepareQuote(ctx, p, marinaID, scope, &in)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := Next(QuoteTransitions, StatusNone, ActionCreate)
		if err != nil {
			return err
		}
		now := s.now()
		loc := location(ctx, tx, scope)
		year := s.today(ctx, tx, scope).Year()
		number, err := store.NextNumber(ctx, tx, scope, SeriesQuote, year)
		if err != nil {
			return err
		}
		v := &auth.ValidationError{}
		policy := mergePolicy(v, defaultPolicy(st), in.Policy)
		if err := v.Err(); err != nil {
			return err
		}
		q := &store.Quote{
			ID: store.NewID(), MarinaID: scope.ID(), Number: number, Year: year, Status: string(t.To), Kind: in.Kind,
			Term: in.Term, CustomerID: in.CustomerID, VesselID: in.VesselID, StartsOn: in.StartsOn, EndsOn: in.EndsOn,
			ValidUntil: *in.ValidUntil, ExpiresAt: endOfDay(*in.ValidUntil, loc), Channel: in.Channel,
			Policy: store.MustJSON(policy), Notes: in.Notes, InquiryID: in.InquiryID, CreatedByUserID: a.userID(),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if q.InquiryID != "" {
			if _, err := store.GetInquiry(ctx, tx, scope, q.InquiryID); err != nil {
				return notFound(err)
			}
		}
		if err := store.InsertQuote(ctx, tx, q); err != nil {
			return err
		}
		holds, err := s.writeOptionsTx(ctx, tx, scope, a, q, opts)
		if err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "quote.create", entityQuote, q.ID, map[string]any{
			"status": q.Status, "kind": q.Kind, "options": len(opts), "hold_allocation_ids": idsOf(holds),
			"valid_until": q.ValidUntil,
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{quote: q, change: event.ChangeCreated}); err != nil {
			return err
		}
		if err := s.emitAllocated(ctx, tx, scope, holds, ""); err != nil {
			return err
		}
		if q.InquiryID != "" {
			if err := s.linkInquiryTx(ctx, tx, scope, a, q.InquiryID, "quoted", q.ID, ""); err != nil {
				return err
			}
		}
		id = q.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.quoteDetail(ctx, s.d.DB.DB, scope, p, id)
}

// quoteHolds returns the hold allocations of the options of a quote, keyed by option id.
func quoteHolds(ctx context.Context, tx bun.IDB, scope store.MarinaScope, quoteID string) ([]store.QuoteOption, []store.QuoteOptionPlacement, map[string]store.Allocation, error) {
	opts, err := store.OptionsOfQuotes(ctx, tx, scope, []string{quoteID})
	if err != nil {
		return nil, nil, nil, err
	}
	oids := make([]string, 0, len(opts))
	for _, o := range opts {
		oids = append(oids, o.ID)
	}
	pls, err := store.PlacementsOfOptions(ctx, tx, scope, oids)
	if err != nil {
		return nil, nil, nil, err
	}
	var aids []string
	for _, pl := range pls {
		if pl.AllocationID != "" {
			aids = append(aids, pl.AllocationID)
		}
	}
	as, err := store.AllocationsByIDs(ctx, tx, scope, aids)
	if err != nil {
		return nil, nil, nil, err
	}
	m := map[string]store.Allocation{}
	for _, al := range as {
		m[al.ID] = al
	}
	return opts, pls, m, nil
}

// cancelQuoteHolds ends the active holds of the options (all when keep is "") except those of option keep.
func (s *Service) cancelQuoteHolds(ctx context.Context, tx bun.IDB, scope store.MarinaScope, pls []store.QuoteOptionPlacement,
	allocs map[string]store.Allocation, keep string, status occupancy.AllocStatus, reason string,
) ([]store.Allocation, error) {
	var ended []store.Allocation
	for _, pl := range pls {
		al, ok := allocs[pl.AllocationID]
		if !ok || pl.OptionID == keep || al.Status != store.AllocActive {
			continue
		}
		ended = append(ended, al)
	}
	return ended, s.cancelAllocs(ctx, tx, scope, ended, status, reason)
}

// lockQuote loads a quote for a state change, checking ifMatch (0 = no check: the customer's magic-link
// acceptance, whose link is the authority; the handlers always pass the required If-Match).
func lockQuote(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, ifMatch int) (*store.Quote, error) {
	q, err := store.LockQuote(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && q.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	return q, nil
}

func updateQuote(ctx context.Context, tx bun.IDB, scope store.MarinaScope, q *store.Quote, now time.Time) error {
	q.UpdatedAt = now
	if err := store.UpdateQuote(ctx, tx, scope, q.Version, q); err != nil {
		return versionErr(err)
	}
	return store.SetQuoteM4(ctx, tx, scope, q)
}

// UpdateQuote replaces a draft quote and its options (contracts.write, If-Match): the holds of the old options
// are cancelled and the new ones allocated in one transaction.
func (s *Service) UpdateQuote(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in QuoteInput) (*QuoteDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	cur, err := store.GetQuote(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if _, err := Next(QuoteTransitions, Status(cur.Status), ActionUpdate); err != nil {
		return nil, err
	}
	opts, st, err := s.prepareQuote(ctx, p, marinaID, scope, &in)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		q, err := lockQuote(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(QuoteTransitions, Status(q.Status), ActionUpdate)
		if err != nil {
			return err
		}
		_, pls, allocs, err := quoteHolds(ctx, tx, scope, q.ID)
		if err != nil {
			return err
		}
		ended, err := s.cancelQuoteHolds(ctx, tx, scope, pls, allocs, "", occupancy.StatusCancelled, event.ReleasedCancel)
		if err != nil {
			return err
		}
		if err := store.DeleteQuoteOptions(ctx, tx, scope, q.ID); err != nil {
			return err
		}
		v := &auth.ValidationError{}
		policy := mergePolicy(v, defaultPolicy(st), in.Policy)
		if err := v.Err(); err != nil {
			return err
		}
		q.Kind, q.Term, q.CustomerID, q.VesselID = in.Kind, in.Term, in.CustomerID, in.VesselID
		q.StartsOn, q.EndsOn, q.ValidUntil = in.StartsOn, in.EndsOn, *in.ValidUntil
		q.ExpiresAt, q.Channel = endOfDay(*in.ValidUntil, location(ctx, tx, scope)), in.Channel
		q.Policy, q.Notes = store.MustJSON(policy), in.Notes
		if in.InquiryID != "" {
			q.InquiryID = in.InquiryID
		}
		holds, err := s.writeOptionsTx(ctx, tx, scope, a, q, opts)
		if err != nil {
			return err
		}
		if err := updateQuote(ctx, tx, scope, q, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "quote.update", entityQuote, q.ID, map[string]any{
			"options": len(opts), "cancelled_hold_ids": idsOf(ended), "hold_allocation_ids": idsOf(holds),
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{quote: q, change: event.ChangeUpdated}); err != nil {
			return err
		}
		return s.emitAllocated(ctx, tx, scope, holds, "")
	})
	if err != nil {
		return nil, err
	}
	return s.quoteDetail(ctx, s.d.DB.DB, scope, p, id)
}

// MarkQuoteSent marks a draft quote as sent (contracts.write, If-Match; no email in M3).
func (s *Service) MarkQuoteSent(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*QuoteDetail, error) {
	return s.quoteAction(ctx, p, marinaID, id, ifMatch, ActionSend, "")
}

// DeclineQuote declines a draft or sent quote and cancels its holds (contracts.write, If-Match).
func (s *Service) DeclineQuote(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*QuoteDetail, error) {
	return s.quoteAction(ctx, p, marinaID, id, ifMatch, ActionDecline, reason)
}

func (s *Service) quoteAction(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, action Action, reason string) (*QuoteDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, 500)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("reason", "too_long")
		return nil, v
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		q, err := lockQuote(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		if (q.Status == string(QuoteDraft) || q.Status == string(QuoteSent)) && !s.now().Before(q.ExpiresAt) {
			return ErrQuoteExpired
		}
		t, err := Next(QuoteTransitions, Status(q.Status), action)
		if err != nil {
			return err
		}
		now := s.now()
		from := q.Status
		var ended []store.Allocation
		change := ""
		if action == ActionDecline {
			_, pls, allocs, err := quoteHolds(ctx, tx, scope, q.ID)
			if err != nil {
				return err
			}
			if ended, err = s.cancelQuoteHolds(ctx, tx, scope, pls, allocs, "", occupancy.StatusCancelled, event.ReleasedCancel); err != nil {
				return err
			}
			q.DecidedAt, q.DeclineReason, change = now, reason, event.ChangeDeclined
		} else {
			q.SentAt = now
		}
		q.Status = string(t.To)
		if err := updateQuote(ctx, tx, scope, q, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "quote."+string(action), entityQuote, q.ID, map[string]any{
			"from_status": from, "status": q.Status, "cancelled_hold_ids": idsOf(ended),
		}); err != nil {
			return err
		}
		return s.emitTransition(ctx, tx, scope, t, subject{quote: q, change: change})
	})
	if err != nil {
		return nil, err
	}
	return s.quoteDetail(ctx, s.d.DB.DB, scope, p, id)
}

// AcceptInput accepts one option of a quote.
type AcceptInput struct {
	OptionID string
	Activate bool                // mooring: create the contract active ("firmato su carta") instead of draft
	Confirm  *ManualConfirmation // transit: confirm with the manual driver
	// M4 (m4-plan §3.2): the plan choice and the signers of the signature request.
	Plan    string // standard (default), early_payment
	Signers []SignerInput
}

// AcceptResult is the outcome of a quote acceptance (m4-plan §7.3.1).
type AcceptResult struct {
	Quote              *QuoteDetail
	Contract           *Detail
	SignatureRequestID string
	DocumentID         string
	Warnings           []string
}

// AcceptQuote accepts an option (contracts.write, If-Match): 13.3 steps 1–2 in one transaction. The option's
// holds become the contract's (or the transit's) allocations, or are allocated now (409 resource_taken creates
// nothing); the other options' holds are cancelled; the contract or transit carries the option's snapshot.
func (s *Service) AcceptQuote(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in AcceptInput) (*QuoteDetail, *Detail, error) {
	r, err := s.AcceptQuoteM4(ctx, p, marinaID, id, ifMatch, in)
	if err != nil {
		return nil, nil, err
	}
	return r.Quote, r.Contract, nil
}

// AcceptQuoteM4 is AcceptQuote with the M4 steps of 13.3 in the same transaction (m4-plan §3.2): step 3 the
// instalment plan (standard or early payment), step 4 the contract document and, when the marina's signature
// driver is not paper and activate is not asked, the signature request (contract awaiting_signature). The
// insurance policy block refuses with 422 insurance_required.
func (s *Service) AcceptQuoteM4(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in AcceptInput) (*AcceptResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if in.Confirm != nil {
		ref, ok := cleanText(in.Confirm.Reference, 200)
		if !ok {
			v := &auth.ValidationError{}
			v.Add("confirm.reference", "too_long")
			return nil, v
		}
		in.Confirm.Reference = ref
	}
	if err := validateSignatureInput(SignatureInput{Signers: in.Signers}); err != nil {
		return nil, err
	}
	warnings, err := s.quoteInsurance(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	var res *AcceptResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		q, err := lockQuote(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		res, err = s.acceptTx(ctx, tx, scope, actor{p}, q, in, ViaPanel)
		return err
	})
	if err != nil {
		if q, qerr := store.GetQuote(ctx, s.d.DB.DB, scope, id); qerr == nil {
			err = s.withAlternatives(ctx, p, marinaID, q.VesselID, err)
		}
		return nil, err
	}
	res.Warnings = warnings
	if res.Quote, err = s.quoteDetail(ctx, s.d.DB.DB, scope, p, id); err != nil {
		return nil, err
	}
	res.Contract, err = s.detail(ctx, s.d.DB.DB, scope, p, res.Contract.Row.ID)
	return res, err
}

// quoteInsurance applies the insurance policy to the vessel of a mooring quote on its start.
func (s *Service) quoteInsurance(ctx context.Context, scope store.MarinaScope, id string) ([]string, error) {
	q, err := store.GetQuote(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if q.Kind != KindMooring || q.VesselID == "" {
		return nil, nil
	}
	cs, err := s.settingsTx(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	return checkInsurance(ctx, s.d.DB.DB, scope, cs, q.VesselID, q.StartsOn)
}

// acceptTx accepts an option of a locked quote: M3 steps 1–2, then the M4 steps 3–4 (shared by the panel and
// the public magic-link acceptance). The returned Contract holds only the row (the caller reloads the detail).
func (s *Service) acceptTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, q *store.Quote, in AcceptInput, via string) (*AcceptResult, error) {
	st, err := store.ContractOccupancySettings(ctx, tx, scope, s.now())
	if err != nil {
		return nil, err
	}
	now := s.now()
	if q.Status == string(QuoteExpired) || ((q.Status == string(QuoteDraft) || q.Status == string(QuoteSent)) && !now.Before(q.ExpiresAt)) {
		return nil, ErrQuoteExpired
	}
	t, err := Next(QuoteTransitions, Status(q.Status), ActionAccept)
	if err != nil {
		return nil, err
	}
	if (q.Kind == KindTransit && in.Activate) || (q.Kind == KindMooring && in.Confirm != nil) {
		v := &auth.ValidationError{}
		v.Add("activate", "not_allowed")
		return nil, v
	}
	if in.Plan != "" && in.Plan != PlanStandard && in.Plan != PlanEarlyPayment {
		v := &auth.ValidationError{}
		v.Add("plan", "invalid")
		return nil, v
	}
	opts, pls, allocs, err := quoteHolds(ctx, tx, scope, q.ID)
	if err != nil {
		return nil, err
	}
	idx := slices.IndexFunc(opts, func(o store.QuoteOption) bool { return o.ID == in.OptionID })
	if idx < 0 {
		v := &auth.ValidationError{}
		v.Add("option_id", "invalid")
		return nil, v
	}
	opt := opts[idx]
	others, err := s.cancelQuoteHolds(ctx, tx, scope, pls, allocs, opt.ID, occupancy.StatusCancelled, event.ReleasedCancel)
	if err != nil {
		return nil, err
	}
	// The chosen option's holds are superseded by the contract's allocations (the same nights, now on the
	// contract lines; the hold has no line, so it is not a moved_from predecessor); missing holds are allocated
	// now.
	var segs []Segment
	var moved []string // superseded hold per segment ("" = allocated now)
	for _, pl := range pls {
		if pl.OptionID != opt.ID {
			continue
		}
		segs = append(segs, Segment{
			ResourceType: pl.ResourceType, ResourceID: pl.ResourceID, X: pl.XCm, Y: pl.YCm, RotationDd: pl.RotationDd,
			First: pl.FirstNight, Last: pl.LastNight,
		})
		m := ""
		if al, ok := allocs[pl.AllocationID]; ok && al.Status == store.AllocActive {
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusSuperseded); err != nil {
				return nil, err
			}
			m = al.ID
		}
		moved = append(moved, m)
	}
	snap := DecodeSnapshot(opt.PriceSnapshot)
	var price *priced
	var services []ServiceRequest
	if snap != nil {
		price = &priced{snapshot: *snap, raw: opt.PriceSnapshot}
		for _, sv := range snap.Request.Services {
			services = append(services, ServiceRequest{ItemID: sv.ItemID, QtyMilli: int64(sv.Qty)})
		}
	}
	var fit *occupancy.FitOverride
	if len(opt.FitOverride) > 0 {
		fit = &occupancy.FitOverride{}
		if jsonUnmarshal(opt.FitOverride, fit) != nil {
			fit = nil
		}
	}
	policy, err := DecodePolicy(q.Policy)
	if err != nil || policy.Version == 0 {
		policy = defaultPolicy(st)
	}
	// Closures apply to new arrivals only: a held option was already accepted by the engine.
	arrival := len(moved) == 0 || moved[0] == ""
	var c *store.Contract
	if q.Kind == KindTransit {
		c, err = s.createTransitTx(ctx, tx, scope, a, TransitInput{
			VesselID: q.VesselID, CustomerID: q.CustomerID, Arrival: q.StartsOn, Departure: q.EndsOn, Placements: segs,
			Channel: q.Channel, Notes: q.Notes, Services: services, Confirm: in.Confirm,
		}, price, st, arrival, &fromQuote{quoteID: q.ID, fit: fit, policy: policy})
	} else {
		c, err = s.createMooringTx(ctx, tx, scope, a, newContract{
			kind: KindMooring, term: q.Term, customerID: q.CustomerID, vesselID: q.VesselID, startsOn: q.StartsOn,
			endsOn: q.EndsOn, channel: q.Channel, quoteID: q.ID, price: price, policy: policy, notes: q.Notes,
			segments: segs, services: services, arrival: arrival, fit: fit, priceIsBinding: true, plan: in.Plan,
		}, in.Activate, st)
	}
	if err != nil {
		return nil, err
	}
	res := &AcceptResult{Contract: &Detail{Row: *c}}
	from := q.Status
	q.Status, q.AcceptedOptionID, q.ContractID, q.DecidedAt, q.AcceptedVia = string(t.To), opt.ID, c.ID, now, via
	if err := updateQuote(ctx, tx, scope, q, now); err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "quote.accept", entityQuote, q.ID, map[string]any{
		"from_status": from, "status": q.Status, "option_id": opt.ID, "contract_id": c.ID,
		"superseded_hold_ids": moved, "cancelled_hold_ids": idsOf(others), "via": via, "plan": choiceOr(in.Plan),
	}); err != nil {
		return nil, err
	}
	if err := s.emitTransition(ctx, tx, scope, t, subject{quote: q}); err != nil {
		return nil, err
	}
	if q.InquiryID != "" {
		if err := s.linkInquiryTx(ctx, tx, scope, a, q.InquiryID, "booked", q.ID, c.ID); err != nil {
			return nil, err
		}
	}
	if q.Kind != KindMooring {
		return res, nil
	}
	// Step 4: the contract document, and the signature request when the driver is not paper.
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	if !in.Activate && cs.SignatureDriver != DriverPaper && s.signer != nil {
		cc, err := lockContract(ctx, tx, scope, c.ID, KindMooring, 0)
		if err != nil {
			return nil, err
		}
		reqID, err := s.requestContractSignatureTx(ctx, tx, scope, a, cc, SignatureInput{Signers: in.Signers})
		if err != nil {
			return nil, err
		}
		res.SignatureRequestID, res.DocumentID = reqID, ""
		res.Contract.Row = *cc
		return res, nil
	}
	docID, err := s.composeTx(ctx, tx, scope, a, docs.ContractKindOf(c.Kind, c.Term), docs.SubjectContract, c.ID, false)
	if err != nil {
		return nil, err
	}
	res.DocumentID = docID
	return res, s.commitDocHook()
}

// GetQuote returns a quote with its options (contracts.read).
func (s *Service) GetQuote(ctx context.Context, p *auth.Principal, marinaID, id string) (*QuoteDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	return s.quoteDetail(ctx, s.d.DB.DB, scope, p, id)
}

func (s *Service) quoteDetail(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*QuoteDetail, error) {
	q, err := store.GetQuote(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	opts, pls, allocs, err := quoteHolds(ctx, db, scope, q.ID)
	if err != nil {
		return nil, err
	}
	v, err := store.GetVessel(ctx, db, scope, q.VesselID)
	if err != nil {
		return nil, err
	}
	withNames := s.can(ctx, p, scope.ID(), auth.PermCustomersRead)
	ps, err := parties(ctx, db, scope, []string{q.CustomerID}, withNames)
	if err != nil {
		return nil, err
	}
	rids := make([]string, 0, len(pls))
	for _, pl := range pls {
		rids = append(rids, pl.ResourceID)
	}
	refs, err := store.ResourceRefs(ctx, db, scope, rids)
	if err != nil {
		return nil, err
	}
	d := &QuoteDetail{
		Row: *q, Code: QuoteCode(q), Vessel: *v, Customer: partyOf(ps, q.CustomerID), Resources: refs,
		Maps: map[string]string{}, ShowNotes: withNames,
	}
	for _, o := range opts {
		od := OptionDetail{Row: o, Snapshot: DecodeSnapshot(o.PriceSnapshot)}
		for _, pl := range pls {
			if pl.OptionID == o.ID {
				if al, ok := allocs[pl.AllocationID]; ok && al.Status != store.AllocActive {
					pl.AllocationID = "" // the hold is gone
				}
				od.Placements = append(od.Placements, pl)
			}
		}
		d.Options = append(d.Options, od)
	}
	for _, al := range allocs {
		d.Maps[al.ID] = al.MapID
	}
	return d, nil
}

// QuoteSummary is a quote in a list.
type QuoteSummary struct {
	Row           store.Quote
	Code          string
	Vessel        store.Vessel
	Customer      *Party
	OptionsCount  int
	MinTotalCents *int64
}

// QuoteFilter narrows ListQuotes.
type QuoteFilter struct {
	Status, Kind, CustomerID, Search string
}

// ListQuotes returns a page of quotes (contracts.read).
func (s *Service) ListQuotes(ctx context.Context, p *auth.Principal, marinaID string, f QuoteFilter, cursor string, limit int) (store.Page[QuoteSummary], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[QuoteSummary]{}, err
	}
	db := s.d.DB.DB
	sf := store.QuoteFilter{Status: f.Status, Kind: f.Kind, CustomerID: f.CustomerID}
	if q := strings.TrimSpace(f.Search); q != "" {
		sf.Search = customer.Fold(q)
		if n, err := strconv.ParseInt(lastDigits(q), 10, 64); err == nil {
			sf.Number = n
		}
	}
	page, err := store.ListQuotes(ctx, db, scope, sf, cursor, limit)
	if err != nil {
		return store.Page[QuoteSummary]{}, notFound(err)
	}
	ids := make([]string, 0, len(page.Items))
	var vids, cids []string
	for _, q := range page.Items {
		ids = append(ids, q.ID)
		vids = append(vids, q.VesselID)
		cids = append(cids, q.CustomerID)
	}
	opts, err := store.OptionsOfQuotes(ctx, db, scope, ids)
	if err != nil {
		return store.Page[QuoteSummary]{}, err
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return store.Page[QuoteSummary]{}, err
	}
	vm := map[string]store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = vs[i]
	}
	ps, err := parties(ctx, db, scope, cids, s.can(ctx, p, scope.ID(), auth.PermCustomersRead))
	if err != nil {
		return store.Page[QuoteSummary]{}, err
	}
	out := make([]QuoteSummary, 0, len(page.Items))
	for i := range page.Items {
		q := page.Items[i]
		sm := QuoteSummary{Row: q, Code: QuoteCode(&q), Vessel: vm[q.VesselID], Customer: partyOf(ps, q.CustomerID)}
		for _, o := range opts {
			if o.QuoteID != q.ID {
				continue
			}
			sm.OptionsCount++
			if sm.MinTotalCents == nil || o.TotalCents < *sm.MinTotalCents {
				t := o.TotalCents
				sm.MinTotalCents = &t
			}
		}
		out = append(out, sm)
	}
	return store.Page[QuoteSummary]{Items: out, NextCursor: page.NextCursor}, nil
}
