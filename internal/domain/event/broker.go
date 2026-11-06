package event

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/coste360/porto/pkg/ext"
)

// defaultBrokerCapacity is the per-subscription queue length used when NewBroker gets a non-positive one.
const defaultBrokerCapacity = 64

// Broker is the in-process ext.EventBus behind the SSE streams. Each marina is a topic; a subscription is a
// bounded queue. Publish never waits: when a subscription's queue is full the event is skipped for that
// subscription only and counted in Dropped (SSE clients recover through Last-Event-ID replay).
//
// Concurrency: one mutex guards the topic table. Publish sends (non-blocking) while holding it and a
// subscription's channel is closed only while holding it, so a send on a closed channel cannot happen.
type Broker struct {
	log      *slog.Logger
	capacity int

	mu     sync.Mutex
	seq    uint64
	topics map[string]map[uint64]chan ext.Event // marina id -> subscription id -> queue

	dropped atomic.Int64
}

var _ ext.EventBus = (*Broker)(nil)

// NewBroker returns a Broker whose subscriptions queue up to capacity events each.
func NewBroker(log *slog.Logger, capacity int) *Broker {
	if capacity <= 0 {
		capacity = defaultBrokerCapacity
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Broker{log: log, capacity: capacity, topics: make(map[string]map[uint64]chan ext.Event)}
}

// Publish offers ev to every live subscription of ev.MarinaID. Events without a marina have no live audience.
func (b *Broker) Publish(_ context.Context, ev ext.Event) error {
	if ev.MarinaID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, queue := range b.topics[ev.MarinaID] {
		select {
		case queue <- ev:
		default:
			b.dropped.Add(1)
			b.log.Debug("live event skipped for a slow subscriber", "marina_id", ev.MarinaID, "event_type", ev.Type)
		}
	}
	return nil
}

// Subscribe opens a subscription to the events of marinaID. It ends, closing the channel, when the returned
// function is called (any number of times) or when ctx is done.
func (b *Broker) Subscribe(ctx context.Context, marinaID string) (<-chan ext.Event, func(), error) {
	if marinaID == "" {
		return nil, nil, errors.New("event: subscribe needs a marina id")
	}
	queue := make(chan ext.Event, b.capacity)

	b.mu.Lock()
	b.seq++
	id := b.seq
	topic := b.topics[marinaID]
	if topic == nil {
		topic = make(map[uint64]chan ext.Event)
		b.topics[marinaID] = topic
	}
	topic[id] = queue
	b.mu.Unlock()

	end := func() { b.unsubscribe(marinaID, id) }
	stop := context.AfterFunc(ctx, end)
	return queue, func() {
		stop()
		end()
	}, nil
}

// unsubscribe removes and closes one subscription; removing an unknown one is a no-op, which makes it idempotent.
func (b *Broker) unsubscribe(marinaID string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	topic := b.topics[marinaID]
	queue, ok := topic[id]
	if !ok {
		return
	}
	delete(topic, id)
	if len(topic) == 0 {
		delete(b.topics, marinaID)
	}
	close(queue)
}

// Dropped is the number of (event, subscription) deliveries skipped because the subscription was full.
func (b *Broker) Dropped() int64 { return b.dropped.Load() }

// Subscribers is the number of open subscriptions across all marinas (tests, metrics).
func (b *Broker) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	total := 0
	for _, topic := range b.topics {
		total += len(topic)
	}
	return total
}
