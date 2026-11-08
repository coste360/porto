package utility

import (
	"context"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/utility/consume"
	"github.com/coste360/porto/internal/store"
)

// Consumptions (m9-plan §9.3, D21, D23): the intervals between consecutive non-void readings of a power or water
// meter, each split by nights among the occupants of the meter's resource (contract, transit and owner allocations
// with a contract; other nights are the unassigned share, never charged). Recomputation reconciles the open rows
// (pending, anomaly, unassigned) with the readings; intervals with a charged or excluded row are frozen and the
// readings inside them do not split them (they are out of order). A confirmed reading inside a charged interval
// produces correction deltas instead (charged later as adjustments, INV-51).

var errFrozenEdge = errors.New("utility: a frozen interval lost a bound")

type pair struct{ from, to string }

// chargeable reports whether a meter produces consumptions (power and water).
func chargeable(m *store.UtilityMeter) bool {
	return m.Kind == store.MeterPower || m.Kind == store.MeterWater
}

func isCorrection(c *store.UtilityConsumption) bool {
	return strings.HasPrefix(c.ShareKey, store.ConsumptionCorrectionPrefix)
}

// occupantRows splits one interval among the occupants and returns the consumption rows (not yet inserted).
func (s *Service) occupantRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.UtilityMeter, iv consume.Interval,
	status string,
) ([]store.UtilityConsumption, error) {
	segs, occ, err := s.segments(ctx, db, scope, m, iv.First, iv.Last)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []store.UtilityConsumption
	for _, sh := range consume.Split(iv.Delta, iv.First, iv.Last, segs) {
		row := store.UtilityConsumption{
			ID: store.NewID(), MeterID: m.ID, FromReadingID: iv.FromID, ToReadingID: iv.ToID, ShareKey: store.ConsumptionShareUnassigned,
			ResourceType: m.ResourceType, ResourceID: m.ResourceID, FirstNight: sh.First, LastNight: sh.Last, QtyMilli: int64(sh.Qty),
			Status: store.ConsumptionUnassigned, CreatedAt: now, UpdatedAt: now,
		}
		if sh.Key != "" {
			o := occ[sh.Key]
			row.ShareKey, row.AllocationID, row.ContractID = sh.Key, sh.Key, o.ContractID
			row.CustomerID = o.BillingCustomerID
			if row.CustomerID == "" {
				row.CustomerID = o.CustomerID
			}
			row.Status = store.ConsumptionPending
		}
		if status == store.ConsumptionAnomaly {
			row.Status = store.ConsumptionAnomaly
		}
		out = append(out, row)
	}
	return out, nil
}

// segments returns the occupant segments of the meter's resource on [first, last]: the runs of nights each
// allocation with a contract holds in the occupancy rows (a released night resold to another boat belongs to the
// reseller); owner nights without a contract stay unassigned (D29).
func (s *Service) segments(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.UtilityMeter, first, last calendar.Date) (
	[]consume.Segment, map[string]store.MeterOccupant, error,
) {
	occ := map[string]store.MeterOccupant{}
	if m.ResourceID == "" {
		return nil, occ, nil
	}
	rows, err := store.MeterOccupants(ctx, db, scope, m.ResourceType, m.ResourceID, first, last)
	if err != nil {
		return nil, nil, err
	}
	for _, o := range rows {
		if o.ContractID != "" {
			occ[o.AllocationID] = o
		}
	}
	nights, err := store.OccupiedNights(ctx, db, scope, m.ResourceType, m.ResourceID, first, last)
	if err != nil {
		return nil, nil, err
	}
	var segs []consume.Segment
	for _, n := range nights {
		if _, ok := occ[n.AllocationID]; !ok {
			continue
		}
		if k := len(segs) - 1; k >= 0 && segs[k].Key == n.AllocationID && segs[k].Last.AddDays(1).Equal(n.Night) {
			segs[k].Last = n.Night
			continue
		}
		segs = append(segs, consume.Segment{Key: n.AllocationID, First: n.Night, Last: n.Night})
	}
	return segs, occ, nil
}

