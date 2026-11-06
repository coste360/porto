package document

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Owner types and document types (m1-plan §3.1).
const (
	OwnerCustomer = "customer"
	OwnerVessel   = "vessel"
)

var types = []string{
	"identity_card", "passport", "driving_licence", "boating_licence", "navigation_licence",
	"registration_certificate", "insurance_policy", "safety_certificate", "power_declaration", "signed_contract",
	"proxy", "photo", "other",
}

// File limits and the signed link lifetime (m1-plan §4.4, D19).
const (
	MaxFileBytes = filestore.MaxFileBytes
	LinkTTL      = 5 * time.Minute
	maxFileName  = 120
	purpose      = "document"
	urlKeyName   = "documents.file_url"
)

var fileTypes = []string{filestore.TypePDF, filestore.TypePNG, filestore.TypeJPEG}

// Document is the API view of a document: the number decrypted, the file described.
type Document struct {
	Row         store.Document
	Number      string
	HasFile     bool
	ContentType string
	SizeBytes   int64
}

// Input describes a new document.
type Input struct {
	OwnerType, CustomerID, VesselID string
	Type                            string
	Number, Issuer, Notes           string
	IssuedOn, ExpiresOn             calendar.Date
}

// Patch lists the fields to change (nil = unchanged); a pointer to the zero value clears a nullable field.
type Patch struct {
	Type      *string
	Number    *string
	Issuer    *string
	Notes     *string
	IssuedOn  *calendar.Date
	ExpiresOn *calendar.Date
}

// File is a downloaded document file.
type File struct {
	Content     []byte
	ContentType string
	FileName    string
}

// FileURL is a signed download link.
type FileURL struct {
	URL       string
	ExpiresAt time.Time
}

func numberAAD(id string) string { return "document-number:" + id }

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func clean(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= maxRunes
}

