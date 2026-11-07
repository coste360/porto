package app

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedCash seeds the cash and collection data of the demo marina (m5-plan §10): the default register and a
// "banchina" register, three closed sessions (one with a −2,00 € difference) and one open, 40 counter sales of
// catalogue items (half with a commercial document of the fiscal driver none, some walk-in), payment requests for
// the overdue charges, a reminder run, two dunning exclusions (repayment plan, disputed). No provider settings row:
// the demo is fully offline (Stripe disabled). Everything goes through the services as the demo owner; a no-op
// when the marina already has cash sessions. The clock is the app's, so every session carries today's business
// date.
func (a *App) seedCash(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("cash_sessions").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	rng := rand.New(rand.NewPCG(51, 5)) //nolint:gosec // demo data: deterministic by design
	if _, err := a.Cash.ListRegisters(ctx, p, m); err != nil {
		return fmt.Errorf("default register: %w", err)
	}
	dock, err := a.Cash.CreateRegister(ctx, p, m, cash.RegisterInput{Code: "banchina", Name: "Cassa banchina", Active: true})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	items, err := a.demoSaleItems(ctx, scope)
	if err != nil {
		return err
	}
	var customers []string
	if err := a.DB.NewSelect().TableExpr("customers").Column("id").Where("marina_id = ?", scope.ID()).
		Where("anonymized_at IS NULL").OrderExpr("id").Limit(20).Scan(ctx, &customers); err != nil {
		return err
	}
	sale := func(registerID string, i int) error {
		if len(items) == 0 {
			return nil
		}
		it := items[i%len(items)]
		in := cash.CounterSaleInput{
			Lines:  []cash.CounterSaleLine{{ItemID: it.id, QtyMilli: int64(1+rng.IntN(3)) * 1000, Unit: ptrCents(it.unit)}},
			Method: store.PaymentMethodCash, RegisterID: registerID, FiscalDocument: i%2 == 0, Receipt: cash.ReceiptPrint,
		}
		if i%5 == 1 {
			in.Method = store.PaymentMethodCardPOS
		}
		if (!in.FiscalDocument || i%3 == 0) && len(customers) > 0 {
			in.CustomerID = customers[rng.IntN(len(customers))]
		}
		if in.CustomerID == "" {
			in.FiscalDocument = true
		}
		if _, err := a.Cash.CounterSale(ctx, p, m, in); err != nil {
			return fmt.Errorf("counter sale %d: %w", i, err)
		}
		res.CounterSales++
		return nil
	}
	// Three closed sessions of the main register, the second short by 2,00 €; then one open on the dock.
	n := 0
	for s := range 3 {
		sess, err := a.Cash.OpenSession(ctx, p, m, "", 100_00)
		if err != nil {
			return fmt.Errorf("open session: %w", err)
		}
		res.CashSessions++
		for range 10 {
			if err := sale("", n); err != nil {
				return err
			}
			n++
		}
		if _, err := a.Cash.AddMovement(ctx, p, m, sess.ID, cash.MovementOut, 20_00, "Versamento in banca"); err != nil {
			return fmt.Errorf("movement: %w", err)
		}
		cur, err := a.Cash.GetSession(ctx, p, m, sess.ID)
		if err != nil {
			return err
		}
		var counted []cash.CountedInput
		for _, e := range cur.Summary.Expected {
			amount := e.Amount
			if s == 1 && e.Method == store.PaymentMethodCash {
				amount -= 2_00
			}
			counted = append(counted, cash.CountedInput{Method: e.Method, Amount: amount})
		}
		if _, err := a.Cash.CloseSession(ctx, p, m, sess.ID, cur.Version, counted, ""); err != nil {
			return fmt.Errorf("close session: %w", err)
		}
	}
	if _, err := a.Cash.OpenSession(ctx, p, m, dock.ID, 50_00); err != nil {
		return fmt.Errorf("open dock session: %w", err)
	}
	res.CashSessions++
	for range 10 {
		if err := sale(dock.ID, n); err != nil {
			return err
		}
		n++
	}
	if err := a.DB.NewSelect().TableExpr("fiscal_documents").ColumnExpr("COUNT(*)").Where("marina_id = ?", scope.ID()).
		Scan(ctx, &res.FiscalDocuments); err != nil {
		return err
	}
	return a.seedCollect(ctx, p, scope, res)
}

// seedCollect seeds the payment requests of the overdue charges, today's reminder run and two exclusions.
func (a *App) seedCollect(ctx context.Context, p *auth.Principal, scope store.MarinaScope, res *SeedResult) error {
	m := scope.ID()
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := occupancy.Today(a.Clock, loc)
	open, err := store.OpenPositiveCharges(ctx, a.DB.DB, scope, store.OpenChargesFilter{})
	if err != nil {
		return err
	}
	var overdue []string
	var disputed, planCustomer string
	for _, ch := range open {
		if ch.CustomerID == "" || !ch.DueOn.Before(today) {
			continue
		}
		switch {
		case disputed == "":
			disputed = ch.ID
		case planCustomer == "" && ch.CustomerID != "":
			planCustomer = ch.CustomerID
		}
		if len(overdue) < collect.MaxBulkCharges {
			overdue = append(overdue, ch.ID)
		}
	}
	if disputed != "" {
		if _, _, err := a.Collect.CreateExclusion(ctx, p, m, collect.ExclusionInput{ChargeID: disputed, Kind: "disputed", Note: "Importo contestato dal cliente"}); err != nil {
			return fmt.Errorf("exclusion: %w", err)
		}
		res.DunningExclusion++
	}
	if planCustomer != "" {
		if _, _, err := a.Collect.CreateExclusion(ctx, p, m, collect.ExclusionInput{
			CustomerID: planCustomer, Kind: "payment_plan", Until: today.AddDays(90), Note: "Piano di rientro concordato",
		}); err != nil {
			return fmt.Errorf("exclusion: %w", err)
		}
		res.DunningExclusion++
	}
	if len(overdue) > 0 {
		r, err := a.Collect.CreateRequestsBulk(ctx, p, m, overdue, true)
		if err != nil {
			return fmt.Errorf("payment requests: %w", err)
		}
		res.PaymentRequests += r.Created
	}
	if _, err := a.Collect.RunNow(ctx, p, m); err != nil {
		return fmt.Errorf("reminder run: %w", err)
	}
	return a.DB.NewSelect().TableExpr("reminder_items").ColumnExpr("COUNT(*)").Where("marina_id = ?", m).Scan(ctx, &res.ReminderItems)
}

type saleItem struct {
	id   string
	unit money.Cents
}

// demoSaleItems returns up to five active non-berth catalogue items with a demo unit price.
func (a *App) demoSaleItems(ctx context.Context, scope store.MarinaScope) ([]saleItem, error) {
	var rows []store.Item
	if err := a.DB.NewSelect().Model(&rows).Where("marina_id = ?", scope.ID()).Where("active = ?", true).
		Where("item_type IN (?)", bun.List([]string{"product", "service", "utility"})).OrderExpr("code").Limit(5).Scan(ctx); err != nil {
		return nil, err
	}
	prices := []money.Cents{18_50, 3_00, 5_00, 45_00, 7_50}
	out := make([]saleItem, 0, len(rows))
	for i, r := range rows {
		out = append(out, saleItem{id: r.ID, unit: prices[i%len(prices)]})
	}
	return out, nil
}

func ptrCents(c money.Cents) *money.Cents { return &c }
