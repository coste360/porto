package signature

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

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

// OTP policy (m4-plan §6.5, §15 D13): 6 digits, 10 minutes, 5 attempts per code, 3 codes per 15 minutes.
const (
	OTPDigits      = 6
	OTPTTL         = 10 * time.Minute
	OTPMaxAttempts = 5
	OTPMaxCodes    = 3
	OTPWindow      = 15 * time.Minute
)

// ClientMeta are the request data recorded in the evidence (the user agent only as SHA-256).
type ClientMeta struct {
	IP        string
	UserAgent string
}

// PublicSignature is what the signing page shows to the signer of a link (only this signer's data).
type PublicSignature struct {
	RequestStatus, SignerStatus, Role, DocumentTitle string
	Name, FiscalCode                                 string
	AttemptsLeft                                     int
}

// PublicResult is the outcome of a public signing action.
type PublicResult struct {
	SignerStatus, RequestStatus string
	AttemptsLeft                int
}

type signerCtx struct {
	scope store.MarinaScope
	r     *store.SignatureRequest
	sg    *store.SignatureSigner
}

func (s *Service) resolveSigner(ctx context.Context, db bun.IDB, l *magiclink.Link) (*signerCtx, error) {
	if l.Purpose != magiclink.PurposeSignature || l.SubjectType != linkSubject {
		return nil, magiclink.ErrInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, db, l.MarinaID)
	if err != nil {
		return nil, magiclink.ErrInvalid
	}
	sg, err := store.GetSignatureSigner(ctx, db, scope, l.SubjectID)
	if err != nil || sg.LinkID != l.ID {
		return nil, magiclink.ErrInvalid
	}
	r, err := store.GetSignatureRequest(ctx, db, scope, sg.RequestID)
	if err != nil {
		return nil, magiclink.ErrInvalid
	}
	return &signerCtx{scope: scope, r: r, sg: sg}, nil
}

func (s *Service) resolveToken(ctx context.Context, slug, token string) (*signerCtx, error) {
	l, err := s.d.Links.ResolvePurpose(ctx, slug, token, magiclink.PurposeSignature)
	if err != nil {
		return nil, err
	}
	return s.resolveSigner(ctx, s.d.DB.DB, l)
}

func (c *signerCtx) canAct(now time.Time) bool {
	return c.sg.Status == SignerInvited && (c.r.Status == StatusSent || c.r.Status == StatusPartiallySigned) &&
		now.Before(c.r.ExpiresAt) && c.r.Driver == DriverOTP
}

// PublicView returns the signing page data of a resolved link and records the first opening (time, IP and user
// agent hash go to the evidence).
func (s *Service) PublicView(ctx context.Context, l *magiclink.Link, meta ClientMeta) (*PublicSignature, error) {
	c, err := s.resolveSigner(ctx, s.d.DB.DB, l)
	if err != nil {
		return nil, err
	}
	if c.sg.OpenedAt.IsZero() && c.canAct(s.now()) {
		ev := s.readEvidence(c.sg)
		ev.Method, ev.IP, ev.UserAgentSHA256, ev.OpenedAt = "otp", meta.IP, crypto.SHA256Hex(meta.UserAgent), s.now()
		sealed, err := s.sealEvidence(c.sg.ID, ev)
		if err != nil {
			return nil, err
		}
		if err := store.SetSignerColumns(ctx, s.d.DB.DB, c.scope, c.sg.ID, map[string]any{
			"opened_at": s.now(), "evidence_enc": sealed,
		}, s.now()); err != nil {
			return nil, err
		}
	}
	out := &PublicSignature{RequestStatus: c.r.Status, SignerStatus: c.sg.Status, Role: c.sg.Role, AttemptsLeft: OTPMaxAttempts}
	out.Name, _ = s.d.Keys.DecryptString(c.sg.DisplayNameEnc, signerAAD(c.sg.ID))
	if c.sg.CustomerID != "" {
		if cu, err := store.GetCustomer(ctx, s.d.DB.DB, c.scope, c.sg.CustomerID); err == nil && cu.Kind == "person" {
			out.FiscalCode = cu.FiscalCode
		}
	}
	if doc, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, c.scope, c.r.DocumentID); err == nil {
		out.DocumentTitle = docs.Title(docs.Kind(doc.Kind), c.sg.Locale)
	}
	if o, err := store.LatestSignatureOTP(ctx, s.d.DB.DB, c.scope, c.sg.ID); err == nil && o.ConsumedAt.IsZero() && s.now().Before(o.ExpiresAt) {
		out.AttemptsLeft = max(0, OTPMaxAttempts-o.Attempts)
	}
	return out, nil
}

