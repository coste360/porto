package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/coste360/porto/internal/domain/docs/layout"
)

// Producer is the /Producer of every PDF.
const Producer = "Porto"

const family = "go"

// Meta are the document metadata.
type Meta struct {
	Title    string
	Author   string // the marina
	Subject  string
	Lang     string // it, en
	Instant  time.Time
	LogoPNG  []byte // the marina logo (PNG or JPEG), drawn in the image boxes
	LogoType string // "PNG" or "JPG"
}

// mm converts 1/100 mm to millimetres (the only float conversion of the pipeline).
func mm(v int64) float64 { return float64(v) / 100 }

// Draw renders a layout. The same layout, metadata and instant give the same bytes.
func Draw(l layout.Layout, m Meta) ([]byte, error) {
	pdf := fpdf.NewCustom(&fpdf.InitType{OrientationStr: "P", UnitStr: "mm", SizeStr: "A4"})
	pdf.SetCompression(true)
	pdf.SetCatalogSort(true)
	at := m.Instant.UTC().Truncate(time.Second)
	pdf.SetCreationDate(at)
	pdf.SetModificationDate(at)
	pdf.SetProducer(Producer, false)
	pdf.SetCreator(Producer, false)
	if m.Title != "" {
		pdf.SetTitle(m.Title, true)
	}
	if m.Author != "" {
		pdf.SetAuthor(m.Author, true)
	}
	if m.Subject != "" {
		pdf.SetSubject(m.Subject, true)
	}
	if m.Lang != "" {
		pdf.SetLang(m.Lang)
	}
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	for _, f := range []struct {
		style string
		face  layout.FontFace
	}{{"", layout.Regular}, {"B", layout.Bold}, {"I", layout.Italic}, {"BI", layout.BoldItalic}} {
		pdf.AddUTF8FontFromBytes(family, f.style, layout.FontBytes(f.face))
	}
	logo := ""
	if len(m.LogoPNG) > 0 && (m.LogoType == "PNG" || m.LogoType == "JPG") {
		logo = "logo"
		pdf.RegisterImageOptionsReader(logo, fpdf.ImageOptions{ImageType: m.LogoType}, bytes.NewReader(m.LogoPNG))
		if pdf.Err() {
			pdf.ClearError()
			logo = ""
		}
	}
	pages := l.Pages
	if len(pages) == 0 {
		pages = []layout.Page{{}}
	}
	for _, p := range pages {
		pdf.AddPage()
		pdf.SetDrawColor(0, 0, 0)
		pdf.SetTextColor(0, 0, 0)
		for _, b := range p.Boxes {
			switch b.Kind {
			case layout.KindText:
				style := ""
				if b.Style.Bold {
					style += "B"
				}
				if b.Style.Italic {
					style += "I"
				}
				size := float64(b.Style.Size) / 10
				pdf.SetFont(family, style, size)
				// Baseline: the top of the line plus the font ascent (about 0.8 em) centred in the line height.
				sizeMM := size * 25.4 / 72
				base := mm(b.Y) + (mm(b.H)-sizeMM)/2 + sizeMM*0.8
				pdf.Text(mm(b.X), base, b.Text)
			case layout.KindRule:
				pdf.SetLineWidth(0.2)
				pdf.Line(mm(b.X), mm(b.Y), mm(b.X+b.W), mm(b.Y))
			case layout.KindCell:
				pdf.SetLineWidth(0.15)
				pdf.Rect(mm(b.X), mm(b.Y), mm(b.W), mm(b.H), "D")
			case layout.KindSignature:
				pdf.SetLineWidth(0.3)
				pdf.Rect(mm(b.X), mm(b.Y), mm(b.W), mm(b.H), "D")
			case layout.KindPolygon:
				if len(b.Points) < 3 {
					continue
				}
				pts := make([]fpdf.PointType, len(b.Points))
				for i, q := range b.Points {
					pts[i] = fpdf.PointType{X: mm(q[0]), Y: mm(q[1])}
				}
				pdf.SetLineWidth(0.15)
				style := "D"
				if b.Fill > 0 {
					pdf.SetFillColor(b.Fill, b.Fill, b.Fill)
					style = "FD"
				}
				pdf.Polygon(pts, style)
			case layout.KindImage:
				if logo != "" {
					pdf.ImageOptions(logo, mm(b.X), mm(b.Y), mm(b.W), mm(b.H), false, fpdf.ImageOptions{ImageType: m.LogoType}, 0, "")
				}
			}
		}
	}
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("pdf: draw: %w", err)
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("pdf: output: %w", err)
	}
	return out.Bytes(), nil
}

// PageCount returns the number of pages of a PDF written by this package (latest revision).
func PageCount(raw []byte) (int, error) {
	d, err := parse(raw)
	if err != nil {
		return 0, err
	}
	pages, err := d.pagesObject()
	if err != nil {
		return 0, err
	}
	c, ok := dictInt(pages.dict, "/Count")
	if !ok {
		return 0, errors.New("pdf: page tree without /Count")
	}
	return c, nil
}
