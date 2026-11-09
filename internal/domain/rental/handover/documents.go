package handover

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Rental documents (m11-plan §10, D14): the rental contract (composed at confirmation, or here on the first
// signature when missing; signed at check-out on screen, by OTP or on paper) and one handover report per phase
// (composed after the commit of the phase, rendered by the docs job, never on the critical path). PDFs are served
// through 5-minute links signed for the caller (INV-74).

const (
	// PDFLinkTTL is the validity of a rental PDF link.
	PDFLinkTTL = 5 * time.Minute
	pdfKeyName = "rental.pdf_url"
	// PhaseContract names the contract in PDFLink.
	PhaseContract = "contract"
	signTTL       = 7 * 24 * time.Hour
)

// ensureContract returns the live contract document of a booking, composing it when missing.
func (s *Service) ensureContract(ctx context.Context, scope store.MarinaScope, p *auth.Principal, bookingID string) (*store.GeneratedDocument, error) {
	doc, err := store.RentalDocument(ctx, s.d.DB.DB, scope, string(docs.KindRentalContract), bookingID)
	if errors.Is(err, store.ErrNotFound) {
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if _, err := s.lock(ctx, tx, scope, bookingID, 0); err != nil {
				return err
			}
			if _, err := store.RentalDocument(ctx, tx, scope, string(docs.KindRentalContract), bookingID); err == nil {
				return nil
			}
			_, err := s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
				Kind: docs.KindRentalContract, SubjectType: docs.SubjectRentalBooking, SubjectID: bookingID, PDFA: true, CreatedBy: p.UserID,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		doc, err = store.RentalDocument(ctx, s.d.DB.DB, scope, string(docs.KindRentalContract), bookingID)
	}
	if err != nil {
		return nil, err
	}
	if doc.Status != store.DocReady {
		if err := s.d.Docs.RenderNow(ctx, doc.ID); err != nil {
			return nil, err
		}
		if doc, err = store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, doc.ID); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

func signatureView(r *store.SignatureRequest) *SignatureView {
	return &SignatureView{RequestID: r.ID, Driver: r.Driver, Status: r.Status, DocumentID: r.DocumentID, SignedDocumentID: r.SignedDocumentID}
}

// SignContract signs the rental contract on screen, or opens an OTP or paper signature.
func (s *Service) SignContract(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in SignInput) (*SignatureView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	in.SignerName, in.DeviceLabel = strings.TrimSpace(in.SignerName), strings.TrimSpace(in.DeviceLabel)
	switch in.Driver {
	case SignScreen:
		if len(in.PNG) == 0 || len(in.PNG) > signature.MaxScreenSignatureBytes {
			return nil, invalid("signature_png", "invalid")
		}
	case SignOTP, SignPaper:
	default:
		return nil, invalid("driver", "invalid")
	}
	if utf8.RuneCountInString(in.SignerName) > 120 || utf8.RuneCountInString(in.DeviceLabel) > 80 {
		return nil, invalid("signer_name", "too_long")
	}
	b, err := store.GetRentalBooking(ctx, s.d.DB.DB, scope, bookingID)
	if err != nil {
		return nil, notFound(err)
	}
	if b.Status != store.RentalStatusConfirmed {
		return nil, ErrInvalidTransition
	}
	if r, err := store.RentalContractSignature(ctx, s.d.DB.DB, scope, bookingID); err == nil && r.Status == signature.StatusSigned {
		return signatureView(r), nil // already signed: one effect
	}
	if in.Driver == SignOTP && b.CustomerID == "" {
		return nil, ErrCustomerRequired
	}
	doc, err := s.ensureContract(ctx, scope, p, bookingID)
	if err != nil {
		return nil, err
	}
	actor := p.Actor()
	var reqID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := s.lock(ctx, tx, scope, bookingID, 0, store.RentalStatusConfirmed)
		if err != nil {
			return err
		}
		if r, err := store.RentalContractSignature(ctx, tx, scope, bookingID); err == nil && r.Status == signature.StatusSigned {
			reqID = r.ID
			return nil
		}
		switch in.Driver {
		case SignScreen:
			name := in.SignerName
			if name == "" {
				name = s.renterName(ctx, tx, scope, b)
			}
			res, err := s.d.Signatures.SignOnScreenTx(ctx, tx, scope, actor, signature.ScreenSignInput{
				SubjectType: store.SignatureSubjectRental, SubjectID: b.ID, DocumentID: doc.ID, SignerName: name, PNG: in.PNG,
				DeviceLabel: in.DeviceLabel, At: s.now(),
			})
			if err != nil {
				if errors.Is(err, signature.ErrScreenImageInvalid) {
					return invalid("signature_png", "invalid")
				}
				return err
			}
			reqID = res.RequestID
		default:
			if err := s.d.Signatures.CancelTx(ctx, tx, scope, store.SignatureSubjectRental, b.ID); err != nil {
				return err
			}
			id, err := s.d.Signatures.OpenTx(ctx, tx, scope, signature.OpenRequest{
				SubjectType: store.SignatureSubjectRental, SubjectID: b.ID, DocumentID: doc.ID, Driver: in.Driver,
				Signers: []signature.SignerSpec{{Role: "customer", CustomerID: b.CustomerID}}, ExpiresAt: s.now().Add(signTTL),
				CreatedBy: p.UserID,
			})
			if err != nil {
				return err
			}
			reqID = id
		}
		return s.audit(ctx, tx, scope, actor, "rental_booking.contract_sign", "rental_booking", b.ID, map[string]any{
			"driver": in.Driver, "signature_request_id": reqID, "document_id": doc.ID,
		})
	})
	if err != nil {
		return nil, err
	}
	r, err := store.GetSignatureRequest(ctx, s.d.DB.DB, scope, reqID)
	if err != nil {
		return nil, err
	}
	return signatureView(r), nil
}

