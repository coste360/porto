package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Online booking settings, texts and quotas (m8-plan §3, §11) over onlineconf.Service. booking_enabled and
// embed_origins are read-only here: they are marina fields edited through updateMarina.

func onlineSettingsView(s onlineconf.Settings, bookingEnabled bool, origins []string) api.OnlineBookingSettings {
	days := make([]int, 0, len(s.ClosedWeekdays))
	for _, d := range s.ClosedWeekdays {
		days = append(days, int(d))
	}
	tiers := make([]api.CancellationTier, 0, len(s.CancellationTiers))
	for _, t := range s.CancellationTiers {
		tiers = append(tiers, api.CancellationTier{MinDaysBefore: t.MinDaysBefore, RefundBp: t.RefundBP})
	}
	docs := make([]api.BookingDocumentType, 0, len(s.RequiredFields.Documents))
	for _, d := range s.RequiredFields.Documents {
		docs = append(docs, api.BookingDocumentType(d))
	}
	groups := append([]string{}, s.Approval.GroupIDs...)
	approval := api.OnlineApprovalRules{Always: s.Approval.Always, GroupIds: groups, HoldHours: s.Approval.HoldHours}
	if s.Approval.LOAOverCm != nil {
		v := int64(*s.Approval.LOAOverCm)
		approval.LoaOverCm = &v
	}
	if origins == nil {
		origins = []string{}
	}
	out := api.OnlineBookingSettings{
		Rules: api.BookingRules{
			MinNights: s.MinNights, MaxNights: s.MaxNights, MinAdvanceHours: s.MinAdvanceHours,
			MaxAdvanceDays: s.MaxAdvanceDays, ArrivalFrom: s.ArrivalFrom, ArrivalTo: s.ArrivalTo, ClosedWeekdays: days,
		},
		PaymentMode: api.OnlinePaymentMode(s.PaymentMode), DepositBp: s.DepositBP, CancellationTiers: tiers,
		RefundAs: api.RefundAs(s.RefundAs), Approval: approval,
		RequiredFields: api.BookingRequiredFields{
			Documents: docs, PeopleOnBoard: s.RequiredFields.PeopleOnBoard, Phone: s.RequiredFields.Phone,
			DocumentsLater: s.RequiredFields.DocumentsLater,
		},
		RevealBerth: api.RevealBerth(s.RevealBerth), LatePaymentRecovery: s.LatePaymentRecovery,
		AllowWithoutProvider: s.AllowWithoutProvider, MaxLiveHolds: s.MaxLiveHolds, ThemePreset: s.ThemePreset,
		HideHeaderInEmbed: s.HideHeaderInEmbed, ShowFeet: s.ShowFeet, Saved: s.Saved(), Version: s.Version,
		BookingEnabled: bookingEnabled, EmbedOrigins: origins,
	}
	a := s.ArrivalInfo
	if a.VHFChannel != "" || a.Phone != "" || a.RulesURL != "" || !a.Text.IsZero() {
		ai := &api.ArrivalInfo{VhfChannel: view.String(a.VHFChannel), Phone: view.String(a.Phone), RulesUrl: view.String(a.RulesURL)}
		if !a.Text.IsZero() {
			ai.Text = &api.BookingI18nText{It: a.Text.It, En: view.String(a.Text.En)}
		}
		out.ArrivalInfo = ai
	}
	return out
}

