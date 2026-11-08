package booking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Booking document types (m8-plan §5.2) and the owner of each: vessel papers go on the vessel, personal papers on
// the customer.
var (
	documentTypes       = []string{"boating_licence", "navigation_licence", "insurance_policy", "registration_certificate", "identity_card"}
	vesselDocumentTypes = []string{"navigation_licence", "insurance_policy", "registration_certificate"}
)

// MaxDocumentBytes bounds an uploaded document (10 MiB, m8-plan §5.2).
const MaxDocumentBytes = 10 << 20

// contactAAD is the KeyRing AAD of the encrypted contact of a booking.
func contactAAD(id string) string { return "booking-contact:" + id }

func onlineTier(t contract.OnlineTier) onlineconf.Tier {
	return onlineconf.Tier{MinDaysBefore: t.MinDaysBefore, RefundBP: t.RefundBP}
}

func cleanName(s string, n int) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	return s, s != "" && utf8.RuneCountInString(s) <= n
}

// checkDetails validates the details step and normalises the contact.
func checkDetails(set onlineconf.Settings, in *DetailsInput) error {
	v := &auth.ValidationError{}
	var ok bool
	if in.Customer.FirstName, ok = cleanName(in.Customer.FirstName, 100); !ok {
		v.Add("customer.first_name", "required")
	}
	if in.Customer.LastName, ok = cleanName(in.Customer.LastName, 100); !ok {
		v.Add("customer.last_name", "required")
	}
	if in.Customer.Email, ok = customer.NormalizeEmail(in.Customer.Email); !ok {
		v.Add("customer.email", "invalid")
	}
	switch {
	case strings.TrimSpace(in.Customer.Phone) != "":
		if in.Customer.Phone, ok = customer.NormalizePhone(in.Customer.Phone); !ok {
			v.Add("customer.phone", "invalid")
		}
	case set.RequiredFields.Phone:
		v.Add("customer.phone", "required")
	}
	c := strings.ToUpper(strings.TrimSpace(in.Customer.Country))
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
		v.Add("customer.country", "invalid")
	}
	in.Customer.Country = c
	if in.Customer.Locale != "en" {
		in.Customer.Locale = "it"
	}
	if in.Vessel.Name, ok = cleanName(in.Vessel.Name, 120); !ok {
		v.Add("vessel.name", "required")
	}
	switch {
	case in.PeopleOnBoard == 0 && set.RequiredFields.PeopleOnBoard:
		v.Add("people_on_board", "required")
	case in.PeopleOnBoard < 0 || in.PeopleOnBoard > 500:
		v.Add("people_on_board", "out_of_range")
	}
	if t := in.ExpectedArrivalTime; t != "" {
		m := policy.ParseHHMM(t)
		from, to := policy.ParseHHMM(set.ArrivalFrom), policy.ParseHHMM(set.ArrivalTo)
		if (m == 0 && t != "00:00") || (to > 0 && (m < from || m > to)) {
			v.Add("expected_arrival_time", "invalid")
		}
	}
	return v.Err()
}

