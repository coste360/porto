package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/http/problem"
)

// Proxy is the loopback server of the remote mode (m6b-plan §7.4, D11): it reverse-proxies the panel and its API
// to the online instance adding the device token, answers /api/desktop/v1 and /api/bridge/v1 locally, and
// serves the cached shell when the instance is unreachable. Only the app window may use it: the Host must be the
// loopback origin, every request carries the porto_remote cookie obtained once with the per-launch secret of
// /__porto/enter, unsafe requests must be same-origin. The device token never reaches the page (INV-85).
type Proxy struct {
	o     Options
	up    *Upstream
	rp    *httputil.ReverseProxy
	cache *ShellCache
	local http.Handler

	mu       sync.Mutex
	launch   string          // hash of the current launch secret ("" = used)
	sessions map[string]bool // hashes of the porto_remote cookies handed out

	offline   atomic.Bool // dev switch (setDesktopRemoteOffline)
	reachable atomic.Bool
	revoked   atomic.Bool
	checking  atomic.Bool
}

// Options configure a Proxy.
type Options struct {
	Origin *url.URL
	Pins   Pins
	// Token returns the device token (read from the SecretStore by the caller).
	Token func() string
	// Port is the loopback port the proxy is served on.
	Port int
	// CacheDir holds the shell cache (<data>/remote-cache).
	CacheDir string
	// Dev enables setDesktopRemoteOffline (PORTO_DEV=true).
	Dev bool
	// Local serves /api/desktop/v1 and /api/bridge/v1 (see NewLocalAPI); nil = 404.
	Local http.Handler
	// OnRevoked is called once when the instance answers 401 to the device token itself (device revoked).
	OnRevoked func()
	// Transport overrides the upstream transport (tests); nil = NewTransport(Origin, Pins).
	Transport http.RoundTripper
	Log       *slog.Logger
}

// Paths and cookie of the proxy.
const (
	EnterPath    = "/__porto/enter"
	CookieName   = "porto_remote"
	AppPinHeader = "Porto-App-Pin"
)

// New returns a Proxy.
func New(o Options) *Proxy {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	tr := o.Transport
	if tr == nil {
		tr = NewTransport(o.Origin, o.Pins)
	}
	p := &Proxy{
		o: o, cache: &ShellCache{Dir: o.CacheDir}, local: o.Local, sessions: map[string]bool{},
		up: &Upstream{Origin: o.Origin, Client: &http.Client{Transport: tr, CheckRedirect: noRedirect}, Token: o.Token},
	}
	p.reachable.Store(true)
	p.rp = &httputil.ReverseProxy{
		Rewrite:        p.rewrite,
		Transport:      tr,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.errorHandler,
		ErrorLog:       nil,
	}
	return p
}

// Upstream returns the client of the online instance (device token, pinned TLS).
func (p *Proxy) Upstream() *Upstream { return p.up }

// Cache returns the shell cache.
func (p *Proxy) Cache() *ShellCache { return p.cache }

// Reachable reports whether the last upstream request succeeded (false while the dev switch is on).
func (p *Proxy) Reachable() bool { return p.reachable.Load() && !p.offline.Load() }

// SetOffline is the dev switch of setDesktopRemoteOffline.
func (p *Proxy) SetOffline(off bool) { p.offline.Store(off) }

// EnterURL mints a new launch secret and returns the URL the window opens: the secret is in the fragment, so it
// never appears in a request line or a log; the enter page exchanges it once for the porto_remote cookie and
// opens next (a panel path under /admin/, default /admin/).
func (p *Proxy) EnterURL(next string) (string, error) {
	k, err := crypto.RandomToken(32)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.launch = crypto.SHA256Hex(k)
	p.mu.Unlock()
	u := p.origin() + EnterPath + "#k=" + k
	if next != "" && next != "/admin/" && strings.HasPrefix(next, "/admin/") && !strings.Contains(next, "//") {
		u += "&next=" + url.QueryEscape(next)
	}
	return u, nil
}

func (p *Proxy) origin() string { return "http://127.0.0.1:" + strconv.Itoa(p.o.Port) }

