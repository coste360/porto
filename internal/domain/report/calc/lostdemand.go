package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// Inquiry is an unserved inquiry (declined or lost, §3.4). Value is the list price of the whole stay (the service
// quotes it on the smallest compatible berth); not OK when unpriced.
type Inquiry struct {
	ID                 string
	Arrival, Departure calendar.Date // departure exclusive
	Outcome            string
	Purpose            string
	Term               string
	Channel            string
	Source             string
	LOA, Beam          geo.Cm
	PricedCategory     string
	Value              Opt[money.Cents]
}

// LostDemandInput is the input of BuildLostDemand.
type LostDemandInput struct {
	From, To  calendar.Date
	Full      bool
	GroupBy   string
	Channel   string
	Purpose   string
	Inquiries []Inquiry
}

// BuildLostDemand builds R4 (§3.4): one record per inquiry with nights in the period; values spread by night.
func BuildLostDemand(in LostDemandInput) (LostDemandResult, Facts) {
	count, value := Daily{}, Daily{}
	timeGroups := map[string]*LostDemandTotals{}
	var timeKeys []string
	tg := func(k string) *LostDemandTotals {
		t, ok := timeGroups[k]
		if !ok {
			t = &LostDemandTotals{Value: Opt[money.Cents]{OK: in.Full}}
			timeGroups[k] = t
			timeKeys = append(timeKeys, k)
		}
		return t
	}
	var recs []LostDemandRecord
	for i := range in.Inquiries {
		q := &in.Inquiries[i]
		if (in.Channel != "" && q.Channel != in.Channel) || (in.Purpose != "" && q.Purpose != in.Purpose) {
			continue
		}
		last := q.Departure.AddDays(-1)
		n := overlapDays(q.Arrival, last, in.From, in.To)
		if n == 0 {
			continue
		}
		rec := LostDemandRecord{
			InquiryID: q.ID, Arrival: q.Arrival, Departure: q.Departure, NightsInPeriod: n, Outcome: q.Outcome,
			Purpose: q.Purpose, Term: q.Term, Channel: q.Channel, Source: q.Source, LOA: q.LOA, Beam: q.Beam,
			LengthBand: LengthBand(int64(q.LOA)), PricedCategory: q.PricedCategory,
			Ref: Ref{Type: "inquiry", ID: q.ID, Label: q.Arrival.String()},
		}
		countDay := maxDate(q.Arrival, in.From)
		count.Add(countDay, 1, 0)
		if in.GroupBy == "month" || in.GroupBy == "week" {
			tg(timeKey(countDay, in.GroupBy)).Count++
		}
		days := q.Arrival.DaysUntil(last) + 1
		var v money.Cents
		for d := maxDate(q.Arrival, in.From); !d.After(last) && !d.After(in.To); d = d.AddDays(1) {
			quota := accrual.DayQuota(q.Value.Value, days, q.Arrival.DaysUntil(d))
			if !q.Value.OK {
				quota = 0
			}
			v += quota
			value.Add(d, int64(quota), 0)
			if in.GroupBy == "month" || in.GroupBy == "week" {
				t := tg(timeKey(d, in.GroupBy))
				t.Nights++
				t.Value.Value += quota
			}
		}
		if in.Full && q.Value.OK {
			rec.Value = Some(v)
		}
		if !q.Value.OK && (in.GroupBy == "month" || in.GroupBy == "week") {
			tg(timeKey(countDay, in.GroupBy)).Unpriced++
		}
		recs = append(recs, rec)
	}
	slices.SortStableFunc(recs, func(a, b LostDemandRecord) int {
		if c := a.Arrival.Compare(b.Arrival); c != 0 {
			return c
		}
		return compareKeys([]string{a.InquiryID}, []string{b.InquiryID})
	})
	fold := func(rs []LostDemandRecord) LostDemandTotals { return FoldLostDemand(in.Full, rs) }
	res := LostDemandResult{Totals: fold(recs), Records: recs}
	if !in.Full {
		value = Daily{}
	}
	if res.Totals.Unpriced > 0 {
		res.Notes = append(res.Notes, Note{Code: "unpriced", Count: res.Totals.Unpriced})
	}
	switch in.GroupBy {
	case "month", "week":
		for _, k := range timeKeys {
			t := *timeGroups[k]
			if !in.Full {
				t.Value, t.Unpriced = None[money.Cents](), 0
			}
			res.Groups = append(res.Groups, Group[LostDemandTotals]{Key: k, Label: k, Totals: t})
		}
	case "":
	default:
		key := map[string]func(LostDemandRecord) string{
			"reason": func(r LostDemandRecord) string { return r.Outcome }, "channel": func(r LostDemandRecord) string { return r.Channel },
			"source": func(r LostDemandRecord) string { return r.Source }, "purpose": func(r LostDemandRecord) string { return r.Purpose },
			"length_band": func(r LostDemandRecord) string { return r.LengthBand },
		}[in.GroupBy]
		if key != nil {
			res.Groups = GroupRecords(recs, func(r LostDemandRecord) []string { return []string{key(r)} }, fold)
		}
	}
	return res, Facts{IndLostDemandCount: count, IndLostDemandValue: value}
}

// FoldLostDemand is the only producer of lost demand totals (values only with full access).
func FoldLostDemand(full bool, recs []LostDemandRecord) LostDemandTotals {
	t := LostDemandTotals{}
	var v money.Cents
	for _, r := range recs {
		t.Count++
		t.Nights += r.NightsInPeriod
		if r.Value.OK {
			v += r.Value.Value
		} else {
			t.Unpriced++
		}
	}
	if full {
		t.Value = Some(v)
	} else {
		t.Unpriced = 0
	}
	return t
}
