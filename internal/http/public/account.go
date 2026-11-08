package public

import (
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/reqctx"
)

// Customer area operations (m8-plan §7, tag account) over portal.Service. The session token comes only from the
// Porto-Customer-Session header and the link token only from the request body (it travels in the URL fragment of
// the email link); neither is ever logged. Every unknown, expired or foreign token or id answers 404 link_invalid.

// maxAccountUpload bounds a customer-area document upload (m8-plan §5.2).
const maxAccountUpload = 10 << 20

func accountSession(h *api.CustomerSessionHeader) string {
	if h == nil {
		return ""
	}
	return *h
}

func accountIdempotency(k *api.IdempotencyKey) string {
	if k == nil {
		return ""
	}
	return *k
}

func accountHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
}

func accountRemoteIP(r *http.Request) string {
	if info := reqctx.From(r.Context()); info != nil && info.ClientIP.IsValid() {
		return info.ClientIP.String()
	}
	return ""
}

// RequestAccountLink implements api.ServerInterface (POST /api/public/v1/{slug}/account/login).
func (h *Handler) RequestAccountLink(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.RequestAccountLinkParams) {
	accountHeaders(w)
	var in api.AccountLoginInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	bot := ""
	if params.PortoBotToken != nil {
		bot = *params.PortoBotToken
	}
	if err := h.Portal.RequestLink(r.Context(), slug, string(in.Email), string(in.Locale), bot, accountRemoteIP(r)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusAccepted, api.AccountLoginAccepted{Status: api.AccountLoginAcceptedStatusAccepted})
}

// OpenAccountSession implements api.ServerInterface (POST /api/public/v1/{slug}/account/session).
func (h *Handler) OpenAccountSession(w http.ResponseWriter, r *http.Request, slug api.Slug) {
	accountHeaders(w)
	var in api.AccountSessionInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Portal.OpenSession(r.Context(), slug, in.Token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.AccountSession{SessionToken: s.Token, ExpiresAt: s.ExpiresAt.UTC()})
}

// CloseAccountSession implements api.ServerInterface (DELETE /api/public/v1/{slug}/account/session).
func (h *Handler) CloseAccountSession(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.CloseAccountSessionParams) {
	accountHeaders(w)
	if err := h.Portal.CloseSession(r.Context(), slug, accountSession(params.PortoCustomerSession)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ListAccountVessels implements api.ServerInterface (GET /api/public/v1/{slug}/account/vessels).
func (h *Handler) ListAccountVessels(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.ListAccountVesselsParams) {
	accountHeaders(w)
	vs, err := h.Portal.Vessels(r.Context(), slug, accountSession(params.PortoCustomerSession))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AccountVesselList{Items: make([]api.AccountVessel, 0, len(vs))}
	for _, v := range vs {
		item := api.AccountVessel{Id: v.ID, Name: v.Name, LoaCm: v.LOACm, BeamCm: v.BeamCm}
		if v.DraftCm > 0 {
			d := v.DraftCm
			item.DraftCm = &d
		}
		if v.HullType != "" {
			ht := api.HullType(v.HullType)
			item.HullType = &ht
		}
		out.Items = append(out.Items, item)
	}
	respond.JSON(w, http.StatusOK, out)
}

// ListAccountBookings implements api.ServerInterface (GET /api/public/v1/{slug}/account/bookings).
func (h *Handler) ListAccountBookings(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.ListAccountBookingsParams) {
	accountHeaders(w)
	bs, err := h.Portal.Bookings(r.Context(), slug, accountSession(params.PortoCustomerSession))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AccountBookingList{Items: make([]api.AccountBookingSummary, 0, len(bs))}
	for i := range bs {
		out.Items = append(out.Items, accountSummary(&bs[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetAccountBooking implements api.ServerInterface (GET /api/public/v1/{slug}/account/bookings/{contractId}).
func (h *Handler) GetAccountBooking(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.GetAccountBookingParams) {
	accountHeaders(w)
	b, err := h.Portal.Booking(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, accountBooking(b))
}

// UploadAccountDocument implements api.ServerInterface (POST /api/public/v1/{slug}/account/bookings/{contractId}/documents).
func (h *Handler) UploadAccountDocument(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.UploadAccountDocumentParams) {
	accountHeaders(w)
	up := booking.DocumentUpload{Type: string(params.Type), ContentType: r.Header.Get("Content-Type"), FileName: "documento"}
	if params.XFileName != nil && *params.XFileName != "" {
		up.FileName = *params.XFileName
	}
	if params.ExpiresOn != nil {
		d, err := calendar.Parse(*params.ExpiresOn)
		if err != nil {
			respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "expires_on", Code: "invalid"}))
			return
		}
		up.ExpiresOn = d
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAccountUpload))
	if err != nil {
		respond.Error(w, r, document.ErrFileTooLarge)
		return
	}
	up.Body = body
	d, err := h.Portal.UploadDocument(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID, up)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, accountDocument(*d))
}

// GetAccountDocumentURL implements api.ServerInterface (GET /api/public/v1/{slug}/account/documents/{documentId}/url).
func (h *Handler) GetAccountDocumentURL(w http.ResponseWriter, r *http.Request, slug api.Slug, documentID api.DocumentId, params api.GetAccountDocumentURLParams) {
	accountHeaders(w)
	u, err := h.Portal.DocumentURL(r.Context(), slug, accountSession(params.PortoCustomerSession), documentID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.DocumentFileUrl{Url: u.URL, ExpiresAt: u.ExpiresAt.UTC()})
}

// GetAccountDocumentFile implements api.ServerInterface (GET /api/public/v1/{slug}/account/documents/{documentId}/file):
// the signed link is the credential (customer, marina, 5-minute expiry); any invalid link is the same answer.
func (h *Handler) GetAccountDocumentFile(w http.ResponseWriter, r *http.Request, slug api.Slug, documentID api.DocumentId, params api.GetAccountDocumentFileParams) {
	accountHeaders(w)
	f, err := h.Portal.OpenDocument(r.Context(), slug, params.C, documentID, params.Exp, params.Sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", f.ContentType)
	hd.Set("Content-Length", strconv.Itoa(len(f.Content)))
	hd.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(f.FileName))
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Content) //nolint:gosec // G705: stored file bytes served with sandbox CSP, nosniff and a sniffed content type
}

// PayAccountBalance implements api.ServerInterface (POST /api/public/v1/{slug}/account/bookings/{contractId}/pay-balance).
func (h *Handler) PayAccountBalance(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.PayAccountBalanceParams) {
	accountHeaders(w)
	res, err := h.Portal.PayBalance(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.AccountPayBalanceResult{CheckoutUrl: res.CheckoutURL, AmountCents: int64(res.Amount)})
}

// PreviewAccountCancellation implements api.ServerInterface (GET /api/public/v1/{slug}/account/bookings/{contractId}/cancellation).
func (h *Handler) PreviewAccountCancellation(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.PreviewAccountCancellationParams) {
	accountHeaders(w)
	p, err := h.Portal.PreviewCancellation(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, accountCancellation(p))
}

// CancelAccountBooking implements api.ServerInterface (POST /api/public/v1/{slug}/account/bookings/{contractId}/cancel).
func (h *Handler) CancelAccountBooking(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.CancelAccountBookingParams) {
	accountHeaders(w)
	var in api.AccountCancelInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Portal.Cancel(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID,
		in.ExpectedRefundCents)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.AccountCancelResult{
		Status: api.OnlineBookingStatus(res.Status), RefundCents: int64(res.Refund), RefundAs: api.RefundAs(res.RefundAs),
	})
}

