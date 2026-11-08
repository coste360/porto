package public

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
)

// Public booking page operations (m8-plan §11, tag booking-public) over booking.Service. Tokens come only from
// headers (Porto-Booking-Token, Porto-Status-Token, Porto-Customer-Session) and are never logged. Every unknown,
// expired or foreign token or id answers 404 link_invalid. Responses are not cached, except the configuration.

// maxBookingUpload bounds a booking document upload (m8-plan §5.2).
const maxBookingUpload = 10 << 20

func bkHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
}

func bkStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func bkOpt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func bkI18n(n booking.Name) api.I18nText {
	out := api.I18nText{It: n.It}
	if out.It == "" {
		out.It = n.En
	}
	if n.En != "" {
		en := n.En
		out.En = &en
	}
	return out
}

func bkLong(t onlineconf.LongText) api.BookingI18nText {
	return api.BookingI18nText{It: t.It, En: bkOpt(t.En)}
}

func bkTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func bkDate(s string, field string) (calendar.Date, error) {
	d, err := calendar.Parse(s)
	if err != nil {
		return calendar.Date{}, &booking.RulesError{Field: field, Code: "invalid"}
	}
	return d, nil
}

func bkTiers(ts []onlineconf.Tier) []api.CancellationTier {
	out := make([]api.CancellationTier, 0, len(ts))
	for _, t := range ts {
		out = append(out, api.CancellationTier{MinDaysBefore: t.MinDaysBefore, RefundBp: t.RefundBP})
	}
	return out
}

func bkTokens(bookingTok *api.BookingTokenHeader, statusTok *api.StatusTokenHeader) booking.Tokens {
	t := booking.Tokens{}
	if bookingTok != nil {
		t.Booking = *bookingTok
	}
	if statusTok != nil {
		t.Status = *statusTok
	}
	return t
}

func bkVessel(v api.BookingVesselInput) booking.Vessel {
	out := booking.Vessel{LOACm: geo.Cm(v.LoaCm), BeamCm: geo.Cm(v.BeamCm)}
	if v.DraftCm != nil {
		out.DraftCm = geo.Cm(*v.DraftCm)
	}
	if v.HullType != nil {
		out.HullType = string(*v.HullType)
	}
	return out
}

func bkServices(in *[]api.BookingServiceInput) []booking.ServiceRequest {
	if in == nil {
		return nil
	}
	out := make([]booking.ServiceRequest, 0, len(*in))
	for _, s := range *in {
		out = append(out, booking.ServiceRequest{ItemID: s.ItemId, QtyMilli: s.QtyMilli})
	}
	return out
}

func bkQuote(q booking.Quote) api.BookingQuote {
	out := api.BookingQuote{
		ApprovalRequired: q.ApprovalRequired, CancellationTiers: bkTiers(q.CancellationTiers), Category: q.Category,
		Currency: q.Currency, DueNowCents: int64(q.DueNowCents), NetCents: int64(q.NetCents), Nights: q.Nights,
		PaymentMode: api.OnlinePaymentMode(q.PaymentMode), PromoApplied: q.PromoApplied, TotalCents: int64(q.TotalCents),
		VatCents: int64(q.VATCents), Lines: []api.BookingPriceLine{},
	}
	if out.Currency == "" {
		out.Currency = "EUR"
	}
	if out.PaymentMode == "" {
		out.PaymentMode = api.OnlinePaymentMode(onlineconf.PaymentFull)
	}
	for _, l := range q.Lines {
		label := bkI18n(l.Label)
		if label.It == "" {
			label.It = l.Kind
		}
		out.Lines = append(out.Lines, api.BookingPriceLine{
			Kind: api.BookingPriceLineKind(l.Kind), Label: label, ItemId: bkOpt(l.ItemID), QtyMilli: l.QtyMilli, TotalCents: int64(l.TotalCents),
		})
	}
	return out
}

