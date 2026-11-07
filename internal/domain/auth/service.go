// Package auth authenticates users, API keys and devices, and holds the role/permission model (spec 26). Every
// domain service authorises through Principal.Authorize; the HTTP layer only authenticates.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// Session lifetimes and token TTLs (m0-plan §5).
const (
	SessionIdle     = 24 * time.Hour
	SessionAbsolute = 14 * 24 * time.Hour
	ResetTokenTTL   = time.Hour
	InviteTokenTTL  = 7 * 24 * time.Hour
	touchInterval   = time.Minute
)

// Token prefixes.
const (
	APIKeyPrefix      = "po_live_"
	DeviceTokenPrefix = "po_dev_"
)

// Mailer queues account emails in the caller's transaction (implemented by notify.Service).
type Mailer interface {
	EnqueuePasswordReset(ctx context.Context, tx bun.IDB, in notify.PasswordResetMail) error
	EnqueueInvitation(ctx context.Context, tx bun.IDB, in notify.InvitationMail) error
}

// Options tune the service.
type Options struct {
	// RequireTOTPForOwners forces owner and accounting users to enrol TOTP (PORTO_REQUIRE_TOTP_FOR_OWNERS).
	RequireTOTPForOwners bool
	// Argon2 overrides the hashing cost (tests); zero means DefaultArgon2Params.
	Argon2 Argon2Params
}

// Service is the authentication service.
type Service struct {
	db       *store.DB
	clock    clock.Clock
	keys     *crypto.KeyRing
	audit    *audit.Writer
	mailer   Mailer
	log      *slog.Logger
	opts     Options
	hasher   *Hasher
	dummy    string
	throttle *LoginThrottle
	resets   *resetThrottle
}

// NewService returns the auth service.
func NewService(db *store.DB, c clock.Clock, keys *crypto.KeyRing, aw *audit.Writer, n Mailer, log *slog.Logger, o Options) (*Service, error) {
	params := o.Argon2
	if params == (Argon2Params{}) {
		params = DefaultArgon2Params
	}
	h := &Hasher{Params: params}
	dummy, err := h.Hash("porto dummy password for unknown accounts")
	if err != nil {
		return nil, err
	}
	return &Service{
		db: db, clock: c, keys: keys, audit: aw, mailer: n, log: log, opts: o, hasher: h, dummy: dummy,
		throttle: NewLoginThrottle(c), resets: newResetThrottle(c),
	}, nil
}

// Hasher returns the password hasher (CLI, seed).
func (s *Service) Hasher() *Hasher { return s.hasher }

func (s *Service) now() time.Time { return store.UTC(s.clock.Now()) }

// NormalizeEmail lowercases and validates an email address (bare address, 3 to 254 characters).
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if len(e) < 3 || len(e) > 254 {
		return "", ErrInvalidInput
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || a.Name != "" {
		return "", ErrInvalidInput
	}
	return e, nil
}

// LoginInput is a login attempt.
type LoginInput struct {
	Email, Password, TOTPCode, RecoveryCode string
}

// LoginResult is a successful login: Token goes into the porto_session cookie.
type LoginResult struct {
	Token     string
	Principal *Principal
	ExpiresAt time.Time
}

