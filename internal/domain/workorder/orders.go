package workorder

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// LinePricer prices the catalogue selections (packages and single items, with their formulas) of an order created
// without a quote, with the quote line engine. Implemented by the quote side and wired in internal/app; without
// it, an order without a quote takes manual lines only.
type LinePricer interface {
	PriceOrderLinesTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID, vesselID string, on calendar.Date,
		selections []Selection, jobInputs map[string]price.VariableValue) ([]price.Line, price.Snapshot, error)
}

// SetLinePricer wires the quote line engine for orders without a quote.
func (s *Service) SetLinePricer(lp LinePricer) { s.pricer = lp }

var billingModes = []string{store.BillingModeActual, store.BillingModeFixed, store.BillingModeProgress}

// ---- creation ----

// newOrder numbers and inserts an order in tx (number per year of the creation date, C<year>-NNNNNN).
func (s *Service) newOrder(ctx context.Context, tx bun.IDB, scope store.MarinaScope, o *store.WorkOrder) error {
	today := s.today(ctx, tx, scope)
	n, err := store.NextNumber(ctx, tx, scope, store.SeriesWorkOrder, today.Year())
	if err != nil {
		return err
	}
	now := s.now()
	o.ID, o.Number, o.Year, o.Status, o.CreatedAt, o.UpdatedAt = store.NewID(), n, today.Year(), store.WorkOrderPlanned, now, now
	if o.BillingMode == "" {
		o.BillingMode = store.BillingModeActual
	}
	return store.InsertWorkOrder(ctx, tx, scope, o)
}

// labourRates adds to r the labour item of every active qualification and, for items not priced yet, the
// catalogue price on day.
func (s *Service) labourRates(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *rateSnapshot, day calendar.Date) error {
	quals, err := store.WOActiveQualifications(ctx, tx, scope)
	if err != nil {
		return err
	}
	for _, q := range quals {
		if q.LabourItemID == "" {
			continue
		}
		r.LabourItems[q.ID] = q.LabourItemID
		if _, ok := r.Units[q.LabourItemID]; ok {
			continue
		}
		p, ok, err := s.catalogPrice(ctx, tx, scope, q.LabourItemID, day)
		if err != nil {
			return err
		}
		if ok {
			r.Units[q.LabourItemID] = p
		}
	}
	return nil
}

// quoteLines turns accepted quote lines into budget lines and tasks of the order.
func (s *Service) quoteLines(order *store.WorkOrder, q yardquote.AcceptedQuote, origin, variationID string, firstSeq, firstTask int,
	now time.Time,
) ([]store.WorkOrderLine, []store.WorkTask) {
	var lines []store.WorkOrderLine
	var tasks []store.WorkTask
	seq, tseq := firstSeq, firstTask
	for _, ql := range q.Lines {
		item := q.Snapshot.Items[ql.ItemID]
		l := store.WorkOrderLine{
			ID: store.NewID(), WorkOrderID: order.ID, Seq: seq, Origin: origin, VariationID: variationID, QuoteLineID: ql.ID,
			ItemID: ql.ItemID, WorkTypeID: ql.WorkTypeID, QualificationID: ql.QualificationID, Description: ql.Description,
			QtyMilli: ql.QtyMilli, UnitCents: ql.UnitCents, DiscountCents: ql.DiscountCents, NetCents: ql.NetCents,
			VATCents: ql.VATCents, GrossCents: ql.GrossCents, VATRateBP: ql.VATRateBP, VATNature: ql.VATNature,
			PriceIncludesVAT: ql.PriceIncludesVAT, UnitCostCents: ql.UnitCostCents, EstimatedHoursMilli: ql.EstimatedHoursMilli,
			CreatedAt: now, UpdatedAt: now,
		}
		if l.QualificationID == "" {
			l.QualificationID = item.QualificationID
		}
		if l.UnitCostCents == 0 {
			l.UnitCostCents = int64(item.CostCents)
		}
		lines = append(lines, l)
		seq++
		var spec *price.TaskSpec
		if len(ql.Task) > 0 && string(ql.Task) != "null" {
			spec = &price.TaskSpec{}
			if json.Unmarshal(ql.Task, spec) != nil {
				spec = nil
			}
		}
		if spec == nil && !account.IsLabour(item.ItemType) {
			continue
		}
		title := ql.Description
		var checklist []ChecklistItem
		if spec != nil {
			if t := i18nPick(spec.Title); t != "" {
				title = t
			}
			for i, c := range spec.Checklist {
				checklist = append(checklist, ChecklistItem{Key: checkKey(i), Label: c})
			}
		}
		if title == "" {
			title = item.Code
		}
		if title == "" {
			title = "—"
		}
		title, _ = cleanText(title, maxTitleRunes)
		if r := []rune(title); len(r) > maxTitleRunes {
			title = string(r[:maxTitleRunes])
		}
		cl, _ := json.Marshal(nonNilChecklist(checklist))
		tasks = append(tasks, store.WorkTask{
			ID: store.NewID(), WorkOrderID: order.ID, Seq: tseq, Title: title, QualificationID: l.QualificationID,
			WorkTypeID: l.WorkTypeID, EstimatedHoursMilli: l.EstimatedHoursMilli, Status: store.WorkTaskTodo, Checklist: cl,
			SourceLineID: l.ID, CreatedAt: now, UpdatedAt: now,
		})
		tseq++
	}
	return lines, tasks
}

