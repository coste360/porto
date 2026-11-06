package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auditlog"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func nextCursor(c string) *string { return view.String(c) }

// ---- organizations ----

// ListOrganizations implements api.ServerInterface.
func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.Marinas.ListOrganizations(r.Context(), principal(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.OrganizationList{Items: make([]api.Organization, 0, len(orgs))}
	for i := range orgs {
		out.Items = append(out.Items, view.Organization(&orgs[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// UpdateOrganization implements api.ServerInterface.
func (h *Handler) UpdateOrganization(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId, params api.UpdateOrganizationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.OrganizationPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	o, err := h.Marinas.UpdateOrganization(r.Context(), principal(r), orgID, version, marina.OrganizationPatch{
		Name: in.Name, VATNumber: in.VatNumber, FiscalCode: in.FiscalCode, BillingAddress: view.AddressIn(in.BillingAddress),
		SDICode: in.SdiCode, PEC: in.Pec,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, o.Version)
	respond.JSON(w, http.StatusOK, view.Organization(o))
}

// ListOrgOwners implements api.ServerInterface.
func (h *Handler) ListOrgOwners(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId) {
	ms, err := h.Team.ListOrgOwners(r.Context(), principal(r), orgID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Members(ms))
}

func memberInput(in api.MemberCreate) team.AddMemberInput {
	out := team.AddMemberInput{Email: in.Email, Name: in.Name}
	if in.Locale != nil {
		out.Locale = string(*in.Locale)
	}
	if in.Role != nil {
		out.Role = auth.Role(*in.Role)
	}
	return out
}

// AddOrgOwner implements api.ServerInterface.
func (h *Handler) AddOrgOwner(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId, _ api.AddOrgOwnerParams) {
	var in api.MemberCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	m, err := h.Team.AddOrgOwner(r.Context(), principal(r), orgID, memberInput(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.Member(m))
}

// RemoveOrgOwner implements api.ServerInterface.
func (h *Handler) RemoveOrgOwner(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId, userID api.UserId) {
	if err := h.Team.RemoveOrgOwner(r.Context(), principal(r), orgID, userID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- marinas ----

// ListMarinas implements api.ServerInterface.
func (h *Handler) ListMarinas(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId) {
	ms, err := h.Marinas.ListMarinas(r.Context(), principal(r), orgID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.MarinaList{Items: make([]api.Marina, 0, len(ms))}
	for i := range ms {
		out.Items = append(out.Items, view.Marina(&ms[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateMarina implements api.ServerInterface.
func (h *Handler) CreateMarina(w http.ResponseWriter, r *http.Request, orgID api.OrganizationId, _ api.CreateMarinaParams) {
	var in api.MarinaCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ci := marina.CreateMarinaInput{Name: in.Name, Slug: in.Slug, Timezone: str(in.Timezone), Currency: str(in.Currency)}
	if in.Kind != nil {
		ci.Kind = string(*in.Kind)
	}
	if in.DefaultLocale != nil {
		ci.DefaultLocale = string(*in.DefaultLocale)
	}
	if in.Locales != nil {
		ci.Locales = view.LocalesIn(*in.Locales)
	}
	m, err := h.Marinas.CreateMarina(r.Context(), principal(r), orgID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, m.Version)
	respond.JSON(w, http.StatusCreated, view.Marina(m))
}

// GetMarina implements api.ServerInterface.
func (h *Handler) GetMarina(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	m, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, m.Version)
	respond.JSON(w, http.StatusOK, view.Marina(m))
}

func rawObject(m *map[string]any) (*json.RawMessage, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(*m)
	if err != nil {
		return nil, err
	}
	raw := json.RawMessage(b)
	return &raw, nil
}

// UpdateMarina implements api.ServerInterface.
func (h *Handler) UpdateMarina(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateMarinaParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.MarinaPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	patch := marina.MarinaPatch{
		Name: in.Name, Timezone: in.Timezone, Currency: in.Currency, ConcessionRef: in.ConcessionRef,
		BookingEnabled: in.BookingEnabled, AllowedEmbedOrigins: in.AllowedEmbedOrigins, Address: view.AddressIn(in.Address),
		ClearGeo: respond.IsNull(fields, "geo"), ClearConcessionExpiresOn: respond.IsNull(fields, "concession_expires_on"),
	}
	v := &auth.ValidationError{}
	if in.Kind != nil {
		k := string(*in.Kind)
		patch.Kind = &k
	}
	if in.DefaultLocale != nil {
		l := string(*in.DefaultLocale)
		patch.DefaultLocale = &l
	}
	if in.Locales != nil {
		ls := view.LocalesIn(*in.Locales)
		patch.Locales = &ls
	}
	if in.Contacts != nil {
		patch.Contacts = &marina.Contacts{
			Email: str(in.Contacts.Email), Phone: str(in.Contacts.Phone), Website: str(in.Contacts.Website),
			VHFChannel: str(in.Contacts.VhfChannel),
		}
	}
	if in.Geo != nil {
		patch.Geo = &marina.Geo{LatE7: in.Geo.LatE7, LonE7: in.Geo.LonE7}
	}
	if in.ConcessionExpiresOn != nil {
		d, err := calendar.Parse(*in.ConcessionExpiresOn)
		if err != nil {
			v.Add("concession_expires_on", "invalid")
		} else {
			patch.ConcessionExpiresOn = &d
		}
	}
	if patch.Settings, err = rawObject(in.Settings); err != nil {
		v.Add("settings", "invalid")
	}
	if patch.Theme, err = rawObject(in.Theme); err != nil {
		v.Add("theme", "invalid")
	}
	if err := v.Err(); err != nil {
		respond.Error(w, r, err)
		return
	}
	m, err := h.Marinas.UpdateMarina(r.Context(), principal(r), marinaID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, m.Version)
	respond.JSON(w, http.StatusOK, view.Marina(m))
}

// ArchiveMarina implements api.ServerInterface.
func (h *Handler) ArchiveMarina(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	if err := h.Marinas.ArchiveMarina(r.Context(), principal(r), marinaID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- members ----

// ListMembers implements api.ServerInterface.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	ms, err := h.Team.ListMembers(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Members(ms))
}

// AddMember implements api.ServerInterface.
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.AddMemberParams) {
	var in api.MemberCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	m, err := h.Team.AddMember(r.Context(), principal(r), marinaID, memberInput(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.Member(m))
}

// UpdateMember implements api.ServerInterface.
func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, userID api.UserId) {
	var in api.MemberUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	m, err := h.Team.ChangeRole(r.Context(), principal(r), marinaID, userID, auth.Role(in.Role))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Member(m))
}

// RemoveMember implements api.ServerInterface.
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, userID api.UserId) {
	if err := h.Team.RemoveMember(r.Context(), principal(r), marinaID, userID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// SendMemberPasswordReset implements api.ServerInterface.
func (h *Handler) SendMemberPasswordReset(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, userID api.UserId) {
	if err := h.Team.SendPasswordReset(r.Context(), principal(r), marinaID, userID); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ---- API keys ----

// ListApiKeys implements api.ServerInterface.
func (h *Handler) ListApiKeys(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) { //nolint:revive // generated name
	keys, err := h.APIKeys.List(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.ApiKeyList{Items: make([]api.ApiKey, 0, len(keys))}
	for i := range keys {
		out.Items = append(out.Items, view.APIKey(&keys[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateApiKey implements api.ServerInterface.
func (h *Handler) CreateApiKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateApiKeyParams) { //nolint:revive // generated name
	var in api.ApiKeyCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	secret, k, err := h.APIKeys.Create(r.Context(), principal(r), marinaID, apikey.CreateInput{
		Name: in.Name, Scopes: view.PermissionsIn(in.Scopes), ExpiresAt: in.ExpiresAt,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.ApiKeyCreated{ApiKey: view.APIKey(k), Secret: secret})
}

// RevokeApiKey implements api.ServerInterface.
func (h *Handler) RevokeApiKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, keyID string) { //nolint:revive // generated name
	if err := h.APIKeys.Revoke(r.Context(), principal(r), marinaID, keyID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- devices ----

// CreatePairingCode implements api.ServerInterface.
func (h *Handler) CreatePairingCode(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreatePairingCodeParams) {
	var in api.PairingCodeCreate
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	pi := device.PairingInput{UserID: str(in.UserId), TTL: time.Duration(intOr(in.TtlSeconds, 0)) * time.Second}
	if in.Kind != nil {
		pi.Kind = string(*in.Kind)
	}
	c, err := h.Devices.CreatePairingCode(r.Context(), principal(r), marinaID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, api.PairingCode{
		Code: c.Code, ExpiresAt: c.ExpiresAt.UTC(), QrPayload: c.QRPayload, QrSvg: c.QRSVG,
	})
}

// ListDevices implements api.ServerInterface.
func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	ds, err := h.Devices.List(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.DeviceList{Items: make([]api.Device, 0, len(ds))}
	for i := range ds {
		out.Items = append(out.Items, view.Device(&ds[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// RevokeDevice implements api.ServerInterface.
func (h *Handler) RevokeDevice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID string) {
	if err := h.Devices.Revoke(r.Context(), principal(r), marinaID, deviceID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- webhooks ----

// ListWebhookEndpoints implements api.ServerInterface.
func (h *Handler) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	es, err := h.Webhooks.List(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.WebhookEndpointList{Items: make([]api.WebhookEndpoint, 0, len(es))}
	for i := range es {
		out.Items = append(out.Items, view.WebhookEndpoint(&es[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateWebhookEndpoint implements api.ServerInterface.
func (h *Handler) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateWebhookEndpointParams) {
	var in api.WebhookEndpointCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	secret, e, err := h.Webhooks.Create(r.Context(), principal(r), marinaID, webhook.CreateInput{
		URL: in.Url, Description: str(in.Description), Events: in.Events,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, e.Version)
	respond.JSON(w, http.StatusCreated, api.WebhookEndpointCreated{Endpoint: view.WebhookEndpoint(e), Secret: secret})
}

// GetWebhookEndpoint implements api.ServerInterface.
func (h *Handler) GetWebhookEndpoint(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId) {
	e, err := h.Webhooks.Get(r.Context(), principal(r), marinaID, endpointID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, e.Version)
	respond.JSON(w, http.StatusOK, view.WebhookEndpoint(e))
}

// UpdateWebhookEndpoint implements api.ServerInterface.
func (h *Handler) UpdateWebhookEndpoint(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId, params api.UpdateWebhookEndpointParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.WebhookEndpointPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	e, err := h.Webhooks.Update(r.Context(), principal(r), marinaID, endpointID, version, webhook.UpdateInput{
		URL: in.Url, Description: in.Description, Events: in.Events, Active: in.Active,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, e.Version)
	respond.JSON(w, http.StatusOK, view.WebhookEndpoint(e))
}

// DeleteWebhookEndpoint implements api.ServerInterface.
func (h *Handler) DeleteWebhookEndpoint(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId) {
	if err := h.Webhooks.Delete(r.Context(), principal(r), marinaID, endpointID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// RotateWebhookSecret implements api.ServerInterface.
func (h *Handler) RotateWebhookSecret(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId) {
	secret, until, err := h.Webhooks.RotateSecretUntil(r.Context(), principal(r), marinaID, endpointID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.WebhookSecretRotated{Secret: secret, PreviousValidUntil: until.UTC()})
}

// TestWebhookEndpoint implements api.ServerInterface.
func (h *Handler) TestWebhookEndpoint(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId, _ api.TestWebhookEndpointParams) {
	id, err := h.Webhooks.SendTest(r.Context(), principal(r), marinaID, endpointID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusAccepted, api.WebhookTestResult{DeliveryId: id})
}

// ListWebhookDeliveries implements api.ServerInterface.
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId, params api.ListWebhookDeliveriesParams) {
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	page, err := h.Webhooks.ListDeliveries(r.Context(), principal(r), marinaID, endpointID, status, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.WebhookDeliveryPage{Items: make([]api.WebhookDelivery, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Items = append(out.Items, view.WebhookDelivery(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// RedeliverWebhook implements api.ServerInterface.
func (h *Handler) RedeliverWebhook(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, endpointID api.EndpointId, deliveryID string, _ api.RedeliverWebhookParams) {
	if err := h.Webhooks.Redeliver(r.Context(), principal(r), marinaID, endpointID, deliveryID); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ---- audit ----

// ListAuditLog implements api.ServerInterface.
func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListAuditLogParams) {
	f := auditlog.Filter{EntityType: str(params.EntityType), EntityID: str(params.EntityId), ActorID: str(params.ActorId)}
	if params.From != nil {
		f.From = params.From.UTC()
	}
	if params.To != nil {
		f.To = params.To.UTC()
	}
	page, err := h.Audit.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AuditEntryPage{Items: make([]api.AuditEntry, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Items = append(out.Items, view.AuditEntry(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}