func bkDocuments(ds []booking.Document) []api.BookingDocument {
	out := make([]api.BookingDocument, 0, len(ds))
	for _, d := range ds {
		out = append(out, api.BookingDocument{Id: d.ID, Type: api.BookingDocumentType(d.Type), UploadedAt: d.UploadedAt.UTC(), Verified: d.Verified})
	}
	return out
}

func bkBooking(b *booking.Booking) api.Booking {
	out := api.Booking{
		AmountDueNowCents: int64(b.AmountDueNow), ApprovalDeadline: bkTime(b.ApprovalDeadline), ApprovalRequired: b.ApprovalRequired,
		Arrival: b.Arrival.String(), BookingId: b.BookingID, Category: b.Category, Departure: b.Departure.String(),
		DetailsComplete: b.DetailsComplete, Documents: bkDocuments(b.Documents), HoldExpiresAt: bkTime(b.HoldExpiresAt),
		MissingDocuments: []api.BookingDocumentType{}, Quote: bkQuote(b.Quote), Reference: b.Reference,
		Status: api.OnlineBookingStatus(b.Status),
		Vessel: api.BookingVesselView{LoaCm: int64(b.Vessel.LOACm), BeamCm: int64(b.Vessel.BeamCm)},
	}
	for _, m := range b.MissingDocuments {
		out.MissingDocuments = append(out.MissingDocuments, api.BookingDocumentType(m))
	}
	if b.Vessel.DraftCm > 0 {
		d := int64(b.Vessel.DraftCm)
		out.Vessel.DraftCm = &d
	}
	if b.Vessel.HullType != "" {
		h := api.HullType(b.Vessel.HullType)
		out.Vessel.HullType = &h
	}
	if b.Vessel.Name != "" {
		out.Vessel.Name = bkOpt(b.Vessel.Name)
	}
	return out
}

// clientKey is the lost-demand dedupe key of a request: a hash of the IP prefix (/24, /48), never stored.
func clientKey(r *http.Request) string {
	info := reqctx.From(r.Context())
	if info == nil || !info.ClientIP.IsValid() {
		return ""
	}
	ip := info.ClientIP.Unmap()
	bits := 24
	if ip.Is6() {
		bits = 48
	}
	p, err := ip.Prefix(bits)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(p.String()))
	return hex.EncodeToString(sum[:8])
}

func remoteIP(r *http.Request) string {
	if info := reqctx.From(r.Context()); info != nil && info.ClientIP.IsValid() {
		return info.ClientIP.String()
	}
	return ""
}

