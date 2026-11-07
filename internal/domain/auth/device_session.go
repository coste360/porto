package auth

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// Device sessions (m6a-plan §3.3, D9): a browser paired by QR on the local network gets an ordinary session row
// bound to its device. It lives longer than a login (a phone on the quay must not log in every day), acts on the
// device's marina only, dies with the device (revoke deletes it) and is rejected as soon as the device is revoked.
const (
	DeviceSessionIdle     = 30 * 24 * time.Hour
	DeviceSessionAbsolute = 180 * 24 * time.Hour
	// DeviceSessionPendingTOTP is the whole lifetime of a device session still waiting for the second factor: the
	// pairing code replaces the password, so the window to guess a TOTP code must be as short as the code's.
	DeviceSessionPendingTOTP = 10 * time.Minute
	deviceTouchInterval      = 5 * time.Minute
)

// DeviceSession is a device session just created: Token goes into the porto_session cookie and is never stored.
type DeviceSession struct {
	Token     string
	SessionID string
	ExpiresAt time.Time
	// TOTPRequired is true when the user has a confirmed second factor: the session is unusable until
	// VerifyDeviceSessionTOTP accepts a code entered on the device.
	TOTPRequired bool
}

// CreateDeviceSessionTx opens a device session for userID bound to deviceID in the caller's transaction (the
// pairing that consumed the code and created the device). For a user with TOTP the session is pending and lives
// DeviceSessionPendingTOTP only; VerifyDeviceSessionTOTP replaces it with a full one.
func CreateDeviceSessionTx(ctx context.Context, tx bun.IDB, userID, deviceID string, now time.Time) (*DeviceSession, error) {
	hasTOTP, err := confirmedTOTP(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if hasTOTP {
		return insertDeviceSession(ctx, tx, userID, deviceID, false, now)
	}
	return insertDeviceSession(ctx, tx, userID, deviceID, true, now)
}

func insertDeviceSession(ctx context.Context, tx bun.IDB, userID, deviceID string, verified bool, now time.Time) (*DeviceSession, error) {
	spec := sessionSpec{
		UserID: userID, DeviceID: deviceID, TOTPVerified: verified,
		Idle: DeviceSessionIdle, Absolute: DeviceSessionAbsolute,
	}
	if !verified {
		spec.Idle, spec.Absolute = DeviceSessionPendingTOTP, DeviceSessionPendingTOTP
	}
	token, sess, err := insertSession(ctx, tx, spec, now)
	if err != nil {
		return nil, err
	}
	return &DeviceSession{Token: token, SessionID: sess.ID, ExpiresAt: sess.ExpiresAt, TOTPRequired: !verified}, nil
}

// CreateDeviceSessionTx is CreateDeviceSessionTx on the service clock.
func (s *Service) CreateDeviceSessionTx(ctx context.Context, tx bun.IDB, userID, deviceID string) (*DeviceSession, error) {
	return CreateDeviceSessionTx(ctx, tx, userID, deviceID, s.now())
}

func confirmedTOTP(ctx context.Context, db bun.IDB, userID string) (bool, error) {
	t, err := store.GetTOTP(ctx, db, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return !t.ConfirmedAt.IsZero(), nil
}

// NarrowGrants returns the grants of memberships that apply to one marina, each narrowed to that marina: an
// organization-wide role acts on this marina only (devices, device sessions).
func NarrowGrants(ms []store.Membership, orgID, marinaID string) []Grant {
	var out []Grant
	for _, g := range grantsFromMemberships(ms) {
		if g.OrganizationID == orgID && (g.MarinaID == "" || g.MarinaID == marinaID) {
			out = append(out, Grant{OrganizationID: g.OrganizationID, MarinaID: marinaID, Role: g.Role})
		}
	}
	return out
}

// authenticateDeviceSession completes AuthenticateSession for a session bound to a device: the device must exist,
// be a browser device and not be revoked; the user must still hold a grant on its marina; a user with TOTP must
// have verified the session. The principal is a user narrowed to the device's marina, with DeviceID set.
func (s *Service) authenticateDeviceSession(ctx context.Context, sess *store.Session, user *store.User, now time.Time) (*Principal, error) {
	d, err := store.DeviceByID(ctx, s.db.DB, sess.DeviceID)
	if err != nil || !d.RevokedAt.IsZero() || d.Access != store.DeviceAccessBrowser || d.UserID != user.ID {
		if err == nil || errors.Is(err, store.ErrNotFound) {
			_ = store.DeleteSession(ctx, s.db.DB, sess.ID)
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	hasTOTP, err := confirmedTOTP(ctx, s.db.DB, user.ID)
	if err != nil {
		return nil, err
	}
	if hasTOTP && !sess.TOTPVerified {
		return nil, ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.db.DB, d.MarinaID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	ms, err := store.MembershipsOfUser(ctx, s.db.DB, user.ID)
	if err != nil {
		return nil, err
	}
	grants := NarrowGrants(ms, scope.OrgID(), scope.ID())
	if len(grants) == 0 {
		return nil, ErrUnauthenticated
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		idle := now.Add(DeviceSessionIdle)
		if idle.After(sess.ExpiresAt) {
			idle = sess.ExpiresAt
		}
		if err := store.TouchSession(ctx, s.db.DB, sess.ID, now, idle); err != nil {
			s.log.WarnContext(ctx, "touch device session", "error", err)
		}
	}
	if d.LastUsedAt.IsZero() || now.Sub(d.LastUsedAt) >= deviceTouchInterval {
		if err := store.TouchDevice(ctx, s.db.DB, d.ID, now); err != nil {
			s.log.WarnContext(ctx, "touch device", "error", err)
		}
	}
	p := &Principal{
		Kind: KindUser, UserID: user.ID, SessionID: sess.ID, DeviceID: d.ID, Grants: grants, Locale: user.Locale,
	}
	// The owner TOTP policy still applies: a device session never bypasses a required enrolment.
	if s.opts.RequireTOTPForOwners && s.policyRequiresTOTP(p) && !hasTOTP {
		p.TOTPEnrolmentRequired = true
	}
	return p, nil
}

// VerifyDeviceSessionTOTP checks a TOTP code (replay safe) or a recovery code for a pending device session of a
// user with a second factor. On success the pending session is deleted and a new, verified device session for the
// same device is returned (sessions rotate on TOTP confirmation): the caller sets its token in the cookie. It
// returns (nil, nil) when the token is already a verified device session. ErrUnauthenticated when the token is not
// a live device session; ErrInvalidTOTP on a wrong code.
func (s *Service) VerifyDeviceSessionTOTP(ctx context.Context, token, code, recovery string) (*DeviceSession, error) {
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
	if sess.DeviceID == "" || !now.Before(sess.ExpiresAt) || !now.Before(sess.IdleExpiresAt) {
		return nil, ErrUnauthenticated
	}
	if sess.TOTPVerified {
		return nil, nil
	}
	t, err := store.GetTOTP(ctx, s.db.DB, sess.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	if t.ConfirmedAt.IsZero() {
		return nil, ErrUnauthenticated
	}
	// Same budget as a login: 5 failures per user in 15 minutes, attempts per peer (a 6-digit code must not be
	// guessable from a paired phone).
	info := reqctx.From(ctx)
	account := "device-session:" + sess.UserID
	if ok, wait := s.throttle.Check(info.ClientIP, account); !ok {
		return nil, &RateLimitError{RetryAfter: wait}
	}
	defer s.throttle.Release(account)
	var out *DeviceSession
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.checkSecondFactor(ctx, tx, sess.UserID, t, code, recovery, now); err != nil {
			return err
		}
		if err := store.DeletePendingDeviceSession(ctx, tx, sess.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrUnauthenticated
			}
			return err
		}
		var err error
		out, err = insertDeviceSession(ctx, tx, sess.UserID, sess.DeviceID, true, now)
		return err
	})
	switch {
	case err == nil:
		s.throttle.Success(account)
	case errors.Is(err, ErrInvalidTOTP):
		s.throttle.Failure(info.ClientIP, account)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
