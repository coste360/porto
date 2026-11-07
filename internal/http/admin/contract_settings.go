package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Contract settings (m4-plan §7.3.1): default policy, signature driver, renewal, arrears and insurance policy,
// required fields of the public acceptance, bank details (masked without payments.collect) and office email.

func writeSettings(w http.ResponseWriter, s *contract.Settings) {
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, view.ContractSettings(s))
}

// GetContractSettings implements api.ServerInterface (GET .../contract-settings).
func (h *Handler) GetContractSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Contracts.GetContractSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeSettings(w, s)
}

// UpdateContractSettings implements api.ServerInterface (PUT .../contract-settings).
func (h *Handler) UpdateContractSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateContractSettingsParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ContractSettingsUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	patch := contract.SettingsPatch{
		MarinaCountersign: in.MarinaCountersign, RequirePaperScan: in.RequirePaperScan, SignatureDays: in.SignatureDays,
		SignatureReminderDays: in.SignatureReminderDays, RenewalDeadlineDays: in.RenewalDeadlineDays, ArrearsDays: in.ArrearsDays,
		ProviderCode: in.ProviderCode, OfficeEmail: in.OfficeEmail, SignatureDriver: enumPtr(in.SignatureDriver),
		InsurancePolicy: enumPtr(in.InsurancePolicy),
	}
	if in.DefaultPolicy != nil {
		patch.DefaultPolicy = ctrPolicyIn(in.DefaultPolicy)
	}
	if in.RequiredFields != nil {
		fs := make([]string, 0, len(*in.RequiredFields))
		for _, f := range *in.RequiredFields {
			fs = append(fs, string(f))
		}
		patch.RequiredFields = &fs
	}
	if b := in.BankDetails; b != nil {
		patch.BankDetails = &contract.BankDetails{IBAN: str(b.Iban), Holder: str(b.Holder), Bank: str(b.Bank), BIC: str(b.Bic)}
	}
	s, err := h.Contracts.UpdateContractSettings(r.Context(), principal(r), marinaID, ifMatch, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeSettings(w, s)
}
