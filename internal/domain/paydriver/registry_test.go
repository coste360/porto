package paydriver

import (
	"slices"
	"testing"

	"github.com/coste360/porto/internal/domain/paydriver/manual"
)

func TestRegistry_ManualAlwaysPresent(t *testing.T) {
	r := NewRegistry(nil)
	if _, ok := r.Get(manual.Code); !ok || !slices.Equal(r.Codes(), []string{"manual"}) {
		t.Fatalf("codes = %v", r.Codes())
	}
}