// Login checks credentials (and the second factor when enrolled) and opens a new session. Unknown emails, users
// without a password and inactive users get the same error after the same work (a dummy hash verification).
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	info := reqctx.From(ctx)
	if ok, wait := s.throttle.Check(info.ClientIP, in.Email); !ok {
		return nil, &RateLimitError{RetryAfter: wait}
	}
	defer s.throttle.Release(in.Email)
	email, emailErr := NormalizeEmail(in.Email)
	var user *store.User
	if emailErr == nil {
		u, err := store.UserByEmail(ctx, s.db.DB, email)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		user = u
	}
	hash := s.dummy
	if user != nil && user.Active && user.PasswordHash != "" {
		hash = user.PasswordHash
	}
	ok, err := s.hasher.Verify(in.Password, hash)
	if err != nil {
		return nil, fmt.Errorf("auth: verify: %w", err)
	}
	if !ok || hash == s.dummy {
		s.loginFailed(ctx, info, in.Email, user)
		return nil, ErrInvalidCredentials
	}

	totp, err := store.GetTOTP(ctx, s.db.DB, user.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	totpEnabled := totp != nil && !totp.ConfirmedAt.IsZero()

	var result *LoginResult
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if totpEnabled {
			if err := s.checkSecondFactor(ctx, tx, user.ID, totp, in.TOTPCode, in.RecoveryCode, now); err != nil {
				return err
			}
		}
		if s.hasher.NeedsRehash(user.PasswordHash) {
			if h, err := s.hasher.Hash(in.Password); err == nil {
				if err := store.RehashUserPassword(ctx, tx, user.ID, h, now); err != nil {
					return err
				}
			}
		}
		token, sess, err := s.newSession(ctx, tx, user.ID, totpEnabled, now)
		if err != nil {
			return err
		}
		if err := store.TouchUserLogin(ctx, tx, user.ID, now); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			Actor: audit.Actor{Type: audit.ActorUser, ID: user.ID}, Action: "auth.login", EntityType: "user", EntityID: user.ID,
		}); err != nil {
			return err
		}
		p, err := s.userPrincipal(ctx, tx, user, sess.ID, totpEnabled)
		if err != nil {
			return err
		}
		result = &LoginResult{Token: token, Principal: p, ExpiresAt: sess.ExpiresAt}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidTOTP) {
			s.loginFailed(ctx, info, in.Email, user)
		}
		return nil, err
	}
	s.throttle.Success(in.Email)
	return result, nil
}

func (s *Service) loginFailed(ctx context.Context, info *reqctx.Info, account string, user *store.User) {
	s.throttle.Failure(info.ClientIP, account)
	e := audit.Entry{Actor: audit.System, Action: "auth.login_failed", EntityType: "user"}
	if user != nil {
		e.EntityID = user.ID
	}
	if err := s.audit.Write(ctx, s.db.DB, e); err != nil {
		s.log.ErrorContext(ctx, "audit login failure", "error", err)
	}
}

// checkSecondFactor verifies a TOTP code (replay-safe) or consumes a recovery code.
func (s *Service) checkSecondFactor(ctx context.Context, tx bun.IDB, userID string, t *store.UserTOTP, code, recovery string, now time.Time) error {
	switch {
	case strings.TrimSpace(code) != "":
		return s.verifyTOTP(ctx, tx, userID, t, code, now)
	case strings.TrimSpace(recovery) != "":
		ok, err := store.UseRecoveryCode(ctx, tx, userID, hashRecoveryCode(recovery), now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidTOTP
		}
		return nil
	default:
		return ErrTOTPRequired
	}
}

// verifyTOTP checks code against the user's secret and records the step, so a code is accepted once.
func (s *Service) verifyTOTP(ctx context.Context, tx bun.IDB, userID string, t *store.UserTOTP, code string, now time.Time) error {
	secret, err := s.keys.DecryptString(t.SecretEnc, "totp:"+userID)
	if err != nil {
		return fmt.Errorf("auth: decrypt totp secret: %w", err)
	}
	step, ok := matchTOTP(secret, code, now)
	if !ok {
		return ErrInvalidTOTP
	}
	advanced, err := store.AdvanceTOTPStep(ctx, tx, userID, step, now)
	if err != nil {
		return err
	}
	if !advanced {
		return ErrInvalidTOTP // replay of an already used (or older) code
	}
	return nil
}

func (s *Service) newSession(ctx context.Context, tx bun.IDB, userID string, totpVerified bool, now time.Time) (string, *store.Session, error) {
	return insertSession(ctx, tx, sessionSpec{UserID: userID, TOTPVerified: totpVerified, Idle: SessionIdle, Absolute: SessionAbsolute}, now)
}

