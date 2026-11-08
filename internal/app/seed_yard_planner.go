package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/lifting"
	"github.com/coste360/porto/internal/domain/yardplan"
	"github.com/coste360/porto/internal/store"
)

// seedYardPlanner adds the M10 demo of PLANNER (m10-plan §16.2): four employees with calendars and qualifications
// (the yard worker and the yard manager demo users are linked to two of them for the phone), the operators of the
// travel lift and the crane of the M9 equipment seed, and three lifting bookings of tomorrow plus a request.
// Everything goes through the services as the demo owner; a no-op when the marina already has employees. The
// assignments come after the work orders: seedYardPlannerAssignments.
func (a *App) seedYardPlanner(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("employees").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	var quals []store.YardQualification
	if err := a.DB.NewSelect().Model(&quals).Where("marina_id = ?", scope.ID()).OrderExpr("sort_order, code").Scan(ctx); err != nil {
		return err
	}
	byCode := map[string]string{}
	var liftQuals []string
	for _, q := range quals {
		byCode[q.Code] = q.ID
		if q.LiftOperator {
			liftQuals = append(liftQuals, q.ID)
		}
	}
	pick := func(codes ...string) []string {
		var out []string
		for _, c := range codes {
			if id := byCode[c]; id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	userOf := func(role auth.Role) string {
		u, err := store.UserByEmail(ctx, a.DB.DB, string(role)+"@"+DemoEmailDomain)
		if err != nil {
			return ""
		}
		return u.ID
	}
	type emp struct {
		code, first, last, dept, user string
		cost                          int64
		quals                         []string
		week                          []yardplan.CalendarDay
	}
	partTime := []yardplan.CalendarDay{{Weekday: 1, Minutes: 360}, {Weekday: 2, Minutes: 360}, {Weekday: 3, Minutes: 360}, {Weekday: 4, Minutes: 360}, {Weekday: 5, Minutes: 360}, {Weekday: 6, Minutes: 240}}
	emps := []emp{
		{code: "Y01", first: "Marco", last: "Esposito", dept: "Piazzale", user: userOf(auth.RoleYard), cost: 2200, quals: append(pick("hand"), liftQuals...)},
		{code: "Y02", first: "Luca", last: "Ferraro", dept: "Verniciatura", cost: 2600, quals: pick("painter", "hand")},
		{code: "Y03", first: "Giulia", last: "Rinaldi", dept: "Verniciatura", cost: 2600, quals: pick("painter"), week: partTime},
		{code: "Y04", first: "Andrea", last: "Galli", dept: "Piazzale", user: userOf(auth.RoleYardMgr), cost: 3000, quals: append(pick("hand"), liftQuals...)},
	}
	var operators []string
	for _, e := range emps {
		cost := e.cost
		v, err := a.YardPlan.CreateEmployee(ctx, p, m, yardplan.EmployeeInput{
			Code: e.code, FirstName: e.first, LastName: e.last, Department: e.dept, UserID: e.user, HourlyCostCents: &cost,
			QualificationIDs: e.quals, Calendar: e.week,
		})
		if err != nil {
			return fmt.Errorf("employee %s: %w", e.code, err)
		}
		if len(liftQuals) > 0 && (e.code == "Y01" || e.code == "Y04") {
			operators = append(operators, v.Row.ID)
		}
	}
	today := calendar.In(a.Clock.Now(), seedPlannerLocation(ctx, a, scope))
	if _, err := a.YardPlan.CreateAbsence(ctx, p, m, mustEmployeeID(ctx, a, scope, "Y03"), yardplan.AbsenceInput{
		From: today.AddDays(7), To: today.AddDays(11), Kind: store.AbsenceVacation, Note: "Ferie",
	}); err != nil {
		return fmt.Errorf("absence: %w", err)
	}

	day, err := a.Lifting.ListDay(ctx, p, m, lifting.DayFilter{Date: today.AddDays(1)})
	if err != nil {
		return err
	}
	var lift, crane string
	for _, e := range day.Equipment {
		switch e.Row.Kind {
		case "travel_lift":
			if lift == "" {
				lift = e.Row.ID
			}
		case "crane":
			if crane == "" {
				crane = e.Row.ID
			}
		}
	}
	if len(operators) > 0 {
		for _, eq := range []string{lift, crane} {
			if eq == "" {
				continue
			}
			if _, err := a.Lifting.PutOperators(ctx, p, m, eq, operators); err != nil {
				return fmt.Errorf("operators: %w", err)
			}
		}
	}
	var vessels []store.Vessel
	if err := a.DB.NewSelect().Model(&vessels).Where("marina_id = ?", scope.ID()).
		Where("displacement_kg IS NULL OR displacement_kg <= ?", 70_000).OrderExpr("name, id").Limit(2).Scan(ctx); err != nil {
		return err
	}
	if lift == "" || len(vessels) == 0 {
		return nil
	}
	op := func(i int) string {
		if len(operators) == 0 {
			return ""
		}
		return operators[i%len(operators)]
	}
	ip := func(v int) *int { return &v }
	tomorrow := today.AddDays(1)
	bookings := []lifting.BookingInput{
		{EquipmentID: lift, Date: tomorrow, FromSlot: ip(32), ToSlot: ip(34), Purpose: store.LiftPurposeHaulOut, VesselID: vessels[0].ID, OperatorEmployeeID: op(0), Note: "Alaggio per carena"},
		{EquipmentID: lift, Date: tomorrow, FromSlot: ip(40), ToSlot: ip(42), Purpose: store.LiftPurposeLaunch, VesselID: vessels[len(vessels)-1].ID, OperatorEmployeeID: op(1)},
		{Purpose: store.LiftPurposeHaulOut, VesselID: vessels[len(vessels)-1].ID, RequestOnly: true, Note: "Da pianificare"},
	}
	if crane != "" {
		bookings = append(bookings, lifting.BookingInput{EquipmentID: crane, Date: tomorrow, FromSlot: ip(36), ToSlot: ip(38), Purpose: store.LiftPurposeOther, OperatorEmployeeID: op(0), Note: "Sbarco albero"})
	}
	for i, b := range bookings {
		if _, err := a.Lifting.CreateBooking(ctx, p, m, b); err != nil {
			// A machine the maintenance seed holds out of service tomorrow simply gets no demo booking.
			if errors.Is(err, lifting.ErrOutOfService) {
				continue
			}
			return fmt.Errorf("booking %d: %w", i, err)
		}
	}
	return nil
}

// seedYardPlannerAssignments plans the open tasks of the demo work orders with the auto-plan proposal, confirmed
// as is (m10-plan §6.4). It runs after seedYardOrders; a no-op when there are assignments already or no open task.
func (a *App) seedYardPlannerAssignments(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("task_assignments").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	prop, err := a.YardPlan.ProposeYardPlan(ctx, p, res.MarinaID, yardplan.ProposalInput{HorizonDays: 21})
	if err != nil || len(prop.Changes) == 0 {
		return err
	}
	if _, err := a.YardPlan.PutAssignments(ctx, p, res.MarinaID, yardplan.BatchInput{Changes: prop.Changes}); err != nil {
		return fmt.Errorf("assignments: %w", err)
	}
	return nil
}

func mustEmployeeID(ctx context.Context, a *App, scope store.MarinaScope, code string) string {
	var id string
	_ = a.DB.NewSelect().TableExpr("employees").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", scope.ID()).
		Where("code = ?", code).Scan(ctx, &id)
	return id
}

func seedPlannerLocation(ctx context.Context, a *App, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
