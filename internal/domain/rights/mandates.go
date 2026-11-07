package rights

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Management mandates (spec 14.3; m4-plan §4.3, §15 D16): the owner nights of the mandate period are removed so
// the berth is sellable; the revenue of every transit or contract sold on the berth inside the period is spread
// by night (accrual) and attributed to the mandate with the owner share. Overlapping mandates of a right are
// refused under the per-right lock (berth_rights row lock + version bump).

// Mandate channels and settlements.
var (
	MandateChannels    = []string{"transit", "period"}
	MandateSettlements = []string{"credit", "transfer"}
)

// Mandate is a mandate with its berth and accrued revenue.
type Mandate struct {
	store.ManagementMandate
	BerthID         string
	RevenueCents    int64
	OwnerShareCents int64
	SoldNightsKept  int
}

// MandateInput are the fields of a new mandate.
type MandateInput struct {
	RightID            string
	FromOn, ToOn       calendar.Date
	OwnerShareBP       int64
	MinGuaranteedCents int64
	Channels           []string
	Settlement         string
}

func (s *Service) mandateViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, ms []store.ManagementMandate) ([]Mandate, error) {
	ids := make([]string, len(ms))
	rids := map[string]bool{}
	for i := range ms {
		ids[i] = ms[i].ID
		rids[ms[i].RightID] = true
	}
	revs, err := store.RevenuesOfMandates(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	berth := map[string]string{}
	for id := range rids {
		r, err := store.GetBerthRight(ctx, db, scope, id)
		if err != nil {
			return nil, err
		}
		berth[id] = r.BerthID
	}
	out := make([]Mandate, len(ms))
	for i, m := range ms {
		out[i] = Mandate{ManagementMandate: m, BerthID: berth[m.RightID]}
		for _, r := range revs {
			if r.MandateID == m.ID {
				out[i].RevenueCents += r.RevenueCents
				out[i].OwnerShareCents += r.OwnerShareCents
			}
		}
	}
	return out, nil
}

// ListMandates returns a page of mandates (rights.manage).
func (s *Service) ListMandates(ctx context.Context, p *auth.Principal, marinaID string, f store.MandateFilter, cursor string, limit int) (store.Page[Mandate], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[Mandate]{}, err
	}
	if f.Status != "" && !slices.Contains([]string{MandateActive, MandateEnded, MandateCancelled}, f.Status) {
		return store.Page[Mandate]{}, invalidField("status", "invalid")
	}
	var out store.Page[Mandate]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		page, err := store.ListMandates(ctx, tx, scope, f, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.mandateViews(ctx, tx, scope, page.Items)
		out = store.Page[Mandate]{Items: items, NextCursor: page.NextCursor}
		return err
	})
	return out, err
}

