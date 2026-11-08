package calc

import (
	"cmp"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/money"
)

// OriginOpeningBalance is excluded from the default origins of the charge reports (D14).
const OriginOpeningBalance = "opening_balance"

// ChargeLine is a line of a non-cancelled charge with the charge fields the reports read.
type ChargeLine struct {
	ChargeID        string
	Number          string
	Seq             int
	IssuedOn        calendar.Date
	DueOn           calendar.Date
	OriginType      string
	CustomerID      string
	ContractID      string
	ContractNumber  string
	ItemID          string
	ItemCode        string
	Description     string
	RevenueClass    string
	VATRateBP       int64
	VATNature       string
	ServiceFrom     calendar.Date // the line's service period, else the charge's; zero = none
	ServiceTo       calendar.Date
	Net, VAT, Gross money.Cents
	PaymentStatus   string
	InvoicingStatus string
	FiscalMode      string
}

// Span is the competence period of a line: its service period, else the single day it was issued (§3.7).
func (l *ChargeLine) Span() (calendar.Date, calendar.Date) {
	if !l.ServiceFrom.IsZero() && !l.ServiceTo.IsZero() && !l.ServiceTo.Before(l.ServiceFrom) {
		return l.ServiceFrom, l.ServiceTo
	}
	return l.IssuedOn, l.IssuedOn
}

func (l *ChargeLine) accrualLine(amount money.Cents) accrual.Line {
	f, t := l.Span()
	return accrual.Line{ID: l.ChargeID, Amount: amount, From: f, To: t}
}

// ChargeFilter selects charge lines (R8, R10).
type ChargeFilter struct {
	CustomerID      string
	ItemID          string
	Origins         []string // empty: every origin except opening_balance
	PaymentStatus   string
	InvoicingStatus string
	FiscalMode      string
}

func (f *ChargeFilter) match(l *ChargeLine) bool {
	if len(f.Origins) == 0 {
		if l.OriginType == OriginOpeningBalance {
			return false
		}
	} else if !slices.Contains(f.Origins, l.OriginType) {
		return false
	}
	return (f.CustomerID == "" || l.CustomerID == f.CustomerID) && (f.ItemID == "" || l.ItemID == f.ItemID) &&
		(f.PaymentStatus == "" || l.PaymentStatus == f.PaymentStatus) &&
		(f.InvoicingStatus == "" || l.InvoicingStatus == f.InvoicingStatus) && (f.FiscalMode == "" || l.FiscalMode == f.FiscalMode)
}

func chargeRecord(l *ChargeLine, name string) ChargesRecord {
	return ChargesRecord{
		ChargeID: l.ChargeID, Number: l.Number, Seq: l.Seq, IssuedOn: l.IssuedOn, DueOn: l.DueOn, OriginType: l.OriginType,
		CustomerID: l.CustomerID, CustomerName: name, ItemID: l.ItemID, ItemCode: l.ItemCode, Description: l.Description,
		RevenueClass: l.RevenueClass, VATRateBP: l.VATRateBP, VATNature: l.VATNature, ServiceFrom: l.ServiceFrom,
		ServiceTo: l.ServiceTo, Net: l.Net, VAT: l.VAT, Gross: l.Gross, PaymentStatus: l.PaymentStatus,
		InvoicingStatus: l.InvoicingStatus, FiscalMode: l.FiscalMode, Ref: Ref{Type: "charge", ID: l.ChargeID, Label: l.Number},
	}
}

// dayQuotas calls fn for each day of the line's span inside [from, to] with the net and VAT quotas (VAT is
// allocated itself, gross = net + VAT: totals stay exact, §3.8).
func dayQuotas(l *ChargeLine, from, to calendar.Date, fn func(d calendar.Date, net, vat money.Cents)) bool {
	sf, st := l.Span()
	days := sf.DaysUntil(st) + 1
	if days <= 0 || days > accrual.MaxDays {
		return false
	}
	for d := maxDate(sf, from); !d.After(st) && !d.After(to); d = d.AddDays(1) {
		i := sf.DaysUntil(d)
		fn(d, accrual.DayQuota(l.Net, days, i), accrual.DayQuota(l.VAT, days, i))
	}
	return true
}

