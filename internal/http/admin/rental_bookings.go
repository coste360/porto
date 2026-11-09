package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/qty"
)

// Rental booking operations (m11-plan §13.1): planner, availability, bookings, transitions, blocks and the day
// view. Owned by BOOKING. apiRentalBooking and writeRentalBooking are shared with rental_handover.go.

func apiSlotPoint(p slots.Point) api.RentalSlotPoint {
	return api.RentalSlotPoint{Date: p.Date.String(), Slot: p.Slot}
}

// slotPointIn parses a slot point of a request body ("" field names the point for the 400).
func slotPointIn(v *auth.ValidationError, field string, p *api.RentalSlotPoint) *slots.Point {
	if p == nil {
		return nil
	}
	d, err := calendar.Parse(p.Date)
	if err != nil {
		v.Add(field+".date", "invalid")
		return nil
	}
	if p.Slot < 0 || p.Slot > slots.PerDay {
		v.Add(field+".slot", "invalid")
		return nil
	}
	return &slots.Point{Date: d, Slot: p.Slot}
}

func rangeIn(v *auth.ValidationError, start, end *api.RentalSlotPoint) *slots.Range {
	s, e := slotPointIn(v, "start", start), slotPointIn(v, "end", end)
	if s == nil || e == nil {
		return nil
	}
	return &slots.Range{Start: *s, End: *e}
}

func extrasIn(in *[]api.RentalExtraQty) []fleet.ExtraQty {
	if in == nil {
		return nil
	}
	out := make([]fleet.ExtraQty, 0, len(*in))
	for _, x := range *in {
		out = append(out, fleet.ExtraQty{ExtraID: x.ExtraId, Qty: qty.Milli(x.QtyMilli)})
	}
	return out
}

func renterIn(in *api.RentalRenter) *reservation.Renter {
	if in == nil {
		return nil
	}
	return &reservation.Renter{Name: str(in.Name), Phone: str(in.Phone), Email: str(in.Email)}
}

