//go:build integration

package acceptance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/perfbudget"
	"github.com/coste360/porto/internal/testutil/sensorsim"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Load acceptance of M12 (spec 32; m12-plan §14): 1,000 sensors, one reading per minute each, on SQLite (a WAL file,
// real clock, real jobs, real HTTP server and SSE client). The operational view must never lag more than 2 s.
const (
	loadDevices  = 1000
	loadGateways = 10
	loadOwnKeys  = 500
	loadGWPeriod = 3 * time.Second
)

// latencies collects durations.
type latencies struct {
	mu sync.Mutex
	d  []time.Duration
}

func (l *latencies) add(d time.Duration) {
	l.mu.Lock()
	l.d = append(l.d, d)
	l.mu.Unlock()
}

func (l *latencies) stats() (n int, p50, p95, p99, maxD time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := slices.Clone(l.d)
	slices.Sort(s)
	if len(s) == 0 {
		return 0, 0, 0, 0, 0
	}
	at := func(q int) time.Duration { return s[min(len(s)-1, len(s)*q/100)] }
	return len(s), at(50), at(95), at(99), s[len(s)-1]
}

func envDuration(t *testing.T, name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return d
	}
	return def
}

func envInt(t *testing.T, name string, def int) int {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return n
	}
	return def
}

type sseEvent struct {
	at          time.Time
	typ         string
	resourceIDs []string
}

