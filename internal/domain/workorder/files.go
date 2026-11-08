package workorder

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"mime"
	"slices"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Task files (m10-plan §5.3): photos and documents uploaded as the raw body (offline kind yard.activity_done through
// the dependent upload), stored encrypted through filestore with purpose "document" (INV-74), linked by
// work_task_files, served through a signed link valid 5 minutes for the caller only. A replay (same client_ref or
// same bytes) returns the file already stored.

const (
	// MaxFileBytes bounds a task file.
	MaxFileBytes = 8 << 20
	// FileLinkTTL is the validity of a file link.
	FileLinkTTL = 5 * time.Minute
	fileKeyName = "workorder.file_url"
	filePurpose = "document"
)

var fileTypes = []string{filestore.TypeJPEG, filestore.TypePNG, filestore.TypePDF}

// fileScope authorises a file operation: yard.manage, yard.read (reads only) or yard.timesheets on an assigned task.
func (s *Service) fileScope(ctx context.Context, p *auth.Principal, marinaID, taskID string, write bool) (store.MarinaScope, *store.WorkTask, error) {
	var extra []auth.Permission
	if !write {
		extra = []auth.Permission{auth.PermYardRead}
	}
	scope, perm, e, err := s.workerScope(ctx, p, marinaID, extra...)
	if err != nil {
		return scope, nil, err
	}
	t, err := store.GetWorkTask(ctx, s.d.DB.DB, scope, taskID)
	if err != nil {
		return scope, nil, notFound(err)
	}
	if err := s.assignedTask(ctx, s.d.DB.DB, scope, perm, e, taskID); err != nil {
		return scope, nil, err
	}
	return scope, t, nil
}

// UploadFile attaches a photo or document to a task (yard.manage, or yard.timesheets on an assigned task).
func (s *Service) UploadFile(ctx context.Context, p *auth.Principal, marinaID, taskID string, f FileUpload) (*TaskFile, error) {
	scope, _, err := s.fileScope(ctx, p, marinaID, taskID, true)
	if err != nil {
		return nil, err
	}
	if len(f.ClientRef) > maxClientRef {
		f.ClientRef = ""
	}
	if f.ClientRef != "" {
		if prev, err := store.WorkTaskFileByClientRef(ctx, s.d.DB.DB, scope, f.ClientRef); err == nil {
			if prev.TaskID != taskID {
				return nil, invalid("client_ref", "reused")
			}
			return prev, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	mt, _, perr := mime.ParseMediaType(f.ContentType)
	if perr != nil || !slices.Contains(fileTypes, mt) {
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
		return nil, errors.New("workorder: file store not wired")
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
		t, err := store.LockWorkTask(ctx, tx, scope, taskID)
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
		if err := store.InsertWorkTaskFile(ctx, tx, scope, &store.WorkTaskFile{
			ID: store.NewID(), TaskID: t.ID, FileID: fileID, Kind: kind, ClientRef: f.ClientRef, CreatedByUserID: p.UserID, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "work_task.file", "work_task", t.ID,
			map[string]any{"content_type": ct, "size_bytes": blob.Size, "kind": kind}); err != nil {
			return err
		}
		return s.taskChanged(ctx, tx, scope, t)
	})
	if err != nil {
		if store.IsUniqueViolation(err) && f.ClientRef != "" {
			if prev, gerr := store.WorkTaskFileByClientRef(ctx, s.d.DB.DB, scope, f.ClientRef); gerr == nil {
				return prev, nil
			}
		}
		return nil, err
	}
	if existing != nil {
		return existing, nil // the deferred cleanup removes the blob just written
	}
	committed = true
	files, err := store.WorkTaskFiles(ctx, s.d.DB.DB, scope, []string{taskID})
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
	files, err := store.WorkTaskFiles(ctx, db, scope, []string{taskID})
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].SHA256 == digest {
			return &files[i], nil
		}
	}
	return nil, nil //nolint:nilnil // no file with these bytes yet
}

// ListFiles lists the files of a task (yard.read, yard.manage, or yard.timesheets on an assigned task).
func (s *Service) ListFiles(ctx context.Context, p *auth.Principal, marinaID, taskID string) ([]TaskFile, error) {
	scope, _, err := s.fileScope(ctx, p, marinaID, taskID, false)
	if err != nil {
		return nil, err
	}
	return store.WorkTaskFiles(ctx, s.d.DB.DB, scope, []string{taskID})
}

func (s *Service) fileSignature(marinaID, taskID, fileID, principalID string, exp int64) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("workorder: key ring not wired")
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
	files, err := s.ListFiles(ctx, p, marinaID, taskID)
	if err != nil {
		return store.MarinaScope{}, nil, err
	}
	scope, _, _ := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardManage, auth.PermYardRead, auth.PermYardTimesheets)
	for i := range files {
		if files[i].FileID == fileID {
			return scope, &files[i], nil
		}
	}
	return store.MarinaScope{}, nil, auth.ErrNotFound
}

// FileLink returns a 5-minute signed link to a task file for the caller only (GET …/yard/tasks/{taskId}/files/
// {fileId}/file?exp=&sig=, served by OpenFile).
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
	u := "/api/admin/v1/marinas/" + scope.ID() + "/yard/tasks/" + taskID + "/files/" + f.FileID + "/file?exp=" +
		strconv.FormatInt(expires.Unix(), 10) + "&sig=" + sig
	return &FileLink{URL: u, ExpiresAt: expires}, nil
}

// OpenFile checks a signed link and returns the decrypted file; a missing, tampered, foreign or expired signature
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
	return &File{Content: content, ContentType: f.ContentType, FileName: "yard-task-file" + ext}, nil
}
