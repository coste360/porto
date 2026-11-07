package markdown

import (
	"strconv"
	"strings"
)

// BlockKind is the kind of a block.
type BlockKind int

// Block kinds.
const (
	Paragraph BlockKind = iota
	Heading
	List
	Table
	Rule
	PageBreak
	Logo
	Signature
)

// SpanKind is the kind of an inline span.
type SpanKind int

// Span kinds: text, a hard line break, and the page number / page count placeholders of headers and footers.
const (
	SpanText SpanKind = iota
	SpanBreak
	SpanPage
	SpanPages
)

// Span is a run of inline text with one style.
type Span struct {
	Kind   SpanKind
	Text   string
	Bold   bool
	Italic bool
}

// Inline is a sequence of spans.
type Inline []Span

// Align is a table column alignment.
type Align int

// Alignments.
const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Item is a list item with an optional nested list (one level).
type Item struct {
	Content Inline
	Sub     *Block
}

// Block is a block of the AST.
type Block struct {
	Kind    BlockKind
	Level   int    // Heading 1..3
	Ordered bool   // List
	Start   int    // List: first number of an ordered list
	Role    string // Signature
	Content Inline // Paragraph, Heading
	Items   []Item // List
	Header  []Inline
	Aligns  []Align
	Rows    [][]Inline
}

// Parse parses the Markdown subset (m4-plan §6.3). Anything outside the subset is literal text.
func Parse(src string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, Block{Kind: Paragraph, Content: ParseInline(strings.Join(para, " "))})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flush()
		case line == "{{pagebreak}}":
			flush()
			out = append(out, Block{Kind: PageBreak})
		case line == "{{logo}}":
			flush()
			out = append(out, Block{Kind: Logo})
		case strings.HasPrefix(line, "{{signature ") && strings.HasSuffix(line, "}}"):
			role, err := strconv.Unquote(strings.TrimSpace(line[len("{{signature ") : len(line)-2]))
			if err != nil {
				para = append(para, line)
				continue
			}
			flush()
			out = append(out, Block{Kind: Signature, Role: role})
		case isRule(line):
			flush()
			out = append(out, Block{Kind: Rule})
		case headingLevel(line) > 0:
			flush()
			lv := headingLevel(line)
			out = append(out, Block{Kind: Heading, Level: lv, Content: ParseInline(strings.TrimSpace(line[lv:]))})
		case listMarker(raw) != nil:
			flush()
			b, n := parseList(lines[i:])
			out = append(out, b)
			i += n - 1
		case strings.HasPrefix(line, "|") && i+1 < len(lines) && isAlignRow(strings.TrimSpace(lines[i+1])):
			flush()
			b, n := parseTable(lines[i:])
			out = append(out, b)
			i += n - 1
		default:
			para = append(para, line)
		}
	}
	flush()
	return out
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, c := range line {
		if c != '-' && c != ' ' {
			return false
		}
	}
	return strings.Count(line, "-") >= 3
}

func headingLevel(line string) int {
	for lv := 3; lv >= 1; lv-- {
		if strings.HasPrefix(line, strings.Repeat("#", lv)+" ") {
			return lv
		}
	}
	return 0
}

type marker struct {
	indent  int
	ordered bool
	num     int
	rest    string
}

func listMarker(raw string) *marker {
	trimmed := strings.TrimLeft(raw, " ")
	indent := len(raw) - len(trimmed)
	if indent > 6 {
		return nil
	}
	for _, b := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, b) {
			return &marker{indent: indent, rest: strings.TrimSpace(trimmed[2:])}
		}
	}
	j := 0
	for j < len(trimmed) && j < 4 && trimmed[j] >= '0' && trimmed[j] <= '9' {
		j++
	}
	if j > 0 && strings.HasPrefix(trimmed[j:], ". ") {
		n, _ := strconv.Atoi(trimmed[:j])
		return &marker{indent: indent, ordered: true, num: n, rest: strings.TrimSpace(trimmed[j+2:])}
	}
	return nil
}