func (s *Service) pdfSignature(marinaID, bookingID, phase, principalID string, exp int64) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("handover: key ring not wired")
	}
	key, err := s.d.Keys.Derive(pdfKeyName, 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(marinaID + "|" + bookingID + "|" + phase + "|" + principalID + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// PDFLink returns a 5-minute link signed for the caller to the contract (phase "contract") or a handover PDF.
func (s *Service) PDFLink(ctx context.Context, p *auth.Principal, marinaID, bookingID, phase string) (string, time.Time, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return "", time.Time{}, err
	}
	if phase != PhaseContract && !validPhase(phase) {
		return "", time.Time{}, auth.ErrNotFound
	}
	exp := s.d.Clock.Now().Add(PDFLinkTTL).UTC().Truncate(time.Second)
	sig, err := s.pdfSignature(scope.ID(), bookingID, phase, p.ID(), exp.Unix())
	if err != nil {
		return "", time.Time{}, err
	}
	base := "/api/admin/v1/marinas/" + scope.ID() + "/rental/bookings/" + bookingID
	path := base + "/contract/pdf"
	if phase != PhaseContract {
		path = base + "/handovers/" + phase + "/pdf"
	}
	return path + "?exp=" + strconv.FormatInt(exp.Unix(), 10) + "&sig=" + sig, exp, nil
}

func (s *Service) checkPDFLink(scope store.MarinaScope, p *auth.Principal, bookingID, phase string, exp int64, sig string) error {
	want, err := s.pdfSignature(scope.ID(), bookingID, phase, p.ID(), exp)
	if err != nil {
		return err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return document.ErrLinkExpired
	}
	return nil
}

func (s *Service) servePDF(ctx context.Context, scope store.MarinaScope, doc *store.GeneratedDocument, name string) (*PDF, error) {
	if doc.Status == store.DocPending || doc.Status == store.DocFailed || doc.Status == "rendering" {
		if err := s.d.Docs.RenderNow(ctx, doc.ID); err != nil {
			return nil, err
		}
	}
	b, _, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, scope, doc.ID)
	if err != nil {
		return nil, err
	}
	return &PDF{Name: name, Bytes: b}, nil
}

