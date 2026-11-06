// Package apikey manages marina API keys. A key is shown once at creation ("po_live_<prefix>_<secret>"); Porto
// stores only its public prefix and a SHA-256 hash. Scopes are fixed at creation, are a subset of the creator's
// permissions and never include users.manage.
package apikey

import (
	"context"
	"errors"
	"log/slog"
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
	"github.com/coste360/porto/internal/store"
)

// ErrInvalidScope is returned for unknown scopes, users.manage, or scopes the creator does not hold.
var ErrInvalidScope = errors.New("apikey: invalid scope")

// APIKey is the public view of a key (never the secret).
type APIKey struct {
	ID         string
	Name       string
	Prefix     string
	Scopes     []auth.Permission
	CreatedBy  string
	ExpiresAt  time.Time
	LastUsedAt time.Time
	RevokedAt  time.Time
	CreatedAt  time.Time
}

// CreateInput describes a new key.
type CreateInput struct {
	Name      string
	Scopes    []auth.Permission
	ExpiresAt *time.Time
}

// Service manages API keys.
type Service struct {
	db     *store.DB
	clock  clock.Clock
	audit  *audit.Writer
	outbox *event.Outbox
	log    *slog.Logger
}

// NewService returns the API key service.
func NewService(db *store.DB, c clock.Clock, aw *audit.Writer, ob *event.Outbox, log *slog.Logger) *Service {
	return &Service{db: db, clock: c, audit: aw, outbox: ob, log: log}
}

func toAPIKey(k *store.APIKey) APIKey {
	scopes := make([]auth.Permission, 0, len(k.Scopes))
	for _, s := range k.Scopes {
		scopes = append(scopes, auth.Permission(s))
	}
	return APIKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: scopes, CreatedBy: k.CreatedByUserID, ExpiresAt: k.ExpiresAt,
		LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt,
	}
}

// List returns the keys of a marina (users.manage).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string) ([]APIKey, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListAPIKeys(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]APIKey, 0, len(rows))
	for i := range rows {
		out = append(out, toAPIKey(&rows[i]))
	}
	return out, nil
}

// Create makes a key and returns its secret (shown once) with the stored view.
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in CreateInput) (string, *APIKey, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return "", nil, err
	}
	now := store.UTC(s.clock.Now())
	name := strings.TrimSpace(in.Name)
	v := &auth.ValidationError{}
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		v.Add("name", "invalid")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		v.Add("expires_at", "in_the_past")
	}
	if err := v.Err(); err != nil {
		return "", nil, err
	}
	if len(in.Scopes) == 0 {
		return "", nil, ErrInvalidScope
	}
	var scopes store.Strings
	for _, sc := range in.Scopes {
		if !auth.APIKeyScopeAllowed(sc) || !p.Can(scope.OrgID(), scope.ID(), sc) {
			return "", nil, ErrInvalidScope
		}
		if !slices.Contains(scopes, string(sc)) {
			scopes = append(scopes, string(sc))
		}
	}
	slices.Sort(scopes)

	var secret string
	var out *APIKey
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		// 40 random bits of prefix: a collision (unique violation) is astronomically rare and fails the request.
		raw, err := crypto.RandomBase32(8)
		if err != nil {
			return err
		}
		body, err := crypto.RandomToken(32)
		if err != nil {
			return err
		}
		prefix := auth.APIKeyPrefix + strings.ToLower(raw)
		secret = prefix + "_" + body
		row := &store.APIKey{
			ID: store.NewID(), MarinaID: scope.ID(), Name: name, Prefix: prefix, KeyHash: crypto.SHA256Hex(secret),
			Scopes: scopes, CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if in.ExpiresAt != nil {
			row.ExpiresAt = store.UTC(*in.ExpiresAt)
		}
		if err := store.InsertAPIKey(ctx, tx, row); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "api_key.create",
			EntityType: "api_key", EntityID: row.ID, Diff: map[string]any{"name": name, "scopes": scopes, "prefix": row.Prefix},
		}); err != nil {
			return err
		}
		if _, err := s.outbox.Write(ctx, tx, &scope, event.APIKeyCreated, event.APIKeyData{APIKeyID: row.ID}); err != nil {
			return err
		}
		k := toAPIKey(row)
		out = &k
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return secret, out, nil
}

// Revoke revokes a key of a marina (users.manage).
func (s *Service) Revoke(ctx context.Context, p *auth.Principal, marinaID, keyID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermUsersManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		k, err := store.GetAPIKey(ctx, tx, scope, keyID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return auth.ErrNotFound
			}
			return err
		}
		if !k.RevokedAt.IsZero() {
			return nil
		}
		if err := store.RevokeAPIKey(ctx, tx, scope, keyID, store.UTC(s.clock.Now())); err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "api_key.revoke",
			EntityType: "api_key", EntityID: keyID,
		}); err != nil {
			return err
		}
		_, err = s.outbox.Write(ctx, tx, &scope, event.APIKeyRevoked, event.APIKeyData{APIKeyID: keyID})
		return err
	})
}
