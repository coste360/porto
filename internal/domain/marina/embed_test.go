package marina

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
)

type fakeLookup struct {
	calls   atomic.Int64
	origins map[string][]string
	fail    atomic.Bool
}

func (f *fakeLookup) lookup(_ context.Context, slug string) (*PublicMarina, error) {
	f.calls.Add(1)
	if f.fail.Load() {
		return nil, errors.New("db down")
	}
	o, ok := f.origins[slug]
	if !ok {
		return nil, auth.ErrNotFound
	}
	return &PublicMarina{Slug: slug, EmbedOrigins: o}, nil
}

func TestEmbedOriginCache_TTLAndNegative(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	f := &fakeLookup{origins: map[string][]string{"porto-a": {"https://a.example"}, "porto-b": nil}}
	c := newEmbedOriginCache(f.lookup, clk, time.Minute, 100)
	ctx := context.Background()

	o, ok := c.EmbedOrigins(ctx, "porto-a")
	if !ok || len(o) != 1 || o[0] != "https://a.example" {
		t.Fatalf("porto-a: %v %v", o, ok)
	}
	o[0] = "mutated"
	if o, _ := c.EmbedOrigins(ctx, "porto-a"); o[0] != "https://a.example" {
		t.Fatalf("cache aliasing: %v", o)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("calls %d, want 1 (cached)", f.calls.Load())
	}
	if o, ok := c.EmbedOrigins(ctx, "porto-b"); !ok || len(o) != 0 {
		t.Fatalf("porto-b: %v %v", o, ok)
	}
	if _, ok := c.EmbedOrigins(ctx, "unknown"); ok {
		t.Fatal("unknown slug resolved")
	}
	if _, ok := c.EmbedOrigins(ctx, "unknown"); ok || f.calls.Load() != 3 {
		t.Fatalf("negative entry not cached: calls %d", f.calls.Load())
	}
	clk.Advance(61 * time.Second)
	c.EmbedOrigins(ctx, "porto-a")
	if f.calls.Load() != 4 {
		t.Fatalf("calls %d after TTL, want 4", f.calls.Load())
	}
}

func TestEmbedOriginCache_ErrorNotCached(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	f := &fakeLookup{origins: map[string][]string{"porto-a": {"https://a.example"}}}
	c := newEmbedOriginCache(f.lookup, clk, time.Minute, 100)
	f.fail.Store(true)
	if _, ok := c.EmbedOrigins(context.Background(), "porto-a"); ok {
		t.Fatal("error resolved as known")
	}
	f.fail.Store(false)
	if _, ok := c.EmbedOrigins(context.Background(), "porto-a"); !ok {
		t.Fatal("error was cached")
	}
}

func TestEmbedOriginCache_Bounded(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	f := &fakeLookup{origins: map[string][]string{}}
	c := newEmbedOriginCache(f.lookup, clk, time.Minute, 10)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.EmbedOrigins(context.Background(), fmt.Sprintf("slug-%d", i))
		}()
	}
	wg.Wait()
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > 10 {
		t.Fatalf("cache has %d entries, max 10", n)
	}
}
