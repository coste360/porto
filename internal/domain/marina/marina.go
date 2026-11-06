// Package marina manages organizations and marinas: listing, creation, settings updates with optimistic
// concurrency (If-Match), archiving and the public marina card.
package marina

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/slug"
	"github.com/coste360/porto/internal/store"
)

// Errors.
var (
	ErrSlugTaken       = errors.New("marina: slug taken")
	ErrVersionMismatch = errors.New("marina: version mismatch")
	ErrAlreadyArchived = errors.New("marina: already archived")
)

// ValidateSlug checks a marina slug (see package slug).
func ValidateSlug(s string) error { return slug.Validate(s) }

// Kinds of facility.
var Kinds = []string{"marina", "yard", "both"}

// Address is a postal address.
type Address struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Province   string `json:"province"`
	Country    string `json:"country"`
}

// Contacts are the public contacts of a marina.
type Contacts struct {
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Website    string `json:"website"`
	VHFChannel string `json:"vhf_channel"`
}

// Geo is a WGS84 position in degrees × 10⁷ (integers, no floats).
type Geo struct {
	LatE7 int64 `json:"lat_e7"`
	LonE7 int64 `json:"lon_e7"`
}

// Organization is the API view of an organization.
type Organization struct {
	ID             string
	Name           string
	VATNumber      string
	FiscalCode     string
	BillingAddress Address
	SDICode        string
	PEC            string
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Marina is the API view of a marina.
type Marina struct {
	ID                  string
	OrganizationID      string
	Slug                string
	Name                string
	Kind                string
	Timezone            string
	Currency            string
	DefaultLocale       string
	Locales             []string
	Contacts            Contacts
	Address             Address
	Geo                 *Geo
	ConcessionRef       string
	ConcessionExpiresOn calendar.Date
	Settings            json.RawMessage
	Theme               json.RawMessage
	BookingEnabled      bool
	AllowedEmbedOrigins []string
	Version             int
	ArchivedAt          time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// PublicMarina is what anonymous visitors see.
type PublicMarina struct {
	Slug           string
	Name           string
	DefaultLocale  string
	Locales        []string
	BookingEnabled bool
}

// Service manages organizations and marinas.
type Service struct {
	db     *store.DB
	clock  clock.Clock
	audit  *audit.Writer
	outbox *event.Outbox
	log    *slog.Logger
}

// NewService returns the marina service.
func NewService(db *store.DB, c clock.Clock, aw *audit.Writer, ob *event.Outbox, log *slog.Logger) *Service {
	return &Service{db: db, clock: c, audit: aw, outbox: ob, log: log}
}

func (s *Service) now() time.Time { return store.UTC(s.clock.Now()) }

func toOrganization(o *store.Organization) Organization {
	out := Organization{
		ID: o.ID, Name: o.Name, VATNumber: o.VATNumber, FiscalCode: o.FiscalCode, SDICode: o.SDICode, PEC: o.PEC,
		Version: o.Version, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
	_ = json.Unmarshal(o.BillingAddress, &out.BillingAddress)
	return out
}

func toMarina(m *store.Marina) Marina {
	out := Marina{
		ID: m.ID, OrganizationID: m.OrganizationID, Slug: m.Slug, Name: m.Name, Kind: m.Kind, Timezone: m.Timezone,
		Currency: m.Currency, DefaultLocale: m.DefaultLocale, Locales: slices.Clone([]string(m.Locales)),
		ConcessionRef: m.ConcessionRef, ConcessionExpiresOn: m.ConcessionExpiresOn,
		Settings: jsonObject(m.Settings), Theme: jsonObject(m.Theme), BookingEnabled: m.BookingEnabled,
		AllowedEmbedOrigins: slices.Clone([]string(m.AllowedEmbedOrigins)), Version: m.Version, ArchivedAt: m.ArchivedAt,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	_ = json.Unmarshal(m.Contacts, &out.Contacts)
	_ = json.Unmarshal(m.Address, &out.Address)
	if len(m.Geo) > 0 && string(m.Geo) != "null" {
		var g Geo
		if json.Unmarshal(m.Geo, &g) == nil {
			out.Geo = &g
		}
	}
	return out
}

func jsonObject(j store.JSON) json.RawMessage {
	if len(j) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(j.Compact())
}

// ListOrganizations returns the organizations where the principal has any grant.
func (s *Service) ListOrganizations(ctx context.Context, p *auth.Principal) ([]Organization, error) {
	if p == nil {
		return nil, auth.ErrUnauthenticated
	}
	if p.TOTPEnrolmentRequired {
		return nil, auth.ErrTOTPEnrolmentRequired
	}
	var ids []string
	for _, g := range p.Grants {
		if !slices.Contains(ids, g.OrganizationID) {
			ids = append(ids, g.OrganizationID)
		}
	}
	rows, err := store.OrganizationsByIDs(ctx, s.db.DB, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Organization, 0, len(rows))
	for i := range rows {
		out = append(out, toOrganization(&rows[i]))
	}
	return out, nil
}

// OrganizationPatch lists the fields to change (nil = unchanged).
type OrganizationPatch struct {
	Name           *string
	VATNumber      *string
	FiscalCode     *string
	BillingAddress *Address
	SDICode        *string
	PEC            *string
}

var (
	vatPattern    = regexp.MustCompile(`^[A-Z]{0,2}[0-9A-Z]{8,14}$`)
	fiscalPattern = regexp.MustCompile(`^[0-9A-Z]{11,16}$`)
	sdiPattern    = regexp.MustCompile(`^[0-9A-Z]{7}$`)
)

// UpdateOrganization changes an organization when its version equals ifMatch (organization.manage).
func (s *Service) UpdateOrganization(ctx context.Context, p *auth.Principal, orgID string, ifMatch int, in OrganizationPatch) (*Organization, error) {
	scope, err := p.AuthorizeOrg(ctx, s.db.DB, orgID, auth.PermOrganizationManage)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	var out *Organization
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.GetOrganization(ctx, tx, scope)
		if err != nil {
			return err
		}
		changed := map[string]any{}
		if in.Name != nil {
			if !auth.ValidName(*in.Name) {
				v.Add("name", "invalid")
			}
			o.Name = strings.TrimSpace(*in.Name)
			changed["name"] = o.Name
		}
		setCode := func(field string, dst *string, src *string, re *regexp.Regexp) {
			if src == nil {
				return
			}
			val := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(*src), " ", ""))
			if val != "" && !re.MatchString(val) {
				v.Add(field, "invalid")
			}
			*dst = val
			changed[field] = val
		}
		setCode("vat_number", &o.VATNumber, in.VATNumber, vatPattern)
		setCode("fiscal_code", &o.FiscalCode, in.FiscalCode, fiscalPattern)
		setCode("sdi_code", &o.SDICode, in.SDICode, sdiPattern)
		if in.PEC != nil {
			pec := strings.TrimSpace(*in.PEC)
			if pec != "" {
				if norm, err := auth.NormalizeEmail(pec); err != nil {
					v.Add("pec", "invalid")
				} else {
					pec = norm
				}
			}
			o.PEC = pec
			changed["pec_changed"] = true
		}
		if in.BillingAddress != nil {
			validateAddress(v, "billing_address", in.BillingAddress)
			o.BillingAddress = store.MustJSON(in.BillingAddress)
			changed["billing_address_changed"] = true
		}
		if err := v.Err(); err != nil {
			return err
		}
		o.UpdatedAt = s.now()
		if err := store.UpdateOrganization(ctx, tx, scope, ifMatch, o); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.ID(), Actor: p.Actor(), Action: "organization.update", EntityType: "organization",
			EntityID: scope.ID(), Diff: changed,
		}); err != nil {
			return err
		}
		if _, err := s.outbox.WriteOrg(ctx, tx, scope, event.OrganizationUpdated, event.OrganizationData{OrganizationID: scope.ID()}); err != nil {
			return err
		}
		org := toOrganization(o)
		out = &org
		return nil
	})
	return out, err
}

