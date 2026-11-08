package calc

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/money"
)

// Contract kinds.
const (
	ContractMooring = "mooring"
	ContractTransit = "transit"
	ContractCharter = "charter"
	ContractStorage = "storage"
)

// Contract is a contract or transit with a qualifying status (§3.6, D12).
type Contract struct {
	ID         string
	Number     string
	Kind       string
	Term       string
	Status     string
	Channel    string
	CustomerID string
	VesselID   string
	Category   string // price_class of its first resource line
	StartsOn   calendar.Date
	EndsOn     calendar.Date // exclusive
	Total      money.Cents
}

// Line is the contract value as an accrual line over its nights.
func (c *Contract) Line() accrual.Line {
	return accrual.Line{ID: c.ID, Amount: c.Total, From: c.StartsOn, To: c.EndsOn.AddDays(-1)}
}

// qualifying statuses (§3.6, D12).
var qualifyingStatuses = map[string][]string{
	ContractMooring: {"active", "expired", "renewed", "terminated", "suspended"},
	ContractCharter: {"active", "expired", "renewed", "terminated", "suspended"},
	ContractTransit: {"confirmed", "arrived", "departed"},
	// Storage contracts count from M9 (m9-plan §11), with the mooring state table (D1).
	ContractStorage: {"active", "expired", "renewed", "terminated", "suspended"},
}

// Qualifying reports whether a contract of kind with status counts as production.
func Qualifying(kind, status string) bool { return slices.Contains(qualifyingStatuses[kind], status) }

// QualifyingStatuses returns the qualifying statuses of a kind (for the SQL filter).
func QualifyingStatuses(kind string) []string { return slices.Clone(qualifyingStatuses[kind]) }

func refOfContract(c *Contract) Ref {
	if c.Kind == ContractTransit {
		return Ref{Type: "transit", ID: c.ID, Label: c.Number}
	}
	return Ref{Type: "contract", ID: c.ID, Label: c.Number}
}

// ---- R6 production ----

// ProductionInput is the input of BuildProduction.
type ProductionInput struct {
	From, To   calendar.Date
	GroupBy    string
	Kind       string
	CustomerID string
	Contracts  []Contract
	Names      map[string]string // customer id → display name (only with customers.read)
}

