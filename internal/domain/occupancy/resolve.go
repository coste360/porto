package occupancy

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Attribute tags of berths read by the engine (berths.attributes).
const (
	TagMultihullOK = "multihull_ok"
	TagTransitOnly = "transit_only"
)

// resource is an occupiable element with what the engine needs: its map, its fit target and, for dynamic
// areas, the footprint view and the depth profile.
type resource struct {
	Type        engine.ResourceType
	ID, Code    string
	MapID       string
	StructureID string
	Active      bool
	GroupIDs    []string
	Target      fit.Target
	Area        *layout.Area
	Depth       []engine.DepthPoint
	PowerA      int64
	PowerPhase  string
	Water       bool
	Dedicated   bool
	Online      bool
}

func cmPtr(v int64) *geo.Cm {
	if v <= 0 {
		return nil
	}
	c := geo.Cm(v)
	return &c
}

func i64Ptr(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func berthResource(b *store.Berth) *resource {
	return &resource{
		Type: engine.ResourceBerth, ID: b.ID, Code: b.Code, MapID: b.MapID, StructureID: b.StructureID, Active: b.Active,
		Target: fit.Target{
			Kind: fit.TargetBerth, MaxLOA: geo.Cm(b.MaxLOACm), MaxBeam: geo.Cm(b.MaxBeamCm), Length: geo.Cm(b.LengthCm),
			Width: geo.Cm(b.WidthCm), Depth: cmPtr(b.DepthCm), MaxAirDraft: cmPtr(b.MaxAirDraftCm),
			MultihullOK: slices.Contains(b.Attributes, TagMultihullOK), OverhangAllowed: b.OverhangAllowed, PowerA: i64Ptr(b.PowerA),
		},
		PowerA: b.PowerA, PowerPhase: b.PowerPhase, Water: b.Water, Dedicated: slices.Contains(b.Attributes, TagTransitOnly),
		Online: b.OnlineSellable,
	}
}

type depthSpan struct {
	FromCm  int64 `json:"from_cm"`
	ToCm    int64 `json:"to_cm"`
	DepthCm int64 `json:"depth_cm"`
}

func unmarshalJSON[T any](j store.JSON) T {
	var v T
	if len(j) > 0 {
		_ = json.Unmarshal(j, &v)
	}
	return v
}

// LayoutArea returns the footprint view of a stored dynamic area.
func LayoutArea(a *store.DynamicArea) (layout.Area, error) {
	d := layout.DynamicArea{
		Kind: layout.AreaKind(a.Kind), Mode: layout.MooringMode(a.MooringMode), Cell: geo.Cm(a.CellCm), Margin: geo.Cm(a.MarginCm),
		GridRotation: geo.Angle(a.GridRotationDd), Blocked: unmarshalJSON[[]int](a.BlockedCells),
		Outline: unmarshalJSON[[]geo.Point](a.Outline),
	}
	if a.FromCm != nil && a.ToCm != nil {
		d.From, d.To = geo.Cm(*a.FromCm), geo.Cm(*a.ToCm)
	}
	return layout.AreaOf(d, layout.Map{})
}

func areaResource(a *store.DynamicArea) (*resource, error) {
	la, err := LayoutArea(a)
	if err != nil {
		return nil, err
	}
	kind := fit.TargetLinearArea
	if la.Kind == layout.AreaSurface {
		kind = fit.TargetSurfaceArea
	}
	r := &resource{
		Type: engine.ResourceArea, ID: a.ID, Code: a.Code, MapID: a.MapID, StructureID: a.StructureID, Active: a.Active, Area: &la,
		Target: fit.Target{Kind: kind, MaxLOA: geo.Cm(a.MaxLOACm), MaxBeam: geo.Cm(a.MaxBeamCm), MultihullOK: true},
	}
	if kind == fit.TargetSurfaceArea {
		r.Target.MaxAirDraft = cmPtr(a.MaxHeightCm)
	}
	for _, sp := range unmarshalJSON[[]depthSpan](a.DepthProfile) {
		r.Depth = append(r.Depth, engine.DepthPoint{AtCm: geo.Cm(sp.FromCm), DepthCm: geo.Cm(sp.DepthCm)})
	}
	return r, nil
}

func landResource(l *store.LandResource) *resource {
	return &resource{
		Type: engine.ResourceLand, ID: l.ID, Code: l.Code, MapID: l.MapID, Active: l.Active,
		Target: fit.Target{
			Kind: fit.TargetLand, LandKind: l.Kind, MaxLOA: geo.Cm(l.LengthCm), MaxBeam: geo.Cm(l.WidthCm),
			Length: geo.Cm(l.LengthCm), Width: geo.Cm(l.WidthCm),
		},
	}
}

// loadResource returns a resource of the marina by type and id (ErrNotFound for another marina's id).
func loadResource(ctx context.Context, db bun.IDB, scope store.MarinaScope, typ engine.ResourceType, id string) (*resource, error) {
	var r *resource
	switch typ {
	case engine.ResourceBerth:
		b, err := store.BerthByID(ctx, db, scope, id)
		if err != nil {
			return nil, err
		}
		r = berthResource(b)
	case engine.ResourceArea:
		a, err := store.DynamicAreaByID(ctx, db, scope, id)
		if err != nil {
			return nil, err
		}
		if r, err = areaResource(a); err != nil {
			return nil, &PlacementError{Reason: PlacementOutsideArea}
		}
	case engine.ResourceLand:
		l, err := store.LandResourceByID(ctx, db, scope, id)
		if err != nil {
			return nil, err
		}
		r = landResource(l)
	default:
		return nil, ErrNotFound
	}
	pr, err := store.PricingResourceByID(ctx, db, scope, string(typ), id)
	if err != nil {
		return nil, err
	}
	r.GroupIDs = pr.GroupIDs
	return r, nil
}

// target returns the fit target of a placement on r: the depth of a linear area is the minimum depth under the
// footprint.
func (r *resource) target(p *engine.Placement, v layout.Vessel) fit.Target {
	t := r.Target
	if r.Area != nil && r.Area.Kind == layout.AreaLinear && p != nil {
		t.Depth = engine.MinDepth(r.Depth, p.X, p.X+engine.FootprintWidth(*r.Area, v))
	}
	return t
}

// vesselInfo is the effective size of a vessel (measured when dimensions_source = measured, spec 11.2).
type vesselInfo struct {
	ID     string
	Fit    fit.Vessel
	Layout layout.Vessel
}

func effective(declared, measured int64, useMeasured bool) int64 {
	if useMeasured && measured > 0 {
		return measured
	}
	return declared
}

func vesselOf(v *store.Vessel, insurance map[string]calendar.Date) *vesselInfo {
	m := v.DimensionsSource == "measured"
	loa, beam := effective(v.LOACm, v.MeasuredLOACm, m), effective(v.BeamCm, v.MeasuredBeamCm, m)
	fv := fit.Vessel{
		LOA: geo.Cm(loa), Beam: geo.Cm(beam), Draft: cmPtr(effective(v.DraftCm, v.MeasuredDraftCm, m)),
		AirDraft: cmPtr(effective(v.AirDraftCm, v.MeasuredAirDraftCm, m)), Hull: v.HullType, PowerRequiredA: i64Ptr(v.PowerRequiredA),
	}
	if d, ok := insurance[v.ID]; ok {
		fv.InsuranceUntil = &d
	}
	return &vesselInfo{ID: v.ID, Fit: fv, Layout: layout.Vessel{LOA: fv.LOA, Beam: fv.Beam}}
}

func loadVessel(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*vesselInfo, error) {
	v, err := store.GetVessel(ctx, db, scope, id)
	if err != nil {
		return nil, err
	}
	ins, err := store.InsuranceUntil(ctx, db, scope, []string{id})
	if err != nil {
		return nil, err
	}
	return vesselOf(v, ins), nil
}

// settings returns the occupancy settings of a marina (defaults when never stored) and the fit rules.
func settings(ctx context.Context, db bun.IDB, scope store.MarinaScope, now time.Time) (*store.OccupancySettings, fit.Rules, error) {
	o, err := store.GetOccupancySettings(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		o = store.DefaultOccupancySettings(scope.ID(), now)
	} else if err != nil {
		return nil, fit.Rules{}, err
	}
	tol := int64(layout.DefaultLOATol)
	h, err := store.GetHarborSettings(ctx, db, scope)
	if err == nil {
		tol = h.LOAToleranceCm
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, fit.Rules{}, err
	}
	return o, fit.Rules{
		LOATolerance: geo.Cm(tol), SafetyClearance: geo.Cm(o.SafetyClearanceCm), DepthWarnMargin: fit.DefaultDepthWarnMargin,
		WasteThresholdBP: o.WasteThresholdBP,
	}, nil
}

// marinaLocation returns the time zone of a marina.
func marinaLocation(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*time.Location, error) {
	name, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return calendar.LoadLocation(name)
}

// placementOf returns the placement stored on an allocation (nil for berths and land).
func placementOf(a *store.Allocation) *engine.Placement {
	if a.XCm == nil {
		return nil
	}
	p := &engine.Placement{X: geo.Cm(*a.XCm)}
	if a.YCm != nil {
		p.Y = geo.Cm(*a.YCm)
	}
	if a.RotationDd != nil {
		p.Rotation = geo.Angle(*a.RotationDd)
	}
	return p
}

func setPlacement(a *store.Allocation, p *engine.Placement) {
	a.XCm, a.YCm, a.RotationDd = nil, nil, nil
	if p == nil {
		return
	}
	x, y, r := int64(p.X), int64(p.Y), int64(p.Rotation)
	a.XCm, a.YCm, a.RotationDd = &x, &y, &r
}

// placementError converts footprint and plan errors to *PlacementError (nil when err is not one of them).
func placementError(err error) *PlacementError {
	switch {
	case errors.Is(err, layout.ErrUnsnapped):
		return &PlacementError{Reason: PlacementUnsnapped}
	case errors.Is(err, layout.ErrOutsideArea), errors.Is(err, layout.ErrInvalidArea):
		return &PlacementError{Reason: PlacementOutsideArea}
	case errors.Is(err, layout.ErrBlockedCell):
		return &PlacementError{Reason: PlacementBlockedCell}
	case errors.Is(err, layout.ErrInvalidVessel):
		return &PlacementError{Reason: PlacementInvalidVessel}
	case errors.Is(err, engine.ErrPlacementRequired), errors.Is(err, engine.ErrAreaRequired):
		return &PlacementError{Reason: PlacementRequired}
	case errors.Is(err, engine.ErrPlacementNotAllowed):
		return &PlacementError{Reason: PlacementNotAllowed}
	}
	return nil
}