func onlineSettingsIn(in api.OnlineBookingSettingsInput) onlineconf.Settings {
	s := onlineconf.Settings{
		MinNights: in.Rules.MinNights, MaxNights: in.Rules.MaxNights, MinAdvanceHours: in.Rules.MinAdvanceHours,
		MaxAdvanceDays: in.Rules.MaxAdvanceDays, ArrivalFrom: in.Rules.ArrivalFrom, ArrivalTo: in.Rules.ArrivalTo,
		PaymentMode: string(in.PaymentMode), DepositBP: in.DepositBp, RefundAs: string(in.RefundAs),
		Approval: onlineconf.Approval{
			Always: in.Approval.Always, GroupIDs: append([]string{}, in.Approval.GroupIds...), HoldHours: in.Approval.HoldHours,
		},
		RequiredFields: onlineconf.RequiredFields{
			PeopleOnBoard: in.RequiredFields.PeopleOnBoard, Phone: in.RequiredFields.Phone,
			DocumentsLater: in.RequiredFields.DocumentsLater, Documents: []string{},
		},
		RevealBerth: string(in.RevealBerth), LatePaymentRecovery: in.LatePaymentRecovery,
		AllowWithoutProvider: in.AllowWithoutProvider, MaxLiveHolds: in.MaxLiveHolds, ThemePreset: in.ThemePreset,
		HideHeaderInEmbed: in.HideHeaderInEmbed, ShowFeet: in.ShowFeet,
	}
	for _, d := range in.Rules.ClosedWeekdays {
		s.ClosedWeekdays = append(s.ClosedWeekdays, time.Weekday(d))
	}
	for _, t := range in.CancellationTiers {
		s.CancellationTiers = append(s.CancellationTiers, onlineconf.Tier{MinDaysBefore: t.MinDaysBefore, RefundBP: t.RefundBp})
	}
	if in.Approval.LoaOverCm != nil {
		v := geo.Cm(*in.Approval.LoaOverCm)
		s.Approval.LOAOverCm = &v
	}
	for _, d := range in.RequiredFields.Documents {
		s.RequiredFields.Documents = append(s.RequiredFields.Documents, string(d))
	}
	if a := in.ArrivalInfo; a != nil {
		s.ArrivalInfo = onlineconf.ArrivalInfo{VHFChannel: str(a.VhfChannel), Phone: str(a.Phone), RulesURL: str(a.RulesUrl)}
		if a.Text != nil {
			s.ArrivalInfo.Text = onlineconf.LongText{It: a.Text.It, En: str(a.Text.En)}
		}
	}
	return s
}

// ifMatchOrZero parses If-Match like respond.IfMatch but accepts "0": the version of a resource that was never
// saved (online settings with their defaults, a channel connection not created yet).
func ifMatchOrZero(w http.ResponseWriter, r *http.Request, header *string) (int, bool) {
	if header != nil && strings.Trim(strings.TrimPrefix(strings.TrimSpace(*header), "W/"), `"`) == "0" {
		return 0, true
	}
	return respond.IfMatch(w, r, header)
}

func (h *Handler) writeOnlineSettings(w http.ResponseWriter, r *http.Request, marinaID string, s onlineconf.Settings) {
	m, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, onlineSettingsView(s, m.BookingEnabled, m.AllowedEmbedOrigins))
}

// GetOnlineBookingSettings implements api.ServerInterface (GET .../online-booking/settings).
func (h *Handler) GetOnlineBookingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.OnlineConf.Get(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineSettings(w, r, marinaID, s)
}

