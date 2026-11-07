package signature

import (
	"context"
	"errors"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// onDocumentReady is the docs post-render hook: requests waiting for the document are sent.
func (s *Service) onDocumentReady(ctx context.Context, marinaID, documentID string) {
	rows, err := store.SignatureRequestsWithDocument(ctx, s.d.DB.DB, marinaID, documentID)
	if err != nil {
		return
	}
	for _, r := range rows {
		if err := s.dispatch(ctx, marinaID, r.ID); err != nil {
			s.d.Log.WarnContext(ctx, "signature dispatch deferred to the job", "signature_request_id", r.ID)
		}
	}
}

// dispatch sends a request whose document is ready (pending_document → sent). Idempotent.
func (s *Service) dispatch(ctx context.Context, marinaID, requestID string) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return err
	}
	r, err := store.GetSignatureRequest(ctx, s.d.DB.DB, scope, requestID)
	if err != nil || r.Status != StatusPendingDocument {
		return err
	}
	doc, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, r.DocumentID)
	if err != nil {
		return err
	}
	if doc.Status != store.DocReady {
		if doc.Status == store.DocPending || doc.Status == store.DocRendering {
			return s.d.Docs.RenderPending(ctx, doc.ID) // the hook calls dispatch again when ready
		}
		return nil
	}
	set := map[string]any{}
	if r.Driver == DriverProvider {
		env, err := s.providerRequest(ctx, scope, r)
		if err != nil {
			return err
		}
		set["envelope_ref"] = env.Ref
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ok, err := store.TransitionSignatureRequest(ctx, tx, scope, r.ID, []string{StatusPendingDocument}, StatusSent, set, s.now())
		if err != nil || !ok {
			return err
		}
		r.Status = StatusSent
		if r.Driver == DriverOTP {
			if err := s.inviteNextTx(ctx, tx, scope, r); err != nil {
				return err
			}
		}
		return s.write(ctx, tx, scope, audit.System, "signature.invite", r, map[string]any{"status": StatusSent}, "")
	})
}

// inviteNextTx invites the signers due now: every waiting signer for parallel requests, the first waiting one for
// sequential requests (none while an invited one has not signed yet).
func (s *Service) inviteNextTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest) error {
	signers, err := store.SignersOf(ctx, tx, scope, r.ID)
	if err != nil {
		return err
	}
	for i := range signers {
		sg := &signers[i]
		if r.OrderMode == "sequential" {
			if sg.Status == SignerInvited {
				return nil
			}
			if sg.Status != SignerWaitingStatus {
				continue
			}
			return s.inviteTx(ctx, tx, scope, r, sg)
		}
		if sg.Status == SignerWaitingStatus {
			if err := s.inviteTx(ctx, tx, scope, r, sg); err != nil {
				return err
			}
		}
	}
	return nil
}

// Signer statuses used by the service.
const (
	SignerInvited  = "invited"
	SignerSigned   = "signed"
	SignerDeclined = "declined"
)

func (s *Service) inviteTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, sg *store.SignatureSigner) error {
	now := s.now()
	set := map[string]any{"invited_at": now}
	email := ""
	if sg.EmailEnc != "" {
		email, _ = s.d.Keys.DecryptString(sg.EmailEnc, signerAAD(sg.ID))
	}
	var token string
	if email != "" && s.d.Links != nil {
		var id string
		var err error
		token, id, err = s.d.Links.CreateTx(ctx, tx, scope, magiclink.PurposeSignature, linkSubject, sg.ID, sg.CustomerID, r.ExpiresAt)
		if err != nil {
			return err
		}
		set["link_id"] = id
	}
	ok, err := store.TransitionSigner(ctx, tx, scope, sg.ID, []string{SignerWaitingStatus}, SignerInvited, set, now)
	if err != nil || !ok {
		return err
	}
	sg.Status = SignerInvited
	if token != "" {
		if err := s.mailTx(ctx, tx, scope, r, sg, email, notify.TemplateSignatureRequest, "sig:"+sg.ID+":invite", map[string]string{
			notify.ValueLink: s.linkURL(ctx, tx, scope, token),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) linkURL(ctx context.Context, db bun.IDB, scope store.MarinaScope, token string) string {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return ""
	}
	return s.d.Links.URL(m.Slug, magiclink.PurposeSignature, token)
}

// mailTx queues an email to a signer with the common values (marina, customer name, document title, deadline).
func (s *Service) mailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, sg *store.SignatureSigner, email, template, dedupe string, values map[string]string) error {
	if s.d.Notify == nil || email == "" {
		return nil
	}
	name, _ := s.d.Keys.DecryptString(sg.DisplayNameEnc, signerAAD(sg.ID))
	v := map[string]string{
		notify.ValueCustomerName: name,
		notify.ValueDeadlineOn:   s.marinaDate(ctx, tx, scope, r.ExpiresAt, sg.Locale),
	}
	if m, err := store.GetMarina(ctx, tx, scope); err == nil {
		v[notify.ValueMarinaName] = m.Name
	}
	if doc, err := store.GetGeneratedDocument(ctx, tx, scope, r.DocumentID); err == nil {
		v[notify.ValueDocumentTitle] = docs.Title(docs.Kind(doc.Kind), sg.Locale)
	}
	for k, x := range values {
		v[k] = x
	}
	_, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: template, MarinaID: scope.ID(), CustomerID: sg.CustomerID, UserID: sg.UserID, To: email,
		Locale: sg.Locale, DedupeKey: dedupe, SourceType: "signature", SourceID: r.ID, Values: v,
	})
	return err
}

