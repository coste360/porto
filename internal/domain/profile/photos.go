package profile

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Photo limits (m12-plan §12.1, D24).
const (
	// MaxPhotoBytes bounds an upload.
	MaxPhotoBytes = 8 << 20
	// MaxPhotoSide is the longest side of a stored photo.
	MaxPhotoSide = 2400
	// maxSourcePixels bounds the decoded size of an upload (decompression bombs).
	maxSourcePixels = 40_000_000
	jpegQuality     = 85
	// filePurpose is the stored_files purpose of profile photos (the 00002 CHECK is frozen).
	filePurpose = "document"
)

// Upload errors (the handler maps them to 415 and 413).
var (
	ErrUnsupportedMedia = errors.New("profile: unsupported media type")
	ErrPhotoTooLarge    = errors.New("profile: photo too large")
)

// reencode decodes a JPEG or PNG and encodes it again with the standard library, which writes no metadata (EXIF,
// GPS, XMP, text chunks are dropped), shrinking the longest side to MaxPhotoSide with an integer box filter.
func reencode(body []byte, contentType string) ([]byte, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, ErrPhotoInvalid
	}
	want := map[string]string{filestore.TypeJPEG: "jpeg", filestore.TypePNG: "png"}[contentType]
	if format != want || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > filestore.MaxImageSide ||
		cfg.Height > filestore.MaxImageSide || int64(cfg.Width)*int64(cfg.Height) > maxSourcePixels {
		return nil, 0, 0, ErrPhotoInvalid
	}
	src, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, ErrPhotoInvalid
	}
	img := shrink(src, MaxPhotoSide)
	var out bytes.Buffer
	if contentType == filestore.TypeJPEG {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return out.Bytes(), b.Dx(), b.Dy(), nil
}

// shrink returns src when its longest side is at most side, else a box-filtered copy whose longest side is side
// (the other side rounded down, at least 1). Integer arithmetic only.
func shrink(src image.Image, side int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if max(sw, sh) <= side {
		return src
	}
	dw, dh := side, max(1, sh*side/sw)
	if sh > sw {
		dw, dh = max(1, sw*side/sh), side
	}
	type acc struct{ r, g, b, a, n uint64 }
	bins := make([]acc, dw*dh)
	for y := range sh {
		dy := y * dh / sh
		for x := range sw {
			dx := x * dw / sw
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			c := &bins[dy*dw+dx]
			c.r += uint64(r)
			c.g += uint64(g)
			c.b += uint64(bl)
			c.a += uint64(a)
			c.n++
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for i, c := range bins {
		if c.n == 0 {
			continue
		}
		o := i * 4
		dst.Pix[o] = uint8((c.r / c.n) >> 8)
		dst.Pix[o+1] = uint8((c.g / c.n) >> 8)
		dst.Pix[o+2] = uint8((c.b / c.n) >> 8)
		dst.Pix[o+3] = uint8((c.a / c.n) >> 8)
	}
	return dst
}

// UploadPhoto decodes, re-encodes (no metadata, longest side at most 2,400 px) and stores a photo (marina.configure).
// The photo is referenced by the profile only through a later PUT.
func (s *Service) UploadPhoto(ctx context.Context, p *auth.Principal, marinaID, contentType string, body io.Reader) (*PhotoFile, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	mt, _, perr := mime.ParseMediaType(contentType)
	if perr != nil || (mt != filestore.TypeJPEG && mt != filestore.TypePNG) {
		return nil, ErrUnsupportedMedia
	}
	raw, err := io.ReadAll(io.LimitReader(body, MaxPhotoBytes+1))
	if err != nil {
		return nil, ErrPhotoInvalid
	}
	if len(raw) > MaxPhotoBytes {
		return nil, ErrPhotoTooLarge
	}
	clean, w, h, err := reencode(raw, mt)
	if err != nil {
		return nil, err
	}
	if s.d.Files == nil {
		return nil, errors.New("profile: file store not wired")
	}
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(clean), filestore.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		if err := store.InsertStoredFile(ctx, tx, &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: filePurpose, ContentType: mt, SizeBytes: blob.Size, SHA256: blob.SHA256,
			StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h), CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, actPhotoUpload, entityPhoto, fileID, "photo", map[string]any{
			"file_id": fileID, "content_type": mt, "width": w, "height": h, "size_bytes": blob.Size,
		})
	})
	if err != nil {
		return nil, err
	}
	committed = true
	return &PhotoFile{FileID: fileID, ContentType: mt, Width: w, Height: h, SizeBytes: blob.Size}, nil
}

// DeletePhoto deletes an uploaded photo and removes it from the profile (marina.configure). Another marina's file
// or a file not uploaded through the profile is 404. If-Match is the profile version (0 when never saved); the
// photo leaves photos and access_images and the version is bumped when it was referenced.
func (s *Service) DeletePhoto(ctx context.Context, p *auth.Principal, marinaID, fileID string, ifMatch int) (*View, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out *View
	var storageKey string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, m, err := load(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		ok, err := s.isProfilePhoto(ctx, tx, scope, fileID)
		if err != nil {
			return err
		}
		if !ok {
			return auth.ErrNotFound
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		var photos, access []Photo
		decodeJSON(row.Photos, &photos)
		decodeJSON(row.AccessImages, &access)
		drop := func(ps []Photo) ([]Photo, bool) {
			out := slices.DeleteFunc(slices.Clone(ps), func(x Photo) bool { return x.FileID == fileID })
			for i := range out {
				out[i].Sort = i
			}
			if out == nil {
				out = []Photo{}
			}
			return out, len(out) != len(ps)
		}
		photos, inPhotos := drop(photos)
		access, inAccess := drop(access)
		now := s.now()
		next := *row
		if row.Version > 0 && (inPhotos || inAccess) {
			next.Photos, next.AccessImages = store.MustJSON(photos), store.MustJSON(access)
			if err := store.SaveMarinaProfile(ctx, tx, scope, ifMatch, &next, now); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return ErrVersionMismatch
				}
				return err
			}
		}
		if storageKey, err = store.SoftDeleteStoredFile(ctx, tx, scope, fileID, now); err != nil {
			return notFound(err)
		}
		if err := s.write(ctx, tx, p, scope, actPhotoDelete, entityPhoto, fileID, "photo", map[string]any{
			"file_id": fileID, "version": next.Version,
		}); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, scope, &next, m)
		return err
	})
	if err != nil {
		return nil, err
	}
	if s.d.Files != nil && storageKey != "" {
		_ = s.d.Files.Remove(storageKey)
	}
	s.forget(scope.ID())
	return out, nil
}

// GetPhoto returns the bytes of an uploaded photo, published or not (marina.read). Another marina's file or a
// file not uploaded through the profile is 404.
func (s *Service) GetPhoto(ctx context.Context, p *auth.Principal, marinaID, fileID string) (*Image, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	ok, err := s.isProfilePhoto(ctx, s.d.DB.DB, scope, fileID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, auth.ErrNotFound
	}
	return s.readPhoto(ctx, scope, fileID)
}

func (s *Service) readPhoto(ctx context.Context, scope store.MarinaScope, fileID string) (*Image, error) {
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, fileID)
	if err != nil {
		return nil, notFound(err)
	}
	if s.d.Files == nil {
		return nil, errors.New("profile: file store not wired")
	}
	body, err := s.d.Files.Open(ctx, scope.ID(), fileID, f.StorageKey)
	if err != nil {
		return nil, err
	}
	return &Image{ContentType: f.ContentType, Body: body, ETag: `"` + f.SHA256 + `"`}, nil
}