// ContractPDF returns the rental contract PDF (signed when available) through a signed link (exp, sig).
func (s *Service) ContractPDF(ctx context.Context, p *auth.Principal, marinaID, bookingID string, exp int64, sig string) (*PDF, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if err := s.checkPDFLink(scope, p, bookingID, PhaseContract, exp, sig); err != nil {
		return nil, err
	}
	if r, err := store.RentalContractSignature(ctx, s.d.DB.DB, scope, bookingID); err == nil && r.SignedDocumentID != "" {
		doc, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, r.SignedDocumentID)
		if err == nil {
			return s.servePDF(ctx, scope, doc, "contratto-noleggio.pdf")
		}
	}
	doc, err := s.ensureContract(ctx, scope, p, bookingID)
	if err != nil {
		return nil, err
	}
	return s.servePDF(ctx, scope, doc, "contratto-noleggio.pdf")
}

// HandoverPDF returns the handover PDF of a phase through a signed link (exp, sig).
func (s *Service) HandoverPDF(ctx context.Context, p *auth.Principal, marinaID, bookingID, phase string, exp int64, sig string) (*PDF, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	if !validPhase(phase) {
		return nil, auth.ErrNotFound
	}
	if err := s.checkPDFLink(scope, p, bookingID, phase, exp, sig); err != nil {
		return nil, err
	}
	h, err := store.GetRentalHandover(ctx, s.d.DB.DB, scope, bookingID, phase)
	if err != nil || h.CompletedAt.IsZero() {
		return nil, auth.ErrNotFound
	}
	doc, err := store.RentalDocument(ctx, s.d.DB.DB, scope, string(docs.KindRentalHandover), h.ID)
	if err != nil {
		return nil, notFound(err)
	}
	return s.servePDF(ctx, scope, doc, "verbale-"+phase+".pdf")
}

// ---- data source of the rental documents ----

type dataSource struct{ s *Service }

func (dataSource) Kinds() []docs.Kind {
	return []docs.Kind{docs.KindRentalContract, docs.KindRentalHandover}
}

func (dataSource) DemoView(kind docs.Kind, _ string) (any, error) {
	v := docs.DemoView(kind)
	if v == nil {
		return nil, fmt.Errorf("handover: no demo for %s", kind)
	}
	return v, nil
}

func (ds dataSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind docs.Kind, _ docs.SubjectType, subjectID, locale string) (any, error) {
	s := ds.s
	switch kind {
	case docs.KindRentalContract:
		b, err := store.GetRentalBooking(ctx, tx, scope, subjectID)
		if err != nil {
			return nil, err
		}
		return s.contractView(ctx, tx, scope, b, locale)
	case docs.KindRentalHandover:
		h, err := store.RentalHandoverByID(ctx, tx, scope, subjectID)
		if err != nil {
			return nil, err
		}
		b, err := store.GetRentalBooking(ctx, tx, scope, h.BookingID)
		if err != nil {
			return nil, err
		}
		return s.handoverView(ctx, tx, scope, b, h, locale)
	}
	return nil, fmt.Errorf("handover: kind %s", kind)
}

func localTime(t time.Time, loc *time.Location) string { return t.In(loc).Format("02/01/2006 15:04") }

func i18n(raw store.JSON, locale string) string {
	m := map[string]string{}
	_ = json.Unmarshal(raw, &m)
	if v := m[locale]; v != "" {
		return v
	}
	return m["it"]
}