func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Document) ([]Document, error) {
	var fids []string
	for i := range rows {
		fids = append(fids, rows[i].FileID)
	}
	files, err := store.StoredFilesByIDs(ctx, db, scope, fids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.StoredFile{}
	for i := range files {
		byID[files[i].ID] = &files[i]
	}
	out := make([]Document, 0, len(rows))
	for i := range rows {
		r := rows[i]
		d := Document{Row: r}
		if r.NumberEnc != "" {
			n, err := s.d.Keys.DecryptString(r.NumberEnc, numberAAD(r.ID))
			if err != nil {
				return nil, err
			}
			d.Number = n
		}
		if f, ok := byID[r.FileID]; ok {
			d.HasFile, d.ContentType, d.SizeBytes = true, f.ContentType, f.SizeBytes
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.Document) (*Document, error) {
	vs, err := s.views(ctx, db, scope, []store.Document{*row})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

func (s *Service) writeAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, action, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action,
		EntityType: "document", EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, d *store.Document, change string) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.DocumentChanged, event.DocumentChangedData{DocumentID: d.ID, OwnerType: d.OwnerType, Change: change})
	return err
}

// checkOwner verifies that the owner of a document exists in the marina and, for a customer, can still change.
func checkOwner(ctx context.Context, db bun.IDB, scope store.MarinaScope, d *store.Document) error {
	if d.OwnerType == OwnerVessel {
		_, err := store.GetVessel(ctx, db, scope, d.VesselID)
		return notFound(err)
	}
	c, err := store.GetCustomer(ctx, db, scope, d.CustomerID)
	if err != nil {
		return notFound(err)
	}
	switch {
	case !c.AnonymizedAt.IsZero():
		return customer.ErrAnonymized
	case c.MergedIntoID != "":
		return customer.ErrMerged
	}
	return nil
}

// setFields validates and writes the editable fields, returning the names of those that changed.
func (s *Service) setFields(row *store.Document, p Patch) ([]string, error) {
	v := &auth.ValidationError{}
	var changed []string
	mark := func(f string, c bool) {
		if c {
			changed = append(changed, f)
		}
	}
	if p.Type != nil {
		if !slices.Contains(types, *p.Type) {
			v.Add("type", "invalid")
		}
		mark("type", row.Type != *p.Type)
		row.Type = *p.Type
	}
	if p.Number != nil {
		n, ok := clean(*p.Number, 64)
		switch {
		case !ok:
			v.Add("number", "too_long")
		case n == "":
			mark("number", row.NumberEnc != "")
			row.NumberEnc = ""
		default:
			enc, err := s.d.Keys.EncryptString(n, numberAAD(row.ID))
			if err != nil {
				return nil, err
			}
			changed = append(changed, "number")
			row.NumberEnc = enc
		}
	}
	if p.Issuer != nil {
		t, ok := clean(*p.Issuer, 200)
		if !ok {
			v.Add("issuer", "too_long")
		}
		mark("issuer", row.Issuer != t)
		row.Issuer = t
	}
	if p.Notes != nil {
		t, ok := clean(*p.Notes, 2000)
		if !ok {
			v.Add("notes", "too_long")
		}
		mark("notes", row.Notes != t)
		row.Notes = t
	}
	if p.IssuedOn != nil {
		mark("issued_on", !row.IssuedOn.Equal(*p.IssuedOn))
		row.IssuedOn = *p.IssuedOn
	}
	if p.ExpiresOn != nil {
		mark("expires_on", !row.ExpiresOn.Equal(*p.ExpiresOn))
		row.ExpiresOn = *p.ExpiresOn
	}
	if !row.IssuedOn.IsZero() && !row.ExpiresOn.IsZero() && row.ExpiresOn.Before(row.IssuedOn) {
		v.Add("expires_on", "before_issued_on")
	}
	return changed, v.Err()
}

// Create adds a document to a customer or a vessel and syncs its deadline (customers.write).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in Input) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	switch in.OwnerType {
	case OwnerCustomer:
		if in.CustomerID == "" || in.VesselID != "" {
			v.Add("customer_id", "required")
		}
	case OwnerVessel:
		if in.VesselID == "" || in.CustomerID != "" {
			v.Add("vessel_id", "required")
		}
	default:
		v.Add("owner_type", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.Document{
		ID: store.NewID(), MarinaID: scope.ID(), OwnerType: in.OwnerType, CustomerID: in.CustomerID,
		VesselID: in.VesselID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	t, number, issuer, notes := in.Type, in.Number, in.Issuer, in.Notes
	changed, err := s.setFields(row, Patch{Type: &t, Number: &number, Issuer: &issuer, Notes: &notes, IssuedOn: &in.IssuedOn, ExpiresOn: &in.ExpiresOn})
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkOwner(ctx, tx, scope, row); err != nil {
			return err
		}
		if err := store.InsertDocument(ctx, tx, row); err != nil {
			return err
		}
		if _, err := deadline.SyncDocument(ctx, tx, scope, row, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "document.create", row.ID, map[string]any{"type": row.Type, "owner_type": row.OwnerType, "fields": changed}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row, event.ChangeCreated); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// Get returns a document (customers.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.view(ctx, s.d.DB.DB, scope, row)
}

// Filter narrows List.
type Filter = store.DocumentFilter

// List returns a page of documents (customers.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[Document], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return store.Page[Document]{}, err
	}
	if f.Type != "" && !slices.Contains(types, f.Type) {
		return store.Page[Document]{}, &auth.ValidationError{Fields: []auth.FieldError{{Field: "type", Code: "invalid"}}}
	}
	page, err := store.ListDocuments(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Document]{}, err
	}
	items, err := s.views(ctx, s.d.DB.DB, scope, page.Items)
	if err != nil {
		return store.Page[Document]{}, err
	}
	return store.Page[Document]{Items: items, NextCursor: page.NextCursor}, nil
}

