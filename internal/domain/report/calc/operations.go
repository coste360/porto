package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// ---- R13 presences ----

// PresenceEvent is an arrived or departed event (in time order).
type PresenceEvent struct {
	ID             string
	VesselID       string
	VesselName     string
	Event          string // arrived, departed
	Night          calendar.Date
	ContractID     string
	ContractNumber string
	ContractKind   string
	BerthCode      string
}

// AbsenceContract is an active annual mooring contract with its vessel.
type AbsenceContract struct {
	ID, Number string
	VesselID   string
	VesselName string
	StartsOn   calendar.Date
	EndsOn     calendar.Date // exclusive
}

// ReleaseInfo is an active release overlapping the period: ResoldNights are the nights of its range held by the
// resale allocation; Resale is the resale contract value (release revenue).
type ReleaseInfo struct {
	ID             string
	ContractID     string
	ContractNumber string
	VesselName     string
	BerthCode      string
	From, To       calendar.Date
	Credit         money.Cents
	ResoldNights   []calendar.Date
	Resale         ContractValue
}

// PresencesInput is the input of BuildPresences. Events may start before the period (absences need the last
// departure before it).
type PresencesInput struct {
	From, To, Today calendar.Date
	Full            bool
	GroupBy         string
	Section         string // movements (default), absences, releases
	Events          []PresenceEvent
	Contracts       []AbsenceContract
	Releases        []ReleaseInfo
}

// BuildPresences builds R13 (§3.13).
func BuildPresences(in PresencesInput) (PresencesResult, Facts) {
	arrivals, departures := Daily{}, Daily{}
	var moves, abs, rels []PresencesRecord
	byVessel := map[string][]PresenceEvent{}
	for _, e := range in.Events {
		if e.Event != "arrived" && e.Event != "departed" {
			continue
		}
		byVessel[e.VesselID] = append(byVessel[e.VesselID], e)
		if !inPeriod(e.Night, in.From, in.To) {
			continue
		}
		moves = append(moves, PresencesRecord{
			Section: "movements", PresenceID: e.ID, Event: e.Event, Night: e.Night,
			ContractID: e.ContractID, ContractNumber: e.ContractNumber, ContractKind: e.ContractKind, VesselID: e.VesselID,
			VesselName: e.VesselName, BerthCode: e.BerthCode, Ref: Ref{Type: "presence", ID: e.ID, Label: e.VesselName},
		})
		if e.Event == "arrived" {
			arrivals.Add(e.Night, 1, 0)
		} else {
			departures.Add(e.Night, 1, 0)
		}
	}
	end := in.To
	if !in.Today.IsZero() && in.Today.Before(end) {
		end = in.Today
	}
	for _, c := range in.Contracts {
		lo, hi := maxDate(c.StartsOn, in.From), c.EndsOn.AddDays(-1)
		if end.Before(hi) {
			hi = end
		}
		if hi.Before(lo) {
			continue
		}
		evs := slices.Clone(byVessel[c.VesselID])
		slices.SortStableFunc(evs, func(a, b PresenceEvent) int { return a.Night.Compare(b.Night) })
		var n int64
		var outFrom calendar.Date
		out := false
		for _, e := range evs {
			switch {
			case e.Event == "departed" && !out:
				out, outFrom = true, e.Night
			case e.Event == "arrived" && out:
				n += overlapDays(outFrom, e.Night.AddDays(-1), lo, hi)
				out = false
			}
		}
		if out {
			n += overlapDays(outFrom, hi, lo, hi)
		}
		if n > 0 {
			abs = append(abs, PresencesRecord{
				Section: "absences", ContractID: c.ID, ContractNumber: c.Number, ContractKind: ContractMooring,
				VesselID: c.VesselID, VesselName: c.VesselName, AbsenceNights: n, Ref: Ref{Type: "contract", ID: c.ID, Label: c.Number},
			})
		}
	}
	for _, r := range in.Releases {
		released := overlapDays(r.From, r.To, in.From, in.To)
		if released == 0 {
			continue
		}
		rec := PresencesRecord{
			Section: "releases", ReleaseID: r.ID, ReleaseFrom: r.From, ReleaseTo: r.To, ContractID: r.ContractID,
			ContractNumber: r.ContractNumber, ContractKind: ContractMooring, VesselName: r.VesselName, BerthCode: r.BerthCode,
			ReleasedNights: released, Ref: Ref{Type: "release", ID: r.ID, Label: r.ContractNumber},
		}
		var revenue money.Cents
		for _, d := range r.ResoldNights {
			if inPeriod(d, in.From, in.To) && inPeriod(d, r.From, r.To) {
				rec.ResoldNights++
				revenue += r.Resale.On(d)
			}
		}
		if in.Full {
			rec.Credit, rec.Revenue = Some(r.Credit), Some(revenue)
		}
		rels = append(rels, rec)
	}
	slices.SortStableFunc(moves, func(a, b PresencesRecord) int {
		if c := a.Night.Compare(b.Night); c != 0 {
			return c
		}
		return compareKeys([]string{a.Event, a.VesselName, a.PresenceID}, []string{b.Event, b.VesselName, b.PresenceID})
	})
	slices.SortStableFunc(abs, func(a, b PresencesRecord) int {
		return compareKeys([]string{a.ContractNumber, a.ContractID}, []string{b.ContractNumber, b.ContractID})
	})
	slices.SortStableFunc(rels, func(a, b PresencesRecord) int {
		if c := a.ReleaseFrom.Compare(b.ReleaseFrom); c != 0 {
			return c
		}
		return compareKeys([]string{a.ContractNumber, a.ReleaseID}, []string{b.ContractNumber, b.ReleaseID})
	})
	all := append(append(slices.Clone(moves), abs...), rels...)
	fold := func(rs []PresencesRecord) PresencesTotals { return FoldPresences(in.Full, rs) }
	res := PresencesResult{Totals: fold(all)}
	switch in.Section {
	case "absences":
		res.Records = abs
	case "releases":
		res.Records = rels
	default:
		res.Records = moves
	}
	key := map[string]func(PresencesRecord) string{
		"day":           func(r PresencesRecord) string { return r.Night.String() },
		"week":          func(r PresencesRecord) string { return WeekKey(r.Night) },
		"month":         func(r PresencesRecord) string { return MonthKey(r.Night) },
		"contract_kind": func(r PresencesRecord) string { return orNone(r.ContractKind) },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(moves, func(r PresencesRecord) []string { return []string{key(r)} }, fold)
	}
	return res, Facts{IndArrivals: arrivals, IndDepartures: departures}
}

