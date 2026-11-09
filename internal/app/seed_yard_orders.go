package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/lifting"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedYardOrders adds the M10 demo of ORDERS (m10-plan §16.2). The first five draft quotes of the catalogue seed
// are accepted from the panel (advance charges) and become work orders on the boats on the hard (bay = their
// storage allocation): two in progress (hauled out a few days ago, materials issued and returned, an external
// cost), one on hold waiting for parts, two starting this week; each gets extra tasks besides the ANTIVEG ones.
// Three small orders without a quote complete the board: one completed waiting for its launch today, one
// delivered, one completed and invoiced with its final account. Their hours are logged and approved on past days.
// The lifting schedule follows the orders (haul-outs done in the past, launches and haul-outs today, tomorrow
// and this week on the travel lift, mast and engine lifts on the crane). Hours of the open orders follow the
// planner (seedYardPlannerAssignments). Everything goes through the services as the demo owner, past moments
// through an App whose clock is set to them; a no-op when the marina already has work orders or no yard catalogue.
func (a *App) seedYardOrders(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.WorkOrders == nil || a.YardQuotes == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("work_orders").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	var quotes []store.YardQuote
	if err := a.DB.NewSelect().Model(&quotes).Where("marina_id = ?", scope.ID()).Where("kind = ?", store.YardQuoteJob).
		Where("status = ?", store.YardQuoteDraft).OrderExpr("year, number").Limit(int64(len(seedYardOrderPlans))).Scan(ctx); err != nil || len(quotes) == 0 {
		return err
	}
	loc := seedPlannerLocation(ctx, a, scope)
	now := a.Clock.Now()
	today := calendar.In(now, loc)
	y := &seedYard{a: a, ctx: ctx, p: p, m: m, scope: scope, loc: loc, today: today, now: now}
	if err := y.load(); err != nil {
		return err
	}
	boats, err := a.seedYardBoats(ctx, scope, today, seedYardQuotes)
	if err != nil {
		return err
	}
	allocOf := map[string]string{}
	for _, b := range boats {
		allocOf[b.VesselID] = b.AllocationID
	}
	fake := clock.NewFake(now)
	past, err := NewWithOptions(ctx, a.Config, a.Version, a.ext, a.Log, Options{
		DB: a.DB, Clock: fake, Argon2: a.opts.Argon2, Transport: a.opts.Transport,
	})
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	y.past, y.fake = past, fake

	var first *workorder.OrderView
	var firstQuote store.YardQuote
	for i, q := range quotes {
		plan := seedYardOrderPlans[i]
		acc, err := a.YardQuotes.Accept(ctx, p, m, q.ID, q.Version)
		if err != nil {
			return fmt.Errorf("yard quote accept: %w", err)
		}
		if acc.Order.WorkOrderID == "" {
			continue
		}
		o, err := y.prepare(acc.Order.WorkOrderID, allocOf[q.VesselID], plan)
		if err != nil {
			return err
		}
		if err := y.advance(o, plan); err != nil {
			return err
		}
		if i == 0 {
			first, firstQuote = o, q
		}
	}
	if first == nil {
		return nil
	}
	if err := y.manualOrders(firstQuote, first, boats); err != nil {
		return err
	}
	return y.lifts()
}

// seedYardOrderPlan is the scenario of an accepted quote's work order.
type seedYardOrderPlan struct {
	start, delivery int    // planned dates, days from today
	status          string // final status: in_progress, on_hold, planned
	extra           []seedYardTask
	external        int64 // external cost (cents), 0 = none
}

type seedYardTask struct {
	title, workType, qual string
	hours                 int64 // estimate, whole hours
}

var seedYardOrderPlans = []seedYardOrderPlan{
	{-6, 5, store.WorkOrderInProgress, []seedYardTask{
		{"Revisione motore e invertitore", "MECH", "hand", 20},
		{"Lucidatura opera morta", "HULL", "hand", 16},
		{"Sostituzione anodi", "HULL", "hand", 6},
	}, 15000},
	{-3, 8, store.WorkOrderInProgress, []seedYardTask{
		{"Tagliando motore", "MECH", "hand", 16}, {"Ripristino gelcoat a prua", "HULL", "painter", 20},
	}, 0},
	{-8, 6, store.WorkOrderOnHold, []seedYardTask{
		{"Sostituzione cuscinetto asse", "MECH", "hand", 10},
	}, 42000},
	{1, 10, store.WorkOrderPlanned, []seedYardTask{
		{"Revisione winch e stopper", "MECH", "hand", 16},
		{"Trattamento teak in coperta", "HULL", "painter", 24},
		{"Sostituzione tientibene", "HULL", "hand", 12},
	}, 0},
	{3, 14, store.WorkOrderPlanned, []seedYardTask{
		{"Verniciatura fondo pozzetto", "HULL", "painter", 20},
		{"Controllo impianto elettrico", "MECH", "hand", 12},
		{"Revisione impianto idraulico", "MECH", "hand", 10},
	}, 0},
}