func optCents(v *int64) *api.Cents {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// apiRentalBooking converts a booking view (shared with the handover handlers).
func apiRentalBooking(v *reservation.BookingView) api.RentalBooking {
	b := v.RentalBooking
	out := api.RentalBooking{
		Id: b.ID, Number: reservation.Number(b.Year, b.Number), Status: api.RentalBookingStatus(b.Status),
		Channel: api.RentalChannel(b.Channel), UnitId: b.UnitID, UnitCode: v.UnitCode, FormulaId: b.FormulaID,
		FormulaCode: v.FormulaCode, Kind: api.RentalFormulaKind(v.Kind), SkipperId: view.String(b.SkipperID),
		Start: apiSlotPoint(slots.Point{Date: b.StartsOn, Slot: b.StartSlot}), End: apiSlotPoint(slots.Point{Date: b.EndsOn, Slot: b.EndSlot}),
		StartsAt: v.StartsAt.UTC(), EndsAt: v.EndsAt.UTC(), HoldExpiresAt: view.Time(b.HoldExpiresAt),
		SlotsLost: !b.SlotsLostAt.IsZero(), CustomerId: view.String(b.CustomerID), PartySize: b.PartySize,
		InvoiceRequested: b.InvoiceRequested, FiscalMode: api.RentalFiscalMode(v.FiscalMode), AllowOutsideHours: b.AllowOutsideHours,
		TotalCents: b.TotalCents, EngineVersion: b.EngineVersion, LicenceRequired: v.LicenceRequired, Note: b.Note,
		CheckedOutAt: view.Time(b.CheckedOutAt), ReturnedAt: view.Time(b.ReturnedAt), ClosedAt: view.Time(b.ClosedAt),
		CancelledAt: view.Time(b.CancelledAt), CancelReason: view.String(b.CancelReason), NoShowAt: view.Time(b.NoShowAt),
		Version: int64(b.Version), CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC(),
		Deposit: api.RentalDeposit{
			AmountCents: b.DepositCents, Status: api.RentalDepositStatus(b.DepositStatus), RetainedCents: b.DepositRetainedCents,
			DecisionCents: optCents(b.DepositDecisionCents), IntentId: view.String(b.DepositIntentID),
			PaymentId: view.String(b.DepositPaymentID), WaiveReason: view.String(b.DepositWaiveReason),
			CheckoutUrl: view.String(v.CheckoutURL), ExpiresAt: view.Time(v.DepositExpiresAt),
		},
		Lines: []api.RentalPriceLine{}, Extras: []api.RentalBookingExtra{}, Charges: []api.RentalBookingChargeRef{},
	}
	if b.DepositMode != "" {
		m := api.RentalDepositMode(b.DepositMode)
		out.Deposit.Mode = &m
	}
	if v.Renter != nil {
		out.Renter = &api.RentalRenter{Name: view.String(v.Renter.Name), Phone: view.String(v.Renter.Phone), Email: view.String(v.Renter.Email)}
	}
	for _, l := range v.Snapshot.Breakdown.Lines {
		line := api.RentalPriceLine{
			Seq: l.Seq, Kind: api.RentalPriceLineKind(l.Kind), ItemId: l.ItemID, ItemCode: l.ItemCode, QtyMilli: int64(l.Qty),
			UnitCents: int64(l.UnitCents), AmountCents: int64(l.Amount), PriceIncludesVat: l.PriceIncludesVAT,
		}
		if len(l.RuleIDs) > 0 {
			ids := append([]string(nil), l.RuleIDs...)
			line.RuleIds = &ids
		}
		out.Lines = append(out.Lines, line)
	}
	for _, x := range v.Extras {
		out.Extras = append(out.Extras, api.RentalBookingExtra{
			ExtraId: x.ExtraID, ItemId: v.ExtraItems[x.ExtraID], QtyMilli: x.QtyMilli, Source: api.RentalBookingExtraSource(x.Source),
		})
	}
	for _, c := range v.Charges {
		out.Charges = append(out.Charges, api.RentalBookingChargeRef{
			ChargeId: c.ChargeID, Kind: api.RentalBookingChargeRefKind(c.Kind), Status: c.Status, TotalCents: int64(c.Total),
			PaidAmountCents: int64(c.Paid), FiscalMode: api.RentalFiscalMode(c.FiscalMode),
		})
	}
	return out
}

// writeRentalBooking writes a booking with its ETag (shared with the handover handlers).
func writeRentalBooking(w http.ResponseWriter, v *reservation.BookingView, status int) {
	respond.SetETag(w, v.Version)
	respond.JSON(w, status, apiRentalBooking(v))
}

// rentalBad answers a malformed request after the access check, so another marina is 404 and a missing
// permission 403 before any 400.
func (h *Handler) rentalBad(w http.ResponseWriter, r *http.Request, marinaID string, err error) {
	if aerr := h.RentalBookings.CheckAccess(r.Context(), principal(r), marinaID); aerr != nil {
		respond.Error(w, r, aerr)
		return
	}
	respond.Error(w, r, err)
}

func apiRentalBlock(b *reservation.Block) api.RentalBlock {
	return api.RentalBlock{
		Id: b.ID, UnitId: view.String(b.UnitID), SkipperId: view.String(b.SkipperID),
		From: apiSlotPoint(slots.Point{Date: b.FromOn, Slot: b.FromSlot}), To: apiSlotPoint(slots.Point{Date: b.ToOn, Slot: b.ToSlot}),
		Reason: api.RentalBlockReason(b.Reason), Note: b.Note, Version: int64(b.Version), CreatedAt: b.CreatedAt.UTC(),
		UpdatedAt: b.UpdatedAt.UTC(),
	}
}

func dateIn(v *auth.ValidationError, field string, s *string) calendar.Date {
	if d := parseDate(v, field, s); d != nil {
		return *d
	}
	return calendar.Date{}
}

// GetRentalPlanner implements api.ServerInterface (GET .../rental/planner).
func (h *Handler) GetRentalPlanner(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetRentalPlannerParams) {
	v := &auth.ValidationError{}
	q := reservation.PlannerQuery{From: dateIn(v, "from", params.From), To: dateIn(v, "to", params.To)}
	if params.View != nil {
		q.View = string(*params.View)
	}
	if params.Category != nil {
		q.Category = string(*params.Category)
	}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	p, err := h.RentalBookings.Planner(r.Context(), principal(r), marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RentalPlanner{
		From: p.From.String(), To: p.To.String(), View: api.RentalPlannerView(p.View), OpenFromSlot: p.Settings.Row.OpenFromSlot,
		OpenToSlot: p.Settings.Row.OpenToSlot, BufferSlots: p.Settings.Row.BufferSlots, Bands: []api.RentalBand{},
		Rows: []api.RentalPlannerRow{},
	}
	for _, b := range p.Settings.Bands {
		out.Bands = append(out.Bands, api.RentalBand{Code: b.Code, FromSlot: b.FromSlot, ToSlot: b.ToSlot, Label: i18nOf(b.Label)})
	}
	for _, row := range p.Rows {
		ar := api.RentalPlannerRow{Id: row.ID, Kind: api.RentalPlannerRowKind(row.Kind), Code: row.Code, Segments: []api.RentalPlannerSegment{}}
		if row.Kind == "unit" {
			c, st, seats := api.RentalCategory(row.Category), api.RentalUnitStatus(row.Status), row.Seats
			ar.Category, ar.Status, ar.Seats = &c, &st, &seats
		}
		for _, sg := range row.Segments {
			seg := api.RentalPlannerSegment{
				BookingId: view.String(sg.BookingID), BlockId: view.String(sg.BlockID), Kind: api.RentalSlotKind(sg.Kind),
				Number: view.String(sg.Number), From: apiSlotPoint(sg.From), To: apiSlotPoint(sg.To),
			}
			if sg.Status != "" {
				st := api.RentalBookingStatus(sg.Status)
				seg.Status = &st
			}
			if sg.Reason != "" {
				rs := api.RentalBlockReason(sg.Reason)
				seg.Reason = &rs
			}
			ar.Segments = append(ar.Segments, seg)
		}
		out.Rows = append(out.Rows, ar)
	}
	respond.JSON(w, http.StatusOK, out)
}

func i18nOf(m map[string]string) api.I18nText {
	out := api.I18nText{It: m["it"]}
	if en, ok := m["en"]; ok && en != "" {
		out.En = &en
	}
	return out
}

// SearchRentalAvailability implements api.ServerInterface (GET .../rental/availability).
func (h *Handler) SearchRentalAvailability(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.SearchRentalAvailabilityParams) {
	v := &auth.ValidationError{}
	q := reservation.AvailabilityQuery{FormulaID: str(params.FormulaId), Skippers: params.Skipper != nil && *params.Skipper}
	if params.Category != nil {
		q.Category = string(*params.Category)
	}
	d0, d1 := dateIn(v, "starts_on", params.StartsOn), dateIn(v, "ends_on", params.EndsOn)
	for name, val := range map[string]any{"starts_on": params.StartsOn, "ends_on": params.EndsOn, "start_slot": params.StartSlot, "end_slot": params.EndSlot} {
		switch x := val.(type) {
		case *string:
			if x == nil {
				v.Add(name, "required")
			}
		case *int:
			if x == nil {
				v.Add(name, "required")
			}
		}
	}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	q.Range = slots.Range{Start: slots.Point{Date: d0, Slot: *params.StartSlot}, End: slots.Point{Date: d1, Slot: *params.EndSlot}}
	av, err := h.RentalBookings.Availability(r.Context(), principal(r), marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var out api.RentalAvailability
	out.Units, out.Skippers = planEmpty(out.Units), planEmpty(out.Skippers)
	for _, u := range av.Units {
		items, x := planAppend(out.Units)
		out.Units = items
		x.UnitId, x.Code, x.Category, x.Seats = u.UnitID, u.Code, api.RentalCategory(u.Category), u.Seats
		x.FormulaIds, x.LicenceRequired = u.FormulaIDs, u.LicenceRequired
	}
	for _, sk := range av.Skippers {
		items, x := planAppend(out.Skippers)
		out.Skippers = items
		x.SkipperId, x.Name, x.WithinHours = sk.SkipperID, sk.Name, sk.WithinHours
	}
	respond.JSON(w, http.StatusOK, out)
}

// ListRentalBookings implements api.ServerInterface (GET .../rental/bookings).
func (h *Handler) ListRentalBookings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalBookingsParams) {
	v := &auth.ValidationError{}
	f := reservation.ListFilter{
		From: dateIn(v, "from", params.From), To: dateIn(v, "to", params.To), UnitID: str(params.UnitId),
		CustomerID: str(params.CustomerId), Cursor: str(params.Cursor),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Limit != nil {
		f.Limit = *params.Limit
	}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	items, next, err := h.RentalBookings.List(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RentalBookingList{Items: make([]api.RentalBooking, 0, len(items)), NextCursor: view.String(next)}
	for i := range items {
		out.Items = append(out.Items, apiRentalBooking(&items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateRentalBooking implements api.ServerInterface (POST .../rental/bookings).
func (h *Handler) CreateRentalBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalBookingParams) {
	var in api.RentalBookingInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.rentalBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	rg := rangeIn(v, &in.Start, &in.End)
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	b, err := h.RentalBookings.Create(r.Context(), principal(r), marinaID, reservation.CreateInput{
		UnitID: in.UnitId, FormulaID: in.FormulaId, SkipperID: str(in.SkipperId), Range: *rg, Hold: in.Hold != nil && *in.Hold,
		CustomerID: str(in.CustomerId), Renter: renterIn(in.Renter), PartySize: in.PartySize, Extras: extrasIn(in.Extras),
		InvoiceRequested: in.InvoiceRequested != nil && *in.InvoiceRequested, AllowOutsideHours: in.AllowOutsideHours != nil && *in.AllowOutsideHours,
		Note: str(in.Note),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBooking(w, b, http.StatusCreated)
}

// GetRentalBooking implements api.ServerInterface (GET .../rental/bookings/{rentalBookingId}).
func (h *Handler) GetRentalBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId) {
	b, err := h.RentalBookings.Get(r.Context(), principal(r), marinaID, bookingID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBooking(w, b, http.StatusOK)
}

// UpdateRentalBooking implements api.ServerInterface (PATCH .../rental/bookings/{rentalBookingId}).
func (h *Handler) UpdateRentalBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.UpdateRentalBookingParams) {
	var in api.RentalBookingPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.rentalBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	var rg *slots.Range
	if in.Start != nil || in.End != nil {
		if in.Start == nil || in.End == nil {
			v.Add("end", "required_with_start")
		} else {
			rg = rangeIn(v, in.Start, in.End)
		}
	}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p := reservation.Patch{
		UnitID: in.UnitId, FormulaID: in.FormulaId, Range: rg, InvoiceRequested: in.InvoiceRequested,
		AllowOutsideHours: in.AllowOutsideHours, KeepPrice: in.KeepPrice != nil && *in.KeepPrice, Note: in.Note,
		Renter: renterIn(in.Renter), PartySize: in.PartySize,
		ClearSkipper: respond.IsNull(fields, "skipper_id"), ClearCustomer: respond.IsNull(fields, "customer_id"),
		ClearPartySize: respond.IsNull(fields, "party_size"),
	}
	if in.SkipperId != nil && *in.SkipperId != "" {
		p.SkipperID = in.SkipperId
	} else if in.SkipperId != nil {
		p.ClearSkipper = true
	}
	if in.CustomerId != nil && *in.CustomerId != "" {
		p.CustomerID = in.CustomerId
	} else if in.CustomerId != nil {
		p.ClearCustomer = true
	}
	if in.Extras != nil {
		p.Extras, p.ExtrasSet = extrasIn(in.Extras), true
	}
	b, err := h.RentalBookings.Update(r.Context(), principal(r), marinaID, bookingID, version, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBooking(w, b, http.StatusOK)
}

// TransitionRentalBooking implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/transition).
func (h *Handler) TransitionRentalBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.TransitionRentalBookingParams) {
	var in api.RentalTransitionInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.rentalBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	end := slotPointIn(v, "end", in.End)
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	t := reservation.TransitionInput{Action: string(in.Action), Reason: str(in.Reason), End: end, FormulaID: str(in.FormulaId)}
	if in.Until != nil {
		t.Until = in.Until.UTC()
	}
	b, err := h.RentalBookings.Transition(r.Context(), principal(r), marinaID, bookingID, version, t)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBooking(w, b, http.StatusOK)
}

// ListRentalBlocks implements api.ServerInterface (GET .../rental/blocks).
func (h *Handler) ListRentalBlocks(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalBlocksParams) {
	v := &auth.ValidationError{}
	f := reservation.BlockFilter{From: dateIn(v, "from", params.From), To: dateIn(v, "to", params.To), UnitID: str(params.UnitId), SkipperID: str(params.SkipperId)}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	items, err := h.RentalBookings.ListBlocks(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RentalBlockList{Items: make([]api.RentalBlock, 0, len(items))}
	for i := range items {
		out.Items = append(out.Items, apiRentalBlock(&items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

func writeRentalBlock(w http.ResponseWriter, b *reservation.Block, status int) {
	respond.SetETag(w, b.Version)
	respond.JSON(w, status, apiRentalBlock(b))
}

// CreateRentalBlock implements api.ServerInterface (POST .../rental/blocks).
func (h *Handler) CreateRentalBlock(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalBlockParams) {
	var in api.RentalBlockInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.rentalBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	from, to := slotPointIn(v, "from", &in.From), slotPointIn(v, "to", &in.To)
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	b, err := h.RentalBookings.CreateBlock(r.Context(), principal(r), marinaID, reservation.BlockInput{
		UnitID: str(in.UnitId), SkipperID: str(in.SkipperId), Range: slots.Range{Start: *from, End: *to}, Reason: string(in.Reason),
		Note: str(in.Note),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBlock(w, b, http.StatusCreated)
}

// UpdateRentalBlock implements api.ServerInterface (PATCH .../rental/blocks/{rentalBlockId}).
func (h *Handler) UpdateRentalBlock(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, blockID api.RentalBlockId, params api.UpdateRentalBlockParams) {
	var in api.RentalBlockPatch
	if err := respond.Decode(r, &in, false); err != nil {
		h.rentalBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	var rg *slots.Range
	if in.From != nil || in.To != nil {
		if in.From == nil || in.To == nil {
			v.Add("to", "required_with_from")
		} else if from, to := slotPointIn(v, "from", in.From), slotPointIn(v, "to", in.To); from != nil && to != nil {
			rg = &slots.Range{Start: *from, End: *to}
		}
	}
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p := reservation.BlockPatch{Range: rg, Note: in.Note}
	if in.Reason != nil {
		reason := string(*in.Reason)
		p.Reason = &reason
	}
	b, err := h.RentalBookings.UpdateBlock(r.Context(), principal(r), marinaID, blockID, version, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeRentalBlock(w, b, http.StatusOK)
}

// DeleteRentalBlock implements api.ServerInterface (DELETE .../rental/blocks/{rentalBlockId}).
func (h *Handler) DeleteRentalBlock(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, blockID api.RentalBlockId, params api.DeleteRentalBlockParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.RentalBookings.DeleteBlock(r.Context(), principal(r), marinaID, blockID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiDayEntries(in []reservation.DayEntry) []api.RentalDayEntry {
	out := make([]api.RentalDayEntry, 0, len(in))
	for _, e := range in {
		x := api.RentalDayEntry{
			BookingId: e.BookingID, Number: e.Number, Status: api.RentalBookingStatus(e.Status), UnitId: e.UnitID, UnitCode: e.UnitCode,
			SkipperId: view.String(e.SkipperID), RenterName: view.String(e.RenterName), Start: apiSlotPoint(e.Range.Start),
			End: apiSlotPoint(e.Range.End), StartsAt: e.StartsAt.UTC(), EndsAt: e.EndsAt.UTC(), Late: e.Late,
			DepositStatus: api.RentalDepositStatus(e.DepositStatus),
		}
		if e.Late {
			m := e.LateMinutes
			x.LateMinutes = &m
		}
		if e.Unpaid > 0 {
			c := int64(e.Unpaid)
			x.UnpaidCents = &c
		}
		out = append(out, x)
	}
	return out
}

// GetRentalDay implements api.ServerInterface (GET .../rental/day).
func (h *Handler) GetRentalDay(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetRentalDayParams) {
	v := &auth.ValidationError{}
	date := dateIn(v, "date", params.Date)
	if err := v.Err(); err != nil {
		h.rentalBad(w, r, marinaID, err)
		return
	}
	day, err := h.RentalBookings.Day(r.Context(), principal(r), marinaID, reservation.DayQuery{Date: date})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RentalDay{
		Date: day.Date.String(), Pickups: apiDayEntries(day.Pickups), Out: apiDayEntries(day.Out),
		ReturnsDue: apiDayEntries(day.ReturnsDue), Late: apiDayEntries(day.Late),
	})
}
