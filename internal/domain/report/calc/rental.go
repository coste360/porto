package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// R-rental (M11, m11-plan §11): views utilisation, revenue, damages and deposits. Contract-phase types; FLEET owns
// the builder (BuildRental) and the tile wiring. Deposits are never revenue: deposit_retention charges are excluded
// from the revenue view and the rental_revenue tile and shown in damages (INV-98).

// Rental views.
const (
	RentalViewUtilisation = "utilisation"
	RentalViewRevenue     = "revenue"
	RentalViewDamages     = "damages"
	RentalViewDeposits    = "deposits"
)

// RentalViews lists the views of R-rental (the first is the default).
var RentalViews = []string{RentalViewUtilisation, RentalViewRevenue, RentalViewDamages, RentalViewDeposits}

// RentalTotals (OpenAPI RentalReportTotals).
type RentalTotals struct {
	Records        int64
	AvailableSlots Opt[int64]
	RentedSlots    Opt[int64]
	UtilisationBP  BP
	Net            Opt[money.Cents]
	VAT            Opt[money.Cents]
	Gross          Opt[money.Cents]
	Estimate       Opt[money.Cents]
	Retained       Opt[money.Cents]
	Deposit        Opt[money.Cents]
	Released       Opt[money.Cents]
	Outstanding    Opt[money.Cents]
}

// RentalRecord (OpenAPI RentalReportRecord): the fields of its view, the others unset.
type RentalRecord struct {
	Date           calendar.Date
	UnitID         string
	UnitCode       string
	Category       string
	FormulaID      string
	FormulaCode    string
	BookingID      string
	BookingNumber  string
	LineKind       string
	ChargeID       string
	AvailableSlots Opt[int64]
	RentedSlots    Opt[int64]
	UtilisationBP  BP
	Net            Opt[money.Cents]
	VAT            Opt[money.Cents]
	Gross          Opt[money.Cents]
	DamageID       string
	DamageStatus   string
	Estimate       Opt[money.Cents]
	Retained       Opt[money.Cents]
	DepositMode    string
	DepositStatus  string
	Deposit        Opt[money.Cents]
	Released       Opt[money.Cents]
	Outstanding    Opt[money.Cents]
	Ref            Ref
}

// RentalResult is the rental report.
type RentalResult = Result[RentalTotals, RentalRecord]

// Rental indicator keys (OpenAPI TileKey).
const (
	IndRentalUtilisation = string(TileRentalUtilisation)
	IndRentalRevenue     = string(TileRentalRevenue)
)

func init() {
	Indicators[IndRentalUtilisation] = Indicator{Unit: UnitBP, Fraction: true, Scale: ScaleBP}
	Indicators[IndRentalRevenue] = Indicator{Unit: UnitCents}
}

// Rental line kinds of the revenue view (charge lines of rental-origin charges).
const (
	RentalLineUnit       = "unit"
	RentalLineSkipper    = "skipper"
	RentalLineExtra      = "extra"
	RentalLineLate       = "late"
	RentalLineFuel       = "fuel"
	RentalLineDamage     = "damage"
	RentalLineAdjustment = "adjustment"
)

// RentalUnitInfo is a unit of the fleet (utilisation rows).
type RentalUnitInfo struct {
	ID        string
	Code      string
	Category  string
	VesselID  string
	Retired   bool
	CreatedOn calendar.Date
}

// RentalSlotDay counts the slots of a unit on a date inside the open window: Rented (booked rows of live
// bookings) and Blocked (block rows).
type RentalSlotDay struct {
	UnitID  string
	Date    calendar.Date
	Rented  int64
	Blocked int64
}

// RentalChargeLine is a line of a rental-origin charge issued on IssuedOn (deposit retentions are never rental
// origin: INV-98).
type RentalChargeLine struct {
	ChargeID      string
	IssuedOn      calendar.Date
	BookingID     string
	BookingNumber string
	UnitID        string
	UnitCode      string
	Category      string
	VesselID      string
	FormulaID     string
	FormulaCode   string
	LineKind      string
	Net           money.Cents
	VAT           money.Cents
	Gross         money.Cents
}

// RentalDamageRow is a damage recorded on Date.
type RentalDamageRow struct {
	ID            string
	Date          calendar.Date
	BookingID     string
	BookingNumber string
	UnitID        string
	UnitCode      string
	Category      string
	VesselID      string
	Status        string
	Estimate      money.Cents
	Retained      money.Cents
}

// RentalDepositRow is the deposit of a booking starting on Date.
type RentalDepositRow struct {
	BookingID     string
	BookingNumber string
	Date          calendar.Date
	UnitID        string
	UnitCode      string
	Category      string
	VesselID      string
	FormulaID     string
	FormulaCode   string
	Mode          string
	Status        string
	Deposit       money.Cents
	Retained      money.Cents
}

// RentalInput is the input of BuildRental.
type RentalInput struct {
	From, To   calendar.Date
	View       string
	GroupBy    string
	UnitID     string
	Category   string
	FormulaID  string
	WindowDay  int64 // open_to_slot − open_from_slot
	Units      []RentalUnitInfo
	SlotDays   []RentalSlotDay
	ChargeLine []RentalChargeLine
	Damages    []RentalDamageRow
	Deposits   []RentalDepositRow
}