// seedYard carries the lookups of the yard orders seed.
type seedYard struct {
	a           *App
	past        *App
	fake        *clock.Fake
	ctx         context.Context
	p           *auth.Principal
	m           string
	scope       store.MarinaScope
	loc         *time.Location
	today       calendar.Date
	now         time.Time
	emps        []store.Employee
	quals       map[string]string // code → id
	workTypes   map[string]string // code → id
	lift, crane string
	operators   []string
	orders      []seedYardOrder
}

// seedYardOrder is a demo order and what its lifts need.
type seedYardOrder struct {
	id, vessel      string
	start, delivery calendar.Date
	status          string
}

func (y *seedYard) load() error {
	db := y.a.DB
	if err := db.NewSelect().Model(&y.emps).Where("marina_id = ?", y.scope.ID()).Where("active = ?", true).OrderExpr("code").Scan(y.ctx); err != nil {
		return err
	}
	var quals []store.YardQualification
	if err := db.NewSelect().Model(&quals).Where("marina_id = ?", y.scope.ID()).Scan(y.ctx); err != nil {
		return err
	}
	y.quals = map[string]string{}
	for _, q := range quals {
		y.quals[q.Code] = q.ID
	}
	wts, err := store.ListWorkTypes(y.ctx, db.DB, y.scope, nil)
	if err != nil {
		return err
	}
	y.workTypes = map[string]string{}
	for _, w := range wts {
		y.workTypes[w.Code] = w.ID
	}
	var eq []store.Equipment
	if err := db.NewSelect().Model(&eq).Where("marina_id = ?", y.scope.ID()).Where("active = ?", true).Where("bookable = ?", true).
		OrderExpr("code").Scan(y.ctx); err != nil {
		return err
	}
	for _, e := range eq {
		switch {
		case e.Kind == "travel_lift" && y.lift == "":
			y.lift = e.ID
		case e.Kind == "crane" && y.crane == "":
			y.crane = e.ID
		}
	}
	if y.lift != "" {
		ops, err := y.a.Lifting.ListOperators(y.ctx, y.p, y.m, y.lift)
		if err != nil {
			return err
		}
		y.operators = ops
	}
	return nil
}

// at is a moment of day d in the marina time zone.
func (y *seedYard) at(d calendar.Date, hour int) time.Time {
	return d.Time(y.loc).Add(time.Duration(hour) * time.Hour)
}

// prepare puts the accepted order on its bay and planned dates and adds the extra tasks of the plan.
func (y *seedYard) prepare(orderID, allocationID string, plan seedYardOrderPlan) (*workorder.OrderView, error) {
	o, err := y.a.WorkOrders.GetOrder(y.ctx, y.p, y.m, orderID)
	if err != nil {
		return nil, err
	}
	start, delivery := y.today.AddDays(plan.start), y.today.AddDays(plan.delivery)
	patch := workorder.OrderPatch{PlannedStartOn: &start, PlannedDeliveryOn: &delivery}
	if allocationID != "" {
		patch.AllocationID = &allocationID
	}
	if o, err = y.a.WorkOrders.UpdateOrder(y.ctx, y.p, y.m, orderID, int64(o.Row.Version), patch); err != nil {
		return nil, fmt.Errorf("seed order update: %w", err)
	}
	for _, t := range plan.extra {
		if _, err := y.a.WorkOrders.CreateTask(y.ctx, y.p, y.m, orderID, workorder.TaskInput{
			Title: t.title, QualificationID: y.quals[t.qual], WorkTypeID: y.workTypes[t.workType], EstimatedHours: qty.Milli(t.hours * 1000),
		}); err != nil {
			return nil, fmt.Errorf("seed task: %w", err)
		}
	}
	y.orders = append(y.orders, seedYardOrder{id: orderID, vessel: o.Row.VesselID, start: start, delivery: delivery, status: plan.status})
	return y.a.WorkOrders.GetOrder(y.ctx, y.p, y.m, orderID)
}

