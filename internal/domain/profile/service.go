// Package profile is the M12 public marina profile (spec 25; m12-plan §11, §12): the draft edited in the panel,
// publication on /p/{slug} next to the booking page, photos re-encoded without metadata, the public JSON, the
// simplified map, the transit price matrix when published and the aggregate occupancy when enabled, the JSON-LD
// Marina/Place head of the page. Nothing is sent to external directories. Public outputs carry no personal data and
// no per-berth occupancy (INV-103). Writes carry audit and profile.changed in the same transaction.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/marina"

	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// Service is the profile service.
type Service struct {
	d Deps

	mu        sync.Mutex
	heads     map[string]*Head          // marina|lang|version -> rendered head (60 s)
	occupancy map[string]occupancyEntry // marina -> aggregate (60 s)
}

type occupancyEntry struct {
	data    *PublicOccupancyData
	etag    string
	expires time.Time
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d, heads: map[string]*Head{}, occupancy: map[string]occupancyEntry{}}
}

// Audit actions and profile.changed changes.
const (
	actUpdate      = "marina_profile.update"
	actPublish     = "marina_profile.publish"
	actUnpublish   = "marina_profile.unpublish"
	actPhotoUpload = "marina_profile.photo_upload"
	actPhotoDelete = "marina_profile.photo_delete"
	entityProfile  = "marina_profile"
	entityPhoto    = "profile_photo"
)

// Missing items of a publication (422 profile_incomplete details).
const (
	MissingPosition = "position"
	MissingContact  = "contact"
	MissingName     = "name"
)

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) baseURL() string { return strings.TrimRight(s.d.BaseURL, "/") }

func pageURL(base, slug string) string { return base + "/p/" + slug }

// decodeJSON decodes a JSON column into v, leaving v unchanged when the column is empty or invalid.
func decodeJSON(raw store.JSON, v any) {
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, v)
	}
}

// defaults builds the never-saved profile of a marina: position, address, contacts and locales prefilled from
// the marina, version 0, nothing published.
func defaults(m *store.Marina) store.MarinaProfile {
	var addr marina.Address
	var contacts marina.Contacts
	var g marina.Geo
	decodeJSON(m.Address, &addr)
	decodeJSON(m.Contacts, &contacts)
	row := store.MarinaProfile{
		MarinaID: m.ID, Phone: contacts.Phone, Email: contacts.Email,
		ShowMap: true,
	}
	if strings.HasPrefix(contacts.Website, "https://") {
		row.Website = contacts.Website
	}
	if len(m.Geo) > 0 && json.Unmarshal(m.Geo, &g) == nil && (g.LatE7 != 0 || g.LonE7 != 0) {
		lat, lon := g.LatE7, g.LonE7
		row.EntranceLatE7, row.EntranceLonE7 = &lat, &lon
	}
	street := strings.TrimSpace(strings.TrimSpace(addr.Line1) + " " + strings.TrimSpace(addr.Line2))
	row.Address = store.MustJSON(Address{Street: street, PostalCode: addr.PostalCode, City: addr.City, Province: addr.Province, Country: addr.Country})
	vhf := []int{}
	if n, err := strconv.Atoi(strings.TrimSpace(contacts.VHFChannel)); err == nil && n >= 1 && n <= 88 {
		vhf = append(vhf, n)
	}
	row.VHFChannels = store.MustJSON(vhf)
	row.Locales = store.MustJSON(marinaLocales(m))
	empty := store.MustJSON(Text{})
	row.HoursNote, row.AccessNotes, row.Lights, row.Description = empty, empty, empty, empty
	for _, dst := range []*store.JSON{&row.OfficeHours, &row.DockHours, &row.Shelter, &row.AccessImages, &row.Photos, &row.OccupancyCategories, &row.POI} {
		*dst = store.JSON("[]")
	}
	row.Services = store.JSON("{}")
	return row
}

// marinaLocales returns the profile languages a marina can publish: it, plus en when the marina offers it.
func marinaLocales(m *store.Marina) []string {
	out := []string{"it"}
	if slices.Contains([]string(m.Locales), "en") {
		out = append(out, "en")
	}
	return out
}

// load returns the saved profile or the defaults (version 0) and the marina.
func load(ctx context.Context, db bun.IDB, scope store.MarinaScope, lock bool) (*store.MarinaProfile, *store.Marina, error) {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return nil, nil, notFound(err)
	}
	get := store.GetMarinaProfile
	if lock {
		get = store.LockMarinaProfile
	}
	row, err := get(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		d := defaults(m)
		return &d, m, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return row, m, nil
}

// missingOf returns what publishing still needs.
func missingOf(row *store.MarinaProfile, m *store.Marina) []string {
	out := []string{}
	if row.EntranceLatE7 == nil || row.EntranceLonE7 == nil {
		out = append(out, MissingPosition)
	}
	if strings.TrimSpace(row.Phone) == "" && strings.TrimSpace(row.Email) == "" && strings.TrimSpace(row.Website) == "" {
		out = append(out, MissingContact)
	}
	if strings.TrimSpace(m.Name) == "" {
		out = append(out, MissingName)
	}
	return out
}

