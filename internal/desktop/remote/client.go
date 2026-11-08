package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
)

// The online instance is chosen by the owner of the desktop and is very often on the office network (a desktop of
// the marina in local mode, a server in the office): the anti-SSRF egress client, which refuses private addresses
// by design, does not apply. The client never follows redirects, never uses an environment proxy and carries the
// device token only in the Authorization header of requests to the paired origin.

// Timeouts of the upstream client.
const (
	dialTimeout   = 10 * time.Second
	headerTimeout = 30 * time.Second
	pairTimeout   = 20 * time.Second
)

// NewTransport returns the HTTP transport to the origin u with the TLS policy of p.
func NewTransport(u *url.URL, p Pins) *http.Transport {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           d.DialContext,
		TLSClientConfig:       ClientTLSConfig(u.Hostname(), p),
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: headerTimeout,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// ParseOrigin validates the URL of an online instance: https (http only in development), no credentials, no
// path, query or fragment. It returns the origin URL.
func ParseOrigin(raw string, dev bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	bad := &desktopport.SettingsError{Fields: []string{"url"}}
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, bad
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !dev {
			return nil, bad
		}
	default:
		return nil, bad
	}
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}, nil
}

// PairLink is a parsed porto://pair?v=1&url=…&code=…&fp=… payload.
type PairLink struct {
	URL, Code, Fingerprint string
}

// ParsePairLink parses the "Copia collegamento" payload of the devices page.
func ParsePairLink(link string) (PairLink, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "porto" || (u.Host != "pair" && u.Opaque != "//pair") {
		return PairLink{}, &desktopport.SettingsError{Fields: []string{"link"}}
	}
	q := u.Query()
	if v := q.Get("v"); v != "" && v != "1" {
		return PairLink{}, &desktopport.SettingsError{Fields: []string{"link"}}
	}
	l := PairLink{URL: q.Get("url"), Code: q.Get("code"), Fingerprint: q.Get("fp")}
	if l.URL == "" || l.Code == "" {
		return PairLink{}, &desktopport.SettingsError{Fields: []string{"link"}}
	}
	return l, nil
}

// PairInput is what Pair sends to the online instance.
type PairInput struct {
	URL         string
	Code        string
	Fingerprint string // hex or base64url, "" for a public-CA target
	Name        string
	Platform    string
	AppVersion  string
	Dev         bool
	// Transport overrides the HTTP transport (tests); its TLS policy must still be the pairing one, so tests pass
	// nil and use real certificates.
	Transport http.RoundTripper
}

// Paired is the outcome of a successful pairing. Token is the device token (store it in the SecretStore only).
type Paired struct {
	Origin     *url.URL
	Token      string
	DeviceID   string
	MarinaID   string
	MarinaName string
	Pins       Pins
}

// Pair pairs the app with the online instance (POST /api/device/v1/pair, kind desktop). Errors:
// *desktopport.SettingsError (url, code, fp), *desktopport.PinMismatchError, desktopport.ErrRemoteUnreachable,
// *auth.RateLimitError.
func Pair(ctx context.Context, in PairInput) (*Paired, error) {
	origin, err := ParseOrigin(in.URL, in.Dev)
	if err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" || len(code) > 64 {
		return nil, &desktopport.SettingsError{Fields: []string{"code"}}
	}
	var leafFP string
	if in.Fingerprint != "" {
		if leafFP = NormalizeFingerprint(in.Fingerprint); leafFP == "" {
			return nil, &desktopport.SettingsError{Fields: []string{"fp"}}
		}
	}
	obs := &PairingObservation{}
	rt := in.Transport
	if rt == nil {
		tr := NewTransport(origin, Pins{})
		if leafFP != "" {
			tr.TLSClientConfig = PairingTLSConfig(leafFP, obs)
		}
		rt = tr
	}
	client := &http.Client{Transport: rt, CheckRedirect: noRedirect, Timeout: pairTimeout}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Porto desktop"
	}
	av := in.AppVersion
	body, _ := json.Marshal(api.PairRequest{
		Code: code, Kind: api.DeviceKind("desktop"), Name: name, Platform: api.DevicePlatform(platformOf(in.Platform)),
		AppVersion: &av,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String()+"/api/device/v1/pair", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &auth.RateLimitError{RetryAfter: time.Minute}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return nil, &desktopport.SettingsError{Fields: []string{"code"}}
	default:
		return nil, fmt.Errorf("%w: pairing answered %d", desktopport.ErrRemoteUnreachable, resp.StatusCode)
	}
	var res api.PairResult
	if err := json.Unmarshal(b, &res); err != nil || res.Token == "" || res.Device.Id == "" || res.Marina.Id == "" {
		return nil, fmt.Errorf("%w: unexpected pairing answer", desktopport.ErrRemoteUnreachable)
	}
	out := &Paired{
		Origin: origin, Token: res.Token, DeviceID: res.Device.Id, MarinaID: res.Marina.Id, MarinaName: res.Marina.Name,
	}
	if leafFP != "" {
		out.Pins = obs.Pins()
		if out.Pins.LeafFP == "" {
			out.Pins.LeafFP = leafFP
		}
	}
	return out, nil
}

func platformOf(p string) string {
	switch p {
	case "darwin", "macos":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return "other"
	}
}

// transportError keeps a pin mismatch (with its fingerprints) and turns every other network failure into
// ErrRemoteUnreachable.
func transportError(err error) error {
	var pm *desktopport.PinMismatchError
	if errors.As(err, &pm) {
		return pm
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %w", desktopport.ErrRemoteUnreachable, err)
}

// Upstream calls the online instance with the device token.
type Upstream struct {
	Origin *url.URL
	Client *http.Client
	Token  func() string
}

// NewUpstream returns the client of a paired instance.
func NewUpstream(origin *url.URL, p Pins, token func() string) *Upstream {
	return &Upstream{Origin: origin, Client: &http.Client{Transport: NewTransport(origin, p), CheckRedirect: noRedirect}, Token: token}
}

// JSON sends a JSON request (body may be nil) and decodes a JSON answer into out (may be nil). It returns the
// status code; transport errors are ErrRemoteUnreachable or *PinMismatchError.
func (u *Upstream) JSON(ctx context.Context, method, path string, header http.Header, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.Origin.String()+path, rd)
	if err != nil {
		return 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.Token())
	resp, err := u.Client.Do(req)
	if err != nil {
		return 0, transportError(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("remote: decode %s: %w", path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	}
	return resp.StatusCode, nil
}
