package signature

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// signerSignedTx records the signature of a signer (conditional on its status) with its evidence, moves the request
// to partially_signed and, for sequential requests, invites the next signer. It reports whether every signer has
// signed (the caller then completes the request after commit).
func (s *Service) signerSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, sg *store.SignatureSigner, ev evidence, a audit.Actor, from []string) (bool, error) {
	now := s.now()
	ev.SignedAt, ev.RequestID = now, r.ID
	sealed, err := s.sealEvidence(sg.ID, ev)
	if err != nil {
		return false, err
	}
	set := map[string]any{"signed_at": now, "evidence_enc": sealed}
	if ev.ScanFileID != "" {
		set["paper_scan_file_id"] = ev.ScanFileID
	}
	ok, err := store.TransitionSigner(ctx, tx, scope, sg.ID, from, SignerSigned, set, now)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrNotSigner
	}
	sg.Status = SignerSigned
	if _, err := store.TransitionSignatureRequest(ctx, tx, scope, r.ID, []string{StatusSent}, StatusPartiallySigned, nil, now); err != nil {
		return false, err
	}
	if r.Status == StatusSent {
		r.Status = StatusPartiallySigned
	}
	diff := map[string]any{"signer_seq": sg.Seq, "role": sg.Role, "method": ev.Method}
	if ev.Method == "paper" {
		if err := s.write(ctx, tx, scope, a, "signature.paper_confirm", r, diff, ""); err != nil {
			return false, err
		}
	} else if err := s.write(ctx, tx, scope, a, "signature.sign", r, diff, ""); err != nil {
		return false, err
	}
	signers, err := store.SignersOf(ctx, tx, scope, r.ID)
	if err != nil {
		return false, err
	}
	all := true
	for _, x := range signers {
		if x.Status != SignerSigned {
			all = false
		}
	}
	if !all && r.Driver == DriverOTP {
		if err := s.inviteNextTx(ctx, tx, scope, r); err != nil {
			return false, err
		}
	}
	return all, nil
}

var errCompleted = errors.New("signature: already completed")

// complete builds the signed PDF of a request whose signers have all signed (the evidence page for OTP signatures,
// a cover page for paper, the provider's PDF), stores it as a new immutable document and calls the subject, all in
// one transaction. Concurrent completions store one signed document (conditional transition). Idempotent.
func (s *Service) complete(ctx context.Context, scope store.MarinaScope, requestID string, supplied *ext.SignedDocument) error {
	r, err := store.GetSignatureRequest(ctx, s.d.DB.DB, scope, requestID)
	if err != nil {
		return notFound(err)
	}
	if r.Status != StatusSent && r.Status != StatusPartiallySigned {
		return nil
	}
	signers, err := store.SignersOf(ctx, s.d.DB.DB, scope, r.ID)
	if err != nil {
		return err
	}
	for _, sg := range signers {
		if sg.Status != SignerSigned && supplied == nil {
			return nil
		}
	}
	source, doc, err := s.d.Docs.DocumentPDF(ctx, s.d.DB.DB, scope, r.DocumentID)
	if err != nil {
		return err
	}
	now := s.now()
	var signed []byte
	switch {
	case supplied != nil:
		if !bytes.HasPrefix(supplied.PDF, []byte("%PDF-")) {
			return errors.New("signature: provider returned no PDF")
		}
		signed = supplied.PDF
	default:
		extra, err := s.closingPage(ctx, scope, r, doc, signers, now)
		if err != nil {
			return err
		}
		if signed, err = pdf.AppendPages(source, extra); err != nil {
			return err
		}
	}
	pages, err := pdf.PageCount(signed)
	if err != nil {
		pages = doc.Pages
	}
	pf, err := s.d.Docs.PutFile(ctx, scope, signed, filestore.TypePDF)
	if err != nil {
		return err
	}
	stored := false
	defer func() {
		if !stored {
			s.d.Docs.DiscardFile(pf)
		}
	}()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		signedID, err := s.d.Docs.InsertSignedTx(ctx, tx, scope, r.DocumentID, pf, pages, "")
		if err != nil {
			return err
		}
		ok, err := store.TransitionSignatureRequest(ctx, tx, scope, r.ID, []string{StatusSent, StatusPartiallySigned}, StatusSigned,
			map[string]any{"signed_document_id": signedID, "completed_at": now}, now)
		if err != nil {
			return err
		}
		if !ok {
			return errCompleted
		}
		r.Status, r.SignedDocumentID = StatusSigned, signedID
		if sub := s.subject(r.SubjectType); sub != nil {
			if err := sub.OnSignedTx(ctx, tx, scope, r.SubjectID, signedID, now); err != nil {
				return err
			}
		}
		if err := s.revokeLinksTx(ctx, tx, scope, r); err != nil {
			return err
		}
		return s.write(ctx, tx, scope, audit.System, "signature.sign", r,
			map[string]any{"status": StatusSigned, "signed_document_id": signedID, "sha256": pf.SHA256}, event.SignatureCompleted)
	})
	if errors.Is(err, errCompleted) {
		return nil
	}
	if err == nil {
		stored = true
	}
	return err
}

