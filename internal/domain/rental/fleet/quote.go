package fleet

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Problem slugs of a quote option that cannot be offered.
const (
	ProblemNoPrice            = "no_price"
	ProblemSkipperRequired    = "skipper_required"
	ProblemSkipperUnavailable = "skipper_unavailable"
	ProblemUnitNotBookable    = "unit_not_bookable"
	ProblemRangeMismatch      = slots.CodeFormulaRangeMismatch
)

// FormulaTime returns the time part of a formula for the pure slots package.
func FormulaTime(f *store.RentalFormula) slots.Formula {
	out := slots.Formula{Kind: f.Kind, Band: f.BandCode, MinSlots: f.MinSlots, MinDays: f.MinDays, MaxDays: f.MaxDays}
	if f.MaxSlots != nil {
		out.MaxSlots = *f.MaxSlots
	}
	return out
}

// SkipperProblem returns the problem slug of a formula for the skipper choice ("" when acceptable): required
// without a skipper, or a skipper on a formula that offers none.
func SkipperProblem(f *store.RentalFormula, withSkipper bool) string {
	switch {
	case f.Skipper == store.RentalSkipperRequired && !withSkipper:
		return ProblemSkipperRequired
	case f.Skipper == store.RentalSkipperNone && withSkipper:
		return ProblemSkipperUnavailable
	}
	return ""
}

// formulaFits reports whether a formula serves a unit category (empty categories = every category).
func formulaFits(f *store.RentalFormula, category string) bool {
	c := FormulaCategories(f)
	return len(c) == 0 || slices.Contains(c, category)
}

// catalogsTx loads the active price lists of the marina whose validity intersects [from, to], with the given items
// attached (pure engine input).
func catalogsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, from, to calendar.Date, itemIDs []string) ([]pricing.Catalog, error) {
	actives, err := store.ActivePriceLists(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	cats := make([]pricing.Catalog, 0, len(actives))
	for i := range actives {
		l := &actives[i]
		if to.Before(l.ValidFrom) || from.After(l.ValidTo) {
			continue
		}
		cat, err := pricelist.LoadCatalog(ctx, tx, scope, l.ID, itemIDs...)
		if err != nil {
			return nil, err
		}
		cats = append(cats, cat)
	}
	return cats, nil
}

// pricingCtx is what pricing a unit and range needs, loaded once per quote.
type pricingCtx struct {
	unit     *store.RentalUnit
	settings *Settings
	kw       int64
	extras   []pricing.RentalExtra
	category string
	channel  pricing.Channel
	cats     []pricing.Catalog
}

// loadPricing loads the unit, the settings, the extras (validated: active, category, quantity) and the catalogues
// covering the range.
func (s *Service) loadPricing(ctx context.Context, tx bun.IDB, scope store.MarinaScope, unitID string, r slots.Range,
	extras []ExtraQty, customerID, channel string, withSkipper bool,
) (*pricingCtx, error) {
	u, err := store.GetRentalUnit(ctx, tx, scope, unitID)
	if err != nil {
		return nil, notFound(err)
	}
	set, err := s.LoadSettingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	refs, err := store.LoadRentalUnitRefs(ctx, tx, scope, []store.RentalUnit{*u})
	if err != nil {
		return nil, err
	}
	pc := &pricingCtx{unit: u, settings: set, kw: store.EngineKWMilli(refs.Engines[u.VesselID]), channel: pricing.ChannelDesk}
	if channel != "" {
		pc.channel = pricing.Channel(channel)
	}
	if customerID != "" {
		if pc.category, err = store.CustomerCategoryByID(ctx, tx, scope, customerID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				v := &auth.ValidationError{}
				v.Add("customer_id", "not_found")
				return nil, v
			}
			return nil, err
		}
	}
	items := []string{u.ItemID}
	if withSkipper && set.Row.SkipperItemID != "" {
		items = append(items, set.Row.SkipperItemID)
	}
	v := &auth.ValidationError{}
	for _, x := range extras {
		e, err := store.GetRentalExtra(ctx, tx, scope, x.ExtraID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				v.Add("extras.extra_id", "not_found")
				continue
			}
			return nil, err
		}
		cats := decodeStrings(e.Categories)
		if !e.Active || (len(cats) > 0 && !slices.Contains(cats, u.Category)) {
			v.Add("extras.extra_id", "not_available")
			continue
		}
		q := x.Qty
		if q == 0 {
			q = qty.Milli(e.DefaultQtyMilli)
		}
		if q <= 0 || int64(q) > e.MaxQtyMilli {
			v.Add("extras.qty_milli", "out_of_range")
			continue
		}
		pc.extras = append(pc.extras, pricing.RentalExtra{ItemID: e.ItemID, Qty: q, PerDay: e.ChargeMode == store.RentalExtraPerDay})
		items = append(items, e.ItemID)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if pc.cats, err = catalogsTx(ctx, tx, scope, r.Start.Date, r.End.Date, items); err != nil {
		return nil, err
	}
	return pc, nil
}

