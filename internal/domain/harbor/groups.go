package harbor

import (
	"context"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Berth groups (spec 8.6): marina-level, overlapping memberships set per element in the map documents.

// Group kinds.
var groupKinds = []string{"structure", "transit", "area", "custom"}

// Group is a berth group with its member counts.
type Group struct {
	ID           string
	Code         string
	Name         I18nText
	Kind         string
	SortOrder    int
	Version      int
	MemberCounts map[string]int // berth, dynamic_area, land_resource
}

// GroupInput are the fields of a new group.
type GroupInput struct {
	Code string
	Name I18nText
	Kind string // default custom
}

// GroupPatch lists the fields to change (nil = unchanged).
type GroupPatch struct {
	Code      *string
	Name      *I18nText
	Kind      *string
	SortOrder *int
}

func groupOf(g *store.BerthGroup, counts map[string]int) Group {
	c := map[string]int{resourceBerth: 0, resourceArea: 0, resourceLand: 0}
	for k, v := range counts {
		c[k] = v
	}
	return Group{ID: g.ID, Code: g.Code, Name: unmarshal[I18nText](g.Name), Kind: g.Kind, SortOrder: g.SortOrder, Version: g.Version, MemberCounts: c}
}

// ListGroups returns the berth groups of a marina with their member counts (map.view).
func (s *Service) ListGroups(ctx context.Context, p *auth.Principal, marinaID string) ([]Group, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out []Group
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		gs, err := store.ListBerthGroups(ctx, tx, scope)
		if err != nil {
			return err
		}
		counts, err := store.GroupMemberCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = make([]Group, 0, len(gs))
		for i := range gs {
			out = append(out, groupOf(&gs[i], counts[gs[i].ID]))
		}
		return nil
	})
	return out, err
}

func (s *Service) groupEvent(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, action, id string, diff any) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: "berth_group", EntityID: id, Diff: diff,
	}); err != nil {
		return err
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{Change: event.ChangeGroups})
	return err
}

// CreateGroup creates a berth group (harbor.configure); a code already taken is a conflict.
func (s *Service) CreateGroup(ctx context.Context, p *auth.Principal, marinaID string, in GroupInput) (*Group, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	in.Code = strings.TrimSpace(in.Code)
	if in.Kind == "" {
		in.Kind = "custom"
	}
	c := checker{&auth.ValidationError{}}
	c.code("code", in.Code)
	c.i18n("name", in.Name, true)
	c.enum("kind", in.Kind, groupKinds)
	if err := c.v.Err(); err != nil {
		return nil, err
	}
	var out Group
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		g, err := s.insertGroup(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		out = groupOf(g, nil)
		return s.groupEvent(ctx, tx, p, scope, "berth_group.create", g.ID, map[string]any{"kind": g.Kind})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) insertGroup(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in GroupInput) (*store.BerthGroup, error) {
	now := s.now()
	gs, err := store.ListBerthGroups(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	order := 0
	for _, g := range gs {
		order = max(order, g.SortOrder+1)
	}
	g := &store.BerthGroup{
		ID: store.NewID(), Code: in.Code, Name: store.MustJSON(in.Name), Kind: in.Kind, SortOrder: order, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertBerthGroup(ctx, tx, scope, g); err != nil {
		return nil, err
	}
	return g, nil
}

// UpdateGroup changes a group when its version equals ifMatch (harbor.configure).
func (s *Service) UpdateGroup(ctx context.Context, p *auth.Principal, marinaID, groupID string, ifMatch int, in GroupPatch) (*Group, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	c := checker{&auth.ValidationError{}}
	if in.Code != nil {
		*in.Code = strings.TrimSpace(*in.Code)
		c.code("code", *in.Code)
	}
	if in.Name != nil {
		c.i18n("name", *in.Name, true)
	}
	if in.Kind != nil {
		c.enum("kind", *in.Kind, groupKinds)
	}
	if in.SortOrder != nil {
		c.rangeI("sort_order", int64(*in.SortOrder), 0, 100000)
	}
	if err := c.v.Err(); err != nil {
		return nil, err
	}
	var out Group
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		g, err := store.GetBerthGroup(ctx, tx, scope, groupID)
		if err != nil {
			return err
		}
		var fields []string
		if in.Code != nil {
			g.Code = *in.Code
			fields = append(fields, "code")
		}
		if in.Name != nil {
			g.Name = store.MustJSON(*in.Name)
			fields = append(fields, "name")
		}
		if in.Kind != nil {
			g.Kind = *in.Kind
			fields = append(fields, "kind")
		}
		if in.SortOrder != nil {
			g.SortOrder = *in.SortOrder
			fields = append(fields, "sort_order")
		}
		g.UpdatedAt = s.now()
		if err := store.UpdateBerthGroup(ctx, tx, scope, g, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		counts, err := store.GroupMemberCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = groupOf(g, counts[g.ID])
		return s.groupEvent(ctx, tx, p, scope, "berth_group.update", g.ID, map[string]any{"fields": fields, "version": g.Version})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteGroup deletes a group and its memberships when its version equals ifMatch (harbor.configure).
func (s *Service) DeleteGroup(ctx context.Context, p *auth.Principal, marinaID, groupID string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.DeleteBerthGroup(ctx, tx, scope, groupID, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		return s.groupEvent(ctx, tx, p, scope, "berth_group.delete", groupID, nil)
	})
}

// BerthSummary is a berth in the marina-wide list.
type BerthSummary struct {
	ID, MapID, Code, StructureCode, MooringType, PriceClass string
	WidthCm, LengthCm, MaxLOACm, MaxBeamCm, DepthCm         int64
	Active                                                  bool
}

// BerthFilter narrows ListBerths.
type BerthFilter struct{ MapID, GroupID, Q string }

// ListBerths returns a page of the marina's berths (map.view). Unknown map or group ids give an empty page.
func (s *Service) ListBerths(ctx context.Context, p *auth.Principal, marinaID string, f BerthFilter, cursor string, limit int) (store.Page[BerthSummary], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return store.Page[BerthSummary]{}, err
	}
	for _, id := range []string{f.MapID, f.GroupID} {
		if id != "" && !store.ValidID(id) {
			return store.Page[BerthSummary]{Items: []BerthSummary{}}, nil
		}
	}
	page, err := store.ListBerths(ctx, s.d.DB.DB, scope, store.BerthFilter{MapID: f.MapID, GroupID: f.GroupID, Q: strings.TrimSpace(f.Q)}, cursor, limit)
	if err != nil {
		return store.Page[BerthSummary]{}, err
	}
	out := store.Page[BerthSummary]{Items: make([]BerthSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, r := range page.Items {
		out.Items = append(out.Items, BerthSummary{
			ID: r.ID, MapID: r.MapID, Code: r.Code, StructureCode: r.StructureCode, MooringType: r.MooringType, PriceClass: r.PriceClass,
			WidthCm: r.WidthCm, LengthCm: r.LengthCm, MaxLOACm: r.MaxLOACm, MaxBeamCm: r.MaxBeamCm, DepthCm: r.DepthCm, Active: r.Active,
		})
	}
	return out, nil
}
