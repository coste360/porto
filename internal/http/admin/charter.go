package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Charter operations (m4-plan §7.3.1): fleets, charter contracts and the rotation of fleet vessels.

func writeFleet(w http.ResponseWriter, f *contract.Fleet, status int) {
	respond.SetETag(w, f.Row.Version)
	respond.JSON(w, status, view.CharterFleet(f))
}

// ListCharterFleets implements api.ServerInterface (GET .../charter-fleets).
func (h *Handler) ListCharterFleets(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListCharterFleetsParams) {
	fs, err := h.Contracts.ListCharterFleets(r.Context(), principal(r), marinaID, str(params.CustomerId))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.CharterFleetList{Items: make([]api.CharterFleet, 0, len(fs))}
	for i := range fs {
		out.Items = append(out.Items, view.CharterFleet(&fs[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateCharterFleet implements api.ServerInterface (POST .../charter-fleets).
func (h *Handler) CreateCharterFleet(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateCharterFleetParams) {
	var in api.CharterFleetInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	f, err := h.Contracts.CreateCharterFleet(r.Context(), principal(r), marinaID, contract.FleetInput{CustomerID: in.CustomerId, Name: in.Name, VesselIDs: in.VesselIds})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeFleet(w, f, http.StatusCreated)
}

// GetCharterFleet implements api.ServerInterface (GET .../charter-fleets/{fleetId}).
func (h *Handler) GetCharterFleet(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FleetId) {
	f, err := h.Contracts.GetCharterFleet(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeFleet(w, f, http.StatusOK)
}

// UpdateCharterFleet implements api.ServerInterface (PATCH .../charter-fleets/{fleetId}).
func (h *Handler) UpdateCharterFleet(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FleetId, params api.UpdateCharterFleetParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.CharterFleetPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	f, err := h.Contracts.UpdateCharterFleet(r.Context(), principal(r), marinaID, id, ifMatch, contract.FleetPatch{Name: in.Name, VesselIDs: in.VesselIds})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeFleet(w, f, http.StatusOK)
}

// CreateCharter implements api.ServerInterface (POST .../charters).
func (h *Handler) CreateCharter(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateCharterParams) {
	var in api.CharterCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ci := contract.CharterInput{
		FleetID: in.FleetId, StartsOn: d.parse(in.StartsOn), EndsOn: d.parse(in.EndsOn), PricingMode: string(in.PricingMode),
		FlatCents: in.FlatCents, FlatReason: str(in.FlatReason), ChangeoverWeekday: in.ChangeoverWeekday,
		Services: ctrServices(in.Services), Policy: ctrPolicyIn(in.Policy), Activate: boolOr(in.Activate, false),
		FitOverride: ctrFit(in.FitOverride),
	}
	for _, pl := range in.Placements {
		ci.Placements = append(ci.Placements, contract.CharterPlacementInput{
			VesselID: pl.VesselId,
			Target: contract.Segment{
				ResourceType: string(pl.Target.ResourceType), ResourceID: pl.Target.ResourceId, X: pl.Target.XCm, Y: pl.Target.YCm,
				RotationDd: pl.Target.RotationDd,
			},
		})
	}
	if in.OutNights != nil {
		for _, o := range *in.OutNights {
			ci.OutNights = append(ci.OutNights, contract.OutNight{From: d.parse(o.From), To: d.parse(o.To), BerthID: str(o.BerthId)})
		}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Contracts.CreateCharter(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusCreated)
}

// RotateCharter implements api.ServerInterface (POST .../charters/{contractId}/rotate).
func (h *Handler) RotateCharter(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.RotateCharterParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.CharterRotate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	from := d.parse(in.FromNight)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	as := make([]contract.RotationAssignment, 0, len(in.Assignments))
	for _, a := range in.Assignments {
		as = append(as, contract.RotationAssignment{LineID: a.LineId, VesselID: a.VesselId})
	}
	c, err := h.Contracts.RotateCharter(r.Context(), principal(r), marinaID, contractID, ifMatch, from, as)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}
