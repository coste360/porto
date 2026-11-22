package testjar

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestJar_IgnoresWallClockExpiry(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:8080/api")
	j := New()
	// A session set by a server on a fake clock in the past: already expired on the wall clock.
	past := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	j.SetCookies(u, []*http.Cookie{
		{Name: "porto_session", Value: "s", Path: "/", Expires: past, HttpOnly: true},
		{Name: "porto_csrf", Value: "c", Path: "/", Expires: past},
	})
	if got := len(j.Cookies(u)); got != 2 {
		t.Fatalf("cookies kept = %d, want 2", got)
	}

	// The server deletes a cookie with Max-Age < 0 (logout) or an Expires at the Unix epoch.
	j.SetCookies(u, []*http.Cookie{
		{Name: "porto_session", Value: "", Path: "/", MaxAge: -1},
		{Name: "porto_csrf", Value: "", Path: "/", Expires: time.Unix(0, 0)},
	})
	if got := j.Cookies(u); len(got) != 0 {
		t.Fatalf("cookies after deletion = %v, want none", got)
	}
}
