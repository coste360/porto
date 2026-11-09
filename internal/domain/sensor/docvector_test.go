package sensor

import (
	"os"
	"strings"
	"testing"
)

// TestSensorProtocolDoc_Vector keeps docs/sensor-protocol.md aligned with the normative test vector of m12-plan §4.2
// (checked by ingestauth.TestIngestAuth_DocVector).
func TestSensorProtocolDoc_Vector(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/sensor-protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"po_sns_abcdefghijklmnop_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
		"t=1812789005,v1=e5bdc2797062e970aa0d670ef05f915511c015e053da7dabd287de8c6ae88983",
		"7cf0c5ecad60883d179adee8544493fd7257aadd2a59255a5e0003ca414212b2",
		"de05e1d6e0072a34bb3ee2ea4b40d73a19416950304d012e13cf100dd340f98c",
		`{"readings":[{"device_id":"A81758FFFE0312AB","observed_at":"2027-06-12T08:30:00Z","state":"occupied","distance_cm":142,"battery_pct":87,"rssi":-97}]}`,
	} {
		if !strings.Contains(string(doc), s) {
			t.Errorf("docs/sensor-protocol.md misses %q", s)
		}
	}
}