func i18nPick(m map[string]string) string {
	for _, k := range []string{"it", "en"} {
		if v := m[k]; v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}

func checkKey(i int) string { return "c" + itoa(i+1) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func nonNilChecklist(c []ChecklistItem) []ChecklistItem {
	if c == nil {
		return []ChecklistItem{}
	}
	return c
}

// CreateFromQuoteTx implements yardquote.OrderCreator: the order, its budget lines, its tasks and the advance
// billing of an accepted job quote, in the accept transaction (INV-96: one order per quote, work_orders_quote_uidx).
func (s *Service) CreateFromQuoteTx(ctx context.Context, tx bun.Tx, scope store.MarinaScope, actor audit.Actor, q yardquote.AcceptedQuote) (yardquote.OrderRef, error) {
	if q.Quote.Kind != store.YardQuoteJob {
		return yardquote.OrderRef{}, ErrInvalidTransition
	}
	if _, err := store.WorkOrderByQuote(ctx, tx, scope, q.Quote.ID); err == nil {
		return yardquote.OrderRef{}, ErrInvalidTransition
	} else if !errors.Is(err, store.ErrNotFound) {
		return yardquote.OrderRef{}, err
	}
	now := s.now()
	o := &store.WorkOrder{
		QuoteID: q.Quote.ID, CustomerID: q.Quote.CustomerID, VesselID: q.Quote.VesselID, BillingMode: q.Quote.BillingMode,
		PlannedStartOn: q.Quote.PlannedStartOn, PlannedDeliveryOn: q.Quote.PlannedDeliveryOn,
		DepositCents: q.Quote.DepositAmountCents, Notes: clip(q.Quote.Notes, maxNotesRunes), CreatedByUserID: actorUser(actor),
	}
	if len(q.Lines) > 0 {
		o.WorkTypeID = q.Lines[0].WorkTypeID
	}
	if err := s.newOrder(ctx, tx, scope, o); err != nil {
		if store.IsUniqueViolation(err) {
			return yardquote.OrderRef{}, ErrInvalidTransition
		}
		return yardquote.OrderRef{}, err
	}
	lines, tasks := s.quoteLines(o, q, store.WorkLineOriginQuote, "", 1, 1, now)
	r := rateSnapshot{Units: map[string]money.Cents{}, LabourItems: map[string]string{}, VAT: map[string]account.VAT{}}
	for id, p := range q.Snapshot.Prices {
		if p.Found {
			r.Units[id] = p.UnitCents
		}
	}
	for _, l := range lines {
		r.Units[l.ItemID] = money.Cents(l.UnitCents)
		r.VAT[l.ItemID] = account.VAT{RateBP: int64(l.VATRateBP), Nature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT}
	}
	if err := s.labourRates(ctx, tx, scope, &r, s.today(ctx, tx, scope)); err != nil {
		return yardquote.OrderRef{}, err
	}
	o.RateSnapshot, _ = json.Marshal(r)
	if err := store.UpdateWorkOrder(ctx, tx, scope, o, o.Version, now, "rate_snapshot"); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.insertBudget(ctx, tx, scope, o, lines, tasks); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.audit(ctx, tx, scope, actor, "work_order.create", "work_order", o.ID, map[string]any{
		"number": FormatNumber(o.Year, o.Number), "quote_id": q.Quote.ID, "billing_mode": o.BillingMode, "lines": len(lines),
		"tasks": len(tasks), "via": q.Via,
	}); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.statusChanged(ctx, tx, scope, o.ID, "", o.Status); err != nil {
		return yardquote.OrderRef{}, err
	}
	ref := yardquote.OrderRef{WorkOrderID: o.ID, Number: FormatNumber(o.Year, o.Number)}
	set, err := s.settings(ctx, tx, scope)
	if err != nil {
		return ref, err
	}
	if o.DepositCents > 0 && set.AdvanceOnAccept {
		res, err := s.billTx(ctx, tx, scope, actor, o, BillingInput{Kind: BillingAdvance, Amount: ptr(money.Cents(o.DepositCents))}, false, true)
		if err != nil {
			return ref, err
		}
		ref.AdvanceChargeID = res.ChargeID
	}
	return ref, nil
}

// ApplyVariationTx implements yardquote.OrderCreator: the budget lines and tasks of an accepted variation quote
// (INV-96: one variation per quote, work_order_variations_quote_uidx).
func (s *Service) ApplyVariationTx(ctx context.Context, tx bun.Tx, scope store.MarinaScope, actor audit.Actor, q yardquote.AcceptedQuote) (yardquote.OrderRef, error) {
	if q.Quote.Kind != store.YardQuoteVariation || q.Quote.WorkOrderID == "" {
		return yardquote.OrderRef{}, ErrInvalidTransition
	}
	o, err := store.LockWorkOrder(ctx, tx, scope, q.Quote.WorkOrderID)
	if err != nil {
		return yardquote.OrderRef{}, notFound(err)
	}
	if !Open(o.Status) {
		return yardquote.OrderRef{}, ErrClosed
	}
	now := s.now()
	v := &store.WorkOrderVariation{ID: store.NewID(), WorkOrderID: o.ID, QuoteID: q.Quote.ID, AcceptedAt: q.AcceptedAt, CreatedAt: now}
	if v.AcceptedAt.IsZero() {
		v.AcceptedAt = now
	}
	if err := store.InsertWorkOrderVariation(ctx, tx, scope, v); err != nil {
		if store.IsUniqueViolation(err) {
			return yardquote.OrderRef{}, ErrInvalidTransition
		}
		return yardquote.OrderRef{}, err
	}
	seq, err := store.MaxWorkOrderLineSeq(ctx, tx, scope, o.ID)
	if err != nil {
		return yardquote.OrderRef{}, err
	}
	tseq, err := store.MaxWorkTaskSeq(ctx, tx, scope, o.ID)
	if err != nil {
		return yardquote.OrderRef{}, err
	}
	lines, tasks := s.quoteLines(o, q, store.WorkLineOriginVariation, v.ID, seq+1, tseq+1, now)
	r := rateSnapshotOf(o.RateSnapshot)
	for _, l := range lines {
		if _, ok := r.Units[l.ItemID]; !ok {
			r.Units[l.ItemID] = money.Cents(l.UnitCents)
		}
		if _, ok := r.VAT[l.ItemID]; !ok {
			r.VAT[l.ItemID] = account.VAT{RateBP: int64(l.VATRateBP), Nature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT}
		}
	}
	o.RateSnapshot, _ = json.Marshal(r)
	if err := store.UpdateWorkOrder(ctx, tx, scope, o, o.Version, now, "rate_snapshot"); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.insertBudget(ctx, tx, scope, o, lines, tasks); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.audit(ctx, tx, scope, actor, "work_order.variation", "work_order", o.ID, map[string]any{
		"quote_id": q.Quote.ID, "variation_id": v.ID, "lines": len(lines), "tasks": len(tasks), "via": q.Via,
	}); err != nil {
		return yardquote.OrderRef{}, err
	}
	if err := s.orderChanged(ctx, tx, scope, o.ID, "line", v.ID); err != nil {
		return yardquote.OrderRef{}, err
	}
	return yardquote.OrderRef{WorkOrderID: o.ID, Number: FormatNumber(o.Year, o.Number)}, nil
}

// insertBudget writes lines and tasks and recomputes the order completion.
func (s *Service) insertBudget(ctx context.Context, tx bun.IDB, scope store.MarinaScope, o *store.WorkOrder, lines []store.WorkOrderLine, tasks []store.WorkTask) error {
	if err := store.InsertWorkOrderLines(ctx, tx, scope, lines); err != nil {
		if store.IsForeignKeyViolation(err) {
			return invalid("lines", "invalid")
		}
		return err
	}
	for i := range tasks {
		if err := store.InsertWorkTask(ctx, tx, scope, &tasks[i]); err != nil {
			return err
		}
	}
	return s.recomputeCompletion(ctx, tx, scope, o.ID)
}

// recomputeCompletion stores the hours-weighted completion of an order (cancelled tasks excluded).
func (s *Service) recomputeCompletion(ctx context.Context, tx bun.IDB, scope store.MarinaScope, orderID string) error {
	tasks, err := store.WorkTasksOfOrders(ctx, tx, scope, []string{orderID})
	if err != nil {
		return err
	}
	var p []int64
	var h []qty.Milli
	for _, t := range tasks {
		if t.Status == store.WorkTaskCancelled {
			continue
		}
		p = append(p, int64(t.ProgressBP))
		h = append(h, qty.Milli(t.EstimatedHoursMilli))
	}
	bp, err := account.Completion(p, h)
	if err != nil {
		return err
	}
	return store.SetWorkOrderCompletion(ctx, tx, scope, orderID, int(bp))
}

func (s *Service) statusChanged(ctx context.Context, tx bun.IDB, scope store.MarinaScope, orderID, from, to string) error {
	return s.emit(ctx, tx, scope, event.WorkOrderStatusChanged, event.WorkOrderStatusChangedData{WorkOrderID: orderID, From: from, To: to})
}

func actorUser(a audit.Actor) string {
	if a.Type == audit.ActorUser && store.ValidID(a.ID) {
		return a.ID
	}
	return ""
}

func ptr[T any](v T) *T { return &v }

func clip(s string, n int) string {
	s, _ = cleanText(s, n)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// validateOrderRefs checks the references of an order (work type, responsible, position) in the marina.
func (s *Service) validateOrderRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, o *store.WorkOrder) error {
	v := &auth.ValidationError{}
	if ok, err := store.WOWorkTypesExist(ctx, db, scope, []string{o.WorkTypeID}); err != nil {
		return err
	} else if !ok {
		v.Add("work_type_id", "invalid")
	}
	if o.ResponsibleUserID != "" {
		if ok, err := store.WOUserInMarina(ctx, db, scope, o.ResponsibleUserID); err != nil {
			return err
		} else if !ok {
			v.Add("responsible_user_id", "invalid")
		}
	}
	if o.AllocationID != "" {
		if ok, err := store.WOAllocationOfVessel(ctx, db, scope, o.AllocationID, o.VesselID); err != nil {
			return err
		} else if !ok {
			v.Add("allocation_id", "invalid")
		}
	}
	if !o.PlannedStartOn.IsZero() && !o.PlannedDeliveryOn.IsZero() && o.PlannedDeliveryOn.Before(o.PlannedStartOn) {
		v.Add("planned_delivery_on", "before_start")
	}
	var ok bool
	if o.PositionNote, ok = cleanText(o.PositionNote, maxPositionRunes); !ok {
		v.Add("position_note", "too_long")
	}
	if o.Notes, ok = cleanText(o.Notes, maxNotesRunes); !ok {
		v.Add("notes", "too_long")
	}
	return v.Err()
}

// CreateOrder creates an order without a quote (yard.manage): manual lines priced by hand and catalogue selections
// priced by the quote line engine.
func (s *Service) CreateOrder(ctx context.Context, p *auth.Principal, marinaID string, in OrderInput) (*OrderView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if in.BillingMode != "" && !slices.Contains(billingModes, in.BillingMode) {
		return nil, invalid("billing_mode", "invalid")
	}
	if len(in.Selections) > 0 && s.pricer == nil {
		return nil, invalid("selections", "unavailable")
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ok, err := store.WOCustomerVesselExist(ctx, tx, scope, in.CustomerID, in.VesselID)
		if err != nil {
			return err
		}
		if !ok {
			return invalid("customer_id", "invalid")
		}
		o := &store.WorkOrder{
			CustomerID: in.CustomerID, VesselID: in.VesselID, WorkTypeID: in.WorkTypeID, ResponsibleUserID: in.ResponsibleUserID,
			BillingMode: in.BillingMode, PlannedStartOn: in.PlannedStartOn, PlannedDeliveryOn: in.PlannedDeliveryOn,
			AllocationID: in.AllocationID, PositionNote: in.PositionNote, Notes: in.Notes, CreatedByUserID: p.UserID,
		}
		if err := s.validateOrderRefs(ctx, tx, scope, o); err != nil {
			return err
		}
		today := s.today(ctx, tx, scope)
		lines, err := s.manualLines(ctx, tx, scope, in.ManualLines, in.WorkTypeID)
		if err != nil {
			return err
		}
		var priced []price.Line
		var snap price.Snapshot
		if len(in.Selections) > 0 {
			if priced, snap, err = s.pricer.PriceOrderLinesTx(ctx, tx, scope, in.CustomerID, in.VesselID, today, in.Selections, in.JobInputs); err != nil {
				return err
			}
		}
		if err := s.newOrder(ctx, tx, scope, o); err != nil {
			return err
		}
		id = o.ID
		now := s.now()
		var ql []store.YardQuoteLine
		for i, l := range priced {
			if len(l.Issues) > 0 {
				return ErrNoPrice
			}
			var task store.JSON
			if l.Spec.Task != nil {
				task, _ = json.Marshal(l.Spec.Task)
			}
			ql = append(ql, store.YardQuoteLine{
				Seq: i + 1, ItemID: l.Spec.ItemID, WorkTypeID: l.Spec.WorkTypeID, QualificationID: l.QualificationID,
				Description: l.Spec.Description, QtyMilli: int64(l.Qty), UnitCents: int64(l.Unit), DiscountCents: int64(l.Discount),
				NetCents: int64(l.Net), VATCents: int64(l.VAT), GrossCents: int64(l.Gross), VATRateBP: int(l.VATData.RateBP),
				VATNature: l.VATData.Nature, PriceIncludesVAT: l.VATData.PriceIncludesVAT, UnitCostCents: int64(l.UnitCost),
				EstimatedHoursMilli: int64(l.EstimatedHours), Task: task,
			})
		}
		pl, tasks := s.quoteLines(o, yardquote.AcceptedQuote{Lines: ql, Snapshot: snap}, store.WorkLineOriginManual, "", 1, 1, now)
		for i := range lines {
			lines[i].WorkOrderID, lines[i].Seq = o.ID, len(pl)+i+1
			if lines[i].QualificationID == "" && lines[i].EstimatedHoursMilli == 0 {
				continue
			}
			// A manual labour line produces its task, as a labour line of a quote.
			title := clip(lines[i].Description, maxTitleRunes)
			if title == "" {
				title = "—"
			}
			tasks = append(tasks, store.WorkTask{
				ID: store.NewID(), WorkOrderID: o.ID, Seq: len(tasks) + 1, Title: title, QualificationID: lines[i].QualificationID,
				WorkTypeID: lines[i].WorkTypeID, EstimatedHoursMilli: lines[i].EstimatedHoursMilli, Status: store.WorkTaskTodo,
				Checklist: store.JSON("[]"), SourceLineID: lines[i].ID, CreatedAt: now, UpdatedAt: now,
			})
		}
		lines = append(pl, lines...)
		if o.WorkTypeID == "" && len(lines) > 0 {
			o.WorkTypeID = lines[0].WorkTypeID
		}
		r := rateSnapshot{Units: map[string]money.Cents{}, LabourItems: map[string]string{}, VAT: map[string]account.VAT{}}
		for _, l := range lines {
			r.Units[l.ItemID] = money.Cents(l.UnitCents)
			r.VAT[l.ItemID] = account.VAT{RateBP: int64(l.VATRateBP), Nature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT}
		}
		if err := s.labourRates(ctx, tx, scope, &r, today); err != nil {
			return err
		}
		o.RateSnapshot, _ = json.Marshal(r)
		if err := store.UpdateWorkOrder(ctx, tx, scope, o, o.Version, now, "rate_snapshot", "work_type_id"); err != nil {
			return err
		}
		if err := s.insertBudget(ctx, tx, scope, o, lines, tasks); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_order.create", "work_order", o.ID, map[string]any{
			"number": FormatNumber(o.Year, o.Number), "billing_mode": o.BillingMode, "lines": len(lines), "tasks": len(tasks),
		}); err != nil {
			return err
		}
		return s.statusChanged(ctx, tx, scope, o.ID, "", o.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, p, marinaID, id)
}

// manualLines prices manual lines as the quote engine does for M5 lines: amount = qty × unit − discount; net-priced
// items charge net.PercentBP(rate), gross-priced items split the amount (money.VATSplit).
func (s *Service) manualLines(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in []ManualLine, orderWT string) ([]store.WorkOrderLine, error) {
	if len(in) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(in))
	wts := []string{}
	for _, l := range in {
		ids = append(ids, l.ItemID)
		wts = append(wts, l.WorkTypeID)
	}
	items, err := store.ItemsByIDs(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if ok, err := store.WOWorkTypesExist(ctx, tx, scope, wts); err != nil {
		return nil, err
	} else if !ok {
		return nil, invalid("manual_lines.work_type_id", "invalid")
	}
	costs, err := store.WOItemCosts(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	quals, err := store.WOActiveQualifications(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	qualOf := map[string]string{}
	for _, q := range quals {
		qualOf[q.LabourItemID] = q.ID
	}
	now := s.now()
	out := make([]store.WorkOrderLine, 0, len(in))
	for i, l := range in {
		it, ok := byID[l.ItemID]
		if !ok {
			return nil, invalid("manual_lines["+itoa(i)+"].item_id", "invalid")
		}
		wt := l.WorkTypeID
		if wt == "" {
			wt = orderWT
		}
		if wt == "" {
			return nil, invalid("manual_lines["+itoa(i)+"].work_type_id", "required")
		}
		if l.Qty < 0 || l.Unit < 0 || l.Discount < 0 {
			return nil, invalid("manual_lines["+itoa(i)+"]", "negative")
		}
		amount, err := l.Qty.MulCents(l.Unit)
		if err != nil {
			return nil, invalid("manual_lines["+itoa(i)+"].qty_milli", "overflow")
		}
		if l.Discount > amount {
			return nil, invalid("manual_lines["+itoa(i)+"].discount_cents", "too_large")
		}
		amount -= l.Discount
		var net, vat money.Cents
		if it.PriceIncludesVAT {
			net, vat = money.VATSplit(amount, it.VATRateBP)
		} else {
			net, vat = amount, amount.PercentBP(it.VATRateBP)
		}
		desc := clip(l.Description, 500)
		line := store.WorkOrderLine{
			ID: store.NewID(), Origin: store.WorkLineOriginManual, ItemID: it.ID, WorkTypeID: wt, Description: desc,
			QtyMilli: int64(l.Qty), UnitCents: int64(l.Unit), DiscountCents: int64(l.Discount), NetCents: int64(net),
			VATCents: int64(vat), GrossCents: int64(net + vat), VATRateBP: int(it.VATRateBP), VATNature: it.VATNature,
			PriceIncludesVAT: it.PriceIncludesVAT, UnitCostCents: costs[it.ID], CreatedAt: now, UpdatedAt: now,
		}
		if account.IsLabour(it.ItemType) {
			line.QualificationID = qualOf[it.ID]
			line.EstimatedHoursMilli = int64(l.Qty)
		}
		out = append(out, line)
	}
	return out, nil
}

// ---- reads ----

func (s *Service) orderViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.WorkOrder, withLines bool) ([]OrderView, error) {
	out := make([]OrderView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	vids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		vids = append(vids, r.VesselID)
	}
	vessels, err := store.WOVessels(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	approved, err := store.WOApprovedHours(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	estimated, err := store.WOEstimatedHours(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byOrder := map[string][]store.WorkOrderLine{}
	items := map[string]store.Item{}
	if withLines {
		lines, err := store.WorkOrderLines(ctx, db, scope, ids)
		if err != nil {
			return nil, err
		}
		var itemIDs []string
		for _, l := range lines {
			byOrder[l.WorkOrderID] = append(byOrder[l.WorkOrderID], l)
			itemIDs = append(itemIDs, l.ItemID)
		}
		its, err := store.ItemsByIDs(ctx, db, scope, itemIDs)
		if err != nil {
			return nil, err
		}
		for _, it := range its {
			items[it.ID] = it
		}
	}
	for _, r := range rows {
		ls := byOrder[r.ID]
		if ls == nil {
			ls = []store.WorkOrderLine{}
		}
		out = append(out, OrderView{
			Row: r, Lines: ls, Items: items, VesselName: vessels[r.VesselID].Name,
			HoursApproved: qty.Milli(approved[r.ID]), HoursEstimated: qty.Milli(estimated[r.ID]), Warnings: []string{},
		})
	}
	return out, nil
}

// ListOrders lists orders, newest first (yard.read or yard.manage).
func (s *Service) ListOrders(ctx context.Context, p *auth.Principal, marinaID string, f OrderFilter) ([]OrderView, string, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	sf := store.WorkOrderFilter{CustomerID: f.CustomerID, VesselID: f.VesselID, Q: f.Q}
	if f.Status != "" {
		sf.Statuses = []string{f.Status}
	}
	if f.OpenOnly {
		sf.Statuses = store.WorkOrderOpenStatuses
		if f.Status != "" {
			if !slices.Contains(store.WorkOrderOpenStatuses, f.Status) {
				return []OrderView{}, "", nil
			}
			sf.Statuses = []string{f.Status}
		}
	}
	page, err := store.ListWorkOrders(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.orderViews(ctx, s.d.DB.DB, scope, page.Items, false)
	return items, page.NextCursor, err
}

// GetOrder reads an order with its budget lines (yard.read or yard.manage).
func (s *Service) GetOrder(ctx context.Context, p *auth.Principal, marinaID, id string) (*OrderView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.orderView(ctx, s.d.DB.DB, scope, id)
}

func (s *Service) orderView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*OrderView, error) {
	o, err := store.GetWorkOrder(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.orderViews(ctx, db, scope, []store.WorkOrder{*o}, true)
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// boardStatuses are the board columns (cancelled hidden).
var boardStatuses = []string{
	store.WorkOrderPlanned, store.WorkOrderInProgress, store.WorkOrderOnHold, store.WorkOrderCompleted,
	store.WorkOrderDelivered, store.WorkOrderInvoiced,
}

// Board returns the board "barche in lavorazione": the orders by status, cancelled hidden, invoiced only while the
// boat was not delivered (yard.read or yard.manage).
func (s *Service) Board(ctx context.Context, p *auth.Principal, marinaID string) (*Board, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	rows, err := store.WorkOrdersByStatus(ctx, db, scope, boardStatuses)
	if err != nil {
		return nil, err
	}
	kept := rows[:0]
	for _, r := range rows {
		if r.Status == store.WorkOrderInvoiced && !r.DeliveredAt.IsZero() {
			continue
		}
		kept = append(kept, r)
	}
	views, err := s.orderViews(ctx, db, scope, kept, false)
	if err != nil {
		return nil, err
	}
	vids := make([]string, 0, len(kept))
	for _, r := range kept {
		vids = append(vids, r.VesselID)
	}
	vessels, err := store.WOVessels(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	b := &Board{Today: today}
	cols := map[string]*BoardColumn{}
	for _, st := range boardStatuses {
		b.Columns = append(b.Columns, BoardColumn{Status: st, Cards: []BoardCard{}})
	}
	for i := range b.Columns {
		cols[b.Columns[i].Status] = &b.Columns[i]
	}
	for _, v := range views {
		c := BoardCard{
			Order: v.Row, VesselName: v.VesselName, LOACm: vessels[v.Row.VesselID].LOACm, HoursApproved: v.HoursApproved,
			HoursEstimated: v.HoursEstimated,
		}
		if !v.Row.PlannedDeliveryOn.IsZero() && v.Row.DeliveredAt.IsZero() && v.Row.PlannedDeliveryOn.Before(today) {
			d := v.Row.PlannedDeliveryOn.DaysUntil(today)
			c.DelayDays = &d
		}
		cols[v.Row.Status].Cards = append(cols[v.Row.Status].Cards, c)
	}
	return b, nil
}

// ---- changes ----

// UpdateOrder changes responsible, planned dates, position, notes, main work type and consumed quantities
// (yard.manage, If-Match).
func (s *Service) UpdateOrder(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch OrderPatch) (*OrderView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.LockWorkOrder(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(o.Version) != version {
			return ErrVersionMismatch
		}
		if o.Status == store.WorkOrderCancelled {
			return ErrClosed
		}
		diff := map[string]any{}
		if patch.ResponsibleUserID != nil {
			o.ResponsibleUserID = *patch.ResponsibleUserID
			diff["responsible_user_id"] = o.ResponsibleUserID
		}
		if patch.PlannedStartOn != nil {
			o.PlannedStartOn = *patch.PlannedStartOn
			diff["planned_start_on"] = o.PlannedStartOn
		}
		if patch.PlannedDeliveryOn != nil {
			o.PlannedDeliveryOn = *patch.PlannedDeliveryOn
			diff["planned_delivery_on"] = o.PlannedDeliveryOn
		}
		if patch.AllocationID != nil {
			o.AllocationID = *patch.AllocationID
			diff["allocation_id"] = o.AllocationID
		}
		if patch.PositionNote != nil {
			o.PositionNote = *patch.PositionNote
		}
		if patch.Notes != nil {
			o.Notes = *patch.Notes
		}
		if patch.WorkTypeID != nil {
			o.WorkTypeID = *patch.WorkTypeID
			diff["work_type_id"] = o.WorkTypeID
		}
		if err := s.validateOrderRefs(ctx, tx, scope, o); err != nil {
			return err
		}
		if len(patch.Consumed) > 0 {
			lines, err := store.WorkOrderLines(ctx, tx, scope, []string{o.ID})
			if err != nil {
				return err
			}
			known := map[string]bool{}
			for _, l := range lines {
				known[l.ID] = true
			}
			now := s.now()
			for i, c := range patch.Consumed {
				if !known[c.LineID] {
					return invalid("consumed["+itoa(i)+"].line_id", "invalid")
				}
				if c.Consumed != nil && *c.Consumed < 0 {
					return invalid("consumed["+itoa(i)+"].consumed_qty_milli", "negative")
				}
				if err := store.SetWorkOrderLineConsumed(ctx, tx, scope, o.ID, c.LineID, c.Consumed, now); err != nil {
					return err
				}
			}
			diff["consumed"] = len(patch.Consumed)
		}
		if err := store.UpdateWorkOrder(ctx, tx, scope, o, int(version), s.now(), "responsible_user_id", "planned_start_on",
			"planned_delivery_on", "allocation_id", "position_note", "notes", "work_type_id"); err != nil {
			if store.IsForeignKeyViolation(err) {
				return invalid("work_order", "invalid_reference")
			}
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_order.update", "work_order", o.ID, diff); err != nil {
			return err
		}
		return s.orderChanged(ctx, tx, scope, o.ID, "order", o.ID)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, p, marinaID, id)
}

// apiActions are the actions of transitionWorkOrder (invoice belongs to the final billing).
var apiActions = []string{ActionStart, ActionHold, ActionResume, ActionComplete, ActionReopen, ActionDeliver, ActionCancel}

// Transition changes the state of an order (yard.manage, If-Match): the table of states.go; cancel refuses an order
// with live charges (409 work_order_has_charges) and cancels its future lifting bookings in the same transaction.
func (s *Service) Transition(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, action, reason string) (*OrderView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(apiActions, action) {
		return nil, invalid("action", "invalid")
	}
	reason, ok := cleanText(reason, maxReasonRunes)
	if !ok {
		return nil, invalid("reason", "too_long")
	}
	if action == ActionCancel && reason == "" {
		return nil, invalid("reason", "required")
	}
	var warnings []string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.LockWorkOrder(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(o.Version) != version {
			return ErrVersionMismatch
		}
		from := o.Status
		to, ok := Next(from, action)
		if !ok {
			return ErrInvalidTransition
		}
		now := s.now()
		cols := []string{"status"}
		switch action {
		case ActionStart:
			if o.StartedAt.IsZero() {
				o.StartedAt = now
				cols = append(cols, "started_at")
			}
		case ActionHold:
			o.HoldReason = reason
			cols = append(cols, "hold_reason")
		case ActionResume:
			o.HoldReason = ""
			cols = append(cols, "hold_reason")
		case ActionComplete:
			o.CompletedAt = now
			cols = append(cols, "completed_at")
			if o.CompletionBP < 10000 {
				warnings = append(warnings, WarnTasksIncomplete)
			}
		case ActionReopen:
			o.CompletedAt = time.Time{}
			cols = append(cols, "completed_at")
		case ActionDeliver:
			if from == store.WorkOrderInvoiced && !o.DeliveredAt.IsZero() {
				return ErrInvalidTransition
			}
			o.DeliveredAt = now
			cols = append(cols, "delivered_at")
		case ActionCancel:
			live, err := store.LiveChargesOfWorkOrder(ctx, tx, scope, o.ID)
			if err != nil {
				return err
			}
			if len(live) > 0 {
				return ErrHasCharges
			}
			o.CancelledAt, o.CancelReason = now, reason
			cols = append(cols, "cancelled_at", "cancel_reason")
			if s.d.Bookings != nil {
				if err := s.d.Bookings.CancelForWorkOrderTx(ctx, tx, scope, p.Actor(), o.ID, s.today(ctx, tx, scope)); err != nil {
					return err
				}
			}
		}
		o.Status = to
		if err := store.UpdateWorkOrder(ctx, tx, scope, o, int(version), now, cols...); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_order."+action, "work_order", o.ID, map[string]any{
			"from": from, "to": to, "reason": reason != "",
		}); err != nil {
			return err
		}
		if from == to {
			return s.orderChanged(ctx, tx, scope, o.ID, "order", o.ID)
		}
		return s.statusChanged(ctx, tx, scope, o.ID, from, to)
	})
	if err != nil {
		return nil, err
	}
	v, err := s.GetOrder(ctx, p, marinaID, id)
	if v != nil && len(warnings) > 0 {
		v.Warnings = warnings
	}
	return v, err
}

// CreateVariation creates a draft variation quote (extra works) of an open order (yard.manage): the order is locked
// and checked here, the quote side prices and stores the quote with the order's customer and vessel (D27).
func (s *Service) CreateVariation(ctx context.Context, p *auth.Principal, marinaID, id string, in yardquote.QuoteInput) (*yardquote.QuoteView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if s.d.Quotes == nil {
		return nil, ErrNotImplemented
	}
	var out *yardquote.QuoteView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, err := store.LockWorkOrder(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		out, err = s.d.Quotes.CreateVariationTx(ctx, tx, scope, p, yardquote.VariationOrder{
			WorkOrderID: o.ID, Number: FormatNumber(o.Year, o.Number), CustomerID: o.CustomerID, VesselID: o.VesselID,
		}, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