// PublicDocument returns the PDF to sign of a resolved signature link.
func (s *Service) PublicDocument(ctx context.Context, l *magiclink.Link) ([]byte, error) {
	c, err := s.resolveSigner(ctx, s.d.DB.DB, l)
	if err != nil {
		return nil, err
	}
	if c.r.Status == StatusSigned && c.r.SignedDocumentID != "" {
		b, _, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, c.scope, c.r.SignedDocumentID)
		return b, err
	}
	b, _, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, c.scope, c.r.DocumentID)
	return b, err
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// RequestOTP sends a new code to the signer of a link after the identity confirmation (≤ 3 codes per 15 minutes,
// ErrOTPRateLimited beyond). The code is sent by email and by every registered sms notifier when the signer has a
// phone; it is stored only as an HMAC.
func (s *Service) RequestOTP(ctx context.Context, slug, token string, identityConfirmed bool, meta ClientMeta) (*PublicResult, error) {
	c, err := s.resolveToken(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	if !identityConfirmed {
		v := &auth.ValidationError{}
		v.Add("identity_confirmed", "required")
		return nil, v.Err()
	}
	now := s.now()
	if !c.canAct(now) {
		return nil, ErrNotSigner
	}
	code, err := randomCode()
	if err != nil {
		return nil, err
	}
	var phone string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n, err := store.CountSignatureOTPsSince(ctx, tx, c.scope, c.sg.ID, now.Add(-OTPWindow))
		if err != nil {
			return err
		}
		if n >= OTPMaxCodes {
			return ErrOTPRateLimited
		}
		o := &store.SignatureOTP{
			ID: store.NewID(), MarinaID: c.scope.ID(), SignerID: c.sg.ID, CodeHMAC: s.otpHMAC(c.sg.ID, code),
			ExpiresAt: now.Add(OTPTTL), SentAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertSignatureOTP(ctx, tx, o); err != nil {
			return err
		}
		name, _ := s.d.Keys.DecryptString(c.sg.DisplayNameEnc, signerAAD(c.sg.ID))
		declared := name
		if c.sg.CustomerID != "" {
			if cu, err := store.GetCustomer(ctx, tx, c.scope, c.sg.CustomerID); err == nil {
				if cu.Kind == "person" && cu.FiscalCode != "" {
					declared += ", " + cu.FiscalCode
				}
				phone = docs.FirstContact(cu.Phones)
			}
		}
		ev := s.readEvidence(c.sg)
		ev.Method, ev.DeclaredIdentity, ev.OTPSentAt, ev.EmailHash = "otp", declared, now, s.emailHash(c.sg)
		if ev.OpenedAt.IsZero() {
			ev.OpenedAt, ev.IP, ev.UserAgentSHA256 = now, meta.IP, crypto.SHA256Hex(meta.UserAgent)
		}
		sealed, err := s.sealEvidence(c.sg.ID, ev)
		if err != nil {
			return err
		}
		if err := store.SetSignerColumns(ctx, tx, c.scope, c.sg.ID, map[string]any{"evidence_enc": sealed}, now); err != nil {
			return err
		}
		email, _ := s.d.Keys.DecryptString(c.sg.EmailEnc, signerAAD(c.sg.ID))
		if err := s.mailTx(ctx, tx, c.scope, c.r, c.sg, email, notify.TemplateSignatureOTP, "sig:otp:"+o.ID, map[string]string{
			notify.ValueCode:      code,
			notify.ValueExpiresAt: o.ExpiresAt.UTC().Format("15:04 UTC"),
		}); err != nil {
			return err
		}
		return s.write(ctx, tx, c.scope, audit.System, "signature.otp_sent", c.r, map[string]any{"signer_seq": c.sg.Seq}, "")
	})
	if err != nil {
		return nil, err
	}
	if phone != "" {
		for _, n := range s.d.Notifiers {
			if n.Channel() != "sms" {
				continue
			}
			// Best effort, not recorded in message_log (§15 D22).
			if err := n.Notify(context.WithoutCancel(ctx), ext.Notification{
				Channel: "sms", Template: notify.TemplateSignatureOTP, Locale: c.sg.Locale, To: phone,
				Data: map[string]string{notify.ValueCode: code},
			}); err != nil {
				s.d.Log.WarnContext(ctx, "otp sms not sent", "notifier", n.Name())
			}
		}
	}
	return &PublicResult{SignerStatus: c.sg.Status, RequestStatus: c.r.Status, AttemptsLeft: OTPMaxAttempts}, nil
}

