package auth

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/slug"
	"github.com/coste360/porto/internal/qrsvg"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// SupportedLocales are the UI languages (Italian is the source).
var SupportedLocales = []string{"it", "en"}

// ValidLocale reports whether l is a supported UI language.
func ValidLocale(l string) bool {
	for _, s := range SupportedLocales {
		if s == l {
			return true
		}
	}
	return false
}

// ValidName checks a person or organization name: 1 to 200 characters after trimming, no control characters.
func ValidName(s string) bool {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 200 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Service) requireUser(p *Principal) error {
	if p == nil || p.Kind != KindUser || p.UserID == "" {
		return ErrForbidden
	}
	return nil
}

func userActor(userID string) audit.Actor { return audit.Actor{Type: audit.ActorUser, ID: userID} }

// ChangePassword replaces the password of the session user after checking the current one, and ends the user's
// other sessions.
func (s *Service) ChangePassword(ctx context.Context, p *Principal, current, next string) error {
	if err := s.requireUser(p); err != nil {
		return err
	}
	user, err := store.GetUser(ctx, s.db.DB, p.UserID)
	if err != nil {
		return ErrUnauthenticated
	}
	if user.PasswordHash == "" {
		return ErrInvalidCredentials
	}
	ok, err := s.hasher.Verify(current, user.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if err := store.SetUserPassword(ctx, tx, user.ID, hash, now); err != nil {
			return err
		}
		if _, err := store.DeleteUserSessions(ctx, tx, user.ID, p.SessionID); err != nil {
			return err
		}
		if err := store.InvalidateResetTokens(ctx, tx, user.ID, now); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: userActor(user.ID), Action: "auth.password_changed", EntityType: "user", EntityID: user.ID})
	})
}

// SetPassword sets a user's password without the current one (operator tools: CLI, demo seed). It ends every
// session of the user.
func (s *Service) SetPassword(ctx context.Context, userID, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if err := store.SetUserPassword(ctx, tx, userID, hash, now); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if _, err := store.DeleteUserSessions(ctx, tx, userID, ""); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: audit.System, Action: "auth.password_set", EntityType: "user", EntityID: userID})
	})
}

// UpdateProfile changes the name and/or locale of the session user.
func (s *Service) UpdateProfile(ctx context.Context, p *Principal, name, locale *string) error {
	if err := s.requireUser(p); err != nil {
		return err
	}
	user, err := store.GetUser(ctx, s.db.DB, p.UserID)
	if err != nil {
		return ErrUnauthenticated
	}
	v := &ValidationError{}
	newName, newLocale := user.Name, user.Locale
	if name != nil {
		if !ValidName(*name) {
			v.Add("name", "invalid")
		}
		newName = strings.TrimSpace(*name)
	}
	if locale != nil {
		if !ValidLocale(*locale) {
			v.Add("locale", "invalid")
		}
		newLocale = *locale
	}
	if err := v.Err(); err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.UpdateUserProfile(ctx, tx, user.ID, newName, newLocale, s.now()); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{
			Actor: userActor(user.ID), Action: "user.profile_updated", EntityType: "user", EntityID: user.ID,
			Diff: map[string]any{"locale": newLocale, "name_changed": name != nil && newName != user.Name},
		})
	})
}

// TOTPEnrolment is a pending enrolment: show the secret and the QR code once.
type TOTPEnrolment struct {
	Secret     string
	OTPAuthURL string
	QRSVG      string
}

