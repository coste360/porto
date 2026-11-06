// Package team manages who can work on a marina: marina members, organization owners, invitations and password
// resets sent by an administrator. An organization always keeps at least one organization-wide owner whose user
// is active (last-owner rule, checked under LockOrganization).
package team

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Errors.
var (
	ErrAlreadyMember = errors.New("team: already a member")
	ErrLastOwner     = errors.New("team: the organization would have no owner left")
)

// Member scopes.
const (
	ScopeOrganization = "organization"
	ScopeMarina       = "marina"
)

// Member is one grant of a user, as shown in team listings.
type Member struct {
	UserID            string
	Email             string
	Name              string
	Role              auth.Role
	Scope             string // organization | marina
	Active            bool
	TOTPEnabled       bool
	InvitationPending bool
	LastLoginAt       time.Time
}

// AddMemberInput describes a new member. Locale applies only when the user does not exist yet.
type AddMemberInput struct {
	Email, Name, Locale string
	Role                auth.Role
}

// Service manages members.
type Service struct {
	db     *store.DB
	clock  clock.Clock
	auth   *auth.Service
	audit  *audit.Writer
	outbox *event.Outbox
	log    *slog.Logger
}

// NewService returns the team service.
func NewService(db *store.DB, c clock.Clock, a *auth.Service, aw *audit.Writer, ob *event.Outbox, log *slog.Logger) *Service {
	return &Service{db: db, clock: c, auth: a, audit: aw, outbox: ob, log: log}
}

func (s *Service) now() time.Time { return store.UTC(s.clock.Now()) }

func toMember(r store.MemberRow) Member {
	scope := ScopeMarina
	if r.MarinaID == "" {
		scope = ScopeOrganization
	}
	return Member{
		UserID: r.UserID, Email: r.Email, Name: r.Name, Role: auth.Role(r.Role), Scope: scope, Active: r.Active,
		TOTPEnabled: r.TOTPEnabled, InvitationPending: !r.HasPassword, LastLoginAt: r.LastLoginAt,
	}
}

// ListMembers lists the grants that apply to a marina (users.manage).
func (s *Service) ListMembers(ctx context.Context, p *auth.Principal, marinaID string) ([]Member, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.MarinaMembers(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMember(r))
	}
	return out, nil
}

func validateNewMember(in *AddMemberInput, roleRequired bool) (string, error) {
	v := &auth.ValidationError{}
	email, err := auth.NormalizeEmail(in.Email)
	if err != nil {
		v.Add("email", "invalid")
	}
	in.Name = strings.TrimSpace(in.Name)
	if !auth.ValidName(in.Name) {
		v.Add("name", "invalid")
	}
	if in.Locale == "" {
		in.Locale = "it"
	}
	if !auth.ValidLocale(in.Locale) {
		v.Add("locale", "invalid")
	}
	if roleRequired && !in.Role.Valid() {
		v.Add("role", "invalid")
	}
	return email, v.Err()
}

// findOrCreateUser returns the user with email, creating an invited user (no password) when missing.
func (s *Service) findOrCreateUser(ctx context.Context, tx bun.IDB, email string, in AddMemberInput, now time.Time) (*store.User, bool, error) {
	u, err := store.UserByEmail(ctx, tx, email)
	if err == nil {
		return u, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	u = &store.User{ID: store.NewID(), Email: email, Name: in.Name, Locale: in.Locale, Active: true, CreatedAt: now, UpdatedAt: now}
	inserted, err := store.InsertUserIfAbsent(ctx, tx, u)
	if err != nil {
		return nil, false, err
	}
	if !inserted {
		// Created concurrently: use it.
		u, err = store.UserByEmail(ctx, tx, email)
		return u, false, err
	}
	return u, true, nil
}

// inviterName returns the display name of the acting user ("" for API keys and devices without a user).
func (s *Service) inviterName(ctx context.Context, tx bun.IDB, p *auth.Principal) string {
	if p.UserID == "" {
		return ""
	}
	u, err := store.GetUser(ctx, tx, p.UserID)
	if err != nil {
		return ""
	}
	return u.Name
}

// AddMember grants a role on a marina. A new email creates an invited user and sends the invitation; an
// existing user without a password gets a new invitation.
func (s *Service) AddMember(ctx context.Context, p *auth.Principal, marinaID string, in AddMemberInput) (*Member, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	email, err := validateNewMember(&in, true)
	if err != nil {
		return nil, err
	}
	var out *Member
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		user, _, err := s.findOrCreateUser(ctx, tx, email, in, now)
		if err != nil {
			return err
		}
		if err := store.InsertMembership(ctx, tx, &store.Membership{
			ID: store.NewID(), UserID: user.ID, OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Role: string(in.Role),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrAlreadyMember
			}
			return err
		}
		if user.PasswordHash == "" {
			m, err := store.GetMarina(ctx, tx, scope)
			if err != nil {
				return err
			}
			if err := s.auth.IssueInvitation(ctx, tx, auth.InvitationInput{
				User: user, MarinaID: scope.ID(), MarinaName: m.Name, InviterName: s.inviterName(ctx, tx, p),
			}); err != nil {
				return err
			}
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "member.add",
			EntityType: "user", EntityID: user.ID, Diff: map[string]any{"role": in.Role, "scope": ScopeMarina},
		}); err != nil {
			return err
		}
		if _, err := s.outbox.Write(ctx, tx, &scope, event.MemberAdded, event.MemberData{UserID: user.ID, Role: string(in.Role), Scope: ScopeMarina}); err != nil {
			return err
		}
		out = memberOf(user, in.Role, ScopeMarina)
		return nil
	})
	return out, err
}

