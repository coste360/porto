// Package workordertest builds the backend of the work order integration tests: contracttest's harbour, the
// charge and work order services, the catalogue of m10-plan §4.4 (items, work types ANTIFOULING and PROPULSION,
// qualifications hand and painter, yard costs), three employees and an accepted quote of the antifouling job on a
// 12 m vessel, so that the order side can be exercised without the quote engine. Test support only.
package workordertest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// Env is the backend.
type Env struct {
	*contracttest.Env
	Charges *charge.Service
	WO      *workorder.Service
	Scope   store.MarinaScope

	Items     map[string]string // code → id
	WTAnti    string
	WTProp    string
	QualHand  string
	QualPaint string
	Luca      string // employee (hand, 22.00/h)
	Marco     string // employee (hand, 24.00/h), linked to the Yard user
	Giulia    string // employee (painter, 26.00/h)
	Boat      string // 12 × 4 m, draft 1.5 m
	Yard      *auth.Principal
	YardMgr   *auth.Principal
	Office    *auth.Principal
	Bookings  *FakeBookings
}

// FakeBookings records the lifting cancellations of cancelled orders.
type FakeBookings struct{ Cancelled []string }

// CancelForWorkOrderTx implements workorder.BookingCanceller.
func (f *FakeBookings) CancelForWorkOrderTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _ audit.Actor, id string, _ calendar.Date) error {
	f.Cancelled = append(f.Cancelled, id)
	return nil
}

// New builds the environment on db.
func New(t testing.TB, db *store.DB) *Env {
	t.Helper()
	e := &Env{Env: contracttest.New(t, db), Items: map[string]string{}}
	e.Scope = e.Env.Scope(t)
	e.Charges = charge.NewService(charge.Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: e.Files, Prices: e.Prices, Log: e.Log,
	})
	e.WO = workorder.NewService(workorder.Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Keys: e.Keys, Files: e.Files, Charges: e.Charges,
		Prices: e.Prices,
	})
	e.Bookings = &FakeBookings{}
	e.WO.SetBookingCanceller(e.Bookings)
	e.catalog(t)
	e.Boat = e.AddVessel(t, "Maestrale", 1200, 400, "monohull_sail")
	e.Yard = e.AddUser(t, "yard@example.test", auth.RoleYard, e.OrgID, e.MarinaID)
	e.YardMgr = e.AddUser(t, "yardmgr@example.test", auth.RoleYardMgr, e.OrgID, e.MarinaID)
	e.Office = e.AddUser(t, "office@example.test", auth.RoleOffice, e.OrgID, e.MarinaID)
	e.Luca = e.employee(t, "LUC", "Luca", 2200, "", e.QualHand)
	e.Marco = e.employee(t, "MAR", "Marco", 2400, e.Yard.UserID, e.QualHand)
	e.Giulia = e.employee(t, "GIU", "Giulia", 2600, "", e.QualPaint)
	return e
}

func (e *Env) now() time.Time { return store.UTC(e.Clock.Now()) }

