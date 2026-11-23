// Package testjar is the cookie jar of the HTTP tests.
//
// net/http/cookiejar drops a cookie whose Expires is before the wall clock. The servers under test run on a
// clock.Fake fixed in the past (fixture.T0 and the scenarios' own dates), so the session cookie they set expires
// at that fake time plus the session lifetime: once the real date passes it, the jar stops sending the cookie
// and every authenticated request answers 401. The tests would then fail on a calendar date, not on the code.
//
// The jar returned by New ignores Expires and keeps a cookie until the server deletes it (Max-Age <= 0, or an
// Expires at the Unix epoch): whether a session has expired is decided by the server against its injected clock,
// which is what the tests check.
package testjar

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// deletedBefore marks an Expires used to delete a cookie (the Unix epoch, by convention), not a real expiry.
var deletedBefore = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

type jar struct{ inner *cookiejar.Jar }

// New returns an empty jar that never expires a cookie on the wall clock.
func New() http.CookieJar {
	inner, err := cookiejar.New(nil)
	if err != nil {
		panic(err) // cookiejar.New fails only on a bad PublicSuffixList, and none is given
	}
	return &jar{inner: inner}
}

func (j *jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	kept := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		cc := *c //nolint:gosec // G124: a copy of a cookie the server set, its attributes are kept as received
		if cc.MaxAge >= 0 && (cc.Expires.IsZero() || cc.Expires.After(deletedBefore)) {
			cc.Expires = time.Time{}
			cc.RawExpires = ""
		}
		kept = append(kept, &cc)
	}
	j.inner.SetCookies(u, kept)
}

func (j *jar) Cookies(u *url.URL) []*http.Cookie { return j.inner.Cookies(u) }
