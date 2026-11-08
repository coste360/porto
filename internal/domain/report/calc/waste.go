package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/money"
)

// WasteAllocation is an active allocation of an occupied kind on a berth overlapping the period (§3.3).
type WasteAllocation struct {
	Allocation
	BerthID string
	First   calendar.Date // allocation nights (inclusive)
	Last    calendar.Date
	Nights  []calendar.Date // its occupancy rows inside the period
	Vessel  fit.Vessel
	// ListTotal is the list price of the whole stay [First, Last] for a vessel of the berth's maximum size (the
	// service quotes it); not OK when unpriced. Owner allocations have none.
	ListTotal Opt[money.Cents]
}

// ContractValue is a contract value and its days [StartsOn, EndsOn) (accrual by day, D12).
type ContractValue struct {
	Total    money.Cents
	StartsOn calendar.Date
	EndsOn   calendar.Date // exclusive
}

// Days is the number of days of the contract.
func (c ContractValue) Days() int {
	if c.StartsOn.IsZero() || !c.EndsOn.After(c.StartsOn) {
		return 0
	}
	return c.StartsOn.DaysUntil(c.EndsOn)
}

// On returns the contract's DayQuota on night d (0 outside the contract).
func (c ContractValue) On(d calendar.Date) money.Cents {
	return accrual.DayQuota(c.Total, c.Days(), c.StartsOn.DaysUntil(d))
}

// WasteInput is the input of BuildWaste.
type WasteInput struct {
	From, To    calendar.Date
	ThresholdBP int64
	Full        bool // reports.all: money columns
	GroupBy     string
	Berths      []Resource // the selected berths (dimension filters applied by the caller or here)
	Dims        Dims
	Allocations []WasteAllocation
}

// BuildWaste builds R3 (§3.3): allocations on berths whose waste (10000 − fill) exceeds the threshold, with the
// applied price share and the list price share of their nights in the period.
func BuildWaste(in WasteInput) WasteResult {
	berths := map[string]*Resource{}
	for i := range in.Berths {
		r := &in.Berths[i]
		if r.Type == TypeBerth && matchDims(r, in.Dims) {
			berths[r.ID] = r
		}
	}
	var recs []WasteRecord
	for i := range in.Allocations {
		a := &in.Allocations[i]
		b := berths[a.BerthID]
		if b == nil || !Occupied(a.Kind) || !a.HasVessel {
			continue
		}
		fill := fit.FillBP(a.Vessel, fit.Target{Kind: fit.TargetBerth, Length: b.Length, Width: b.Width})
		waste := fit.FullBP - fill
		if waste <= in.ThresholdBP {
			continue
		}
		var nights []calendar.Date
		for _, n := range a.Nights {
			if inPeriod(n, in.From, in.To) {
				nights = append(nights, n)
			}
		}
		rec := WasteRecord{
			AllocationID: a.ID, Kind: a.Kind, ContractID: a.ContractID, ContractNumber: a.ContractNumber, BerthID: b.ID,
			BerthCode: b.Code, Category: b.Category, VesselID: a.VesselID, VesselName: a.VesselName, LOA: a.Vessel.LOA,
			Beam: a.Vessel.Beam, BerthLength: b.Length, BerthWidth: b.Width, FillBP: fill, WasteBP: waste,
			NightsInPeriod: int64(len(nights)), Ref: refOfAllocation(&a.Allocation),
		}
		if in.Full {
			rec.Applied, rec.List, rec.Lost = wastePrices(a, nights)
		}
		recs = append(recs, rec)
	}
	slices.SortStableFunc(recs, func(a, b WasteRecord) int {
		if a.WasteBP != b.WasteBP {
			return int(b.WasteBP - a.WasteBP)
		}
		return compareKeys([]string{a.BerthCode, a.ContractNumber, a.AllocationID}, []string{b.BerthCode, b.ContractNumber, b.AllocationID})
	})
	res := WasteResult{ThresholdBP: in.ThresholdBP}
	res.Records = recs
	fold := func(rs []WasteRecord) WasteTotals { return FoldWaste(in.Full, rs) }
	res.Totals = fold(recs)
	if res.Totals.Unpriced > 0 {
		res.Notes = append(res.Notes, Note{Code: "unpriced", Count: res.Totals.Unpriced})
	}
	switch in.GroupBy {
	case "kind":
		res.Groups = GroupRecords(recs, func(r WasteRecord) []string { return []string{r.Kind} }, fold)
	case "":
	default:
		res.Groups = GroupRecords(recs, func(r WasteRecord) []string { return dimKeys(berths[r.BerthID], in.GroupBy) }, fold)
	}
	return res
}

// wastePrices returns the applied price, the list price and the lost revenue of an allocation's nights.
func wastePrices(a *WasteAllocation, nights []calendar.Date) (applied, list, lost Opt[money.Cents]) {
	if a.Kind == KindOwner {
		return Some[money.Cents](0), None[money.Cents](), None[money.Cents]()
	}
	var ap money.Cents
	for _, n := range nights {
		ap += a.Contract.On(n)
	}
	applied = Some(ap)
	if !a.ListTotal.OK {
		return applied, None[money.Cents](), None[money.Cents]()
	}
	days := a.First.DaysUntil(a.Last) + 1
	var lp money.Cents
	for _, n := range nights {
		lp += accrual.DayQuota(a.ListTotal.Value, days, a.First.DaysUntil(n))
	}
	return applied, Some(lp), Some(max(0, lp-ap))
}

// FoldWaste is the only producer of waste totals. Money sums the priced records (list and lost) and every record
// (applied); without full access the money is hidden.
func FoldWaste(full bool, recs []WasteRecord) WasteTotals {
	t := WasteTotals{}
	var ap, lp, lo money.Cents
	for _, r := range recs {
		t.Count++
		t.Nights += r.NightsInPeriod
		t.WasteBPNights += r.WasteBP * r.NightsInPeriod
		ap += r.Applied.Value
		if r.List.OK {
			lp += r.List.Value
		} else if r.Kind != KindOwner {
			t.Unpriced++
		}
		if r.Lost.OK {
			lo += r.Lost.Value
		}
	}
	if full {
		t.Applied, t.List, t.Lost = Some(ap), Some(lp), Some(lo)
	} else {
		t.Unpriced = 0
	}
	return t
}
