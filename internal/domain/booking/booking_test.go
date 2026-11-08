package booking

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/store"
)

func testService(t *testing.T) (*Service, *clock.Fake) {
	t.Helper()
	keys, err := crypto.ParseKeyRing(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2027, 7, 1, 10, 0, 0, 0, time.UTC))
	return NewService(Deps{Keys: keys, Clock: clk}), clk
}

func TestBookingToken_RoundTrip(t *testing.T) {
	s, _ := testService(t)
	ob := &store.OnlineBooking{ID: store.NewID(), TokenNonce: "nonce-1"}
	tok, err := s.bookingToken(ob.ID, ob.TokenNonce)
	if err != nil || len(tok) < 40 {
		t.Fatalf("token %q %v", tok, err)
	}
	again, _ := s.bookingToken(ob.ID, ob.TokenNonce)
	if again != tok {
		t.Fatal("token not recomputable (idempotent replay)")
	}
	if !s.checkBookingToken(ob, tok) || !s.checkBookingToken(ob, strings.ToUpper(tok)) {
		t.Fatal("valid token refused")
	}
	other := &store.OnlineBooking{ID: store.NewID(), TokenNonce: "nonce-1"}
	for name, c := range map[string]struct {
		ob  *store.OnlineBooking
		tok string
	}{
		"empty": {ob, ""}, "other booking": {other, tok}, "other nonce": {&store.OnlineBooking{ID: ob.ID, TokenNonce: "n2"}, tok},
		"truncated": {ob, tok[:len(tok)-1]},
	} {
		if s.checkBookingToken(c.ob, c.tok) {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestStatusToken_Expiry(t *testing.T) {
	s, clk := testService(t)
	id := store.NewID()
	exp := clk.Now().Add(48 * time.Hour)
	tok, err := s.statusToken(id, exp)
	if err != nil {
		t.Fatal(err)
	}
	if !s.checkStatusToken(id, tok, clk.Now()) {
		t.Fatal("valid status token refused")
	}
	if s.checkStatusToken(store.NewID(), tok, clk.Now()) {
		t.Fatal("status token of another booking accepted")
	}
	if s.checkStatusToken(id, tok, exp.Add(time.Second)) {
		t.Fatal("expired status token accepted")
	}
	e, sig, _ := strings.Cut(tok, ".")
	if s.checkStatusToken(id, e+"9."+sig, clk.Now()) || s.checkStatusToken(id, "garbage", clk.Now()) {
		t.Fatal("tampered status token accepted")
	}
	if bt, _ := s.bookingToken(id, "n"); s.checkStatusToken(id, bt, clk.Now()) {
		t.Fatal("a booking token is not a status token")
	}
}

func TestLostDemand_DedupeTTL(t *testing.T) {
	d := newDedupe(30*time.Minute, 3)
	now := time.Date(2027, 7, 1, 10, 0, 0, 0, time.UTC)
	if d.seen("a", now) || !d.seen("a", now.Add(29*time.Minute)) {
		t.Fatal("repeat within TTL not deduped")
	}
	if d.seen("a", now.Add(31*time.Minute)) {
		t.Fatal("entry outlived its TTL")
	}
	base := now.Add(time.Hour)
	for _, k := range []string{"b", "c", "d", "e"} {
		d.seen(k, base)
	}
	if len(d.byKey) > 3 {
		t.Fatalf("cache grew to %d entries", len(d.byKey))
	}
}

func TestBooking_ConsentTexts(t *testing.T) {
	cur := []onlineconf.Text{{Kind: "terms", Version: 2, SHA256: "t"}, {Kind: "privacy", Version: 1, SHA256: "p"}}
	if got, err := consentTexts(cur, Consents{TermsVersion: 2, PrivacyVersion: 1}); err != nil || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := consentTexts(cur, Consents{TermsVersion: 1, PrivacyVersion: 1}); !errors.Is(err, ErrTextOutdated) {
		t.Fatalf("outdated: %v", err)
	}
	if _, err := consentTexts(cur, Consents{PrivacyVersion: 1}); !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("missing terms: %v", err)
	}
	// Terms or privacy not published: refused, never stored without consents (spec 30, m8-plan §5.1).
	for _, published := range [][]onlineconf.Text{nil, cur[:1], cur[1:]} {
		_, err := consentTexts(published, Consents{TermsVersion: 2, PrivacyVersion: 1})
		var re *RulesError
		if !errors.As(err, &re) || re.Field != "consents" || re.Code != CodeTextsUnpublished {
			t.Fatalf("unpublished %v: %v", published, err)
		}
	}
	if _, err := consentTexts(cur, Consents{TermsVersion: 2, PrivacyVersion: 1, Marketing: true, MarketingVersion: 1}); !errors.Is(err, ErrTextOutdated) {
		t.Fatalf("marketing without text: %v", err)
	}
}

func TestBooking_ReturnURLAndOrigin(t *testing.T) {
	s, _ := testService(t)
	s.d.BaseURL = "https://porto.test/"
	m := &store.Marina{Slug: "porto-a", AllowedEmbedOrigins: store.Strings{"https://club.example"}}
	if got := allowedReturnTo(m, "https://club.example/prenota?x=1"); got != "https://club.example/prenota?x=1" {
		t.Fatalf("allowed return_to dropped: %q", got)
	}
	for _, bad := range []string{"https://evil.example/", "javascript:alert(1)", "//club.example/", ""} {
		if allowedReturnTo(m, bad) != "" {
			t.Fatalf("return_to %q accepted", bad)
		}
	}
	u := s.returnURL(&marinaCtx{m: m}, "id1", "st1", "https://club.example/x", true)
	if !strings.HasPrefix(u, "https://porto.test/b/porto-a/r/id1#") || !strings.Contains(u, "st=st1") || !strings.Contains(u, "cancelled=1") {
		t.Fatalf("return url %q", u)
	}
	if end := arrivalDayEnd(calendar.MustParse("2027-07-02"), time.UTC); end.Format(time.RFC3339) != "2027-07-02T23:59:00Z" {
		t.Fatalf("arrival day end %v", end)
	}
}
