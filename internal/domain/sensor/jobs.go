package sensor

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Job limits (m12-plan §3.4, §5.3, §7).
const (
	pollLimit       = 500
	pollTimeout     = 20 * time.Second
	silenceBatch    = 500
	retentionChunk  = 5000
	retentionChunks = 200
)

// RunPoll is the job sensors.poll (every 15 s): each enabled source whose last_polled_at + poll_seconds <= now is
// polled (limit 500, timeout 20 s) and fed to IngestSource.
func (s *Service) RunPoll(ctx context.Context) error {
	if len(s.d.Sources) == 0 {
		return nil
	}
	rows, err := store.EnabledSensorSources(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	now := s.d.Clock.Now()
	for _, r := range rows {
		if r.LastPolledAt != nil && now.Sub(*r.LastPolledAt) < time.Duration(r.PollSeconds)*time.Second {
			continue
		}
		if _, err := s.pollSource(ctx, r.MarinaID, r.ID, false); err != nil && !errors.Is(err, ErrSourceBusy) {
			s.d.Log.WarnContext(ctx, "sensor source poll failed", "marina_id", r.MarinaID, "source_id", r.ID, "error_class", errClass(err))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// errClass names an error without its text (a driver error may quote vendor responses or secrets).
func errClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "error"
}

// pollSource polls one source (job or manual) and stores its readings and cursor in one transaction; a driver
// error stores only the error class and keeps the cursor. manual marks pollSensorSourceNow (ErrSourceBusy while
// the job polls the same source).
func (s *Service) pollSource(ctx context.Context, marinaID, sourceID string, manual bool) (*PollResult, error) {
	if _, busy := s.polling.LoadOrStore(sourceID, struct{}{}); busy {
		return nil, ErrSourceBusy
	}
	defer s.polling.Delete(sourceID)
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, notFound(err)
	}
	src, err := store.GetSensorSource(ctx, s.d.DB.DB, scope, sourceID)
	if err != nil {
		return nil, notFound(err)
	}
	if !src.Enabled && !manual {
		return &PollResult{}, nil
	}
	fail := func(code string) (*PollResult, error) {
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			return store.SetSensorSourcePoll(ctx, tx, scope, sourceID, src.Cursor, s.d.Clock.Now(), code)
		})
		if err != nil {
			return nil, err
		}
		return &PollResult{ErrorCode: code}, nil
	}
	drv := s.driver(src.Code)
	if drv == nil {
		return fail(store.SensorSourceErrUnavailable)
	}
	cfg, err := s.decryptConfig(src)
	if err != nil {
		return fail(store.SensorSourceErrAuth)
	}
	pctx, cancel := context.WithTimeout(ctx, pollTimeout)
	readings, next, err := drv.Poll(pctx, ext.SensorSourceConfig{MarinaID: marinaID, Values: cfg, HTTP: s.egress}, src.Cursor, pollLimit)
	timedOut := pctx.Err() != nil
	cancel()
	if err != nil || timedOut {
		code := store.SensorSourceErrUnavailable
		if timedOut || errors.Is(err, context.DeadlineExceeded) {
			code = store.SensorSourceErrTimeout
		}
		return fail(code)
	}
	if len(readings) > pollLimit {
		return fail(store.SensorSourceErrBadResponse)
	}
	return s.IngestSource(ctx, marinaID, sourceID, readings, next)
}

// RunSilence is the job sensors.silence (every minute): devices whose last observed reading is older than
// offline_after_minutes become unknown (reason silence), re-checked under the state row lock, at most 500 devices
// per transaction.
func (s *Service) RunSilence(ctx context.Context) error {
	marinas, err := store.MarinasWithKnownSensorStates(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	for _, m := range marinas {
		if err := s.silenceMarina(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) silenceMarina(ctx context.Context, marinaID string) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return err
	}
	set, err := store.LoadSensorSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return err
	}
	offline := time.Duration(set.OfflineAfterMinutes) * time.Minute
	for {
		now := s.d.Clock.Now()
		ids, err := store.SilentSensorDevices(ctx, s.d.DB.DB, marinaID, now.Add(-offline), silenceBatch)
		if err != nil || len(ids) == 0 {
			return err
		}
		changed := 0
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			changed = 0
			states, err := store.LockSensorDeviceStates(ctx, tx, marinaID, ids)
			if err != nil {
				return err
			}
			devices, err := store.SensorDevicesByIDs(ctx, tx, scope, ids)
			if err != nil {
				return err
			}
			for _, id := range ids {
				st, d := states[id], devices[id]
				if st == nil || d == nil {
					continue
				}
				// Re-check under the lock: a reading may have arrived since the select.
				hs, ch := hysteresis.Silence(toHysteresis(st), now, offline)
				if ch == nil {
					continue
				}
				fromHysteresis(st, hs)
				st.UpdatedAt = store.UTC(now)
				if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
					return err
				}
				if err := s.writeChange(ctx, tx, scope, d, *ch, now); err != nil {
					return err
				}
				changed++
			}
			if changed > 0 {
				store.AfterCommit(ctx, s.wake)
			}
			return nil
		})
		if err != nil {
			return err
		}
		s.metrics.Changes.Add(int64(changed))
		if len(ids) < silenceBatch || changed == 0 {
			return nil
		}
	}
}

// RunRetention is the job sensors.retention (hourly, at start): raw readings older than RawRetention are deleted in
// chunks of 5,000 (at most 200 chunks per run, one transaction each, so ingest never waits more than one chunk);
// state changes are never purged.
func (s *Service) RunRetention(ctx context.Context) error {
	cutoff := s.d.Clock.Now().Add(-s.d.RawRetention)
	for range retentionChunks {
		var n int64
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			n, err = store.DeleteSensorReadingsBefore(ctx, tx, cutoff, retentionChunk)
			return err
		})
		if err != nil {
			return err
		}
		if n < retentionChunk || ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
