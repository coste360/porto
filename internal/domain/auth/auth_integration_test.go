//go:build integration

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

const ownerPassword = "owner password 2026"

type fakeMailer struct {
	mu      sync.Mutex
	resets  []notify.PasswordResetMail
	invites []notify.InvitationMail
}

func (f *fakeMailer) EnqueuePasswordReset(_ context.Context, _ bun.IDB, m notify.PasswordResetMail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, m)
	return nil
}

func (f *fakeMailer) EnqueueInvitation(_ context.Context, _ bun.IDB, m notify.InvitationMail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invites = append(f.invites, m)
	return nil
}

func (f *fakeMailer) lastReset() notify.PasswordResetMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resets[len(f.resets)-1]
}

type env struct {
	db     *store.DB
	clock  *clock.Fake
	svc    *Service
	mailer *fakeMailer
	owner  *CreateOwnerResult
}

func newEnv(t *testing.T, db *store.DB, opts Options) *env {
	t.Helper()
	k, _ := crypto.GenerateKey()
	keys, _ := crypto.ParseKeyRing(k)
	c := clock.NewFake(t0)
	m := &fakeMailer{}
	opts.Argon2 = TestArgon2Params
	svc, err := NewService(db, c, keys, audit.NewWriter(c), m, slog.New(slog.DiscardHandler), opts)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := svc.CreateOwner(context.Background(), CreateOwnerInput{
		Email: "Owner@Example.test", Name: "Olga Owner", Password: ownerPassword, Organization: "Porto SpA",
		MarinaName: "Marina Uno", MarinaSlug: "marina-uno", Timezone: "Europe/Rome", Locale: "it",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{db: db, clock: c, svc: svc, mailer: m, owner: owner}
}

func ipCtx(ip string) context.Context {
	return reqctx.With(context.Background(), &reqctx.Info{ClientIP: netip.MustParseAddr(ip), RequestID: "req-1"})
}

func (e *env) login(t *testing.T, in LoginInput) *LoginResult {
	t.Helper()
	res, err := e.svc.Login(ipCtx("203.0.113.1"), in)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return res
}

func TestCreateOwner_AndLogin(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		if e.owner.MarinaID == "" {
			t.Fatal("no marina")
		}
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		if res.Token == "" || !res.ExpiresAt.Equal(t0.Add(SessionAbsolute)) {
			t.Fatalf("result %+v", res)
		}
		p, err := e.svc.AuthenticateSession(ctx, res.Token)
		if err != nil || p.UserID != e.owner.UserID || len(p.Grants) != 1 || p.Grants[0].Role != RoleOwner {
			t.Fatalf("principal %+v %v", p, err)
		}
		if _, err := p.Authorize(ctx, db.DB, e.owner.MarinaID, PermUsersManage); err != nil {
			t.Fatal(err)
		}
		me, err := e.svc.Me(ctx, p)
		if err != nil || len(me.Organizations) != 1 || me.Organizations[0].Role != RoleOwner || len(me.Marinas) != 1 ||
			len(me.Marinas[0].Permissions) != len(Permissions()) || me.User.Email != "owner@example.test" {
			t.Fatalf("me %+v %v", me, err)
		}
		if _, err := e.svc.CreateOwner(ctx, CreateOwnerInput{Email: "owner@example.test", Name: "X", Password: ownerPassword, Organization: "Y"}); !errors.Is(err, ErrUserExists) {
			t.Fatalf("duplicate owner: %v", err)
		}
		if _, err := e.svc.CreateOwner(ctx, CreateOwnerInput{Email: "x@example.test", Name: "X", Password: ownerPassword, Organization: "Y", MarinaName: "M", MarinaSlug: "marina-uno"}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("taken slug: %v", err)
		}
		if n, _ := store.CountAudit(ctx, db.DB, "auth.login"); n != 1 {
			t.Fatalf("login audit rows = %d", n)
		}
		if err := e.svc.Logout(ctx, res.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.AuthenticateSession(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("after logout: %v", err)
		}
	})
}

func TestLogin_NoUserEnumeration(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		_, errUnknown := e.svc.Login(ipCtx("203.0.113.2"), LoginInput{Email: "nobody@example.test", Password: "whatever password"})
		_, errWrong := e.svc.Login(ipCtx("203.0.113.3"), LoginInput{Email: "owner@example.test", Password: "wrong password!!"})
		_, errMalformed := e.svc.Login(ipCtx("203.0.113.4"), LoginInput{Email: "not an email", Password: "x"})
		for _, err := range []error{errUnknown, errWrong, errMalformed} {
			if !errors.Is(err, ErrInvalidCredentials) || err.Error() != ErrInvalidCredentials.Error() {
				t.Fatalf("got %v", err)
			}
		}
		// Invited users (no password yet) look the same.
		ctx := context.Background()
		u := &store.User{ID: store.NewID(), Email: "invited@example.test", Name: "I", Locale: "it", Active: true, CreatedAt: t0, UpdatedAt: t0}
		_ = store.InsertUser(ctx, db.DB, u)
		if _, err := e.svc.Login(ipCtx("203.0.113.5"), LoginInput{Email: "invited@example.test", Password: ""}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("invited: %v", err)
		}
		if n, _ := store.CountAudit(ctx, db.DB, "auth.login_failed"); n != 4 {
			t.Fatalf("failed login audit rows = %d", n)
		}
	})
}

