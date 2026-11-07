package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/pkg/ext"
)

var piiPattern = regexp.MustCompile(`(?i)email|phone|name|token|secret|password|address|@`)

func sampleEvents(t *testing.T) []ext.Event {
	t.Helper()
	var out []ext.Event
	i := 0
	for typ, data := range event.Samples() {
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		i++
		out = append(out, ext.Event{
			ID: "0190000" + string(rune('a'+i)), Type: typ, Version: 1, MarinaID: "m1", Data: b,
			OccurredAt: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
		})
	}
	return out
}

func TestSSE_PayloadHasNoPII(t *testing.T) {
	for _, ev := range sampleEvents(t) {
		m := MessageOf(ev)
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var keys map[string]any
		_ = json.Unmarshal(b, &keys)
		for k := range keys {
			if !map[string]bool{
				"topic": true, "type": true, "marina_id": true, "entity_ids": true,
				"map_id": true, "resource_ids": true, "nights": true, // M3 (m3-plan §7.5, §13 D22)
			}[k] {
				t.Fatalf("%s: unexpected key %q", ev.Type, k)
			}
		}
		for _, id := range m.EntityIDs {
			if piiPattern.MatchString(id) {
				t.Fatalf("%s: entity id %q looks like personal data", ev.Type, id)
			}
		}
	}
	// Data with personal fields must not leak: only *_id values are kept.
	m := MessageOf(ext.Event{
		Type: event.MemberAdded, MarinaID: "m1",
		Data: json.RawMessage(`{"user_id":"u1","email":"a@b.it","name":"Mario","role":"office"}`),
	})
	if len(m.EntityIDs) != 1 || m.EntityIDs[0] != "u1" {
		t.Fatalf("entity ids = %v", m.EntityIDs)
	}
	for _, id := range m.ResourceIDs {
		if piiPattern.MatchString(id) {
			t.Fatalf("resource id %q looks like personal data", id)
		}
	}
}

// TestSSE_OccupancyNarrowing: occupancy events fill map_id, resource_ids and nights; malformed values are dropped.
func TestSSE_OccupancyNarrowing(t *testing.T) {
	m := MessageOf(ext.Event{
		Type: event.OccupancyAllocated, MarinaID: "m1",
		Data: json.RawMessage(`{"allocation_ids":["a1"],"resource_ids":["r1","r2"],"map_id":"map1","from":"2027-07-01","to":"2027-07-03","kind":"transit"}`),
	})
	if m.Topic != event.TopicOccupancy || m.MapID != "map1" || len(m.ResourceIDs) != 2 || m.Nights == nil ||
		m.Nights.From != "2027-07-01" || m.Nights.To != "2027-07-03" {
		t.Fatalf("message = %+v", m)
	}
	m = MessageOf(ext.Event{Type: event.OccupancyAllocated, MarinaID: "m1", Data: json.RawMessage(`{"from":"Mario","to":"2027-07-03","resource_ids":[1]}`)})
	if m.Nights != nil || len(m.ResourceIDs) != 0 {
		t.Fatalf("malformed narrowing kept: %+v", m)
	}
}

func TestServe_StreamsTopicsAndReplays(t *testing.T) {
	broker := event.NewBroker(slog.New(slog.DiscardHandler), 16)
	replay := NewReplayBus(broker, 8)
	s := NewServer(replay, replay, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	first := ext.Event{ID: "e1", Type: event.MemberAdded, MarinaID: "m1", Data: json.RawMessage(`{"user_id":"u1"}`)}
	if err := replay.Publish(ctx, first); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.Serve(w, r, "client", "m1", r.Header.Get("Last-Event-ID"))
	}))
	defer srv.Close()

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL, nil)
	req.Header.Set("Last-Event-ID", "e0-unknown")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", want)
				}
				if strings.HasPrefix(l, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %q", want)
			}
		}
	}
	waitFor("retry: 3000")
	waitFor("event: resync")
	// Subscribe happened before headers were written: a published event arrives.
	if err := replay.Publish(ctx, ext.Event{
		ID: "e2", Type: event.MarinaUpdated, MarinaID: "m1",
		Data: json.RawMessage(`{"marina_id":"m1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := replay.Publish(ctx, ext.Event{ID: "x", Type: event.WebhookTest, MarinaID: "m1", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	waitFor("id: e2")
	waitFor("event: marina.changed")
	waitFor(`data: {"topic":"marina.changed","type":"marina.updated","marina_id":"m1","entity_ids":["m1"]}`)

	if got, ok := replay.Since("m1", "e1"); !ok || len(got) != 2 || got[0].ID != "e2" {
		t.Fatalf("since = %v %v", got, ok)
	}
}

func TestServe_CapsConcurrentStreams(t *testing.T) {
	s := NewServer(event.NewBroker(slog.New(slog.DiscardHandler), 1), nil, slog.New(slog.DiscardHandler))
	for range MaxStreamsPerClient {
		if !s.acquire("c") {
			t.Fatal("refused below the cap")
		}
	}
	if err := s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "c", "m1", ""); err != ErrTooManyStreams { //nolint:errorlint // sentinel
		t.Fatalf("err = %v", err)
	}
}
