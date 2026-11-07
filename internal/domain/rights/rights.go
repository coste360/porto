package rights

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Berth rights, holders and authorised vessels, owner occupancy (spec 14.1; m4-plan §4.1).

// Kinds lists the right kinds.
var Kinds = []string{KindOwnershipShare, KindLongTermUse, KindMembership}

// Title are the deed data of a right.
type Title struct {
	Deed   string        `json:"deed,omitempty"`
	Number string        `json:"number,omitempty"`
	Notary string        `json:"notary,omitempty"`
	Date   calendar.Date `json:"date,omitzero"`
}

// Holder is a holder of a right. DisplayName is set only with customers.read.
type Holder struct {
	CustomerID  string
	DisplayName string
	ShareMillis int64
}

// Vessel is an authorised vessel of a right (no registration, no owner).
type Vessel struct {
	VesselID    string
	Name        string
	LOACm       int64
	BeamCm      int64
	DraftCm     int64
	HullType    string
	Primary     bool
	FitOverride json.RawMessage
}

// Right is a berth right with its holders, vessels and owner allocations.
type Right struct {
	ID              string
	BerthID         string
	BerthCode       string
	Kind            string
	FromOn, ToOn    calendar.Date // inclusive last night
	Title           Title
	Status          string
	EndReason       string
	Holders         []Holder
	Vessels         []Vessel
	Allocations     []store.Allocation // active owner allocations
	ActiveMandateID string
	UnpaidFees      bool
	CreatedAt       time.Time
	Version         int
}

// VesselInput is an authorised vessel of a right input.
type VesselInput struct {
	VesselID string
	Primary  bool
}

// RightInput are the fields of a new right. ToOn zero = the concession expiry − 1 day.
type RightInput struct {
	BerthID     string
	Kind        string
	FromOn      calendar.Date
	ToOn        calendar.Date
	Title       Title
	Holders     []allot.Share
	Vessels     []VesselInput
	FitOverride string // reason: accept the blocking fit codes of the primary vessel (occupancy.override_fit)
}

// RightPatch lists the fields to change (nil = unchanged).
type RightPatch struct {
	Title   *Title
	Holders []allot.Share
}

// RightFilter narrows ListRights.
type RightFilter = store.RightFilter

func sharesOf(hs []store.BerthRightHolder) []allot.Share {
	out := make([]allot.Share, len(hs))
	for i, h := range hs {
		out[i] = allot.Share{CustomerID: h.CustomerID, Millis: h.ShareMillis}
	}
	return out
}

// checkShares validates holder shares (INV-40) and returns a *SumError.
func checkShares(shares []allot.Share) error {
	sum, err := allot.CheckShares(shares)
	if err != nil {
		return &SumError{Err: ErrSharesInvalid, SumMillis: sum}
	}
	return nil
}

// checkCustomers returns ErrNotFound unless every customer belongs to the marina.
func checkCustomers(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) error {
	uniq := map[string]bool{}
	for _, id := range ids {
		if !store.ValidID(id) {
			return store.ErrNotFound
		}
		uniq[id] = true
	}
	list := make([]string, 0, len(uniq))
	for id := range uniq {
		list = append(list, id)
	}
	cs, err := store.CustomersByIDs(ctx, db, scope, list)
	if err != nil {
		return err
	}
	if len(cs) != len(list) {
		return store.ErrNotFound
	}
	return nil
}

func holderRows(shares []allot.Share, now time.Time) []store.BerthRightHolder {
	out := make([]store.BerthRightHolder, len(shares))
	for i, sh := range shares {
		out[i] = store.BerthRightHolder{ID: store.NewID(), CustomerID: sh.CustomerID, ShareMillis: sh.Millis, CreatedAt: now, UpdatedAt: now}
	}
	return out
}

