// Package fixture builds a small, real Porto backend for domain tests: database, fake clock, key ring, audit,
// outbox, notify (with a capturing transport) and auth, plus an organization with one marina and its owner.
package fixture

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
)

// T0 is the start instant of the fake clock.
var T0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

// Password is the password of every fixture user.
const Password = "fixture password 2026"

// Mailbox captures sent emails.
type Mailbox struct {
	mu   sync.Mutex
	Sent []notify.Message
}

// Send implements notify.Transport.
func (m *Mailbox) Send(_ context.Context, msg notify.Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, msg)
	return "captured", nil
}

// Messages returns a copy of the captured emails.
func (m *Mailbox) Messages() []notify.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]notify.Message(nil), m.Sent...)
}

// Env is a running backend.
type Env struct {
	DB      *store.DB
	Clock   *clock.Fake
	Keys    *crypto.KeyRing
	Audit   *audit.Writer
	Outbox  *event.Outbox
	Notify  *notify.Service
	Mailbox *Mailbox
	Auth    *auth.Service
	Log     *slog.Logger

	OrgID    string
	MarinaID string
	OwnerID  string
	Owner    *auth.Principal
}

// New builds an Env on db with an organization "Porto Test", a marina "marina-test" and its owner
// (owner@porto.test, organization-wide).
func New(t testing.TB, db *store.DB) *Env {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := crypto.ParseKeyRing(k)
	if err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(T0)
	log := slog.New(slog.DiscardHandler)
	e := &Env{DB: db, Clock: c, Keys: keys, Audit: audit.NewWriter(c), Outbox: event.NewOutbox(c), Mailbox: &Mailbox{}, Log: log}
	e.Notify = notify.NewService(db, c, keys, e.Mailbox, "http://localhost:8080", log)
	e.Auth, err = auth.NewService(db, c, keys, e.Audit, e.Notify, log, auth.Options{Argon2: auth.TestArgon2Params})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Olga Owner", Password: Password, Organization: "Porto Test",
		MarinaName: "Marina Test", MarinaSlug: "marina-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	e.OrgID, e.MarinaID, e.OwnerID = res.OrganizationID, res.MarinaID, res.UserID
	e.Owner = e.Login(t, "owner@porto.test")
	return e
}

// Ctx returns a request context with a client IP and request id.
func Ctx() context.Context {
	return reqctx.With(context.Background(), &reqctx.Info{ClientIP: netip.MustParseAddr("203.0.113.10"), RequestID: "test-req"})
}

var loginSeq atomic.Uint32

// Login logs a fixture user in and returns the principal.
func (e *Env) Login(t testing.TB, email string) *auth.Principal {
	t.Helper()
	// Each login comes from its own /24 so that the per-IP login limit never trips in tests.
	n := loginSeq.Add(1)
	ip := netip.AddrFrom4([4]byte{198, byte(18 + n/256%2), byte(n % 256), 1})
	ctx := reqctx.With(context.Background(), &reqctx.Info{ClientIP: ip, RequestID: "test-login"})
	res, err := e.Auth.Login(ctx, auth.LoginInput{Email: email, Password: Password})
	if err != nil {
		t.Fatalf("fixture login %s: %v", email, err)
	}
	return res.Principal
}

// AddUser creates an active user with the fixture password and a grant (marinaID "" = organization-wide) and
// returns the logged-in principal.
func (e *Env) AddUser(t testing.TB, email string, role auth.Role, orgID, marinaID string) *auth.Principal {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	hash, err := e.Auth.Hasher().Hash(Password)
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{ID: store.NewID(), Email: email, PasswordHash: hash, Name: "User " + string(role), Locale: "it", Active: true, CreatedAt: now, UpdatedAt: now}
	if err := store.InsertUser(ctx, e.DB.DB, u); err != nil {
		t.Fatal(err)
	}
	m := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgID, MarinaID: marinaID, Role: string(role), CreatedAt: now, UpdatedAt: now}
	if err := store.InsertMembership(ctx, e.DB.DB, m); err != nil {
		t.Fatal(err)
	}
	return e.Login(t, email)
}

// AddMarina creates another marina in the fixture organization and returns its id.
func (e *Env) AddMarina(t testing.TB, slug string) string {
	t.Helper()
	m := auth.NewMarinaRow(e.OrgID, "Marina "+slug, slug, "Europe/Rome", "it", store.UTC(e.Clock.Now()))
	if err := store.InsertMarina(context.Background(), e.DB.DB, m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

// OtherOrganization creates a second organization with its own marina and owner, unrelated to the fixture one.
func (e *Env) OtherOrganization(t testing.TB) (orgID, marinaID string, owner *auth.Principal) {
	t.Helper()
	res, err := e.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "other-owner@porto.test", Name: "Other Owner", Password: Password, Organization: "Altro Porto",
		MarinaName: "Marina Altra", MarinaSlug: "marina-altra",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.OrganizationID, res.MarinaID, e.Login(t, "other-owner@porto.test")
}

// Events returns the outbox events of a type, oldest first.
func (e *Env) Events(t testing.TB, typ string) []store.OutboxEvent {
	t.Helper()
	var out []store.OutboxEvent
	if err := e.DB.NewSelect().Model(&out).Where("type = ?", typ).OrderExpr("id").Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return out
}

// AuditCount returns the number of audit rows with an action.
func (e *Env) AuditCount(t testing.TB, action string) int {
	t.Helper()
	n, err := store.CountAudit(context.Background(), e.DB.DB, action)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