// ChargesInput is the input of BuildCharges.
type ChargesInput struct {
	From, To calendar.Date
	Basis    string // issued (default), competence
	GroupBy  string
	Filter   ChargeFilter
	Lines    []ChargeLine
	Names    map[string]string
}

// BuildCharges builds R8 (§3.8): one record per charge line, full amounts by issue date (issued basis) or the
// line's quotas inside the period (competence basis).
func BuildCharges(in ChargesInput) (ChargesResult, Facts) {
	charged := Daily{}
	months := map[string]*ChargesTotals{}
	monthCharges := map[string]map[string]bool{}
	addMonth := func(k, chargeID string, net, vat money.Cents) {
		t, ok := months[k]
		if !ok {
			t = &ChargesTotals{}
			months[k] = t
			monthCharges[k] = map[string]bool{}
		}
		if !monthCharges[k][chargeID] {
			monthCharges[k][chargeID] = true
			t.Charges++
		}
		t.Net += net
		t.VAT += vat
		t.Gross += net + vat
		t.Charged += net + vat
	}
	var recs []ChargesRecord
	invalid := int64(0)
	for i := range in.Lines {
		l := &in.Lines[i]
		if !in.Filter.match(l) {
			continue
		}
		if in.Basis != "competence" {
			if !inPeriod(l.IssuedOn, in.From, in.To) {
				continue
			}
			r := chargeRecord(l, in.Names[l.CustomerID])
			r.Month = MonthKey(l.IssuedOn)
			charged.Add(l.IssuedOn, int64(l.Gross), 0)
			if in.GroupBy == "month" {
				addMonth(r.Month, l.ChargeID, l.Net, l.VAT)
				months[r.Month].Lines++
			}
			recs = append(recs, r)
			continue
		}
		sf, st := l.Span()
		if overlapDays(sf, st, in.From, in.To) == 0 {
			continue
		}
		r := chargeRecord(l, in.Names[l.CustomerID])
		r.Net, r.VAT, r.Gross = 0, 0, 0
		seen := map[string]bool{}
		ok := dayQuotas(l, in.From, in.To, func(d calendar.Date, net, vat money.Cents) {
			r.Net += net
			r.VAT += vat
			charged.Add(d, int64(net+vat), 0)
			if in.GroupBy == "month" {
				k := MonthKey(d)
				addMonth(k, l.ChargeID, net, vat)
				if !seen[k] {
					seen[k] = true
					months[k].Lines++
				}
			}
		})
		if !ok {
			invalid++
			continue
		}
		r.Gross = r.Net + r.VAT
		recs = append(recs, r)
	}
	sortCharges(recs)
	res := ChargesResult{Basis: basisOr(in.Basis, "issued")}
	res.Records = recs
	res.Totals = FoldCharges(recs)
	if invalid > 0 {
		res.Notes = append(res.Notes, Note{Code: "invalid_service_period", Count: invalid})
	}
	switch in.GroupBy {
	case "month":
		keys := make([]string, 0, len(months))
		for k := range months {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			res.Groups = append(res.Groups, Group[ChargesTotals]{Key: k, Label: k, Totals: *months[k]})
		}
	case "":
	default:
		key := chargeGroupKey(in.GroupBy)
		if key != nil {
			res.Groups = GroupRecords(recs, func(r ChargesRecord) []string { return []string{key(r)} }, FoldCharges)
		}
	}
	return res, Facts{IndCharged: charged}
}

func basisOr(b, def string) string {
	if b == "" {
		return def
	}
	return b
}

func chargeGroupKey(by string) func(ChargesRecord) string {
	return map[string]func(ChargesRecord) string{
		"origin":           func(r ChargesRecord) string { return r.OriginType },
		"customer":         func(r ChargesRecord) string { return orNone(r.CustomerID) },
		"item":             func(r ChargesRecord) string { return orNone(r.ItemCode) },
		"revenue_class":    func(r ChargesRecord) string { return orUnclassified(r.RevenueClass) },
		"payment_status":   func(r ChargesRecord) string { return r.PaymentStatus },
		"invoicing_status": func(r ChargesRecord) string { return r.InvoicingStatus },
		"vat_rate":         func(r ChargesRecord) string { return vatKey(r.VATRateBP, r.VATNature) },
	}[by]
}