func memberOf(u *store.User, role auth.Role, scope string) *Member {
	return &Member{
		UserID: u.ID, Email: u.Email, Name: u.Name, Role: role, Scope: scope, Active: u.Active,
		InvitationPending: u.PasswordHash == "", LastLoginAt: u.LastLoginAt,
	}
}

// ChangeRole changes the marina-level role of a member.
func (s *Service) ChangeRole(ctx context.Context, p *auth.Principal, marinaID, userID string, role auth.Role) (*Member, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, &auth.ValidationError{Fields: []auth.FieldError{{Field: "role", Code: "invalid"}}}
	}
	var out *Member
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.MarinaMembership(ctx, tx, scope, userID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return auth.ErrNotFound
			}
			return err
		}
		user, err := store.GetUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if m.Role != string(role) {
			if err := store.UpdateMembershipRole(ctx, tx, m.ID, string(role), s.now()); err != nil {
				return err
			}
			if err := s.audit.Write(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "member.role_change",
				EntityType: "user", EntityID: userID, Diff: map[string]any{"from": m.Role, "to": role},
			}); err != nil {
				return err
			}
			if _, err := s.outbox.Write(ctx, tx, &scope, event.MemberRoleChanged, event.MemberData{UserID: userID, Role: string(role), Scope: ScopeMarina}); err != nil {
				return err
			}
		}
		out = memberOf(user, role, ScopeMarina)
		return nil
	})
	return out, err
}

// RemoveMember removes the marina-level grant of a user. Devices of that user on the marina stop working at
// their next request (their access is derived from the grants).
func (s *Service) RemoveMember(ctx context.Context, p *auth.Principal, marinaID, userID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.MarinaMembership(ctx, tx, scope, userID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return auth.ErrNotFound
			}
			return err
		}
		if err := store.DeleteMembership(ctx, tx, m.ID); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "member.remove",
			EntityType: "user", EntityID: userID, Diff: map[string]any{"role": m.Role},
		}); err != nil {
			return err
		}
		_, err = s.outbox.Write(ctx, tx, &scope, event.MemberRemoved, event.MemberData{UserID: userID, Scope: ScopeMarina})
		return err
	})
}

// SendPasswordReset sends a reset link (or a new invitation, if the user never set a password) to a user who has
// a grant on the marina.
func (s *Service) SendPasswordReset(ctx context.Context, p *auth.Principal, marinaID, userID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if !s.hasGrantOn(ctx, tx, scope, userID) {
			return auth.ErrNotFound
		}
		user, err := store.GetUser(ctx, tx, userID)
		if err != nil || !user.Active {
			return auth.ErrNotFound
		}
		if user.PasswordHash == "" {
			m, err := store.GetMarina(ctx, tx, scope)
			if err != nil {
				return err
			}
			err = s.auth.IssueInvitation(ctx, tx, auth.InvitationInput{User: user, MarinaID: scope.ID(), MarinaName: m.Name, InviterName: s.inviterName(ctx, tx, p)})
			if err != nil {
				return err
			}
		} else if err := s.auth.IssuePasswordReset(ctx, tx, user); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "member.password_reset_sent",
			EntityType: "user", EntityID: userID,
		})
	})
}

