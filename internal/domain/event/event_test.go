package event

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

var t0 = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func TestSign_Vector(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	got := Sign("whsec_test", body, t0)
	want := "t=1793959200,v1=374c33c3a86e68e494b1040c2c8c0bbe43b71d7742c2dab6190c376cf411c452"
	if got != want {
		t.Fatalf("Sign = %s\nwant  %s", got, want)
	}
	both := SignAll([]string{"whsec_test", "whsec_old"}, body, t0)
	if both != want+",v1=2bea8b04d50e6973ca7a386bea1e9b9a850af3e088359487fefffcd39f07d513" {
		t.Fatalf("SignAll = %s", both)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"x":1}`)
	h := SignAll([]string{"new", "old"}, body, t0)
	for _, secret := range []string{"new", "old"} {
		if err := VerifySignature(secret, h, body, t0.Add(time.Minute), 5*time.Minute); err != nil {
			t.Fatalf("%s: %v", secret, err)
		}
	}
	cases := []struct {
		name   string
		secret string
		header string
		body   []byte
		now    time.Time
		want   error
	}{
		{"wrong secret", "other", h, body, t0, ErrSignatureMismatch},
		{"tampered body", "new", h, []byte(`{"x":2}`), t0, ErrSignatureMismatch},
		{"too old", "new", h, body, t0.Add(10 * time.Minute), ErrSignatureExpired},
		{"from the future", "new", h, body, t0.Add(-10 * time.Minute), ErrSignatureExpired},
		{"garbage", "new", "nonsense", body, t0, ErrSignatureMalformed},
		{"no v1", "new", "t=1793959200", body, t0, ErrSignatureMalformed},
	}
	for _, tc := range cases {
		if err := VerifySignature(tc.secret, tc.header, tc.body, tc.now, 5*time.Minute); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, err, tc.want)
		}
	}
}

func TestBackoff(t *testing.T) {
	cases := map[int]time.Duration{
		0: 30 * time.Second, 1: 30 * time.Second, 2: time.Minute, 3: 2 * time.Minute, 5: 8 * time.Minute,
		10: 512 * 30 * time.Second, 11: 6 * time.Hour, 50: 6 * time.Hour,
	}
	for n, want := range cases {
		if got := Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestEgress_BlocksPrivate(t *testing.T) {
	p := EgressPolicy{}
	blocked := []string{
		"127.0.0.1", "127.255.255.254", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "169.254.0.1", "100.64.0.1", "100.127.255.255", "0.0.0.0", "0.1.2.3",
		"224.0.0.1", "255.255.255.255", "::1", "::", "fc00::1", "fd12:3456::1", "fe80::1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", "::ffff:10.0.0.1", "::127.0.0.1", "64:ff9b::7f00:1",
		"2002:7f00:1::", "192.0.2.10", "198.18.0.1",
	}
	for _, s := range blocked {
		if err := p.CheckAddr(netip.MustParseAddr(s)); !errors.Is(err, ErrEgressBlocked) {
			t.Errorf("%s: want blocked, got %v", s, err)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "172.32.0.1", "100.128.0.1", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if err := p.CheckAddr(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s: want allowed, got %v", s, err)
		}
	}
	if err := (EgressPolicy{AllowPrivate: true}).CheckAddr(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("AllowPrivate: %v", err)
	}
}

func TestEgress_CheckURL(t *testing.T) {
	p := EgressPolicy{}
	cases := map[string]bool{
		"https://hooks.example.com/porto":      true,
		"https://hooks.example.com:8443/x?y=1": true,
		"http://hooks.example.com/":            false,
		"ftp://hooks.example.com/":             false,
		"https://user:pw@hooks.example.com/":   false,
		"https://127.0.0.1/":                   false,
		"https://[::1]/":                       false,
		"https://localhost/":                   false,
		"https://a.localhost/":                 false,
		"https:///nohost":                      false,
		"/relative":                            false,
		"https://hooks.example.com/#frag":      false,
	}
	for raw, ok := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.CheckURL(u); (err == nil) != ok {
			t.Errorf("%s: ok=%v err=%v", raw, ok, err)
		}
	}
	u, _ := url.Parse("http://127.0.0.1:9/")
	if err := (EgressPolicy{AllowHTTP: true, AllowPrivate: true}).CheckURL(u); err != nil {
		t.Errorf("dev policy: %v", err)
	}
}

func TestEgress_DNSRebinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	// The name first resolves to a public address (what a validation step would see), then to loopback when
	// the client dials: the dial must be refused.
	var calls atomic.Int32
	lookup := func(context.Context, string) ([]netip.Addr, error) {
		if calls.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	p := EgressPolicy{AllowHTTP: true}
	first, _ := lookup(context.Background(), "rebind.example")
	if err := p.CheckAddr(first[0]); err != nil {
		t.Fatalf("first resolution should pass: %v", err)
	}
	client := newEgressClient(p, 2*time.Second, lookup)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://rebind.example:"+port+"/", nil)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("request to a rebound loopback address succeeded")
	}
	if !errors.Is(err, ErrEgressBlocked) {
		t.Fatalf("want ErrEgressBlocked, got %v", err)
	}

	// Literal private IPs are refused by the socket check too.
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("literal loopback allowed")
	}
}

func TestEgress_NoRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("redirect followed")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	client := NewEgressClient(EgressPolicy{AllowHTTP: true, AllowPrivate: true}, 2*time.Second)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestEvents_DataHasNoPII(t *testing.T) {
	samples := Samples()
	for _, ti := range Types() {
		sample, ok := samples[ti.Type]
		if !ok {
			t.Errorf("no sample for %s", ti.Type)
			continue
		}
		b, _ := json.Marshal(sample)
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if err := CheckNoPII(m); err != nil {
			t.Errorf("%s: %v", ti.Type, err)
		}
	}
	if err := CheckNoPII(map[string]any{"x": map[string]any{"user_email": "a"}}); err == nil {
		t.Fatal("nested PII key not detected")
	}
}

func TestTypes_Catalogue(t *testing.T) {
	seen := map[string]bool{}
	for _, ti := range Types() {
		if seen[ti.Type] {
			t.Fatalf("duplicate %s", ti.Type)
		}
		seen[ti.Type] = true
		if ti.Type == WebhookTest {
			if ti.Public || ti.Topic != "" {
				t.Fatalf("webhook.test must be private and not on SSE: %+v", ti)
			}
			continue
		}
		// M1 types (m1-plan §4.6): only the spec 27.3 ones are public.
		m1 := map[string]TypeInfo{
			ConfigMapChanged:   {ConfigMapChanged, TopicMap, true},
			CustomerCreated:    {CustomerCreated, TopicRegistry, true},
			CustomerAnonymized: {CustomerAnonymized, TopicRegistry, true},
			CustomerUpdated:    {CustomerUpdated, TopicRegistry, false},
			CustomerMerged:     {CustomerMerged, TopicRegistry, false},
			VesselChanged:      {VesselChanged, TopicRegistry, false},
			DocumentChanged:    {DocumentChanged, TopicRegistry, false},
			DeadlineChanged:    {DeadlineChanged, TopicRegistry, false},
			// M2 (m2-plan §4.7): public (spec 27.3).
			ConfigPricingChanged: {ConfigPricingChanged, TopicPricing, true},
		}
		if want, ok := m1[ti.Type]; ok {
			if ti != want {
				t.Errorf("%s: %+v, want %+v", ti.Type, ti, want)
			}
			continue
		}
		wantTopic := TopicTeam
		if strings.HasPrefix(ti.Type, "organization.") || strings.HasPrefix(ti.Type, "marina.") {
			wantTopic = TopicMarina
		}
		if ti.Topic != wantTopic || !ti.Public {
			t.Errorf("%s: %+v", ti.Type, ti)
		}
	}
	if IsPublic("nope") || TopicOf("nope") != "" {
		t.Fatal("unknown type")
	}
}

func TestEnvelope(t *testing.T) {
	ev := ext.Event{ID: "0190", Type: MemberAdded, Version: 1, MarinaID: "m", OccurredAt: t0, Data: json.RawMessage(`{"user_id":"u"}`)}
	b, _ := json.Marshal(EnvelopeOf(ev))
	want := `{"id":"evt_0190","type":"member.added","version":1,"marina_id":"m","occurred_at":"2026-11-06T10:00:00Z","data":{"user_id":"u"}}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestBroker_FanOutAndDrop(t *testing.T) {
	b := NewBroker(slog.New(slog.DiscardHandler), 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chA, stopA, err := b.Subscribe(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	chB, _, _ := b.Subscribe(ctx, "b")
	_ = b.Publish(ctx, ext.Event{ID: "1", MarinaID: "a"})
	_ = b.Publish(ctx, ext.Event{ID: "2", MarinaID: "a"}) // buffer full: dropped
	if ev := <-chA; ev.ID != "1" {
		t.Fatalf("got %+v", ev)
	}
	select {
	case ev := <-chB:
		t.Fatalf("marina b got %+v", ev)
	default:
	}
	if b.Dropped() != 1 {
		t.Fatalf("dropped = %d", b.Dropped())
	}
	stopA()
	stopA()
	if _, ok := <-chA; ok {
		t.Fatal("channel not closed")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for b.Subscribers() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Subscribers() != 0 {
		t.Fatal("subscription not removed on context end")
	}
	if _, _, err := b.Subscribe(ctx, ""); err == nil {
		t.Fatal("empty marina accepted")
	}
}
