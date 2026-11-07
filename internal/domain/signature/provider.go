package signature

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

func credentialsAAD(marinaID string) string { return "signature-credentials:" + marinaID }
func webhookAAD(marinaID string) string     { return "signature-webhook:" + marinaID }

// withCredentials decrypts the provider credentials of a marina into the context of one call (only in memory).
func (s *Service) withCredentials(ctx context.Context, scope store.MarinaScope) (context.Context, error) {
	st, err := store.GetSignatureSettings(ctx, s.d.DB.DB, scope)
	if errors.Is(err, store.ErrNotFound) {
		return ext.WithProviderCredentials(ctx, map[string]string{}), nil
	}
	if err != nil {
		return nil, err
	}
	creds := map[string]string{}
	if st.CredentialsEnc != "" {
		raw, err := s.d.Keys.DecryptString(st.CredentialsEnc, credentialsAAD(scope.ID()))
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &creds)
	}
	return ext.WithProviderCredentials(ctx, creds), nil
}

// Settings are the provider settings without secrets.
type Settings struct {
	ProviderCode       string
	HasCredentials     bool
	HasWebhookSecret   bool
	AvailableProviders []string
	WebhookURL         string
	Version            int
}

// SettingsInput changes the provider settings; nil = unchanged, an empty provider clears it.
type SettingsInput struct {
	ProviderCode  *string
	Credentials   map[string]string // write-only
	WebhookSecret *string           // write-only
}

func (s *Service) settingsView(scope store.MarinaScope, st *store.SignatureSettings) *Settings {
	out := &Settings{AvailableProviders: s.ProviderCodes()}
	if st != nil {
		out.ProviderCode, out.Version = st.ProviderCode, st.Version
		out.HasCredentials, out.HasWebhookSecret = st.CredentialsEnc != "", st.WebhookSecretEnc != ""
	}
	if out.ProviderCode != "" && s.d.Links != nil {
		out.WebhookURL = s.d.Links.BaseURL() + "/webhooks/signature/" + out.ProviderCode + "/" + scope.ID()
	}
	return out
}

// GetSettings returns the provider settings (providers.configure).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	st, err := store.GetSignatureSettings(ctx, s.d.DB.DB, scope)
	if errors.Is(err, store.ErrNotFound) {
		return s.settingsView(scope, nil), nil
	}
	if err != nil {
		return nil, err
	}
	return s.settingsView(scope, st), nil
}

// UpdateSettings sets the provider and its write-only credentials (providers.configure). A provider that is not
// registered is ErrProviderUnknown. ifMatch 0 creates the row.
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	var out *Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		st, err := store.GetSignatureSettings(ctx, tx, scope)
		switch {
		case errors.Is(err, store.ErrNotFound):
			st = &store.SignatureSettings{CreatedAt: s.now()}
			if ifMatch != 0 {
				return store.ErrVersionMismatch
			}
		case err != nil:
			return err
		default:
			if ifMatch == 0 {
				ifMatch = st.Version
			}
		}
		diff := map[string]any{}
		if in.ProviderCode != nil {
			code := strings.TrimSpace(*in.ProviderCode)
			if code != "" && !slices.Contains(s.ProviderCodes(), code) {
				return ErrProviderUnknown
			}
			st.ProviderCode, diff["provider_code"] = code, code
		}
		if in.Credentials != nil {
			b, err := json.Marshal(in.Credentials)
			if err != nil {
				return err
			}
			if st.CredentialsEnc, err = s.d.Keys.EncryptString(string(b), credentialsAAD(scope.ID())); err != nil {
				return err
			}
			diff["credentials"] = "changed"
		}
		if in.WebhookSecret != nil {
			if st.WebhookSecretEnc, err = s.d.Keys.EncryptString(*in.WebhookSecret, webhookAAD(scope.ID())); err != nil {
				return err
			}
			diff["webhook_secret"] = "changed"
		}
		st.UpdatedAt = s.now()
		if err := store.SaveSignatureSettings(ctx, tx, scope, ifMatch, st); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "signature_settings.update",
			EntityType: "signature_settings", EntityID: scope.ID(), Diff: diff,
		}); err != nil {
			return err
		}
		out = s.settingsView(scope, st)
		return nil
	})
	return out, err
}

