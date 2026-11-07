package signature

import (
	"regexp"
	"testing"

	"github.com/coste360/porto/internal/crypto"
)

func TestOTP_CodeHashing(t *testing.T) {
	k1, _ := crypto.GenerateKey()
	k2, _ := crypto.GenerateKey()
	r1, _ := crypto.ParseKeyRing(k1)
	r2, _ := crypto.ParseKeyRing(k2)
	a, b := NewService(Deps{Keys: r1}), NewService(Deps{Keys: r2})
	h := a.otpHMAC("signer-1", "123456")
	if h == "123456" || len(h) != 64 {
		t.Fatalf("hash %q", h)
	}
	if h != a.otpHMAC("signer-1", "123456") {
		t.Fatal("not deterministic")
	}
	if h == a.otpHMAC("signer-2", "123456") || h == a.otpHMAC("signer-1", "123457") {
		t.Fatal("hash ignores signer or code")
	}
	if h == b.otpHMAC("signer-1", "123456") {
		t.Fatal("hash does not depend on the key ring")
	}
	six := regexp.MustCompile(`^[0-9]{6}$`)
	seen := map[string]bool{}
	for range 200 {
		c, err := randomCode()
		if err != nil || !six.MatchString(c) {
			t.Fatalf("code %q %v", c, err)
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("codes repeat too often: %d distinct of 200", len(seen))
	}
}
