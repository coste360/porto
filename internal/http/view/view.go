// Package view converts domain values into the generated API models (and back for the few request types that
// need it). It holds no logic beyond the shape of the wire format.
package view

import (
	"encoding/json"
	"time"

	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auditlog"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/reqctx"
)

// Time returns nil for the zero instant.
func Time(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// String returns nil for "".
func String(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Int returns nil for 0.
func Int(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Roles converts roles.
func Roles(rs []auth.Role) []api.Role {
	out := make([]api.Role, 0, len(rs))
	for _, r := range rs {
		out = append(out, api.Role(r))
	}
	return out
}

// Permissions converts permissions.
func Permissions(ps []auth.Permission) []api.Permission {
	out := make([]api.Permission, 0, len(ps))
	for _, p := range ps {
		out = append(out, api.Permission(p))
	}
	return out
}

// PermissionsIn converts API permissions to domain permissions (validated by the services).
func PermissionsIn(ps []api.Permission) []auth.Permission {
	out := make([]auth.Permission, 0, len(ps))
	for _, p := range ps {
		out = append(out, auth.Permission(p))
	}
	return out
}

// Locales converts locales.
func Locales(ls []string) []api.Locale {
	out := make([]api.Locale, 0, len(ls))
	for _, l := range ls {
		out = append(out, api.Locale(l))
	}
	return out
}

// LocalesIn converts API locales.
func LocalesIn(ls []api.Locale) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, string(l))
	}
	return out
}

// Me converts the account view.
func Me(m *auth.MeInfo, method reqctx.AuthMethod) api.Me {
	out := api.Me{
		AuthMethod:            api.MeAuthMethod(method),
		TotpEnrolmentRequired: m.TOTPEnrolmentRequired,
		User: api.MeUser{
			Id: m.User.ID, Email: m.User.Email, Name: m.User.Name, Locale: api.Locale(m.User.Locale),
			TotpEnabled: m.TOTPEnabled,
		},
		Organizations: []api.MeOrganization{},
		Marinas:       []api.MeMarina{},
	}
	if method == reqctx.AuthNone {
		out.AuthMethod = api.MeAuthMethodSession
	}
	for _, o := range m.Organizations {
		mo := api.MeOrganization{Id: o.ID, Name: o.Name}
		if o.Role != "" {
			r := api.Role(o.Role)
			mo.Role = &r
		}
		out.Organizations = append(out.Organizations, mo)
	}
	for _, mm := range m.Marinas {
		out.Marinas = append(out.Marinas, api.MeMarina{
			Id: mm.ID, OrganizationId: mm.OrganizationID, Slug: mm.Slug, Name: mm.Name,
			Roles: Roles(mm.Roles), Permissions: Permissions(mm.Permissions),
		})
	}
	return out
}

// Address converts an address (every field present, possibly empty).
func Address(a marina.Address) api.Address {
	return api.Address{
		Line1: &a.Line1, Line2: &a.Line2, PostalCode: &a.PostalCode, City: &a.City, Province: &a.Province,
		Country: &a.Country,
	}
}

// AddressIn converts a request address (absent fields are empty).
func AddressIn(a *api.Address) *marina.Address {
	if a == nil {
		return nil
	}
	return &marina.Address{
		Line1: deref(a.Line1), Line2: deref(a.Line2), PostalCode: deref(a.PostalCode), City: deref(a.City),
		Province: deref(a.Province), Country: deref(a.Country),
	}
}

// Organization converts an organization.
func Organization(o *marina.Organization) api.Organization {
	return api.Organization{
		Id: o.ID, Name: o.Name, VatNumber: o.VATNumber, FiscalCode: o.FiscalCode, BillingAddress: Address(o.BillingAddress),
		SdiCode: o.SDICode, Pec: o.PEC, Version: o.Version, CreatedAt: o.CreatedAt.UTC(), UpdatedAt: o.UpdatedAt.UTC(),
	}
}

