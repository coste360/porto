package contract

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/store"
)

// Read models of contracts and transits. Customer display names are filled only when the caller has
// customers.read; the confirmation reference only with contracts.write or transits.desk (m3-plan §7.1).

// Party is a customer as embedded in views (Name empty without customers.read).
type Party struct {
	ID   string
	Name string
}

// Detail is a contract or transit with its lines, allocations, releases and presences.
type Detail struct {
	Row             store.Contract
	Code            string
	Lines           []store.ContractLine
	Allocations     []store.Allocation // every status, ordered by first night
	Releases        []store.Release
	Presences       []store.Presence
	Vessel          store.Vessel
	Customer        *Party
	BillingCustomer *Party
	Resources       map[string]store.ResourceRef
	Snapshot        *PriceSnapshot
	HoldExpiresAt   time.Time // earliest expiry of an active hold
	ShowRef         bool
	ShowNotes       bool
	PresenceState   string
}

// Summary is a contract or transit in a list.
type Summary struct {
	Row           store.Contract
	Code          string
	Vessel        store.Vessel
	Customer      *Party
	Resources     []store.ResourceRef // current resources of the active allocations
	HoldExpiresAt time.Time
	PresenceState string
}

func seriesOf(kind string) string {
	if kind == KindTransit {
		return SeriesTransit
	}
	return SeriesMooring
}

// Code returns the display code of a contract row.
func Code(c *store.Contract) string { return ContractCode(seriesOf(c.Kind), c.Year, c.Number) }

func (s *Service) can(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission) bool {
	_, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	return err == nil
}

// parties loads customers and returns them as parties (names only when withNames).
func parties(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string, withNames bool) (map[string]Party, error) {
	out := map[string]Party{}
	cs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		pt := Party{ID: cs[i].ID}
		if withNames {
			pt.Name = customer.DisplayName(&cs[i])
		}
		out[cs[i].ID] = pt
	}
	return out, nil
}

func partyOf(m map[string]Party, id string) *Party {
	if id == "" {
		return nil
	}
	if p, ok := m[id]; ok {
		return &p
	}
	return &Party{ID: id}
}

func holdExpiry(as []store.Allocation) time.Time {
	var t time.Time
	for _, a := range as {
		if a.Status == store.AllocActive && a.Kind == store.AllocKindHold && !a.ExpiresAt.IsZero() && (t.IsZero() || a.ExpiresAt.Before(t)) {
			t = a.ExpiresAt
		}
	}
	return t
}