// advance moves an accepted order to its plan status: started orders get materials and their external cost;
// the order on hold has some work done before it stopped.
func (y *seedYard) advance(o *workorder.OrderView, plan seedYardOrderPlan) error {
	if plan.status == store.WorkOrderPlanned {
		return nil
	}
	v, err := y.a.WorkOrders.Transition(y.ctx, y.p, y.m, o.Row.ID, int64(o.Row.Version), workorder.ActionStart, "")
	if err != nil {
		return fmt.Errorf("seed start: %w", err)
	}
	// Materials of the budget: one more than budgeted issued, one returned.
	for _, l := range o.Lines {
		it := o.Items[l.ItemID]
		if it.ItemType != "material" && it.ItemType != "product" {
			continue
		}
		for _, mv := range []struct {
			kind string
			q    qty.Milli
		}{{store.StockIssue, qty.Milli(l.QtyMilli + 1000)}, {store.StockReturn, 1000}} {
			if _, err := y.a.WorkOrders.CreateMovement(y.ctx, y.p, y.m, workorder.MovementInput{
				WorkOrderID: o.Row.ID, ItemID: l.ItemID, Kind: mv.kind, Qty: mv.q,
			}); err != nil {
				return fmt.Errorf("seed %s: %w", mv.kind, err)
			}
		}
	}
	if plan.external > 0 {
		if err := y.external(o, plan.external); err != nil {
			return err
		}
	}
	if plan.status == store.WorkOrderOnHold {
		tasks, err := y.a.WorkOrders.ListTasks(y.ctx, y.p, y.m, o.Row.ID)
		if err != nil {
			return err
		}
		if err := y.logPast(tasks, y.today.AddDays(plan.start), y.today.AddDays(-2), 1); err != nil {
			return err
		}
		if _, err := y.a.WorkOrders.Transition(y.ctx, y.p, y.m, o.Row.ID, int64(v.Row.Version), workorder.ActionHold, "In attesa del cuscinetto dal fornitore"); err != nil {
			return fmt.Errorf("seed hold: %w", err)
		}
	}
	return nil
}

// external records an external cost on the item of the yard settings, else the first service of the budget.
func (y *seedYard) external(o *workorder.OrderView, cost int64) error {
	extItem := ""
	if set, ok, err := store.WorkOrderYardSettings(y.ctx, y.a.DB.DB, y.scope); err != nil {
		return err
	} else if ok {
		extItem = set.ExternalItemID
	}
	for _, l := range o.Lines {
		if extItem == "" && o.Items[l.ItemID].ItemType == "service" {
			extItem = l.ItemID
		}
	}
	if extItem == "" {
		return nil
	}
	supplier, desc := "Eliche Tirreno Srl", "Lucidatura e bilanciatura elica"
	if cost > 30000 {
		supplier, desc = "Meccanica Navale Gaeta Srl", "Cuscinetto e boccola asse elica"
	}
	if _, err := y.a.WorkOrders.CreateExternal(y.ctx, y.p, y.m, o.Row.ID, workorder.ExternalInput{
		SupplierName: supplier, Reference: fmt.Sprintf("FT %d/%d", 100+len(y.orders), y.today.Year()), Description: desc,
		WorkTypeID: o.Row.WorkTypeID, Cost: money.Cents(cost), ItemID: extItem, IncurredOn: y.today.AddDays(-2),
	}); err != nil {
		return fmt.Errorf("seed external: %w", err)
	}
	return nil
}