// GetBookingConfig implements api.ServerInterface (GET /api/public/v1/{slug}/booking/config).
func (h *Handler) GetBookingConfig(w http.ResponseWriter, r *http.Request, slug api.Slug) {
	c, err := h.Booking.Config(r.Context(), slug)
	if err != nil {
		bkHeaders(w)
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	st := c.Settings
	out := api.BookingConfig{
		ApprovalAlways: st.Approval.Always, CancellationTiers: bkTiers(st.CancellationTiers), Categories: []api.BookingCategory{},
		Currency: c.Currency, DepositBp: int64(st.DepositBP), Enabled: c.Enabled, Payable: c.Payable,
		PaymentMode: api.OnlinePaymentMode(st.PaymentMode), ShowFeet: st.ShowFeet, Services: []api.BookingServiceOption{},
		Texts: []api.BookingTextRef{}, Theme: api.BookingTheme{HideHeaderInEmbed: st.HideHeaderInEmbed, Preset: st.ThemePreset},
		RequiredFields: api.BookingRequiredFields{
			Documents: []api.BookingDocumentType{}, DocumentsLater: st.RequiredFields.DocumentsLater,
			PeopleOnBoard: st.RequiredFields.PeopleOnBoard, Phone: st.RequiredFields.Phone,
		},
		Rules: api.BookingRules{
			ArrivalFrom: st.ArrivalFrom, ArrivalTo: st.ArrivalTo, ClosedWeekdays: []int{}, MaxAdvanceDays: st.MaxAdvanceDays,
			MaxNights: st.MaxNights, MinAdvanceHours: st.MinAdvanceHours, MinNights: st.MinNights,
		},
	}
	if st.Approval.LOAOverCm != nil {
		v := int64(*st.Approval.LOAOverCm)
		out.ApprovalLoaOverCm = &v
	}
	for _, d := range st.ClosedWeekdays {
		out.Rules.ClosedWeekdays = append(out.Rules.ClosedWeekdays, int(d))
	}
	for _, d := range st.RequiredFields.Documents {
		out.RequiredFields.Documents = append(out.RequiredFields.Documents, api.BookingDocumentType(d))
	}
	if a := st.ArrivalInfo; a.VHFChannel != "" || a.Phone != "" || a.RulesURL != "" || !a.Text.IsZero() {
		ai := &api.ArrivalInfo{VhfChannel: bkOpt(a.VHFChannel), Phone: bkOpt(a.Phone), RulesUrl: bkOpt(a.RulesURL)}
		if !a.Text.IsZero() {
			t := bkLong(a.Text)
			ai.Text = &t
		}
		out.ArrivalInfo = ai
	}
	for _, cat := range c.Categories {
		bc := api.BookingCategory{Code: cat.Code, Name: bkI18n(cat.Name)}
		if cat.MaxLOACm > 0 {
			v := int64(cat.MaxLOACm)
			bc.MaxLoaCm = &v
		}
		if cat.MaxBeamCm > 0 {
			v := int64(cat.MaxBeamCm)
			bc.MaxBeamCm = &v
		}
		out.Categories = append(out.Categories, bc)
	}
	for _, s := range c.Services {
		so := api.BookingServiceOption{DefaultSelected: s.DefaultSelected, ItemId: s.ItemID, Name: bkI18n(s.Name), Unit: api.ItemUnit(s.Unit)}
		if s.Description.It != "" || s.Description.En != "" {
			so.Description = &api.I18nTextOptional{It: bkOpt(s.Description.It), En: bkOpt(s.Description.En)}
		}
		if s.UnitPriceCents > 0 {
			v := int64(s.UnitPriceCents)
			so.UnitPriceCents = &v
		}
		if s.MaxQtyMilli > 0 {
			v := s.MaxQtyMilli
			so.MaxQtyMilli = &v
		}
		out.Services = append(out.Services, so)
	}
	for _, t := range c.Texts {
		out.Texts = append(out.Texts, api.BookingTextRef{Kind: api.BookingTextKind(t.Kind), Version: t.Version, Text: bkLong(t.Text)})
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetBookingAvailability implements api.ServerInterface (GET /api/public/v1/{slug}/booking/availability).
func (h *Handler) GetBookingAvailability(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.GetBookingAvailabilityParams) {
	bkHeaders(w)
	in := booking.AvailabilityInput{VesselID: bkStr(params.VesselId), ClientKey: clientKey(r)}
	if params.PortoCustomerSession != nil {
		in.SessionToken = *params.PortoCustomerSession
	}
	var err error
	if in.Arrival, err = bkDate(params.Arrival, "arrival"); err != nil {
		respond.Error(w, r, err)
		return
	}
	if in.Departure, err = bkDate(params.Departure, "departure"); err != nil {
		respond.Error(w, r, err)
		return
	}
	if in.VesselID == "" {
		if params.LoaCm == nil || params.BeamCm == nil {
			respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "loa_cm", Code: "required"}))
			return
		}
		in.Vessel = booking.Vessel{LOACm: geo.Cm(*params.LoaCm), BeamCm: geo.Cm(*params.BeamCm)}
		if params.DraftCm != nil {
			in.Vessel.DraftCm = geo.Cm(*params.DraftCm)
		}
		if params.HullType != nil {
			in.Vessel.HullType = string(*params.HullType)
		}
	}
	a, err := h.Booking.Availability(r.Context(), slug, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.BookingAvailability{Arrival: a.Arrival.String(), Departure: a.Departure.String(), Nights: a.Nights, Categories: []api.BookingCategoryAvailability{}}
	if a.Reason != "" {
		reason := api.BookingBlockReason(a.Reason)
		out.Reason = &reason
	}
	for _, c := range a.Categories {
		ca := api.BookingCategoryAvailability{ApprovalRequired: c.ApprovalRequired, Available: c.Available, Code: c.Code, Name: bkI18n(c.Name)}
		if c.Available && c.FromCents > 0 {
			v := int64(c.FromCents)
			ca.FromCents = &v
		}
		out.Categories = append(out.Categories, ca)
	}
	respond.JSON(w, http.StatusOK, out)
}

// QuoteBooking implements api.ServerInterface (POST /api/public/v1/{slug}/booking/quote).
func (h *Handler) QuoteBooking(w http.ResponseWriter, r *http.Request, slug api.Slug) {
	bkHeaders(w)
	var body api.BookingQuoteInput
	if !respond.DecodeOrFail(w, r, &body, false) {
		return
	}
	in := booking.QuoteInput{Vessel: bkVessel(body.Vessel), Category: body.Category, Services: bkServices(body.Services), PromoCode: bkStr(body.PromoCode)}
	var err error
	if in.Arrival, err = bkDate(body.Arrival, "arrival"); err == nil {
		in.Departure, err = bkDate(body.Departure, "departure")
	}
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	q, err := h.Booking.Quote(r.Context(), slug, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, bkQuote(*q))
}

// CreateBooking implements api.ServerInterface (POST /api/public/v1/{slug}/bookings). Idempotency is in the
// domain (x-porto-idempotency: domain): the response, which carries the booking token, is never stored.
func (h *Handler) CreateBooking(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.CreateBookingParams) {
	bkHeaders(w)
	var body api.BookingCreate
	if !respond.DecodeOrFail(w, r, &body, false) {
		return
	}
	canonical, _ := json.Marshal(body)
	sum := sha256.Sum256(canonical)
	in := booking.CreateInput{
		Category: body.Category, Services: bkServices(body.Services), PromoCode: bkStr(body.PromoCode), Locale: string(body.Locale),
		EmbedParentOrigin: bkStr(body.EmbedParentOrigin), IdempotencyKey: params.IdempotencyKey, BodySHA256: hex.EncodeToString(sum[:]),
		VesselID: bkStr(body.VesselId), RemoteIP: remoteIP(r),
	}
	if params.PortoCustomerSession != nil {
		in.SessionToken = *params.PortoCustomerSession
	}
	if params.PortoBotToken != nil {
		in.BotToken = *params.PortoBotToken
	}
	if body.Vessel != nil {
		in.Vessel = bkVessel(*body.Vessel)
	} else if in.VesselID == "" {
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "vessel", Code: "required"}))
		return
	}
	var err error
	if in.Arrival, err = bkDate(body.Arrival, "arrival"); err == nil {
		in.Departure, err = bkDate(body.Departure, "departure")
	}
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	c, err := h.Booking.Create(r.Context(), slug, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.BookingCreated{
		AmountDueNowCents: int64(c.AmountDueNow), ApprovalRequired: c.ApprovalRequired, BookingId: c.BookingID,
		BookingToken: c.BookingToken, HoldExpiresAt: c.HoldExpiresAt.UTC(), Quote: bkQuote(c.Quote), Reference: c.Reference,
		Status: api.OnlineBookingStatus(c.Status),
	})
}

