package maintenance

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

// Task files (m9-plan §8.3): photos and documents uploaded as the raw body (offline kind maintenance.report through
// the dependent upload), stored encrypted through filestore with purpose "document" (INV-74), linked by
// maintenance_task_files, served through a signed link valid 5 minutes for the caller only.

const (
	// MaxFileBytes bounds a task file.
	MaxFileBytes = 8 << 20
	// FileLinkTTL is the validity of a file link.
	FileLinkTTL = 5 * time.Minute
	fileKeyName = "maintenance.file_url"
	filePurpose = "document"
)

var fileTypes = []string{filestore.TypeJPEG, filestore.TypePNG, filestore.TypePDF}

// UploadFile attaches a photo or document to a task (maintenance.manage, or maintenance.report on a task the user
// may work on or reported). The same bytes as an existing file of the task return that file.
func (s *Service) UploadFile(ctx context.Context, p *auth.Principal, marinaID, taskID string, f FileUpload) (*TaskFile, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	t, err := store.GetMaintenanceTask(ctx, s.d.DB.DB, scope, taskID)
	if err != nil {
		return nil, notFound(err)
	}
	if !canWork(p, scope, perm, t) && t.ReportedByUserID != p.UserID {
		return nil, auth.ErrForbidden
	}
	mt, _, perr := mime.ParseMediaType(f.ContentType)
	if perr != nil || !contains(fileTypes, mt) {
		return nil, document.ErrUnsupportedMedia
	}
	if len(f.Data) > MaxFileBytes {
		return nil, document.ErrFileTooLarge
	}
	if len(f.Data) == 0 {
		return nil, document.ErrInvalidFile
	}
	ct, w, h, err := filestore.Sniff(f.Data, mt)
	switch {
	case errors.Is(err, filestore.ErrUnsupportedType):
		return nil, document.ErrUnsupportedMedia
	case err != nil:
		return nil, document.ErrInvalidFile
	}
	if s.d.Files == nil {
		return nil, errors.New("maintenance: file store not wired")
	}
	sum := sha256.Sum256(f.Data)
	digest := hex.EncodeToString(sum[:])
	if same, err := sameFile(ctx, s.d.DB.DB, scope, taskID, digest); err != nil || same != nil {
		return same, err
	}
	fileID := store.NewID()
	blob, err := s.d.Files.Put(ctx, scope.ID(), fileID, bytes.NewReader(f.Data), MaxFileBytes)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.d.Files.Remove(blob.StorageKey)
		}
	}()
	kind := "photo"
	if ct == filestore.TypePDF {
		kind = "document"
	}
	var existing *TaskFile
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMaintenanceTask(ctx, tx, scope, taskID)
		if err != nil {
			return notFound(err)
		}
		if existing, err = sameFile(ctx, tx, scope, taskID, digest); err != nil || existing != nil {
			return err
		}
		now := s.now()
		sf := &store.StoredFile{
			ID: fileID, MarinaID: scope.ID(), Purpose: filePurpose, ContentType: ct, SizeBytes: blob.Size,
			SHA256: blob.SHA256, StorageKey: blob.StorageKey, WidthPx: int64(w), HeightPx: int64(h),
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertStoredFile(ctx, tx, sf); err != nil {
			return err
		}
		if err := store.InsertMaintenanceTaskFile(ctx, tx, scope, &store.MaintenanceTaskFile{
			ID: store.NewID(), TaskID: t.ID, FileID: fileID, Kind: kind, CreatedByUserID: p.UserID, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "maintenance_task.file", "maintenance_task", t.ID,
			map[string]any{"content_type": ct, "size_bytes": blob.Size, "kind": kind}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "task", t.ID, t.ElementType, t.ElementID, t.Status)
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil // the deferred cleanup removes the blob just written
	}
	committed = true
	files, err := store.MaintenanceTaskFiles(ctx, s.d.DB.DB, scope, []string{taskID})
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].FileID == fileID {
			return &files[i], nil
		}
	}
	return nil, store.ErrNotFound
}

