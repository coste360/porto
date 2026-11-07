//go:build integration

package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// browserDevice inserts a browser device of the owner on the owner's marina and opens a device session for it.
func browserDevice(t *testing.T, e *env) (deviceID string, sess *DeviceSession) {
	t.Helper()
	ctx := context.Background()
	err := e.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		raw, _ := crypto.RandomBase32(8)
		d := &store.Device{
			ID: store.NewID(), MarinaID: e.owner.MarinaID, UserID: e.owner.UserID, Name: "Telefono", Platform: "android",
			Kind: "mobile", TokenPrefix: DeviceTokenPrefix + raw, TokenHash: crypto.SHA256Hex(raw),
			Access: store.DeviceAccessBrowser, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := store.InsertDevice(ctx, tx, d); err != nil {
			return err
		}
		deviceID = d.ID
		var err error
		sess, err = e.svc.CreateDeviceSessionTx(ctx, tx, e.owner.UserID, d.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return deviceID, sess
}

func TestDeviceSession_Expiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		deviceID, sess := browserDevice(t, e)
		if sess.TOTPRequired || !sess.ExpiresAt.Equal(t0.Add(DeviceSessionAbsolute)) {
			t.Fatalf("session %+v", sess)
		}
		p, err := e.svc.AuthenticateSession(ctx, sess.Token)
		if err != nil || p.Kind != KindUser || p.DeviceID != deviceID || p.SessionID != sess.SessionID {
			t.Fatalf("principal %+v %v", p, err)
		}
		// Narrowed to the device marina: the organization-wide owner cannot manage the organization from the phone,
		// nor administer the desktop.
		if len(p.Grants) != 1 || p.Grants[0].MarinaID != e.owner.MarinaID || p.IsOwnerAnywhere() ||
			p.Can(e.owner.OrganizationID, "", PermOrganizationManage) || !p.Can(e.owner.OrganizationID, e.owner.MarinaID, PermMarinaRead) {
			t.Fatalf("grants %+v", p.Grants)
		}
		d, _ := store.DeviceByID(ctx, db.DB, deviceID)
		if !d.LastUsedAt.Equal(t0) {
			t.Fatalf("last used %v", d.LastUsedAt)
		}

		// Used every 29 days the session survives past the login lifetimes...
		for range 5 {
			e.clock.Advance(29 * 24 * time.Hour)
			if _, err := e.svc.AuthenticateSession(ctx, sess.Token); err != nil {
				t.Fatalf("at %v: %v", e.clock.Now(), err)
			}
		}
		d, _ = store.DeviceByID(ctx, db.DB, deviceID)
		if !d.LastUsedAt.Equal(e.clock.Now()) {
			t.Fatal("device last use not touched")
		}
		// ...until the absolute expiry (180 days).
		e.clock.Set(t0.Add(DeviceSessionAbsolute))
		if _, err := e.svc.AuthenticateSession(ctx, sess.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past absolute expiry: %v", err)
		}

		// Idle for 30 days: gone.
		e.clock.Set(t0)
		_, idle := browserDevice(t, e)
		e.clock.Advance(DeviceSessionIdle)
		if _, err := e.svc.AuthenticateSession(ctx, idle.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past idle expiry: %v", err)
		}
	})
}

func TestDeviceSession_RevokedDeviceRejected(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		deviceID, sess := browserDevice(t, e)
		scope, err := store.ResolveMarinaScope(ctx, db.DB, e.owner.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		// Defence in depth: even if the session row survived, a revoked device does not authenticate.
		if err := store.RevokeDevice(ctx, db.DB, scope, deviceID, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.AuthenticateSession(ctx, sess.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("revoked device: %v", err)
		}
		if n, _ := store.CountSessionsOfDevice(ctx, db.DB, deviceID); n != 0 {
			t.Fatalf("session of a revoked device kept: %d", n)
		}
		// A browser device has no usable bearer token.
		if _, err := e.svc.AuthenticateDevice(ctx, DeviceTokenPrefix+"aaaaaaaa_x"); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
}

func TestDeviceSession_TOTPUserMustVerify(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		secret, recovery, loginToken := enrolTOTP(t, e, res.Principal)

		deviceID, sess := browserDevice(t, e)
		if !sess.TOTPRequired {
			t.Fatal("TOTP user got a verified device session")
		}
		if _, err := e.svc.AuthenticateSession(ctx, sess.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("unverified device session accepted: %v", err)
		}
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, "000000", ""); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("wrong code: %v", err)
		}
		e.clock.Advance(time.Minute)
		code, _ := totpCode(secret, totpStep(e.clock.Now()))
		verified, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, code, "")
		if err != nil || verified == nil || verified.TOTPRequired || verified.Token == sess.Token ||
			!verified.ExpiresAt.Equal(e.clock.Now().Add(DeviceSessionAbsolute)) {
			t.Fatalf("verified %+v %v", verified, err)
		}
		// The session rotated: the pending token is dead, the new one is a full device session of the same device.
		if _, err := e.svc.AuthenticateSession(ctx, sess.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("pending token still valid after verification: %v", err)
		}
		if p, err := e.svc.AuthenticateSession(ctx, verified.Token); err != nil || p.DeviceID != deviceID {
			t.Fatalf("verified device session: %+v %v", p, err)
		}
		if n, _ := store.CountSessionsOfDevice(ctx, db.DB, deviceID); n != 1 {
			t.Fatalf("%d sessions for the device, want 1", n)
		}
		// Verifying again with the new token is a no-op.
		if again, err := e.svc.VerifyDeviceSessionTOTP(ctx, verified.Token, code, ""); err != nil || again != nil {
			t.Fatalf("second verification: %+v %v", again, err)
		}
		// Recovery codes work too, once.
		_, other := browserDevice(t, e)
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, other.Token, "", recovery[0]); err != nil {
			t.Fatal(err)
		}
		_, third := browserDevice(t, e)
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, third.Token, "", recovery[0]); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("reused recovery code: %v", err)
		}
		// An ordinary login session is not a device session.
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, loginToken, code, ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("login session: %v", err)
		}
	})
}

