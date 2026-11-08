package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/channel"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Booking channel connections and reservations (m8-plan §8) over channel.Service. Secrets are write-only: the
// views report only whether they are set.

func bookingChannelView(i channel.Info) api.BookingChannel {
	out := api.BookingChannel{
		Code: i.Code,
		Capabilities: api.ChannelCapabilities{
			Push: i.Capabilities.Push, Pull: i.Capabilities.Pull, Webhook: i.Capabilities.Webhook, Ack: i.Capabilities.Ack,
		},
	}
	if c := i.Connection; c != nil {
		cm := map[string]string{}
		for k, v := range c.CategoryMap {
			cm[k] = v
		}
		out.Connection = &api.BookingChannelConnection{
			Enabled: c.Enabled, PushEnabled: c.PushEnabled, CategoryMap: cm, HasCredentials: c.HasCredentials,
			HasWebhookSecret: c.HasWebhookSecret, WebhookUrl: c.WebhookURL, LastPushAt: view.Time(c.LastPushAt),
			LastPullAt: view.Time(c.LastPullAt), LastError: view.String(c.LastError), Version: c.Version,
		}
	}
	return out
}

// ListBookingChannels implements api.ServerInterface (GET .../booking-channels).
func (h *Handler) ListBookingChannels(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	infos, err := h.Channels.List(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.BookingChannelList{Items: make([]api.BookingChannel, 0, len(infos))}
	for _, i := range infos {
		out.Items = append(out.Items, bookingChannelView(i))
	}
	respond.JSON(w, http.StatusOK, out)
}

// UpdateBookingChannel implements api.ServerInterface (PUT .../booking-channels/{channelCode}). If-Match 0 creates
// the connection.
func (h *Handler) UpdateBookingChannel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.ChannelCode, params api.UpdateBookingChannelParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.BookingChannelUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	u := channel.Update{Enabled: in.Enabled, PushEnabled: in.PushEnabled, CategoryMap: map[string]string{}, WebhookSecret: in.WebhookSecret}
	for k, v := range in.CategoryMap {
		u.CategoryMap[k] = v
	}
	if in.Credentials != nil {
		u.Credentials = map[string]string{}
		for k, v := range *in.Credentials {
			u.Credentials[k] = v
		}
	}
	info, err := h.Channels.Save(r.Context(), principal(r), marinaID, code, ifMatch, u)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if info.Connection != nil {
		respond.SetETag(w, info.Connection.Version)
	}
	respond.JSON(w, http.StatusOK, bookingChannelView(*info))
}

// TestBookingChannel implements api.ServerInterface (POST .../booking-channels/{channelCode}/test).
func (h *Handler) TestBookingChannel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.ChannelCode) {
	res, err := h.Channels.Test(r.Context(), principal(r), marinaID, code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ChannelTestResult{Ok: res.OK, ErrorCode: view.String(res.ErrorCode)})
}

// SyncBookingChannel implements api.ServerInterface (POST .../booking-channels/{channelCode}/sync).
func (h *Handler) SyncBookingChannel(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.ChannelCode) {
	res, err := h.Channels.Sync(r.Context(), principal(r), marinaID, code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ChannelSyncResult{Pulled: res.Pulled, Booked: res.Booked, Rejected: res.Rejected, Pushed: res.Pushed})
}

// ListChannelReservations implements api.ServerInterface (GET .../channel-reservations).
func (h *Handler) ListChannelReservations(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListChannelReservationsParams) {
	f := channel.ReservationFilter{Channel: str(params.Channel), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	page, err := h.Channels.Reservations(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.ChannelReservationPage{Items: make([]api.ChannelReservation, 0, len(page.Items)), NextCursor: view.String(page.NextCursor)}
	for _, res := range page.Items {
		item := api.ChannelReservation{
			Id: res.ID, ChannelCode: res.ChannelCode, ExternalRef: res.ExternalRef,
			Status: api.ChannelReservationStatus(res.Status), CreatedAt: res.CreatedAt.UTC(),
		}
		if res.RejectReason != "" {
			rr := api.ChannelReservationRejectReason(res.RejectReason)
			item.RejectReason = &rr
		}
		if res.ContractID != "" {
			item.Transit = &api.ContractBrief{Id: res.ContractID, Kind: api.ContractKindTransit}
		}
		out.Items = append(out.Items, item)
	}
	respond.JSON(w, http.StatusOK, out)
}