func (p *Proxy) hostAllowed(h string) bool {
	port := strconv.Itoa(p.o.Port)
	return h == net.JoinHostPort("127.0.0.1", port) || h == net.JoinHostPort("localhost", port)
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.hostAllowed(r.Host) {
		http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
		return
	}
	if r.URL.Path == EnterPath {
		p.enter(w, r)
		return
	}
	// A second launch brings this window to the front with the instance token (checked by the runtime).
	if r.Method == http.MethodPost && r.URL.Path == "/api/desktop/v1/activate" && p.local != nil {
		p.local.ServeHTTP(w, r)
		return
	}
	if !p.hasSession(r) {
		problem.Write(w, r, problem.Unauthorized)
		return
	}
	if unsafe(r.Method) && !p.sameOrigin(r) {
		problem.Write(w, r, problem.Forbidden)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/desktop/v1/"), strings.HasPrefix(r.URL.Path, "/api/bridge/v1/"):
		if p.local == nil {
			problem.Write(w, r, problem.NotFound)
			return
		}
		p.local.ServeHTTP(w, r)
		return
	case r.URL.Path == "/" || r.URL.Path == "":
		http.Redirect(w, r, "/admin/", http.StatusFound)
		return
	}
	if p.offline.Load() || p.revoked.Load() {
		p.serveOffline(w, r)
		return
	}
	p.rp.ServeHTTP(w, r)
}

func unsafe(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

func (p *Proxy) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	o := r.Header.Get("Origin")
	return o != "" && (o == "http://"+r.Host)
}

func (p *Proxy) hasSession(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}
	h := crypto.SHA256Hex(c.Value)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[h]
}

// enterMessages are the only user-facing strings of the enter page (it, the source language, and en): the page is
// served before the panel and its i18n catalogue load, so it picks the language from the webview locale.
const enterMessages = `var enterRefusedIT='Porto: accesso non valido. Riavvia l\'app.',` +
	`enterRefusedEN='Porto: access refused. Restart the app.';`

// enterScript reads the launch secret from the fragment, removes it from the address bar and history, exchanges it
// for the cookie and opens the panel.
const enterScript = `(function(){` + enterMessages + `var q=new URLSearchParams(location.hash.slice(1)),k=q.get('k')||'',n=q.get('next')||'';` +
	`if(n.indexOf('/admin/')!==0||n.indexOf('//')>=0){n='/admin/'}` +
	`history.replaceState(null,'',location.pathname);` +
	`fetch('/__porto/enter',{method:'POST',credentials:'same-origin',` +
	`headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'k='+encodeURIComponent(k)})` +
	`.then(function(r){if(r.ok){location.replace(n)}else{var it=/^it\b/i.test(navigator.language||'');` +
	`document.documentElement.lang=it?'it':'en';` +
	`document.body.textContent=it?enterRefusedIT:enterRefusedEN}})})();`

