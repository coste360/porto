package collect

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/paydriver/manual"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Payment provider settings (m5-plan §5.4): a marina enables an online driver with providers.configure; without
// a settings row the driver is disabled (Porto works offline). Secrets are write-only and KeyRing-encrypted (AAD
// paycreds:<marina>:<provider>); they are decrypted per call and passed in the context.

// CredentialSecretKey is the credentials key of the secret API key (drivers read it from the context).
const CredentialSecretKey = "secret_key"

func moneyString(c int64) string { return money.Cents(c).String() }

func credsAAD(marinaID, provider string) string { return "paycreds:" + marinaID + ":" + provider }

// ProviderInfo is a registered driver with the marina's state.
type ProviderInfo struct {
	Code         string
	Capabilities ext.PaymentCapabilities
	Enabled      bool
	Mode         string
}

// ProviderSettings are the settings of a provider without secrets.
type ProviderSettings struct {
	Row              store.PaymentProviderSettings
	HasSecretKey     bool
	HasWebhookSecret bool
	Methods          []string
	WebhookURL       string
}

// ProviderSettingsInput configures a provider (secrets nil = unchanged, "" = cleared).
type ProviderSettingsInput struct {
	Enabled             bool
	Mode                string
	Methods             []string
	PublishableKey      *string
	SecretKey           *string
	WebhookSecret       *string
	SEPAEnabled         bool
	StatementDescriptor *string
}

