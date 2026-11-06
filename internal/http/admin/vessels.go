package admin

import (
	"encoding/json"
	"net/http"

	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Vessel operations (m1-plan §4.4).

func enumPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func intPtr64(v *int) *int64 {
	if v == nil {
		return nil
	}
	n := int64(*v)
	return &n
}

func vesselPatch(in api.VesselPatch, fields map[string]json.RawMessage) vessel.Patch {
	p := vessel.Patch{
		Name: in.Name, Registration: nullable(fields, "registration", in.Registration),
		RegistryOffice: nullable(fields, "registry_office", in.RegistryOffice), Flag: nullable(fields, "flag", in.Flag),
		HomePort: nullable(fields, "home_port", in.HomePort), HullType: enumPtr(in.HullType),
		Model: nullable(fields, "model", in.Model), Builder: nullable(fields, "builder", in.Builder),
		HullColor: nullable(fields, "hull_color", in.HullColor), HullMaterial: nullable(fields, "hull_material", in.HullMaterial),
		YearBuilt: nullableInt(fields, "year_built", intPtr64(in.YearBuilt)), LOACm: in.LoaCm, BeamCm: in.BeamCm,
		LWLCm: nullableInt(fields, "lwl_cm", in.LwlCm), DraftCm: nullableInt(fields, "draft_cm", in.DraftCm),
		AirDraftCm:       nullableInt(fields, "air_draft_cm", in.AirDraftCm),
		DisplacementKg:   nullableInt(fields, "displacement_kg", in.DisplacementKg),
		DimensionsSource: enumPtr(in.DimensionsSource), Engines: view.EnginesIn(in.Engines),
		Fuel: nullable(fields, "fuel", enumPtr(in.Fuel)), PowerRequiredA: nullableInt(fields, "power_required_a", intPtr64(in.PowerRequiredA)),
		Notes: in.Notes,
	}
	return p
}

func partyInputs(in []api.VesselPartyInput) []vessel.PartyInput {
	out := make([]vessel.PartyInput, 0, len(in))
	for _, p := range in {
		out = append(out, vessel.PartyInput{CustomerID: p.CustomerId, Role: string(p.Role), ShareMillis: p.ShareMillis})
	}
	return out
}

// writeVessel answers with a vessel and its documents.
func (h *Handler) writeVessel(w http.ResponseWriter, r *http.Request, marinaID string, v *vessel.Vessel, status int) {
	docs, err := h.Documents.OfVessel(r.Context(), principal(r), marinaID, v.Row.ID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, view.Vessel(v, docs))
}

// ListVessels implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/vessels).
func (h *Handler) ListVessels(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListVesselsParams) {
	f := vessel.Filter{Search: str(params.Q), CustomerID: str(params.CustomerId)}
	if params.HullType != nil {
		f.HullType = string(*params.HullType)
	}
	if params.Archived != nil {
		f.Archived = *params.Archived
	}
	page, err := h.Vessels.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.VesselPage{Items: view.VesselSummaries(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateVessel implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/vessels).
func (h *Handler) CreateVessel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateVesselParams) {
	var in api.VesselInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	loa, beam := in.LoaCm, in.BeamCm
	var power *int64
	if in.PowerRequiredA != nil {
		n := int64(*in.PowerRequiredA)
		power = &n
	}
	patch := vessel.Patch{
		Name: &in.Name, Registration: in.Registration, RegistryOffice: in.RegistryOffice, Flag: in.Flag,
		HomePort: in.HomePort, HullType: enumPtr(in.HullType), Model: in.Model, Builder: in.Builder,
		HullColor: in.HullColor, HullMaterial: in.HullMaterial, YearBuilt: intPtr64(in.YearBuilt), LOACm: &loa,
		BeamCm: &beam, LWLCm: in.LwlCm, DraftCm: in.DraftCm, AirDraftCm: in.AirDraftCm, DisplacementKg: in.DisplacementKg,
		Engines: view.EnginesIn(in.Engines), Fuel: enumPtr(in.Fuel), PowerRequiredA: power, Notes: in.Notes,
	}
	var parties []vessel.PartyInput
	if in.Parties != nil {
		parties = partyInputs(*in.Parties)
	}
	v, err := h.Vessels.Create(r.Context(), principal(r), marinaID, patch, parties)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeVessel(w, r, marinaID, v, http.StatusCreated)
}

// GetVessel implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}).
func (h *Handler) GetVessel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId) {
	v, err := h.Vessels.Get(r.Context(), principal(r), marinaID, vesselID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeVessel(w, r, marinaID, v, http.StatusOK)
}

// UpdateVessel implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}).
func (h *Handler) UpdateVessel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId, params api.UpdateVesselParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.VesselPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	v, err := h.Vessels.Update(r.Context(), principal(r), marinaID, vesselID, version, vesselPatch(in, fields))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeVessel(w, r, marinaID, v, http.StatusOK)
}

// ReplaceVesselParties implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/parties).
func (h *Handler) ReplaceVesselParties(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId, params api.ReplaceVesselPartiesParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.VesselPartiesReplace
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v, err := h.Vessels.ReplaceParties(r.Context(), principal(r), marinaID, vesselID, version, partyInputs(in.Parties))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeVessel(w, r, marinaID, v, http.StatusOK)
}

// RecordVesselMeasurement implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/measurements).
func (h *Handler) RecordVesselMeasurement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId) {
	var in api.VesselMeasurementInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	m := vessel.Measurement{LOACm: in.LoaCm, BeamCm: in.BeamCm}
	if in.DraftCm != nil {
		m.DraftCm = *in.DraftCm
	}
	if in.AirDraftCm != nil {
		m.AirDraftCm = *in.AirDraftCm
	}
	v, err := h.Vessels.RecordMeasurement(r.Context(), principal(r), marinaID, vesselID, m)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeVessel(w, r, marinaID, v, http.StatusOK)
}

// ArchiveVessel implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/archive).
func (h *Handler) ArchiveVessel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId) {
	if err := h.Vessels.Archive(r.Context(), principal(r), marinaID, vesselID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// UnarchiveVessel implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/unarchive).
func (h *Handler) UnarchiveVessel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, vesselID api.VesselId) {
	if err := h.Vessels.Unarchive(r.Context(), principal(r), marinaID, vesselID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
