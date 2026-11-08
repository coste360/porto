package booking

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/domain/slug"
	"github.com/coste360/porto/internal/store"
)

// marinaCtx is a public request resolved to its marina.
type marinaCtx struct {
	m     *store.Marina
	scope store.MarinaScope
	loc   *time.Location
	set   onlineconf.Settings
}

// resolve resolves a public slug (ErrLinkInvalid when unknown or, unless allowDisabled, booking disabled).
func (s *Service) resolve(ctx context.Context, slugValue string, allowDisabled bool) (*marinaCtx, error) {
	if slug.Validate(slugValue) != nil {
		return nil, ErrLinkInvalid
	}
	db := s.d.DB.DB
	m, err := store.MarinaBySlug(ctx, db, slugValue)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrLinkInvalid
		}
		return nil, err
	}
	if !m.BookingEnabled && !allowDisabled {
		return nil, ErrLinkInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, db, m.ID)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	set, err := onlineconf.Load(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return &marinaCtx{m: m, scope: scope, loc: locationOf(m.Timezone), set: set}, nil
}

// scoped resolves an already scoped request (customer area, channels, jobs).
func (s *Service) scoped(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*marinaCtx, error) {
	m := new(store.Marina)
	if err := db.NewSelect().Model(m).Where("id = ?", scope.ID()).Limit(1).Scan(ctx); err != nil {
		return nil, err
	}
	set, err := onlineconf.Load(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return &marinaCtx{m: m, scope: scope, loc: locationOf(m.Timezone), set: set}, nil
}

func locationOf(tz string) *time.Location {
	if loc, err := calendar.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (mc *marinaCtx) today(now time.Time) calendar.Date { return calendar.In(now, mc.loc) }

// stayRules converts the settings to the pure stay rules.
func stayRules(set onlineconf.Settings) policy.StayRules {
	return policy.StayRules{
		MinNights: set.MinNights, MaxNights: set.MaxNights, MinAdvanceHours: set.MinAdvanceHours,
		MaxAdvanceDays: set.MaxAdvanceDays, ClosedWeekdays: set.ClosedWeekdays,
		ArrivalToMinutes: policy.ParseHHMM(set.ArrivalTo),
	}
}

func approvalRules(set onlineconf.Settings) policy.ApprovalRules {
	return policy.ApprovalRules{LOAOverCm: set.Approval.LOAOverCm, GroupIDs: set.Approval.GroupIDs, Always: set.Approval.Always}
}

func tiersOf(set onlineconf.Settings) []policy.Tier {
	out := make([]policy.Tier, 0, len(set.CancellationTiers))
	for _, t := range set.CancellationTiers {
		out = append(out, policy.Tier{MinDaysBefore: t.MinDaysBefore, RefundBP: t.RefundBP})
	}
	return policy.SortTiers(out)
}

// scopedPrincipal is a principal limited to perms in one marina, for the public surfaces that act on behalf of a
// marina without a user (documents, prices).
func scopedPrincipal(scope store.MarinaScope, perms ...auth.Permission) *auth.Principal {
	return &auth.Principal{
		Kind: auth.KindAPIKey, Grants: []auth.Grant{{OrganizationID: scope.OrgID(), MarinaID: scope.ID()}}, Scopes: perms,
	}
}

// ---- Tokens (m8-plan §4.3, §5.3, §16 D11) ----

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func (s *Service) mac(purpose string, parts ...string) ([]byte, error) {
	if s.d.Keys == nil {
		return nil, errors.New("booking: key ring not configured")
	}
	key, err := s.d.Keys.Derive(purpose, 32)
	if err != nil {
		return nil, err
	}
	h := hmac.New(sha256.New, key)
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return h.Sum(nil), nil
}

// bookingToken is base32(HMAC("booking-token", id ‖ nonce)): recomputable, never stored.
func (s *Service) bookingToken(id, nonce string) (string, error) {
	sum, err := s.mac("booking-token", id, nonce)
	if err != nil {
		return "", err
	}
	return strings.ToLower(b32.EncodeToString(sum)), nil
}

// checkBookingToken compares a presented booking token in constant time.
func (s *Service) checkBookingToken(ob *store.OnlineBooking, presented string) bool {
	if presented == "" || ob == nil {
		return false
	}
	want, err := s.bookingToken(ob.ID, ob.TokenNonce)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(strings.TrimSpace(presented)))) == 1
}

// statusToken is "<exp>.<base32 HMAC("booking-status", id ‖ exp)>", read-only, valid until exp (unix seconds).
func (s *Service) statusToken(id string, exp time.Time) (string, error) {
	e := strconv.FormatInt(exp.Unix(), 10)
	sum, err := s.mac("booking-status", id, e)
	if err != nil {
		return "", err
	}
	return e + "." + strings.ToLower(b32.EncodeToString(sum)), nil
}

// checkStatusToken verifies a status token of a booking against now.
func (s *Service) checkStatusToken(id, tok string, now time.Time) bool {
	e, _, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	want, err := s.statusToken(id, time.Unix(exp, 0))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(strings.TrimSpace(tok)))) == 1
}

// tokenValidUntil is the end of a booking token's validity: 30 days after departure (marina midnight).
func tokenValidUntil(departure calendar.Date, loc *time.Location) time.Time {
	return departure.AddDays(30).Time(loc)
}

func randomString(n int, alphabet string) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newReference returns an unused 8-character Crockford reference of the marina.
func newReference(ctx context.Context, db bun.IDB, scope store.MarinaScope) (string, error) {
	for range 5 {
		ref, err := randomString(8, crockford)
		if err != nil {
			return "", err
		}
		taken, err := store.OnlineReferenceTaken(ctx, db, scope, ref)
		if err != nil {
			return "", err
		}
		if !taken {
			return ref, nil
		}
	}
	return "", errors.New("booking: no free reference")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
