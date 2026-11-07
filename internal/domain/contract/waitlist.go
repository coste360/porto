package contract

import (
	"context"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Waiting list (spec 13.9; m4-plan §3.6, §15 D26): entries by size, term and berth groups ordered by priority and
// date. When an annual mooring contract is terminated or cancelled, the first waiting entry that fits the freed
// berth is proposed to the office as a waitlist_offer deadline (no hold, no email to the customer); the office
// sends a quote from the entry, which marks it offered.

const entityWaitlist = "waitlist_entry"

// Waiting list statuses.
const (
	WaitWaiting   = "waiting"
	WaitOffered   = "offered"
	WaitConverted = "converted"
	WaitWithdrawn = "withdrawn"
)

// WaitlistInput creates an entry.
type WaitlistInput struct {
	CustomerID     string
	VesselID       string
	LOA, Beam, Dra int64 // cm; taken from the vessel when it is given and they are 0
	Term           string
	GroupIDs       []string
	RequestedFrom  calendar.Date
	Priority       int
	Notes          string
}

// WaitlistPatch changes an entry.
type WaitlistPatch struct {
	GroupIDs *[]string
	Notes    *string
	Priority *int
	Status   *string // withdrawn, waiting (back on the list), converted
}

// WaitlistRow is an entry with its customer.
type WaitlistRow struct {
	store.WaitlistEntry
	Customer *Party
	Vessel   *store.Vessel
}

// ListWaitlist returns the entries (contracts.read).
func (s *Service) ListWaitlist(ctx context.Context, p *auth.Principal, marinaID, status, term string, limit int) ([]WaitlistRow, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListWaitlistEntries(ctx, s.d.DB.DB, scope, status, term, limit)
	if err != nil {
		return nil, err
	}
	return s.waitlistRows(ctx, s.d.DB.DB, scope, p, rows)
}

func (s *Service) waitlistRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.WaitlistEntry) ([]WaitlistRow, error) {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.CustomerID)
	}
	ps, err := parties(ctx, db, scope, ids, s.can(ctx, p, scope.ID(), auth.PermCustomersRead))
	if err != nil {
		return nil, err
	}
	out := make([]WaitlistRow, 0, len(rows))
	for _, r := range rows {
		row := WaitlistRow{WaitlistEntry: r, Customer: partyOf(ps, r.CustomerID)}
		if r.VesselID != "" {
			if v, err := store.GetVessel(ctx, db, scope, r.VesselID); err == nil {
				row.Vessel = v
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// CreateWaitlistEntry adds an entry (contracts.write).
func (s *Service) CreateWaitlistEntry(ctx context.Context, p *auth.Principal, marinaID string, in WaitlistInput) (*WaitlistRow, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if err := checkParties(ctx, db, scope, in.VesselID, in.CustomerID); err != nil {
		return nil, err
	}
	if in.VesselID != "" {
		vs, err := store.GetVessel(ctx, db, scope, in.VesselID)
		if err != nil {
			return nil, notFound(err)
		}
		if in.LOA == 0 {
			in.LOA = vs.LOACm
		}
		if in.Beam == 0 {
			in.Beam = vs.BeamCm
		}
		if in.Dra == 0 {
			in.Dra = vs.DraftCm
		}
	}
	v := &auth.ValidationError{}
	if in.LOA <= 0 {
		v.Add("loa_cm", "required")
	}
	if in.Beam <= 0 {
		v.Add("beam_cm", "required")
	}
	if !contains(terms, in.Term) {
		v.Add("term", "invalid")
	}
	if in.RequestedFrom.IsZero() {
		v.Add("requested_from", "required")
	}
	notes, ok := cleanText(in.Notes, 2000)
	if !ok {
		v.Add("notes", "too_long")
	}
	if len(in.GroupIDs) > 50 {
		v.Add("group_ids", "too_many")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	a := actor{p}
	now := s.now()
	e := &store.WaitlistEntry{
		ID: store.NewID(), MarinaID: scope.ID(), CustomerID: in.CustomerID, VesselID: in.VesselID, LOACm: in.LOA,
		BeamCm: in.Beam, DraftCm: in.Dra, Term: in.Term, GroupIDs: store.Strings(nonNil(in.GroupIDs)), RequestedFrom: in.RequestedFrom,
		Priority: in.Priority, Status: WaitWaiting, Notes: notes, CreatedByUserID: a.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertWaitlistEntry(ctx, tx, e); err != nil {
			return err
		}
		return s.writeAudit(ctx, tx, scope, a, "waitlist.create", entityWaitlist, e.ID, map[string]any{
			"term": e.Term, "loa_cm": e.LOACm, "beam_cm": e.BeamCm, "priority": e.Priority,
		})
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.waitlistRows(ctx, db, scope, p, []store.WaitlistEntry{*e})
	if err != nil {
		return nil, err
	}
	return &rows[0], nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// UpdateWaitlistEntry changes an entry (contracts.write, If-Match).
func (s *Service) UpdateWaitlistEntry(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in WaitlistPatch) (*WaitlistRow, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	var out *store.WaitlistEntry
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		e, err := store.GetWaitlistEntry(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ifMatch != 0 && e.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		var fields []string
		if in.GroupIDs != nil {
			e.GroupIDs, fields = store.Strings(nonNil(*in.GroupIDs)), append(fields, "group_ids")
		}
		if in.Notes != nil {
			n, ok := cleanText(*in.Notes, 2000)
			if !ok {
				v.Add("notes", "too_long")
			}
			e.Notes, fields = n, append(fields, "notes")
		}
		if in.Priority != nil {
			e.Priority, fields = *in.Priority, append(fields, "priority")
		}
		if in.Status != nil {
			if !contains([]string{WaitWaiting, WaitWithdrawn, WaitConverted}, *in.Status) {
				v.Add("status", "invalid")
			}
			e.Status, fields = *in.Status, append(fields, "status")
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.UpdateWaitlistEntry(ctx, tx, scope, e.Version, e, s.now()); err != nil {
			return versionErr(err)
		}
		out = e
		return s.writeAudit(ctx, tx, scope, a, "waitlist.update", entityWaitlist, e.ID, map[string]any{"fields": fields, "status": e.Status})
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.waitlistRows(ctx, s.d.DB.DB, scope, p, []store.WaitlistEntry{*out})
	if err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// QuoteFromWaitlist creates a quote for an entry and marks it offered (contracts.write, If-Match on the entry).
// The quote input comes prefilled by the client from the entry; its customer must be the entry's.
func (s *Service) QuoteFromWaitlist(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in QuoteInput) (*QuoteDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	e, err := store.GetWaitlistEntry(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && e.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if e.Status != WaitWaiting && e.Status != WaitOffered {
		return nil, &TransitionError{From: e.Status, Action: "offer"}
	}
	if in.CustomerID == "" {
		in.CustomerID = e.CustomerID
	}
	if in.CustomerID != e.CustomerID {
		v := &auth.ValidationError{}
		v.Add("customer_id", "not_the_entry_customer")
		return nil, v
	}
	if in.VesselID == "" {
		in.VesselID = e.VesselID
	}
	if in.Kind == "" {
		in.Kind = KindMooring
	}
	if in.Term == "" {
		in.Term = e.Term
	}
	q, err := s.CreateQuote(ctx, p, marinaID, in)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		e, err := store.GetWaitlistEntry(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		e.Status, e.OfferedQuoteID, e.OfferedAt = WaitOffered, q.Row.ID, s.now()
		if err := store.UpdateWaitlistEntry(ctx, tx, scope, e.Version, e, s.now()); err != nil {
			return versionErr(err)
		}
		return s.writeAudit(ctx, tx, scope, a, "waitlist.offer", entityWaitlist, e.ID, map[string]any{"quote_id": q.Row.ID})
	})
	if err != nil {
		return nil, err
	}
	return q, nil
}

// waitlistFits reports whether an entry fits a berth and its groups.
func waitlistFits(e store.WaitlistEntry, b store.BerthDims, groups []string) bool {
	if b.MaxLOACm > 0 && e.LOACm > b.MaxLOACm {
		return false
	}
	if b.MaxBeamCm > 0 && e.BeamCm > b.MaxBeamCm {
		return false
	}
	if b.DepthCm > 0 && e.DraftCm > 0 && e.DraftCm > b.DepthCm {
		return false
	}
	if len(e.GroupIDs) == 0 {
		return true
	}
	for _, g := range e.GroupIDs {
		if contains(groups, g) {
			return true
		}
	}
	return false
}

// onContractFreedTx proposes the freed berth of a terminated or cancelled annual mooring contract to the first
// fitting waiting entry, as a waitlist_offer deadline for the office.
func (s *Service) onContractFreedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) error {
	if c.Kind != KindMooring || c.Term != TermAnnual {
		return nil
	}
	if pr, err := store.ProposalOfSuccessor(ctx, tx, scope, c.ID); err != nil || pr != nil {
		return err // a renewal successor freeing its hold is not a free berth
	}
	lines, err := store.LinesOfContracts(ctx, tx, scope, []string{c.ID})
	if err != nil {
		return err
	}
	var berths []string
	for _, l := range lines {
		if l.ResourceType == store.ResourceBerth {
			berths = append(berths, l.ResourceID)
		}
	}
	if len(berths) == 0 {
		return nil
	}
	dims, err := store.BerthDimsByIDs(ctx, tx, scope, berths)
	if err != nil || len(dims) == 0 {
		return err
	}
	b := dims[0]
	groups, err := store.BerthGroupIDs(ctx, tx, scope, b.ID)
	if err != nil {
		return err
	}
	entries, err := store.ListWaitlistEntries(ctx, tx, scope, WaitWaiting, TermAnnual, store.MaxLimit)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !waitlistFits(e, b, groups) {
			continue
		}
		existing, err := store.DeadlinesOfSource(ctx, tx, scope, entityWaitlist, e.ID)
		if err != nil {
			return err
		}
		for _, d := range existing {
			if d.Kind == "waitlist_offer" && d.Status == "open" {
				return nil
			}
		}
		now := s.now()
		note := strings.TrimSpace("berth:" + b.ID + " contract:" + c.ID)
		if len(existing) > 0 {
			return nil // one offer row per entry (deadlines_source_uidx); the office already has it
		}
		if err := store.InsertDeadline(ctx, tx, &store.Deadline{
			ID: store.NewID(), MarinaID: scope.ID(), SourceType: entityWaitlist, SourceID: e.ID, Kind: "waitlist_offer",
			CustomerID: e.CustomerID, VesselID: e.VesselID, DueOn: s.today(ctx, tx, scope), Status: "open", Note: note,
			NotifiedAt: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return s.writeAudit(ctx, tx, scope, system, "waitlist.offer", entityWaitlist, e.ID, map[string]any{
			"berth_id": b.ID, "contract_id": c.ID, "deadline": "waitlist_offer",
		})
	}
	return nil
}
