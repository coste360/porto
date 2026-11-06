package reqctx

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

func TestWithFrom(t *testing.T) {
	info := &Info{RequestID: "req-1", ClientIP: netip.MustParseAddr("192.0.2.1"), AuthMethod: AuthSession}
	ctx := With(context.Background(), info)
	if got := From(ctx); got != info {
		t.Fatalf("From = %+v, want the stored pointer", got)
	}
	// Later middleware mutates the shared Info.
	From(ctx).AuthMethod = AuthAPIKey
	if info.AuthMethod != AuthAPIKey {
		t.Fatal("mutation not visible")
	}
}

func TestFrom_NeverNil(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"nil info stored", With(context.Background(), nil)},
		{"nil ctx", nil},
	}
	for _, tt := range tests {
		got := From(tt.ctx)
		if got == nil {
			t.Fatalf("%s: nil", tt.name)
		}
		if got.AuthMethod != AuthNone || got.RequestID != "" || got.ClientIP.IsValid() {
			t.Fatalf("%s: not zero: %+v", tt.name, got)
		}
		got.RequestID = "x"
		if From(tt.ctx).RequestID != "" {
			t.Fatalf("%s: zero Info is shared", tt.name)
		}
	}
}

func TestTruncateUserAgent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int // expected byte length
	}{
		{"short", "curl/8.0", 8},
		{"exact", strings.Repeat("a", 256), 256},
		{"long", strings.Repeat("a", 300), 256},
		{"multibyte boundary", strings.Repeat("a", 255) + "è", 255},
	}
	for _, tt := range tests {
		got := TruncateUserAgent(tt.in)
		if len(got) != tt.want || !utf8.ValidString(got) {
			t.Errorf("%s: len %d valid %v, want %d", tt.name, len(got), utf8.ValidString(got), tt.want)
		}
	}
}

func TestRapid_TruncateUserAgent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		got := TruncateUserAgent(s)
		if len(got) > MaxUserAgent || !utf8.ValidString(got) {
			t.Fatalf("len %d valid %v", len(got), utf8.ValidString(got))
		}
		if utf8.ValidString(s) && !strings.HasPrefix(s, got) {
			t.Fatal("not a prefix of a valid input")
		}
	})
}
