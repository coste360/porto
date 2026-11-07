package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Mooring contract operations (m3-plan §7.3) and the input helpers shared by the quote, transit, inquiry and
// presence handlers (ctr prefix: the occupancy handlers of the same package are written in parallel).

// ctrDates parses YYYY-MM-DD values: an empty value is the zero date (the service reports it as required after
// authorising), an invalid one makes ok false.
type ctrDates struct{ ok bool }

func (d *ctrDates) parse(s string) calendar.Date {
	if s == "" {
		return calendar.Date{}
	}
	v, err := calendar.Parse(s)
	if err != nil {
		d.ok = false
		return calendar.Date{}
	}
	return v
}

func (d *ctrDates) ptr(s *string) *calendar.Date {
	if s == nil || *s == "" {
		return nil
	}
	v := d.parse(*s)
	return &v
}

func newCtrDates() *ctrDates { return &ctrDates{ok: true} }

// ctrBadBody answers a body the handler cannot map (an invalid date): another marina is still 404 first.
func (h *Handler) ctrBadBody(w http.ResponseWriter, r *http.Request, marinaID string) {
	h.badPricingBody(w, r, marinaID)
}

func ctrSegments(d *ctrDates, in []api.SegmentInput) []contract.Segment {
	out := make([]contract.Segment, 0, len(in))
	for _, s := range in {
		out = append(out, contract.Segment{
			ResourceType: string(s.ResourceType), ResourceID: s.ResourceId, X: s.XCm, Y: s.YCm, RotationDd: s.RotationDd,
			First: d.parse(s.FirstNight), Last: d.parse(s.LastNight),
		})
	}
	return out
}

func ctrServices(in *[]api.SimulateService) []contract.ServiceRequest {
	if in == nil {
		return nil
	}
	out := make([]contract.ServiceRequest, 0, len(*in))
	for _, s := range *in {
		q := int64(1000)
		if s.QtyMilli != nil {
			q = *s.QtyMilli
		}
		out = append(out, contract.ServiceRequest{ItemID: s.ItemId, QtyMilli: q})
	}
	return out
}

func ctrFit(in *api.FitOverrideInput) *contract.FitOverrideInput {
	if in == nil {
		return nil
	}
	return &contract.FitOverrideInput{Reason: in.Reason}
}

func ctrPolicyIn(in *api.ContractPolicy) *contract.PolicyInput {
	if in == nil {
		return nil
	}
	out := &contract.PolicyInput{RepriceOnMove: in.RepriceOnMove, AllowSegments: in.AllowSegments}
	if in.Version != nil {
		v := int(*in.Version)
		out.Version = &v
	}
	if in.ReleasePolicy != nil {
		rp := string(*in.ReleasePolicy)
		out.ReleasePolicy = &rp
	}
	return out
}

func ctrManual(in *api.ManualConfirmation) *contract.ManualConfirmation {
	if in == nil {
		return nil
	}
	mc := &contract.ManualConfirmation{Reference: str(in.Reference)}
	if in.PayAtDeparture != nil {
		mc.PayAtDeparture = *in.PayAtDeparture
	}
	return mc
}

func ctrReason(r *http.Request, w http.ResponseWriter) (string, bool) {
	var in *api.ReasonInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return "", false
	}
	if in == nil {
		return "", true
	}
	return str(in.Reason), true
}

// writeContract answers with a contract or transit and its ETag.
func writeContract(w http.ResponseWriter, d *contract.Detail, status int) {
	respond.SetETag(w, d.Row.Version)
	respond.JSON(w, status, view.Contract(d))
}

// ListContracts implements api.ServerInterface (GET .../contracts).
func (h *Handler) ListContracts(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListContractsParams) {
	d := newCtrDates()
	f := contract.ListFilter{
		Kind: str(enumPtr(params.Kind)), Status: str(enumPtr(params.Status)), Term: str(enumPtr(params.Term)),
		CustomerID: str(params.CustomerId), VesselID: str(params.VesselId), ActiveOn: d.ptr(params.ActiveOn), Search: str(params.Q),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	page, err := h.Contracts.ListContracts(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ContractPage{Items: view.ContractSummaries(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateContract implements api.ServerInterface (POST .../contracts).
func (h *Handler) CreateContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateContractParams) {
	var in api.ContractCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ci := contract.MooringInput{
		Term: string(in.Term), CustomerID: in.CustomerId, BillingCustomerID: str(in.BillingCustomerId), VesselID: in.VesselId,
		StartsOn: d.parse(in.StartsOn), EndsOn: d.ptr(in.EndsOn), Placements: ctrSegments(d, in.Placements),
		Services: ctrServices(in.Services), Policy: ctrPolicyIn(in.Policy), Notes: str(in.Notes),
		FitOverride: ctrFit(in.FitOverride),
	}
	if in.Activate != nil {
		ci.Activate = *in.Activate
	}
	if in.ExtraResources != nil {
		for _, x := range *in.ExtraResources {
			ci.Extras = append(ci.Extras, contract.ExtraResource{LandResourceID: x.LandResourceId, ItemID: str(x.ItemId)})
		}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Contracts.CreateContract(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusCreated)
}

// GetContract implements api.ServerInterface (GET .../contracts/{contractId}); it also serves transits.
func (h *Handler) GetContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	c, err := h.Contracts.GetContract(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// UpdateContract implements api.ServerInterface (PATCH .../contracts/{contractId}).
func (h *Handler) UpdateContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.UpdateContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ContractPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Contracts.UpdateContract(r.Context(), principal(r), marinaID, contractID, ifMatch, contract.MooringPatch{
		Notes: in.Notes, BillingCustomerID: in.BillingCustomerId, Policy: ctrPolicyIn(in.Policy),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// ActivateContract implements api.ServerInterface (POST .../contracts/{contractId}/activate).
func (h *Handler) ActivateContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.ActivateContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	c, err := h.Contracts.ActivateContract(r.Context(), principal(r), marinaID, contractID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// CancelContract implements api.ServerInterface (POST .../contracts/{contractId}/cancel).
func (h *Handler) CancelContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.CancelContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason, ok := ctrReason(r, w)
	if !ok {
		return
	}
	c, err := h.Contracts.CancelContract(r.Context(), principal(r), marinaID, contractID, ifMatch, reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}

// TerminateContract implements api.ServerInterface (POST .../contracts/{contractId}/terminate).
func (h *Handler) TerminateContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.TerminateContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.TerminateInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	eff := d.parse(in.EffectiveOn)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Contracts.TerminateContract(r.Context(), principal(r), marinaID, contractID, ifMatch, eff, in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeContract(w, c, http.StatusOK)
}
