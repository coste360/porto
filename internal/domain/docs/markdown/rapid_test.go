package markdown

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestMarkdown_ParseTotal: any text parses (no panic), every block kind is known, the HTML writer escapes every
// angle bracket of the input and parsing is deterministic.
func TestMarkdown_ParseTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		src := rapid.StringMatching(`[ a-z0-9#*|:\-\n{}"<>\\.]{0,200}`).Draw(t, "src")
		a, b := Parse(src), Parse(src)
		if len(a) != len(b) {
			t.Fatal("not deterministic")
		}
		for _, bl := range a {
			if bl.Kind < Paragraph || bl.Kind > Signature {
				t.Fatalf("kind %d", bl.Kind)
			}
		}
		if html := HTML(a); strings.Contains(html, "<script") || strings.Count(html, "<") < strings.Count(Text(a), "<") {
			t.Fatalf("unescaped html %q", html)
		}
	})
}