// FoldPresences is the only producer of presences totals.
func FoldPresences(full bool, recs []PresencesRecord) PresencesTotals {
	t := PresencesTotals{}
	var credit, revenue money.Cents
	for _, r := range recs {
		switch r.Section {
		case "movements":
			if r.Event == "arrived" {
				t.Arrivals++
			} else {
				t.Departures++
			}
		case "absences":
			t.AbsenceNights += r.AbsenceNights
		case "releases":
			t.Releases++
			t.ReleasedNights += r.ReleasedNights
			t.ResoldNights += r.ResoldNights
			credit += r.Credit.Value
			revenue += r.Revenue.Value
		}
	}
	if full {
		t.ReleaseCredit, t.ReleaseRevenue = Some(credit), Some(revenue)
	}
	return t
}

// ---- R14 rights and condominium ----

// RightHolder is a holder of a right of use active in the period.
type RightHolder struct {
	RightID     string
	BerthID     string
	BerthCode   string
	HolderID    string
	ShareMillis int64
}

// HolderCharge is a charge of a holder instalment (or of a right) issued in the period.
type HolderCharge struct {
	ChargeID, Number string
	RightID          string
	CustomerID       string
	IssuedOn         calendar.Date
	Total            money.Cents
	Paid             money.Cents
	Credited         money.Cents
}