// BeginTOTPEnrolment creates (or replaces) a pending TOTP enrolment for the session user.
func (s *Service) BeginTOTPEnrolment(ctx context.Context, p *Principal) (*TOTPEnrolment, error) {
	if err := s.requireUser(p); err != nil {
		return nil, err
	}
	user, err := store.GetUser(ctx, s.db.DB, p.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	enc, err := s.keys.EncryptString(secret, "totp:"+user.ID)
	if err != nil {
		return nil, err
	}
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		err := store.ReplacePendingTOTP(ctx, tx, &store.UserTOTP{
			ID: store.NewID(), UserID: user.ID, SecretEnc: enc, CreatedAt: now, UpdatedAt: now,
		})
		if errors.Is(err, store.ErrConflict) {
			return ErrTOTPAlreadyEnabled
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	u := otpauthURL(secret, user.Email)
	svg, err := qrsvg.Render(u)
	if err != nil {
		return nil, err
	}
	return &TOTPEnrolment{Secret: secret, OTPAuthURL: u, QRSVG: svg}, nil
}

// ConfirmTOTP activates a pending enrolment with a valid code, returns 10 fresh recovery codes, ends every
// session of the user and opens a new one (newToken replaces the cookie).
func (s *Service) ConfirmTOTP(ctx context.Context, p *Principal, code string) (recoveryCodes []string, newToken string, err error) {
	if err := s.requireUser(p); err != nil {
		return nil, "", err
	}
	t, err := store.GetTOTP(ctx, s.db.DB, p.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", ErrTOTPNotEnrolled
	}
	if err != nil {
		return nil, "", err
	}
	if !t.ConfirmedAt.IsZero() {
		return nil, "", ErrTOTPAlreadyEnabled
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, "", err
	}
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if err := s.verifyTOTP(ctx, tx, p.UserID, t, code, now); err != nil {
			return err
		}
		if err := store.ConfirmTOTP(ctx, tx, p.UserID, now); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrTOTPAlreadyEnabled
			}
			return err
		}
		if err := store.ReplaceRecoveryCodes(ctx, tx, p.UserID, hashes, now); err != nil {
			return err
		}
		// End every session of the user, not only the current one: sessions opened with the password alone
		// must not survive the second factor being turned on.
		if _, err := store.DeleteUserSessions(ctx, tx, p.UserID, ""); err != nil {
			return err
		}
		tok, _, err := s.newSession(ctx, tx, p.UserID, true, now)
		if err != nil {
			return err
		}
		newToken = tok
		return s.audit.Write(ctx, tx, audit.Entry{Actor: userActor(p.UserID), Action: "auth.totp_enabled", EntityType: "user", EntityID: p.UserID})
	})
	if err != nil {
		return nil, "", err
	}
	return codes, newToken, nil
}

// totpRequiredFor reports whether policy forbids the user from disabling TOTP.
func (s *Service) totpRequiredFor(p *Principal) bool {
	return s.opts.RequireTOTPForOwners && s.policyRequiresTOTP(p)
}

// DisableTOTP removes TOTP after checking password and a current code.
func (s *Service) DisableTOTP(ctx context.Context, p *Principal, password, code string) error {
	if err := s.requireUser(p); err != nil {
		return err
	}
	if s.totpRequiredFor(p) {
		return ErrTOTPRequiredByPolicy
	}
	user, err := store.GetUser(ctx, s.db.DB, p.UserID)
	if err != nil {
		return ErrUnauthenticated
	}
	ok, err := s.hasher.Verify(password, orDummy(user.PasswordHash, s.dummy))
	if err != nil {
		return err
	}
	if !ok || user.PasswordHash == "" {
		return ErrInvalidCredentials
	}
	t, err := store.GetTOTP(ctx, s.db.DB, user.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && t.ConfirmedAt.IsZero()) {
		return ErrTOTPNotEnrolled
	}
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.verifyTOTP(ctx, tx, user.ID, t, code, s.now()); err != nil {
			return err
		}
		if err := store.DeleteTOTP(ctx, tx, user.ID); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: userActor(user.ID), Action: "auth.totp_disabled", EntityType: "user", EntityID: user.ID})
	})
}

func orDummy(hash, dummy string) string {
	if hash == "" {
		return dummy
	}
	return hash
}

// RegenerateRecoveryCodes replaces the recovery codes after checking a current TOTP code.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p *Principal, code string) ([]string, error) {
	if err := s.requireUser(p); err != nil {
		return nil, err
	}
	t, err := store.GetTOTP(ctx, s.db.DB, p.UserID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && t.ConfirmedAt.IsZero()) {
		return nil, ErrTOTPNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if err := s.verifyTOTP(ctx, tx, p.UserID, t, code, now); err != nil {
			return err
		}
		if err := store.ReplaceRecoveryCodes(ctx, tx, p.UserID, hashes, now); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: userActor(p.UserID), Action: "auth.recovery_codes_regenerated", EntityType: "user", EntityID: p.UserID})
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// RequestPasswordReset sends a reset link to an active account. The answer is the same whether or not the
// account exists; only the rate limit (per account and per IP prefix, applied to every email) is reported.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	info := reqctx.From(ctx)
	if ok, wait := s.resets.take(info.ClientIP, email); !ok {
		return &RateLimitError{RetryAfter: wait}
	}
	norm, err := NormalizeEmail(email)
	if err != nil {
		return &ValidationError{Fields: []FieldError{{Field: "email", Code: "invalid"}}}
	}
	user, err := store.UserByEmail(ctx, s.db.DB, norm)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !user.Active {
		return nil
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.IssuePasswordReset(ctx, tx, user); err != nil {
			return err
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: audit.System, Action: "auth.password_reset_requested", EntityType: "user", EntityID: user.ID})
	})
}

