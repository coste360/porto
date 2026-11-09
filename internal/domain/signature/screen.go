package signature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png" // signature image decoding
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// On-screen signature (M11, m11-plan §7 step 2, §10, D14): the signer draws on the operator's phone or tablet; the
// PNG (at most 200 KiB, image/png sniffed) is drawn on an evidence page (signer, operator, device, instant, SHA-256 of
// the signed source) appended to the composed document; the signed copy is inserted with docs.InsertSignedTx and
// the request is created already signed, in the caller's transaction. It is a simple electronic signature ("firma
// elettronica semplice", D6 wording unchanged). The PDF writer appends pages only (it never edits a page of the
// source), so the drawn signature lives on the appended evidence page, next to the evidence it belongs to.

// DriverScreen is the on-screen signature driver (signature_requests.driver).
const DriverScreen = store.SignatureDriverScreen

// MaxScreenSignatureBytes bounds the drawn signature PNG.
const MaxScreenSignatureBytes = 200 << 10

// ErrScreenNotImplemented is kept for the contract skeleton's callers; the screen driver is implemented.
var ErrScreenNotImplemented = errors.New("signature: screen driver not implemented")

// ErrScreenImageInvalid is a signature image that is not a PNG or is larger than MaxScreenSignatureBytes (422
// validation_failed on signature_png).
var ErrScreenImageInvalid = errors.New("signature: screen image invalid")

// ScreenSignInput is an on-screen signature of a subject's composed document.
type ScreenSignInput struct {
	SubjectType string // rental
	SubjectID   string
	DocumentID  string // the composed (unsigned) document
	SignerName  string // personal data: inside the encrypted evidence only
	PNG         []byte
	DeviceLabel string
	At          time.Time
}

// ScreenSignResult is the signed request and document.
type ScreenSignResult struct {
	RequestID        string
	SignedDocumentID string
}