func validateAddress(v *auth.ValidationError, prefix string, a *Address) {
	for field, val := range map[string]string{
		"line1": a.Line1, "line2": a.Line2, "postal_code": a.PostalCode, "city": a.City, "province": a.Province,
	} {
		if utf8.RuneCountInString(val) > 200 {
			v.Add(prefix+"."+field, "too_long")
		}
	}
	if a.Country != "" && !isUpperAlpha(a.Country, 2) {
		v.Add(prefix+".country", "invalid")
	}
}

func isUpperAlpha(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// ListMarinas returns the active marinas of an organization that the principal can see: all of them with an
// organization-wide grant, otherwise those with a marina-level grant.
func (s *Service) ListMarinas(ctx context.Context, p *auth.Principal, orgID string) ([]Marina, error) {
	if p == nil {
		return nil, auth.ErrUnauthenticated
	}
	if p.TOTPEnrolmentRequired {
		return nil, auth.ErrTOTPEnrolmentRequired
	}
	if !store.ValidID(orgID) || !p.InOrganization(orgID) {
		return nil, auth.ErrNotFound
	}
	scope, err := store.ResolveOrgScope(ctx, s.db.DB, orgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, auth.ErrNotFound
		}
		return nil, err
	}
	var only []string
	if !p.OrgWide(orgID) {
		only = []string{}
		for _, g := range p.Grants {
			if g.OrganizationID == orgID && g.MarinaID != "" {
				only = append(only, g.MarinaID)
			}
		}
	}
	rows, err := store.ListMarinas(ctx, s.db.DB, scope, only)
	if err != nil {
		return nil, err
	}
	out := make([]Marina, 0, len(rows))
	for i := range rows {
		if p.Can(orgID, rows[i].ID, auth.PermMarinaRead) {
			out = append(out, toMarina(&rows[i]))
		}
	}
	return out, nil
}

