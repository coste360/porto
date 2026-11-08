package booking

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// Lost demand (m8-plan §9, §16 D17): an anonymous online search with no availability becomes an inquiries row
// (channel online, source public, purpose transit, dimensions and dates only: no customer, no vessel, no IP, no
// user agent). A repeated search of the same client, dimensions and dates within 30 minutes is not recorded again:
// the dedupe key is a hash kept only in memory.

const (
	lostDemandTTL     = 30 * time.Minute
	lostDemandEntries = 10_000
)

// dedupe is a bounded in-memory TTL set of hashed keys (oldest evicted first).
type dedupe struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	order *list.List // of *dedupeEntry, oldest first
	byKey map[string]*list.Element
}

type dedupeEntry struct {
	key string
	at  time.Time
}

func newDedupe(ttl time.Duration, maxEntries int) *dedupe {
	return &dedupe{ttl: ttl, max: maxEntries, order: list.New(), byKey: map[string]*list.Element{}}
}

// seen reports whether key was recorded in the last ttl and records it otherwise.
func (d *dedupe) seen(key string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for e := d.order.Front(); e != nil; e = d.order.Front() {
		de, _ := e.Value.(*dedupeEntry)
		if de == nil || now.Sub(de.at) < d.ttl {
			break
		}
		delete(d.byKey, de.key)
		d.order.Remove(e)
	}
	if _, ok := d.byKey[key]; ok {
		return true
	}
	if d.order.Len() >= d.max {
		e := d.order.Front()
		if de, ok := e.Value.(*dedupeEntry); ok {
			delete(d.byKey, de.key)
		}
		d.order.Remove(e)
	}
	d.byKey[key] = d.order.PushBack(&dedupeEntry{key: key, at: now})
	return false
}

// recordLostDemand inserts the anonymous inquiry of an unanswered search; a failure is logged (no personal data)
// and never fails the search.
func (s *Service) recordLostDemand(ctx context.Context, mc *marinaCtx, in AvailabilityInput, outcome string) {
	if in.Arrival.IsZero() || !in.Departure.After(in.Arrival) || in.Vessel.LOACm <= 0 || in.Vessel.BeamCm <= 0 {
		return
	}
	key := sha256Hex(mc.scope.ID() + "|" + in.ClientKey + "|" + strconv.FormatInt(int64(in.Vessel.LOACm), 10) + "|" +
		strconv.FormatInt(int64(in.Vessel.BeamCm), 10) + "|" + strconv.FormatInt(int64(in.Vessel.DraftCm), 10) + "|" +
		in.Arrival.String() + "|" + in.Departure.String())
	if s.lost.seen(key, s.d.Clock.Now()) {
		return
	}
	row := &store.Inquiry{
		MarinaID: mc.scope.ID(), Channel: store.OnlineChannelOnline, Source: "public", LOACm: int64(in.Vessel.LOACm),
		BeamCm: int64(in.Vessel.BeamCm), DraftCm: int64(in.Vessel.DraftCm), Arrival: in.Arrival, Departure: in.Departure,
		Outcome: outcome,
	}
	err := s.d.DB.RunInTx(context.WithoutCancel(ctx), func(ctx context.Context, tx bun.Tx) error {
		return store.InsertAnonymousInquiry(ctx, tx, row, s.now())
	})
	if err != nil {
		s.d.Log.WarnContext(ctx, "lost demand not recorded", "marina_id", mc.scope.ID(), "err", err)
	}
}
