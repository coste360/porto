//go:build integration

package acceptance

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: RFC 6238 TOTP is defined over HMAC-SHA1
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// authenticatorCode is what a phone authenticator shows for secret at instant at (RFC 6238, 30 s, 6 digits).
func authenticatorCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30)) //nolint:gosec // G115: positive Unix time
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// TestAcceptance_M6aPairTOTPRotatesSession: a user with TOTP pairs a phone by QR; the pending device session is
// unusable, the second factor replaces it with a new one (new cookies) and the pending token stops working.
func TestAcceptance_M6aPairTOTPRotatesSession(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newDesktopHarness(t, db)
		ctx := context.Background()
		o, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "titolare@porto.test", Name: "Titolare", Password: password, Organization: "Porto Srl",
			MarinaName: "Marina", MarinaSlug: "marina-totp",
		})
		if err != nil {
			t.Fatal(err)
		}
		M := A + "/marinas/" + o.MarinaID
		owner := newWebClient(h.loopback, "")
		owner.expect(t, 200, "POST", A+"/auth/login", map[string]string{"email": "titolare@porto.test", "password": password})
		enrol := owner.expect(t, 200, "POST", A+"/auth/totp/enroll", nil)
		secret := str(enrol, "secret")
		confirmed := owner.expect(t, 200, "POST", A+"/auth/totp/confirm", map[string]any{"code": authenticatorCode(t, secret, h.app.Clock.Now())})
		codes, _ := confirmed["codes"].([]any)
		if len(codes) == 0 {
			t.Fatalf("no recovery codes %v", confirmed)
		}

		st, err := h.lan.Enable(ctx, desktopport.LANOptions{})
		if err != nil {
			t.Fatal(err)
		}
		pc := owner.expect(t, 201, "POST", M+"/devices/pairing-codes", map[string]any{"user_id": o.UserID, "kind": "mobile"})
		phone := newWebClient(st.URL, str(pc, "fingerprint"))
		paired := phone.expect(t, 201, "POST", "/api/device/v1/pair-session", map[string]any{
			"code": str(pc, "code"), "name": "Telefono", "platform": "android", "kind": "mobile",
		})
		if paired["totp_required"] != true {
			t.Fatalf("pair-session %v", paired)
		}
		pending := phone.cookie("porto_session")
		if pending == nil {
			t.Fatal("no pending session cookie")
		}
		phone.expect(t, 401, "GET", A+"/auth/me", nil)

		r := phone.do(t, "POST", "/api/device/v1/pair-session/totp", map[string]any{"recovery_code": codes[0]})
		if r.status != http.StatusNoContent {
			t.Fatalf("verify %d %s", r.status, r.body)
		}
		rotated := phone.cookie("porto_session")
		if rotated == nil || rotated.Value == pending.Value {
			t.Fatal("the session cookie was not rotated on TOTP confirmation")
		}
		phone.expect(t, 200, "GET", A+"/auth/me", nil)

		// Whoever kept the pending token gets nothing.
		stale := newWebClient(st.URL, str(pc, "fingerprint"))
		u, _ := url.Parse(st.URL)
		stale.http.Jar.SetCookies(u, []*http.Cookie{{Name: "porto_session", Value: pending.Value, Path: "/"}})
		stale.expect(t, 401, "GET", A+"/auth/me", nil)
		assertNoPII(t, h.logs.String(), pending.Value, rotated.Value, str(pc, "code"))
	})
}
