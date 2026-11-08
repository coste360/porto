package maintenance

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/store"
)

// Plan limits (m9-plan §8.2).
const (
	maxChecklist   = 40
	maxTargets     = 500
	maxLeadDays    = 180
	maxPlanNights  = 60
	maxNameRunes   = 100
	maxLabelRunes  = 200
	maxChecklistID = 40
)

var interventions = []string{"underwater_inspection", "pennant_replacement", "chain_check", "rcd_test", "general", "custom"}

// PlanFields lists the keys of PlanInput that an update may set (the JSON names of MaintenancePlanPatch).
var PlanFields = []string{
	"name", "intervention", "intervention_label", "targets", "periodicity", "lead_days", "assignee_user_id",
	"supplier_name", "checklist", "requires_free", "nights", "active",
}

// periodicityOf returns the due periodicity of a plan row.
func periodicityOf(p *store.MaintenancePlan) due.Periodicity {
	out := due.Periodicity{Kind: p.PeriodKind, MonthDay: p.PeriodMonthDay}
	if p.PeriodEvery != nil {
		out.Every = *p.PeriodEvery
	}
	if !p.PeriodStartsOn.IsZero() {
		d := p.PeriodStartsOn
		out.StartsOn = &d
	}
	return out
}

// ChecklistOf decodes a checklist JSON column.
func ChecklistOf(j store.JSON) []ChecklistEntry {
	var raw []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	_ = json.Unmarshal(j, &raw)
	out := make([]ChecklistEntry, 0, len(raw))
	for _, r := range raw {
		out = append(out, ChecklistEntry{Key: r.Key, Label: r.Label})
	}
	return out
}

func checklistJSON(list []ChecklistEntry) store.JSON {
	type entry struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	out := make([]entry, 0, len(list))
	for _, c := range list {
		out = append(out, entry(c))
	}
	return store.MustJSON(out)
}

// ResultsOf decodes a checklist results JSON column.
func ResultsOf(j store.JSON) []ChecklistResult {
	var raw []struct {
		Key  string `json:"key"`
		Done bool   `json:"done"`
		Note string `json:"note,omitempty"`
	}
	_ = json.Unmarshal(j, &raw)
	out := make([]ChecklistResult, 0, len(raw))
	for _, r := range raw {
		out = append(out, ChecklistResult{Key: r.Key, Done: r.Done, Note: r.Note})
	}
	return out
}

func resultsJSON(list []ChecklistResult) store.JSON {
	type entry struct {
		Key  string `json:"key"`
		Done bool   `json:"done"`
		Note string `json:"note,omitempty"`
	}
	out := make([]entry, 0, len(list))
	for _, c := range list {
		out = append(out, entry(c))
	}
	return store.MustJSON(out)
}

// validateChecklist trims and checks a checklist (≤ 40 lines, unique keys).
func validateChecklist(list []ChecklistEntry, v *auth.ValidationError) []ChecklistEntry {
	if len(list) > maxChecklist {
		v.Add("checklist", "too_many")
		return list
	}
	seen := map[string]bool{}
	out := make([]ChecklistEntry, 0, len(list))
	for _, c := range list {
		k, ok1 := cleanText(c.Key, maxChecklistID)
		l, ok2 := cleanText(c.Label, maxLabelRunes)
		if !ok1 || !ok2 || k == "" || l == "" || seen[k] {
			v.Add("checklist", "invalid")
			continue
		}
		seen[k] = true
		out = append(out, ChecklistEntry{Key: k, Label: l})
	}
	return out
}

// checkElements verifies that every element exists in the marina; it returns their infos keyed by type:id.
func checkElements(ctx context.Context, db bun.IDB, scope store.MarinaScope, els []Element) (map[string]store.ElementInfo, error) {
	refs := map[string][]string{}
	for _, e := range els {
		if !contains(elementTypes, e.Type) {
			return nil, ErrElementInvalid
		}
		refs[e.Type] = append(refs[e.Type], e.ID)
	}
	infos, err := store.ElementInfos(ctx, db, scope, refs)
	if err != nil {
		return nil, err
	}
	for _, e := range els {
		if _, ok := infos[e.Type+":"+e.ID]; !ok {
			return nil, ErrElementInvalid
		}
	}
	return infos, nil
}

