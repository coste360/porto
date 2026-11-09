package fakesensor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

func TestFakeSensor_PollCursor(t *testing.T) {
	s := New()
	t0 := time.Date(2027, 6, 12, 8, 0, 0, 0, time.UTC)
	for i := range 5 {
		s.Push("m1", ext.SensorReading{DeviceExternalID: "D1", ObservedAt: t0.Add(time.Duration(i) * time.Minute), State: "free"})
	}
	s.Push("m2", ext.SensorReading{DeviceExternalID: "X", ObservedAt: t0, State: "free"})
	cfg := ext.SensorSourceConfig{MarinaID: "m1"}
	rs, next, err := s.Poll(context.Background(), cfg, "", 3)
	if err != nil || len(rs) != 3 || next != "3" {
		t.Fatalf("%v %d %q", err, len(rs), next)
	}
	// Idempotent for a cursor.
	again, next2, _ := s.Poll(context.Background(), cfg, "", 3)
	if len(again) != 3 || next2 != next || !again[0].ObservedAt.Equal(rs[0].ObservedAt) {
		t.Fatal("not idempotent")
	}
	rs, next, _ = s.Poll(context.Background(), cfg, next, 3)
	if len(rs) != 2 || next != "5" {
		t.Fatalf("%d %q", len(rs), next)
	}
	rs, next, _ = s.Poll(context.Background(), cfg, next, 3)
	if len(rs) != 0 || next != "5" {
		t.Fatalf("%d %q", len(rs), next)
	}
	s.Fail(ErrUnavailable)
	if _, c, err := s.Poll(context.Background(), cfg, "5", 3); !errors.Is(err, ErrUnavailable) || c != "5" {
		t.Fatal("fail")
	}
	if _, _, err := s.Poll(context.Background(), cfg, "x", 3); err == nil {
		t.Fatal("bad cursor")
	}
	if s.Polls() != 6 {
		t.Fatalf("polls %d", s.Polls())
	}
}
