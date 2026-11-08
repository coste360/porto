package portal

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// TemplatePortalLogin is the notify template of the login email (it, en).
const TemplatePortalLogin = "portal_login"

// TokenChars is the length of link and session tokens: 52 Crockford base32 characters (260 random bits).
const TokenChars = 52

// Login request limits per email hash (m8-plan §7.1); the router limits per IP prefix.
const (
	LoginPerEmail       = 5
	LoginWindow         = 15 * time.Minute
	loginDedupeBucket   = 10 * time.Minute
	touchEvery          = time.Minute
	maxEmailLimiterKeys = 10_000
)

// Retention of expired links and sessions before Cleanup deletes them.
const Retention = 7 * 24 * time.Hour

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB            *store.DB
	Clock         clock.Clock
	Log           *slog.Logger
	Booking       *booking.Service
	Documents     *document.Service
	Notify        *notify.Service
	BotProtection ext.BotProtection
	BaseURL       string
}

// Service is the customer-area service.
type Service struct {
	d       Deps
	limiter *emailLimiter
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	d.BaseURL = strings.TrimRight(d.BaseURL, "/")
	return &Service{d: d, limiter: newEmailLimiter()}
}

// hashToken is the stored form of a link or session token.
func hashToken(token string) string { return crypto.SHA256Hex(token) }

func newToken() (string, error) { return crypto.RandomBase32(TokenChars) }

// validToken reports whether token has the shape of a portal token (cheap rejection before any lookup).
func validToken(token string) bool {
	if len(token) != TokenChars {
		return false
	}
	for _, r := range token {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func slugEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// LinkURL is the customer-area URL of a link token: the token sits in the fragment, so it never reaches the
// server or any access log.
func (s *Service) LinkURL(slug, token string) string {
	return s.d.BaseURL + "/b/" + slug + "/a#t=" + token
}

// RequestLink queues the login email when the normalised email matches customers of the marina with at least one
// transit. It always returns nil for a valid request with comparable work (no enumeration); rate limits per IP
// prefix (router) and per email hash (here) apply.
func (s *Service) RequestLink(ctx context.Context, slug, email, locale, botToken, remoteIP string) error {
	norm, ok := customer.NormalizeEmail(email)
	if !ok {
		v := &auth.ValidationError{}
		v.Add("email", "invalid")
		return v
	}
	m, err := store.MarinaBySlug(ctx, s.d.DB.DB, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrLinkInvalid
		}
		return err
	}
	if s.d.BotProtection != nil {
		ok, err := s.d.BotProtection.Verify(ctx, botToken, remoteIP)
		if err != nil {
			return fmt.Errorf("portal: bot protection: %w", err)
		}
		if !ok {
			v := &auth.ValidationError{}
			v.Add("bot_token", "invalid")
			return v
		}
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, m.ID)
	if err != nil {
		return ErrLinkInvalid
	}
	now := s.d.Clock.Now()
	// The token is generated for every request: unknown emails do the same random and hashing work.
	token, err := newToken()
	if err != nil {
		return err
	}
	tokenHash := hashToken(token)
	if !s.limiter.allow(m.ID+"|"+crypto.SHA256Hex(norm), now) {
		return nil // limited: same answer, nothing sent
	}
	ids, err := s.matchCustomers(ctx, scope, norm)
	if err != nil {
		s.d.Log.ErrorContext(ctx, "portal login lookup failed", "marina_id", m.ID, "err", err)
		return nil
	}
	if locale == "" {
		locale = m.DefaultLocale
	}
	expires := store.UTC(now.Add(LoginLinkTTL))
	bucket := strconv.FormatInt(now.Unix()/int64(loginDedupeBucket/time.Second), 10)
	for i, cid := range ids {
		if i > 0 {
			if token, err = newToken(); err != nil {
				return err
			}
			tokenHash = hashToken(token)
		}
		if err := s.sendLogin(ctx, scope, m, cid, norm, locale, token, tokenHash, bucket, expires); err != nil {
			// Logged without personal data; the answer stays the same (no enumeration).
			s.d.Log.ErrorContext(ctx, "portal login email failed", "marina_id", m.ID, "customer_id", cid, "err", err)
		}
	}
	return nil
}

// errDuplicateLogin rolls back a login link whose email was already sent in the current dedupe bucket.
var errDuplicateLogin = errors.New("portal: login email already sent")

func (s *Service) sendLogin(ctx context.Context, scope store.MarinaScope, m *store.Marina, customerID, email, locale,
	token, tokenHash, bucket string, expires time.Time,
) error {
	now := store.UTC(s.d.Clock.Now())
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		link := &store.CustomerPortalLink{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: customerID, Kind: store.PortalLinkLogin,
			TokenHash: tokenHash, ExpiresAt: expires, CreatedAt: now,
		}
		if err := store.InsertPortalLink(ctx, tx, link); err != nil {
			return err
		}
		_, queued, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
			Template: TemplatePortalLogin, MarinaID: scope.ID(), CustomerID: customerID, To: email, Locale: locale,
			DedupeKey: "portal-login:" + customerID + ":" + bucket, SourceType: "portal_link", SourceID: link.ID,
			Values: map[string]string{
				notify.ValueMarinaName: m.Name, notify.ValueLink: s.LinkURL(m.Slug, token),
				notify.ValueExpiresAt: strconv.Itoa(int(LoginLinkTTL / time.Minute)),
			},
		})
		if err != nil {
			return err
		}
		if !queued {
			return errDuplicateLogin
		}
		return nil
	})
	if errors.Is(err, errDuplicateLogin) {
		return nil
	}
	return err
}

