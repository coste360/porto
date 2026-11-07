// Package signature runs signature requests (spec 15.4; m4-plan §6.5): signers in order or in parallel, the
// paper driver (scan upload and confirmation), the OTP driver ("firma elettronica semplice": email code, evidence
// page appended by incremental update) and the adapter over registered ext.SignatureProvider drivers (InfoCert is
// an interface only, spec 33 D5), reminders and expiry (job signatures.sync) and the provider webhook dispatch.
// Completion calls the registered Subject in the same transaction.
package signature

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Keys      *crypto.KeyRing
	Files     *filestore.Store
	Docs      *docs.Service
	Links     *magiclink.Service
	Notify    *notify.Service
	Providers []ext.SignatureProvider
	Notifiers []ext.Notifier // sms channel for OTP codes, when an embedder registers one
	Log       *slog.Logger
}

// Service is the signature service.
type Service struct {
	d Deps

	mu       sync.RWMutex
	subjects map[string]Subject
	otpKey   []byte
}

// NewService returns the service and registers the post-render hook that invites the signers of requests
// waiting for their document.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d, subjects: map[string]Subject{}}
	if d.Keys != nil {
		s.otpKey, _ = d.Keys.Derive("signature-otp", 32)
	}
	if d.Docs != nil {
		d.Docs.OnReady(s.onDocumentReady)
	}
	return s
}

// RegisterSubject registers the callbacks of a subject kind (called by internal/app at wiring).
func (s *Service) RegisterSubject(sub Subject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subjects[sub.Kind()] = sub
}

func (s *Service) subject(kind string) Subject {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subjects[kind]
}

// ProviderCodes returns the codes of the registered providers.
func (s *Service) ProviderCodes() []string {
	out := make([]string, 0, len(s.d.Providers))
	for _, p := range s.d.Providers {
		out = append(out, p.Code())
	}
	return out
}

func (s *Service) provider(code string) (ext.SignatureProvider, bool) {
	for _, p := range s.d.Providers {
		if p.Code() == code {
			return p, true
		}
	}
	return nil, false
}

// SignerSpec is one signer of a new request.
type SignerSpec struct {
	Role       string // customer, co_holder, outgoing_holder, incoming_holder, legal_representative, marina
	CustomerID string
	UserID     string
}

// OpenRequest opens a signature request for a subject.
type OpenRequest struct {
	SubjectType  string // contract, addendum, right_transfer
	SubjectID    string
	DocumentID   string // generated_documents row (finalised as PDF/A)
	Driver       string // paper, otp, provider
	ProviderCode string
	Signers      []SignerSpec
	Sequential   bool
	ExpiresAt    time.Time
	CreatedBy    string
}

var roles = map[string]bool{
	"customer": true, "co_holder": true, "outgoing_holder": true, "incoming_holder": true,
	"legal_representative": true, "marina": true,
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func signerAAD(id string) string   { return "signer:" + id }
func evidenceAAD(id string) string { return "evidence:" + id }

func actorOf(userID string) audit.Actor {
	if userID == "" {
		return audit.System
	}
	return audit.Actor{Type: audit.ActorUser, ID: userID}
}

func (s *Service) write(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action string, r *store.SignatureRequest, diff any, typ string) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "signature_request",
		EntityID: r.ID, Diff: diff,
	}); err != nil {
		return err
	}
	if typ == "" {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, event.SignatureData{
		SignatureRequestID: r.ID, SubjectType: r.SubjectType, SubjectID: r.SubjectID, Status: r.Status, Driver: r.Driver,
	})
	return err
}

// party is the decrypted identity of a signer.
type party struct {
	name, email, phone, fiscalCode, locale string
}