// logPast logs and approves hours on the tasks over the working days from..to (at most hoursPerDay per task and
// day, never beyond the estimate) by the employees holding the task qualification, then records the progress.
func (y *seedYard) logPast(tasks []workorder.TaskView, from, to calendar.Date, hoursPerDay int64) error {
	var approve []workorder.EntryVersion
	for ti, t := range tasks {
		logged := int64(0)
		for d, k := from, 0; !d.After(to); d, k = d.AddDays(1), k+1 {
			if wd := d.Time(time.UTC).Weekday(); wd == time.Sunday {
				continue
			}
			h := seedStep(min(hoursPerDay*1000, t.Row.EstimatedHoursMilli-logged))
			if h <= 0 {
				break
			}
			e := y.employeeFor(t.Row.QualificationID, ti+k)
			if e == "" {
				break
			}
			hm := qty.Milli(h)
			v, err := y.a.WorkOrders.CreateEntry(y.ctx, y.p, y.m, workorder.EntryInput{
				ClientRef: fmt.Sprintf("seed-%s-%s", t.Row.ID, d), EmployeeID: e, TaskID: t.Row.ID, WorkDate: d, Hours: &hm,
			})
			if err != nil {
				return fmt.Errorf("seed entry: %w", err)
			}
			approve = append(approve, workorder.EntryVersion{ID: v.Row.ID, Version: int64(v.Row.Version)})
			logged += h
		}
		if t.Row.EstimatedHoursMilli > 0 && logged > 0 {
			bp := min(10000, logged*10000/t.Row.EstimatedHoursMilli)
			if _, err := y.a.WorkOrders.UpdateProgress(y.ctx, y.p, y.m, t.Row.ID, workorder.ProgressInput{
				ClientRef: "seed-p-" + t.Row.ID, ProgressBP: &bp,
			}); err != nil {
				return fmt.Errorf("seed progress: %w", err)
			}
		}
	}
	if len(approve) == 0 {
		return nil
	}
	if _, err := y.a.WorkOrders.Approve(y.ctx, y.p, y.m, approve); err != nil {
		return fmt.Errorf("seed approve: %w", err)
	}
	return nil
}

// employeeFor returns an employee holding the qualification (any employee when none is required), rotating on i.
func (y *seedYard) employeeFor(qualID string, i int) string {
	var ok []string
	for _, e := range y.emps {
		if qualID == "" {
			ok = append(ok, e.ID)
			continue
		}
		var n int64
		if c, err := y.a.DB.NewSelect().TableExpr("employee_qualifications").Where("marina_id = ?", y.scope.ID()).
			Where("employee_id = ?", e.ID).Where("qualification_id = ?", qualID).Count(y.ctx); err == nil {
			n = c
		}
		if n > 0 {
			ok = append(ok, e.ID)
		}
	}
	if len(ok) == 0 {
		return ""
	}
	return ok[i%len(ok)]
}