// accountDateChange decodes a date change body (both dates required, departure after arrival).
func accountDateChange(w http.ResponseWriter, r *http.Request, key string) (booking.DateChange, bool) {
	var in api.DateChangeInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return booking.DateChange{}, false
	}
	a, errA := calendar.Parse(in.Arrival)
	d, errD := calendar.Parse(in.Departure)
	var fields []problem.FieldError
	if errA != nil {
		fields = append(fields, problem.FieldError{Field: "arrival", Code: "invalid"})
	}
	if errD != nil {
		fields = append(fields, problem.FieldError{Field: "departure", Code: "invalid"})
	}
	if errA == nil && errD == nil && !d.After(a) {
		fields = append(fields, problem.FieldError{Field: "departure", Code: "before_arrival"})
	}
	if len(fields) > 0 {
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(fields...))
		return booking.DateChange{}, false
	}
	out := booking.DateChange{Arrival: a, Departure: d, IdempotencyKey: key}
	if in.ExpectedTotalCents != nil {
		c := money.Cents(*in.ExpectedTotalCents)
		out.ExpectedTotalCents = &c
	}
	return out, true
}

// PreviewAccountDateChange implements api.ServerInterface (POST /api/public/v1/{slug}/account/bookings/{contractId}/change-dates/preview).
func (h *Handler) PreviewAccountDateChange(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.PreviewAccountDateChangeParams) {
	accountHeaders(w)
	in, ok := accountDateChange(w, r, "")
	if !ok {
		return
	}
	p, err := h.Portal.PreviewDateChange(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.DateChangePreview{
		Available: p.Available, OldTotalCents: int64(p.OldTotal), NewTotalCents: int64(p.NewTotal),
		DeltaCents: int64(p.Delta), RefundCents: int64(p.Refund), SupplementCents: int64(p.Supplement),
	}
	if p.Reason != "" {
		reason := api.BookingBlockReason(p.Reason)
		out.Reason = &reason
	}
	respond.JSON(w, http.StatusOK, out)
}

// ChangeAccountDates implements api.ServerInterface (POST /api/public/v1/{slug}/account/bookings/{contractId}/change-dates).
func (h *Handler) ChangeAccountDates(w http.ResponseWriter, r *http.Request, slug api.Slug, contractID api.ContractId, params api.ChangeAccountDatesParams) {
	accountHeaders(w)
	in, ok := accountDateChange(w, r, accountIdempotency(params.IdempotencyKey))
	if !ok {
		return
	}
	if in.ExpectedTotalCents == nil {
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "expected_total_cents", Code: "required"}))
		return
	}
	res, err := h.Portal.ChangeDates(r.Context(), slug, accountSession(params.PortoCustomerSession), contractID, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.DateChangeResult{
		Status: api.OnlineBookingStatus(res.Status), Arrival: res.Arrival.String(), Departure: res.Departure.String(),
		NewTotalCents: int64(res.NewTotal), DeltaCents: int64(res.Delta), RefundCents: int64(res.Refund),
		SupplementCents: int64(res.Supplement),
	})
}

