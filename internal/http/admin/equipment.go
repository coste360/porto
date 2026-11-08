package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Equipment registry (M9, m9-plan §8.1). Owned by UTILITIES.

func mntEquipment(e *maintenance.EquipmentView) api.Equipment {
	r := e.Row
	return api.Equipment{
		Id: r.ID, Code: r.Code, Name: r.Name, Kind: api.EquipmentKind(r.Kind), MapId: view.String(r.MapID), XCm: r.XCm, YCm: r.YCm,
		CapacityKg: r.CapacityKg, Active: r.Active, OutOfService: e.OutOfService, Bookable: r.Bookable,
		DefaultMinutes: r.DefaultMinutes, Version: int64(r.Version),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func writeEquipment(w http.ResponseWriter, e *maintenance.EquipmentView, status int) {
	respond.SetETag(w, e.Row.Version)
	respond.JSON(w, status, mntEquipment(e))
}

// mntDecode decodes a body; a malformed one is 400 after the marina check (another marina stays 404).
func (h *Handler) mntDecode(w http.ResponseWriter, r *http.Request, marinaID string, v any, optional bool) bool {
	if err := respond.Decode(r, v, optional); err != nil {
		h.badPricingBody(w, r, marinaID)
		return false
	}
	return true
}

// ListEquipment implements api.ServerInterface.
func (h *Handler) ListEquipment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListEquipmentParams) {
	kind := ""
	if params.Kind != nil {
		kind = string(*params.Kind)
	}
	items, next, err := h.Maintenance.ListEquipment(r.Context(), principal(r), marinaID, kind, params.Active, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.Equipment, 0, len(items))
	for i := range items {
		out = append(out, mntEquipment(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.EquipmentPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateEquipment implements api.ServerInterface.
func (h *Handler) CreateEquipment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateEquipmentParams) {
	var in api.EquipmentInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	e, err := h.Maintenance.CreateEquipment(r.Context(), principal(r), marinaID, maintenance.EquipmentInput{
		Code: in.Code, Name: in.Name, Kind: string(in.Kind), MapID: str(in.MapId), X: in.XCm, Y: in.YCm, CapacityKg: in.CapacityKg,
		Active: in.Active, Bookable: in.Bookable, DefaultMinutes: in.DefaultMinutes,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEquipment(w, e, http.StatusCreated)
}

// GetEquipment implements api.ServerInterface.
func (h *Handler) GetEquipment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.EquipmentId) {
	e, err := h.Maintenance.GetEquipment(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEquipment(w, e, http.StatusOK)
}

// UpdateEquipment implements api.ServerInterface.
func (h *Handler) UpdateEquipment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.EquipmentId, params api.UpdateEquipmentParams) {
	var in api.EquipmentPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := maintenance.EquipmentPatch{
		Name: in.Name, MapID: in.MapId, X: in.XCm, Y: in.YCm, CapacityKg: in.CapacityKg, Active: in.Active,
		ClearPosition: respond.IsNull(fields, "x_cm") || respond.IsNull(fields, "y_cm"), ClearCapacity: respond.IsNull(fields, "capacity_kg"),
		Bookable: in.Bookable, DefaultMinutes: in.DefaultMinutes,
	}
	if in.Code != nil {
		c := *in.Code
		patch.Code = &c
	}
	if in.Kind != nil {
		k := string(*in.Kind)
		patch.Kind = &k
	}
	if respond.IsNull(fields, "map_id") {
		empty := ""
		patch.MapID, patch.ClearPosition = &empty, true
	}
	e, err := h.Maintenance.UpdateEquipment(r.Context(), principal(r), marinaID, id, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEquipment(w, e, http.StatusOK)
}