// CreateMarinaInput is a new marina.
type CreateMarinaInput struct {
	Name, Slug, Kind, Timezone, Currency, DefaultLocale string
	Locales                                             []string
}

// CreateMarina creates a marina in an organization (organization.manage).
func (s *Service) CreateMarina(ctx context.Context, p *auth.Principal, orgID string, in CreateMarinaInput) (*Marina, error) {
	scope, err := p.AuthorizeOrg(ctx, s.db.DB, orgID, auth.PermOrganizationManage)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	in.Kind = orDefault(in.Kind, "marina")
	in.Timezone = orDefault(in.Timezone, "Europe/Rome")
	in.Currency = strings.ToUpper(orDefault(in.Currency, "EUR"))
	in.DefaultLocale = orDefault(in.DefaultLocale, "it")
	if len(in.Locales) == 0 {
		in.Locales = []string{in.DefaultLocale}
	}
	v := &auth.ValidationError{}
	if !auth.ValidName(in.Name) {
		v.Add("name", "invalid")
	}
	if ValidateSlug(in.Slug) != nil {
		v.Add("slug", "invalid")
	}
	validateCommon(v, in.Kind, in.Timezone, in.Currency, in.DefaultLocale, in.Locales)
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *Marina
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		row := auth.NewMarinaRow(scope.ID(), in.Name, in.Slug, in.Timezone, in.DefaultLocale, now)
		row.Kind, row.Currency, row.Locales = in.Kind, in.Currency, store.Strings(dedupe(in.Locales))
		if err := store.InsertMarina(ctx, tx, row); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrSlugTaken
			}
			return err
		}
		ms, err := store.ResolveMarinaScope(ctx, tx, row.ID)
		if err != nil {
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.ID(), MarinaID: row.ID, Actor: p.Actor(), Action: "marina.create",
			EntityType: "marina", EntityID: row.ID, Diff: map[string]any{"slug": row.Slug, "kind": row.Kind},
		}); err != nil {
			return err
		}
		if _, err := s.outbox.Write(ctx, tx, &ms, event.MarinaCreated, event.MarinaData{MarinaID: row.ID}); err != nil {
			return err
		}
		m := toMarina(row)
		out = &m
		return nil
	})
	return out, err
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func validateCommon(v *auth.ValidationError, kind, tz, currency, defLocale string, locales []string) {
	if !slices.Contains(Kinds, kind) {
		v.Add("kind", "invalid")
	}
	if _, err := calendar.LoadLocation(tz); err != nil || tz == "" || tz == "Local" {
		v.Add("timezone", "invalid")
	}
	if !isUpperAlpha(currency, 3) {
		v.Add("currency", "invalid")
	}
	for _, l := range locales {
		if !auth.ValidLocale(l) {
			v.Add("locales", "invalid")
			break
		}
	}
	if !auth.ValidLocale(defLocale) || !slices.Contains(locales, defLocale) {
		v.Add("default_locale", "invalid")
	}
}

