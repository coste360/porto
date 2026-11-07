package markdown

import (
	"html"
	"strconv"
	"strings"
)

// HTML writes blocks as escaped HTML for email bodies. Page breaks, logos and signature fields have no meaning in
// an email and are left out.
func HTML(blocks []Block) string {
	var b strings.Builder
	for _, bl := range blocks {
		switch bl.Kind {
		case Paragraph:
			b.WriteString("<p>" + inlineHTML(bl.Content) + "</p>\n")
		case Heading:
			lv := strconv.Itoa(bl.Level)
			b.WriteString("<h" + lv + ">" + inlineHTML(bl.Content) + "</h" + lv + ">\n")
		case List:
			listHTML(&b, bl)
		case Table:
			b.WriteString("<table>\n<thead><tr>")
			for j, h := range bl.Header {
				b.WriteString("<th" + alignAttr(bl.Aligns[j]) + ">" + inlineHTML(h) + "</th>")
			}
			b.WriteString("</tr></thead>\n<tbody>\n")
			for _, row := range bl.Rows {
				b.WriteString("<tr>")
				for j, c := range row {
					b.WriteString("<td" + alignAttr(bl.Aligns[j]) + ">" + inlineHTML(c) + "</td>")
				}
				b.WriteString("</tr>\n")
			}
			b.WriteString("</tbody>\n</table>\n")
		case Rule:
			b.WriteString("<hr>\n")
		}
	}
	return b.String()
}

func alignAttr(a Align) string {
	switch a {
	case AlignCenter:
		return ` style="text-align:center"`
	case AlignRight:
		return ` style="text-align:right"`
	}
	return ""
}

func listHTML(b *strings.Builder, bl Block) {
	tag := "ul"
	if bl.Ordered {
		tag = "ol"
	}
	b.WriteString("<" + tag + ">\n")
	for _, it := range bl.Items {
		b.WriteString("<li>" + inlineHTML(it.Content))
		if it.Sub != nil {
			b.WriteString("\n")
			listHTML(b, *it.Sub)
		}
		b.WriteString("</li>\n")
	}
	b.WriteString("</" + tag + ">\n")
}

func inlineHTML(in Inline) string {
	var b strings.Builder
	for _, s := range in {
		if s.Kind == SpanBreak {
			b.WriteString("<br>")
			continue
		}
		t := html.EscapeString(s.PlainOrEmpty())
		if s.Italic {
			t = "<em>" + t + "</em>"
		}
		if s.Bold {
			t = "<strong>" + t + "</strong>"
		}
		b.WriteString(t)
	}
	return b.String()
}

// PlainOrEmpty is the text of a text span ("" for markers).
func (s Span) PlainOrEmpty() string {
	if s.Kind == SpanText {
		return s.Text
	}
	return ""
}

// Text writes blocks as plain text (the text part of emails).
func Text(blocks []Block) string {
	var b strings.Builder
	for _, bl := range blocks {
		switch bl.Kind {
		case Paragraph, Heading:
			b.WriteString(bl.Content.PlainText("", "") + "\n\n")
		case List:
			for i, it := range bl.Items {
				if bl.Ordered {
					b.WriteString(strconv.Itoa(bl.Start+i) + ". ")
				} else {
					b.WriteString("- ")
				}
				b.WriteString(it.Content.PlainText("", "") + "\n")
				if it.Sub != nil {
					for _, si := range it.Sub.Items {
						b.WriteString("  - " + si.Content.PlainText("", "") + "\n")
					}
				}
			}
			b.WriteString("\n")
		case Table:
			cells := make([]string, len(bl.Header))
			for j, h := range bl.Header {
				cells[j] = h.PlainText("", "")
			}
			b.WriteString(strings.Join(cells, " | ") + "\n")
			for _, row := range bl.Rows {
				for j, c := range row {
					cells[j] = c.PlainText("", "")
				}
				b.WriteString(strings.Join(cells, " | ") + "\n")
			}
			b.WriteString("\n")
		case Rule:
			b.WriteString("----\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
