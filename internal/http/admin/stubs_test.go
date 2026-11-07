package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdmin_NoStubsLeft: no handler in this package answers 501 any more (the CONTRACT stub marker is gone).
// m4_stubs.go holds the M4 CONTRACT stubs while CONTRACTS, RIGHTS-CHARGES and DOCUMENTS implement them and is
// deleted at the end of M4 (m4-plan §13.1); it is the only file exempted, and only while it exists.
func TestAdmin_NoStubsLeft(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if f == "m4_stubs.go" {
			t.Logf("%s: M4 stubs still pending", f)
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, "STUB(M1)") || strings.Contains(src, "STUB(M2)") || strings.Contains(src, "STUB(M3)") || strings.Contains(src, "STUB(M4)") || strings.Contains(src, "notImplemented") ||
			strings.Contains(src, "StatusNotImplemented") {
			t.Errorf("%s still has stubs", f)
		}
	}
}
