package harbor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/store"
)

// SaveMap replaces the whole document of a map when its version equals ifMatch (harbor.configure; spec 8.5,
// m1-plan §3.3). The complete post-save state is validated inside the transaction: a document with errors is
// refused with *MapInvalidError and nothing is written.
func (s *Service) SaveMap(ctx context.Context, p *auth.Principal, marinaID, mapID string, ifMatch int, req SaveRequest) (*SaveResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	var out *SaveResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.LockMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		out, err = s.applyDocument(ctx, tx, p, scope, m, ifMatch, &req, "map.save", event.ChangeUpdated)
		return err
	})
	return out, err
}

// ValidateMap runs the save validation on a document without writing (harbor.configure): field errors are
// validation_failed, the geometric report is returned (errors and warnings).
func (s *Service) ValidateMap(ctx context.Context, p *auth.Principal, marinaID, mapID string, req SaveRequest) (layout.Report, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return layout.Report{}, err
	}
	var rep layout.Report
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetMap(ctx, tx, scope, mapID); err != nil {
			return err
		}
		prep, settings, err := s.prepareIn(ctx, tx, scope, mapID, &req)
		if err != nil {
			return err
		}
		rep = layout.ValidateMap(prep.layout, settings.validation())
		return nil
	})
	return rep, err
}

// prepareIn checks a document against the saved state of a map inside tx.
func (s *Service) prepareIn(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mapID string, req *SaveRequest) (*prepared, Settings, error) {
	elems, groups, files, err := docContext(ctx, tx, scope, mapID)
	if err != nil {
		return nil, Settings{}, err
	}
	settings, err := loadSettings(ctx, tx, scope)
	if err != nil {
		return nil, Settings{}, err
	}
	prep, err := prepare(req, existingOf(elems), groups, files)
	if err != nil {
		return nil, Settings{}, err
	}
	return prep, settings, nil
}

// applyDocument is the single write path of the harbour map elements (INV-04, m1-plan §3.3). It runs inside
// the caller's transaction, after the map row was locked: version check, field checks and reference
// resolution, ValidateMap on the complete post-save state, the occupancy guard, the element diff, the group
// memberships, the map version bump, audit and outbox. Nothing is written when any step fails.
func (s *Service) applyDocument(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, m *store.Map, expected int,
	req *SaveRequest, action, change string,
) (*SaveResult, error) {
	if m.Version != expected {
		return nil, ErrVersionMismatch
	}
	before, groups, files, err := docContext(ctx, tx, scope, m.ID)
	if err != nil {
		return nil, err
	}
	settings, err := loadSettings(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	prep, err := prepare(req, existingOf(before), groups, files)
	if err != nil {
		return nil, err
	}
	rep := layout.ValidateMap(prep.layout, settings.validation())
	if !rep.OK() {
		return nil, &MapInvalidError{Report: rep}
	}
	now := s.now()
	after, members, err := prep.rows(now)
	if err != nil {
		return nil, err
	}
	keepCreated(before, after)
	if err := s.d.Guard.CheckChanges(ctx, tx, scope, m.ID, elementChanges(before, after)); err != nil {
		return nil, err
	}
	sum, err := store.SaveMapElements(ctx, tx, scope, m.ID, before, after)
	if err != nil {
		// An element with occupancy rows (past nights included) cannot be deleted: the RESTRICT foreign keys of
		// 00006_occupancy refuse it (m3-plan §3.7). History is kept; deactivate the element instead.
		if store.IsForeignKeyViolation(err) {
			return nil, fmt.Errorf("%w: %w", ErrElementInUse, err)
		}
		return nil, err
	}
	if err := store.ReplaceGroupMembers(ctx, tx, scope, m.ID, members); err != nil {
		return nil, err
	}
	m.Kind, m.Name, m.Icon = req.Kind, store.MustJSON(req.Name), req.Icon
	m.WidthCm, m.HeightCm, m.GridCm = req.WidthCm, req.HeightCm, req.GridCm
	m.BackgroundFileID, m.Background = "", nil
	if b := req.Background; b != nil {
		m.BackgroundFileID = b.FileID
		m.Background = store.MustJSON(b)
	}
	m.Georef = nil
	if req.Georef != nil {
		m.Georef = store.MustJSON(req.Georef)
	}
	m.ConcessionOutline = nil
	if len(req.ConcessionOutline) > 0 {
		m.ConcessionOutline = store.MustJSON(req.ConcessionOutline)
	}
	m.WaterPolygons = store.MustJSON(nonNilPolys(req.WaterPolygons))
	m.LandPolygons = store.MustJSON(nonNilPolys(req.LandPolygons))
	m.UpdatedAt = now
	if err := store.UpdateMap(ctx, tx, scope, m, expected); err != nil {
		if errors.Is(err, store.ErrVersionMismatch) {
			return nil, ErrVersionMismatch
		}
		return nil, err
	}
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: "map", EntityID: m.ID,
		Diff: map[string]any{"added": sum.Added, "updated": sum.Updated, "removed": sum.Removed, "version": m.Version},
	}); err != nil {
		return nil, err
	}
	if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{MapID: m.ID, Version: m.Version, Change: change}); err != nil {
		return nil, err
	}
	doc, err := loadDocument(ctx, tx, scope, m)
	if err != nil {
		return nil, err
	}
	return &SaveResult{Document: *doc, IDMap: prep.idMap, Warnings: rep.Warnings}, nil
}

