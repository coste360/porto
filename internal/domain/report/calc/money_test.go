package calc

import (
	"testing"

	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/money"
)

func TestWaste_Table(t *testing.T) {
	b := berth("b1", "A01", "m") // 1000 × 400
	nights := func(from, to string) []NightRow { return rows("b1", "x", KindContract, from, to) }
	var ns []calendarDate
	for _, r := range nights("2026-06-01", "2026-06-10") {
		ns = append(ns, r.Night)
	}
	mk := func(id, kind string, loa, beam int64) WasteAllocation {
		return WasteAllocation{
			Allocation: Allocation{
				ID: id, Kind: kind, ResourceID: "b1", HasVessel: true, ContractID: "c-" + id, ContractNumber: id,
				Contract: ContractValue{Total: 10_000, StartsOn: d("2026-06-01"), EndsOn: d("2026-06-11")},
			},
			BerthID: "b1", First: d("2026-06-01"), Last: d("2026-06-10"), Nights: ns,
			Vessel: fit.Vessel{LOA: geoCm(loa), Beam: geoCm(beam)}, ListTotal: Some[money.Cents](30_000),
		}
	}
	small := mk("small", KindContract, 500, 200)      // fill 2500, waste 7500
	fits := mk("fits", KindTransit, 1000, 400)        // fill 10000
	owner := mk("owner", KindOwner, 500, 200)         // listed, no money
	unpriced := mk("unpriced", KindTransit, 600, 200) // fill 3000
	unpriced.ListTotal = None[money.Cents]()
	in := WasteInput{
		From: d("2026-06-01"), To: d("2026-06-05"), ThresholdBP: 4000, Full: true, Berths: []Resource{b},
		Allocations: []WasteAllocation{small, fits, owner, unpriced},
	}
	res := BuildWaste(in)
	if res.Totals.Count != 3 || res.Totals.Nights != 15 || res.Totals.Unpriced != 1 {
		t.Fatalf("totals %+v", res.Totals)
	}
	// Applied: 5 nights × 1000; list: 5 × 3000; lost 10 000 for small only (owner and unpriced have no list).
	if res.Totals.Applied.Value != 10_000 || res.Totals.List.Value != 15_000 || res.Totals.Lost.Value != 10_000 {
		t.Fatalf("money %+v", res.Totals)
	}
	if res.Records[0].WasteBP != 7500 || res.Records[0].FillBP != 2500 {
		t.Fatalf("order/waste %+v", res.Records[0])
	}
	for _, r := range res.Records {
		if r.Kind == KindOwner && (r.Lost.OK || r.List.OK) {
			t.Fatal("owner allocations have no lost revenue")
		}
	}
	in.ThresholdBP = 7000
	if r := BuildWaste(in); r.Totals.Count != 2 {
		t.Fatalf("threshold %+v", r.Totals)
	}
	in.Full = false
	r := BuildWaste(in)
	if r.Totals.Applied.OK || r.Totals.Lost.OK || r.Records[0].Applied.OK {
		t.Fatal("money must be hidden without reports.all")
	}
}