// vesselRows validates the authorised vessels (distinct, of the marina, at most one primary, one primary when
// any) and returns the rows and the primary vessel id ("" when none).
func vesselRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, in []VesselInput, now time.Time) ([]store.BerthRightVessel, string, error) {
	v := &auth.ValidationError{}
	seen := map[string]bool{}
	primary := ""
	ids := make([]string, 0, len(in))
	for _, x := range in {
		if !store.ValidID(x.VesselID) {
			return nil, "", store.ErrNotFound
		}
		if seen[x.VesselID] {
			v.Add("vessels", "duplicate")
		}
		seen[x.VesselID] = true
		ids = append(ids, x.VesselID)
		if x.Primary {
			if primary != "" {
				v.Add("vessels", "primary_not_unique")
			}
			primary = x.VesselID
		}
	}
	if len(in) > 0 && primary == "" {
		v.Add("vessels", "primary_required")
	}
	if len(in) > 10 {
		v.Add("vessels", "too_many")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, "", err
	}
	if len(vs) != len(ids) {
		return nil, "", store.ErrNotFound
	}
	out := make([]store.BerthRightVessel, len(in))
	for i, x := range in {
		out[i] = store.BerthRightVessel{ID: store.NewID(), VesselID: x.VesselID, IsPrimary: x.Primary, CreatedAt: now, UpdatedAt: now}
	}
	return out, primary, nil
}

// defaultEnd returns the default last night of a right: the concession expiry − 1 day (zero when unknown).
func defaultEnd(ctx context.Context, db bun.IDB, scope store.MarinaScope) (calendar.Date, error) {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return calendar.Date{}, err
	}
	if m.ConcessionExpiresOn.IsZero() {
		return calendar.Date{}, nil
	}
	return m.ConcessionExpiresOn.AddDays(-1), nil
}

func displayName(c *store.Customer) string {
	if !c.AnonymizedAt.IsZero() {
		return ""
	}
	if c.Kind == "company" {
		return c.CompanyName
	}
	return strings.TrimSpace(c.LastName + " " + c.FirstName)
}

// names returns the display names of customers (empty map without customers.read).
func names(ctx context.Context, db bun.IDB, scope store.MarinaScope, withNames bool, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if !withNames || len(ids) == 0 {
		return out, nil
	}
	cs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		out[cs[i].ID] = displayName(&cs[i])
	}
	return out, nil
}

