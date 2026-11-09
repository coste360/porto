//go:build integration

package ingest_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: the harbour is inserted directly
	"github.com/coste360/porto/pkg/ext"
)

const path = "/api/ingest/v1/readings"

type srv struct {
	app   *app.App
	url   string
	clock *clock.Fake
}

func newSrv(t *testing.T) *srv {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2027, 6, 12, 8, 30, 5, 0, time.UTC))
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler), app.Options{Argon2: auth.TestArgon2Params, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Router)
	t.Cleanup(ts.Close)
	return &srv{app: a, url: ts.URL, clock: clk}
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) problem() string {
	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r.body, &p)
	return p.Type
}

func (s *srv) post(t *testing.T, ct string, body []byte, headers ...string) resp {
	t.Helper()
	req, _ := http.NewRequest("POST", s.url+path, bytes.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, res.Header, b}
}

func (s *srv) signed(t *testing.T, key string, body []byte) resp {
	t.Helper()
	id, _ := ingestauth.KeyIDOf(key)
	ts := s.clock.Now().Unix()
	return s.post(t, "application/json", body, "Porto-Key", id,
		"Porto-Signature", "t="+strconv.FormatInt(ts, 10)+",v1="+ingestauth.Sign(key, ts, "POST", path, body))
}

func TestIngestHandler_Scenario(t *testing.T) {
	s := newSrv(t)
	ctx := context.Background()
	o, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "owner@porto.test", Name: "Owner", Password: "handler-password-1", Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-i"})
	if err != nil {
		t.Fatal(err)
	}
	h := contracttest.InsertHarbour(t, s.app.DB, o.MarinaID, store.UTC(s.clock.Now()), 2)
	login, err := s.app.Auth.Login(ctx, auth.LoginInput{Email: "owner@porto.test", Password: "handler-password-1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.app.Sensors.CreateDevice(ctx, login.Principal, o.MarinaID, sensor.DeviceInput{
		ExternalID: "A81758FFFE0312AB", Code: "S1", Kind: "ultrasonic", ResourceType: "berth", ResourceID: h.Berths[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.app.Sensors.CreateGateway(ctx, login.Principal, o.MarinaID, sensor.GatewayInput{Code: "GW1", Name: "Gw", Kind: "lorawan"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:30:00Z","state":"occupied","distance_cm":142,"battery_pct":87,"rssi":-97},` +
		`{"device_id":"OTHER","observed_at":"2027-06-12T08:30:00Z","state":"occupied"}]}`)

	r := s.signed(t, d.Key, body)
	if r.status != 200 || r.header.Get("Cache-Control") != "no-store" || r.header.Get("Set-Cookie") != "" {
		t.Fatalf("200: %d %s", r.status, r.body)
	}
	var res api.SensorIngestResult
	if err := json.Unmarshal(r.body, &res); err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 1 || res.Duplicates != 0 || len(res.Rejected) != 1 || res.Rejected[0].Index != 1 || res.Rejected[0].Code != "device_mismatch" {
		t.Fatalf("result %s", r.body)
	}
	if r = s.signed(t, d.Key, body); !strings.Contains(string(r.body), `"duplicates":1`) {
		t.Fatalf("replay %s", r.body)
	}

	// 401: unknown key, bad signature, missing headers, bearer over plain http; one generic problem.
	other, _ := ingestauth.FormatKey(ingestauth.DevicePrefix, [10]byte{1}, [32]byte{2})
	for name, rr := range map[string]resp{
		"unknown key":   s.signed(t, other, body),
		"no headers":    s.post(t, "application/json", body),
		"bad signature": s.post(t, "application/json", body, "Porto-Key", d.Device.KeyID, "Porto-Signature", "t=1812789005,v1="+strings.Repeat("0", 64)),
		"bearer http":   s.post(t, "application/json", body, "Authorization", "Bearer "+g.Key),
		"device bearer": s.post(t, "application/json", body, "Authorization", "Bearer "+d.Key),
	} {
		if rr.status != 401 || rr.problem() != "ingest_unauthorized" || rr.header.Get("Porto-Server-Time") != "" {
			t.Errorf("%s: %d %s", name, rr.status, rr.body)
		}
	}
	// 401 timestamp: a valid signature 301 s old carries the server time.
	ts := s.clock.Now().Unix() - 301
	rr := s.post(t, "application/json", body, "Porto-Key", d.Device.KeyID, "Porto-Signature", "t="+strconv.FormatInt(ts, 10)+",v1="+ingestauth.Sign(d.Key, ts, "POST", path, body))
	if rr.status != 401 || rr.problem() != "ingest_timestamp_out_of_range" || rr.header.Get("Porto-Server-Time") != strconv.FormatInt(s.clock.Now().Unix(), 10) {
		t.Fatalf("timestamp: %d %s %v", rr.status, rr.body, rr.header)
	}
	// 400 invalid_payload, 413, 415.
	for _, b := range []string{`{"readings":[]}`, `{"nope":1}`, `not json`} {
		if rr := s.signed(t, d.Key, []byte(b)); rr.status != 400 || rr.problem() != "invalid_payload" {
			t.Errorf("payload %q: %d %s", b, rr.status, rr.body)
		}
	}
	big := []byte(`{"readings":[` + strings.Repeat(" ", 256<<10) + `]}`)
	if rr := s.signed(t, d.Key, big); rr.status != 413 {
		t.Errorf("413: %d %s", rr.status, rr.body)
	}
	if rr := s.post(t, "text/plain", body, "Porto-Key", d.Device.KeyID, "Porto-Signature", "t=1,v1="+strings.Repeat("0", 64)); rr.status != 415 {
		t.Errorf("415: %d %s", rr.status, rr.body)
	}
	// 429: 30 requests per minute per device key (counted before the signature check).
	limited := false
	for i := range 40 {
		b := []byte(`{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:2` + strconv.Itoa(i%10) + `:0` + strconv.Itoa(i/10) + `Z","state":"free"}]}`)
		rr := s.signed(t, d.Key, b)
		if rr.status == 429 {
			if rr.problem() != "rate_limited" || rr.header.Get("Retry-After") == "" {
				t.Fatalf("429: %s %v", rr.body, rr.header)
			}
			limited = true
			break
		}
		if rr.status != 200 {
			t.Fatalf("request %d: %d %s", i, rr.status, rr.body)
		}
	}
	if !limited {
		t.Fatal("per-key limit not applied")
	}
	// Admin routes do not accept sensor keys.
	req, _ := http.NewRequest("GET", s.url+"/api/admin/v1/marinas/"+o.MarinaID+"/sensors/devices", nil)
	req.Header.Set("Authorization", "Bearer "+g.Key)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 401 {
		t.Fatalf("admin with a gateway key: %d", res2.StatusCode)
	}
}