func TestProduction_Table(t *testing.T) {
	cs := []Contract{
		{ID: "a", Number: "ORM-1", Kind: ContractMooring, Term: "annual", Status: "active", Channel: "desk", StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Total: 365_00},
		{ID: "t", Number: "TRN-1", Kind: ContractTransit, Status: "departed", Channel: "online", StartsOn: d("2026-03-30"), EndsOn: d("2026-04-03"), Total: 401},
		{ID: "x", Number: "TRN-2", Kind: ContractTransit, Status: "cancelled", StartsOn: d("2026-03-01"), EndsOn: d("2026-03-05"), Total: 999},
		{ID: "n", Number: "TRN-3", Kind: ContractTransit, Status: "no_show", StartsOn: d("2026-03-01"), EndsOn: d("2026-03-05"), Total: 999},
		{ID: "term", Number: "ORM-2", Kind: ContractMooring, Term: "seasonal", Status: "terminated", StartsOn: d("2026-03-01"), EndsOn: d("2026-04-30"), Total: 6000},
		{ID: "draft", Number: "ORM-3", Kind: ContractMooring, Term: "annual", Status: "draft", StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Total: 1},
	}
	res, facts := BuildProduction(ProductionInput{From: d("2026-04-01"), To: d("2026-04-30"), Contracts: cs, GroupBy: "kind"})
	// annual: 30 days × 100; transit: 2 nights (04-01, 04-02) of 4 → 100 + 101 (remainder on the last day);
	// terminated keeps its value: 29 of its 60 days are in April → 2900.
	if res.Totals.Contracts != 3 || res.Totals.Competence != 3000+201+2900 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Totals.Transits != 1 || res.Totals.TransitNights != 2 || res.Totals.TransitAvgNight.Value != 100 {
		t.Fatalf("transit %+v", res.Totals)
	}
	if facts[IndProduction].Sum().Num != int64(res.Totals.Competence) {
		t.Fatal("daily production")
	}
	if s := facts[IndTransitAvgNight].Sum(); s.Num != 201 || s.Den != 2 {
		t.Fatalf("daily transit %+v", s)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups %+v", res.Groups)
	}
	res, _ = BuildProduction(ProductionInput{From: d("2026-04-01"), To: d("2026-04-30"), Contracts: cs, Kind: ContractTransit})
	if res.Totals.Contracts != 1 || TileValueOf(IndTransitAvgNight, res.Totals).Value.Value != 100 {
		t.Fatalf("kind filter %+v %+v", res.Totals, TileValueOf(IndTransitAvgNight, res.Totals))
	}
}

func TestCustomers_Table(t *testing.T) {
	cs := []Contract{
		{ID: "old", Number: "ORM-2020-1", Kind: ContractMooring, Term: "annual", Status: "renewed", Channel: "desk", CustomerID: "c1", StartsOn: d("2020-03-01"), EndsOn: d("2026-03-01"), Total: 1000},
		{ID: "new1", Number: "ORM-2026-1", Kind: ContractMooring, Term: "annual", Status: "active", Channel: "desk", CustomerID: "c1", StartsOn: d("2026-03-01"), EndsOn: d("2027-03-01"), Total: 36500},
		{ID: "t", Number: "TRN-2026-1", Kind: ContractTransit, Status: "departed", Channel: "online", CustomerID: "c2", StartsOn: d("2026-05-01"), EndsOn: d("2026-05-03"), Total: 200},
		{ID: "s", Number: "ORM-2026-2", Kind: ContractMooring, Term: "seasonal", Status: "expired", Channel: "desk", CustomerID: "c3", StartsOn: d("2025-05-01"), EndsOn: d("2026-04-01"), Total: 3360},
	}
	in := CustomersInput{
		From: d("2026-01-01"), To: d("2026-06-30"), Contracts: cs, GroupBy: "channel",
		Customers: map[string]CustomerInfo{"c1": {ID: "c1", Name: "Rossi", Nationality: "IT"}, "c2": {ID: "c2", Nationality: "FR"}},
		Arrears:   map[string]money.Cents{"c1": 500}, Renewals: []Renewal{{SourceContractID: "s", NewContractID: "missing"}},
	}
	res, facts := BuildCustomers(in)
	if res.Totals.Customers != 3 || res.Totals.NewCustomers != 1 || res.Totals.Arrears != 500 {
		t.Fatalf("%+v", res.Totals)
	}
	var c1 CustomersRecord
	for _, r := range res.Records {
		if r.CustomerID == "c1" {
			c1 = r
		}
	}
	if c1.SeniorityYears != 6 || c1.SeniorityBand != "5_9" || c1.New != 0 || c1.Contracts != 2 || c1.AcquisitionChannel != "desk" {
		t.Fatalf("c1 %+v", c1)
	}
	// renewals: old (annual, renewed, last night 2026-02-28) and s (seasonal, last night 2026-03-31, not renewed).
	if res.Totals.RenewalEligible != 2 || res.Totals.RenewalRenewed != 1 || res.Totals.RenewalRateBP.Value != 5000 {
		t.Fatalf("renewals %+v", res.Totals)
	}
	if s := facts[IndRenewalRate].Sum(); s.Num != 1 || s.Den != 2 || facts[IndNewCustomers].Sum().Num != 1 {
		t.Fatal("daily")
	}
	in.Section, in.NewOnly = "renewals", true
	res, _ = BuildCustomers(in)
	if len(res.Records) != 2 || res.Totals.Customers != 1 {
		t.Fatalf("sections %+v", res.Totals)
	}
}