// GetMandate returns a mandate (rights.manage).
func (s *Service) GetMandate(ctx context.Context, p *auth.Principal, marinaID, id string) (*Mandate, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	m, err := store.GetMandate(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	vs, err := s.mandateViews(ctx, s.d.DB.DB, scope, []store.ManagementMandate{*m})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// CreateMandate creates a mandate on an active right for nights ≥ today: the owner rows of the period are removed
// (Truncate + AllocateOwner after the period). An overlapping mandate of the right is 409 mandate_overlap
// (rights.manage).
func (s *Service) CreateMandate(ctx context.Context, p *auth.Principal, marinaID string, in MandateInput) (*Mandate, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.FromOn.IsZero() || in.ToOn.IsZero() || in.ToOn.Before(in.FromOn) {
		v.Add("to_on", "before_from_on")
	}
	if in.OwnerShareBP < 0 || in.OwnerShareBP > 10_000 {
		v.Add("owner_share_bp", "out_of_range")
	}
	if in.MinGuaranteedCents < 0 {
		v.Add("min_guaranteed_cents", "out_of_range")
	}
	if len(in.Channels) == 0 {
		v.Add("channels", "required")
	}
	for _, c := range in.Channels {
		if !slices.Contains(MandateChannels, c) {
			v.Add("channels", "invalid")
		}
	}
	if !slices.Contains(MandateSettlements, in.Settlement) {
		v.Add("settlement", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *Mandate
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockBerthRight(ctx, tx, scope, in.RightID)
		if err != nil {
			return err
		}
		if r.Status != RightActive {
			return ErrInvalidTransition
		}
		today := s.today(ctx, tx, scope)
		if in.FromOn.Before(today) {
			return invalidField("from_on", "in_the_past")
		}
		if in.FromOn.Before(r.FromOn) || in.ToOn.After(r.ToOn) {
			return invalidField("to_on", "outside_right")
		}
		over, err := store.OverlappingMandates(ctx, tx, scope, r.ID, in.FromOn, in.ToOn, "")
		if err != nil {
			return err
		}
		for _, o := range over {
			if o.Status == MandateEnded && !o.EndedOn.IsZero() && !o.EndedOn.After(in.FromOn) {
				continue // ended before the new period starts
			}
			return ErrMandateOverlap
		}
		now := s.now()
		events, err := s.removeOwnerNights(ctx, tx, scope, r, in.FromOn, in.ToOn)
		if err != nil {
			return err
		}
		m := &store.ManagementMandate{
			ID: store.NewID(), RightID: r.ID, FromOn: in.FromOn, ToOn: in.ToOn, OwnerShareBP: in.OwnerShareBP,
			MinGuaranteedCents: in.MinGuaranteedCents, Channels: store.Strings(in.Channels), Settlement: in.Settlement,
			Status: MandateActive, CreatedByUserID: userOf(p), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertMandate(ctx, tx, scope, m); err != nil {
			return err
		}
		// the per-right lock: two mandates of one right serialise on the right row and its version
		r.UpdatedAt = now
		if err := store.UpdateBerthRight(ctx, tx, scope, r, r.Version); err != nil {
			return err
		}
		events = append(events, ev{event.MandateCreated, event.MandateData{MandateID: m.ID, RightID: r.ID, BerthID: r.BerthID, From: m.FromOn, To: m.ToOn}})
		if err := s.write(ctx, tx, actorOf(p), scope, "mandate.create", "mandate", m.ID,
			map[string]any{"right_id": r.ID, "from_on": m.FromOn, "to_on": m.ToOn, "owner_share_bp": m.OwnerShareBP}, events...); err != nil {
			return err
		}
		out = &Mandate{ManagementMandate: *m, BerthID: r.BerthID}
		return nil
	})
	return out, err
}

// removeOwnerNights removes the owner rows of a right on [from, to] and links the tail allocations to the right.
func (s *Service) removeOwnerNights(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.BerthRight, from, to calendar.Date) ([]ev, error) {
	allocs, err := store.AllocationsOfRights(ctx, tx, scope, []string{r.ID}, true)
	if err != nil {
		return nil, err
	}
	var events []ev
	for i := range allocs[r.ID] {
		a := &allocs[r.ID][i]
		if a.LastNight.Before(from) || a.FirstNight.After(to) {
			continue
		}
		tail, err := s.d.Occupancy.RemoveOwnerNightsTx(ctx, tx, scope, s.now(), a.ID, from, to)
		if err != nil {
			return nil, err
		}
		lo, hi := maxDate(from, a.FirstNight), minDate(to, a.LastNight)
		events = append(events, releasedEvent(a, lo, hi, "release"))
		if tail != nil {
			if err := store.LinkRightAllocation(ctx, tx, scope, r.ID, tail.ID, s.now()); err != nil {
				return nil, err
			}
			events = append(events, allocatedEvent(tail))
		}
	}
	return events, nil
}

func maxDate(a, b calendar.Date) calendar.Date {
	if a.After(b) {
		return a
	}
	return b
}

func minDate(a, b calendar.Date) calendar.Date {
	if a.Before(b) {
		return a
	}
	return b
}

// EndMandate ends an active mandate from the night on: the nights [on, to_on] still free go back to the owner,
// nights already sold stay sold (counted in SoldNightsKept) (rights.manage).
func (s *Service) EndMandate(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, on calendar.Date, reason string) (*Mandate, error) {
	return s.closeMandate(ctx, p, marinaID, id, ifMatch, on, reason, false)
}

// CancelMandate cancels an active mandate: every free night of its period from today goes back to the owner
// (rights.manage).
func (s *Service) CancelMandate(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Mandate, error) {
	return s.closeMandate(ctx, p, marinaID, id, ifMatch, calendar.Date{}, reason, true)
}

func (s *Service) closeMandate(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, on calendar.Date, reason string, cancel bool) (*Mandate, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(reason)) > 500 {
		return nil, invalidField("reason", "too_long")
	}
	if !cancel && on.IsZero() {
		return nil, invalidField("on", "required")
	}
	var out *Mandate
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMandate(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		r, err := store.LockBerthRight(ctx, tx, scope, m.RightID)
		if err != nil {
			return err
		}
		if m.Version != ifMatch {
			return ErrVersionMismatch
		}
		if m.Status != MandateActive {
			return ErrInvalidTransition
		}
		today := s.today(ctx, tx, scope)
		start := on
		action := "mandate.end"
		if cancel {
			start, action = m.FromOn, "mandate.cancel"
		} else if !on.After(m.FromOn) || on.After(m.ToOn) {
			return invalidField("on", "out_of_period")
		}
		if start.Before(today) {
			start = today
		}
		last := minDate(m.ToOn, r.ToOn)
		kept := 0
		var events []ev
		if r.Status == RightActive && !last.Before(start) {
			events, kept, err = s.restoreOwnerNights(ctx, tx, scope, r, start, last)
			if err != nil {
				return err
			}
		}
		now := s.now()
		v := m.Version
		// to_on stays: the nights sold under the mandate before its end stay attributed to it (revenue, Q17);
		// ended_on is the first night given back to the owner.
		if cancel {
			m.Status, m.EndedOn = MandateCancelled, maxDate(start, m.FromOn)
		} else {
			m.Status, m.EndedOn = MandateEnded, on
		}
		m.UpdatedAt = now
		if err := store.UpdateMandate(ctx, tx, scope, m, v); err != nil {
			return err
		}
		events = append(events, ev{event.MandateEnded, event.MandateData{MandateID: m.ID, RightID: r.ID, BerthID: r.BerthID, From: m.FromOn, To: m.ToOn}})
		if err := s.write(ctx, tx, actorOf(p), scope, action, "mandate", m.ID,
			map[string]any{"status": m.Status, "to_on": m.ToOn, "sold_nights_kept": kept}, events...); err != nil {
			return err
		}
		vs, err := s.mandateViews(ctx, tx, scope, []store.ManagementMandate{*m})
		if err != nil {
			return err
		}
		vs[0].SoldNightsKept = kept
		out = &vs[0]
		return nil
	})
	return out, err
}

// restoreOwnerNights allocates owner rows on the free nights of [from, to] of a right's berth and returns the
// events and the number of nights that stay sold.
func (s *Service) restoreOwnerNights(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.BerthRight, from, to calendar.Date) ([]ev, int, error) {
	taken, err := store.ListAllocations(ctx, tx, scope, store.AllocationFilter{
		ResourceType: store.ResourceBerth, ResourceIDs: []string{r.BerthID}, Status: store.AllocActive, From: from, To: to,
	})
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	type span struct{ first, last calendar.Date }
	var busy []span
	for _, a := range taken {
		if a.Kind == store.AllocKindHold && !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(now) {
			continue // expired holds are reclaimed by the allocation (INV-05)
		}
		busy = append(busy, span{maxDate(a.FirstNight, from), minDate(a.LastNight, to)})
	}
	slices.SortFunc(busy, func(a, b span) int { return a.first.Compare(b.first) })
	vessel, err := primaryOf(ctx, tx, scope, r.ID)
	if err != nil {
		return nil, 0, err
	}
	var events []ev
	kept := 0
	next := from
	free := func(a, b calendar.Date) error {
		if b.Before(a) {
			return nil
		}
		na, err := s.allocateOwner(ctx, tx, scope, r, vessel, a, b, nil, "")
		if err != nil {
			return err
		}
		events = append(events, allocatedEvent(na))
		return nil
	}
	for _, b := range busy {
		if b.last.Before(next) {
			continue
		}
		if b.first.After(next) {
			if err := free(next, b.first.AddDays(-1)); err != nil {
				return nil, 0, err
			}
		}
		lo := maxDate(b.first, next)
		kept += lo.DaysUntil(b.last) + 1
		next = b.last.AddDays(1)
	}
	if err := free(next, to); err != nil {
		return nil, 0, err
	}
	return events, kept, nil
}

// ---- revenues ----

// AccrueMandates runs the revenue accrual of the marina now (rights.manage) and returns the rows that changed.
func (s *Service) AccrueMandates(ctx context.Context, p *auth.Principal, marinaID string) (int, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return 0, err
	}
	return s.accrueMarina(ctx, scope, actorOf(p))
}

