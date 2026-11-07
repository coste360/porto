package contract

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/store"
)

// Signature of contracts (m4-plan §3.1, §6.5): request_signature composes the contract document (PDF/A) and
// opens a signature request; the signature service calls back OnSignedTx (sign: awaiting_signature → active)
// or OnFailedTx (signature_failed: → draft) in the transaction that completes or fails the request.

// HolderIssuer issues the due holder instalments (rights.Service.IssueDueTx); the installments.issue job of this
// package covers both tables (m4-plan §13.3).
type HolderIssuer interface {
	IssueDueTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, today calendar.Date, limit int) (int, error)
}

// SignatureSubjects returns the signature subjects of this package: contract and addendum.
func (s *Service) SignatureSubjects() []signature.Subject {
	return []signature.Subject{contractSubject{s}, addendumSubject{s}}
}

// Signature subject kinds of this package.
const (
	SubjectContract = "contract"
	SubjectAddendum = "addendum"
)

// SignerInput is one signer of a request: the customer (default), another customer or a marina user.
type SignerInput struct {
	Role       string
	CustomerID string
	UserID     string
}

// SignatureInput asks for a signature (requestContractSignature; quote and renewal acceptance).
type SignatureInput struct {
	Driver     string // "" = the marina's driver
	Signers    []SignerInput
	Sequential bool
}

// signatureSpec is what requestSignatureTx needs.
type signatureSpec struct {
	subjectType string
	subjectID   string
	docKind     docs.Kind
	docSubject  docs.SubjectType
	customerID  string
	in          SignatureInput
}

// signers returns the signers of a request: the given ones, else the customer, plus the marina when the
// settings ask for a countersignature.
func signers(cs *store.ContractSettings, customerID string, in []SignerInput) []signature.SignerSpec {
	var out []signature.SignerSpec
	for _, s := range in {
		out = append(out, signature.SignerSpec{Role: s.Role, CustomerID: s.CustomerID, UserID: s.UserID})
	}
	if len(out) == 0 {
		out = append(out, signature.SignerSpec{Role: "customer", CustomerID: customerID})
	}
	if cs.MarinaCountersign {
		hasMarina := false
		for _, s := range out {
			hasMarina = hasMarina || s.Role == "marina"
		}
		if !hasMarina {
			out = append(out, signature.SignerSpec{Role: "marina"})
		}
	}
	return out
}

func validateSignatureInput(in SignatureInput) error {
	v := &auth.ValidationError{}
	if in.Driver != "" && in.Driver != DriverPaper && in.Driver != DriverOTP && in.Driver != DriverProvider {
		v.Add("driver", "invalid")
	}
	if len(in.Signers) > 6 {
		v.Add("signers", "too_many")
	}
	for _, s := range in.Signers {
		if s.Role == "" {
			v.Add("signers.role", "required")
		}
		if (s.CustomerID == "") == (s.UserID == "") && s.Role != "marina" {
			v.Add("signers.customer_id", "invalid")
		}
	}
	return v.Err()
}

// composeTx composes a document of a subject (nil composer = no document) and renders it after commit.
func (s *Service) composeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, kind docs.Kind, st docs.SubjectType, id string, pdfa bool) (string, error) {
	if s.composer == nil {
		return "", nil
	}
	docID, err := s.composer.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
		Kind: kind, SubjectType: st, SubjectID: id, Locale: subjectLocale(ctx, tx, scope, st, id), PDFA: pdfa,
		CreatedBy: a.userID(), At: s.now(),
	})
	if err != nil {
		return "", err
	}
	comp := s.composer
	bg := context.WithoutCancel(ctx)
	store.AfterCommit(ctx, func() {
		if err := comp.RenderPending(bg, docID); err != nil {
			s.d.Log.Warn("document render deferred to the job", "document_id", docID, "err", err)
		}
	})
	return docID, nil
}

// subjectLocale is the locale of the customer a document is addressed to (empty = the marina default, chosen by
// docs). Lookup failures fall back to the default: the locale never blocks a document.
func subjectLocale(ctx context.Context, tx bun.IDB, scope store.MarinaScope, st docs.SubjectType, id string) string {
	customerID := ""
	contractOf := func(cid string) {
		if c, err := store.GetContract(ctx, tx, scope, cid); err == nil {
			customerID = c.CustomerID
		}
	}
	switch st {
	case docs.SubjectQuote:
		if q, err := store.GetQuote(ctx, tx, scope, id); err == nil {
			customerID = q.CustomerID
		}
	case docs.SubjectContract:
		contractOf(id)
	case docs.SubjectAddendum:
		if ad, err := store.GetAddendumByID(ctx, tx, scope, id); err == nil {
			contractOf(ad.ContractID)
		}
	case docs.SubjectRenewalProposal:
		if pr, err := store.GetRenewalProposal(ctx, tx, scope, id); err == nil {
			contractOf(pr.SourceContractID)
		}
	}
	if customerID == "" {
		return ""
	}
	cu, err := store.GetCustomer(ctx, tx, scope, customerID)
	if err != nil {
		return ""
	}
	return cu.Locale
}

