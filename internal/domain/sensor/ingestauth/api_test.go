package ingestauth

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	docKey  = "po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	docBody = `{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:30:00Z","state":"occupied","distance_cm":142,"battery_pct":87,"rssi":-97}]}`
	docT    = int64(1812789005)
	docSig  = "e5bdc2797062e970aa0d670ef05f915511c015e053da7dabd287de8c6ae88983"
	path    = "/api/ingest/v1/readings"
)

// TestIngestAuth_DocVector checks the normative test vector of m12-plan §4.2 (and docs/sensor-protocol.md).
func TestIngestAuth_DocVector(t *testing.T) {
	if len(docBody) != 149 {
		t.Fatalf("body length %d", len(docBody))
	}
	if got := Canonical(docT, "POST", path, []byte(docBody)); !strings.HasSuffix(got, "\n7cf0c5ecad60883d179adee8544493fd7257aadd2a59255a5e0003ca414212b2") ||
		!strings.HasPrefix(got, "v1\n1812789005\nPOST\n/api/ingest/v1/readings\n") {
		t.Fatalf("canonical %q", got)
	}
	if got := Sign(docKey, docT, "POST", path, []byte(docBody)); got != docSig {
		t.Fatalf("signature %s", got)
	}
	if got := HashKey(docKey); got != "de05e1d6e0072a34bb3ee2ea4b40d73a19416950304d012e13cf100dd340f98c" {
		t.Fatalf("hash %s", got)
	}
	p, err := ParseHeaders(Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: "t=1812789005,v1=" + docSig})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyHMAC(docKey, p, "POST", path, []byte(docBody), time.Unix(docT, 0)); err != nil {
		t.Fatal(err)
	}
	// The vector is also quoted verbatim in docs/sensor-protocol.md (TestSensorProtocolDoc_Vector in package sensor).
}

// sourceOf parses api.go and returns its imports and the selector expressions it uses ("hmac.Equal").
func sourceOf(t *testing.T) (imports, selectors map[string]bool) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "api.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	imports, selectors = map[string]bool{}, map[string]bool{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		imports[path] = true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok {
				selectors[id.Name+"."+se.Sel.Name] = true
			}
		}
		return true
	})
	return imports, selectors
}

func sig(key string, t int64, body string) string {
	return "t=" + strconv.FormatInt(t, 10) + ",v1=" + Sign(key, t, "POST", path, []byte(body))
}

func TestIngestAuth_Table(t *testing.T) {
	now := time.Unix(docT, 0)
	gwKey, gwID := FormatKey(GatewayPrefix, [10]byte{1, 2, 3}, [32]byte{9, 9})
	otherKey, _ := FormatKey(DevicePrefix, [10]byte{7}, [32]byte{7})
	good := sig(docKey, docT, docBody)
	cases := []struct {
		name    string
		h       Headers
		key     string // key passed to VerifyHMAC ("" = unknown)
		method  string
		path    string
		body    string
		now     time.Time
		parse   error
		verify  error
		gateway bool
	}{
		{name: "ok", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey},
		{name: "missing headers", h: Headers{}, parse: ErrUnauthorized},
		{name: "missing signature", h: Headers{PortoKey: "po_sns_abcdefghijklmnop"}, parse: ErrUnauthorized},
		{name: "garbled key id", h: Headers{PortoKey: "po_sns_ABC", PortoSignature: good}, parse: ErrUnauthorized},
		{name: "garbled signature", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: "t=x,v1=zz"}, parse: ErrUnauthorized},
		{name: "no v1", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: "t=1812789005"}, parse: ErrUnauthorized},
		{name: "two t", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: "t=1,t=2,v1=" + docSig}, parse: ErrUnauthorized},
		{name: "three v1", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good + ",v1=" + docSig + ",v1=" + docSig}, parse: ErrUnauthorized},
		{name: "unknown field", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good + ",v0=ab"}, parse: ErrUnauthorized},
		{name: "mixed hmac and bearer", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good, Authorization: "Bearer " + gwKey}, parse: ErrUnauthorized},
		{name: "two v1, second matches", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: "t=1812789005,v1=" + strings.Repeat("0", 64) + ",v1=" + docSig}, key: docKey},
		{name: "wrong key", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: otherKey, verify: ErrUnauthorized},
		{name: "unknown key", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: "", verify: ErrUnauthorized},
		{name: "t + 299 s", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, now: now.Add(299 * time.Second)},
		{name: "t + 300 s", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, now: now.Add(300 * time.Second)},
		{name: "t + 301 s", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, now: now.Add(301 * time.Second), verify: ErrTimestampOutOfRange},
		{name: "t - 299 s", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, now: now.Add(-299 * time.Second)},
		{name: "t - 301 s", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, now: now.Add(-301 * time.Second), verify: ErrTimestampOutOfRange},
		{name: "old t with bad signature is unauthorized", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: otherKey, now: now.Add(time.Hour), verify: ErrUnauthorized},
		{name: "body tampering", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, body: strings.Replace(docBody, "occupied", "free", 1), verify: ErrUnauthorized},
		{name: "path tampering", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, path: "/api/ingest/v1/readings/", verify: ErrUnauthorized},
		{name: "method tampering", h: Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: good}, key: docKey, method: "PUT", verify: ErrUnauthorized},
		{name: "gateway hmac", h: Headers{PortoKey: gwID, PortoSignature: sig(gwKey, docT, docBody)}, key: gwKey, gateway: true},
		{name: "gateway bearer", h: Headers{Authorization: "Bearer " + gwKey}, gateway: true},
		{name: "bearer lowercase scheme", h: Headers{Authorization: "bearer " + gwKey}, gateway: true},
		{name: "device key as bearer", h: Headers{Authorization: "Bearer " + docKey}, parse: ErrUnauthorized},
		{name: "bearer garbled", h: Headers{Authorization: "Bearer po_gw_x"}, parse: ErrUnauthorized},
		{name: "basic auth", h: Headers{Authorization: "Basic " + gwKey}, parse: ErrUnauthorized},
		{name: "bearer plus signature", h: Headers{Authorization: "Bearer " + gwKey, PortoSignature: good}, parse: ErrUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseHeaders(c.h)
			if !errors.Is(err, c.parse) {
				t.Fatalf("parse: got %v want %v", err, c.parse)
			}
			if err != nil {
				return
			}
			if p.Gateway != c.gateway {
				t.Fatalf("gateway %v", p.Gateway)
			}
			if p.Mode == ModeBearer {
				if !VerifyBearer(p.Bearer, HashKey(gwKey)) || VerifyBearer(p.Bearer, HashKey(otherKey)) || VerifyBearer(p.Bearer, "") {
					t.Fatal("bearer comparison")
				}
				return
			}
			method, pth, body, n := "POST", path, docBody, now
			if c.method != "" {
				method = c.method
			}
			if c.path != "" {
				pth = c.path
			}
			if c.body != "" {
				body = c.body
			}
			if !c.now.IsZero() {
				n = c.now
			}
			if err := VerifyHMAC(c.key, p, method, pth, []byte(body), n); !errors.Is(err, c.verify) {
				t.Fatalf("verify: got %v want %v", err, c.verify)
			}
		})
	}
}

// TestIngestAuth_PreviousKeyGrace documents the grace rule: during a rotation the service verifies the request with
// the previous key while now < prev_valid_until, so a request signed with either key passes; after the grace only
// the new key matches.
func TestIngestAuth_PreviousKeyGrace(t *testing.T) {
	newKey, id := FormatKey(DevicePrefix, [10]byte{5}, [32]byte{5})
	prevKey, prevID := FormatKey(DevicePrefix, [10]byte{6}, [32]byte{6})
	if id == prevID {
		t.Fatal("ids collide")
	}
	now := time.Unix(docT, 0)
	p, _ := ParseHeaders(Headers{PortoKey: prevID, PortoSignature: sig(prevKey, docT, docBody)})
	if err := VerifyHMAC(prevKey, p, "POST", path, []byte(docBody), now); err != nil {
		t.Fatalf("previous key in grace: %v", err)
	}
	if err := VerifyHMAC(newKey, p, "POST", path, []byte(docBody), now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("previous signature with the new key: %v", err)
	}
}

func TestIngestAuth_ConstantTime(t *testing.T) {
	_, sel := sourceOf(t)
	for _, s := range []string{"hmac.Equal", "subtle.ConstantTimeCompare"} {
		if !sel[s] {
			t.Errorf("api.go does not use %s", s)
		}
	}
	if sel["bytes.Equal"] {
		t.Error("api.go compares secrets with bytes.Equal")
	}
	p, err := ParseHeaders(Headers{PortoKey: "po_sns_abcdefghijklmnop", PortoSignature: sig(docKey, docT, docBody)})
	if err != nil {
		t.Fatal(err)
	}
	before := hmacCount.Load()
	if err := VerifyHMAC("", p, "POST", path, []byte(docBody), time.Unix(docT, 0)); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if hmacCount.Load()-before != 1 {
		t.Fatal("an unknown key must still compute one HMAC")
	}
	before = hmacCount.Load()
	_ = VerifyHMAC(docKey, p, "POST", path, []byte(docBody), time.Unix(docT, 0))
	if hmacCount.Load()-before != 1 {
		t.Fatal("a known key computes exactly one HMAC")
	}
}

func TestKeyFormat_Table(t *testing.T) {
	key, id := FormatKey(DevicePrefix, [10]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, [32]byte{255})
	if len(key) != 67 || len(id) != len(DevicePrefix)+KidLen || !strings.HasPrefix(key, id+"_") {
		t.Fatalf("device key %q (%d) id %q", key, len(key), id)
	}
	gw, gwID := FormatKey(GatewayPrefix, [10]byte{}, [32]byte{})
	if len(gw) != 66 || !ValidKeyID(gwID) {
		t.Fatalf("gateway key %q (%d)", gw, len(gw))
	}
	cases := []struct {
		key string
		id  string
		ok  bool
	}{
		{docKey, "po_sns_abcdefghijklmnop", true},
		{key, id, true},
		{gw, gwID, true},
		{"", "", false},
		{"po_live_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "", false},
		{"po_sns_ABCDEFGHIJKLMNOP_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "", false},
		{"po_sns_abcdefghijklmno1_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "", false},
		{"po_sns_abcdefghijklmnop-AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "", false},
		{"po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh", "", false},
		{"po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", "", false},
		{"po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdH+8", "", false},
	}
	for _, c := range cases {
		gotID, ok := KeyIDOf(c.key)
		if ok != c.ok || gotID != c.id {
			t.Errorf("KeyIDOf(%q) = %q %v", c.key, gotID, ok)
		}
	}
	for s, want := range map[string]bool{
		"po_sns_abcdefghijklmnop": true, "po_gw_234567abcdefghij": true, "po_sns_abcdefghijklmno": false,
		"po_sns_abcdefghijklmnopq": false, "po_dev_abcdefghijklmnop": false, "po_gw_abcdefghijklmn0p": false,
	} {
		if ValidKeyID(s) != want {
			t.Errorf("ValidKeyID(%q) != %v", s, want)
		}
	}
}

// TestIngestAuth_Purity keeps the package pure: no clock, no randomness, no store (m12-plan §0.3).
func TestIngestAuth_Purity(t *testing.T) {
	imports, sel := sourceOf(t)
	for _, s := range []string{"time.Now", "time.Since", "time.Until"} {
		if sel[s] {
			t.Errorf("api.go uses %s", s)
		}
	}
	for imp := range imports {
		if imp == "crypto/rand" || imp == "math/rand" || imp == "math/rand/v2" || imp == "os" || strings.Contains(imp, ".") {
			t.Errorf("api.go imports %s", imp)
		}
	}
}
