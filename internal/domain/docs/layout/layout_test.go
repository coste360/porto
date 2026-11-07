package layout

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/docs/markdown"
)

func TestLayout_Basics(t *testing.T) {
	src := "# Titolo\n\nUn paragrafo **con grassetto**.\n\n- a\n- b\n\n| A | B |\n|---|---:|\n| x | 1 |\n\n---\n\n{{signature \"customer\"}}\n\n{{pagebreak}}\n\nSeconda pagina."
	l := Lay(Input{
		Body: markdown.Parse(src), Footer: markdown.Parse("Pagina {{page}} di {{pages}}"),
		Labels: Labels{Roles: map[string]string{"customer": "Il cliente"}, Name: "Nome", Date: "Data"},
	})
	if len(l.Pages) != 2 {
		t.Fatalf("pages %d", len(l.Pages))
	}
	txt := l.Text()
	for _, want := range []string{
		"=== page 1 ===", "Titolo", "Un paragrafo con grassetto.", "• a", "A | B", "x | 1",
		"----", "[signature customer]", "Il cliente", "Pagina 1 di 2", "=== page 2 ===", "Seconda pagina.", "Pagina 2 di 2",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("text lacks %q:\n%s", want, txt)
		}
	}
	if len(l.SignatureFields) != 1 || l.SignatureFields[0].Page != 1 || l.SignatureFields[0].Role != "customer" {
		t.Fatalf("signature fields %+v", l.SignatureFields)
	}
	for _, p := range l.Pages {
		for _, b := range p.Boxes {
			if b.X < 0 || b.X+b.W > PageWidth+1 || b.Y < 0 || b.Y+b.H > PageHeight {
				t.Fatalf("box outside the page: %+v", b)
			}
		}
	}
	// Same input, same layout.
	first, second := Lay(Input{Body: markdown.Parse(src)}), Lay(Input{Body: markdown.Parse(src)})
	if first.Text() != second.Text() {
		t.Fatal("not deterministic")
	}
}

func TestLayout_WidowOrphan(t *testing.T) {
	words := strings.Repeat("parola ", 4000)
	l := Lay(Input{Body: markdown.Parse(words)})
	if len(l.Pages) < 2 {
		t.Fatal("expected several pages")
	}
	for i, p := range l.Pages {
		ys := map[int64]bool{}
		for _, b := range p.Boxes {
			ys[b.Y] = true
		}
		if len(ys) < 2 {
			t.Fatalf("page %d has %d line(s)", i+1, len(ys))
		}
	}
}

var rowCode = regexp.MustCompile(`^R[0-9]+$`)

// TestLayout_TableRowsNeverSplit: every table row (its cell frames) lies on a single page and inside the body area.
func TestLayout_TableRowsNeverSplit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := rapid.IntRange(1, 120).Draw(t, "rows")
		var b strings.Builder
		b.WriteString(strings.Repeat("Intro.\n\n", rapid.IntRange(0, 40).Draw(t, "intro")))
		b.WriteString("| Codice | Descrizione | Importo |\n|---|---|---:|\n")
		for i := range rows {
			words := rapid.IntRange(1, 60).Draw(t, fmt.Sprintf("w%d", i))
			fmt.Fprintf(&b, "| R%d | %s | %d |\n", i, strings.Repeat("testo ", words), i)
		}
		l := Lay(Input{Body: markdown.Parse(b.String()), MarginMM: int64(rapid.IntRange(10, 30).Draw(t, "m"))})
		seen := map[string]int{}
		for pi, p := range l.Pages {
			for _, bx := range p.Boxes {
				if bx.Kind == KindCell && bx.Y+bx.H > PageHeight {
					t.Fatalf("cell beyond the page bottom on page %d", pi+1)
				}
				if bx.Kind == KindText && rowCode.MatchString(bx.Text) {
					if prev, ok := seen[bx.Text]; ok && prev != pi {
						t.Fatalf("row %s split across pages %d and %d", bx.Text, prev+1, pi+1)
					}
					seen[bx.Text] = pi
				}
			}
		}
		if len(seen) != rows {
			t.Fatalf("rows laid out %d of %d", len(seen), rows)
		}
	})
}

func TestTextWidth_Monotonic(t *testing.T) {
	if TextWidth("ab", false, false, 100) <= TextWidth("a", false, false, 100) {
		t.Fatal("width not increasing")
	}
	if TextWidth("W", true, false, 100) < TextWidth("W", false, false, 100) {
		t.Fatal("bold narrower than regular")
	}
	if TextWidth("€‰•", false, false, 100) == 0 {
		t.Fatal("symbols have no width")
	}
}