// GetMarina returns a marina (marina.read).
func (s *Service) GetMarina(ctx context.Context, p *auth.Principal, marinaID string) (*Marina, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetMarina(ctx, s.db.DB, scope)
	if err != nil {
		return nil, err
	}
	m := toMarina(row)
	return &m, nil
}

// MarinaPatch lists the fields to change (nil = unchanged). ClearGeo and ClearConcessionExpiresOn set the
// nullable fields to null.
type MarinaPatch struct { //nolint:revive // name fixed by docs/m0-plan.md §1.2
	Name                     *string
	Kind                     *string
	Timezone                 *string
	Currency                 *string
	DefaultLocale            *string
	Locales                  *[]string
	Contacts                 *Contacts
	Address                  *Address
	Geo                      *Geo
	ClearGeo                 bool
	ConcessionRef            *string
	ConcessionExpiresOn      *calendar.Date
	ClearConcessionExpiresOn bool
	Settings                 *json.RawMessage
	Theme                    *json.RawMessage
	BookingEnabled           *bool
	AllowedEmbedOrigins      *[]string
}

// Maximum size of the free-form settings and theme documents.
const maxDocumentBytes = 64 << 10

var originPattern = regexp.MustCompile(`^https?://[a-z0-9.-]+(:[0-9]{1,5})?$`)

// UpdateMarina changes a marina when its version equals ifMatch (marina.configure).
func (s *Service) UpdateMarina(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in MarinaPatch) (*Marina, error) {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out *Marina
	err = s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return err
		}
		changed, err := applyMarinaPatch(row, in)
		if err != nil {
			return err
		}
		row.UpdatedAt = s.now()
		if err := store.UpdateMarina(ctx, tx, scope, ifMatch, row); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "marina.update",
			EntityType: "marina", EntityID: scope.ID(), Diff: map[string]any{"fields": changed},
		}); err != nil {
			return err
		}
		if _, err := s.outbox.Write(ctx, tx, &scope, event.MarinaUpdated, event.MarinaData{MarinaID: scope.ID()}); err != nil {
			return err
		}
		m := toMarina(row)
		out = &m
		return nil
	})
	return out, err
}

