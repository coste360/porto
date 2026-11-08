package report

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/swap"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// The run<X> functions build one report on a resolved period (q.From, q.To set): the same code path for the
// report endpoint, the dashboard tile (with the tile's drill parameters) and the comparison (INV-62).

func calcDims(d Dims) calc.Dims {
	return calc.Dims{MapID: d.MapID, MapKind: d.MapKind, StructureID: d.StructureID, GroupID: d.GroupID, Category: d.Category}
}

func familiesOf(fam string) []string {
	if fam == "" {
		return []string{calc.FamilyBerth}
	}
	return []string{fam}
}

func (r *run) runOccupancy(q Params) (*OccupancyResult, calc.Facts, error) {
	in, _, err := r.occupancyInput(q.From, q.To, familiesOf(q.Family), q.Variant == "contractual", 0)
	if err != nil {
		return nil, nil, err
	}
	in.Variant, in.Family, in.ResourceType, in.Dims, in.GroupBy = q.Variant, q.Family, q.ResourceType, calcDims(q.Dims), q.GroupBy
	res, facts := calc.BuildOccupancy(in)
	return &res, facts, nil
}

func (r *run) runEfficiency(q Params) (*EfficiencyResult, calc.Facts, error) {
	in, _, err := r.occupancyInput(q.From, q.To, familiesOf(q.Family), false, 0)
	if err != nil {
		return nil, nil, err
	}
	in.Family, in.ResourceType, in.Dims, in.GroupBy = q.Family, q.ResourceType, calcDims(q.Dims), q.GroupBy
	res, facts := calc.BuildEfficiency(in)
	return &res, facts, nil
}

func (r *run) runVessels(q Params) (*VesselsResult, calc.Facts, error) {
	in, _, err := r.occupancyInput(q.From, q.To, []string{calc.FamilyBerth, calc.FamilyLand, calc.FamilyLinear}, false, 0)
	if err != nil {
		return nil, nil, err
	}
	in.Dims = calcDims(q.Dims)
	res := calc.BuildVessels(in, q.GroupBy)
	return &res, calc.Facts{}, nil
}

func (r *run) runBerthRevenue(q Params) (*BerthRevenueResult, calc.Facts, error) {
	in, _, err := r.occupancyInput(q.From, q.To, []string{calc.FamilyBerth}, false, 0)
	if err != nil {
		return nil, nil, err
	}
	in.Dims = calcDims(q.Dims)
	res, facts := calc.BuildBerthRevenue(in, q.GroupBy)
	return &res, facts, nil
}

// contractType maps a contract kind and term to the pricing contract type (period terms price as transits).
func contractType(kind, term string) (pricing.ContractType, bool) {
	switch kind {
	case calc.ContractTransit:
		return pricing.ContractTransit, true
	case calc.ContractCharter:
		return pricing.ContractCharter, true
	case calc.ContractMooring, "contract":
		switch term {
		case "annual":
			return pricing.ContractAnnual, true
		case "seasonal":
			return pricing.ContractSeasonal, true
		case "monthly":
			return pricing.ContractMonthly, true
		}
		return pricing.ContractTransit, true
	}
	return "", false
}

func (r *run) quoter() (*pricelist.ReportQuoter, error) {
	if r.s.d.Prices == nil {
		return nil, errors.New("report: no price list service")
	}
	return r.s.d.Prices.NewReportQuoter(r.ctx, r.tx, r.scope())
}