// loadRights builds the views of right rows: berth code, holders, vessels, active owner allocations, current mandate and
// unpaid fees.
func (s *Service) loadRights(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.BerthRight) ([]Right, error) {
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	hs, err := store.HoldersOfRights(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	vs, err := store.VesselsOfRights(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	allocs, err := store.AllocationsOfRights(ctx, db, scope, ids, true)
	if err != nil {
		return nil, err
	}
	bids := make([]string, len(rows))
	for i := range rows {
		bids[i] = rows[i].BerthID
	}
	berths, err := store.RightsBerthsByIDs(ctx, db, scope, bids)
	if err != nil {
		return nil, err
	}
	ms, err := store.MandatesOfRights(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	unpaid, err := store.RightsWithUnpaidFees(ctx, db, scope, today)
	if err != nil {
		return nil, err
	}
	var cids, vids []string
	for _, h := range hs {
		cids = append(cids, h.CustomerID)
	}
	for _, v := range vs {
		vids = append(vids, v.VesselID)
	}
	nm, err := names(ctx, db, scope, can(p, scope, auth.PermCustomersRead), cids)
	if err != nil {
		return nil, err
	}
	vrows, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vinfo := map[string]*store.Vessel{}
	for i := range vrows {
		vinfo[vrows[i].ID] = &vrows[i]
	}
	out := make([]Right, len(rows))
	for i := range rows {
		r := &rows[i]
		x := Right{
			ID: r.ID, BerthID: r.BerthID, BerthCode: berths[r.BerthID].Code, Kind: r.Kind, FromOn: r.FromOn, ToOn: r.ToOn, Status: r.Status, EndReason: r.EndReason,
			Holders: []Holder{}, Vessels: []Vessel{}, Allocations: allocs[r.ID], UnpaidFees: unpaid[r.ID], CreatedAt: r.CreatedAt, Version: r.Version,
		}
		if len(r.Title) > 0 {
			_ = json.Unmarshal(r.Title, &x.Title)
		}
		for _, h := range hs {
			if h.RightID == r.ID {
				x.Holders = append(x.Holders, Holder{CustomerID: h.CustomerID, DisplayName: nm[h.CustomerID], ShareMillis: h.ShareMillis})
			}
		}
		for _, v := range vs {
			if v.RightID != r.ID {
				continue
			}
			vv := Vessel{VesselID: v.VesselID, Primary: v.IsPrimary, FitOverride: json.RawMessage(v.FitOverride)}
			if info := vinfo[v.VesselID]; info != nil {
				vv.Name, vv.LOACm, vv.BeamCm, vv.DraftCm, vv.HullType = info.Name, info.LOACm, info.BeamCm, info.DraftCm, info.HullType
			}
			x.Vessels = append(x.Vessels, vv)
		}
		for _, m := range ms {
			if m.RightID == r.ID && m.Status == MandateActive && !today.Before(m.FromOn) && !today.After(m.ToOn) {
				x.ActiveMandateID = m.ID
			}
		}
		out[i] = x
	}
	return out, nil
}

func (s *Service) loadRight(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, r *store.BerthRight) (*Right, error) {
	out, err := s.loadRights(ctx, db, scope, p, []store.BerthRight{*r})
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// ListRights returns a page of rights (rights.manage).
func (s *Service) ListRights(ctx context.Context, p *auth.Principal, marinaID string, f RightFilter, cursor string, limit int) (store.Page[Right], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[Right]{}, err
	}
	if f.Status != "" && !slices.Contains([]string{RightActive, RightTransferred, RightEnded, RightCancelled}, f.Status) {
		v := &auth.ValidationError{}
		v.Add("status", "invalid")
		return store.Page[Right]{}, v
	}
	var out store.Page[Right]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		page, err := store.ListBerthRights(ctx, tx, scope, f, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.loadRights(ctx, tx, scope, p, page.Items)
		if err != nil {
			return err
		}
		out = store.Page[Right]{Items: items, NextCursor: page.NextCursor}
		return nil
	})
	return out, err
}

// GetRight returns a right (rights.manage).
func (s *Service) GetRight(ctx context.Context, p *auth.Principal, marinaID, id string) (*Right, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Right
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetBerthRight(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		out, err = s.loadRight(ctx, tx, scope, p, r)
		return err
	})
	return out, err
}

// CreateRight creates a right with its holders and vessels and the owner occupancy of its nights from
// max(from_on, today) (rights.manage). A night already taken is 409 resource_taken and nothing is written.
func (s *Service) CreateRight(ctx context.Context, p *auth.Principal, marinaID string, in RightInput) (*Right, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if in.FitOverride != "" && !can(p, scope, auth.PermOccupancyOverrideFit) {
		return nil, auth.ErrForbidden
	}
	var out *Right
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := s.createRightTx(ctx, tx, scope, p, in, "")
		if err != nil {
			return err
		}
		out, err = s.loadRight(ctx, tx, scope, p, r)
		return err
	})
	return out, err
}

