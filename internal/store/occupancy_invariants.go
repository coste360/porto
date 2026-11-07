package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Violation is a row (or group of rows) that breaks an occupancy invariant query of m3-plan §6.
type Violation struct {
	Query  string   // Q1..Q16
	IDs    []string // allocation, release, contract or resource ids involved
	Detail string   // ids, dates and codes only (no personal data)
}

// PlanCheck runs the invariant loops that need the pure engines (Q14 fit or override, Q15 rows = plan). The
// store does not import domain packages: internal/domain/occupancy registers its implementation at init.
type PlanCheck func(ctx context.Context, db bun.IDB, marinaID string, today calendar.Date) ([]Violation, error)

var (
	planCheckMu sync.RWMutex
	planCheck   PlanCheck
)

// RegisterPlanCheck installs the Q14/Q15 check (called by internal/domain/occupancy).
func RegisterPlanCheck(fn PlanCheck) {
	planCheckMu.Lock()
	defer planCheckMu.Unlock()
	planCheck = fn
}

// CheckOccupancyInvariants runs the invariant queries Q1–Q16 of m3-plan §6 on a marina and returns every
// violation (none on a coherent database). Used by tests, the season simulation and the demo seed check. Q11
// (no expired hold rows after the job) depends on the instant: see CountExpiredHoldRows. Q14 and Q15 run when
// internal/domain/occupancy is linked in (it registers them); without it they are reported as a violation, so a
// caller can never believe they passed.
func CheckOccupancyInvariants(ctx context.Context, db bun.IDB, marinaID string, today calendar.Date) ([]Violation, error) {
	var out []Violation
	add := func(q string, detail string, ids ...string) {
		out = append(out, Violation{Query: q, IDs: ids, Detail: detail})
	}
	type keyRow struct {
		Resource string        `bun:"resource"`
		Cell     int           `bun:"cell"`
		Night    calendar.Date `bun:"night"`
	}
	for _, f := range Families() {
		// Q1 no double occupancy (structural, holds even without the primary key)
		cell, group := "0", f.Column+", night"
		if f.Cells {
			cell, group = "cell", f.Column+", cell, night"
		}
		var dup []keyRow
		err := db.NewRaw(`SELECT `+f.Column+` AS resource, `+cell+` AS cell, night FROM `+f.Table+` WHERE marina_id = ?
			GROUP BY `+group+` HAVING count(*) > 1`, marinaID).Scan(ctx, &dup)
		if err != nil {
			return nil, fmt.Errorf("Q1 %s: %w", f.Table, err)
		}
		for _, d := range dup {
			add("Q1", f.Table+" "+d.Night.String(), d.Resource)
		}
		// Q2 rows only for active allocations of the same marina; Q3 inside the period, same resource, kind, expiry
		type badRow struct {
			AllocationID string        `bun:"allocation_id"`
			Night        calendar.Date `bun:"night"`
			Q            string        `bun:"q"`
		}
		var bad []badRow
		err = db.NewRaw(`SELECT o.allocation_id, o.night,
				CASE WHEN a.status <> 'active' OR a.marina_id <> o.marina_id THEN 'Q2' ELSE 'Q3' END AS q
			FROM `+f.Table+` AS o JOIN allocations AS a ON a.id = o.allocation_id
			WHERE o.marina_id = ? AND (a.status <> 'active' OR a.marina_id <> o.marina_id OR o.night < a.first_night
				OR o.night > a.last_night OR a.resource_type <> ? OR a.resource_id <> o.`+f.Column+` OR a.kind <> o.kind
				OR (a.expires_at IS NULL) <> (o.expires_at IS NULL) OR a.expires_at <> o.expires_at)`,
			marinaID, f.ResourceType).Scan(ctx, &bad)
		if err != nil {
			return nil, fmt.Errorf("Q2/Q3 %s: %w", f.Table, err)
		}
		for _, b := range bad {
			add(b.Q, f.Table+" "+b.Night.String(), b.AllocationID)
		}
		// Q4 no row on a released night of its allocation
		var rel []badRow
		err = db.NewRaw(`SELECT o.allocation_id, o.night, 'Q4' AS q FROM `+f.Table+` AS o
			JOIN releases AS r ON r.allocation_id = o.allocation_id AND r.status = 'active'
			WHERE o.marina_id = ? AND o.night >= r.from_night AND o.night <= r.to_night`, marinaID).Scan(ctx, &rel)
		if err != nil {
			return nil, fmt.Errorf("Q4 %s: %w", f.Table, err)
		}
		for _, b := range rel {
			add("Q4", f.Table+" "+b.Night.String(), b.AllocationID)
		}
	}
	// Q5 releases inside their allocation and not overlapping each other
	var ids []string
	if err := db.NewRaw(`SELECT r.id FROM releases AS r JOIN allocations AS a ON a.id = r.allocation_id
		WHERE r.marina_id = ? AND r.status = 'active' AND (r.from_night < a.first_night OR r.to_night > a.last_night)`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q5: %w", err)
	}
	for _, id := range ids {
		add("Q5", "release outside its allocation", id)
	}
	type pair struct {
		A string `bun:"a"`
		B string `bun:"b"`
	}
	var pairs []pair
	if err := db.NewRaw(`SELECT r1.id AS a, r2.id AS b FROM releases AS r1 JOIN releases AS r2
			ON r1.allocation_id = r2.allocation_id AND r1.id < r2.id
		WHERE r1.marina_id = ? AND r1.status = 'active' AND r2.status = 'active'
			AND r1.from_night <= r2.to_night AND r2.from_night <= r1.to_night`, marinaID).Scan(ctx, &pairs); err != nil {
		return nil, fmt.Errorf("Q5: %w", err)
	}
	for _, p := range pairs {
		add("Q5", "overlapping releases", p.A, p.B)
	}
	// Q6 active allocations of the same contract line do not overlap
	pairs = nil
	if err := db.NewRaw(`SELECT a1.id AS a, a2.id AS b FROM allocations AS a1 JOIN allocations AS a2
			ON a1.contract_line_id = a2.contract_line_id AND a1.id < a2.id
		WHERE a1.marina_id = ? AND a1.status = 'active' AND a2.status = 'active'
			AND a1.first_night <= a2.last_night AND a2.first_night <= a1.last_night`, marinaID).Scan(ctx, &pairs); err != nil {
		return nil, fmt.Errorf("Q6: %w", err)
	}
	for _, p := range pairs {
		add("Q6", "overlapping allocations of a contract line", p.A, p.B)
	}
	if err := checkContractCoverage(ctx, db, marinaID, add); err != nil {
		return nil, err
	}
	// Q8 transits in held/pending_payment/confirmed/arrived have at least one active allocation (a held transit
	// whose hold was reclaimed lazily keeps an expired one until the contracts.expire job reconciles it, §3.3)
	ids = nil
	if err := db.NewRaw(`SELECT c.id FROM contracts AS c WHERE c.marina_id = ? AND c.kind = 'transit'
			AND c.status IN ('held','pending_payment','confirmed','arrived')
			AND NOT EXISTS (SELECT 1 FROM contract_lines AS l JOIN allocations AS a ON a.contract_line_id = l.id
				WHERE l.contract_id = c.id AND (a.status = 'active' OR (c.status = 'held' AND a.status = 'expired')))`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q8: %w", err)
	}
	for _, id := range ids {
		add("Q8", "live transit without allocation", id)
	}
	// Q8 quotes in draft/sent still valid (valid_until >= today) have an active hold allocation for every
	// placement of their hold options
	ids = nil
	if err := db.NewRaw(`SELECT p.id FROM quotes AS q JOIN quote_options AS o ON o.quote_id = q.id AND o.hold = ?
			JOIN quote_option_placements AS p ON p.option_id = o.id
			LEFT JOIN allocations AS a ON a.id = p.allocation_id
		WHERE q.marina_id = ? AND q.status IN ('draft','sent') AND q.valid_until >= ?
			AND (a.id IS NULL OR a.status <> 'active' OR a.kind <> 'hold')`, true, marinaID, today).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q8: %w", err)
	}
	for _, id := range ids {
		add("Q8", "valid quote hold option without its hold", id)
	}
	// Q9 dead contracts hold no future nights; a terminated one keeps only nights before terminated_on
	ids = nil
	if err := db.NewRaw(`SELECT a.id FROM allocations AS a JOIN contract_lines AS l ON l.id = a.contract_line_id
			JOIN contracts AS c ON c.id = l.contract_id
		WHERE a.marina_id = ? AND a.status = 'active' AND (c.status IN ('cancelled','no_show')
			OR (c.kind = 'transit' AND c.status = 'expired')
			OR (c.status = 'terminated' AND c.terminated_on IS NOT NULL AND a.last_night >= c.terminated_on))`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q9: %w", err)
	}
	for _, id := range ids {
		add("Q9", "dead contract holds nights", id)
	}
	// Q10 kinds coherent with the contract state
	ids = nil
	if err := db.NewRaw(`SELECT a.id FROM allocations AS a JOIN contract_lines AS l ON l.id = a.contract_line_id
			JOIN contracts AS c ON c.id = l.contract_id
		WHERE a.marina_id = ? AND a.status = 'active' AND (
			(c.kind = 'transit' AND c.status IN ('held','pending_payment','awaiting_approval') AND a.kind <> 'hold')
			OR (c.kind = 'transit' AND c.status IN ('confirmed','arrived','departed') AND a.kind <> 'transit')
			OR (c.kind = 'mooring' AND c.status = 'draft' AND a.kind <> 'hold')
			OR (c.kind = 'mooring' AND c.status IN ('active','terminated','expired') AND a.kind <> 'contract'))`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q10: %w", err)
	}
	for _, id := range ids {
		add("Q10", "allocation kind incoherent with the contract status", id)
	}
	// Q12 moved_from chain: same line and vessel, no overlap with an active predecessor
	type chain struct {
		ID        string        `bun:"id"`
		LineA     string        `bun:"line_a"`
		LineP     string        `bun:"line_p"`
		VesselA   string        `bun:"vessel_a"`
		VesselP   string        `bun:"vessel_p"`
		StatusP   string        `bun:"status_p"`
		FirstA    calendar.Date `bun:"first_a"`
		FirstP    calendar.Date `bun:"first_p"`
		LastP     calendar.Date `bun:"last_p"`
		ResourceA string        `bun:"resource_a"`
	}
	var chains []chain
	if err := db.NewRaw(`SELECT a.id, coalesce(CAST(a.contract_line_id AS TEXT), '') AS line_a, coalesce(CAST(p.contract_line_id AS TEXT), '') AS line_p,
			coalesce(CAST(a.vessel_id AS TEXT), '') AS vessel_a, coalesce(CAST(p.vessel_id AS TEXT), '') AS vessel_p, p.status AS status_p,
			a.first_night AS first_a, p.first_night AS first_p, p.last_night AS last_p, a.resource_id AS resource_a
		FROM allocations AS a JOIN allocations AS p ON p.id = a.moved_from_id
		WHERE a.marina_id = ? AND a.status = 'active'`, marinaID).Scan(ctx, &chains); err != nil {
		return nil, fmt.Errorf("Q12: %w", err)
	}
	for _, c := range chains {
		if c.LineA != c.LineP || c.VesselA != c.VesselP || (c.StatusP == AllocActive && !c.FirstA.After(c.LastP)) {
			add("Q12", "moved_from chain broken", c.ID)
		}
	}
	if err := checkPresenceState(ctx, db, marinaID, add); err != nil {
		return nil, err
	}
	// Q16 resold_allocation_id points to an allocation of the same marina on the same resource. The reseller may
	// later be truncated, moved or cancelled, and the release clipped, so the overlap is checked only at resale time.
	ids = nil
	if err := db.NewRaw(`SELECT r.id FROM releases AS r LEFT JOIN allocations AS a ON a.id = r.resold_allocation_id
		WHERE r.marina_id = ? AND r.resold_allocation_id IS NOT NULL AND (a.id IS NULL OR a.marina_id <> r.marina_id
			OR a.resource_id <> r.resource_id)`, marinaID).Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q16: %w", err)
	}
	for _, id := range ids {
		add("Q16", "resold allocation not on the released resource", id)
	}
	if err := checkRightsOccupancy(ctx, db, marinaID, add); err != nil {
		return nil, err
	}
	planCheckMu.RLock()
	fn := planCheck
	planCheckMu.RUnlock()
	if fn == nil {
		add("Q14", "plan check not registered (link internal/domain/occupancy)")
	} else {
		more, err := fn(ctx, db, marinaID, today)
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	return out, nil
}

