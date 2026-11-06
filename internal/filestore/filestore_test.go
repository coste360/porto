package filestore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/crypto"
)

const (
	marinaA = "01900000-0000-7000-8000-00000000000a"
	fileA   = "01900000-0000-7000-8000-0000000000f1"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ring, err := crypto.ParseKeyRing(k)
	if err != nil {
		t.Fatal(err)
	}
	return New(filepath.Join(t.TempDir(), "files"), ring)
}

func TestStore_PutOpenRemove(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	plain := []byte("%PDF-1.4 hello")
	b, err := s.Put(ctx, marinaA, fileA, bytes.NewReader(plain), MaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	if b.StorageKey != marinaA+"/"+fileA+".bin" || b.Size != int64(len(plain)) || len(b.SHA256) != 64 {
		t.Fatalf("blob = %+v", b)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), marinaA, fileA+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("%PDF")) || bytes.Contains(raw, []byte("hello")) {
		t.Fatal("blob is not encrypted at rest")
	}
	got, err := s.Open(ctx, marinaA, fileA, b.StorageKey)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open = %q, %v", got, err)
	}
	// The blob is bound to its marina and id.
	other := "01900000-0000-7000-8000-00000000000b"
	if err := os.MkdirAll(filepath.Join(s.Dir(), other), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), other, fileA+".bin"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, other, fileA, StorageKey(other, fileA)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("blob moved to another marina: %v", err)
	}
	if _, err := s.Open(ctx, marinaA, fileA, StorageKey(other, fileA)); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("mismatched storage key: %v", err)
	}
	if err := s.Remove(b.StorageKey); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(b.StorageKey); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if _, err := s.Open(ctx, marinaA, fileA, b.StorageKey); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("open removed: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir(), marinaA))
	if len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestStore_PutRejects(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, marinaA, fileA, strings.NewReader("12345"), 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	for _, id := range []string{"../x", "a/b", "", strings.Repeat("a", 65)} {
		if _, err := s.Put(ctx, marinaA, id, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("file id %q: %v", id, err)
		}
	}
	if err := s.Remove("../../etc/passwd"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("remove traversal: %v", err)
	}
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSniff_Scenario(t *testing.T) {
	pngBytes := encodePNG(t, 40, 30)
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"><rect width="10" height="10"/></svg>`)
	cases := []struct {
		name     string
		content  []byte
		declared string
		wantType string
		w, h     int
		err      error
	}{
		{"png", pngBytes, "image/png", TypePNG, 40, 30, nil},
		{"jpeg", jb.Bytes(), "image/jpeg; charset=binary", TypeJPEG, 20, 10, nil},
		{"pdf", []byte("%PDF-1.7\n..."), "application/pdf", TypePDF, 0, 0, nil},
		{"svg", svg, "image/svg+xml", TypeSVG, 800, 600, nil},
		{"png declared as jpeg", pngBytes, "image/jpeg", "", 0, 0, ErrTypeMismatch},
		{"text declared as pdf", []byte("hello"), "application/pdf", "", 0, 0, ErrTypeMismatch},
		{"gif", []byte("GIF89a"), "image/gif", "", 0, 0, ErrUnsupportedType},
		{"svg with script", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "image/svg+xml", "", 0, 0, ErrUnsafeSVG},
		{"svg with handler", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="x()"></svg>`), "image/svg+xml", "", 0, 0, ErrUnsafeSVG},
		{"svg with external image", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><image href="https://x/y.png"/></svg>`), "image/svg+xml", "", 0, 0, ErrUnsafeSVG},
		{"truncated png", pngBytes[:20], "image/png", "", 0, 0, ErrInvalidImage},
		{"oversized png", encodePNG(t, MaxImageSide+1, 1), "image/png", "", 0, 0, ErrInvalidImage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ct, w, h, err := Sniff(c.content, c.declared)
			if !errors.Is(err, c.err) || ct != c.wantType || w != c.w || h != c.h {
				t.Fatalf("Sniff = %q %d×%d %v, want %q %d×%d %v", ct, w, h, err, c.wantType, c.w, c.h, c.err)
			}
		})
	}
}
