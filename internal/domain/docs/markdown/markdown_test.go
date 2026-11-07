package markdown

import (
	"strings"
	"testing"
)

func TestMarkdown_Scenario(t *testing.T) {
	src := strings.Join([]string{
		"# Contratto",
		"## Parti",
		"### Note",
		"Testo **grassetto** e *corsivo* su",
		"due righe.",
		"",
		"- uno",
		"  - uno.a",
		"- due",
		"",
		"3. tre",
		"4. quattro",
		"",
		"| A | B | C |",
		"|:---|:---:|---:|",
		"| 1 | 2 \\| x | 3 |",
		"",
		"---",
		"{{pagebreak}}",
		"{{logo}}",
		`{{signature "customer"}}`,
		`\# non titolo`,
		"riga{{br}}a capo {{page}}/{{pages}}",
		"#senza spazio",
	}, "\n")
	bs := Parse(src)
	kinds := []BlockKind{Heading, Heading, Heading, Paragraph, List, List, Table, Rule, PageBreak, Logo, Signature, Paragraph}
	if len(bs) != len(kinds) {
		t.Fatalf("blocks %d, want %d: %+v", len(bs), len(kinds), bs)
	}
	for i, k := range kinds {
		if bs[i].Kind != k {
			t.Fatalf("block %d kind %v want %v", i, bs[i].Kind, k)
		}
	}
	if bs[0].Level != 1 || bs[1].Level != 2 || bs[2].Level != 3 {
		t.Fatal("heading levels")
	}
	p := bs[3].Content
	if len(p) != 5 || !p[1].Bold || !p[3].Italic || p[4].Text != " su due righe." {
		t.Fatalf("inline: %+v", p)
	}
	if len(bs[4].Items) != 2 || bs[4].Items[0].Sub == nil || bs[4].Items[0].Sub.Items[0].Content.PlainText("", "") != "uno.a" {
		t.Fatalf("list: %+v", bs[4])
	}
	if !bs[5].Ordered || bs[5].Start != 3 || len(bs[5].Items) != 2 {
		t.Fatalf("ordered: %+v", bs[5])
	}
	tb := bs[6]
	if len(tb.Header) != 3 || len(tb.Rows) != 1 || tb.Aligns[0] != AlignLeft || tb.Aligns[1] != AlignCenter ||
		tb.Aligns[2] != AlignRight || tb.Rows[0][1].PlainText("", "") != "2 | x" {
		t.Fatalf("table: %+v", tb)
	}
	if bs[10].Role != "customer" {
		t.Fatal("signature role")
	}
	last := bs[11].Content.PlainText("7", "9")
	if last != "# non titolo riga\na capo 7/9 #senza spazio" {
		t.Fatalf("last paragraph %q", last)
	}

	html := HTML(bs)
	for _, want := range []string{
		"<h1>Contratto</h1>", "<strong>grassetto</strong>", "<em>corsivo</em>", "<ol>",
		`<td style="text-align:center">2 | x</td>`, "<hr>", "riga<br>a capo",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("html lacks %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "pagebreak") || strings.Contains(html, "signature") {
		t.Fatal("html carries print-only blocks")
	}
	esc := HTML(Parse(`<script>alert(1)</script> & "x"`))
	if strings.Contains(esc, "<script>") || !strings.Contains(esc, "&lt;script&gt;") {
		t.Fatalf("html not escaped: %s", esc)
	}
	txt := Text(bs)
	if !strings.Contains(txt, "- uno\n  - uno.a\n- due") || !strings.Contains(txt, "3. tre") {
		t.Fatalf("text:\n%s", txt)
	}
}
