package docs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/coste360/porto/internal/domain/docs/layout"
	"github.com/coste360/porto/internal/domain/docs/markdown"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/docs/tmpl"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/store"
)

// Ephemeral documents (m5-plan §8.3): receipts, statements of account and export summaries are rendered on demand
// with the M4 pipeline (template language, layout, PDF) and never archived in generated_documents. Their views are
// fixed here, so they render even while the catalogue keeps the kinds unpublishable (AvailableFrom "M5").

// ephemeralViews are the view types of the ephemeral kinds.
var ephemeralViews = map[Kind]reflect.Type{
	KindReceipt:             reflect.TypeOf(views.ReceiptView{}),
	KindStatementOfAccount:  reflect.TypeOf(views.StatementView{}),
	KindChargeExportSummary: reflect.TypeOf(views.ChargeExportSummaryView{}),
}

// ephemeralTitles are the PDF titles of the ephemeral kinds while the catalogue has none.
var ephemeralTitles = map[string]map[Kind]string{
	"it": {KindReceipt: "Ricevuta di cortesia", KindStatementOfAccount: "Estratto conto", KindChargeExportSummary: "Riepilogo esportazione addebiti"},
	"en": {KindReceipt: "Courtesy receipt", KindStatementOfAccount: "Statement of account", KindChargeExportSummary: "Charge export summary"},
}

// ErrNotEphemeral is returned for a kind that is not rendered ephemerally.
var ErrNotEphemeral = errors.New("docs: not an ephemeral kind")

// ephemeralSchema returns the validation schema of an ephemeral kind: the catalogue view when the kind is
// published, else the fixed view of ephemeralViews.
func ephemeralSchema(kind Kind) (tmpl.Schema, error) {
	if f := Fields(kind); len(f) > 0 {
		return Schema(kind), nil
	}
	t, ok := ephemeralViews[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotEphemeral, kind)
	}
	var fields []tmpl.Field
	walkFields(t, "", &fields)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	s := tmpl.Schema{}
	for _, f := range fields {
		s[f.Path] = f
	}
	return s, nil
}

// renderEphemeralImpl is the body of Service.RenderEphemeral.
func (s *Service) renderEphemeralImpl(ctx context.Context, scope store.MarinaScope, kind Kind, locale string, view any) ([]byte, error) {
	schema, err := ephemeralSchema(kind)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	locales := []string{}
	if locale != "" {
		locales = append(locales, normLocale(locale))
	}
	locales = append(locales, normLocale(m.DefaultLocale), "it")
	src, _, _, loc, err := s.resolveSource(ctx, db, scope, kind, "", locales)
	if err != nil {
		return nil, err
	}
	title := Title(kind, loc)
	if title == "" {
		title = ephemeralTitles[normLocale(loc)][kind]
	}
	at := ephemeralInstant(view)
	if at.IsZero() {
		at = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return renderWithSchema(RenderInput{
		Kind: kind, Source: src, View: view, Locale: loc, Today: s.today(ctx, db, scope, at), At: at,
		Title: title, Author: m.Name, Logo: s.logo(ctx, db, scope),
	}, schema)
}

// ephemeralInstant is the instant a view is about (so the same view gives the same bytes): the creation of an
// export, the date of a receipt, the end of a statement.
func ephemeralInstant(view any) time.Time {
	switch v := view.(type) {
	case views.ChargeExportSummaryView:
		return v.CreatedAt.UTC().Truncate(time.Second)
	case *views.ChargeExportSummaryView:
		return v.CreatedAt.UTC().Truncate(time.Second)
	case views.ReceiptView:
		return v.Date.Time(time.UTC)
	case *views.ReceiptView:
		return v.Date.Time(time.UTC)
	case views.StatementView:
		return v.To.Time(time.UTC)
	case *views.StatementView:
		return v.To.Time(time.UTC)
	}
	return time.Time{}
}

// renderWithSchema is Render with an explicit schema (the ephemeral kinds have no catalogue view yet).
func renderWithSchema(in RenderInput, schema tmpl.Schema) ([]byte, error) {
	var issues []TemplateIssue
	for _, part := range []struct {
		name, text string
		mode       tmpl.Mode
	}{{"body", in.Source.Body, tmpl.ModeBody}, {"header", in.Source.Header, tmpl.ModeHeaderFooter}, {"footer", in.Source.Footer, tmpl.ModeHeaderFooter}} {
		t, err := tmpl.Parse(part.text)
		if err == nil {
			err = t.Validate(schema, tmpl.Options{Mode: part.mode})
		}
		var te *tmpl.Error
		if errors.As(err, &te) {
			for _, i := range te.Issues {
				issues = append(issues, TemplateIssue{Line: i.Line, Col: i.Col, Code: prefix(part.name, i.Code), Path: i.Path})
			}
		} else if err != nil {
			return nil, err
		}
	}
	if len(issues) > 0 {
		return nil, &TemplateError{Issues: issues}
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
	raw, err := pdf.Draw(layout.Lay(li), meta)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDocumentBytes {
		return nil, ErrTemplateLimits
	}
	return raw, nil
}