func (e *Env) item(t testing.TB, code, typ, unit string, costCents int64) string {
	t.Helper()
	ctx := fixture.Ctx()
	classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil || len(classes) == 0 {
		t.Fatalf("revenue classes: %v", err)
	}
	incl, bp := false, int64(2200)
	it, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: code, Name: catalog.I18nText{It: code}, ItemType: typ, Unit: unit, PriceIncludesVAT: &incl, VATRateBP: &bp,
		RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatalf("item %s: %v", code, err)
	}
	now := e.now()
	e.Insert(t, &store.YardItemSettings{
		ItemID: it.ID, MarinaID: e.MarinaID, YardEnabled: true, CostCents: costCents, TestCase: nil, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	e.Items[code] = it.ID
	return it.ID
}

func (e *Env) catalog(t testing.TB) {
	e.item(t, "SRV-ALAG", "service", "m", 0)
	e.item(t, "SRV-LAV", "service", "m2", 100)
	hand := e.item(t, "LAB-HAND", "labour", "hour", 0)
	paint := e.item(t, "LAB-PAINT", "labour", "hour", 0)
	e.item(t, "MAT-AVEG", "material", "piece", 5800)
	e.item(t, "SRV-VARO", "service", "m", 0)
	e.item(t, "EXT-SRV", "service", "lump", 0)
	now := e.now()
	wt := func(code string) string {
		w := &store.WorkType{ID: store.NewID(), MarinaID: e.MarinaID, Code: code, Name: store.JSON(`{"it":"` + code + `"}`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		e.Insert(t, w)
		return w.ID
	}
	e.WTAnti, e.WTProp = wt("ANTIFOULING"), wt("PROPULSION")
	q := func(code, item string, cost int64) string {
		r := &store.YardQualification{
			ID: store.NewID(), MarinaID: e.MarinaID, Code: code, Name: store.JSON(`{"it":"` + code + `"}`), LabourItemID: item,
			DefaultCostCents: cost, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, r)
		return r.ID
	}
	e.QualHand, e.QualPaint = q("hand", hand, 2200), q("painter", paint, 2600)
	// Yard settings: the external costs use EXT-SRV (markup 20 %), advance at acceptance (deposit 30 %).
	e.Insert(t, &store.YardSettings{
		MarinaID: e.MarinaID, TestVessel: store.JSON(`{}`), QuoteValidityDays: 30, DepositBP: 3000, AdvanceOnAccept: true,
		ExternalMarkupBP: 2000, ExternalItemID: e.Items["EXT-SRV"], LiftFromSlot: 28, LiftToSlot: 76, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
}

func (e *Env) employee(t testing.TB, code, name string, cost int64, userID, qual string) string {
	t.Helper()
	now := e.now()
	emp := &store.Employee{
		ID: store.NewID(), MarinaID: e.MarinaID, Code: code, FirstName: name, LastName: "Test", UserID: userID, HourlyCostCents: cost,
		Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, emp, &store.EmployeeQualification{EmployeeID: emp.ID, QualificationID: qual, MarinaID: e.MarinaID})
	for wd := 1; wd <= 5; wd++ {
		e.Insert(t, &store.EmployeeCalendar{EmployeeID: emp.ID, Weekday: wd, MarinaID: e.MarinaID, Minutes: 480})
	}
	return emp.ID
}

// Line is a quote line of the antifouling job (m10-plan §4.4).
type Line struct {
	Code       string
	Qty, Unit  int64
	Cost       int64
	Qual       string
	TaskTitle  string
	WorkTypeID string
}

// AntifoulingLines are the six lines of §4.4 (layers = 2, monohull): net 1,702.60, VAT 374.57, gross 2,077.17.
func (e *Env) AntifoulingLines() []Line {
	return []Line{
		{Code: "SRV-ALAG", Qty: 12000, Unit: 1800},
		{Code: "SRV-LAV", Qty: 56100, Unit: 400, Cost: 100},
		{Code: "LAB-HAND", Qty: 8400, Unit: 3800, Qual: e.QualHand, TaskTitle: "Carteggiatura"},
		{Code: "LAB-PAINT", Qty: 5600, Unit: 4500, Qual: e.QualPaint, TaskTitle: "Antivegetativa"},
		{Code: "MAT-AVEG", Qty: 5000, Unit: 9500, Cost: 5800},
		{Code: "SRV-VARO", Qty: 12000, Unit: 1800},
	}
}

// AcceptedQuote inserts an accepted job quote with the lines (deposit 30 %) and returns what the quote side passes
// to the OrderCreator.
func (e *Env) AcceptedQuote(t testing.TB, lines []Line, billingMode string) yardquote.AcceptedQuote {
	t.Helper()
	now := e.now()
	today := calendar.In(now, time.UTC)
	q := store.YardQuote{
		ID: store.NewID(), MarinaID: e.MarinaID, Year: today.Year(), Kind: store.YardQuoteJob,
		CustomerID: e.Customer, VesselID: e.Boat, Status: store.YardQuoteAccepted, QuotedOn: today, ValidUntil: today.AddDays(30),
		ExpiresAt: now.Add(30 * 24 * time.Hour), BillingMode: billingMode, JobInputs: store.JSON(`{}`), VesselOverrides: store.JSON(`{}`),
		Snapshot: store.JSON(`{}`), EstimatedHours: store.JSON(`{}`), AcceptedVia: store.AcceptedViaPanel, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	var n int64
	if err := e.DB.NewSelect().TableExpr("yard_quotes").ColumnExpr("COUNT(*)").Where("marina_id = ?", e.MarinaID).Scan(fixture.Ctx(), &n); err != nil {
		t.Fatal(err)
	}
	q.Number = n + 1
	snap := price.Snapshot{Prices: map[string]price.UnitPrice{}, Items: map[string]price.ItemData{}}
	var rows []store.YardQuoteLine
	var gross money.Cents
	for i, l := range lines {
		id := e.Items[l.Code]
		net, _ := moneyMul(l.Qty, l.Unit)
		vat := net.PercentBP(2200)
		gross += net + vat
		wt := l.WorkTypeID
		if wt == "" {
			wt = e.WTAnti
		}
		r := store.YardQuoteLine{
			ID: store.NewID(), MarinaID: e.MarinaID, QuoteID: q.ID, Seq: i + 1, Source: store.YardLineSourcePackage, ItemID: id,
			WorkTypeID: wt, QualificationID: l.Qual, Description: l.Code, QtyMilli: l.Qty, UnitCents: l.Unit, NetCents: int64(net),
			VATCents: int64(vat), GrossCents: int64(net + vat), VATRateBP: 2200, UnitCostCents: l.Cost, Issues: store.JSON(`[]`),
		}
		typ := "service"
		if l.Qual != "" {
			typ, r.EstimatedHoursMilli = "labour", l.Qty
		}
		if l.Code == "MAT-AVEG" {
			typ = "material"
		}
		if l.TaskTitle != "" {
			r.Task, _ = json.Marshal(price.TaskSpec{Title: map[string]string{"it": l.TaskTitle}})
		}
		rows = append(rows, r)
		snap.Prices[id] = price.UnitPrice{ItemID: id, UnitCents: money.Cents(l.Unit), Found: true}
		snap.Items[id] = price.ItemData{
			ItemID: id, Code: l.Code, ItemType: typ, CostCents: money.Cents(l.Cost), QualificationID: l.Qual,
			VAT: price.ItemVAT{RateBP: 2200},
		}
	}
	q.NetCents, q.GrossCents = 0, int64(gross)
	q.DepositAmountCents = int64(gross.PercentBP(3000))
	bp := 3000
	q.DepositBP = &bp
	e.Insert(t, &q)
	return yardquote.AcceptedQuote{Quote: q, Lines: rows, Snapshot: snap, AcceptedAt: now, Via: store.AcceptedViaPanel}
}

func moneyMul(qtyMilli, unit int64) (money.Cents, error) {
	return money.Cents(unit).MulRatChecked(qtyMilli, 1000)
}

// Accept creates the order of an accepted quote with the owner as actor.
func (e *Env) Accept(t testing.TB, q yardquote.AcceptedQuote) yardquote.OrderRef {
	t.Helper()
	var ref yardquote.OrderRef
	err := e.DB.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
		var err error
		ref, err = e.WO.CreateFromQuoteTx(ctx, tx, e.Scope, e.Owner.Actor(), q)
		return err
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	return ref
}

// YardBasics are the rows InsertYardBasics writes for the HTTP tests.
type YardBasics struct {
	WorkType      string
	Qualification string
	Luca          string // employee without a user
	Marco         string // employee linked to the user with yardEmail
}

// InsertYardBasics inserts, for the handler tests that build items through the API, the yard rows whose API belongs
// to other builders: yard settings (markup 20 %, external item), the yard cost of the items, a work type
// ANTIFOULING, a qualification hand on the labour item and two employees (Marco linked to the yard user).
func InsertYardBasics(t testing.TB, db *store.DB, marinaID string, costs map[string]int64, labourItem, externalItem, yardEmail string) YardBasics {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC() //nolint:forbidigo // test fixture rows, the HTTP test server runs on the wall clock
	ins := func(r any) {
		if _, err := db.NewInsert().Model(r).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
	for item, cost := range costs {
		ins(&store.YardItemSettings{ItemID: item, MarinaID: marinaID, YardEnabled: true, CostCents: cost, Version: 1, CreatedAt: now, UpdatedAt: now})
	}
	wt := &store.WorkType{ID: store.NewID(), MarinaID: marinaID, Code: "ANTIFOULING", Name: store.JSON(`{"it":"Antivegetativa"}`), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	q := &store.YardQualification{ID: store.NewID(), MarinaID: marinaID, Code: "hand", Name: store.JSON(`{"it":"Manovale"}`), LabourItemID: labourItem, DefaultCostCents: 2200, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	ins(&store.YardSettings{MarinaID: marinaID, TestVessel: store.JSON(`{}`), QuoteValidityDays: 30, DepositBP: 3000, AdvanceOnAccept: true, ExternalMarkupBP: 2000, ExternalItemID: externalItem, LiftFromSlot: 28, LiftToSlot: 76, Version: 1, CreatedAt: now, UpdatedAt: now})
	ins(wt)
	ins(q)
	var yardUser string
	if err := db.NewSelect().TableExpr("users").Column("id").Where("email = ?", yardEmail).Scan(ctx, &yardUser); err != nil {
		t.Fatal(err)
	}
	luca := &store.Employee{ID: store.NewID(), MarinaID: marinaID, Code: "LUC", FirstName: "Luca", LastName: "B", HourlyCostCents: 2200, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	marco := &store.Employee{ID: store.NewID(), MarinaID: marinaID, Code: "MAR", FirstName: "Marco", LastName: "C", UserID: yardUser, HourlyCostCents: 2400, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	ins(luca)
	ins(marco)
	ins(&store.EmployeeQualification{EmployeeID: luca.ID, QualificationID: q.ID, MarinaID: marinaID})
	ins(&store.EmployeeQualification{EmployeeID: marco.ID, QualificationID: q.ID, MarinaID: marinaID})
	return YardBasics{WorkType: wt.ID, Qualification: q.ID, Luca: luca.ID, Marco: marco.ID}
}

// Assign plans hours of an employee on a task for a day (planner rows, written directly).
func Assign(t testing.TB, db *store.DB, marinaID, taskID, employeeID string, day calendar.Date, hoursMilli int64) {
	t.Helper()
	now := time.Now().UTC() //nolint:forbidigo // test fixture rows
	if _, err := db.NewInsert().Model(&store.TaskAssignment{
		ID: store.NewID(), MarinaID: marinaID, TaskID: taskID, EmployeeID: employeeID, WorkDate: day, HoursMilli: hoursMilli,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}
