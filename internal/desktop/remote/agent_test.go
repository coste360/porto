package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/pkg/ext"
)

func TestPair_PinnedTLSAndErrors(t *testing.T) {
	ca := newCA(t, "Porto local CA")
	leaf := ca.leaf(t)
	var got api.PairRequest
	s := tlsServer(t, leaf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/device/v1/pair" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.Code != "GOOD-CODE" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.PairResult{
			Token: testToken, Device: api.Device{Id: "dev-1"}, Marina: api.MarinaRef{Id: "m-1", Name: "Marina"},
		})
	}))
	fp := FingerprintB64(leaf.Certificate[0])
	ctx := context.Background()
	p, err := Pair(ctx, PairInput{URL: s.URL, Code: "GOOD-CODE", Fingerprint: fp, Platform: "darwin", AppVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Token != testToken || p.DeviceID != "dev-1" || p.MarinaID != "m-1" || p.Pins.CAFP != Fingerprint(ca.cert.Raw) ||
		p.Pins.LeafFP != Fingerprint(leaf.Certificate[0]) {
		t.Fatalf("paired %+v", p)
	}
	if got.Kind != "desktop" || got.Platform != "macos" {
		t.Fatalf("request %+v", got)
	}
	var se *desktopport.SettingsError
	if _, err := Pair(ctx, PairInput{URL: s.URL, Code: "BAD", Fingerprint: fp}); !errors.As(err, &se) || se.Fields[0] != "code" {
		t.Fatalf("bad code: %v", err)
	}
	var pm *desktopport.PinMismatchError
	if _, err := Pair(ctx, PairInput{URL: s.URL, Code: "GOOD-CODE", Fingerprint: strings.Repeat("1", 64)}); !errors.As(err, &pm) {
		t.Fatalf("wrong fingerprint: %v", err)
	}
	if _, err := Pair(ctx, PairInput{URL: strings.Replace(s.URL, "https", "http", 1), Code: "x"}); !errors.As(err, &se) {
		t.Fatalf("http without dev: %v", err)
	}
	if _, err := Pair(ctx, PairInput{URL: "https://127.0.0.1:1", Code: "GOOD-CODE"}); !errors.Is(err, desktopport.ErrRemoteUnreachable) {
		t.Fatalf("unreachable: %v", err)
	}
}

type fakeFiscal struct {
	mu     sync.Mutex
	issued []string
	mode   string
}

type deferred struct{}

func (deferred) Error() string  { return "offline" }
func (deferred) Deferred() bool { return true }

func (f *fakeFiscal) Code() string { return "fake" }
func (f *fakeFiscal) Issue(_ context.Context, d ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	switch f.mode {
	case "offline":
		return nil, deferred{}
	case "fail":
		return nil, errors.New("paper out")
	}
	f.mu.Lock()
	f.issued = append(f.issued, d.DocumentID)
	f.mu.Unlock()
	return &ext.FiscalDocumentResult{ExternalRef: "RT-" + d.Number}, nil
}
func (f *fakeFiscal) Void(context.Context, string) error { return nil }
func (f *fakeFiscal) Status(context.Context) (ext.DeviceStatus, error) {
	return ext.DeviceStatus{Online: true}, nil
}

// fakeBridge is a fake instance of the bridge endpoints: a list of pending documents, a claim that hands out the
// first pending one (Max must be 1) with a fresh claim id, and an outcome endpoint that applies only the current
// claim (409 otherwise, like SetBridgeOutcome). dropNext makes the next outcome lose its answer.
type fakeBridge struct {
	mu        sync.Mutex
	docs      []string
	status    map[string]string
	claim     map[string]string
	outcomes  map[string]api.FiscalOutcome
	keys      map[string]string
	claims    int
	seq       int
	dropNext  int // answers to lose (the transport retries a request with Idempotency-Key once)
	badMax    bool
	expireAll bool // every claim is stale by the time its outcome arrives
}

func newFakeBridge(docs ...string) *fakeBridge {
	b := &fakeBridge{docs: docs, status: map[string]string{}, claim: map[string]string{}, outcomes: map[string]api.FiscalOutcome{}, keys: map[string]string{}}
	for _, d := range docs {
		b.status[d] = "pending"
	}
	return b
}

func (b *fakeBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case r.URL.Path == "/api/device/v1/fiscal/claim":
		b.claims++
		var in api.FiscalClaimRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Max != 1 {
			b.badMax = true
		}
		out := api.FiscalClaim{Items: []api.FiscalClaimDocument{}}
		for _, d := range b.docs {
			if b.status[d] == "pending" {
				b.seq++
				b.claim[d] = "c" + strconv.Itoa(b.seq)
				out.Items = append(out.Items, api.FiscalClaimDocument{DocumentId: d, ClaimId: b.claim[d], Number: "N-" + d, TotalCents: 1000, ClaimUntil: time.Now()})
				break
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case strings.HasPrefix(r.URL.Path, "/api/device/v1/fiscal/documents/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/device/v1/fiscal/documents/"), "/outcome")
		var o api.FiscalOutcome
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &o)
		if b.expireAll || o.ClaimId != b.claim[id] || b.status[id] != "pending" {
			b.expireAll = false
			w.WriteHeader(http.StatusConflict)
			return
		}
		if b.dropNext > 0 {
			// The connection drops before the instance answers (nothing applied).
			b.dropNext--
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if o.Status != "deferred" {
			b.status[id] = string(o.Status)
		}
		b.outcomes[id], b.keys[id] = o, r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func agentFor(t *testing.T, b *fakeBridge, drv FiscalDriver, journal string) (*FiscalAgent, *bool) {
	t.Helper()
	s := httptest.NewServer(b)
	t.Cleanup(s.Close)
	origin, _ := url.Parse(s.URL)
	up := NewUpstream(origin, Pins{}, func() string { return testToken })
	reachable := true
	a := &FiscalAgent{Upstream: up, Driver: func() FiscalDriver { return drv }, Reachable: func() bool { return reachable }, Journal: journal}
	return a, &reachable
}

func TestFiscalAgent_ClaimsIssuesReports(t *testing.T) {
	b := newFakeBridge("d1", "d2")
	drv := &fakeFiscal{}
	a, reachable := agentFor(t, b, drv, "")
	n, err := a.RunOnce(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("run: %d %v", n, err)
	}
	if b.badMax {
		t.Fatal("claimed more than one document at a time")
	}
	if b.outcomes["d1"].Status != "issued" || *b.outcomes["d1"].ExternalRef != "RT-N-d1" || b.keys["d1"] != "d1:"+b.claim["d1"] {
		t.Fatalf("outcome %+v key %q", b.outcomes["d1"], b.keys["d1"])
	}
	// Deferred: the device is unreachable, the document stays pending and the run stops.
	b2 := newFakeBridge("d3", "d4")
	drv2 := &fakeFiscal{mode: "offline"}
	a2, _ := agentFor(t, b2, drv2, "")
	if _, err := a2.RunOnce(context.Background()); err != nil || b2.outcomes["d3"].Status != "deferred" || b2.claims != 1 {
		t.Fatalf("deferred: %v %+v claims %d", err, b2.outcomes["d3"], b2.claims)
	}
	drv2.mode = "fail"
	if _, err := a2.RunOnce(context.Background()); err != nil || b2.status["d3"] != "failed" || *b2.outcomes["d3"].ErrorCode != "driver_error" {
		t.Fatalf("failed: %v %+v", err, b2.outcomes["d3"])
	}
	// Unreachable or no local device: no claim at all.
	before := b.claims
	*reachable = false
	_, _ = a.RunOnce(context.Background())
	*reachable = true
	a.Driver = func() FiscalDriver { return nil }
	_, _ = a.RunOnce(context.Background())
	if b.claims != before {
		t.Fatal("claimed while unreachable or without a device")
	}
}

// TestFiscalAgent_LostOutcomeNoReissue: INV-86 on the device. A lost answer, a lease that expired before the
// outcome arrived, and an agent restart in between never call the driver a second time for the same document.
func TestFiscalAgent_LostOutcomeNoReissue(t *testing.T) {
	ctx := context.Background()
	journal := filepath.Join(t.TempDir(), "fiscal-journal.json")

	// Lost answer: the outcome never reached the instance; the journalled outcome is resent, nothing printed again.
	b := newFakeBridge("d1")
	b.dropNext = 2
	drv := &fakeFiscal{}
	a, _ := agentFor(t, b, drv, journal)
	if _, err := a.RunOnce(ctx); err == nil {
		t.Fatal("lost answer not reported")
	}
	// A new agent (app restart) reads the journal and reports without printing again.
	a2 := &FiscalAgent{Upstream: a.Upstream, Driver: a.Driver, Journal: journal}
	if _, err := a2.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if b.status["d1"] != "issued" || len(drv.issued) != 1 {
		t.Fatalf("after restart: status %s, driver calls %v", b.status["d1"], drv.issued)
	}

	// Expired lease: the outcome is refused (409), the document is claimed again: the journalled outcome is reported
	// under the new claim, no second print.
	b3 := newFakeBridge("d2")
	b3.expireAll = true
	drv3 := &fakeFiscal{}
	a3, _ := agentFor(t, b3, drv3, filepath.Join(t.TempDir(), "j.json"))
	var logs bytes.Buffer
	a3.Log = testLogger(&logs)
	if _, err := a3.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if b3.status["d2"] != "issued" || len(drv3.issued) != 1 || b3.keys["d2"] != "d2:"+b3.claim["d2"] {
		t.Fatalf("expired lease: status %s driver calls %v key %s", b3.status["d2"], drv3.issued, b3.keys["d2"])
	}
	// The refused outcome is logged with ids and status only (INV-70): no token, no device reference.
	if !strings.Contains(logs.String(), "fiscal outcome refused") || strings.Contains(logs.String(), testToken) ||
		strings.Contains(logs.String(), "RT-") {
		t.Fatalf("agent logs: %s", logs.String())
	}
	if raw, err := os.ReadFile(a3.Journal); err != nil || strings.Contains(string(raw), "d2") {
		t.Fatalf("journal kept an acknowledged outcome: %s %v", raw, err)
	}
}