func (s *Service) resolveParty(ctx context.Context, tx bun.IDB, scope store.MarinaScope, sp SignerSpec) (party, error) {
	switch {
	case sp.CustomerID != "":
		c, err := store.GetCustomer(ctx, tx, scope, sp.CustomerID)
		if err != nil {
			return party{}, notFound(err)
		}
		p := docs.PartyOf(c)
		return party{name: p.DisplayName, email: p.Email, phone: p.Phone, fiscalCode: c.FiscalCode, locale: c.Locale}, nil
	case sp.UserID != "":
		u, err := store.MarinaMemberUser(ctx, tx, scope, sp.UserID)
		if err != nil {
			return party{}, notFound(err)
		}
		return party{name: u.Name, email: u.Email, locale: u.Locale}, nil
	default:
		m, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return party{}, err
		}
		return party{name: m.Name, locale: m.DefaultLocale}, nil
	}
}

// OpenTx opens the request inside the caller's transaction (status pending_document until the document is
// ready); the signers are invited after the render (job signatures.sync or the post-render hook). An open
// request for the same subject is ErrSignatureOpen (signature_requests_open_uidx). Returns the request id.
func (s *Service) OpenTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req OpenRequest) (string, error) {
	switch req.Driver {
	case DriverPaper, DriverOTP:
		req.ProviderCode = ""
	case DriverProvider:
		if _, ok := s.provider(req.ProviderCode); !ok {
			return "", ErrProviderUnknown
		}
	default:
		return "", fmt.Errorf("%w: driver %q", auth.ErrInvalidInput, req.Driver)
	}
	if len(req.Signers) == 0 || len(req.Signers) > 10 {
		return "", fmt.Errorf("%w: signers", auth.ErrInvalidInput)
	}
	now := s.now()
	if !req.ExpiresAt.After(now) {
		return "", fmt.Errorf("%w: expiry", auth.ErrInvalidInput)
	}
	if _, err := store.GetGeneratedDocument(ctx, tx, scope, req.DocumentID); err != nil {
		return "", notFound(err)
	}
	mode := "parallel"
	if req.Sequential {
		mode = "sequential"
	}
	r := &store.SignatureRequest{
		ID: store.NewID(), MarinaID: scope.ID(), SubjectType: req.SubjectType, SubjectID: req.SubjectID,
		DocumentID: req.DocumentID, Driver: req.Driver, ProviderCode: req.ProviderCode, OrderMode: mode,
		Status: StatusPendingDocument, ExpiresAt: store.UTC(req.ExpiresAt), CreatedByUserID: req.CreatedBy,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertSignatureRequest(ctx, tx, r); err != nil {
		if store.IsUniqueViolation(err) {
			return "", ErrSignatureOpen
		}
		return "", err
	}
	signers := make([]store.SignatureSigner, 0, len(req.Signers))
	for i, sp := range req.Signers {
		if !roles[sp.Role] {
			return "", fmt.Errorf("%w: role %q", auth.ErrInvalidInput, sp.Role)
		}
		p, err := s.resolveParty(ctx, tx, scope, sp)
		if err != nil {
			return "", err
		}
		id := store.NewID()
		nameEnc, err := s.d.Keys.EncryptString(p.name, signerAAD(id))
		if err != nil {
			return "", err
		}
		row := store.SignatureSigner{
			ID: id, MarinaID: scope.ID(), RequestID: r.ID, Seq: i + 1, Role: sp.Role, CustomerID: sp.CustomerID,
			UserID: sp.UserID, DisplayNameEnc: nameEnc, Locale: localeOr(p.locale), Status: SignerWaitingStatus,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if p.email != "" {
			if row.EmailEnc, err = s.d.Keys.EncryptString(p.email, signerAAD(id)); err != nil {
				return "", err
			}
			if s.d.Notify != nil {
				row.EmailHash = s.d.Notify.RecipientHash(p.email)
			}
		}
		signers = append(signers, row)
	}
	if err := store.InsertSignatureSigners(ctx, tx, signers); err != nil {
		return "", err
	}
	if err := s.write(ctx, tx, scope, actorOf(req.CreatedBy), "signature.open", r,
		map[string]any{
			"subject_type": r.SubjectType, "subject_id": r.SubjectID, "driver": r.Driver,
			"order_mode": mode, "signers": len(signers), "document_id": r.DocumentID,
		},
		event.SignatureRequested); err != nil {
		return "", err
	}
	marinaID, reqID := scope.ID(), r.ID
	store.AfterCommit(ctx, func() {
		bg := context.WithoutCancel(ctx)
		if err := s.dispatch(bg, marinaID, reqID); err != nil {
			s.d.Log.WarnContext(bg, "signature dispatch deferred to the job", "signature_request_id", reqID)
		}
	})
	return r.ID, nil
}

// SignerWaitingStatus is the initial status of a signer.
const SignerWaitingStatus = "waiting"

func localeOr(l string) string {
	if l == "en" {
		return "en"
	}
	return "it"
}

// CancelTx cancels the open request of a subject inside the caller's transaction (no-op when none is open). The
// subject is not called back: the caller is the subject itself.
func (s *Service) CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectType, subjectID string) error {
	r, err := store.OpenSignatureRequestOf(ctx, tx, scope, subjectType, subjectID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.cancelTx(ctx, tx, scope, r, audit.System, false)
}

func (s *Service) cancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, a audit.Actor, callback bool) error {
	ok, err := store.TransitionSignatureRequest(ctx, tx, scope, r.ID, store.SigOpenStatuses, StatusCancelled,
		map[string]any{"completed_at": s.now()}, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotSigner
	}
	r.Status = StatusCancelled
	if err := s.revokeLinksTx(ctx, tx, scope, r); err != nil {
		return err
	}
	if callback {
		if sub := s.subject(r.SubjectType); sub != nil {
			if err := sub.OnFailedTx(ctx, tx, scope, r.SubjectID, StatusCancelled); err != nil {
				return err
			}
		}
	}
	if err := s.write(ctx, tx, scope, a, "signature.cancel", r, map[string]any{"status": StatusCancelled}, event.SignatureFailed); err != nil {
		return err
	}
	if r.Driver == DriverProvider && r.EnvelopeRef != "" {
		ref, code, marinaID := r.EnvelopeRef, r.ProviderCode, scope.ID()
		store.AfterCommit(ctx, func() { s.providerCancel(context.WithoutCancel(ctx), marinaID, code, ref) })
	}
	return nil
}

func (s *Service) revokeLinksTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest) error {
	signers, err := store.SignersOf(ctx, tx, scope, r.ID)
	if err != nil {
		return err
	}
	for _, sg := range signers {
		if err := store.RevokeAccessLinks(ctx, tx, scope, magiclink.PurposeSignature, linkSubject, sg.ID, "", s.now()); err != nil {
			return err
		}
	}
	return nil
}

