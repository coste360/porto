package layout

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/coste360/porto/internal/domain/docs/markdown"
)

// A4 portrait in 1/100 mm.
const (
	PageWidth  int64 = 21000
	PageHeight int64 = 29700
)

// Box kinds.
const (
	KindText      = "text"
	KindRule      = "rule"
	KindCell      = "cell"      // table cell border
	KindImage     = "image"     // the marina logo
	KindSignature = "signature" // signature field frame
	KindPolygon   = "polygon"   // a closed outline (map print: berths and vessel silhouettes)
)

// Align of a text box inside its line (informative: X already accounts for it).
const (
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

// Style is the text style of a box; Size is in tenths of a point.
type Style struct {
	Bold   bool  `json:"bold,omitempty"`
	Italic bool  `json:"italic,omitempty"`
	Size   int64 `json:"size"`
}

// Box is a positioned element (integer 1/100 mm from the top-left corner of the page; Y of a text box is the top
// of its line).
type Box struct {
	X     int64  `json:"x"`
	Y     int64  `json:"y"`
	W     int64  `json:"w"`
	H     int64  `json:"h"`
	Kind  string `json:"kind"`
	Text  string `json:"text,omitempty"`
	Style Style  `json:"style"`
	// Points of a polygon (1/100 mm) and its grey fill (0 = none, 1..255 = grey level, 255 white).
	Points [][2]int64 `json:"points,omitempty"`
	Fill   int        `json:"fill,omitempty"`
}

// Page is one laid-out page.
type Page struct {
	Boxes []Box `json:"boxes"`
}

// SignatureField is the frame of a {{signature "role"}} block (page is 1-based).
type SignatureField struct {
	Role string `json:"role"`
	Page int    `json:"page"`
	X    int64  `json:"x"`
	Y    int64  `json:"y"`
	W    int64  `json:"w"`
	H    int64  `json:"h"`
}

// Layout is the result of a layout.
type Layout struct {
	Pages           []Page           `json:"pages"`
	SignatureFields []SignatureField `json:"signature_fields"`
}

// ImageSize is the pixel size of the logo (the aspect ratio is kept).
type ImageSize struct{ W, H int64 }

// Labels are the localised texts of signature frames.
type Labels struct {
	Roles map[string]string // role → label ("Il cliente")
	Name  string            // "Nome e cognome"
	Date  string            // "Data"
}

// Input is what to lay out.
type Input struct {
	Body, Header, Footer []markdown.Block
	MarginMM             int64 // 5..50, default 20
	FontSizePt           int64 // 7..16, default 10
	Logo                 *ImageSize
	Labels               Labels
}

// Signature frame and logo sizes.
const (
	sigW     int64 = 8000
	sigH     int64 = 3000
	logoMaxH int64 = 2000
	logoMaxW int64 = 6000
	cellPad  int64 = 150
	listInd  int64 = 600
)

// unit is a group of lines paginated together: lines may be split between pages with at least minKeep lines on
// each side, except when atomic (table rows, signature frames, images).
type unit struct {
	lines        []line
	atomic       bool
	keepWithNext bool
	pageBreak    bool
	gapAfter     int64
	tableHeader  *line // repeated on a new page for tables
	signature    string
}

// line is a row of boxes relative to its top (y = 0) with its height.
type line struct {
	h     int64
	boxes []Box
}

type engine struct {
	in      Input
	base    int64 // body font size, tenths of a point
	x0, w   int64
	lineGap int64
}

func lineHeight(size int64) int64 { return size * 254 * 135 / (72 * 100) }

// Lay lays out the input on A4 pages.
func Lay(in Input) Layout {
	if in.MarginMM < 5 || in.MarginMM > 50 {
		in.MarginMM = 20
	}
	if in.FontSizePt < 7 || in.FontSizePt > 16 {
		in.FontSizePt = 10
	}
	e := &engine{in: in, base: in.FontSizePt * 10}
	e.x0 = in.MarginMM * 100
	e.w = PageWidth - 2*e.x0
	e.lineGap = lineHeight(e.base) / 2

	small := e.base * 8 / 10
	measure := func(blocks []markdown.Block) int64 {
		var h int64
		for _, u := range e.units(blocks, small, "999", "999") {
			for _, l := range u.lines {
				h += l.h
			}
		}
		return h
	}
	headerH, footerH := measure(in.Header), measure(in.Footer)
	top := e.x0
	if headerH > 0 {
		top += headerH + e.lineGap
	}
	bottom := PageHeight - e.x0
	if footerH > 0 {
		bottom -= footerH + e.lineGap
	}

	units := e.units(in.Body, e.base, "", "")
	var out Layout
	pages := [][]Box{nil}
	y := top
	newPage := func() {
		pages = append(pages, nil)
		y = top
	}
	place := func(l line) {
		for _, b := range l.boxes {
			b.Y += y
			pages[len(pages)-1] = append(pages[len(pages)-1], b)
		}
		y += l.h
	}
	empty := func() bool { return y == top }
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u.pageBreak {
			if !empty() {
				newPage()
			}
			continue
		}
		total := int64(0)
		for _, l := range u.lines {
			total += l.h
		}
		if u.keepWithNext && i+1 < len(units) {
			next := units[i+1]
			need := total
			for k := 0; k < len(next.lines) && k < 2; k++ {
				need += next.lines[k].h
			}
			if y+need > bottom && !empty() {
				newPage()
			}
		}
		switch {
		case u.tableHeader != nil:
			for k, l := range u.lines {
				if y+l.h > bottom && !empty() {
					newPage()
					if k > 0 {
						place(*u.tableHeader)
					}
				}
				place(l)
			}
		case u.atomic || len(u.lines) < 4:
			if y+total > bottom && !empty() {
				newPage()
			}
			if u.signature != "" {
				out.SignatureFields = append(out.SignatureFields, SignatureField{
					Role: u.signature, Page: len(pages), X: e.x0, Y: y, W: sigW, H: sigH,
				})
			}
			for _, l := range u.lines {
				place(l)
			}
		default:
			k := 0
			for k < len(u.lines) {
				fit, acc := 0, y
				for j := k; j < len(u.lines) && acc+u.lines[j].h <= bottom; j++ {
					acc += u.lines[j].h
					fit++
				}
				rest := len(u.lines) - k
				if fit >= rest {
					for ; k < len(u.lines); k++ {
						place(u.lines[k])
					}
					break
				}
				if rest-fit < 2 { // widow: leave at least two lines for the next page
					fit = rest - 2
				}
				if fit < 2 && !empty() { // orphan: start on a new page
					newPage()
					continue
				}
				fit = max(fit, 1)
				for j := 0; j < fit; j++ {
					place(u.lines[k])
					k++
				}
				newPage()
			}
		}
		y += u.gapAfter
	}

	n := len(pages)
	for p := range pages {
		pg := strconv.Itoa(p + 1)
		all := strconv.Itoa(n)
		hy := e.x0
		for _, u := range e.units(in.Header, small, pg, all) {
			for _, l := range u.lines {
				for _, b := range l.boxes {
					b.Y += hy
					pages[p] = append(pages[p], b)
				}
				hy += l.h
			}
		}
		fy := PageHeight - e.x0 - footerH
		for _, u := range e.units(in.Footer, small, pg, all) {
			for _, l := range u.lines {
				for _, b := range l.boxes {
					b.Y += fy
					pages[p] = append(pages[p], b)
				}
				fy += l.h
			}
		}
		out.Pages = append(out.Pages, Page{Boxes: pages[p]})
	}
	return out
}

