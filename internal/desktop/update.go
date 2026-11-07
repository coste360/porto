package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/internal/desktopport"
)

// Version check (m6a-plan §2.8, D16): notify only, never download or install, never required.
const (
	UpdateInterval  = 24 * time.Hour
	maxUpdateBody   = 64 << 10
	updateTimeout   = 15 * time.Second
	maxUpdateURLLen = 2048
)

// Updater checks the version manifest. With the check off it makes no request at all.
type Updater struct {
	URL     string
	Client  *http.Client
	Version string
	GOOS    string
	GOARCH  string
	Log     *slog.Logger
	// Enabled reads the current switch (desktop.json and PORTO_UPDATE_CHECK).
	Enabled func() bool

	mu     sync.Mutex
	latest *desktopport.UpdateInfo
}

type updateManifest struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
}

// UserAgent is the only thing the request says about this installation: product, version and OS.
func (u *Updater) UserAgent() string {
	return fmt.Sprintf("Porto-Desktop/%s (%s; %s)", u.Version, u.GOOS, u.GOARCH)
}

// Latest returns the newer version found by the last successful check, nil when none (or the check is off).
func (u *Updater) Latest() *desktopport.UpdateInfo {
	if u == nil || (u.Enabled != nil && !u.Enabled()) {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest == nil {
		return nil
	}
	c := *u.latest
	return &c
}

// Check runs one check when enabled. Any failure is logged at debug level and leaves the previous result.
func (u *Updater) Check(ctx context.Context) {
	if u.Enabled != nil && !u.Enabled() {
		return
	}
	info, err := u.fetch(ctx)
	if err != nil {
		if u.Log != nil {
			u.Log.DebugContext(ctx, "desktop version check failed", "error", err)
		}
		return
	}
	u.mu.Lock()
	u.latest = info
	u.mu.Unlock()
}

func (u *Updater) fetch(ctx context.Context) (*desktopport.UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.URL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", u.UserAgent())
	req.Header.Set("Accept", "application/json")
	res, err := u.Client.Do(req) //nolint:gosec // G704: egress client (https, public addresses only)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(res.Body, maxUpdateBody)).Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	latest, ok := parseSemver(m.Version)
	if !ok {
		return nil, errors.New("manifest: bad version")
	}
	current, ok := parseSemver(u.Version)
	if !ok || !semverLess(current, latest) {
		return nil, nil
	}
	link, err := url.Parse(m.URL)
	if err != nil || link.Scheme != "https" || link.Host == "" || len(m.URL) > maxUpdateURLLen {
		return nil, errors.New("manifest: bad url")
	}
	return &desktopport.UpdateInfo{Version: strings.TrimPrefix(m.Version, "v"), URL: link.String()}, nil
}

// Run checks at start and every UpdateInterval until ctx is done; recheck triggers an immediate check (the switch
// was turned on).
func (u *Updater) Run(ctx context.Context, recheck <-chan struct{}) {
	u.Check(ctx)
	t := time.NewTicker(UpdateInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.Check(ctx)
		case <-recheck:
			u.Check(ctx)
		}
	}
}

// parseSemver reads MAJOR.MINOR.PATCH with an optional "v" prefix; pre-release and build suffixes make the
// version invalid (a development build is never "older" than a release and never sees a banner).
func parseSemver(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func semverLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
