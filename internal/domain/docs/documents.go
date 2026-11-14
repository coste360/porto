package docs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // logo decoding
	_ "image/png"  // logo decoding
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// LinkTTL is the validity of a download link (m4-plan §0.13: 5 minutes, bound to the user).
const LinkTTL = 5 * time.Minute

// MaxUploadBytes is the largest uploaded scan or attachment.
const MaxUploadBytes = 15 << 20

// PendingFile is a blob written to the file store whose stored_files row is inserted in a later transaction.
type PendingFile struct {
	ID, StorageKey, SHA256, ContentType string
	Size                                int64
}

// PutFile encrypts and writes content for the marina (outside any transaction).
func (s *Service) PutFile(ctx context.Context, scope store.MarinaScope, content []byte, contentType string) (*PendingFile, error) {
	id := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), id, bytesReader(content), MaxUploadBytes+1)
	if err != nil {
		return nil, err
	}
	return &PendingFile{ID: id, StorageKey: blob.StorageKey, SHA256: blob.SHA256, ContentType: contentType, Size: blob.Size}, nil
}

// InsertFileTx inserts the stored_files row of a pending file (purpose document) in tx.
func (s *Service) InsertFileTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, f *PendingFile, userID string) error {
	now := s.now()
	return store.InsertStoredFile(ctx, tx, &store.StoredFile{
		ID: f.ID, MarinaID: scope.ID(), Purpose: "document", ContentType: f.ContentType, SizeBytes: f.Size,
		SHA256: f.SHA256, StorageKey: f.StorageKey, CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now,
	})
}

// DiscardFile removes the blob of a pending file whose transaction did not commit.
func (s *Service) DiscardFile(f *PendingFile) {
	if f != nil {
		_ = s.d.Files.Remove(f.StorageKey)
	}
}

// OpenStoredFile returns the decrypted content of a stored file of the marina.
func (s *Service) OpenStoredFile(ctx context.Context, db bun.IDB, scope store.MarinaScope, fileID string) ([]byte, *store.StoredFile, error) {
	f, err := store.GetStoredFile(ctx, db, scope, fileID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	b, err := s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
	return b, f, err
}

// DocumentPDF returns the bytes of a ready document (ErrNotReady while pending, rendering or failed). The caller
// has authorised scope.
func (s *Service) DocumentPDF(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) ([]byte, *store.GeneratedDocument, error) {
	row, err := store.GetGeneratedDocument(ctx, db, scope, id)
	if err != nil {
		return nil, nil, notFound(err)
	}
	if row.FileID == "" || (row.Status != store.DocReady && row.Status != store.DocSuperseded) {
		return nil, row, ErrNotReady
	}
	b, _, err := s.OpenStoredFile(ctx, db, scope, row.FileID)
	if err != nil {
		return nil, row, err
	}
	if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != row.SHA256 {
		return nil, row, fmt.Errorf("docs: document %s does not match its hash", row.ID)
	}
	return b, row, nil
}

// GetDocumentRow returns a document row for internal callers (signature, comms) of an authorised scope.
func (s *Service) GetDocumentRow(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*store.GeneratedDocument, error) {
	row, err := store.GetGeneratedDocument(ctx, db, scope, id)
	return row, notFound(err)
}

// LoadAttachment implements notify.AttachmentLoader: a ready generated document or a stored file of the marina.
// A document still pending or rendering is notify.ErrAttachmentPending: the email waits for it (the render wakes it,
// renderRow's ready hooks); a failed document is an ordinary error, retried and eventually given up.
func (s *Service) LoadAttachment(ctx context.Context, marinaID string, ref notify.AttachmentRef) (*notify.Attachment, error) {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, err
	}
	if ref.DocumentID != "" {
		b, row, err := s.DocumentPDF(ctx, s.d.DB.DB, scope, ref.DocumentID)
		if errors.Is(err, ErrNotReady) && row != nil && (row.Status == store.DocPending || row.Status == store.DocRendering) {
			return nil, fmt.Errorf("%w: %w", notify.ErrAttachmentPending, err)
		}
		if err != nil {
			return nil, err
		}
		return &notify.Attachment{Name: fileName(row), ContentType: filestore.TypePDF, Content: b}, nil
	}
	b, f, err := s.OpenStoredFile(ctx, s.d.DB.DB, scope, ref.FileID)
	if err != nil {
		return nil, err
	}
	ext := ".pdf"
	switch f.ContentType {
	case filestore.TypePNG:
		ext = ".png"
	case filestore.TypeJPEG:
		ext = ".jpg"
	}
	return &notify.Attachment{Name: "allegato" + ext, ContentType: f.ContentType, Content: b}, nil
}

func fileName(row *store.GeneratedDocument) string {
	return row.Kind + "-" + row.ID[len(row.ID)-8:] + ".pdf"
}

// Document is a generated document with its template reference.
type Document struct {
	store.GeneratedDocument
	TemplateID      string
	TemplateVersion int
}

func (s *Service) withTemplate(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.GeneratedDocument) []Document {
	out := make([]Document, len(rows))
	cache := map[string]*store.TemplateVersion{}
	for i, r := range rows {
		out[i] = Document{GeneratedDocument: r}
		if r.TemplateVersionID == "" {
			continue
		}
		v, ok := cache[r.TemplateVersionID]
		if !ok {
			v, _ = store.GetTemplateVersionByID(ctx, db, scope, r.TemplateVersionID)
			cache[r.TemplateVersionID] = v
		}
		if v != nil {
			out[i].TemplateID, out[i].TemplateVersion = v.TemplateID, v.Version
		}
	}
	return out
}