// checkContractCoverage is Q7: the vessel lines (berths, areas) of a live contract (active mooring, held,
// confirmed or arrived transit) cover every night of [starts_on, ends_on) with their active allocations, without
// gaps or overlaps; land lines stay inside the period. Only a held transit with an expired allocation that no
// active allocation succeeds (a lazily reclaimed hold, §3.3) is exempt until the contracts.expire job reconciles
// it; draft contracts are not checked (hold_lost).
func checkContractCoverage(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	type row struct {
		ContractID   string        `bun:"contract_id"`
		StartsOn     calendar.Date `bun:"starts_on"`
		EndsOn       calendar.Date `bun:"ends_on"`
		Status0      string        `bun:"contract_status"`
		LineID       string        `bun:"line_id"`
		ResourceType string        `bun:"resource_type"`
		MovedFrom    string        `bun:"moved_from_id"`
		AllocID      string        `bun:"alloc_id"`
		Status       string        `bun:"status"`
		First        calendar.Date `bun:"first_night"`
		Last         calendar.Date `bun:"last_night"`
	}
	var rows []row
	err := db.NewRaw(`SELECT c.id AS contract_id, c.starts_on, c.ends_on, c.status AS contract_status, l.id AS line_id, l.resource_type,
			coalesce(CAST(a.moved_from_id AS TEXT), '') AS moved_from_id,
			coalesce(CAST(a.id AS TEXT), '') AS alloc_id, coalesce(a.status, '') AS status, a.first_night, a.last_night
		FROM contracts AS c JOIN contract_lines AS l ON l.contract_id = c.id AND l.line_kind = 'resource'
			LEFT JOIN allocations AS a ON a.contract_line_id = l.id
		WHERE c.marina_id = ? AND ((c.kind = 'mooring' AND c.status = 'active')
			OR (c.kind = 'transit' AND c.status IN ('held','confirmed','arrived')))
		ORDER BY c.id, a.first_night`, marinaID).Scan(ctx, &rows)
	if err != nil {
		return fmt.Errorf("Q7: %w", err)
	}
	type contract struct {
		starts, ends calendar.Date
		held         bool
		expired      []string // expired allocations
		succeeded    map[string]bool
		vesselLine   bool
		vessel       []row
		land         []row
	}
	cs := map[string]*contract{}
	var order []string
	for _, r := range rows {
		c := cs[r.ContractID]
		if c == nil {
			c = &contract{starts: r.StartsOn, ends: r.EndsOn, held: r.Status0 == "held", succeeded: map[string]bool{}}
			cs[r.ContractID] = c
			order = append(order, r.ContractID)
		}
		if r.ResourceType != ResourceLand {
			c.vesselLine = true
		}
		switch r.Status {
		case AllocExpired:
			c.expired = append(c.expired, r.AllocID)
		case AllocActive:
			if r.MovedFrom != "" {
				c.succeeded[r.MovedFrom] = true
			}
			if r.ResourceType == ResourceLand {
				c.land = append(c.land, r)
			} else {
				c.vessel = append(c.vessel, r)
			}
		}
	}
	for _, id := range order {
		c := cs[id]
		// Exempt: a held transit whose hold was reclaimed lazily (an expired allocation without an active
		// successor) until the contracts.expire job reconciles it. Any other contract is checked on its active
		// allocations, whatever expired allocations it has in its history.
		exempt := false
		for _, a := range c.expired {
			if c.held && !c.succeeded[a] {
				exempt = true
			}
		}
		if exempt {
			continue
		}
		last := c.ends.AddDays(-1)
		slices.SortFunc(c.vessel, func(a, b row) int { return a.First.Compare(b.First) })
		next := c.starts
		for _, r := range c.vessel {
			if !r.First.Equal(next) {
				add("Q7", "gap or overlap at "+r.First.String(), id, r.AllocID)
			}
			if r.Last.After(next.AddDays(-1)) {
				next = r.Last.AddDays(1)
			}
		}
		if c.vesselLine && !next.Equal(last.AddDays(1)) {
			add("Q7", "period not covered up to "+last.String(), id)
		}
		for _, r := range c.land {
			if r.First.Before(c.starts) || r.Last.After(last) {
				add("Q7", "land line outside the period", id, r.AllocID)
			}
		}
	}
	return nil
}

