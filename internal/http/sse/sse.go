// Package sse serves the realtime stream of a marina (GET /api/admin/v1/stream) as server-sent events. Messages
// carry ids and enums only (INV-70): topic, event type, marina id and the entity ids found in the event data.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/pkg/ext"
)

// Defaults of the stream.
const (
	DefaultReplaySize   = 256
	MaxStreamsPerClient = 5
	heartbeatEvery      = 25 * time.Second
	retryMillis         = 3000
)

// ErrTooManyStreams is returned when a principal already has MaxStreamsPerClient open streams.
var ErrTooManyStreams = errors.New("sse: too many concurrent streams")

// ReplayBus decorates an ext.EventBus with a ring of the last events of each marina, so that a client that
// reconnects with Last-Event-ID receives what it missed (as long as it is still in the ring).
type ReplayBus struct {
	inner ext.EventBus
	size  int

	mu    sync.Mutex
	rings map[string][]ext.Event
}

// NewReplayBus wraps inner, keeping size events per marina.
func NewReplayBus(inner ext.EventBus, size int) *ReplayBus {
	if size <= 0 {
		size = DefaultReplaySize
	}
	return &ReplayBus{inner: inner, size: size, rings: map[string][]ext.Event{}}
}

// Publish records the event and forwards it.
func (b *ReplayBus) Publish(ctx context.Context, ev ext.Event) error {
	if ev.MarinaID != "" {
		b.mu.Lock()
		ring := b.rings[ev.MarinaID]
		ring = append(ring, ev)
		if len(ring) > b.size {
			ring = append([]ext.Event(nil), ring[len(ring)-b.size:]...)
		}
		b.rings[ev.MarinaID] = ring
		b.mu.Unlock()
	}
	return b.inner.Publish(ctx, ev)
}

// Subscribe forwards to the inner bus.
func (b *ReplayBus) Subscribe(ctx context.Context, marinaID string) (<-chan ext.Event, func(), error) {
	return b.inner.Subscribe(ctx, marinaID)
}

// Since returns the buffered events of a marina after lastID (ids are UUIDv7, so they sort by time). It
// reports false when lastID is not in the ring anymore (the client should reload its state).
func (b *ReplayBus) Since(marinaID, lastID string) ([]ext.Event, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ring := b.rings[marinaID]
	for i, ev := range ring {
		if ev.ID == lastID {
			return append([]ext.Event(nil), ring[i+1:]...), true
		}
	}
	return nil, false
}

// Message is the data of one SSE message. MapID, ResourceIDs and Nights (M3, spec 27.4) let a client narrow its
// invalidation; they are copied from the event data keys map_id, resource_ids and from/to.
type Message struct {
	Topic       string   `json:"topic"`
	Type        string   `json:"type"`
	MarinaID    string   `json:"marina_id"`
	EntityIDs   []string `json:"entity_ids"`
	MapID       string   `json:"map_id,omitempty"`
	ResourceIDs []string `json:"resource_ids,omitempty"`
	Nights      *Nights  `json:"nights,omitempty"`
}

// Nights is the inclusive range of nights an occupancy event touches (YYYY-MM-DD).
type Nights struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// datePattern is the only form accepted for Nights values (anything else is dropped, never echoed).
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// MessageOf builds the SSE data of an event: only the values of keys ending in "_id" are kept as entity ids;
// map_id, resource_ids (strings only) and from/to (dates only) fill the narrowing fields.
func MessageOf(ev ext.Event) Message {
	m := Message{Topic: event.TopicOf(ev.Type), Type: ev.Type, MarinaID: ev.MarinaID, EntityIDs: []string{}}
	var data map[string]any
	if json.Unmarshal(ev.Data, &data) == nil {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := data[k].(string); ok && strings.HasSuffix(k, "_id") && s != "" {
				m.EntityIDs = append(m.EntityIDs, s)
			}
		}
		if s, ok := data["map_id"].(string); ok {
			m.MapID = s
		}
		if list, ok := data["resource_ids"].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok && s != "" {
					m.ResourceIDs = append(m.ResourceIDs, s)
				}
			}
		}
		from, _ := data["from"].(string)
		to, _ := data["to"].(string)
		if datePattern.MatchString(from) && datePattern.MatchString(to) {
			m.Nights = &Nights{From: from, To: to}
		}
	}
	return m
}

// Server serves event streams.
type Server struct {
	bus    ext.EventBus
	replay *ReplayBus
	log    *slog.Logger

	mu    sync.Mutex
	open  map[string]int
	beats time.Duration
}

// NewServer returns a stream server; replay may be nil (no resume).
func NewServer(bus ext.EventBus, replay *ReplayBus, log *slog.Logger) *Server {
	return &Server{bus: bus, replay: replay, log: log, open: map[string]int{}, beats: heartbeatEvery}
}

func (s *Server) acquire(client string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open[client] >= MaxStreamsPerClient {
		return false
	}
	s.open[client]++
	return true
}

func (s *Server) release(client string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open[client]--; s.open[client] <= 0 {
		delete(s.open, client)
	}
}

// Serve streams the events of marinaID until the client goes away. The caller has already authorised the
// principal (client identifies it for the concurrency cap). It returns ErrTooManyStreams before writing anything
// when the cap is reached.
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, client, marinaID, lastEventID string) error {
	rc := http.NewResponseController(w)
	if !s.acquire(client) {
		return ErrTooManyStreams
	}
	defer s.release(client)

	ch, cancel, err := s.bus.Subscribe(r.Context(), marinaID)
	if err != nil {
		return err
	}
	defer cancel()
	// The server's write timeout does not apply to a long-lived stream.
	_ = rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", retryMillis); err != nil {
		return nil
	}
	sent := map[string]bool{}
	if lastEventID != "" && s.replay != nil {
		missed, ok := s.replay.Since(marinaID, lastEventID)
		if !ok {
			// The client is too far behind: tell it to reload.
			_, _ = fmt.Fprint(w, "event: resync\ndata: {}\n\n")
		}
		for _, ev := range missed {
			if !writeEvent(w, ev) {
				return nil
			}
			sent[ev.ID] = true
		}
	}
	_ = rc.Flush()

	tick := time.NewTicker(s.beats)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if sent[ev.ID] {
				continue
			}
			if !writeEvent(w, ev) {
				return nil
			}
			_ = rc.Flush()
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			_ = rc.Flush()
		}
	}
}

// writeEvent writes one message; events without a topic (webhook.test) are not streamed.
func writeEvent(w http.ResponseWriter, ev ext.Event) bool {
	m := MessageOf(ev)
	if m.Topic == "" {
		return true
	}
	data, err := json.Marshal(m)
	if err != nil {
		return true
	}
	_, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, m.Topic, data)
	return err == nil
}