// detail loads the view of a contract in db.
func (s *Service) detail(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Detail, error) {
	c, err := store.GetContract(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	lines, err := store.LinesOfContracts(ctx, db, scope, []string{c.ID})
	if err != nil {
		return nil, err
	}
	allocs, err := contractAllocs(ctx, db, scope, c.ID)
	if err != nil {
		return nil, err
	}
	releases, err := store.ReleasesOfAllocations(ctx, db, scope, idsOf(allocs))
	if err != nil {
		return nil, err
	}
	presences, err := store.PresencesOfContracts(ctx, db, scope, []string{c.ID})
	if err != nil {
		return nil, err
	}
	v, err := store.GetVessel(ctx, db, scope, c.VesselID)
	if err != nil {
		return nil, err
	}
	withNames := s.can(ctx, p, scope.ID(), auth.PermCustomersRead)
	ps, err := parties(ctx, db, scope, []string{c.CustomerID, c.BillingCustomerID}, withNames)
	if err != nil {
		return nil, err
	}
	var rids []string
	for _, l := range lines {
		rids = append(rids, l.ResourceID)
	}
	for _, a := range allocs {
		rids = append(rids, a.ResourceID)
	}
	for _, r := range releases {
		rids = append(rids, r.ResourceID)
	}
	refs, err := store.ResourceRefs(ctx, db, scope, rids)
	if err != nil {
		return nil, err
	}
	d := &Detail{
		Row: *c, Code: Code(c), Lines: lines, Allocations: allocs, Releases: releases, Presences: presences, Vessel: *v,
		Customer: partyOf(ps, c.CustomerID), BillingCustomer: partyOf(ps, c.BillingCustomerID), Resources: refs,
		Snapshot: DecodeSnapshot(c.PriceSnapshot), HoldExpiresAt: holdExpiry(allocs), ShowNotes: withNames,
		ShowRef: s.can(ctx, p, scope.ID(), auth.PermContractsWrite) || s.can(ctx, p, scope.ID(), auth.PermTransitsDesk),
	}
	if vp, err := store.GetVesselPresence(ctx, db, scope, c.VesselID); err == nil {
		d.PresenceState = vp.State
	}
	return d, nil
}

// GetContract returns a contract or a transit (contracts.read).
func (s *Service) GetContract(ctx context.Context, p *auth.Principal, marinaID, id string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// ListFilter narrows ListContracts and ListTransits.
type ListFilter struct {
	Kind        string // listContracts: default mooring
	Status      string
	Term        string
	CustomerID  string
	VesselID    string
	ActiveOn    *calendar.Date
	ArrivalFrom *calendar.Date
	ArrivalTo   *calendar.Date
	Search      string
}

// ListContracts returns a page of mooring contracts (contracts.read).
func (s *Service) ListContracts(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, cursor string, limit int) (store.Page[Summary], error) {
	if f.Kind == "" {
		f.Kind = KindMooring
	}
	if f.Kind == KindTransit {
		v := &auth.ValidationError{}
		v.Add("kind", "invalid")
		return store.Page[Summary]{}, v
	}
	return s.list(ctx, p, marinaID, f, cursor, limit)
}

// ListTransits returns a page of transits (contracts.read).
func (s *Service) ListTransits(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, cursor string, limit int) (store.Page[Summary], error) {
	f.Kind = KindTransit
	return s.list(ctx, p, marinaID, f, cursor, limit)
}

func (s *Service) list(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, cursor string, limit int) (store.Page[Summary], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	sf := store.ContractFilter{
		Kind: f.Kind, Status: f.Status, Term: f.Term, CustomerID: f.CustomerID, VesselID: f.VesselID, ActiveOn: f.ActiveOn,
		ArrivalFrom: f.ArrivalFrom, ArrivalTo: f.ArrivalTo,
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		sf.Search = customer.Fold(q)
		if n, err := strconv.ParseInt(lastDigits(q), 10, 64); err == nil {
			sf.Number = n
		}
	}
	db := s.d.DB.DB
	page, err := store.ListContracts(ctx, db, scope, sf, cursor, limit)
	if err != nil {
		return store.Page[Summary]{}, notFound(err)
	}
	out, err := s.summaries(ctx, db, scope, p, page.Items)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	return store.Page[Summary]{Items: out, NextCursor: page.NextCursor}, nil
}

// lastDigits returns the trailing digits of a code like TRN-2026-0042 ("" when none).
func lastDigits(s string) string {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[i:]
}

func (s *Service) summaries(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.Contract) ([]Summary, error) {
	ids := make([]string, 0, len(rows))
	var vids, cids []string
	for i := range rows {
		ids = append(ids, rows[i].ID)
		vids = append(vids, rows[i].VesselID)
		cids = append(cids, rows[i].CustomerID)
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = vs[i]
	}
	ps, err := parties(ctx, db, scope, cids, s.can(ctx, p, scope.ID(), auth.PermCustomersRead))
	if err != nil {
		return nil, err
	}
	allocs, err := store.AllocationsOfContracts(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byContract := map[string][]store.Allocation{}
	var rids []string
	for _, a := range allocs {
		byContract[a.ContractID] = append(byContract[a.ContractID], a.Allocation)
		rids = append(rids, a.ResourceID)
	}
	refs, err := store.ResourceRefs(ctx, db, scope, rids)
	if err != nil {
		return nil, err
	}
	pres, err := store.VesselPresences(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(rows))
	for i := range rows {
		c := rows[i]
		sm := Summary{
			Row: c, Code: Code(&c), Vessel: vm[c.VesselID], Customer: partyOf(ps, c.CustomerID),
			HoldExpiresAt: holdExpiry(byContract[c.ID]), PresenceState: pres[c.VesselID].State,
		}
		seen := map[string]bool{}
		for _, a := range byContract[c.ID] {
			if a.Status != store.AllocActive || seen[a.ResourceID] {
				continue
			}
			seen[a.ResourceID] = true
			ref := refs[a.ResourceID]
			if ref.ID == "" {
				ref = store.ResourceRef{Type: a.ResourceType, ID: a.ResourceID, MapID: a.MapID}
			}
			sm.Resources = append(sm.Resources, ref)
		}
		out = append(out, sm)
	}
	return out, nil
}