// sessionSpec describes a session to open: an ordinary login, or a device session (DeviceID set, longer
// lifetimes, M6a).
type sessionSpec struct {
	UserID, DeviceID string
	TOTPVerified     bool
	Idle, Absolute   time.Duration
}

func insertSession(ctx context.Context, tx bun.IDB, spec sessionSpec, now time.Time) (string, *store.Session, error) {
	token, err := crypto.RandomToken(32)
	if err != nil {
		return "", nil, err
	}
	info := reqctx.From(ctx)
	sess := &store.Session{
		ID: store.NewID(), TokenHash: crypto.SHA256Hex(token), UserID: spec.UserID, TOTPVerified: spec.TOTPVerified,
		ExpiresAt: now.Add(spec.Absolute), IdleExpiresAt: now.Add(spec.Idle), LastSeenAt: now, DeviceID: spec.DeviceID,
		UserAgent: reqctx.TruncateUserAgent(info.UserAgent), CreatedAt: now, UpdatedAt: now,
	}
	if info.ClientIP.IsValid() {
		sess.IP = info.ClientIP.String()
	}
	if err := store.InsertSession(ctx, tx, sess); err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

// Logout ends the session of token (no error when it does not exist).
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return store.DeleteSessionByTokenHash(ctx, s.db.DB, crypto.SHA256Hex(token))
}

// AuthenticateSession returns the principal of a session token and slides its idle expiry.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, ErrUnauthenticated
	}
	sess, err := store.SessionByTokenHash(ctx, s.db.DB, crypto.SHA256Hex(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) || !now.Before(sess.IdleExpiresAt) {
		_ = store.DeleteSession(ctx, s.db.DB, sess.ID)
		return nil, ErrUnauthenticated
	}
	user, err := store.GetUser(ctx, s.db.DB, sess.UserID)
	if err != nil || !user.Active {
		return nil, ErrUnauthenticated
	}
	if sess.DeviceID != "" {
		return s.authenticateDeviceSession(ctx, sess, user, now)
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		idle := now.Add(SessionIdle)
		if idle.After(sess.ExpiresAt) {
			idle = sess.ExpiresAt
		}
		if err := store.TouchSession(ctx, s.db.DB, sess.ID, now, idle); err != nil {
			s.log.WarnContext(ctx, "touch session", "error", err)
		}
	}
	return s.userPrincipal(ctx, s.db.DB, user, sess.ID, sess.TOTPVerified)
}