var enterCSP = func() string {
	sum := sha256.Sum256([]byte(enterScript))
	return "default-src 'none'; connect-src 'self'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

func (p *Proxy) enter(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	switch r.Method {
	case http.MethodGet:
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", enterCSP)
		_, _ = io.WriteString(w, `<!doctype html><html lang="it"><head><meta charset="utf-8"><title>Porto</title></head><body><script>`+enterScript+`</script></body></html>`)
	case http.MethodPost:
		if !p.sameOrigin(r) {
			problem.Write(w, r, problem.Forbidden)
			return
		}
		_ = r.ParseForm()
		k := r.PostForm.Get("k")
		p.mu.Lock()
		ok := p.launch != "" && k != "" && crypto.ConstantTimeEqual(crypto.SHA256Hex(k), p.launch)
		if ok {
			p.launch = "" // single use
		}
		p.mu.Unlock()
		if !ok {
			problem.Write(w, r, problem.Unauthorized)
			return
		}
		sess, err := crypto.RandomToken(32)
		if err != nil {
			problem.Write(w, r, problem.InternalError)
			return
		}
		p.mu.Lock()
		p.sessions[crypto.SHA256Hex(sess)] = true
		p.mu.Unlock()
		// Not Secure: the loopback origin is plain http (a Secure cookie would never be sent back).
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: sess, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}) //nolint:gosec // G124: loopback http origin
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// strippedRequestHeaders never leave the app: credentials of the page, local secrets, forwarding headers.
var strippedRequestHeaders = []string{
	"Cookie", "Authorization", AppPinHeader, "X-Csrf-Token", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host",
	"X-Forwarded-Proto", "X-Real-Ip", "Origin", "Referer", "Sec-Fetch-Site",
}

func (p *Proxy) rewrite(pr *httputil.ProxyRequest) {
	pr.SetURL(p.o.Origin)
	pr.Out.Host = p.o.Origin.Host
	for _, k := range strippedRequestHeaders {
		pr.Out.Header.Del(k)
	}
	pr.Out.Header.Set("Authorization", "Bearer "+p.o.Token())
	// The panel is same-origin for the instance: tell it the request comes from its own origin.
	if unsafe(pr.In.Method) {
		pr.Out.Header.Set("Origin", p.o.Origin.String())
	}
	if pr.In.Method == http.MethodGet {
		if kind, _ := classify(pr.In.URL.Path); kind != "" {
			pr.Out.Header.Del("If-None-Match") // the cache needs full bodies
			pr.Out.Header.Del("If-Modified-Since")
		}
	}
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	p.reachable.Store(true)
	h := resp.Header
	h.Del("Set-Cookie")
	h.Del("Strict-Transport-Security") // the loopback origin is plain http
	if loc := h.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil && u.IsAbs() && u.Host == p.o.Origin.Host {
			u.Scheme, u.Host = "", ""
			h.Set("Location", u.String())
		}
	}
	req := resp.Request
	if resp.StatusCode == http.StatusUnauthorized && strings.HasPrefix(req.URL.Path, "/api/") {
		p.checkRevoked()
	}
	if req.Method == http.MethodGet && resp.StatusCode == http.StatusOK {
		if kind, name := classify(req.URL.Path); kind != "" && cacheable(resp, kind) {
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxCachedBody+1))
			_ = resp.Body.Close()
			if err != nil {
				return err
			}
			if len(body) <= maxCachedBody {
				if err := p.cache.Store(kind, name, body); err != nil {
					p.o.Log.Warn("remote: shell cache", "error", err)
				}
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			h.Set("Content-Length", strconv.Itoa(len(body)))
		}
	}
	return nil
}

func cacheable(resp *http.Response, kind string) bool {
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return false
	}
	if kind == shellIndex {
		return strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html")
	}
	return true
}

// checkRevoked asks the instance whether the device token itself is still valid (a 401 can also be a panel
// request without permission); a 401 on getDeviceSelf means the device was revoked.
func (p *Proxy) checkRevoked() {
	if p.revoked.Load() || !p.checking.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer p.checking.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		code, err := p.up.JSON(ctx, http.MethodGet, "/api/device/v1/self", nil, nil, nil)
		if err == nil && code == http.StatusUnauthorized && p.revoked.CompareAndSwap(false, true) {
			p.o.Log.Warn("remote: the device was revoked by the online instance")
			if p.o.OnRevoked != nil {
				p.o.OnRevoked()
			}
		}
	}()
}

// Revoked reports whether the device was found revoked.
func (p *Proxy) Revoked() bool { return p.revoked.Load() }

func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	var pm *desktopport.PinMismatchError
	if errors.As(err, &pm) {
		p.reachable.Store(false)
		p.o.Log.Warn("remote: certificate of the online instance does not match the pin")
		problem.Write(w, r, problem.RemotePinMismatch, problem.WithDetails(map[string]string{
			"expected": pm.Expected, "presented": pm.Presented,
		}))
		return
	}
	p.reachable.Store(false)
	p.o.Log.Warn("remote: online instance unreachable", "error", err)
	p.serveOffline(w, r)
}

// serveOffline answers without the instance: the cached shell, 503 remote_unreachable for the API.
func (p *Proxy) serveOffline(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if kind, name := classify(r.URL.Path); kind != "" {
			if b, ct, ok := p.cache.Load(kind, name); ok {
				w.Header().Set("Content-Type", ct)
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Content-Length", strconv.Itoa(len(b)))
				if r.Method == http.MethodGet {
					_, _ = w.Write(b)
				}
				return
			}
		}
	}
	problem.Write(w, r, problem.RemoteUnreachable, problem.RetryAfter(5*time.Second))
}
