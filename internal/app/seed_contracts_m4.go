package app

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/store"
)

// seedContractsM4 seeds the M4 demo data owned by CONTRACTS (m4-plan §10): contract settings (OTP signature, demo
// bank details, default policy deposit 30 % + 3 quarters), the instalment plan of every active contract with the
// past instalments settled through the manual driver (6 left overdue), a renewal campaign for next year (previewed,
// launched, some proposals accepted and declined from the panel), three addenda (one awaiting signature) and
// waiting-list entries. Idempotent: it
// runs only while the demo marina has no stored contract settings.
func (a *App) seedContractsM4(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Contracts == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if cs, err := store.GetContractSettings(ctx, a.DB.DB, scope, a.Clock.Now()); err != nil || cs.Stored {
		return err
	}
	m := res.MarinaID
	bp := int64(3000)
	pay := &contract.PaymentPolicy{
		Deposit: &contract.DepositPolicy{BP: &bp}, DepositDueDays: 7, IssueDaysBeforeDue: 15, RoundingUnitCents: 1,
		Schedule:     contract.SchedulePolicy{Mode: "interval", EveryMonths: 3, Count: 3, FirstDueOffsetMonths: 3},
		EarlyPayment: &contract.EarlyPaymentPolicy{DiscountBP: 300, PayByOffsetDays: 15},
	}
	driver, noScan := contract.DriverOTP, false
	if _, err := a.Contracts.UpdateContractSettings(ctx, p, m, 0, contract.SettingsPatch{
		DefaultPolicy: &contract.PolicyInput{Payment: pay, Renewal: &contract.RenewalPolicy{NoticeDays: 60}}, SignatureDriver: &driver,
		RequirePaperScan: &noScan,
		BankDetails:      &contract.BankDetails{IBAN: "IT60X0542811101000000123456", Holder: "Marina Demo S.r.l.", Bank: "Banca Demo"},
	}); err != nil {
		return fmt.Errorf("contract settings: %w", err)
	}
	rng := rand.New(rand.NewPCG(41, 5)) //nolint:gosec // demo data: deterministic by design
	page, err := a.Contracts.ListContracts(ctx, p, m, contract.ListFilter{Status: string(contract.StatusActive)}, "", store.MaxLimit)
	if err != nil {
		return err
	}
	overdue := 0
	for i := range page.Items {
		c := page.Items[i].Row
		plan, err := a.Contracts.ReplanInstallments(ctx, p, m, c.ID, c.Version, contract.ReplanInput{Plan: contract.PlanStandard, Payment: pay})
		if err != nil {
			return fmt.Errorf("plan %s: %w", c.ID, err)
		}
		res.Installments += len(plan.Items)
		for _, it := range plan.Items {
			if it.Status != store.InstallmentIssued {
				continue
			}
			if overdue < 6 && rng.IntN(4) == 0 {
				overdue++
				continue
			}
			if _, err := a.Contracts.SettleInstallment(ctx, p, m, it.ID, it.Version, contract.SettleInput{Method: "bank_transfer"}); err != nil {
				return fmt.Errorf("settle %s: %w", it.ID, err)
			}
		}
	}
	if err := a.seedRenewalCampaign(ctx, p, scope, res); err != nil {
		return err
	}
	if err := a.seedAddenda(ctx, p, scope, page.Items, res); err != nil {
		return err
	}
	customers, err := store.SeedCustomerIDs(ctx, a.DB.DB, scope, 6)
	if err != nil {
		return err
	}
	for i, id := range customers {
		if _, err := a.Contracts.CreateWaitlistEntry(ctx, p, m, contract.WaitlistInput{
			CustomerID: id, LOA: int64(900 + 150*i), Beam: int64(300 + 20*i), Term: contract.TermAnnual,
			RequestedFrom: calendarToday(a, scope), Priority: i % 3,
		}); err != nil {
			return fmt.Errorf("waitlist: %w", err)
		}
		res.WaitlistEntries++
	}
	return nil
}