func (s *Service) rentalInfo(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, locale string) (docs.RentalInfo, views.Vessel, views.Party, error) {
	loc := s.location(ctx, db, scope)
	unit, err := store.GetRentalUnit(ctx, db, scope, b.UnitID)
	if err != nil {
		return docs.RentalInfo{}, views.Vessel{}, views.Party{}, err
	}
	formula, err := store.GetRentalFormula(ctx, db, scope, b.FormulaID)
	if err != nil {
		return docs.RentalInfo{}, views.Vessel{}, views.Party{}, err
	}
	snap := snapshotOf(b)
	info := docs.RentalInfo{
		Number: fmt.Sprintf("NL%04d-%06d", b.Year, b.Number), UnitCode: unit.Code, Category: unit.Category, Seats: unit.Seats,
		Formula: i18n(formula.Name, locale), Kind: formula.Kind,
		Start: localTime(slots.Instant(b.StartsOn, b.StartSlot, loc), loc), End: localTime(slots.Instant(b.EndsOn, b.EndSlot, loc), loc),
		LicenceRequired: snap.LicenceRequired, FuelMetered: formula.Fuel == store.RentalFuelMetered,
		GraceMinutes: formula.GraceMinutes, LateStepMinutes: formula.LateStepMinutes, QR: "PR1:" + b.ID, Notes: views.Notes(b.Note),
	}
	if info.Formula == "" {
		info.Formula = formula.Code
	}
	if b.SkipperID != "" {
		if sk, err := store.GetRentalSkipper(ctx, db, scope, b.SkipperID); err == nil {
			info.Skipper = sk.Name
		}
	}
	var vessel views.Vessel
	if v, err := store.GetVessel(ctx, db, scope, unit.VesselID); err == nil {
		vessel = views.Vessel{Name: v.Name, Registration: v.Registration, Flag: v.Flag, HullType: v.HullType, LOA: views.Cm(v.LOACm), Beam: views.Cm(v.BeamCm)}
	}
	var party views.Party
	if b.CustomerID != "" {
		if c, err := store.GetCustomer(ctx, db, scope, b.CustomerID); err == nil {
			party = docs.PartyOf(c)
		}
	} else if r := s.renter(b); r != nil {
		party = views.Party{Kind: "person", DisplayName: r.Name, Phone: r.Phone, Email: r.Email}
	}
	return info, vessel, party, nil
}

func (s *Service) contractView(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, locale string) (any, error) {
	mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	info, vessel, party, err := s.rentalInfo(ctx, db, scope, b, locale)
	if err != nil {
		return nil, err
	}
	snap := snapshotOf(b)
	v := docs.RentalContractView{
		Marina: mv, Renter: party, Vessel: vessel, Rental: info, Lines: []docs.RentalLine{}, Total: snap.Total(),
		DepositMode: b.DepositMode,
	}
	ids := make([]string, 0, len(snap.Breakdown.Lines))
	for _, l := range snap.Breakdown.Lines {
		ids = append(ids, l.ItemID)
	}
	items, err := store.ItemsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, it := range items {
		names[it.ID] = i18n(it.Name, locale)
		if names[it.ID] == "" {
			names[it.ID] = it.Code
		}
	}
	for i, l := range snap.Breakdown.Lines {
		v.Lines = append(v.Lines, docs.RentalLine{Seq: i + 1, Description: names[l.ItemID], Qty: qtyText(l.Qty, locale), Amount: l.Amount})
	}
	v.Deposit = money.Cents(b.DepositCents)
	if v.Deposit == 0 && b.DepositMode != store.RentalDepositWaived {
		if u, err := store.GetRentalUnit(ctx, db, scope, b.UnitID); err == nil {
			v.Deposit = money.Cents(u.DepositCents)
		}
	}
	return v, nil
}

func qtyText(q qty.Milli, locale string) string {
	sep := byte(',')
	if locale == "en" {
		sep = '.'
	}
	s := q.Format(3, sep)
	s = strings.TrimRight(strings.TrimRight(s, "0"), string(sep))
	return s
}