// recompute reconciles the open consumptions of a meter with its readings and returns how many rows it changed.
func (s *Service) recompute(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m *store.UtilityMeter) (int, error) {
	if !chargeable(m) {
		return 0, nil
	}
	readings, err := store.MeterReadingsOf(ctx, tx, scope, m.ID)
	if err != nil {
		return 0, err
	}
	existing, err := store.ConsumptionsOfMeter(ctx, tx, scope, m.ID)
	if err != nil {
		return 0, err
	}
	byID := map[string]*store.MeterReading{}
	for i := range readings {
		byID[readings[i].ID] = &readings[i]
	}
	frozen := map[pair]bool{}
	for i := range existing {
		c := &existing[i]
		if !isCorrection(c) && (c.Status == store.ConsumptionCharged || c.Status == store.ConsumptionExcluded) {
			frozen[pair{c.FromReadingID, c.ToReadingID}] = true
		}
	}
	for p := range frozen {
		if byID[p.from] == nil || byID[p.to] == nil {
			return 0, errFrozenEdge
		}
	}
	// Readings strictly inside a frozen interval do not split it.
	seq := make([]consume.Reading, 0, len(readings))
	for i := range readings {
		r := &readings[i]
		inside := false
		for p := range frozen {
			a, b := byID[p.from], byID[p.to]
			if r.ReadAt.After(a.ReadAt) && r.ReadAt.Before(b.ReadAt) {
				inside = true
				break
			}
		}
		if !inside {
			seq = append(seq, pureReading(r))
		}
	}
	set, err := store.GetUtilitySettings(ctx, tx, scope, s.now())
	if err != nil {
		return 0, err
	}
	type rowKey struct{ from, to, share string }
	desired := map[rowKey]store.UtilityConsumption{}
	var order []rowKey
	for _, iv := range consume.Intervals(meterParams(m, set), seq) {
		if frozen[pair{iv.FromID, iv.ToID}] {
			continue
		}
		status := ""
		if to := byID[iv.ToID]; to != nil && to.Status == store.ReadingAnomaly {
			status = store.ConsumptionAnomaly
		}
		rows, err := s.occupantRows(ctx, tx, scope, m, iv, status)
		if err != nil {
			return 0, err
		}
		for _, r := range rows {
			k := rowKey{r.FromReadingID, r.ToReadingID, r.ShareKey}
			desired[k] = r
			order = append(order, k)
		}
	}
	changed := 0
	var drop []string
	now := s.now()
	for i := range existing {
		c := &existing[i]
		if isCorrection(c) || frozen[pair{c.FromReadingID, c.ToReadingID}] {
			continue
		}
		k := rowKey{c.FromReadingID, c.ToReadingID, c.ShareKey}
		want, ok := desired[k]
		if !ok || want.QtyMilli != c.QtyMilli || !want.FirstNight.Equal(c.FirstNight) || !want.LastNight.Equal(c.LastNight) ||
			want.ResourceID != c.ResourceID {
			drop = append(drop, c.ID)
			continue
		}
		delete(desired, k)
		// Same share: keep the row (and a manual assignment) and follow the anomaly state of the reading.
		next := c.Status
		switch {
		case want.Status == store.ConsumptionAnomaly && c.Status != store.ConsumptionAnomaly:
			next = store.ConsumptionAnomaly
		case want.Status != store.ConsumptionAnomaly && c.Status == store.ConsumptionAnomaly:
			next, c.ContractID, c.CustomerID = want.Status, want.ContractID, want.CustomerID
		}
		if next != c.Status {
			v := c.Version
			c.Status = next
			if err := store.UpdateUtilityConsumption(ctx, tx, scope, c, v, now); err != nil {
				return 0, err
			}
			changed++
		}
	}
	if err := store.DeleteOpenUtilityConsumptions(ctx, tx, scope, drop); err != nil {
		return 0, err
	}
	changed += len(drop)
	var ins []store.UtilityConsumption
	for _, k := range order {
		if r, ok := desired[k]; ok {
			ins = append(ins, r)
			delete(desired, k)
		}
	}
	if err := store.InsertUtilityConsumptions(ctx, tx, scope, ins); err != nil {
		return 0, err
	}
	return changed + len(ins), nil
}

