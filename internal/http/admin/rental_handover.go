package admin

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/handover"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qrsvg"
	"github.com/coste360/porto/internal/qty"
)

// Rental handover operations (m11-plan §13.1): check-out and check-in drafts (checklist and photos are
// offline-capable), identity, contract signature and PDF, deposit, damages, check-in preview and commit, close,
// handover PDFs and quick cash. The handlers decode, call the handover service (which authorises first: another
// marina, or another marina's id, is 404 before anything else) and encode.

// ---- conversions ----

func hoDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func hoFile(f *handover.FileView) api.RentalHandoverFile {
	return api.RentalHandoverFile{
		Id: f.ID, FileId: f.FileID, Kind: api.RentalFileKind(f.Kind), DamageId: view.String(f.DamageID),
		ContentType: f.ContentType, CreatedAt: f.CreatedAt.UTC(),
	}
}

func hoFiles(fs []handover.FileView) []api.RentalHandoverFile {
	out := make([]api.RentalHandoverFile, 0, len(fs))
	for i := range fs {
		out = append(out, hoFile(&fs[i]))
	}
	return out
}

func hoView(v *handover.View) api.RentalHandover {
	h := v.Handover
	out := api.RentalHandover{
		BookingId: h.BookingID, Phase: api.RentalPhase(h.Phase), Notes: h.Notes, Version: int64(h.Version),
		Checklist: make([]api.RentalChecklistEntry, 0, len(v.Checklist)), Files: hoFiles(v.Files),
		Missing: make([]api.RentalHandoverMissing, 0, len(v.Missing)), FuelEighths: h.FuelEighths,
		EngineHoursMilli: h.EngineHoursMilli, RefuelMilli: h.RefuelMilli, ActualAt: view.Time(h.ActualAt),
		CompletedAt: view.Time(h.CompletedAt), Id: view.String(h.ID),
	}
	signed := v.ContractSigned
	out.ContractSigned = &signed
	for _, c := range v.Checklist {
		out.Checklist = append(out.Checklist, api.RentalChecklistEntry{Key: c.Key, Ok: c.OK, Note: view.String(c.Note)})
	}
	for _, m := range v.Missing {
		out.Missing = append(out.Missing, api.RentalHandoverMissing(m))
	}
	if id := v.Identity; id != nil {
		sum := api.RentalIdentitySummary{
			DocumentType: api.RentalIdentitySummaryDocumentType(id.DocumentType), DocumentNumberMasked: id.DocumentNumberMasked,
			DocumentExpiresOn: id.DocumentExpiresOn.String(), LicenceNumberMasked: view.String(id.LicenceNumberMasked),
			LicenceCategory: view.String(id.LicenceCategory), LicenceExpiresOn: hoDate(id.LicenceExpiresOn),
		}
		if id.Purged {
			t := true
			sum.Purged, sum.DocumentExpiresOn, sum.DocumentType = &t, "", api.RentalIdentitySummaryDocumentType("other")
		}
		out.Identity = &sum
	}
	return out
}

func writeHandover(w http.ResponseWriter, v *handover.View) {
	respond.SetETag(w, v.Handover.Version)
	respond.JSON(w, http.StatusOK, hoView(v))
}

func hoDeposit(d *handover.DepositView) api.RentalDeposit {
	out := api.RentalDeposit{
		Status: api.RentalDepositStatus(d.Status), AmountCents: int64(d.Amount), RetainedCents: int64(d.Retained),
		PaymentId: view.String(d.PaymentID), IntentId: view.String(d.IntentID), CheckoutUrl: view.String(d.CheckoutURL),
		ExpiresAt: view.Time(d.ExpiresAt), WaiveReason: view.String(d.WaiveReason), CheckoutQrSvg: qrOf(d.CheckoutURL),
	}
	if d.Mode != "" {
		m := api.RentalDepositMode(d.Mode)
		out.Mode = &m
	}
	if d.Decision != nil {
		c := int64(*d.Decision)
		out.DecisionCents = &c
	}
	return out
}