// matchCustomers returns the live customers of the marina with the exact normalised email and at least one transit.
func (s *Service) matchCustomers(ctx context.Context, scope store.MarinaScope, email string) ([]string, error) {
	cands, err := store.CustomerDuplicateCandidates(ctx, s.d.DB.DB, scope, "", "", "", []string{email})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, c := range cands {
		if hasEmail(c.Emails, email) {
			ids = append(ids, c.ID)
		}
	}
	return store.CustomersWithTransits(ctx, s.d.DB.DB, scope, ids)
}

func hasEmail(raw store.JSON, email string) bool {
	var contacts []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &contacts); err != nil {
		return false
	}
	for _, c := range contacts {
		if n, ok := customer.NormalizeEmail(c.Value); ok && n == email {
			return true
		}
	}
	return false
}

// OpenSession exchanges a link token for a session (login links are single use).
func (s *Service) OpenSession(ctx context.Context, slug, linkToken string) (*Session, error) {
	if !validToken(linkToken) || slug == "" {
		return nil, ErrLinkInvalid
	}
	link, mslug, err := store.PortalLinkByHash(ctx, s.d.DB.DB, hashToken(linkToken))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrLinkInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("portal: resolving link: %w", err)
	}
	now := s.d.Clock.Now()
	if !slugEqual(mslug, slug) || !now.Before(link.ExpiresAt) {
		return nil, ErrLinkInvalid
	}
	if link.Kind == store.PortalLinkLogin && !link.UsedAt.IsZero() {
		return nil, ErrLinkInvalid
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, link.MarinaID)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	at := store.UTC(now)
	sess := &store.CustomerPortalSession{
		ID: store.NewID(), MarinaID: scope.ID(), CustomerID: link.CustomerID, TokenHash: hashToken(token),
		LastSeenAt: at, ExpiresAt: store.UTC(now.Add(SessionAbsoluteTTL)), CreatedAt: at,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		live, err := store.LiveCustomer(ctx, tx, scope, link.CustomerID)
		if err != nil {
			return err
		}
		if !live {
			return ErrLinkInvalid
		}
		switch link.Kind {
		case store.PortalLinkLogin:
			ok, err := store.UsePortalLoginLink(ctx, tx, link.ID, now)
			if err != nil {
				return err
			}
			if !ok {
				return ErrLinkInvalid
			}
		default:
			if err := store.TouchPortalBookingLink(ctx, tx, link.ID, now); err != nil {
				return err
			}
		}
		return store.InsertPortalSession(ctx, tx, sess)
	})
	if err != nil {
		return nil, err
	}
	return &Session{Token: token, ExpiresAt: sess.ExpiresAt}, nil
}

