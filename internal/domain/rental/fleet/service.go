// Package fleet is the rental fleet (spec 19.1-19.2, m11-plan §3, §5): units linked to registry vessels,
// skippers and their weekly hours, formulas, extras and settings, the stateless quote and the price snapshot seam
// used by reservation. Units and skippers are operations (rental.manage); formulas, extras and settings are
// configuration (marina.configure, D20). Prices come only from the M2 price lists through pricing.RentalQuote.
// Writes carry an audit row in the same transaction; configuration changes emit no outbox event (rental events
// are about bookings). Skipper names and contacts never reach audit diffs, logs or events (INV-70).
package fleet

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// PreauthChecker reports whether an enabled provider supports pre-authorisation (collect.Service).
type PreauthChecker interface {
	PreauthAvailable(ctx context.Context, db bun.IDB, scope store.MarinaScope) (bool, error)
}

// Deps are the collaborators of the service.
type Deps struct {
	DB      *store.DB
	Clock   clock.Clock
	Audit   *audit.Writer
	Outbox  *event.Outbox
	Log     *slog.Logger
	Keys    *crypto.KeyRing
	Prices  *pricelist.Service
	Preauth PreauthChecker
}

// Service is the rental fleet service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// versionErr maps the store version mismatch to the package sentinel.
func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// authorize checks perm on the marina (another marina is auth.ErrNotFound) and, for each id, that the row exists
// in the marina (table, id pairs), so another marina's ids are 404 before anything else.
func (s *Service) authorize(ctx context.Context, p *auth.Principal, marinaID string, perm auth.Permission, refs ...string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	if err != nil {
		return scope, err
	}
	for i := 0; i+1 < len(refs); i += 2 {
		if err := store.RentalExists(ctx, s.d.DB.DB, scope, refs[i], refs[i+1]); err != nil {
			return scope, notFound(err)
		}
	}
	return scope, nil
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	if s.d.Audit == nil {
		return nil
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

// ---- shared validation ----

var (
	unitCodeRe    = regexp.MustCompile(`^[A-Z0-9_-]{1,16}$`)
	formulaCodeRe = regexp.MustCompile(`^[A-Z0-9_-]{1,24}$`)
	bandCodeRe    = regexp.MustCompile(`^[a-z0-9_]{1,16}$`)
	keyRe         = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
)

// Enumerations (CHECKs of 00023).
var (
	categories    = []string{store.RentalCategoryRIB, store.RentalCategoryOpen, store.RentalCategoryCabin, store.RentalCategorySail, store.RentalCategoryPedalo, store.RentalCategorySUP}
	licenceModes  = []string{store.RentalLicenceNever, store.RentalLicenceAlways, store.RentalLicenceThreshold}
	fuelTypes     = []string{store.RentalFuelPetrol, store.RentalFuelDiesel, store.RentalFuelElectric, store.RentalFuelNone}
	unitStatuses  = []string{store.RentalUnitAvailable, store.RentalUnitMaintenance, store.RentalUnitRetired}
	ownerships    = []string{store.RentalOwnershipMarina, store.RentalOwnershipThirdParty}
	formulaKinds  = []string{store.RentalKindHourly, store.RentalKindHalfDay, store.RentalKindDay, store.RentalKindMultiDay, store.RentalKindWeek}
	skipperModes  = []string{store.RentalSkipperNone, store.RentalSkipperOptional, store.RentalSkipperRequired}
	fuelModes     = []string{store.RentalFuelIncluded, store.RentalFuelMetered}
	chargeModes   = []string{store.RentalExtraPerRental, store.RentalExtraPerDay}
	depositModes  = []string{"cash", "card_pos", "preauth"}
	lateSteps     = []int{15, 30, 60}
	revenueRental = "rental"
)

// Categories returns the unit categories.
func Categories() []string { return slices.Clone(categories) }

func validCategories(v *auth.ValidationError, field string, list []string) {
	for _, c := range list {
		if !slices.Contains(categories, c) {
			v.Add(field, "invalid")
			return
		}
	}
}

func validI18n(v *auth.ValidationError, field string, m map[string]string, maxLen int) {
	if len([]rune(m["it"])) == 0 {
		v.Add(field, "required")
		return
	}
	for k, t := range m {
		if len(k) != 2 || len([]rune(t)) > maxLen {
			v.Add(field, "invalid")
			return
		}
	}
}

// itemCheck validates an item reference: it must exist in the marina and, when rental is set, belong to the
// revenue class rental (D3).
func itemCheck(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *auth.ValidationError, field, id string, rental bool) error {
	if id == "" {
		return nil
	}
	code, err := store.ItemRevenueClassCode(ctx, db, scope, id)
	if errors.Is(err, store.ErrNotFound) {
		v.Add(field, "not_found")
		return nil
	}
	if err != nil {
		return err
	}
	if rental && code != revenueRental {
		v.Add(field, "not_rental")
	}
	return nil
}

// location returns the marina time zone (UTC when unknown).
func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
