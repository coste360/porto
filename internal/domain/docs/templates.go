package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs/tmpl"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// KindDetail is a catalogue entry with its fields.
type KindDetail struct {
	Info   KindInfo
	Fields []tmpl.Field
}

// Publishable reports whether marina templates of the kind can be published in M4.
func (k KindInfo) Publishable() bool { return k.AvailableFrom == "" && !k.BuiltinOnly && k.View != nil }

// ListTemplateKinds returns the catalogue with the fields of every kind (contracts.read).
func (s *Service) ListTemplateKinds(ctx context.Context, p *auth.Principal, marinaID string) ([]KindDetail, error) {
	if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead); err != nil {
		return nil, err
	}
	var out []KindDetail
	for _, k := range Catalogue() {
		out = append(out, KindDetail{Info: k, Fields: Fields(k.Kind)})
	}
	return out, nil
}

// Template is a template with its published version per locale.
type Template struct {
	store.Template
	Locales map[string]int
}

// TemplateInput creates a template.
type TemplateInput struct {
	Kind, Code, Name       string
	IsDefault, FromBuiltin bool
}

// TemplatePatch changes a template (nil = unchanged).
type TemplatePatch struct {
	Name      *string
	IsDefault *bool
	Archived  *bool
}

// VersionInput is a new draft version.
type VersionInput struct {
	Locale, Body, Header, Footer string
	MarginMM, FontSizePt         int64
}

type pageSettings struct {
	MarginsMM  int64 `json:"margins_mm,omitempty"`
	FontSizePt int64 `json:"font_size_pt,omitempty"`
}

// PageOf decodes the page settings of a version.
func PageOf(v *store.TemplateVersion) (margins, font int64) {
	var ps pageSettings
	_ = json.Unmarshal(v.Page, &ps)
	return ps.MarginsMM, ps.FontSizePt
}

// SourceOf returns the source of a version.
func SourceOf(v *store.TemplateVersion) Source {
	m, f := PageOf(v)
	return Source{Body: v.BodyMD, Header: v.HeaderMD, Footer: v.FooterMD, MarginMM: m, FontSizePt: f}
}

func validationErr(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v.Err()
}

func (s *Service) templateView(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Template) ([]Template, error) {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	locs, err := store.PublishedTemplateLocales(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Template, len(rows))
	for i, r := range rows {
		out[i] = Template{Template: r, Locales: locs[r.ID]}
	}
	return out, nil
}

// ListTemplates returns the templates of the marina (contracts.read).
func (s *Service) ListTemplates(ctx context.Context, p *auth.Principal, marinaID, kind string) ([]Template, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListTemplates(ctx, s.d.DB.DB, scope, kind)
	if err != nil {
		return nil, err
	}
	return s.templateView(ctx, s.d.DB.DB, scope, rows)
}

