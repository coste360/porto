package payment

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Office worklist (spec 28.3; m5-plan §4.5, §16 D25): items computed from their sources, merged with
// work_item_states (assignee, status). Each item is visible only with the permission of its source.

// Worklist kinds.
const (
	WorkReminderApproval      = "reminder_approval"
	WorkChargesToExport       = "charges_to_export"
	WorkPaymentFailed         = "payment_failed"
	WorkLatePayment           = "late_payment"
	WorkOverpayment           = "overpayment"
	WorkSEPAFailed            = "sepa_failed"
	WorkBillingItemMissing    = "billing_item_missing"
	WorkBillingTargetMismatch = "billing_target_mismatch"
	WorkArrearsPaid           = "arrears_paid"
	WorkDeadline              = "deadline"
	WorkDispute               = "dispute"
)

// computedKinds maps the source types with a table of their own to their kind and permission.
var computedKinds = map[string]struct {
	kind string
	perm auth.Permission
}{
	"reminder_item":  {WorkReminderApproval, auth.PermChargesManage},
	"charge":         {WorkChargesToExport, auth.PermChargesExport},
	"payment_intent": {WorkPaymentFailed, auth.PermChargesManage},
	"sepa_mandate":   {WorkSEPAFailed, auth.PermChargesManage},
	"deadline":       {WorkDeadline, auth.PermContractsRead},
}

// storedKinds are recorded with store.RaiseWorkItem (source_type = kind) by the services that detect them.
var storedKinds = []string{
	WorkLatePayment, WorkOverpayment, WorkBillingItemMissing, WorkBillingTargetMismatch, WorkArrearsPaid, WorkDispute,
	WorkPaymentFailed,
}

// WorkItem is one worklist item.
type WorkItem struct {
	Kind, SourceType, SourceID string
	Status                     string
	AssigneeUserID             string
	Note                       string
	CustomerID, CustomerName   string
	Amount                     money.Cents
	DueOn                      calendar.Date
	CreatedAt                  time.Time
}

// WorkFilter selects worklist items ("" status = open and in progress).
type WorkFilter struct {
	Kind, Status, AssigneeUserID string
}

// kindOf returns the kind and permission of a source type.
func kindOf(sourceType string) (string, auth.Permission, bool) {
	if c, ok := computedKinds[sourceType]; ok {
		return c.kind, c.perm, true
	}
	if slices.Contains(storedKinds, sourceType) {
		return sourceType, auth.PermChargesManage, true
	}
	return "", "", false
}

func (s *Service) worklist(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal) ([]WorkItem, error) {
	states, err := store.WorkItemStates(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	byKey := map[string]store.WorkItemState{}
	for _, st := range states {
		byKey[st.SourceType+"/"+st.SourceID] = st
	}
	var sources []store.WorkSource
	add := func(perm auth.Permission, f func() ([]store.WorkSource, error)) error {
		if !can(p, scope, perm) {
			return nil
		}
		rows, err := f()
		if err != nil {
			return err
		}
		sources = append(sources, rows...)
		return nil
	}
	today := s.today(ctx, db, scope)
	if err := add(auth.PermChargesManage, func() ([]store.WorkSource, error) { return store.ReminderApprovalSources(ctx, db, scope) }); err != nil {
		return nil, err
	}
	if err := add(auth.PermChargesExport, func() ([]store.WorkSource, error) {
		days := 30
		if bs, err := store.LedgerBillingSettings(ctx, db, scope); err == nil && bs.ExportReminderDays > 0 {
			days = bs.ExportReminderDays
		}
		return store.ChargesToExportSources(ctx, db, scope, today.AddDays(-days))
	}); err != nil {
		return nil, err
	}
	if err := add(auth.PermChargesManage, func() ([]store.WorkSource, error) { return store.FailedIntentSources(ctx, db, scope) }); err != nil {
		return nil, err
	}
	if err := add(auth.PermChargesManage, func() ([]store.WorkSource, error) { return store.FailedMandateSources(ctx, db, scope) }); err != nil {
		return nil, err
	}
	if err := add(auth.PermContractsRead, func() ([]store.WorkSource, error) { return store.OpenDeadlineSources(ctx, db, scope) }); err != nil {
		return nil, err
	}
	out := make([]WorkItem, 0, len(sources)+len(states))
	seen := map[string]bool{}
	for _, src := range sources {
		kind, _, _ := kindOf(src.SourceType)
		key := src.SourceType + "/" + src.SourceID
		seen[key] = true
		it := WorkItem{
			Kind: kind, SourceType: src.SourceType, SourceID: src.SourceID, Status: store.WorkItemOpen,
			CustomerID: src.CustomerID, Amount: money.Cents(src.AmountCents), DueOn: src.DueOn, CreatedAt: src.CreatedAt,
		}
		if st, ok := byKey[key]; ok {
			it.Status, it.AssigneeUserID, it.Note = st.Status, st.AssigneeUserID, st.Note
		}
		out = append(out, it)
	}
	if can(p, scope, auth.PermChargesManage) {
		for _, st := range states {
			if seen[st.SourceType+"/"+st.SourceID] || !slices.Contains(storedKinds, st.SourceType) {
				continue
			}
			out = append(out, WorkItem{
				Kind: st.SourceType, SourceType: st.SourceType, SourceID: st.SourceID, Status: st.Status,
				AssigneeUserID: st.AssigneeUserID, Note: st.Note, CreatedAt: st.CreatedAt,
			})
		}
	}
	var custs []string
	for _, it := range out {
		custs = append(custs, it.CustomerID)
	}
	names, err := s.customerNames(ctx, db, scope, p, custs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].CustomerName = names[out[i].CustomerID]
	}
	slices.SortFunc(out, func(a, b WorkItem) int {
		if c := strings.Compare(b.SourceID, a.SourceID); c != 0 { // newest first (UUIDv7)
			return c
		}
		return strings.Compare(a.SourceType, b.SourceType)
	})
	return out, nil
}

