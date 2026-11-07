package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Millesimal tables, common charge budgets, items, runs, statements and holder instalment handlers
// (m4-plan §7.3.2).

// ---- millesimal tables ----

// ListMillesimalTables implements api.ServerInterface (GET .../millesimal-tables).
func (h *Handler) ListMillesimalTables(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Rights.ListMillesimalTables(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.MillesimalTableList{Items: make([]api.MillesimalTable, len(list))}
	for i := range list {
		out.Items[i] = view.MillesimalTable(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateMillesimalTable implements api.ServerInterface (POST .../millesimal-tables).
func (h *Handler) CreateMillesimalTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateMillesimalTableParams) {
	var in api.MillesimalTableInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.CreateMillesimalTable(r.Context(), principal(r), marinaID, in.Name, in.Code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.MillesimalTable(out))
}

// GetMillesimalTable implements api.ServerInterface (GET .../millesimal-tables/{millesimalTableId}).
func (h *Handler) GetMillesimalTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId) {
	out, err := h.Rights.GetMillesimalTable(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.MillesimalTable(out))
}

// UpdateMillesimalTable implements api.ServerInterface (PATCH .../millesimal-tables/{millesimalTableId}).
func (h *Handler) UpdateMillesimalTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId, params api.UpdateMillesimalTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.MillesimalTablePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.UpdateMillesimalTable(r.Context(), principal(r), marinaID, id, version, in.Name, in.Status != nil)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.MillesimalTable(out))
}

// DeleteMillesimalTable implements api.ServerInterface (DELETE .../millesimal-tables/{millesimalTableId}).
func (h *Handler) DeleteMillesimalTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId, params api.DeleteMillesimalTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Rights.DeleteMillesimalTable(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ActivateMillesimalTable implements api.ServerInterface (POST .../millesimal-tables/{millesimalTableId}/activate).
func (h *Handler) ActivateMillesimalTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId, params api.ActivateMillesimalTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.ActivateMillesimalTable(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.MillesimalTable(out))
}

// GetMillesimalEntries implements api.ServerInterface (GET .../millesimal-tables/{millesimalTableId}/entries).
func (h *Handler) GetMillesimalEntries(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId, params api.GetMillesimalEntriesParams) {
	out, err := h.Rights.GetMillesimalEntries(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if params.Format != nil && *params.Format == api.GetMillesimalEntriesParamsFormatCsv {
		rows := make([][]string, len(out.Entries))
		for i, e := range out.Entries {
			rows[i] = []string{e.BerthCode, strconv.FormatInt(e.Millis, 10)}
		}
		rcCSV(w, "millesimal-entries.csv", []string{"berth_code", "millis"}, rows)
		return
	}
	respond.JSON(w, http.StatusOK, view.MillesimalEntries(out))
}

// ReplaceMillesimalEntries implements api.ServerInterface (PUT .../millesimal-tables/{millesimalTableId}/entries).
func (h *Handler) ReplaceMillesimalEntries(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MillesimalTableId, params api.ReplaceMillesimalEntriesParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.MillesimalEntriesInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ei := rights.EntriesInput{CSV: in.Csv}
	if in.Entries != nil {
		for _, e := range *in.Entries {
			ei.Entries = append(ei.Entries, rights.MillesimalEntry{BerthID: e.BerthId, Millis: e.Millis})
		}
	}
	out, err := h.Rights.ReplaceMillesimalEntries(r.Context(), principal(r), marinaID, id, version, ei)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if t, err := h.Rights.GetMillesimalTable(r.Context(), principal(r), marinaID, id); err == nil {
		respond.SetETag(w, t.Version)
	}
	respond.JSON(w, http.StatusOK, view.MillesimalEntries(out))
}

// ---- budgets ----

// ListCommonChargeBudgets implements api.ServerInterface (GET .../common-charge-budgets).
func (h *Handler) ListCommonChargeBudgets(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Rights.ListBudgets(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.BudgetList{Items: make([]api.Budget, len(list))}
	for i := range list {
		out.Items[i] = view.Budget(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateCommonChargeBudget implements api.ServerInterface (POST .../common-charge-budgets).
func (h *Handler) CreateCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateCommonChargeBudgetParams) {
	var in api.BudgetInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.CreateBudget(r.Context(), principal(r), marinaID, in.Year, str(in.Name))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Budget(out))
}

// GetCommonChargeBudget implements api.ServerInterface (GET .../common-charge-budgets/{budgetId}).
func (h *Handler) GetCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId) {
	out, err := h.Rights.GetBudget(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.Budget(out))
}

func (h *Handler) writeBudget(w http.ResponseWriter, r *http.Request, b *rights.Budget, err error) {
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, b.Version)
	respond.JSON(w, http.StatusOK, view.Budget(b))
}

// UpdateCommonChargeBudget implements api.ServerInterface (PATCH .../common-charge-budgets/{budgetId}).
func (h *Handler) UpdateCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, params api.UpdateCommonChargeBudgetParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.BudgetPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	b, err := h.Rights.UpdateBudget(r.Context(), principal(r), marinaID, id, version, in.Name)
	h.writeBudget(w, r, b, err)
}

