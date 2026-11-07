package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// Transit operations (m3-plan §7.3).

func ctrNewVessel(in *api.NewVesselInput) *contract.NewVessel {
	if in == nil {
		return nil
	}
	nv := &contract.NewVessel{Name: in.Name, LOACm: in.LoaCm, BeamCm: in.BeamCm, HullType: string(in.HullType)}
	if in.DraftCm != nil {
		nv.DraftCm = *in.DraftCm
	}
	return nv
}

// ListTransits implements api.ServerInterface (GET .../transits).
func (h *Handler) ListTransits(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListTransitsParams) {
	d := newCtrDates()
	f := contract.ListFilter{
		Status: str(enumPtr(params.Status)), ArrivalFrom: d.ptr(params.ArrivalFrom), ArrivalTo: d.ptr(params.ArrivalTo),
		ActiveOn: d.ptr(params.On), Search: str(params.Q),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Contracts.ListTransits(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ContractPage{Items: view.ContractSummaries(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateTransit implements api.ServerInterface (POST .../transits). Placements come from the chosen search
// solution's segments; solution_key with search_request is not resolved server side in M3 (the client sends the
// segments it shows).
func (h *Handler) CreateTransit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateTransitParams) {
	var in api.TransitCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ti := contract.TransitInput{
		VesselID: str(in.VesselId), NewVessel: ctrNewVessel(in.NewVessel), CustomerID: str(in.CustomerId),
		Arrival: d.parse(in.Arrival), Departure: d.parse(in.Departure), Channel: str(enumPtr(in.Channel)),
		ExpectedArrivalTime: str(in.ExpectedArrivalTime), Notes: str(in.Notes), Services: ctrServices(in.Services),
		FitOverride: ctrFit(in.FitOverride), Confirm: ctrManual(in.Confirm), InquiryID: str(in.InquiryId),
	}
	if in.Placements != nil {
		ti.Placements = ctrSegments(d, *in.Placements)
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	if in.Placements == nil && in.SolutionKey != nil {
		// Authorise first so another marina is still 404.
		if _, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); err != nil {
			respond.Error(w, r, err)
			return
		}
		v := &auth.ValidationError{}
		v.Add("placements", "required")
		respond.Error(w, r, v)
		return
	}
	c, err := h.Contracts.CreateTransit(r.Context(), principal(r), marinaID, ti)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusCreated)
}

// UpdateTransit implements api.ServerInterface (PATCH .../transits/{contractId}).
func (h *Handler) UpdateTransit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.UpdateTransitParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.TransitPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Contracts.UpdateTransit(r.Context(), principal(r), marinaID, contractID, ifMatch, contract.TransitPatch{
		CustomerID: in.CustomerId, Notes: in.Notes, ExpectedArrivalTime: in.ExpectedArrivalTime, ExpectedDepartureTime: in.ExpectedDepartureTime,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// CancelTransit implements api.ServerInterface (POST .../transits/{contractId}/cancel).
func (h *Handler) CancelTransit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.CancelTransitParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason, ok := ctrReason(r, w)
	if !ok {
		return
	}
	c, err := h.Contracts.CancelTransit(r.Context(), principal(r), marinaID, contractID, ifMatch, reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// ConfirmTransit implements api.ServerInterface (POST .../transits/{contractId}/confirm): the manual driver.
func (h *Handler) ConfirmTransit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.ConfirmTransitParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.ManualConfirmation
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	if in != nil && in.Payment != nil {
		// M5 (m5-plan §8.2): confirmation with the payment collected at the desk, one transaction.
		pay := payment.TransitPayment{
			Method: string(in.Payment.Method), CashRegisterID: str(in.Payment.RegisterId), Reference: str(in.Reference),
			IdempotencyKey: str(params.IdempotencyKey),
		}
		if in.Payment.AmountCents != nil {
			pay.Amount = money.Cents(*in.Payment.AmountCents)
		}
		if _, err := h.Payments.ConfirmTransitWithPayment(r.Context(), principal(r), marinaID, contractID, ifMatch, pay); err != nil {
			respond.Error(w, r, err)
			return
		}
		c, err := h.Contracts.GetContract(r.Context(), principal(r), marinaID, contractID)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeContract(w, c, http.StatusOK)
		return
	}
	mc := contract.ManualConfirmation{}
	if m := ctrManual(in); m != nil {
		mc = *m
	}
	c, err := h.Contracts.ConfirmTransit(r.Context(), principal(r), marinaID, contractID, ifMatch, mc)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// MarkTransitNoShow implements api.ServerInterface (POST .../transits/{contractId}/no-show).
func (h *Handler) MarkTransitNoShow(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.MarkTransitNoShowParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	c, err := h.Contracts.MarkNoShow(r.Context(), principal(r), marinaID, contractID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// ChangeTransitStay implements api.ServerInterface (POST .../transits/{contractId}/stay).
func (h *Handler) ChangeTransitStay(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.ChangeTransitStayParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.StayChange
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	sc := contract.StayChange{Arrival: d.ptr(in.Arrival), Departure: d.ptr(in.Departure), FitOverride: ctrFit(in.FitOverride)}
	if t := in.ExtensionTarget; t != nil {
		sc.ExtensionTarget = &contract.Segment{ResourceType: string(t.ResourceType), ResourceID: t.ResourceId, X: t.XCm, Y: t.YCm, RotationDd: t.RotationDd}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, eff, err := h.Contracts.ChangeStay(r.Context(), principal(r), marinaID, contractID, ifMatch, sc)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Row.Version)
	applied := eff.Applied
	respond.JSON(w, http.StatusOK, api.StayChangeResult{
		Contract: view.Contract(c),
		PriceEffect: api.PriceEffect{
			OldTotalCents: eff.OldTotalCents, NewTotalCents: eff.NewTotalCents, DeltaCents: eff.DeltaCents, Applied: &applied,
		},
	})
}