// RunAccrual is the mandates.accrue job: once per marina day after 02:00 marina time it accrues the revenues of
// every marina with mandates (idempotent: unchanged rows are not written).
func (s *Service) RunAccrual(ctx context.Context) error {
	ids, err := store.MarinasWithMandates(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		loc := s.location(ctx, s.d.DB.DB, scope)
		local := s.d.Clock.Now().In(loc)
		day := calendar.In(local, loc)
		if local.Hour() < 2 {
			continue
		}
		s.mu.Lock()
		done := s.accrued[id].Equal(day)
		s.mu.Unlock()
		if done {
			continue
		}
		if _, err := s.accrueMarina(ctx, scope, audit.System); err != nil {
			s.d.Log.Warn("mandates.accrue", "marina_id", id, "error", err)
			continue
		}
		s.mu.Lock()
		s.accrued[id] = day
		s.mu.Unlock()
	}
	return nil
}

// accrueMarina recomputes the revenue rows of the marina's mandates, one transaction per mandate.
func (s *Service) accrueMarina(ctx context.Context, scope store.MarinaScope, actor audit.Actor) (int, error) {
	today := s.today(ctx, s.d.DB.DB, scope)
	ms, err := store.MandatesToAccrue(ctx, s.d.DB.DB, scope, today.AddDays(-60))
	if err != nil {
		return 0, err
	}
	total := 0
	for i := range ms {
		n := 0
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			m, err := store.GetMandate(ctx, tx, scope, ms[i].ID)
			if err != nil {
				return err
			}
			n, err = s.accrueTx(ctx, tx, scope, actor, m)
			return err
		})
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// revenueLine is the revenue of one sold allocation inside a mandate period.
type revenueLine struct {
	sold       store.SoldAllocation
	first      calendar.Date
	last       calendar.Date
	nights     int
	revenue    money.Cents
	ownerShare money.Cents
}