// units converts blocks to pagination units at a base size (header/footer use a smaller one).
func (e *engine) units(blocks []markdown.Block, base int64, page, pages string) []unit {
	var out []unit
	gap := lineHeight(base) / 2
	for _, b := range blocks {
		switch b.Kind {
		case markdown.Paragraph:
			out = append(out, unit{lines: e.wrap(b.Content, base, false, e.x0, e.w, AlignLeft, page, pages), gapAfter: gap})
		case markdown.Heading:
			size := base * []int64{0, 16, 13, 11}[b.Level] / 10
			out = append(out, unit{
				lines: e.wrap(b.Content, size, true, e.x0, e.w, AlignLeft, page, pages), keepWithNext: true,
				gapAfter: gap / 2,
			})
		case markdown.List:
			out = append(out, e.list(b, base, e.x0, page, pages)...)
			if len(out) > 0 {
				out[len(out)-1].gapAfter = gap
			}
		case markdown.Table:
			out = append(out, e.table(b, base, page, pages, gap))
		case markdown.Rule:
			h := lineHeight(base) / 2
			out = append(out, unit{atomic: true, lines: []line{{h: h, boxes: []Box{{
				X: e.x0, Y: h / 2, W: e.w, H: 0, Kind: KindRule,
			}}}}, gapAfter: gap / 2})
		case markdown.PageBreak:
			out = append(out, unit{pageBreak: true})
		case markdown.Logo:
			if e.in.Logo == nil || e.in.Logo.W <= 0 || e.in.Logo.H <= 0 {
				continue
			}
			h := logoMaxH
			w := e.in.Logo.W * h / e.in.Logo.H
			if w > logoMaxW {
				w = logoMaxW
				h = e.in.Logo.H * w / e.in.Logo.W
			}
			out = append(out, unit{atomic: true, lines: []line{{h: h, boxes: []Box{{
				X: e.x0, Y: 0, W: w, H: h, Kind: KindImage,
			}}}}, gapAfter: gap})
		case markdown.Signature:
			out = append(out, e.signature(b.Role, base, gap))
		}
	}
	return out
}

