package lan

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

// CA download for phones on the local network (m6b-plan §8.5, D18): the local CA certificate (DER) and an
// unsigned iOS configuration profile with the CA only, public on the LAN listener, offered only for a
// name-constrained CA. Installing it lets the phone trust the desktop origin, so its service worker (offline
// start) works.

// CA download paths.
const (
	CACertPath        = "/lan/porto-ca.crt"
	CAMobileConfigPth = "/lan/porto-ca.mobileconfig"
)

// caHandler answers the download paths and passes everything else to next.
func (s *Server) caHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != CACertPath && r.URL.Path != CAMobileConfigPth {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		c := s.current.Load()
		if c == nil || !c.CAConstrained() {
			http.NotFound(w, r)
			return
		}
		var body []byte
		h := w.Header()
		if r.URL.Path == CACertPath {
			body = c.CA.Raw
			h.Set("Content-Type", "application/x-x509-ca-cert")
			h.Set("Content-Disposition", `attachment; filename="porto-ca.crt"`)
		} else {
			body = MobileConfig(c.CA.Raw, c.CAFingerprint())
			h.Set("Content-Type", "application/x-apple-aspen-config")
			h.Set("Content-Disposition", `attachment; filename="porto-ca.mobileconfig"`)
		}
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}

// uuidOf formats the first 16 bytes of a hex fingerprint as a UUID (stable identifiers of the profile).
func uuidOf(fpHex string, variant byte) string {
	b := []byte(fpHex[:32])
	b[0] = "0123456789abcdef"[variant%16]
	s := strings.ToUpper(string(b))
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// Texts of the profile (Italian, shown by iOS while installing).
const (
	profileCertDescription = "Certificato del porto per la rete locale"                                                    //nolint:misspell // Italian
	profileDescription     = "Installa il certificato della rete locale del porto (solo indirizzi privati e nomi .local)." //nolint:misspell // Italian
)

// MobileConfig returns an unsigned iOS configuration profile that installs the CA (DER) as a root certificate.
// After installing it, iOS requires the user to enable full trust in Settings > General > About > Certificate
// Trust Settings (the help page explains it).
func MobileConfig(caDER []byte, caFPHex string) []byte {
	b64 := base64.StdEncoding.EncodeToString(caDER)
	var lines []string
	for len(b64) > 64 {
		lines = append(lines, b64[:64])
		b64 = b64[64:]
	}
	lines = append(lines, b64)
	id := caFPHex[:16]
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key>
			<string>porto-ca.crt</string>
			<key>PayloadContent</key>
			<data>
			` + strings.Join(lines, "\n\t\t\t") + `
			</data>
			<key>PayloadDescription</key>
			<string>` + profileCertDescription + `</string>
			<key>PayloadDisplayName</key>
			<string>Porto CA locale</string>
			<key>PayloadIdentifier</key>
			<string>it.coste360.porto.lan.ca.` + id + `.cert</string>
			<key>PayloadType</key>
			<string>com.apple.security.root</string>
			<key>PayloadUUID</key>
			<string>` + uuidOf(caFPHex, 1) + `</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDescription</key>
	<string>` + profileDescription + `</string>
	<key>PayloadDisplayName</key>
	<string>Porto - rete locale</string>
	<key>PayloadIdentifier</key>
	<string>it.coste360.porto.lan.ca.` + id + `</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>` + uuidOf(caFPHex, 2) + `</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`)
}