// GetTemplate returns a template (contracts.read).
func (s *Service) GetTemplate(ctx context.Context, p *auth.Principal, marinaID, id string) (*Template, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetTemplate(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	out, err := s.templateView(ctx, s.d.DB.DB, scope, []store.Template{*row})
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

func validCode(c string) bool {
	if c == "" || len(c) > 64 {
		return false
	}
	for _, r := range c {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// CreateTemplate creates a template of a publishable kind (templates.configure); FromBuiltin adds draft
// versions with the built-in text in it and en.
func (s *Service) CreateTemplate(ctx context.Context, p *auth.Principal, marinaID string, in TemplateInput) (*Template, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	info, ok := KindInfoOf(Kind(in.Kind))
	if !ok {
		return nil, validationErr("kind", "invalid")
	}
	if !info.Publishable() {
		return nil, ErrKindNotPublishable
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validCode(in.Code) {
		return nil, validationErr("code", "invalid")
	}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 120 {
		return nil, validationErr("name", "invalid")
	}
	now := s.now()
	row := &store.Template{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: in.Kind, Code: in.Code, Name: in.Name, IsDefault: in.IsDefault,
		CreatedByUserID: p.UserID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertTemplate(ctx, tx, row); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		if in.IsDefault {
			if err := store.ClearDefaultTemplates(ctx, tx, scope, in.Kind, row.ID, now); err != nil {
				return err
			}
		}
		if in.FromBuiltin {
			for _, loc := range builtinLocales {
				src, _, ok := Builtin(Kind(in.Kind), loc)
				if !ok {
					continue
				}
				v := &store.TemplateVersion{
					ID: store.NewID(), MarinaID: scope.ID(), TemplateID: row.ID, Version: 1, Locale: loc,
					BodyMD: src.Body, HeaderMD: src.Header, FooterMD: src.Footer, Page: store.JSON(`{}`),
					Status: store.TemplateDraft, CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
				}
				if err := store.InsertTemplateVersion(ctx, tx, v); err != nil {
					return err
				}
			}
		}
		return s.write(ctx, tx, scope, p.Actor(), "template.create", "template", row.ID,
			map[string]any{"kind": in.Kind, "code": in.Code, "is_default": in.IsDefault, "from_builtin": in.FromBuiltin},
			event.TemplateChanged, event.TemplateChangedData{TemplateID: row.ID, Change: "created"})
	})
	if err != nil {
		return nil, err
	}
	return &Template{Template: *row}, nil
}

// UpdateTemplate renames, sets as default or archives a template (templates.configure).
func (s *Service) UpdateTemplate(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in TemplatePatch) (*Template, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	var out *Template
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetTemplate(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		now := s.now()
		diff := map[string]any{}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			if n == "" || utf8.RuneCountInString(n) > 120 {
				return validationErr("name", "invalid")
			}
			row.Name, diff["name"] = n, true
		}
		if in.Archived != nil {
			if *in.Archived {
				row.ArchivedAt, row.IsDefault = now, false
			} else {
				row.ArchivedAt = timeZero
			}
			diff["archived"] = *in.Archived
		}
		if in.IsDefault != nil {
			row.IsDefault = *in.IsDefault && row.ArchivedAt.IsZero()
			diff["is_default"] = row.IsDefault
		}
		row.UpdatedAt = now
		if err := store.UpdateTemplate(ctx, tx, scope, ifMatch, row); err != nil {
			return err
		}
		if row.IsDefault {
			if err := store.ClearDefaultTemplates(ctx, tx, scope, row.Kind, row.ID, now); err != nil {
				return err
			}
		}
		change := "updated"
		if in.Archived != nil && *in.Archived {
			change = "archived"
		}
		if err := s.write(ctx, tx, scope, p.Actor(), "template.update", "template", row.ID, diff,
			event.TemplateChanged, event.TemplateChangedData{TemplateID: row.ID, Change: change}); err != nil {
			return err
		}
		v, err := s.templateView(ctx, tx, scope, []store.Template{*row})
		if err != nil {
			return err
		}
		out = &v[0]
		return nil
	})
	return out, err
}

// ListTemplateVersions returns the versions of a template (templates.configure).
func (s *Service) ListTemplateVersions(ctx context.Context, p *auth.Principal, marinaID, templateID, locale string) ([]store.TemplateVersion, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetTemplate(ctx, s.d.DB.DB, scope, templateID); err != nil {
		return nil, notFound(err)
	}
	return store.ListTemplateVersions(ctx, s.d.DB.DB, scope, templateID, locale)
}

func checkVersionInput(in VersionInput) error {
	if !auth.ValidLocale(in.Locale) {
		return validationErr("locale", "invalid")
	}
	if len(in.Body) > 262144 || len(in.Header) > 16384 || len(in.Footer) > 16384 {
		return validationErr("body_md", "too_long")
	}
	if in.MarginMM != 0 && (in.MarginMM < 5 || in.MarginMM > 50) {
		return validationErr("page.margins_mm", "invalid")
	}
	if in.FontSizePt != 0 && (in.FontSizePt < 7 || in.FontSizePt > 16) {
		return validationErr("page.font_size_pt", "invalid")
	}
	return nil
}

// CreateTemplateVersion creates a draft version validated against the catalogue (templates.configure): an
// invalid body is *TemplateError (422 template_invalid with line and column) and nothing is stored.
func (s *Service) CreateTemplateVersion(ctx context.Context, p *auth.Principal, marinaID, templateID string, in VersionInput) (*store.TemplateVersion, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	if err := checkVersionInput(in); err != nil {
		return nil, err
	}
	var out *store.TemplateVersion
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetTemplate(ctx, tx, scope, templateID)
		if err != nil {
			return notFound(err)
		}
		src := Source{Body: in.Body, Header: in.Header, Footer: in.Footer, MarginMM: in.MarginMM, FontSizePt: in.FontSizePt}
		if err := ValidateSource(Kind(t.Kind), src); err != nil {
			return err
		}
		n, err := store.NextTemplateVersion(ctx, tx, scope, templateID, in.Locale)
		if err != nil {
			return err
		}
		now := s.now()
		page := store.MustJSON(pageSettings{MarginsMM: in.MarginMM, FontSizePt: in.FontSizePt})
		out = &store.TemplateVersion{
			ID: store.NewID(), MarinaID: scope.ID(), TemplateID: templateID, Version: n, Locale: in.Locale,
			BodyMD: in.Body, HeaderMD: in.Header, FooterMD: in.Footer, Page: page, Status: store.TemplateDraft,
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertTemplateVersion(ctx, tx, out); err != nil {
			if store.IsUniqueViolation(err) {
				return store.ErrConflict
			}
			return err
		}
		return s.write(ctx, tx, scope, p.Actor(), "template.version_create", "template", templateID,
			map[string]any{"version": n, "locale": in.Locale, "sha256": src.SHA256()},
			event.TemplateChanged, event.TemplateChangedData{TemplateID: templateID, Change: "version"})
	})
	return out, err
}

// PublishTemplateVersion publishes a draft version for its locale; the previous published one is superseded
// (templates.configure). A published version never changes (trigger).
func (s *Service) PublishTemplateVersion(ctx context.Context, p *auth.Principal, marinaID, templateID string, version int, locale string) (*store.TemplateVersion, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	if !auth.ValidLocale(locale) {
		return nil, validationErr("locale", "invalid")
	}
	var out *store.TemplateVersion
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetTemplate(ctx, tx, scope, templateID)
		if err != nil {
			return notFound(err)
		}
		info, _ := KindInfoOf(Kind(t.Kind))
		if !info.Publishable() {
			return ErrKindNotPublishable
		}
		v, err := store.GetTemplateVersion(ctx, tx, scope, templateID, version, locale)
		if err != nil {
			return notFound(err)
		}
		if v.Status != store.TemplateDraft {
			return ErrImmutable
		}
		src := SourceOf(v)
		if err := ValidateSource(Kind(t.Kind), src); err != nil {
			return err
		}
		now := s.now()
		if err := store.SupersedePublishedVersion(ctx, tx, scope, templateID, locale, now); err != nil {
			return mapImmutable(err)
		}
		ok, err := store.PublishTemplateVersion(ctx, tx, scope, v.ID, src.SHA256(), p.UserID, now)
		if err != nil {
			if store.IsUniqueViolation(err) {
				return store.ErrConflict
			}
			return mapImmutable(err)
		}
		if !ok {
			return ErrImmutable
		}
		v.Status, v.SHA256, v.PublishedAt, v.PublishedByUserID = store.TemplatePublished, src.SHA256(), now, p.UserID
		out = v
		return s.write(ctx, tx, scope, p.Actor(), "template.publish", "template", templateID,
			map[string]any{"version": version, "locale": locale, "sha256": src.SHA256()},
			event.TemplateChanged, event.TemplateChangedData{TemplateID: templateID, Change: "published"})
	})
	return out, err
}