func (e *engine) signature(role string, base, gap int64) unit {
	lh := lineHeight(base)
	label := e.in.Labels.Roles[role]
	if label == "" {
		label = role
	}
	boxes := []Box{{X: e.x0, Y: 0, W: sigW, H: sigH, Kind: KindSignature, Text: role}}
	texts := []string{label, e.in.Labels.Name + ":", e.in.Labels.Date + ":"}
	for i, t := range texts {
		st := Style{Size: base, Bold: i == 0}
		boxes = append(boxes, Box{
			X: e.x0 + cellPad, Y: cellPad + int64(i)*lh, W: TextWidth(t, st.Bold, false, base), H: lh,
			Kind: KindText, Text: t, Style: st,
		})
	}
	return unit{atomic: true, signature: role, lines: []line{{h: sigH, boxes: boxes}}, gapAfter: gap}
}

func (e *engine) list(b markdown.Block, base, x int64, page, pages string) []unit {
	var out []unit
	for i, it := range b.Items {
		marker := "•"
		if b.Ordered {
			marker = strconv.Itoa(b.Start+i) + "."
		}
		lines := e.wrap(it.Content, base, false, x+listInd, e.w-(x-e.x0)-listInd, AlignLeft, page, pages)
		if len(lines) > 0 {
			mw := TextWidth(marker, false, false, base)
			lines[0].boxes = append([]Box{{
				X: x + listInd - mw - 150, Y: 0, W: mw, H: lineHeight(base), Kind: KindText, Text: marker,
				Style: Style{Size: base},
			}}, lines[0].boxes...)
		}
		out = append(out, unit{lines: lines})
		if it.Sub != nil {
			out = append(out, e.list(*it.Sub, base, x+listInd, page, pages)...)
		}
	}
	return out
}