func hoDamage(d *handover.DamageView) api.RentalDamage {
	return api.RentalDamage{
		Id: d.ID, BookingId: d.BookingID, Description: d.Description, EstimateCents: d.EstimateCents,
		Status: api.RentalDamageStatus(d.Status), RetainedCents: d.RetainedCents, ChargedCents: d.ChargedCents,
		WaiveReason: view.String(d.WaiveReason), Files: hoFiles(d.Files), Version: int64(d.Version), CreatedAt: d.CreatedAt.UTC(),
	}
}

func writeDamage(w http.ResponseWriter, d *handover.DamageView, status int) {
	respond.SetETag(w, d.Version)
	respond.JSON(w, status, hoDamage(d))
}

func hoChargeRefs(cs []reservation.ChargeRef) []api.RentalBookingChargeRef {
	out := make([]api.RentalBookingChargeRef, 0, len(cs))
	for _, c := range cs {
		out = append(out, api.RentalBookingChargeRef{
			ChargeId: c.ChargeID, Kind: api.RentalBookingChargeRefKind(c.Kind), Status: c.Status, TotalCents: int64(c.Total),
			PaidAmountCents: int64(c.Paid), FiscalMode: api.RentalFiscalMode(c.FiscalMode),
		})
	}
	return out
}

func hoExtras(p *[]api.RentalExtraQty) []fleet.ExtraQty {
	if p == nil {
		return nil
	}
	out := make([]fleet.ExtraQty, 0, len(*p))
	for _, e := range *p {
		out = append(out, fleet.ExtraQty{ExtraID: e.ExtraId, Qty: qty.Milli(e.QtyMilli)})
	}
	return out
}

// bookingAfter answers a transition with the booking as GET returns it (view, ETag).
func (h *Handler) bookingAfter(w http.ResponseWriter, r *http.Request, marinaID, bookingID string) {
	h.GetRentalBooking(w, r, marinaID, bookingID)
}

// redirectPDF answers a PDF request without a signed link with a 303 to a link signed for the caller (INV-74).
func (h *Handler) redirectPDF(w http.ResponseWriter, r *http.Request, marinaID, bookingID, phase string) {
	u, _, err := h.RentalHandover.PDFLink(r.Context(), principal(r), marinaID, bookingID, phase)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if !strings.HasPrefix(u, "/api/admin/v1/marinas/") {
		respond.Error(w, r, auth.ErrNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u, http.StatusSeeOther) //nolint:gosec // G710: a same-origin path built by the service from validated ids
}

// ---- operations ----

// GetHandover implements api.ServerInterface (GET .../rental/bookings/{rentalBookingId}/handovers/{phase}).
func (h *Handler) GetHandover(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, phase api.RentalPhase) {
	v, err := h.RentalHandover.GetHandover(r.Context(), principal(r), marinaID, bookingID, string(phase))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeHandover(w, v)
}

// SaveHandoverChecklist implements api.ServerInterface (PUT .../handovers/{phase}/checklist, offline).
func (h *Handler) SaveHandoverChecklist(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, phase api.RentalPhase, _ api.SaveHandoverChecklistParams) {
	var in api.RentalChecklistInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ci := handover.ChecklistInput{ClientRef: in.ClientRef, FuelEighths: in.FuelEighths, Notes: str(in.Notes)}
	if in.DeviceAt != nil {
		ci.DeviceAt = *in.DeviceAt
	}
	for _, it := range in.Items {
		ci.Items = append(ci.Items, handover.ChecklistEntry{Key: it.Key, OK: it.Ok, Note: str(it.Note)})
	}
	if in.EngineHoursMilli != nil {
		m := qty.Milli(*in.EngineHoursMilli)
		ci.EngineHours = &m
	}
	if in.RefuelMilli != nil {
		m := qty.Milli(*in.RefuelMilli)
		ci.Refuel = &m
	}
	v, err := h.RentalHandover.SaveChecklist(r.Context(), principal(r), marinaID, bookingID, string(phase), ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeHandover(w, v)
}

// UploadHandoverPhoto implements api.ServerInterface (POST .../handovers/{phase}/photos, raw body, offline).
func (h *Handler) UploadHandoverPhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, phase api.RentalPhase, params api.UploadHandoverPhotoParams) {
	if _, err := h.RentalHandover.GetHandover(r.Context(), principal(r), marinaID, bookingID, string(phase)); err != nil {
		respond.Error(w, r, err) // another marina is 404 before the query parameters are checked
		return
	}
	if params.Kind == nil {
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "kind", Code: "required"}))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, handover.MaxPhotoBytes+1))
	if err != nil {
		respond.Error(w, r, respond.ErrBadJSON)
		return
	}
	ref := str(params.ClientRef)
	if ref == "" {
		ref = str(params.IdempotencyKey)
	}
	f, err := h.RentalHandover.UploadPhoto(r.Context(), principal(r), marinaID, bookingID, string(phase), handover.PhotoInput{
		Kind: string(*params.Kind), DamageID: str(params.DamageId), ClientRef: ref,
		ContentType: r.Header.Get("Content-Type"), Body: body,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, hoFile(f))
}

