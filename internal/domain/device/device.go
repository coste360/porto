// Package device pairs mobile and desktop devices with a marina. An administrator (or the user, for themselves)
// creates a short-lived pairing code shown as text and QR; the device exchanges it once for a device token
// ("po_dev_<prefix>_<secret>") that acts as the user on that marina only.
package device

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/qrsvg"
	"github.com/coste360/porto/internal/store"
)

// Errors.
var (
	ErrPairingCodeInvalid = errors.New("device: pairing code invalid, used or expired")
)

// Pairing code TTLs.
const (
	DefaultPairingTTL = 10 * time.Minute
	MinPairingTTL     = time.Minute
	MaxPairingTTL     = time.Hour
)

// Allowed values.
var (
	Platforms = []string{"ios", "android", "windows", "macos", "linux", "other"}
	Kinds     = []string{"mobile", "desktop"}
)

// Device is the public view of a paired device.
type Device struct {
	ID          string
	MarinaID    string
	Name        string
	Platform    string
	Kind        string
	AppVersion  string
	UserID      string
	UserName    string
	TokenPrefix string
	LastUsedAt  time.Time
	RevokedAt   time.Time
	CreatedAt   time.Time
}

// PairingInput asks for a pairing code. UserID "" means the caller themselves.
type PairingInput struct {
	UserID string
	Kind   string
	TTL    time.Duration
}

// PairingCode is shown once to the administrator.
type PairingCode struct {
	Code      string // XXXX-XXXX
	QRPayload string
	QRSVG     string
	ExpiresAt time.Time
}

// PairInput is what a device sends to pair.
type PairInput struct {
	Code, Name, Platform, Kind, AppVersion string
}

// PairResult is the outcome of a pairing: Token is shown once to the device.
type PairResult struct {
	Token    string
	Device   Device
	MarinaID string
	UserID   string
}

// Options configure the service.
type Options struct {
	BaseURL string // PORTO_BASE_URL, embedded in the QR payload
	// Demo pairing (PORTO_DEMO_PAIRING_USER / _MARINA): both set enables DemoPair.
	DemoUserEmail  string
	DemoMarinaSlug string
}

// Service manages devices.
type Service struct {
	db     *store.DB
	clock  clock.Clock
	audit  *audit.Writer
	outbox *event.Outbox
	log    *slog.Logger
	opts   Options
}

// NewService returns the device service.
func NewService(db *store.DB, c clock.Clock, aw *audit.Writer, ob *event.Outbox, log *slog.Logger, o Options) *Service {
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	return &Service{db: db, clock: c, audit: aw, outbox: ob, log: log, opts: o}
}

func (s *Service) now() time.Time { return store.UTC(s.clock.Now()) }

func toDevice(d *store.Device, userName string) Device {
	return Device{
		ID: d.ID, MarinaID: d.MarinaID, Name: d.Name, Platform: d.Platform, Kind: d.Kind, AppVersion: d.AppVersion,
		UserID: d.UserID, UserName: userName, TokenPrefix: d.TokenPrefix, LastUsedAt: d.LastUsedAt,
		RevokedAt: d.RevokedAt, CreatedAt: d.CreatedAt,
	}
}

// normalizeCode turns user input into the canonical 8-character Crockford form.
func normalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hashCode(code string) string { return crypto.SHA256Hex("pairing:" + normalizeCode(code)) }

// CreatePairingCode creates a pairing code for a user of the marina. For oneself it needs devices.pair_self,
// for another user devices.manage, and that user must have a grant on the marina. The paired device acts with
// the target's grants, so pairing for another user also requires the caller to hold every permission the
// target holds on the marina (no escalation through a device token), and is never allowed to API keys.
func (s *Service) CreatePairingCode(ctx context.Context, p *auth.Principal, marinaID string, in PairingInput) (*PairingCode, error) {
	target := in.UserID
	perm := auth.PermDevicesPairSelf
	forOther := false
	if target == "" || target == p.UserID {
		if p.Kind != auth.KindUser {
			return nil, auth.ErrForbidden
		}
		target = p.UserID
	} else {
		if p.Kind == auth.KindAPIKey {
			return nil, auth.ErrForbidden
		}
		perm = auth.PermDevicesManage
		forOther = true
	}
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, perm)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.Kind != "" && !slices.Contains(Kinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	ttl := in.TTL
	if ttl == 0 {
		ttl = DefaultPairingTTL
	}
	if ttl < MinPairingTTL || ttl > MaxPairingTTL {
		v.Add("ttl_seconds", "out_of_range")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *PairingCode
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		grants, ok := targetGrants(ctx, tx, scope, target)
		if !ok {
			return auth.ErrNotFound
		}
		if forOther && !covers(p, scope, grants) {
			return auth.ErrForbidden
		}
		raw, err := crypto.RandomBase32(8)
		if err != nil {
			return err
		}
		code := raw[:4] + "-" + raw[4:]
		now := s.now()
		row := &store.PairingCode{
			ID: store.NewID(), MarinaID: scope.ID(), UserID: target, CreatedByUserID: p.UserID, Kind: in.Kind,
			CodeHash: hashCode(code), ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertPairingCode(ctx, tx, row); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "device.pairing_code_create",
			EntityType: "pairing_code", EntityID: row.ID, Diff: map[string]any{"user_id": target, "kind": in.Kind, "ttl_seconds": int(ttl / time.Second)},
		}); err != nil {
			return err
		}
		payload := "porto://pair?v=1&url=" + url.QueryEscape(s.opts.BaseURL) + "&code=" + code
		svg, err := qrsvg.Render(payload)
		if err != nil {
			return err
		}
		out = &PairingCode{Code: code, QRPayload: payload, QRSVG: svg, ExpiresAt: row.ExpiresAt}
		return nil
	})
	return out, err
}