// revenueLines spreads the total of every contract sold on the berth by night (accrual, INV-60) and keeps the
// nights inside [from, to] of the mandate.
func revenueLines(m *store.ManagementMandate, sold []store.SoldAllocation, from, to calendar.Date) ([]revenueLine, error) {
	if m.Status == MandateCancelled {
		return nil, nil
	}
	lo, hi := maxDate(from, m.FromOn), minDate(to, m.ToOn)
	var out []revenueLine
	for _, a := range sold {
		first, last := maxDate(lo, a.First), minDate(hi, a.Last)
		if last.Before(first) {
			continue
		}
		line := accrual.Line{ID: a.AllocationID, Amount: money.Cents(a.TotalCents), From: a.StartsOn, To: a.EndsOn.AddDays(-1)}
		if line.Days() == 0 {
			continue
		}
		rev, err := accrual.Within(line, first, last)
		if err != nil {
			return nil, err
		}
		out = append(out, revenueLine{sold: a, first: first, last: last, nights: first.DaysUntil(last) + 1, revenue: rev, ownerShare: rev.PercentBP(m.OwnerShareBP)})
	}
	return out, nil
}

// accrueTx upserts the revenue rows of one mandate; rows of allocations no longer sold go to zero.
func (s *Service) accrueTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, m *store.ManagementMandate) (int, error) {
	r, err := store.GetBerthRight(ctx, tx, scope, m.RightID)
	if err != nil {
		return 0, err
	}
	sold, err := store.SoldAllocationsOn(ctx, tx, scope, r.BerthID, m.FromOn, m.ToOn)
	if err != nil {
		return 0, err
	}
	lines, err := revenueLines(m, sold, m.FromOn, m.ToOn)
	if err != nil {
		return 0, err
	}
	existing, err := store.RevenuesOfMandates(ctx, tx, scope, []string{m.ID})
	if err != nil {
		return 0, err
	}
	now := s.now()
	changedRows := 0
	seen := map[string]bool{}
	var rev, share int64
	through := m.FromOn
	for _, l := range lines {
		seen[l.sold.AllocationID] = true
		row := &store.MandateRevenue{
			ID: store.NewID(), MandateID: m.ID, AllocationID: l.sold.AllocationID, ContractID: l.sold.ContractID, Nights: l.nights,
			RevenueCents: int64(l.revenue), OwnerShareCents: int64(l.ownerShare), ThroughOn: l.last, CreatedAt: now, UpdatedAt: now,
		}
		ch, err := store.UpsertMandateRevenue(ctx, tx, scope, row)
		if err != nil {
			return 0, err
		}
		if ch {
			changedRows++
		}
		rev += int64(l.revenue)
		share += int64(l.ownerShare)
		if l.last.After(through) {
			through = l.last
		}
	}
	for _, e := range existing {
		if seen[e.AllocationID] || (e.Nights == 0 && e.RevenueCents == 0) {
			continue
		}
		row := &store.MandateRevenue{MandateID: m.ID, AllocationID: e.AllocationID, ContractID: e.ContractID, ThroughOn: e.ThroughOn, UpdatedAt: now}
		ch, err := store.UpsertMandateRevenue(ctx, tx, scope, row)
		if err != nil {
			return 0, err
		}
		if ch {
			changedRows++
		}
	}
	if changedRows == 0 {
		return 0, nil
	}
	return changedRows, s.write(ctx, tx, actor, scope, "mandate.accrue", "mandate", m.ID,
		map[string]any{"rows": changedRows, "revenue_cents": rev, "owner_share_cents": share},
		ev{event.MandateRevenueAccrued, event.MandateRevenueData{MandateID: m.ID, Rows: changedRows, RevenueCents: rev, OwnerShareCents: share, Through: through}})
}