// checkPresenceState is Q13: vessel_presence is the state after the last presence event of the vessel.
func checkPresenceState(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	var vps []VesselPresence
	if err := db.NewSelect().Model(&vps).Where("marina_id = ?", marinaID).Scan(ctx); err != nil {
		return fmt.Errorf("Q13: %w", err)
	}
	if len(vps) == 0 {
		return nil
	}
	type last struct {
		VesselID string    `bun:"vessel_id"`
		ID       string    `bun:"id"`
		Event    string    `bun:"event"`
		At       time.Time `bun:"at"`
	}
	var ps []last
	if err := db.NewSelect().Model((*Presence)(nil)).Column("vessel_id", "id", "event", "at").Where("marina_id = ?", marinaID).
		OrderExpr("vessel_id, created_at, id").Scan(ctx, &ps); err != nil {
		return fmt.Errorf("Q13: %w", err)
	}
	latest := map[string]last{}
	for _, p := range ps {
		latest[p.VesselID] = p
	}
	stateOf := map[string]string{"arrived": "in_port", "launched": "in_port", "departed": "out", "hauled_out": "ashore"}
	for _, vp := range vps {
		l, ok := latest[vp.VesselID]
		if !ok || l.ID != vp.LastPresenceID || stateOf[l.Event] != vp.State {
			add("Q13", "vessel_presence differs from the last presence event", vp.VesselID)
		}
	}
	return nil
}

