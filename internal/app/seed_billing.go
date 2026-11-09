package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedBilling seeds the charges side of the demo marina (m5-plan §10), through the services as the demo owner:
// billing settings (due 30 days, automatic payment requests), the origin items (demo items ONERI 22 %, COND
// 22 %, SUBENTRO 22 %, PENALE N1, RIMBORSO 22 %, CAUZIONE N1, SALDO N1: VAT choices are demo data), the backfill of
// the M4 instalments and holder instalments seeded before the settings existed, a termination charge when a
// terminated contract has an amount, two exports (instalments, reconciled with invoice numbers FT-<n>; holder
// instalments) leaving transits and the rest to_invoice, and twelve opening balances (dated at the start of the
// demo history). It is a no-op once the
// billing settings were saved (the hooks already charged what the earlier seeds issued).
func (a *App) seedBilling(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	m := res.MarinaID
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, m)
	if err != nil {
		return err
	}
	if bs, err := store.GetBillingSettings(ctx, a.DB.DB, scope, a.Clock.Now()); err != nil || bs.Version > 0 {
		return err
	}
	items, err := a.seedBillingItems(ctx, p, m)
	if err != nil {
		return fmt.Errorf("billing items: %w", err)
	}
	cur, err := a.Charges.GetSettings(ctx, p, m)
	if err != nil {
		return err
	}
	if _, err := a.Charges.UpdateSettings(ctx, p, m, cur.Row.Version, charge.SettingsInput{
		DefaultDueDays: 30, AutoPaymentRequest: true, Reminder: cur.Reminder, ReminderHour: 9,
		Arrears: charge.ArrearsPolicy{Enabled: false, DaysOverdue: 60}, ExportReminderDays: 30, FiscalDriver: "none",
		FiscalDrivers: []string{"none", "export"},
		OriginItems: map[string]string{
			"management_fee": items["ONERI"], "common_advance": items["COND"], "common_balance": items["COND"],
			"transfer_fee": items["SUBENTRO"], "termination_penalty": items["PENALE"], "termination_refund": items["RIMBORSO"],
			"deposit_retention": items["CAUZIONE"], "opening_balance": items["SALDO"], "contract_trueup_fallback": items["ONERI"],
		},
	}); err != nil {
		return fmt.Errorf("billing settings: %w", err)
	}
	before, err := store.CountCharges(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	if _, err := a.Charges.BackfillMarina(ctx, scope); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}
	if err := a.seedTerminationCharge(ctx, p, scope); err != nil {
		return fmt.Errorf("termination charge: %w", err)
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := occupancy.Today(a.Clock, loc)
	from := today.AddDays(-400)
	for _, origins := range [][]string{{store.ChargeOriginInstallment}, {store.ChargeOriginHolderInstallment}} {
		e, err := a.Charges.CreateExport(ctx, p, m, charge.ExportFilter{PeriodFrom: from, PeriodTo: today, OriginTypes: origins}, "")
		if errors.Is(err, charge.ErrExportEmpty) {
			continue
		}
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		res.ChargeExports++
		if origins[0] != store.ChargeOriginInstallment {
			continue
		}
		ids, err := store.ExportChargeIDs(ctx, a.DB.DB, scope, e.ID)
		if err != nil {
			return err
		}
		cs, err := store.ChargesByIDs(ctx, a.DB.DB, scope, ids)
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString("Numero addebito;Numero fattura;Data\n")
		for i, c := range cs {
			fmt.Fprintf(&b, "%s;FT-%d;%02d/%02d/%04d\n", c.Number, i+1, today.Day(), int(today.Month()), today.Year())
		}
		if _, _, err := a.Charges.Reconcile(ctx, p, m, charge.ReconcileInput{CSV: b.String()}); err != nil {
			return fmt.Errorf("reconcile: %w", err)
		}
	}
	// The opening balances come from the software used before Porto: imported on the day the demo history
	// starts (1 December Y−2, seed_history.go), not in the current month.
	opened := calendar.New(today.Year()-2, time.December, 1)
	past, _, err := a.seedClockApp(ctx, opened.Time(loc).Add(9*time.Hour))
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	n, err := past.seedOpeningBalances(ctx, p, scope, opened)
	if err != nil {
		return fmt.Errorf("opening balances: %w", err)
	}
	res.OpeningBalances = n
	after, err := store.CountCharges(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	res.Charges = after - before
	return nil
}

// seedBillingItems creates the demo origin items (or reuses them by code) and returns code → id.
func (a *App) seedBillingItems(ctx context.Context, p *auth.Principal, m string) (map[string]string, error) {
	classes, err := a.Catalog.ListRevenueClasses(ctx, p, m, false)
	if err != nil {
		return nil, err
	}
	if len(classes) == 0 {
		return nil, errors.New("no revenue class")
	}
	class := classes[0].ID
	for _, c := range classes {
		if strings.Contains(strings.ToLower(c.Code), "serviz") || strings.Contains(strings.ToLower(c.Code), "other") {
			class = c.ID
		}
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, m)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	incl := true
	for _, it := range []struct {
		code, it, en string
		bp           int64
		nature       string
	}{
		{"ONERI", "Canone di gestione", "Management fee", 2200, ""},
		{"COND", "Oneri condominiali", "Common charges", 2200, ""},
		{"SUBENTRO", "Diritto di subentro", "Transfer fee", 2200, ""},
		{"PENALE", "Penale di recesso", "Termination penalty", 0, "N1"},
		{"RIMBORSO", "Rimborso per recesso", "Termination refund", 2200, ""},
		{"CAUZIONE", "Trattenuta cauzione", "Deposit retention", 0, "N1"},
		{"SALDO", "Saldo di apertura", "Opening balance", 0, "N1"},
	} {
		if ex, err := store.ItemByCode(ctx, a.DB.DB, scope, it.code); err == nil {
			out[it.code] = ex.ID
			continue
		}
		bp, en := it.bp, it.en
		created, err := a.Catalog.CreateItem(ctx, p, m, catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.it, En: &en}, ItemType: "fee", Unit: "lump", VATRateBP: &bp,
			VATNature: it.nature, PriceIncludesVAT: &incl, RevenueClassID: class,
		})
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", it.code, err)
		}
		out[it.code] = created.ID
	}
	return out, nil
}