// applyMarinaPatch validates in and applies it to row; it returns the names of the changed fields.
func applyMarinaPatch(row *store.Marina, in MarinaPatch) ([]string, error) {
	v := &auth.ValidationError{}
	var changed []string
	mark := func(f string) { changed = append(changed, f) }
	if in.Name != nil {
		if !auth.ValidName(*in.Name) {
			v.Add("name", "invalid")
		}
		row.Name = strings.TrimSpace(*in.Name)
		mark("name")
	}
	if in.Kind != nil {
		row.Kind = *in.Kind
		mark("kind")
	}
	if in.Timezone != nil {
		row.Timezone = *in.Timezone
		mark("timezone")
	}
	if in.Currency != nil {
		row.Currency = strings.ToUpper(*in.Currency)
		mark("currency")
	}
	if in.DefaultLocale != nil {
		row.DefaultLocale = *in.DefaultLocale
		mark("default_locale")
	}
	if in.Locales != nil {
		row.Locales = store.Strings(dedupe(*in.Locales))
		mark("locales")
	}
	validateCommon(v, row.Kind, row.Timezone, row.Currency, row.DefaultLocale, row.Locales)
	if in.Contacts != nil {
		c := *in.Contacts
		c.Email = strings.TrimSpace(c.Email)
		if c.Email != "" {
			if norm, err := auth.NormalizeEmail(c.Email); err != nil {
				v.Add("contacts.email", "invalid")
			} else {
				c.Email = norm
			}
		}
		for f, val := range map[string]string{"phone": c.Phone, "website": c.Website, "vhf_channel": c.VHFChannel} {
			if utf8.RuneCountInString(val) > 200 {
				v.Add("contacts."+f, "too_long")
			}
		}
		row.Contacts = store.MustJSON(c)
		mark("contacts")
	}
	if in.Address != nil {
		validateAddress(v, "address", in.Address)
		row.Address = store.MustJSON(in.Address)
		mark("address")
	}
	switch {
	case in.ClearGeo:
		row.Geo = nil
		mark("geo")
	case in.Geo != nil:
		if in.Geo.LatE7 < -900_000_000 || in.Geo.LatE7 > 900_000_000 {
			v.Add("geo.lat_e7", "out_of_range")
		}
		if in.Geo.LonE7 < -1_800_000_000 || in.Geo.LonE7 > 1_800_000_000 {
			v.Add("geo.lon_e7", "out_of_range")
		}
		row.Geo = store.MustJSON(in.Geo)
		mark("geo")
	}
	if in.ConcessionRef != nil {
		if utf8.RuneCountInString(*in.ConcessionRef) > 200 {
			v.Add("concession_ref", "too_long")
		}
		row.ConcessionRef = strings.TrimSpace(*in.ConcessionRef)
		mark("concession_ref")
	}
	switch {
	case in.ClearConcessionExpiresOn:
		row.ConcessionExpiresOn = calendar.Date{}
		mark("concession_expires_on")
	case in.ConcessionExpiresOn != nil:
		row.ConcessionExpiresOn = *in.ConcessionExpiresOn
		mark("concession_expires_on")
	}
	if in.Settings != nil {
		if doc, ok := jsonDocument(*in.Settings); ok {
			row.Settings = doc
		} else {
			v.Add("settings", "invalid")
		}
		mark("settings")
	}
	if in.Theme != nil {
		if doc, ok := jsonDocument(*in.Theme); ok {
			row.Theme = doc
		} else {
			v.Add("theme", "invalid")
		}
		mark("theme")
	}
	if in.BookingEnabled != nil {
		row.BookingEnabled = *in.BookingEnabled
		mark("booking_enabled")
	}
	if in.AllowedEmbedOrigins != nil {
		origins := make([]string, 0, len(*in.AllowedEmbedOrigins))
		for _, o := range *in.AllowedEmbedOrigins {
			o = strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
			if !originPattern.MatchString(o) {
				v.Add("allowed_embed_origins", "invalid")
				break
			}
			origins = append(origins, o)
		}
		if len(origins) > 20 {
			v.Add("allowed_embed_origins", "too_many")
		}
		row.AllowedEmbedOrigins = store.Strings(dedupe(origins))
		mark("allowed_embed_origins")
	}
	return changed, v.Err()
}

// jsonDocument accepts a JSON object of bounded size and returns it compacted.
func jsonDocument(raw json.RawMessage) (store.JSON, bool) {
	if len(raw) > maxDocumentBytes {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// ArchiveMarina archives a marina (organization.manage on its organization). Archived marinas disappear from
// every listing and request path.
func (s *Service) ArchiveMarina(ctx context.Context, p *auth.Principal, marinaID string) error {
	scope, err := p.Authorize(ctx, s.db.DB, marinaID, auth.PermOrganizationManage)
	if err != nil {
		return err
	}
	return s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.ArchiveMarina(ctx, tx, scope, s.now()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrAlreadyArchived
			}
			return err
		}
		if err := s.audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "marina.archive",
			EntityType: "marina", EntityID: scope.ID(),
		}); err != nil {
			return err
		}
		_, err := s.outbox.Write(ctx, tx, &scope, event.MarinaArchived, event.MarinaData{MarinaID: scope.ID()})
		return err
	})
}

// PublicBySlug returns the public card of an active marina.
func (s *Service) PublicBySlug(ctx context.Context, slugValue string) (*PublicMarina, error) {
	if ValidateSlug(slugValue) != nil {
		return nil, auth.ErrNotFound
	}
	m, err := store.MarinaBySlug(ctx, s.db.DB, slugValue)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, auth.ErrNotFound
		}
		return nil, err
	}
	return &PublicMarina{
		Slug: m.Slug, Name: m.Name, DefaultLocale: m.DefaultLocale, Locales: slices.Clone([]string(m.Locales)),
		BookingEnabled: m.BookingEnabled,
	}, nil
}
