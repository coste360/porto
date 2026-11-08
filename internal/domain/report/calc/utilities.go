package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// ---- R-utilities (m9-plan §11, §9) ----

// Meter kinds and consumption states (utility_meters.kind, utility_consumptions.status).
const (
	MeterPower  = "power"
	MeterWater  = "water"
	MeterHours  = "hours"
	MeterCycles = "cycles"

	ConsumptionPending    = "pending"
	ConsumptionAnomaly    = "anomaly"
	ConsumptionUnassigned = "unassigned"
	ConsumptionCharged    = "charged"
	ConsumptionExcluded   = "excluded"
)

// Notes of the utilities report.
const (
	NoteNoPrice = "no_price" // pending consumptions without a current price (left out of pending_value)
)

// UtilityConsumption is a utility_consumptions row with its meter and contract; Charged is the gross of the
// charge line of the consumption (OK when charged and found), PendingValue the value at the current price of a
// pending consumption (OK when priced).
type UtilityConsumption struct {
	ID           string
	MeterID      string
	MeterCode    string
	Kind         string
	ResourceType string
	ResourceID   string
	ContractID   string
	ContractCode string
	ContractKind string
	First, Last  calendar.Date
	QtyMilli     int64
	Status       string
	Charged      Opt[money.Cents]
	PendingValue Opt[money.Cents]
}

// UtilitiesInput is the input of BuildUtilities.
type UtilitiesInput struct {
	From, To     calendar.Date
	GroupBy      string // kind, meter, status, contract, month
	Kind         string
	Status       string
	MeterID      string
	ContractID   string
	Full         bool // reports.all: amounts
	Consumptions []UtilityConsumption
}

// UtilitiesTotals (OpenAPI UtilitiesReportTotals): quantities by kind exclude excluded consumptions.
type UtilitiesTotals struct {
	PowerMilli      int64
	WaterMilli      int64
	HoursMilli      int64
	CyclesMilli     int64
	PendingMilli    int64
	UnassignedMilli int64
	ChargedMilli    int64
	Charged         Opt[money.Cents]
	PendingValue    Opt[money.Cents]
}

// UtilitiesRecord (OpenAPI UtilitiesReportRecord).
type UtilitiesRecord struct {
	ConsumptionID string
	MeterID       string
	MeterCode     string
	Kind          string
	ResourceType  string
	ResourceID    string
	ContractID    string
	ContractCode  string
	First, Last   calendar.Date
	Nights        int64
	QtyMilli      int64
	Status        string
	Charged       Opt[money.Cents]
	PendingValue  Opt[money.Cents] // value at the current price of a pending record (reports.all, OK when priced)
	Ref           Ref
}

// UtilitiesResult is the utilities report.
type UtilitiesResult = Result[UtilitiesTotals, UtilitiesRecord]

// FoldUtilities is the only producer of utilities totals.
func FoldUtilities(recs []UtilitiesRecord, full bool) UtilitiesTotals {
	t := UtilitiesTotals{}
	var charged, pending money.Cents
	for i := range recs {
		r := &recs[i]
		switch r.Status {
		case ConsumptionPending:
			t.PendingMilli += r.QtyMilli
			pending += r.PendingValue.Value
		case ConsumptionUnassigned:
			t.UnassignedMilli += r.QtyMilli
		case ConsumptionCharged:
			t.ChargedMilli += r.QtyMilli
			charged += r.Charged.Value
		}
		if r.Status == ConsumptionExcluded {
			continue
		}
		switch r.Kind {
		case MeterPower:
			t.PowerMilli += r.QtyMilli
		case MeterWater:
			t.WaterMilli += r.QtyMilli
		case MeterHours:
			t.HoursMilli += r.QtyMilli
		case MeterCycles:
			t.CyclesMilli += r.QtyMilli
		}
	}
	if full {
		t.Charged, t.PendingValue = Some(charged), Some(pending)
	}
	return t
}

// BuildUtilities builds the utilities report: one record per consumption overlapping the period (whole interval,
// not prorated) matching the filters, by meter code and first night. Facts: utility_unbilled (pending value) by
// last night.
func BuildUtilities(in UtilitiesInput) (UtilitiesResult, Facts) {
	recs := make([]UtilitiesRecord, 0, len(in.Consumptions))
	var noPrice int64
	for i := range in.Consumptions {
		c := &in.Consumptions[i]
		if c.Last.Before(in.From) || c.First.After(in.To) || (in.Kind != "" && c.Kind != in.Kind) ||
			(in.Status != "" && c.Status != in.Status) || (in.MeterID != "" && c.MeterID != in.MeterID) ||
			(in.ContractID != "" && c.ContractID != in.ContractID) {
			continue
		}
		r := UtilitiesRecord{
			ConsumptionID: c.ID, MeterID: c.MeterID, MeterCode: c.MeterCode, Kind: c.Kind, ResourceType: c.ResourceType,
			ResourceID: c.ResourceID, ContractID: c.ContractID, ContractCode: c.ContractCode, First: c.First, Last: c.Last,
			Nights: int64(c.First.DaysUntil(c.Last) + 1), QtyMilli: c.QtyMilli, Status: c.Status,
			Ref: Ref{Type: "utility_consumption", ID: c.ID, Label: c.MeterCode},
		}
		if in.Full {
			if c.Status == ConsumptionCharged {
				r.Charged = c.Charged
			}
			if c.Status == ConsumptionPending {
				r.PendingValue = c.PendingValue
				if !c.PendingValue.OK {
					noPrice++
				}
			}
		}
		recs = append(recs, r)
	}
	slices.SortStableFunc(recs, func(a, b UtilitiesRecord) int {
		return compareKeys([]string{a.MeterCode, a.First.String(), a.ConsumptionID}, []string{b.MeterCode, b.First.String(), b.ConsumptionID})
	})
	res := UtilitiesResult{Records: recs, Totals: FoldUtilities(recs, in.Full)}
	if noPrice > 0 {
		res.Notes = append(res.Notes, Note{Code: NoteNoPrice, Count: noPrice})
	}
	fold := func(rs []UtilitiesRecord) UtilitiesTotals { return FoldUtilities(rs, in.Full) }
	key := map[string]func(UtilitiesRecord) string{
		"kind":     func(r UtilitiesRecord) string { return r.Kind },
		"meter":    func(r UtilitiesRecord) string { return r.MeterID },
		"status":   func(r UtilitiesRecord) string { return r.Status },
		"contract": func(r UtilitiesRecord) string { return orNone(r.ContractID) },
		"month":    func(r UtilitiesRecord) string { return MonthKey(r.Last) },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(recs, func(r UtilitiesRecord) []string { return []string{key(r)} }, fold)
		labels := map[string]string{}
		for _, r := range recs {
			switch in.GroupBy {
			case "meter":
				labels[r.MeterID] = r.MeterCode
			case "contract":
				if r.ContractID != "" {
					labels[r.ContractID] = r.ContractCode
				}
			}
		}
		for i := range res.Groups {
			if l := labels[res.Groups[i].Key]; l != "" {
				res.Groups[i].Label = l
			}
		}
	}
	unbilled := Daily{}
	for i := range recs {
		if recs[i].Status == ConsumptionPending && recs[i].PendingValue.OK {
			unbilled.Add(recs[i].Last, int64(recs[i].PendingValue.Value), 0)
		}
	}
	return res, Facts{IndUtilityUnbilled: unbilled}
}
