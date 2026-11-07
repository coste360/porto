// Package magiclink holds the customer access links of the public pages (m4-plan §6.6, INV-75): 32-byte random
// tokens shown once and stored as SHA-256, one active link per (purpose, subject, customer) (a new one revokes the
// previous), resolved in constant time by hash with marina slug, expiry and revocation checks.
package magiclink

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
)

// Purposes (access_links.purpose).
const (
	PurposeQuote       = "quote"
	PurposeRenewal     = "renewal"
	PurposeSignature   = "signature"
	PurposeUnsubscribe = "unsubscribe"
)

// TokenChars is the length of a token: 52 Crockford base32 characters, 260 random bits.
const TokenChars = 52

// Retention after expiry before the cleanup job deletes a link; signature codes are deleted after 24 hours.
const (
	LinkRetention = 90 * 24 * time.Hour
	OTPRetention  = 24 * time.Hour
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB      *store.DB
	Clock   clock.Clock
	BaseURL string // PORTO_BASE_URL; public URLs are {BaseURL}/b/{slug}/m/{purpose}/{token}
	Log     *slog.Logger
}

// Service is the magic-link service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	d.BaseURL = strings.TrimRight(d.BaseURL, "/")
	return &Service{d: d}
}

// Link is a resolved link.
type Link struct {
	ID          string
	MarinaID    string
	Purpose     string
	SubjectType string
	SubjectID   string
	CustomerID  string
	ExpiresAt   time.Time
}

// HashToken is the stored form of a token.
func HashToken(token string) string { return crypto.SHA256Hex(token) }

func validPurpose(p string) bool {
	switch p {
	case PurposeQuote, PurposeRenewal, PurposeSignature, PurposeUnsubscribe:
		return true
	}
	return false
}

// CreateTx creates a link inside the caller's transaction, revoking the active one for the same purpose,
// subject and customer. It returns the token (never stored, never logged) and the link id.
func (s *Service) CreateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, purpose, subjectType, subjectID, customerID string, expiresAt time.Time) (token, id string, err error) {
	if !validPurpose(purpose) || subjectType == "" || subjectID == "" {
		return "", "", fmt.Errorf("magiclink: invalid link request")
	}
	now := store.UTC(s.d.Clock.Now())
	if !expiresAt.After(now) {
		return "", "", fmt.Errorf("magiclink: expiry in the past")
	}
	if err := store.RevokeAccessLinks(ctx, tx, scope, purpose, subjectType, subjectID, customerID, now); err != nil {
		return "", "", err
	}
	token, err = crypto.RandomBase32(TokenChars)
	if err != nil {
		return "", "", err
	}
	row := &store.AccessLink{
		ID: store.NewID(), MarinaID: scope.ID(), Purpose: purpose, SubjectType: subjectType, SubjectID: subjectID,
		CustomerID: customerID, TokenHash: HashToken(token), ExpiresAt: store.UTC(expiresAt), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertAccessLink(ctx, tx, row); err != nil {
		return "", "", err
	}
	return token, row.ID, nil
}

// RevokeTx revokes the active links of a purpose and subject (all customers).
func (s *Service) RevokeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, purpose, subjectType, subjectID string) error {
	return store.RevokeAccessLinks(ctx, tx, scope, purpose, subjectType, subjectID, "", s.d.Clock.Now())
}

// Resolve returns the link of token for the marina of slug and records its use; any failure is ErrInvalid, the
// same answer for unknown, expired, revoked and foreign tokens.
func (s *Service) Resolve(ctx context.Context, slug, token string) (*Link, error) {
	if len(token) != TokenChars || slug == "" {
		return nil, ErrInvalid
	}
	row, mslug, err := store.AccessLinkByHash(ctx, s.d.DB.DB, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrInvalid
	}
	if err != nil {
		// A database failure (busy, cancelled) is not an invalid link: report it so the caller can retry.
		return nil, fmt.Errorf("magiclink: resolving link: %w", err)
	}
	now := s.d.Clock.Now()
	ok := subtle.ConstantTimeCompare([]byte(mslug), []byte(slug)) == 1
	if !ok || !row.RevokedAt.IsZero() || !now.Before(row.ExpiresAt) {
		return nil, ErrInvalid
	}
	if err := store.TouchAccessLink(ctx, s.d.DB.DB, row.ID, now); err != nil {
		return nil, err
	}
	return &Link{
		ID: row.ID, MarinaID: row.MarinaID, Purpose: row.Purpose, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
		CustomerID: row.CustomerID, ExpiresAt: row.ExpiresAt,
	}, nil
}

// ResolvePurpose is Resolve restricted to one purpose.
func (s *Service) ResolvePurpose(ctx context.Context, slug, token, purpose string) (*Link, error) {
	l, err := s.Resolve(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	if l.Purpose != purpose {
		return nil, ErrInvalid
	}
	return l, nil
}

// URL is the public URL of a link.
func (s *Service) URL(slug, purpose, token string) string {
	return s.d.BaseURL + "/b/" + slug + "/m/" + purpose + "/" + token
}

// Cleanup deletes links expired for more than 90 days and OTP rows older than 24 hours (job links.cleanup).
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.d.Clock.Now()
	n, err := store.DeleteExpiredAccessLinks(ctx, s.d.DB.DB, now.Add(-LinkRetention))
	if err != nil {
		return err
	}
	o, err := store.DeleteOldOTPs(ctx, s.d.DB.DB, now.Add(-OTPRetention))
	if err != nil {
		return err
	}
	if n+o > 0 {
		s.d.Log.InfoContext(ctx, "links cleanup", "links", n, "otps", o)
	}
	return nil
}

// BaseURL is PORTO_BASE_URL without the trailing slash.
func (s *Service) BaseURL() string { return s.d.BaseURL }
