package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/lifting"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Lifting equipment bookings and operators (M10, m10-plan §11, §13). Owned by PLANNER.

func apiBooking(b *lifting.Booking) api.EquipmentBooking {
	return api.EquipmentBooking{
		Id: b.ID, EquipmentId: view.String(b.EquipmentID), Date: optIsoDate(b.Date), FromSlot: b.FromSlot, ToSlot: b.ToSlot,
		Purpose: api.EquipmentBookingPurpose(b.Purpose), VesselId: view.String(b.VesselID), VesselName: view.String(b.VesselName),
		WorkOrderId: view.String(b.WorkOrderID), ContractId: view.String(b.ContractID), StorageMovementId: view.String(b.StorageMovementID),
		AllocationId: view.String(b.AllocationID), OperatorEmployeeId: view.String(b.OperatorEmployeeID),
		Status: api.EquipmentBookingStatus(b.Status), Source: api.EquipmentBookingSource(b.Source), Note: b.Note,
		Version: int64(b.Version), CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC(),
	}
}

func apiBookings(in []lifting.Booking) []api.EquipmentBooking {
	out := make([]api.EquipmentBooking, 0, len(in))
	for i := range in {
		out = append(out, apiBooking(&in[i]))
	}
	return out
}

func writeBooking(w http.ResponseWriter, b *lifting.Booking, status int) {
	respond.SetETag(w, b.Version)
	respond.JSON(w, status, apiBooking(b))
}

// ListEquipmentBookings implements api.ServerInterface.
func (h *Handler) ListEquipmentBookings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListEquipmentBookingsParams) {
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"date": params.Date})
	if !ok {
		return
	}
	f := lifting.DayFilter{Date: d["date"], EquipmentID: str(params.EquipmentId), WorkOrderID: str(params.WorkOrderId)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	day, err := h.Lifting.ListDay(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.EquipmentBookingDay{
		Date: day.Date.String(), WindowFromSlot: day.Window.From, WindowToSlot: day.Window.To,
		Bookings: apiBookings(day.Bookings), Requests: apiBookings(day.Requests),
	}
	out.Equipment = planEmpty(out.Equipment)
	for _, e := range day.Equipment {
		items, x := planAppend(out.Equipment)
		out.Equipment = items
		x.Id, x.Code, x.Name, x.Kind = e.Row.ID, e.Row.Code, e.Row.Name, api.EquipmentKind(e.Row.Kind)
		x.CapacityKg, x.OutOfService, x.DefaultMinutes = e.Row.CapacityKg, e.OutOfService, e.Row.DefaultMinutes
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateEquipmentBooking implements api.ServerInterface.
func (h *Handler) CreateEquipmentBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateEquipmentBookingParams) {
	var in api.EquipmentBookingInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"date": in.Date})
	if !ok {
		return
	}
	b, err := h.Lifting.CreateBooking(r.Context(), principal(r), marinaID, lifting.BookingInput{
		EquipmentID: str(in.EquipmentId), Date: d["date"], FromSlot: in.FromSlot, ToSlot: in.ToSlot, Purpose: string(in.Purpose),
		VesselID: str(in.VesselId), WorkOrderID: str(in.WorkOrderId), ContractID: str(in.ContractId),
		StorageMovementID: str(in.StorageMovementId), OperatorEmployeeID: str(in.OperatorEmployeeId),
		RequestOnly: in.RequestOnly != nil && *in.RequestOnly, Note: str(in.Note),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeBooking(w, b, http.StatusCreated)
}

// UpdateEquipmentBooking implements api.ServerInterface.
func (h *Handler) UpdateEquipmentBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, equipmentBookingID api.EquipmentBookingId, params api.UpdateEquipmentBookingParams) {
	var in api.EquipmentBookingPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	date := parseDate(v, "date", in.Date)
	if err := v.Err(); err != nil {
		h.planBad(w, r, marinaID, err)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	b, err := h.Lifting.UpdateBooking(r.Context(), principal(r), marinaID, equipmentBookingID, int64(version), lifting.BookingPatch{
		EquipmentID: in.EquipmentId, Date: date, FromSlot: in.FromSlot, ToSlot: in.ToSlot, OperatorEmployeeID: in.OperatorEmployeeId,
		ClearOperator: respond.IsNull(fields, "operator_employee_id"), Note: in.Note,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeBooking(w, b, http.StatusOK)
}

// CancelEquipmentBooking implements api.ServerInterface.
func (h *Handler) CancelEquipmentBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, equipmentBookingID api.EquipmentBookingId, params api.CancelEquipmentBookingParams) {
	var in *api.EquipmentBookingCancelInput
	if err := respond.Decode(r, &in, true); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason := ""
	if in != nil {
		reason = str(in.Reason)
	}
	b, err := h.Lifting.CancelBooking(r.Context(), principal(r), marinaID, equipmentBookingID, int64(version), reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeBooking(w, b, http.StatusOK)
}

// CompleteEquipmentBooking implements api.ServerInterface.
func (h *Handler) CompleteEquipmentBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, equipmentBookingID api.EquipmentBookingId, params api.CompleteEquipmentBookingParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	b, err := h.Lifting.CompleteBooking(r.Context(), principal(r), marinaID, equipmentBookingID, int64(version))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeBooking(w, b, http.StatusOK)
}

// ListEquipmentOperators implements api.ServerInterface.
func (h *Handler) ListEquipmentOperators(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, equipmentID api.EquipmentId) {
	ids, err := h.Lifting.ListOperators(r.Context(), principal(r), marinaID, equipmentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.EquipmentOperators{EquipmentId: equipmentID, EmployeeIds: ids})
}

// PutEquipmentOperators implements api.ServerInterface.
func (h *Handler) PutEquipmentOperators(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, equipmentID api.EquipmentId) {
	var in api.EquipmentOperatorsInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	ids, err := h.Lifting.PutOperators(r.Context(), principal(r), marinaID, equipmentID, in.EmployeeIds)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.EquipmentOperators{EquipmentId: equipmentID, EmployeeIds: ids})
}