// BuildProduction builds R6 (§3.6): one record per qualifying contract overlapping the period with its
// competence (accrual.Within of the contract value).
func BuildProduction(in ProductionInput) (ProductionResult, Facts) {
	prod, transit := Daily{}, Daily{}
	months := map[string]*ProductionTotals{}
	var monthKeys []string
	var recs []ProductionRecord
	for i := range in.Contracts {
		c := &in.Contracts[i]
		if !Qualifying(c.Kind, c.Status) || (in.Kind != "" && c.Kind != in.Kind) ||
			(in.CustomerID != "" && c.CustomerID != in.CustomerID) {
			continue
		}
		l := c.Line()
		n := overlapDays(l.From, l.To, in.From, in.To)
		if n == 0 || l.Days() == 0 || l.Days() > accrual.MaxDays {
			continue
		}
		rec := ProductionRecord{
			ContractID: c.ID, Number: c.Number, Kind: c.Kind, Term: c.Term, Status: c.Status, Channel: c.Channel,
			CustomerID: c.CustomerID, CustomerName: in.Names[c.CustomerID], Category: c.Category, StartsOn: c.StartsOn,
			EndsOn: c.EndsOn, Total: c.Total, NightsInPeriod: n, Ref: refOfContract(c),
		}
		days := l.Days()
		for d := maxDate(l.From, in.From); !d.After(l.To) && !d.After(in.To); d = d.AddDays(1) {
			q := accrual.DayQuota(c.Total, days, l.From.DaysUntil(d))
			rec.Competence += q
			prod.Add(d, int64(q), 0)
			if c.Kind == ContractTransit {
				transit.Add(d, int64(q), 1)
			}
			if in.GroupBy == "month" {
				k := MonthKey(d)
				t, ok := months[k]
				if !ok {
					t = &ProductionTotals{}
					months[k] = t
					monthKeys = append(monthKeys, k)
				}
				t.Nights++
				t.Competence += q
				if c.Kind == ContractTransit {
					t.TransitNights++
					t.TransitCompetence += q
				}
			}
		}
		recs = append(recs, rec)
	}
	slices.SortStableFunc(recs, func(a, b ProductionRecord) int {
		if c := a.StartsOn.Compare(b.StartsOn); c != 0 {
			return c
		}
		return compareKeys([]string{a.Number, a.ContractID}, []string{b.Number, b.ContractID})
	})
	res := ProductionResult{Records: recs, Totals: FoldProduction(recs)}
	switch in.GroupBy {
	case "month":
		slices.Sort(monthKeys)
		for _, k := range monthKeys {
			t := *months[k]
			t.TransitAvgNight = Scaled(int64(t.TransitCompetence), t.TransitNights, ScaleAvg).asCents()
			res.Groups = append(res.Groups, Group[ProductionTotals]{Key: k, Label: k, Totals: t})
		}
	case "":
	default:
		key := map[string]func(ProductionRecord) string{
			"kind": func(r ProductionRecord) string { return r.Kind }, "term": func(r ProductionRecord) string { return orNone(r.Term) },
			"category": func(r ProductionRecord) string { return orNone(r.Category) },
			"channel":  func(r ProductionRecord) string { return r.Channel },
			"customer": func(r ProductionRecord) string { return orNone(r.CustomerID) },
		}[in.GroupBy]
		if key != nil {
			res.Groups = GroupRecords(recs, func(r ProductionRecord) []string { return []string{key(r)} }, FoldProduction)
			if in.GroupBy == "customer" {
				for i := range res.Groups {
					if n := in.Names[res.Groups[i].Key]; n != "" {
						res.Groups[i].Label = n
					}
				}
			}
		}
	}
	return res, Facts{IndProduction: prod, IndTransitAvgNight: transit}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func (o Opt[T]) asCents() Opt[money.Cents] {
	return Opt[money.Cents]{Value: money.Cents(o.Value), OK: o.OK}
}

// FoldProduction is the only producer of production totals.
func FoldProduction(recs []ProductionRecord) ProductionTotals {
	t := ProductionTotals{}
	for _, r := range recs {
		t.Contracts++
		t.Nights += r.NightsInPeriod
		t.Competence += r.Competence
		if r.Kind == ContractTransit {
			t.Transits++
			t.TransitNights += r.NightsInPeriod
			t.TransitCompetence += r.Competence
		}
	}
	t.TransitAvgNight = Scaled(int64(t.TransitCompetence), t.TransitNights, ScaleAvg).asCents()
	return t
}

// ---- R5 customers and R5 renewals (§3.5, §3.17) ----

// CustomerInfo is the profile of a customer used by the report (display name only with customers.read).
type CustomerInfo struct {
	ID, Name, Category, Nationality string
}

// Renewal is the renewal state of an eligible contract: a proposal whose new contract qualifies.
type Renewal struct {
	SourceContractID string
	NewContractID    string
}

// CustomersInput is the input of BuildCustomers. Contracts are every qualifying contract of the marina (the first
// contract of a customer may be years old); Arrears is the overdue residual of each customer as of today.
type CustomersInput struct {
	From, To  calendar.Date
	GroupBy   string
	Section   string // customers (default), renewals
	NewOnly   bool
	Contracts []Contract
	Customers map[string]CustomerInfo
	Arrears   map[string]money.Cents
	Renewals  []Renewal
}

// Seniority bands.
func seniorityBand(years int64) string {
	switch {
	case years <= 0:
		return "0"
	case years <= 2:
		return "1_2"
	case years <= 4:
		return "3_4"
	case years <= 9:
		return "5_9"
	default:
		return "ge10"
	}
}

// valueBand classifies the production of a customer in the period (euro thresholds).
func valueBand(v money.Cents) string {
	switch {
	case v < 100_000:
		return "lt1k"
	case v < 500_000:
		return "1k_5k"
	case v < 1_000_000:
		return "5k_10k"
	case v < 2_500_000:
		return "10k_25k"
	default:
		return "ge25k"
	}
}

func wholeYears(from, to calendar.Date) int64 {
	if to.Before(from) {
		return 0
	}
	y := to.Year() - from.Year()
	if to.Month() < from.Month() || (to.Month() == from.Month() && to.Day() < from.Day()) {
		y--
	}
	return int64(y)
}

// BuildCustomers builds R5 (§3.5) with its renewals section (§3.17).
func BuildCustomers(in CustomersInput) (CustomersResult, Facts) {
	newDaily, renewDaily := Daily{}, Daily{}
	first := map[string]*Contract{}
	for i := range in.Contracts {
		c := &in.Contracts[i]
		if c.CustomerID == "" || !Qualifying(c.Kind, c.Status) {
			continue
		}
		f := first[c.CustomerID]
		if f == nil || c.StartsOn.Before(f.StartsOn) || (c.StartsOn == f.StartsOn && c.ID < f.ID) {
			first[c.CustomerID] = c
		}
	}
	byCustomer := map[string]*CustomersRecord{}
	for i := range in.Contracts {
		c := &in.Contracts[i]
		if c.CustomerID == "" || !Qualifying(c.Kind, c.Status) {
			continue
		}
		l := c.Line()
		if l.Days() == 0 || l.Days() > accrual.MaxDays || overlapDays(l.From, l.To, in.From, in.To) == 0 {
			continue
		}
		r := byCustomer[c.CustomerID]
		if r == nil {
			r = &CustomersRecord{Section: "customers", CustomerID: c.CustomerID}
			byCustomer[c.CustomerID] = r
		}
		v, _ := accrual.Within(l, in.From, in.To)
		r.Value += v
		r.Contracts++
	}
	for id, f := range first {
		if inPeriod(f.StartsOn, in.From, in.To) && byCustomer[id] == nil {
			byCustomer[id] = &CustomersRecord{Section: "customers", CustomerID: id}
		}
	}
	var recs []CustomersRecord
	for id, r := range byCustomer {
		f := first[id]
		info := in.Customers[id]
		r.CustomerName, r.Category, r.Nationality = info.Name, info.Category, info.Nationality
		r.FirstContractOn, r.AcquisitionChannel = f.StartsOn, f.Channel
		r.SeniorityYears = wholeYears(f.StartsOn, in.To)
		r.SeniorityBand = seniorityBand(r.SeniorityYears)
		if inPeriod(f.StartsOn, in.From, in.To) {
			r.New = 1
		}
		r.Arrears = in.Arrears[id]
		r.Ref = Ref{Type: "customer", ID: id, Label: info.Name}
		if in.NewOnly && r.New == 0 {
			continue
		}
		if r.New == 1 {
			newDaily.Add(f.StartsOn, 1, 0)
		}
		recs = append(recs, *r)
	}
	slices.SortStableFunc(recs, func(a, b CustomersRecord) int {
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		return compareKeys([]string{a.CustomerName, a.CustomerID}, []string{b.CustomerName, b.CustomerID})
	})
	renewed := map[string]string{}
	byID := map[string]*Contract{}
	for i := range in.Contracts {
		byID[in.Contracts[i].ID] = &in.Contracts[i]
	}
	for _, rn := range in.Renewals {
		if nc := byID[rn.NewContractID]; nc != nil && Qualifying(nc.Kind, nc.Status) {
			renewed[rn.SourceContractID] = rn.NewContractID
		}
	}
	var ren []CustomersRecord
	for i := range in.Contracts {
		c := &in.Contracts[i]
		if c.Kind != ContractMooring || (c.Term != "annual" && c.Term != "seasonal") || !Qualifying(c.Kind, c.Status) {
			continue
		}
		last := c.EndsOn.AddDays(-1)
		if !inPeriod(last, in.From, in.To) {
			continue
		}
		info := in.Customers[c.CustomerID]
		r := CustomersRecord{
			Section: "renewals", CustomerID: c.CustomerID, CustomerName: info.Name, Category: info.Category,
			Nationality: info.Nationality, ContractID: c.ID, ContractNumber: c.Number, Term: c.Term, LastNight: last,
			Ref: refOfContract(c),
		}
		if nc, ok := renewed[c.ID]; ok || c.Status == "renewed" {
			r.Renewed, r.NewContractID = 1, nc
		}
		renewDaily.Add(last, r.Renewed, 1)
		ren = append(ren, r)
	}
	slices.SortStableFunc(ren, func(a, b CustomersRecord) int {
		if c := a.LastNight.Compare(b.LastNight); c != 0 {
			return c
		}
		return compareKeys([]string{a.ContractNumber, a.ContractID}, []string{b.ContractNumber, b.ContractID})
	})
	all := append(slices.Clone(recs), ren...)
	res := CustomersResult{Totals: FoldCustomers(all)}
	if in.Section == "renewals" {
		res.Records = ren
	} else {
		res.Records = recs
	}
	key := map[string]func(CustomersRecord) string{
		"category": func(r CustomersRecord) string { return orNone(r.Category) }, "nationality": func(r CustomersRecord) string { return orNone(r.Nationality) },
		"seniority": func(r CustomersRecord) string { return r.SeniorityBand }, "channel": func(r CustomersRecord) string { return orNone(r.AcquisitionChannel) },
		"value_band": func(r CustomersRecord) string { return valueBand(r.Value) },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(recs, func(r CustomersRecord) []string { return []string{key(r)} }, FoldCustomers)
	}
	return res, Facts{IndNewCustomers: newDaily, IndRenewalRate: renewDaily}
}

// FoldCustomers is the only producer of customers totals (both sections).
func FoldCustomers(recs []CustomersRecord) CustomersTotals {
	t := CustomersTotals{}
	for _, r := range recs {
		if r.Section == "renewals" {
			t.RenewalEligible++
			t.RenewalRenewed += r.Renewed
			continue
		}
		t.Customers++
		t.NewCustomers += r.New
		t.Contracts += r.Contracts
		t.Value += r.Value
		t.Arrears += r.Arrears
	}
	t.RenewalRateBP = Ratio(t.RenewalRenewed, t.RenewalEligible)
	return t
}