func mapImmutable(err error) error {
	if store.IsTriggerViolation(err, store.TemplateVersionImmutableMessage) ||
		store.IsTriggerViolation(err, store.DocumentImmutableMessage) {
		return ErrImmutable
	}
	return err
}

// PreviewInput asks for a preview: a saved version (0 = the published one in locale, else the latest draft), or
// an unsaved body; on the demo data or on a real record of the kind.
type PreviewInput struct {
	Version     int
	Locale      string
	Body        *string
	Header      *string
	Footer      *string
	SubjectType string // "" = demo data
	SubjectID   string
}

// PreviewTemplate renders a preview PDF synchronously; nothing is stored (templates.configure).
func (s *Service) PreviewTemplate(ctx context.Context, p *auth.Principal, marinaID, templateID string, in PreviewInput) (*Rendered, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTemplatesConfigure)
	if err != nil {
		return nil, err
	}
	if !auth.ValidLocale(in.Locale) {
		return nil, validationErr("locale", "invalid")
	}
	t, err := store.GetTemplate(ctx, s.d.DB.DB, scope, templateID)
	if err != nil {
		return nil, notFound(err)
	}
	kind := Kind(t.Kind)
	var src Source
	switch {
	case in.Version > 0:
		v, err := store.GetTemplateVersion(ctx, s.d.DB.DB, scope, templateID, in.Version, in.Locale)
		if err != nil {
			return nil, notFound(err)
		}
		src = SourceOf(v)
	default:
		if v, err := store.PublishedTemplateVersion(ctx, s.d.DB.DB, scope, templateID, in.Locale); err == nil {
			src = SourceOf(v)
		} else if b, _, ok := Builtin(kind, in.Locale); ok {
			src = b
		}
	}
	if in.Body != nil {
		src.Body = *in.Body
	}
	if in.Header != nil {
		src.Header = *in.Header
	}
	if in.Footer != nil {
		src.Footer = *in.Footer
	}
	view := DemoView(kind)
	if in.SubjectType != "" {
		ds, ok := s.DataSourceFor(kind)
		if !ok {
			return nil, ErrNoDataSource
		}
		if !p.Can(scope.OrgID(), scope.ID(), subjectPermission(in.SubjectType)) || !p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead) {
			return nil, auth.ErrForbidden
		}
		err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			view, err = ds.View(ctx, tx, scope, kind, SubjectType(in.SubjectType), in.SubjectID, in.Locale)
			return err
		})
		if err != nil {
			return nil, notFound(err)
		}
	}
	now := s.now()
	info, _ := KindInfoOf(kind)
	m, _ := store.GetMarina(ctx, s.d.DB.DB, scope)
	author := ""
	if m != nil {
		author = m.Name
	}
	return Render(RenderInput{
		Kind: kind, Source: src, View: view, Locale: in.Locale, Today: s.today(ctx, s.d.DB.DB, scope, now), At: now,
		PDFA: len(info.SignatureRoles) > 0, Title: Title(kind, in.Locale), Author: author,
		Logo: s.logo(ctx, s.d.DB.DB, scope),
	})
}

