package contract

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Contract settings of a marina (m4-plan §5.1, §7.3.1): default policy, signature driver, renewal and arrears
// days, insurance policy, required fields of the public acceptance, bank details and office email (encrypted).

// Signature drivers.
const (
	DriverPaper    = "paper"
	DriverOTP      = "otp"
	DriverProvider = "provider"
)

// Insurance policies (11.5, m4-plan §15 D29).
const (
	InsuranceWarn  = "warn"
	InsuranceBlock = "block"
)

// RequiredFields of the public quote acceptance.
var RequiredFields = []string{"address", "fiscal_code", "vessel_registration", "insurance_expiry", "identity_document"}

// BankDetails are the marina's bank coordinates shown in payment requests.
type BankDetails struct {
	IBAN   string `json:"iban"`
	Holder string `json:"holder"`
	Bank   string `json:"bank"`
	BIC    string `json:"bic"`
}

// Settings are the contract settings with the decrypted fields the principal may see.
type Settings struct {
	Row         *store.ContractSettings
	Policy      Policy
	Bank        *BankDetails // nil when not set
	BankMasked  bool         // IBAN reduced to its last 4 characters (no payments.collect)
	OfficeEmail string       // "" without customers.read
}

// SettingsPatch updates the settings (nil = unchanged).
type SettingsPatch struct {
	DefaultPolicy         *PolicyInput
	SignatureDriver       *string
	ProviderCode          *string // "" clears; needs providers.configure
	MarinaCountersign     *bool
	RequirePaperScan      *bool
	SignatureDays         *int
	SignatureReminderDays *int
	RenewalDeadlineDays   *int
	ArrearsDays           *int
	InsurancePolicy       *string
	RequiredFields        *[]string
	BankDetails           *BankDetails
	OfficeEmail           *string
}

func bankAAD(marinaID string) string   { return "bank-details:" + marinaID }
func officeAAD(marinaID string) string { return "office-email:" + marinaID }

// GetContractSettings returns the settings of a marina (contracts.read).
func (s *Service) GetContractSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetContractSettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return nil, err
	}
	return s.settingsView(p, scope, row), nil
}

func (s *Service) settingsView(p *auth.Principal, scope store.MarinaScope, row *store.ContractSettings) *Settings {
	out := &Settings{Row: row}
	out.Policy, _ = DecodePolicy(row.DefaultPolicy)
	if b := s.bankDetails(row); b != nil {
		if !p.Can(scope.OrgID(), scope.ID(), auth.PermPaymentsCollect) {
			if n := len(b.IBAN); n > 4 {
				b.IBAN = strings.Repeat("•", 4) + b.IBAN[n-4:]
			}
			out.BankMasked = true
		}
		out.Bank = b
	}
	if row.OfficeEmailEnc != "" && p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead) && s.d.Keys != nil {
		out.OfficeEmail, _ = s.d.Keys.DecryptString(row.OfficeEmailEnc, officeAAD(row.MarinaID))
	}
	return out
}

// bankDetails decrypts the bank details of a settings row (nil when absent or unreadable).
func (s *Service) bankDetails(row *store.ContractSettings) *BankDetails {
	if row.BankDetailsEnc == "" || s.d.Keys == nil {
		return nil
	}
	raw, err := s.d.Keys.DecryptString(row.BankDetailsEnc, bankAAD(row.MarinaID))
	if err != nil {
		return nil
	}
	var b BankDetails
	if json.Unmarshal([]byte(raw), &b) != nil {
		return nil
	}
	return &b
}

// UpdateContractSettings applies a patch (contracts.write; provider_code also providers.configure; If-Match).
func (s *Service) UpdateContractSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsPatch) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if in.ProviderCode != nil {
		if _, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure); err != nil {
			return nil, err
		}
	}
	var out *store.ContractSettings
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetContractSettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		if ifMatch != 0 && row.Version != ifMatch {
			return ErrVersionMismatch
		}
		fields, err := s.applySettings(&auth.ValidationError{}, scope, row, in)
		if err != nil {
			return err
		}
		if err := store.SaveContractSettings(ctx, tx, scope, row.Version, row, s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "contract_settings.update", "contract_settings", scope.ID(), map[string]any{
			"fields": fields, "signature_driver": row.SignatureDriver, "insurance_policy": row.InsurancePolicy,
		}); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.settingsView(p, scope, out), nil
}