func hasGrant(ctx context.Context, tx bun.IDB, scope store.MarinaScope, userID string) bool {
	_, ok := targetGrants(ctx, tx, scope, userID)
	return ok
}

// targetGrants returns the grants an active user holds on the marina (organization-wide and marina-level);
// ok is false when the user is missing, inactive or has no grant there.
func targetGrants(ctx context.Context, tx bun.IDB, scope store.MarinaScope, userID string) ([]auth.Grant, bool) {
	u, err := store.GetUser(ctx, tx, userID)
	if err != nil || !u.Active {
		return nil, false
	}
	ms, err := store.MembershipsOfUser(ctx, tx, userID)
	if err != nil {
		return nil, false
	}
	var out []auth.Grant
	for _, m := range ms {
		if m.OrganizationID == scope.OrgID() && (m.MarinaID == "" || m.MarinaID == scope.ID()) {
			out = append(out, auth.Grant{OrganizationID: m.OrganizationID, MarinaID: m.MarinaID, Role: auth.Role(m.Role)})
		}
	}
	return out, len(out) > 0
}

// covers reports whether the caller holds on the marina every permission that grants give there.
func covers(p *auth.Principal, scope store.MarinaScope, grants []auth.Grant) bool {
	target := &auth.Principal{Kind: auth.KindUser, Grants: grants}
	for _, perm := range target.PermissionsOn(scope.OrgID(), scope.ID()) {
		if !p.Can(scope.OrgID(), scope.ID(), perm) {
			return false
		}
	}
	return true
}

func validatePairInput(in *PairInput) error {
	v := &auth.ValidationError{}
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 100 {
		v.Add("name", "invalid")
	}
	if !slices.Contains(Platforms, in.Platform) {
		v.Add("platform", "invalid")
	}
	if !slices.Contains(Kinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	if utf8.RuneCountInString(in.AppVersion) > 50 {
		v.Add("app_version", "too_long")
	}
	return v.Err()
}

// Pair exchanges a pairing code for a device token. The code is consumed by a single conditional UPDATE, so it
// pairs at most one device even when sent many times at once.
func (s *Service) Pair(ctx context.Context, in PairInput) (*PairResult, error) {
	if err := validatePairInput(&in); err != nil {
		return nil, err
	}
	if len(normalizeCode(in.Code)) != 8 {
		return nil, ErrPairingCodeInvalid
	}
	var out *PairResult
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		pc, err := store.ConsumePairingCode(ctx, tx, hashCode(in.Code), now)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrPairingCodeInvalid
			}
			return err
		}
		if pc.Kind != "" && pc.Kind != in.Kind {
			return ErrPairingCodeInvalid
		}
		scope, err := store.ResolveMarinaScope(ctx, tx, pc.MarinaID)
		if err != nil || !hasGrant(ctx, tx, scope, pc.UserID) {
			return ErrPairingCodeInvalid
		}
		res, err := s.createDevice(ctx, tx, scope, pc.UserID, pc.ID, in, now)
		if err != nil {
			return err
		}
		if err := store.SetPairingDevice(ctx, tx, pc.ID, res.Device.ID, now); err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

// DemoEnabled reports whether demo pairing is configured.
func (s *Service) DemoEnabled() bool {
	return s.opts.DemoUserEmail != "" && s.opts.DemoMarinaSlug != ""
}

// DemoPair pairs a device as the configured demo user without a code (demo installations only). When demo
// pairing is not configured it returns auth.ErrNotFound.
func (s *Service) DemoPair(ctx context.Context, in PairInput) (*PairResult, error) {
	if !s.DemoEnabled() {
		return nil, auth.ErrNotFound
	}
	if err := validatePairInput(&in); err != nil {
		return nil, err
	}
	var out *PairResult
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.MarinaBySlug(ctx, tx, s.opts.DemoMarinaSlug)
		if err != nil {
			return auth.ErrNotFound
		}
		u, err := store.UserByEmail(ctx, tx, strings.ToLower(s.opts.DemoUserEmail))
		if err != nil {
			return auth.ErrNotFound
		}
		scope, err := store.ResolveMarinaScope(ctx, tx, m.ID)
		if err != nil || !hasGrant(ctx, tx, scope, u.ID) {
			return auth.ErrNotFound
		}
		out, err = s.createDevice(ctx, tx, scope, u.ID, "", in, s.now())
		return err
	})
	return out, err
}

