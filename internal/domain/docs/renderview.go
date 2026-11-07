package docs

import (
	"context"
	"fmt"
	"time"

	"github.com/coste360/porto/internal/store"
)

// RenderView renders the published template of a kind (or its built-in default) on a view the caller built,
// without storing a document: the synchronous downloads of rights statements and holder chains (m4-plan §6.4).
// The caller has authorised scope; locale empty = the marina default, else it. It never writes to the database.
func (s *Service) RenderView(ctx context.Context, scope store.MarinaScope, kind Kind, locale string, view any) ([]byte, error) {
	if _, ok := KindInfoOf(kind); !ok {
		return nil, fmt.Errorf("%w: %s", ErrKindNotPublishable, kind)
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
	at := store.UTC(s.now()).Truncate(time.Second)
	out, err := Render(RenderInput{
		Kind: kind, Source: src, View: view, Locale: loc, Today: s.today(ctx, db, scope, at), At: at,
		Title: Title(kind, loc), Author: m.Name, Logo: s.logo(ctx, db, scope),
	})
	if err != nil {
		return nil, err
	}
	return out.PDF, nil
}