// seedTerminationCharge charges the stored termination amount of the first terminated contract that has one.
func (a *App) seedTerminationCharge(ctx context.Context, p *auth.Principal, scope store.MarinaScope) error {
	var rows []store.Contract
	if err := a.DB.NewSelect().Model(&rows).Where("marina_id = ?", scope.ID()).Where("status = 'terminated'").
		Where("termination_amount_cents IS NOT NULL").Where("termination_amount_cents <> 0").OrderExpr("id").Limit(1).
		Scan(ctx); err != nil || len(rows) == 0 {
		return err
	}
	_, err := a.Charges.CreateTermination(ctx, p, scope.ID(), rows[0].ID, rows[0].Version)
	if errors.Is(err, charge.ErrChargeExists) {
		return nil
	}
	return err
}

// seedOpeningBalances imports twelve opening balances (one credit) of the first customers, matched by id.
func (a *App) seedOpeningBalances(ctx context.Context, p *auth.Principal, scope store.MarinaScope, today calendar.Date) (int, error) {
	var ids []string
	if err := a.DB.NewSelect().Table("customers").Column("id").Where("marina_id = ?", scope.ID()).
		Where("merged_into_id IS NULL").Where("anonymized_at IS NULL").OrderExpr("id").Limit(12).Scan(ctx, &ids); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	rng := rand.New(rand.NewPCG(51, 5)) //nolint:gosec // demo data: deterministic by design
	var b strings.Builder
	b.WriteString("Codice cliente;Importo;Scadenza;Riferimento\n")
	due := today.AddDays(15)
	for i, id := range ids {
		amount := money.Cents(5000 + rng.IntN(150000))
		if i == len(ids)-1 {
			amount = -money.Cents(2500 + rng.IntN(10000)) // a customer with credit from the previous software
		}
		fmt.Fprintf(&b, "%s;%s;%02d/%02d/%04d;FT-PREC-%d\n", id, strings.Replace(amount.String(), ".", ",", 1),
			due.Day(), int(due.Month()), due.Year(), i+1)
	}
	rec, err := a.Charges.ImportOpeningBalances(ctx, p, scope.ID(), charge.OpeningInput{
		Format: "csv", FileBase64: base64.StdEncoding.EncodeToString([]byte(b.String())), HasHeader: true,
		Mapping: charge.OpeningMapping{CustomerMatch: "customer_code", CustomerColumn: 0, AmountColumn: 1, DueOnColumn: 2, DescriptionColumn: -1, ReferenceColumn: 3},
	})
	if err != nil {
		return 0, err
	}
	return rec.ChargeCount, nil
}