// ---- mapping (prefixed account* so that they never clash with the booking handlers) ----

func accountI18n(n *booking.Name) *api.I18nText {
	if n == nil || (n.It == "" && n.En == "") {
		return nil
	}
	out := &api.I18nText{It: n.It}
	if n.En != "" {
		en := n.En
		out.En = &en
	}
	return out
}

func accountOptString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func accountSummary(b *booking.CustomerBooking) api.AccountBookingSummary {
	return api.AccountBookingSummary{
		ContractId: b.ContractID, Code: b.Code, Reference: accountOptString(b.Reference),
		Status: api.OnlineBookingStatus(b.Status), Channel: api.Channel(b.Channel), Arrival: b.Arrival.String(),
		Departure: b.Departure.String(), VesselName: b.VesselName, Category: accountI18n(b.Category),
		TotalCents: int64(b.Total), PaidCents: int64(b.Paid), ResidualCents: int64(b.Residual),
		ApprovalPending: b.ApprovalPending, BerthCode: accountOptString(b.BerthCode),
	}
}

func accountDocument(d booking.Document) api.BookingDocument {
	return api.BookingDocument{
		Id: d.ID, Type: api.BookingDocumentType(d.Type), UploadedAt: d.UploadedAt.UTC(), Verified: d.Verified,
	}
}

func accountCancellation(p *booking.CancellationPreview) api.CancellationPreview {
	out := api.CancellationPreview{
		Allowed: p.Allowed, PaidCents: int64(p.Paid), RefundCents: int64(p.Refund), RefundAs: api.RefundAs(p.RefundAs),
	}
	if out.RefundAs == "" {
		out.RefundAs = api.RefundAs("refund")
	}
	if p.Reason != "" {
		reason := api.CancellationPreviewReason(p.Reason)
		out.Reason = &reason
	}
	if p.Tier != nil {
		out.Tier = &api.CancellationTier{MinDaysBefore: p.Tier.MinDaysBefore, RefundBp: p.Tier.RefundBP}
	}
	return out
}

func accountBooking(b *booking.CustomerBooking) api.AccountBooking {
	out := api.AccountBooking{
		ContractId: b.ContractID, Code: b.Code, Reference: accountOptString(b.Reference),
		Status: api.OnlineBookingStatus(b.Status), Channel: api.Channel(b.Channel), Arrival: b.Arrival.String(),
		Departure: b.Departure.String(), VesselName: b.VesselName, Category: accountI18n(b.Category),
		TotalCents: int64(b.Total), PaidCents: int64(b.Paid), ResidualCents: int64(b.Residual),
		ApprovalPending: b.ApprovalPending, BerthCode: accountOptString(b.BerthCode),
		QrPayload: accountOptString(b.QRPayload), CanCancel: b.CanCancel, CanChangeDates: b.CanChangeDates,
		CanPayBalance: b.CanPayBalance, Documents: []api.BookingDocument{},
		MissingDocuments: []api.BookingDocumentType{}, Receipts: []api.AccountReceipt{},
	}
	if a := b.ArrivalInfo; a != nil {
		ai := &api.ArrivalInfo{VhfChannel: accountOptString(a.VHFChannel), Phone: accountOptString(a.Phone), RulesUrl: accountOptString(a.RulesURL)}
		if !a.Text.IsZero() {
			ai.Text = &api.BookingI18nText{It: a.Text.It, En: accountOptString(a.Text.En)}
		}
		out.ArrivalInfo = ai
	}
	if v := b.Vessel; v != nil {
		vv := &api.BookingVesselView{Name: accountOptString(v.Name), LoaCm: int64(v.LOACm), BeamCm: int64(v.BeamCm)}
		if v.DraftCm > 0 {
			d := int64(v.DraftCm)
			vv.DraftCm = &d
		}
		if v.HullType != "" {
			ht := api.HullType(v.HullType)
			vv.HullType = &ht
		}
		out.Vessel = vv
	}
	for _, d := range b.Documents {
		out.Documents = append(out.Documents, accountDocument(d))
	}
	for _, t := range b.MissingDocuments {
		out.MissingDocuments = append(out.MissingDocuments, api.BookingDocumentType(t))
	}
	for _, rc := range b.Receipts {
		out.Receipts = append(out.Receipts, api.AccountReceipt{
			PaymentId: rc.PaymentID, ReceivedOn: rc.ReceivedOn.String(), AmountCents: int64(rc.Amount),
			Method: api.PaymentMethod(rc.Method),
		})
	}
	if b.Cancellation != nil {
		c := accountCancellation(b.Cancellation)
		out.Cancellation = &c
	}
	return out
}
