package contract

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// NewVessel is a vessel declared at the desk or on the dock (transit for an unknown vessel, unexpected arrival,
// m3-plan §13 D29): estimated dimensions as declared; the dock may record a measurement later.
type NewVessel struct {
	Name     string
	LOACm    int64
	BeamCm   int64
	HullType string
	DraftCm  int64 // 0 = unknown
}

var hullTypes = []string{"monohull_motor", "monohull_sail", "catamaran", "trimaran", "rib", "gozzo", "other"}

// checkNewVessel validates a declared vessel.
func checkNewVessel(v *auth.ValidationError, field string, nv *NewVessel) {
	name, ok := cleanText(nv.Name, 120)
	if name == "" {
		v.Add(field+".name", "required")
	} else if !ok {
		v.Add(field+".name", "too_long")
	}
	nv.Name = name
	if nv.LOACm < 1 || nv.LOACm > 20000 {
		v.Add(field+".loa_cm", "out_of_range")
	}
	if nv.BeamCm < 1 || nv.BeamCm > 20000 {
		v.Add(field+".beam_cm", "out_of_range")
	}
	if nv.DraftCm < 0 || nv.DraftCm > 5000 {
		v.Add(field+".draft_cm", "out_of_range")
	}
	if nv.HullType == "" {
		nv.HullType = "other"
	}
	if !slices.Contains(hullTypes, nv.HullType) {
		v.Add(field+".hull_type", "invalid")
	}
}

// NewVesselTx inserts a declared vessel in tx with its audit row and vessel.changed event (the registry write of
// a desk or dock flow, done in the flow's transaction). The name is never logged nor emitted.
func (s *Service) NewVesselTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, nv NewVessel, now time.Time) (*store.Vessel, error) {
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: scope.ID(), Name: nv.Name, HullType: nv.HullType, LOACm: nv.LOACm, BeamCm: nv.BeamCm,
		DraftCm: nv.DraftCm, DimensionsSource: "declared", Engines: store.JSON("[]"),
		SearchText: strings.TrimSpace(customer.Fold(nv.Name)), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertVessel(ctx, tx, v); err != nil {
		return nil, err
	}
	if err := s.d.Audit.Write(ctx, tx, auditEntry(scope, actor{p}, "vessel.create", "vessel", v.ID,
		map[string]any{"fields": []string{"name", "loa_cm", "beam_cm", "hull_type", "draft_cm"}, "origin": "desk"})); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.VesselChanged, event.VesselChangedData{VesselID: v.ID, Change: event.ChangeCreated}); err != nil {
		return nil, err
	}
	return v, nil
}