// createRightTx validates and writes a right, its holders, vessels and owner occupancy, with audit and events.
// transferID is set when the right is the successor of a transfer (events are then written by the transfer).
func (s *Service) createRightTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, in RightInput, transferID string) (*store.BerthRight, error) {
	now := s.now()
	today := s.today(ctx, tx, scope)
	v := &auth.ValidationError{}
	if !slices.Contains(Kinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	if in.FromOn.IsZero() {
		v.Add("from_on", "required")
	}
	if in.ToOn.IsZero() {
		end, err := defaultEnd(ctx, tx, scope)
		if err != nil {
			return nil, err
		}
		if end.IsZero() {
			v.Add("to_on", "required")
		}
		in.ToOn = end
	}
	if !in.FromOn.IsZero() && !in.ToOn.IsZero() && in.ToOn.Before(in.FromOn) {
		v.Add("to_on", "before_from_on")
	}
	if !in.ToOn.IsZero() && in.ToOn.Before(today) {
		v.Add("to_on", "in_the_past")
	}
	if len(in.Holders) > 20 {
		v.Add("holders", "too_many")
	}
	checkTitle(v, in.Title)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := checkShares(in.Holders); err != nil {
		return nil, err
	}
	b, err := store.BerthByID(ctx, tx, scope, in.BerthID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(in.Holders))
	for i, h := range in.Holders {
		ids[i] = h.CustomerID
	}
	if err := checkCustomers(ctx, tx, scope, ids); err != nil {
		return nil, err
	}
	vrows, primary, err := vesselRows(ctx, tx, scope, in.Vessels, now)
	if err != nil {
		return nil, err
	}
	r := &store.BerthRight{
		ID: store.NewID(), BerthID: b.ID, Kind: in.Kind, FromOn: in.FromOn, ToOn: in.ToOn, Title: store.MustJSON(in.Title),
		Status: RightActive, CreatedByUserID: userOf(p), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertBerthRight(ctx, tx, scope, r); err != nil {
		return nil, err
	}
	if err := store.ReplaceRightHolders(ctx, tx, scope, r.ID, holderRows(in.Holders, now)); err != nil {
		return nil, err
	}
	var override *occupancy.FitOverride
	if in.FitOverride != "" {
		override = occupancy.NewFitOverride(p, in.FitOverride)
	}
	first := in.FromOn
	if first.Before(today) {
		first = today
	}
	a, err := s.allocateOwner(ctx, tx, scope, r, primary, first, r.ToOn, override, userOf(p))
	if err != nil {
		return nil, err
	}
	if a != nil && len(a.FitOverride) > 0 {
		for i := range vrows {
			if vrows[i].IsPrimary {
				vrows[i].FitOverride = a.FitOverride
			}
		}
	}
	if err := store.ReplaceRightVessels(ctx, tx, scope, r.ID, vrows); err != nil {
		return nil, err
	}
	events := []ev{}
	if transferID == "" {
		events = append(events, ev{event.RightCreated, event.RightData{RightID: r.ID, BerthID: r.BerthID, Kind: r.Kind, From: r.FromOn, To: r.ToOn}})
	}
	if a != nil {
		events = append(events, allocatedEvent(a))
	}
	diff := map[string]any{"berth_id": r.BerthID, "kind": r.Kind, "from_on": r.FromOn, "to_on": r.ToOn, "holders": len(in.Holders), "transfer_id": transferID}
	if err := s.write(ctx, tx, actorOf(p), scope, "right.create", "right", r.ID, diff, events...); err != nil {
		return nil, err
	}
	return r, nil
}

func checkTitle(v *auth.ValidationError, t Title) {
	if len(t.Deed) > 200 {
		v.Add("title.deed", "too_long")
	}
	if len(t.Number) > 64 {
		v.Add("title.number", "too_long")
	}
	if len(t.Notary) > 200 {
		v.Add("title.notary", "too_long")
	}
}

// allocateOwner writes the owner allocation of a right for [first, last] (nil when the period is empty) and links
// it to the right.
func (s *Service) allocateOwner(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.BerthRight, vesselID string,
	first, last calendar.Date, override *occupancy.FitOverride, userID string,
) (*store.Allocation, error) {
	if last.Before(first) {
		return nil, nil
	}
	in := occupancy.OwnerAllocation{BerthID: r.BerthID, First: first, Last: last, FitOverride: override, UserID: userID}
	if vesselID != "" {
		in.VesselID = &vesselID
	}
	a, err := s.d.Occupancy.AllocateOwnerTx(ctx, tx, scope, s.now(), in)
	if err != nil {
		return nil, err
	}
	if err := store.LinkRightAllocation(ctx, tx, scope, r.ID, a.ID, s.now()); err != nil {
		return nil, err
	}
	return a, nil
}

// primaryOf returns the primary authorised vessel of a right ("" when none).
func primaryOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, rightID string) (string, error) {
	vs, err := store.VesselsOfRights(ctx, db, scope, []string{rightID})
	if err != nil {
		return "", err
	}
	for _, v := range vs {
		if v.IsPrimary {
			return v.VesselID, nil
		}
	}
	return "", nil
}

// truncateOwner drops the owner nights ≥ from of a right (allocations starting later are cancelled) and returns
// the release events.
func (s *Service) truncateOwner(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rightID string, from calendar.Date, reason string) ([]ev, error) {
	allocs, err := store.AllocationsOfRights(ctx, tx, scope, []string{rightID}, true)
	if err != nil {
		return nil, err
	}
	var out []ev
	for i := range allocs[rightID] {
		a := &allocs[rightID][i]
		if a.LastNight.Before(from) {
			continue
		}
		start := from
		if start.Before(a.FirstNight) {
			start = a.FirstNight
		}
		if err := s.d.Occupancy.TruncateTx(ctx, tx, scope, a.ID, start); err != nil {
			return nil, err
		}
		out = append(out, releasedEvent(a, start, a.LastNight, reason))
	}
	return out, nil
}

// UpdateRight changes the title data or the holders of an active right when its version equals ifMatch
// (rights.manage). Holder shares are checked in the same transaction (INV-40).
func (s *Service) UpdateRight(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in RightPatch) (*Right, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Right
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockBerthRight(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		if r.Status != RightActive {
			return ErrInvalidTransition
		}
		now := s.now()
		diff := map[string]any{}
		if in.Title != nil {
			v := &auth.ValidationError{}
			checkTitle(v, *in.Title)
			if err := v.Err(); err != nil {
				return err
			}
			r.Title = store.MustJSON(*in.Title)
			diff["title"] = true
		}
		if in.Holders != nil {
			if err := checkShares(in.Holders); err != nil {
				return err
			}
			ids := make([]string, len(in.Holders))
			for i, h := range in.Holders {
				ids[i] = h.CustomerID
			}
			if err := checkCustomers(ctx, tx, scope, ids); err != nil {
				return err
			}
			if err := store.ReplaceRightHolders(ctx, tx, scope, r.ID, holderRows(in.Holders, now)); err != nil {
				return err
			}
			diff["holders"] = len(in.Holders)
		}
		r.UpdatedAt = now
		if err := store.UpdateBerthRight(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "right.update", "right", r.ID, diff, changed("right", r.ID, "updated")); err != nil {
			return err
		}
		out, err = s.loadRight(ctx, tx, scope, p, r)
		return err
	})
	return out, err
}