// IssuePasswordReset creates a reset token (1 h) and queues the mail, in tx.
func (s *Service) IssuePasswordReset(ctx context.Context, tx bun.IDB, user *store.User) error {
	token, expires, err := s.newResetToken(ctx, tx, user.ID, store.PurposeReset, ResetTokenTTL)
	if err != nil {
		return err
	}
	return s.mailer.EnqueuePasswordReset(ctx, tx, notify.PasswordResetMail{
		UserID: user.ID, Email: user.Email, Name: user.Name, Locale: user.Locale, Token: token, ExpiresAt: expires,
	})
}

// InvitationInput describes an invitation to a marina.
type InvitationInput struct {
	User        *store.User
	MarinaID    string
	MarinaName  string
	InviterName string
}

// IssueInvitation creates an invitation token (7 days) and queues the mail, in tx. Accepting the invitation is a
// password reset with purpose "invite".
func (s *Service) IssueInvitation(ctx context.Context, tx bun.IDB, in InvitationInput) error {
	token, expires, err := s.newResetToken(ctx, tx, in.User.ID, store.PurposeInvite, InviteTokenTTL)
	if err != nil {
		return err
	}
	return s.mailer.EnqueueInvitation(ctx, tx, notify.InvitationMail{
		UserID: in.User.ID, Email: in.User.Email, Name: in.User.Name, Locale: in.User.Locale, Token: token,
		MarinaID: in.MarinaID, MarinaName: in.MarinaName, InviterName: in.InviterName, ExpiresAt: expires,
	})
}

func (s *Service) newResetToken(ctx context.Context, tx bun.IDB, userID, purpose string, ttl time.Duration) (string, time.Time, error) {
	token, err := crypto.RandomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	row := &store.PasswordResetToken{
		ID: store.NewID(), UserID: userID, TokenHash: crypto.SHA256Hex(token), Purpose: purpose,
		ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertResetToken(ctx, tx, row); err != nil {
		return "", time.Time{}, err
	}
	return token, row.ExpiresAt, nil
}

// ConfirmPasswordReset sets a new password with a reset or invitation token. The token is single use; every
// session of the user ends and the user's other tokens are invalidated.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if token == "" {
		return ErrInvalidResetToken
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		t, err := store.ConsumeResetToken(ctx, tx, crypto.SHA256Hex(token), now)
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidResetToken
		}
		if err != nil {
			return err
		}
		user, err := store.GetUser(ctx, tx, t.UserID)
		if err != nil || !user.Active {
			return ErrInvalidResetToken
		}
		if err := store.SetUserPassword(ctx, tx, user.ID, hash, now); err != nil {
			return err
		}
		if _, err := store.DeleteUserSessions(ctx, tx, user.ID, ""); err != nil {
			return err
		}
		if err := store.InvalidateResetTokens(ctx, tx, user.ID, now); err != nil {
			return err
		}
		action := "auth.password_reset"
		if t.Purpose == store.PurposeInvite {
			action = "auth.invitation_accepted"
		}
		return s.audit.Write(ctx, tx, audit.Entry{Actor: userActor(user.ID), Action: action, EntityType: "user", EntityID: user.ID})
	})
}

// CreateOwnerInput describes the first owner of a new organization (CLI `admin create`, bootstrap on serve).
type CreateOwnerInput struct {
	Email, Name, Password, Organization string
	// Optional first marina.
	MarinaName, MarinaSlug, Timezone, Locale string
}

// CreateOwnerResult identifies what CreateOwner made.
type CreateOwnerResult struct {
	UserID, OrganizationID, MarinaID string
}

// CreateOwner creates a user with a password, a new organization, the organization-wide owner grant and,
// optionally, the first marina, in one transaction.
func (s *Service) CreateOwner(ctx context.Context, in CreateOwnerInput) (*CreateOwnerResult, error) {
	v := &ValidationError{}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		v.Add("email", "invalid")
	}
	if !ValidName(in.Name) {
		v.Add("name", "invalid")
	}
	if !ValidName(in.Organization) {
		v.Add("organization", "invalid")
	}
	locale := in.Locale
	if locale == "" {
		locale = "it"
	}
	if !ValidLocale(locale) {
		v.Add("locale", "invalid")
	}
	tz := in.Timezone
	if tz == "" {
		tz = "Europe/Rome"
	}
	if in.MarinaName != "" || in.MarinaSlug != "" {
		if !ValidName(in.MarinaName) {
			v.Add("marina_name", "invalid")
		}
		if slug.Validate(in.MarinaSlug) != nil {
			v.Add("marina_slug", "invalid")
		}
		if _, err := calendar.LoadLocation(tz); err != nil {
			v.Add("timezone", "invalid")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}
	res := &CreateOwnerResult{}
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		user := &store.User{
			ID: store.NewID(), Email: email, PasswordHash: hash, Name: strings.TrimSpace(in.Name), Locale: locale,
			Active: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertUser(ctx, tx, user); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrUserExists
			}
			return err
		}
		org := &store.Organization{
			ID: store.NewID(), Name: strings.TrimSpace(in.Organization), BillingAddress: store.JSON(`{}`), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertOrganization(ctx, tx, org); err != nil {
			return err
		}
		if err := store.InsertMembership(ctx, tx, &store.Membership{
			ID: store.NewID(), UserID: user.ID, OrganizationID: org.ID, Role: string(RoleOwner), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		res.UserID, res.OrganizationID = user.ID, org.ID
		if in.MarinaSlug != "" {
			m := NewMarinaRow(org.ID, strings.TrimSpace(in.MarinaName), in.MarinaSlug, tz, locale, now)
			if err := store.InsertMarina(ctx, tx, m); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return &ValidationError{Fields: []FieldError{{Field: "marina_slug", Code: "taken"}}}
				}
				return err
			}
			res.MarinaID = m.ID
		}
		return s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: org.ID, MarinaID: res.MarinaID, Actor: audit.System, Action: "organization.bootstrap",
			EntityType: "organization", EntityID: org.ID,
			Diff: map[string]any{"owner_user_id": user.ID, "marina_id": res.MarinaID},
		})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// NewMarinaRow returns a marina row with Porto's defaults (kind marina, EUR, one locale).