func (s *Service) handoverView(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.RentalBooking, h *store.RentalHandover, locale string) (any, error) {
	mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	info, vessel, party, err := s.rentalInfo(ctx, db, scope, b, locale)
	if err != nil {
		return nil, err
	}
	loc := s.location(ctx, db, scope)
	phase := map[string][2]string{"it": {"consegna", "riconsegna"}, "en": {"check-out", "check-in"}}
	lp := phase["it"]
	if locale == "en" {
		lp = phase["en"]
	}
	label := lp[0]
	if h.Phase == store.RentalPhaseCheckin {
		label = lp[1]
	}
	files, err := store.RentalHandoverFilesOf(ctx, db, scope, []string{h.ID})
	if err != nil {
		return nil, err
	}
	photos := 0
	for _, f := range files {
		if f.Kind == store.RentalFileBoat || f.Kind == store.RentalFileDamage {
			photos++
		}
	}
	pi := docs.RentalPhaseInfo{Phase: label, Photos: photos, Notes: h.Notes}
	if !h.ActualAt.IsZero() {
		pi.At = localTime(h.ActualAt, loc)
	}
	if h.FuelEighths != nil {
		pi.FuelEighths = *h.FuelEighths
	}
	if h.EngineHoursMilli != nil {
		pi.EngineHours = qtyText(qty.Milli(*h.EngineHoursMilli), locale)
	}
	if h.CompletedByUserID != "" {
		if u, err := store.MarinaMemberUser(ctx, db, scope, h.CompletedByUserID); err == nil {
			pi.Operator = u.Name
		}
	}
	v := docs.RentalHandoverView{
		Marina: mv, Renter: party, Vessel: vessel, Rental: info, Handover: pi, Checklist: []docs.RentalCheckItem{},
		Damages: []docs.RentalDamageRow{}, Deposit: money.Cents(b.DepositCents), DepositMode: b.DepositMode,
	}
	set, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var list []store.RentalChecklistItem
	_ = json.Unmarshal(set.Checklist, &list)
	labels := map[string]string{}
	for _, it := range list {
		labels[it.Key] = it.Label[locale]
		if labels[it.Key] == "" {
			labels[it.Key] = it.Label["it"]
		}
	}
	var items []ChecklistEntry
	_ = json.Unmarshal(h.Checklist, &items)
	for _, it := range items {
		l := labels[it.Key]
		if l == "" {
			l = it.Key
		}
		v.Checklist = append(v.Checklist, docs.RentalCheckItem{Label: l, OK: it.OK, Note: it.Note})
	}
	if h.Phase == store.RentalPhaseCheckin {
		dms, err := store.RentalDamagesOf(ctx, db, scope, b.ID)
		if err != nil {
			return nil, err
		}
		for _, d := range dms {
			v.Damages = append(v.Damages, docs.RentalDamageRow{Description: d.Description, Estimate: money.Cents(d.EstimateCents), Status: d.Status})
		}
		rows, err := store.RentalChargesOf(ctx, db, scope, b.ID)
		if err != nil {
			return nil, err
		}
		if ret := liveCharge(rows, store.RentalChargeReturn); ret != nil {
			v.ReturnTotal = money.Cents(ret.TotalCents)
			lines, err := store.CollectChargeLines(ctx, db, scope, ret.ID)
			if err == nil {
				formula, ferr := store.GetRentalFormula(ctx, db, scope, b.FormulaID)
				for _, l := range lines {
					if ferr == nil && l.ItemID == formula.LateItemID {
						v.LateFee += money.Cents(l.GrossCents)
					}
					if ferr == nil && l.ItemID == formula.FuelItemID {
						v.Fuel += money.Cents(l.GrossCents)
					}
				}
			}
		}
		if !b.ReturnedAt.IsZero() {
			late := int(b.ReturnedAt.Sub(slots.Instant(b.EndsOn, b.EndSlot, loc)) / time.Minute)
			v.LateMinutes = max(late, 0)
		}
		v.DepositRetained = money.Cents(b.DepositRetainedCents)
		if b.DepositMode == store.RentalDepositCash || b.DepositMode == store.RentalDepositCardPOS {
			v.DepositReleased = money.Cents(b.DepositCents - b.DepositRetainedCents)
		}
	}
	return v, nil
}