func nonNilPolys[T any](p [][]T) [][]T {
	if p == nil {
		return [][]T{}
	}
	return p
}

// keepCreated carries the creation time of saved rows over to their new version.
func keepCreated(before, after *store.MapElements) {
	created := map[string]any{}
	for _, r := range before.Structures {
		created[r.ID] = r.CreatedAt
	}
	for _, r := range before.Pedestals {
		created[r.ID] = r.CreatedAt
	}
	for _, r := range before.Berths {
		created[r.ID] = r.CreatedAt
	}
	for _, r := range before.Areas {
		created[r.ID] = r.CreatedAt
	}
	for _, r := range before.LandResources {
		created[r.ID] = r.CreatedAt
	}
	for _, r := range before.Decorations {
		created[r.ID] = r.CreatedAt
	}
	set := func(id string, dst any) {
		if c, ok := created[id]; ok {
			reflect.ValueOf(dst).Elem().Set(reflect.ValueOf(c))
		}
	}
	for i := range after.Structures {
		set(after.Structures[i].ID, &after.Structures[i].CreatedAt)
	}
	for i := range after.Pedestals {
		set(after.Pedestals[i].ID, &after.Pedestals[i].CreatedAt)
	}
	for i := range after.Berths {
		set(after.Berths[i].ID, &after.Berths[i].CreatedAt)
	}
	for i := range after.Areas {
		set(after.Areas[i].ID, &after.Areas[i].CreatedAt)
	}
	for i := range after.LandResources {
		set(after.LandResources[i].ID, &after.LandResources[i].CreatedAt)
	}
	for i := range after.Decorations {
		set(after.Decorations[i].ID, &after.Decorations[i].CreatedAt)
	}
}

// elementChanges lists the removed and reshaped occupiable elements, for the occupancy guard (spec 8.5).
func elementChanges(before, after *store.MapElements) []ElementChange {
	var out []ElementChange
	berths := map[string]*store.Berth{}
	for i := range after.Berths {
		berths[after.Berths[i].ID] = &after.Berths[i]
	}
	for i := range before.Berths {
		b := &before.Berths[i]
		n, ok := berths[b.ID]
		switch {
		case !ok:
			out = append(out, ElementChange{Kind: resourceBerth, ID: b.ID, Removed: true})
		case n.AnchorXCm != b.AnchorXCm || n.AnchorYCm != b.AnchorYCm || n.RotationDd != b.RotationDd || n.WidthCm != b.WidthCm ||
			n.LengthCm != b.LengthCm || n.MaxLOACm != b.MaxLOACm || n.MaxBeamCm != b.MaxBeamCm || n.SwingRadiusCm != b.SwingRadiusCm:
			out = append(out, ElementChange{Kind: resourceBerth, ID: b.ID, GeometryChanged: true})
		}
	}
	areas := map[string]*store.DynamicArea{}
	for i := range after.Areas {
		areas[after.Areas[i].ID] = &after.Areas[i]
	}
	for i := range before.Areas {
		a := &before.Areas[i]
		n, ok := areas[a.ID]
		switch {
		case !ok:
			out = append(out, ElementChange{Kind: resourceArea, ID: a.ID, Removed: true})
		case areaShapeChanged(a, n):
			out = append(out, ElementChange{Kind: resourceArea, ID: a.ID, GeometryChanged: true})
		}
	}
	lands := map[string]*store.LandResource{}
	for i := range after.LandResources {
		lands[after.LandResources[i].ID] = &after.LandResources[i]
	}
	for i := range before.LandResources {
		l := &before.LandResources[i]
		n, ok := lands[l.ID]
		switch {
		case !ok:
			out = append(out, ElementChange{Kind: resourceLand, ID: l.ID, Removed: true})
		case n.XCm != l.XCm || n.YCm != l.YCm || n.RotationDd != l.RotationDd || n.WidthCm != l.WidthCm || n.LengthCm != l.LengthCm:
			out = append(out, ElementChange{Kind: resourceLand, ID: l.ID, GeometryChanged: true})
		}
	}
	return out
}

// areaShapeChanged reports whether an area changed in a way that can invalidate placements.
func areaShapeChanged(a, b *store.DynamicArea) bool {
	ptrEq := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Kind != b.Kind || a.StructureID != b.StructureID || a.Side != b.Side || !ptrEq(a.FromCm, b.FromCm) || !ptrEq(a.ToCm, b.ToCm) ||
		a.MooringMode != b.MooringMode || a.CellCm != b.CellCm || a.MarginCm != b.MarginCm || a.MaxLOACm != b.MaxLOACm ||
		a.MaxBeamCm != b.MaxBeamCm || a.MaxHeightCm != b.MaxHeightCm || a.GridRotationDd != b.GridRotationDd ||
		!bytes.Equal(a.Outline.Compact(), b.Outline.Compact()) || !bytes.Equal(a.BlockedCells.Compact(), b.BlockedCells.Compact())
}