func (r *run) runWaste(q Params) (*WasteResult, error) {
	rules, err := r.fitRules()
	if err != nil {
		return nil, err
	}
	threshold := fit.FullBP - r.settings.WasteThresholdBP
	if q.ThresholdBP != nil {
		threshold = *q.ThresholdBP
	}
	suggest := !q.NoSuggestions
	// Suggestions need the whole stays: rows of up to one year around the period.
	lo, hi, margin := q.From, q.To, 0
	if suggest {
		margin = 366
		lo, hi = q.From.AddDays(-margin), q.To.AddDays(margin)
	}
	in, byID, err := r.occupancyInput(q.From, q.To, nil, false, margin)
	if err != nil {
		return nil, err
	}
	berthRows, err := store.ReportBerthNights(r.ctx, r.tx, r.scope(), store.ReportFilter{From: lo, To: hi})
	if err != nil {
		return nil, err
	}
	nightsOf := map[string][]calendar.Date{}
	busy := map[string]map[calendar.Date]string{}
	for _, row := range berthRows {
		if calc.Occupied(row.Kind) {
			nightsOf[row.AllocationID] = append(nightsOf[row.AllocationID], row.Night)
		}
		if busy[row.ResourceID] == nil {
			busy[row.ResourceID] = map[calendar.Date]string{}
		}
		busy[row.ResourceID][row.Night] = row.AllocationID
	}
	var berths []calc.Resource
	for _, res := range in.Resources {
		if res.Type == calc.TypeBerth {
			berths = append(berths, res)
		}
	}
	berthByID := map[string]*calc.Resource{}
	for i := range berths {
		berthByID[berths[i].ID] = &berths[i]
	}
	var qt *pricelist.ReportQuoter
	if r.acc.Full {
		if qt, err = r.quoter(); err != nil {
			return nil, err
		}
	}
	var was []calc.WasteAllocation
	for _, a := range in.Allocations {
		row := byID[a.ID]
		b := berthByID[a.ResourceID]
		if row == nil || b == nil || row.ResourceType != calc.TypeBerth || !calc.Occupied(a.Kind) {
			continue
		}
		wa := calc.WasteAllocation{
			Allocation: a, BerthID: b.ID, First: row.FirstNight, Last: row.LastNight,
			Nights: nightsOf[a.ID], Vessel: vesselOf(row),
		}
		listed := fit.FullBP-fit.FillBP(wa.Vessel, fit.Target{Kind: fit.TargetBerth, Length: b.Length, Width: b.Width}) > threshold
		if listed && qt != nil && a.Kind != calc.KindOwner {
			if ct, ok := contractType(a.ContractKind, row.ContractTerm); ok {
				total, err := qt.Total(r.ctx, pricelist.ReportQuote{
					ContractType: ct, Arrival: row.FirstNight, Departure: row.LastNight.AddDays(1), Category: b.Category,
					BerthLength: b.Length, BerthWidth: b.Width, GroupIDs: b.GroupIDs, LOA: b.MaxLOA, Beam: b.MaxBeam, QuotedOn: row.FirstNight,
				})
				if err == nil {
					wa.ListTotal = calc.Some(total)
				}
			}
		}
		was = append(was, wa)
	}
	res := calc.BuildWaste(calc.WasteInput{
		From: q.From, To: q.To, ThresholdBP: threshold, Full: r.acc.Full, GroupBy: q.GroupBy,
		Berths: berths, Dims: calcDims(q.Dims), Allocations: was,
	})
	if suggest {
		listed := map[string]bool{}
		for _, rec := range res.Records {
			listed[rec.AllocationID] = true
		}
		si := swap.Input{Busy: busy, Closed: map[string]map[calendar.Date]bool{}, Rules: rules, Today: r.today}
		for i := range berths {
			b := &berths[i]
			t := r.berthTarget(b.ID)
			if t == nil {
				continue
			}
			si.Berths = append(si.Berths, swap.Berth{ID: b.ID, Code: b.Code, MapID: b.MapID, Target: *t})
			if cn := calc.ClosedNights(b, in.Closures, lo, hi); len(cn) > 0 {
				m := map[calendar.Date]bool{}
				for _, d := range cn {
					m[d] = true
				}
				si.Closed[b.ID] = m
			}
		}
		for _, wa := range was {
			si.Allocs = append(si.Allocs, swap.Alloc{
				ID: wa.ID, BerthID: wa.BerthID, MapID: berthByID[wa.BerthID].MapID, Kind: wa.Kind,
				Nights: wa.Nights, Vessel: wa.Vessel, Listed: listed[wa.ID], VesselID: wa.VesselID,
			})
		}
		sugs, truncated := swap.Suggest(si)
		res.Suggestions = sugs
		if truncated {
			res.Notes = append(res.Notes, calc.Note{Code: "truncated_suggestions", Count: 1})
		}
	}
	if res.Suggestions == nil {
		res.Suggestions = []calc.WasteSuggestion{}
	}
	return &res, nil
}

