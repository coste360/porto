package contract

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Inquiries (spec 13.2): requests for a berth with their outcome, including the "unserved request" saved from
// the incoming-vessel search panel.

var (
	inquiryChannels = []string{"desk", "online", "api", "external"}
	inquirySources  = []string{"phone", "email", "desk", "public", "channel"}
	inquiryOutcomes = []string{"open", "quoted", "booked", "declined_no_space", "declined_size", "declined_other", "lost"}
)

// InquiryInput creates an inquiry. Without a vessel the dimensions are required.
type InquiryInput struct {
	Purpose            string // transit, contract
	Term               string
	Source             string
	Channel            string
	VesselID           string
	LOACm, BeamCm      int64
	DraftCm            int64
	Arrival, Departure calendar.Date
	CustomerID         string
	Outcome            string
	Notes              string
}

// InquiryPatch changes outcome, links and notes (nil keeps, "" clears a link).
type InquiryPatch struct {
	Outcome    *string
	QuoteID    *string
	ContractID *string
	Notes      *string
}

// InquiryView is an inquiry with its embedded vessel and customer.
type InquiryView struct {
	Row      store.Inquiry
	Vessel   *store.Vessel
	Customer *Party
	ShowNote bool
}

// CreateInquiry records an inquiry (transits.desk).
func (s *Service) CreateInquiry(ctx context.Context, p *auth.Principal, marinaID string, in InquiryInput) (*InquiryView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.Purpose != "transit" && in.Purpose != "contract" {
		v.Add("purpose", "invalid")
	}
	if in.Term != "" && !slices.Contains(terms, in.Term) {
		v.Add("term", "invalid")
	}
	if !slices.Contains(inquirySources, in.Source) {
		v.Add("source", "invalid")
	}
	if in.Channel == "" {
		in.Channel = ChannelDesk
	}
	if !slices.Contains(inquiryChannels, in.Channel) {
		v.Add("channel", "invalid")
	}
	if in.Outcome == "" {
		in.Outcome = "open"
	}
	if !slices.Contains(inquiryOutcomes, in.Outcome) {
		v.Add("outcome", "invalid")
	}
	checkStay(v, in.Arrival, in.Departure, "arrival", "departure", maxContractNights)
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	if in.VesselID != "" {
		vs, err := store.GetVessel(ctx, s.d.DB.DB, scope, in.VesselID)
		if err != nil {
			return nil, notFound(err)
		}
		if in.LOACm == 0 {
			in.LOACm = vs.LOACm
		}
		if in.BeamCm == 0 {
			in.BeamCm = vs.BeamCm
		}
		if in.DraftCm == 0 {
			in.DraftCm = vs.DraftCm
		}
	}
	if in.LOACm < 1 || in.LOACm > 20000 {
		v.Add("loa_cm", "required")
	}
	if in.BeamCm < 1 || in.BeamCm > 20000 {
		v.Add("beam_cm", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := checkParties(ctx, s.d.DB.DB, scope, "", in.CustomerID); err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.Inquiry{
		ID: store.NewID(), MarinaID: scope.ID(), Channel: in.Channel, Source: in.Source, Purpose: in.Purpose, Term: in.Term,
		VesselID: in.VesselID, CustomerID: in.CustomerID, LOACm: in.LOACm, BeamCm: in.BeamCm, DraftCm: in.DraftCm,
		Arrival: in.Arrival, Departure: in.Departure, Outcome: in.Outcome, Notes: notes,
		CreatedByUserID: actor{p}.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertInquiry(ctx, tx, row); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, actor{p}, "inquiry.create", entityInquiry, row.ID, map[string]any{
			"purpose": row.Purpose, "outcome": row.Outcome, "arrival": row.Arrival, "departure": row.Departure,
		}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.InquiryChanged, event.InquiryChangedData{InquiryID: row.ID, Change: event.ChangeCreated})
	})
	if err != nil {
		return nil, err
	}
	return s.inquiryView(ctx, s.d.DB.DB, scope, p, row)
}