// StatementLine is one sold allocation of a mandate statement.
type StatementLine struct {
	AllocationID    string
	ContractID      string
	ContractKind    string
	ContractTerm    string
	ContractStatus  string
	ContractCode    string
	FirstNight      calendar.Date
	LastNight       calendar.Date
	Nights          int
	RevenueCents    int64
	OwnerShareCents int64
}

// Statement is the revenue statement of a mandate for [From, To].
type Statement struct {
	Mandate              Mandate
	From, To             calendar.Date
	Lines                []StatementLine
	RevenueCents         int64
	OwnerShareCents      int64
	GuaranteedTopUpCents int64
}

// MandateStatement returns the statement of a mandate (default: its whole period) with the guaranteed minimum
// shown as max(0, min − Σ owner share) (rights.manage).
func (s *Service) MandateStatement(ctx context.Context, p *auth.Principal, marinaID, id string, from, to calendar.Date) (*Statement, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Statement
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		out, err = s.statementTx(ctx, tx, scope, id, from, to)
		return err
	})
	return out, err
}

func (s *Service) statementTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, from, to calendar.Date) (*Statement, error) {
	m, err := store.GetMandate(ctx, tx, scope, id)
	if err != nil {
		return nil, err
	}
	if from.IsZero() {
		from = m.FromOn
	}
	if to.IsZero() {
		to = m.ToOn
	}
	if to.Before(from) {
		return nil, invalidField("to", "before_from")
	}
	vs, err := s.mandateViews(ctx, tx, scope, []store.ManagementMandate{*m})
	if err != nil {
		return nil, err
	}
	sold, err := store.SoldAllocationsOn(ctx, tx, scope, vs[0].BerthID, maxDate(from, m.FromOn), minDate(to, m.ToOn))
	if err != nil {
		return nil, err
	}
	lines, err := revenueLines(m, sold, from, to)
	if err != nil {
		return nil, err
	}
	st := &Statement{Mandate: vs[0], From: from, To: to, Lines: []StatementLine{}}
	for _, l := range lines {
		st.Lines = append(st.Lines, StatementLine{
			AllocationID: l.sold.AllocationID, ContractID: l.sold.ContractID, ContractKind: l.sold.ContractKind, ContractTerm: l.sold.Term,
			ContractStatus: l.sold.ContractStat, ContractCode: contractCode(l.sold.ContractKind, l.sold.ContractYear, l.sold.ContractNo),
			FirstNight: l.first, LastNight: l.last, Nights: l.nights, RevenueCents: int64(l.revenue), OwnerShareCents: int64(l.ownerShare),
		})
		st.RevenueCents += int64(l.revenue)
		st.OwnerShareCents += int64(l.ownerShare)
	}
	if top := m.MinGuaranteedCents - st.OwnerShareCents; top > 0 {
		st.GuaranteedTopUpCents = top
	}
	return st, nil
}

// contractCode is the display code of a contract (ORM-2027-0001, TRN-2027-0001), as the contract package prints it.
func contractCode(kind string, year int, number int64) string {
	prefix := "ORM"
	if kind == "transit" {
		prefix = "TRN"
	}
	return fmt.Sprintf("%s-%04d-%04d", prefix, year, number)
}
