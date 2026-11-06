package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Deadline operations (m1-plan §3.2, §4.4).

// ListDeadlines implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/deadlines).
func (h *Handler) ListDeadlines(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListDeadlinesParams) {
	v := &auth.ValidationError{}
	f := deadline.Filter{
		Status: str(enumPtr(params.Status)), Kind: str(enumPtr(params.Kind)), Urgency: str(enumPtr(params.Urgency)),
		DueBefore: dateOrZero(parseDate(v, "due_before", params.DueBefore)), DueAfter: dateOrZero(parseDate(v, "due_after", params.DueAfter)),
		CustomerID: str(params.CustomerId), VesselID: str(params.VesselId),
	}
	if err := v.Err(); err != nil {
		respond.Error(w, r, err)
		return
	}
	page, err := h.Deadlines.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.DeadlinePage{Items: view.Deadlines(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// UpdateDeadline implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/deadlines/{deadlineId}).
func (h *Handler) UpdateDeadline(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deadlineID api.DeadlineId, params api.UpdateDeadlineParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.DeadlinePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d, err := h.Deadlines.Update(r.Context(), principal(r), marinaID, deadlineID, version, deadline.Patch{Status: enumPtr(in.Status), Note: in.Note})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, view.Deadline(d))
}

// ListDeadlinePolicies implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/deadline-policies).
func (h *Handler) ListDeadlinePolicies(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	ps, err := h.Deadlines.ListPolicies(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.DeadlinePolicies(ps))
}

// ReplaceDeadlinePolicies implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/deadline-policies).
func (h *Handler) ReplaceDeadlinePolicies(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.DeadlinePolicyList
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	items := make([]deadline.Policy, 0, len(in.Items))
	for _, it := range in.Items {
		items = append(items, deadline.Policy{Kind: string(it.Kind), LeadDays: it.LeadDays})
	}
	ps, err := h.Deadlines.ReplacePolicies(r.Context(), principal(r), marinaID, items)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.DeadlinePolicies(ps))
}