// RemindRequest sends a reminder to the invited signers still to sign (contracts.write).
func (s *Service) RemindRequest(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Request, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	var out *Request
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetSignatureRequest(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ifMatch != 0 && r.Version != ifMatch {
			return store.ErrVersionMismatch
		}
		if r.Status != StatusSent && r.Status != StatusPartiallySigned {
			return ErrNotSigner
		}
		if err := s.remindTx(ctx, tx, scope, r, p.Actor()); err != nil {
			return err
		}
		out, err = s.getRequest(ctx, tx, scope, p, id)
		return err
	})
	return out, err
}

func (s *Service) remindTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, a audit.Actor) error {
	n := r.RemindersSent + 1
	if err := store.TouchSignatureRequest(ctx, tx, scope, r.ID, r.Version, map[string]any{
		"reminders_sent": n, "last_reminder_at": s.now(),
	}, s.now()); err != nil {
		return err
	}
	r.Version++
	signers, err := store.SignersOf(ctx, tx, scope, r.ID)
	if err != nil {
		return err
	}
	for i := range signers {
		sg := &signers[i]
		if sg.Status != SignerInvited || sg.EmailEnc == "" || r.Driver != DriverOTP {
			continue
		}
		email, _ := s.d.Keys.DecryptString(sg.EmailEnc, signerAAD(sg.ID))
		// A reminder rotates the link: the token of the previous email is not known any more.
		token, linkID, err := s.d.Links.CreateTx(ctx, tx, scope, magiclink.PurposeSignature, linkSubject, sg.ID, sg.CustomerID, r.ExpiresAt)
		if err != nil {
			return err
		}
		if err := store.SetSignerColumns(ctx, tx, scope, sg.ID, map[string]any{"link_id": linkID}, s.now()); err != nil {
			return err
		}
		if err := s.mailTx(ctx, tx, scope, r, sg, email, notify.TemplateSignatureReminder,
			"sig:"+sg.ID+":reminder:"+strconv.Itoa(n), map[string]string{notify.ValueLink: s.linkURL(ctx, tx, scope, token)}); err != nil {
			return err
		}
	}
	return s.write(ctx, tx, scope, a, "signature.invite", r, map[string]any{"reminder": n}, "")
}

// providerRequest starts the envelope at the provider (outside any transaction, credentials only in memory).
func (s *Service) providerRequest(ctx context.Context, scope store.MarinaScope, r *store.SignatureRequest) (*ext.SignatureEnvelope, error) {
	prov, ok := s.provider(r.ProviderCode)
	if !ok {
		return nil, ErrProviderUnknown
	}
	ctx, err := s.withCredentials(ctx, scope)
	if err != nil {
		return nil, err
	}
	pdfBytes, doc, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, scope, r.DocumentID)
	if err != nil {
		return nil, err
	}
	signers, err := store.SignersOf(ctx, s.d.DB.DB, scope, r.ID)
	if err != nil {
		return nil, err
	}
	req := ext.SignatureRequest{
		MarinaID: scope.ID(), RequestID: r.ID, Document: pdfBytes, DocumentSHA256: doc.SHA256,
		Title: docs.Title(docs.Kind(doc.Kind), doc.Locale), Sequential: r.OrderMode == "sequential", ExpiresAt: r.ExpiresAt,
		CallbackURL: s.d.Links.BaseURL() + "/webhooks/signature/" + r.ProviderCode + "/" + scope.ID(),
	}
	for _, sg := range signers {
		x := ext.Signer{Seq: sg.Seq, Role: sg.Role, Locale: sg.Locale}
		x.Name, _ = s.d.Keys.DecryptString(sg.DisplayNameEnc, signerAAD(sg.ID))
		if sg.EmailEnc != "" {
			x.Email, _ = s.d.Keys.DecryptString(sg.EmailEnc, signerAAD(sg.ID))
		}
		req.Signers = append(req.Signers, x)
	}
	env, err := prov.Request(ctx, req)
	if err != nil {
		return nil, err
	}
	if env == nil || env.Ref == "" {
		return nil, errors.New("signature: provider returned no envelope")
	}
	return env, nil
}

func (s *Service) providerCancel(ctx context.Context, marinaID, code, ref string) {
	prov, ok := s.provider(code)
	if !ok {
		return
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return
	}
	ctx, err = s.withCredentials(ctx, scope)
	if err != nil {
		return
	}
	if err := prov.Cancel(ctx, ref); err != nil {
		s.d.Log.WarnContext(ctx, "provider cancel failed", "provider", code)
	}
}

// emailHash is the keyed hash recorded in the evidence (never the address).
func (s *Service) emailHash(sg *store.SignatureSigner) string {
	if sg.EmailHash != "" {
		return sg.EmailHash
	}
	return crypto.SHA256Hex(sg.ID)
}
