package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ShellCache keeps the panel shell of the online instance for an offline start (m6b-plan §7.4): the index (the
// SPA entry, served for every navigation under /admin/), the hashed assets and the top-level static files. Only
// the shell is cached, never API data. Assets are tagged with the index generation that fetched them and pruned
// once they are older than the last two index versions (lazy chunks are referenced from JavaScript, not from the
// index, so the generation is the reliable marker).
type ShellCache struct {
	Dir string

	mu sync.Mutex
}

// Shell resource kinds.
const (
	shellIndex  = "index"
	shellAsset  = "asset"
	shellStatic = "static"
)

const maxCachedBody = 32 << 20

var (
	safeName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,200}$`)
	staticSuffix = []string{".png", ".svg", ".ico", ".webmanifest", ".woff2", ".txt"}
)

// classify tells whether a GET path is part of the shell and how it is cached.
func classify(p string) (kind, name string) {
	if p == "/admin" || p == "/admin/" {
		return shellIndex, ""
	}
	rest, ok := strings.CutPrefix(p, "/admin/")
	if !ok {
		return "", ""
	}
	if a, ok := strings.CutPrefix(rest, "assets/"); ok {
		if safeName.MatchString(a) && !strings.Contains(a, "..") {
			return shellAsset, a
		}
		return "", ""
	}
	if !strings.Contains(rest, "/") {
		for _, s := range staticSuffix {
			if strings.HasSuffix(rest, s) && safeName.MatchString(rest) && !strings.Contains(rest, "..") {
				return shellStatic, rest
			}
		}
	}
	if path.Ext(rest) == "" && !strings.HasPrefix(rest, "api/") {
		return shellIndex, "" // an SPA route
	}
	return "", ""
}

type cacheManifest struct {
	Generation int            `json:"generation"`
	Assets     map[string]int `json:"assets"`
}

func (c *ShellCache) file(parts ...string) string {
	return filepath.Join(append([]string{c.Dir}, parts...)...)
}

func (c *ShellCache) manifest() cacheManifest {
	m := cacheManifest{Assets: map[string]int{}}
	b, err := os.ReadFile(c.file("manifest.json"))
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m.Assets == nil {
		m.Assets = map[string]int{}
	}
	return m
}

func (c *ShellCache) saveManifest(m cacheManifest) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeAtomic(c.file("manifest.json"), b)
}

// Store saves a shell resource.
func (c *ShellCache) Store(kind, name string, body []byte) error {
	if c == nil || c.Dir == "" || len(body) > maxCachedBody {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.file("assets"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(c.file("static"), 0o700); err != nil {
		return err
	}
	m := c.manifest()
	switch kind {
	case shellIndex:
		cur, err := os.ReadFile(c.file("index.html"))
		if err == nil && bytes.Equal(cur, body) {
			return nil
		}
		if err == nil {
			if err := writeAtomic(c.file("index.prev.html"), cur); err != nil {
				return err
			}
		}
		if err := writeAtomic(c.file("index.html"), body); err != nil {
			return err
		}
		m.Generation++
		for n, g := range m.Assets {
			if g < m.Generation-1 {
				_ = os.Remove(c.file("assets", n))
				delete(m.Assets, n)
			}
		}
		return c.saveManifest(m)
	case shellAsset:
		if err := writeAtomic(c.file("assets", name), body); err != nil {
			return err
		}
		m.Assets[name] = m.Generation
		return c.saveManifest(m)
	case shellStatic:
		return writeAtomic(c.file("static", name), body)
	}
	return nil
}

// Load returns a cached shell resource and its content type.
func (c *ShellCache) Load(kind, name string) ([]byte, string, bool) {
	if c == nil || c.Dir == "" {
		return nil, "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var p, ct string
	switch kind {
	case shellIndex:
		p, ct = c.file("index.html"), "text/html; charset=utf-8"
	case shellAsset:
		p, ct = c.file("assets", name), mime.TypeByExtension(path.Ext(name))
	case shellStatic:
		p, ct = c.file("static", name), mime.TypeByExtension(path.Ext(name))
	default:
		return nil, "", false
	}
	b, err := os.ReadFile(p) //nolint:gosec // G703: name matched safeName (no separators, no "..") in classify
	if err != nil {
		return nil, "", false
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	if strings.HasSuffix(name, ".webmanifest") {
		ct = "application/manifest+json"
	}
	return b, ct, true
}

// Clear deletes the cache (unpair).
func (c *ShellCache) Clear() error {
	if c == nil || c.Dir == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := os.RemoveAll(c.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func writeAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
