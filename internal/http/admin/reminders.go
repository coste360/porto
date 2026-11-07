package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Reminder handlers (m5-plan §7.3.3): runs, manual reminders, approval of final notices and dunning exclusions.

func (h *Handler) itemNames(r *http.Request, marinaID string, items []collect.ItemView) map[string]string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Item.CustomerID)
	}
	return h.Collect.CustomerNames(r.Context(), principal(r), marinaID, ids)
}

// ListReminderRuns implements api.ServerInterface.
func (h *Handler) ListReminderRuns(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListReminderRunsParams) {
	page, err := h.Collect.ListRuns(r.Context(), principal(r), marinaID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ReminderRunPage(page))
}

// CreateReminderRun implements api.ServerInterface.
func (h *Handler) CreateReminderRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateReminderRunParams) {
	run, err := h.Collect.RunNow(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ReminderRun(run))
}

// GetReminderRun implements api.ServerInterface.
func (h *Handler) GetReminderRun(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReminderRunId, params api.GetReminderRunParams) {
	run, items, err := h.Collect.GetRun(r.Context(), principal(r), marinaID, id, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ReminderRunDetail(run, items, h.itemNames(r, marinaID, items.Items)))
}

// SendReminders implements api.ServerInterface.
func (h *Handler) SendReminders(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.SendRemindersParams) {
	var in api.SendRemindersInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Collect.SendReminders(r.Context(), principal(r), marinaID, in.ChargeIds, str(enumPtr(in.Level)))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.SendRemindersResult{Queued: res.Queued, Skipped: res.Skipped})
}

// ApproveReminderItem implements api.ServerInterface.
func (h *Handler) ApproveReminderItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReminderItemId) {
	it, err := h.Collect.ApproveItem(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ReminderItem(*it, h.itemNames(r, marinaID, []collect.ItemView{*it})))
}

// DismissReminderItem implements api.ServerInterface.
func (h *Handler) DismissReminderItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReminderItemId) {
	it, err := h.Collect.DismissItem(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ReminderItem(*it, h.itemNames(r, marinaID, []collect.ItemView{*it})))
}

func (h *Handler) exclusionNames(r *http.Request, marinaID string, es ...collect.Exclusion) map[string]string {
	ids := make([]string, 0, len(es))
	for _, e := range es {
		ids = append(ids, e.CustomerID)
	}
	return h.Collect.CustomerNames(r.Context(), principal(r), marinaID, ids)
}

// ListDunningExclusions implements api.ServerInterface.
func (h *Handler) ListDunningExclusions(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListDunningExclusionsParams) {
	f := collect.ExclusionFilter{CustomerID: str(params.CustomerId), ChargeID: str(params.ChargeId)}
	page, today, err := h.Collect.ListExclusions(r.Context(), principal(r), marinaID, f, boolOr(params.Active, false), str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.DunningExclusionPage(page, today, h.exclusionNames(r, marinaID, page.Items...)))
}

// CreateDunningExclusion implements api.ServerInterface.
func (h *Handler) CreateDunningExclusion(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateDunningExclusionParams) {
	var in api.DunningExclusionInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	var until calendar.Date
	if in.Until != nil {
		d, ok := dateParam(w, r, in.Until)
		if !ok {
			return
		}
		until = d
	}
	e, today, err := h.Collect.CreateExclusion(r.Context(), principal(r), marinaID, collect.ExclusionInput{
		CustomerID: str(in.CustomerId), ChargeID: str(in.ChargeId), Kind: string(in.Kind), Until: until, Note: str(in.Note),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view.DunningExclusion(e, today, h.exclusionNames(r, marinaID, *e)))
}

// DeleteDunningExclusion implements api.ServerInterface.
func (h *Handler) DeleteDunningExclusion(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.DunningExclusionId) {
	if err := h.Collect.RemoveExclusion(r.Context(), principal(r), marinaID, id); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