// correct writes the correction deltas of a confirmed reading r inside a charged interval [A, C]: the shares of
// [A, r] + [r, C] minus the charged shares of [A, C], per occupant (D23). Nothing when r is not inside one.
func (s *Service) correct(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m *store.UtilityMeter, r *store.MeterReading) error {
	if !chargeable(m) {
		return nil
	}
	existing, err := store.ConsumptionsOfMeter(ctx, tx, scope, m.ID)
	if err != nil {
		return err
	}
	groups := map[pair][]store.UtilityConsumption{}
	charged := map[pair]bool{}
	for _, c := range existing {
		if isCorrection(&c) {
			continue
		}
		p := pair{c.FromReadingID, c.ToReadingID}
		groups[p] = append(groups[p], c)
		if c.Status == store.ConsumptionCharged {
			charged[p] = true
		}
	}
	for p := range charged {
		a, err := store.GetMeterReading(ctx, tx, scope, p.from)
		if err != nil {
			return err
		}
		c, err := store.GetMeterReading(ctx, tx, scope, p.to)
		if err != nil {
			return err
		}
		if !r.ReadAt.After(a.ReadAt) || !r.ReadAt.Before(c.ReadAt) {
			continue
		}
		set, err := store.GetUtilitySettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		newQty := map[string]int64{}
		occ := map[string]store.MeterOccupant{}
		for _, iv := range consume.Intervals(meterParams(m, set), []consume.Reading{pureReading(a), pureReading(r), pureReading(c)}) {
			segs, o, err := s.segments(ctx, tx, scope, m, iv.First, iv.Last)
			if err != nil {
				return err
			}
			for k, v := range o {
				occ[k] = v
			}
			for _, sh := range consume.Split(iv.Delta, iv.First, iv.Last, segs) {
				key := sh.Key
				if key == "" {
					key = store.ConsumptionShareUnassigned
				}
				newQty[key] += int64(sh.Qty)
			}
		}
		oldQty := map[string]int64{}
		first, last := groups[p][0].FirstNight, groups[p][0].LastNight
		for _, g := range groups[p] {
			oldQty[g.ShareKey] += g.QtyMilli
			if g.FirstNight.Before(first) {
				first = g.FirstNight
			}
			if g.LastNight.After(last) {
				last = g.LastNight
			}
			if g.ShareKey != store.ConsumptionShareUnassigned && g.ContractID != "" {
				if _, ok := occ[g.ShareKey]; !ok {
					occ[g.ShareKey] = store.MeterOccupant{AllocationID: g.ShareKey, ContractID: g.ContractID, BillingCustomerID: g.CustomerID}
				}
			}
		}
		keys := map[string]bool{}
		for k := range newQty {
			keys[k] = true
		}
		for k := range oldQty {
			keys[k] = true
		}
		now := s.now()
		var rows []store.UtilityConsumption
		for k := range keys {
			diff := newQty[k] - oldQty[k]
			if diff == 0 {
				continue
			}
			row := store.UtilityConsumption{
				ID: store.NewID(), MeterID: m.ID, FromReadingID: a.ID, ToReadingID: c.ID,
				ShareKey: store.ConsumptionCorrectionPrefix + r.ID + ":" + k, ResourceType: m.ResourceType, ResourceID: m.ResourceID,
				FirstNight: first, LastNight: last, QtyMilli: diff, Status: store.ConsumptionUnassigned, CreatedAt: now, UpdatedAt: now,
			}
			if o, ok := occ[k]; ok && k != store.ConsumptionShareUnassigned {
				row.AllocationID, row.ContractID, row.Status = k, o.ContractID, store.ConsumptionPending
				row.CustomerID = o.BillingCustomerID
				if row.CustomerID == "" {
					row.CustomerID = o.CustomerID
				}
			}
			rows = append(rows, row)
		}
		if err := store.InsertUtilityConsumptions(ctx, tx, scope, rows); err != nil {
			return err
		}
	}
	return nil
}