func object(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = map[string]any{}
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Marina converts a marina.
func Marina(m *marina.Marina) api.Marina {
	c := m.Contacts
	out := api.Marina{
		Id: m.ID, OrganizationId: m.OrganizationID, Slug: m.Slug, Name: m.Name, Kind: api.MarinaKind(m.Kind),
		Timezone: m.Timezone, Currency: m.Currency, DefaultLocale: api.Locale(m.DefaultLocale), Locales: Locales(m.Locales),
		Contacts:      api.MarinaContacts{Email: &c.Email, Phone: &c.Phone, Website: &c.Website, VhfChannel: &c.VHFChannel},
		Address:       Address(m.Address),
		ConcessionRef: m.ConcessionRef, Settings: object(m.Settings), Theme: object(m.Theme),
		BookingEnabled: m.BookingEnabled, AllowedEmbedOrigins: nonNil(m.AllowedEmbedOrigins), Version: m.Version,
		ArchivedAt: Time(m.ArchivedAt), CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
	if m.Geo != nil {
		out.Geo = &api.GeoPoint{LatE7: m.Geo.LatE7, LonE7: m.Geo.LonE7}
	}
	if !m.ConcessionExpiresOn.IsZero() {
		s := m.ConcessionExpiresOn.String()
		out.ConcessionExpiresOn = &s
	}
	return out
}

// Member converts a member.
func Member(m *team.Member) api.Member {
	return api.Member{
		UserId: m.UserID, Email: m.Email, Name: m.Name, Role: api.Role(m.Role), Scope: api.MemberScope(m.Scope),
		Active: m.Active, TotpEnabled: m.TOTPEnabled, InvitationPending: m.InvitationPending, LastLoginAt: Time(m.LastLoginAt),
	}
}

// Members converts a member list.
func Members(ms []team.Member) api.MemberList {
	out := api.MemberList{Items: make([]api.Member, 0, len(ms))}
	for i := range ms {
		out.Items = append(out.Items, Member(&ms[i]))
	}
	return out
}

// APIKey converts an API key.
func APIKey(k *apikey.APIKey) api.ApiKey {
	return api.ApiKey{
		Id: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: Permissions(k.Scopes), CreatedBy: String(k.CreatedBy),
		ExpiresAt: Time(k.ExpiresAt), LastUsedAt: Time(k.LastUsedAt), RevokedAt: Time(k.RevokedAt), CreatedAt: k.CreatedAt.UTC(),
	}
}

// Device converts a device.
func Device(d *device.Device) api.Device {
	return api.Device{
		Id: d.ID, Name: d.Name, Platform: api.DevicePlatform(d.Platform), Kind: api.DeviceKind(d.Kind),
		AppVersion: d.AppVersion, User: api.UserRef{Id: d.UserID, Name: d.UserName}, TokenPrefix: d.TokenPrefix,
		LastUsedAt: Time(d.LastUsedAt), RevokedAt: Time(d.RevokedAt), CreatedAt: d.CreatedAt.UTC(),
	}
}

// DeviceSelf converts the device self view.
func DeviceSelf(s *device.SelfInfo) api.DeviceSelf {
	return api.DeviceSelf{
		Device:      Device(&s.Device),
		User:        api.UserRef{Id: s.UserID, Name: s.UserName},
		Marina:      api.MarinaRef{Id: s.MarinaID, Slug: s.MarinaSlug, Name: s.MarinaName},
		Permissions: Permissions(s.Permissions),
	}
}

// WebhookEndpoint converts an endpoint.
func WebhookEndpoint(e *webhook.Endpoint) api.WebhookEndpoint {
	return api.WebhookEndpoint{
		Id: e.ID, Url: e.URL, Description: e.Description, Events: nonNil(e.Events), Active: e.Active,
		ConsecutiveFailures: e.ConsecutiveFailures, DisabledAt: Time(e.DisabledAt), DisabledReason: String(e.DisabledReason),
		HasPreviousSecret: e.HasPreviousSecret, Version: e.Version, CreatedAt: e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC(),
	}
}

// WebhookDelivery converts a delivery.
func WebhookDelivery(d *webhook.Delivery) api.WebhookDelivery {
	out := api.WebhookDelivery{
		Id: d.ID, EventId: d.EventID, EventType: d.EventType, Status: api.WebhookDeliveryStatus(d.Status),
		Attempts: d.Attempts, LastStatusCode: Int(d.LastStatusCode), LastError: String(d.LastError),
		ResponseExcerpt: String(d.ResponseExcerpt), DeliveredAt: Time(d.DeliveredAt), FailedAt: Time(d.FailedAt),
		CreatedAt: d.CreatedAt.UTC(),
	}
	if d.Status == "pending" {
		out.NextAttemptAt = Time(d.NextAttemptAt)
	}
	return out
}

// AuditEntry converts an audit row (the IP address is never exposed).
func AuditEntry(e *auditlog.Entry) api.AuditEntry {
	var diff any
	if len(e.Diff) > 0 {
		_ = json.Unmarshal(e.Diff, &diff)
	}
	return api.AuditEntry{
		Id: e.ID, Actor: api.AuditActor{Type: api.AuditActorType(e.ActorType), Id: String(e.ActorID)}, Action: e.Action,
		EntityType: e.EntityType, EntityId: String(e.EntityID), Diff: diff, RequestId: e.RequestID, CreatedAt: e.CreatedAt.UTC(),
	}
}
