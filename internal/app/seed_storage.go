package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/storage"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedStorage adds the M9 storage demo (m9-plan §15.2): a storage price list, storage contracts in the shed and
// the yard placed by the optimizer with launch dates spread over the spring, and one draft scenario of the shed.
// A no-op when the marina already has storage contracts.
func (a *App) seedStorage(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Storage == nil {
		return nil
	}
	db := a.DB.DB
	scope, err := store.ResolveMarinaScope(ctx, db, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := store.CountStorageContracts(ctx, db, scope); err != nil || n > 0 {
		return err
	}
	areas, err := store.StorageAreas(ctx, db, scope, "")
	if err != nil || len(areas) == 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	m := res.MarinaID
	if err := a.seedStorageList(ctx, p, m, today); err != nil {
		return err
	}
	vessels, err := store.AllVessels(ctx, db, scope)
	if err != nil {
		return err
	}
	owners, err := (&seedOcc{a: a, ctx: ctx}).owners(scope, vessels)
	if err != nil {
		return err
	}
	// Boats without a stay today, smaller first, so that the demo sheds fill with plausible winter guests.
	busy := map[string]bool{}
	allocs, err := store.ListAllocations(ctx, db, scope, store.AllocationFilter{Status: store.AllocActive, From: today, To: today.AddDays(180)})
	if err != nil {
		return err
	}
	for _, al := range allocs {
		busy[al.VesselID] = true
	}
	var pool []store.Vessel
	for _, v := range vessels {
		if !busy[v.ID] && owners[v.ID] != "" && v.LOACm >= 500 && v.LOACm <= 1400 && v.BeamCm > 0 && v.BeamCm <= 450 {
			pool = append(pool, v)
		}
	}
	slices.SortFunc(pool, func(x, y store.Vessel) int { return int(x.LOACm*1000+x.BeamCm) - int(y.LOACm*1000+y.BeamCm) })
	starts, ends := today, today.AddDays(200)
	quota := map[string]int{"shed": 10, "yard": 8}
	next := 0
	for _, ar := range areas {
		for k := 0; k < quota[ar.MapKind] && next < len(pool); next++ {
			v := pool[next]
			launch := today.AddDays(120 + 4*k)
			_, err := a.Storage.CreateContract(ctx, p, m, storage.ContractInput{
				Term: contract.TermPeriod, CustomerID: owners[v.ID], VesselID: v.ID, StartsOn: starts, EndsOn: &ends, AreaID: ar.ID,
				Activate: true, Details: contract.StorageDetailsInput{Support: store.StorageSupportCradle, LaunchOn: &launch},
			})
			var fe *occupancy.FitError
			switch {
			case errors.Is(err, storage.ErrNoSpace):
				k = quota[ar.MapKind]
				continue
			case errors.As(err, &fe):
				continue
			case err != nil:
				return fmt.Errorf("storage contract: %w", err)
			}
			res.Contracts++
			k++
		}
	}
	for _, ar := range areas {
		if ar.MapKind != "shed" {
			continue
		}
		if _, err := a.Storage.CreateScenario(ctx, p, m, storage.ScenarioInput{
			AreaID: ar.ID, Name: "Varo di primavera", From: today, To: today.AddDays(199),
		}); err != nil && !errors.Is(err, storage.ErrTargetInvalid) {
			return fmt.Errorf("storage scenario: %w", err)
		}
	}
	return nil
}

// seedStorageList creates and activates the storage price list of the year (flat per month by price class).
func (a *App) seedStorageList(ctx context.Context, p *auth.Principal, m string, today calendar.Date) error {
	classes, err := a.Catalog.ListRevenueClasses(ctx, p, m, true)
	if err != nil {
		return err
	}
	if len(classes) == 0 {
		return nil
	}
	rc := classes[0].ID
	for _, c := range classes {
		if c.Code == "storage" {
			rc = c.ID
		}
	}
	incl, vat := true, int64(2200)
	en := "Dry storage (month)"
	it, err := a.Catalog.CreateItem(ctx, p, m, catalog.ItemInput{
		Code: "RIM-MESE", Name: catalog.I18nText{It: "Rimessaggio (mese)", En: &en}, ItemType: "berth_fee", Unit: "month",
		VATRateBP: &vat, PriceIncludesVAT: &incl, RevenueClassID: rc,
	})
	if err != nil {
		return fmt.Errorf("storage item: %w", err)
	}
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractStorage: it.ID}
	set.Rounding = map[pricing.ContractType]pricing.TotalRounding{pricing.AnyContractType: pricing.RoundEuro}
	types := []pricing.ContractType{pricing.ContractStorage}
	amount := func(c int64) *money.Cents { v := money.Cents(c); return &v }
	y := today.Year()
	doc := pricelist.SaveRequest{
		Code: "RIM-" + strconv.Itoa(y), Name: catalog.I18nText{It: "Rimessaggio " + strconv.Itoa(y)},
		ValidFrom: calendar.New(y, 1, 1), ValidTo: calendar.New(y+1, 12, 31), ContractTypes: types, Settings: set,
		Rules: []pricelist.Rule{
			{Key: "rim-scoperto", Rule: pricing.Rule{
				Stage: pricing.StageBase, Name: "Piazzale", ItemID: it.ID, Basis: pricing.BasisFlat, Unit: pricing.UnitMonth,
				AmountCents: amount(18000), Scope: pricing.Scope{ContractTypes: types},
			}},
		},
	}
	l, err := a.PriceLists.Create(ctx, p, m, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		return fmt.Errorf("storage list: %w", err)
	}
	saved, err := a.PriceLists.Save(ctx, p, m, l.ID, l.Version, doc)
	if err != nil {
		return fmt.Errorf("storage list save: %w", err)
	}
	if _, err := a.PriceLists.Activate(ctx, p, m, l.ID, saved.Document.Version, calendar.Date{}); err != nil {
		return fmt.Errorf("storage list activate: %w", err)
	}
	return nil
}