// CloseSession revokes the session.
func (s *Service) CloseSession(ctx context.Context, slug, sessionToken string) error {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return err
	}
	return store.RevokePortalSession(ctx, s.d.DB.DB, p.Scope, p.SessionID, s.d.Clock.Now())
}

// Resolve returns the principal of a valid session of the marina of slug and slides its idle expiry.
func (s *Service) Resolve(ctx context.Context, slug, sessionToken string) (*Principal, error) {
	if !validToken(sessionToken) || slug == "" {
		return nil, ErrLinkInvalid
	}
	sess, mslug, err := store.PortalSessionByHash(ctx, s.d.DB.DB, hashToken(sessionToken))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrLinkInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("portal: resolving session: %w", err)
	}
	now := s.d.Clock.Now()
	switch {
	case !slugEqual(mslug, slug), !sess.RevokedAt.IsZero(), !now.Before(sess.ExpiresAt),
		!now.Before(sess.LastSeenAt.Add(SessionIdleTTL)):
		return nil, ErrLinkInvalid
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, sess.MarinaID)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	live, err := store.LiveCustomer(ctx, s.d.DB.DB, scope, sess.CustomerID)
	if err != nil {
		return nil, err
	}
	if !live {
		return nil, ErrLinkInvalid
	}
	if now.Sub(sess.LastSeenAt) >= touchEvery {
		if err := store.TouchPortalSession(ctx, s.d.DB.DB, sess.ID, now); err != nil {
			return nil, err
		}
	}
	return &Principal{Scope: scope, CustomerID: sess.CustomerID, SessionID: sess.ID}, nil
}

// IssueBookingLinkTx creates the reusable customer-area link of a confirmation email in tx and returns the raw
// token (for the email only; never logged).
func (s *Service) IssueBookingLinkTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID, contractID string) (string, error) {
	c, err := store.GetContract(ctx, tx, scope, contractID)
	if err != nil {
		return "", err
	}
	if c.CustomerID != customerID || customerID == "" {
		return "", fmt.Errorf("portal: booking link for a customer that is not the transit's")
	}
	m, err := store.GetMarina(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	loc, err := calendar.LoadLocation(m.Timezone)
	if err != nil {
		return "", err
	}
	now := store.UTC(s.d.Clock.Now())
	expires := store.UTC(c.EndsOn.Time(loc).Add(BookingLinkGrace))
	if !expires.After(now) {
		expires = store.UTC(now.Add(BookingLinkGrace))
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	link := &store.CustomerPortalLink{
		ID: store.NewID(), MarinaID: scope.ID(), CustomerID: customerID, Kind: store.PortalLinkBooking,
		ContractID: contractID, TokenHash: hashToken(token), ExpiresAt: expires, CreatedAt: now,
	}
	if err := store.InsertPortalLink(ctx, tx, link); err != nil {
		return "", err
	}
	return token, nil
}

// Cleanup deletes links and sessions expired for more than Retention.
func (s *Service) Cleanup(ctx context.Context) error {
	n, err := store.DeleteExpiredPortalRows(ctx, s.d.DB.DB, s.d.Clock.Now().Add(-Retention))
	if err != nil {
		return err
	}
	if n > 0 {
		s.d.Log.InfoContext(ctx, "portal cleanup", "rows", n)
	}
	return nil
}

// Vessels returns the customer's vessels.
func (s *Service) Vessels(ctx context.Context, slug, sessionToken string) ([]Vessel, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	ids, err := store.CustomerVesselIDs(ctx, s.d.DB.DB, p.Scope, p.CustomerID)
	if err != nil {
		return nil, err
	}
	rows, err := store.VesselsByIDs(ctx, s.d.DB.DB, p.Scope, ids)
	if err != nil {
		return nil, err
	}
	out := []Vessel{}
	for _, v := range rows {
		if !v.ArchivedAt.IsZero() {
			continue
		}
		out = append(out, Vessel{
			ID: v.ID, Name: v.Name, LOACm: v.LOACm, BeamCm: v.BeamCm, DraftCm: v.DraftCm, HullType: v.HullType,
		})
	}
	return out, nil
}

// Bookings returns the customer's transits (booking.CustomerBookings).
func (s *Service) Bookings(ctx context.Context, slug, sessionToken string) ([]booking.CustomerBooking, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.CustomerBookings(ctx, p.Scope, p.CustomerID)
}

// Booking returns one transit of the customer (booking.CustomerBooking).
func (s *Service) Booking(ctx context.Context, slug, sessionToken, contractID string) (*booking.CustomerBooking, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.CustomerBooking(ctx, p.Scope, p.CustomerID, contractID)
}

// UploadDocument uploads a document for one of the customer's transits.
func (s *Service) UploadDocument(ctx context.Context, slug, sessionToken, contractID string, up booking.DocumentUpload) (*booking.Document, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.AttachCustomerDocument(ctx, p.Scope, p.CustomerID, contractID, up)
}

// DocumentURL returns a short-lived signed URL of one of the customer's own documents.
func (s *Service) DocumentURL(ctx context.Context, slug, sessionToken, documentID string) (*document.FileURL, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	u, err := s.d.Documents.CustomerFileURL(ctx, s.d.DB.DB, p.Scope, slug, p.CustomerID, documentID)
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, store.ErrNotFound) {
		return nil, ErrLinkInvalid
	}
	return u, err
}

// OpenDocument returns the file of a signed customer-area link (no session: the link is the credential, bound to
// the marina, the customer and a 5-minute expiry).
func (s *Service) OpenDocument(ctx context.Context, slug, customerID, documentID string, exp int64, sig string) (*document.File, error) {
	m, err := store.MarinaBySlug(ctx, s.d.DB.DB, slug)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, m.ID)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	f, err := s.d.Documents.OpenCustomerFile(ctx, scope, customerID, documentID, exp, sig)
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, store.ErrNotFound) || errors.Is(err, document.ErrLinkExpired) {
		return nil, ErrLinkInvalid
	}
	return f, err
}