// parseList reads a list and its one level of nesting; it returns the block and the lines used.
func parseList(lines []string) (Block, int) {
	first := listMarker(lines[0])
	b := Block{Kind: List, Ordered: first.ordered, Start: max(first.num, 1)}
	base := first.indent
	i := 0
	for ; i < len(lines); i++ {
		m := listMarker(lines[i])
		if m == nil {
			line := strings.TrimSpace(lines[i])
			if line == "" || len(b.Items) == 0 || isBlockStart(lines, i) {
				break
			}
			// Continuation line of the last item (or of its last sub-item).
			last := &b.Items[len(b.Items)-1]
			if last.Sub != nil {
				si := &last.Sub.Items[len(last.Sub.Items)-1]
				si.Content = append(si.Content, Span{Text: " "})
				si.Content = append(si.Content, ParseInline(line)...)
			} else {
				last.Content = append(last.Content, Span{Text: " "})
				last.Content = append(last.Content, ParseInline(line)...)
			}
			continue
		}
		if m.indent >= base+2 && len(b.Items) > 0 {
			last := &b.Items[len(b.Items)-1]
			if last.Sub == nil {
				last.Sub = &Block{Kind: List, Ordered: m.ordered, Start: max(m.num, 1)}
			}
			last.Sub.Items = append(last.Sub.Items, Item{Content: ParseInline(m.rest)})
			continue
		}
		if m.ordered != b.Ordered {
			break
		}
		b.Items = append(b.Items, Item{Content: ParseInline(m.rest)})
	}
	return b, i
}

func isBlockStart(lines []string, i int) bool {
	line := strings.TrimSpace(lines[i])
	return isRule(line) || headingLevel(line) > 0 || strings.HasPrefix(line, "{{") ||
		(strings.HasPrefix(line, "|") && i+1 < len(lines) && isAlignRow(strings.TrimSpace(lines[i+1])))
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			cur.WriteByte(c)
			cur.WriteByte(line[i+1])
			i++
			continue
		}
		if c == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func isAlignRow(line string) bool {
	if !strings.HasPrefix(line, "|") {
		return false
	}
	cells := splitRow(line)
	for _, c := range cells {
		c = strings.TrimSpace(c)
		core := strings.TrimSuffix(strings.TrimPrefix(c, ":"), ":")
		if len(core) < 3 || strings.Trim(core, "-") != "" {
			return false
		}
	}
	return len(cells) > 0
}

func parseTable(lines []string) (Block, int) {
	header := splitRow(lines[0])
	b := Block{Kind: Table}
	for _, h := range header {
		b.Header = append(b.Header, ParseInline(h))
	}
	for _, a := range splitRow(lines[1]) {
		a = strings.TrimSpace(a)
		switch {
		case strings.HasPrefix(a, ":") && strings.HasSuffix(a, ":"):
			b.Aligns = append(b.Aligns, AlignCenter)
		case strings.HasSuffix(a, ":"):
			b.Aligns = append(b.Aligns, AlignRight)
		default:
			b.Aligns = append(b.Aligns, AlignLeft)
		}
	}
	for len(b.Aligns) < len(header) {
		b.Aligns = append(b.Aligns, AlignLeft)
	}
	b.Aligns = b.Aligns[:len(header)]
	i := 2
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := splitRow(line)
		row := make([]Inline, len(header))
		for j := range row {
			if j < len(cells) {
				row[j] = ParseInline(cells[j])
			}
		}
		b.Rows = append(b.Rows, row)
	}
	return b, i
}

// ParseInline parses emphasis (**bold**, *italic*), backslash escapes and the inline markers {{br}}, {{page}} and
// {{pages}}. Escaped characters are always literal.
func ParseInline(s string) Inline {
	var out Inline
	var cur strings.Builder
	bold, italic := false, false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, Span{Text: cur.String(), Bold: bold, Italic: italic})
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			cur.WriteByte(s[i+1])
			i++
		case strings.HasPrefix(s[i:], "{{br}}"):
			flush()
			out = append(out, Span{Kind: SpanBreak})
			i += len("{{br}}") - 1
		case strings.HasPrefix(s[i:], "{{pages}}"):
			flush()
			out = append(out, Span{Kind: SpanPages, Bold: bold, Italic: italic})
			i += len("{{pages}}") - 1
		case strings.HasPrefix(s[i:], "{{page}}"):
			flush()
			out = append(out, Span{Kind: SpanPage, Bold: bold, Italic: italic})
			i += len("{{page}}") - 1
		case strings.HasPrefix(s[i:], "**"):
			flush()
			bold = !bold
			i++
		case c == '*':
			flush()
			italic = !italic
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func isASCIIPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

// PlainText returns the text of an inline (page placeholders as given).
func (in Inline) PlainText(page, pages string) string {
	var b strings.Builder
	for _, s := range in {
		switch s.Kind {
		case SpanBreak:
			b.WriteByte('\n')
		case SpanPage:
			b.WriteString(page)
		case SpanPages:
			b.WriteString(pages)
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