// Allotment is a common-charge allocation of an issued run (advance = ripartizione, balance = conguaglio);
// CustomerID empty is the marina's own share.
type Allotment struct {
	RunID      string
	RunKind    string
	ItemCode   string
	BerthID    string
	BerthCode  string
	RightID    string
	CustomerID string
	Date       calendar.Date
	Amount     money.Cents
}

// MandateMove is a mandate revenue (Kind revenue), a payout (refunds payout) or a credit (payments from mandate).
type MandateMove struct {
	Kind      string // revenue, payout, credit
	ID        string
	MandateID string
	RightID   string
	Date      calendar.Date
	Amount    money.Cents
	RefType   string // mandate, refund, payment
}

// RightsInput is the input of BuildRights.
type RightsInput struct {
	GroupBy   string
	Section   string // holders (default), charges, allotments, mandates
	Holders   []RightHolder
	Charges   []HolderCharge
	Allotment []Allotment
	Mandates  []MandateMove
	Names     map[string]string
}

// BuildRights builds R14 (§3.14).
func BuildRights(in RightsInput) RightsResult {
	var hs, cs, as, ms []RightsRecord
	for _, h := range in.Holders {
		hs = append(hs, RightsRecord{
			Section: "holders", RightID: h.RightID, RightNumber: h.BerthCode, BerthID: h.BerthID,
			BerthCode: h.BerthCode, HolderID: h.HolderID, HolderName: in.Names[h.HolderID], ShareMillis: h.ShareMillis,
			Ref: Ref{Type: "right", ID: h.RightID, Label: h.BerthCode},
		})
	}
	for _, c := range in.Charges {
		cs = append(cs, RightsRecord{
			Section: "charges", RightID: c.RightID, HolderID: c.CustomerID, HolderName: in.Names[c.CustomerID],
			ChargeID: c.ChargeID, Number: c.Number, IssuedOn: c.IssuedOn, Date: c.IssuedOn, Issued: c.Total, Paid: c.Paid,
			Residual: c.Total - c.Paid - c.Credited, Ref: Ref{Type: "charge", ID: c.ChargeID, Label: c.Number},
		})
	}
	for _, a := range in.Allotment {
		as = append(as, RightsRecord{
			Section: "allotments", RightID: a.RightID, BerthID: a.BerthID, BerthCode: a.BerthCode,
			HolderID: a.CustomerID, HolderName: in.Names[a.CustomerID], RunID: a.RunID, RunKind: a.RunKind, ItemCode: a.ItemCode,
			Date: a.Date, Amount: a.Amount, Ref: Ref{Type: "common_charge_run", ID: a.RunID, Label: a.RunKind},
		})
	}
	for _, m := range in.Mandates {
		ms = append(ms, RightsRecord{
			Section: "mandates", RightID: m.RightID, MandateID: m.MandateID, Kind: m.Kind, Date: m.Date,
			Amount: m.Amount, Ref: Ref{Type: m.RefType, ID: m.ID},
		})
	}
	slices.SortStableFunc(hs, func(a, b RightsRecord) int {
		return compareKeys([]string{a.BerthCode, a.RightID, a.HolderName, a.HolderID}, []string{b.BerthCode, b.RightID, b.HolderName, b.HolderID})
	})
	byDate := func(a, b RightsRecord) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return compareKeys([]string{a.Number, a.RunID, a.BerthCode, a.ItemCode, a.HolderID, a.Ref.ID},
			[]string{b.Number, b.RunID, b.BerthCode, b.ItemCode, b.HolderID, b.Ref.ID})
	}
	slices.SortStableFunc(cs, byDate)
	slices.SortStableFunc(as, byDate)
	slices.SortStableFunc(ms, byDate)
	all := append(append(append(slices.Clone(hs), cs...), as...), ms...)
	res := RightsResult{Totals: FoldRights(all)}
	switch in.Section {
	case "charges":
		res.Records = cs
	case "allotments":
		res.Records = as
	case "mandates":
		res.Records = ms
	default:
		res.Records = hs
	}
	key := map[string]func(RightsRecord) []string{
		"item":     func(r RightsRecord) []string { return keyIf(r.Section == "allotments", r.ItemCode) },
		"run_kind": func(r RightsRecord) []string { return keyIf(r.Section == "allotments", r.RunKind) },
		"holder":   func(r RightsRecord) []string { return keyIf(r.Section != "mandates", orNone(r.HolderID)) },
		"month":    func(r RightsRecord) []string { return keyIf(!r.Date.IsZero(), MonthKey(r.Date)) },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(all, key, FoldRights)
	}
	return res
}