// ListDocuments returns the generated documents of the marina (contracts.read).
func (s *Service) ListDocuments(ctx context.Context, p *auth.Principal, marinaID string, f store.GeneratedDocumentFilter, cursor string, limit int) (store.Page[Document], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[Document]{}, err
	}
	page, err := store.ListGeneratedDocuments(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Document]{}, err
	}
	return store.Page[Document]{Items: s.withTemplate(ctx, s.d.DB.DB, scope, page.Items), NextCursor: page.NextCursor}, nil
}

// GetDocument returns a generated document (contracts.read, or rights.manage for rights subjects).
func (s *Service) GetDocument(ctx context.Context, p *auth.Principal, marinaID, id string) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !p.Can(scope.OrgID(), scope.ID(), subjectPermission(row.SubjectType)) {
		return nil, auth.ErrForbidden
	}
	d := s.withTemplate(ctx, s.d.DB.DB, scope, []store.GeneratedDocument{*row})[0]
	return &d, nil
}

// FileLink is a signed download link.
type FileLink struct {
	URL       string
	ExpiresAt time.Time
}

func (s *Service) linkSig(marinaID, id, userID string, exp int64) string {
	m := hmac.New(sha256.New, s.linkKey)
	m.Write([]byte(marinaID + "|" + id + "|" + userID + "|" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

// DocumentLink returns a download link valid 5 minutes for this user (subject permission and customers.read).
func (s *Service) DocumentLink(ctx context.Context, p *auth.Principal, marinaID, id string) (*FileLink, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !p.Can(scope.OrgID(), scope.ID(), subjectPermission(row.SubjectType)) {
		return nil, auth.ErrForbidden
	}
	if row.FileID == "" {
		return nil, ErrNotReady
	}
	exp := s.d.Clock.Now().Add(LinkTTL).UTC().Truncate(time.Second)
	sig := s.linkSig(scope.ID(), row.ID, p.ID(), exp.Unix())
	u := "/api/admin/v1/marinas/" + scope.ID() + "/generated-documents/" + row.ID + "/file?exp=" +
		strconv.FormatInt(exp.Unix(), 10) + "&sig=" + sig
	return &FileLink{URL: u, ExpiresAt: exp}, nil
}

// ErrLinkExpired means a download link is missing, tampered, of another user or expired (the M1 sentinel, 403
// download_link_expired).
var ErrLinkExpired = document.ErrLinkExpired

// OpenDocument checks a signed link and returns the PDF (customers.read and the subject permission).
func (s *Service) OpenDocument(ctx context.Context, p *auth.Principal, marinaID, id string, exp int64, sig string) ([]byte, *store.GeneratedDocument, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, nil, err
	}
	row, err := store.GetGeneratedDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, nil, notFound(err)
	}
	if !p.Can(scope.OrgID(), scope.ID(), subjectPermission(row.SubjectType)) {
		return nil, nil, auth.ErrForbidden
	}
	want := s.linkSig(scope.ID(), row.ID, p.ID(), exp)
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, nil, ErrLinkExpired
	}
	b, row, err := s.DocumentPDF(ctx, s.d.DB.DB, scope, id)
	return b, row, err
}

// InsertSignedTx stores the signed PDF of a source document as a new immutable row (signed = true,
// signed_from_id = source) in tx; f was written with PutFile. It returns the new document id.
func (s *Service) InsertSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, sourceID string, f *PendingFile, pages int, userID string) (string, error) {
	src, err := store.GetGeneratedDocument(ctx, tx, scope, sourceID)
	if err != nil {
		return "", notFound(err)
	}
	if err := s.InsertFileTx(ctx, tx, scope, f, userID); err != nil {
		return "", err
	}
	now := s.now()
	row := &store.GeneratedDocument{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: src.Kind, TemplateVersionID: src.TemplateVersionID,
		BuiltinRef: src.BuiltinRef, Locale: src.Locale, SubjectType: src.SubjectType, SubjectID: src.SubjectID,
		Status: store.DocReady, FileID: f.ID, SHA256: f.SHA256, Pages: pages, PDFA: src.PDFA, Signed: true,
		SignedFromID: src.ID, RenderedAt: now, CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertGeneratedDocument(ctx, tx, row); err != nil {
		return "", err
	}
	if err := s.write(ctx, tx, scope, actorOf(userID), "document.compose", "generated_document", row.ID,
		map[string]any{"signed_from_id": src.ID, "sha256": f.SHA256, "pages": pages},
		event.DocumentGenerated, event.DocumentGeneratedData{
			DocumentID: row.ID, Kind: row.Kind, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
			Status: store.DocReady, Signed: true,
		}); err != nil {
		return "", err
	}
	return row.ID, nil
}

type theme struct {
	LogoFileID string `json:"logo_file_id"`
}

// logo loads the marina logo (theme.logo_file_id, PNG or JPEG); nil when absent or unreadable.
func (s *Service) logo(ctx context.Context, db bun.IDB, scope store.MarinaScope) *Logo {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil || len(m.Theme) == 0 {
		return nil
	}
	var t theme
	if json.Unmarshal(m.Theme, &t) != nil || t.LogoFileID == "" {
		return nil
	}
	b, f, err := s.OpenStoredFile(ctx, db, scope, t.LogoFileID)
	if err != nil {
		return nil
	}
	typ := map[string]string{filestore.TypePNG: "PNG", filestore.TypeJPEG: "JPG"}[f.ContentType]
	if typ == "" {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytesReader(b))
	if err != nil {
		return nil
	}
	return &Logo{Data: b, Type: typ, W: int64(cfg.Width), H: int64(cfg.Height)}
}

// DocumentFilter narrows ListDocuments (the store filter; an alias for the HTTP layer).
type DocumentFilter = store.GeneratedDocumentFilter