// manualOrders adds the orders without a quote on boats of the demo, with the service lines of the first order:
// one completed yesterday (launch today), one delivered last week, one completed and invoiced (final account).
func (y *seedYard) manualOrders(q store.YardQuote, o *workorder.OrderView, boats []seedYardBoat) error {
	var lines []workorder.ManualLine
	for _, l := range o.Lines {
		if o.Items[l.ItemID].ItemType == "service" && len(lines) < 2 {
			lines = append(lines, workorder.ManualLine{ItemID: l.ItemID, WorkTypeID: l.WorkTypeID, Qty: qty.Milli(l.QtyMilli), Unit: money.Cents(l.UnitCents)})
		}
	}
	if len(lines) == 0 {
		return nil
	}
	type manual struct {
		boat                  int // index in boats (falls back to the first quote's boat)
		start, delivery, done int
		note                  string
		task                  seedYardTask
		final                 string // completed, delivered, invoiced
	}
	for _, mo := range []manual{
		{8, -12, -1, -1, "Alaggio, carena e varo", seedYardTask{"Pulizia carena e zinchi", "HULL", "hand", 12}, store.WorkOrderCompleted},
		{7, -20, -8, -9, "Rimessaggio motore fuoribordo", seedYardTask{"Tagliando fuoribordo", "MECH", "hand", 6}, store.WorkOrderDelivered},
		{-1, -10, -2, -3, "Alaggio e varo per ispezione", seedYardTask{"Ispezione opera viva", "HULL", "hand", 4}, store.WorkOrderInvoiced},
	} {
		customer, vessel := q.CustomerID, q.VesselID
		if mo.boat >= 0 && mo.boat < len(boats) {
			customer, vessel = boats[mo.boat].CustomerID, boats[mo.boat].VesselID
		}
		v, err := y.a.WorkOrders.CreateOrder(y.ctx, y.p, y.m, workorder.OrderInput{
			CustomerID: customer, VesselID: vessel, WorkTypeID: o.Row.WorkTypeID, BillingMode: store.BillingModeActual,
			PlannedStartOn: y.today.AddDays(mo.start), PlannedDeliveryOn: y.today.AddDays(mo.delivery), ManualLines: lines, Notes: mo.note,
		})
		if err != nil {
			return fmt.Errorf("seed order: %w", err)
		}
		if _, err := y.a.WorkOrders.CreateTask(y.ctx, y.p, y.m, v.Row.ID, workorder.TaskInput{
			Title: mo.task.title, QualificationID: y.quals[mo.task.qual], WorkTypeID: y.workTypes[mo.task.workType],
			EstimatedHours: qty.Milli(mo.task.hours * 1000),
		}); err != nil {
			return fmt.Errorf("seed task: %w", err)
		}
		if v, err = y.a.WorkOrders.Transition(y.ctx, y.p, y.m, v.Row.ID, int64(v.Row.Version), workorder.ActionStart, ""); err != nil {
			return fmt.Errorf("seed start: %w", err)
		}
		tasks, err := y.a.WorkOrders.ListTasks(y.ctx, y.p, y.m, v.Row.ID)
		if err != nil {
			return err
		}
		if err := y.logPast(tasks, y.today.AddDays(mo.start+1), y.today.AddDays(mo.done), 6); err != nil {
			return err
		}
		if v, err = y.a.WorkOrders.GetOrder(y.ctx, y.p, y.m, v.Row.ID); err != nil {
			return err
		}
		if v, err = y.a.WorkOrders.Transition(y.ctx, y.p, y.m, v.Row.ID, int64(v.Row.Version), workorder.ActionComplete, ""); err != nil {
			return fmt.Errorf("seed complete: %w", err)
		}
		y.orders = append(y.orders, seedYardOrder{id: v.Row.ID, vessel: vessel, start: y.today.AddDays(mo.start), delivery: y.today.AddDays(mo.delivery), status: mo.final})
		switch mo.final {
		case store.WorkOrderDelivered:
			if _, err := y.a.WorkOrders.Transition(y.ctx, y.p, y.m, v.Row.ID, int64(v.Row.Version), workorder.ActionDeliver, ""); err != nil {
				return fmt.Errorf("seed deliver: %w", err)
			}
		case store.WorkOrderInvoiced:
			if _, err := y.a.WorkOrders.CreateBilling(y.ctx, y.p, y.m, v.Row.ID, workorder.BillingInput{Kind: workorder.BillingFinal, DueOn: y.today.AddDays(30)}, false); err != nil {
				return fmt.Errorf("seed final billing: %w", err)
			}
		}
	}
	return nil
}