// UpdateInquiry changes an inquiry (transits.desk, If-Match).
func (s *Service) UpdateInquiry(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in InquiryPatch) (*InquiryView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	var row *store.Inquiry
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetInquiry(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		var fields []string
		if in.Outcome != nil {
			if !slices.Contains(inquiryOutcomes, *in.Outcome) {
				v.Add("outcome", "invalid")
			}
			r.Outcome = *in.Outcome
			fields = append(fields, "outcome")
		}
		if in.Notes != nil {
			n, ok := cleanText(*in.Notes, 2000)
			if !ok {
				v.Add("notes", "too_long")
			}
			r.Notes = n
			fields = append(fields, "notes")
		}
		if in.QuoteID != nil {
			if *in.QuoteID != "" {
				if _, err := store.GetQuote(ctx, tx, scope, *in.QuoteID); err != nil {
					return notFound(err)
				}
			}
			r.QuoteID = *in.QuoteID
			fields = append(fields, "quote_id")
		}
		if in.ContractID != nil {
			if *in.ContractID != "" {
				if _, err := store.GetContract(ctx, tx, scope, *in.ContractID); err != nil {
					return notFound(err)
				}
			}
			r.ContractID = *in.ContractID
			fields = append(fields, "contract_id")
		}
		if err := v.Err(); err != nil {
			return err
		}
		r.UpdatedAt = s.now()
		if err := store.UpdateInquiry(ctx, tx, scope, ifMatch, r); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, actor{p}, "inquiry.update", entityInquiry, r.ID, map[string]any{"fields": fields, "outcome": r.Outcome}); err != nil {
			return err
		}
		row = r
		return s.emit(ctx, tx, scope, event.InquiryChanged, event.InquiryChangedData{InquiryID: r.ID, Change: event.ChangeUpdated})
	})
	if err != nil {
		return nil, err
	}
	return s.inquiryView(ctx, s.d.DB.DB, scope, p, row)
}

// linkInquiryTx sets the outcome and the quote or contract of an inquiry served by a quote, contract or transit.
func (s *Service) linkInquiryTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, id, outcome, quoteID, contractID string) error {
	r, err := store.GetInquiry(ctx, tx, scope, id)
	if err != nil {
		return notFound(err)
	}
	expected := r.Version
	r.Outcome = outcome
	if quoteID != "" {
		r.QuoteID = quoteID
	}
	if contractID != "" {
		r.ContractID = contractID
	}
	r.UpdatedAt = s.now()
	if err := store.UpdateInquiry(ctx, tx, scope, expected, r); err != nil {
		return versionErr(err)
	}
	if err := s.writeAudit(ctx, tx, scope, a, "inquiry.update", entityInquiry, r.ID, map[string]any{
		"fields": []string{"outcome"}, "outcome": outcome, "quote_id": quoteID, "contract_id": contractID,
	}); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.InquiryChanged, event.InquiryChangedData{InquiryID: r.ID, Change: event.ChangeUpdated})
}

// InquiryFilter narrows ListInquiries.
type InquiryFilter = store.InquiryFilter

// ListInquiries returns a page of inquiries (contracts.read).
func (s *Service) ListInquiries(ctx context.Context, p *auth.Principal, marinaID string, f InquiryFilter, cursor string, limit int) (store.Page[InquiryView], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[InquiryView]{}, err
	}
	db := s.d.DB.DB
	page, err := store.ListInquiries(ctx, db, scope, f, cursor, limit)
	if err != nil {
		return store.Page[InquiryView]{}, notFound(err)
	}
	out := make([]InquiryView, 0, len(page.Items))
	for i := range page.Items {
		v, err := s.inquiryView(ctx, db, scope, p, &page.Items[i])
		if err != nil {
			return store.Page[InquiryView]{}, err
		}
		out = append(out, *v)
	}
	return store.Page[InquiryView]{Items: out, NextCursor: page.NextCursor}, nil
}

func (s *Service) inquiryView(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, r *store.Inquiry) (*InquiryView, error) {
	withNames := s.can(ctx, p, scope.ID(), auth.PermCustomersRead)
	out := &InquiryView{Row: *r, ShowNote: withNames}
	if r.VesselID != "" {
		v, err := store.GetVessel(ctx, db, scope, r.VesselID)
		if err == nil {
			out.Vessel = v
		}
	}
	if r.CustomerID != "" {
		ps, err := parties(ctx, db, scope, []string{r.CustomerID}, withNames)
		if err != nil {
			return nil, err
		}
		out.Customer = partyOf(ps, r.CustomerID)
	}
	return out, nil
}
