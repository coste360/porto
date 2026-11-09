package sensor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/sensor/hysteresis"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Protocol limits (m12-plan §4.1, D5).
const (
	MaxBatch       = 500
	MaxBodyBytes   = 256 << 10
	futureSkew     = 120 * time.Second
	maxReadingAge  = 72 * time.Hour
	ingestBudget   = 10 * time.Second
	maxFirmwareLen = 64
)

// item is one validated reading of a batch, resolved to its device.
type item struct {
	index    int
	device   *store.SensorDevice
	observed time.Time // UTC, truncated to ms
	state    string
	distance *int64
	battery  *int
	rssi     *int
	firmware string
}

// caller is the authenticated key of a request.
type caller struct {
	scope   store.MarinaScope
	device  *store.SensorDevice  // device key
	gateway *store.SensorGateway // gateway key
}

// Ingest authenticates a batch with its device or gateway key (sensor/ingestauth), resolves the marina from the
// key (INV-102) and stores the readings in one transaction (m12-plan §4.4): readings ON CONFLICT DO NOTHING,
// hysteresis fold of the new readings from the locked device states, health, state changes and their
// sensor.state_changed events. After commit it wakes the reconcile job when a change was written. Errors:
// ErrIngestUnauthorized, *TimestampError, *RateLimitError, ErrInvalidPayload, ErrBusy.
func (s *Service) Ingest(ctx context.Context, req IngestRequest) (*IngestResult, error) {
	now := s.d.Clock.Now()
	c, err := s.authenticate(ctx, req, now)
	if err != nil {
		return nil, err
	}
	raws, err := decodeBatch(req.Body)
	if err != nil {
		return nil, err
	}
	res := &IngestResult{Rejected: []Rejected{}, ServerTime: now.UTC()}
	ids := make([]string, 0, len(raws))
	for _, r := range raws {
		if r.DeviceID != "" {
			ids = append(ids, r.DeviceID)
		}
	}
	var devices map[string]*store.SensorDevice
	if c.device != nil {
		devices = map[string]*store.SensorDevice{c.device.ExternalID: c.device}
	} else {
		devices, err = store.SensorDevicesByExternalIDs(ctx, s.d.DB.DB, c.scope, ids)
		if err != nil {
			return nil, err
		}
	}
	origin := store.SensorOriginDevice
	if c.gateway != nil {
		origin = store.SensorOriginGateway
	}
	items := make([]item, 0, len(raws))
	for i, r := range raws {
		d := devices[r.DeviceID]
		code := ""
		switch {
		case r.bad || r.DeviceID == "":
			code = ItemInvalidValue
		case c.device != nil && r.DeviceID != c.device.ExternalID:
			code = ItemDeviceMismatch
		case d == nil:
			code = ItemUnknownDevice
		case c.gateway != nil && d.GatewayID != c.gateway.ID:
			code = ItemDeviceNotBound
		case !d.Active:
			code = ItemDeviceInactive
		}
		if code != "" {
			res.Rejected = append(res.Rejected, Rejected{Index: i, DeviceID: printable(r.DeviceID), Code: code})
			continue
		}
		it, code := s.validate(r, now)
		if code != "" {
			res.Rejected = append(res.Rejected, Rejected{Index: i, DeviceID: printable(r.DeviceID), Code: code})
			continue
		}
		it.index, it.device = i, d
		items = append(items, it)
	}
	// recheck re-applies the device checks to the row re-read under the state lock (INV-102: lock then re-check).
	recheck := func(d *store.SensorDevice) string {
		switch {
		case d == nil:
			return ItemUnknownDevice
		case c.device != nil && d.ID != c.device.ID:
			return ItemDeviceMismatch
		case c.gateway != nil && d.GatewayID != c.gateway.ID:
			return ItemDeviceNotBound
		case !d.Active:
			return ItemDeviceInactive
		}
		return ""
	}
	out, err := s.store(ctx, c.scope, items, origin, now, recheck, func(ctx context.Context, tx bun.Tx) error {
		if c.gateway == nil {
			return nil
		}
		if err := store.TouchSensorGateway(ctx, tx, c.scope, c.gateway.ID, now); err != nil {
			return err
		}
		// The touch holds the gateway row lock (PostgreSQL): a deactivation committed meanwhile is seen here.
		g, err := store.GetSensorGateway(ctx, tx, c.scope, c.gateway.ID)
		if err != nil || !g.Active {
			return ErrIngestUnauthorized
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Accepted, res.Duplicates = out.accepted, out.duplicates
	if len(out.rejected) > 0 {
		res.Rejected = append(res.Rejected, out.rejected...)
		sort.SliceStable(res.Rejected, func(i, j int) bool { return res.Rejected[i].Index < res.Rejected[j].Index })
	}
	return res, nil
}

// authenticate parses the headers, looks the key id up (any marina), applies the per-key limit, then verifies the
// signature or the bearer in constant time (m12-plan §3.3, §4.2, §4.6).
func (s *Service) authenticate(ctx context.Context, req IngestRequest, now time.Time) (*caller, error) {
	p, err := ingestauth.ParseHeaders(ingestauth.Headers{PortoKey: req.PortoKey, PortoSignature: req.PortoSignature, Authorization: req.Authorization})
	if err != nil {
		return nil, ErrIngestUnauthorized
	}
	if p.Mode == ingestauth.ModeBearer && !req.Secure {
		return nil, ErrIngestUnauthorized
	}
	c := &caller{}
	var cols keyCols
	var marinaID, aad string
	active := false
	if p.Gateway {
		g, err := store.SensorGatewayByKeyID(ctx, s.d.DB.DB, p.KeyID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if g != nil {
			c.gateway, cols, marinaID, aad, active = g, gatewayKeyCols(g), g.MarinaID, aadGateway+g.ID, g.Active
		}
	} else {
		d, err := store.SensorDeviceByKeyID(ctx, s.d.DB.DB, p.KeyID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if d != nil {
			// An inactive device still authenticates: its items are refused with device_inactive.
			c.device, cols, marinaID, aad, active = d, deviceKeyCols(d), d.MarinaID, aadDevice+d.ID, true
		}
	}
	verifierEnc, hash := "", ""
	if c.device != nil || c.gateway != nil {
		verifierEnc, hash = matchKey(cols, p.KeyID, now)
		if verifierEnc != "" && s.d.Limiter != nil {
			if ok, retry := s.d.Limiter.Allow(p.KeyID, p.Gateway); !ok {
				return nil, &RateLimitError{RetryAfter: retry}
			}
		}
	}
	switch p.Mode {
	case ingestauth.ModeBearer:
		if !ingestauth.VerifyBearer(p.Bearer, hash) || hash == "" {
			return nil, ErrIngestUnauthorized
		}
	default:
		key := ""
		if verifierEnc != "" {
			if plain, err := s.d.Keys.DecryptString(verifierEnc, aad); err == nil {
				key = plain
			}
		}
		switch err := ingestauth.VerifyHMAC(key, p, req.Method, req.Path, req.Body, now); {
		case errors.Is(err, ingestauth.ErrTimestampOutOfRange):
			return nil, &TimestampError{ServerTime: now}
		case err != nil:
			return nil, ErrIngestUnauthorized
		}
	}
	if !active {
		return nil, ErrIngestUnauthorized
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrIngestUnauthorized
		}
		return nil, err
	}
	c.scope = scope
	return c, nil
}

// rawReading is one item as sent; fields are decoded one by one so that a bad value rejects the item, not the
// batch (unknown fields are ignored).
type rawReading struct {
	DeviceID   string
	ObservedAt json.RawMessage
	State      json.RawMessage
	DistanceCm json.RawMessage
	BatteryPct json.RawMessage
	RSSI       json.RawMessage
	Firmware   json.RawMessage
	bad        bool
}

// decodeBatch parses {"readings": [...]} with 1..MaxBatch objects (ErrInvalidPayload otherwise).
func decodeBatch(body []byte) ([]rawReading, error) {
	var doc struct {
		Readings *[]json.RawMessage `json:"readings"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil || dec.More() || doc.Readings == nil {
		return nil, ErrInvalidPayload
	}
	n := len(*doc.Readings)
	if n == 0 || n > MaxBatch {
		return nil, ErrInvalidPayload
	}
	out := make([]rawReading, 0, n)
	for _, raw := range *doc.Readings {
		var fields map[string]json.RawMessage
		var r rawReading
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			r.bad = true
			out = append(out, r)
			continue
		}
		if v, ok := fields["device_id"]; ok {
			if json.Unmarshal(v, &r.DeviceID) != nil {
				r.bad = true
			}
		}
		r.ObservedAt, r.State, r.DistanceCm = fields["observed_at"], fields["state"], fields["distance_cm"]
		r.BatteryPct, r.RSSI, r.Firmware = fields["battery_pct"], fields["rssi"], fields["firmware"]
		out = append(out, r)
	}
	return out, nil
}

func isNull(m json.RawMessage) bool { return len(m) == 0 || string(m) == "null" }

func optInt(m json.RawMessage, lo, hi int64) (*int64, bool) {
	if isNull(m) {
		return nil, true
	}
	var n int64
	if err := json.Unmarshal(m, &n); err != nil || n < lo || n > hi {
		return nil, false
	}
	return &n, true
}

// validate checks the values of one reading (m12-plan §4.1) and returns the item or the rejection code.
func (s *Service) validate(r rawReading, now time.Time) (item, string) {
	if r.bad || r.DeviceID == "" || isNull(r.ObservedAt) {
		return item{}, ItemInvalidValue
	}
	var ts string
	if json.Unmarshal(r.ObservedAt, &ts) != nil {
		return item{}, ItemInvalidValue
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return item{}, ItemInvalidValue
	}
	t = t.UTC().Truncate(time.Millisecond)
	if t.After(now.Add(futureSkew)) {
		return item{}, ItemObservedInFuture
	}
	if t.Before(now.Add(-min(maxReadingAge, s.d.RawRetention))) {
		return item{}, ItemObservedTooOld
	}
	var st string
	if isNull(r.State) || json.Unmarshal(r.State, &st) != nil ||
		(st != store.SensorOccupied && st != store.SensorFree && st != store.SensorUnknown) {
		return item{}, ItemInvalidState
	}
	it := item{observed: t, state: st}
	var ok bool
	if it.distance, ok = optInt(r.DistanceCm, 0, 10000); !ok {
		return item{}, ItemInvalidValue
	}
	b, ok := optInt(r.BatteryPct, 0, 100)
	if !ok {
		return item{}, ItemInvalidValue
	}
	rs, ok := optInt(r.RSSI, -200, 0)
	if !ok {
		return item{}, ItemInvalidValue
	}
	if b != nil {
		v := int(*b)
		it.battery = &v
	}
	if rs != nil {
		v := int(*rs)
		it.rssi = &v
	}
	if !isNull(r.Firmware) {
		if json.Unmarshal(r.Firmware, &it.firmware) != nil || utf8.RuneCountInString(it.firmware) > maxFirmwareLen {
			return item{}, ItemInvalidValue
		}
	}
	return it, ""
}

// printable returns the external id as sent when it is a well-formed id, "" otherwise (never echo arbitrary
// bytes back).
func printable(id string) string {
	if reExternalID.MatchString(id) {
		return id
	}
	return ""
}

// IngestSource stores the readings of a polled source (job sensors.poll and pollSensorSourceNow) through the same
// transaction as Ingest, together with the new cursor (a crash re-polls the same cursor; idempotency makes it
// harmless). Readings of devices not bound to the source are rejected.
func (s *Service) IngestSource(ctx context.Context, marinaID, sourceID string, readings []ext.SensorReading, next string) (*PollResult, error) {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, err
	}
	now := s.d.Clock.Now()
	if len(next) > 512 {
		next = next[:512]
	}
	ids := make([]string, 0, len(readings))
	for _, r := range readings {
		ids = append(ids, r.DeviceExternalID)
	}
	devices, err := store.SensorDevicesByExternalIDs(ctx, s.d.DB.DB, scope, ids)
	if err != nil {
		return nil, err
	}
	res := &PollResult{}
	items := make([]item, 0, len(readings))
	for i, r := range readings {
		d := devices[r.DeviceExternalID]
		if d == nil || d.SourceID != sourceID || !d.Active {
			res.Rejected++
			continue
		}
		raw := rawReading{DeviceID: r.DeviceExternalID}
		raw.ObservedAt, _ = json.Marshal(r.ObservedAt.UTC().Format(time.RFC3339Nano))
		raw.State, _ = json.Marshal(r.State)
		if r.DistanceCm != nil {
			raw.DistanceCm, _ = json.Marshal(*r.DistanceCm)
		}
		if r.BatteryPct != nil {
			raw.BatteryPct, _ = json.Marshal(*r.BatteryPct)
		}
		if r.RSSI != nil {
			raw.RSSI, _ = json.Marshal(*r.RSSI)
		}
		if r.Firmware != "" {
			raw.Firmware, _ = json.Marshal(r.Firmware)
		}
		it, code := s.validate(raw, now)
		if code != "" {
			res.Rejected++
			continue
		}
		it.index, it.device = i, d
		items = append(items, it)
	}
	recheck := func(d *store.SensorDevice) string {
		if d == nil || d.SourceID != sourceID || !d.Active {
			return ItemDeviceNotBound
		}
		return ""
	}
	out, err := s.store(ctx, scope, items, store.SensorOriginSource, now, recheck, func(ctx context.Context, tx bun.Tx) error {
		return store.SetSensorSourcePoll(ctx, tx, scope, sourceID, next, now, "")
	})
	if err != nil {
		return nil, err
	}
	res.Accepted, res.Duplicates = out.accepted, out.duplicates
	res.Rejected += len(out.rejected)
	if res.Rejected > 0 {
		s.d.Log.InfoContext(ctx, "sensor source readings rejected", "marina_id", scope.ID(), "source_id", sourceID, "rejected", res.Rejected)
	}
	return res, nil
}

// testHookBeforeStoreTx runs between the device reads of a batch and its transaction (tests of lock then
// re-check only; nil in production).
var testHookBeforeStoreTx func()

type storeResult struct {
	accepted, duplicates, changes int
	rejected                      []Rejected
}

func readingKey(deviceID string, t time.Time) string {
	return deviceID + "|" + t.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
}

// store is the ingest transaction of m12-plan §4.4 for validated items; extra runs inside the same transaction
// (gateway contact, source cursor). The devices are re-read inside the transaction once their state rows are
// locked (UpdateDevice locks the same state row, so both sides serialise on it): recheck returns the rejection
// code of an item whose device no longer qualifies (deactivated, unbound), and the re-read row is the one the
// state change records (a device moved meanwhile is recorded on its new resource, after the reset).
func (s *Service) store(ctx context.Context, scope store.MarinaScope, items []item, origin string, now time.Time,
	recheck func(*store.SensorDevice) string, extra func(context.Context, bun.Tx) error,
) (storeResult, error) {
	var res storeResult
	// Duplicates inside the batch: the first occurrence wins, the others are counted as duplicates.
	seen := make(map[string]bool, len(items))
	uniq := items[:0:0]
	for _, it := range items {
		k := readingKey(it.device.ID, it.observed)
		if seen[k] {
			res.duplicates++
			continue
		}
		seen[k] = true
		uniq = append(uniq, it)
	}
	sort.SliceStable(uniq, func(i, j int) bool {
		if uniq[i].device.ID != uniq[j].device.ID {
			return uniq[i].device.ID < uniq[j].device.ID
		}
		return uniq[i].observed.Before(uniq[j].observed)
	})
	var allIDs []string
	seenID := map[string]bool{}
	for _, it := range uniq {
		if !seenID[it.device.ID] {
			seenID[it.device.ID] = true
			allIDs = append(allIDs, it.device.ID)
		}
	}
	all := uniq
	ctx, cancel := context.WithTimeout(ctx, ingestBudget)
	defer cancel()
	start := s.d.Clock.Now()
	var deviceIDs []string
	var devices map[string]*store.SensorDevice
	inTx := func(ctx context.Context, tx bun.Tx) error {
		res.accepted, res.changes, res.rejected = 0, 0, nil
		uniq, deviceIDs = all, allIDs
		if extra != nil {
			if err := extra(ctx, tx); err != nil {
				return err
			}
		}
		if len(uniq) == 0 {
			return nil
		}
		set, err := store.LoadSensorSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		params := hysteresis.Params{Readings: set.HysteresisReadings, Window: time.Duration(set.HysteresisMinutes) * time.Minute}
		if err := store.EnsureSensorDeviceStates(ctx, tx, scope.ID(), deviceIDs, now); err != nil {
			return err
		}
		states, err := store.LockSensorDeviceStates(ctx, tx, scope.ID(), deviceIDs)
		if err != nil {
			return err
		}
		// Lock then re-check: the device rows as committed now, after the state locks.
		devices, err = store.SensorDevicesByIDs(ctx, tx, scope, deviceIDs)
		if err != nil {
			return err
		}
		kept := make([]item, 0, len(uniq))
		for _, it := range uniq {
			d := devices[it.device.ID]
			if code := recheck(d); code != "" {
				res.rejected = append(res.rejected, Rejected{Index: it.index, DeviceID: printable(it.device.ExternalID), Code: code})
				continue
			}
			it.device = d
			kept = append(kept, it)
		}
		uniq = kept
		deviceIDs = deviceIDs[:0:0]
		for _, id := range allIDs {
			if d := devices[id]; d != nil && recheck(d) == "" {
				deviceIDs = append(deviceIDs, id)
			}
		}
		if len(uniq) == 0 {
			return nil
		}
		// received_at is read under the state locks: its order is the commit order of the device's readings.
		received := store.UTC(s.d.Clock.Now())
		rows := make([]store.SensorReading, 0, len(uniq))
		for _, it := range uniq {
			rows = append(rows, store.SensorReading{
				DeviceID: it.device.ID, ObservedAt: it.observed, MarinaID: scope.ID(), ReceivedAt: received, State: it.state,
				DistanceCm: it.distance, BatteryPct: it.battery, RSSI: it.rssi, Origin: origin,
			})
		}
		inserted, err := store.InsertSensorReadings(ctx, tx, rows)
		if err != nil {
			return err
		}
		isNew := make(map[string]bool, len(inserted))
		for _, k := range inserted {
			isNew[readingKey(k.DeviceID, k.ObservedAt)] = true
		}
		res.accepted = len(inserted)
		// Fold the new readings of each device from its locked state.
		for _, id := range deviceIDs {
			st := states[id]
			if st == nil {
				return errors.New("sensor: state row missing after ensure")
			}
			hs := toHysteresis(st)
			var applied []*item
			var changes []hysteresis.Change
			for i := range uniq {
				it := &uniq[i]
				if it.device.ID != id || !isNew[readingKey(id, it.observed)] {
					continue
				}
				var ch *hysteresis.Change
				var outcome hysteresis.Outcome
				hs, ch, outcome = hysteresis.Step(hs, hysteresis.Reading{ObservedAt: it.observed, State: it.state}, params)
				if outcome == hysteresis.Applied {
					applied = append(applied, it)
				}
				if ch != nil {
					changes = append(changes, *ch)
				}
			}
			fromHysteresis(st, hs)
			t := store.UTC(now)
			st.LastContactAt, st.UpdatedAt = &t, t
			// Health: the latest value of each field among the applied readings (out-of-order ones are stale).
			for _, it := range applied {
				if it.battery != nil {
					st.BatteryPct = it.battery
				}
				if it.rssi != nil {
					st.RSSI = it.rssi
				}
				if it.distance != nil {
					st.DistanceCm = it.distance
				}
				if it.firmware != "" {
					st.Firmware = it.firmware
				}
			}
			if err := store.UpsertSensorDeviceState(ctx, tx, st); err != nil {
				return err
			}
			for _, ch := range changes {
				if err := s.writeChange(ctx, tx, scope, devices[id], ch, now); err != nil {
					return err
				}
			}
			res.changes += len(changes)
		}
		if res.changes > 0 {
			store.AfterCommit(ctx, s.wake)
		}
		return nil
	}
	if testHookBeforeStoreTx != nil {
		testHookBeforeStoreTx()
	}
	if err := s.d.DB.RunInTx(ctx, inTx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || store.IsSerializationFailure(err) {
			return storeResult{}, ErrBusy
		}
		return storeResult{}, err
	}
	s.metrics.IngestTx.Add(1)
	s.metrics.IngestTxNano.Add(int64(s.d.Clock.Now().Sub(start)))
	s.metrics.Readings.Add(int64(res.accepted))
	res.duplicates += len(all) - len(res.rejected) - res.accepted
	s.metrics.Duplicates.Add(int64(res.duplicates))
	s.metrics.Changes.Add(int64(res.changes))
	return res, nil
}

// wake signals the reconcile job without blocking (m12-plan §4.4 step 6).
func (s *Service) wake() {
	if s.d.Wake == nil {
		return
	}
	select {
	case s.d.Wake <- struct{}{}:
	default:
	}
}

// writeChange appends a confirmed physical change of a device to sensor_state_changes and writes its
// sensor.state_changed event in the same transaction (ids, enums and instants only, INV-70).
func (s *Service) writeChange(ctx context.Context, tx bun.IDB, scope store.MarinaScope, d *store.SensorDevice, ch hysteresis.Change, now time.Time) error {
	row := &store.SensorStateChange{
		MarinaID: scope.ID(), DeviceID: d.ID, ResourceType: d.ResourceType, ResourceID: d.ResourceID, MapID: d.MapID,
		FromCm: d.FromCm, ToCm: d.ToCm, FromState: ch.From, ToState: ch.To, At: ch.At, Reason: ch.Reason, CreatedAt: now,
	}
	if err := store.InsertSensorStateChange(ctx, tx, row); err != nil {
		return err
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.SensorStateChanged, event.SensorStateChangedData{
		DeviceID: d.ID, ResourceType: d.ResourceType, ResourceID: d.ResourceID, ResourceIDs: []string{d.ResourceID}, MapID: d.MapID,
		FromCm: d.FromCm, ToCm: d.ToCm, From: ch.From, To: ch.To, At: row.At.UTC().Format(time.RFC3339Nano), Reason: ch.Reason,
	})
	return err
}

func timeOf(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.UTC()
}

func ptrOf(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := store.UTC(t)
	return &u
}

// toHysteresis converts a state row to the pure state.
func toHysteresis(st *store.SensorDeviceState) hysteresis.State {
	confirmed := st.Physical
	if confirmed == "" {
		confirmed = hysteresis.Unknown
	}
	return hysteresis.State{
		Confirmed: confirmed, ConfirmedSince: timeOf(st.PhysicalSince), Candidate: st.Candidate,
		CandidateSince: timeOf(st.CandidateSince), CandidateCount: st.CandidateCount, LastObservedAt: timeOf(st.LastObservedAt),
	}
}

// fromHysteresis writes the pure state back into the row.
func fromHysteresis(st *store.SensorDeviceState, h hysteresis.State) {
	st.Physical, st.PhysicalSince = h.Confirmed, ptrOf(h.ConfirmedSince)
	st.Candidate, st.CandidateSince, st.CandidateCount = h.Candidate, ptrOf(h.CandidateSince), h.CandidateCount
	if h.Candidate == "" {
		st.CandidateSince, st.CandidateCount = nil, 0
	}
	st.LastObservedAt = ptrOf(h.LastObservedAt)
}