// linkSubject is the access_links.subject_type of signature links (one link per signer).
const linkSubject = "signature_signer"

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// Request is a request with its signers (names and emails decrypted only for customers.read).
type Request struct {
	store.SignatureRequest
	Signers []Signer
}

// Signer is a signer as shown to staff.
type Signer struct {
	store.SignatureSigner
	DisplayName, Email string
}

func (s *Service) withSigners(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.SignatureRequest) ([]Request, error) {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	signers, err := store.SignersOf(ctx, db, scope, ids...)
	if err != nil {
		return nil, err
	}
	pii := p != nil && p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
	by := map[string][]Signer{}
	for _, sg := range signers {
		v := Signer{SignatureSigner: sg}
		if pii {
			v.DisplayName, _ = s.d.Keys.DecryptString(sg.DisplayNameEnc, signerAAD(sg.ID))
			if sg.EmailEnc != "" {
				v.Email, _ = s.d.Keys.DecryptString(sg.EmailEnc, signerAAD(sg.ID))
			}
		}
		by[sg.RequestID] = append(by[sg.RequestID], v)
	}
	out := make([]Request, len(rows))
	for i, r := range rows {
		out[i] = Request{SignatureRequest: r, Signers: by[r.ID]}
	}
	return out, nil
}

// ListRequests returns the signature requests of the marina (contracts.read).
func (s *Service) ListRequests(ctx context.Context, p *auth.Principal, marinaID string, f store.SignatureRequestFilter, cursor string, limit int) (store.Page[Request], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[Request]{}, err
	}
	page, err := store.ListSignatureRequests(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Request]{}, err
	}
	items, err := s.withSigners(ctx, s.d.DB.DB, scope, p, page.Items)
	return store.Page[Request]{Items: items, NextCursor: page.NextCursor}, err
}

