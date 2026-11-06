package harbor

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// MapCreate are the fields of a new map.
type MapCreate struct {
	Kind              string
	Name              I18nText
	Icon              string
	WidthCm, HeightCm int64
	GridCm            int64 // 0 = the marina default
}

// ListMaps returns the maps of a marina with their element counts, by sort order (map.view).
func (s *Service) ListMaps(ctx context.Context, p *auth.Principal, marinaID string) ([]Summary, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out []Summary
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		maps, err := store.ListMaps(ctx, tx, scope)
		if err != nil {
			return err
		}
		counts, err := store.CountMapElements(ctx, tx, scope, "")
		if err != nil {
			return err
		}
		out = make([]Summary, 0, len(maps))
		for i := range maps {
			out = append(out, summaryOf(&maps[i], counts[maps[i].ID]))
		}
		return nil
	})
	return out, err
}

// loadDocument reads the complete document of a map.
func loadDocument(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.Map) (*Document, error) {
	e, err := store.LoadMapElements(ctx, db, scope, m.ID)
	if err != nil {
		return nil, err
	}
	members, err := store.GroupMembersOfMap(ctx, db, scope, m.ID)
	if err != nil {
		return nil, err
	}
	d := documentOf(m, e, members)
	return &d, nil
}

// GetMap returns the document of a map (map.view).
func (s *Service) GetMap(ctx context.Context, p *auth.Principal, marinaID, mapID string) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		out, err = loadDocument(ctx, tx, scope, m)
		return err
	})
	return out, err
}

func checkCreate(in *MapCreate) error {
	c := checker{&auth.ValidationError{}}
	if in.Icon == "" {
		in.Icon = "map"
	}
	checkMeta(c, &Meta{Kind: in.Kind, Name: in.Name, Icon: in.Icon, WidthCm: in.WidthCm, HeightCm: in.HeightCm, GridCm: max(in.GridCm, 1)})
	if in.GridCm != 0 {
		c.rangeI("grid_cm", in.GridCm, 1, 10000)
	}
	return c.v.Err()
}

// newMapRow inserts an empty map (inside tx) with the next sort order.
func (s *Service) newMapRow(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in MapCreate) (*store.Map, error) {
	if in.GridCm == 0 {
		st, err := loadSettings(ctx, tx, scope)
		if err != nil {
			return nil, err
		}
		in.GridCm = st.DefaultGridCm
	}
	order, err := store.NextMapSortOrder(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	m := &store.Map{
		ID: store.NewID(), Kind: in.Kind, Name: store.MustJSON(in.Name), Icon: in.Icon, WidthCm: in.WidthCm, HeightCm: in.HeightCm,
		GridCm: in.GridCm, WaterPolygons: store.JSON("[]"), LandPolygons: store.JSON("[]"), SortOrder: order, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertMap(ctx, tx, scope, m); err != nil {
		return nil, err
	}
	return m, nil
}

// CreateMap creates an empty map (harbor.configure).
func (s *Service) CreateMap(ctx context.Context, p *auth.Principal, marinaID string, in MapCreate) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	if err := checkCreate(&in); err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := s.newMapRow(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "map.create", EntityType: "map", EntityID: m.ID,
			Diff: map[string]any{"kind": m.Kind, "width_cm": m.WidthCm, "height_cm": m.HeightCm},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{MapID: m.ID, Version: m.Version, Change: event.ChangeCreated}); err != nil {
			return err
		}
		out, err = loadDocument(ctx, tx, scope, m)
		return err
	})
	return out, err
}

// DeleteMap deletes a map without berths, dynamic areas and land resources (harbor.configure). Structures,
// pedestals and decorations go with it.
func (s *Service) DeleteMap(ctx context.Context, p *auth.Principal, marinaID, mapID string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.LockMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		if m.Version != ifMatch {
			return ErrVersionMismatch
		}
		counts, err := store.CountMapElements(ctx, tx, scope, m.ID)
		if err != nil {
			return err
		}
		if counts[m.ID].Occupiable() > 0 {
			return ErrMapNotEmpty
		}
		if err := store.DeleteMap(ctx, tx, scope, m.ID, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "map.delete", EntityType: "map", EntityID: m.ID,
			Diff: map[string]any{"version": m.Version},
		}); err != nil {
			return err
		}
		_, err = s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{MapID: m.ID, Change: event.ChangeDeleted})
		return err
	})
}

// ReorderMaps sets the order of the maps (harbor.configure): ids must be every map of the marina, once.
func (s *Service) ReorderMaps(ctx context.Context, p *auth.Principal, marinaID string, ids []string) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		maps, err := store.ListMaps(ctx, tx, scope)
		if err != nil {
			return err
		}
		have := make([]string, 0, len(maps))
		for _, m := range maps {
			have = append(have, m.ID)
		}
		want := slices.Clone(ids)
		slices.Sort(have)
		slices.Sort(want)
		if !slices.Equal(have, want) {
			v := &auth.ValidationError{}
			v.Add("map_ids", "must_list_every_map")
			return v
		}
		if err := store.SetMapSortOrders(ctx, tx, scope, ids, s.now()); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "map.reorder", EntityType: "map",
			Diff: map[string]any{"count": len(ids)},
		}); err != nil {
			return err
		}
		_, err = s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{Change: event.ChangeReordered})
		return err
	})
}
