package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of print templates and their versions (m4-plan §5.3, §6.1–§6.2). Every function is scoped by the marina.

// Template version statuses.
const (
	TemplateDraft      = "draft"
	TemplatePublished  = "published"
	TemplateSuperseded = "superseded"
)

// InsertTemplate inserts a template (a duplicate code is a unique violation of templates_code_uidx).
func InsertTemplate(ctx context.Context, db bun.IDB, t *Template) error {
	_, err := db.NewInsert().Model(t).Exec(ctx)
	return err
}

// GetTemplate returns one template of a marina.
func GetTemplate(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Template, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(Template)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// ListTemplates returns the templates of a marina (kind "" = all), archived ones included, by kind and code.
func ListTemplates(ctx context.Context, db bun.IDB, s MarinaScope, kind string) ([]Template, error) {
	var out []Template
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("kind, code")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	return out, q.Scan(ctx)
}

// DefaultTemplate returns the non-archived default template of a kind, or ErrNotFound.
func DefaultTemplate(ctx context.Context, db bun.IDB, s MarinaScope, kind string) (*Template, error) {
	t := new(Template)
	err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("kind = ?", kind).
		Where("is_default = ?", true).Where("archived_at IS NULL").OrderExpr("updated_at DESC, id").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateTemplate writes name, default flag and archive time when the version equals ifMatch (0 = the loaded
// version) and bumps it.
func UpdateTemplate(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, t *Template) error {
	want := ifMatch
	if want == 0 {
		want = t.Version
	}
	res, err := db.NewUpdate().Model(t).Column("name", "is_default", "archived_at", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", t.ID).Where("version = ?", want).
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetTemplate(ctx, db, s, t.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	t.Version = want + 1
	return nil
}

// ClearDefaultTemplates removes the default flag from the other templates of a kind.
func ClearDefaultTemplates(ctx context.Context, db bun.IDB, s MarinaScope, kind, exceptID string, at time.Time) error {
	_, err := db.NewUpdate().Table("templates").Set("is_default = ?", false).Set("version = version + 1").
		Set("updated_at = ?", at).Where("marina_id = ?", s.ID()).Where("kind = ?", kind).
		Where("is_default = ?", true).Where("id <> ?", exceptID).Exec(ctx)
	return err
}

// InsertTemplateVersion inserts a version.
func InsertTemplateVersion(ctx context.Context, db bun.IDB, v *TemplateVersion) error {
	_, err := db.NewInsert().Model(v).Exec(ctx)
	return err
}

// NextTemplateVersion returns the next version number of a template in a locale.
func NextTemplateVersion(ctx context.Context, db bun.IDB, s MarinaScope, templateID, locale string) (int, error) {
	var n int
	err := db.NewSelect().Table("template_versions").ColumnExpr("COALESCE(MAX(version), 0)").
		Where("marina_id = ?", s.ID()).Where("template_id = ?", templateID).Where("locale = ?", locale).Scan(ctx, &n)
	return n + 1, err
}

// ListTemplateVersions returns the versions of a template (locale "" = all), newest first.
func ListTemplateVersions(ctx context.Context, db bun.IDB, s MarinaScope, templateID, locale string) ([]TemplateVersion, error) {
	var out []TemplateVersion
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("template_id = ?", templateID).
		OrderExpr("locale, version DESC")
	if locale != "" {
		q = q.Where("locale = ?", locale)
	}
	return out, q.Scan(ctx)
}

// GetTemplateVersion returns a version by number and locale.
func GetTemplateVersion(ctx context.Context, db bun.IDB, s MarinaScope, templateID string, version int, locale string) (*TemplateVersion, error) {
	v := new(TemplateVersion)
	err := db.NewSelect().Model(v).Where("marina_id = ?", s.ID()).Where("template_id = ?", templateID).
		Where("version = ?", version).Where("locale = ?", locale).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// GetTemplateVersionByID returns a version by id.
func GetTemplateVersionByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*TemplateVersion, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	v := new(TemplateVersion)
	if err := db.NewSelect().Model(v).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// PublishedTemplateVersion returns the published version of a template in a locale, or ErrNotFound.
func PublishedTemplateVersion(ctx context.Context, db bun.IDB, s MarinaScope, templateID, locale string) (*TemplateVersion, error) {
	v := new(TemplateVersion)
	err := db.NewSelect().Model(v).Where("marina_id = ?", s.ID()).Where("template_id = ?", templateID).
		Where("locale = ?", locale).Where("status = ?", TemplatePublished).Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// PublishedTemplateLocales returns, per template id, the published version per locale.
func PublishedTemplateLocales(ctx context.Context, db bun.IDB, s MarinaScope, templateIDs []string) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	if len(templateIDs) == 0 {
		return out, nil
	}
	var rows []TemplateVersion
	err := db.NewSelect().Model(&rows).Column("template_id", "locale", "version").Where("marina_id = ?", s.ID()).
		Where("template_id IN (?)", bun.List(templateIDs)).Where("status = ?", TemplatePublished).Scan(ctx)
	for _, r := range rows {
		if out[r.TemplateID] == nil {
			out[r.TemplateID] = map[string]int{}
		}
		out[r.TemplateID][r.Locale] = r.Version
	}
	return out, err
}

// SupersedePublishedVersion marks the published version of a template in a locale superseded (the only change the
// trigger allows on a published row).
func SupersedePublishedVersion(ctx context.Context, db bun.IDB, s MarinaScope, templateID, locale string, at time.Time) error {
	_, err := db.NewUpdate().Table("template_versions").Set("status = ?", TemplateSuperseded).
		Where("marina_id = ?", s.ID()).Where("template_id = ?", templateID).Where("locale = ?", locale).
		Where("status = ?", TemplatePublished).Exec(ctx)
	_ = at
	return err
}

// PublishTemplateVersion publishes a draft version (conditional on status draft); false when it is not a draft.
func PublishTemplateVersion(ctx context.Context, db bun.IDB, s MarinaScope, id, sha, userID string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("template_versions").Set("status = ?", TemplatePublished).
		Set("sha256 = ?", sha).Set("published_at = ?", UTC(at)).Set("published_by_user_id = ?", nullable(userID)).
		Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status = ?", TemplateDraft).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}
