package contract

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Data source of the contract template kinds (m4-plan §6.2, §13.3): quote, contract_* and charter, addendum,
// renewal_proposal, payment_request, reminder and installment_notice. View runs inside the compose transaction
// and fills the frozen views structs; the customer data is printed on the customer's own documents only.

type contractSource struct{ s *Service }

// DataSource returns the docs data source of the contract kinds.
func (s *Service) DataSource() docs.DataSource { return contractSource{s} }

func (contractSource) Kinds() []docs.Kind {
	return []docs.Kind{
		docs.KindQuote, docs.KindContractAnnual, docs.KindContractSeasonal, docs.KindContractMonthly,
		docs.KindContractPeriod, docs.KindCharter, docs.KindAddendum, docs.KindRenewalProposal,
		docs.KindPaymentRequest, docs.KindReminder, docs.KindInstallmentNotice,
	}
}

func (contractSource) DemoView(kind docs.Kind, _ string) (any, error) {
	return docs.DemoView(kind), nil
}

func (cs contractSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind docs.Kind, st docs.SubjectType, id, _ string) (any, error) {
	s := cs.s
	mv, err := docs.MarinaHeader(ctx, tx, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	switch kind {
	case docs.KindQuote:
		return s.quoteView(ctx, tx, scope, mv, id)
	case docs.KindAddendum:
		return s.addendumView(ctx, tx, scope, mv, id)
	case docs.KindRenewalProposal:
		return s.renewalView(ctx, tx, scope, mv, id)
	case docs.KindPaymentRequest, docs.KindReminder, docs.KindInstallmentNotice:
		return s.installmentView(ctx, tx, scope, mv, id)
	default:
		if st != docs.SubjectContract {
			return nil, errors.New("contract: unexpected subject " + string(st))
		}
		return s.contractView(ctx, tx, scope, mv, id)
	}
}

func partyView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) views.Party {
	if id == "" {
		return views.Party{}
	}
	c, err := store.GetCustomer(ctx, tx, scope, id)
	if err != nil {
		return views.Party{}
	}
	return docs.PartyOf(c)
}

func vesselView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) views.Vessel {
	v, err := store.GetVessel(ctx, tx, scope, id)
	if err != nil {
		return views.Vessel{}
	}
	return views.Vessel{
		Name: v.Name, Registration: v.Registration, Flag: v.Flag, HullType: v.HullType, LOA: views.Cm(v.LOACm),
		Beam: views.Cm(v.BeamCm), Draft: views.Cm(v.DraftCm),
	}
}

// priceView converts a snapshot (total from the contract when it has adjustments).
func priceView(raw store.JSON, total, deposit int64) views.Price {
	p := views.Price{Total: money.Cents(total), Deposit: money.Cents(deposit), Lines: []views.PriceLine{}, Adjustments: []views.Adjustment{}}
	snap := DecodeSnapshot(raw)
	if snap == nil {
		return p
	}
	p.Net, p.VAT = snap.Breakdown.Net, snap.Breakdown.VAT
	p.Lines = priceLines(snap.Breakdown)
	for _, a := range snap.Adjustments {
		p.Adjustments = append(p.Adjustments, views.Adjustment{Kind: a.Kind, Amount: money.Cents(a.NewCents - a.OldCents)})
	}
	return p
}

func priceLines(b pricing.Breakdown) []views.PriceLine {
	out := make([]views.PriceLine, 0, len(b.Lines))
	for _, l := range b.Lines {
		pl := views.PriceLine{Description: l.ItemCode, Qty: views.Millis(l.Qty), Amount: l.Gross, VATRate: views.BP(l.VATRateBP)}
		if l.Qty > 0 {
			pl.UnitPrice = l.Gross.MulRat(1000, int64(l.Qty))
		}
		out = append(out, pl)
	}
	return out
}

func contractInfo(c *store.Contract) views.ContractInfo {
	ci := views.ContractInfo{Code: Code(c), Kind: c.Kind, Term: c.Term, StartsOn: c.StartsOn, EndsOn: c.EndsOn}
	ci.IssuedOn = calendarOf(c.CreatedAt)
	if !c.SignedAt.IsZero() {
		ci.SignedOn = calendarOf(c.SignedAt)
	}
	return ci
}

func installmentsView(rows []store.Installment) []views.Installment {
	out := make([]views.Installment, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.Installment{Seq: r.Seq, Kind: r.Kind, IssueOn: r.IssueOn, DueOn: r.DueOn, Amount: money.Cents(r.AmountCents), Status: r.Status})
	}
	return out
}

