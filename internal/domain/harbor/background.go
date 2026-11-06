package harbor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// BackgroundFile is an uploaded background image (MapBackgroundFile).
type BackgroundFile struct {
	FileID      string
	ContentType string
	WidthPx     int64
	HeightPx    int64
	SizeBytes   int64
}

// readLimited reads at most max bytes; more is ErrFileTooLarge.
func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, ErrInvalidFile
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		if strings.Contains(err.Error(), "too large") { // http.MaxBytesError of the request body limit
			return nil, ErrFileTooLarge
		}
		return nil, fmt.Errorf("harbor: read upload: %w", err)
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrFileTooLarge
	}
	if len(b) == 0 {
		return nil, ErrInvalidFile
	}
	return b, nil
}

// sniffError maps a filestore sniffing error.
func sniffError(err error) error {
	if errors.Is(err, filestore.ErrUnsupportedType) {
		return ErrUnsupportedMedia
	}
	return ErrInvalidFile
}

// UploadBackground stores a background image of a map (harbor.configure; spec 8.2). The content is sniffed, never
// trusted from the declared type: PNG, JPEG or SVG (without scripts or external references), at most 15 MiB.
// The calibration is saved with the map document; the file is encrypted at rest (internal/filestore).
func (s *Service) UploadBackground(ctx context.Context, p *auth.Principal, marinaID, mapID, contentType string, body io.Reader) (*BackgroundFile, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetMap(ctx, s.d.DB.DB, scope, mapID); err != nil {
		return nil, err
	}
	content, err := readLimited(body, filestore.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	ct, w, h, err := filestore.Sniff(content, contentType)
	if err != nil {
		return nil, sniffError(err)
	}
	if !slices.Contains(imageMediaTypes, ct) {
		return nil, ErrUnsupportedMedia
	}
	if w <= 0 || h <= 0 {
		return nil, ErrInvalidFile // an SVG without viewBox or size cannot be calibrated
	}
	id := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), id, bytes.NewReader(content), filestore.MaxFileBytes)
	if err != nil {
		if errors.Is(err, filestore.ErrTooLarge) {
			return nil, ErrFileTooLarge
		}
		return nil, err
	}
	now := s.now()
	f := &store.StoredFile{
		ID: id, MarinaID: scope.ID(), Purpose: purposeBackground, MapID: mapID, ContentType: ct, SizeBytes: blob.Size, SHA256: blob.SHA256,
		StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h), CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertStoredFile(ctx, tx, f); err != nil {
			return err
		}
		return s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "map.background_upload", EntityType: "map",
			EntityID: mapID, Diff: map[string]any{"file_id": id, "content_type": ct, "size_bytes": blob.Size},
		})
	})
	if err != nil {
		_ = s.d.Files.Remove(blob.StorageKey)
		return nil, err
	}
	return &BackgroundFile{FileID: id, ContentType: ct, WidthPx: int64(w), HeightPx: int64(h), SizeBytes: blob.Size}, nil
}

// Background returns the bytes and type of a background image of a map (map.view).
func (s *Service) Background(ctx context.Context, p *auth.Principal, marinaID, mapID, fileID string) ([]byte, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, "", err
	}
	m, err := store.GetMap(ctx, s.d.DB.DB, scope, mapID)
	if err != nil {
		return nil, "", err
	}
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, fileID)
	if err != nil {
		return nil, "", err
	}
	// A background of another map of the same marina is not found (m1-plan §0 rule 10): the file must be the
	// map's saved background or one uploaded for this map (shown by the editor before the save).
	if f.Purpose != purposeBackground || (f.MapID != mapID && m.BackgroundFileID != f.ID) {
		return nil, "", store.ErrNotFound
	}
	b, err := s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
	if err != nil {
		return nil, "", err
	}
	return b, f.ContentType, nil
}
