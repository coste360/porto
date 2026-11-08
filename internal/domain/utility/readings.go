package utility

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/utility/consume"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Readings (m9-plan §9.2): cumulative indexes recorded from the phone (offline kind utility.reading). Anomalies
// are flagged, never refused; the office confirms or voids them. A replay with the same client_ref returns the
// stored reading (INV-83).

const (
	maxNoteRunes   = 500
	maxClientRef   = 100
	futureTolerate = 5 * time.Minute
)

var readingKinds = []string{store.ReadingRegular, store.ReadingInitial, store.ReadingReplacementOld, store.ReadingReplacementNew}

// AnomalyCodesOf decodes the anomaly codes of a reading.
func AnomalyCodesOf(r *store.MeterReading) []string {
	out := []string{}
	_ = json.Unmarshal(r.AnomalyCodes, &out)
	return out
}

// meterParams returns the pure meter parameters, the daily threshold falling back to the utility settings.
func meterParams(m *store.UtilityMeter, set *store.UtilitySettings) consume.Meter {
	out := consume.Meter{MultiplierMilli: m.MultiplierMilli, RolloverMilli: m.RolloverMilli, DailyThresholdMilli: m.DailyThresholdMilli}
	if out.DailyThresholdMilli == nil && set != nil {
		switch m.Kind {
		case store.MeterPower:
			out.DailyThresholdMilli = set.PowerDailyThresholdMilli
		case store.MeterWater:
			out.DailyThresholdMilli = set.WaterDailyThresholdMilli
		}
	}
	return out
}

func pureReading(r *store.MeterReading) consume.Reading {
	return consume.Reading{ID: r.ID, At: r.ReadAt, On: r.ReadOn, Value: qty.Milli(r.ValueMilli), Kind: r.Kind, Status: r.Status}
}

// readAt chooses the instant of a reading: the given one, else Porto-Offline-Queued-At of a replay within 48 hours
// and not in the future, else now.
func readAt(in ReadingInput, now time.Time) (time.Time, error) {
	if in.ReadAt != nil {
		if in.ReadAt.After(now.Add(futureTolerate)) {
			v := &auth.ValidationError{}
			v.Add("read_at", "future")
			return time.Time{}, v
		}
		return in.ReadAt.UTC(), nil
	}
	if !in.QueuedAt.IsZero() && !in.QueuedAt.After(now) && now.Sub(in.QueuedAt) <= OfflineReplayWindow {
		return in.QueuedAt.UTC(), nil
	}
	return now, nil
}