// TestAcceptance_M12_SensorLoad is the M12 acceptance criterion (L1–L6 of m12-plan §14.2).
// PORTO_M12_LOAD_DURATION (default 3m) and PORTO_M12_LOAD_PREFILL_DAYS (default 1) tune the run.
// A real-time load test (wall clock, latency percentiles): it runs only with PORTO_PERF_BUDGET=1
// (perfbudget.Require: make test-perf, Performance workflow), never in the merge gates.
func TestAcceptance_M12_SensorLoad(t *testing.T) {
	perfbudget.Require(t)
	duration := envDuration(t, "PORTO_M12_LOAD_DURATION", 3*time.Minute)
	prefillDays := envInt(t, "PORTO_M12_LOAD_PREFILL_DAYS", 1)
	// One warm-up minute on top of the duration: the first two readings of each device confirm its initial state,
	// so true flips (from the third reading on) are measured over the whole duration.
	minutes := int(duration/time.Minute) + 1
	if minutes < 3 {
		t.Fatal("PORTO_M12_LOAD_DURATION must be at least 2m")
	}
	db := testdb.SQLite(t)
	h := newHarness(t, db)
	clk := clock.System{}
	ctx := context.Background()
	own, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Org", MarinaName: "Marina carico", MarinaSlug: "marina-carico",
	})
	if err != nil {
		t.Fatal(err)
	}
	o := h.cookieClient()
	o.login(t, "owner@porto.test")
	M := A + "/marinas/" + own.MarinaID
	mem := o.expect(t, 201, "POST", M+"/members", map[string]any{"email": "dock@porto.test", "name": "dock", "role": "dock"})
	if err := h.app.Auth.SetPassword(ctx, str(mem, "user_id"), password); err != nil {
		t.Fatal(err)
	}
	dock := h.cookieClient()
	dock.login(t, "dock@porto.test")

	// A map with 1,000 berths in a 40 × 25 grid.
	now := store.UTC(clk.Now())
	mp := &store.Map{
		ID: store.NewID(), MarinaID: own.MarinaID, Kind: "water", Name: store.JSON(`{"it":"Porto"}`), Icon: "map", WidthCm: 100000,
		HeightCm: 100000, GridCm: 50, WaterPolygons: store.JSON(`[]`), LandPolygons: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	berths := make([]store.Berth, loadDevices)
	berthIndex := map[string]int{}
	for i := range berths {
		berths[i] = store.Berth{
			ID: store.NewID(), MarinaID: own.MarinaID, MapID: mp.ID, Code: fmt.Sprintf("B%04d", i), Side: "none",
			AnchorXCm: int64(1000 + 500*(i%40)), AnchorYCm: int64(1000 + 1500*(i/40)), WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200,
			MaxBeamCm: 400, MooringType: "mooring_line", PriceClass: "L12", Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		berthIndex[berths[i].ID] = i
	}
	if _, err := db.NewInsert().Model(mp).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewInsert().Model(&berths).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	// Gateways and devices through the API.
	gwKeys := make([]string, loadGateways)
	gwIDs := make([]string, loadGateways)
	for g := range loadGateways {
		r := o.expect(t, 201, "POST", M+"/sensors/gateways", map[string]any{"code": fmt.Sprintf("GW%02d", g), "name": "Gateway", "kind": "lorawan"})
		gwKeys[g], gwIDs[g] = str(r, "key"), str(r["gateway"].(map[string]any), "id")
	}
	ext := make([]string, loadDevices)
	keys := make([]string, loadDevices)
	devIDs := make([]string, loadDevices)
	for d := range loadDevices {
		ext[d] = fmt.Sprintf("LOAD%06d", d)
		in := map[string]any{"external_id": ext[d], "code": fmt.Sprintf("S%04d", d), "kind": "ultrasonic", "resource_type": "berth", "resource_id": berths[d].ID}
		if d >= loadOwnKeys {
			in["gateway_id"] = gwIDs[(d-loadOwnKeys)%loadGateways]
		}
		r := o.expect(t, 201, "POST", M+"/sensors/devices", in)
		keys[d], devIDs[d] = str(r, "key"), str(r["device"].(map[string]any), "id")
	}
	set := o.expect(t, 200, "GET", M+"/sensors/settings", nil)
	set["presence_effect_enabled"] = true
	delete(set, "version")
	delete(set, "raw_retention_days")
	o.expect(t, 200, "PUT", M+"/sensors/settings", set, "If-Match", `"0"`)

	// Prefill: one synthetic reading per device and minute over prefillDays, all before the run.
	prefillEnd := clk.Now()
	prefill := 0
	if prefillDays > 0 {
		t0 := clk.Now()
		perDevice := prefillDays * 24 * 60
		batch := make([]store.SensorReading, 0, 50000)
		flush := func() {
			if len(batch) == 0 {
				return
			}
			rows := batch
			if err := h.app.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error { return store.BulkInsertSensorReadings(ctx, tx, rows) }); err != nil {
				t.Fatal(err)
			}
			prefill += len(rows)
			batch = batch[:0]
		}
		base := prefillEnd.Add(-time.Duration(perDevice+1) * time.Minute)
		for d := range loadDevices {
			for m := range perDevice {
				st := store.SensorFree
				if (m/37+d)%3 == 0 {
					st = store.SensorOccupied
				}
				at := base.Add(time.Duration(m)*time.Minute + time.Duration(d)*60*time.Millisecond).UTC().Truncate(time.Millisecond)
				batch = append(batch, store.SensorReading{DeviceID: devIDs[d], ObservedAt: at, MarinaID: own.MarinaID, ReceivedAt: at, State: st, Origin: store.SensorOriginDevice})
				if len(batch) == cap(batch) {
					flush()
				}
			}
		}
		flush()
		t.Logf("prefill: %d readings in %v", prefill, clk.Now().Sub(t0).Round(time.Second))
	}

	start := clk.Now().Truncate(time.Second).Add(3 * time.Second)

	// Real jobs (outbox dispatch, reconcile, silence, ...).
	jobsCtx, stopJobs := context.WithCancel(ctx)
	h.app.Jobs.Start(jobsCtx)
	defer func() { stopJobs(); h.app.Jobs.Wait() }()

	// The freshness check reads the operational view: the Sensori map context (getSensorMap).
	if r := dock.do(t, "GET", M+"/sensors/map?map_id="+mp.ID, nil); r.status != http.StatusOK {
		t.Fatalf("getSensorMap: %d", r.status)
	}
	readPhysical := func(c *client, d int) (string, error) {
		r := c.do(t, "GET", M+"/sensors/map?map_id="+mp.ID, nil)
		if r.status != 200 {
			return "", fmt.Errorf("map %d", r.status)
		}
		var m struct {
			Entries []struct {
				ResourceID string `json:"resource_id"`
				Physical   string `json:"physical"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(r.body, &m); err != nil {
			return "", err
		}
		for _, e := range m.Entries {
			if e.ResourceID == berths[d].ID {
				return e.Physical, nil
			}
		}
		return "", fmt.Errorf("berth missing from the map")
	}

	sim := sensorsim.New(12, loadDevices, sensorsim.Defaults(minutes))
	expStates := map[int][]string{}
	for _, e := range sim.Expected(minutes + 2) {
		expStates[e.Device] = append(expStates[e.Device], e.To)
	}
	expectedState := func(d, k int) string {
		if k < len(expStates[d]) {
			return expStates[d][k]
		}
		return ""
	}
	var (
		mu         sync.Mutex
		acceptedAt = map[[2]int]time.Time{}
		accepted   int
		duplicates int
		fivexx     int
		otherErr   []string
		reqLat     latencies
		views      = map[int][]time.Time{} // per device: when a view showed the state after each SSE message
		sseByDev   = map[int][]sseEvent{}
		pollLat    latencies
	)
	client := &sensorsim.Client{BaseURL: h.srv.URL, HTTP: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 128}}}
	send := func(key string, items [][2]int, rs []sensorsim.Reading) {
		body := sensorsim.Body(rs...)
		t0 := clk.Now()
		status, res, err := client.Post(ctx, key, body, t0)
		t1 := clk.Now()
		reqLat.add(t1.Sub(t0))
		mu.Lock()
		defer mu.Unlock()
		switch {
		case status >= 500:
			fivexx++
		case err != nil || res == nil:
			otherErr = append(otherErr, fmt.Sprintf("%d %v", status, err))
		default:
			accepted += res.Accepted
			duplicates += res.Duplicates
			for _, it := range items {
				if _, ok := acceptedAt[it]; !ok {
					acceptedAt[it] = t1
				}
			}
		}
	}

	// SSE client: one dock session.
	sseCtx, stopSSE := context.WithCancel(ctx)
	defer stopSSE()
	req, _ := http.NewRequestWithContext(sseCtx, "GET", h.srv.URL+A+"/stream?marina_id="+own.MarinaID, nil)
	sseRes, err := (&http.Client{Jar: dock.http.Jar}).Do(req)
	if err != nil || sseRes.StatusCode != 200 {
		t.Fatalf("stream: %v", err)
	}
	var viewWG sync.WaitGroup
	go func() {
		defer sseRes.Body.Close()
		sc := bufio.NewScanner(sseRes.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			at := clk.Now()
			var msg struct {
				Type        string   `json:"type"`
				ResourceIDs []string `json:"resource_ids"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &msg) != nil || msg.Type != "sensor.state_changed" || len(msg.ResourceIDs) != 1 {
				continue
			}
			d, ok := berthIndex[msg.ResourceIDs[0]]
			if !ok {
				continue
			}
			mu.Lock()
			sseByDev[d] = append(sseByDev[d], sseEvent{at: at, typ: msg.Type, resourceIDs: msg.ResourceIDs})
			k := len(sseByDev[d]) - 1
			mu.Unlock()
			viewWG.Add(1)
			go func(d, k int) {
				defer viewWG.Done()
				// The UI invalidates and refetches on the message: read the view until it shows the new state.
				for range 20 {
					exp := expectedState(d, k)
					ph, err := readPhysical(dock, d)
					if err == nil && ph == exp {
						mu.Lock()
						for len(views[d]) <= k {
							views[d] = append(views[d], time.Time{})
						}
						views[d][k] = clk.Now()
						mu.Unlock()
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
			}(d, k)
		}
	}()

	// L4 pollers: 5 sessions reading the Sensori map and the day view every 2 s.
	pollCtx, stopPoll := context.WithCancel(ctx)
	var pollWG sync.WaitGroup
	for range 5 {
		pollWG.Go(func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-pollCtx.Done():
					return
				case <-tick.C:
				}
				t0 := clk.Now()
				dock.do(t, "GET", M+"/sensors/map?map_id="+mp.ID, nil)
				dock.do(t, "GET", M+"/maps/"+mp.ID+"/day", nil)
				pollLat.add(clk.Now().Sub(t0))
			}
		})
	}

	// Device-key senders: device d reads at start + m minutes + d × 60 ms.
	type job struct {
		at   time.Time
		d, m int
	}
	work := make(chan job, 1024)
	var sendWG sync.WaitGroup
	for range 48 {
		sendWG.Go(func() {
			for j := range work {
				it := sim.Item(start, j.d, j.m, ext[j.d])
				send(keys[j.d], [][2]int{{j.d, j.m}}, []sensorsim.Reading{it})
				if sim.Replay(j.d, j.m) {
					send(keys[j.d], [][2]int{{j.d, j.m}}, []sensorsim.Reading{it})
				}
			}
		})
	}
	var schedWG sync.WaitGroup
	schedWG.Go(func() {
		var jobs []job
		for m := range minutes {
			for d := range loadOwnKeys {
				jobs = append(jobs, job{at: sim.At(start, d, m), d: d, m: m})
			}
		}
		slices.SortFunc(jobs, func(a, b job) int { return a.at.Compare(b.at) })
		for _, j := range jobs {
			if w := j.at.Sub(clk.Now()); w > 0 {
				time.Sleep(w)
			}
			work <- j
		}
		close(work)
	})
	// Gateway senders: every 3 s each gateway posts the readings of its devices that came due.
	for g := range loadGateways {
		schedWG.Go(func() {
			var mine []int
			for d := loadOwnKeys; d < loadDevices; d++ {
				if (d-loadOwnKeys)%loadGateways == g {
					mine = append(mine, d)
				}
			}
			next := make(map[int]int, len(mine))
			end := start.Add(time.Duration(minutes) * time.Minute)
			time.Sleep(start.Add(time.Duration(g) * 300 * time.Millisecond).Sub(clk.Now()))
			for {
				now := clk.Now()
				var items [][2]int
				var rs []sensorsim.Reading
				for _, d := range mine {
					for next[d] < minutes && !sim.At(start, d, next[d]).After(now) {
						items = append(items, [2]int{d, next[d]})
						rs = append(rs, sim.Item(start, d, next[d], ext[d]))
						next[d]++
					}
				}
				if len(rs) > 0 {
					send(gwKeys[g], items, rs)
				}
				if now.After(end) {
					return
				}
				time.Sleep(loadGWPeriod)
			}
		})
	}
	wallStart := clk.Now()
	schedWG.Wait()
	sendWG.Wait()
	time.Sleep(3 * time.Second) // drain: dispatch and the last view checks
	stopPoll()
	pollWG.Wait()
	viewWG.Wait()
	wall := clk.Now().Sub(wallStart)
	stopSSE()

	// L5: correctness after drain.
	mu.Lock()
	defer mu.Unlock()
	if fivexx > 0 || len(otherErr) > 0 {
		t.Errorf("L1: %d 5xx, other errors %v", fivexx, otherErr[:min(5, len(otherErr))])
	}
	scope, _ := store.ResolveMarinaScope(ctx, db.DB, own.MarinaID)
	rows, err := store.CountSensorReadings(ctx, db.DB, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows != accepted+prefill {
		t.Errorf("L5: %d readings, accepted %d + prefill %d", rows, accepted, prefill)
	}
	if want := minutes * loadDevices; accepted != want {
		t.Errorf("L5: accepted %d, sent %d distinct readings", accepted, want)
	}
	expected := sim.Expected(minutes)
	var changes int
	if err := db.NewSelect().Table("sensor_state_changes").ColumnExpr("COUNT(*)").Where("marina_id = ?", own.MarinaID).Scan(ctx, &changes); err != nil {
		t.Fatal(err)
	}
	if changes != len(expected) {
		t.Errorf("L5: %d state changes, %d expected flips", changes, len(expected))
	}
	if vs, err := store.CheckSensorInvariants(ctx, db.DB, own.MarinaID); err != nil || len(vs) > 0 {
		t.Errorf("L5: Q40/Q41 %v %v", vs[:min(3, len(vs))], err)
	}

	// L2 and L3: per device, the k-th expected change against the k-th SSE message and the k-th fresh view.
	byDev := map[int][]sensorsim.Expected{}
	for _, e := range expected {
		byDev[e.Device] = append(byDev[e.Device], e)
	}
	var l2, l3 latencies
	missing, extra := 0, 0
	for d := range loadDevices {
		exp, got := byDev[d], sseByDev[d]
		if len(got) != len(exp) {
			if len(got) < len(exp) {
				missing += len(exp) - len(got)
			} else {
				extra += len(got) - len(exp)
			}
		}
		for k := range min(len(exp), len(got)) {
			acc, ok := acceptedAt[[2]int{d, exp[k].Completing}]
			if !ok {
				continue
			}
			l2.add(max(0, got[k].at.Sub(acc)))
			if k < len(views[d]) && !views[d][k].IsZero() {
				l3.add(max(0, views[d][k].Sub(acc)))
			} else {
				l3.add(time.Hour) // never fresh: fails the bound
			}
		}
	}
	if missing > 0 || extra > 0 {
		t.Errorf("L2: %d expected changes not seen, %d seen in excess", missing, extra)
	}

	// L6: write budget.
	sm := h.app.Sensors.Metrics()
	ingestBusy := time.Duration(sm.IngestTxNano.Load())
	var recBusy, recMax time.Duration
	var recRuns int64
	if h.app.SensorWatch != nil {
		wm := h.app.SensorWatch.Metrics()
		recBusy, recMax, recRuns = time.Duration(wm.RunNano.Load()), time.Duration(wm.MaxNano.Load()), wm.Runs.Load()
	}

	report := func(name string, l *latencies) (time.Duration, time.Duration, time.Duration) {
		n, p50, p95, p99, mx := l.stats()
		t.Logf("%-28s n=%-7d p50=%-10v p95=%-10v p99=%-10v max=%v", name, n, p50.Round(time.Millisecond), p95.Round(time.Millisecond), p99.Round(time.Millisecond), mx.Round(time.Millisecond))
		return p95, p99, mx
	}
	t.Logf("M12 load: %d devices, %d minutes, wall %v, prefill %d rows, readings %d, accepted %d, duplicates %d, changes %d (expected %d)",
		loadDevices, minutes, wall.Round(time.Second), prefill, rows, accepted, duplicates, changes, len(expected))
	_, p99, mx := report("L1 ingest request", &reqLat)
	if mx > 2*time.Second || p99 > 250*time.Millisecond {
		t.Errorf("L1: p99 %v max %v (want ≤ 250 ms, ≤ 2 s)", p99, mx)
	}
	p95, _, mx := report("L2 accepted → SSE", &l2)
	if mx > 2*time.Second || p95 > 500*time.Millisecond {
		t.Errorf("L2: p95 %v max %v (want ≤ 500 ms, ≤ 2 s)", p95, mx)
	}
	_, _, mx = report("L3 accepted → fresh view", &l3)
	if mx > 2*time.Second {
		t.Errorf("L3: max %v (want ≤ 2 s)", mx)
	}
	p95, _, mx = report("L4 view queries (5 pollers)", &pollLat)
	if mx > 2*time.Second || p95 > 500*time.Millisecond {
		t.Errorf("L4: p95 %v max %v (want ≤ 500 ms, ≤ 2 s)", p95, mx)
	}
	share := (ingestBusy + recBusy) * 1000 / wall
	t.Logf("L6 write budget: ingest tx %d, %v; reconcile runs %d, %v (max %v); %d‰ of wall time",
		sm.IngestTx.Load(), ingestBusy.Round(time.Millisecond), recRuns, recBusy.Round(time.Millisecond), recMax.Round(time.Millisecond), int64(share))
	if ingestBusy+recBusy > wall*30/100 || recMax > 250*time.Millisecond {
		t.Errorf("L6: busy %v of %v, reconcile max %v", ingestBusy+recBusy, wall, recMax)
	}
}
