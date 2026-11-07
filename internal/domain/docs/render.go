package docs

import (
	"errors"
	"fmt"
	"time"

	"github.com/coste360/porto/internal/domain/docs/layout"
	"github.com/coste360/porto/internal/domain/docs/markdown"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/docs/tmpl"
)

// Logo is the marina logo drawn in {{logo}} blocks.
type Logo struct {
	Data []byte
	Type string // PNG or JPG
	W, H int64  // pixels
}

// RenderInput is everything a rendering needs; rendering is a pure function of it (same input, same bytes).
type RenderInput struct {
	Kind   Kind
	Source Source
	View   any
	Locale string
	Today  string    // marina date of the composition (YYYY-MM-DD)
	At     time.Time // instant of the composition (PDF dates)
	PDFA   bool
	Title  string
	Author string
	Logo   *Logo
	// Extra lets the map print add its drawing to the first page.
	Extra func(l *layout.Layout)
}

// Rendered is a rendered PDF.
type Rendered struct {
	PDF   []byte
	Pages int
	Text  string // golden text of the layout
}

// ValidateSource checks body, header and footer of a source against the catalogue of a kind and returns the
// issues as *TemplateError.
func ValidateSource(kind Kind, src Source) error {
	info, ok := KindInfoOf(kind)
	if !ok {
		return fmt.Errorf("%w: unknown kind %s", ErrKindNotPublishable, kind)
	}
	schema := Schema(kind)
	var issues []TemplateIssue
	add := func(part string, err error) {
		var te *tmpl.Error
		if errors.As(err, &te) {
			for _, i := range te.Issues {
				issues = append(issues, TemplateIssue{Line: i.Line, Col: i.Col, Code: prefix(part, i.Code), Path: i.Path})
			}
		}
	}
	for _, part := range []struct {
		name, text string
		mode       tmpl.Mode
	}{{"body", src.Body, tmpl.ModeBody}, {"header", src.Header, tmpl.ModeHeaderFooter}, {"footer", src.Footer, tmpl.ModeHeaderFooter}} {
		t, err := tmpl.Parse(part.text)
		if err != nil {
			add(part.name, err)
			continue
		}
		add(part.name, t.Validate(schema, tmpl.Options{Mode: part.mode, Roles: info.SignatureRoles}))
	}
	if len(issues) > 0 {
		return &TemplateError{Issues: issues}
	}
	return nil
}

// prefix marks issues of the header and footer (the body keeps the bare code, as the editor shows it inline).
func prefix(part, code string) string {
	if part == "body" {
		return code
	}
	return part + "." + code
}

// Render evaluates the source on the view and lays out, draws and (for documents to sign) finalises the PDF.
func Render(in RenderInput) (*Rendered, error) {
	if err := ValidateSource(in.Kind, in.Source); err != nil {
		return nil, err
	}
	data := ToValue(in.View)
	env := tmpl.Env{Locale: in.Locale, Today: in.Today}
	eval := func(text string) ([]markdown.Block, error) {
		t, err := tmpl.Parse(text)
		if err != nil {
			return nil, err
		}
		md, err := t.Eval(data, env)
		if errors.Is(err, tmpl.ErrLimits) {
			return nil, ErrTemplateLimits
		}
		if err != nil {
			return nil, err
		}
		return markdown.Parse(md), nil
	}
	body, err := eval(in.Source.Body)
	if err != nil {
		return nil, err
	}
	header, err := eval(in.Source.Header)
	if err != nil {
		return nil, err
	}
	footer, err := eval(in.Source.Footer)
	if err != nil {
		return nil, err
	}
	loc := normLocale(in.Locale)
	if _, ok := roleLabels[loc]; !ok {
		loc = "it"
	}
	li := layout.Input{
		Body: body, Header: header, Footer: footer, MarginMM: in.Source.MarginMM, FontSizePt: in.Source.FontSizePt,
		Labels: layout.Labels{Roles: roleLabels[loc], Name: nameDateLabels[loc][0], Date: nameDateLabels[loc][1]},
	}
	meta := pdf.Meta{Title: in.Title, Author: in.Author, Lang: loc, Instant: in.At}
	if in.Logo != nil && in.Logo.W > 0 && in.Logo.H > 0 {
		li.Logo = &layout.ImageSize{W: in.Logo.W, H: in.Logo.H}
		meta.LogoPNG, meta.LogoType = in.Logo.Data, in.Logo.Type
	}
	l := layout.Lay(li)
	if in.Extra != nil {
		in.Extra(&l)
	}
	raw, err := pdf.Draw(l, meta)
	if err != nil {
		return nil, err
	}
	if in.PDFA {
		raw, err = pdf.FinalizePDFA(raw, pdf.ArchiveMeta{Title: in.Title, Author: in.Author, Lang: loc, Instant: in.At})
		if err != nil {
			return nil, err
		}
	}
	if len(raw) > MaxDocumentBytes {
		return nil, ErrTemplateLimits
	}
	return &Rendered{PDF: raw, Pages: len(l.Pages), Text: l.Text()}, nil
}

// MaxDocumentBytes is the largest generated PDF.
const MaxDocumentBytes = 15 << 20

// RenderNote renders a one-page note (the paper signature cover page): a title and paragraphs of plain text, every
// value escaped. Deterministic for the same input.
func RenderNote(title string, paragraphs []string, locale string, at time.Time) ([]byte, error) {
	md := "# " + tmpl.Escape(title, false) + "\n\n"
	for _, p := range paragraphs {
		md += tmpl.Escape(p, false) + "\n\n"
	}
	l := layout.Lay(layout.Input{Body: markdown.Parse(md)})
	return pdf.Draw(l, pdf.Meta{Title: title, Lang: normLocale(locale), Instant: at})
}

// RenderBuiltin renders the built-in template of a kind on a view (the OTP evidence page).
func RenderBuiltin(kind Kind, view any, locale string, at time.Time, today string) (*Rendered, error) {
	src, _, ok := Builtin(kind, locale)
	if !ok {
		return nil, fmt.Errorf("docs: no built-in for %s", kind)
	}
	return Render(RenderInput{Kind: kind, Source: src, View: view, Locale: locale, Today: today, At: at, Title: Title(kind, locale)})
}