func TestLogin_RateLimited(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		for range 5 {
			if _, err := e.svc.Login(ipCtx("203.0.113.9"), LoginInput{Email: "owner@example.test", Password: "wrong password!!"}); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatal(err)
			}
		}
		_, err := e.svc.Login(ipCtx("198.51.100.1"), LoginInput{Email: "owner@example.test", Password: ownerPassword})
		var rl *RateLimitError
		if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
			t.Fatalf("want rate limit, got %v", err)
		}
		e.clock.Advance(16 * time.Minute)
		e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
	})
}

// Concurrent wrong-password attempts for one account from many /24 prefixes: at most 5 reach the password
// check (each of those is reported as invalid credentials), all the others are rate limited up front.
func TestConcurrency_LoginAccountLimit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		var checked, limited, other atomic.Int64
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				ctx := ipCtx(fmt.Sprintf("10.%d.%d.1", i/200, i%200))
				_, err := e.svc.Login(ctx, LoginInput{Email: "owner@example.test", Password: "wrong password!!"})
				var rl *RateLimitError
				switch {
				case errors.Is(err, ErrInvalidCredentials):
					checked.Add(1)
				case errors.As(err, &rl):
					limited.Add(1)
				default:
					other.Add(1)
				}
			})
		}
		wg.Wait()
		if checked.Load() > 5 || checked.Load() == 0 || other.Load() != 0 || checked.Load()+limited.Load() != 100 {
			t.Fatalf("checked=%d limited=%d other=%d, want at most 5 checked", checked.Load(), limited.Load(), other.Load())
		}
	})
}

func TestSession_RotatedOnLogin(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		a := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		b := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		if a.Token == b.Token || a.Principal.SessionID == b.Principal.SessionID {
			t.Fatal("login reused a session")
		}
		sess, err := store.SessionByTokenHash(ctx, db.DB, crypto.SHA256Hex(b.Token))
		if err != nil || sess.TokenHash == b.Token || sess.IP != "203.0.113.1" {
			t.Fatalf("stored session %+v %v", sess, err)
		}
	})
}

func TestSession_IdleAndAbsoluteExpiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		// Sliding: a request every 20 h keeps it alive...
		for range 5 {
			e.clock.Advance(20 * time.Hour)
			if _, err := e.svc.AuthenticateSession(ctx, res.Token); err != nil {
				t.Fatalf("sliding renewal failed: %v", err)
			}
		}
		// ...until the absolute limit (14 days).
		e.clock.Advance(10 * 24 * time.Hour)
		if _, err := e.svc.AuthenticateSession(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past absolute expiry: %v", err)
		}
		res = e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		e.clock.Advance(SessionIdle + time.Second)
		if _, err := e.svc.AuthenticateSession(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past idle expiry: %v", err)
		}
	})
}

func enrolTOTP(t *testing.T, e *env, p *Principal) (secret string, recovery []string, token string) {
	t.Helper()
	ctx := context.Background()
	enr, err := e.svc.BeginTOTPEnrolment(ctx, p)
	if err != nil || enr.QRSVG == "" {
		t.Fatalf("begin: %v", err)
	}
	code, _ := totpCode(enr.Secret, totpStep(e.clock.Now()))
	recovery, token, err = e.svc.ConfirmTOTP(ctx, p, code)
	if err != nil || len(recovery) != 10 || token == "" {
		t.Fatalf("confirm: %v", err)
	}
	return enr.Secret, recovery, token
}