// OfCustomer returns every document of a customer (customers.read).
func (s *Service) OfCustomer(ctx context.Context, p *auth.Principal, marinaID, customerID string) ([]Document, error) {
	return s.ofOwner(ctx, p, marinaID, customerID, store.DocumentsOfCustomer)
}

// OfVessel returns every document of a vessel (customers.read).
func (s *Service) OfVessel(ctx context.Context, p *auth.Principal, marinaID, vesselID string) ([]Document, error) {
	return s.ofOwner(ctx, p, marinaID, vesselID, store.DocumentsOfVessel)
}

func (s *Service) ofOwner(ctx context.Context, p *auth.Principal, marinaID, id string,
	load func(context.Context, bun.IDB, store.MarinaScope, string) ([]store.Document, error),
) ([]Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	rows, err := load(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, s.d.DB.DB, scope, rows)
}

// Update changes a document and syncs its deadline (customers.write, If-Match).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in Patch) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetDocument(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		changed, err := s.setFields(row, in)
		if err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := checkOwner(ctx, tx, scope, row); err != nil {
			return err
		}
		now := s.now()
		row.UpdatedAt = now
		if err := store.UpdateDocument(ctx, tx, scope, ifMatch, row); err != nil {
			return versionErr(err)
		}
		if _, err := deadline.SyncDocument(ctx, tx, scope, row, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "document.update", row.ID, map[string]any{"fields": changed}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row, event.ChangeUpdated); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// Delete removes a document with its file and deadline (customers.write, If-Match).
func (s *Service) Delete(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetDocument(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := store.DeleteDeadlinesOfSource(ctx, tx, scope, deadline.SourceDocument, row.ID, ""); err != nil {
			return err
		}
		if err := store.DeleteDocument(ctx, tx, scope, row.ID, ifMatch); err != nil {
			return versionErr(err)
		}
		if err := s.dropFile(ctx, tx, scope, row.FileID); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "document.delete", row.ID, map[string]any{"type": row.Type}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, row, event.ChangeDeleted)
	})
}

// dropFile soft-deletes a stored file and removes its blob after commit ("" = nothing to do).
func (s *Service) dropFile(ctx context.Context, tx bun.IDB, scope store.MarinaScope, fileID string) error {
	if fileID == "" {
		return nil
	}
	key, err := store.SoftDeleteStoredFile(ctx, tx, scope, fileID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	store.AfterCommit(ctx, func() {
		if err := s.d.Files.Remove(key); err != nil {
			s.d.Log.WarnContext(ctx, "document: remove blob", "err", err)
		}
	})
	return nil
}

// SanitizeFileName keeps the base name of an uploaded file without control or path characters, at most 120
// characters; "" when nothing usable is left.
func SanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(strings.TrimSpace(name))
	if name == "." || name == "/" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxFileName {
		out = string([]rune(out)[:maxFileName])
	}
	return out
}

