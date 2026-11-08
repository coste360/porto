package portal

import (
	"strings"
	"testing"
	"time"
)

func TestPortal_EmailLimiter(t *testing.T) {
	l := newEmailLimiter()
	t0 := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for i := 0; i < LoginPerEmail; i++ {
		if !l.allow("a", t0.Add(time.Duration(i)*time.Minute)) {
			t.Fatalf("request %d refused", i)
		}
	}
	if l.allow("a", t0.Add(5*time.Minute)) {
		t.Fatal("sixth request in the window allowed")
	}
	if !l.allow("b", t0.Add(5*time.Minute)) {
		t.Fatal("another email limited")
	}
	if !l.allow("a", t0.Add(LoginWindow+time.Minute)) {
		t.Fatal("request after the window refused")
	}
}

func TestPortal_TokenShape(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(tok) || validToken(tok[1:]) || validToken(strings.Repeat("-", TokenChars)) {
		t.Fatalf("token %q", tok)
	}
	if h := hashToken(tok); len(h) != 64 || h == hashToken(tok+"x") {
		t.Fatalf("hash %q", h)
	}
	s := NewService(Deps{BaseURL: "https://porto.test/"})
	if got := s.LinkURL("demo", tok); got != "https://porto.test/b/demo/a#t="+tok {
		t.Fatalf("url %s", got)
	}
}