// consentTexts checks the accepted versions against the current published texts (m8-plan §5.1). Terms and privacy
// are always required: a marina that has not published both cannot take bookings (booking_rules consents
// texts_unpublished, and Config reports payable false), so no booking is ever stored without its GDPR consents.
// A version that is not the current one is ErrTextOutdated.
func consentTexts(current []onlineconf.Text, in Consents) ([]onlineconf.Text, error) {
	published := func(kind string) bool {
		return slices.ContainsFunc(current, func(t onlineconf.Text) bool { return t.Kind == kind })
	}
	if !requiredTextsPublished(current) {
		return nil, &RulesError{Field: "consents", Code: CodeTextsUnpublished}
	}
	var out []onlineconf.Text
	for _, k := range []struct {
		kind    string
		version int
		need    bool
	}{
		{store.BookingTextTerms, in.TermsVersion, true},
		{store.BookingTextPrivacy, in.PrivacyVersion, true},
		{store.BookingTextMarketing, in.MarketingVersion, in.Marketing},
	} {
		if !k.need {
			continue
		}
		if !published(k.kind) {
			if k.version != 0 {
				return nil, ErrTextOutdated
			}
			continue
		}
		if k.version <= 0 {
			if k.kind == store.BookingTextMarketing {
				return nil, &RulesError{Field: "consents.marketing_version", Code: "missing"}
			}
			return nil, ErrConsentRequired
		}
		t, err := onlineconf.CheckConsentVersion(current, k.kind, k.version)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, nil
}

// CodeTextsUnpublished is the booking_rules code of a marina that has not published its terms and privacy texts.
const CodeTextsUnpublished = "texts_unpublished"

// requiredTextsPublished reports whether both terms and privacy are published (required consents, spec 30).
func requiredTextsPublished(current []onlineconf.Text) bool {
	for _, kind := range []string{store.BookingTextTerms, store.BookingTextPrivacy} {
		if !slices.ContainsFunc(current, func(t onlineconf.Text) bool { return t.Kind == kind }) {
			return false
		}
	}
	return true
}

// SubmitDetails stores the customer and vessel details and the consents of a held booking (m8-plan §5.1).
func (s *Service) SubmitDetails(ctx context.Context, slug, bookingID string, t Tokens, in DetailsInput) (*Booking, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ob, c, err := s.loadBooking(ctx, db, mc, bookingID, t, false)
	if err != nil {
		return nil, err
	}
	if c.Status != string(contract.StatusHeld) {
		return nil, &contract.TransitionError{From: c.Status, Action: contract.ActionUpdate}
	}
	if err := checkDetails(mc.set, &in); err != nil {
		return nil, err
	}
	current, err := onlineconf.CurrentTexts(ctx, db, mc.scope)
	if err != nil {
		return nil, err
	}
	texts, err := consentTexts(current, in.Consents)
	if err != nil {
		return nil, err
	}
	contact, err := json.Marshal(Contact{
		FirstName: in.Customer.FirstName, LastName: in.Customer.LastName, Email: in.Customer.Email, Phone: in.Customer.Phone,
		Country: in.Customer.Country, Locale: in.Customer.Locale,
	})
	if err != nil {
		return nil, err
	}
	enc, err := s.d.Keys.EncryptString(string(contact), contactAAD(ob.ID))
	if err != nil {
		return nil, err
	}
	who := contract.PublicActor()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		customerID, err := s.resolveCustomerTx(ctx, tx, mc.scope, who.Actor, in.Customer)
		if err != nil {
			return err
		}
		if _, err := s.d.Contracts.SetOnlineCustomerTx(ctx, tx, mc.scope, who, c.ID, contract.OnlineCustomer{
			CustomerID: customerID, VesselName: in.Vessel.Name, Registration: in.Vessel.Registration, Flag: in.Vessel.Flag,
			HullType: in.Vessel.HullType, DraftCm: int64(in.Vessel.DraftCm), ExpectedArrivalTime: in.ExpectedArrivalTime,
			Notes: in.Notes,
		}); err != nil {
			return err
		}
		ob, err := store.GetOnlineBooking(ctx, tx, mc.scope, ob.ID)
		if err != nil {
			return err
		}
		ob.ContactEnc, ob.PeopleOnBoard, ob.Locale = enc, in.PeopleOnBoard, in.Customer.Locale
		if err := store.UpdateOnlineBooking(ctx, tx, mc.scope, ob, s.now()); err != nil {
			return err
		}
		return s.recordConsentsTx(ctx, tx, mc.scope, customerID, texts, in.Consents.Marketing)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, slug, bookingID, t)
}