func keyIf(ok bool, k string) []string {
	if !ok {
		return nil
	}
	return []string{k}
}

// FoldRights is the only producer of rights totals.
func FoldRights(recs []RightsRecord) RightsTotals {
	t := RightsTotals{}
	rights, holders, berths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range recs {
		switch r.Section {
		case "holders":
			rights[r.RightID], holders[r.HolderID], berths[r.BerthID] = true, true, true
		case "charges":
			t.ChargesIssued += r.Issued
			t.ChargesCollected += r.Paid
			t.ChargesResidual += r.Residual
		case "allotments":
			if r.RunKind == "balance" {
				t.Balance += r.Amount
			} else {
				t.Advance += r.Amount
			}
			if r.HolderID == "" {
				t.MarinaShare += r.Amount
			}
		case "mandates":
			if r.Kind == "revenue" {
				t.MandateRevenue += r.Amount
			} else {
				t.MandatePayout += r.Amount
			}
		}
	}
	t.Rights, t.Holders, t.Berths = int64(len(rights)), int64(len(holders)), int64(len(berths))
	return t
}

// ---- R15 cash and fiscal receipts ----

// CashSession is a cash session of the period with its expected and counted amounts by method.
type CashSession struct {
	ID           string
	RegisterCode string
	Status       string
	BusinessDate calendar.Date
	Float        money.Cents
	Expected     map[string]money.Cents
	Counted      map[string]money.Cents
	Difference   money.Cents
}

// FiscalDocument is an issued or void fiscal document (corrispettivo) with its VAT rows and payment method.
type FiscalDocument struct {
	ID, Number   string
	Status       string // issued, void
	BusinessDate calendar.Date
	Method       string
	Total        money.Cents
	VAT          []summary.VATRow
}

// CashInput is the input of BuildCash.
type CashInput struct {
	GroupBy   string
	Section   string // sessions (default), fiscal
	Sessions  []CashSession
	Documents []FiscalDocument
}

func sumMap(m map[string]money.Cents) money.Cents {
	var s money.Cents
	for _, v := range m {
		s += v
	}
	return s
}

// BuildCash builds R15 (§3.15): issued documents make the totals, void ones are listed apart (D15).
func BuildCash(in CashInput) CashResult {
	var ss, fs []CashRecord
	vatOf := map[string][]summary.VATRow{}
	for _, s := range in.Sessions {
		ss = append(ss, CashRecord{
			Section: "sessions", BusinessDate: s.BusinessDate, SessionID: s.ID, RegisterCode: s.RegisterCode,
			Status: s.Status, Float: s.Float, Expected: sumMap(s.Expected), Counted: sumMap(s.Counted), Difference: s.Difference,
			Ref: Ref{Type: "cash_session", ID: s.ID, Label: s.RegisterCode},
		})
	}
	for _, d := range in.Documents {
		r := CashRecord{
			Section: "fiscal", BusinessDate: d.BusinessDate, DocumentID: d.ID, Number: d.Number, Status: d.Status,
			Method: d.Method, Ref: Ref{Type: "fiscal_document", ID: d.ID, Label: d.Number},
		}
		switch d.Status {
		case "issued":
			r.Total = d.Total
			vatOf[d.ID] = d.VAT
		case "void":
			r.Void = -d.Total
		default:
			continue
		}
		fs = append(fs, r)
	}
	byDay := func(a, b CashRecord) int {
		if c := a.BusinessDate.Compare(b.BusinessDate); c != 0 {
			return c
		}
		return compareKeys([]string{a.RegisterCode, a.Number, a.Ref.ID}, []string{b.RegisterCode, b.Number, b.Ref.ID})
	}
	slices.SortStableFunc(ss, byDay)
	slices.SortStableFunc(fs, byDay)
	all := append(slices.Clone(ss), fs...)
	fold := func(rs []CashRecord) CashTotals { return FoldCash(rs, vatOf) }
	res := CashResult{Totals: fold(all)}
	if in.Section == "fiscal" {
		res.Records = fs
	} else {
		res.Records = ss
	}
	key := map[string]func(CashRecord) []string{
		"register": func(r CashRecord) []string { return keyIf(r.Section == "sessions", r.RegisterCode) },
		"day":      func(r CashRecord) []string { return []string{r.BusinessDate.String()} },
		"month":    func(r CashRecord) []string { return []string{MonthKey(r.BusinessDate)} },
		"method":   func(r CashRecord) []string { return keyIf(r.Section == "fiscal", orNone(r.Method)) },
		"vat_rate": func(r CashRecord) []string {
			var ks []string
			for _, v := range vatOf[r.DocumentID] {
				ks = append(ks, vatKey(v.RateBP, v.Nature))
			}
			return ks
		},
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(all, key, fold)
	}
	return res
}