func (e *engine) table(b markdown.Block, base int64, page, pages string, gap int64) unit {
	cols := len(b.Header)
	natural := make([]int64, cols)
	minW := make([]int64, cols)
	measure := func(row []markdown.Inline, bold bool) {
		for j := 0; j < cols && j < len(row); j++ {
			txt := row[j].PlainText(page, pages)
			w := TextWidth(txt, bold, false, base) + 2*cellPad
			natural[j] = max(natural[j], w)
			for _, word := range strings.Fields(txt) {
				minW[j] = max(minW[j], TextWidth(word, bold, false, base)+2*cellPad)
			}
		}
	}
	measure(b.Header, true)
	for _, r := range b.Rows {
		measure(r, false)
	}
	// Each column gets its longest word (capped to an equal share), then the rest of the width is shared in
	// proportion to what each column still needs to fit on one line.
	widths := make([]int64, cols)
	var minSum, extra int64
	for j := range natural {
		minW[j] = min(max(minW[j], 1000), e.w/int64(cols))
		natural[j] = max(natural[j], minW[j])
		widths[j] = minW[j]
		minSum += minW[j]
		extra += natural[j] - minW[j]
	}
	free := e.w - minSum
	var used int64
	for j := range widths {
		if extra > 0 {
			widths[j] += (natural[j] - minW[j]) * free / extra
		} else {
			widths[j] += free / int64(cols)
		}
		if j == cols-1 {
			widths[j] = e.w - used
		}
		used += widths[j]
	}
	row := func(cells []markdown.Inline, bold bool) line {
		var boxes []Box
		h := lineHeight(base)
		x := e.x0
		var cellLines [][]line
		for j := 0; j < cols; j++ {
			var in markdown.Inline
			if j < len(cells) {
				in = cells[j]
			}
			if bold {
				in = boldInline(in)
			}
			al := AlignLeft
			if j < len(b.Aligns) {
				al = []string{AlignLeft, AlignCenter, AlignRight}[b.Aligns[j]]
			}
			ls := e.wrap(in, base, false, x+cellPad, widths[j]-2*cellPad, al, page, pages)
			cellLines = append(cellLines, ls)
			var ch int64
			for _, l := range ls {
				ch += l.h
			}
			h = max(h, ch)
			x += widths[j]
		}
		h += 2 * cellPad
		x = e.x0
		for j, ls := range cellLines {
			boxes = append(boxes, Box{X: x, Y: 0, W: widths[j], H: h, Kind: KindCell})
			y := cellPad
			for _, l := range ls {
				for _, bx := range l.boxes {
					bx.Y += y
					boxes = append(boxes, bx)
				}
				y += l.h
			}
			x += widths[j]
		}
		return line{h: h, boxes: boxes}
	}
	head := row(b.Header, true)
	u := unit{tableHeader: &head, lines: []line{head}, gapAfter: gap}
	for _, r := range b.Rows {
		u.lines = append(u.lines, row(r, false))
	}
	return u
}

func boldInline(in markdown.Inline) markdown.Inline {
	out := make(markdown.Inline, len(in))
	for i, s := range in {
		s.Bold = true
		out[i] = s
	}
	return out
}

type word struct {
	text      string
	bold, ita bool
	space     bool // preceded by a space
	brk       bool // hard break before
}

func words(in markdown.Inline, page, pages string) []word {
	var out []word
	space, brk := false, false
	for _, s := range in {
		switch s.Kind {
		case markdown.SpanBreak:
			brk = true
			continue
		case markdown.SpanPage:
			s.Text = page
		case markdown.SpanPages:
			s.Text = pages
		}
		t := s.Text
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, " ") {
			space = true
		}
		fields := strings.Fields(t)
		for i, f := range fields {
			out = append(out, word{text: f, bold: s.Bold, ita: s.Italic, space: space || i > 0, brk: brk})
			space, brk = false, false
		}
		if strings.HasSuffix(t, " ") {
			space = true
		}
	}
	return out
}

// wrap breaks an inline into lines of width w starting at x, merging consecutive words of one style in one box.
func (e *engine) wrap(in markdown.Inline, size int64, forceBold bool, x, w int64, align string, page, pages string) []line {
	lh := lineHeight(size)
	ws := words(in, page, pages)
	if forceBold {
		for i := range ws {
			ws[i].bold = true
		}
	}
	var lines []line
	var cur []word
	var curW int64
	emit := func() {
		lines = append(lines, e.boxLine(cur, size, x, w, align, lh))
		cur, curW = nil, 0
	}
	for _, wd := range splitLong(ws, size, w) {
		ww := TextWidth(wd.text, wd.bold, wd.ita, size)
		sp := int64(0)
		if len(cur) > 0 && wd.space {
			sp = TextWidth(" ", wd.bold, wd.ita, size)
		}
		if len(cur) > 0 && (wd.brk || curW+sp+ww > w) {
			emit()
			sp = 0
		}
		if len(cur) == 0 {
			wd.space = false
		}
		cur = append(cur, wd)
		curW += sp + ww
	}
	if len(cur) > 0 || len(lines) == 0 {
		emit()
	}
	return lines
}