// UploadFile stores (or replaces) the file of a document, encrypted at rest (customers.write). The content
// type is sniffed from the bytes and must match the declared one: PDF, PNG or JPEG, at most 15 MiB. The
// caller's authorisation is checked before the body is read.
func (s *Service) UploadFile(ctx context.Context, p *auth.Principal, marinaID, id, declaredType, fileName string, body io.Reader) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	row, err := store.GetDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	mt, _, perr := mime.ParseMediaType(declaredType)
	if perr != nil || !slices.Contains(fileTypes, mt) {
		return nil, ErrUnsupportedMedia
	}
	content, err := io.ReadAll(io.LimitReader(body, MaxFileBytes+1))
	if err != nil {
		// The per-operation body limit (16 MiB) is above MaxFileBytes+1, so this is a broken upload.
		return nil, ErrInvalidFile
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, ErrFileTooLarge
	}
	if len(content) == 0 {
		return nil, ErrInvalidFile
	}
	ct, w, h, err := filestore.Sniff(content, mt)
	switch {
	case errors.Is(err, filestore.ErrUnsupportedType):
		return nil, ErrUnsupportedMedia
	case err != nil:
		return nil, ErrInvalidFile
	}
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(content), MaxFileBytes)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	name := SanitizeFileName(fileName)
	if name == "" {
		name = "document" + extension(ct)
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetDocument(ctx, tx, scope, row.ID)
		if err != nil {
			return notFound(err)
		}
		if err := checkOwner(ctx, tx, scope, row); err != nil {
			return err
		}
		now := s.now()
		f := &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: purpose, ContentType: ct, SizeBytes: blob.Size,
			SHA256: blob.SHA256, StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertStoredFile(ctx, tx, f); err != nil {
			return err
		}
		old := row.FileID
		row.FileID, row.FileName, row.UpdatedAt = fileID, name, now
		if err := store.UpdateDocument(ctx, tx, scope, 0, row); err != nil {
			return versionErr(err)
		}
		if err := s.dropFile(ctx, tx, scope, old); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "document.file", row.ID, map[string]any{"content_type": ct, "size_bytes": blob.Size}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row, event.ChangeFile); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, row)
		return err
	})
	if err == nil {
		committed = true
	}
	return out, err
}

func extension(ct string) string {
	switch ct {
	case filestore.TypePDF:
		return ".pdf"
	case filestore.TypePNG:
		return ".png"
	default:
		return ".jpg"
	}
}

// DeleteFile removes the file of a document (customers.write). A document without a file is not found.
func (s *Service) DeleteFile(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetDocument(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if row.FileID == "" {
			return auth.ErrNotFound
		}
		old := row.FileID
		row.FileID, row.FileName, row.UpdatedAt = "", "", s.now()
		if err := store.UpdateDocument(ctx, tx, scope, 0, row); err != nil {
			return versionErr(err)
		}
		if err := s.dropFile(ctx, tx, scope, old); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "document.file_delete", row.ID, nil); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, row, event.ChangeFile)
	})
}

// signature is the HMAC of a download link: it binds the marina, the document, the principal and the expiry.
func (s *Service) signature(marinaID, documentID, principalID string, exp int64) (string, error) {
	key, err := s.d.Keys.Derive(urlKeyName, 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(marinaID + "|" + documentID + "|" + principalID + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// FileURL returns a download link valid for 5 minutes and only for the caller (customers.read). A document
// without a file is not found.
func (s *Service) FileURL(ctx context.Context, p *auth.Principal, marinaID, id string) (*FileURL, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if row.FileID == "" {
		return nil, auth.ErrNotFound
	}
	expires := s.d.Clock.Now().Add(LinkTTL).UTC().Truncate(time.Second)
	sig, err := s.signature(scope.ID(), row.ID, p.ID(), expires.Unix())
	if err != nil {
		return nil, err
	}
	u := "/api/admin/v1/marinas/" + scope.ID() + "/documents/" + row.ID + "/file?exp=" +
		strconv.FormatInt(expires.Unix(), 10) + "&sig=" + sig
	return &FileURL{URL: u, ExpiresAt: expires}, nil
}

// OpenFile checks a signed link and returns the decrypted file (customers.read). A missing, tampered,
// foreign or expired signature is ErrLinkExpired.
func (s *Service) OpenFile(ctx context.Context, p *auth.Principal, marinaID, id string, exp int64, sig string) (*File, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetDocument(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if row.FileID == "" {
		return nil, auth.ErrNotFound
	}
	want, err := s.signature(scope.ID(), row.ID, p.ID(), exp)
	if err != nil {
		return nil, err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, ErrLinkExpired
	}
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, row.FileID)
	if err != nil {
		return nil, notFound(err)
	}
	content, err := s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
	if err != nil {
		return nil, err
	}
	return &File{Content: content, ContentType: f.ContentType, FileName: row.FileName}, nil
}
