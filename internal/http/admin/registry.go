package admin

import (
	"encoding/json"
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// M1 registry operations (m1-plan §4.4): helpers shared by customers.go, vessels.go, documents.go and
// deadlines.go, plus the customer page and GDPR export, which combine the registry services.

// nullable returns the pointer to set for an optional text field of a PATCH body: the value, "" when the
// field is an explicit null (clear), nil when absent.
func nullable(fields map[string]json.RawMessage, key string, v *string) *string {
	if respond.IsNull(fields, key) {
		empty := ""
		return &empty
	}
	return v
}

// nullableInt is nullable for numbers (0 clears).
func nullableInt(fields map[string]json.RawMessage, key string, v *int64) *int64 {
	if respond.IsNull(fields, key) {
		var zero int64
		return &zero
	}
	return v
}

// parseDate parses an optional date field, recording a validation error on bad input.
func parseDate(v *auth.ValidationError, field string, s *string) *calendar.Date {
	if s == nil {
		return nil
	}
	if *s == "" {
		return &calendar.Date{}
	}
	d, err := calendar.Parse(*s)
	if err != nil {
		v.Add(field, "invalid")
		return nil
	}
	return &d
}

// GetCustomerOverview implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers/{customerId}/overview).
func (h *Handler) GetCustomerOverview(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	ctx, p := r.Context(), principal(r)
	ov, err := h.Customers.Overview(ctx, p, marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	vids := make([]string, 0, len(ov.Vessels))
	for _, l := range ov.Vessels {
		vids = append(vids, l.VesselID)
	}
	vs, err := h.Vessels.GetMany(ctx, p, marinaID, vids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	docs, err := h.Documents.OfCustomer(ctx, p, marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	dls, err := h.Deadlines.ForCustomer(ctx, p, marinaID, customerID, vids, false)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	summaries := map[string]api.VesselSummary{}
	for _, s := range view.VesselSummaries(vessel.Summaries(vs)) {
		summaries[s.Id] = s
	}
	links := make([]api.CustomerVesselLink, 0, len(ov.Vessels))
	for _, l := range ov.Vessels {
		s, ok := summaries[l.VesselID]
		if !ok {
			continue
		}
		roles := make([]api.VesselPartyRole, 0, len(l.Roles))
		for _, role := range l.Roles {
			roles = append(roles, api.VesselPartyRole(role))
		}
		var share *int64
		if l.ShareMillis > 0 {
			n := l.ShareMillis
			share = &n
		}
		links = append(links, api.CustomerVesselLink{Vessel: s, Roles: roles, ShareMillis: share})
	}
	respond.SetETag(w, ov.Customer.Version)
	respond.JSON(w, http.StatusOK, api.CustomerOverview{
		Customer: view.Customer(&ov.Customer), Vessels: links, Documents: view.Documents(docs),
		Deadlines: view.Deadlines(dls), Consents: view.Consents(ov.Consents),
		Duplicates: view.CustomerSummaries(ov.Duplicates),
	})
}

// ExportCustomer implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers/{customerId}/export).
func (h *Handler) ExportCustomer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId) {
	ctx, p := r.Context(), principal(r)
	ex, err := h.Customers.Export(ctx, p, marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var vids []string
	parties := make([]api.VesselPartyRow, 0, len(ex.Parties))
	for _, pr := range ex.Parties {
		parties = append(parties, api.VesselPartyRow{
			VesselId: pr.VesselID, CustomerId: pr.CustomerID, Role: api.VesselPartyRole(pr.Role), ShareMillis: view.Int64(pr.ShareMillis),
		})
		vids = append(vids, pr.VesselID)
	}
	vs, err := h.Vessels.GetMany(ctx, p, marinaID, vids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	vessels := make([]api.Vessel, 0, len(vs))
	for i := range vs {
		vdocs, err := h.Documents.OfVessel(ctx, p, marinaID, vs[i].Row.ID)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		vessels = append(vessels, view.Vessel(&vs[i], vdocs))
	}
	docs, err := h.Documents.OfCustomer(ctx, p, marinaID, customerID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	dls, err := h.Deadlines.ForCustomer(ctx, p, marinaID, customerID, vids, true)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="customer-`+ex.Customer.ID+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.CustomerExport{
		Format: "porto-customer/1", ExportedAt: h.Clock.Now().UTC(), Customer: view.Customer(&ex.Customer),
		Consents: view.Consents(ex.Consents), Vessels: vessels, Parties: parties, Documents: view.Documents(docs),
		Deadlines: view.Deadlines(dls),
	})
}
