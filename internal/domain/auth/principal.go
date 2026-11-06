package auth

import (
	"context"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/store"
)

// PrincipalKind is the kind of authenticated caller.
type PrincipalKind string

// Principal kinds.
const (
	KindUser   PrincipalKind = "user"
	KindAPIKey PrincipalKind = "api_key"
	KindDevice PrincipalKind = "device"
)

// Grant is a role on an organization (MarinaID "") or on one marina.
type Grant struct {
	OrganizationID string
	MarinaID       string
	Role           Role
}

// Principal is the authenticated caller of a request.
type Principal struct {
	Kind      PrincipalKind
	UserID    string
	SessionID string
	APIKeyID  string
	DeviceID  string
	Grants    []Grant
	// Scopes are the permissions of an API key (the key's grant has no role).
	Scopes []Permission
	// TOTPEnrolmentRequired limits a session to the enrolment endpoints (policy, m0-plan §3).
	TOTPEnrolmentRequired bool
	Locale                string
}

// grantsFor returns the grants that apply to a marina of an organization (marinaID "" = organization-level
// operation: only organization-wide grants apply).
func (p *Principal) grantsFor(orgID, marinaID string) []Grant {
	var out []Grant
	for _, g := range p.Grants {
		if g.OrganizationID != orgID {
			continue
		}
		if g.MarinaID == "" || (marinaID != "" && g.MarinaID == marinaID) {
			out = append(out, g)
		}
	}
	return out
}

// Can reports whether the principal holds perm on the marina (or, with marinaID "", on the organization).
// Grants combine: the organization-wide grant and the marina-level grant add up. organization.manage requires an
// organization-wide grant. API keys are limited to their scopes.
func (p *Principal) Can(orgID, marinaID string, perm Permission) bool {
	if p == nil || p.TOTPEnrolmentRequired {
		return false
	}
	grants := p.grantsFor(orgID, marinaID)
	if p.Kind == KindAPIKey {
		return len(grants) > 0 && APIKeyScopeAllowed(perm) && slices.Contains(p.Scopes, perm)
	}
	for _, g := range grants {
		if perm == PermOrganizationManage && g.MarinaID != "" {
			continue
		}
		if Can(g.Role, perm) {
			return true
		}
	}
	return false
}

// PermissionsOn returns every permission the principal holds on a marina.
func (p *Principal) PermissionsOn(orgID, marinaID string) []Permission {
	var out []Permission
	for _, perm := range Permissions() {
		if p.Can(orgID, marinaID, perm) {
			out = append(out, perm)
		}
	}
	return out
}

// RolesOn returns the roles that apply to a marina (organization-wide first).
func (p *Principal) RolesOn(orgID, marinaID string) []Role {
	var out []Role
	for _, g := range p.grantsFor(orgID, marinaID) {
		if g.Role != "" && !slices.Contains(out, g.Role) {
			out = append(out, g.Role)
		}
	}
	return out
}

// HasRole reports whether the principal holds role r anywhere.
func (p *Principal) HasRole(r Role) bool {
	for _, g := range p.Grants {
		if g.Role == r {
			return true
		}
	}
	return false
}

// Authorize resolves the marina and checks perm. It returns ErrNotFound when the marina does not exist, is
// archived, or the principal has no grant on it (so that existence does not leak), and ErrForbidden when a
// grant exists but lacks the permission. Every marina-scoped service method starts with it.
func (p *Principal) Authorize(ctx context.Context, db bun.IDB, marinaID string, perm Permission) (store.MarinaScope, error) {
	if p == nil {
		return store.MarinaScope{}, ErrUnauthenticated
	}
	if p.TOTPEnrolmentRequired {
		return store.MarinaScope{}, ErrTOTPEnrolmentRequired
	}
	scope, err := store.ResolveMarinaScope(ctx, db, marinaID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.MarinaScope{}, ErrNotFound
		}
		return store.MarinaScope{}, err
	}
	if len(p.grantsFor(scope.OrgID(), scope.ID())) == 0 {
		return store.MarinaScope{}, ErrNotFound
	}
	if !p.Can(scope.OrgID(), scope.ID(), perm) {
		return store.MarinaScope{}, ErrForbidden
	}
	return scope, nil
}

// AuthorizeOrg resolves the organization and checks perm against organization-wide grants. Without any grant in
// the organization the answer is ErrNotFound; with marina-level grants only, or a grant lacking perm, it is
// ErrForbidden.
func (p *Principal) AuthorizeOrg(ctx context.Context, db bun.IDB, orgID string, perm Permission) (store.OrgScope, error) {
	if p == nil {
		return store.OrgScope{}, ErrUnauthenticated
	}
	if p.TOTPEnrolmentRequired {
		return store.OrgScope{}, ErrTOTPEnrolmentRequired
	}
	scope, err := store.ResolveOrgScope(ctx, db, orgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.OrgScope{}, ErrNotFound
		}
		return store.OrgScope{}, err
	}
	if !p.InOrganization(scope.ID()) {
		return store.OrgScope{}, ErrNotFound
	}
	if !p.Can(scope.ID(), "", perm) {
		return store.OrgScope{}, ErrForbidden
	}
	return scope, nil
}

// InOrganization reports whether the principal has any grant in the organization.
func (p *Principal) InOrganization(orgID string) bool {
	for _, g := range p.Grants {
		if g.OrganizationID == orgID {
			return true
		}
	}
	return false
}

// OrgWide reports whether the principal has an organization-wide grant in orgID.
func (p *Principal) OrgWide(orgID string) bool {
	for _, g := range p.Grants {
		if g.OrganizationID == orgID && g.MarinaID == "" {
			return true
		}
	}
	return false
}

// Actor returns the audit actor of the principal.
func (p *Principal) Actor() audit.Actor {
	if p == nil {
		return audit.System
	}
	switch p.Kind {
	case KindAPIKey:
		return audit.Actor{Type: audit.ActorAPIKey, ID: p.APIKeyID}
	case KindDevice:
		return audit.Actor{Type: audit.ActorDevice, ID: p.DeviceID}
	default:
		return audit.Actor{Type: audit.ActorUser, ID: p.UserID}
	}
}

// ID is a stable identifier of the principal (rate limits, idempotency scope, SSE stream caps).
func (p *Principal) ID() string {
	switch p.Kind {
	case KindAPIKey:
		return "api_key:" + p.APIKeyID
	case KindDevice:
		return "device:" + p.DeviceID
	default:
		return "user:" + p.UserID
	}
}

type principalKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal stored in ctx.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok && p != nil
}

// grantsFromMemberships converts membership rows into grants.
func grantsFromMemberships(ms []store.Membership) []Grant {
	out := make([]Grant, 0, len(ms))
	for _, m := range ms {
		out = append(out, Grant{OrganizationID: m.OrganizationID, MarinaID: m.MarinaID, Role: Role(m.Role)})
	}
	return out
}
