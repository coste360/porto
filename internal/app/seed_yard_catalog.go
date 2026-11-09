package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedYardCatalog adds the M10 demo of QUOTE (m10-plan §16.2): the §4.4 catalogue (work types, qualifications,
// items with yard settings and prices, hull_area, the ANTIVEG package) and ten quotes (see below). Everything goes through the
// services as the demo owner; a no-op when the marina already has work types.
//
// The yard prices live in the active price list CANT-<year>. Creating a list needs a contract type, so it is
// created for charter and then saved with no contract type: it serves no mooring quote (no overlap with the M2
// lists, no effect on mooring prices or reports), and the unit prices of yard items ignore contract-type scopes (D1).
func (a *App) seedYardCatalog(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.YardQuotes == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if wts, err := store.ListWorkTypes(ctx, a.DB.DB, scope, nil); err != nil || len(wts) > 0 {
		return err
	}
	m := res.MarinaID
	en := func(s string) *string { return &s }
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	y := today.Year()

	// Work types.
	wt := map[string]string{}
	for i, w := range []struct{ code, it, en string }{
		{"ANTIFOULING", "Antivegetativa", "Antifouling"}, {"HULL", "Carena", "Hull"}, {"MECH", "Meccanica", "Mechanics"},
	} {
		order := i
		row, err := a.YardQuotes.CreateWorkType(ctx, p, m, yardquote.WorkTypeInput{Code: w.code, Name: yardquote.I18n{It: w.it, En: en(w.en)}, SortOrder: &order})
		if err != nil {
			return fmt.Errorf("work type %s: %w", w.code, err)
		}
		wt[w.code] = row.ID
	}

	// Items.
	classes, err := a.Catalog.ListRevenueClasses(ctx, p, m, true)
	if err != nil {
		return err
	}
	classID := map[string]string{}
	for _, c := range classes {
		classID[c.Code] = c.ID
	}
	items := map[string]string{}
	vat, excl := int64(2200), false
	for _, it := range []struct {
		code, typ, unit, class, it, en string
	}{
		{"SRV-ALAG", "service", "m", "services", "Alaggio", "Haul-out"},
		{"SRV-LAV", "service", "m2", "services", "Lavaggio carena", "Hull wash"},
		{"LAB-HAND", "labour", "hour", "yard_labour", "Ora operaio", "Yard hand hour"},
		{"LAB-PAINT", "labour", "hour", "yard_labour", "Ora verniciatore", "Painter hour"},
		{"MAT-AVEG", "material", "piece", "yard_materials", "Antivegetativa 2,5 l", "Antifouling 2.5 l tin"},
		{"SRV-VARO", "service", "m", "services", "Varo", "Launch"},
		{"LAB-LIFT", "labour", "hour", "yard_labour", "Ora gruista", "Lift operator hour"},
	} {
		created, err := a.Catalog.CreateItem(ctx, p, m, catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.it, En: en(it.en)}, ItemType: it.typ, Unit: it.unit, VATRateBP: &vat,
			PriceIncludesVAT: &excl, RevenueClassID: classID[it.class],
		})
		if err != nil {
			return fmt.Errorf("item %s: %w", it.code, err)
		}
		items[it.code] = created.ID
		res.Items++
	}

	// Price list CANT-Y with flat base rules (§4.4 unit prices).
	cents := func(c int64) *money.Cents { v := money.Cents(c); return &v }
	doc := pricelist.SaveRequest{
		Code: "CANT-" + strconv.Itoa(y), Name: catalog.I18nText{It: "Cantiere " + strconv.Itoa(y), En: en("Yard " + strconv.Itoa(y))},
		ValidFrom: calendar.New(y, 1, 1), ValidTo: calendar.New(y+1, 12, 31), ContractTypes: []pricing.ContractType{},
		Settings: pricing.DefaultSettings(),
	}
	for _, r := range []struct {
		code  string
		cents int64
	}{{"SRV-ALAG", 1800}, {"SRV-LAV", 400}, {"LAB-HAND", 3800}, {"LAB-PAINT", 4500}, {"MAT-AVEG", 9500}, {"SRV-VARO", 1800}, {"LAB-LIFT", 5000}} {
		doc.Rules = append(doc.Rules, pricelist.Rule{Key: "yard-" + r.code, Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: r.code, ItemID: items[r.code], Basis: pricing.BasisFlat, Unit: pricing.UnitStay,
			AmountCents: cents(r.cents),
		}})
	}
	l, err := a.PriceLists.Create(ctx, p, m, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo,
		ContractTypes: []pricing.ContractType{pricing.ContractCharter},
	})
	if err != nil {
		return fmt.Errorf("yard list: %w", err)
	}
	saved, err := a.PriceLists.Save(ctx, p, m, l.ID, l.Version, doc)
	if err != nil {
		return fmt.Errorf("yard list save: %w", err)
	}
	if _, err := a.PriceLists.Activate(ctx, p, m, l.ID, saved.Document.Version, calendar.Date{}); err != nil {
		return fmt.Errorf("yard list activate: %w", err)
	}
	res.PriceLists++
	res.PriceRules += len(doc.Rules)

	// Qualifications (labour items linked) and yard settings of the other items.
	_, err = a.YardQuotes.CreateQualification(ctx, p, m, yardquote.QualificationInput{
		Code: "hand", Name: yardquote.I18n{It: "Operaio", En: en("Yard hand")}, LabourItemID: items["LAB-HAND"], DefaultCostCents: ptrOf(int64(2200)),
	})
	if err != nil {
		return fmt.Errorf("qualification hand: %w", err)
	}
	if _, err := a.YardQuotes.CreateQualification(ctx, p, m, yardquote.QualificationInput{
		Code: "painter", Name: yardquote.I18n{It: "Verniciatore", En: en("Painter")}, LabourItemID: items["LAB-PAINT"], DefaultCostCents: ptrOf(int64(2600)),
	}); err != nil {
		return fmt.Errorf("qualification painter: %w", err)
	}
	if _, err := a.YardQuotes.CreateQualification(ctx, p, m, yardquote.QualificationInput{
		Code: "lift", Name: yardquote.I18n{It: "Gruista", En: en("Lift operator")}, LabourItemID: items["LAB-LIFT"],
		DefaultCostCents: ptrOf(int64(2800)), LiftOperator: ptrOf(true),
	}); err != nil {
		return fmt.Errorf("qualification lift: %w", err)
	}
	for code, st := range map[string]struct {
		cost     int64
		category string
		qf       string
	}{
		"SRV-ALAG": {0, "Movimentazione", "loa"}, "SRV-LAV": {100, "Carena", "round(loa * beam, 1)"},
		"MAT-AVEG": {5800, "Vernici", "1"}, "SRV-VARO": {0, "Movimentazione", "loa"},
	} {
		cost, cat, qf := st.cost, st.category, st.qf
		if _, err := a.YardQuotes.PutItemSettings(ctx, p, m, items[code], 0, yardquote.ItemSettingsInput{
			CostCents: &cost, Category: &cat, QtyFormula: &qf,
		}); err != nil {
			return fmt.Errorf("item settings %s: %w", code, err)
		}
	}

	// Variables: hull_area (derived) and layers (job).
	if _, err := a.YardQuotes.CreateVariable(ctx, p, m, yardquote.VariableInput{
		Name: "hull_area", Source: store.FormulaSourceDerived, Kind: store.FormulaKindNumber, Unit: "m2",
		Expression: "loa * (beam + draft) * 0.85", Label: &yardquote.I18n{It: "Superficie carena", En: en("Hull area")},
	}); err != nil {
		return fmt.Errorf("variable hull_area: %w", err)
	}
	lo, hi, def := qty.MustParse("1"), qty.MustParse("4"), qty.MustParse("2")
	if _, err := a.YardQuotes.CreateVariable(ctx, p, m, yardquote.VariableInput{
		Name: "layers", Source: store.FormulaSourceJob, Kind: store.FormulaKindNumber, Unit: "pcs", Min: &lo, Max: &hi, Default: &def,
		Label: &yardquote.I18n{It: "Mani di antivegetativa", En: en("Antifouling coats")},
	}); err != nil {
		return fmt.Errorf("variable layers: %w", err)
	}

	// Package ANTIVEG v1.
	pkg, err := a.YardQuotes.CreatePackage(ctx, p, m, yardquote.PackageInput{
		Code: "ANTIVEG", Name: yardquote.I18n{It: "Antivegetativa completa", En: en("Full antifouling")}, WorkTypeID: wt["ANTIFOULING"],
		JobVars: []string{"layers"},
		Lines: []yardquote.PackageLine{
			{Seq: 1, ItemID: items["SRV-ALAG"], QtyFormula: "loa", PriceFormula: `if(hull_type == "catamaran", list_price * 1.5, list_price)`},
			{Seq: 2, ItemID: items["SRV-LAV"], QtyFormula: "round(hull_area, 1)"},
			{Seq: 3, ItemID: items["LAB-HAND"], QtyFormula: "round(hull_area * 0.15, 1)", Task: &yardquote.PackageTask{
				Title: yardquote.I18n{It: "Carteggiatura", En: en("Sanding")}, Checklist: []string{"Lavaggio", "Carteggiatura", "Primer"},
			}},
			{Seq: 4, ItemID: items["LAB-PAINT"], QtyFormula: "round(hull_area * layers * 0.05, 1)", Task: &yardquote.PackageTask{
				Title: yardquote.I18n{It: "Antivegetativa", En: en("Antifouling")}, Checklist: []string{"Prima mano", "Seconda mano"},
			}},
			{Seq: 5, ItemID: items["MAT-AVEG"], QtyFormula: "ceil(hull_area * layers / 25)"},
			{Seq: 6, ItemID: items["SRV-VARO"], QtyFormula: "loa"},
		},
	})
	if err != nil {
		return fmt.Errorf("package ANTIVEG: %w", err)
	}

	// Ten quotes on the demo boats (seedYardBoats: the boats on the hard first). The first five are drafts the
	// orders seed accepts (their work orders), then two sent, two left as drafts and one declined.
	boats, err := a.seedYardBoats(ctx, scope, today, seedYardQuotes)
	if err != nil {
		return err
	}
	for i, b := range boats {
		start, delivery := today.AddDays(seedYardQuotePlan[i][0]), today.AddDays(seedYardQuotePlan[i][1])
		in := yardquote.QuoteInput{
			CustomerID: b.CustomerID, VesselID: b.VesselID, Selections: []yardquote.Selection{{PackageID: pkg.Row.ID}},
			Conditions:     "Acconto del 30% all'accettazione, saldo alla consegna.",
			PlannedStartOn: &start, PlannedDeliveryOn: &delivery,
		}
		if i%3 == 2 {
			in.Selections = append(in.Selections, yardquote.Selection{ItemID: items["SRV-LAV"]})
		}
		q, err := a.YardQuotes.Create(ctx, p, m, in)
		if err != nil {
			return fmt.Errorf("yard quote: %w", err)
		}
		switch i {
		case 5, 6:
			if _, err := a.YardQuotes.Send(ctx, p, m, q.Row.ID, q.Row.Version, true, ""); err != nil {
				return fmt.Errorf("yard quote send: %w", err)
			}
		case 9:
			if _, err := a.YardQuotes.Decline(ctx, p, m, q.Row.ID, q.Row.Version, "Lavori rinviati"); err != nil {
				return fmt.Errorf("yard quote decline: %w", err)
			}
		}
	}
	return nil
}