// VerifyOTP signs with the code received: a correct live code signs in one transaction (evidence included) and the
// request is completed after commit when every signer has signed; a wrong code counts one attempt
// (*OTPError with the attempts left); a code without attempts left is ErrOTPLocked.
func (s *Service) VerifyOTP(ctx context.Context, slug, token, code string, meta ClientMeta) (*PublicResult, error) {
	c, err := s.resolveToken(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if !c.canAct(now) {
		return nil, ErrNotSigner
	}
	code = strings.TrimSpace(code)
	o, err := store.LatestSignatureOTP(ctx, s.d.DB.DB, c.scope, c.sg.ID)
	if err != nil || !o.ConsumedAt.IsZero() || !now.Before(o.ExpiresAt) {
		return nil, &OTPError{AttemptsLeft: 0}
	}
	if o.Attempts >= OTPMaxAttempts {
		return nil, ErrOTPLocked
	}
	if len(code) == OTPDigits && hmac.Equal([]byte(s.otpHMAC(c.sg.ID, code)), []byte(o.CodeHMAC)) {
		all := false
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			ok, err := store.ConsumeOTP(ctx, tx, o.ID, OTPMaxAttempts, now)
			if err != nil {
				return err
			}
			if !ok {
				return ErrOTPLocked
			}
			sg, err := store.GetSignatureSigner(ctx, tx, c.scope, c.sg.ID)
			if err != nil {
				return err
			}
			r, err := store.GetSignatureRequest(ctx, tx, c.scope, c.r.ID)
			if err != nil {
				return err
			}
			if r.Status != StatusSent && r.Status != StatusPartiallySigned {
				return ErrNotSigner
			}
			ev := s.readEvidence(sg)
			ev.Method, ev.DocumentSHA256 = "otp", s.documentSHA(ctx, tx, c.scope, r.DocumentID)
			all, err = s.signerSignedTx(ctx, tx, c.scope, r, sg, ev, audit.System, []string{SignerInvited})
			c.r = r
			return err
		})
		if err != nil {
			return nil, err
		}
		if all {
			if err := s.complete(context.WithoutCancel(ctx), c.scope, c.r.ID, nil); err != nil {
				s.d.Log.WarnContext(ctx, "signature completion deferred to the job", "signature_request_id", c.r.ID)
			}
		}
		r, _ := store.GetSignatureRequest(ctx, s.d.DB.DB, c.scope, c.r.ID)
		status := c.r.Status
		if r != nil {
			status = r.Status
		}
		return &PublicResult{SignerStatus: SignerSigned, RequestStatus: status}, nil
	}
	var left int
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n, ok, err := store.CountOTPAttempt(ctx, tx, o.ID, OTPMaxAttempts, now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrOTPLocked
		}
		left = OTPMaxAttempts - n
		return s.write(ctx, tx, c.scope, audit.System, "signature.otp_failed", c.r, map[string]any{"signer_seq": c.sg.Seq, "attempts": n}, "")
	})
	if err != nil {
		return nil, err
	}
	return nil, &OTPError{AttemptsLeft: left}
}

func (s *Service) documentSHA(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) string {
	if d, err := store.GetGeneratedDocument(ctx, db, scope, id); err == nil {
		return d.SHA256
	}
	return ""
}

// DeclinePublic declines the signature: the signer and the request are declined and the subject is called back.
func (s *Service) DeclinePublic(ctx context.Context, slug, token, reason string, meta ClientMeta) (*PublicResult, error) {
	c, err := s.resolveToken(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	if !c.canAct(s.now()) {
		return nil, ErrNotSigner
	}
	_ = reason // free text from the customer: not stored (it may hold personal data)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ok, err := store.TransitionSigner(ctx, tx, c.scope, c.sg.ID, []string{SignerInvited}, SignerDeclined,
			map[string]any{"declined_at": s.now()}, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotSigner
		}
		return s.failTx(ctx, tx, c.scope, c.r, StatusDeclined, audit.System)
	})
	if err != nil {
		return nil, err
	}
	return &PublicResult{SignerStatus: SignerDeclined, RequestStatus: StatusDeclined}, nil
}