// DeleteCommonChargeBudget implements api.ServerInterface (DELETE .../common-charge-budgets/{budgetId}).
func (h *Handler) DeleteCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, params api.DeleteCommonChargeBudgetParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Rights.DeleteBudget(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ApproveCommonChargeBudget implements api.ServerInterface (POST .../common-charge-budgets/{budgetId}/approve).
func (h *Handler) ApproveCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, params api.ApproveCommonChargeBudgetParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	b, err := h.Rights.ApproveBudget(r.Context(), principal(r), marinaID, id, version)
	h.writeBudget(w, r, b, err)
}

// CloseCommonChargeBudget implements api.ServerInterface (POST .../common-charge-budgets/{budgetId}/close).
func (h *Handler) CloseCommonChargeBudget(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, params api.CloseCommonChargeBudgetParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	b, err := h.Rights.CloseBudget(r.Context(), principal(r), marinaID, id, version)
	h.writeBudget(w, r, b, err)
}

// SetBudgetActuals implements api.ServerInterface (PUT .../common-charge-budgets/{budgetId}/actuals).
func (h *Handler) SetBudgetActuals(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, params api.SetBudgetActualsParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ActualsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	var actuals []rights.Actual
	if in.Items != nil {
		for _, it := range *in.Items {
			actuals = append(actuals, rights.Actual{ItemID: it.ItemId, ActualCents: it.ActualCents})
		}
	}
	items, err := h.Rights.SetBudgetActuals(r.Context(), principal(r), marinaID, id, version, actuals, in.Csv)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if b, err := h.Rights.GetBudget(r.Context(), principal(r), marinaID, id); err == nil {
		respond.SetETag(w, b.Version)
	}
	respond.JSON(w, http.StatusOK, view.BudgetItems(items))
}

// ListBudgetItems implements api.ServerInterface (GET .../common-charge-budgets/{budgetId}/items).
func (h *Handler) ListBudgetItems(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId) {
	items, err := h.Rights.ListBudgetItems(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.BudgetItems(items))
}