func NewMarinaRow(orgID, name, slugValue, timezone, locale string, now time.Time) *store.Marina {
	return &store.Marina{
		ID: store.NewID(), OrganizationID: orgID, Slug: slugValue, Name: name, Kind: "marina", Timezone: timezone,
		Currency: "EUR", DefaultLocale: locale, Locales: store.Strings{locale}, Contacts: store.JSON(`{}`),
		Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1, Theme: store.JSON(`{}`),
		AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// MeOrganization is an organization the principal belongs to.
type MeOrganization struct {
	ID, Name string
	Role     Role // organization-wide role, "" for marina-level members
}

// MeMarina is a marina the principal can open.
type MeMarina struct {
	ID, OrganizationID, Slug, Name string
	Roles                          []Role
	Permissions                    []Permission
}

// MeInfo is the account view of the session user (GET /auth/me).
type MeInfo struct {
	User                  store.User
	TOTPEnabled           bool
	TOTPEnrolmentRequired bool
	Organizations         []MeOrganization
	Marinas               []MeMarina
}

// Me describes the principal: user, organizations and marinas with roles and permissions.
func (s *Service) Me(ctx context.Context, p *Principal) (*MeInfo, error) {
	if p == nil || p.UserID == "" {
		return nil, ErrUnauthenticated
	}
	user, err := store.GetUser(ctx, s.db.DB, p.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	info := &MeInfo{User: *user, TOTPEnrolmentRequired: p.TOTPEnrolmentRequired}
	if t, err := store.GetTOTP(ctx, s.db.DB, user.ID); err == nil && !t.ConfirmedAt.IsZero() {
		info.TOTPEnabled = true
	}
	var orgIDs, wideOrgs, marinaIDs []string
	seen := map[string]bool{}
	for _, g := range p.Grants {
		if !seen[g.OrganizationID] {
			seen[g.OrganizationID] = true
			orgIDs = append(orgIDs, g.OrganizationID)
		}
		if g.MarinaID == "" {
			wideOrgs = append(wideOrgs, g.OrganizationID)
		} else {
			marinaIDs = append(marinaIDs, g.MarinaID)
		}
	}
	orgs, err := store.OrganizationsByIDs(ctx, s.db.DB, orgIDs)
	if err != nil {
		return nil, err
	}
	for _, o := range orgs {
		mo := MeOrganization{ID: o.ID, Name: o.Name}
		for _, g := range p.Grants {
			if g.OrganizationID == o.ID && g.MarinaID == "" {
				mo.Role = g.Role
			}
		}
		info.Organizations = append(info.Organizations, mo)
	}
	wide, err := store.ActiveMarinasOfOrganizations(ctx, s.db.DB, wideOrgs)
	if err != nil {
		return nil, err
	}
	direct, err := store.ActiveMarinasByIDs(ctx, s.db.DB, marinaIDs)
	if err != nil {
		return nil, err
	}
	added := map[string]bool{}
	for _, m := range append(wide, direct...) {
		if added[m.ID] {
			continue
		}
		added[m.ID] = true
		info.Marinas = append(info.Marinas, MeMarina{
			ID: m.ID, OrganizationID: m.OrganizationID, Slug: m.Slug, Name: m.Name,
			Roles: p.RolesOn(m.OrganizationID, m.ID), Permissions: p.PermissionsOn(m.OrganizationID, m.ID),
		})
	}
	return info, nil
}
