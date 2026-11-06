package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCursor_RoundTrip(t *testing.T) {
	id := NewID()
	if !ValidID(id) {
		t.Fatalf("NewID %q not valid", id)
	}
	got, err := DecodeCursor(EncodeCursor(id))
	if err != nil || got != id {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if got, err := DecodeCursor(""); err != nil || got != "" {
		t.Fatal("empty cursor")
	}
	for _, bad := range []string{"!!", EncodeCursor("not-an-id"), "YzE6"} {
		if _, err := DecodeCursor(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNewID_SortsByCreation(t *testing.T) {
	prev := NewID()
	for range 1000 {
		next := NewID()
		if next <= prev {
			t.Fatalf("%s <= %s", next, prev)
		}
		prev = next
	}
}

func TestClampLimitAndPage(t *testing.T) {
	if ClampLimit(0) != DefaultLimit || ClampLimit(-1) != DefaultLimit || ClampLimit(1000) != MaxLimit || ClampLimit(7) != 7 {
		t.Fatal("clamp")
	}
	p := pageOf([]string{"a", "b", "c"}, 2, func(s string) string { return NewID() })
	if len(p.Items) != 2 || p.NextCursor == "" {
		t.Fatalf("%+v", p)
	}
	p = pageOf[string](nil, 2, func(s string) string { return s })
	if p.Items == nil || p.NextCursor != "" {
		t.Fatalf("%+v", p)
	}
}

func TestWriteQueue_FIFOAndCancel(t *testing.T) {
	q := newWriteQueue()
	ctx := context.Background()
	if err := q.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	order := make(chan int, 3)
	for i := range 3 {
		go func() {
			if err := q.acquire(ctx); err != nil {
				t.Error(err)
				return
			}
			order <- i
			q.release()
		}()
		// Wait until goroutine i is queued so that arrival order is deterministic.
		for {
			q.mu.Lock()
			n := len(q.waiters)
			q.mu.Unlock()
			if n == i+1 {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	errc := make(chan error, 1)
	go func() { errc <- q.acquire(cctx) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	q.release()
	for want := range 3 {
		if got := <-order; got != want {
			t.Fatalf("served %d, want %d (FIFO)", got, want)
		}
	}
	if err := q.acquire(ctx); err != nil {
		t.Fatal("queue not free at the end")
	}
}

func TestJSONAndStrings(t *testing.T) {
	var s Strings
	if err := s.Scan(`["a","b"]`); err != nil || len(s) != 2 {
		t.Fatal(s, err)
	}
	if err := s.Scan(nil); err != nil || s == nil || len(s) != 0 {
		t.Fatal("nil scan")
	}
	v, _ := Strings(nil).Value()
	if v != "[]" {
		t.Fatalf("nil value %v", v)
	}
	var j JSON
	if err := j.Scan([]byte(`{ "a" : 1 }`)); err != nil || string(j.Compact()) != `{"a":1}` {
		t.Fatal(string(j), err)
	}
	if v, _ := JSON(nil).Value(); v != nil {
		t.Fatal("empty JSON must be NULL")
	}
	if !UTC(time.Date(2026, 1, 1, 10, 0, 0, 1500, time.FixedZone("x", 3600))).Equal(time.Date(2026, 1, 1, 9, 0, 0, 1000, time.UTC)) {
		t.Fatal("UTC truncation")
	}
}
