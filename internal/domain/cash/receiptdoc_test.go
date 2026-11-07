package cash

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/docs"
)

// TestReceiptBuiltin_Renders: the built-in A4 receipt templates (it, en) render the receipt view and carry the
// "Non valido ai fini fiscali" notice.
func TestReceiptBuiltin_Renders(t *testing.T) {
	for _, loc := range []string{"it", "en"} {
		src, _, ok := docs.Builtin(docs.KindReceipt, loc)
		if !ok || !strings.Contains(src.Body, "Non valido ai fini fiscali") {
			t.Fatalf("%s: builtin missing", loc)
		}
		if info, ok := docs.KindInfoOf(docs.KindReceipt); !ok || info.View == nil {
			t.Fatal("kind receipt has no view in the catalogue")
		}
		r, err := docs.RenderBuiltin(docs.KindReceipt, sampleReceipt(), loc, time.Date(2027, 7, 3, 10, 0, 0, 0, time.UTC), "2027-07-03")
		if err != nil {
			t.Fatalf("%s: %v", loc, err)
		}
		if r == nil || len(r.PDF) == 0 {
			t.Fatalf("%s: empty", loc)
		}
	}
}