func line(id string, seq int, issued, from, to string, net, vat money.Cents) ChargeLine {
	l := ChargeLine{
		ChargeID: id, Number: "A-" + id, Seq: seq, IssuedOn: d(issued), DueOn: d(issued), OriginType: "installment",
		CustomerID: "c1", Net: net, VAT: vat, Gross: net + vat, PaymentStatus: "open", InvoicingStatus: "to_invoice", FiscalMode: "invoice",
	}
	if from != "" {
		l.ServiceFrom, l.ServiceTo = d(from), d(to)
	}
	return l
}

func TestAccruals_Table(t *testing.T) {
	ls := []ChargeLine{
		line("1", 1, "2026-01-15", "2026-01-01", "2026-12-31", 36500, 8030), // annual, issued in the period
		line("2", 1, "2026-03-10", "", "", 1000, 220),                       // one day
		line("3", 1, "2026-07-01", "2026-07-01", "2026-07-31", 3100, 0),     // after C: no deferral, no competence
	}
	cs := []Contract{{ID: "k", Number: "ORM-1", Kind: ContractMooring, Term: "annual", Status: "active", StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Total: 73000}}
	res := BuildAccruals(AccrualsInput{
		From: d("2026-01-01"), To: d("2026-03-31"), Lines: ls, Contracts: cs,
		ContractCharges: []ContractCharge{{ContractID: "k", IssuedOn: d("2026-01-15"), Gross: 10_000}, {ContractID: "k", IssuedOn: d("2026-05-01"), Gross: 99}},
	})
	// line 1: 90 days of 365 → net 9000; line 2: 1000.
	if res.Totals.CompetenceNet != 10_000 || res.Totals.DeferralNet != 36500-9000 {
		t.Fatalf("%+v", res.Totals)
	}
	// contract: matured 90 × 200 = 18 000, charged 10 000 by C → rateo 8000.
	if res.Totals.Accrual != 8000 || res.Totals.ClosingDate != d("2026-03-31") {
		t.Fatalf("accrual %+v", res.Totals)
	}
	if len(res.Records) != 2 || res.Records[0].Section != "competence" {
		t.Fatalf("records %+v", res.Records)
	}
}