// CreateBudgetItem implements api.ServerInterface (POST .../common-charge-budgets/{budgetId}/items).
func (h *Handler) CreateBudgetItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, _ api.CreateBudgetItemParams) {
	var in api.BudgetItemInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.CreateBudgetItem(r.Context(), principal(r), marinaID, id, rights.ItemInput{
		Name: &in.Name, TableID: &in.TableId, BudgetCents: &in.BudgetCents, Sort: in.Sort,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.BudgetItem(out))
}

// UpdateBudgetItem implements api.ServerInterface (PATCH .../common-charge-budgets/{budgetId}/items/{budgetItemId}).
func (h *Handler) UpdateBudgetItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, budgetID api.BudgetId, id api.BudgetItemId, params api.UpdateBudgetItemParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.BudgetItemPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	out, err := h.Rights.UpdateBudgetItem(r.Context(), principal(r), marinaID, budgetID, id, version, rights.ItemInput{
		Name: in.Name, TableID: in.TableId, BudgetCents: in.BudgetCents, Sort: in.Sort,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.BudgetItem(out))
}

// DeleteBudgetItem implements api.ServerInterface (DELETE .../common-charge-budgets/{budgetId}/items/{budgetItemId}).
func (h *Handler) DeleteBudgetItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, budgetID api.BudgetId, id api.BudgetItemId, params api.DeleteBudgetItemParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Rights.DeleteBudgetItem(r.Context(), principal(r), marinaID, budgetID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ---- runs ----

// ListCommonChargeRuns implements api.ServerInterface (GET .../common-charge-budgets/{budgetId}/runs).
func (h *Handler) ListCommonChargeRuns(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId) {
	list, err := h.Rights.ListCommonRuns(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.CommonChargeRunList{Items: make([]api.CommonChargeRun, len(list))}
	for i := range list {
		out.Items[i] = view.CommonRun(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateCommonChargeRun implements api.ServerInterface (POST .../common-charge-budgets/{budgetId}/runs).
func (h *Handler) CreateCommonChargeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.BudgetId, _ api.CreateCommonChargeRunParams) {
	var in api.CommonRunInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ri := rights.CommonRunInput{Kind: string(in.Kind), ShareBP: in.ShareBp}
	if in.Plan != nil {
		ri.Plan, _ = json.Marshal(in.Plan)
	}
	out, err := h.Rights.CreateCommonRun(r.Context(), principal(r), marinaID, id, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.CommonRun(out))
}

// GetCommonChargeRun implements api.ServerInterface (GET .../common-charge-runs/{commonRunId}).
func (h *Handler) GetCommonChargeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommonRunId) {
	out, err := h.Rights.GetCommonRun(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.CommonRun(out))
}

// IssueCommonChargeRun implements api.ServerInterface (POST .../common-charge-runs/{commonRunId}/issue).
func (h *Handler) IssueCommonChargeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommonRunId, params api.IssueCommonChargeRunParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.IssueCommonRun(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.CommonRun(out))
}

// DiscardCommonChargeRun implements api.ServerInterface (POST .../common-charge-runs/{commonRunId}/discard).
func (h *Handler) DiscardCommonChargeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommonRunId, params api.DiscardCommonChargeRunParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.DiscardCommonRun(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.CommonRun(out))
}

// ListCommonChargeAllocations implements api.ServerInterface (GET .../common-charge-runs/{commonRunId}/allocations).
func (h *Handler) ListCommonChargeAllocations(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommonRunId, params api.ListCommonChargeAllocationsParams) {
	ctx, p := r.Context(), principal(r)
	f := rights.CommonAllocationFilter{ItemID: str(params.ItemId), CustomerID: str(params.CustomerId)}
	if params.Format != nil && *params.Format == api.ListCommonChargeAllocationsParamsFormatCsv {
		list, err := h.Rights.AllCommonAllocations(ctx, p, marinaID, id, f)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		rows := make([][]string, len(list))
		for i, a := range list {
			rows[i] = []string{a.ItemID, a.BerthCode, a.RightID, a.CustomerID, rcCents(a.BerthShareCents), rcCents(a.AmountCents)}
		}
		rcCSV(w, "common-charge-allocations.csv", []string{"item_id", "berth_code", "right_id", "customer_id", "berth_share", "amount"}, rows)
		return
	}
	page, err := h.Rights.ListCommonAllocations(ctx, p, marinaID, id, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.CommonAllocationPage{Items: make([]api.CommonAllocation, len(page.Items)), NextCursor: view.String(page.NextCursor)}
	for i := range page.Items {
		out.Items[i] = view.CommonAllocation(&page.Items[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetCommonChargeStatements implements api.ServerInterface (GET .../common-charge-runs/{commonRunId}/statements):
// by holder (default; one holder with customer_id) or by item, as PDF (default) or CSV.
func (h *Handler) GetCommonChargeStatements(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommonRunId, params api.GetCommonChargeStatementsParams) {
	ctx, p := r.Context(), principal(r)
	asCSV := params.Format != nil && *params.Format == api.GetCommonChargeStatementsParamsFormatCsv
	if params.By != nil && *params.By == api.GetCommonChargeStatementsParamsByItem {
		v, err := h.Rights.ItemsStatement(ctx, p, marinaID, id)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		if asCSV {
			rows := make([][]string, len(v.Items))
			for i, it := range v.Items {
				rows[i] = []string{it.Item, it.Table, rcCents(int64(it.Amount)), rcCents(int64(it.Allocated)), rcCents(int64(it.MarinaShare))}
			}
			rcCSV(w, "common-charges-items.csv", []string{"item", "table", "amount", "allocated", "marina_share"}, rows)
			return
		}
		h.rcPDF(w, r, marinaID, "common-charges-items.pdf", docs.KindCommonChargesItems, v)
		return
	}
	list, err := h.Rights.HolderStatements(ctx, p, marinaID, id, str(params.CustomerId))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if asCSV {
		var rows [][]string
		for _, st := range list {
			for _, l := range st.Lines {
				rows = append(rows, []string{
					st.Holder.DisplayName, l.Item, l.Table, l.Berth, strconv.FormatInt(int64(l.Millis), 10),
					rcCents(int64(l.ItemAmount)), rcCents(int64(l.BerthShare)), rcCents(int64(l.HolderAmount)),
				})
			}
		}
		rcCSV(w, "common-charges-holders.csv", []string{"holder", "item", "table", "berth", "millis", "item_amount", "berth_share", "holder_amount"}, rows)
		return
	}
	if len(list) != 1 {
		v := &auth.ValidationError{}
		v.Add("customer_id", "required")
		respond.Error(w, r, v)
		return
	}
	one := list[0]
	h.rcPDF(w, r, marinaID, "common-charges-holder.pdf", docs.KindCommonChargesHolder, one)
}

// ---- holder instalments ----

// ListHolderInstallments implements api.ServerInterface (GET .../holder-installments).
func (h *Handler) ListHolderInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListHolderInstallmentsParams) {
	v := &auth.ValidationError{}
	f := rights.HolderInstallmentFilter{
		From: rcOptDate(v, "from", params.From), To: rcOptDate(v, "to", params.To),
		CustomerID: str(params.CustomerId), RightID: str(params.RightId),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.SourceType != nil {
		f.SourceType = string(*params.SourceType)
	}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format == api.ListHolderInstallmentsParamsFormatCsv {
		list, err := h.Rights.AllHolderInstallments(ctx, p, marinaID, f)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		rows := make([][]string, len(list))
		for i, x := range list {
			rows[i] = []string{
				x.ID, x.SourceType, x.CustomerID, x.DisplayName, strconv.Itoa(x.Seq), x.Kind, x.IssueOn.String(), x.DueOn.String(),
				rcCents(x.AmountCents), x.Status, strconv.FormatBool(x.Overdue),
			}
		}
		rcCSV(w, "holder-installments.csv", []string{
			"id", "source_type", "customer_id", "holder", "seq", "kind", "issue_on", "due_on",
			"amount", "status", "overdue",
		}, rows)
		return
	}
	page, err := h.Rights.ListHolderInstallments(ctx, p, marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.HolderInstallmentPage{Items: make([]api.HolderInstallment, len(page.Items)), NextCursor: view.String(page.NextCursor)}
	for i := range page.Items {
		out.Items[i] = view.HolderInstallment(&page.Items[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// SettleHolderInstallment implements api.ServerInterface (POST .../holder-installments/{holderInstallmentId}/settle).
func (h *Handler) SettleHolderInstallment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.HolderInstallmentId, params api.SettleHolderInstallmentParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.SettleInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	si := rights.SettleInput{Method: string(in.Method), Reference: str(in.Reference), SettledOn: rcOptDate(v, "settled_on", in.SettledOn)}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	out, err := h.Rights.SettleHolderInstallment(r.Context(), principal(r), marinaID, id, version, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.HolderInstallment(out))
}

// SettleHolderInstallments implements api.ServerInterface (POST .../holder-installments/settle).
func (h *Handler) SettleHolderInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.SettleHolderInstallmentsParams) {
	var in api.BulkSettleInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	si := rights.SettleInput{Method: string(in.Method), Reference: str(in.Reference), SettledOn: rcOptDate(v, "settled_on", in.SettledOn)}
	if v.Err() != nil {
		h.rcInvalid(w, r, marinaID, v)
		return
	}
	items := make([]rights.SettleItem, len(in.Items))
	for i, it := range in.Items {
		items[i] = rights.SettleItem{ID: it.Id, Version: it.Version}
	}
	n, err := h.Rights.SettleHolderInstallments(r.Context(), principal(r), marinaID, items, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.BulkSettleResult{Settled: n})
}
