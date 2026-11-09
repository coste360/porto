// Package watch is the M12 reconciliation service (spec 24.4-24.5; m12-plan §8-§10): the reconcile job that turns
// physical states, contractual facts and presences into alerts and proposals (pure rules in sensor/reconcile), the
// alert actions that call the existing domain services with the user's permission, the Sensori map context, the
// physical history and the discrepancy rows of the sensors report, and the occupancy.SensorReader of the day view.
//
// The only automatic effects are sensor alerts and, when the marina enables it, the presences of annual mooring
// contracts (presence.RecordSensorTx). Nothing here writes allocations, occupancy rows, releases, contracts or
// charges except through a user-confirmed action (INV-100). Events and SSE carry no personal data (INV-70).
package watch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// wakeDebounce is the minimum gap between two runs woken by the ingestion (m12-plan §8.4).
const wakeDebounce = 2 * time.Second

// Service is the reconciliation service.
type Service struct {
	d       Deps
	metrics *Metrics
	wake    chan struct{}
}

// NewService returns the service. When Deps.Wake is set, a goroutine forwards the ingestion wakes to Wake at most
// once every 2 s (a wake arriving meanwhile is kept and forwarded after the gap).
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.RawRetention <= 0 {
		d.RawRetention = 30 * 24 * time.Hour
	}
	s := &Service{d: d, metrics: &Metrics{}}
	if d.Wake != nil {
		s.wake = make(chan struct{}, 1)
		go s.debounce(d.Wake)
	}
	return s
}

func (s *Service) debounce(in <-chan struct{}) {
	for range in {
		select {
		case s.wake <- struct{}{}:
		default:
		}
		time.Sleep(wakeDebounce)
	}
}

// Metrics returns the reconcile counters.
func (s *Service) Metrics() *Metrics { return s.metrics }

// Wake is the channel the job runner listens to for extra runs (the debounced ingest wake; nil = none).
func (s *Service) Wake() <-chan struct{} {
	if s.wake == nil {
		return nil
	}
	return s.wake
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// authorize checks perm on the marina (another marina is auth.ErrNotFound) and, for each (table, id) pair of refs,
// that the row exists in the marina.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission, refs ...string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	if err != nil {
		return scope, err
	}
	for i := 0; i+1 < len(refs); i += 2 {
		if err := store.SensorExists(ctx, s.d.DB.DB, scope, refs[i], refs[i+1]); err != nil {
			return scope, notFound(err)
		}
	}
	return scope, nil
}

var _ occupancy.SensorReader = (*Service)(nil)