// sameFile returns the file of the task whose plaintext SHA-256 is digest, nil when none.
func sameFile(ctx context.Context, db bun.IDB, scope store.MarinaScope, taskID, digest string) (*TaskFile, error) {
	files, err := store.MaintenanceTaskFiles(ctx, db, scope, []string{taskID})
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].SHA256 == digest {
			return &files[i], nil
		}
	}
	return nil, nil
}

func (s *Service) fileSignature(marinaID, taskID, fileID, principalID string, exp int64) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("maintenance: key ring not wired")
	}
	key, err := s.d.Keys.Derive(fileKeyName, 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(marinaID + "|" + taskID + "|" + fileID + "|" + principalID + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func (s *Service) taskFile(ctx context.Context, p *auth.Principal, marinaID, taskID, fileID string) (store.MarinaScope, *TaskFile, error) {
	scope, perm, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return store.MarinaScope{}, nil, err
	}
	t, err := store.GetMaintenanceTask(ctx, s.d.DB.DB, scope, taskID)
	if err != nil {
		return store.MarinaScope{}, nil, notFound(err)
	}
	if !canWork(p, scope, perm, t) && t.ReportedByUserID != p.UserID {
		return store.MarinaScope{}, nil, auth.ErrForbidden
	}
	files, err := store.MaintenanceTaskFiles(ctx, s.d.DB.DB, scope, []string{taskID})
	if err != nil {
		return store.MarinaScope{}, nil, err
	}
	for i := range files {
		if files[i].FileID == fileID {
			return scope, &files[i], nil
		}
	}
	return store.MarinaScope{}, nil, auth.ErrNotFound
}

// FileLink returns a 5-minute signed link to a task file for the caller only. The link targets
// GET …/maintenance/tasks/{taskId}/files/{fileId}/file?exp=&sig= (served by OpenFile).
func (s *Service) FileLink(ctx context.Context, p *auth.Principal, marinaID, taskID, fileID string) (*FileLink, error) {
	scope, f, err := s.taskFile(ctx, p, marinaID, taskID, fileID)
	if err != nil {
		return nil, err
	}
	expires := s.d.Clock.Now().Add(FileLinkTTL).UTC().Truncate(time.Second)
	sig, err := s.fileSignature(scope.ID(), taskID, f.FileID, p.ID(), expires.Unix())
	if err != nil {
		return nil, err
	}
	u := "/api/admin/v1/marinas/" + scope.ID() + "/maintenance/tasks/" + taskID + "/files/" + f.FileID + "/file?exp=" +
		strconv.FormatInt(expires.Unix(), 10) + "&sig=" + sig
	return &FileLink{URL: u, ExpiresAt: expires}, nil
}

// OpenFile checks a signed link and returns the decrypted file. A missing, tampered, foreign or expired signature
// is document.ErrLinkExpired.
func (s *Service) OpenFile(ctx context.Context, p *auth.Principal, marinaID, taskID, fileID string, exp int64, sig string) (*File, error) {
	scope, f, err := s.taskFile(ctx, p, marinaID, taskID, fileID)
	if err != nil {
		return nil, err
	}
	want, err := s.fileSignature(scope.ID(), taskID, f.FileID, p.ID(), exp)
	if err != nil {
		return nil, err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, document.ErrLinkExpired
	}
	content, err := s.d.Files.Open(ctx, scope.ID(), f.FileID, f.StorageKey)
	if err != nil {
		return nil, err
	}
	ext := map[string]string{filestore.TypePNG: ".png", filestore.TypePDF: ".pdf"}[f.ContentType]
	if ext == "" {
		ext = ".jpg"
	}
	return &File{Content: content, ContentType: f.ContentType, FileName: "maintenance-file" + ext}, nil
}