// PutHandoverIdentity implements api.ServerInterface (PUT .../rental/bookings/{rentalBookingId}/identity).
func (h *Handler) PutHandoverIdentity(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId) {
	var in api.RentalIdentityInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ii := handover.IdentityInput{
		DocumentType: string(in.DocumentType), DocumentNumber: in.DocumentNumber, DocumentIssuer: str(in.DocumentIssuer),
		LicenceNumber: str(in.LicenceNumber), LicenceCategory: str(in.LicenceCategory),
	}
	if in.DocumentExpiresOn != "" {
		if d := parseDate(v, "document_expires_on", &in.DocumentExpiresOn); d != nil {
			ii.DocumentExpiresOn = *d
		}
	}
	if d := parseDate(v, "licence_expires_on", in.LicenceExpiresOn); d != nil {
		ii.LicenceExpiresOn = *d
	}
	if err := v.Err(); err != nil {
		if _, aerr := h.RentalHandover.GetHandover(r.Context(), principal(r), marinaID, bookingID, "checkout"); aerr != nil {
			respond.Error(w, r, aerr)
			return
		}
		respond.Error(w, r, err)
		return
	}
	out, err := h.RentalHandover.PutIdentity(r.Context(), principal(r), marinaID, bookingID, ii)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeHandover(w, out)
}

// SignRentalContract implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/contract/sign).
func (h *Handler) SignRentalContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, _ api.SignRentalContractParams) {
	var in api.RentalSignInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	si := handover.SignInput{Driver: string(in.Driver), SignerName: str(in.SignerName), Email: str(in.Email), DeviceLabel: str(in.DeviceLabel)}
	if raw := str(in.SignaturePng); raw != "" {
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			si.PNG = []byte{0} // refused by the service after authorisation (validation_failed on signature_png)
		} else {
			si.PNG = b
		}
	}
	out, err := h.RentalHandover.SignContract(r.Context(), principal(r), marinaID, bookingID, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RentalSignature{
		RequestId: out.RequestID, Driver: api.RentalSignatureDriver(out.Driver), Status: out.Status, DocumentId: out.DocumentID,
		SignedDocumentId: view.String(out.SignedDocumentID),
	})
}

// GetRentalContractPdf implements api.ServerInterface (GET .../rental/bookings/{rentalBookingId}/contract/pdf).
func (h *Handler) GetRentalContractPdf(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.GetRentalContractPdfParams) {
	if params.Exp == nil && params.Sig == nil {
		h.redirectPDF(w, r, marinaID, bookingID, handover.PhaseContract)
		return
	}
	exp, sig := signedParams(params.Exp, params.Sig)
	f, err := h.RentalHandover.ContractPDF(r.Context(), principal(r), marinaID, bookingID, exp, sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	serveStoredFile(w, "application/pdf", f.Name, f.Bytes)
}

// GetRentalDeposit implements api.ServerInterface (GET .../rental/bookings/{rentalBookingId}/deposit).
func (h *Handler) GetRentalDeposit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId) {
	d, err := h.RentalHandover.GetDeposit(r.Context(), principal(r), marinaID, bookingID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, hoDeposit(d))
}