func TestCharges_CompetenceVATExact(t *testing.T) {
	ls := []ChargeLine{line("1", 1, "2026-01-10", "2026-01-01", "2026-03-31", 10_001, 2_201), line("1", 2, "2026-01-10", "", "", 500, 110)}
	issued, f := BuildCharges(ChargesInput{From: d("2026-01-01"), To: d("2026-12-31"), Lines: ls})
	if issued.Totals.Charges != 1 || issued.Totals.Lines != 2 || issued.Totals.Gross != 12_812 || issued.Totals.Charged != 12_812 {
		t.Fatalf("issued %+v", issued.Totals)
	}
	if f[IndCharged].Sum().Num != 12_812 {
		t.Fatal("daily charged")
	}
	var sum ChargesTotals
	for _, m := range []string{"2026-01", "2026-02", "2026-03"} {
		from := d(m + "-01")
		to := monthOf(from.AddDays(32)).AddDays(-1)
		r, _ := BuildCharges(ChargesInput{From: from, To: to, Basis: "competence", Lines: ls})
		sum.Net += r.Totals.Net
		sum.VAT += r.Totals.VAT
		sum.Gross += r.Totals.Gross
		if r.Totals.Gross != r.Totals.Net+r.Totals.VAT {
			t.Fatalf("gross must be net + VAT: %+v", r.Totals)
		}
	}
	if sum.Net != 10_501 || sum.VAT != 2_311 || sum.Gross != 12_812 {
		t.Fatalf("competence over the service period must be exact: %+v", sum)
	}
	// Default origins exclude opening balances.
	ob := line("9", 1, "2026-02-01", "", "", 100, 0)
	ob.OriginType = OriginOpeningBalance
	r, _ := BuildCharges(ChargesInput{From: d("2026-01-01"), To: d("2026-12-31"), Lines: append(ls, ob)})
	if r.Totals.Lines != 2 {
		t.Fatal("opening balances must be excluded by default")
	}
	r, _ = BuildCharges(ChargesInput{From: d("2026-01-01"), To: d("2026-12-31"), Lines: append(ls, ob), Filter: ChargeFilter{Origins: []string{OriginOpeningBalance}}})
	if r.Totals.Lines != 1 {
		t.Fatal("explicit origin")
	}
}

func TestRevenueClasses_MonthsSumToTotal(t *testing.T) {
	a := line("1", 1, "2026-01-10", "2026-01-15", "2026-03-14", 5900, 0)
	a.RevenueClass = "moor"
	b := line("2", 1, "2026-02-01", "", "", 700, 0)
	b.RevenueClass = "moor_x"
	c := line("3", 1, "2026-02-01", "", "", 300, 0)
	res := BuildRevenueClasses(RevenueInput{
		From: d("2026-01-01"), To: d("2026-12-31"), Lines: []ChargeLine{a, b, c},
		Classes: []RevenueClass{{Code: "moor", Label: "Ormeggi"}, {Code: "moor_x", Label: "Extra", ParentCode: "moor"}},
	})
	if res.Totals.Net != 6900 || res.Totals.Unclassified != 300 {
		t.Fatalf("%+v", res.Totals)
	}
	for _, g := range res.Classes {
		var s money.Cents
		for _, m := range g.Months {
			s += m.Net
		}
		if s != g.Net {
			t.Fatalf("class %s months %d != %d", g.Key, s, g.Net)
		}
		if g.Key == "moor" && g.Net != 6600 {
			t.Fatalf("parent rolls up its children: %+v", g)
		}
	}
	var recs money.Cents
	for _, r := range res.Records {
		recs += r.Net
	}
	if recs != res.Totals.Net || len(res.Records) != 5 {
		t.Fatalf("records %d %+v", recs, res.Records)
	}
}

func TestPayments_Table(t *testing.T) {
	in := PaymentsInput{
		From: d("2026-06-01"), To: d("2026-06-30"),
		Payments: []Payment{
			{ID: "p1", Number: "P1", Kind: "payment", Method: "cash", Amount: 1000, Fee: 0, ReceivedOn: d("2026-06-02")},
			{ID: "p2", Number: "P2", Kind: "deposit", Method: "card_online", Amount: 500, Fee: 15, ReceivedOn: d("2026-06-03")},
			{ID: "p3", Number: "P3", Kind: "credit", Method: "credit_note", Amount: 300, ReceivedOn: d("2026-06-03")},
		},
		Refunds: []Refund{
			{ID: "r1", Number: "R1", Kind: "refund", Status: "succeeded", Method: "cash", Amount: 200, BusinessDate: d("2026-06-04")},
			{ID: "r2", Number: "R2", Kind: "refund", Status: "failed", Method: "cash", Amount: 50, BusinessDate: d("2026-06-04")},
			{ID: "r3", Number: "R3", Kind: "payout", Status: "succeeded", Method: "bank_transfer", Amount: 700, BusinessDate: d("2026-06-05")},
		},
	}
	res, facts := BuildPayments(in)
	if res.Totals.Count != 3 || res.Totals.Collected != 1300 || res.Totals.Refunds != -200 || res.Totals.Payouts != 700 || res.Totals.Fees != 15 {
		t.Fatalf("%+v", res.Totals)
	}
	if facts[IndCollected].Sum().Num != 1300 {
		t.Fatal("daily")
	}
	in.Section = "payouts"
	if r, _ := BuildPayments(in); len(r.Records) != 1 || r.Totals.Collected != 1300 {
		t.Fatal("payout section")
	}
}

