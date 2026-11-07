package layout

import (
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// FontFace identifies one of the embedded Go fonts (golang.org/x/image/font/gofont, BSD-3). docs/pdf embeds the
// same bytes, so the widths measured here are the widths drawn there.
type FontFace int

// Faces.
const (
	Regular FontFace = iota
	Bold
	Italic
	BoldItalic
)

// FontBytes returns the TrueType bytes of a face.
func FontBytes(f FontFace) []byte {
	switch f {
	case Bold:
		return gobold.TTF
	case Italic:
		return goitalic.TTF
	case BoldItalic:
		return gobolditalic.TTF
	default:
		return goregular.TTF
	}
}

type metrics struct {
	font *sfnt.Font
	upem int64
	mu   sync.Mutex
	buf  sfnt.Buffer
	adv  map[rune]int64
}

var (
	fontsOnce sync.Once
	faces     [4]*metrics
)

func loadFonts() {
	for i := range faces {
		f, err := sfnt.Parse(FontBytes(FontFace(i)))
		if err != nil {
			panic("layout: embedded font: " + err.Error()) // embedded bytes: a programming error
		}
		faces[i] = &metrics{font: f, upem: int64(f.UnitsPerEm()), adv: map[rune]int64{}}
	}
}

func faceOf(bold, italic bool) FontFace {
	switch {
	case bold && italic:
		return BoldItalic
	case bold:
		return Bold
	case italic:
		return Italic
	}
	return Regular
}

// advance returns the advance width of r in font units (the width of '?' for a missing glyph).
func (m *metrics) advance(r rune) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.adv[r]; ok {
		return a
	}
	ppem := fixed.Int26_6(m.upem << 6)
	idx, err := m.font.GlyphIndex(&m.buf, r)
	if err != nil || idx == 0 {
		idx, _ = m.font.GlyphIndex(&m.buf, '?')
	}
	a, err := m.font.GlyphAdvance(&m.buf, idx, ppem, font.HintingNone)
	v := int64(0)
	if err == nil {
		v = int64(a) >> 6
	}
	m.adv[r] = v
	return v
}

// TextWidth is the width of s in 1/100 mm at size (tenths of a point) in the face of bold and italic.
func TextWidth(s string, bold, italic bool, size int64) int64 {
	fontsOnce.Do(loadFonts)
	m := faces[faceOf(bold, italic)]
	var units int64
	for _, r := range s {
		units += m.advance(r)
	}
	return units * size * 254 / (72 * m.upem)
}