// ListProviders returns the registered drivers with the marina's enabled state (payments.collect). The manual
// driver is always enabled.
func (s *Service) ListProviders(ctx context.Context, p *auth.Principal, marinaID string) ([]ProviderInfo, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListProviderSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	byCode := map[string]store.PaymentProviderSettings{}
	for _, r := range rows {
		byCode[r.ProviderCode] = r
	}
	var out []ProviderInfo
	for _, code := range s.d.Drivers.Codes() {
		drv, _ := s.d.Drivers.Get(code)
		info := ProviderInfo{Code: code, Capabilities: drv.Capabilities()}
		if code == manual.Code {
			info.Enabled = true
		} else if r, ok := byCode[code]; ok {
			info.Enabled, info.Mode = r.Enabled, r.Mode
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) onlineDriver(code string) (ext.PaymentProvider, error) {
	if code == "" || code == manual.Code {
		return nil, ErrProviderUnknown
	}
	drv, ok := s.d.Drivers.Get(code)
	if !ok {
		return nil, ErrProviderUnknown
	}
	return drv, nil
}

// WebhookURL is the URL the marina registers at the provider.
func (s *Service) WebhookURL(provider, marinaID string) string {
	return s.d.BaseURL + "/webhooks/payments/" + provider + "/" + marinaID
}

// GetProviderSettings returns the settings of a provider (providers.configure); never configured = disabled
// defaults with version 1 (the row is written by the first save, If-Match "1").
func (s *Service) GetProviderSettings(ctx context.Context, p *auth.Principal, marinaID, code string) (*ProviderSettings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	if _, err := s.onlineDriver(code); err != nil {
		return nil, err
	}
	row, err := store.GetProviderSettings(ctx, s.d.DB.DB, scope, code)
	if errors.Is(err, store.ErrNotFound) {
		row = &store.PaymentProviderSettings{MarinaID: scope.ID(), ProviderCode: code, Mode: "test", Methods: store.JSON(`["card"]`), Version: 1}
	} else if err != nil {
		return nil, err
	}
	return s.settingsView(row), nil
}

func (s *Service) settingsView(row *store.PaymentProviderSettings) *ProviderSettings {
	var methods []string
	_ = json.Unmarshal(row.Methods, &methods)
	if methods == nil {
		methods = []string{}
	}
	return &ProviderSettings{
		Row: *row, HasSecretKey: row.CredentialsEnc != "", HasWebhookSecret: row.WebhookSecretEnc != "", Methods: methods,
		WebhookURL: s.WebhookURL(row.ProviderCode, row.MarinaID),
	}
}

// UpdateProviderSettings configures a provider (providers.configure, If-Match; "1" for the first save). Enabling
// requires a secret key and a webhook secret.
func (s *Service) UpdateProviderSettings(ctx context.Context, p *auth.Principal, marinaID, code string, ifMatch int, in ProviderSettingsInput) (*ProviderSettings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	drv, err := s.onlineDriver(code)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.Mode != "test" && in.Mode != "live" {
		v.Add("mode", "invalid")
	}
	caps := drv.Capabilities()
	methods := []string{}
	for _, m := range in.Methods {
		if !slices.Contains(caps.Methods, m) {
			v.Add("methods", "invalid")
			break
		}
		if !slices.Contains(methods, m) {
			methods = append(methods, m)
		}
	}
	if in.SEPAEnabled && !caps.MandateSetup {
		v.Add("sepa_enabled", "unsupported")
	}
	for name, sec := range map[string]*string{"secret_key": in.SecretKey, "webhook_secret": in.WebhookSecret, "publishable_key": in.PublishableKey} {
		if sec != nil && len(*sec) > 500 {
			v.Add(name, "too_long")
		}
	}
	if in.StatementDescriptor != nil && len(*in.StatementDescriptor) > 22 {
		v.Add("statement_descriptor", "too_long")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	if s.d.Keys == nil {
		return nil, errors.New("collect: key ring not wired")
	}
	var out *ProviderSettings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetProviderSettings(ctx, tx, scope, code)
		expected, current := 0, 1 // never configured: version 1 for the client, inserted by the store
		switch {
		case errors.Is(err, store.ErrNotFound):
			row = &store.PaymentProviderSettings{ProviderCode: code}
		case err != nil:
			return err
		default:
			expected, current = row.Version, row.Version
		}
		if current != ifMatch {
			return ErrVersionMismatch
		}
		aad := credsAAD(scope.ID(), code)
		if in.SecretKey != nil {
			row.CredentialsEnc = ""
			if k := strings.TrimSpace(*in.SecretKey); k != "" {
				raw, _ := json.Marshal(map[string]string{CredentialSecretKey: k})
				if row.CredentialsEnc, err = s.d.Keys.EncryptString(string(raw), aad); err != nil {
					return err
				}
			}
		}
		if in.WebhookSecret != nil {
			row.WebhookSecretEnc = ""
			if k := strings.TrimSpace(*in.WebhookSecret); k != "" {
				if row.WebhookSecretEnc, err = s.d.Keys.EncryptString(k, aad); err != nil {
					return err
				}
			}
		}
		if in.PublishableKey != nil {
			k := strings.TrimSpace(*in.PublishableKey)
			row.PublishableKeyLast4 = ""
			if len(k) >= 4 {
				row.PublishableKeyLast4 = k[len(k)-4:]
			}
		}
		if in.StatementDescriptor != nil {
			row.StatementDescriptor = strings.TrimSpace(*in.StatementDescriptor)
		}
		row.Enabled, row.Mode, row.SEPAEnabled, row.Methods = in.Enabled, in.Mode, in.SEPAEnabled, store.MustJSON(methods)
		if row.Enabled && (row.CredentialsEnc == "" || row.WebhookSecretEnc == "") {
			return invalid("secret_key", "required")
		}
		if err := store.SaveProviderSettings(ctx, tx, scope, row, expected, s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "provider_settings.update", "payment_provider", code, map[string]any{
			"enabled": row.Enabled, "mode": row.Mode, "sepa_enabled": row.SEPAEnabled, "methods": methods,
			"credentials_changed": in.SecretKey != nil, "webhook_secret_changed": in.WebhookSecret != nil,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "provider", ID: row.ID, Change: event.ChangeUpdated}); err != nil {
			return err
		}
		out = s.settingsView(row)
		return nil
	})
	return out, err
}

// credentialTester is implemented by drivers that can test credentials (Stripe: account balance).
type credentialTester interface {
	TestCredentials(ctx context.Context) error
}

// TestProvider calls the provider with the stored credentials (providers.configure). The answer carries a code,
// never provider text.
func (s *Service) TestProvider(ctx context.Context, p *auth.Principal, marinaID, code string) (bool, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return false, "", err
	}
	drv, err := s.onlineDriver(code)
	if err != nil {
		return false, "", err
	}
	row, err := store.GetProviderSettings(ctx, s.d.DB.DB, scope, code)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.CredentialsEnc == "") {
		return false, "no_credentials", nil
	}
	if err != nil {
		return false, "", err
	}
	t, ok := drv.(credentialTester)
	if !ok {
		return false, "not_supported", nil
	}
	cctx, err := s.withCreds(ctx, scope, row)
	if err != nil {
		return false, "credentials_unreadable", nil
	}
	if err := t.TestCredentials(cctx); err != nil {
		return false, providerCode(err), nil
	}
	return true, "", nil
}