// WebhookProvider returns the registered provider and the decrypted webhook secret of a marina for a callback. An
// unknown provider, an unknown marina or a marina not configured with the provider are all ErrProviderUnknown
// (404 on the webhook route: existence must not leak).
func (s *Service) WebhookProvider(ctx context.Context, providerCode, marinaID string) (ext.SignatureProvider, []byte, error) {
	prov, ok := s.provider(providerCode)
	if !ok || !store.ValidID(marinaID) {
		return nil, nil, ErrProviderUnknown
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, nil, ErrProviderUnknown
	}
	st, err := store.GetSignatureSettings(ctx, s.d.DB.DB, scope)
	if err != nil || st.ProviderCode != providerCode || st.WebhookSecretEnc == "" {
		return nil, nil, ErrProviderUnknown
	}
	secret, err := s.d.Keys.DecryptString(st.WebhookSecretEnc, webhookAAD(scope.ID()))
	if err != nil {
		return nil, nil, ErrProviderUnknown
	}
	return prov, []byte(secret), nil
}

// ApplyProviderEvent applies a verified provider callback: the envelope status is reconciled with the request
// (conditional updates, so replays change nothing). Unknown envelopes are ignored.
func (s *Service) ApplyProviderEvent(ctx context.Context, providerCode, marinaID string, ev *ext.SignatureEvent) error {
	if ev == nil || ev.EnvelopeRef == "" {
		return nil
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return ErrProviderUnknown
	}
	r, err := store.SignatureRequestByEnvelope(ctx, s.d.DB.DB, scope, providerCode, ev.EnvelopeRef)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.reconcile(ctx, scope, r, &ext.SignatureEnvelope{Ref: ev.EnvelopeRef, Status: ev.Status})
}

// reconcile applies an envelope state to a request: signer statuses, completion (downloading the signed PDF) or
// failure.
func (s *Service) reconcile(ctx context.Context, scope store.MarinaScope, r *store.SignatureRequest, env *ext.SignatureEnvelope) error {
	if r.Status != StatusSent && r.Status != StatusPartiallySigned {
		return nil
	}
	prov, ok := s.provider(r.ProviderCode)
	if !ok {
		return ErrProviderUnknown
	}
	switch env.Status {
	case ext.EnvelopeSigned:
		cctx, err := s.withCredentials(ctx, scope)
		if err != nil {
			return err
		}
		doc, err := prov.Download(cctx, r.EnvelopeRef)
		if err != nil {
			return err
		}
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			signers, err := store.SignersOf(ctx, tx, scope, r.ID)
			if err != nil {
				return err
			}
			for _, sg := range signers {
				if sg.Status == SignerSigned {
					continue
				}
				sealed, err := s.sealEvidence(sg.ID, evidence{Method: "provider", RequestID: r.ID, SignedAt: s.now()})
				if err != nil {
					return err
				}
				if _, err := store.TransitionSigner(ctx, tx, scope, sg.ID, []string{SignerWaitingStatus, SignerInvited}, SignerSigned,
					map[string]any{"signed_at": s.now(), "evidence_enc": sealed}, s.now()); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		return s.complete(ctx, scope, r.ID, doc)
	case ext.EnvelopeDeclined, ext.EnvelopeExpired, ext.EnvelopeCancelled:
		status := map[string]string{
			ext.EnvelopeDeclined: StatusDeclined, ext.EnvelopeExpired: StatusExpired, ext.EnvelopeCancelled: StatusCancelled,
		}[env.Status]
		return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			err := s.failTx(ctx, tx, scope, r, status, audit.System)
			if errors.Is(err, ErrNotSigner) {
				return nil
			}
			return err
		})
	case ext.EnvelopePartiallySigned:
		return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			signers, err := store.SignersOf(ctx, tx, scope, r.ID)
			if err != nil {
				return err
			}
			for _, st := range env.Signers {
				if st.Status != SignerSigned {
					continue
				}
				for _, sg := range signers {
					if sg.Seq == st.Seq && sg.Status != SignerSigned {
						if _, err := store.TransitionSigner(ctx, tx, scope, sg.ID, []string{SignerWaitingStatus, SignerInvited}, SignerSigned,
							map[string]any{"signed_at": s.now()}, s.now()); err != nil {
							return err
						}
					}
				}
			}
			_, err = store.TransitionSignatureRequest(ctx, tx, scope, r.ID, []string{StatusSent}, StatusPartiallySigned, nil, s.now())
			return err
		})
	}
	return nil
}

// poll asks the provider for the envelope state (job signatures.sync).
func (s *Service) poll(ctx context.Context, scope store.MarinaScope, r *store.SignatureRequest) error {
	prov, ok := s.provider(r.ProviderCode)
	if !ok || r.EnvelopeRef == "" {
		return nil
	}
	cctx, err := s.withCredentials(ctx, scope)
	if err != nil {
		return err
	}
	env, err := prov.Status(cctx, r.EnvelopeRef)
	if err != nil || env == nil {
		return err
	}
	return s.reconcile(ctx, scope, r, env)
}