// closingPage renders the page appended to the signed PDF: the OTP evidence page when any signer signed with a
// code, else the paper cover page.
func (s *Service) closingPage(ctx context.Context, scope store.MarinaScope, r *store.SignatureRequest, doc *store.GeneratedDocument, signers []store.SignatureSigner, at time.Time) ([]byte, error) {
	locale := doc.Locale
	title := docs.Title(docs.Kind(doc.Kind), locale)
	otp := false
	var list []views.EvidenceSigner
	for i := range signers {
		sg := &signers[i]
		ev := s.readEvidence(sg)
		name, _ := s.d.Keys.DecryptString(sg.DisplayNameEnc, signerAAD(sg.ID))
		if ev.Method == "otp" {
			otp = true
		}
		declared := ev.DeclaredIdentity
		if ev.Method == "paper" {
			declared = paperLabel(locale)
		}
		list = append(list, views.EvidenceSigner{
			Role: sg.Role, Name: name, DeclaredIdentity: declared, EmailHash: ev.EmailHash, IP: ev.IP,
			UserAgentSHA256: ev.UserAgentSHA256, OpenedAt: ev.OpenedAt, OTPSentAt: ev.OTPSentAt, SignedAt: ev.SignedAt,
		})
	}
	if otp {
		mv, err := docs.MarinaHeader(ctx, s.d.DB.DB, scope, s.d.Keys)
		if err != nil {
			return nil, err
		}
		view := views.EvidenceView{Marina: mv, DocumentTitle: title, DocumentSHA256: doc.SHA256, RequestID: r.ID, Signers: list}
		out, err := docs.RenderBuiltin(docs.KindSignatureEvidence, view, locale, at, at.Format("2006-01-02"))
		if err != nil {
			return nil, err
		}
		return out.PDF, nil
	}
	lines := []string{paperCover(locale), title, "SHA-256: " + doc.SHA256}
	for _, x := range list {
		lines = append(lines, x.Role+": "+x.Name)
	}
	return docs.RenderNote(paperLabel(locale), lines, locale, at)
}

func paperLabel(locale string) string {
	if locale == "en" {
		return "Signed on paper"
	}
	return "Firmato su carta"
}

func paperCover(locale string) string {
	if locale == "en" {
		return "Signed on paper, the scan is archived."
	}
	return "Firmato su carta, scansione archiviata."
}

// failTx closes an open request as declined or expired and calls the subject.
func (s *Service) failTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.SignatureRequest, status string, a audit.Actor) error {
	ok, err := store.TransitionSignatureRequest(ctx, tx, scope, r.ID, store.SigOpenStatuses, status,
		map[string]any{"completed_at": s.now()}, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotSigner
	}
	r.Status = status
	if err := s.revokeLinksTx(ctx, tx, scope, r); err != nil {
		return err
	}
	if sub := s.subject(r.SubjectType); sub != nil {
		if err := sub.OnFailedTx(ctx, tx, scope, r.SubjectID, status); err != nil {
			return err
		}
	}
	action := "signature.decline"
	if status == StatusExpired {
		action = "signature.expire"
	}
	return s.write(ctx, tx, scope, a, action, r, map[string]any{"status": status}, event.SignatureFailed)
}
