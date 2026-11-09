package app

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
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
	pay := seedPaymentPolicy()
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
	for i := range page.Items {
		c := page.Items[i].Row
		cur, err := a.Contracts.ContractInstallments(ctx, p, m, c.ID)
		if err != nil {
			return fmt.Errorf("plan %s: %w", c.ID, err)
		}
		if len(cur.Items) > 1 {
			// Signed with the demo clause by the occupancy seed: already planned.
			res.Installments += len(cur.Items)
			continue
		}
		plan, err := a.Contracts.ReplanInstallments(ctx, p, m, c.ID, c.Version, contract.ReplanInput{Plan: contract.PlanStandard, Payment: pay})
		if err != nil {
			return fmt.Errorf("plan %s: %w", c.ID, err)
		}
		res.Installments += len(plan.Items)
	}
	if err := a.seedSettleInstallments(ctx, p, scope, rng); err != nil {
		return err
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

// seedSettleInstallments walks through the issue and due days of the instalments up to today on a fake clock: each
// pass runs the instalment issue job (so every instalment is issued on its issue day) and settles, through the
// manual driver, the issued instalments falling due by then on their due date. Six instalments due in the last two
// months are left unpaid (overdue). The charges backfilled later by the billing seed keep these issue and
// settlement dates, so the year's charges and receipts are spread over the months as in the previous-year history.
func (a *App) seedSettleInstallments(ctx context.Context, p *auth.Principal, scope store.MarinaScope, rng *rand.Rand) error {
	m := scope.ID()
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	now := a.Clock.Now()
	today := calendar.In(now, loc)
	past, fake, err := a.seedClockApp(ctx, now)
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	// The days on which something is issued or falls due, up to today (no pass on the days in between).
	days, err := seedInstallmentDays(ctx, a, scope, today)
	if err != nil {
		return err
	}
	decided := map[string]bool{}
	overdue := 0
	for _, d := range days {
		at := d.Time(loc).Add(9 * time.Hour)
		if d == today || at.After(now) {
			at = now
		}
		fake.Set(at)
		if _, err := past.Contracts.IssueMarina(ctx, m); err != nil {
			return fmt.Errorf("issue: %w", err)
		}
		due, err := store.AllInstallmentRows(ctx, a.DB.DB, scope, store.InstallmentFilter{Status: store.InstallmentIssued, To: &d}, 100_000)
		if err != nil {
			return err
		}
		for _, it := range due {
			if decided[it.ID] {
				continue
			}
			decided[it.ID] = true
			if overdue < 6 && it.DueOn.DaysUntil(today) <= 60 && rng.IntN(4) == 0 {
				overdue++
				continue
			}
			on := it.DueOn
			if _, err := past.Contracts.SettleInstallment(ctx, p, m, it.ID, it.Version, contract.SettleInput{Method: "bank_transfer", SettledOn: &on}); err != nil {
				return fmt.Errorf("settle %s: %w", it.ID, err)
			}
		}
	}
	return nil
}

// seedInstallmentDays returns, sorted, the issue and due days up to last of the live instalments of the marina,
// and last itself.
func seedInstallmentDays(ctx context.Context, a *App, scope store.MarinaScope, last calendar.Date) ([]calendar.Date, error) {
	set := map[calendar.Date]bool{last: true}
	for _, st := range []string{store.InstallmentScheduled, store.InstallmentIssued} {
		rows, err := store.AllInstallmentRows(ctx, a.DB.DB, scope, store.InstallmentFilter{Status: st}, 100_000)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			for _, d := range []calendar.Date{r.IssueOn, r.DueOn} {
				if !d.IsZero() && !d.After(last) {
					set[d] = true
				}
			}
		}
	}
	out := make([]calendar.Date, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	slices.SortFunc(out, func(x, y calendar.Date) int {
		switch {
		case x.Before(y):
			return -1
		case y.Before(x):
			return 1
		}
		return 0
	})
	return out, nil
}

// seedPaymentPolicy is the demo payment clause (deposit 30 % due in 7 days, then 3 quarterly instalments, each
// issued 15 days before its due date, early payment −3 %): the default policy of the demo and the clause of the
// contracts the occupancy seed creates.
func seedPaymentPolicy() *contract.PaymentPolicy {
	bp := int64(3000)
	return &contract.PaymentPolicy{
		Deposit: &contract.DepositPolicy{BP: &bp}, DepositDueDays: 7, IssueDaysBeforeDue: 15, RoundingUnitCents: 1,
		Schedule:     contract.SchedulePolicy{Mode: "interval", EveryMonths: 3, Count: 3, FirstDueOffsetMonths: 3},
		EarlyPayment: &contract.EarlyPaymentPolicy{DiscountBP: 300, PayByOffsetDays: 15},
	}
}

// seedAnnualPolicy is the payment clause of the i-th annual demo contract (the previous-year history copies the
// clause of the same customer's contract): a quarter keep the default quarterly plan, a quarter pay by monthly
// direct debit (issued and due on the 1st), a quarter in four instalments on the 10th of March, June, September
// and November, a quarter in two halves (end of April and of September). Real marinas offer such options; for the
// demo they spread the year's charges and receipts over many days, as in a real ledger.
func seedAnnualPolicy(i int) *contract.PaymentPolicy {
	pay := seedPaymentPolicy()
	switch i % 4 {
	case 1:
		bp := int64(1000)
		pay.Deposit.BP, pay.IssueDaysBeforeDue = &bp, 0
		pay.Schedule = contract.SchedulePolicy{Mode: "interval", EveryMonths: 1, Count: 11, FirstDueOffsetMonths: 1}
	case 2:
		pay.IssueDaysBeforeDue = 20
		pay.Schedule = contract.SchedulePolicy{Mode: "fixed_dates", Dates: []contract.FixedDueOn{
			{DueOn: "03-10", BP: 2500}, {DueOn: "06-10", BP: 2500}, {DueOn: "09-10", BP: 2500}, {DueOn: "11-10", BP: 2500},
		}}
	case 3:
		pay.IssueDaysBeforeDue = 30
		pay.Schedule = contract.SchedulePolicy{Mode: "fixed_dates", Dates: []contract.FixedDueOn{
			{DueOn: "04-30", BP: 5000}, {DueOn: "09-30", BP: 5000},
		}}
	}
	return pay
}

// seedSignedPolicy is the clause of the i-th contract signed by the occupancy seed: the annual clause
// (seedAnnualPolicy) or the default demo clause, so that the deposit is issued at signature and the instalments on
// their issue days (seedSettleInstallments walks through them).
func seedSignedPolicy(term string, i int) *contract.PolicyInput {
	pay := seedPaymentPolicy()
	if term == contract.TermAnnual {
		pay = seedAnnualPolicy(i)
	}
	return &contract.PolicyInput{Payment: pay, Renewal: &contract.RenewalPolicy{NoticeDays: 60}}
}

// seedPolicyOf is the clause of a stored contract policy (the previous-year history replays it); nil when the
// policy has no payment clause (the marina default applies).
func seedPolicyOf(raw store.JSON) *contract.PolicyInput {
	pol, err := contract.DecodePolicy(raw)
	if err != nil || pol.Payment == nil {
		return nil
	}
	return &contract.PolicyInput{Payment: pol.Payment, Renewal: pol.Renewal}
}

// seedAnnualSignedOn is the day the i-th of n annual demo contracts starting on jan1 is signed: from 2 to 16
// January, in creation order (customers renew at the office in the first weeks of the year).
func seedAnnualSignedOn(jan1 calendar.Date, i, n int) calendar.Date {
	return jan1.AddDays(1 + i*15/max(n, 1))
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