func vatKey(rate int64, nature string) string {
	if rate == 0 && nature != "" {
		return "0:" + nature
	}
	return itoa(rate)
}

func sortCharges(recs []ChargesRecord) {
	slices.SortStableFunc(recs, func(a, b ChargesRecord) int {
		if c := a.IssuedOn.Compare(b.IssuedOn); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Number, b.Number); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Seq, b.Seq); c != 0 {
			return c
		}
		return cmp.Compare(a.Month, b.Month)
	})
}

// FoldCharges is the only producer of charges totals; charged is the gross of the records.
func FoldCharges(recs []ChargesRecord) ChargesTotals {
	t := ChargesTotals{}
	seen := map[string]bool{}
	for _, r := range recs {
		if !seen[r.ChargeID] {
			seen[r.ChargeID] = true
			t.Charges++
		}
		t.Lines++
		t.Net += r.Net
		t.VAT += r.VAT
		t.Gross += r.Gross
	}
	t.Charged = t.Gross
	return t
}

// ---- R10 revenue classes ----

// Unclassified is the class of lines without a revenue class.
const Unclassified = "unclassified"

func orUnclassified(s string) string {
	if s == "" {
		return Unclassified
	}
	return s
}

// RevenueClass is a revenue class (one level of parents).
type RevenueClass struct {
	Code, Label, ParentCode string
}

// RevenueInput is the input of BuildRevenueClasses.
type RevenueInput struct {
	From, To calendar.Date
	Basis    string // competence (default), issued
	Filter   ChargeFilter
	Lines    []ChargeLine
	Classes  []RevenueClass
	Names    map[string]string
}

// BuildRevenueClasses builds R10 (§3.10): the net by revenue class and month; records are the line
// contributions per month.
func BuildRevenueClasses(in RevenueInput) RevenueClassesResult {
	basis := basisOr(in.Basis, "competence")
	var recs []ChargesRecord
	for i := range in.Lines {
		l := &in.Lines[i]
		if !in.Filter.match(l) {
			continue
		}
		if basis == "issued" {
			if inPeriod(l.IssuedOn, in.From, in.To) {
				r := chargeRecord(l, in.Names[l.CustomerID])
				r.Month = MonthKey(l.IssuedOn)
				recs = append(recs, r)
			}
			continue
		}
		sf, st := l.Span()
		if overlapDays(sf, st, in.From, in.To) == 0 {
			continue
		}
		byMonth := map[string]*ChargesRecord{}
		var order []string
		dayQuotas(l, in.From, in.To, func(d calendar.Date, net, vat money.Cents) {
			k := MonthKey(d)
			r := byMonth[k]
			if r == nil {
				c := chargeRecord(l, in.Names[l.CustomerID])
				c.Month, c.Net, c.VAT, c.Gross = k, 0, 0, 0
				r = &c
				byMonth[k] = r
				order = append(order, k)
			}
			r.Net += net
			r.VAT += vat
			r.Gross += net + vat
		})
		for _, k := range order {
			recs = append(recs, *byMonth[k])
		}
	}
	sortCharges(recs)
	return RevenueClassesResult{Basis: basis, Records: recs, Totals: FoldRevenueClasses(recs), Classes: revenueClasses(recs, in.Classes)}
}

// FoldRevenueClasses is the only producer of revenue class totals.
func FoldRevenueClasses(recs []ChargesRecord) RevenueClassesTotals {
	t := RevenueClassesTotals{}
	for _, r := range recs {
		t.Lines++
		t.Net += r.Net
		if r.RevenueClass == "" {
			t.Unclassified += r.Net
		}
	}
	return t
}