// applyPlan validates in and writes it on row (fields: nil = every field).
func (s *Service) applyPlan(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MaintenancePlan, in PlanInput,
	fields map[string]bool,
) ([]store.MaintenancePlanTarget, bool, error) {
	set := func(k string) bool { return fields == nil || fields[k] }
	v := &auth.ValidationError{}
	var ok bool
	if set("name") {
		if row.Name, ok = cleanText(in.Name, maxNameRunes); !ok || row.Name == "" {
			v.Add("name", "invalid")
		}
	}
	if set("intervention") {
		row.Intervention = in.Intervention
	}
	if set("intervention_label") || set("intervention") {
		if set("intervention_label") {
			row.InterventionLabel, ok = cleanText(in.InterventionLabel, maxNameRunes)
			if !ok {
				v.Add("intervention_label", "invalid")
			}
		}
	}
	if !contains(interventions, row.Intervention) {
		v.Add("intervention", "invalid")
	}
	if row.Intervention == "custom" && row.InterventionLabel == "" {
		v.Add("intervention_label", "required")
	}
	if set("periodicity") {
		p := in.Periodicity
		row.PeriodKind, row.PeriodMonthDay, row.PeriodMeterID = p.Kind, "", ""
		row.PeriodEvery, row.PeriodStartsOn = nil, calendar.Date{}
		switch p.Kind {
		case due.KindMonths, due.KindCycles:
			every := p.Every
			row.PeriodEvery = &every
		case due.KindFixedDate:
			row.PeriodMonthDay = p.MonthDay
		}
		if p.StartsOn != nil {
			row.PeriodStartsOn = *p.StartsOn
		}
		if p.Kind == due.KindCycles {
			row.PeriodMeterID = in.MeterID
		}
		if !p.Valid() {
			v.Add("periodicity", "invalid")
		}
		if p.Kind == due.KindCycles {
			m, err := store.GetUtilityMeter(ctx, db, scope, in.MeterID)
			switch {
			case err != nil && !isNotFound(err):
				return nil, false, err
			case err != nil || (m.Kind != store.MeterHours && m.Kind != store.MeterCycles):
				v.Add("periodicity.meter_id", "invalid")
			}
		}
	}
	if set("lead_days") {
		row.LeadDays = in.LeadDays
	}
	if row.LeadDays < 0 || row.LeadDays > maxLeadDays {
		v.Add("lead_days", "invalid")
	}
	if set("assignee_user_id") {
		row.AssigneeUserID = in.AssigneeUserID
		if row.AssigneeUserID != "" && !store.ValidID(row.AssigneeUserID) {
			v.Add("assignee_user_id", "invalid")
		}
	}
	if set("supplier_name") {
		if row.SupplierName, ok = cleanText(in.SupplierName, maxNameRunes); !ok {
			v.Add("supplier_name", "invalid")
		}
	}
	if set("checklist") {
		row.Checklist = checklistJSON(validateChecklist(in.Checklist, v))
	}
	if set("requires_free") {
		row.RequiresFree = in.RequiresFree
	}
	if set("nights") {
		row.Nights = in.Nights
		if row.Nights == 0 {
			row.Nights = 1
		}
	}
	if row.Nights < 1 || row.Nights > maxPlanNights {
		v.Add("nights", "invalid")
	}
	if set("active") && in.Active != nil {
		row.Active = *in.Active
	}
	var targets []store.MaintenancePlanTarget
	targetsSet := set("targets")
	if targetsSet {
		if len(in.Targets) == 0 || len(in.Targets) > maxTargets {
			v.Add("targets", "count")
		}
		seen := map[string]bool{}
		for _, t := range in.Targets {
			k := t.Type + ":" + t.ID
			if seen[k] {
				continue
			}
			seen[k] = true
			targets = append(targets, store.MaintenancePlanTarget{ElementType: t.Type, ElementID: t.ID})
		}
	}
	if err := v.Err(); err != nil {
		return nil, false, err
	}
	if targetsSet {
		els := make([]Element, 0, len(targets))
		for _, t := range targets {
			els = append(els, Element{Type: t.ElementType, ID: t.ElementID})
		}
		if _, err := checkElements(ctx, db, scope, els); err != nil {
			return nil, false, err
		}
	}
	return targets, targetsSet, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

func (s *Service) planView(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.MaintenancePlan) ([]PlanView, error) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	ts, err := store.MaintenancePlanTargets(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	by := map[string][]store.MaintenancePlanTarget{}
	for _, t := range ts {
		by[t.PlanID] = append(by[t.PlanID], t)
	}
	out := make([]PlanView, 0, len(rows))
	for _, r := range rows {
		t := by[r.ID]
		if t == nil {
			t = []store.MaintenancePlanTarget{}
		}
		out = append(out, PlanView{Row: r, Targets: t})
	}
	return out, nil
}

// ListPlans lists the plans (maintenance.manage).
func (s *Service) ListPlans(ctx context.Context, p *auth.Principal, marinaID string, active *bool, elementType, cursor string,
	limit int,
) ([]PlanView, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListMaintenancePlans(ctx, s.d.DB.DB, scope, store.MaintenancePlanFilter{Active: active, ElementType: elementType}, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.planView(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// CreatePlan creates a plan (maintenance.manage). Without a start, months plans count from the creation day.
func (s *Service) CreatePlan(ctx context.Context, p *auth.Principal, marinaID string, in PlanInput) (*PlanView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.MaintenancePlan{ID: store.NewID(), Active: true, CreatedAt: now, UpdatedAt: now}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if in.Periodicity.StartsOn == nil && in.Periodicity.Kind != due.KindCycles {
			t := s.today(ctx, tx, scope)
			in.Periodicity.StartsOn = &t
		}
		targets, _, err := s.applyPlan(ctx, tx, scope, row, in, nil)
		if err != nil {
			return err
		}
		if err := store.InsertMaintenancePlan(ctx, tx, scope, row); err != nil {
			return planWriteErr(err)
		}
		if err := store.ReplaceMaintenancePlanTargets(ctx, tx, scope, row.ID, targets); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "maintenance_plan.create", "maintenance_plan", row.ID,
			map[string]any{"intervention": row.Intervention, "period_kind": row.PeriodKind, "targets": len(targets)}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "plan", row.ID, "", "", "")
	})
	if err != nil {
		return nil, err
	}
	return s.GetPlan(ctx, p, marinaID, row.ID)
}

func planWriteErr(err error) error {
	if store.IsForeignKeyViolation(err) {
		return ErrElementInvalid
	}
	return versionErr(err)
}

// GetPlan reads a plan (maintenance.manage).
func (s *Service) GetPlan(ctx context.Context, p *auth.Principal, marinaID, id string) (*PlanView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	row, err := store.GetMaintenancePlan(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.planView(ctx, s.d.DB.DB, scope, []store.MaintenancePlan{*row})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// UpdatePlan changes a plan; only the keys in fields are written (maintenance.manage, If-Match). Tasks already
// generated keep their due date; the next ones follow the new periodicity.
func (s *Service) UpdatePlan(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, in PlanInput,
	fields map[string]bool,
) (*PlanView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetMaintenancePlan(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(row.Version) != version {
			return ErrVersionMismatch
		}
		if fields["periodicity"] && in.Periodicity.StartsOn == nil && !row.PeriodStartsOn.IsZero() && in.Periodicity.Kind != due.KindCycles {
			d := row.PeriodStartsOn
			in.Periodicity.StartsOn = &d
		}
		if fields["periodicity"] && in.Periodicity.Kind == due.KindCycles && in.MeterID == "" {
			in.MeterID = row.PeriodMeterID
		}
		targets, targetsSet, err := s.applyPlan(ctx, tx, scope, row, in, fields)
		if err != nil {
			return err
		}
		if err := store.UpdateMaintenancePlan(ctx, tx, scope, row, int(version), s.now()); err != nil {
			return planWriteErr(err)
		}
		if targetsSet {
			if err := store.ReplaceMaintenancePlanTargets(ctx, tx, scope, row.ID, targets); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(fields))
		for _, k := range PlanFields {
			if fields[k] {
				keys = append(keys, k)
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "maintenance_plan.update", "maintenance_plan", row.ID, map[string]any{"fields": keys}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "plan", row.ID, "", "", "")
	})
	if err != nil {
		return nil, err
	}
	return s.GetPlan(ctx, p, marinaID, id)
}