func (s *Service) applySettings(v *auth.ValidationError, scope store.MarinaScope, row *store.ContractSettings, in SettingsPatch) ([]string, error) {
	var fields []string
	setInt := func(name string, dst *int, src *int, lo, hi int) {
		if src == nil {
			return
		}
		if *src < lo || *src > hi {
			v.Add(name, "invalid")
		}
		*dst = *src
		fields = append(fields, name)
	}
	if in.DefaultPolicy != nil {
		base, err := DecodePolicy(row.DefaultPolicy)
		if err != nil {
			base = Policy{}
		}
		base = mergePolicy(v, base, in.DefaultPolicy)
		if base.ReleasePolicy == "" {
			base.ReleasePolicy = ReleaseWithConsent
		}
		row.DefaultPolicy = store.MustJSON(base)
		fields = append(fields, "default_policy")
	}
	if in.SignatureDriver != nil {
		if !slices.Contains([]string{DriverPaper, DriverOTP, DriverProvider}, *in.SignatureDriver) {
			v.Add("signature_driver", "invalid")
		}
		row.SignatureDriver = *in.SignatureDriver
		fields = append(fields, "signature_driver")
	}
	if in.ProviderCode != nil {
		row.ProviderCode = strings.TrimSpace(*in.ProviderCode)
		fields = append(fields, "provider_code")
	}
	if row.SignatureDriver == DriverProvider && row.ProviderCode == "" {
		v.Add("provider_code", "required")
	}
	if in.MarinaCountersign != nil {
		row.MarinaCountersign = *in.MarinaCountersign
		fields = append(fields, "marina_countersign")
	}
	if in.RequirePaperScan != nil {
		row.RequirePaperScan = *in.RequirePaperScan
		fields = append(fields, "require_paper_scan")
	}
	setInt("signature_days", &row.SignatureDays, in.SignatureDays, 1, 90)
	setInt("signature_reminder_days", &row.SignatureReminderDays, in.SignatureReminderDays, 1, 30)
	setInt("renewal_deadline_days", &row.RenewalDeadlineDays, in.RenewalDeadlineDays, 1, 365)
	setInt("arrears_days", &row.ArrearsDays, in.ArrearsDays, 0, 365)
	if in.InsurancePolicy != nil {
		if *in.InsurancePolicy != InsuranceWarn && *in.InsurancePolicy != InsuranceBlock {
			v.Add("insurance_policy", "invalid")
		}
		row.InsurancePolicy = *in.InsurancePolicy
		fields = append(fields, "insurance_policy")
	}
	if in.RequiredFields != nil {
		for _, f := range *in.RequiredFields {
			if !slices.Contains(RequiredFields, f) {
				v.Add("required_fields", "invalid")
			}
		}
		row.RequiredFields = store.MustJSON(*in.RequiredFields)
		fields = append(fields, "required_fields")
	}
	if in.BankDetails != nil {
		b := *in.BankDetails
		b.IBAN = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(b.IBAN), " ", ""))
		if len(b.IBAN) > 34 || len(b.Holder) > 140 || len(b.Bank) > 140 || len(b.BIC) > 11 {
			v.Add("bank_details", "too_long")
		}
		if s.d.Keys == nil {
			return nil, errors.New("contract: key ring not wired")
		}
		if b == (BankDetails{}) {
			row.BankDetailsEnc = ""
		} else {
			raw, _ := json.Marshal(b)
			enc, err := s.d.Keys.EncryptString(string(raw), bankAAD(scope.ID()))
			if err != nil {
				return nil, err
			}
			row.BankDetailsEnc = enc
		}
		fields = append(fields, "bank_details")
	}
	if in.OfficeEmail != nil {
		e := strings.TrimSpace(*in.OfficeEmail)
		if e != "" && (!strings.Contains(e, "@") || len(e) > 254) {
			v.Add("office_email", "invalid")
		}
		if s.d.Keys == nil {
			return nil, errors.New("contract: key ring not wired")
		}
		row.OfficeEmailEnc = ""
		if e != "" {
			enc, err := s.d.Keys.EncryptString(e, officeAAD(scope.ID()))
			if err != nil {
				return nil, err
			}
			row.OfficeEmailEnc = enc
		}
		fields = append(fields, "office_email")
	}
	return fields, v.Err()
}

// settingsTx returns the settings of a marina inside tx (defaults when absent).
func (s *Service) settingsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (*store.ContractSettings, error) {
	return store.GetContractSettings(ctx, tx, scope, s.now())
}
