package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Management fee table and run handlers (m4-plan §7.3.2).

// ListFeeTables implements api.ServerInterface (GET .../fee-tables).
func (h *Handler) ListFeeTables(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListFeeTablesParams) {
	list, err := h.Rights.ListFeeTables(r.Context(), principal(r), marinaID, intOr(params.Year, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.FeeTableList{Items: make([]api.FeeTable, len(list))}
	for i := range list {
		out.Items[i] = view.FeeTable(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateFeeTable implements api.ServerInterface (POST .../fee-tables).
func (h *Handler) CreateFeeTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateFeeTableParams) {
	var in api.FeeTableInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	fi := rights.FeeTableInput{Year: in.Year, Name: in.Name, Basis: string(in.Basis), Rows: view.FeeTableRowsIn(in.Rows)}
	if in.IstatBp != nil {
		fi.IstatBP = *in.IstatBp
	}
	out, err := h.Rights.CreateFeeTable(r.Context(), principal(r), marinaID, fi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.FeeTable(out))
}

// GetFeeTable implements api.ServerInterface (GET .../fee-tables/{feeTableId}).
func (h *Handler) GetFeeTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeTableId) {
	out, err := h.Rights.GetFeeTable(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeTable(out))
}

// UpdateFeeTable implements api.ServerInterface (PATCH .../fee-tables/{feeTableId}).
func (h *Handler) UpdateFeeTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeTableId, params api.UpdateFeeTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.FeeTablePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	patch := rights.FeeTablePatch{Name: in.Name, IstatBP: in.IstatBp}
	if in.Rows != nil {
		rows := view.FeeTableRowsIn(*in.Rows)
		patch.Rows = &rows
	}
	out, err := h.Rights.UpdateFeeTable(r.Context(), principal(r), marinaID, id, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeTable(out))
}

// DeleteFeeTable implements api.ServerInterface (DELETE .../fee-tables/{feeTableId}).
func (h *Handler) DeleteFeeTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeTableId, params api.DeleteFeeTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Rights.DeleteFeeTable(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ActivateFeeTable implements api.ServerInterface (POST .../fee-tables/{feeTableId}/activate).
func (h *Handler) ActivateFeeTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeTableId, params api.ActivateFeeTableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.ActivateFeeTable(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeTable(out))
}

// ListFeeRuns implements api.ServerInterface (GET .../fee-runs).
func (h *Handler) ListFeeRuns(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListFeeRunsParams) {
	list, err := h.Rights.ListFeeRuns(r.Context(), principal(r), marinaID, intOr(params.Year, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.FeeRunList{Items: make([]api.FeeRun, len(list))}
	for i := range list {
		out.Items[i] = view.FeeRun(&list[i])
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateFeeRun implements api.ServerInterface (POST .../fee-runs).
func (h *Handler) CreateFeeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateFeeRunParams) {
	var in api.FeeRunInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	plan, _ := json.Marshal(in.Plan)
	out, err := h.Rights.CreateFeeRun(r.Context(), principal(r), marinaID, in.Year, in.TableId, plan)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.FeeRun(out))
}

// GetFeeRun implements api.ServerInterface (GET .../fee-runs/{feeRunId}).
func (h *Handler) GetFeeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeRunId) {
	out, err := h.Rights.GetFeeRun(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeRun(out))
}

// SimulateFeeRun implements api.ServerInterface (POST .../fee-runs/{feeRunId}/simulate).
func (h *Handler) SimulateFeeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeRunId, params api.SimulateFeeRunParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	ctx, p := r.Context(), principal(r)
	out, err := h.Rights.SimulateFeeRun(ctx, p, marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if run, err := h.Rights.GetFeeRun(ctx, p, marinaID, id); err == nil {
		respond.SetETag(w, run.Version)
	}
	respond.JSON(w, http.StatusOK, view.FeeSimulation(out))
}

// IssueFeeRun implements api.ServerInterface (POST .../fee-runs/{feeRunId}/issue).
func (h *Handler) IssueFeeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeRunId, params api.IssueFeeRunParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.IssueFeeRun(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeRun(out))
}

// DiscardFeeRun implements api.ServerInterface (POST .../fee-runs/{feeRunId}/discard).
func (h *Handler) DiscardFeeRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeRunId, params api.DiscardFeeRunParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	out, err := h.Rights.DiscardFeeRun(r.Context(), principal(r), marinaID, id, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusOK, view.FeeRun(out))
}

// ListFeeRunLines implements api.ServerInterface (GET .../fee-runs/{feeRunId}/lines).
func (h *Handler) ListFeeRunLines(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.FeeRunId, params api.ListFeeRunLinesParams) {
	ctx, p := r.Context(), principal(r)
	if params.Format != nil && *params.Format == api.ListFeeRunLinesParamsFormatCsv {
		lines, err := h.Rights.AllFeeLines(ctx, p, marinaID, id)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		var rows [][]string
		for _, l := range lines {
			if len(l.HolderShares) == 0 {
				rows = append(rows, []string{
					l.RightID, l.BerthID, l.Category, rcCents(l.BaseCents), strconv.Itoa(l.Days), rcCents(l.AmountCents),
					"", "", "", strconv.FormatBool(l.Excluded), l.ExclusionReason,
				})
			}
			for _, hs := range l.HolderShares {
				rows = append(rows, []string{
					l.RightID, l.BerthID, l.Category, rcCents(l.BaseCents), strconv.Itoa(l.Days), rcCents(l.AmountCents),
					hs.CustomerID, strconv.FormatInt(hs.ShareMillis, 10), rcCents(hs.AmountCents), strconv.FormatBool(l.Excluded), l.ExclusionReason,
				})
			}
		}
		rcCSV(w, "fee-run-lines.csv", []string{
			"right_id", "berth_id", "category", "base", "days", "amount", "customer_id", "share_millis",
			"holder_amount", "excluded", "exclusion_reason",
		}, rows)
		return
	}
	page, err := h.Rights.ListFeeLines(ctx, p, marinaID, id, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.FeeLinePage{Items: make([]api.FeeLine, len(page.Items)), NextCursor: view.String(page.NextCursor)}
	for i := range page.Items {
		out.Items[i] = view.FeeLine(&page.Items[i])
	}
	respond.JSON(w, http.StatusOK, out)
}
