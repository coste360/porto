package marina

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
)

// Embed origin cache defaults (m8-plan §10): the framing headers of /b/{slug} pages read the allowed origins at
// most once a minute per slug, and the cache never grows past embedCacheMax slugs (unknown slugs included, so
// that a scan of random slugs cannot grow memory or hit the database on every request).
const (
	EmbedCacheTTL = 60 * time.Second
	embedCacheMax = 4096
)

// EmbedOriginCache resolves the origins allowed to embed the booking page of a slug
// (marinas.allowed_embed_origins), cached for EmbedCacheTTL. It is safe for concurrent use.
type EmbedOriginCache struct {
	lookup func(ctx context.Context, slug string) (*PublicMarina, error)
	clock  clock.Clock
	ttl    time.Duration
	max    int

	mu      sync.Mutex
	entries map[string]embedEntry
}

type embedEntry struct {
	origins []string
	known   bool
	expires time.Time
}

// NewEmbedOriginCache returns a cache over the public marina card of s.
func NewEmbedOriginCache(s *Service, c clock.Clock) *EmbedOriginCache {
	return newEmbedOriginCache(s.PublicBySlug, c, EmbedCacheTTL, embedCacheMax)
}

func newEmbedOriginCache(lookup func(context.Context, string) (*PublicMarina, error), c clock.Clock, ttl time.Duration, maxEntries int) *EmbedOriginCache {
	return &EmbedOriginCache{lookup: lookup, clock: c, ttl: ttl, max: maxEntries, entries: map[string]embedEntry{}}
}

// EmbedOrigins returns the allowed origins of slug; ok is false for an unknown (or archived) marina. A database
// error is not cached and reads as unknown, which keeps the strictest framing headers.
func (c *EmbedOriginCache) EmbedOrigins(ctx context.Context, slug string) ([]string, bool) {
	now := c.clock.Now()
	c.mu.Lock()
	if e, hit := c.entries[slug]; hit && now.Before(e.expires) {
		c.mu.Unlock()
		return slices.Clone(e.origins), e.known
	}
	c.mu.Unlock()

	m, err := c.lookup(ctx, slug)
	var entry embedEntry
	switch {
	case err == nil:
		entry = embedEntry{origins: slices.Clone(m.EmbedOrigins), known: true}
	case errors.Is(err, auth.ErrNotFound):
		entry = embedEntry{}
	default:
		return nil, false
	}
	entry.expires = now.Add(c.ttl)

	c.mu.Lock()
	if len(c.entries) >= c.max {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.max {
			clear(c.entries)
		}
	}
	c.entries[slug] = entry
	c.mu.Unlock()
	return slices.Clone(entry.origins), entry.known
}
