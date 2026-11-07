package tmpl

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestTemplate_EscapeRoundTrip: an escaped value has no unescaped Markdown control character and no newline, and
// formatting a number is the same for equal inputs (pure, deterministic).
func TestTemplate_EscapeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.String().Draw(t, "v")
		notes := rapid.Bool().Draw(t, "notes")
		e := Escape(v, notes)
		if strings.ContainsAny(e, "\n\r") {
			t.Fatalf("newline survived in %q", e)
		}
		for i := 0; i < len(e); i++ {
			c := e[i]
			if c == '\\' {
				i++
				continue
			}
			if c < 128 && strings.ContainsRune("*_#|[]()`>{}-+!.", rune(c)) {
				if !notes || !strings.HasPrefix(e[i:], "{{br}}") {
					t.Fatalf("unescaped %q in %q", c, e)
				}
				i += len("{{br}}") - 1
			}
		}
		n := rapid.Int64().Draw(t, "n")
		first, second := formatMoney(n, "it"), formatMoney(n, "it")
		if first != second || fixed(n, 2, "en", false) == "" {
			t.Fatal("formatting not deterministic")
		}
	})
}
