package admin

import (
	"encoding/csv"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Customer communications and the per-customer message registry (m4-plan §7.3.5).

func (h *Handler) commReply(w http.ResponseWriter, r *http.Request, status int, c *comms.Communication, err error) {
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, status, view.Communication(c))
}

// ListCommunications implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/communications).
func (h *Handler) ListCommunications(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListCommunicationsParams) {
	page, err := h.Comms.List(r.Context(), principal(r), marinaID, str(enumPtr(params.Status)), str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.Communications(page.Items, page.NextCursor))
}

// CreateCommunication implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/communications).
func (h *Handler) CreateCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateCommunicationParams) {
	var in api.CommunicationInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ci := comms.Input{
		Name: in.Name, Category: string(in.Category), Audience: view.AudienceIn(in.Audience), TemplateID: str(in.TemplateId),
		Subject: str(in.Subject), BodyMD: str(in.BodyMd), Attachments: view.AttachmentsIn(in.Attachments),
	}
	if in.Channels != nil {
		ci.Channels = *in.Channels
	}
	if in.ScheduledAt != nil {
		ci.ScheduledAt = *in.ScheduledAt
	}
	c, err := h.Comms.Create(r.Context(), principal(r), marinaID, ci)
	h.commReply(w, r, http.StatusCreated, c, err)
}

// GetCommunication implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}).
func (h *Handler) GetCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId) {
	c, err := h.Comms.Get(r.Context(), principal(r), marinaID, id)
	h.commReply(w, r, http.StatusOK, c, err)
}

// UpdateCommunication implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}).
func (h *Handler) UpdateCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId, params api.UpdateCommunicationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.CommunicationPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p := comms.Patch{Name: in.Name, Subject: in.Subject, BodyMD: in.BodyMd, TemplateID: in.TemplateId, Channels: in.Channels}
	if in.Category != nil {
		c := string(*in.Category)
		p.Category = &c
	}
	if in.Audience != nil {
		a := view.AudienceIn(*in.Audience)
		p.Audience = &a
	}
	if in.Attachments != nil {
		a := view.AttachmentsIn(in.Attachments)
		p.Attachments = &a
	}
	c, err := h.Comms.Update(r.Context(), principal(r), marinaID, id, version, p)
	h.commReply(w, r, http.StatusOK, c, err)
}

// PreviewCommunication implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}/preview).
func (h *Handler) PreviewCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId) {
	var in api.CommunicationPreviewInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	pv, err := h.Comms.PreviewCommunication(r.Context(), principal(r), marinaID, id, str(in.CustomerId))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.CommunicationPreview{}
	out.Recipients.Count, out.Recipients.SkippedNoConsent, out.Recipients.SkippedNoEmail = pv.Count, pv.SkippedNoConsent, pv.SkippedNoEmail
	if pv.Sample != nil {
		out.Sample = &struct {
			Html    string `json:"html"` //nolint:revive // must match the generated anonymous struct
			Subject string `json:"subject"`
			Text    string `json:"text"`
		}{Html: pv.Sample.HTML, Subject: pv.Sample.Subject, Text: pv.Sample.Text}
	}
	respond.JSON(w, http.StatusOK, out)
}

// SendCommunication implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}/send).
func (h *Handler) SendCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId, params api.SendCommunicationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	c, err := h.Comms.Send(r.Context(), principal(r), marinaID, id, version)
	h.commReply(w, r, http.StatusOK, c, err)
}

// ScheduleCommunication implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}/schedule).
func (h *Handler) ScheduleCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId, params api.ScheduleCommunicationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ScheduleInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Comms.Schedule(r.Context(), principal(r), marinaID, id, version, in.ScheduledAt)
	h.commReply(w, r, http.StatusOK, c, err)
}

// CancelCommunication implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}/cancel).
func (h *Handler) CancelCommunication(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId, params api.CancelCommunicationParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	c, err := h.Comms.Cancel(r.Context(), principal(r), marinaID, id, version)
	h.commReply(w, r, http.StatusOK, c, err)
}

// ListCommunicationRecipients implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/communications/{commCampaignId}/recipients).
func (h *Handler) ListCommunicationRecipients(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CommCampaignId, params api.ListCommunicationRecipientsParams) {
	status := str(enumPtr(params.Status))
	if params.Format != nil && *params.Format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="recipients.csv"`)
		w.Header().Set("Cache-Control", "no-store")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"customer_id", "display_name", "status", "delivery_status", "sent_at"})
		cursor := ""
		first := true
		for {
			page, err := h.Comms.Recipients(r.Context(), principal(r), marinaID, id, status, cursor, 200)
			if err != nil {
				if first {
					w.Header().Del("Content-Disposition")
					respond.Error(w, r, err)
				}
				return
			}
			first = false
			for _, it := range page.Items {
				sent := ""
				if !it.SentAt.IsZero() {
					sent = it.SentAt.UTC().Format(time.RFC3339)
				}
				_ = cw.Write([]string{it.CustomerID, csvSafe(it.DisplayName), it.Status, it.Delivery, sent})
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		cw.Flush()
		return
	}
	page, err := h.Comms.Recipients(r.Context(), principal(r), marinaID, id, status, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.CommRecipients(page.Items, page.NextCursor))
}

// csvSafe neutralises spreadsheet formulas in a cell.
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}

// ListCustomerMessages implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customers/{customerId}/messages).
func (h *Handler) ListCustomerMessages(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, customerID api.CustomerId, params api.ListCustomerMessagesParams) {
	page, err := h.Comms.CustomerMessages(r.Context(), principal(r), marinaID, customerID, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.CustomerMessages(page.Items, page.NextCursor))
}