// GetBooking implements api.ServerInterface (GET /api/public/v1/{slug}/bookings/{bookingId}).
func (h *Handler) GetBooking(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.GetBookingParams) {
	bkHeaders(w)
	b, err := h.Booking.Get(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, nil))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, bkBooking(b))
}

// AbandonBooking implements api.ServerInterface (DELETE /api/public/v1/{slug}/bookings/{bookingId}).
func (h *Handler) AbandonBooking(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.AbandonBookingParams) {
	bkHeaders(w)
	if err := h.Booking.Abandon(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, nil)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// SubmitBookingDetails implements api.ServerInterface (PUT /api/public/v1/{slug}/bookings/{bookingId}/details).
func (h *Handler) SubmitBookingDetails(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.SubmitBookingDetailsParams) {
	bkHeaders(w)
	var body api.BookingDetailsInput
	if !respond.DecodeOrFail(w, r, &body, false) {
		return
	}
	in := booking.DetailsInput{
		Customer: booking.CustomerDetails{
			FirstName: body.Customer.FirstName, LastName: body.Customer.LastName, Email: string(body.Customer.Email),
			Phone: bkStr(body.Customer.Phone), Country: body.Customer.Country, Locale: string(body.Customer.Locale),
		},
		Vessel: booking.VesselDetails{
			Name: body.Vessel.Name, Registration: bkStr(body.Vessel.Registration), Flag: bkStr(body.Vessel.Flag),
			HullType: string(body.Vessel.HullType),
		},
		ExpectedArrivalTime: bkStr(body.ExpectedArrivalTime), Notes: bkStr(body.Notes),
		Consents: booking.Consents{
			TermsVersion: body.Consents.TermsVersion, PrivacyVersion: body.Consents.PrivacyVersion, Marketing: body.Consents.Marketing,
		},
	}
	if body.Vessel.DraftCm != nil {
		in.Vessel.DraftCm = geo.Cm(*body.Vessel.DraftCm)
	}
	if body.PeopleOnBoard != nil {
		in.PeopleOnBoard = *body.PeopleOnBoard
	}
	if body.Consents.MarketingVersion != nil {
		in.Consents.MarketingVersion = *body.Consents.MarketingVersion
	}
	b, err := h.Booking.SubmitDetails(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, nil), in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, bkBooking(b))
}

