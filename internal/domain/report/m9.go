package report

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// M9 reports (m9-plan §11): storage, maintenance and utilities, on the M7 machinery (one read transaction, pure
// builders in calc, tiles = Totals of the drill report).

// Storage returns the storage report; production only with reports.all (reports.operational or reports.all).
func (s *Service) Storage(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*StorageResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyStorage, false, func(r *run, q Params) (*StorageResult, error) {
		res, _, err := r.runStorage(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Maintenance returns the maintenance report; external costs only with reports.all (reports.operational or
// reports.all).
func (s *Service) Maintenance(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*MaintenanceResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyMaintenance, false, func(r *run, q Params) (*MaintenanceResult, error) {
		res, _, err := r.runMaintenance(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// Utilities returns the utilities report; amounts only with reports.all (reports.operational or reports.all).
func (s *Service) Utilities(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*UtilitiesResult, error) {
	return exec(ctx, s, p, marinaID, q, calc.KeyUtilities, false, func(r *run, q Params) (*UtilitiesResult, error) {
		res, _, err := r.runUtilities(q)
		if err != nil {
			return nil, err
		}
		return res, finish(res, q, r.today)
	})
}

// storageMapKinds are the map kinds of the storage report (m9-plan §11: yard and shed surfaces).
var storageMapKinds = []string{"yard", "shed"}

// storageAccess evaluates the exits of one area (optimizer.AccessibilityTimeline on optimizer.NewGrid). It is a
// variable so the integration tests of the report can run before the optimizer: the accessibility itself is
// tested in the optimizer package.
var storageAccess = func(a layout.Area, exits []int, fixed []optimizer.Obstacle, items []optimizer.TimelineItem,
	from, to calendar.Date,
) []optimizer.Access {
	return optimizer.AccessibilityTimeline(optimizer.NewGrid(a, exits), fixed, items, from, to)
}

func (r *run) runStorage(q Params) (*StorageResult, calc.Facts, error) {
	if _, err := r.loadResources(); err != nil {
		return nil, nil, err
	}
	in := calc.StorageInput{
		From: q.From, To: q.To, GroupBy: q.GroupBy, ExitsBlockedOnly: q.ExitsBlockedOnly, Full: r.acc.Full,
		Allocations: map[string]*calc.StorageAllocation{},
	}
	areaRows := map[string]*store.DynamicArea{}
	for i := range r.res.Areas {
		a := &r.res.Areas[i]
		kind := r.res.MapKinds[a.MapID]
		if a.Kind != "surface" || !slices.Contains(storageMapKinds, kind) || (q.Dims.MapID != "" && a.MapID != q.Dims.MapID) ||
			(q.Dims.MapKind != "" && kind != q.Dims.MapKind) || (q.AreaID != "" && a.ID != q.AreaID) {
			continue
		}
		var blocked []int
		_ = json.Unmarshal(a.BlockedCells, &blocked)
		n := int64(0)
		for _, c := range blocked {
			if c >= 0 && int64(c) < a.CellCount {
				n++
			}
		}
		in.Areas = append(in.Areas, calc.StorageArea{
			ID: a.ID, Code: a.Code, MapID: a.MapID, MapKind: kind, CellCm: geo.Cm(a.CellCm), UsableCells: max(0, a.CellCount-n),
		})
		areaRows[a.ID] = a
	}
	if len(in.Areas) == 0 {
		res, facts := calc.BuildStorage(in)
		return &res, facts, nil
	}
	f := store.ReportFilter{From: q.From, To: q.To}
	rows, err := store.ReportCellNights(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, nil, err
	}
	in.Rows = nightRows(rows)
	arows, err := store.ReportAllocations(r.ctx, r.tx, r.scope(), store.ReportAllocationFilter{From: q.From, To: q.To})
	if err != nil {
		return nil, nil, err
	}
	var storageIDs []string
	for i := range arows {
		a := &arows[i]
		if a.ContractKind == calc.ContractStorage && a.ContractID != "" && areaRows[a.ResourceID] != nil {
			storageIDs = append(storageIDs, a.ContractID)
		}
	}
	slices.Sort(storageIDs)
	storageIDs = slices.Compact(storageIDs)
	details, err := store.ReportStorageExits(r.ctx, r.tx, r.scope(), storageIDs)
	if err != nil {
		return nil, nil, err
	}
	det := make(map[string]*store.ReportStorageExitRow, len(details))
	for i := range details {
		det[details[i].ContractID] = &details[i]
	}
	byArea := map[string][]optimizer.TimelineItem{}
	withExits := map[string]bool{} // areas with an exit in the period
	producing := map[string]bool{}
	for i := range arows {
		a := &arows[i]
		if a.ResourceType != calc.TypeArea || areaRows[a.ResourceID] == nil || !calc.Occupied(a.Kind) {
			continue
		}
		v := vesselOf(a)
		exit, arrival := a.LastNight.AddDays(1), a.FirstNight
		if d := det[a.ContractID]; d != nil && a.ContractKind == calc.ContractStorage {
			exit = a.ContractEndsOn
			if !d.LaunchOn.IsZero() {
				exit = d.LaunchOn
			}
			if !d.HaulOutOn.IsZero() && d.HaulOutOn.After(arrival) && !d.HaulOutOn.After(a.LastNight) {
				arrival = d.HaulOutOn
			}
		}
		sa := &calc.StorageAllocation{
			ID: a.ID, AreaID: a.ResourceID, Kind: a.Kind, ContractID: a.ContractID, ContractKind: a.ContractKind,
			ContractCode: contractCode(a.ContractKind, a.ContractYear, a.ContractNumber), VesselID: a.VesselID,
			LOA: v.LOA, Beam: v.Beam, ExitOn: exit,
		}
		if r.acc.Full && a.ContractID != "" && !producing[a.ContractID] && calc.Qualifying(a.ContractKind, a.ContractStatus) {
			producing[a.ContractID] = true
			sa.Production = calc.Some(calc.Competence(money.Cents(a.ContractTotal), a.ContractStartsOn, a.ContractEndsOn, q.From, q.To))
		}
		in.Allocations[a.ID] = sa
		if a.XCm == nil || a.YCm == nil {
			continue
		}
		rot := int64(0)
		if a.RotationDd != nil {
			rot = *a.RotationDd
		}
		byArea[a.ResourceID] = append(byArea[a.ResourceID], optimizer.TimelineItem{
			Placed: optimizer.Placed{
				ID: a.ID, LOA: v.LOA, Beam: v.Beam, First: a.FirstNight, Last: a.LastNight, ExitOn: exit,
				At: layout.Placement{X: geo.Cm(*a.XCm), Y: geo.Cm(*a.YCm), Rotation: geo.Angle(rot)},
			},
			ArrivalOn: arrival,
		})
		if !exit.Before(q.From) && !exit.After(q.To) {
			withExits[a.ResourceID] = true
		}
	}
	for id := range byArea {
		if !withExits[id] {
			delete(byArea, id)
		}
	}
	if err := r.storageExits(q, areaRows, byArea, in.Allocations); err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildStorage(in)
	return &res, facts, nil
}

// storageExits marks the allocations whose exit event in the period is not accessible (m9-plan §5).
func (r *run) storageExits(q Params, areas map[string]*store.DynamicArea, items map[string][]optimizer.TimelineItem,
	allocs map[string]*calc.StorageAllocation,
) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	obs, err := store.ReportCellObstacles(r.ctx, r.tx, r.scope(), ids, q.From, q.To)
	if err != nil {
		return err
	}
	fixed := map[string][]optimizer.Obstacle{}
	for _, o := range obs {
		list := fixed[o.AreaID]
		if n := len(list); n > 0 && list[n-1].ID == o.AllocationID && list[n-1].First.Equal(o.First) && list[n-1].Last.Equal(o.Last) {
			list[n-1].Cells = append(list[n-1].Cells, o.Cell)
		} else {
			list = append(list, optimizer.Obstacle{ID: o.AllocationID, Cells: []int{o.Cell}, First: o.First, Last: o.Last})
		}
		fixed[o.AreaID] = list
	}
	for _, id := range ids {
		la, ok := r.areas[id]
		if !ok {
			continue
		}
		var exits []int
		_ = json.Unmarshal(areas[id].Exits, &exits)
		slices.Sort(exits)
		for _, ac := range storageAccess(la, exits, fixed[id], items[id], q.From, q.To) {
			if ac.Event != optimizer.EventExit || ac.Accessible {
				continue
			}
			if a := allocs[ac.ID]; a != nil && a.ExitOn.Equal(ac.On) {
				a.ExitBlocked = true
			}
		}
	}
	return nil
}

func (r *run) runMaintenance(q Params) (*MaintenanceResult, calc.Facts, error) {
	rows, err := store.ReportMaintenanceTasks(r.ctx, r.tx, r.scope(), q.From, q.To, r.localDayStart(q.From), r.localDayStart(q.To.AddDays(1)))
	if err != nil {
		return nil, nil, err
	}
	tasks := make([]calc.MaintenanceTask, 0, len(rows))
	for i := range rows {
		t := &rows[i]
		ct := calc.MaintenanceTask{
			ID: t.ID, ElementType: t.ElementType, ElementID: t.ElementID, ElementCode: t.ElementCode,
			Intervention: t.Intervention, Source: t.Source, Priority: t.Priority, Status: t.Status, DueOn: t.DueOn,
			CreatedOn: calendar.In(t.CreatedAt, r.loc), ClosedOn: calendar.In(t.UpdatedAt, r.loc),
		}
		if !t.DoneAt.IsZero() {
			ct.DoneOn = calendar.In(t.DoneAt, r.loc)
		}
		if t.InternalMinutes != nil {
			ct.InternalMinutes = *t.InternalMinutes
		}
		if t.ExternalCostCents != nil {
			ct.ExternalCost = money.Cents(*t.ExternalCostCents)
		}
		tasks = append(tasks, ct)
	}
	res, facts := calc.BuildMaintenance(calc.MaintenanceInput{
		From: q.From, To: q.To, Today: r.today, GroupBy: q.GroupBy, Status: q.Status, ElementType: q.ElementType,
		OverdueOnly: q.TaskOverdueOnly, Full: r.acc.Full, Tasks: tasks,
	})
	return &res, facts, nil
}

func (r *run) runUtilities(q Params) (*UtilitiesResult, calc.Facts, error) {
	rows, err := store.ReportConsumptions(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return nil, nil, err
	}
	cs := make([]calc.UtilityConsumption, len(rows))
	for i := range rows {
		u := &rows[i]
		cs[i] = calc.UtilityConsumption{
			ID: u.ID, MeterID: u.MeterID, MeterCode: u.MeterCode, Kind: u.Kind, ResourceType: u.ResourceType,
			ResourceID: u.ResourceID, ContractID: u.ContractID, ContractKind: u.ContractKind,
			ContractCode: contractCode(u.ContractKind, u.ContractYear, u.ContractNumber), First: u.FirstNight,
			Last: u.LastNight, QtyMilli: u.QtyMilli, Status: u.Status,
		}
	}
	if r.acc.Full {
		if err := r.utilityAmounts(rows, cs); err != nil {
			return nil, nil, err
		}
	}
	res, facts := calc.BuildUtilities(calc.UtilitiesInput{
		From: q.From, To: q.To, GroupBy: q.GroupBy, Kind: q.Kind, Status: q.Status, MeterID: q.MeterID,
		ContractID: q.ContractID, Full: r.acc.Full, Consumptions: cs,
	})
	return &res, facts, nil
}

// utilityAmounts sets the charged gross of the charged consumptions (the line of their charge with the same nights
// and quantity, paired in order) and the value at the current price of the pending ones (the tariff item of the
// meter, else of the utility settings for power and water; the price of its last night, as the charging run).
func (r *run) utilityAmounts(rows []store.ReportConsumptionRow, cs []calc.UtilityConsumption) error {
	var chargeIDs []string
	for i := range rows {
		if rows[i].Status == calc.ConsumptionCharged && rows[i].ChargeID != "" {
			chargeIDs = append(chargeIDs, rows[i].ChargeID)
		}
	}
	slices.Sort(chargeIDs)
	chargeIDs = slices.Compact(chargeIDs)
	lines, err := store.ReportUtilityLines(r.ctx, r.tx, r.scope(), chargeIDs)
	if err != nil {
		return err
	}
	used := make([]bool, len(lines))
	for i := range rows {
		u := &rows[i]
		if u.Status != calc.ConsumptionCharged || u.ChargeID == "" {
			continue
		}
		for j := range lines {
			l := &lines[j]
			if !used[j] && l.ChargeID == u.ChargeID && l.QtyMilli == u.QtyMilli && l.ServiceFrom.Equal(u.FirstNight) &&
				l.ServiceTo.Equal(u.LastNight) {
				used[j] = true
				cs[i].Charged = calc.Some(money.Cents(l.GrossCents))
				break
			}
		}
	}
	power, water, err := store.ReportUtilityItems(r.ctx, r.tx, r.scope())
	if err != nil {
		return err
	}
	type key struct {
		item string
		day  calendar.Date
	}
	prices := map[key]*money.Cents{}
	for i := range rows {
		u := &rows[i]
		if u.Status != calc.ConsumptionPending {
			continue
		}
		item := u.MeterItemID
		if item == "" {
			switch u.Kind {
			case calc.MeterPower:
				item = power
			case calc.MeterWater:
				item = water
			}
		}
		if item == "" {
			continue
		}
		k := key{item, u.LastNight}
		p, ok := prices[k]
		if !ok {
			p, err = r.itemPrice(item, u.LastNight)
			if err != nil {
				return err
			}
			prices[k] = p
		}
		if p == nil {
			continue
		}
		if v, err := qty.Milli(u.QtyMilli).MulCents(*p); err == nil {
			cs[i].PendingValue = calc.Some(v)
		}
	}
	return nil
}

// itemPrice is the unit price of an item on a day from the active base rules (the rule order of
// store.ActiveBaseRulesOfItem: highest priority, then most recent; unit price, else flat amount), nil when none.
func (r *run) itemPrice(itemID string, day calendar.Date) (*money.Cents, error) {
	rules, err := store.ActiveBaseRulesOfItem(r.ctx, r.tx, r.scope(), itemID, day)
	if err != nil {
		return nil, err
	}
	for _, rl := range rules {
		var body pricing.Rule
		if json.Unmarshal(rl.Body, &body) != nil {
			continue
		}
		if body.UnitCents != nil {
			v := *body.UnitCents
			return &v, nil
		}
		if body.AmountCents != nil {
			v := *body.AmountCents
			return &v, nil
		}
	}
	return nil, nil //nolint:nilnil // no price is not an error: the record stays unpriced (note no_price)
}