func TestLostDemand_Table(t *testing.T) {
	in := LostDemandInput{From: d("2026-07-01"), To: d("2026-07-31"), Full: true, GroupBy: "week", Inquiries: []Inquiry{
		{ID: "i1", Arrival: d("2026-06-29"), Departure: d("2026-07-03"), Outcome: "declined_no_space", Purpose: "transit", Channel: "desk", LOA: 1200, Beam: 400, Value: Some[money.Cents](400)},
		{ID: "i2", Arrival: d("2026-07-10"), Departure: d("2026-07-12"), Outcome: "declined_size", Purpose: "transit", Channel: "online", LOA: 3000, Beam: 800},
		{ID: "i3", Arrival: d("2026-08-01"), Departure: d("2026-08-02"), Outcome: "lost", Purpose: "transit"},
	}}
	res, facts := BuildLostDemand(in)
	if res.Totals.Count != 2 || res.Totals.Nights != 2+2 || res.Totals.Value.Value != 200 || res.Totals.Unpriced != 1 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Records[1].LengthBand != "ge25" || facts[IndLostDemandValue].Sum().Num != 200 || facts[IndLostDemandCount].Sum().Num != 2 {
		t.Fatalf("%+v", res.Records)
	}
	var gc int64
	for _, g := range res.Groups {
		gc += g.Totals.Count
	}
	if gc != 2 {
		t.Fatalf("week groups count %d", gc)
	}
	in.Full = false
	if r, _ := BuildLostDemand(in); r.Totals.Value.OK || r.Records[0].Value.OK {
		t.Fatal("values hidden")
	}
}

func TestReceivables_Table(t *testing.T) {
	asOf := d("2026-06-30")
	in := ReceivablesInput{
		AsOf: asOf, Open: []OpenCharge{
			{ID: "a", Number: "A1", DueOn: d("2026-07-15"), IssuedOn: d("2026-06-01"), Total: 1000, Bucket: BucketNotDue},
			{ID: "b", Number: "A2", DueOn: d("2026-05-01"), IssuedOn: d("2026-04-01"), Total: 1000, Paid: 400, Bucket: BucketD31_60},
			{ID: "c", Number: "A3", DueOn: d("2026-01-01"), IssuedOn: d("2025-12-01"), Total: 500, Credited: 500, Bucket: BucketD90Plus},
		}, Future: []ScheduledInstallment{{Kind: "installment", ID: "i", DueOn: d("2026-08-01"), Amount: 300}},
		Reminders: []Reminder{{ID: "r", ChargeID: "b", Level: "formal", SentOn: d("2026-06-10")}},
	}
	res := BuildReceivables(in)
	if res.Totals.Count != 2 || res.Totals.Open != 1600 || res.Totals.Overdue != 600 || res.Totals.Buckets.D31_60 != 600 ||
		res.Totals.Future != 300 || res.Totals.RemindersSent != 1 {
		t.Fatalf("%+v", res.Totals)
	}
	in.OverdueOnly = true
	if r := BuildReceivables(in); r.Totals.Open != 600 || r.Totals.Overdue != 600 {
		t.Fatalf("overdue %+v", r.Totals)
	}
	if a := ArrearsOf(in.Open, asOf); len(a) != 0 {
		t.Fatalf("no customer: %v", a)
	}
}