func (r *run) runLostDemand(q Params) (*LostDemandResult, calc.Facts, error) {
	rows, err := store.ReportInquiries(r.ctx, r.tx, r.scope(), store.ReportFilter{From: q.From, To: q.To})
	if err != nil {
		return nil, nil, err
	}
	var qt *pricelist.ReportQuoter
	var berths []calc.Resource
	var rules fit.Rules
	if r.acc.Full {
		res, err := r.loadResources()
		if err != nil {
			return nil, nil, err
		}
		if rules, err = r.fitRules(); err != nil {
			return nil, nil, err
		}
		for _, b := range res {
			if b.Type == calc.TypeBerth {
				berths = append(berths, b)
			}
		}
		slices.SortStableFunc(berths, func(a, b calc.Resource) int {
			x, y := int64(a.Length)*int64(a.Width), int64(b.Length)*int64(b.Width)
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
			return compareStr(a.Code, b.Code)
		})
		if qt, err = r.quoter(); err != nil {
			return nil, nil, err
		}
	}
	in := calc.LostDemandInput{From: q.From, To: q.To, Full: r.acc.Full, GroupBy: q.GroupBy, Channel: q.Channel, Purpose: q.Purpose}
	for _, iq := range rows {
		ci := calc.Inquiry{
			ID: iq.ID, Arrival: iq.Arrival, Departure: iq.Departure, Outcome: iq.Outcome, Purpose: iq.Purpose,
			Term: iq.Term, Channel: iq.Channel, Source: iq.Source, LOA: geo.Cm(iq.LOACm), Beam: geo.Cm(iq.BeamCm),
		}
		if qt != nil {
			v := fit.Vessel{LOA: ci.LOA, Beam: ci.Beam}
			if iq.DraftCm > 0 {
				d := geo.Cm(iq.DraftCm)
				v.Draft = &d
			}
			for i := range berths {
				b := &berths[i]
				t := r.berthTarget(b.ID)
				if t == nil || !fit.Check(v, *t, iq.Departure.AddDays(-1), rules).OK {
					continue
				}
				ci.PricedCategory = b.Category
				ct, _ := contractType(iq.Purpose, iq.Term)
				total, err := qt.Total(r.ctx, pricelist.ReportQuote{
					ContractType: ct, Arrival: iq.Arrival, Departure: iq.Departure,
					Category: b.Category, BerthLength: b.Length, BerthWidth: b.Width, GroupIDs: b.GroupIDs, LOA: ci.LOA, Beam: ci.Beam,
					QuotedOn: iq.Arrival,
				})
				if err == nil {
					ci.Value = calc.Some(total)
				}
				break
			}
		}
		in.Inquiries = append(in.Inquiries, ci)
	}
	res, facts := calc.BuildLostDemand(in)
	return &res, facts, nil
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// qualifyingStatuses is the status filter of the qualifying contracts (§3.6).
func qualifyingStatuses() map[string][]string {
	return map[string][]string{
		calc.ContractMooring: calc.QualifyingStatuses(calc.ContractMooring),
		calc.ContractCharter: calc.QualifyingStatuses(calc.ContractCharter),
		calc.ContractTransit: calc.QualifyingStatuses(calc.ContractTransit),
	}
}

func contracts(rows []store.ReportContractRow) []calc.Contract {
	out := make([]calc.Contract, 0, len(rows))
	for _, c := range rows {
		out = append(out, calc.Contract{
			ID: c.ID, Number: contractCode(c.Kind, c.Year, c.Number), Kind: c.Kind, Term: c.Term,
			Status: c.Status, Channel: c.Channel, CustomerID: c.CustomerID, VesselID: c.VesselID, Category: c.Category,
			StartsOn: c.StartsOn, EndsOn: c.EndsOn, Total: money.Cents(c.TotalCents),
		})
	}
	return out
}

func customerIDs[T any](rows []T, id func(T) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if k := id(r); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func (r *run) runProduction(q Params) (*ProductionResult, calc.Facts, error) {
	rows, err := store.ReportContracts(r.ctx, r.tx, r.scope(), store.ReportContractFilter{Statuses: qualifyingStatuses(), StartsBy: q.To, EndsAfter: q.From})
	if err != nil {
		return nil, nil, err
	}
	cs := contracts(rows)
	names, err := r.customerNames(customerIDs(cs, func(c calc.Contract) string { return c.CustomerID }))
	if err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildProduction(calc.ProductionInput{
		From: q.From, To: q.To, GroupBy: q.GroupBy, Kind: q.Kind,
		CustomerID: q.CustomerID, Contracts: cs, Names: names,
	})
	return &res, facts, nil
}

func openCharges(rows []store.ReportOpenChargeRow, asOf calendar.Date) []calc.OpenCharge {
	out := make([]calc.OpenCharge, 0, len(rows))
	for _, c := range rows {
		out = append(out, calc.OpenCharge{
			ID: c.ID, Number: c.Number, CustomerID: c.CustomerID, OriginType: c.OriginType,
			IssuedOn: c.IssuedOn, DueOn: c.DueOn, Total: money.Cents(c.TotalCents), Paid: money.Cents(c.PaidAmountCents),
			Credited: money.Cents(c.CreditedCents), Bucket: payment.BucketOf(c.DueOn, asOf),
		})
	}
	return out
}

func (r *run) runCustomers(q Params) (*CustomersResult, calc.Facts, error) {
	rows, err := store.ReportContracts(r.ctx, r.tx, r.scope(), store.ReportContractFilter{Statuses: qualifyingStatuses(), StartsBy: q.To})
	if err != nil {
		return nil, nil, err
	}
	cs := contracts(rows)
	infos, err := r.customerInfos(customerIDs(cs, func(c calc.Contract) string { return c.CustomerID }))
	if err != nil {
		return nil, nil, err
	}
	open, err := store.ReportOpenCharges(r.ctx, r.tx, r.scope(), r.today)
	if err != nil {
		return nil, nil, err
	}
	ren, err := store.ReportRenewals(r.ctx, r.tx, r.scope())
	if err != nil {
		return nil, nil, err
	}
	in := calc.CustomersInput{
		From: q.From, To: q.To, GroupBy: q.GroupBy, Section: q.Section, NewOnly: q.NewOnly, Contracts: cs,
		Customers: infos, Arrears: calc.ArrearsOf(openCharges(open, r.today), r.today),
	}
	for _, x := range ren {
		in.Renewals = append(in.Renewals, calc.Renewal{SourceContractID: x.SourceContractID, NewContractID: x.NewContractID})
	}
	res, facts := calc.BuildCustomers(in)
	return &res, facts, nil
}

func chargeLines(rows []store.ReportChargeLineRow) []calc.ChargeLine {
	out := make([]calc.ChargeLine, 0, len(rows))
	for _, l := range rows {
		out = append(out, calc.ChargeLine{
			ChargeID: l.ChargeID, Number: l.Number, Seq: l.Seq, IssuedOn: l.IssuedOn, DueOn: l.DueOn,
			OriginType: l.OriginType, CustomerID: l.CustomerID, ContractID: l.ContractID,
			ContractNumber: contractCode(l.ContractKind, l.ContractYear, l.ContractNumber), ItemID: l.ItemID, ItemCode: l.ItemCode,
			Description: l.Description, RevenueClass: l.RevenueClass, VATRateBP: l.VATRateBP, VATNature: l.VATNature,
			ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo, Net: money.Cents(l.NetCents), VAT: money.Cents(l.VATCents),
			Gross: money.Cents(l.GrossCents), PaymentStatus: l.PaymentStatus, InvoicingStatus: l.InvoicingStatus, FiscalMode: l.FiscalMode,
		})
	}
	return out
}

func (r *run) lines(q Params) ([]calc.ChargeLine, map[string]string, error) {
	rows, err := store.ReportChargeLines(r.ctx, r.tx, r.scope(), store.ReportFilter{From: q.From, To: q.To})
	if err != nil {
		return nil, nil, err
	}
	ls := chargeLines(rows)
	names, err := r.customerNames(customerIDs(ls, func(l calc.ChargeLine) string { return l.CustomerID }))
	return ls, names, err
}

func chargeFilter(q Params) calc.ChargeFilter {
	return calc.ChargeFilter{
		CustomerID: q.CustomerID, ItemID: q.ItemID, Origins: q.Origins, PaymentStatus: q.PaymentStatus,
		InvoicingStatus: q.InvoicingStatus, FiscalMode: q.FiscalMode,
	}
}

func (r *run) runCharges(q Params) (*ChargesResult, calc.Facts, error) {
	ls, names, err := r.lines(q)
	if err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildCharges(calc.ChargesInput{
		From: q.From, To: q.To, Basis: q.Basis, GroupBy: q.GroupBy,
		Filter: chargeFilter(q), Lines: ls, Names: names,
	})
	return &res, facts, nil
}

func (r *run) runRevenueClasses(q Params) (*RevenueClassesResult, error) {
	ls, names, err := r.lines(q)
	if err != nil {
		return nil, err
	}
	rows, err := store.ReportRevenueClasses(r.ctx, r.tx, r.scope())
	if err != nil {
		return nil, err
	}
	var classes []calc.RevenueClass
	for _, c := range rows {
		var name struct {
			It string `json:"it"`
			En string `json:"en"`
		}
		_ = json.Unmarshal(c.Name, &name)
		label := name.It
		if q.Lang == "en" && name.En != "" {
			label = name.En
		}
		classes = append(classes, calc.RevenueClass{Code: c.Code, Label: label, ParentCode: c.ParentCode})
	}
	res := calc.BuildRevenueClasses(calc.RevenueInput{
		From: q.From, To: q.To, Basis: q.Basis, Filter: chargeFilter(Params{}),
		Lines: ls, Classes: classes, Names: names,
	})
	return &res, nil
}

func (r *run) runAccruals(q Params) (*AccrualsResult, error) {
	ls, names, err := r.lines(q)
	if err != nil {
		return nil, err
	}
	rows, err := store.ReportContracts(r.ctx, r.tx, r.scope(), store.ReportContractFilter{Statuses: qualifyingStatuses(), StartsBy: q.To})
	if err != nil {
		return nil, err
	}
	cs := contracts(rows)
	chs, err := store.ReportContractCharges(r.ctx, r.tx, r.scope(), q.To)
	if err != nil {
		return nil, err
	}
	more, err := r.customerNames(customerIDs(cs, func(c calc.Contract) string { return c.CustomerID }))
	if err != nil {
		return nil, err
	}
	for k, v := range more {
		names[k] = v
	}
	in := calc.AccrualsInput{
		From: q.From, To: q.To, GroupBy: q.GroupBy, Section: q.Section, CustomerID: q.CustomerID, Lines: ls,
		Contracts: cs, Names: names,
	}
	for _, c := range chs {
		in.ContractCharges = append(in.ContractCharges, calc.ContractCharge{ContractID: c.ContractID, IssuedOn: c.IssuedOn, Gross: money.Cents(c.TotalCents)})
	}
	res := calc.BuildAccruals(in)
	return &res, nil
}

func (r *run) runPayments(q Params) (*PaymentsResult, calc.Facts, error) {
	f := store.ReportFilter{From: q.From, To: q.To}
	ps, err := store.ReportPayments(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, nil, err
	}
	rs, err := store.ReportRefunds(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, nil, err
	}
	in := calc.PaymentsInput{From: q.From, To: q.To, GroupBy: q.GroupBy, Section: q.Section, Method: q.Method, CustomerID: q.CustomerID}
	var ids []string
	for _, p := range ps {
		in.Payments = append(in.Payments, calc.Payment{
			ID: p.ID, Number: p.Number, Kind: p.Kind, Method: p.Method, Source: p.Source,
			ProviderCode: p.ProviderCode, CustomerID: p.CustomerID, Amount: money.Cents(p.AmountCents), Fee: money.Cents(p.FeeCents),
			ReceivedOn: p.ReceivedOn,
		})
		ids = append(ids, p.CustomerID)
	}
	for _, x := range rs {
		in.Refunds = append(in.Refunds, calc.Refund{
			ID: x.ID, Number: x.Number, Kind: x.Kind, Status: x.Status, Method: x.Method,
			Source: x.Source, ProviderCode: x.ProviderCode, CustomerID: x.CustomerID, Amount: money.Cents(x.AmountCents),
			BusinessDate: x.BusinessDate,
		})
		ids = append(ids, x.CustomerID)
	}
	if in.Names, err = r.customerNames(customerIDs(ids, func(s string) string { return s })); err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildPayments(in)
	return &res, facts, nil
}

func (r *run) runReceivables(q Params) (*ReceivablesResult, error) {
	asOf := q.AsOf
	open, err := store.ReportOpenCharges(r.ctx, r.tx, r.scope(), asOf)
	if err != nil {
		return nil, err
	}
	inst, err := store.ReportScheduledInstallments(r.ctx, r.tx, r.scope(), asOf.AddDays(90))
	if err != nil {
		return nil, err
	}
	rem, err := store.ReportReminders(r.ctx, r.tx, r.scope(), r.localDayStart(q.From), r.localDayStart(q.To.AddDays(1)))
	if err != nil {
		return nil, err
	}
	in := calc.ReceivablesInput{
		AsOf: asOf, GroupBy: q.GroupBy, Section: q.Section, OverdueOnly: q.OverdueOnly, CustomerID: q.CustomerID,
		Origin: q.Origin, Open: openCharges(open, asOf),
	}
	var ids []string
	for _, c := range open {
		ids = append(ids, c.CustomerID)
	}
	for _, i := range inst {
		label := contractCode(i.ContractKind, i.ContractYear, i.ContractNumber)
		in.Future = append(in.Future, calc.ScheduledInstallment{
			Kind: i.Kind, ID: i.ID, Number: label, CustomerID: i.CustomerID,
			DueOn: i.DueOn, Amount: money.Cents(i.AmountCents),
		})
		ids = append(ids, i.CustomerID)
	}
	for _, x := range rem {
		in.Reminders = append(in.Reminders, calc.Reminder{
			ID: x.ID, ChargeID: x.ChargeID, ChargeNumber: x.ChargeNumber,
			CustomerID: x.CustomerID, Level: x.Level, SentOn: calendar.In(x.UpdatedAt, r.loc),
		})
		ids = append(ids, x.CustomerID)
	}
	if in.Names, err = r.customerNames(customerIDs(ids, func(s string) string { return s })); err != nil {
		return nil, err
	}
	res := calc.BuildReceivables(in)
	return &res, nil
}

func (r *run) runPresences(q Params) (*PresencesResult, calc.Facts, error) {
	in := calc.PresencesInput{From: q.From, To: q.To, Today: r.today, Full: r.acc.Full, GroupBy: q.GroupBy, Section: q.Section}
	rows, err := store.ReportContracts(r.ctx, r.tx, r.scope(), store.ReportContractFilter{
		Statuses: map[string][]string{calc.ContractMooring: calc.QualifyingStatuses(calc.ContractMooring)}, StartsBy: q.To, EndsAfter: q.From,
	})
	if err != nil {
		return nil, nil, err
	}
	var vids []string
	for _, c := range rows {
		if c.Term == "annual" {
			vids = append(vids, c.VesselID)
		}
	}
	// The state of each annual vessel when the period starts is its last event before From, however old
	// (a vessel that left years ago and never came back is absent the whole period, §3.13).
	seed, err := store.ReportLastPresencesBefore(r.ctx, r.tx, r.scope(), vids, q.From)
	if err != nil {
		return nil, nil, err
	}
	evs, err := store.ReportPresences(r.ctx, r.tx, r.scope(), store.ReportFilter{From: q.From, To: q.To})
	if err != nil {
		return nil, nil, err
	}
	for _, e := range append(seed, evs...) {
		in.Events = append(in.Events, calc.PresenceEvent{
			ID: e.ID, VesselID: e.VesselID, VesselName: e.VesselName, Event: e.Event,
			Night: e.Night, ContractID: e.ContractID, ContractNumber: contractCode(e.ContractKind, e.ContractYear, e.ContractNumber),
			ContractKind: e.ContractKind, BerthCode: e.BerthCode,
		})
	}
	vs, err := store.ReportVessels(r.ctx, r.tx, r.scope(), vids)
	if err != nil {
		return nil, nil, err
	}
	vname := map[string]string{}
	for _, v := range vs {
		vname[v.ID] = v.Name
	}
	for _, c := range rows {
		if c.Term != "annual" {
			continue
		}
		in.Contracts = append(in.Contracts, calc.AbsenceContract{
			ID: c.ID, Number: contractCode(c.Kind, c.Year, c.Number),
			VesselID: c.VesselID, VesselName: vname[c.VesselID], StartsOn: c.StartsOn, EndsOn: c.EndsOn,
		})
	}
	rels, err := store.ReportReleases(r.ctx, r.tx, r.scope(), store.ReportFilter{From: q.From, To: q.To})
	if err != nil {
		return nil, nil, err
	}
	var resold []string
	for _, rl := range rels {
		if rl.ResoldAllocationID != "" {
			resold = append(resold, rl.ResoldAllocationID)
		}
	}
	nights, err := store.ReportAllocationNights(r.ctx, r.tx, r.scope(), resold)
	if err != nil {
		return nil, nil, err
	}
	nightsOf := map[string][]calendar.Date{}
	for _, n := range nights {
		nightsOf[n.AllocationID] = append(nightsOf[n.AllocationID], n.Night)
	}
	resale, err := store.ReportAllocations(r.ctx, r.tx, r.scope(), store.ReportAllocationFilter{IDs: resold})
	if err != nil {
		return nil, nil, err
	}
	resaleOf := map[string]*store.ReportAllocationRow{}
	for i := range resale {
		resaleOf[resale[i].ID] = &resale[i]
	}
	res, err := r.loadResources()
	if err != nil {
		return nil, nil, err
	}
	code := map[string]string{}
	for _, x := range res {
		code[x.ID] = x.Code
	}
	relVessels := map[string]string{}
	if len(rels) > 0 {
		var ids []string
		for _, rl := range rels {
			ids = append(ids, rl.VesselID)
		}
		vs, err := store.ReportVessels(r.ctx, r.tx, r.scope(), ids)
		if err != nil {
			return nil, nil, err
		}
		for _, v := range vs {
			relVessels[v.ID] = v.Name
		}
	}
	for _, rl := range rels {
		ri := calc.ReleaseInfo{
			ID: rl.ID, ContractID: rl.ContractID, ContractNumber: contractCode(rl.ContractKind, rl.ContractYear, rl.ContractNumber),
			VesselName: relVessels[rl.VesselID], BerthCode: code[rl.ResourceID], From: rl.FromNight, To: rl.ToNight,
			Credit: money.Cents(rl.CreditCents), ResoldNights: nightsOf[rl.ResoldAllocationID],
		}
		if a := resaleOf[rl.ResoldAllocationID]; a != nil && calc.Qualifying(a.ContractKind, a.ContractStatus) {
			ri.Resale = calc.ContractValue{Total: money.Cents(a.ContractTotal), StartsOn: a.ContractStartsOn, EndsOn: a.ContractEndsOn}
		}
		in.Releases = append(in.Releases, ri)
	}
	out, facts := calc.BuildPresences(in)
	return &out, facts, nil
}

func (r *run) runRights(q Params) (*RightsResult, error) {
	f := store.ReportFilter{From: q.From, To: q.To}
	hs, err := store.ReportRights(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, err
	}
	chs, err := store.ReportHolderCharges(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, err
	}
	runs, err := store.ReportCommonRuns(r.ctx, r.tx, r.scope(), r.localDayStart(q.From), r.localDayStart(q.To.AddDays(1)))
	if err != nil {
		return nil, err
	}
	ms, err := store.ReportMandates(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, err
	}
	in := calc.RightsInput{GroupBy: q.GroupBy, Section: q.Section}
	var ids []string
	for _, h := range hs {
		in.Holders = append(in.Holders, calc.RightHolder{
			RightID: h.RightID, BerthID: h.BerthID, BerthCode: h.BerthCode,
			HolderID: h.CustomerID, ShareMillis: h.ShareMillis,
		})
		ids = append(ids, h.CustomerID)
	}
	for _, c := range chs {
		in.Charges = append(in.Charges, calc.HolderCharge{
			ChargeID: c.ID, Number: c.Number, RightID: c.RightID, CustomerID: c.CustomerID,
			IssuedOn: c.IssuedOn, Total: money.Cents(c.TotalCents), Paid: money.Cents(c.PaidAmountCents), Credited: money.Cents(c.CreditedCents),
		})
		ids = append(ids, c.CustomerID)
	}
	for _, a := range runs {
		in.Allotment = append(in.Allotment, calc.Allotment{
			RunID: a.RunID, RunKind: a.RunKind, ItemCode: a.ItemName, BerthID: a.BerthID,
			BerthCode: a.BerthCode, RightID: a.RightID, CustomerID: a.CustomerID, Date: calendar.In(a.IssuedAt, r.loc),
			Amount: money.Cents(a.AmountCents),
		})
		ids = append(ids, a.CustomerID)
	}
	for _, m := range ms {
		ref := map[string]string{"revenue": "mandate", "credit": "payment", "payout": "refund"}[m.Kind]
		in.Mandates = append(in.Mandates, calc.MandateMove{
			Kind: m.Kind, ID: m.ID, MandateID: m.MandateID, RightID: m.RightID,
			Date: m.Date, Amount: money.Cents(m.AmountCents), RefType: ref,
		})
	}
	if in.Names, err = r.customerNames(customerIDs(ids, func(s string) string { return s })); err != nil {
		return nil, err
	}
	res := calc.BuildRights(in)
	return &res, nil
}

func (r *run) runCash(q Params) (*CashResult, error) {
	f := store.ReportFilter{From: q.From, To: q.To}
	ss, err := store.ReportCashSessions(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, err
	}
	ds, err := store.ReportFiscalDocuments(r.ctx, r.tx, r.scope(), f)
	if err != nil {
		return nil, err
	}
	in := calc.CashInput{GroupBy: q.GroupBy, Section: q.Section}
	for _, s := range ss {
		cs := calc.CashSession{
			ID: s.ID, RegisterCode: s.RegisterCode, Status: s.Status, BusinessDate: s.BusinessDate,
			Float: money.Cents(s.FloatCents), Difference: money.Cents(s.DifferenceCents),
		}
		_ = json.Unmarshal(s.Expected, &cs.Expected)
		_ = json.Unmarshal(s.Counted, &cs.Counted)
		in.Sessions = append(in.Sessions, cs)
	}
	for _, d := range ds {
		var buckets []struct {
			RateBP int64       `json:"vat_rate_bp"`
			Nature string      `json:"vat_nature"`
			Net    money.Cents `json:"net_cents"`
			VAT    money.Cents `json:"vat_cents"`
			Gross  money.Cents `json:"gross_cents"`
		}
		_ = json.Unmarshal(d.VAT, &buckets)
		fd := calc.FiscalDocument{
			ID: d.ID, Number: d.Number, Status: d.Status, BusinessDate: d.BusinessDate, Method: d.Method,
			Total: money.Cents(d.TotalCents),
		}
		for _, b := range buckets {
			fd.VAT = append(fd.VAT, summary.VATRow{RateBP: b.RateBP, Nature: b.Nature, Net: b.Net, VAT: b.VAT, Gross: b.Gross})
		}
		in.Documents = append(in.Documents, fd)
	}
	res := calc.BuildCash(in)
	return &res, nil
}
