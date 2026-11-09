// Package sensorsim is the deterministic occupancy-sensor simulator of the M12 load acceptance (m12-plan §14.1):
// a seeded schedule of true berth states with flips and single dissenting "wave" readings, the instants at which
// each device reads, which reading completes the hysteresis of each true change (N = 2), the replays, and an HTTP
// client that signs requests with the protocol of docs/sensor-protocol.md. Test support only.
package sensorsim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
)

// Path is the ingestion endpoint.
const Path = "/api/ingest/v1/readings"

// States of the simulation (the simulator never sends unknown).
const (
	Occupied = "occupied"
	Free     = "free"
)

// Options tune the schedule. Percentages are per device and minute; ReplayPerMille is per request.
type Options struct {
	Minutes        int           // length of the schedule
	Period         time.Duration // reading period of a device (60 s)
	Step           time.Duration // offset between consecutive devices (60 ms)
	FlipPct        int           // true state flips (5)
	WavePct        int           // single dissenting readings (3)
	ReplayPerMille int           // replayed requests (5)
}

// Defaults are the options of m12-plan §14.1.
func Defaults(minutes int) Options {
	return Options{Minutes: minutes, Period: time.Minute, Step: 60 * time.Millisecond, FlipPct: 5, WavePct: 3, ReplayPerMille: 5}
}

// Sim is a schedule for n devices. It is immutable after New.
type Sim struct {
	opt   Options
	n     int
	truth [][]string // [device][minute] true state
	wave  [][]bool   // [device][minute] the reading dissents
	rng   *rand.Rand
}

// Expected is a confirmed change the simulator expects: device, the minute of the reading that completes it (the
// second agreeing reading), the minute of the first agreeing reading (the change instant) and the new state.
type Expected struct {
	Device     int
	Completing int
	First      int
	To         string
}

// New draws the schedule with seed. Flips are at least 3 minutes apart and never in the first 2 minutes (the first
// two readings confirm the initial state); waves never touch a flip (minutes f-1, f, f+1) or the first 2 minutes.
func New(seed uint64, n int, opt Options) *Sim {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // deterministic simulation, not secrets
	s := &Sim{opt: opt, n: n, truth: make([][]string, n), wave: make([][]bool, n), rng: rng}
	for d := range n {
		s.truth[d], s.wave[d] = make([]string, opt.Minutes+2), make([]bool, opt.Minutes+2)
		cur := Free
		if rng.IntN(2) == 0 {
			cur = Occupied
		}
		last := -10
		flips := map[int]bool{}
		for m := range opt.Minutes + 2 {
			if m >= 2 && m-last >= 3 && rng.IntN(100) < opt.FlipPct {
				cur, last = other(cur), m
				flips[m] = true
			}
			s.truth[d][m] = cur
		}
		for m := 2; m < opt.Minutes; m++ {
			if !flips[m-1] && !flips[m] && !flips[m+1] && !s.wave[d][m-1] && rng.IntN(100) < opt.WavePct {
				s.wave[d][m] = true
			}
		}
	}
	return s
}

func other(st string) string {
	if st == Occupied {
		return Free
	}
	return Occupied
}

// Devices is the number of devices.
func (s *Sim) Devices() int { return s.n }

// Offset is when device d reads within each period.
func (s *Sim) Offset(d int) time.Duration {
	return time.Duration(d) * s.opt.Step % s.opt.Period
}

// At is the instant of reading m of device d for a schedule starting at start (truncated to ms by Porto).
func (s *Sim) At(start time.Time, d, m int) time.Time {
	return start.Add(time.Duration(m)*s.opt.Period + s.Offset(d))
}

// State is what device d reports at minute m.
func (s *Sim) State(d, m int) string {
	if s.wave[d][m] {
		return other(s.truth[d][m])
	}
	return s.truth[d][m]
}

// Truth is the true state of device d at minute m.
func (s *Sim) Truth(d, m int) string { return s.truth[d][m] }

// Expected returns the changes a hysteresis with N = 2 confirms over minutes [0, upTo): the initial state at
// reading 1 and every flip at the reading after it.
func (s *Sim) Expected(upTo int) []Expected {
	upTo = min(upTo, s.opt.Minutes+2)
	var out []Expected
	for d := range s.n {
		if upTo > 1 {
			out = append(out, Expected{Device: d, Completing: 1, First: 0, To: s.truth[d][0]})
		}
		for m := 2; m+1 < upTo; m++ {
			if s.truth[d][m] != s.truth[d][m-1] {
				out = append(out, Expected{Device: d, Completing: m + 1, First: m, To: s.truth[d][m]})
			}
		}
	}
	return out
}

// Replay reports whether the request of device d at minute m is sent twice (deterministic per (d, m)).
func (s *Sim) Replay(d, m int) bool {
	h := uint64(d)*1_000_003 + uint64(m)*7919 + 17
	h ^= h >> 13
	h *= 0x2545F4914F6CDD1D
	return int(h%1000) < s.opt.ReplayPerMille
}

// Reading is one item of a batch.
type Reading struct {
	DeviceID   string `json:"device_id"`
	ObservedAt string `json:"observed_at"`
	State      string `json:"state"`
	BatteryPct *int   `json:"battery_pct,omitempty"`
}

// Item returns the reading of device d (external id ext) at minute m.
func (s *Sim) Item(start time.Time, d, m int, ext string) Reading {
	return Reading{DeviceID: ext, ObservedAt: s.At(start, d, m).UTC().Format(time.RFC3339Nano), State: s.State(d, m)}
}

// Body marshals a batch.
func Body(rs ...Reading) []byte {
	b, _ := json.Marshal(map[string]any{"readings": rs})
	return b
}

// Result is the decoded 200 response.
type Result struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Rejected   []struct {
		Index int    `json:"index"`
		Code  string `json:"code"`
	} `json:"rejected"`
}

// Client posts signed batches.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// Sign returns the Porto-Key and Porto-Signature headers of body signed with key at t.
func Sign(key string, t time.Time, body []byte) (keyID, signature string) {
	keyID, _ = ingestauth.KeyIDOf(key)
	ts := t.Unix()
	return keyID, "t=" + strconv.FormatInt(ts, 10) + ",v1=" + ingestauth.Sign(key, ts, http.MethodPost, Path, body)
}

// Post signs body with key at now and posts it; it returns the status and, on 200, the decoded result.
func (c *Client) Post(ctx context.Context, key string, body []byte, now time.Time) (int, *Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+Path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	id, sig := Sign(key, now, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Porto-Key", id)
	req.Header.Set("Porto-Signature", sig)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, err
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil, fmt.Errorf("sensorsim: status %d: %s", res.StatusCode, raw)
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, &out, nil
}
