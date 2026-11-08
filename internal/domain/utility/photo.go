package utility

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"mime"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Reading photos (m9-plan §9.2): the raw body (offline dependent op of utility.reading), stored encrypted through
// filestore with purpose "document" (INV-74), served through a signed link valid 5 minutes for the caller only.

const (
	// MaxPhotoBytes bounds a reading photo.
	MaxPhotoBytes = 8 << 20
	// PhotoLinkTTL is the validity of a photo link.
	PhotoLinkTTL = 5 * time.Minute
	photoKeyName = "utility.reading_photo_url"
	photoPurpose = "document"
)

var photoTypes = []string{filestore.TypeJPEG, filestore.TypePNG}

// PhotoLink is a signed link to a reading photo.
type PhotoLink struct {
	URL       string
	ExpiresAt time.Time
}

// Photo is a decrypted reading photo.
type Photo struct {
	Content     []byte
	ContentType string
	FileName    string
}

// UploadPhoto attaches a photo to a reading, replacing the previous one (presence.write). The same bytes as the
// current photo return the current file id without writing again (INV-83).
func (s *Service) UploadPhoto(ctx context.Context, p *auth.Principal, marinaID, readingID string, f PhotoUpload) (string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	if err != nil {
		return "", err
	}
	if _, err := store.GetMeterReading(ctx, s.d.DB.DB, scope, readingID); err != nil {
		return "", notFound(err)
	}
	mt, _, perr := mime.ParseMediaType(f.ContentType)
	if perr != nil || !contains(photoTypes, mt) {
		return "", document.ErrUnsupportedMedia
	}
	if len(f.Data) > MaxPhotoBytes {
		return "", document.ErrFileTooLarge
	}
	if len(f.Data) == 0 {
		return "", document.ErrInvalidFile
	}
	ct, w, h, err := filestore.Sniff(f.Data, mt)
	switch {
	case errors.Is(err, filestore.ErrUnsupportedType):
		return "", document.ErrUnsupportedMedia
	case err != nil:
		return "", document.ErrInvalidFile
	}
	if s.d.Files == nil {
		return "", errors.New("utility: file store not wired")
	}
	sum := sha256.Sum256(f.Data)
	digest := hex.EncodeToString(sum[:])
	if same, err := currentPhotoWithDigest(ctx, s.d.DB.DB, scope, readingID, digest); err != nil || same != "" {
		return same, err
	}
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(f.Data), MaxPhotoBytes)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	var oldKey, existing string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetMeterReading(ctx, tx, scope, readingID)
		if err != nil {
			return notFound(err)
		}
		if _, err := store.LockUtilityMeter(ctx, tx, scope, r.MeterID); err != nil {
			return err
		}
		if existing, err = currentPhotoWithDigest(ctx, tx, scope, readingID, digest); err != nil || existing != "" {
			return err
		}
		if r, err = store.GetMeterReading(ctx, tx, scope, readingID); err != nil {
			return err
		}
		now := s.now()
		sf := &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: photoPurpose, ContentType: ct, SizeBytes: blob.Size, SHA256: blob.SHA256,
			StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h), CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertStoredFile(ctx, tx, sf); err != nil {
			return err
		}
		prev := r.PhotoFileID
		v := r.Version
		r.PhotoFileID = fileID
		if err := store.UpdateMeterReading(ctx, tx, scope, r, v, now); err != nil {
			return err
		}
		if prev != "" {
			k, err := store.SoftDeleteStoredFile(ctx, tx, scope, prev, now)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			oldKey = k
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "meter_reading.photo", "meter_reading", r.ID,
			map[string]any{"content_type": ct, "size_bytes": blob.Size}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "reading", r.ID, r.MeterID, r.Status)
	})
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil // the deferred cleanup removes the blob just written
	}
	committed = true
	if oldKey != "" {
		_ = s.d.Files.Remove(oldKey)
	}
	return fileID, nil
}

// currentPhotoWithDigest returns the id of the reading's photo when its plaintext SHA-256 is digest, or "".
func currentPhotoWithDigest(ctx context.Context, db bun.IDB, scope store.MarinaScope, readingID, digest string) (string, error) {
	r, err := store.GetMeterReading(ctx, db, scope, readingID)
	if err != nil {
		return "", notFound(err)
	}
	if r.PhotoFileID == "" {
		return "", nil
	}
	f, err := store.GetStoredFile(ctx, db, scope, r.PhotoFileID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if f.SHA256 == digest {
		return f.ID, nil
	}
	return "", nil
}

func (s *Service) photoSignature(marinaID, readingID, principalID string, exp int64) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("utility: key ring not wired")
	}
	key, err := s.d.Keys.Derive(photoKeyName, 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(marinaID + "|" + readingID + "|" + principalID + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func (s *Service) photoReading(ctx context.Context, p *auth.Principal, marinaID, readingID string) (store.MarinaScope, *store.MeterReading, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMarinaConfigure, auth.PermPresenceWrite, auth.PermChargesManage)
	if err != nil {
		return store.MarinaScope{}, nil, err
	}
	r, err := store.GetMeterReading(ctx, s.d.DB.DB, scope, readingID)
	if err != nil {
		return store.MarinaScope{}, nil, notFound(err)
	}
	if r.PhotoFileID == "" {
		return store.MarinaScope{}, nil, auth.ErrNotFound
	}
	return scope, r, nil
}

// PhotoLink returns a 5-minute signed link to the reading photo for the caller only (marina.configure,
// presence.write or charges.manage). The link targets GET …/meter-readings/{readingId}/photo/file?exp=&sig=
// (served by OpenPhoto).
func (s *Service) PhotoLink(ctx context.Context, p *auth.Principal, marinaID, readingID string) (*PhotoLink, error) {
	scope, r, err := s.photoReading(ctx, p, marinaID, readingID)
	if err != nil {
		return nil, err
	}
	expires := s.d.Clock.Now().Add(PhotoLinkTTL).UTC().Truncate(time.Second)
	sig, err := s.photoSignature(scope.ID(), r.ID, p.ID(), expires.Unix())
	if err != nil {
		return nil, err
	}
	u := "/api/admin/v1/marinas/" + scope.ID() + "/meter-readings/" + r.ID + "/photo/file?exp=" +
		strconv.FormatInt(expires.Unix(), 10) + "&sig=" + sig
	return &PhotoLink{URL: u, ExpiresAt: expires}, nil
}

// OpenPhoto checks a signed link and returns the decrypted photo. A missing, tampered, foreign or expired signature
// is document.ErrLinkExpired.
func (s *Service) OpenPhoto(ctx context.Context, p *auth.Principal, marinaID, readingID string, exp int64, sig string) (*Photo, error) {
	scope, r, err := s.photoReading(ctx, p, marinaID, readingID)
	if err != nil {
		return nil, err
	}
	want, err := s.photoSignature(scope.ID(), r.ID, p.ID(), exp)
	if err != nil {
		return nil, err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, document.ErrLinkExpired
	}
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, r.PhotoFileID)
	if err != nil {
		return nil, notFound(err)
	}
	content, err := s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
	if err != nil {
		return nil, err
	}
	ext := ".jpg"
	if f.ContentType == filestore.TypePNG {
		ext = ".png"
	}
	return &Photo{Content: content, ContentType: f.ContentType, FileName: "meter-reading" + ext}, nil
}