// FoldCash is the only producer of cash totals.
func FoldCash(recs []CashRecord, vatOf map[string][]summary.VATRow) CashTotals {
	t := CashTotals{VAT: []CashVATTotal{}, Methods: []CashMethodTotal{}}
	var rows []summary.VATRow
	methods := map[string]money.Cents{}
	for _, r := range recs {
		if r.Section == "sessions" {
			t.Sessions++
			t.Difference += r.Difference
			continue
		}
		t.FiscalVoid += r.Void
		if r.Status != "issued" {
			continue
		}
		t.Documents++
		t.FiscalTotal += r.Total
		methods[r.Method] += r.Total
		rows = append(rows, vatOf[r.DocumentID]...)
	}
	for _, v := range summary.VATTotals(rows) {
		t.VAT = append(t.VAT, CashVATTotal{RateBP: v.RateBP, Nature: v.Nature, Net: v.Net, VAT: v.VAT, Gross: v.Gross})
	}
	keys := make([]string, 0, len(methods))
	for k := range methods {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		t.Methods = append(t.Methods, CashMethodTotal{Method: k, Total: methods[k]})
	}
	return t
}

// ---- R16 vessels ----

// BuildVessels builds R16 (§3.16): vessels with at least one physical occupied night (any family) on the selected
// resources. Allocations carry the vessel (hull, flag, effective size).
func BuildVessels(in OccupancyInput, groupBy string) VesselsResult {
	type acc struct {
		rec    VesselsRecord
		nights map[calendar.Date]bool
		kinds  map[string]int64
	}
	vs := map[string]*acc{}
	for _, fam := range []string{FamilyBerth, FamilyLand, FamilyLinear, FamilySurface} {
		sub := in
		sub.Family, sub.ResourceType = fam, ""
		g := buildGrid(&sub)
		for ri := range g.res {
			for aid, ns := range g.byAlloc[ri] {
				a := g.allocs[aid]
				if a == nil || !a.HasVessel || a.VesselID == "" {
					continue
				}
				v := vs[a.VesselID]
				if v == nil {
					v = &acc{
						rec: VesselsRecord{
							VesselID: a.VesselID, Name: a.VesselName, LOA: a.LOA, Beam: a.Beam, HullType: a.HullType,
							Flag: a.Flag, LengthBand: LengthBand(int64(a.LOA)), Ref: Ref{Type: "vessel", ID: a.VesselID, Label: a.VesselName},
						},
						nights: map[calendar.Date]bool{}, kinds: map[string]int64{},
					}
					vs[a.VesselID] = v
				}
				for _, n := range ns {
					d := g.day(n.i)
					if !v.nights[d] {
						v.nights[d] = true
						v.kinds[a.Kind]++
					}
				}
			}
		}
	}
	recs := make([]VesselsRecord, 0, len(vs))
	for _, v := range vs {
		v.rec.Nights = int64(len(v.nights))
		best := ""
		for k, n := range v.kinds {
			if best == "" || n > v.kinds[best] || (n == v.kinds[best] && k < best) {
				best = k
			}
		}
		v.rec.Kind = best
		recs = append(recs, v.rec)
	}
	slices.SortFunc(recs, func(a, b VesselsRecord) int {
		return compareKeys([]string{a.Name, a.VesselID}, []string{b.Name, b.VesselID})
	})
	res := VesselsResult{Records: recs, Totals: FoldVessels(recs)}
	key := map[string]func(VesselsRecord) string{
		"hull_type":   func(r VesselsRecord) string { return r.HullType },
		"length_band": func(r VesselsRecord) string { return r.LengthBand },
		"flag":        func(r VesselsRecord) string { return orNone(r.Flag) },
		"kind":        func(r VesselsRecord) string { return r.Kind },
	}[groupBy]
	if key != nil {
		res.Groups = GroupRecords(recs, func(r VesselsRecord) []string { return []string{key(r)} }, FoldVessels)
	}
	return res
}