func TestTOTP_ReplayRejected(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		secret, recovery, newToken := enrolTOTP(t, e, res.Principal)
		if _, err := e.svc.AuthenticateSession(ctx, res.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("session not rotated on TOTP confirmation")
		}
		if _, err := e.svc.AuthenticateSession(ctx, newToken); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.BeginTOTPEnrolment(ctx, res.Principal); !errors.Is(err, ErrTOTPAlreadyEnabled) {
			t.Fatalf("re-enrol: %v", err)
		}

		if _, err := e.svc.Login(ipCtx("203.0.113.1"), LoginInput{Email: "owner@example.test", Password: ownerPassword}); !errors.Is(err, ErrTOTPRequired) {
			t.Fatalf("no code: %v", err)
		}
		e.clock.Advance(30 * time.Second)
		code, _ := totpCode(secret, totpStep(e.clock.Now()))
		e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword, TOTPCode: code})
		if _, err := e.svc.Login(ipCtx("203.0.113.1"), LoginInput{Email: "owner@example.test", Password: ownerPassword, TOTPCode: code}); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("replayed code: %v", err)
		}
		// An older code of the window is a replay too once a newer step was used.
		prev, _ := totpCode(secret, totpStep(e.clock.Now())-1)
		if _, err := e.svc.Login(ipCtx("203.0.113.1"), LoginInput{Email: "owner@example.test", Password: ownerPassword, TOTPCode: prev}); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("older code: %v", err)
		}
		// Recovery codes work once.
		e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword, RecoveryCode: recovery[0]})
		if _, err := e.svc.Login(ipCtx("203.0.113.1"), LoginInput{Email: "owner@example.test", Password: ownerPassword, RecoveryCode: recovery[0]}); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("reused recovery code: %v", err)
		}

		// Disable needs password and a fresh code.
		p, _ := e.svc.AuthenticateSession(ctx, newToken)
		e.clock.Advance(30 * time.Second)
		code, _ = totpCode(secret, totpStep(e.clock.Now()))
		if err := e.svc.DisableTOTP(ctx, p, "wrong password!!", code); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("disable with wrong password: %v", err)
		}
		if err := e.svc.DisableTOTP(ctx, p, ownerPassword, code); err != nil {
			t.Fatal(err)
		}
		e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
	})
}

// Turning TOTP on ends the sessions opened with the password alone (a stolen password or a session left open
// elsewhere must not keep access once the second factor is enabled).
func TestTOTP_ConfirmEndsOtherSessions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		a := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		b := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		_, _, newToken := enrolTOTP(t, e, a.Principal)
		if _, err := e.svc.AuthenticateSession(ctx, b.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("other session survived TOTP confirmation: %v", err)
		}
		if _, err := e.svc.AuthenticateSession(ctx, newToken); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTOTP_PolicyRequiresEnrolment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{RequireTOTPForOwners: true})
		ctx := context.Background()
		res := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		if !res.Principal.TOTPEnrolmentRequired {
			t.Fatal("owner without TOTP not gated")
		}
		if _, err := res.Principal.Authorize(ctx, db.DB, e.owner.MarinaID, PermMarinaRead); !errors.Is(err, ErrTOTPEnrolmentRequired) {
			t.Fatalf("authorize while gated: %v", err)
		}
		secret, _, token := enrolTOTP(t, e, res.Principal)
		p, err := e.svc.AuthenticateSession(ctx, token)
		if err != nil || p.TOTPEnrolmentRequired {
			t.Fatalf("after enrolment: %+v %v", p, err)
		}
		e.clock.Advance(30 * time.Second)
		code, _ := totpCode(secret, totpStep(e.clock.Now()))
		if err := e.svc.DisableTOTP(ctx, p, ownerPassword, code); !errors.Is(err, ErrTOTPRequiredByPolicy) {
			t.Fatalf("disable under policy: %v", err)
		}
	})
}

func TestPasswordReset_SingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := ipCtx("203.0.113.20")
		session := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		if err := e.svc.RequestPasswordReset(ctx, "nobody@example.test"); err != nil {
			t.Fatalf("unknown email: %v", err)
		}
		if len(e.mailer.resets) != 0 {
			t.Fatal("mail sent for unknown email")
		}
		if err := e.svc.RequestPasswordReset(ctx, "OWNER@example.test"); err != nil {
			t.Fatal(err)
		}
		m := e.mailer.lastReset()
		if m.UserID != e.owner.UserID || m.Token == "" || !m.ExpiresAt.Equal(t0.Add(ResetTokenTTL)) {
			t.Fatalf("mail %+v", m)
		}
		if err := e.svc.ConfirmPasswordReset(ctx, m.Token, "short"); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("weak: %v", err)
		}
		if err := e.svc.ConfirmPasswordReset(ctx, m.Token, "a brand new password"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.ConfirmPasswordReset(ctx, m.Token, "another new password"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("second use: %v", err)
		}
		if _, err := e.svc.AuthenticateSession(context.Background(), session.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("sessions survive a reset")
		}
		e.login(t, LoginInput{Email: "owner@example.test", Password: "a brand new password"})

		// Expired tokens are rejected.
		_ = e.svc.RequestPasswordReset(ctx, "owner@example.test")
		m = e.mailer.lastReset()
		e.clock.Advance(ResetTokenTTL + time.Second)
		if err := e.svc.ConfirmPasswordReset(ctx, m.Token, "yet another password"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("expired: %v", err)
		}
		// Rate limit: 3 per hour per account.
		e.clock.Advance(2 * time.Hour)
		for range 3 {
			if err := e.svc.RequestPasswordReset(ipCtx("198.51.100.30"), "someone@example.test"); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.svc.RequestPasswordReset(ipCtx("198.51.100.31"), "someone@example.test"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("4th request: %v", err)
		}
	})
}

