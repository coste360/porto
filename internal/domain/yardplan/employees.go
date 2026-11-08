package yardplan

import (
	"context"
	"errors"
	"regexp"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

var codeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)

var absenceKinds = map[string]bool{
	store.AbsenceVacation: true, store.AbsenceSick: true, store.AbsenceLeave: true, store.AbsenceOther: true,
}

// MaxAbsenceDays is the longest absence of one row.
const MaxAbsenceDays = 366

func defaultWeek() [7]int {
	return [7]int{DefaultDayMinutes, DefaultDayMinutes, DefaultDayMinutes, DefaultDayMinutes, DefaultDayMinutes, 0, 0}
}

func weekOf(days []CalendarDay, v *auth.ValidationError) [7]int {
	var w [7]int
	seen := map[int]bool{}
	for _, d := range days {
		if d.Weekday < 1 || d.Weekday > 7 || seen[d.Weekday] {
			v.Add("calendar", "invalid_weekday")
			continue
		}
		if d.Minutes < 0 || d.Minutes > MaxCalendarMinutes {
			v.Add("calendar", "invalid_minutes")
			continue
		}
		seen[d.Weekday] = true
		w[d.Weekday-1] = d.Minutes
	}
	return w
}

func validateEmployee(e *store.Employee, v *auth.ValidationError) {
	var ok bool
	if !codeRe.MatchString(e.Code) {
		v.Add("code", "invalid")
	}
	if e.FirstName, ok = cleanText(e.FirstName, 60); !ok || e.FirstName == "" {
		v.Add("first_name", "invalid")
	}
	if e.LastName, ok = cleanText(e.LastName, 60); !ok || e.LastName == "" {
		v.Add("last_name", "invalid")
	}
	if e.Department, ok = cleanText(e.Department, 40); !ok {
		v.Add("department", "invalid")
	}
	if e.HourlyCostCents < 0 {
		v.Add("hourly_cost_cents", "invalid")
	}
}

// checkRefs validates the user link and the qualifications against the marina.
func checkRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, userID string, quals []string, v *auth.ValidationError) error {
	if userID != "" {
		ok, err := store.UserExists(ctx, db, scope, userID)
		if err != nil {
			return err
		}
		if !ok {
			v.Add("user_id", "not_found")
		}
	}
	if len(quals) > 0 {
		ok, err := store.QualificationsExist(ctx, db, scope, quals)
		if err != nil {
			return err
		}
		if !ok {
			v.Add("qualification_ids", "not_found")
		}
	}
	return nil
}