// rentalGroups are the group_by values of each view.
var rentalGroups = map[string][]string{
	RentalViewUtilisation: {"unit", "category", "day", "week", "month"},
	RentalViewRevenue:     {"unit", "category", "formula", "line_kind", "day", "week", "month"},
	RentalViewDamages:     {"unit", "category", "status", "month"},
	RentalViewDeposits:    {"unit", "category", "formula", "mode", "status", "month"},
}

// RentalGroupAllowed reports whether group_by is valid for the view.
func RentalGroupAllowed(view, groupBy string) bool {
	return groupBy == "" || slices.Contains(rentalGroups[view], groupBy)
}

// Deposit statuses of the deposits view.
func depositOutstanding(status string) bool {
	switch status {
	case "pending", "held", "authorized", "capture_pending":
		return true
	}
	return false
}

func depositSettled(status string) bool {
	switch status {
	case "released", "retained", "partially_retained":
		return true
	}
	return false
}

// FoldRental is the only producer of rental totals (INV-62: tiles read them).
func FoldRental(view string, recs []RentalRecord) RentalTotals {
	t := RentalTotals{Records: int64(len(recs))}
	switch view {
	case RentalViewRevenue:
		var net, vat, gross money.Cents
		for i := range recs {
			net += recs[i].Net.Value
			vat += recs[i].VAT.Value
			gross += recs[i].Gross.Value
		}
		t.Net, t.VAT, t.Gross = Some(net), Some(vat), Some(gross)
	case RentalViewDamages:
		var est, ret money.Cents
		for i := range recs {
			est += recs[i].Estimate.Value
			ret += recs[i].Retained.Value
		}
		t.Estimate, t.Retained = Some(est), Some(ret)
	case RentalViewDeposits:
		var dep, ret, rel, out money.Cents
		for i := range recs {
			dep += recs[i].Deposit.Value
			ret += recs[i].Retained.Value
			rel += recs[i].Released.Value
			out += recs[i].Outstanding.Value
		}
		t.Deposit, t.Retained, t.Released, t.Outstanding = Some(dep), Some(ret), Some(rel), Some(out)
	default:
		var av, rent int64
		for i := range recs {
			av += recs[i].AvailableSlots.Value
			rent += recs[i].RentedSlots.Value
		}
		t.AvailableSlots, t.RentedSlots, t.UtilisationBP = Some(av), Some(rent), Ratio(rent, av)
	}
	return t
}

func (in RentalInput) keep(unitID, category, formulaID string) bool {
	return (in.UnitID == "" || unitID == in.UnitID) && (in.Category == "" || category == in.Category) &&
		(in.FormulaID == "" || formulaID == in.FormulaID)
}

