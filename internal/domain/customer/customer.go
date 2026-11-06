package customer

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Kinds, categories and limits (m1-plan §3.1).
const (
	KindPerson  = "person"
	KindCompany = "company"

	maxContacts = 10
	maxTags     = 20
	maxNotes    = 5000
)

var (
	categories = []string{"member", "resident", "affiliated", "charter", "yard"}
	locales    = []string{"it", "en"}
)

// Contact is a labelled email or phone.
type Contact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Address is a billing address (same JSON shape as the organization address).
type Address struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Province   string `json:"province"`
	Country    string `json:"country"`
}

// PaymentTerms are the default payment terms (stored only; used from M5).
type PaymentTerms struct {
	Days       int  `json:"days"`
	EndOfMonth bool `json:"end_of_month"`
}

// Customer is the API view of a customer. The IBAN is never exposed, only its masked form.
type Customer struct {
	ID, Kind                         string
	FirstName, LastName, CompanyName string
	DisplayName                      string
	FiscalCode, VATNumber            string
	SDICode, PEC                     string
	BillingAddress                   Address
	Nationality, Locale              string
	Emails, Phones                   []Contact
	Category                         string
	Tags                             []string
	PaymentTerms                     *PaymentTerms
	IBANMasked                       string
	Notes                            string
	MergedIntoID                     string
	AnonymizedAt, ArchivedAt         time.Time
	Version                          int
	CreatedAt, UpdatedAt             time.Time
}

// Summary is a customer in lists.
type Summary struct {
	ID, Kind, DisplayName    string
	FiscalCode, VATNumber    string
	Category                 string
	Tags                     []string
	PrimaryEmail             string
	PrimaryPhone             string
	VesselCount              int
	ArchivedAt, AnonymizedAt time.Time
	MergedIntoID             string
}

// Patch lists the fields to set (nil = unchanged). On create every nil field takes its default. An empty
// string clears a nullable text field (fiscal code, VAT, SDI, PEC, nationality, category, IBAN).
type Patch struct {
	Kind              *string
	FirstName         *string
	LastName          *string
	CompanyName       *string
	FiscalCode        *string
	VATNumber         *string
	SDICode           *string
	PEC               *string
	BillingAddress    *Address
	Nationality       *string
	Locale            *string
	Emails            *[]Contact
	Phones            *[]Contact
	Category          *string
	Tags              *[]string
	PaymentTerms      *PaymentTerms
	ClearPaymentTerms bool
	IBAN              *string
	Notes             *string
}

// DisplayName returns the name shown for a customer row: "Last First" for people, the company name for
// companies, "" once anonymised (the client shows its own label).
func DisplayName(c *store.Customer) string {
	if !c.AnonymizedAt.IsZero() {
		return ""
	}
	if c.Kind == KindCompany {
		return c.CompanyName
	}
	return strings.TrimSpace(c.LastName + " " + c.FirstName)
}

func decodeContacts(j store.JSON) []Contact {
	out := []Contact{}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	if out == nil {
		out = []Contact{}
	}
	return out
}

func decodeAddress(j store.JSON) Address {
	var a Address
	if len(j) > 0 {
		_ = json.Unmarshal(j, &a)
	}
	return a
}