// calendarToday is the marina's local date of the app clock.
func calendarToday(a *App, scope store.MarinaScope) calendar.Date {
	tz, err := store.MarinaTimezone(context.Background(), a.DB.DB, scope)
	if err != nil {
		return calendar.In(a.Clock.Now(), time.UTC)
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return calendar.In(a.Clock.Now(), loc)
}

// seedRenewalCampaign creates the renewal campaign of the annual contracts ending at the year end, priced on the
// next-year list of the pricing seed: preview, launch, one sync (emails queued), then a few decisions from the
// panel (accepted ones await the customer's OTP signature).
func (a *App) seedRenewalCampaign(ctx context.Context, p *auth.Principal, scope store.MarinaScope, res *SeedResult) error {
	m := res.MarinaID
	today := calendarToday(a, scope)
	y := today.Year()
	lists, err := a.PriceLists.List(ctx, p, m, pricelist.ListFilter{})
	if err != nil {
		return err
	}
	listID := ""
	for _, l := range lists {
		if l.ValidFrom.Year() != y+1 {
			continue
		}
		listID = l.ID
		// The pricing seed leaves next year's list as a draft; the renewals are priced on it, so it is activated
		// here (its source list stays visible as the renewal origin).
		if l.Status == "draft" {
			if _, err := a.PriceLists.Activate(ctx, p, m, l.ID, l.Version, calendar.Date{}); err != nil {
				return fmt.Errorf("activate %s: %w", l.Code, err)
			}
		}
	}
	if listID == "" {
		return nil
	}
	from, _ := calendar.Parse(fmt.Sprintf("%04d-12-01", y))
	to, _ := calendar.Parse(fmt.Sprintf("%04d-01-31", y+1))
	c, err := a.Contracts.CreateRenewalCampaign(ctx, p, m, contract.CampaignInput{
		Name: fmt.Sprintf("Rinnovi annuali %d", y+1), DeadlineOn: today.AddDays(30), PriceListID: listID,
		Filter:  contract.CampaignFilter{Terms: []string{contract.TermAnnual}, EndsFrom: &from, EndsTo: &to},
		Message: "Gentile cliente, ti proponiamo il rinnovo del posto barca per il prossimo anno.",
	})
	if err != nil {
		return fmt.Errorf("renewal campaign: %w", err)
	}
	if c, err = a.Contracts.PreviewRenewalCampaign(ctx, p, m, c.Row.ID, c.Row.Version); err != nil {
		return fmt.Errorf("campaign preview: %w", err)
	}
	res.RenewalCampaigns++
	if c.Counts[string(contract.ProposalPreviewed)] == 0 {
		return nil
	}
	if _, err := a.Contracts.LaunchRenewalCampaign(ctx, p, m, c.Row.ID, c.Row.Version); err != nil {
		return fmt.Errorf("campaign launch: %w", err)
	}
	if _, err := a.Contracts.SyncRenewalsMarina(ctx, m); err != nil {
		return fmt.Errorf("campaign sync: %w", err)
	}
	page, err := a.Contracts.ListCampaignProposals(ctx, p, m, c.Row.ID, string(contract.ProposalSent), "", store.MaxLimit)
	if err != nil {
		return err
	}
	for i, pr := range page.Items {
		res.RenewalProposals++
		switch {
		case i < 6:
			plan := contract.PlanStandard
			if i == 0 {
				plan = contract.PlanEarlyPayment
			}
			if _, err := a.Contracts.AcceptRenewalProposal(ctx, p, m, pr.Row.ID, pr.Row.Version, plan); err != nil {
				return fmt.Errorf("accept proposal: %w", err)
			}
		case i < 9:
			if _, err := a.Contracts.DeclineRenewalProposal(ctx, p, m, pr.Row.ID, pr.Row.Version, "Vendita della barca"); err != nil {
				return fmt.Errorf("decline proposal: %w", err)
			}
		}
	}
	return nil
}

// seedAddenda adds three price-change addenda to active contracts: two activated on paper, one awaiting the
// customer's signature.
func (a *App) seedAddenda(ctx context.Context, p *auth.Principal, scope store.MarinaScope, items []contract.Summary, res *SeedResult) error {
	m := res.MarinaID
	today := calendarToday(a, scope)
	n := 0
	for _, it := range items {
		c := it.Row
		if n == 3 || c.Kind != contract.KindMooring || !today.AddDays(30).Before(c.EndsOn) || c.StartsOn.After(today) {
			continue
		}
		nt := c.TotalCents + 15_000
		ad, err := a.Contracts.CreateAddendum(ctx, p, m, c.ID, contract.AddendumInput{
			Kind: contract.AddPriceChange, EffectiveOn: today.AddDays(1),
			Payload: contract.AddendumPayload{NewTotalCents: &nt, Reason: "Colonnina elettrica dedicata"},
		})
		if err != nil {
			return fmt.Errorf("addendum: %w", err)
		}
		if n < 2 {
			if _, err := a.Contracts.ActivateAddendum(ctx, p, m, c.ID, ad.Row.ID, ad.Row.Version, contract.ActivateInput{}); err != nil {
				return fmt.Errorf("addendum activate: %w", err)
			}
		} else if _, err := a.Contracts.RequestAddendumSignature(ctx, p, m, c.ID, ad.Row.ID, ad.Row.Version, contract.SignatureInput{}); err != nil {
			return fmt.Errorf("addendum signature: %w", err)
		}
		n++
		res.Addenda++
	}
	return nil
}