// userPrincipal builds the principal of a user from their memberships.
func (s *Service) userPrincipal(ctx context.Context, db bun.IDB, u *store.User, sessionID string, totpVerified bool) (*Principal, error) {
	ms, err := store.MembershipsOfUser(ctx, db, u.ID)
	if err != nil {
		return nil, err
	}
	p := &Principal{Kind: KindUser, UserID: u.ID, SessionID: sessionID, Grants: grantsFromMemberships(ms), Locale: u.Locale}
	if s.opts.RequireTOTPForOwners && s.policyRequiresTOTP(p) && !totpVerified {
		t, err := store.GetTOTP(ctx, db, u.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if t == nil || t.ConfirmedAt.IsZero() {
			p.TOTPEnrolmentRequired = true
		}
	}
	return p, nil
}

func (s *Service) policyRequiresTOTP(p *Principal) bool {
	for _, r := range requiresTOTPByPolicy {
		if p.HasRole(r) {
			return true
		}
	}
	return false
}

// parseToken splits "<prefix><8 chars>_<secret>" and returns the lookup prefix ("po_live_xxxxxxxx").
func parseToken(token, prefix string) (lookup string, ok bool) {
	rest, found := strings.CutPrefix(token, prefix)
	if !found || len(rest) < 10 || rest[8] != '_' {
		return "", false
	}
	return prefix + rest[:8], true
}

// AuthenticateAPIKey returns the principal of an API key (Authorization: Bearer po_live_…).
func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (*Principal, error) {
	prefix, ok := parseToken(key, APIKeyPrefix)
	if !ok {
		return nil, ErrUnauthenticated
	}
	k, err := store.APIKeyByPrefix(ctx, s.db.DB, prefix)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	now := s.now()
	if !crypto.ConstantTimeEqual(k.KeyHash, crypto.SHA256Hex(key)) || !k.RevokedAt.IsZero() ||
		(!k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt)) {
		return nil, ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.db.DB, k.MarinaID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if k.LastUsedAt.IsZero() || now.Sub(k.LastUsedAt) >= touchInterval {
		if err := store.TouchAPIKey(ctx, s.db.DB, k.ID, now); err != nil {
			s.log.WarnContext(ctx, "touch api key", "error", err)
		}
	}
	scopes := make([]Permission, 0, len(k.Scopes))
	for _, sc := range k.Scopes {
		if p := Permission(sc); APIKeyScopeAllowed(p) {
			scopes = append(scopes, p)
		}
	}
	return &Principal{
		Kind: KindAPIKey, APIKeyID: k.ID, UserID: k.CreatedByUserID,
		Grants: []Grant{{OrganizationID: scope.OrgID(), MarinaID: scope.ID()}},
		Scopes: scopes,
	}, nil
}

// AuthenticateDevice returns the principal of a device token (Bearer po_dev_…): the user's current grants,
// narrowed to the device's marina. A user who lost access to that marina is rejected.
func (s *Service) AuthenticateDevice(ctx context.Context, token string) (*Principal, error) {
	prefix, ok := parseToken(token, DeviceTokenPrefix)
	if !ok {
		return nil, ErrUnauthenticated
	}
	d, err := store.DeviceByPrefix(ctx, s.db.DB, prefix)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	if !crypto.ConstantTimeEqual(d.TokenHash, crypto.SHA256Hex(token)) || !d.RevokedAt.IsZero() ||
		d.Access == store.DeviceAccessBrowser {
		return nil, ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.db.DB, d.MarinaID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	user, err := store.GetUser(ctx, s.db.DB, d.UserID)
	if err != nil || !user.Active {
		return nil, ErrUnauthenticated
	}
	ms, err := store.MembershipsOfUser(ctx, s.db.DB, user.ID)
	if err != nil {
		return nil, err
	}
	var grants []Grant
	for _, g := range grantsFromMemberships(ms) {
		if g.OrganizationID == scope.OrgID() && (g.MarinaID == "" || g.MarinaID == scope.ID()) {
			// Narrow to the device marina: an organization-wide role acts on this marina only.
			grants = append(grants, Grant{OrganizationID: g.OrganizationID, MarinaID: scope.ID(), Role: g.Role})
		}
	}
	if len(grants) == 0 {
		return nil, ErrUnauthenticated
	}
	now := s.now()
	if d.LastUsedAt.IsZero() || now.Sub(d.LastUsedAt) >= touchInterval {
		if err := store.TouchDevice(ctx, s.db.DB, d.ID, now); err != nil {
			s.log.WarnContext(ctx, "touch device", "error", err)
		}
	}
	return &Principal{Kind: KindDevice, DeviceID: d.ID, UserID: user.ID, Grants: grants, Locale: user.Locale}, nil
}

// CleanupExpired deletes expired sessions, stale reset/invitation tokens and stale pairing codes.
func (s *Service) CleanupExpired(ctx context.Context) (int64, error) {
	now := s.now()
	a, err := store.DeleteExpiredSessions(ctx, s.db.DB, now)
	if err != nil {
		return 0, err
	}
	b, err := store.DeleteStaleResetTokens(ctx, s.db.DB, now.Add(-24*time.Hour))
	if err != nil {
		return a, err
	}
	c, err := store.DeleteStalePairingCodes(ctx, s.db.DB, now.Add(-24*time.Hour))
	if err != nil {
		return a + b, err
	}
	return a + b + c, nil
}