// withCreds decrypts the provider credentials into the context of one call.
func (s *Service) withCreds(ctx context.Context, scope store.MarinaScope, row *store.PaymentProviderSettings) (context.Context, error) {
	if s.d.Keys == nil || row.CredentialsEnc == "" {
		return nil, ErrProviderDisabled
	}
	raw, err := s.d.Keys.DecryptString(row.CredentialsEnc, credsAAD(scope.ID(), row.ProviderCode))
	if err != nil {
		return nil, err
	}
	var creds map[string]string
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, err
	}
	return ext.WithProviderCredentials(ctx, creds), nil
}

// providerCode reduces a driver error to a code (type and code only).
func providerCode(err error) string {
	var pc interface{ ProviderCode() string }
	if errors.As(err, &pc) {
		if c := pc.ProviderCode(); c != "" {
			return c
		}
	}
	if errors.Is(err, ext.ErrNotSupported) {
		return "not_supported"
	}
	return "unavailable"
}

// providerError wraps a driver error as *ProviderError (502 provider_error, details.code).
func providerError(err error) error { return &ProviderError{Code: providerCode(err)} }

// enabled is an online provider usable by a marina: its driver, settings and capability.
type enabled struct {
	drv ext.PaymentProvider
	row *store.PaymentProviderSettings
}

// enabledProvider returns the first enabled provider of the marina with the capability (sorted by code); none is
// ErrProviderDisabled.
func (s *Service) enabledProvider(ctx context.Context, db bun.IDB, scope store.MarinaScope, need func(ext.PaymentCapabilities, *store.PaymentProviderSettings) bool) (*enabled, error) {
	rows, err := store.ListProviderSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := &rows[i]
		if !r.Enabled || r.CredentialsEnc == "" {
			continue
		}
		drv, err := s.onlineDriver(r.ProviderCode)
		if err != nil {
			continue
		}
		if need(drv.Capabilities(), r) {
			return &enabled{drv: drv, row: r}, nil
		}
	}
	return nil, ErrProviderDisabled
}

func needCheckout(c ext.PaymentCapabilities, _ *store.PaymentProviderSettings) bool {
	return c.Checkout
}

func needSEPA(c ext.PaymentCapabilities, r *store.PaymentProviderSettings) bool {
	return c.MandateSetup && c.MandateCharge && r.SEPAEnabled
}

// providerByCode returns an enabled provider by code (webhooks, reconciliation of an intent).
func (s *Service) providerByCode(ctx context.Context, db bun.IDB, scope store.MarinaScope, code string) (*enabled, error) {
	drv, err := s.onlineDriver(code)
	if err != nil {
		return nil, err
	}
	row, err := store.GetProviderSettings(ctx, db, scope, code)
	if err != nil {
		return nil, err
	}
	return &enabled{drv: drv, row: row}, nil
}

// WebhookProvider returns the driver and the decrypted webhook secret of a provider of a marina, for the webhook
// handler (which owns *http.Request). Unknown provider or marina: ErrNotFound; a provider configured for the marina
// without a webhook secret: ErrWebhookSecretMissing (401 invalid_signature, m5-plan §5.4). A disabled provider
// keeps receiving its webhooks (money may still arrive for intents created before it was disabled).
func (s *Service) WebhookProvider(ctx context.Context, providerCode, marinaID string) (ext.PaymentProvider, []byte, error) {
	if !store.ValidID(marinaID) {
		return nil, nil, ErrNotFound
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	e, err := s.providerByCode(ctx, s.d.DB.DB, scope, providerCode)
	if err != nil || s.d.Keys == nil {
		return nil, nil, ErrNotFound
	}
	if e.row.WebhookSecretEnc == "" {
		return nil, nil, ErrWebhookSecretMissing
	}
	secret, err := s.d.Keys.DecryptString(e.row.WebhookSecretEnc, credsAAD(marinaID, providerCode))
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return e.drv, []byte(secret), nil
}

// ProviderEnabled reports whether the marina has an enabled provider with checkout (pay page, meta).
func (s *Service) ProviderEnabled(ctx context.Context, scope store.MarinaScope) bool {
	_, err := s.enabledProvider(ctx, s.d.DB.DB, scope, needCheckout)
	return err == nil
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}
