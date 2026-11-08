package admin

import (
	"bytes"
	"io"
	"net/http"
	"net/url"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Work orders (M10, m10-plan §5, §8-§10, §13): orders, board, transitions, variations, tasks, files, external costs,
// final account, billings. Owned by ORDERS. Handlers decode, call workorder.Service and encode.

func woDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func woOptDate(d *openapi_types.Date) *calendar.Date {
	if d == nil {
		return nil
	}
	v := calendar.New(d.Year(), d.Month(), d.Day())
	return &v
}

func woOrder(v *workorder.OrderView) api.WorkOrder {
	o := v.Row
	lines := make([]api.WorkOrderLine, 0, len(v.Lines))
	for _, l := range v.Lines {
		it := v.Items[l.ItemID]
		al := api.WorkOrderLine{
			Id: l.ID, Seq: l.Seq, Origin: api.WorkOrderLineOrigin(l.Origin), VariationId: view.String(l.VariationID), ItemId: l.ItemID,
			ItemCode: view.String(it.Code), WorkTypeId: l.WorkTypeID, QualificationId: view.String(l.QualificationID),
			Description: l.Description, QtyMilli: l.QtyMilli, UnitCents: l.UnitCents, DiscountCents: l.DiscountCents,
			NetCents: l.NetCents, VatCents: l.VATCents, GrossCents: l.GrossCents, VatRateBp: int64(l.VATRateBP),
			PriceIncludesVat: l.PriceIncludesVAT, UnitCostCents: l.UnitCostCents, EstimatedHoursMilli: l.EstimatedHoursMilli,
			ConsumedQtyMilli: l.ConsumedQtyMilli,
		}
		if it.ItemType != "" {
			t := api.ItemType(it.ItemType)
			al.ItemType = &t
		}
		if l.VATNature != "" {
			n := api.VatNature(l.VATNature)
			al.VatNature = &n
		}
		lines = append(lines, al)
	}
	warnings := v.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	out := api.WorkOrder{
		Id: o.ID, Number: workorder.FormatNumber(o.Year, o.Number), QuoteId: view.String(o.QuoteID), CustomerId: o.CustomerID,
		VesselId: o.VesselID, VesselName: view.String(v.VesselName), WorkTypeId: view.String(o.WorkTypeID),
		ResponsibleUserId: view.String(o.ResponsibleUserID), PlannedStartOn: woDate(o.PlannedStartOn),
		PlannedDeliveryOn: woDate(o.PlannedDeliveryOn), AllocationId: view.String(o.AllocationID), PositionNote: o.PositionNote,
		Status: api.WorkOrderStatus(o.Status), BillingMode: api.YardBillingMode(o.BillingMode), CompletionBp: int64(o.CompletionBP),
		Notes: o.Notes, HoldReason: view.String(o.HoldReason), StartedAt: view.Time(o.StartedAt), CompletedAt: view.Time(o.CompletedAt),
		DeliveredAt: view.Time(o.DeliveredAt), InvoicedAt: view.Time(o.InvoicedAt), CancelledAt: view.Time(o.CancelledAt),
		CancelReason: view.String(o.CancelReason), HoursApprovedMilli: int64(v.HoursApproved),
		HoursEstimatedMilli: int64(v.HoursEstimated), Lines: lines, Warnings: warnings, Version: int64(o.Version),
		CreatedAt: o.CreatedAt.UTC(), UpdatedAt: o.UpdatedAt.UTC(),
	}
	if o.DepositCents > 0 {
		d := o.DepositCents
		out.DepositCents = &d
	}
	return out
}

func writeOrder(w http.ResponseWriter, v *workorder.OrderView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, woOrder(v))
}