var errNoTemplate = errors.New("docs: no template")

// resolveSource picks the template version (or built-in) of a composition: the requested template, else the
// marina default of the kind, else the built-in; the first locale among candidates with a published version.
func (s *Service) resolveSource(ctx context.Context, db bun.IDB, scope store.MarinaScope, kind Kind, templateID string, locales []string) (src Source, versionID, builtinRef, locale string, err error) {
	var t *store.Template
	if templateID != "" {
		t, err = store.GetTemplate(ctx, db, scope, templateID)
		if err != nil {
			return src, "", "", "", notFound(err)
		}
		if Kind(t.Kind) != kind {
			return src, "", "", "", validationErr("template_id", "kind_mismatch")
		}
	} else if dt, derr := store.DefaultTemplate(ctx, db, scope, string(kind)); derr == nil {
		t = dt
	}
	if t != nil {
		for _, loc := range locales {
			if v, verr := store.PublishedTemplateVersion(ctx, db, scope, t.ID, loc); verr == nil {
				return SourceOf(v), v.ID, "", loc, nil
			}
		}
	}
	for _, loc := range locales {
		if b, ref, ok := Builtin(kind, loc); ok && contains(builtinLocales, normLocale(loc)) {
			return b, "", ref, normLocale(loc), nil
		}
	}
	b, ref, ok := Builtin(kind, "it")
	if !ok {
		return src, "", "", "", fmt.Errorf("%w for %s", errNoTemplate, kind)
	}
	return b, "", ref, "it", nil
}

// Version is a template version (the store row; an alias so the HTTP layer does not import the store).
type Version = store.TemplateVersion