// revenueClasses sums the records by class and month; a parent class adds its children (one level).
func revenueClasses(recs []ChargesRecord, classes []RevenueClass) []RevenueClassGroup {
	info := map[string]RevenueClass{}
	for _, c := range classes {
		info[c.Code] = c
	}
	type acc struct {
		net    money.Cents
		months map[string]money.Cents
	}
	sums := map[string]*acc{}
	get := func(k string) *acc {
		a := sums[k]
		if a == nil {
			a = &acc{months: map[string]money.Cents{}}
			sums[k] = a
		}
		return a
	}
	var total money.Cents
	for _, r := range recs {
		k := orUnclassified(r.RevenueClass)
		total += r.Net
		a := get(k)
		a.net += r.Net
		a.months[r.Month] += r.Net
		if p := info[r.RevenueClass].ParentCode; p != "" && p != r.RevenueClass {
			pa := get(p)
			pa.net += r.Net
			pa.months[r.Month] += r.Net
		}
	}
	keys := make([]string, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]RevenueClassGroup, 0, len(keys))
	for _, k := range keys {
		a := sums[k]
		g := RevenueClassGroup{Key: k, Label: info[k].Label, ParentKey: info[k].ParentCode, Net: a.net, ShareBP: Ratio(int64(a.net), int64(total))}
		if g.Label == "" {
			g.Label = k
		}
		ms := make([]string, 0, len(a.months))
		for m := range a.months {
			ms = append(ms, m)
		}
		slices.Sort(ms)
		for _, m := range ms {
			g.Months = append(g.Months, RevenueClassMonth{Month: m, Net: a.months[m]})
		}
		out = append(out, g)
	}
	return out
}

// ---- R7 accruals and deferrals ----

// ContractCharge is a non-cancelled charge of a contract (its gross, for the rateo).
type ContractCharge struct {
	ContractID string
	IssuedOn   calendar.Date
	Gross      money.Cents
}

// AccrualsInput is the input of BuildAccruals: the closing date is To.
type AccrualsInput struct {
	From, To        calendar.Date
	GroupBy         string
	Section         string // competence (default), deferral, accrual
	CustomerID      string
	Lines           []ChargeLine // non-cancelled, any origin
	Contracts       []Contract
	ContractCharges []ContractCharge
	Names           map[string]string
}