func (s *Service) places(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) []views.Place {
	allocs, err := contractAllocs(ctx, tx, scope, contractID)
	if err != nil {
		return []views.Place{}
	}
	var ids []string
	for _, a := range activeOnly(allocs) {
		ids = append(ids, a.ResourceID)
	}
	refs, _ := store.ResourceRefs(ctx, tx, scope, ids)
	out := []views.Place{}
	for _, a := range activeOnly(allocs) {
		p := views.Place{Type: a.ResourceType, FirstNight: a.FirstNight, LastNight: a.LastNight}
		if r, ok := refs[a.ResourceID]; ok {
			p.Code = r.Code
		}
		out = append(out, p)
	}
	return out
}

func policyView(p PaymentPolicy, r RenewalPolicy, c CancellationPolicy) views.Policy {
	v := views.Policy{
		DepositDueDays: p.DepositDueDays, SEPA: p.SEPA, TacitRenewal: r.Tacit, RenewalNoticeDays: r.NoticeDays,
		CancellationNoticeDays: c.NoticeDays, Refund: c.Refund, PenaltyBP: views.BP(c.PenaltyBP), InstallmentsCount: p.Schedule.Count,
	}
	if d := p.Deposit; d != nil {
		if d.BP != nil {
			v.DepositBP = views.BP(*d.BP)
		}
		if d.Cents != nil {
			v.Deposit = money.Cents(*d.Cents)
		}
	}
	if p.Schedule.Mode == "fixed_dates" {
		v.InstallmentsCount = len(p.Schedule.Dates)
	}
	if p.Schedule.Mode == "single" {
		v.InstallmentsCount = 1
	}
	if e := p.EarlyPayment; e != nil {
		v.EarlyPaymentDiscountBP = views.BP(e.DiscountBP)
	}
	return v
}

func (s *Service) contractView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mv views.Marina, id string) (any, error) {
	c, err := store.GetContract(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	pol, _ := DecodePolicy(c.Policy)
	pay, ren, can := effective(pol, cs)
	price := priceView(c.PriceSnapshot, c.TotalCents, c.DepositCents)
	v := views.ContractView{
		Marina: mv, Customer: partyView(ctx, tx, scope, c.CustomerID), Vessel: vesselView(ctx, tx, scope, c.VesselID),
		Contract: contractInfo(c), Berths: s.places(ctx, tx, scope, c.ID), Lines: price.Lines, Price: price,
		Installments: installmentsView(live), Policy: policyView(pay, ren, can),
	}
	if c.BillingCustomerID != "" {
		v.BillingCustomer = partyView(ctx, tx, scope, c.BillingCustomerID)
	} else {
		v.BillingCustomer = v.Customer
	}
	if c.Kind == KindCharter {
		if ch, err := store.GetCharterContract(ctx, tx, scope, c.ID); err == nil {
			f := &views.Fleet{ChangeoverWeekday: ch.ChangeoverWeekday, PricingMode: ch.PricingMode, Vessels: []views.Vessel{}}
			if fl, err := store.GetCharterFleet(ctx, tx, scope, ch.FleetID); err == nil {
				f.Name = fl.Name
			}
			if fvs, err := store.FleetVessels(ctx, tx, scope, []string{ch.FleetID}); err == nil {
				for _, fv := range fvs {
					if fv.Active {
						f.Vessels = append(f.Vessels, vesselView(ctx, tx, scope, fv.VesselID))
					}
				}
			}
			v.Fleet = f
		}
	}
	return v, nil
}

func (s *Service) quoteView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mv views.Marina, id string) (any, error) {
	q, err := store.GetQuote(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	opts, err := store.OptionsOfQuotes(ctx, tx, scope, []string{q.ID})
	if err != nil {
		return nil, err
	}
	var oids []string
	for _, o := range opts {
		oids = append(oids, o.ID)
	}
	pls, err := store.PlacementsOfOptions(ctx, tx, scope, oids)
	if err != nil {
		return nil, err
	}
	var rids []string
	for _, p := range pls {
		rids = append(rids, p.ResourceID)
	}
	refs, _ := store.ResourceRefs(ctx, tx, scope, rids)
	v := views.QuoteView{
		Marina: mv, Customer: partyView(ctx, tx, scope, q.CustomerID), Vessel: vesselView(ctx, tx, scope, q.VesselID),
		Quote: views.QuoteInfo{
			Code: ContractCode(SeriesQuote, q.Year, q.Number), IssuedOn: calendarOf(q.CreatedAt), Kind: q.Kind, Term: q.Term,
			StartsOn: q.StartsOn, EndsOn: q.EndsOn, Notes: views.Notes(q.Notes),
		},
		ValidUntil: q.ValidUntil, Options: []views.QuoteOption{},
	}
	for _, o := range opts {
		qo := views.QuoteOption{Seq: o.Seq, Label: o.Label, Price: priceView(o.PriceSnapshot, o.TotalCents, o.DepositCents), Placements: []views.Place{}}
		for _, p := range pls {
			if p.OptionID != o.ID {
				continue
			}
			pl := views.Place{Type: p.ResourceType, FirstNight: p.FirstNight, LastNight: p.LastNight}
			if r, ok := refs[p.ResourceID]; ok {
				pl.Code = r.Code
			}
			qo.Placements = append(qo.Placements, pl)
		}
		v.Options = append(v.Options, qo)
	}
	return v, nil
}

func (s *Service) addendumView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mv views.Marina, id string) (any, error) {
	ad, err := store.GetAddendumByID(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	c, err := store.GetContract(ctx, tx, scope, ad.ContractID)
	if err != nil {
		return nil, err
	}
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	pl := decodeAddendum(ad).Payload
	info := views.AddendumInfo{
		Seq: ad.Seq, Kind: ad.Kind, EffectiveOn: ad.EffectiveOn, Description: views.Notes(pl.Reason),
		OldTotal: money.Cents(c.TotalCents), NewTotal: money.Cents(c.TotalCents + ad.AmountDeltaCents), Delta: money.Cents(ad.AmountDeltaCents),
	}
	if ad.Status == string(AddendumActive) {
		info.OldTotal = money.Cents(c.TotalCents - ad.AmountDeltaCents)
		info.NewTotal = money.Cents(c.TotalCents)
	}
	if pl.VesselID != "" {
		nv := vesselView(ctx, tx, scope, pl.VesselID)
		info.NewVessel = &nv
	}
	return views.AddendumView{
		Marina: mv, Customer: partyView(ctx, tx, scope, c.CustomerID), Vessel: vesselView(ctx, tx, scope, c.VesselID),
		Contract: contractInfo(c), Addendum: info, Lines: []views.PriceLine{}, Installments: installmentsView(live),
	}, nil
}

func (s *Service) renewalView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mv views.Marina, id string) (any, error) {
	pr, err := store.GetRenewalProposal(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	src, err := store.GetContract(ctx, tx, scope, pr.SourceContractID)
	if err != nil {
		return nil, err
	}
	v := views.RenewalView{
		Marina: mv, Customer: partyView(ctx, tx, scope, src.CustomerID), Vessel: vesselView(ctx, tx, scope, src.VesselID),
		Contract: contractInfo(src), OldPrice: priceView(src.PriceSnapshot, src.TotalCents, src.DepositCents),
		Deadline: pr.DeadlineOn, Berths: []views.Place{}, Installments: []views.Installment{},
	}
	if pr.CampaignID != "" {
		if c, err := store.GetRenewalCampaign(ctx, tx, scope, pr.CampaignID); err == nil {
			v.Message = views.Notes(c.Message)
		}
	}
	if pr.NewContractID != "" {
		succ, err := store.GetContract(ctx, tx, scope, pr.NewContractID)
		if err != nil {
			return nil, err
		}
		v.NewContract = contractInfo(succ)
		v.NewPrice = priceView(succ.PriceSnapshot, succ.TotalCents, succ.DepositCents)
		v.Berths = s.places(ctx, tx, scope, succ.ID)
		live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{succ.ID}, true)
		if err != nil {
			return nil, err
		}
		v.Installments = installmentsView(live)
		if plan, err := s.altOf(ctx, tx, scope, succ); err == nil && plan != nil {
			v.EarlyPaymentTotal = plan.Total
		}
	}
	return v, nil
}

