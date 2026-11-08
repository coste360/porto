package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdmin_NoStubsLeft: no handler in this package answers 501 any more (the CONTRACT stub marker is gone).
func TestAdmin_NoStubsLeft(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, "STUB(M1)") || strings.Contains(src, "STUB(M2)") || strings.Contains(src, "STUB(M3)") || strings.Contains(src, "STUB(M4)") || strings.Contains(src, "STUB(M5)") || strings.Contains(src, "STUB(M6") || strings.Contains(src, "STUB(M8)") || strings.Contains(src, "STUB(M9)") || strings.Contains(src, "notImplemented") ||
			strings.Contains(src, "StatusNotImplemented") {
			t.Errorf("%s still has stubs", f)
		}
	}
}