// PayBalance pays the open residual of a transit (booking.PayBalance).
func (s *Service) PayBalance(ctx context.Context, slug, sessionToken, contractID string) (*booking.PayBalanceResult, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.PayBalance(ctx, p.Scope, p.CustomerID, contractID)
}

// PreviewCancellation previews a cancellation (booking.PreviewCancellation).
func (s *Service) PreviewCancellation(ctx context.Context, slug, sessionToken, contractID string) (*booking.CancellationPreview, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.PreviewCancellation(ctx, p.Scope, p.CustomerID, contractID)
}

// Cancel cancels a transit (booking.CancelByCustomer).
func (s *Service) Cancel(ctx context.Context, slug, sessionToken, contractID string, expectedRefund int64) (*booking.CancelResult, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.CancelByCustomer(ctx, p.Scope, p.CustomerID, contractID, expectedRefund)
}

// PreviewDateChange previews new dates (booking.PreviewDateChange).
func (s *Service) PreviewDateChange(ctx context.Context, slug, sessionToken, contractID string, in booking.DateChange) (*booking.DateChangePreview, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.PreviewDateChange(ctx, p.Scope, p.CustomerID, contractID, in)
}

// ChangeDates changes the dates (booking.ChangeDates).
func (s *Service) ChangeDates(ctx context.Context, slug, sessionToken, contractID string, in booking.DateChange) (*booking.DateChangeResult, error) {
	p, err := s.Resolve(ctx, slug, sessionToken)
	if err != nil {
		return nil, err
	}
	return s.d.Booking.ChangeDates(ctx, p.Scope, p.CustomerID, contractID, in)
}

// emailLimiter is the in-process sliding window of login requests per email hash (never stored).
type emailLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newEmailLimiter() *emailLimiter { return &emailLimiter{hits: map[string][]time.Time{}} }

func (l *emailLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-LoginWindow)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= LoginPerEmail {
		l.hits[key] = kept
		return false
	}
	if len(l.hits) >= maxEmailLimiterKeys {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	l.hits[key] = append(kept, now)
	return true
}