// splitLong hard-splits words wider than w.
func splitLong(ws []word, size, w int64) []word {
	var out []word
	for _, wd := range ws {
		if TextWidth(wd.text, wd.bold, wd.ita, size) <= w || utf8.RuneCountInString(wd.text) < 2 {
			out = append(out, wd)
			continue
		}
		runes := []rune(wd.text)
		start := 0
		for start < len(runes) {
			end := start + 1
			for end < len(runes) && TextWidth(string(runes[start:end+1]), wd.bold, wd.ita, size) <= w {
				end++
			}
			part := wd
			part.text = string(runes[start:end])
			if start > 0 {
				part.space, part.brk = false, false
			}
			out = append(out, part)
			start = end
		}
	}
	return out
}

func (e *engine) boxLine(ws []word, size, x, w int64, align string, lh int64) line {
	type run struct {
		text      strings.Builder
		bold, ita bool
	}
	var runs []*run
	for _, wd := range ws {
		if len(runs) == 0 || runs[len(runs)-1].bold != wd.bold || runs[len(runs)-1].ita != wd.ita {
			r := &run{bold: wd.bold, ita: wd.ita}
			if wd.space {
				r.text.WriteByte(' ')
			}
			r.text.WriteString(wd.text)
			runs = append(runs, r)
			continue
		}
		r := runs[len(runs)-1]
		if wd.space {
			r.text.WriteByte(' ')
		}
		r.text.WriteString(wd.text)
	}
	var total int64
	widths := make([]int64, len(runs))
	for i, r := range runs {
		widths[i] = TextWidth(r.text.String(), r.bold, r.ita, size)
		total += widths[i]
	}
	off := int64(0)
	switch align {
	case AlignCenter:
		off = (w - total) / 2
	case AlignRight:
		off = w - total
	}
	cx := x + max(off, 0)
	var boxes []Box
	for i, r := range runs {
		boxes = append(boxes, Box{
			X: cx, Y: 0, W: widths[i], H: lh, Kind: KindText, Text: r.text.String(),
			Style: Style{Bold: r.bold, Italic: r.ita, Size: size},
		})
		cx += widths[i]
	}
	return line{h: lh, boxes: boxes}
}

// Text is the golden text of the layout: per page, the text boxes grouped by line (same Y, ordered by X),
// table cells separated by " | ", rules as "----", signature frames as "[signature role]" and the logo as "[logo]".
func (l Layout) Text() string {
	var b strings.Builder
	for i, p := range l.Pages {
		b.WriteString("=== page " + strconv.Itoa(i+1) + " ===\n")
		boxes := make([]Box, len(p.Boxes))
		copy(boxes, p.Boxes)
		sort.SliceStable(boxes, func(a, c int) bool {
			if boxes[a].Y != boxes[c].Y {
				return boxes[a].Y < boxes[c].Y
			}
			return boxes[a].X < boxes[c].X
		})
		var cur strings.Builder
		curY := int64(-1)
		lastEnd := int64(0)
		flush := func() {
			if cur.Len() > 0 {
				b.WriteString(strings.TrimRight(cur.String(), " ") + "\n")
				cur.Reset()
			}
		}
		for _, bx := range boxes {
			var t string
			switch bx.Kind {
			case KindText:
				t = bx.Text
			case KindRule:
				t = "----"
			case KindImage:
				t = "[logo]"
			case KindSignature:
				t = "[signature " + bx.Text + "]"
			default:
				continue
			}
			switch {
			case bx.Y != curY:
				flush()
				curY = bx.Y
			case bx.X > lastEnd+cellPad:
				cur.WriteString(" | ")
			case bx.X > lastEnd:
				cur.WriteString(" ")
			}
			cur.WriteString(t)
			lastEnd = bx.X + bx.W
		}
		flush()
	}
	return b.String()
}