// StartRentalDeposit implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/deposit).
func (h *Handler) StartRentalDeposit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, _ api.StartRentalDepositParams) {
	var in api.RentalDepositInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	di := handover.DepositInput{Mode: string(in.Mode), Reason: str(in.Reason), RegisterID: str(in.RegisterId), ProviderCode: str(in.ProviderCode)}
	if in.AmountCents != nil {
		di.Amount = money.Cents(*in.AmountCents)
	}
	d, err := h.RentalHandover.StartDeposit(r.Context(), principal(r), marinaID, bookingID, di)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, hoDeposit(d))
}

// CheckOutRental implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/check-out).
func (h *Handler) CheckOutRental(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.CheckOutRentalParams) {
	var in *api.RentalCheckOutInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	co := handover.CheckOutInput{}
	if in != nil {
		co.Note = str(in.Note)
		if in.ActualAt != nil {
			co.ActualAt = *in.ActualAt
		}
	}
	if _, err := h.RentalHandover.CheckOut(r.Context(), principal(r), marinaID, bookingID, version, co); err != nil {
		respond.Error(w, r, err)
		return
	}
	h.bookingAfter(w, r, marinaID, bookingID)
}

// CreateRentalDamage implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/damages).
func (h *Handler) CreateRentalDamage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, _ api.CreateRentalDamageParams) {
	var in api.RentalDamageInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d, err := h.RentalHandover.CreateDamage(r.Context(), principal(r), marinaID, bookingID, handover.DamageInput{
		Description: in.Description, Estimate: money.Cents(in.EstimateCents),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDamage(w, d, http.StatusCreated)
}

// UpdateRentalDamage implements api.ServerInterface (PATCH .../rental/damages/{rentalDamageId}).
func (h *Handler) UpdateRentalDamage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, damageID api.RentalDamageId, params api.UpdateRentalDamageParams) {
	var in api.RentalDamagePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	dp := handover.DamagePatch{Description: in.Description, Waived: in.Waived, WaiveReason: str(in.WaiveReason)}
	if in.EstimateCents != nil {
		c := money.Cents(*in.EstimateCents)
		dp.Estimate = &c
	}
	d, err := h.RentalHandover.UpdateDamage(r.Context(), principal(r), marinaID, damageID, version, dp)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDamage(w, d, http.StatusOK)
}

// DeleteRentalDamage implements api.ServerInterface (DELETE .../rental/damages/{rentalDamageId}).
func (h *Handler) DeleteRentalDamage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, damageID api.RentalDamageId, params api.DeleteRentalDamageParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.RentalHandover.DeleteDamage(r.Context(), principal(r), marinaID, damageID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

func checkInIn(in api.RentalCheckInInput) handover.CheckInInput {
	ci := handover.CheckInInput{
		ReturnedAt: in.ReturnedAt, Extras: hoExtras(in.Extras), DepositDecision: string(in.DepositDecision),
		WaiveReason: str(in.WaiveReason), Note: str(in.Note),
	}
	if in.RetainCents != nil {
		ci.Retain = money.Cents(*in.RetainCents)
	}
	if in.WaiveDamageIds != nil {
		ci.WaiveDamageIDs = *in.WaiveDamageIds
	}
	return ci
}

// PreviewRentalCheckIn implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/check-in/preview).
func (h *Handler) PreviewRentalCheckIn(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId) {
	var in api.RentalCheckInInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p, err := h.RentalHandover.PreviewCheckIn(r.Context(), principal(r), marinaID, bookingID, checkInIn(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RentalCheckInPreview{
		LateMinutes: p.LateMinutes, LateFeeCents: int64(p.LateFee), FuelLitresMilli: int64(p.FuelLitres), FuelCents: int64(p.Fuel),
		ExtrasCents: int64(p.Extras), DamageEstimateCents: int64(p.DamageEstimate), DamageExcessCents: int64(p.DamageExcess),
		ReturnTotalCents: int64(p.ReturnTotal), DepositCents: int64(p.Deposit), DepositProposalCents: int64(p.DepositProposal),
		Lines: make([]api.RentalPriceLine, 0, len(p.Lines)),
	}
	for i, l := range p.Lines {
		out.Lines = append(out.Lines, api.RentalPriceLine{
			Seq: i + 1, Kind: api.RentalPriceLineKind(l.Kind), ItemId: l.ItemID, ItemCode: l.ItemCode, QtyMilli: int64(l.Qty),
			UnitCents: int64(l.UnitCents), AmountCents: int64(l.Amount), PriceIncludesVat: true,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

// CheckInRental implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/check-in).
func (h *Handler) CheckInRental(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.CheckInRentalParams) {
	var in api.RentalCheckInInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if _, err := h.RentalHandover.CheckIn(r.Context(), principal(r), marinaID, bookingID, version, checkInIn(in)); err != nil {
		respond.Error(w, r, err)
		return
	}
	h.bookingAfter(w, r, marinaID, bookingID)
}

// CloseRental implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/close).
func (h *Handler) CloseRental(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, params api.CloseRentalParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if _, err := h.RentalHandover.Close(r.Context(), principal(r), marinaID, bookingID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	h.bookingAfter(w, r, marinaID, bookingID)
}

// GetRentalHandoverPdf implements api.ServerInterface (GET .../handovers/{phase}/pdf).
func (h *Handler) GetRentalHandoverPdf(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, phase api.RentalPhase, params api.GetRentalHandoverPdfParams) {
	if params.Exp == nil && params.Sig == nil {
		h.redirectPDF(w, r, marinaID, bookingID, string(phase))
		return
	}
	exp, sig := signedParams(params.Exp, params.Sig)
	f, err := h.RentalHandover.HandoverPDF(r.Context(), principal(r), marinaID, bookingID, string(phase), exp, sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	serveStoredFile(w, "application/pdf", f.Name, f.Bytes)
}

// QuickCashRental implements api.ServerInterface (POST .../rental/bookings/{rentalBookingId}/quick-cash).
func (h *Handler) QuickCashRental(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, bookingID api.RentalBookingId, _ api.QuickCashRentalParams) {
	var in api.RentalQuickCashInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	qi := handover.QuickCashInput{Method: string(in.Method), RegisterID: str(in.RegisterId)}
	if in.Target != nil {
		qi.Target = string(*in.Target)
	}
	res, err := h.RentalHandover.QuickCash(r.Context(), principal(r), marinaID, bookingID, qi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RentalQuickCashResult{
		Charges: hoChargeRefs(res.Charges), FiscalDocumentIds: res.FiscalDocumentIDs, PaymentId: view.String(res.PaymentID),
		ReceiptUrl: view.String(res.ReceiptURL), LinkUrl: view.String(res.LinkURL), LinkExpiresAt: view.Time(res.LinkExpiresAt),
		LinkQrSvg: qrOf(res.LinkURL),
	}
	if out.FiscalDocumentIds == nil {
		out.FiscalDocumentIds = []string{}
	}
	respond.JSON(w, http.StatusOK, out)
}

// qrOf renders url as a QR SVG (internal/qrsvg: one path, no text or scripts) for the renter to scan with their own
// phone (m11-plan §7, §9.3); nil when url is empty or cannot be encoded.
func qrOf(url string) *string {
	if url == "" {
		return nil
	}
	svg, err := qrsvg.Render(url)
	if err != nil {
		return nil
	}
	return &svg
}
