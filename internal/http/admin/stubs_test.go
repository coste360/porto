package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdmin_NoStubsLeft: no handler in this package answers 501 any more (the CONTRACT stub marker is gone).
// m3_stubs.go holds the M3 CONTRACT stubs while OCCUPANCY-ENGINE and CONTRACTS implement them and is deleted at
// the end of M3 (m3-plan §11.1); it is the only file exempted, and only while it exists.
func TestAdmin_NoStubsLeft(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if f == "m3_stubs.go" {
			t.Logf("%s: M3 stubs still pending", f)
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, "STUB(M1)") || strings.Contains(src, "STUB(M2)") || strings.Contains(src, "STUB(M3)") || strings.Contains(src, "notImplemented") ||
			strings.Contains(src, "StatusNotImplemented") {
			t.Errorf("%s still has stubs", f)
		}
	}
}