// BuildAccruals builds R7 (§3.7): competence of the period, deferrals (risconti) of the lines issued by the
// closing date C = To, and accruals (ratei) of the contracts matured by C but not charged yet.
func BuildAccruals(in AccrualsInput) AccrualsResult {
	c := in.To
	origin := map[string]string{}
	var comp, def, accr []AccrualsRecord
	invalid := int64(0)
	for i := range in.Lines {
		l := &in.Lines[i]
		if in.CustomerID != "" && l.CustomerID != in.CustomerID {
			continue
		}
		origin[l.ChargeID] = l.OriginType
		sf, st := l.Span()
		// Gross quotas are net quotas plus VAT quotas (VAT = gross − net), as dayQuotas does for R8/R10 (§3.8),
		// so the same line over the same period has the same competence gross in every report.
		netLine, vatLine := l.accrualLine(l.Net), l.accrualLine(l.Gross-l.Net)
		base := AccrualsRecord{
			Date: l.IssuedOn, ChargeID: l.ChargeID, ChargeNumber: l.Number, LineSeq: l.Seq, ContractID: l.ContractID,
			ContractNumber: l.ContractNumber, CustomerID: l.CustomerID, CustomerName: in.Names[l.CustomerID],
			Description: l.Description, ServiceFrom: sf, ServiceTo: st, AmountNet: l.Net, AmountGross: l.Gross,
			Ref: Ref{Type: "charge", ID: l.ChargeID, Label: l.Number},
		}
		if netLine.Days() == 0 || netLine.Days() > accrual.MaxDays {
			invalid++
			continue
		}
		if overlapDays(sf, st, in.From, in.To) > 0 {
			r := base
			r.Section = "competence"
			r.CompetenceNet, _ = accrual.Within(netLine, in.From, in.To)
			compVAT, _ := accrual.Within(vatLine, in.From, in.To)
			r.CompetenceGross = r.CompetenceNet + compVAT
			comp = append(comp, r)
		}
		if !l.IssuedOn.After(c) && st.After(c) {
			r := base
			r.Section = "deferral"
			upNet, upGross := money.Cents(0), money.Cents(0)
			if !sf.After(c) {
				upNet, _ = accrual.Within(netLine, sf, c)
				upVAT, _ := accrual.Within(vatLine, sf, c)
				upGross = upNet + upVAT
			}
			r.DeferralNet, r.DeferralGross = l.Net-upNet, l.Gross-upGross
			if r.DeferralNet != 0 || r.DeferralGross != 0 {
				def = append(def, r)
			}
		}
	}
	charged := map[string]money.Cents{}
	for _, ch := range in.ContractCharges {
		if !ch.IssuedOn.After(c) {
			charged[ch.ContractID] += ch.Gross
		}
	}
	for i := range in.Contracts {
		k := &in.Contracts[i]
		if !Qualifying(k.Kind, k.Status) || k.StartsOn.After(c) || (in.CustomerID != "" && k.CustomerID != in.CustomerID) {
			continue
		}
		l := k.Line()
		if l.Days() == 0 || l.Days() > accrual.MaxDays {
			continue
		}
		matured, _ := accrual.Within(l, k.StartsOn, c)
		rateo := matured - charged[k.ID]
		if rateo <= 0 {
			continue
		}
		accr = append(accr, AccrualsRecord{
			Section: "accrual", Date: k.StartsOn, ContractID: k.ID, ContractNumber: k.Number, CustomerID: k.CustomerID,
			CustomerName: in.Names[k.CustomerID], ServiceFrom: k.StartsOn, ServiceTo: l.To, AmountGross: k.Total,
			Matured: matured, Charged: charged[k.ID], Accrual: rateo, Ref: refOfContract(k),
		})
	}
	for _, s := range [][]AccrualsRecord{comp, def, accr} {
		slices.SortStableFunc(s, func(a, b AccrualsRecord) int {
			if x := a.Date.Compare(b.Date); x != 0 {
				return x
			}
			if x := compareKeys([]string{a.ChargeNumber, a.ContractNumber}, []string{b.ChargeNumber, b.ContractNumber}); x != 0 {
				return x
			}
			return cmp.Compare(a.LineSeq, b.LineSeq)
		})
	}
	all := append(append(slices.Clone(comp), def...), accr...)
	res := AccrualsResult{Totals: FoldAccruals(c, all)}
	switch in.Section {
	case "deferral":
		res.Records = def
	case "accrual":
		res.Records = accr
	default:
		res.Records = comp
	}
	if invalid > 0 {
		res.Notes = append(res.Notes, Note{Code: "invalid_service_period", Count: invalid})
	}
	key := map[string]func(AccrualsRecord) string{
		"section":  func(r AccrualsRecord) string { return r.Section },
		"month":    func(r AccrualsRecord) string { return MonthKey(r.Date) },
		"customer": func(r AccrualsRecord) string { return orNone(r.CustomerID) },
		"origin": func(r AccrualsRecord) string {
			if r.Section == "accrual" {
				return "contract"
			}
			return origin[r.ChargeID]
		},
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(all, func(r AccrualsRecord) []string { return []string{key(r)} },
			func(rs []AccrualsRecord) AccrualsTotals { return FoldAccruals(c, rs) })
	}
	return res
}

// FoldAccruals is the only producer of accruals totals.
func FoldAccruals(closing calendar.Date, recs []AccrualsRecord) AccrualsTotals {
	t := AccrualsTotals{ClosingDate: closing}
	for _, r := range recs {
		switch r.Section {
		case "competence":
			t.CompetenceNet += r.CompetenceNet
			t.CompetenceGross += r.CompetenceGross
		case "deferral":
			t.DeferralNet += r.DeferralNet
			t.DeferralGross += r.DeferralGross
		case "accrual":
			t.Accrual += r.Accrual
		}
	}
	return t
}
