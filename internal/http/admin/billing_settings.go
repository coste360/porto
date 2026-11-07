package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Billing settings and origin items (m5-plan §7.3.1).

// fiscalDriverCodes are the fiscal drivers a marina may choose: the core none and export, then the extensions.
func (h *Handler) fiscalDriverCodes() []string {
	out := []string{"none", "export"}
	if h.Fiscal != nil {
		for _, c := range h.Fiscal.DriverCodes() {
			if c != "none" && c != "export" {
				out = append(out, c)
			}
		}
	}
	return out
}

// GetBillingSettings implements api.ServerInterface (GET .../billing-settings).
func (h *Handler) GetBillingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Charges.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, view.BillingSettings(s, h.fiscalDriverCodes()))
}

// UpdateBillingSettings implements api.ServerInterface (PUT .../billing-settings). The settings of a marina that
// never saved them have version 0: If-Match "0" is accepted for the first save.
func (h *Handler) UpdateBillingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateBillingSettingsParams) {
	ifMatch := 0
	if params.IfMatch == nil || (*params.IfMatch != `"0"` && *params.IfMatch != "0") {
		v, ok := respond.IfMatch(w, r, params.IfMatch)
		if !ok {
			return
		}
		ifMatch = v
	}
	var in api.BillingSettingsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Charges.UpdateSettings(r.Context(), principal(r), marinaID, ifMatch, view.BillingSettingsIn(in, h.fiscalDriverCodes()))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, view.BillingSettings(s, h.fiscalDriverCodes()))
}
