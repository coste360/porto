package storage

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Log       *slog.Logger
	Occupancy *occupancy.Service
	Contracts *contract.Service
	Prices    *pricelist.Service
	// OptimizeBudget is the deadline around optimizer.Arrange (0 = DefaultOptimizeBudget).
	OptimizeBudget time.Duration
}

// Service is the storage service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.OptimizeBudget <= 0 {
		d.OptimizeBudget = DefaultOptimizeBudget
	}
	return &Service{d: d}
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) writeAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, data any) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// ---- storage contracts (m9-plan §3) ----

// CreateContract creates a storage contract (storage.manage; fit override needs occupancy.override_fit). Without a
// placement on an area, optimizer.PlaceOne chooses one on the state of the area read under its lock; no position is
// ErrNoSpace (409).
func (s *Service) CreateContract(ctx context.Context, p *auth.Principal, marinaID string, in ContractInput) (*ContractView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	if err := s.d.Contracts.AuthorizeFitOverride(ctx, p, marinaID, in.FitOverride); err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if (in.AreaID == "") == (in.TrailerSpotID == "") {
		v.Add("area_id", "required")
	}
	if in.TrailerSpotID != "" && in.Placement != nil {
		v.Add("placement", "not_allowed")
	}
	if in.Placement != nil && in.Placement.Rotation%900 != 0 {
		v.Add("placement.rotation_dd", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	ci := contract.StorageInput{
		Term: in.Term, CustomerID: in.CustomerID, BillingCustomerID: in.BillingCustomerID, VesselID: in.VesselID,
		StartsOn: in.StartsOn, EndsOn: in.EndsOn, Services: in.Services, Policy: in.Policy, Notes: in.Notes,
		Activate: in.Activate, FitOverride: in.FitOverride, Details: in.Details,
	}
	var ar *area
	if in.AreaID != "" {
		if ar, err = loadArea(ctx, db, scope, in.AreaID); err != nil {
			return nil, err
		}
		ci.Placement = contract.Segment{ResourceType: store.ResourceArea, ResourceID: in.AreaID}
	} else {
		lr, err := store.LandResourceByID(ctx, db, scope, in.TrailerSpotID)
		if err != nil {
			return nil, notFound(err)
		}
		if lr.Kind != fit.LandTrailerSpot {
			return nil, ErrTargetInvalid
		}
		ci.Placement = contract.Segment{ResourceType: store.ResourceLand, ResourceID: in.TrailerSpotID}
	}
	if in.CradleResourceID != "" {
		lr, err := store.LandResourceByID(ctx, db, scope, in.CradleResourceID)
		if err != nil {
			return nil, notFound(err)
		}
		if lr.Kind != fit.LandCradle {
			v.Add("cradle_resource_id", "invalid")
			return nil, v
		}
		ci.Cradle = &contract.ExtraResource{LandResourceID: lr.ID}
	}
	if err := s.d.Contracts.PrepareStorage(ctx, p, scope, &ci); err != nil {
		return nil, err
	}
	vessel, err := store.GetVessel(ctx, db, scope, in.VesselID)
	if err != nil {
		return nil, notFound(err)
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if ar != nil {
			if _, err := store.LockDynamicArea(ctx, tx, scope, ar.row.ID); err != nil {
				return notFound(err)
			}
			pl, err := s.placeNew(ctx, tx, scope, ar, vessel, &ci, in.Placement)
			if err != nil {
				return err
			}
			x, y, rot := int64(pl.X), int64(pl.Y), int64(pl.Rotation)
			ci.Placement.X, ci.Placement.Y, ci.Placement.RotationDd = &x, &y, &rot
		}
		d, err := s.d.Contracts.CreateStorageTx(ctx, tx, scope, contract.Desk(p), ci)
		if err != nil {
			return err
		}
		id = d.Row.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.contractView(ctx, db, scope, p, id)
}

// placeNew returns the position of a new storage contract on an area: the given one, or optimizer.PlaceOne on the
// state of the area for the period (read in tx, under the area lock). It checks fit with supports and weight
// (support_not_allowed is never overridable).
func (s *Service) placeNew(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ar *area, vessel *store.Vessel,
	ci *contract.StorageInput, given *optimizer.Placement,
) (optimizer.Placement, error) {
	first, last := ci.StartsOn, ci.Prepared.EndsOn.AddDays(-1)
	it := SnapshotItem{
		Allocation:   store.Allocation{ID: "new", VesselID: vessel.ID, FirstNight: first, LastNight: last},
		ContractKind: contract.KindStorage, Vessel: *vessel, Fit: vesselFit(vessel), Support: ci.Details.Support,
	}
	if vessel.DisplacementKg > 0 {
		d := vessel.DisplacementKg
		it.Displacement = &d
	}
	det := &store.StorageContractDetails{}
	if ci.Details.HaulOutOn != nil {
		det.HaulOutOn = *ci.Details.HaulOutOn
	}
	if ci.Details.LaunchOn != nil {
		det.LaunchOn = *ci.Details.LaunchOn
	}
	it.ArrivalOn = contract.StorageArrivalOn(det, first, ci.Prepared.EndsOn)
	it.ExitOn = contract.StorageExitOn(det, ci.Prepared.EndsOn)
	if r := ar.check(&it, last); !r.OK && (ci.FitOverride == nil || !r.Overridable) {
		return optimizer.Placement{}, &occupancy.FitError{Result: r}
	}
	if given != nil {
		return *given, nil
	}
	sn, err := readSnapshot(ctx, tx, scope, ar, first, last, nil)
	if err != nil {
		return optimizer.Placement{}, err
	}
	in := optimizer.Input{
		Grid: ar.grid, Fixed: sn.obstacles(), Fit: ar.fitParams(), Margin: ar.la.Margin, From: first, To: last,
	}
	if ci.FitOverride != nil {
		// The office accepted the dimensional codes: the position search ignores them.
		in.Fit.Target.MaxLOA, in.Fit.Target.MaxBeam, in.Fit.Target.MaxAirDraft, in.Fit.MaxWeightPerCellKg = 0, 0, nil, nil
	}
	for i := range sn.Items {
		x := &sn.Items[i]
		if !x.OnArea || len(x.Cells) == 0 {
			continue
		}
		if f, l, ok := clip(x.Allocation.FirstNight, x.Allocation.LastNight, first, last); ok {
			in.Items = append(in.Items, x.optItem(f, l, x.Placement(), false))
		}
	}
	if len(in.Items) >= optimizer.MaxItems {
		// Too many boats for the accessibility evaluation: the others become plain obstacles.
		for _, x := range in.Items {
			in.Fixed = append(in.Fixed, optimizer.Obstacle{ID: x.ID, Cells: sn.item(x.ID).Cells, First: x.First, Last: x.Last})
		}
		in.Items = nil
	}
	pl, err := optimizer.PlaceOne(in, it.optItem(first, last, nil, false))
	if err != nil {
		return optimizer.Placement{}, err
	}
	if pl == nil {
		return optimizer.Placement{}, ErrNoSpace
	}
	return *pl, nil
}

// GetContract reads a storage contract (storage.read).
func (s *Service) GetContract(ctx context.Context, p *auth.Principal, marinaID, contractID string) (*ContractView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	return s.contractView(ctx, db, scope, p, contractID)
}

// UpdateContract changes the storage details (storage.manage, If-Match on the details version).
func (s *Service) UpdateContract(ctx context.Context, p *auth.Principal, marinaID, contractID string, version int64,
	patch contract.StorageDetailsPatch,
) (*ContractView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := s.d.Contracts.UpdateStorageDetailsTx(ctx, tx, scope, contract.Desk(p), contractID, version, patch)
		return err
	})
	if errors.Is(err, contract.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	if err != nil {
		return nil, err
	}
	return s.contractView(ctx, db, scope, p, contractID)
}

// contractView reads a storage contract with its details, placement, cradle and fit.
func (s *Service) contractView(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*ContractView, error) {
	d, err := s.d.Contracts.StorageDetail(ctx, db, scope, p, id)
	if err != nil {
		return nil, err
	}
	det, err := store.GetStorageDetails(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	v := &ContractView{
		Contract: d, Details: *det, ExitOn: contract.StorageExitOn(det, d.Row.EndsOn),
		ArrivalOn: contract.StorageArrivalOn(det, d.Row.StartsOn, d.Row.EndsOn),
	}
	cradleLines := map[string]bool{}
	for _, l := range d.Lines {
		if l.LineKind == "resource" && l.Seq > 1 {
			cradleLines[l.ID] = true
		}
	}
	today := s.today(ctx, db, scope)
	pick := func(cur *store.Allocation, a *store.Allocation) *store.Allocation {
		// The allocation covering today, else the latest one.
		if cur == nil {
			return a
		}
		curCovers := !cur.FirstNight.After(today) && !cur.LastNight.Before(today)
		if !curCovers && (a.FirstNight.After(cur.FirstNight) || (!a.FirstNight.After(today) && !a.LastNight.Before(today))) {
			return a
		}
		return cur
	}
	for i := range d.Allocations {
		a := &d.Allocations[i]
		if a.Status != store.AllocActive {
			continue
		}
		if cradleLines[a.ContractLineID] {
			v.Cradle = pick(v.Cradle, a)
		} else {
			v.Placement = pick(v.Placement, a)
		}
	}
	if v.Placement != nil && v.Placement.ResourceType == store.ResourceArea {
		if ar, err := loadArea(ctx, db, scope, v.Placement.ResourceID); err == nil {
			it := SnapshotItem{Vessel: d.Vessel, Fit: vesselFit(&d.Vessel), Support: det.Support}
			if d.Vessel.DisplacementKg > 0 {
				w := d.Vessel.DisplacementKg
				it.Displacement = &w
			}
			r := ar.check(&it, v.Placement.LastNight)
			v.Fit = &r
		}
	}
	return v, nil
}

// ---- areas, plan, accessibility, preset (m9-plan §4–§6) ----

// ListAreas lists the surface areas of yard and shed maps (storage.read).
func (s *Service) ListAreas(ctx context.Context, p *auth.Principal, marinaID string) ([]AreaSummary, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.StorageAreas(ctx, db, scope, "")
	if err != nil {
		return nil, err
	}
	drafts, err := store.DraftScenarioCounts(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	out := make([]AreaSummary, 0, len(rows))
	for _, r := range rows {
		ar, err := loadArea(ctx, db, scope, r.ID)
		if errors.Is(err, ErrTargetInvalid) {
			continue // an outline that does not make a grid
		}
		if err != nil {
			return nil, err
		}
		sn, err := readSnapshot(ctx, db, scope, ar, today, today.AddDays(PlanDefaultDays), nil)
		if err != nil {
			return nil, err
		}
		sum := s.summary(ar, sn, today)
		sum.DraftScenarios = drafts[r.ID]
		out = append(out, sum)
	}
	return out, nil
}

// summary computes the area row of a snapshot starting today.
func (s *Service) summary(ar *area, sn *Snapshot, today calendar.Date) AreaSummary {
	sum := AreaSummary{
		Area: ar.row.DynamicArea, MapKind: ar.row.MapKind, CellCm: ar.la.Cell, Cols: ar.la.Cols, Rows: ar.la.Rows,
		GatesValid: len(ar.gates) == 0 && len(ar.grid.Gates) > 0,
	}
	for _, u := range ar.la.Usable {
		if u {
			sum.UsableCells++
		}
	}
	sum.UsableCm2 = int64(sum.UsableCells) * int64(ar.la.Cell) * int64(ar.la.Cell)
	cells := map[int]bool{}
	for i := range sn.Items {
		it := &sn.Items[i]
		if !it.OnArea || it.Allocation.FirstNight.After(today) || it.Allocation.LastNight.Before(today) {
			continue
		}
		sum.BoatsToday++
		for _, c := range it.Cells {
			cells[c] = true
		}
	}
	sum.OccupiedCellsToday = len(cells)
	ev := evaluate(ar, sn, realLayout(sn))
	for _, a := range ev.access {
		if a.Event == optimizer.EventExit && !a.Accessible && a.On.Before(today.AddDays(PlanDefaultDays+1)) {
			sum.ExitsBlockedNext30++
		}
	}
	return sum
}

// Plan returns the plan and timeline of an area for a night window (default today .. today + 30) (storage.read).
func (s *Service) Plan(ctx context.Context, p *auth.Principal, marinaID, areaID string, from, to *calendar.Date) (*PlanView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	ar, err := loadArea(ctx, db, scope, areaID)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	f, t, err := window(today, from, to)
	if err != nil {
		return nil, err
	}
	sn, err := readSnapshot(ctx, db, scope, ar, f, t, nil)
	if err != nil {
		return nil, err
	}
	drafts, err := store.DraftScenarioCounts(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	sum := s.summary(ar, sn, f)
	sum.DraftScenarios = drafts[areaID]
	ev := evaluate(ar, sn, realLayout(sn))
	return &PlanView{Summary: sum, Snapshot: sn, Access: ev.access}, nil
}

// Snapshot reads the state of an area for [from, to] (storage.read).
func (s *Service) Snapshot(ctx context.Context, p *auth.Principal, marinaID, areaID string, from, to *calendar.Date) (*Snapshot, error) {
	pv, err := s.Plan(ctx, p, marinaID, areaID, from, to)
	if err != nil {
		return nil, err
	}
	return pv.Snapshot, nil
}

// Accessibility evaluates the exits of a date (default today) (storage.read): every boat present that morning,
// with the extraction order of the boats leaving that day and the blocking boats of the blocked ones.
func (s *Service) Accessibility(ctx context.Context, p *auth.Principal, marinaID, areaID string, date *calendar.Date) (*AccessView, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	ar, err := loadArea(ctx, db, scope, areaID)
	if err != nil {
		return nil, err
	}
	d := s.today(ctx, db, scope)
	if date != nil {
		d = *date
	}
	sn, err := readSnapshot(ctx, db, scope, ar, d.AddDays(-1), d, nil)
	if err != nil {
		return nil, err
	}
	return accessView(ar, sn, d), nil
}

// accessView evaluates every boat present on the morning of d: the ones leaving that day are extracted greedily,
// the others are tested as if they had to leave (their blocking boats are the extraction cut).
func accessView(ar *area, sn *Snapshot, d calendar.Date) *AccessView {
	var present []optimizer.Placed
	var exiting, staying []string
	vessel := map[string]string{}
	for i := range sn.Items {
		it := &sn.Items[i]
		at := it.Placement()
		if at == nil || len(it.Cells) == 0 || !it.ArrivalOn.Before(d) || it.ExitOn.Before(d) {
			continue
		}
		vessel[it.Allocation.ID] = it.Vessel.ID
		present = append(present, optimizer.Placed{
			ID: it.Allocation.ID, LOA: it.Fit.LOA, Beam: it.Fit.Beam, At: *at, First: it.Allocation.FirstNight,
			Last: it.Allocation.LastNight, ExitOn: it.ExitOn,
		})
		if it.ExitOn.Equal(d) {
			exiting = append(exiting, it.Allocation.ID)
		} else {
			staying = append(staying, it.Allocation.ID)
		}
	}
	g := ar.grid
	// Obstacles of the morning (covering night d−1: exits come before the rows starting on d, m9-plan D7) are part
	// of the grid as unusable cells.
	g.Area.Usable = slices.Clone(g.Area.Usable)
	for _, o := range sn.Obstacles {
		if prev := d.AddDays(-1); !o.First.After(prev) && !o.Last.Before(prev) {
			for _, c := range o.Cells {
				if c >= 0 && c < len(g.Area.Usable) {
					g.Area.Usable[c] = false
				}
			}
		}
	}
	view := &AccessView{AreaID: ar.row.ID, Date: d, Entries: []Access{}, ExtractionOrder: []string{}}
	acc := optimizer.Accessibility(g, present, exiting, d)
	for _, id := range staying {
		one := optimizer.Accessibility(g, present, []string{id}, d)
		for _, a := range one {
			a.Order = 0
			acc = append(acc, a)
		}
	}
	slices.SortFunc(acc, func(x, y optimizer.Access) int {
		ox, oy := x.Order, y.Order
		if ox == 0 {
			ox = 1 << 30
		}
		if oy == 0 {
			oy = 1 << 30
		}
		return cmp.Or(cmp.Compare(ox, oy), cmp.Compare(x.ID, y.ID))
	})
	for _, a := range acc {
		view.Entries = append(view.Entries, Access{Access: a, VesselID: vessel[a.ID]})
		if a.Order > 0 {
			view.ExtractionOrder = append(view.ExtractionOrder, a.ID)
		}
	}
	return view
}

// Arrange runs the optimizer on the real state or a draft; nothing is saved (storage.manage).
func (s *Service) Arrange(ctx context.Context, p *auth.Principal, marinaID, areaID string, in ArrangeInput) (*ArrangeResult, error) {
	ctx, cancel := s.budget(ctx)
	defer cancel()
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	ar, err := loadArea(ctx, db, scope, areaID)
	if err != nil {
		return nil, err
	}
	f, t, err := window(s.today(ctx, db, scope), in.From, in.To)
	if err != nil {
		return nil, err
	}
	sn, err := readSnapshot(ctx, db, scope, ar, f, t, nil)
	if err != nil {
		return nil, err
	}
	lay := realLayout(sn)
	if in.Draft != nil {
		if lay, err = draftLayout(sn, in.Draft); err != nil {
			return nil, err
		}
	}
	seed := SeedOf(areaID + f.String())
	if in.Seed != nil {
		seed = *in.Seed
	}
	res, err := s.arrange(ctx, ar, sn, lay, seed, in.MaxIter)
	if err != nil {
		return nil, err
	}
	ev := evaluate(ar, withFixed(sn, lay), layoutOf(sn, lay, res))
	return &ArrangeResult{
		Items: res.Placements, Indicators: ev.ind, Access: accessOf(sn, res.Access), Seed: seed, Iterations: res.Iterations,
		StoppedBy: res.StoppedBy,
	}, nil
}

// Evaluate computes indicators, accessibility and issues of a draft; nothing is saved (storage.read).
func (s *Service) Evaluate(ctx context.Context, p *auth.Principal, marinaID, areaID string, from, to *calendar.Date,
	draft []LayoutItem,
) (*Evaluation, error) {
	db := s.d.DB.DB
	scope, err := p.Authorize(ctx, db, marinaID, auth.PermStorageRead)
	if err != nil {
		return nil, err
	}
	ar, err := loadArea(ctx, db, scope, areaID)
	if err != nil {
		return nil, err
	}
	f, t, err := window(s.today(ctx, db, scope), from, to)
	if err != nil {
		return nil, err
	}
	sn, err := readSnapshot(ctx, db, scope, ar, f, t, nil)
	if err != nil {
		return nil, err
	}
	lay, err := draftLayout(sn, draft)
	if err != nil {
		return nil, err
	}
	ev := evaluate(ar, withFixed(sn, lay), lay)
	return &Evaluation{Indicators: ev.ind, Access: ev.access, Issues: ev.issues}, nil
}
