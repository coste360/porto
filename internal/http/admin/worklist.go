package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Office worklist (spec 28.3, m5-plan §7.3.2).

// ListWorklist implements api.ServerInterface (GET .../worklist).
func (h *Handler) ListWorklist(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListWorklistParams) {
	page, err := h.Payments.Worklist(r.Context(), principal(r), marinaID, payment.WorkFilter{
		Kind: str(enumPtr(params.Kind)), Status: str(enumPtr(params.Status)), AssigneeUserID: str(params.AssigneeUserId),
	}, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.WorkItemPage{Items: view.WorkItems(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// UpdateWorkItem implements api.ServerInterface (PATCH .../worklist/{sourceType}/{sourceId}).
func (h *Handler) UpdateWorkItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, sourceType api.WorkSourceType, sourceID api.WorkSourceId) {
	var in api.WorkItemPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	patch := payment.WorkPatch{Status: string(in.Status), Note: in.Note}
	if _, ok := fields["assignee_user_id"]; ok {
		v := ""
		if !respond.IsNull(fields, "assignee_user_id") && in.AssigneeUserId != nil {
			v = *in.AssigneeUserId
		}
		patch.AssigneeUserID = &v
	}
	it, err := h.Payments.UpdateWorkItem(r.Context(), principal(r), marinaID, sourceType, sourceID, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.WorkItem(*it))
}