// EndRight ends an active right from the night on (the right keeps the nights before it): the owner occupancy is
// truncated, mandates after on are ended or cancelled (rights.manage).
func (s *Service) EndRight(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, on calendar.Date, reason string) (*Right, error) {
	return s.closeRight(ctx, p, marinaID, id, ifMatch, on, reason, false)
}

// CancelRight cancels a right entered by mistake: every owner allocation, mandate and scheduled holder
// instalment of the right is cancelled (rights.manage).
func (s *Service) CancelRight(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Right, error) {
	return s.closeRight(ctx, p, marinaID, id, ifMatch, calendar.Date{}, reason, true)
}

func (s *Service) closeRight(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, on calendar.Date, reason string, cancel bool) (*Right, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	v := &auth.ValidationError{}
	if reason == "" {
		v.Add("reason", "required")
	} else if len(reason) > 500 {
		v.Add("reason", "too_long")
	}
	if !cancel && on.IsZero() {
		v.Add("on", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *Right
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockBerthRight(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		if r.Status != RightActive {
			return ErrInvalidTransition
		}
		now := s.now()
		var events []ev
		action := "right.cancel"
		if cancel {
			on = r.FromOn
			r.Status = RightCancelled
		} else {
			if !on.After(r.FromOn) || on.After(r.ToOn) {
				v.Add("on", "out_of_period")
				return v
			}
			r.Status, r.ToOn, action = RightEnded, on.AddDays(-1), "right.end"
		}
		rel, err := s.truncateOwner(ctx, tx, scope, r.ID, on, "truncate")
		if err != nil {
			return err
		}
		events = append(events, rel...)
		if err := s.closeMandatesFrom(ctx, tx, scope, r.ID, on, now); err != nil {
			return err
		}
		if cancel {
			if err := s.cancelRightInstallments(ctx, tx, scope, r.ID, calendar.Date{}, now); err != nil {
				return err
			}
		}
		r.EndReason, r.UpdatedAt = reason, now
		if err := store.UpdateBerthRight(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		events = append(events, ev{event.RightEnded, event.RightData{RightID: r.ID, BerthID: r.BerthID, Kind: r.Kind, From: r.FromOn, To: r.ToOn}})
		if err := s.write(ctx, tx, actorOf(p), scope, action, "right", r.ID, map[string]any{"status": r.Status, "to_on": r.ToOn}, events...); err != nil {
			return err
		}
		out, err = s.loadRight(ctx, tx, scope, p, r)
		return err
	})
	return out, err
}

// closeMandatesFrom ends the mandates of a right at night on: a mandate starting on or after on is cancelled, one
// crossing it ends the night before (the owner rows are already gone with the right).
func (s *Service) closeMandatesFrom(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rightID string, on calendar.Date, now time.Time) error {
	ms, err := store.MandatesOfRights(ctx, tx, scope, []string{rightID})
	if err != nil {
		return err
	}
	r, err := store.GetBerthRight(ctx, tx, scope, rightID)
	if err != nil {
		return err
	}
	for i := range ms {
		m := &ms[i]
		if m.Status != MandateActive || m.ToOn.Before(on) {
			continue
		}
		v := m.Version
		if !m.FromOn.Before(on) {
			m.Status = MandateCancelled
		} else {
			m.Status, m.ToOn = MandateEnded, on.AddDays(-1)
		}
		m.EndedOn, m.UpdatedAt = on, now
		if err := store.UpdateMandate(ctx, tx, scope, m, v); err != nil {
			return err
		}
		if err := s.write(ctx, tx, audit.System, scope, "mandate.end", "mandate", m.ID, map[string]any{"status": m.Status, "to_on": m.ToOn},
			ev{event.MandateEnded, event.MandateData{MandateID: m.ID, RightID: rightID, BerthID: r.BerthID, From: m.FromOn, To: m.ToOn}}); err != nil {
			return err
		}
	}
	return nil
}

// RightVesselsInput replaces the authorised vessels; FromNight (default today) is the first night carried by a
// new primary vessel.
type RightVesselsInput struct {
	Vessels     []VesselInput
	FromNight   calendar.Date
	FitOverride string
}

// SetRightVessels replaces the authorised vessels of an active right. When the primary vessel changes, the owner
// nights from FromNight are re-allocated with the new vessel (Truncate + AllocateOwner, no moved_from_id).
func (s *Service) SetRightVessels(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in RightVesselsInput) (*Right, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if in.FitOverride != "" && !can(p, scope, auth.PermOccupancyOverrideFit) {
		return nil, auth.ErrForbidden
	}
	var out *Right
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockBerthRight(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		if r.Status != RightActive {
			return ErrInvalidTransition
		}
		now := s.now()
		today := s.today(ctx, tx, scope)
		from := in.FromNight
		if from.IsZero() {
			from = today
		}
		if from.Before(today) {
			v := &auth.ValidationError{}
			v.Add("from_night", "in_the_past")
			return v
		}
		oldPrimary, err := primaryOf(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		vrows, primary, err := vesselRows(ctx, tx, scope, in.Vessels, now)
		if err != nil {
			return err
		}
		var events []ev
		if primary != oldPrimary {
			var override *occupancy.FitOverride
			if in.FitOverride != "" {
				override = occupancy.NewFitOverride(p, in.FitOverride)
			}
			allocs, err := store.AllocationsOfRights(ctx, tx, scope, []string{r.ID}, true)
			if err != nil {
				return err
			}
			for i := range allocs[r.ID] {
				a := &allocs[r.ID][i]
				if a.LastNight.Before(from) {
					continue
				}
				start := from
				if start.Before(a.FirstNight) {
					start = a.FirstNight
				}
				if err := s.d.Occupancy.TruncateTx(ctx, tx, scope, a.ID, start); err != nil {
					return err
				}
				na, err := s.allocateOwner(ctx, tx, scope, r, primary, start, a.LastNight, override, userOf(p))
				if err != nil {
					return err
				}
				if na != nil && len(na.FitOverride) > 0 {
					for j := range vrows {
						if vrows[j].IsPrimary {
							vrows[j].FitOverride = na.FitOverride
						}
					}
				}
				events = append(events, releasedEvent(a, start, a.LastNight, "truncate"), allocatedEvent(na))
			}
		}
		if err := store.ReplaceRightVessels(ctx, tx, scope, r.ID, vrows); err != nil {
			return err
		}
		r.UpdatedAt = now
		if err := store.UpdateBerthRight(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		events = append(events, changed("right", r.ID, "vessels"))
		diff := map[string]any{"vessels": len(vrows), "primary_changed": primary != oldPrimary, "from_night": from}
		if err := s.write(ctx, tx, actorOf(p), scope, "right.vessels", "right", r.ID, diff, events...); err != nil {
			return err
		}
		out, err = s.loadRight(ctx, tx, scope, p, r)
		return err
	})
	return out, err
}

// HistoryEntry is one right in the holder chain of a berth.
type HistoryEntry struct {
	Right      Right
	TransferID string // the transfer that created the right
}

// History is the holder chain of a berth.
type History struct {
	BerthID   string
	BerthCode string
	Entries   []HistoryEntry
}

// RightHistory returns every right of a berth with its holders, oldest first (rights.manage).
func (s *Service) RightHistory(ctx context.Context, p *auth.Principal, marinaID, berthID string) (*History, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *History
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = s.historyTx(ctx, tx, scope, p, berthID)
		return err
	})
	return out, err
}

func (s *Service) historyTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, berthID string) (*History, error) {
	b, err := store.BerthByID(ctx, tx, scope, berthID)
	if err != nil {
		return nil, err
	}
	rows, err := store.RightsOfBerth(ctx, tx, scope, b.ID)
	if err != nil {
		return nil, err
	}
	rs, err := s.loadRights(ctx, tx, scope, p, rows)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	tr, err := store.CompletedTransfersOfRights(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	h := &History{BerthID: b.ID, BerthCode: b.Code, Entries: make([]HistoryEntry, len(rs))}
	for i := range rs {
		h.Entries[i] = HistoryEntry{Right: rs[i], TransferID: tr[rs[i].ID]}
	}
	return h, nil
}

// Lens statuses of the map rights endpoint.
const (
	LensMarina  = "marina"
	LensHolder  = "holder"
	LensMandate = "mandate"
)

// MapRightItem is the rights status of a berth on a date (no names).
type MapRightItem struct {
	BerthID    string
	RightID    string
	MandateID  string
	Status     string
	UnpaidFees bool
}

// MapRights returns the rights lens of a map on a date (default today): per berth marina, holder or mandate and
// whether the right has unpaid overdue fees (map.view; no personal data).
func (s *Service) MapRights(ctx context.Context, p *auth.Principal, marinaID, mapID string, on calendar.Date) (calendar.Date, []MapRightItem, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return calendar.Date{}, nil, err
	}
	var out []MapRightItem
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetMap(ctx, tx, scope, mapID); err != nil {
			return err
		}
		if on.IsZero() {
			on = s.today(ctx, tx, scope)
		}
		res, err := store.LoadMapResources(ctx, tx, scope, mapID, false)
		if err != nil {
			return err
		}
		ids := make([]string, len(res.Berths))
		for i := range res.Berths {
			ids[i] = res.Berths[i].ID
		}
		rights, err := store.RightsActiveOn(ctx, tx, scope, on, ids)
		if err != nil {
			return err
		}
		byBerth := map[string]*store.BerthRight{}
		rids := make([]string, 0, len(rights))
		for i := range rights {
			byBerth[rights[i].BerthID] = &rights[i]
			rids = append(rids, rights[i].ID)
		}
		ms, err := store.MandatesOfRights(ctx, tx, scope, rids)
		if err != nil {
			return err
		}
		mandateOf := map[string]string{}
		for _, m := range ms {
			ended := m.Status == MandateEnded && !m.EndedOn.IsZero() && !on.Before(m.EndedOn)
			if m.Status != MandateCancelled && !ended && !on.Before(m.FromOn) && !on.After(m.ToOn) {
				mandateOf[m.RightID] = m.ID
			}
		}
		unpaid, err := store.RightsWithUnpaidFees(ctx, tx, scope, s.today(ctx, tx, scope))
		if err != nil {
			return err
		}
		out = make([]MapRightItem, 0, len(res.Berths))
		for _, b := range res.Berths {
			it := MapRightItem{BerthID: b.ID, Status: LensMarina}
			if r := byBerth[b.ID]; r != nil {
				it.RightID, it.Status, it.UnpaidFees = r.ID, LensHolder, unpaid[r.ID]
				if m := mandateOf[r.ID]; m != "" {
					it.MandateID, it.Status = m, LensMandate
				}
			}
			out = append(out, it)
		}
		return nil
	})
	return on, out, err
}

// Filters of the list operations, re-exported for the HTTP layer (handlers do not import store).
type (
	TransferFilter         = store.TransferFilter
	MandateFilter          = store.MandateFilter
	CommonAllocationFilter = store.AllocationFilterCommon
)