// FoldVessels is the only producer of vessels totals.
func FoldVessels(recs []VesselsRecord) VesselsTotals {
	t := VesselsTotals{}
	for _, r := range recs {
		t.Vessels++
		t.Nights += r.Nights
		t.LOASum += r.LOA
	}
	avg := Scaled(int64(t.LOASum), t.Vessels, ScaleAvg)
	t.AvgLOA = Opt[geo.Cm]{Value: geo.Cm(avg.Value), OK: avg.OK}
	return t
}

// ---- R17 berth revenue ----

// BuildBerthRevenue builds R17 (§3.18): per selected berth, the DayQuota of the qualifying contract of every
// occupied row in the period, over the berth length (cents per metre).
func BuildBerthRevenue(in OccupancyInput, groupBy string) (BerthRevenueResult, Facts) {
	in.Family, in.ResourceType = FamilyBerth, ""
	berths := in.selected()
	idx := map[string]*BerthRevenueRecord{}
	recs := make([]BerthRevenueRecord, len(berths))
	for i, b := range berths {
		recs[i] = BerthRevenueRecord{
			BerthID: b.ID, BerthCode: b.Code, Category: b.Category, Length: b.Length,
			Ref: Ref{Type: "berth", ID: b.ID, Label: b.Code},
		}
		idx[b.ID] = &recs[i]
	}
	allocs := map[string]*Allocation{}
	for i := range in.Allocations {
		allocs[in.Allocations[i].ID] = &in.Allocations[i]
	}
	daily := Daily{}
	for _, row := range in.Rows {
		r := idx[row.ResourceID]
		a := allocs[row.AllocationID]
		if r == nil || a == nil || !Occupied(row.Kind) || !inPeriod(row.Night, in.From, in.To) {
			continue
		}
		r.OccupiedNights++
		if !a.Qualifying {
			continue
		}
		q := a.Contract.On(row.Night)
		r.Revenue += q
		daily.Add(row.Night, int64(q), 0)
	}
	for i := range recs {
		recs[i].RevenuePerM = Scaled(int64(recs[i].Revenue), int64(recs[i].Length), ScalePerM).asCents()
	}
	res := BerthRevenueResult{Records: recs, Totals: FoldBerthRevenue(recs)}
	if groupBy != "" {
		byID := resourceIndex(berths)
		res.Groups = GroupRecords(recs, func(r BerthRevenueRecord) []string { return dimKeys(byID[r.BerthID], groupBy) }, FoldBerthRevenue)
	}
	return res, Facts{IndBerthRevenuePerM: daily}
}

// FoldBerthRevenue is the only producer of berth revenue totals.
func FoldBerthRevenue(recs []BerthRevenueRecord) BerthRevenueTotals {
	t := BerthRevenueTotals{}
	for _, r := range recs {
		t.Berths++
		t.Length += r.Length
		t.Revenue += r.Revenue
	}
	t.RevenuePerM = Scaled(int64(t.Revenue), int64(t.Length), ScalePerM).asCents()
	return t
}