// today returns the current night in the marina time zone.
func (s *Service) today(m *store.Marina) calendar.Date {
	loc, err := calendar.LoadLocation(m.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return calendar.In(s.d.Clock.Now(), loc)
}

// berthFigures returns the computed berths_total and transit_berths (overrides win).
func berthFigures(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MarinaProfile) (int, int, error) {
	berths, err := store.PublicActiveBerths(ctx, db, scope)
	if err != nil {
		return 0, 0, err
	}
	total, transit := len(berths), 0
	for _, b := range berths {
		if b.OnlineSellable || slices.Contains([]string(b.Attributes), tagTransitOnly) {
			transit++
		}
	}
	if row.BerthsTotalOverride != nil {
		total = *row.BerthsTotalOverride
	}
	if row.TransitBerthsOverride != nil {
		transit = *row.TransitBerthsOverride
	}
	return total, transit, nil
}

func (s *Service) pricesAvailable(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.Marina) (bool, error) {
	_, err := pricelist.PublicTransitList(ctx, db, scope, s.today(m))
	if errors.Is(err, pricelist.ErrNoPublicMatrix) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) view(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MarinaProfile, m *store.Marina) (*View, error) {
	total, transit, err := berthFigures(ctx, db, scope, row)
	if err != nil {
		return nil, err
	}
	prices, err := s.pricesAvailable(ctx, db, scope, m)
	if err != nil {
		return nil, err
	}
	return &View{
		MarinaProfile: *row, BerthsTotal: total, TransitBerths: transit, PricesAvailable: prices,
		Missing: missingOf(row, m), PublicURL: pageURL(s.baseURL(), m.Slug), LocalMode: s.d.LocalMode,
	}, nil
}

// write records the audit row and profile.changed in tx.
func (s *Service) write(ctx context.Context, tx bun.IDB, p *auth.Principal, scope store.MarinaScope, action, entityType, entityID, change string, diff map[string]any) error {
	if s.d.Audit != nil {
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action,
			EntityType: entityType, EntityID: entityID, Diff: diff,
		}); err != nil {
			return err
		}
	}
	if s.d.Outbox != nil {
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ProfileChanged, event.ProfileChangedData{MarinaID: scope.ID(), Change: change}); err != nil {
			return err
		}
	}
	return nil
}

// forget drops the cached head and aggregate of a marina after a write.
func (s *Service) forget(marinaID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.heads {
		if strings.HasPrefix(k, marinaID+"|") {
			delete(s.heads, k)
		}
	}
	delete(s.occupancy, marinaID)
}

// Get returns the draft (marina.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID string) (*View, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	var out *View
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, m, err := load(ctx, tx, scope, false)
		if err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, row, m)
		return err
	})
	return out, err
}

// Put saves the draft (marina.configure; If-Match 0 for the first save). A published profile stays published and
// must stay publishable (422 profile_incomplete otherwise).
func (s *Service) Put(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in Input) (*View, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	in = normalize(in)
	var out *View
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, m, err := load(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := s.validate(ctx, tx, scope, m, in); err != nil {
			return err
		}
		next := apply(*row, in)
		if next.Published {
			if missing := missingOf(&next, m); len(missing) > 0 {
				return &IncompleteError{Missing: missing}
			}
		}
		if err := store.SaveMarinaProfile(ctx, tx, scope, ifMatch, &next, s.now()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.write(ctx, tx, p, scope, actUpdate, entityProfile, scope.ID(), "updated", map[string]any{
			"version": next.Version, "published": next.Published, "photos": len(in.Photos), "show_map": in.ShowMap,
			"show_prices": in.ShowPrices, "show_occupancy": in.ShowOccupancy,
		}); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, &next, m)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.forget(scope.ID())
	return out, nil
}

// Publish publishes the profile (marina.configure); *IncompleteError when position, contact or name is missing.
// Publishing a never-saved profile (If-Match 0) saves the defaults.
func (s *Service) Publish(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int) (*View, error) {
	return s.setPublished(ctx, p, marinaID, ifMatch, true)
}

// Unpublish withdraws the profile (marina.configure).
func (s *Service) Unpublish(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int) (*View, error) {
	return s.setPublished(ctx, p, marinaID, ifMatch, false)
}

func (s *Service) setPublished(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, publish bool) (*View, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out *View
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, m, err := load(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		next := *row
		now := s.now()
		action, change := actUnpublish, "unpublished"
		if publish {
			if missing := missingOf(&next, m); len(missing) > 0 {
				return &IncompleteError{Missing: missing}
			}
			next.Published, next.PublishedAt = true, &now
			action, change = actPublish, "published"
		} else {
			next.Published, next.PublishedAt = false, nil
		}
		if err := store.SaveMarinaProfile(ctx, tx, scope, ifMatch, &next, now); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.write(ctx, tx, p, scope, action, entityProfile, scope.ID(), change, map[string]any{
			"version": next.Version, "published": next.Published,
		}); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, &next, m)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.forget(scope.ID())
	return out, nil
}

// Preview returns the public JSON and the JSON-LD the draft would publish (marina.configure).
func (s *Service) Preview(ctx context.Context, p *auth.Principal, marinaID, lang string) (*Public, map[string]any, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, nil, err
	}
	var pub *Public
	var ld map[string]any
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, m, err := load(ctx, tx, scope, false)
		if err != nil {
			return err
		}
		pub, ld, err = s.publicOf(ctx, tx, scope, row, m, lang)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return pub, ld, nil
}

// tagTransitOnly is the berth attribute of the berths dedicated to transits (occupancy.TagTransitOnly).
const tagTransitOnly = "transit_only"
