package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Payment provider handlers (m5-plan §7.3.3): drivers with the marina's state, settings (secrets write-only) and
// the credentials test. Porto works without any provider: the manual driver is always enabled.

// ListPaymentProviders implements api.ServerInterface.
func (h *Handler) ListPaymentProviders(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Collect.ListProviders(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PaymentProviders(list))
}

// GetPaymentProviderSettings implements api.ServerInterface.
func (h *Handler) GetPaymentProviderSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.PaymentProviderCode) {
	s, err := h.Collect.GetProviderSettings(r.Context(), principal(r), marinaID, code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, view.ProviderSettings(s))
}

// UpdatePaymentProviderSettings implements api.ServerInterface. The first save carries If-Match "0".
func (h *Handler) UpdatePaymentProviderSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.PaymentProviderCode, params api.UpdatePaymentProviderSettingsParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PaymentProviderSettingsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	methods := make([]string, 0, len(in.Methods))
	for _, m := range in.Methods {
		methods = append(methods, string(m))
	}
	s, err := h.Collect.UpdateProviderSettings(r.Context(), principal(r), marinaID, code, ifMatch, collect.ProviderSettingsInput{
		Enabled: in.Enabled, Mode: string(in.Mode), Methods: methods, PublishableKey: in.PublishableKey, SecretKey: in.SecretKey,
		WebhookSecret: in.WebhookSecret, SEPAEnabled: in.SepaEnabled, StatementDescriptor: in.StatementDescriptor,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, view.ProviderSettings(s))
}

// TestPaymentProvider implements api.ServerInterface.
func (h *Handler) TestPaymentProvider(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, code api.PaymentProviderCode) {
	ok, c, err := h.Collect.TestProvider(r.Context(), principal(r), marinaID, code)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ProviderTestResult{Ok: ok, Code: optString(c)})
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