// UpdateOnlineBookingSettings implements api.ServerInterface (PUT .../online-booking/settings). If-Match 0 saves the
// defaults for the first time.
func (h *Handler) UpdateOnlineBookingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateOnlineBookingSettingsParams) {
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.OnlineBookingSettingsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.OnlineConf.Update(r.Context(), principal(r), marinaID, ifMatch, onlineSettingsIn(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writeOnlineSettings(w, r, marinaID, s)
}

func (h *Handler) bookingTextView(t onlineconf.Text, names map[string]string) api.BookingText {
	out := api.BookingText{
		Id: t.ID, Kind: api.BookingTextKind(t.Kind), Version: t.Version, Sha256: t.SHA256,
		PublishedAt: t.PublishedAt.UTC(), Texts: api.BookingI18nText{It: t.Texts.It, En: view.String(t.Texts.En)},
	}
	if t.PublishedBy != "" {
		out.PublishedBy = &api.UserRef{Id: t.PublishedBy, Name: names[t.PublishedBy]}
	}
	return out
}

// userNames returns the names of staff users for UserRef values (empty map on an empty list).
func (h *Handler) onlineUserNames(r *http.Request, marinaID string, ids []string) map[string]string {
	var want []string
	for _, id := range ids {
		if id != "" {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return map[string]string{}
	}
	names, err := h.OnlineConf.UserNames(r.Context(), principal(r), marinaID, want)
	if err != nil {
		return map[string]string{}
	}
	return names
}

// ListBookingTexts implements api.ServerInterface (GET .../online-booking/texts).
func (h *Handler) ListBookingTexts(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListBookingTextsParams) {
	kind := ""
	if params.Kind != nil {
		kind = string(*params.Kind)
	}
	ts, err := h.OnlineConf.ListTexts(r.Context(), principal(r), marinaID, kind)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		ids = append(ids, t.PublishedBy)
	}
	names := h.onlineUserNames(r, marinaID, ids)
	out := api.BookingTextList{Items: make([]api.BookingText, 0, len(ts))}
	for _, t := range ts {
		out.Items = append(out.Items, h.bookingTextView(t, names))
	}
	respond.JSON(w, http.StatusOK, out)
}

// PublishBookingText implements api.ServerInterface (POST .../online-booking/texts).
func (h *Handler) PublishBookingText(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.PublishBookingTextParams) {
	var in api.BookingTextPublish
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	t, err := h.OnlineConf.PublishText(r.Context(), principal(r), marinaID, string(in.Kind),
		onlineconf.LongText{It: in.Texts.It, En: str(in.Texts.En)})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, h.bookingTextView(*t, h.onlineUserNames(r, marinaID, []string{t.PublishedBy})))
}

func onlineQuotaList(qs []onlineconf.Quota) api.OnlineQuotaList {
	out := api.OnlineQuotaList{Items: make([]api.OnlineQuota, 0, len(qs))}
	for _, q := range qs {
		out.Items = append(out.Items, api.OnlineQuota{
			GroupId: q.GroupID, GroupCode: q.GroupCode, GroupName: api.I18nText{It: q.GroupName.It, En: view.String(q.GroupName.En)},
			Kind: api.OnlineQuotaKind(q.Kind), Value: q.Value, MemberBerths: q.MemberBerths, HasAreas: q.HasAreas,
		})
	}
	return out
}

// GetOnlineQuotas implements api.ServerInterface (GET .../online-booking/quotas).
func (h *Handler) GetOnlineQuotas(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	qs, err := h.OnlineConf.Quotas(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, onlineQuotaList(qs))
}

// UpdateOnlineQuotas implements api.ServerInterface (PUT .../online-booking/quotas).
func (h *Handler) UpdateOnlineQuotas(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.OnlineQuotasUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	sets := make([]onlineconf.QuotaSet, 0, len(in.Items))
	for _, it := range in.Items {
		sets = append(sets, onlineconf.QuotaSet{GroupID: it.GroupId, Kind: string(it.Kind), Value: it.Value})
	}
	qs, err := h.OnlineConf.UpdateQuotas(r.Context(), principal(r), marinaID, sets)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, onlineQuotaList(qs))
}

// GetOnlineQuotaUsage implements api.ServerInterface (GET .../online-booking/quotas/usage). from and to are
// optional in the contract so that another marina answers 404 first; the service reports them as required.
func (h *Handler) GetOnlineQuotaUsage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetOnlineQuotaUsageParams) {
	dates := newCtrDates()
	from, to := dates.parse(str(params.From)), dates.parse(str(params.To))
	if !dates.ok {
		if _, err := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); err != nil {
			respond.Error(w, r, err)
			return
		}
		respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "from", Code: "invalid"}))
		return
	}
	gs, err := h.OnlineConf.QuotaUsage(r.Context(), principal(r), marinaID, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.OnlineQuotaUsage{From: from.String(), To: to.String(), Groups: make([]api.OnlineQuotaGroupUsage, 0, len(gs))}
	for _, g := range gs {
		gu := api.OnlineQuotaGroupUsage{GroupId: g.GroupID, Nights: make([]api.OnlineQuotaNight, 0, len(g.Nights))}
		for _, n := range g.Nights {
			gu.Nights = append(gu.Nights, api.OnlineQuotaNight{Night: n.Night.String(), Cap: n.Cap, Used: n.Used})
		}
		out.Groups = append(out.Groups, gu)
	}
	respond.JSON(w, http.StatusOK, out)
}