func TestPresences_Table(t *testing.T) {
	in := PresencesInput{
		From: d("2026-06-01"), To: d("2026-06-30"), Today: d("2026-06-20"), Full: true,
		Events: []PresenceEvent{
			{ID: "e0", VesselID: "v", Event: "departed", Night: d("2026-05-28")},
			{ID: "e1", VesselID: "v", Event: "arrived", Night: d("2026-06-05")},
			{ID: "e2", VesselID: "v", Event: "departed", Night: d("2026-06-15")},
			{ID: "e3", VesselID: "w", Event: "arrived", Night: d("2026-06-02")},
		},
		Contracts: []AbsenceContract{{ID: "k", Number: "ORM-1", VesselID: "v", StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01")}},
		Releases: []ReleaseInfo{{
			ID: "r", From: d("2026-06-10"), To: d("2026-06-14"), Credit: 500,
			ResoldNights: []calendarDate{d("2026-06-11"), d("2026-06-12")},
			Resale:       ContractValue{Total: 300, StartsOn: d("2026-06-11"), EndsOn: d("2026-06-14")},
		}},
	}
	res, facts := BuildPresences(in)
	// absent 06-01..06-04 (4) and 06-15..06-20 (open until today: 6).
	if res.Totals.Arrivals != 2 || res.Totals.Departures != 1 || res.Totals.AbsenceNights != 10 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Totals.ReleasedNights != 5 || res.Totals.ResoldNights != 2 || res.Totals.ReleaseRevenue.Value != 200 || res.Totals.ReleaseCredit.Value != 500 {
		t.Fatalf("releases %+v", res.Totals)
	}
	if facts[IndArrivals].Sum().Num != 2 || facts[IndDepartures].Sum().Num != 1 {
		t.Fatal("daily")
	}
	in.Full = false
	if r, _ := BuildPresences(in); r.Totals.ReleaseRevenue.OK {
		t.Fatal("money hidden")
	}
}

func TestRights_Table(t *testing.T) {
	res := BuildRights(RightsInput{
		Holders: []RightHolder{{RightID: "r1", BerthID: "b1", BerthCode: "A1", HolderID: "c1", ShareMillis: 500}, {RightID: "r1", BerthID: "b1", BerthCode: "A1", HolderID: "c2", ShareMillis: 500}},
		Charges: []HolderCharge{{ChargeID: "x", Total: 1000, Paid: 300, Credited: 100, IssuedOn: d("2026-02-01")}},
		Allotment: []Allotment{
			{RunID: "run", RunKind: "advance", ItemCode: "Pulizia", Amount: 400, CustomerID: "c1", Date: d("2026-02-01")},
			{RunID: "run", RunKind: "advance", ItemCode: "Pulizia", Amount: 100, Date: d("2026-02-01")},
			{RunID: "bal", RunKind: "balance", ItemCode: "Pulizia", Amount: -50, CustomerID: "c1", Date: d("2026-03-01")},
		},
		Mandates: []MandateMove{{Kind: "revenue", Amount: 900}, {Kind: "payout", Amount: 600}},
		GroupBy:  "run_kind",
	})
	tt := res.Totals
	if tt.Rights != 1 || tt.Holders != 2 || tt.Berths != 1 || tt.ChargesIssued != 1000 || tt.ChargesResidual != 600 ||
		tt.Advance != 500 || tt.Balance != -50 || tt.MarinaShare != 100 || tt.MandateRevenue != 900 || tt.MandatePayout != 600 {
		t.Fatalf("%+v", tt)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups %+v", res.Groups)
	}
}

func TestCash_Table(t *testing.T) {
	res := BuildCash(CashInput{
		Sessions: []CashSession{{
			ID: "s", RegisterCode: "C1", BusinessDate: d("2026-06-01"), Expected: map[string]money.Cents{"cash": 1000, "card_pos": 500},
			Counted: map[string]money.Cents{"cash": 990, "card_pos": 500}, Difference: -10,
		}},
		Documents: []FiscalDocument{
			{
				ID: "f1", Number: "1", Status: "issued", Method: "cash", Total: 1220, BusinessDate: d("2026-06-01"),
				VAT: []summaryVATRow{{RateBP: 2200, Gross: 1220, Net: 1000, VAT: 220}},
			},
			{ID: "f2", Number: "2", Status: "void", Method: "cash", Total: 500, BusinessDate: d("2026-06-01")},
		},
	})
	tt := res.Totals
	if tt.Sessions != 1 || tt.Difference != -10 || tt.Documents != 1 || tt.FiscalTotal != 1220 || tt.FiscalVoid != -500 {
		t.Fatalf("%+v", tt)
	}
	if len(tt.VAT) != 1 || tt.VAT[0].VAT != 220 || len(tt.Methods) != 1 || tt.Methods[0].Total != 1220 {
		t.Fatalf("vat %+v methods %+v", tt.VAT, tt.Methods)
	}
	if res.Records[0].Expected != 1500 || res.Records[0].Counted != 1490 {
		t.Fatalf("%+v", res.Records)
	}
}

// TestCalc_AccrualsCompetenceEqualsChargesCompetence: R7 and R8 (competence basis) spread a line the same way,
// gross quota = net quota + VAT quota, also when the amounts do not divide evenly by the days.
func TestCalc_AccrualsCompetenceEqualsChargesCompetence(t *testing.T) {
	ls := []ChargeLine{
		line("1", 1, "2026-01-01", "2026-01-01", "2026-01-02", 1, 1),          // gross 2 over 2 days
		line("2", 1, "2026-01-05", "2026-01-01", "2026-03-31", 10_001, 2_201), // 90 days
		line("3", 1, "2026-02-01", "2026-02-01", "2026-02-07", 13, 3),
	}
	periods := [][2]string{{"2026-01-01", "2026-01-01"}, {"2026-01-01", "2026-01-31"}, {"2026-02-01", "2026-02-03"}, {"2026-02-15", "2026-03-31"}}
	for _, p := range periods {
		from, to := d(p[0]), d(p[1])
		acc := BuildAccruals(AccrualsInput{From: from, To: to, Lines: ls})
		ch, _ := BuildCharges(ChargesInput{From: from, To: to, Basis: "competence", Lines: ls})
		byLine := map[string]ChargesRecord{}
		for _, r := range ch.Records {
			byLine[r.ChargeID] = r
		}
		for _, r := range acc.Records {
			if r.Section != "competence" {
				continue
			}
			c := byLine[r.ChargeID]
			if r.CompetenceNet != c.Net || r.CompetenceGross != c.Gross {
				t.Errorf("%v line %s: accruals net %d gross %d, charges net %d gross %d", p, r.ChargeID, r.CompetenceNet, r.CompetenceGross, c.Net, c.Gross)
			}
		}
		if acc.Totals.CompetenceGross != ch.Totals.Gross {
			t.Errorf("%v: accruals gross %d, charges gross %d", p, acc.Totals.CompetenceGross, ch.Totals.Gross)
		}
	}
	// Deferral gross at C = gross − (net + VAT quotas up to C): line 1 at 2026-01-01 → 2 − 0.
	res := BuildAccruals(AccrualsInput{From: d("2026-01-01"), To: d("2026-01-01"), Section: "deferral", Lines: ls[:1]})
	if res.Totals.DeferralGross != 2 || res.Totals.DeferralNet != 1 {
		t.Fatalf("deferral %+v", res.Totals)
	}
}
