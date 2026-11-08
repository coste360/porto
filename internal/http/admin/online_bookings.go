package admin

import (
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Desk operations on online bookings and document verification (m8-plan §6, §12.3) over booking.Service and
// document.Service. The decrypted contact is returned only when the service included it (customers.read).

func onlineSummaryView(s *booking.Summary) api.OnlineBookingSummary {
	out := api.OnlineBookingSummary{
		ContractId: s.ContractID, Reference: s.Reference, Code: s.Code, Status: api.OnlineBookingStatus(s.Status),
		Channel: api.OnlineChannel(s.Channel), ChannelCode: view.String(s.ChannelCode), Arrival: s.Arrival.String(),
		Departure: s.Departure.String(), LoaCm: int64(s.LOACm), BeamCm: int64(s.BeamCm), Category: view.String(s.Category),
		ApprovalDeadline: view.Time(s.ApprovalDeadline), HoldExpiresAt: view.Time(s.HoldExpiresAt),
		TotalCents: int64(s.Total), CreatedAt: s.CreatedAt.UTC(), Version: s.Version,
	}
	if s.ApprovalReason != "" {
		r := api.ApprovalReason(s.ApprovalReason)
		out.ApprovalReason = &r
	}
	if s.DueNow > 0 {
		d := int64(s.DueNow)
		out.DueNowCents = &d
	}
	return out
}

func userRef(id string, names map[string]string) *api.UserRef {
	if id == "" {
		return nil
	}
	return &api.UserRef{Id: id, Name: names[id]}
}

func (h *Handler) onlineDetailView(r *http.Request, marinaID string, d *booking.Detail) api.OnlineBookingDetail {
	ids := []string{d.ApprovedByUserID}
	for _, doc := range d.Documents {
		ids = append(ids, doc.VerifiedByUserID)
	}
	names := h.onlineUserNames(r, marinaID, ids)
	s := onlineSummaryView(&d.Summary)
	out := api.OnlineBookingDetail{
		ContractId: s.ContractId, Reference: s.Reference, Code: s.Code, Status: s.Status, Channel: s.Channel,
		ChannelCode: s.ChannelCode, Arrival: s.Arrival, Departure: s.Departure, LoaCm: s.LoaCm, BeamCm: s.BeamCm,
		Category: s.Category, ApprovalReason: s.ApprovalReason, ApprovalDeadline: s.ApprovalDeadline,
		HoldExpiresAt: s.HoldExpiresAt, TotalCents: s.TotalCents, DueNowCents: s.DueNowCents, CreatedAt: s.CreatedAt,
		Version: s.Version,
		Transit: api.ContractBrief{
			Id: d.ContractID, Code: d.TransitCode, Kind: api.ContractKindTransit, Status: api.ContractStatus(d.Status),
		},
		Notes: view.String(d.Notes), ExpectedArrivalTime: view.String(d.ExpectedArrivalTime),
		ApprovedAt: view.Time(d.ApprovedAt), ApprovedBy: userRef(d.ApprovedByUserID, names),
		RejectedAt: view.Time(d.RejectedAt), RejectReason: view.String(d.RejectReason),
		Documents: make([]api.OnlineBookingDocument, 0, len(d.Documents)),
		Consents:  make([]api.OnlineBookingConsent, 0, len(d.Consents)),
		Payment:   onlinePaymentView(d.Payment),
	}
	if d.PeopleOnBoard > 0 {
		out.PeopleOnBoard = view.Int(d.PeopleOnBoard)
	}
	if d.Locale != "" {
		l := api.Locale(d.Locale)
		out.Locale = &l
	}
	if d.CustomerID != "" {
		out.Customer = &api.CustomerBrief{Id: d.CustomerID}
	}
	if c := d.Contact; c != nil {
		out.Contact = &api.OnlineContact{
			FirstName: c.FirstName, LastName: c.LastName, Email: c.Email, Phone: view.String(c.Phone),
			Country: c.Country, Locale: api.Locale(c.Locale),
		}
		if out.Customer != nil {
			out.Customer.DisplayName = view.String(strings.TrimSpace(c.FirstName + " " + c.LastName))
		}
	}
	for _, doc := range d.Documents {
		item := api.OnlineBookingDocument{
			Id: doc.ID, Type: api.DocumentType(doc.Type), FileName: view.String(doc.FileName),
			UploadedAt: doc.UploadedAt.UTC(), VerifiedAt: view.Time(doc.VerifiedAt),
			VerifiedBy: userRef(doc.VerifiedByUserID, names),
		}
		if !doc.ExpiresOn.IsZero() {
			item.ExpiresOn = view.String(doc.ExpiresOn.String())
		}
		out.Documents = append(out.Documents, item)
	}
	for _, c := range d.Consents {
		out.Consents = append(out.Consents, api.OnlineBookingConsent{
			Kind: api.ConsentKind(c.Kind), Version: c.Version, Granted: c.Granted, RecordedAt: c.RecordedAt.UTC(),
		})
	}
	return out
}

func onlinePaymentView(p booking.PaymentState) api.OnlinePaymentState {
	state := p.State
	if state == "" {
		state = "none"
	}
	out := api.OnlinePaymentState{
		State: api.OnlinePaymentStateState(state), IntentId: view.String(p.IntentID), ProviderCode: view.String(p.ProviderCode),
		AuthorizedAt: view.Time(p.AuthorizedAt), CapturedAt: view.Time(p.CapturedAt),
	}
	if p.Amount > 0 {
		a := int64(p.Amount)
		out.AmountCents = &a
	}
	if p.CaptureMethod != "" {
		m := api.OnlinePaymentStateCaptureMethod(p.CaptureMethod)
		out.CaptureMethod = &m
	}
	if p.CaptureFailed {
		f := true
		out.CaptureFailed = &f
	}
	return out
}

func (h *Handler) writeOnlineDetail(w http.ResponseWriter, r *http.Request, marinaID string, d *booking.Detail) {
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, h.onlineDetailView(r, marinaID, d))
}