// ListWorkOrders implements api.ServerInterface.
func (h *Handler) ListWorkOrders(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListWorkOrdersParams) {
	f := workorder.OrderFilter{
		OpenOnly: boolOr(params.OpenOnly, false), CustomerID: str(params.CustomerId), VesselID: str(params.VesselId),
		Q: str(params.Q), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	items, next, err := h.WorkOrders.ListOrders(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkOrder, 0, len(items))
	for i := range items {
		out = append(out, woOrder(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkOrderPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateWorkOrder implements api.ServerInterface.
func (h *Handler) CreateWorkOrder(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateWorkOrderParams) {
	var in api.WorkOrderInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	oi := workorder.OrderInput{
		CustomerID: in.CustomerId, VesselID: in.VesselId, WorkTypeID: str(in.WorkTypeId), ResponsibleUserID: str(in.ResponsibleUserId),
		PlannedStartOn: d.parse(str(in.PlannedStartOn)), PlannedDeliveryOn: d.parse(str(in.PlannedDeliveryOn)),
		AllocationID: str(in.AllocationId), PositionNote: str(in.PositionNote), Notes: str(in.Notes),
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.BillingMode != nil {
		oi.BillingMode = string(*in.BillingMode)
	}
	if in.ManualLines != nil {
		for _, l := range *in.ManualLines {
			ml := workorder.ManualLine{
				ItemID: l.ItemId, WorkTypeID: str(l.WorkTypeId), Description: str(l.Description), Qty: qty.Milli(l.QtyMilli),
				Unit: money.Cents(l.UnitCents),
			}
			if l.DiscountCents != nil {
				ml.Discount = money.Cents(*l.DiscountCents)
			}
			oi.ManualLines = append(oi.ManualLines, ml)
		}
	}
	if in.Selections != nil {
		for _, s := range *in.Selections {
			oi.Selections = append(oi.Selections, workorder.Selection{PackageID: str(s.PackageId), ItemID: str(s.ItemId)})
		}
	}
	jobs, err := yardValuesOf(in.JobInputs)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	oi.JobInputs = jobs
	v, err := h.WorkOrders.CreateOrder(r.Context(), principal(r), marinaID, oi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeOrder(w, v, http.StatusCreated)
}

// GetYardBoard implements api.ServerInterface.
func (h *Handler) GetYardBoard(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	b, err := h.WorkOrders.Board(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.YardBoard{Today: b.Today.String(), Columns: make([]api.YardBoardColumn, 0, len(b.Columns))}
	for _, c := range b.Columns {
		col := api.YardBoardColumn{Status: api.WorkOrderStatus(c.Status), Cards: make([]api.YardBoardCard, 0, len(c.Cards))}
		for _, k := range c.Cards {
			col.Cards = append(col.Cards, api.YardBoardCard{
				WorkOrderId: k.Order.ID, Number: workorder.FormatNumber(k.Order.Year, k.Order.Number), VesselId: k.Order.VesselID,
				VesselName: view.String(k.VesselName), LoaCm: k.LOACm, CompletionBp: int64(k.Order.CompletionBP),
				PlannedDeliveryOn: woDate(k.Order.PlannedDeliveryOn), DelayDays: k.DelayDays, Status: api.WorkOrderStatus(k.Order.Status),
				HoursApprovedMilli: int64(k.HoursApproved), HoursEstimatedMilli: int64(k.HoursEstimated),
			})
		}
		out.Columns = append(out.Columns, col)
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetWorkOrder implements api.ServerInterface.
func (h *Handler) GetWorkOrder(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId) {
	v, err := h.WorkOrders.GetOrder(r.Context(), principal(r), marinaID, workOrderID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeOrder(w, v, http.StatusOK)
}

// UpdateWorkOrder implements api.ServerInterface.
func (h *Handler) UpdateWorkOrder(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, params api.UpdateWorkOrderParams) {
	var in api.WorkOrderPatch
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := workorder.OrderPatch{
		ResponsibleUserID: in.ResponsibleUserId, PlannedStartOn: woOptDate(in.PlannedStartOn),
		PlannedDeliveryOn: woOptDate(in.PlannedDeliveryOn), AllocationID: in.AllocationId, PositionNote: in.PositionNote,
		Notes: in.Notes, WorkTypeID: in.WorkTypeId,
	}
	if in.Consumed != nil {
		for _, c := range *in.Consumed {
			patch.Consumed = append(patch.Consumed, workorder.ConsumedPatch{LineID: c.LineId, Consumed: c.ConsumedQtyMilli})
		}
	}
	v, err := h.WorkOrders.UpdateOrder(r.Context(), principal(r), marinaID, workOrderID, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeOrder(w, v, http.StatusOK)
}

// TransitionWorkOrder implements api.ServerInterface.
func (h *Handler) TransitionWorkOrder(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, params api.TransitionWorkOrderParams) {
	var in api.WorkOrderTransition
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.WorkOrders.Transition(r.Context(), principal(r), marinaID, workOrderID, int64(version), string(in.Action), str(in.Reason))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeOrder(w, v, http.StatusOK)
}

// CreateVariationQuote implements api.ServerInterface: a draft variation quote of an open order (the order side
// locks and checks the order, the quote side prices the quote).
func (h *Handler) CreateVariationQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, _ api.CreateVariationQuoteParams) {
	var body api.YardQuoteInput
	if !h.mntDecode(w, r, marinaID, &body, false) {
		return
	}
	in, ok := yardQuoteInput(body)
	if !ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	q, err := h.WorkOrders.CreateVariation(r.Context(), principal(r), marinaID, workOrderID, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusCreated)
}

// ---- tasks ----

func woChecklist(list []workorder.ChecklistItem) []api.WorkTaskChecklistItem {
	out := make([]api.WorkTaskChecklistItem, 0, len(list))
	for _, c := range list {
		out = append(out, api.WorkTaskChecklistItem{Key: c.Key, Label: c.Label, Done: c.Done})
	}
	return out
}

func woTask(v *workorder.TaskView) api.WorkTask {
	t := v.Row
	out := api.WorkTask{
		Id: t.ID, WorkOrderId: t.WorkOrderID, Seq: t.Seq, Title: t.Title, QualificationId: view.String(t.QualificationID),
		WorkTypeId: view.String(t.WorkTypeID), EstimatedHoursMilli: t.EstimatedHoursMilli, Status: api.WorkTaskStatus(t.Status),
		ProgressBp: int64(t.ProgressBP), Checklist: woChecklist(workorder.ChecklistOf(t.Checklist)), DependsOn: v.DependsOn,
		PlannedFrom: woDate(t.PlannedFrom), PlannedTo: woDate(t.PlannedTo), SourceLineId: view.String(t.SourceLineID),
		HoursApprovedMilli: int64(v.HoursApproved), HoursAssignedMilli: int64(v.HoursAssigned), Version: int64(t.Version),
		CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC(),
	}
	if out.DependsOn == nil {
		out.DependsOn = []string{}
	}
	if len(v.Warnings) > 0 {
		ws := v.Warnings
		out.Warnings = &ws
	}
	return out
}

func writeTaskView(w http.ResponseWriter, v *workorder.TaskView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, woTask(v))
}

// ListWorkTasks implements api.ServerInterface.
func (h *Handler) ListWorkTasks(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId) {
	items, err := h.WorkOrders.ListTasks(r.Context(), principal(r), marinaID, workOrderID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkTask, 0, len(items))
	for i := range items {
		out = append(out, woTask(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkTaskList{Items: out})
}

// CreateWorkTask implements api.ServerInterface.
func (h *Handler) CreateWorkTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, _ api.CreateWorkTaskParams) {
	var in api.WorkTaskInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	ti := workorder.TaskInput{Title: in.Title, QualificationID: str(in.QualificationId), WorkTypeID: str(in.WorkTypeId)}
	if in.EstimatedHoursMilli != nil {
		ti.EstimatedHours = qty.Milli(*in.EstimatedHoursMilli)
	}
	if in.Checklist != nil {
		ti.Checklist = *in.Checklist
	}
	if in.DependsOn != nil {
		ti.DependsOn = *in.DependsOn
	}
	v, err := h.WorkOrders.CreateTask(r.Context(), principal(r), marinaID, workOrderID, ti)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTaskView(w, v, http.StatusCreated)
}

// UpdateWorkTask implements api.ServerInterface.
func (h *Handler) UpdateWorkTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, params api.UpdateWorkTaskParams) {
	var in api.WorkTaskPatch
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := workorder.TaskPatch{Title: in.Title, QualificationID: in.QualificationId, WorkTypeID: in.WorkTypeId}
	if in.EstimatedHoursMilli != nil {
		h := qty.Milli(*in.EstimatedHoursMilli)
		patch.EstimatedHours = &h
	}
	if in.Checklist != nil {
		cl := make([]workorder.ChecklistItem, 0, len(*in.Checklist))
		for _, c := range *in.Checklist {
			cl = append(cl, workorder.ChecklistItem{Key: c.Key, Label: c.Label, Done: c.Done})
		}
		patch.Checklist = &cl
	}
	if in.Status != nil {
		st := string(*in.Status)
		patch.Status = &st
	}
	v, err := h.WorkOrders.UpdateTask(r.Context(), principal(r), marinaID, taskID, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTaskView(w, v, http.StatusOK)
}

// DeleteWorkTask implements api.ServerInterface.
func (h *Handler) DeleteWorkTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, params api.DeleteWorkTaskParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.WorkOrders.DeleteTask(r.Context(), principal(r), marinaID, taskID, int64(version)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// SetTaskDependencies implements api.ServerInterface.
func (h *Handler) SetTaskDependencies(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, params api.SetTaskDependenciesParams) {
	var in api.TaskDependenciesInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.WorkOrders.SetDependencies(r.Context(), principal(r), marinaID, taskID, int64(version), in.DependsOn)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTaskView(w, v, http.StatusOK)
}

// UpdateTaskProgress implements api.ServerInterface (offline kind yard.activity_done).
func (h *Handler) UpdateTaskProgress(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, _ api.UpdateTaskProgressParams) {
	var in api.TaskProgressInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	pi := workorder.ProgressInput{ClientRef: in.ClientRef, ProgressBP: in.ProgressBp, Done: in.Done, RecordedAt: in.RecordedAt}
	if in.Checklist != nil {
		for _, c := range *in.Checklist {
			pi.Checklist = append(pi.Checklist, workorder.ChecklistMark{Key: c.Key, Done: c.Done})
		}
	}
	v, err := h.WorkOrders.UpdateProgress(r.Context(), principal(r), marinaID, taskID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTaskView(w, v, http.StatusOK)
}

func woFile(f *workorder.TaskFile) api.WorkTaskFile {
	return api.WorkTaskFile{FileId: f.FileID, Kind: api.WorkTaskFileKind(f.Kind), ContentType: f.ContentType, CreatedAt: f.CreatedAt.UTC()}
}

// UploadTaskPhoto implements api.ServerInterface (offline kind yard.activity_done).
func (h *Handler) UploadTaskPhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, params api.UploadTaskPhotoParams) {
	data, err := io.ReadAll(io.LimitReader(r.Body, workorder.MaxFileBytes+1))
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	f, err := h.WorkOrders.UploadFile(r.Context(), principal(r), marinaID, taskID, workorder.FileUpload{
		ContentType: r.Header.Get("Content-Type"), Data: data, ClientRef: str(params.IdempotencyKey),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, woFile(f))
}

// ListTaskFiles implements api.ServerInterface.
func (h *Handler) ListTaskFiles(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId) {
	files, err := h.WorkOrders.ListFiles(r.Context(), principal(r), marinaID, taskID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkTaskFile, 0, len(files))
	for i := range files {
		out = append(out, woFile(&files[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkTaskFileList{Items: out})
}

// GetTaskFileLink implements api.ServerInterface.
func (h *Handler) GetTaskFileLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, fileID api.FileId) {
	l, err := h.WorkOrders.FileLink(r.Context(), principal(r), marinaID, taskID, fileID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.FileLink{Url: l.URL, ExpiresAt: l.ExpiresAt})
}

// GetTaskFileFile implements api.ServerInterface.
func (h *Handler) GetTaskFileFile(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, taskID api.TaskId, fileID api.FileId, params api.GetTaskFileFileParams) {
	exp, sig := signedParams(params.Exp, params.Sig)
	f, err := h.WorkOrders.OpenFile(r.Context(), principal(r), marinaID, taskID, fileID, exp, sig)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	serveStoredFile(w, f.ContentType, f.FileName, f.Content)
}

// ---- external costs ----

func woExternal(c *workorder.ExternalCostRow) api.ExternalCost {
	return api.ExternalCost{
		Id: c.ID, WorkOrderId: c.WorkOrderID, SupplierName: c.SupplierName, Reference: c.Reference, IncurredOn: c.IncurredOn.String(),
		Description: c.Description, WorkTypeId: view.String(c.WorkTypeID), CostCents: c.CostCents, Billable: c.Billable,
		PriceCents: c.PriceCents, ItemId: view.String(c.ItemID), Late: c.Late, Version: int64(c.Version),
		CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC(),
	}
}

func writeExternal(w http.ResponseWriter, c *workorder.ExternalCostRow, status int) {
	respond.SetETag(w, c.Version)
	respond.JSON(w, status, woExternal(c))
}

// ListExternalCosts implements api.ServerInterface.
func (h *Handler) ListExternalCosts(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId) {
	items, err := h.WorkOrders.ListExternal(r.Context(), principal(r), marinaID, workOrderID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.ExternalCost, 0, len(items))
	for i := range items {
		out = append(out, woExternal(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.ExternalCostList{Items: out})
}

// CreateExternalCost implements api.ServerInterface.
func (h *Handler) CreateExternalCost(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, _ api.CreateExternalCostParams) {
	var in api.ExternalCostInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	ei := workorder.ExternalInput{
		SupplierName: in.SupplierName, Reference: str(in.Reference), IncurredOn: d.parse(str(in.IncurredOn)),
		Description: str(in.Description), WorkTypeID: str(in.WorkTypeId), Cost: money.Cents(in.CostCents), Billable: in.Billable,
		ItemID: str(in.ItemId),
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.PriceCents != nil {
		p := money.Cents(*in.PriceCents)
		ei.Price = &p
	}
	c, err := h.WorkOrders.CreateExternal(r.Context(), principal(r), marinaID, workOrderID, ei)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeExternal(w, c, http.StatusCreated)
}

// UpdateExternalCost implements api.ServerInterface.
func (h *Handler) UpdateExternalCost(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, externalCostID api.ExternalCostId, params api.UpdateExternalCostParams) {
	var in api.ExternalCostPatch
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	d := newCtrDates()
	patch := workorder.ExternalPatch{
		SupplierName: in.SupplierName, Reference: in.Reference, IncurredOn: d.ptr(in.IncurredOn), Description: in.Description,
		WorkTypeID: in.WorkTypeId, Billable: in.Billable, ItemID: in.ItemId,
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.CostCents != nil {
		c := money.Cents(*in.CostCents)
		patch.Cost = &c
	}
	if in.PriceCents != nil {
		p := money.Cents(*in.PriceCents)
		patch.Price = &p
	}
	c, err := h.WorkOrders.UpdateExternal(r.Context(), principal(r), marinaID, externalCostID, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeExternal(w, c, http.StatusOK)
}

// DeleteExternalCost implements api.ServerInterface.
func (h *Handler) DeleteExternalCost(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, externalCostID api.ExternalCostId, params api.DeleteExternalCostParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.WorkOrders.DeleteExternal(r.Context(), principal(r), marinaID, externalCostID, int64(version)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- final account and billings ----

func woAccountJSON(v *workorder.AccountView) api.FinalAccount {
	a := v.Account
	lines := make([]api.FinalAccountLine, 0, len(a.Lines))
	for _, l := range a.Lines {
		al := api.FinalAccountLine{
			Key: l.Key, Source: api.FinalAccountSource(l.Source), ItemId: view.String(l.ItemID), ItemCode: view.String(v.Items[l.ItemID].Code),
			Description: view.String(v.Descriptions[l.Key]), QualificationId: view.String(l.QualificationID), WorkTypeId: l.WorkTypeID,
			QtyMilli: int64(l.Qty), RevenueCents: int64(l.Revenue), CostCents: int64(l.Cost), MarginCents: int64(l.Margin),
			MarginBp: l.MarginBP, VatRateBp: l.VAT.RateBP, VatCents: int64(l.VATCents), GrossCents: int64(l.Gross),
			BudgetQtyMilli: int64(l.BudgetQty), BudgetNetCents: int64(l.BudgetNet), DeltaNetCents: int64(l.DeltaNet),
		}
		if l.VAT.Nature != "" {
			n := api.VatNature(l.VAT.Nature)
			al.VatNature = &n
		}
		lines = append(lines, al)
	}
	t := a.Totals
	byWT := make([]api.FinalAccountWorkType, 0, len(a.ByWorkType))
	for _, w := range a.ByWorkType {
		byWT = append(byWT, api.FinalAccountWorkType{
			WorkTypeId: w.WorkTypeID, RevenueCents: int64(w.Revenue), CostCents: int64(w.Cost), MarginCents: int64(w.Margin), MarginBp: w.MarginBP,
		})
	}
	vat := make([]api.YardVatBucket, 0, len(v.Buckets))
	for _, b := range v.Buckets {
		yb := api.YardVatBucket{VatRateBp: b.RateBP, NetCents: int64(b.Net), VatCents: int64(b.VAT), GrossCents: int64(b.Gross)}
		if b.Nature != "" {
			n := api.VatNature(b.Nature)
			yb.VatNature = &n
		}
		vat = append(vat, yb)
	}
	return api.FinalAccount{
		WorkOrderId: v.Order.ID, BillingMode: api.YardBillingMode(v.Order.BillingMode), Lines: lines, ByWorkType: byWT, Vat: vat,
		QuoteNetCents: int64(a.QuoteNet), DeltaNetCents: int64(a.DeltaNet), ChargedGrossCents: int64(v.ChargedGross),
		RemainingGrossCents: int64(v.RemainingGross),
		Totals: api.FinalAccountTotals{
			RevenueCents: int64(t.Revenue), CostCents: int64(t.Cost), MarginCents: int64(t.Margin), MarginBp: t.MarginBP,
			RevenueLabourCents: int64(t.RevenueLabour), RevenueMaterialCents: int64(t.RevenueMaterial),
			RevenueServiceCents: int64(t.RevenueService), RevenueExternalCents: int64(t.RevenueExt),
			CostLabourCents: int64(t.CostLabour), CostMaterialCents: int64(t.CostMaterial), CostServiceCents: int64(t.CostService),
			CostExternalCents: int64(t.CostExt), HoursLabourMilli: int64(t.HoursLabour), VatCents: int64(t.VATCents),
			GrossCents: int64(t.Gross),
		},
	}
}

// woAccountTable is the final account as a table (CSV/XLSX, Italian formats of report/export).
func woAccountTable(v *workorder.AccountView, lang string) export.Table {
	it := lang != "en"
	label := func(itl, enl string) string {
		if it {
			return itl
		}
		return enl
	}
	cols := []export.Column{
		{Key: "key", Label: label("Chiave", "Key"), Type: export.TypeText},
		{Key: "source", Label: label("Fonte", "Source"), Type: export.TypeText},
		{Key: "item_code", Label: label("Articolo", "Item"), Type: export.TypeText},
		{Key: "description", Label: label("Descrizione", "Description"), Type: export.TypeText},
		{Key: "qty_milli", Label: label("Quantità", "Quantity"), Type: export.TypeMilli},
		{Key: "revenue_cents", Label: label("Ricavo", "Revenue"), Type: export.TypeCents},
		{Key: "cost_cents", Label: label("Costo", "Cost"), Type: export.TypeCents},
		{Key: "margin_cents", Label: label("Margine", "Margin"), Type: export.TypeCents},
		{Key: "margin_bp", Label: label("Margine %", "Margin %"), Type: export.TypeBP},
		{Key: "vat_cents", Label: label("IVA", "VAT"), Type: export.TypeCents},
		{Key: "budget_qty_milli", Label: label("Quantità preventivo", "Quote quantity"), Type: export.TypeMilli}, //nolint:misspell // Italian
		{Key: "budget_net_cents", Label: label("Imponibile preventivo", "Quote net"), Type: export.TypeCents},    //nolint:misspell // Italian
		{Key: "delta_net_cents", Label: label("Scostamento", "Delta"), Type: export.TypeCents},
	}
	bp := func(p *int64) export.Value {
		if p == nil {
			return export.Null()
		}
		return export.Int(*p)
	}
	rows := make([][]export.Value, 0, len(v.Account.Lines))
	for _, l := range v.Account.Lines {
		rows = append(rows, []export.Value{
			export.Text(l.Key), export.Text(l.Source), export.Text(v.Items[l.ItemID].Code), export.Text(v.Descriptions[l.Key]),
			export.Int(int64(l.Qty)), export.Int(int64(l.Revenue)), export.Int(int64(l.Cost)), export.Int(int64(l.Margin)),
			bp(l.MarginBP), export.Int(int64(l.VATCents)), export.Int(int64(l.BudgetQty)), export.Int(int64(l.BudgetNet)),
			export.Int(int64(l.DeltaNet)),
		})
	}
	t := v.Account.Totals
	total := []export.Value{
		export.Text(label("Totale", "Total")), export.Null(), export.Null(), export.Null(), export.Int(int64(t.HoursLabour)),
		export.Int(int64(t.Revenue)), export.Int(int64(t.Cost)), export.Int(int64(t.Margin)), bp(t.MarginBP),
		export.Int(int64(t.VATCents)), export.Null(), export.Int(int64(v.Account.QuoteNet)), export.Int(int64(v.Account.DeltaNet)),
	}
	byWT := export.Sheet{Name: label("Per lavorazione", "By work type"), Columns: []export.Column{
		{Key: "work_type_id", Label: label("Lavorazione", "Work type"), Type: export.TypeText},
		{Key: "revenue_cents", Label: label("Ricavo", "Revenue"), Type: export.TypeCents},
		{Key: "cost_cents", Label: label("Costo", "Cost"), Type: export.TypeCents},
		{Key: "margin_cents", Label: label("Margine", "Margin"), Type: export.TypeCents},
		{Key: "margin_bp", Label: label("Margine %", "Margin %"), Type: export.TypeBP},
	}}
	for _, w := range v.Account.ByWorkType {
		byWT.Rows = append(byWT.Rows, []export.Value{
			export.Text(w.WorkTypeID), export.Int(int64(w.Revenue)), export.Int(int64(w.Cost)), export.Int(int64(w.Margin)), bp(w.MarginBP),
		})
	}
	return export.Table{Report: calc.ReportKey("work-order-account"), Columns: cols, Rows: rows, Total: total, Summaries: []export.Sheet{byWT}}
}

// GetFinalAccount implements api.ServerInterface (JSON, or CSV/XLSX with ?format=).
func (h *Handler) GetFinalAccount(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, params api.GetFinalAccountParams) {
	v, err := h.WorkOrders.Account(r.Context(), principal(r), marinaID, workOrderID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}
	if format == "" || format == "json" {
		respond.JSON(w, http.StatusOK, woAccountJSON(v))
		return
	}
	lang := "it"
	if p := principal(r); p != nil && p.Locale == "en" {
		lang = "en"
	}
	t := woAccountTable(v, lang)
	var buf bytes.Buffer
	ct, ext := "text/csv; charset=utf-8", "csv"
	if format == "xlsx" {
		ct, ext = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"
		err = export.WriteXLSX(&buf, t)
	} else {
		err = export.WriteCSV(&buf, t)
	}
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	name := "porto-consuntivo-" + workorder.FormatNumber(v.Order.Year, v.Order.Number) + "." + ext
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func woBilling(v *workorder.BillingView) api.WorkOrderBilling {
	b := v.Row
	return api.WorkOrderBilling{
		Id: b.ID, WorkOrderId: b.WorkOrderID, Seq: b.Seq, Kind: api.WorkOrderBillingKind(b.Kind), Status: api.WorkOrderBillingStatus(b.Status),
		ChargeId: view.String(b.ChargeID), ChargeNumber: view.String(v.ChargeNumber), GrossCents: b.GrossCents,
		PercentBp: v.PercentBP, AmountCents: (*int64)(v.Amount), CreatedAt: b.CreatedAt.UTC(),
	}
}

// ListWorkOrderBillings implements api.ServerInterface.
func (h *Handler) ListWorkOrderBillings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId) {
	items, charged, err := h.WorkOrders.ListBillings(r.Context(), principal(r), marinaID, workOrderID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkOrderBilling, 0, len(items))
	for i := range items {
		out = append(out, woBilling(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkOrderBillingList{Items: out, ChargedGrossCents: int64(charged)})
}

// CreateWorkOrderBilling implements api.ServerInterface (dry_run=true: preview, 200, nothing written).
func (h *Handler) CreateWorkOrderBilling(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workOrderID api.WorkOrderId, params api.CreateWorkOrderBillingParams) {
	var in api.WorkOrderBillingInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	bi := workorder.BillingInput{Kind: string(in.Kind), PercentBP: in.PercentBp, DueOn: d.parse(str(in.DueOn))}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.AmountCents != nil {
		a := money.Cents(*in.AmountCents)
		bi.Amount = &a
	}
	dry := boolOr(params.DryRun, false)
	res, err := h.WorkOrders.CreateBilling(r.Context(), principal(r), marinaID, workOrderID, bi, dry)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.WorkOrderBillingResult{
		DryRun: res.DryRun, Kind: api.WorkOrderBillingKind(res.Kind), GrossCents: int64(res.Gross),
		ChargeId: view.String(res.ChargeID), CorrectsChargeId: view.String(res.CorrectsChargeID),
		Lines: make([]api.WorkOrderBillingPreviewLine, 0, len(res.Lines)),
	}
	for _, l := range res.Lines {
		out.Lines = append(out.Lines, api.WorkOrderBillingPreviewLine{
			PlanKey: l.PlanKey, Description: l.Description, NetCents: int64(l.Net), VatCents: int64(l.VAT), GrossCents: int64(l.Gross),
			VatRateBp: l.VATRateBP,
		})
	}
	if res.Billing != nil {
		b := woBilling(res.Billing)
		out.Billing = &b
	}
	status := http.StatusCreated
	if dry {
		status = http.StatusOK
	}
	respond.JSON(w, status, out)
}
