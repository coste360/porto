package presence

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Presence photos (m3-plan §4.4): uploaded as the raw body, stored encrypted through filestore with purpose
// "document" (the stored_files CHECK of M1 knows no other purpose for photos; presences.photo_file_id records
// what the file is), served through a signed link valid 5 minutes for the caller only (customers.read).

const (
	// MaxPhotoBytes bounds a photo.
	MaxPhotoBytes = 8 << 20
	// PhotoLinkTTL is the validity of a photo link.
	PhotoLinkTTL  = 5 * time.Minute
	photoKeyName  = "presences.photo_url"
	photoPurpose  = "document"
	photoFallback = "presence-photo"
)

var photoTypes = []string{filestore.TypeJPEG, filestore.TypePNG}

// PhotoLink is a signed link to a presence photo.
type PhotoLink struct {
	URL       string
	ExpiresAt time.Time
}

// Photo is a decrypted presence photo.
type Photo struct {
	Content     []byte
	ContentType string
	FileName    string
}

func cleanText(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= maxRunes
}

// UploadPhoto stores the photo of a presence, replacing the previous one (presence.write). The service
// authorises before it looks at the content type or reads the body.
func (s *Service) UploadPhoto(ctx context.Context, p *auth.Principal, marinaID, id, declaredType string, body io.Reader) (string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	if err != nil {
		return "", err
	}
	if _, err := store.GetPresence(ctx, s.d.DB.DB, scope, id); err != nil {
		return "", notFound(err)
	}
	mt, _, perr := mime.ParseMediaType(declaredType)
	if perr != nil || !slices.Contains(photoTypes, mt) {
		return "", document.ErrUnsupportedMedia
	}
	content, err := io.ReadAll(io.LimitReader(body, MaxPhotoBytes+1))
	if err != nil {
		return "", document.ErrInvalidFile
	}
	if len(content) > MaxPhotoBytes {
		return "", document.ErrFileTooLarge
	}
	if len(content) == 0 {
		return "", document.ErrInvalidFile
	}
	ct, w, h, err := filestore.Sniff(content, mt)
	switch {
	case errors.Is(err, filestore.ErrUnsupportedType):
		return "", document.ErrUnsupportedMedia
	case err != nil:
		return "", document.ErrInvalidFile
	}
	if s.d.Files == nil {
		return "", errors.New("presence: file store not wired")
	}
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(content), MaxPhotoBytes)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	var oldKey string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pr, err := store.GetPresence(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		now := s.now()
		f := &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: photoPurpose, ContentType: ct, SizeBytes: blob.Size,
			SHA256: blob.SHA256, StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertStoredFile(ctx, tx, f); err != nil {
			return err
		}
		if err := store.SetPresencePhoto(ctx, tx, scope, pr.ID, fileID, now); err != nil {
			return err
		}
		if pr.PhotoFileID != "" {
			k, err := store.SoftDeleteStoredFile(ctx, tx, scope, pr.PhotoFileID, now)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			oldKey = k
		}
		return s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "presence.photo",
			EntityType: "presence", EntityID: pr.ID, Diff: map[string]any{"content_type": ct, "size_bytes": blob.Size},
		})
	})
	if err != nil {
		return "", err
	}
	committed = true
	if oldKey != "" {
		_ = s.d.Files.Remove(oldKey)
	}
	return fileID, nil
}

func (s *Service) photoSignature(marinaID, presenceID, principalID string, exp int64) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("presence: key ring not wired")
	}
	key, err := s.d.Keys.Derive(photoKeyName, 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(marinaID + "|" + presenceID + "|" + principalID + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// PhotoLink returns a signed link to the photo of a presence (customers.read). Without a photo it is not found.
func (s *Service) PhotoLink(ctx context.Context, p *auth.Principal, marinaID, id string) (*PhotoLink, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	pr, err := store.GetPresence(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if pr.PhotoFileID == "" {
		return nil, auth.ErrNotFound
	}
	expires := s.d.Clock.Now().Add(PhotoLinkTTL).UTC().Truncate(time.Second)
	sig, err := s.photoSignature(scope.ID(), pr.ID, p.ID(), expires.Unix())
	if err != nil {
		return nil, err
	}
	u := "/api/admin/v1/marinas/" + scope.ID() + "/presences/" + pr.ID + "/photo/file?exp=" +
		strconv.FormatInt(expires.Unix(), 10) + "&sig=" + sig
	return &PhotoLink{URL: u, ExpiresAt: expires}, nil
}

// OpenPhoto checks a signed link and returns the decrypted photo (customers.read). A missing, tampered, foreign
// or expired signature is document.ErrLinkExpired.
func (s *Service) OpenPhoto(ctx context.Context, p *auth.Principal, marinaID, id string, exp int64, sig string) (*Photo, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	pr, err := store.GetPresence(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if pr.PhotoFileID == "" {
		return nil, auth.ErrNotFound
	}
	want, err := s.photoSignature(scope.ID(), pr.ID, p.ID(), exp)
	if err != nil {
		return nil, err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, document.ErrLinkExpired
	}
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, pr.PhotoFileID)
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
	return &Photo{Content: content, ContentType: f.ContentType, FileName: photoFallback + ext}, nil
}
