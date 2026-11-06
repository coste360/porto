package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func mustKey(t testing.TB) string {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustRing(t testing.TB, keys ...string) *KeyRing {
	t.Helper()
	r, err := ParseKeyRing(strings.Join(keys, ","))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseKeyRing(t *testing.T) {
	raw := bytes.Repeat([]byte{0xfb}, 32) // encodes with '+'/'/' in std and '-'/'_' in url alphabets
	encodings := map[string]string{
		"std":     base64.StdEncoding.EncodeToString(raw),
		"raw std": base64.RawStdEncoding.EncodeToString(raw),
		"url":     base64.URLEncoding.EncodeToString(raw),
		"raw url": base64.RawURLEncoding.EncodeToString(raw),
	}
	var ids []string
	for name, enc := range encodings {
		r, err := ParseKeyRing(enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ids = append(ids, r.ActiveKeyID())
	}
	for _, id := range ids {
		if id != ids[0] || len(id) != 8 {
			t.Fatalf("key ids differ across encodings: %v", ids)
		}
	}

	k1, k2 := mustKey(t), mustKey(t)
	if r, err := ParseKeyRing(" " + k1 + " , " + k2 + ","); err != nil || len(r.keys) != 2 {
		t.Fatalf("two keys with spaces: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrNoKeys},
		{"only commas", " , ,", ErrNoKeys},
		{"not base64", "not-base64!!", ErrInvalidKey},
		{"too short", base64.StdEncoding.EncodeToString(make([]byte, 16)), ErrInvalidKey},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 33)), ErrInvalidKey},
		{"duplicate", k1 + "," + k1, ErrInvalidKey},
	}
	for _, tt := range tests {
		if _, err := ParseKeyRing(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestKeyRing_EncryptDecrypt(t *testing.T) {
	r := mustRing(t, mustKey(t))
	tests := []struct {
		name      string
		plaintext []byte
		aad       []byte
	}{
		{"empty", nil, nil},
		{"text", []byte("whsec_example"), []byte("webhook_endpoint:1:secret")},
		{"binary", []byte{0, 1, 2, 255}, []byte{0}},
		{"large", bytes.Repeat([]byte("x"), 1<<16), nil},
	}
	for _, tt := range tests {
		c, err := r.Encrypt(tt.plaintext, tt.aad)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(c, "v1."+r.ActiveKeyID()+".") {
			t.Fatalf("%s: format %q", tt.name, c)
		}
		got, err := r.Decrypt(c, tt.aad)
		if err != nil || !bytes.Equal(got, tt.plaintext) {
			t.Fatalf("%s: Decrypt = %q, %v", tt.name, got, err)
		}
	}
	c1, _ := r.EncryptString("same", "aad")
	c2, _ := r.EncryptString("same", "aad")
	if c1 == c2 {
		t.Fatal("nonce reuse: identical ciphertexts")
	}
	if s, err := r.DecryptString(c1, "aad"); err != nil || s != "same" {
		t.Fatalf("DecryptString = %q, %v", s, err)
	}
}

func TestDecrypt_WrongAAD(t *testing.T) {
	r := mustRing(t, mustKey(t))
	c, _ := r.EncryptString("secret", "user:1")
	if _, err := r.DecryptString(c, "user:2"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong aad err = %v", err)
	}
	if _, err := r.DecryptString(c, ""); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("missing aad err = %v", err)
	}
}

func TestDecrypt_Malformed(t *testing.T) {
	r := mustRing(t, mustKey(t))
	c, _ := r.EncryptString("secret", "")
	parts := strings.Split(c, ".")
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrMalformed},
		{"no dots", "abc", ErrMalformed},
		{"wrong version", "v2." + parts[1] + "." + parts[2], ErrMalformed},
		{"short key id", "v1.abc." + parts[2], ErrMalformed},
		{"non hex key id", "v1.zzzzzzzz." + parts[2], ErrMalformed},
		{"bad base64", "v1." + parts[1] + ".***", ErrMalformed},
		{"truncated payload", "v1." + parts[1] + ".AAAA", ErrMalformed},
		{"extra part", c + ".x", ErrMalformed},
		{"unknown key", "v1.00000000." + parts[2], ErrUnknownKey},
	}
	for _, tt := range tests {
		if _, err := r.Decrypt(tt.in, nil); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	// Tampering with one byte of the sealed payload fails authentication.
	payload, _ := base64.RawURLEncoding.DecodeString(parts[2])
	payload[len(payload)-1] ^= 1
	tampered := "v1." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(payload)
	if _, err := r.Decrypt(tampered, nil); !errors.Is(err, ErrDecrypt) {
		t.Errorf("tampered err = %v", err)
	}
}

func TestKeyRing_Rotation(t *testing.T) {
	oldKey, newKey := mustKey(t), mustKey(t)
	before := mustRing(t, oldKey)
	c, _ := before.EncryptString("secret", "aad")
	if before.NeedsRotation(c) {
		t.Fatal("fresh ciphertext needs rotation")
	}

	after := mustRing(t, newKey, oldKey)
	if !after.NeedsRotation(c) {
		t.Fatal("old ciphertext should need rotation")
	}
	if s, err := after.DecryptString(c, "aad"); err != nil || s != "secret" {
		t.Fatalf("old key still decrypts: %q, %v", s, err)
	}
	rotated, _ := after.EncryptString("secret", "aad")
	if after.NeedsRotation(rotated) {
		t.Fatal("re-encrypted ciphertext needs rotation")
	}
	if after.NeedsRotation("garbage") {
		t.Fatal("malformed input should not need rotation")
	}

	dropped := mustRing(t, newKey)
	if _, err := dropped.DecryptString(c, "aad"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("dropped key err = %v", err)
	}
}

func TestDerive_Deterministic(t *testing.T) {
	k := mustKey(t)
	a, b := mustRing(t, k), mustRing(t, k, mustKey(t))
	d1, err := a.Derive("session-csrf", 32)
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := b.Derive("session-csrf", 32)
	if !bytes.Equal(d1, d2) {
		t.Fatal("same active key and purpose must derive the same bytes")
	}
	d3, _ := a.Derive("webhook", 32)
	if bytes.Equal(d1, d3) {
		t.Fatal("different purposes derive the same bytes")
	}
	other, _ := mustRing(t, mustKey(t)).Derive("session-csrf", 32)
	if bytes.Equal(d1, other) {
		t.Fatal("different keys derive the same bytes")
	}
	if d, _ := a.Derive("session-csrf", 16); !bytes.Equal(d, d1[:16]) {
		t.Fatal("HKDF output prefix property")
	}
	for _, n := range []int{0, -1, 255*32 + 1} {
		if _, err := a.Derive("x", n); err == nil {
			t.Errorf("Derive(n=%d) accepted", n)
		}
	}
	if _, err := a.Derive("", 32); err == nil {
		t.Error("empty purpose accepted")
	}
}

func TestRandomToken(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok, err := RandomToken(32)
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 43 || strings.ContainsAny(tok, "+/=") || seen[tok] {
			t.Fatalf("bad token %q", tok)
		}
		seen[tok] = true
	}
	if _, err := RandomToken(0); err == nil {
		t.Fatal("RandomToken(0) accepted")
	}
}

func TestRandomBase32(t *testing.T) {
	counts := map[rune]int{}
	for range 200 {
		s, err := RandomBase32(10)
		if err != nil || len(s) != 10 {
			t.Fatalf("RandomBase32 = %q, %v", s, err)
		}
		for _, r := range s {
			if !strings.ContainsRune(CrockfordAlphabet, r) {
				t.Fatalf("char %q outside alphabet", r)
			}
			counts[r]++
		}
	}
	if len(counts) < 30 { // 2000 draws over 32 symbols: all should appear
		t.Fatalf("poor coverage: %d symbols", len(counts))
	}
	if _, err := RandomBase32(0); err == nil {
		t.Fatal("RandomBase32(0) accepted")
	}
}

func TestSHA256Hex(t *testing.T) {
	if got := SHA256Hex("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("SHA256Hex = %s", got)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"secret", "secret", true},
		{"secret", "secreT", false},
		{"secret", "secret1", false},
		{"", "x", false},
	}
	for _, tt := range tests {
		if got := ConstantTimeEqual(tt.a, tt.b); got != tt.want {
			t.Errorf("ConstantTimeEqual(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

func TestRapid_EncryptDecryptRoundTrip(t *testing.T) {
	r := mustRing(t, mustKey(t), mustKey(t))
	rapid.Check(t, func(t *rapid.T) {
		plain := rapid.SliceOf(rapid.Byte()).Draw(t, "plain")
		aad := rapid.SliceOf(rapid.Byte()).Draw(t, "aad")
		c, err := r.Encrypt(plain, aad)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Decrypt(c, aad)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("round trip failed: %v", err)
		}
		if len(aad) > 0 {
			if _, err := r.Decrypt(c, aad[1:]); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("modified aad accepted: %v", err)
			}
		}
	})
}
