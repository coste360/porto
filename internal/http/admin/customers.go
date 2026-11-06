package admin

import (
	"encoding/json"
	"net/http"

	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Customer operations (m1-plan §4.4).

func customerPatch(in api.CustomerPatch, fields map[string]json.RawMessage) customer.Patch {
	p := customer.Patch{
		FirstName: in.FirstName, LastName: in.LastName, CompanyName: in.CompanyName,
		FiscalCode: nullable(fields, "fiscal_code", in.FiscalCode), VATNumber: nullable(fields, "vat_number", in.VatNumber),
		SDICode: nullable(fields, "sdi_code", in.SdiCode), PEC: nullable(fields, "pec", in.Pec),
		BillingAddress: view.CustomerAddressIn(in.BillingAddress), Nationality: nullable(fields, "nationality", in.Nationality),
		Emails: view.ContactsIn(in.Emails), Phones: view.ContactsIn(in.Phones), Tags: in.Tags,
		IBAN: nullable(fields, "iban", in.Iban), Notes: in.Notes,
		ClearPaymentTerms: respond.IsNull(fields, "payment_terms"),
	}
	if in.Kind != nil {
		k := string(*in.Kind)
		p.Kind = &k
	}
	if in.Locale != nil {
		l := string(*in.Locale)
		p.Locale = &l
	}
	if in.Category != nil {
		c := string(*in.Category)
		p.Category = &c
	} else if respond.IsNull(fields, "category") {
		empty := ""
		p.Category = &empty
	}
	if respond.IsNull(fields, "billing_address") {
		p.BillingAddress = &customer.Address{}
	}
	if in.PaymentTerms != nil {
		p.PaymentTerms = &customer.PaymentTerms{Days: in.PaymentTerms.Days, EndOfMonth: in.PaymentTerms.EndOfMonth}
	}
	return p
}

func customerInput(in api.CustomerInput) customer.Patch {
	kind := string(in.Kind)
	p := customerPatch(api.CustomerPatch{
		BillingAddress: in.BillingAddress, Category: in.Category, CompanyName: in.CompanyName, Emails: in.Emails,
		FirstName: in.FirstName, FiscalCode: in.FiscalCode, Iban: in.Iban, LastName: in.LastName, Locale: in.Locale,
		Nationality: in.Nationality, Notes: in.Notes, PaymentTerms: in.PaymentTerms, Pec: in.Pec, Phones: in.Phones,
		SdiCode: in.SdiCode, Tags: in.Tags, VatNumber: in.VatNumber,
	}, nil)
	p.Kind = &kind
	return p
}

// ListCustomers implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers).
func (h *Handler) ListCustomers(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListCustomersParams) {
	f := customer.Filter{Search: str(params.Q), Tag: str(params.Tag)}
	if params.Category != nil {
		f.Category = string(*params.Category)
	}
	if params.Archived != nil {
		f.Archived = *params.Archived
	}
	page, err := h.Customers.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.CustomerPage{Items: view.CustomerSummaries(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// CreateCustomer implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers).
func (h *Handler) CreateCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateCustomerParams) {
	var in api.CustomerInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Customers.Create(r.Context(), principal(r), marinaID, customerInput(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Customer.Version)
	respond.JSON(w, http.StatusCreated, api.CustomerCreated{
		Customer: view.Customer(&res.Customer), PossibleDuplicates: view.CustomerSummaries(res.PossibleDuplicates),
	})
}

// GetCustomer implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers/{customerId}).
func (h *Handler) GetCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	c, err := h.Customers.Get(r.Context(), principal(r), marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusOK, view.Customer(c))
}

// UpdateCustomer implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/customers/{customerId}).
func (h *Handler) UpdateCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, params api.UpdateCustomerParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.CustomerPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	c, err := h.Customers.Update(r.Context(), principal(r), marinaID, customerID, version, customerPatch(in, fields))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusOK, view.Customer(c))
}

// ArchiveCustomer implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers/{customerId}/archive).
func (h *Handler) ArchiveCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	if err := h.Customers.Archive(r.Context(), principal(r), marinaID, customerID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// UnarchiveCustomer implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers/{customerId}/unarchive).
func (h *Handler) UnarchiveCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	if err := h.Customers.Unarchive(r.Context(), principal(r), marinaID, customerID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// MergeCustomers implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers/{customerId}/merge).
func (h *Handler) MergeCustomers(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, params api.MergeCustomersParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.CustomerMergeRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Customers.Merge(r.Context(), principal(r), marinaID, customerID, version, in.SourceIds)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusOK, view.Customer(c))
}

// AnonymizeCustomer implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers/{customerId}/anonymize).
func (h *Handler) AnonymizeCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	var in api.CustomerAnonymizeRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := h.Customers.Anonymize(r.Context(), principal(r), marinaID, customerID, bool(in.Confirm)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ListConsents implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers/{customerId}/consents).
func (h *Handler) ListConsents(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	cs, err := h.Customers.ListConsents(r.Context(), principal(r), marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ConsentList{Items: view.Consents(cs)})
}

// RecordConsent implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/customers/{customerId}/consents).
func (h *Handler) RecordConsent(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, _ api.RecordConsentParams) {
	var in api.ConsentInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Customers.RecordConsent(r.Context(), principal(r), marinaID, customerID, customer.ConsentInput{
		Kind: string(in.Kind), PolicyVersion: in.PolicyVersion, Granted: in.Granted, Source: string(in.Source),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.Consent(c))
}