func (s *Service) installmentView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, mv views.Marina, id string) (any, error) {
	r, err := store.GetInstallment(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	c, err := store.GetContract(ctx, tx, scope, r.ContractID)
	if err != nil {
		return nil, err
	}
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	var open []store.Installment
	for _, x := range live {
		if x.Status == store.InstallmentIssued || x.Status == store.InstallmentScheduled {
			open = append(open, x)
		}
	}
	cust := c.BillingCustomerID
	if cust == "" {
		cust = c.CustomerID
	}
	ci := contractInfo(c)
	today := s.today(ctx, tx, scope)
	return views.InstallmentView{
		Marina: mv, Customer: partyView(ctx, tx, scope, cust), Contract: &ci,
		Installment: installmentsView([]store.Installment{*r})[0], Overdue: r.Status == store.InstallmentIssued && r.DueOn.Before(today),
		PaymentReference: Code(c) + "-" + itoa(r.Seq), Open: installmentsView(open),
	}, nil
}

// altOf is the early-payment alternative of a contract plan (nil when the policy has none).
func (s *Service) altOf(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) (*altTotal, error) {
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	pol, _ := DecodePolicy(c.Policy)
	pay, _, _ := effective(pol, cs)
	if pay.EarlyPayment == nil {
		return nil, nil
	}
	disc := money.Cents(c.TotalCents).PercentBP(pay.EarlyPayment.DiscountBP)
	return &altTotal{Total: money.Cents(c.TotalCents) - disc}, nil
}

type altTotal struct{ Total money.Cents }

func itoa(n int) string { return strconv.Itoa(n) }

// calendarOf is the UTC date of an instant (issue and signature dates printed on documents).
func calendarOf(t time.Time) calendar.Date { return calendar.In(t, time.UTC) }