// resolveCustomerTx reuses the only live customer of the marina with the same normalised email (never modified,
// m8-plan §16 D10) or creates a new person customer, in tx (audit customer.create, customer.created).
func (s *Service) resolveCustomerTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in CustomerDetails) (string, error) {
	cands, err := store.CustomerDuplicateCandidates(ctx, tx, scope, "", "", "", []string{in.Email})
	if err != nil {
		return "", err
	}
	var match []string
	for _, c := range cands {
		var contacts []customer.Contact
		_ = json.Unmarshal(c.Emails, &contacts)
		for _, e := range contacts {
			if n, ok := customer.NormalizeEmail(e.Value); ok && n == in.Email {
				match = append(match, c.ID)
				break
			}
		}
	}
	if len(match) == 1 {
		return match[0], nil
	}
	now := s.now()
	row := &store.Customer{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: customer.KindPerson, FirstName: in.FirstName, LastName: in.LastName,
		Nationality: in.Country, Locale: in.Locale, Emails: store.MustJSON([]customer.Contact{{Label: "", Value: in.Email}}),
		Phones: store.JSON("[]"), BillingAddress: store.JSON("{}"), PaymentTerms: store.JSON("{}"), Tags: store.Strings{},
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if in.Phone != "" {
		row.Phones = store.MustJSON([]customer.Contact{{Label: "", Value: in.Phone}})
	}
	row.SearchText = customer.SearchText(row)
	if err := store.InsertCustomer(ctx, tx, row); err != nil {
		return "", err
	}
	return row.ID, s.customerCreatedTx(ctx, tx, scope, contract.Who{Actor: actor}, row.ID)
}

// customerCreatedTx writes the audit row (field names only) and customer.created of a customer created online.
func (s *Service) customerCreatedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who contract.Who, id string) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: who.Actor, Action: "customer.create", EntityType: "customer",
		EntityID: id, Diff: map[string]any{"fields": []string{"first_name", "last_name", "emails", "phones", "nationality", "locale"}, "origin": "online"},
	}); err != nil {
		return err
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerCreated, event.CustomerData{CustomerID: id})
	return err
}