// snapshot validates the range for a formula and prices it. It returns a *slots.RangeError, a
// *pricing.NoPriceError, or ErrNoPrice when a skipper is asked and the settings have no skipper item.
func (pc *pricingCtx) snapshot(f *store.RentalFormula, r slots.Range, withSkipper bool, today calendar.Date) (price.Snapshot, error) {
	q, err := slots.ValidateRange(FormulaTime(f), pc.settings.Slots(), r)
	if err != nil {
		return price.Snapshot{}, err
	}
	skipperItem := ""
	if withSkipper {
		if pc.settings.Row.SkipperItemID == "" {
			return price.Snapshot{}, ErrNoPrice
		}
		skipperItem = pc.settings.Row.SkipperItemID
	}
	in := price.SnapshotInput{
		UnitID: pc.unit.ID, UnitItemID: pc.unit.ItemID, FormulaID: f.ID, FormulaCode: f.Code, Kind: pricing.RentalKind(f.Kind),
		Start: r.Start.Date, Days: q.Days, Band: q.Band, SkipperItemID: skipperItem, Extras: pc.extras, Channel: pc.channel,
		CustomerCategory: pc.category, QuotedOn: today,
		LicenceRequired: price.LicenceRequired(pc.unit.Licence, pc.kw, pc.settings.Row.LicenceThresholdKWMilli, withSkipper),
	}
	if f.Kind == store.RentalKindHourly {
		in.Slots = q.Slots
	}
	return price.Build(pc.cats, in)
}

// problemOf returns the slug of a quote error, or "" when the error is not a refusal.
func problemOf(err error) string {
	var re *slots.RangeError
	switch {
	case errors.As(err, &re):
		return re.Code
	case errors.Is(err, pricing.ErrNoPrice), errors.Is(err, ErrNoPrice):
		return ProblemNoPrice
	}
	return ""
}

// Quote prices a unit and range for one formula, or every formula sorted by total (stateless).
func (s *Service) Quote(ctx context.Context, p *auth.Principal, marinaID string, in QuoteInput) ([]QuoteOption, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	if in.UnitID == "" || !slots.WellFormed(in.Range) {
		v := &auth.ValidationError{}
		if in.UnitID == "" {
			v.Add("unit_id", "required")
		} else {
			v.Add("end", "invalid")
		}
		return nil, v
	}
	db := s.d.DB.DB
	withSkipper := in.WithSkipper || in.SkipperID != ""
	if in.SkipperID != "" {
		if _, err := store.GetRentalSkipper(ctx, db, scope, in.SkipperID); err != nil {
			return nil, notFound(err)
		}
	}
	var formulas []store.RentalFormula
	if in.FormulaID != "" {
		f, err := store.GetRentalFormula(ctx, db, scope, in.FormulaID)
		if err != nil {
			return nil, notFound(err)
		}
		formulas = []store.RentalFormula{*f}
	}
	pc, err := s.loadPricing(ctx, db, scope, in.UnitID, in.Range, in.Extras, in.CustomerID, "", withSkipper)
	if err != nil {
		return nil, err
	}
	if in.FormulaID == "" {
		active := true
		all, err := store.ListRentalFormulas(ctx, db, scope, &active)
		if err != nil {
			return nil, err
		}
		for _, f := range all {
			if formulaFits(&f, pc.unit.Category) {
				formulas = append(formulas, f)
			}
		}
	}
	loc := s.location(ctx, db, scope)
	today := calendar.In(s.now(), loc)
	out := make([]QuoteOption, 0, len(formulas))
	for i := range formulas {
		f := &formulas[i]
		opt := QuoteOption{FormulaID: f.ID, FormulaCode: f.Code, Kind: f.Kind}
		switch {
		case pc.unit.Status != store.RentalUnitAvailable:
			opt.Problem = ProblemUnitNotBookable
		case !f.Active || !formulaFits(f, pc.unit.Category):
			opt.Problem = ProblemRangeMismatch
		default:
			opt.Problem = SkipperProblem(f, withSkipper)
		}
		if opt.Problem == "" {
			snap, err := pc.snapshot(f, in.Range, withSkipper, today)
			if err != nil {
				if opt.Problem = problemOf(err); opt.Problem == "" {
					return nil, err
				}
			} else {
				opt.OK, opt.Snapshot = true, snap
			}
		}
		out = append(out, opt)
	}
	slices.SortStableFunc(out, func(a, b QuoteOption) int {
		if a.OK != b.OK {
			if a.OK {
				return -1
			}
			return 1
		}
		if a.OK {
			return cmp.Compare(a.Snapshot.Total(), b.Snapshot.Total())
		}
		return 0
	})
	return out, nil
}

// SnapshotTx prices a booking in its transaction (seam for reservation: create, reschedule, extend). The range is
// validated again against the formula (a *slots.RangeError); a missing price is a *pricing.NoPriceError, or
// ErrNoPrice when a skipper is booked and the settings have no skipper item. Unit status, skipper mode, skipper
// hours and licence are the caller's checks (SkipperProblem helps).
func (s *Service) SnapshotTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in SnapshotRequest) (price.Snapshot, error) {
	f, err := store.GetRentalFormula(ctx, tx, scope, in.FormulaID)
	if err != nil {
		return price.Snapshot{}, notFound(err)
	}
	withSkipper := in.SkipperID != ""
	pc, err := s.loadPricing(ctx, tx, scope, in.UnitID, in.Range, in.Extras, in.CustomerID, in.Channel, withSkipper)
	if err != nil {
		return price.Snapshot{}, err
	}
	return pc.snapshot(f, in.Range, withSkipper, calendar.In(s.now(), s.location(ctx, tx, scope)))
}