// ListOnlineBookings implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/online-bookings).
func (h *Handler) ListOnlineBookings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListOnlineBookingsParams) {
	dates := newCtrDates()
	f := booking.ListFilter{
		From: dates.parse(str(params.From)), To: dates.parse(str(params.To)), Cursor: str(params.Cursor),
		Limit: intOr(params.Limit, 0),
	}
	if !dates.ok {
		if _, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); err != nil {
			respond.Error(w, r, err)
			return
		}
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "from", Code: "invalid"}))
		return
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Channel != nil {
		f.Channel = string(*params.Channel)
	}
	page, err := h.Booking.List(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.OnlineBookingPage{Items: make([]api.OnlineBookingSummary, 0, len(page.Items)), NextCursor: view.String(page.NextCursor)}
	for i := range page.Items {
		out.Items = append(out.Items, onlineSummaryView(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetOnlineBooking implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/online-bookings/{contractId}).
func (h *Handler) GetOnlineBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	d, err := h.Booking.Detail(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineDetail(w, r, marinaID, d)
}

// ApproveOnlineBooking implements api.ServerInterface (POST .../online-bookings/{contractId}/approve).
func (h *Handler) ApproveOnlineBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.ApproveOnlineBookingParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	d, err := h.Booking.Approve(r.Context(), principal(r), marinaID, contractID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineDetail(w, r, marinaID, d)
}

// RejectOnlineBooking implements api.ServerInterface (POST .../online-bookings/{contractId}/reject).
func (h *Handler) RejectOnlineBooking(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.RejectOnlineBookingParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.OnlineBookingReject
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d, err := h.Booking.Reject(r.Context(), principal(r), marinaID, contractID, ifMatch, strings.TrimSpace(in.Reason), in.Notify)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineDetail(w, r, marinaID, d)
}

// SendOnlineBookingPaymentLink implements api.ServerInterface (POST .../online-bookings/{contractId}/payment-link).
func (h *Handler) SendOnlineBookingPaymentLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.SendOnlineBookingPaymentLinkParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	d, err := h.Booking.SendPaymentLink(r.Context(), principal(r), marinaID, contractID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineDetail(w, r, marinaID, d)
}

// VerifyDocument implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/documents/{documentId}/verify).
func (h *Handler) VerifyDocument(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, documentID api.DocumentId) {
	var in api.DocumentVerify
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v, err := h.Documents.Verify(r.Context(), principal(r), marinaID, documentID, in.Verified)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	names := h.onlineUserNames(r, marinaID, []string{v.VerifiedByUserID})
	respond.JSON(w, http.StatusOK, api.DocumentVerification{
		DocumentId: v.DocumentID, Verified: v.Verified, VerifiedAt: view.Time(v.VerifiedAt),
		VerifiedBy: userRef(v.VerifiedByUserID, names),
	})
}