// lifts books the lifting schedule of the demo orders: the haul-outs of the started orders on their start day
// (booked and done then, through the past App), and from today the launches of the finished orders, the haul-outs
// of the orders starting this week, crane lifts and one request to plan. Days the maintenance seed holds a machine
// out of service, taken slots and boats over the capacity simply get no booking.
func (y *seedYard) lifts() error {
	if y.lift == "" {
		return nil
	}
	op := func(i int) string {
		if len(y.operators) == 0 {
			return ""
		}
		return y.operators[i%len(y.operators)]
	}
	ip := func(v int) *int { return &v }
	skip := func(err error) error {
		if errors.Is(err, lifting.ErrOutOfService) || errors.Is(err, lifting.ErrSlotTaken) || errors.Is(err, lifting.ErrOperatorBusy) ||
			errors.Is(err, lifting.ErrCapacityExceeded) {
			return nil
		}
		return err
	}
	// Past haul-outs, done.
	for i, o := range y.orders {
		if o.status != store.WorkOrderInProgress && o.status != store.WorkOrderOnHold {
			continue
		}
		y.fake.Set(y.at(o.start, 7))
		b, err := y.past.Lifting.CreateBooking(y.ctx, y.p, y.m, lifting.BookingInput{
			EquipmentID: y.lift, Date: o.start, FromSlot: ip(32 + 4*i), ToSlot: ip(36 + 4*i), Purpose: store.LiftPurposeHaulOut,
			WorkOrderID: o.id, OperatorEmployeeID: op(i), Note: "Alaggio per lavori",
		})
		if err := skip(err); err != nil {
			return fmt.Errorf("seed past haul-out: %w", err)
		}
		if b == nil {
			continue
		}
		y.fake.Set(y.at(o.start, 12))
		if _, err := y.past.Lifting.CompleteBooking(y.ctx, y.p, y.m, b.ID, int64(b.Version)); err != nil {
			return fmt.Errorf("seed haul-out done: %w", err)
		}
	}
	// From today: launches of the finished orders and on the delivery day, haul-outs of the next orders.
	var bookings []lifting.BookingInput
	for i, o := range y.orders {
		switch o.status {
		case store.WorkOrderCompleted:
			bookings = append(bookings, lifting.BookingInput{
				EquipmentID: y.lift, Date: y.today, FromSlot: ip(60), ToSlot: ip(64),
				Purpose: store.LiftPurposeLaunch, WorkOrderID: o.id, OperatorEmployeeID: op(i), Note: "Varo a fine lavori",
			})
		case store.WorkOrderPlanned:
			bookings = append(bookings, lifting.BookingInput{
				EquipmentID: y.lift, Date: o.start, FromSlot: ip(32), ToSlot: ip(36),
				Purpose: store.LiftPurposeHaulOut, WorkOrderID: o.id, OperatorEmployeeID: op(i), Note: "Alaggio per lavori",
			})
		case store.WorkOrderInProgress:
			if d := o.delivery; !d.Before(y.today) && d.Before(y.today.AddDays(8)) {
				bookings = append(bookings, lifting.BookingInput{
					EquipmentID: y.lift, Date: d, FromSlot: ip(64), ToSlot: ip(68),
					Purpose: store.LiftPurposeLaunch, WorkOrderID: o.id, OperatorEmployeeID: op(i), Note: "Varo a fine lavori",
				})
			}
		}
	}
	others := y.otherVessels(3)
	if len(others) > 0 {
		bookings = append(bookings,
			lifting.BookingInput{
				EquipmentID: y.lift, Date: y.today, FromSlot: ip(40), ToSlot: ip(44), Purpose: store.LiftPurposeHaulOut,
				VesselID: others[0], OperatorEmployeeID: op(0), Note: "Alaggio per controllo elica",
			},
			lifting.BookingInput{
				EquipmentID: y.lift, Date: y.today.AddDays(1), FromSlot: ip(52), ToSlot: ip(56), Purpose: store.LiftPurposeMove,
				VesselID: others[0], OperatorEmployeeID: op(1), Note: "Spostamento al capannone",
			},
			lifting.BookingInput{Purpose: store.LiftPurposeHaulOut, VesselID: others[len(others)-1], RequestOnly: true, Note: "Da pianificare"},
		)
	}
	if y.crane != "" {
		for k, c := range []struct {
			day, from int
			note      string
		}{{0, 36, "Sbarco albero"}, {1, 44, "Imbarco motore revisionato"}, {2, 40, "Sbarco albero"}, {4, 36, "Reimbarco albero"}} {
			in := lifting.BookingInput{
				EquipmentID: y.crane, Date: y.today.AddDays(c.day), FromSlot: ip(c.from), ToSlot: ip(c.from + 4),
				Purpose: store.LiftPurposeOther, OperatorEmployeeID: op(k + 1), Note: c.note,
			}
			if k < len(others) {
				in.VesselID = others[k]
			}
			bookings = append(bookings, in)
		}
	}
	for i, b := range bookings {
		_, err := y.a.Lifting.CreateBooking(y.ctx, y.p, y.m, b)
		if err := skip(err); err != nil {
			return fmt.Errorf("seed lifting booking %d: %w", i, err)
		}
	}
	return nil
}

// otherVessels returns up to n light registry boats without a demo order (by name).
func (y *seedYard) otherVessels(n int) []string {
	busy := map[string]bool{}
	for _, o := range y.orders {
		busy[o.vessel] = true
	}
	var vessels []store.Vessel
	if err := y.a.DB.NewSelect().Model(&vessels).Where("marina_id = ?", y.scope.ID()).Where("archived_at IS NULL").
		Where("draft_cm > 0").Where("displacement_kg IS NULL OR displacement_kg <= ?", 20_000).OrderExpr("name, id").Limit(40).Scan(y.ctx); err != nil {
		return nil
	}
	var out []string
	for _, v := range vessels {
		if !busy[v.ID] && len(out) < n {
			out = append(out, v.ID)
		}
	}
	return out
}

// seedStep rounds hours (milli) down to the manual entry step.
func seedStep(h int64) int64 { return h / workorder.ManualStepMilli * workorder.ManualStepMilli }