// UploadBookingDocument implements api.ServerInterface (POST /api/public/v1/{slug}/bookings/{bookingId}/documents):
// the raw body is the file (PDF, PNG or JPEG), ?type= its kind.
func (h *Handler) UploadBookingDocument(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.UploadBookingDocumentParams) {
	bkHeaders(w)
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBookingUpload))
	if err != nil {
		respond.Error(w, r, document.ErrFileTooLarge)
		return
	}
	up.Body = body
	d, err := h.Booking.UploadDocument(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, nil), up)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.BookingDocument{Id: d.ID, Type: api.BookingDocumentType(d.Type), UploadedAt: d.UploadedAt.UTC(), Verified: d.Verified})
}

// StartBookingPayment implements api.ServerInterface (POST /api/public/v1/{slug}/bookings/{bookingId}/pay).
func (h *Handler) StartBookingPayment(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.StartBookingPaymentParams) {
	bkHeaders(w)
	var body api.BookingPayInput
	if !respond.DecodeOrFail(w, r, &body, true) {
		return
	}
	in := booking.PayInput{ReturnTo: bkStr(body.ReturnTo)}
	if params.IdempotencyKey != nil {
		in.IdempotencyKey = *params.IdempotencyKey
	}
	res, err := h.Booking.StartPayment(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, nil), in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.BookingPayResult{RedirectUrl: bkOpt(res.RedirectURL), Status: api.OnlineBookingStatus(res.Status), StatusToken: res.StatusToken})
}

// GetBookingStatus implements api.ServerInterface (GET /api/public/v1/{slug}/bookings/{bookingId}/status).
func (h *Handler) GetBookingStatus(w http.ResponseWriter, r *http.Request, slug api.Slug, bookingID api.BookingId, params api.GetBookingStatusParams) {
	bkHeaders(w)
	st, err := h.Booking.Status(r.Context(), slug, bookingID, bkTokens(params.PortoBookingToken, params.PortoStatusToken))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.BookingStatusView{
		BookingId: st.BookingID, HoldExpiresAt: bkTime(st.HoldExpiresAt), Reference: st.Reference,
		Status: api.OnlineBookingStatus(st.Status), Verifying: st.Verifying,
	})
}