func (s *Service) toCustomer(c *store.Customer) Customer {
	out := Customer{
		ID: c.ID, Kind: c.Kind, FirstName: c.FirstName, LastName: c.LastName, CompanyName: c.CompanyName,
		DisplayName: DisplayName(c), FiscalCode: c.FiscalCode, VATNumber: c.VATNumber, SDICode: c.SDICode,
		PEC: c.PEC, BillingAddress: decodeAddress(c.BillingAddress), Nationality: c.Nationality, Locale: c.Locale,
		Emails: decodeContacts(c.Emails), Phones: decodeContacts(c.Phones), Category: c.Category,
		Tags: slices.Clone([]string(c.Tags)), Notes: c.Notes, MergedIntoID: c.MergedIntoID,
		AnonymizedAt: c.AnonymizedAt, ArchivedAt: c.ArchivedAt, Version: c.Version, CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if len(c.PaymentTerms) > 0 && string(c.PaymentTerms) != "null" {
		var pt PaymentTerms
		if json.Unmarshal(c.PaymentTerms, &pt) == nil {
			out.PaymentTerms = &pt
		}
	}
	if c.IBANEnc != "" && c.IBANLast4 != "" {
		country := "??"
		if iban, err := s.d.Keys.DecryptString(c.IBANEnc, ibanAAD(c.ID)); err == nil && len(iban) >= 2 {
			country = iban[:2]
		}
		out.IBANMasked = MaskIBAN(country, c.IBANLast4)
	}
	return out
}

func toSummary(c *store.Customer, vessels int) Summary {
	out := Summary{
		ID: c.ID, Kind: c.Kind, DisplayName: DisplayName(c), FiscalCode: c.FiscalCode, VATNumber: c.VATNumber,
		Category: c.Category, Tags: slices.Clone([]string(c.Tags)), VesselCount: vessels,
		ArchivedAt: c.ArchivedAt, AnonymizedAt: c.AnonymizedAt, MergedIntoID: c.MergedIntoID,
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if es := decodeContacts(c.Emails); len(es) > 0 {
		out.PrimaryEmail = es[0].Value
	}
	if ps := decodeContacts(c.Phones); len(ps) > 0 {
		out.PrimaryPhone = ps[0].Value
	}
	return out
}

func ibanAAD(customerID string) string { return "iban:" + customerID }

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// searchText builds the normalised search text of a customer: names, fiscal code, VAT, emails, phone digits.
func searchText(c *store.Customer) string {
	parts := []string{Fold(c.FirstName), Fold(c.LastName), Fold(c.CompanyName), Fold(c.FiscalCode), Fold(c.VATNumber), Fold(c.PEC)}
	for _, e := range decodeContacts(c.Emails) {
		parts = append(parts, e.Value)
	}
	for _, p := range decodeContacts(c.Phones) {
		parts = append(parts, Digits(p.Value))
	}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// apply validates p and writes it onto row, returning the names of the changed fields. iban is the new IBAN
// in clear when p sets one ("" = cleared, nil = unchanged).
func (s *Service) apply(row *store.Customer, p Patch, creating bool) ([]string, error) {
	v := &auth.ValidationError{}
	var changed []string
	set := func(field string, cond bool) {
		if cond {
			changed = append(changed, field)
		}
	}
	text := func(field string, in *string, dst *string, maxRunes int) {
		if in == nil {
			return
		}
		t, ok := clean(*in, maxRunes)
		if !ok {
			v.Add(field, "too_long")
			return
		}
		set(field, *dst != t)
		*dst = t
	}
	if p.Kind != nil {
		if *p.Kind != KindPerson && *p.Kind != KindCompany {
			v.Add("kind", "invalid")
		} else {
			set("kind", row.Kind != *p.Kind)
			row.Kind = *p.Kind
		}
	} else if creating {
		v.Add("kind", "required")
	}
	text("first_name", p.FirstName, &row.FirstName, 100)
	text("last_name", p.LastName, &row.LastName, 100)
	text("company_name", p.CompanyName, &row.CompanyName, 200)
	if p.FiscalCode != nil {
		fc := NormalizeCode(*p.FiscalCode)
		switch {
		case fc == "":
		case row.Kind == KindCompany && !ValidCompanyFiscalCode(fc), row.Kind != KindCompany && !ValidPersonFiscalCode(fc):
			v.Add("fiscal_code", "fiscal_code_invalid")
		}
		set("fiscal_code", row.FiscalCode != fc)
		row.FiscalCode = fc
	}
	if p.VATNumber != nil {
		vat := NormalizeCode(*p.VATNumber)
		if vat != "" && !ValidVAT(vat) {
			v.Add("vat_number", "vat_invalid")
		}
		set("vat_number", row.VATNumber != vat)
		row.VATNumber = vat
	}
	if p.SDICode != nil {
		sdi := NormalizeCode(*p.SDICode)
		if sdi != "" && !validSDI(sdi) {
			v.Add("sdi_code", "invalid")
		}
		set("sdi_code", row.SDICode != sdi)
		row.SDICode = sdi
	}
	if p.PEC != nil {
		pec := strings.TrimSpace(*p.PEC)
		if pec != "" {
			n, ok := NormalizeEmail(pec)
			if !ok {
				v.Add("pec", "invalid")
			}
			pec = n
		}
		set("pec", row.PEC != pec)
		row.PEC = pec
	}
	if p.BillingAddress != nil {
		a, ok := cleanAddress(*p.BillingAddress)
		if !ok {
			v.Add("billing_address", "invalid")
		}
		b := store.MustJSON(a)
		set("billing_address", string(row.BillingAddress) != string(b))
		row.BillingAddress = b
	} else if creating {
		row.BillingAddress = store.MustJSON(Address{})
	}
	if p.Nationality != nil {
		n := strings.ToUpper(strings.TrimSpace(*p.Nationality))
		if n != "" && (len(n) != 2 || !isLetter(n[0]) || !isLetter(n[1])) {
			v.Add("nationality", "invalid")
		}
		set("nationality", row.Nationality != n)
		row.Nationality = n
	}
	if p.Locale != nil {
		if !slices.Contains(locales, *p.Locale) {
			v.Add("locale", "invalid")
		}
		set("locale", row.Locale != *p.Locale)
		row.Locale = *p.Locale
	} else if creating {
		row.Locale = "it"
	}
	if p.Emails != nil {
		es, ok := cleanContacts(*p.Emails, NormalizeEmail)
		if !ok {
			v.Add("emails", "invalid")
		}
		b := store.MustJSON(es)
		set("emails", string(row.Emails) != string(b))
		row.Emails = b
	} else if creating {
		row.Emails = store.JSON("[]")
	}
	if p.Phones != nil {
		ps, ok := cleanContacts(*p.Phones, NormalizePhone)
		if !ok {
			v.Add("phones", "invalid")
		}
		b := store.MustJSON(ps)
		set("phones", string(row.Phones) != string(b))
		row.Phones = b
	} else if creating {
		row.Phones = store.JSON("[]")
	}
	if p.Category != nil {
		if *p.Category != "" && !slices.Contains(categories, *p.Category) {
			v.Add("category", "invalid")
		}
		set("category", row.Category != *p.Category)
		row.Category = *p.Category
	}
	if p.Tags != nil {
		tags, ok := cleanTags(*p.Tags)
		if !ok {
			v.Add("tags", "invalid")
		}
		set("tags", !slices.Equal([]string(row.Tags), tags))
		row.Tags = tags
	} else if creating {
		row.Tags = store.Strings{}
	}
	switch {
	case p.ClearPaymentTerms:
		set("payment_terms", len(row.PaymentTerms) > 0)
		row.PaymentTerms = nil
	case p.PaymentTerms != nil:
		if p.PaymentTerms.Days < 0 || p.PaymentTerms.Days > 365 {
			v.Add("payment_terms", "invalid")
		}
		b := store.MustJSON(p.PaymentTerms)
		set("payment_terms", string(row.PaymentTerms) != string(b))
		row.PaymentTerms = b
	}
	if p.IBAN != nil {
		iban := NormalizeCode(*p.IBAN)
		switch {
		case iban == "":
			set("iban", row.IBANEnc != "")
			row.IBANEnc, row.IBANLast4 = "", ""
		case !ValidIBAN(iban):
			v.Add("iban", "iban_invalid")
		default:
			enc, err := s.d.Keys.EncryptString(iban, ibanAAD(row.ID))
			if err != nil {
				return nil, err
			}
			changed = append(changed, "iban")
			row.IBANEnc, row.IBANLast4 = enc, iban[len(iban)-4:]
		}
	}
	if p.Notes != nil {
		t, ok := clean(*p.Notes, maxNotes)
		if !ok {
			v.Add("notes", "too_long")
		}
		set("notes", row.Notes != t)
		row.Notes = t
	}
	// Cross-field rules.
	if row.Kind == KindPerson && row.LastName == "" {
		v.Add("last_name", "required")
	}
	if row.Kind == KindCompany && row.CompanyName == "" {
		v.Add("company_name", "required")
	}
	if p.Kind != nil && !creating && row.FiscalCode != "" && p.FiscalCode == nil {
		// A kind change re-checks the stored fiscal code with the rule of the new kind.
		if (row.Kind == KindCompany && !ValidCompanyFiscalCode(row.FiscalCode)) || (row.Kind == KindPerson && !ValidPersonFiscalCode(row.FiscalCode)) {
			v.Add("fiscal_code", "fiscal_code_invalid")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	row.SearchText = searchText(row)
	return changed, nil
}

func validSDI(s string) bool {
	if len(s) != 6 && len(s) != 7 {
		return false
	}
	for i := range len(s) {
		if !isDigit(s[i]) && !isLetter(s[i]) {
			return false
		}
	}
	return true
}

func cleanAddress(a Address) (Address, bool) {
	ok := true
	f := func(s string, n int) string {
		t, fits := clean(s, n)
		ok = ok && fits
		return t
	}
	out := Address{
		Line1: f(a.Line1, 200), Line2: f(a.Line2, 200), PostalCode: f(a.PostalCode, 20), City: f(a.City, 100),
		Province: f(a.Province, 100), Country: strings.ToUpper(f(a.Country, 2)),
	}
	if out.Country != "" && (len(out.Country) != 2 || !isLetter(out.Country[0]) || !isLetter(out.Country[1])) {
		ok = false
	}
	return out, ok
}

func cleanContacts(in []Contact, normalize func(string) (string, bool)) ([]Contact, bool) {
	if len(in) > maxContacts {
		return nil, false
	}
	out := make([]Contact, 0, len(in))
	for _, c := range in {
		label, ok := clean(c.Label, 40)
		if !ok {
			return nil, false
		}
		value, ok := normalize(c.Value)
		if !ok {
			return nil, false
		}
		if slices.ContainsFunc(out, func(o Contact) bool { return o.Value == value }) {
			continue
		}
		out = append(out, Contact{Label: label, Value: value})
	}
	return out, true
}

func cleanTags(in []string) (store.Strings, bool) {
	if len(in) > maxTags {
		return nil, false
	}
	out := store.Strings{}
	for _, t := range in {
		t, ok := clean(t, 40)
		if !ok || t == "" {
			return nil, false
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, true
}

// writeAudit records a customer action with the changed field names only (never values, m1-plan D14).
func (s *Service) writeAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, action, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action,
		EntityType: "customer", EntityID: id, Diff: diff,
	})
}

// CreateResult is a new customer with the suspected duplicates found at creation.
type CreateResult struct {
	Customer           Customer
	PossibleDuplicates []Summary
}

// Create adds a customer (customers.write) and reports suspected duplicates (same fiscal code, VAT or email).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in Patch) (*CreateResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.Customer{ID: store.NewID(), MarinaID: scope.ID(), Version: 1, CreatedAt: now, UpdatedAt: now}
	changed, err := s.apply(row, in, true)
	if err != nil {
		return nil, err
	}
	var out *CreateResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertCustomer(ctx, tx, row); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "customer.create", row.ID, map[string]any{"fields": changed, "kind": row.Kind}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerCreated, event.CustomerData{CustomerID: row.ID}); err != nil {
			return err
		}
		dups, err := s.duplicates(ctx, tx, scope, row)
		if err != nil {
			return err
		}
		out = &CreateResult{Customer: s.toCustomer(row), PossibleDuplicates: dups}
		return nil
	})
	return out, err
}