// recordConsentsTx inserts one consent row per accepted text (source online, with the text hash).
func (s *Service) recordConsentsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID string, texts []onlineconf.Text, marketing bool) error {
	now := s.now()
	for _, t := range texts {
		if t.Kind == store.BookingTextMarketing && !marketing {
			continue
		}
		if err := store.InsertConsent(ctx, tx, &store.Consent{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: customerID, Kind: t.Kind, PolicyVersion: strconv.Itoa(t.Version),
			Granted: true, Source: "online", RecordedAt: now, TextSHA256: t.SHA256, CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// bookingDocuments returns the booking documents and the required ones still missing. With onlyBooking set (the
// token-only surfaces: public booking view and /pay) it lists only the documents uploaded through that booking, never
// the older documents of a customer the booking was matched to by email (spec 23.3, INV-88); otherwise (customer
// area of the authenticated customer, desk) every document of the transit's customer and vessel.
func (s *Service) bookingDocuments(ctx context.Context, db bun.IDB, mc *marinaCtx, c *store.Contract, onlyBooking string) ([]Document, []string, error) {
	var rows []store.Document
	if onlyBooking != "" {
		bd, err := store.DocumentsOfOnlineBooking(ctx, db, mc.scope, onlyBooking)
		if err != nil {
			return nil, nil, err
		}
		rows = bd
	}
	if onlyBooking == "" && c.VesselID != "" {
		vd, err := store.DocumentsOfVessel(ctx, db, mc.scope, c.VesselID)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, vd...)
	}
	if onlyBooking == "" && c.CustomerID != "" {
		cd, err := store.DocumentsOfCustomer(ctx, db, mc.scope, c.CustomerID)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, cd...)
	}
	docs := []Document{}
	have := map[string]bool{}
	for _, d := range rows {
		if !slices.Contains(documentTypes, d.Type) {
			continue
		}
		have[d.Type] = true
		docs = append(docs, Document{ID: d.ID, Type: d.Type, UploadedAt: d.CreatedAt, Verified: !d.VerifiedAt.IsZero()})
	}
	missing := []string{}
	for _, t := range mc.set.RequiredFields.Documents {
		if !have[t] {
			missing = append(missing, t)
		}
	}
	return docs, missing, nil
}

// attachDocument stores an uploaded document on the transit's vessel or customer (document service: sniffing,
// encryption at rest, INV-74).
func (s *Service) attachDocument(ctx context.Context, scope store.MarinaScope, c *store.Contract, up DocumentUpload) (*Document, error) {
	if !slices.Contains(documentTypes, up.Type) {
		return nil, &RulesError{Field: "type", Code: "invalid"}
	}
	if len(up.Body) == 0 || len(up.Body) > MaxDocumentBytes {
		return nil, document.ErrInvalidFile
	}
	in := document.Input{OwnerType: document.OwnerVessel, VesselID: c.VesselID, Type: up.Type, ExpiresOn: up.ExpiresOn}
	if !slices.Contains(vesselDocumentTypes, up.Type) {
		if c.CustomerID == "" {
			return nil, &RulesError{Field: "details", Code: "missing"}
		}
		in = document.Input{OwnerType: document.OwnerCustomer, CustomerID: c.CustomerID, Type: up.Type, ExpiresOn: up.ExpiresOn}
	}
	p := scopedPrincipal(scope, auth.PermCustomersWrite)
	d, err := s.d.Documents.Create(ctx, p, scope.ID(), in)
	if err != nil {
		return nil, err
	}
	// Link before the upload: a document whose upload fails stays invisible on the token surfaces.
	if ob, err := store.OnlineBookingByContract(ctx, s.d.DB.DB, scope, c.ID); err == nil {
		if err := store.LinkOnlineBookingDocument(ctx, s.d.DB.DB, scope, ob.ID, d.Row.ID, s.now()); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if _, err := s.d.Documents.UploadFile(ctx, p, scope.ID(), d.Row.ID, up.ContentType, up.FileName, bytes.NewReader(up.Body)); err != nil {
		return nil, err
	}
	return &Document{ID: d.Row.ID, Type: up.Type, UploadedAt: d.Row.CreatedAt}, nil
}

// UploadDocument attaches a document to the booking's customer or vessel (m8-plan §5.2).
func (s *Service) UploadDocument(ctx context.Context, slug, bookingID string, t Tokens, up DocumentUpload) (*Document, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	_, c, err := s.loadBooking(ctx, s.d.DB.DB, mc, bookingID, t, false)
	if err != nil {
		return nil, err
	}
	switch contract.Status(c.Status) {
	case contract.StatusHeld, contract.StatusPendingPayment, contract.StatusAwaitingApproval, contract.StatusConfirmed:
	default:
		return nil, &contract.TransitionError{From: c.Status, Action: contract.ActionUpdate}
	}
	return s.attachDocument(ctx, mc.scope, c, up)
}

// returnURL is the top-level return page of a booking with the status token and the allowed return_to in the
// fragment (m8-plan §10).
func (s *Service) returnURL(mc *marinaCtx, id, statusToken, returnTo string, cancelled bool) string {
	frag := url.Values{}
	frag.Set("st", statusToken)
	if returnTo != "" {
		frag.Set("rt", returnTo)
	}
	if cancelled {
		frag.Set("cancelled", "1")
	}
	return strings.TrimRight(s.d.BaseURL, "/") + "/b/" + mc.m.Slug + "/r/" + id + "#" + frag.Encode()
}

// allowedReturnTo keeps return_to only when its origin is an allowed embed origin.
func allowedReturnTo(m *store.Marina, raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	if slices.Contains([]string(m.AllowedEmbedOrigins), u.Scheme+"://"+u.Host) {
		return u.String()
	}
	return ""
}

// StartPayment starts the advance payment or sends the booking for approval (m8-plan §5.3, §6).
func (s *Service) StartPayment(ctx context.Context, slug, bookingID string, t Tokens, in PayInput) (*PayResult, error) {
	mc, err := s.resolve(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	ob, c, err := s.loadBooking(ctx, db, mc, bookingID, t, false)
	if err != nil {
		return nil, err
	}
	st, err := s.statusToken(ob.ID, tokenValidUntil(c.EndsOn, mc.loc))
	if err != nil {
		return nil, err
	}
	out := &PayResult{Status: c.Status, StatusToken: st}
	switch contract.Status(c.Status) {
	case contract.StatusHeld, contract.StatusPendingPayment:
	case contract.StatusAwaitingApproval, contract.StatusConfirmed, contract.StatusArrived, contract.StatusDeparted:
		return out, nil // already sent or paid: the same answer again
	default:
		return nil, ErrHoldExpired
	}
	if c.CustomerID == "" {
		return nil, &RulesError{Field: "details", Code: "missing"}
	}
	_, missing, err := s.bookingDocuments(ctx, db, mc, c, ob.ID)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 && !mc.set.RequiredFields.DocumentsLater {
		return nil, &RulesError{Field: "documents", Code: "missing"}
	}
	_, al, err := holdExpiry(ctx, db, mc.scope, c.ID, s.now())
	if err != nil {
		return nil, err
	}
	if al == nil {
		return nil, ErrHoldExpired
	}
	groups, err := store.GroupsOfResources(ctx, db, mc.scope, "")
	if err != nil {
		return nil, err
	}
	vs, err := store.GetVessel(ctx, db, mc.scope, c.VesselID)
	if err != nil {
		return nil, err
	}
	ap := policy.Approval(approvalRules(mc.set), policy.ApprovalInput{LOACm: geo.Cm(vs.LOACm), GroupIDs: groups[al.ResourceID]})
	prov, perr := s.d.Collect.OnlineCheckoutProvider(ctx, db, mc.scope)
	if perr != nil && !mc.set.AllowWithoutProvider {
		return nil, ErrProviderDisabled
	}
	now := s.now()
	deadline := now.Add(time.Duration(max(mc.set.Approval.HoldHours, 1)) * time.Hour)
	returnTo := allowedReturnTo(mc.m, in.ReturnTo)
	who := contract.PublicActor()
	// Approval without a card authorisation: no payment yet (m8-plan §5.3, §5.6).
	if perr != nil || (ap.Required && !prov.Authorize) {
		reason := ap.Reason
		if perr != nil {
			reason = store.ApprovalReasonNoProvider
		}
		if c.Status != string(contract.StatusHeld) {
			return nil, &contract.TransitionError{From: c.Status, Action: contract.ActionRequestApproval}
		}
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			_, err := s.d.Contracts.RequestApprovalTx(ctx, tx, mc.scope, who, c.ID, reason, deadline)
			return err
		})
		if err != nil {
			return nil, err
		}
		out.Status = string(contract.StatusAwaitingApproval)
		return out, nil
	}
	amount := dueNow(c)
	holdUntil := now.Add(30 * time.Minute)
	if end := arrivalDayEnd(c.StartsOn, mc.loc); end.Before(holdUntil) {
		holdUntil = end
	}
	var plan *collect.OnlineCheckout
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LockContract(ctx, tx, mc.scope, c.ID)
		if err != nil {
			return err
		}
		if ap.Required {
			row, err := store.GetOnlineBooking(ctx, tx, mc.scope, ob.ID)
			if err != nil {
				return err
			}
			row.ApprovalReason = ap.Reason
			if err := store.UpdateOnlineBooking(ctx, tx, mc.scope, row, now); err != nil {
				return err
			}
		}
		switch contract.Status(cur.Status) {
		case contract.StatusHeld:
			if _, err := s.d.Contracts.RequestTransitPaymentTx(ctx, tx, mc.scope, nil, c.ID, 0, holdUntil); err != nil {
				return err
			}
		case contract.StatusPendingPayment:
		default:
			return ErrHoldExpired
		}
		plan, err = s.d.Collect.OnlineCheckoutTx(ctx, tx, mc.scope, collect.OnlineCheckoutInput{
			ContractID: c.ID, CustomerID: c.CustomerID, AmountCents: int64(amount), ExpiresAt: holdUntil,
			CaptureManual: ap.Required, Description: mc.m.Name + " " + ob.Reference, Locale: ob.Locale,
			SuccessURL: s.returnURL(mc, ob.ID, st, returnTo, false), CancelURL: s.returnURL(mc, ob.ID, st, returnTo, true),
			Actor: who.Actor,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrHoldExpired) {
			return nil, ErrHoldExpired
		}
		return nil, err
	}
	redirect, err := s.d.Collect.StartOnlineCheckout(ctx, mc.scope, plan)
	if err != nil {
		return nil, err
	}
	out.Status, out.RedirectURL = string(contract.StatusPendingPayment), redirect
	return out, nil
}