func TestDeviceSession_PendingTOTPExpiresQuickly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		secret, _, _ := enrolTOTP(t, e, res.Principal)
		_, sess := browserDevice(t, e)
		// The pending session lives as long as a pairing code, not 30 days: the code guessing window is short.
		if !sess.ExpiresAt.Equal(e.clock.Now().Add(DeviceSessionPendingTOTP)) {
			t.Fatalf("pending session expires at %v", sess.ExpiresAt)
		}
		row, err := store.SessionByTokenHash(ctx, db.DB, crypto.SHA256Hex(sess.Token))
		if err != nil || !row.IdleExpiresAt.Equal(sess.ExpiresAt) {
			t.Fatalf("pending idle expiry %v %v", row, err)
		}
		e.clock.Advance(DeviceSessionPendingTOTP)
		code, _ := totpCode(secret, totpStep(e.clock.Now()))
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, code, ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("expired pending session verified: %v", err)
		}
	})
}

func TestConcurrency_DeviceSessionVerifyOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		_, recovery, _ := enrolTOTP(t, e, res.Principal)
		deviceID, sess := browserDevice(t, e)
		// 100 confirmations of the same pending session, each with a different recovery code where available: at
		// most one replaces the session.
		var wg sync.WaitGroup
		var ok atomic.Int32
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if v, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, "", recovery[i%len(recovery)]); err == nil && v != nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 {
			t.Fatalf("%d verifications succeeded, want 1", ok.Load())
		}
		if n, _ := store.CountSessionsOfDevice(ctx, db.DB, deviceID); n != 1 {
			t.Fatalf("%d sessions for the device, want 1", n)
		}
	})
}

// TestDeviceSession_NarrowedToDeviceMarina covers INV-81: an organization-wide owner who pairs a phone on one
// marina gets a device session that acts on that marina only.
func TestDeviceSession_NarrowedToDeviceMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		other := NewMarinaRow(e.owner.OrganizationID, "Marina Due", "marina-due", "Europe/Rome", "it", store.UTC(t0))
		if err := store.InsertMarina(ctx, db.DB, other); err != nil {
			t.Fatal(err)
		}
		login := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword}).Principal
		if !login.Can(e.owner.OrganizationID, other.ID, PermMarinaRead) {
			t.Fatal("the login session should reach both marinas")
		}
		_, sess := browserDevice(t, e)
		p, err := e.svc.AuthenticateSession(ctx, sess.Token)
		if err != nil {
			t.Fatal(err)
		}
		if p.Can(e.owner.OrganizationID, other.ID, PermMarinaRead) || !p.Can(e.owner.OrganizationID, e.owner.MarinaID, PermMarinaRead) {
			t.Fatalf("device session grants %+v", p.Grants)
		}
		if _, err := p.Authorize(ctx, db.DB, other.ID, PermMarinaRead); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other marina from the device session: %v", err)
		}
		if _, err := p.Authorize(ctx, db.DB, e.owner.MarinaID, PermMarinaRead); err != nil {
			t.Fatalf("device marina: %v", err)
		}
	})
}

func TestDeviceSession_TOTPVerifyThrottled(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		secret, _, _ := enrolTOTP(t, e, res.Principal)
		_, sess := browserDevice(t, e)
		for i := range loginFailuresPerAccount {
			if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, "000000", ""); !errors.Is(err, ErrInvalidTOTP) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
		e.clock.Advance(time.Minute)
		code, _ := totpCode(secret, totpStep(e.clock.Now()))
		var rl *RateLimitError
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, code, ""); !errors.As(err, &rl) {
			t.Fatalf("over the failure budget: %v", err)
		}
		// The pending session dies before the throttle window ends; a new pairing works after the window.
		e.clock.Advance(loginFailureWindow)
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, sess.Token, code, ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("pending session outlived the throttle window: %v", err)
		}
		_, fresh := browserDevice(t, e)
		code, _ = totpCode(secret, totpStep(e.clock.Now()))
		if _, err := e.svc.VerifyDeviceSessionTOTP(ctx, fresh.Token, code, ""); err != nil {
			t.Fatalf("after the window: %v", err)
		}
	})
}