// duplicates returns the suspected duplicates of a customer.
func (s *Service) duplicates(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Customer) ([]Summary, error) {
	if !c.AnonymizedAt.IsZero() {
		return []Summary{}, nil
	}
	emails := decodeContacts(c.Emails)
	values := make([]string, 0, len(emails))
	for _, e := range emails {
		values = append(values, e.Value)
	}
	cands, err := store.CustomerDuplicateCandidates(ctx, db, scope, c.ID, c.FiscalCode, c.VATNumber, values)
	if err != nil {
		return nil, err
	}
	var rows []store.Customer
	for i := range cands {
		o := &cands[i]
		match := (c.FiscalCode != "" && o.FiscalCode == c.FiscalCode) || (c.VATNumber != "" && o.VATNumber == c.VATNumber)
		if !match {
			for _, e := range decodeContacts(o.Emails) {
				if slices.Contains(values, e.Value) {
					match = true
					break
				}
			}
		}
		if match {
			rows = append(rows, *o)
		}
	}
	return s.summaries(ctx, db, scope, rows)
}

func (s *Service) summaries(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Customer) ([]Summary, error) {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	counts, err := store.VesselCountsOfCustomers(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(rows))
	for i := range rows {
		out = append(out, toSummary(&rows[i], counts[rows[i].ID]))
	}
	return out, nil
}

