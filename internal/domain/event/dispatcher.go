package event

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// dispatchLease is how long a claimed event stays invisible to other dispatchers.
const dispatchLease = time.Minute

// Dispatcher moves outbox events to the sinks (at least once) and to the live bus (best effort).
type Dispatcher struct {
	db    *store.DB
	clock clock.Clock
	sinks []ext.EventSink
	bus   ext.EventBus
	log   *slog.Logger
}

// NewDispatcher returns a Dispatcher. bus may be nil.
func NewDispatcher(db *store.DB, c clock.Clock, sinks []ext.EventSink, bus ext.EventBus, log *slog.Logger) *Dispatcher {
	return &Dispatcher{db: db, clock: c, sinks: sinks, bus: bus, log: log}
}

// DispatchPending claims up to limit due events, hands each to every sink and, when all sinks succeeded, marks it
// dispatched and publishes it on the bus. A failing sink leaves the event pending with a backoff. It returns the
// number of events dispatched.
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	now := store.UTC(d.clock.Now())
	var rows []store.OutboxEvent
	err := d.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		rows, err = store.ClaimOutboxEvents(ctx, tx, now, dispatchLease, limit)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("event: claim outbox: %w", err)
	}
	done := 0
	for i := range rows {
		ev := FromRow(&rows[i])
		if err := d.handle(ctx, ev); err != nil {
			next := store.UTC(d.clock.Now().Add(Backoff(rows[i].Attempts + 1)))
			d.log.WarnContext(ctx, "event dispatch failed", "event_id", ev.ID, "event_type", ev.Type,
				"attempts", rows[i].Attempts+1, "error", err)
			if err := store.MarkOutboxRetry(ctx, d.db.DB, ev.ID, next, err.Error(), store.UTC(d.clock.Now())); err != nil {
				return done, fmt.Errorf("event: mark retry: %w", err)
			}
			continue
		}
		if err := store.MarkOutboxDispatched(ctx, d.db.DB, ev.ID, store.UTC(d.clock.Now())); err != nil {
			return done, fmt.Errorf("event: mark dispatched: %w", err)
		}
		done++
		if d.bus != nil && TopicOf(ev.Type) != "" && ev.MarinaID != "" {
			if err := d.bus.Publish(ctx, ev); err != nil {
				d.log.WarnContext(ctx, "event publish failed", "event_id", ev.ID, "error", err)
			}
		}
	}
	return done, nil
}

func (d *Dispatcher) handle(ctx context.Context, ev ext.Event) error {
	var errs []error
	for _, s := range d.sinks {
		if err := callSink(ctx, s, ev); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func callSink(ctx context.Context, s ext.EventSink, ev ext.Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("sink panicked: %v", r)
		}
	}()
	return s.Handle(ctx, ev)
}

// piiKeys are the substrings that must never appear in event data keys (TestEvents_DataHasNoPII).
var piiKeys = []string{"email", "phone", "name", "token", "secret", "password", "address"}

// CheckNoPII returns an error when a key of the JSON object data (at any depth) looks like personal data.
func CheckNoPII(data map[string]any) error {
	for k, v := range data {
		lk := strings.ToLower(k)
		for _, bad := range piiKeys {
			if strings.Contains(lk, bad) {
				return fmt.Errorf("event data key %q looks like personal data", k)
			}
		}
		if m, ok := v.(map[string]any); ok {
			if err := CheckNoPII(m); err != nil {
				return err
			}
		}
	}
	return nil
}