func (s *Service) hasGrantOn(ctx context.Context, tx bun.IDB, scope store.MarinaScope, userID string) bool {
	ms, err := store.MembershipsOfUser(ctx, tx, userID)
	if err != nil {
		return false
	}
	for _, m := range ms {
		if m.OrganizationID == scope.OrgID() && (m.MarinaID == "" || m.MarinaID == scope.ID()) {
			return true
		}
	}
	return false
}

// ListOrgOwners lists the organization-wide owners (organization.manage).
func (s *Service) ListOrgOwners(ctx context.Context, p *auth.Principal, orgID string) ([]Member, error) {
	scope, err := p.AuthorizeOrg(ctx, s.db.DB, orgID, auth.PermOrganizationManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.OrgOwners(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMember(r))
	}
	return out, nil
}

// AddOrgOwner makes a user an organization-wide owner (the role in the input is ignored).
func (s *Service) AddOrgOwner(ctx context.Context, p *auth.Principal, orgID string, in AddMemberInput) (*Member, error) {
	scope, err := p.AuthorizeOrg(ctx, s.db.DB, orgID, auth.PermOrganizationManage)
	if err != nil {
		return nil, err
	}
	in.Role = auth.RoleOwner
	email, err := validateNewMember(&in, false)
	if err != nil {
		return nil, err
	}
	var out *Member
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		user, _, err := s.findOrCreateUser(ctx, tx, email, in, now)
		if err != nil {
			return err
		}
		if err := store.InsertMembership(ctx, tx, &store.Membership{
			ID: store.NewID(), UserID: user.ID, OrganizationID: scope.ID(), Role: string(auth.RoleOwner),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrAlreadyMember
			}
			return err
		}
		if user.PasswordHash == "" {
			org, err := store.GetOrganization(ctx, tx, scope)
			if err != nil {
				return err
			}
			if err := s.auth.IssueInvitation(ctx, tx, auth.InvitationInput{User: user, MarinaName: org.Name, InviterName: s.inviterName(ctx, tx, p)}); err != nil {
				return err
			}
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.ID(), Actor: p.Actor(), Action: "member.add", EntityType: "user", EntityID: user.ID,
			Diff: map[string]any{"role": auth.RoleOwner, "scope": ScopeOrganization},
		}); err != nil {
			return err
		}
		if _, err := s.outbox.WriteOrg(ctx, tx, scope, event.MemberAdded, event.MemberData{UserID: user.ID, Role: string(auth.RoleOwner), Scope: ScopeOrganization}); err != nil {
			return err
		}
		out = memberOf(user, auth.RoleOwner, ScopeOrganization)
		return nil
	})
	return out, err
}

// RemoveOrgOwner removes an organization-wide owner, unless it is the last active one (ErrLastOwner). The check
// runs under LockOrganization, so concurrent removals cannot leave the organization without an owner.
func (s *Service) RemoveOrgOwner(ctx context.Context, p *auth.Principal, orgID, userID string) error {
	scope, err := p.AuthorizeOrg(ctx, s.db.DB, orgID, auth.PermOrganizationManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.LockOrganization(ctx, tx, scope.ID()); err != nil {
			return err
		}
		m, err := store.OrgMembership(ctx, tx, scope.ID(), userID)
		if err != nil || m.Role != string(auth.RoleOwner) {
			return auth.ErrNotFound
		}
		n, err := store.CountActiveOrgOwners(ctx, tx, scope.ID())
		if err != nil {
			return err
		}
		user, err := store.GetUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if user.Active && n <= 1 {
			return ErrLastOwner
		}
		if err := store.DeleteMembership(ctx, tx, m.ID); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.ID(), Actor: p.Actor(), Action: "member.remove", EntityType: "user", EntityID: userID,
			Diff: map[string]any{"role": auth.RoleOwner, "scope": ScopeOrganization},
		}); err != nil {
			return err
		}
		_, err = s.outbox.WriteOrg(ctx, tx, scope, event.MemberRemoved, event.MemberData{UserID: userID, Scope: ScopeOrganization})
		return err
	})
}