// ListReadings lists readings, newest first, with the delta from the previous valid reading (marina.configure,
// presence.write or charges.manage).
func (s *Service) ListReadings(ctx context.Context, p *auth.Principal, marinaID string, f ReadingFilter) ([]ReadingView, string, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMarinaConfigure, auth.PermPresenceWrite, auth.PermChargesManage)
	if err != nil {
		return nil, "", err
	}
	sf := store.MeterReadingFilter{MeterID: f.MeterID, Status: f.Status}
	if f.From != nil {
		sf.From = *f.From
	}
	if f.To != nil {
		sf.To = *f.To
	}
	if f.Status != "" && !contains([]string{store.ReadingValid, store.ReadingAnomaly, store.ReadingConfirmed, store.ReadingVoid}, f.Status) {
		v := &auth.ValidationError{}
		v.Add("status", "invalid")
		return nil, "", v
	}
	page, err := store.ListMeterReadings(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.readingViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// readingViews adds the delta from the previous non-void reading of the same meter (after the multiplier).
func (s *Service) readingViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.MeterReading) ([]ReadingView, error) {
	out := make([]ReadingView, 0, len(rows))
	series := map[string][]store.MeterReading{}
	meters := map[string]*store.UtilityMeter{}
	for _, r := range rows {
		if _, ok := series[r.MeterID]; ok {
			continue
		}
		rs, err := store.MeterReadingsOf(ctx, db, scope, r.MeterID)
		if err != nil {
			return nil, err
		}
		series[r.MeterID] = rs
		m, err := store.GetUtilityMeter(ctx, db, scope, r.MeterID)
		if err != nil {
			return nil, err
		}
		meters[r.MeterID] = m
	}
	for _, r := range rows {
		v := ReadingView{Row: r}
		if r.Status != store.ReadingVoid {
			rs := series[r.MeterID]
			for i := range rs {
				if rs[i].ID != r.ID || i == 0 {
					continue
				}
				if r.Kind == store.ReadingInitial || r.Kind == store.ReadingReplacementNew {
					break
				}
				prev, cur := pureReading(&rs[i-1]), pureReading(&rs[i])
				iv := consume.Intervals(meterParams(meters[r.MeterID], nil), []consume.Reading{prev, cur})
				if len(iv) == 1 {
					d := iv[0].Delta
					v.Delta = &d
				}
				break
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) readingView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*ReadingView, error) {
	r, err := store.GetMeterReading(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.readingViews(ctx, db, scope, []store.MeterReading{*r})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// RecordReading records a reading (presence.write). Offline replays with the same ClientRef return the stored
// reading; anomalies are flagged (status anomaly) and the consumptions of the meter are recomputed.
func (s *Service) RecordReading(ctx context.Context, p *auth.Principal, marinaID string, in ReadingInput) (*ReadingView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	value, perr := qty.Parse(in.Value)
	if perr != nil || value < 0 {
		v.Add("value", "invalid")
	}
	if in.Kind == "" {
		in.Kind = store.ReadingRegular
	}
	if !contains(readingKinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	note, ok := cleanText(in.Note, maxNoteRunes)
	if !ok {
		v.Add("note", "too_long")
	}
	if len(in.ClientRef) > maxClientRef {
		v.Add("client_ref", "too_long")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if in.ClientRef != "" {
		if prev, err := store.MeterReadingByClientRef(ctx, s.d.DB.DB, scope, in.ClientRef); err == nil {
			return s.readingView(ctx, s.d.DB.DB, scope, prev.ID)
		}
	}
	now := s.now()
	at, err := readAt(in, now)
	if err != nil {
		return nil, err
	}
	id := store.NewID()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.LockUtilityMeter(ctx, tx, scope, in.MeterID)
		if err != nil {
			return notFound(err)
		}
		if !m.Active {
			return ErrElementInvalid
		}
		set, err := store.GetUtilitySettings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		r := &store.MeterReading{
			ID: id, MeterID: m.ID, ReadAt: at, ReadOn: calendar.In(at, location(ctx, tx, scope)), ValueMilli: int64(value),
			Kind: in.Kind, Source: "staff", UserID: p.UserID, Note: note, Status: store.ReadingValid, ClientRef: in.ClientRef,
			CreatedAt: now, UpdatedAt: now,
		}
		codes, err := s.anomalies(ctx, tx, scope, m, set, r)
		if err != nil {
			return err
		}
		if len(codes) > 0 {
			r.Status = store.ReadingAnomaly
		}
		r.AnomalyCodes = store.MustJSON(codes)
		inserted, err := store.InsertMeterReadingIfAbsent(ctx, tx, scope, r)
		if err != nil {
			return err
		}
		if !inserted {
			if in.ClientRef != "" {
				if prev, err := store.MeterReadingByClientRef(ctx, tx, scope, in.ClientRef); err == nil {
					id = prev.ID // a concurrent replay committed first
					return nil
				}
			}
			ve := &auth.ValidationError{}
			ve.Add("read_at", "duplicate")
			return ve
		}
		if _, err := s.recompute(ctx, tx, scope, m); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "meter_reading.record", "meter_reading", r.ID,
			map[string]any{"meter_id": m.ID, "kind": r.Kind, "status": r.Status, "anomalies": codes}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "reading", r.ID, m.ID, r.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.readingView(ctx, s.d.DB.DB, scope, id)
}

// anomalies checks r against the previous non-void reading of the meter and the latest charged one.
func (s *Service) anomalies(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m *store.UtilityMeter, set *store.UtilitySettings,
	r *store.MeterReading,
) ([]string, error) {
	rs, err := store.MeterReadingsOf(ctx, tx, scope, m.ID)
	if err != nil {
		return nil, err
	}
	var prev consume.Reading
	for i := range rs {
		if !rs[i].ReadAt.Before(r.ReadAt) {
			break
		}
		prev = pureReading(&rs[i])
	}
	lastCharged, err := store.LastChargedReadAt(ctx, tx, scope, m.ID)
	if err != nil {
		return nil, err
	}
	return consume.Check(meterParams(m, set), prev, pureReading(r), lastCharged), nil
}

// ConfirmReading confirms an anomalous reading (charges.manage, If-Match): its consumptions become chargeable. A
// confirmed reading inside a charged interval reallocates it with correction deltas (D23).
func (s *Service) ConfirmReading(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) (*ReadingView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetMeterReading(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		m, err := store.LockUtilityMeter(ctx, tx, scope, r.MeterID)
		if err != nil {
			return err
		}
		if int64(r.Version) != version {
			return ErrVersionMismatch
		}
		if r.Status != store.ReadingAnomaly {
			return ErrInvalidTransition
		}
		r.Status = store.ReadingConfirmed
		if err := store.UpdateMeterReading(ctx, tx, scope, r, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if _, err := s.recompute(ctx, tx, scope, m); err != nil {
			return err
		}
		if err := s.correct(ctx, tx, scope, m, r); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "meter_reading.confirm", "meter_reading", r.ID, map[string]any{"meter_id": m.ID}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "reading", r.ID, m.ID, r.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.readingView(ctx, s.d.DB.DB, scope, id)
}

// VoidReading voids a reading (charges.manage, If-Match; ErrReadingCharged when it bounds a charged consumption).
func (s *Service) VoidReading(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*ReadingView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, maxNoteRunes)
	if !ok || reason == "" {
		v := &auth.ValidationError{}
		v.Add("reason", "invalid")
		return nil, v
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetMeterReading(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		m, err := store.LockUtilityMeter(ctx, tx, scope, r.MeterID)
		if err != nil {
			return err
		}
		if int64(r.Version) != version {
			return ErrVersionMismatch
		}
		if r.Status == store.ReadingVoid {
			return ErrInvalidTransition
		}
		charged, err := store.ReadingCharged(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		if charged {
			return ErrReadingCharged
		}
		r.Status, r.VoidReason = store.ReadingVoid, reason
		if err := store.UpdateMeterReading(ctx, tx, scope, r, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if _, err := s.recompute(ctx, tx, scope, m); err != nil {
			if errors.Is(err, errFrozenEdge) {
				return ErrReadingCharged
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "meter_reading.void", "meter_reading", r.ID, map[string]any{"meter_id": m.ID}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "reading", r.ID, m.ID, r.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.readingView(ctx, s.d.DB.DB, scope, id)
}