// Get returns a customer (customers.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Customer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetCustomer(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	c := s.toCustomer(row)
	return &c, nil
}

// Filter narrows List.
type Filter = store.CustomerFilter

// List returns a page of customers (customers.read). f.Search is normalised here.
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[Summary], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	if f.Category != "" && !slices.Contains(categories, f.Category) {
		return store.Page[Summary]{}, &auth.ValidationError{Fields: []auth.FieldError{{Field: "category", Code: "invalid"}}}
	}
	f.Search = SearchTerm(f.Search)
	f.Tag = strings.TrimSpace(f.Tag)
	page, err := store.ListCustomers(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	items, err := s.summaries(ctx, s.d.DB.DB, scope, page.Items)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	return store.Page[Summary]{Items: items, NextCursor: page.NextCursor}, nil
}

// modifiable reports the 409 errors of a customer that can no longer change.
func modifiable(c *store.Customer) error {
	switch {
	case !c.AnonymizedAt.IsZero():
		return ErrAnonymized
	case c.MergedIntoID != "":
		return ErrMerged
	}
	return nil
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

// Update changes a customer when its version equals ifMatch (customers.write).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in Patch) (*Customer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Customer
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCustomer(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if err := modifiable(row); err != nil {
			return err // permanent state: reported before a stale version
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		changed, err := s.apply(row, in, false)
		if err != nil {
			return err
		}
		row.UpdatedAt = s.now()
		if err := store.UpdateCustomer(ctx, tx, scope, ifMatch, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, "customer.update", row.ID, map[string]any{"fields": changed}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerUpdated, event.CustomerUpdatedData{CustomerID: row.ID, Change: event.ChangeUpdated}); err != nil {
			return err
		}
		c := s.toCustomer(row)
		out = &c
		return nil
	})
	return out, err
}