// screenImage checks the drawn signature and returns its pixel size.
func screenImage(b []byte) (int, int, error) {
	if len(b) == 0 || len(b) > MaxScreenSignatureBytes {
		return 0, 0, ErrScreenImageInvalid
	}
	ct, w, h, err := filestore.Sniff(b, filestore.TypePNG)
	if err != nil || ct != filestore.TypePNG || w <= 0 || h <= 0 {
		return 0, 0, ErrScreenImageInvalid
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil {
		return 0, 0, ErrScreenImageInvalid
	}
	return w, h, nil
}

// screenBody is the evidence page of an on-screen signature (built on the signature_evidence view).
func screenBody(locale string) string {
	if locale == "en" {
		return `# Signature evidence

Document: **{{document_title}}**

SHA-256 of the signed document: {{document_sha256}}

Signature request: {{request_id}}

Simple electronic signature drawn on screen on the marina's device, in the presence of the operator. The legal value of this form of signature is for the marina and its advisers to assess.

{{logo}}

{{#each signers}}
## {{this.role}}: {{this.name}}

- {{this.declared_identity}}
- Signed: **{{this.signed_at | date}}**
{{/each}}
`
	}
	return `# Pagina delle evidenze di firma

Documento: **{{document_title}}**

Impronta SHA-256 del documento firmato: {{document_sha256}}

Richiesta di firma: {{request_id}}

Firma elettronica semplice tracciata sullo schermo del dispositivo del porto, alla presenza dell'operatore. Il valore legale di questa forma di firma va valutato dal porto e dai suoi consulenti.

{{logo}}

{{#each signers}}
## {{this.role}}: {{this.name}}

- {{this.declared_identity}}
- Firma: **{{this.signed_at | date}}**
{{/each}}
`
}

func screenLabels(locale string) (signer, operator, device string) {
	if locale == "en" {
		return "Signer", "Operator", "Device"
	}
	return "Firmatario", "Operatore", "Dispositivo"
}

// SignOnScreenTx signs a document on screen in tx (request created signed, subject OnSignedTx called). The document
// must be rendered (docs.ErrNotReady otherwise); an open request of the subject (an OTP or paper one) is cancelled.
// The signed PDF blob is written before the inserts; a rollback of the caller leaves it unreferenced.
func (s *Service) SignOnScreenTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in ScreenSignInput) (*ScreenSignResult, error) {
	w, h, err := screenImage(in.PNG)
	if err != nil {
		return nil, err
	}
	if in.At.IsZero() {
		in.At = s.now()
	}
	at := store.UTC(in.At).Truncate(time.Second)
	in.SignerName = strings.TrimSpace(in.SignerName)
	source, doc, err := s.d.Docs.DocumentPDF(ctx, tx, scope, in.DocumentID)
	if err != nil {
		return nil, err
	}
	if doc.SubjectID != in.SubjectID {
		return nil, notFound(store.ErrNotFound)
	}
	if open, err := store.OpenSignatureRequestOf(ctx, tx, scope, in.SubjectType, in.SubjectID); err == nil {
		if err := s.cancelTx(ctx, tx, scope, open, actor, false); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	locale := doc.Locale
	if locale != "en" {
		locale = "it"
	}
	operator := ""
	if actor.Type == audit.ActorUser && actor.ID != "" {
		if u, err := store.MarinaMemberUser(ctx, tx, scope, actor.ID); err == nil {
			operator = u.Name
		}
	}
	reqID := store.NewID()
	mv, err := docs.MarinaHeader(ctx, tx, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	lSigner, lOperator, lDevice := screenLabels(locale)
	device := strings.TrimSpace(in.DeviceLabel)
	if device == "" {
		device = "-"
	}
	view := views.EvidenceView{
		Marina: mv, DocumentTitle: docs.Title(docs.Kind(doc.Kind), locale), DocumentSHA256: doc.SHA256, RequestID: reqID,
		Signers: []views.EvidenceSigner{
			{Role: lSigner, Name: in.SignerName, DeclaredIdentity: lDevice + ": " + device, SignedAt: at},
			{Role: lOperator, Name: operator, DeclaredIdentity: lDevice + ": " + device, SignedAt: at},
		},
	}
	src, _, ok := docs.Builtin(docs.KindSignatureEvidence, locale)
	if !ok {
		return nil, errors.New("signature: no evidence template")
	}
	src.Body = screenBody(locale)
	page, err := docs.Render(docs.RenderInput{
		Kind: docs.KindSignatureEvidence, Source: src, View: view, Locale: locale, Today: at.Format("2006-01-02"), At: at,
		Title: docs.Title(docs.KindSignatureEvidence, locale), Logo: &docs.Logo{Data: in.PNG, Type: "PNG", W: int64(w), H: int64(h)},
	})
	if err != nil {
		return nil, fmt.Errorf("signature: evidence page: %w", err)
	}
	signed, err := pdf.AppendPages(source, page.PDF)
	if err != nil {
		return nil, err
	}
	pages, err := pdf.PageCount(signed)
	if err != nil {
		pages = doc.Pages + page.Pages
	}
	pf, err := s.d.Docs.PutFile(ctx, scope, signed, filestore.TypePDF)
	if err != nil {
		return nil, err
	}
	ok = false
	defer func() {
		if !ok {
			s.d.Docs.DiscardFile(pf)
		}
	}()
	userID := ""
	if actor.Type == audit.ActorUser {
		userID = actor.ID
	}
	signedID, err := s.d.Docs.InsertSignedTx(ctx, tx, scope, doc.ID, pf, pages, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	r := &store.SignatureRequest{
		ID: reqID, MarinaID: scope.ID(), SubjectType: in.SubjectType, SubjectID: in.SubjectID, DocumentID: doc.ID,
		Driver: DriverScreen, OrderMode: "parallel", Status: StatusSigned, ExpiresAt: now, SignedDocumentID: signedID,
		CompletedAt: now, CreatedByUserID: userID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertSignatureRequest(ctx, tx, r); err != nil {
		return nil, err
	}
	signerID := store.NewID()
	nameEnc, err := s.d.Keys.EncryptString(in.SignerName, signerAAD(signerID))
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealEvidence(signerID, evidence{
		Method: DriverScreen, DeclaredIdentity: device, SignedAt: at, DocumentSHA256: doc.SHA256, RequestID: r.ID,
		ConfirmedBy: userID,
	})
	if err != nil {
		return nil, err
	}
	if err := store.InsertSignatureSigners(ctx, tx, []store.SignatureSigner{{
		ID: signerID, MarinaID: scope.ID(), RequestID: r.ID, Seq: 1, Role: "customer", DisplayNameEnc: nameEnc,
		Locale: locale, Status: SignerSigned, SignedAt: at, EvidenceEnc: sealed, Version: 1, CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		return nil, err
	}
	if sub := s.subject(in.SubjectType); sub != nil {
		if err := sub.OnSignedTx(ctx, tx, scope, in.SubjectID, signedID, at); err != nil {
			return nil, err
		}
	}
	if err := s.write(ctx, tx, scope, actor, "signature.sign", r,
		map[string]any{"status": StatusSigned, "driver": DriverScreen, "signed_document_id": signedID, "sha256": pf.SHA256},
		event.SignatureCompleted); err != nil {
		return nil, err
	}
	ok = true
	return &ScreenSignResult{RequestID: r.ID, SignedDocumentID: signedID}, nil
}