func (s *Service) employeeViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Employee) ([]EmployeeView, error) {
	out := make([]EmployeeView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	quals, err := store.EmployeeQualifications(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	cals, err := store.EmployeeCalendars(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		q := quals[r.ID]
		if q == nil {
			q = []string{}
		}
		out = append(out, EmployeeView{Row: r, QualificationIDs: q, Week: cals[r.ID]})
	}
	return out, nil
}

func (s *Service) employeeView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*EmployeeView, error) {
	e, err := store.GetEmployee(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.employeeViews(ctx, db, scope, []store.Employee{*e})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func uniqueErr(err error) error {
	if store.IsUniqueViolation(err) {
		return ErrCodeTaken
	}
	return err
}

// ListEmployees lists the employees (yard.read or yard.manage).
func (s *Service) ListEmployees(ctx context.Context, p *auth.Principal, marinaID string, f store.EmployeeFilter, cursor string, limit int) ([]EmployeeView, string, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListEmployees(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.employeeViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// GetEmployee reads an employee (yard.read or yard.manage).
func (s *Service) GetEmployee(ctx context.Context, p *auth.Principal, marinaID, id string) (*EmployeeView, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.employeeView(ctx, s.d.DB.DB, scope, id)
}

// CreateEmployee creates an employee with qualifications and calendar (yard.manage).
func (s *Service) CreateEmployee(ctx context.Context, p *auth.Principal, marinaID string, in EmployeeInput) (*EmployeeView, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	e := &store.Employee{
		ID: store.NewID(), Code: in.Code, FirstName: in.FirstName, LastName: in.LastName, UserID: in.UserID,
		Department: in.Department, Active: in.Active == nil || *in.Active, CreatedAt: now, UpdatedAt: now,
	}
	if in.HourlyCostCents != nil {
		e.HourlyCostCents = *in.HourlyCostCents
	}
	v := &auth.ValidationError{}
	validateEmployee(e, v)
	week := defaultWeek()
	if in.Calendar != nil {
		week = weekOf(in.Calendar, v)
	}
	if err := checkRefs(ctx, s.d.DB.DB, scope, e.UserID, in.QualificationIDs, v); err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertEmployee(ctx, tx, scope, e); err != nil {
			return uniqueErr(err)
		}
		if err := store.ReplaceEmployeeQualifications(ctx, tx, scope, e.ID, in.QualificationIDs); err != nil {
			return err
		}
		if err := store.ReplaceEmployeeCalendar(ctx, tx, scope, e.ID, week); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "employee.create", "employee", e.ID, map[string]any{
			"code": e.Code, "active": e.Active, "qualifications": len(in.QualificationIDs),
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "employee", e.ID, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.employeeView(ctx, s.d.DB.DB, scope, e.ID)
}

// UpdateEmployee changes an employee (yard.manage, If-Match).
func (s *Service) UpdateEmployee(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch EmployeePatch) (*EmployeeView, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		e, err := store.LockEmployee(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(e.Version) != version {
			return ErrVersionMismatch
		}
		if patch.Code != nil {
			e.Code = *patch.Code
		}
		if patch.FirstName != nil {
			e.FirstName = *patch.FirstName
		}
		if patch.LastName != nil {
			e.LastName = *patch.LastName
		}
		if patch.ClearUser {
			e.UserID = ""
		} else if patch.UserID != nil {
			e.UserID = *patch.UserID
		}
		if patch.Department != nil {
			e.Department = *patch.Department
		}
		if patch.HourlyCostCents != nil {
			e.HourlyCostCents = *patch.HourlyCostCents
		}
		if patch.Active != nil {
			e.Active = *patch.Active
		}
		v := &auth.ValidationError{}
		validateEmployee(e, v)
		var quals []string
		if patch.QualificationIDs != nil {
			quals = *patch.QualificationIDs
		}
		if err := checkRefs(ctx, tx, scope, e.UserID, quals, v); err != nil {
			return err
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.UpdateEmployee(ctx, tx, scope, e, int(version), s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) || errors.Is(err, store.ErrNotFound) {
				return versionErr(err)
			}
			return uniqueErr(err)
		}
		if patch.QualificationIDs != nil {
			if err := store.ReplaceEmployeeQualifications(ctx, tx, scope, e.ID, quals); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "employee.update", "employee", e.ID, map[string]any{
			"code": e.Code, "active": e.Active, "qualifications_changed": patch.QualificationIDs != nil,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "employee", e.ID, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.employeeView(ctx, s.d.DB.DB, scope, id)
}

// PutEmployeeCalendar replaces the weekly calendar (yard.manage, If-Match on the employee). Days left out are 0.
func (s *Service) PutEmployeeCalendar(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, days []CalendarDay) (*EmployeeView, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		e, err := store.LockEmployee(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(e.Version) != version {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		week := weekOf(days, v)
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.ReplaceEmployeeCalendar(ctx, tx, scope, id, week); err != nil {
			return err
		}
		if err := store.TouchEmployee(ctx, tx, scope, id, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "employee.calendar", "employee", id, map[string]any{"minutes": week}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "calendar", id, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.employeeView(ctx, s.d.DB.DB, scope, id)
}

// ListAbsences lists the absences of an employee overlapping [from, to] (yard.read or yard.manage).
func (s *Service) ListAbsences(ctx context.Context, p *auth.Principal, marinaID, employeeID string, from, to calendar.Date) ([]store.EmployeeAbsence, error) {
	scope, err := s.read(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetEmployee(ctx, s.d.DB.DB, scope, employeeID); err != nil {
		return nil, notFound(err)
	}
	return store.ListAbsences(ctx, s.d.DB.DB, scope, []string{employeeID}, from, to)
}

// CreateAbsence adds an absence (yard.manage).
func (s *Service) CreateAbsence(ctx context.Context, p *auth.Principal, marinaID, employeeID string, in AbsenceInput) (*store.EmployeeAbsence, error) {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetEmployee(ctx, s.d.DB.DB, scope, employeeID); err != nil {
		return nil, notFound(err)
	}
	v := &auth.ValidationError{}
	if in.From.IsZero() {
		v.Add("from_on", "required")
	}
	if in.To.IsZero() {
		v.Add("to_on", "required")
	}
	if !in.From.IsZero() && !in.To.IsZero() {
		if in.To.Before(in.From) {
			v.Add("to_on", "before_from")
		} else if in.From.DaysUntil(in.To)+1 > MaxAbsenceDays {
			v.Add("to_on", "range_too_long")
		}
	}
	if !absenceKinds[in.Kind] {
		v.Add("kind", "invalid")
	}
	if in.MinutesPerDay != nil && (*in.MinutesPerDay < 1 || *in.MinutesPerDay > MaxCalendarMinutes) {
		v.Add("minutes_per_day", "invalid")
	}
	note, ok := cleanText(in.Note, 200)
	if !ok {
		v.Add("note", "too_long")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	a := &store.EmployeeAbsence{
		ID: store.NewID(), EmployeeID: employeeID, FromOn: in.From, ToOn: in.To, Kind: in.Kind, MinutesPerDay: in.MinutesPerDay,
		Note: note, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertAbsence(ctx, tx, scope, a); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "employee.absence_create", "employee", employeeID, map[string]any{
			"absence_id": a.ID, "from": a.FromOn, "to": a.ToOn, "kind": a.Kind,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "absence", employeeID, []calendar.Date{a.FromOn, a.ToOn})
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteAbsence deletes an absence (yard.manage).
func (s *Service) DeleteAbsence(ctx context.Context, p *auth.Principal, marinaID, employeeID, id string) error {
	scope, err := s.manage(ctx, p, marinaID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		a, err := store.GetAbsence(ctx, tx, scope, employeeID, id)
		if err != nil {
			return notFound(err)
		}
		if err := store.DeleteAbsence(ctx, tx, scope, id); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "employee.absence_delete", "employee", employeeID, map[string]any{
			"absence_id": a.ID, "from": a.FromOn, "to": a.ToOn, "kind": a.Kind,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "absence", employeeID, []calendar.Date{a.FromOn, a.ToOn})
	})
}