func TestConcurrency_PasswordResetSingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := ipCtx("203.0.113.40")
		if err := e.svc.RequestPasswordReset(ctx, "owner@example.test"); err != nil {
			t.Fatal(err)
		}
		token := e.mailer.lastReset().Token
		var ok, invalid atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				switch err := e.svc.ConfirmPasswordReset(ctx, token, "concurrent new password"); {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrInvalidResetToken):
					invalid.Add(1)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if ok.Load() != 1 || invalid.Load() != 99 {
			t.Fatalf("ok=%d invalid=%d", ok.Load(), invalid.Load())
		}
	})
}

func TestAuthorize_OtherMarinaIs404(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		other, err := e.svc.CreateOwner(ctx, CreateOwnerInput{
			Email: "other@example.test", Name: "Other", Password: ownerPassword, Organization: "Altro",
			MarinaName: "Marina Due", MarinaSlug: "marina-due",
		})
		if err != nil {
			t.Fatal(err)
		}
		p := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword}).Principal
		for _, id := range []string{other.MarinaID, store.NewID(), "garbage"} {
			if _, err := p.Authorize(ctx, db.DB, id, PermMarinaRead); !errors.Is(err, ErrNotFound) {
				t.Errorf("marina %s: %v", id, err)
			}
		}
		if _, err := p.AuthorizeOrg(ctx, db.DB, other.OrganizationID, PermMarinaRead); !errors.Is(err, ErrNotFound) {
			t.Errorf("other org: %v", err)
		}
		// A viewer of marina one gets 403 (not 404) on a permission it lacks.
		viewer := &Principal{Kind: KindUser, UserID: "v", Grants: []Grant{{OrganizationID: e.owner.OrganizationID, MarinaID: e.owner.MarinaID, Role: RoleViewer}}}
		if _, err := viewer.Authorize(ctx, db.DB, e.owner.MarinaID, PermUsersManage); !errors.Is(err, ErrForbidden) {
			t.Errorf("viewer: %v", err)
		}
		if _, err := viewer.AuthorizeOrg(ctx, db.DB, e.owner.OrganizationID, PermOrganizationManage); !errors.Is(err, ErrForbidden) {
			t.Errorf("viewer org: %v", err)
		}
	})
}

func TestAccount_ChangePasswordAndProfile(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db, Options{})
		ctx := context.Background()
		a := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		b := e.login(t, LoginInput{Email: "owner@example.test", Password: ownerPassword})
		if err := e.svc.ChangePassword(ctx, a.Principal, "wrong password!!", "new password 2026"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
		if err := e.svc.ChangePassword(ctx, a.Principal, ownerPassword, "aaaaaaaaaaaaaa"); !errors.Is(err, ErrWeakPassword) {
			t.Fatal(err)
		}
		if err := e.svc.ChangePassword(ctx, a.Principal, ownerPassword, "new password 2026"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.AuthenticateSession(ctx, a.Token); err != nil {
			t.Fatal("current session ended")
		}
		if _, err := e.svc.AuthenticateSession(ctx, b.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("other session survived")
		}
		name, locale := "Olga O.", "en"
		if err := e.svc.UpdateProfile(ctx, a.Principal, &name, &locale); err != nil {
			t.Fatal(err)
		}
		bad := "fr"
		if err := e.svc.UpdateProfile(ctx, a.Principal, nil, &bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
		u, _ := store.GetUser(ctx, db.DB, e.owner.UserID)
		if u.Name != name || u.Locale != "en" {
			t.Fatalf("user %+v", u)
		}
		if n, err := e.svc.CleanupExpired(ctx); err != nil || n != 0 {
			t.Fatalf("cleanup: %d %v", n, err)
		}
		e.clock.Advance(15 * 24 * time.Hour)
		if n, _ := e.svc.CleanupExpired(ctx); n < 1 {
			t.Fatal("expired session not cleaned")
		}
	})
}