// requestSignatureTx composes the document and opens the signature request of a subject; the holds of a
// contract subject are extended to the request expiry by the caller. It returns the request and document ids.
func (s *Service) requestSignatureTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, cs *store.ContractSettings, sp signatureSpec) (string, string, time.Time, error) {
	if s.signer == nil {
		return "", "", time.Time{}, errors.New("contract: signature service not wired")
	}
	driver := sp.in.Driver
	if driver == "" {
		driver = cs.SignatureDriver
	}
	docID, err := s.composeTx(ctx, tx, scope, a, sp.docKind, sp.docSubject, sp.subjectID, true)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if err := s.commitDocHook(); err != nil {
		return "", "", time.Time{}, err
	}
	expires := s.now().Add(time.Duration(cs.SignatureDays) * 24 * time.Hour)
	reqID, err := s.signer.OpenTx(ctx, tx, scope, signature.OpenRequest{
		SubjectType: sp.subjectType, SubjectID: sp.subjectID, DocumentID: docID, Driver: driver,
		ProviderCode: cs.ProviderCode, Signers: signers(cs, sp.customerID, sp.in.Signers), Sequential: sp.in.Sequential,
		ExpiresAt: expires, CreatedBy: a.userID(),
	})
	if err != nil {
		return "", "", time.Time{}, err
	}
	return reqID, docID, expires, nil
}

func (s *Service) commitDocHook() error {
	if s.afterDocument != nil {
		return s.afterDocument()
	}
	return nil
}

// extendHoldsTx sets the expiry of the active holds of a contract (signature expiry, or back to the draft hold).
func (s *Service) extendHoldsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, until time.Time) ([]string, error) {
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return nil, err
	}
	var ids []string
	now := s.now()
	for _, al := range activeOnly(allocs) {
		if al.Kind == store.AllocKindHold && al.ExpiresAt.After(now) {
			ids = append(ids, al.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, s.alloc.ConvertTx(ctx, tx, scope, now, ids, occupancy.KindHold, &until)
}

// RequestContractSignature moves a draft contract to awaiting_signature: document composed, signature request
// opened, holds extended to the request expiry (contracts.write, If-Match).
func (s *Service) RequestContractSignature(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in SignatureInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	if err := validateSignatureInput(in); err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		_, err = s.requestContractSignatureTx(ctx, tx, scope, a, c, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// requestContractSignatureTx is the request_signature transition of a locked contract.
func (s *Service) requestContractSignatureTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, in SignatureInput) (string, error) {
	t, err := Next(MooringTransitions, Status(c.Status), ActionRequestSignature)
	if err != nil {
		return "", err
	}
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	if c.HoldLost {
		return "", &TransitionError{From: c.Status, Action: ActionRequestSignature}
	}
	reqID, docID, expires, err := s.requestSignatureTx(ctx, tx, scope, a, cs, signatureSpec{
		subjectType: SubjectContract, subjectID: c.ID, docKind: docs.ContractKindOf(c.Kind, c.Term),
		docSubject: docs.SubjectContract, customerID: c.CustomerID, in: in,
	})
	if err != nil {
		return "", err
	}
	extended, err := s.extendHoldsTx(ctx, tx, scope, c, expires)
	if err != nil {
		return "", err
	}
	from := c.Status
	c.Status, c.SignatureRequestID = string(t.To), reqID
	if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
		return "", err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "contract.request_signature", entityContract, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "signature_request_id": reqID, "document_id": docID,
		"expires_at": expires, "extended": extended,
	}); err != nil {
		return "", err
	}
	return reqID, s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeSignatureRequested})
}

// activateTx is the common part of sign and paper activation: holds become contract allocations (lost ones are
// allocated again or the call fails with 409), the first instalment is issued, the renewal proposal of a
// successor advances, the customer is told.
func (s *Service) activateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, t Transition,
	auditAction string, diff map[string]any, sub subject,
) error {
	now := s.now()
	allocs, err := contractAllocs(ctx, tx, scope, c.ID)
	if err != nil {
		return err
	}
	reallocated, holds, err := s.convertOrReallocate(ctx, tx, scope, a, now, c, allocs, occupancy.KindContract)
	if err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	c.ActivatedAt, c.HoldLost = now, false
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	first, err := s.issueFirstTx(ctx, tx, scope, a, c)
	if err != nil {
		return err
	}
	diff["from_status"], diff["status"] = from, c.Status
	diff["converted"], diff["reallocated"] = holds, idsOf(reallocated)
	if first != nil {
		diff["issued_installment_id"] = first.ID
	}
	if err := s.writeAudit(ctx, tx, scope, a, auditAction, entityContract, c.ID, diff); err != nil {
		return err
	}
	sub.contract = c
	if err := s.emitTransition(ctx, tx, scope, t, sub); err != nil {
		return err
	}
	if err := s.emitAllocated(ctx, tx, scope, reallocated, c.ID); err != nil {
		return err
	}
	if err := s.onSuccessorActivatedTx(ctx, tx, scope, a, c, first); err != nil {
		return err
	}
	if _, err := s.queueMailTx(ctx, tx, scope, Mail{
		Template: MailContractActivated, CustomerID: c.CustomerID, DedupeKey: "contract:" + c.ID + ":activated",
		Values: map[string]string{"contract_code": Code(c), "starts_on": c.StartsOn.String(), "ends_on": c.EndsOn.String()},
	}); err != nil && !errors.Is(err, ErrCustomerEmailMissing) {
		return err
	}
	return nil
}

