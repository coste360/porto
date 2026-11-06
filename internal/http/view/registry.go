package view

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/http/api"
)

// Registry mappers (m1-plan §4.4): domain views of customers, vessels, documents and deadlines to the
// contract schemas.

func i64(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

func date(d calendar.Date) *string {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func contacts(cs []customer.Contact) []api.ContactEntry {
	out := make([]api.ContactEntry, 0, len(cs))
	for _, c := range cs {
		out = append(out, api.ContactEntry{Label: c.Label, Value: c.Value})
	}
	return out
}

// ContactsIn converts request contacts.
func ContactsIn(cs *[]api.ContactEntry) *[]customer.Contact {
	if cs == nil {
		return nil
	}
	out := make([]customer.Contact, 0, len(*cs))
	for _, c := range *cs {
		out = append(out, customer.Contact{Label: c.Label, Value: c.Value})
	}
	return &out
}

// CustomerAddressIn converts a request address (absent fields are empty).
func CustomerAddressIn(a *api.Address) *customer.Address {
	if a == nil {
		return nil
	}
	return &customer.Address{
		Line1: deref(a.Line1), Line2: deref(a.Line2), PostalCode: deref(a.PostalCode), City: deref(a.City),
		Province: deref(a.Province), Country: deref(a.Country),
	}
}

func category(s string) *api.CustomerCategory {
	if s == "" {
		return nil
	}
	c := api.CustomerCategory(s)
	return &c
}

// Customer converts a customer.
func Customer(c *customer.Customer) api.Customer {
	a := c.BillingAddress
	out := api.Customer{
		Id: c.ID, Kind: api.CustomerKind(c.Kind), FirstName: c.FirstName, LastName: c.LastName,
		CompanyName: c.CompanyName, DisplayName: c.DisplayName, FiscalCode: String(c.FiscalCode),
		VatNumber: String(c.VATNumber), SdiCode: String(c.SDICode), Pec: String(c.PEC),
		BillingAddress: api.Address{
			Line1: &a.Line1, Line2: &a.Line2, PostalCode: &a.PostalCode, City: &a.City, Province: &a.Province, Country: &a.Country,
		},
		Nationality: String(c.Nationality), Locale: api.Locale(c.Locale), Emails: contacts(c.Emails),
		Phones: contacts(c.Phones), Category: category(c.Category), Tags: nonNil(c.Tags),
		IbanMasked: String(c.IBANMasked), Notes: c.Notes, MergedIntoId: String(c.MergedIntoID),
		AnonymizedAt: Time(c.AnonymizedAt), ArchivedAt: Time(c.ArchivedAt), Version: c.Version,
		CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC(),
	}
	if c.PaymentTerms != nil {
		out.PaymentTerms = &api.PaymentTerms{Days: c.PaymentTerms.Days, EndOfMonth: c.PaymentTerms.EndOfMonth}
	}
	return out
}

// CustomerSummary converts a customer list entry.
func CustomerSummary(s *customer.Summary) api.CustomerSummary {
	return api.CustomerSummary{
		Id: s.ID, Kind: api.CustomerKind(s.Kind), DisplayName: s.DisplayName, FiscalCode: String(s.FiscalCode),
		VatNumber: String(s.VATNumber), Category: category(s.Category), Tags: nonNil(s.Tags),
		PrimaryEmail: String(s.PrimaryEmail), PrimaryPhone: String(s.PrimaryPhone), VesselCount: s.VesselCount,
		ArchivedAt: Time(s.ArchivedAt), AnonymizedAt: Time(s.AnonymizedAt), MergedIntoId: String(s.MergedIntoID),
	}
}

// CustomerSummaries converts a list of summaries.
func CustomerSummaries(ss []customer.Summary) []api.CustomerSummary {
	out := make([]api.CustomerSummary, 0, len(ss))
	for i := range ss {
		out = append(out, CustomerSummary(&ss[i]))
	}
	return out
}

func userRef(u *customer.UserRef) *api.UserRef {
	if u == nil {
		return nil
	}
	return &api.UserRef{Id: u.ID, Name: u.Name}
}

// Consent converts a consent record.
func Consent(c *customer.Consent) api.Consent {
	return api.Consent{
		Id: c.ID, Kind: api.ConsentKind(c.Kind), PolicyVersion: c.PolicyVersion, Granted: c.Granted,
		Source: api.ConsentSource(c.Source), RecordedBy: userRef(c.RecordedBy), RecordedAt: c.RecordedAt.UTC(),
	}
}

// Consents converts consent records.
func Consents(cs []customer.Consent) []api.Consent {
	out := make([]api.Consent, 0, len(cs))
	for i := range cs {
		out = append(out, Consent(&cs[i]))
	}
	return out
}

func engines(es []vessel.Engine) []api.Engine {
	out := make([]api.Engine, 0, len(es))
	for _, e := range es {
		out = append(out, api.Engine{
			Make: String(e.Make), Model: String(e.Model), Serial: String(e.Serial), PowerKw: e.PowerKW, PowerHp: e.PowerHP, Hours: e.Hours,
		})
	}
	return out
}

// EnginesIn converts request engines.
func EnginesIn(es *[]api.Engine) *[]vessel.Engine {
	if es == nil {
		return nil
	}
	out := make([]vessel.Engine, 0, len(*es))
	for _, e := range *es {
		out = append(out, vessel.Engine{
			Make: deref(e.Make), Model: deref(e.Model), Serial: deref(e.Serial), PowerKW: e.PowerKw, PowerHP: e.PowerHp, Hours: e.Hours,
		})
	}
	return &out
}

// Int64 returns nil for 0.
func Int64(n int64) *int64 { return i64(n) }

func share(n int64) *int64 { return i64(n) }

func intp(n int64) *int {
	if n == 0 {
		return nil
	}
	v := int(n)
	return &v
}

// Vessel converts a vessel with its documents.
func Vessel(v *vessel.Vessel, docs []document.Document) api.Vessel {
	r := &v.Row
	parties := make([]api.VesselParty, 0, len(v.Parties))
	for _, p := range v.Parties {
		parties = append(parties, api.VesselParty{
			CustomerId: p.CustomerID, DisplayName: p.DisplayName, Role: api.VesselPartyRole(p.Role), ShareMillis: share(p.ShareMillis),
		})
	}
	out := api.Vessel{
		Id: r.ID, Name: r.Name, Registration: String(r.Registration), RegistryOffice: String(r.RegistryOffice),
		Flag: String(r.Flag), HomePort: String(r.HomePort), HullType: api.HullType(r.HullType), Model: String(r.Model),
		Builder: String(r.Builder), YearBuilt: intp(r.YearBuilt), HullColor: String(r.HullColor),
		HullMaterial: String(r.HullMaterial), LoaCm: r.LOACm, LwlCm: i64(r.LWLCm), BeamCm: r.BeamCm,
		DraftCm: i64(r.DraftCm), AirDraftCm: i64(r.AirDraftCm), DisplacementKg: i64(r.DisplacementKg),
		MeasuredLoaCm: i64(r.MeasuredLOACm), MeasuredBeamCm: i64(r.MeasuredBeamCm),
		MeasuredDraftCm: i64(r.MeasuredDraftCm), MeasuredAirDraftCm: i64(r.MeasuredAirDraftCm),
		MeasuredAt: Time(r.MeasuredAt), MeasuredBy: userRef(v.MeasuredBy),
		DimensionsSource: api.DimensionsSource(r.DimensionsSource), Engines: engines(v.Engines),
		PowerRequiredA: intp(r.PowerRequiredA), Notes: r.Notes, ArchivedAt: Time(r.ArchivedAt),
		EffectiveLoaCm: v.EffectiveLOACm, EffectiveBeamCm: v.EffectiveBeamCm, EffectiveDraftCm: i64(v.EffectiveDraftCm),
		UnitClass: api.UnitClass(v.UnitClass), Parties: parties, Documents: Documents(docs), Version: r.Version,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.Fuel != "" {
		f := api.Fuel(r.Fuel)
		out.Fuel = &f
	}
	return out
}

// VesselSummary converts a vessel list entry.
func VesselSummary(s *vessel.Summary) api.VesselSummary {
	owners := make([]api.VesselOwnerRef, 0, len(s.Owners))
	for _, o := range s.Owners {
		owners = append(owners, api.VesselOwnerRef{CustomerId: o.CustomerID, DisplayName: o.DisplayName})
	}
	return api.VesselSummary{
		Id: s.ID, Name: s.Name, Registration: String(s.Registration), HullType: api.HullType(s.HullType),
		LoaCm: s.LOACm, BeamCm: s.BeamCm, EffectiveLoaCm: s.EffectiveLOACm, EffectiveBeamCm: s.EffectiveBeamCm,
		DimensionsSource: api.DimensionsSource(s.DimensionsSource), UnitClass: api.UnitClass(s.UnitClass),
		Owners: owners, ArchivedAt: Time(s.ArchivedAt),
	}
}

// VesselSummaries converts vessel list entries.
func VesselSummaries(ss []vessel.Summary) []api.VesselSummary {
	out := make([]api.VesselSummary, 0, len(ss))
	for i := range ss {
		out = append(out, VesselSummary(&ss[i]))
	}
	return out
}

// Document converts a document.
func Document(d *document.Document) api.Document {
	r := &d.Row
	out := api.Document{
		Id: r.ID, OwnerType: api.DocumentOwnerType(r.OwnerType), CustomerId: String(r.CustomerID),
		VesselId: String(r.VesselID), Type: api.DocumentType(r.Type), Number: String(d.Number),
		Issuer: String(r.Issuer), IssuedOn: date(r.IssuedOn), ExpiresOn: date(r.ExpiresOn), HasFile: d.HasFile,
		FileName: String(r.FileName), Notes: r.Notes, Version: r.Version, CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}
	if d.HasFile {
		out.ContentType = String(d.ContentType)
		out.SizeBytes = i64(d.SizeBytes)
	} else {
		out.FileName = nil
	}
	return out
}

// Documents converts documents.
func Documents(ds []document.Document) []api.Document {
	out := make([]api.Document, 0, len(ds))
	for i := range ds {
		out = append(out, Document(&ds[i]))
	}
	return out
}

// Deadline converts a deadline.
func Deadline(d *deadline.Deadline) api.Deadline {
	out := api.Deadline{
		Id: d.ID, Kind: api.DeadlineKind(d.Kind), SourceType: api.DeadlineSourceType(d.SourceType), SourceId: d.SourceID,
		DueOn: d.DueOn.String(), Status: api.DeadlineStatus(d.Status), Urgency: api.DeadlineUrgency(d.Urgency),
		Note: d.Note, Version: d.Version,
	}
	if d.Customer != nil {
		out.Customer = &api.CustomerRef{Id: d.Customer.ID, DisplayName: d.Customer.Name}
	}
	if d.Vessel != nil {
		out.Vessel = &api.VesselRef{Id: d.Vessel.ID, Name: d.Vessel.Name}
	}
	if d.DocumentType != "" {
		t := api.DocumentType(d.DocumentType)
		out.DocumentType = &t
	}
	return out
}

// Deadlines converts deadlines.
func Deadlines(ds []deadline.Deadline) []api.Deadline {
	out := make([]api.Deadline, 0, len(ds))
	for i := range ds {
		out = append(out, Deadline(&ds[i]))
	}
	return out
}

// DeadlinePolicies converts the lead-day policies.
func DeadlinePolicies(ps []deadline.Policy) api.DeadlinePolicyList {
	out := api.DeadlinePolicyList{Items: make([]api.DeadlinePolicy, 0, len(ps))}
	for _, p := range ps {
		out.Items = append(out.Items, api.DeadlinePolicy{Kind: api.DeadlineKind(p.Kind), LeadDays: p.LeadDays})
	}
	return out
}