func (s *Service) createDevice(ctx context.Context, tx bun.IDB, scope store.MarinaScope, userID, pairingID string, in PairInput, now time.Time) (*PairResult, error) {
	raw, err := crypto.RandomBase32(8)
	if err != nil {
		return nil, err
	}
	body, err := crypto.RandomToken(32)
	if err != nil {
		return nil, err
	}
	prefix := auth.DeviceTokenPrefix + strings.ToLower(raw)
	token := prefix + "_" + body
	d := &store.Device{
		ID: store.NewID(), MarinaID: scope.ID(), UserID: userID, Name: in.Name, Platform: in.Platform, Kind: in.Kind,
		AppVersion: strings.TrimSpace(in.AppVersion), TokenPrefix: prefix, TokenHash: crypto.SHA256Hex(token),
		PairingCodeID: pairingID, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertDevice(ctx, tx, d); err != nil {
		return nil, err
	}
	actor := audit.Actor{Type: audit.ActorDevice, ID: d.ID}
	if err := s.audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: actor, Action: "device.pair",
		EntityType: "device", EntityID: d.ID, Diff: map[string]any{"user_id": userID, "platform": d.Platform, "kind": d.Kind, "demo": pairingID == ""},
	}); err != nil {
		return nil, err
	}
	if _, err := s.outbox.Write(ctx, tx, &scope, event.DevicePaired, event.DeviceData{DeviceID: d.ID, UserID: userID, Kind: d.Kind}); err != nil {
		return nil, err
	}
	userName := ""
	if u, err := store.GetUser(ctx, tx, userID); err == nil {
		userName = u.Name
	}
	return &PairResult{Token: token, Device: toDevice(d, userName), MarinaID: scope.ID(), UserID: userID}, nil
}

// List returns the devices of a marina (devices.manage).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string) ([]Device, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermDevicesManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListDevices(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(rows))
	for i := range rows {
		out = append(out, toDevice(&rows[i].Device, rows[i].UserName))
	}
	return out, nil
}

// Revoke revokes a device of a marina (devices.manage).
func (s *Service) Revoke(ctx context.Context, p *auth.Principal, marinaID, deviceID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermDevicesManage)
	if err != nil {
		return err
	}
	return s.revoke(ctx, scope, p.Actor(), deviceID)
}

// RevokeSelf revokes the calling device (DELETE /device/v1/self).
func (s *Service) RevokeSelf(ctx context.Context, p *auth.Principal) error {
	if p == nil || p.Kind != auth.KindDevice {
		return auth.ErrForbidden
	}
	d, err := store.DeviceByID(ctx, s.db.DB, p.DeviceID)
	if err != nil {
		return auth.ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.db.DB, d.MarinaID)
	if err != nil {
		return auth.ErrUnauthenticated
	}
	return s.revoke(ctx, scope, p.Actor(), d.ID)
}

func (s *Service) revoke(ctx context.Context, scope store.MarinaScope, actor audit.Actor, deviceID string) error {
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		d, err := store.GetDevice(ctx, tx, scope, deviceID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return auth.ErrNotFound
			}
			return err
		}
		if !d.RevokedAt.IsZero() {
			return nil
		}
		if err := store.RevokeDevice(ctx, tx, scope, deviceID, s.now()); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: actor, Action: "device.revoke",
			EntityType: "device", EntityID: deviceID,
		}); err != nil {
			return err
		}
		_, err = s.outbox.Write(ctx, tx, &scope, event.DeviceRevoked, event.DeviceData{DeviceID: deviceID, UserID: d.UserID})
		return err
	})
}

// SelfInfo describes the calling device (GET /device/v1/self).
type SelfInfo struct {
	Device      Device
	UserID      string
	UserName    string
	MarinaID    string
	MarinaSlug  string
	MarinaName  string
	Permissions []auth.Permission
}

// Self returns the calling device with its user, marina and effective permissions.
func (s *Service) Self(ctx context.Context, p *auth.Principal) (*SelfInfo, error) {
	if p == nil || p.Kind != auth.KindDevice {
		return nil, auth.ErrForbidden
	}
	d, err := store.DeviceByID(ctx, s.db.DB, p.DeviceID)
	if err != nil {
		return nil, auth.ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.db.DB, d.MarinaID)
	if err != nil {
		return nil, auth.ErrUnauthenticated
	}
	m, err := store.GetMarina(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	u, err := store.GetUser(ctx, s.db.DB, d.UserID)
	if err != nil {
		return nil, auth.ErrUnauthenticated
	}
	return &SelfInfo{
		Device: toDevice(d, u.Name), UserID: u.ID, UserName: u.Name, MarinaID: m.ID, MarinaSlug: m.Slug,
		MarinaName: m.Name, Permissions: p.PermissionsOn(scope.OrgID(), scope.ID()),
	}, nil
}