// Worklist returns one page of the office worklist (items by the permission of their source; contracts.read or
// charges.manage to open it).
func (s *Service) Worklist(ctx context.Context, p *auth.Principal, marinaID string, f WorkFilter, cursor string, limit int) (store.Page[WorkItem], error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermChargesManage, auth.PermContractsRead)
	if err != nil {
		return store.Page[WorkItem]{}, err
	}
	after := ""
	if cursor != "" {
		if after, err = store.DecodeCursor(cursor); err != nil {
			return store.Page[WorkItem]{}, err
		}
	}
	all, err := s.worklist(ctx, s.d.DB.DB, scope, p)
	if err != nil {
		return store.Page[WorkItem]{}, err
	}
	var items []WorkItem
	for _, it := range all {
		if f.Kind != "" && it.Kind != f.Kind {
			continue
		}
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		if f.Status == "" && it.Status != store.WorkItemOpen && it.Status != store.WorkItemInProgress {
			continue
		}
		if f.AssigneeUserID != "" && it.AssigneeUserID != f.AssigneeUserID {
			continue
		}
		items = append(items, it)
	}
	start := 0
	if after != "" {
		start = len(items)
		for i, it := range items {
			if it.SourceID == after {
				start = i + 1
				break
			}
		}
	}
	limit = store.ClampLimit(limit)
	end := min(start+limit, len(items))
	page := store.Page[WorkItem]{Items: append([]WorkItem{}, items[start:end]...)}
	if end < len(items) && store.ValidID(items[end-1].SourceID) {
		page.NextCursor = store.EncodeCursor(items[end-1].SourceID)
	}
	return page, nil
}

// WorkPatch changes the assignee or status of a worklist item. Assignee nil keeps it, "" clears it.
type WorkPatch struct {
	AssigneeUserID *string
	Status         string
	Note           *string
}

// UpdateWorkItem assigns a worklist item or changes its status (the permission of the item's source).
func (s *Service) UpdateWorkItem(ctx context.Context, p *auth.Principal, marinaID, sourceType, sourceID string, in WorkPatch) (*WorkItem, error) {
	kind, perm, ok := kindOf(sourceType)
	if !ok {
		if _, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermChargesManage, auth.PermContractsRead); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, perm)
	if err != nil {
		return nil, err
	}
	switch in.Status {
	case store.WorkItemOpen, store.WorkItemInProgress, store.WorkItemDone, store.WorkItemDismissed:
	default:
		return nil, invalid("status", "invalid")
	}
	if in.Note != nil {
		n := strings.TrimSpace(*in.Note)
		if utf8.RuneCountInString(n) > 500 {
			return nil, invalid("note", "too_long")
		}
		in.Note = &n
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.GetWorkItemState(ctx, tx, scope, sourceType, sourceID)
		switch {
		case err == nil:
		case errors.Is(err, store.ErrNotFound):
			if _, computed := computedKinds[sourceType]; !computed {
				return ErrNotFound
			}
			exists, err := store.WorkSourceExists(ctx, tx, scope, sourceType, sourceID)
			if err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			cur = &store.WorkItemState{SourceType: sourceType, SourceID: sourceID}
		default:
			return err
		}
		from := *cur
		cur.Status = in.Status
		if in.AssigneeUserID != nil {
			if *in.AssigneeUserID != "" {
				ok, err := store.UserInOrganization(ctx, tx, scope.OrgID(), *in.AssigneeUserID)
				if err != nil {
					return err
				}
				if !ok {
					return invalid("assignee_user_id", "unknown")
				}
			}
			cur.AssigneeUserID = *in.AssigneeUserID
		}
		if in.Note != nil {
			cur.Note = *in.Note
		}
		if err := store.SaveWorkItemState(ctx, tx, scope, cur, s.now()); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_item.update", "work_item", sourceType+"/"+sourceID, map[string]any{
			"kind": kind, "from_status": from.Status, "status": cur.Status, "assignee_user_id": cur.AssigneeUserID,
		}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "work_item", Change: event.ChangeUpdated})
	})
	if err != nil {
		return nil, err
	}
	all, err := s.worklist(ctx, s.d.DB.DB, scope, p)
	if err != nil {
		return nil, err
	}
	for _, it := range all {
		if it.SourceType == sourceType && it.SourceID == sourceID {
			return &it, nil
		}
	}
	st, err := store.GetWorkItemState(ctx, s.d.DB.DB, scope, sourceType, sourceID)
	if err != nil {
		return nil, err
	}
	return &WorkItem{Kind: kind, SourceType: sourceType, SourceID: sourceID, Status: st.Status, AssigneeUserID: st.AssigneeUserID, Note: st.Note, CreatedAt: st.CreatedAt}, nil
}
