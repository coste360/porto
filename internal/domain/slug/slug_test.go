package slug

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := []string{"demo", "marina-di-porto", "abc", "a1b2", strings.Repeat("a", 63)}
	bad := []string{"", "ab", "Demo", "-demo", "demo-", "de--mo", "de_mo", "dé", "admin", "api", "webhooks", strings.Repeat("a", 64), "a b"}
	for _, s := range ok {
		if err := Validate(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := Validate(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}