// seedYardQuotes is the number of demo yard quotes; seedYardQuotePlan is the planned start and delivery of each
// (days from today): the first five become the work orders of the orders seed (two started, one on hold, two
// starting this week), the others are proposals for the coming weeks.
const seedYardQuotes = 10

var seedYardQuotePlan = [seedYardQuotes][2]int{
	{-6, 5}, {-3, 8}, {-8, 6}, {1, 10}, {3, 14}, {12, 20}, {15, 24}, {20, 30}, {25, 35}, {30, 40},
}

// seedYardBoat is a demo boat for the yard: its customer and, when it is on the hard today, the allocation of its
// storage contract (the bay of its work order).
type seedYardBoat struct {
	VesselID, CustomerID, AllocationID string
}

// seedYardBoats returns up to n boats for the yard demo, deterministic: first the boats stored today in the yard
// or the shed (storage seed), then the registry boats of at least 8 m with beam and draft, by name. Boats without a
// customer are skipped.
func (a *App) seedYardBoats(ctx context.Context, scope store.MarinaScope, today calendar.Date, n int) ([]seedYardBoat, error) {
	var stored []struct {
		ID       string `bun:"id"`
		VesselID string `bun:"vessel_id"`
	}
	if err := a.DB.NewSelect().TableExpr("allocations AS a").ColumnExpr("a.id, a.vessel_id").
		Join("JOIN dynamic_areas AS d ON d.id = a.resource_id AND d.marina_id = a.marina_id").
		Join("JOIN maps AS mp ON mp.id = d.map_id AND mp.marina_id = a.marina_id").
		Where("a.marina_id = ?", scope.ID()).Where("a.resource_type = ?", store.ResourceArea).
		Where("a.kind = ?", "contract").Where("a.status = ?", "active").Where("a.vessel_id IS NOT NULL").
		Where("a.first_night <= ?", today).Where("a.last_night >= ?", today).Where("mp.kind IN (?)", bun.List([]string{"yard", "shed"})).
		OrderExpr("a.first_night, a.id").Scan(ctx, &stored); err != nil {
		return nil, err
	}
	var vessels []store.Vessel
	if err := a.DB.NewSelect().Model(&vessels).Where("marina_id = ?", scope.ID()).Where("archived_at IS NULL").
		Where("draft_cm > 0").Where("beam_cm > 0").Where("loa_cm >= ?", 800).OrderExpr("name, id").Limit(200).Scan(ctx); err != nil {
		return nil, err
	}
	alloc := map[string]string{}
	var ids []string
	for _, s := range stored {
		if _, ok := alloc[s.VesselID]; !ok {
			alloc[s.VesselID] = s.ID
			ids = append(ids, s.VesselID)
		}
	}
	for _, v := range vessels {
		if _, ok := alloc[v.ID]; !ok {
			alloc[v.ID] = ""
			ids = append(ids, v.ID)
		}
	}
	owners, err := (&seedOcc{a: a, ctx: ctx}).ownersOf(scope, ids)
	if err != nil {
		return nil, err
	}
	out := []seedYardBoat{}
	for _, id := range ids {
		if len(out) == n {
			break
		}
		if owners[id] != "" {
			out = append(out, seedYardBoat{VesselID: id, CustomerID: owners[id], AllocationID: alloc[id]})
		}
	}
	return out, nil
}
