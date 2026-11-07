package magiclink

import (
	"testing"

	"github.com/coste360/porto/internal/crypto"
)

func TestMagicLink_TokenHashing(t *testing.T) {
	tok, err := crypto.RandomBase32(TokenChars)
	if err != nil {
		t.Fatal(err)
	}
	h := HashToken(tok)
	if h == tok || len(h) != 64 || h != crypto.SHA256Hex(tok) || HashToken(tok+"X") == h {
		t.Fatalf("hash %q", h)
	}
	s := NewService(Deps{BaseURL: "https://porto.test/"})
	if got := s.URL("marina", PurposeSignature, tok); got != "https://porto.test/b/marina/m/signature/"+tok {
		t.Fatalf("url %s", got)
	}
	if validPurpose("other") || !validPurpose(PurposeUnsubscribe) {
		t.Fatal("purposes")
	}
}