// contractSubject is the signature.Subject of contracts.
type contractSubject struct{ s *Service }

func (contractSubject) Kind() string { return SubjectContract }

// OnSignedTx activates a contract awaiting signature (sign). A contract no longer awaiting its signature
// (cancelled, activated on paper meanwhile) refuses: the completion rolls back (INV-34).
func (cs contractSubject) OnSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, signedDocumentID string, at time.Time) error {
	s := cs.s
	if err := s.requireAllocator(); err != nil {
		return err
	}
	c, err := lockContract(ctx, tx, scope, subjectID, "", 0)
	if err != nil {
		return err
	}
	t, err := Next(MooringTransitions, Status(c.Status), ActionSign)
	if err != nil {
		return err
	}
	driver := DriverOTP
	if st, err := s.settingsTx(ctx, tx, scope); err == nil && st.SignatureDriver != DriverPaper {
		driver = st.SignatureDriver
	}
	c.SignedAt, c.SignedDocumentID, c.SignedOnPaper = store.UTC(at), signedDocumentID, false
	return s.activateTx(ctx, tx, scope, system, c, t, "contract.sign", map[string]any{
		"signature_request_id": c.SignatureRequestID, "signed_document_id": signedDocumentID,
	}, subject{driver: driver})
}

// OnFailedTx brings a contract awaiting signature back to draft (signature_failed) with its holds back to the
// draft hold; a contract in another state is left alone (the request was cancelled because of it).
func (cs contractSubject) OnFailedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, status string) error {
	s := cs.s
	c, err := lockContract(ctx, tx, scope, subjectID, "", 0)
	if err != nil {
		return err
	}
	t, err := Next(MooringTransitions, Status(c.Status), ActionSignatureFailed)
	if err != nil {
		return nil //nolint:nilerr // not awaiting a signature any more: nothing to undo
	}
	st, err := store.ContractOccupancySettings(ctx, tx, scope, s.now())
	if err != nil {
		return err
	}
	until := s.now().Add(time.Duration(st.DraftHoldDays) * 24 * time.Hour)
	if p, err := store.OpenProposalOfSuccessor(ctx, tx, scope, c.ID); err == nil && p != nil {
		if d := endOfDay(p.DeadlineOn, location(ctx, tx, scope)); d.After(s.now()) {
			until = d
		}
	}
	extended, err := s.extendHoldsTx(ctx, tx, scope, c, until)
	if err != nil {
		return err
	}
	from := c.Status
	c.Status = string(t.To)
	if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
		return err
	}
	if err := s.writeAudit(ctx, tx, scope, system, "contract.signature_failed", entityContract, c.ID, map[string]any{
		"from_status": from, "status": c.Status, "request_status": status, "signature_request_id": c.SignatureRequestID,
		"extended": extended,
	}); err != nil {
		return err
	}
	return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeSignatureFailed})
}

// SuspendContract suspends an active contract (contracts.write, If-Match; manual in M4, §15 D9): allocations
// stay, the dock sees the status.
func (s *Service) SuspendContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Detail, error) {
	reason, ok := cleanText(reason, 500)
	return s.simpleTransition(ctx, p, marinaID, id, ifMatch, ActionSuspend, "contract.suspend", event.ChangeSuspended, func(c *store.Contract, now time.Time) error {
		v := &auth.ValidationError{}
		if reason == "" {
			v.Add("reason", "required")
		} else if !ok {
			v.Add("reason", "too_long")
		}
		if err := v.Err(); err != nil {
			return err
		}
		c.SuspendedAt, c.SuspendReason = now, reason
		return nil
	})
}

// ResumeContract brings a suspended contract back to active (contracts.write, If-Match).
func (s *Service) ResumeContract(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Detail, error) {
	return s.simpleTransition(ctx, p, marinaID, id, ifMatch, ActionResume, "contract.resume", event.ChangeResumed, func(c *store.Contract, _ time.Time) error {
		c.SuspendedAt, c.SuspendReason = time.Time{}, ""
		return nil
	})
}

func (s *Service) simpleTransition(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, action Action,
	auditAction, change string, apply func(*store.Contract, time.Time) error,
) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, id, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), action)
		if err != nil {
			return err
		}
		from := c.Status
		now := s.now()
		if err := apply(c, now); err != nil {
			return err
		}
		c.Status = string(t.To)
		if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, auditAction, entityContract, c.ID, map[string]any{"from_status": from, "status": c.Status}); err != nil {
			return err
		}
		return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: change})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}
