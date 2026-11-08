package report

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// run is one authorised report request inside its read transaction: the loaders memoise what does not depend
// on the period (resources, settings, names), so the dashboard reads them once.
type run struct {
	s     *Service
	ctx   context.Context
	tx    bun.IDB
	acc   access
	p     *auth.Principal
	today calendar.Date
	loc   *time.Location
	names bool // customers.read: display names in records

	res       *store.ReportResourceSet
	resources []calc.Resource
	areas     map[string]layout.Area
	settings  *store.OccupancySettings
	tolerance geo.Cm
	nameCache map[string]calc.CustomerInfo
	targets   map[string]*fit.Target
}

func (s *Service) newRun(ctx context.Context, tx bun.IDB, p *auth.Principal, acc access) *run {
	loc := time.UTC
	if tz, err := store.MarinaTimezone(ctx, tx, acc.Scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return &run{
		s: s, ctx: ctx, tx: tx, acc: acc, p: p, loc: loc, today: occupancy.Today(s.d.Clock, loc),
		names:     p.Can(acc.Scope.OrgID(), acc.Scope.ID(), auth.PermCustomersRead),
		nameCache: map[string]calc.CustomerInfo{},
	}
}

func (r *run) scope() store.MarinaScope { return r.acc.Scope }

// period applies the defaults (January 1 of the year of to, today) and validates the period.
func (r *run) period(from, to calendar.Date) (calendar.Date, calendar.Date, error) {
	if to.IsZero() {
		to = r.today
	}
	if from.IsZero() {
		from = calendar.New(to.Year(), time.January, 1)
	}
	if to.Before(from) || from.DaysUntil(to)+1 > MaxPeriodDays {
		return from, to, ErrInvalidPeriod
	}
	return from, to, nil
}

// loadResources reads the active resources once and converts them.
func (r *run) loadResources() ([]calc.Resource, error) {
	if r.resources != nil {
		return r.resources, nil
	}
	set, err := store.ReportResources(r.ctx, r.tx, r.scope())
	if err != nil {
		return nil, err
	}
	r.res = set
	r.areas = map[string]layout.Area{}
	out := make([]calc.Resource, 0, len(set.Berths)+len(set.Areas)+len(set.Lands))
	for i := range set.Berths {
		b := &set.Berths[i]
		out = append(out, calc.Resource{
			ID: b.ID, Code: b.Code, Type: calc.TypeBerth, Family: calc.FamilyBerth, MapID: b.MapID, MapKind: set.MapKinds[b.MapID],
			StructureID: b.StructureID, GroupIDs: set.Groups[b.ID], Category: b.PriceClass, Length: geo.Cm(b.LengthCm),
			Width: geo.Cm(b.WidthCm), MaxLOA: geo.Cm(b.MaxLOACm), MaxBeam: geo.Cm(b.MaxBeamCm), HoldsBoat: true,
		})
	}
	for i := range set.Areas {
		a := &set.Areas[i]
		fam := calc.FamilyLinear
		if a.Kind == "surface" {
			fam = calc.FamilySurface
		}
		var blocked []int
		_ = json.Unmarshal(a.BlockedCells, &blocked)
		n := int64(0)
		for _, c := range blocked {
			if c >= 0 && int64(c) < a.CellCount {
				n++
			}
		}
		if la, err := occupancy.LayoutArea(a); err == nil {
			r.areas[a.ID] = la
		}
		out = append(out, calc.Resource{
			ID: a.ID, Code: a.Code, Type: calc.TypeArea, Family: fam, MapID: a.MapID, MapKind: set.MapKinds[a.MapID],
			StructureID: a.StructureID, GroupIDs: set.Groups[a.ID], Category: a.PriceClass, HoldsBoat: true,
			CellCm: geo.Cm(a.CellCm), UsableCells: max(0, a.CellCount-n),
		})
	}
	for i := range set.Lands {
		l := &set.Lands[i]
		out = append(out, calc.Resource{
			ID: l.ID, Code: l.Code, Type: calc.TypeLand, Family: calc.FamilyLand, MapID: l.MapID, MapKind: set.MapKinds[l.MapID],
			GroupIDs: set.Groups[l.ID], Category: l.PriceClass, Length: geo.Cm(l.LengthCm), Width: geo.Cm(l.WidthCm),
			HoldsBoat: l.Kind == fit.LandCradle || l.Kind == fit.LandTrailerSpot,
		})
	}
	r.resources = out
	return out, nil
}

// berthTarget returns the fit target of an active berth (nil when unknown).
func (r *run) berthTarget(id string) *fit.Target {
	if r.targets == nil {
		r.targets = make(map[string]*fit.Target, len(r.res.Berths))
		for i := range r.res.Berths {
			b := &r.res.Berths[i]
			t := fit.Target{
				Kind: fit.TargetBerth, MaxLOA: geo.Cm(b.MaxLOACm), MaxBeam: geo.Cm(b.MaxBeamCm), Length: geo.Cm(b.LengthCm),
				Width: geo.Cm(b.WidthCm), MultihullOK: slices.Contains(b.Attributes, occupancy.TagMultihullOK), OverhangAllowed: b.OverhangAllowed,
			}
			if b.DepthCm > 0 {
				d := geo.Cm(b.DepthCm)
				t.Depth = &d
			}
			if b.MaxAirDraftCm > 0 {
				d := geo.Cm(b.MaxAirDraftCm)
				t.MaxAirDraft = &d
			}
			r.targets[b.ID] = &t
		}
	}
	return r.targets[id]
}

// fitRules returns the fit rules of the marina (defaults of the migrations when no settings row exists).
func (r *run) fitRules() (fit.Rules, error) {
	if r.settings == nil {
		o, err := store.GetOccupancySettings(r.ctx, r.tx, r.scope())
		switch {
		case errors.Is(err, store.ErrNotFound):
			o = &store.OccupancySettings{SafetyClearanceCm: 30, WasteThresholdBP: 6000}
		case err != nil:
			return fit.Rules{}, err
		}
		r.settings = o
		if h, err := store.GetHarborSettings(r.ctx, r.tx, r.scope()); err == nil {
			r.tolerance = geo.Cm(h.LOAToleranceCm)
		}
	}
	return fit.Rules{
		LOATolerance: r.tolerance, SafetyClearance: geo.Cm(r.settings.SafetyClearanceCm),
		DepthWarnMargin: fit.DefaultDepthWarnMargin, WasteThresholdBP: r.settings.WasteThresholdBP,
	}, nil
}

// contractCode is the display code of a contract.
func contractCode(kind string, year int, number int64) string {
	if kind == "" || number == 0 {
		return ""
	}
	series := contract.SeriesMooring
	switch kind {
	case calc.ContractTransit:
		series = contract.SeriesTransit
	case calc.ContractCharter:
		series = contract.SeriesCharter
	}
	return contract.ContractCode(series, year, number)
}

// vesselOf returns the effective size of an allocation's vessel (measured over declared, D7).
func vesselOf(a *store.ReportAllocationRow) fit.Vessel {
	v := fit.Vessel{LOA: geo.Cm(a.LOACm), Beam: geo.Cm(a.BeamCm), Hull: a.HullType, PowerRequiredA: a.PowerRequiredA}
	if a.DimensionsSource == "measured" {
		if a.MeasuredLOACm != nil {
			v.LOA = geo.Cm(*a.MeasuredLOACm)
		}
		if a.MeasuredBeamCm != nil {
			v.Beam = geo.Cm(*a.MeasuredBeamCm)
		}
	}
	draft := a.DraftCm
	if a.DimensionsSource == "measured" && a.MeasuredDraftCm != nil {
		draft = a.MeasuredDraftCm
	}
	if draft != nil {
		d := geo.Cm(*draft)
		v.Draft = &d
	}
	if a.AirDraftCm != nil {
		d := geo.Cm(*a.AirDraftCm)
		v.AirDraft = &d
	}
	return v
}

// allocations converts allocation rows (footprint of the vessels in linear areas included).
func (r *run) allocations(rows []store.ReportAllocationRow) []calc.Allocation {
	out := make([]calc.Allocation, 0, len(rows))
	for i := range rows {
		a := &rows[i]
		v := vesselOf(a)
		ca := calc.Allocation{
			ID: a.ID, Kind: a.Kind, ResourceID: a.ResourceID, ContractID: a.ContractID,
			ContractNumber: contractCode(a.ContractKind, a.ContractYear, a.ContractNumber), ContractKind: a.ContractKind,
			VesselID: a.VesselID, VesselName: a.VesselName, HasVessel: a.VesselID != "", LOA: v.LOA, Beam: v.Beam,
			HullType: a.HullType, Flag: a.Flag, Qualifying: a.ContractID != "" && calc.Qualifying(a.ContractKind, a.ContractStatus),
			Contract: calc.ContractValue{Total: money.Cents(a.ContractTotal), StartsOn: a.ContractStartsOn, EndsOn: a.ContractEndsOn},
		}
		if la, ok := r.areas[a.ResourceID]; ok && la.Kind == layout.AreaLinear {
			ca.Footprint = engine.FootprintWidth(la, layout.Vessel{LOA: v.LOA, Beam: v.Beam})
		}
		out = append(out, ca)
	}
	return out
}

// footprintCells is the number of cells an area allocation holds (1 for berths and land).
func (r *run) footprintCells(a *store.ReportAllocationRow) int64 {
	la, ok := r.areas[a.ResourceID]
	if !ok || a.XCm == nil || a.YCm == nil {
		return 1
	}
	rot := int64(0)
	if a.RotationDd != nil {
		rot = *a.RotationDd
	}
	v := vesselOf(a)
	cells, err := engine.Cells(engine.AllocSpec{
		ResourceType: engine.ResourceArea, ResourceID: a.ResourceID,
		Placement: &engine.Placement{X: geo.Cm(*a.XCm), Y: geo.Cm(*a.YCm), Rotation: geo.Angle(rot)},
		First:     a.FirstNight, Last: a.LastNight,
	}, &la, layout.Vessel{LOA: v.LOA, Beam: v.Beam})
	if err != nil || len(cells) == 0 {
		return 1
	}
	return int64(len(cells))
}

func nightRows(rows ...[]store.ReportNightRow) []calc.NightRow {
	n := 0
	for _, rs := range rows {
		n += len(rs)
	}
	out := make([]calc.NightRow, 0, n)
	for _, rs := range rows {
		for _, x := range rs {
			out = append(out, calc.NightRow{ResourceID: x.ResourceID, Night: x.Night, AllocationID: x.AllocationID, Kind: x.Kind, Cells: x.Cells})
		}
	}
	return out
}

func closures(cs []store.Closure) []calc.Closure {
	out := make([]calc.Closure, 0, len(cs))
	for _, c := range cs {
		out = append(out, calc.Closure{ScopeType: c.ScopeType, ScopeID: c.ScopeID, From: c.FromDate, To: c.ToDate})
	}
	return out
}

// occupancyInput loads what R1, R2, R3, R16 and R17 read for families (berth rows only when only berths are
// needed); closures are read on [from − margin, to + margin] (R3 suggestions look at whole stays).
func (r *run) occupancyInput(from, to calendar.Date, fams []string, withReleases bool, margin int) (calc.OccupancyInput, map[string]*store.ReportAllocationRow, error) {
	in := calc.OccupancyInput{From: from, To: to}
	res, err := r.loadResources()
	if err != nil {
		return in, nil, err
	}
	in.Resources = res
	f := store.ReportFilter{From: from, To: to}
	cs, err := store.ReportClosures(r.ctx, r.tx, r.scope(), store.ReportFilter{From: from.AddDays(-margin), To: to.AddDays(margin)})
	if err != nil {
		return in, nil, err
	}
	in.Closures = closures(cs)
	var rows [][]store.ReportNightRow
	if slices.Contains(fams, calc.FamilyBerth) {
		b, err := store.ReportBerthNights(r.ctx, r.tx, r.scope(), f)
		if err != nil {
			return in, nil, err
		}
		rows = append(rows, b)
	}
	if slices.Contains(fams, calc.FamilyLand) {
		l, err := store.ReportLandNights(r.ctx, r.tx, r.scope(), f)
		if err != nil {
			return in, nil, err
		}
		rows = append(rows, l)
	}
	if slices.Contains(fams, calc.FamilyLinear) || slices.Contains(fams, calc.FamilySurface) {
		c, err := store.ReportCellNights(r.ctx, r.tx, r.scope(), f)
		if err != nil {
			return in, nil, err
		}
		rows = append(rows, c)
	}
	in.Rows = nightRows(rows...)
	arows, err := store.ReportAllocations(r.ctx, r.tx, r.scope(), store.ReportAllocationFilter{From: from, To: to})
	if err != nil {
		return in, nil, err
	}
	in.Allocations = r.allocations(arows)
	byID := make(map[string]*store.ReportAllocationRow, len(arows))
	for i := range arows {
		byID[arows[i].ID] = &arows[i]
	}
	if withReleases {
		rels, err := store.ReportReleases(r.ctx, r.tx, r.scope(), f)
		if err != nil {
			return in, nil, err
		}
		for _, rl := range rels {
			cells := int64(1)
			if a := byID[rl.AllocationID]; a != nil && rl.ResourceType == calc.TypeArea {
				cells = r.footprintCells(a)
			}
			in.Releases = append(in.Releases, calc.Release{
				ID: rl.ID, AllocationID: rl.AllocationID, ResourceID: rl.ResourceID, ResoldAllocationID: rl.ResoldAllocationID,
				ContractID: rl.ContractID, ContractNumber: contractCode(rl.ContractKind, rl.ContractYear, rl.ContractNumber),
				From: rl.FromNight, To: rl.ToNight, Cells: cells,
			})
		}
	}
	return in, byID, nil
}

// customerInfos returns the profiles of customers (names only with customers.read).
func (r *run) customerInfos(ids []string) (map[string]calc.CustomerInfo, error) {
	var missing []string
	for _, id := range ids {
		if _, ok := r.nameCache[id]; !ok && id != "" && !slices.Contains(missing, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		rows, err := store.ReportCustomers(r.ctx, r.tx, r.scope(), missing)
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			info := calc.CustomerInfo{ID: c.ID, Category: c.Category, Nationality: c.Nationality}
			if r.names {
				sc := store.Customer{Kind: c.Kind, FirstName: c.FirstName, LastName: c.LastName, CompanyName: c.CompanyName}
				if c.IsAnonymized {
					sc.AnonymizedAt = time.Unix(1, 0).UTC()
				}
				info.Name = customer.DisplayName(&sc)
			}
			r.nameCache[c.ID] = info
		}
	}
	out := make(map[string]calc.CustomerInfo, len(ids))
	for _, id := range ids {
		if info, ok := r.nameCache[id]; ok {
			out[id] = info
		}
	}
	return out, nil
}

// names returns the display names of customers (empty without customers.read).
func (r *run) customerNames(ids []string) (map[string]string, error) {
	out := map[string]string{}
	if !r.names {
		return out, nil
	}
	infos, err := r.customerInfos(ids)
	if err != nil {
		return nil, err
	}
	for id, i := range infos {
		out[id] = i.Name
	}
	return out, nil
}

// localDayStart is the instant of midnight of d in the marina time zone.
func (r *run) localDayStart(d calendar.Date) time.Time { return d.Time(r.loc) }