// Archive hides a customer from the default list (customers.write). Archiving an archived customer is a
// conflict.
func (s *Service) Archive(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	return s.setArchived(ctx, p, marinaID, id, true)
}

// Unarchive restores an archived customer (customers.write). A merged customer stays archived.
func (s *Service) Unarchive(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	return s.setArchived(ctx, p, marinaID, id, false)
}

func (s *Service) setArchived(ctx context.Context, p *auth.Principal, marinaID, id string, archived bool) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCustomer(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if err := modifiable(row); err != nil {
			return err
		}
		if archived == !row.ArchivedAt.IsZero() {
			return store.ErrConflict
		}
		now := s.now()
		action, change := "customer.unarchive", event.ChangeUnarchived
		row.ArchivedAt = time.Time{}
		if archived {
			action, change = "customer.archive", event.ChangeArchived
			row.ArchivedAt = now
		}
		row.UpdatedAt = now
		if err := store.UpdateCustomer(ctx, tx, scope, row.Version, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, action, row.ID, nil); err != nil {
			return err
		}
		_, err = s.d.Outbox.Write(ctx, tx, &scope, event.CustomerUpdated, event.CustomerUpdatedData{CustomerID: row.ID, Change: change})
		return err
	})
}

// Duplicates returns the suspected duplicates of a customer (customers.read).
func (s *Service) Duplicates(ctx context.Context, p *auth.Principal, marinaID, id string) ([]Summary, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetCustomer(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.duplicates(ctx, s.d.DB.DB, scope, row)
}