// checkRightsOccupancy is Q17 (INV-43, M4): every owner allocation belongs to a right of the same berth that is not
// cancelled and stays inside the right's period; every active right is covered, from the first night written
// for it to its to_on, by its active owner allocations or by its mandate periods (mandate nights have no owner
// rows, m4-plan §15 D16). Interval arithmetic, no per-night scan.
func checkRightsOccupancy(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	type link struct {
		AllocationID string        `bun:"allocation_id"`
		RightID      string        `bun:"right_id"`
		Status       string        `bun:"status"`
		Kind         string        `bun:"kind"`
		ResourceID   string        `bun:"resource_id"`
		First        calendar.Date `bun:"first_night"`
		Last         calendar.Date `bun:"last_night"`
		BerthID      string        `bun:"berth_id"`
		RightStatus  string        `bun:"right_status"`
		FromOn       calendar.Date `bun:"from_on"`
		ToOn         calendar.Date `bun:"to_on"`
	}
	var links []link
	if err := db.NewRaw(`SELECT l.allocation_id, l.right_id, a.status, a.kind, a.resource_id, a.first_night, a.last_night,
			r.berth_id, r.status AS right_status, r.from_on, r.to_on
		FROM berth_right_allocations AS l JOIN allocations AS a ON a.id = l.allocation_id JOIN berth_rights AS r ON r.id = l.right_id
		WHERE l.marina_id = ?`, marinaID).Scan(ctx, &links); err != nil {
		return fmt.Errorf("Q17: %w", err)
	}
	var orphans []string
	if err := db.NewRaw(`SELECT a.id FROM allocations AS a LEFT JOIN berth_right_allocations AS l ON l.allocation_id = a.id
		WHERE a.marina_id = ? AND a.kind = 'owner' AND a.status = 'active' AND l.allocation_id IS NULL`, marinaID).Scan(ctx, &orphans); err != nil {
		return fmt.Errorf("Q17: %w", err)
	}
	for _, id := range orphans {
		add("Q17", "owner allocation without a right", id)
	}
	type span struct{ first, last calendar.Date }
	firstWritten := map[string]calendar.Date{}
	cover := map[string][]span{}
	for _, l := range links {
		if l.Kind != AllocKindOwner {
			add("Q17", "right linked to a non-owner allocation", l.AllocationID)
			continue
		}
		if f, ok := firstWritten[l.RightID]; !ok || l.First.Before(f) {
			firstWritten[l.RightID] = l.First
		}
		if l.Status != AllocActive {
			continue
		}
		if l.RightStatus == "cancelled" || l.ResourceID != l.BerthID || l.First.Before(l.FromOn) || l.Last.After(l.ToOn) {
			add("Q17", "owner allocation outside its right", l.AllocationID, l.RightID)
		}
		cover[l.RightID] = append(cover[l.RightID], span{l.First, l.Last})
	}
	type mandate struct {
		RightID string        `bun:"right_id"`
		FromOn  calendar.Date `bun:"from_on"`
		ToOn    calendar.Date `bun:"to_on"`
	}
	var ms []mandate
	if err := db.NewRaw(`SELECT right_id, from_on, to_on FROM management_mandates WHERE marina_id = ?`, marinaID).Scan(ctx, &ms); err != nil {
		return fmt.Errorf("Q17: %w", err)
	}
	for _, m := range ms {
		cover[m.RightID] = append(cover[m.RightID], span{m.FromOn, m.ToOn})
	}
	type right struct {
		ID   string        `bun:"id"`
		ToOn calendar.Date `bun:"to_on"`
	}
	var rights []right
	if err := db.NewRaw(`SELECT id, to_on FROM berth_rights WHERE marina_id = ? AND status = 'active'`, marinaID).Scan(ctx, &rights); err != nil {
		return fmt.Errorf("Q17: %w", err)
	}
	for _, r := range rights {
		start, ok := firstWritten[r.ID]
		if !ok {
			add("Q17", "active right without owner occupancy", r.ID)
			continue
		}
		spans := cover[r.ID]
		slices.SortFunc(spans, func(a, b span) int { return a.first.Compare(b.first) })
		next := start // first night not yet covered
		for _, sp := range spans {
			if sp.first.After(next) {
				break
			}
			if !sp.last.Before(next) {
				next = sp.last.AddDays(1)
			}
		}
		if !next.After(r.ToOn) {
			add("Q17", "active right not covered from "+next.String(), r.ID)
		}
	}
	return nil
}