// GetRequest returns a request with its signers (contracts.read; names and emails with customers.read).
func (s *Service) GetRequest(ctx context.Context, p *auth.Principal, marinaID, id string) (*Request, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	return s.getRequest(ctx, s.d.DB.DB, scope, p, id)
}

func (s *Service) getRequest(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Request, error) {
	r, err := store.GetSignatureRequest(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	out, err := s.withSigners(ctx, db, scope, p, []store.SignatureRequest{*r})
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// writePermission is the permission to act on a request of a subject type.
func writePermission(subjectType string) auth.Permission {
	if subjectType == "right_transfer" {
		return auth.PermRightsManage
	}
	return auth.PermContractsWrite
}

// CancelRequest cancels an open request on behalf of staff; the subject is called back (contracts.write).
func (s *Service) CancelRequest(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Request, error) {
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
		if !p.Can(scope.OrgID(), scope.ID(), writePermission(r.SubjectType)) {
			return auth.ErrForbidden
		}
		if ifMatch != 0 && r.Version != ifMatch {
			return store.ErrVersionMismatch
		}
		if err := s.cancelTx(ctx, tx, scope, r, p.Actor(), true); err != nil {
			return err
		}
		out, err = s.getRequest(ctx, tx, scope, p, id)
		return err
	})
	return out, err
}

// otpHMAC is the stored form of a code for a signer.
func (s *Service) otpHMAC(signerID, code string) string {
	m := hmac.New(sha256.New, s.otpKey)
	m.Write([]byte(signerID + "|" + code))
	return hex.EncodeToString(m.Sum(nil))
}

// evidence is the encrypted evidence of one signer.
type evidence struct {
	Method           string    `json:"method"` // otp, paper, provider
	DeclaredIdentity string    `json:"declared_identity,omitempty"`
	EmailHash        string    `json:"email_hash,omitempty"`
	IP               string    `json:"ip,omitempty"`
	UserAgentSHA256  string    `json:"user_agent_sha256,omitempty"`
	OpenedAt         time.Time `json:"opened_at,omitzero"`
	OTPSentAt        time.Time `json:"otp_sent_at,omitzero"`
	SignedAt         time.Time `json:"signed_at,omitzero"`
	DocumentSHA256   string    `json:"document_sha256,omitempty"`
	RequestID        string    `json:"request_id"`
	ConfirmedBy      string    `json:"confirmed_by,omitempty"`
	ScanFileID       string    `json:"scan_file_id,omitempty"`
}

func (s *Service) readEvidence(sg *store.SignatureSigner) evidence {
	var e evidence
	if sg.EvidenceEnc == "" {
		return e
	}
	if raw, err := s.d.Keys.DecryptString(sg.EvidenceEnc, evidenceAAD(sg.ID)); err == nil {
		_ = json.Unmarshal([]byte(raw), &e)
	}
	return e
}

func (s *Service) sealEvidence(signerID string, e evidence) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(b), evidenceAAD(signerID))
}

// marinaDate formats an instant as a marina date for emails.
func (s *Service) marinaDate(ctx context.Context, db bun.IDB, scope store.MarinaScope, at time.Time, locale string) string {
	loc := time.UTC
	if m, err := store.GetMarina(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(m.Timezone); err == nil {
			loc = l
		}
	}
	d := calendar.In(at, loc)
	if locale == "en" {
		return d.String()
	}
	return fmt.Sprintf("%02d/%02d/%04d", d.Day(), int(d.Month()), d.Year())
}

// RequestFilter narrows ListRequests (the store filter; an alias for the HTTP layer).
type RequestFilter = store.SignatureRequestFilter