// BuildRental builds the rental report for the view of in. Facts: rental_utilisation (rented / available slots by
// day) on the utilisation view, rental_revenue (net by issue date) on the revenue view.
func BuildRental(in RentalInput) (RentalResult, Facts) {
	view := in.View
	if view == "" {
		view = RentalViewUtilisation
	}
	var recs []RentalRecord
	facts := Facts{}
	switch view {
	case RentalViewUtilisation:
		type key struct {
			unit string
			day  calendar.Date
		}
		counts := map[key]RentalSlotDay{}
		for _, s := range in.SlotDays {
			k := key{s.UnitID, s.Date}
			c := counts[k]
			c.Rented += s.Rented
			c.Blocked += s.Blocked
			counts[k] = c
		}
		d := Daily{}
		for _, u := range in.Units {
			if !in.keep(u.ID, u.Category, "") || in.FormulaID != "" {
				continue
			}
			for _, day := range calendar.Range(in.From, in.To) {
				c := counts[key{u.ID, day}]
				avail := max(in.WindowDay-c.Blocked, 0)
				if u.Retired || (!u.CreatedOn.IsZero() && day.Before(u.CreatedOn)) {
					avail = 0
				}
				rented := min(c.Rented, max(in.WindowDay, 0))
				if rented == 0 && avail == 0 && (u.Retired || day.Before(u.CreatedOn)) {
					continue
				}
				recs = append(recs, RentalRecord{
					Date: day, UnitID: u.ID, UnitCode: u.Code, Category: u.Category, AvailableSlots: Some(avail),
					RentedSlots: Some(rented), UtilisationBP: Ratio(rented, avail),
					Ref: Ref{Type: "rental_unit", ID: u.ID, Label: u.Code},
				})
				d.Add(day, rented, avail)
			}
		}
		slices.SortStableFunc(recs, func(a, b RentalRecord) int {
			if c := compareKeys([]string{a.UnitCode}, []string{b.UnitCode}); c != 0 {
				return c
			}
			return cmpDate(a.Date, b.Date)
		})
		facts[IndRentalUtilisation] = d
	case RentalViewRevenue:
		d := Daily{}
		for _, l := range in.ChargeLine {
			if !inPeriod(l.IssuedOn, in.From, in.To) || !in.keep(l.UnitID, l.Category, l.FormulaID) {
				continue
			}
			recs = append(recs, RentalRecord{
				Date: l.IssuedOn, UnitID: l.UnitID, UnitCode: l.UnitCode, Category: l.Category, FormulaID: l.FormulaID,
				FormulaCode: l.FormulaCode, BookingID: l.BookingID, BookingNumber: l.BookingNumber, LineKind: l.LineKind,
				ChargeID: l.ChargeID, Net: Some(l.Net), VAT: Some(l.VAT), Gross: Some(l.Gross),
				Ref: Ref{Type: "charge", ID: l.ChargeID, Label: l.BookingNumber},
			})
			d.Add(l.IssuedOn, int64(l.Net), 0)
		}
		slices.SortStableFunc(recs, func(a, b RentalRecord) int {
			if c := cmpDate(a.Date, b.Date); c != 0 {
				return c
			}
			return compareKeys([]string{a.BookingNumber, a.ChargeID, a.LineKind}, []string{b.BookingNumber, b.ChargeID, b.LineKind})
		})
		facts[IndRentalRevenue] = d
	case RentalViewDamages:
		for _, x := range in.Damages {
			if !inPeriod(x.Date, in.From, in.To) || !in.keep(x.UnitID, x.Category, "") || in.FormulaID != "" {
				continue
			}
			recs = append(recs, RentalRecord{
				Date: x.Date, UnitID: x.UnitID, UnitCode: x.UnitCode, Category: x.Category, BookingID: x.BookingID,
				BookingNumber: x.BookingNumber, DamageID: x.ID, DamageStatus: x.Status, Estimate: Some(x.Estimate),
				Retained: Some(x.Retained), Ref: Ref{Type: "rental_booking", ID: x.BookingID, Label: x.BookingNumber},
			})
		}
		slices.SortStableFunc(recs, func(a, b RentalRecord) int {
			if c := cmpDate(a.Date, b.Date); c != 0 {
				return c
			}
			return compareKeys([]string{a.BookingNumber, a.DamageID}, []string{b.BookingNumber, b.DamageID})
		})
	case RentalViewDeposits:
		for _, x := range in.Deposits {
			if !inPeriod(x.Date, in.From, in.To) || !in.keep(x.UnitID, x.Category, x.FormulaID) || x.Mode == "" || x.Status == "none" {
				continue
			}
			r := RentalRecord{
				Date: x.Date, UnitID: x.UnitID, UnitCode: x.UnitCode, Category: x.Category, FormulaID: x.FormulaID,
				FormulaCode: x.FormulaCode, BookingID: x.BookingID, BookingNumber: x.BookingNumber, DepositMode: x.Mode,
				DepositStatus: x.Status, Deposit: Some[money.Cents](0), Retained: Some[money.Cents](0),
				Released: Some[money.Cents](0), Outstanding: Some[money.Cents](0),
				Ref: Ref{Type: "rental_booking", ID: x.BookingID, Label: x.BookingNumber},
			}
			if x.Mode != "waived" && x.Status != "waived" {
				r.Deposit = Some(x.Deposit)
				switch {
				case depositSettled(x.Status):
					r.Retained = Some(min(x.Retained, x.Deposit))
					r.Released = Some(x.Deposit - r.Retained.Value)
				case depositOutstanding(x.Status):
					r.Outstanding = Some(x.Deposit)
				}
			}
			recs = append(recs, r)
		}
		slices.SortStableFunc(recs, func(a, b RentalRecord) int {
			if c := cmpDate(a.Date, b.Date); c != 0 {
				return c
			}
			return compareKeys([]string{a.BookingNumber}, []string{b.BookingNumber})
		})
	}
	if recs == nil {
		recs = []RentalRecord{}
	}
	fold := func(rs []RentalRecord) RentalTotals { return FoldRental(view, rs) }
	res := RentalResult{Records: recs, Totals: fold(recs)}
	key := map[string]func(RentalRecord) []string{
		"unit":      func(r RentalRecord) []string { return []string{orNone(r.UnitCode)} },
		"category":  func(r RentalRecord) []string { return []string{orNone(r.Category)} },
		"formula":   func(r RentalRecord) []string { return []string{orNone(r.FormulaCode)} },
		"line_kind": func(r RentalRecord) []string { return []string{orNone(r.LineKind)} },
		"status":    func(r RentalRecord) []string { return []string{orNone(r.DamageStatus + r.DepositStatus)} },
		"mode":      func(r RentalRecord) []string { return []string{orNone(r.DepositMode)} },
		"day": func(r RentalRecord) []string {
			return dateKey(r.Date, func(d calendar.Date) string { return d.String() })
		},
		"week":  func(r RentalRecord) []string { return dateKey(r.Date, WeekKey) },
		"month": func(r RentalRecord) []string { return dateKey(r.Date, MonthKey) },
	}[in.GroupBy]
	if key != nil && RentalGroupAllowed(view, in.GroupBy) {
		res.Groups = GroupRecords(recs, key, fold)
	}
	return res, facts
}
