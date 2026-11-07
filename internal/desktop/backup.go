package desktop

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/desktopport"
)

// Backup scheduling (m6a-plan §2.8, D14).
const (
	BackupTick     = 15 * time.Minute
	DailyInterval  = 24 * time.Hour
	ExitBackupWait = 60 * time.Second
)

var backupNameRE = regexp.MustCompile(`^porto-(\d{8})-(\d{6})-(daily|exit|manual|pre-restore)\.tar\.gz$`)

var backupKinds = []string{desktopport.BackupDaily, desktopport.BackupExit, desktopport.BackupManual, desktopport.BackupPreRestore}

// BackupName returns the file name of a backup taken at t (formatted in loc) of kind.
func BackupName(t time.Time, loc *time.Location, kind string) string {
	return "porto-" + t.In(loc).Format("20060102-150405") + "-" + kind + ".tar.gz"
}

// ParseBackupName returns the time (in loc) and kind of a backup file name; ok is false for any other name,
// so only Porto's own files are ever listed, restored or pruned.
func ParseBackupName(name string, loc *time.Location) (t time.Time, kind string, ok bool) {
	m := backupNameRE.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, "", false
	}
	t, err := time.ParseInLocation("20060102150405", m[1]+m[2], loc)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, m[3], true
}

// backupEntry is a listed backup file.
type backupEntry struct {
	desktopport.BackupFile
	// Valid: non-empty archive with its .sha256 file (a backup interrupted before the rename never gets here).
	Valid bool
}

// PruneCandidates returns the names to delete (m6a-plan §2.8): beyond the newest keepDaily daily and keepExit
// exit files. manual and pre-restore files are never pruned, and neither is the newest valid file of any kind. It
// is pure.
func PruneCandidates(files []backupEntry, keepDaily, keepExit int) []string {
	var newest *backupEntry
	for i := range files {
		f := &files[i]
		if f.Valid && (newest == nil || f.CreatedAt.After(newest.CreatedAt) ||
			(f.CreatedAt.Equal(newest.CreatedAt) && f.ID > newest.ID)) {
			newest = f
		}
	}
	var out []string
	for kind, keep := range map[string]int{desktopport.BackupDaily: keepDaily, desktopport.BackupExit: keepExit} {
		var of []backupEntry
		for _, f := range files {
			if f.Kind == kind {
				of = append(of, f)
			}
		}
		sortNewestFirst(of)
		for i, f := range of {
			if i < keep || (newest != nil && f.ID == newest.ID) {
				continue
			}
			out = append(out, f.ID)
		}
	}
	slices.Sort(out)
	return out
}

func sortNewestFirst(fs []backupEntry) {
	slices.SortFunc(fs, func(a, b backupEntry) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
}

// Snapshot writes a portable backup archive (internal/backup) of the live database and file store to w.
type Snapshot func(ctx context.Context, w io.Writer) error

// Backups writes, lists and prunes the backup archives and runs the daily schedule.
type Backups struct {
	Paths    Paths
	Clock    clock.Clock
	Loc      *time.Location
	Log      *slog.Logger
	Settings func() FileSettings

	busy    sync.Mutex
	mu      sync.Mutex
	lastErr string
}

// dir returns the folder in use: the configured one when it can be created, else <data>/backups (fallback true).
func (b *Backups) dir() (dir string, fallback bool) {
	def := b.Paths.Backups()
	conf := b.Settings().BackupDir
	if conf != "" && conf != def {
		if err := os.MkdirAll(conf, 0o700); err == nil {
			if st, err := os.Stat(conf); err == nil && st.IsDir() && writable(conf) {
				return conf, false
			}
		}
		_ = os.MkdirAll(def, 0o700)
		return def, true
	}
	_ = os.MkdirAll(def, 0o700)
	return def, false
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".porto-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// list returns the backups of the folder in use, newest first.
func (b *Backups) list() ([]backupEntry, string, bool, error) {
	dir, fallback := b.dir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, dir, fallback, err
	}
	var out []backupEntry
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		t, kind, ok := ParseBackupName(e.Name(), b.loc())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		f := backupEntry{BackupFile: desktopport.BackupFile{
			ID: e.Name(), Kind: kind, SizeBytes: info.Size(), CreatedAt: t.UTC(),
		}}
		if sum, err := readSum(filepath.Join(dir, e.Name()+".sha256")); err == nil {
			f.SHA256 = sum
			f.Valid = info.Size() > 0
		}
		out = append(out, f)
	}
	sortNewestFirst(out)
	return out, dir, fallback, nil
}

func readSum(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: sidecar of a listed backup in the backup folder
	if err != nil {
		return "", err
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, 256)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	if len(sum) != 64 {
		return "", errors.New("desktop: bad checksum file")
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", err
	}
	return sum, nil
}

func (b *Backups) loc() *time.Location {
	if b.Loc == nil {
		return time.Local
	}
	return b.Loc
}

// List returns the backups and the status of the folder.
func (b *Backups) List() ([]desktopport.BackupFile, desktopport.BackupStatus, error) {
	files, dir, fallback, err := b.list()
	st := desktopport.BackupStatus{Dir: dir, Fallback: fallback}
	b.mu.Lock()
	st.LastError = b.lastErr
	b.mu.Unlock()
	if err != nil {
		return nil, st, err
	}
	out := make([]desktopport.BackupFile, 0, len(files))
	for _, f := range files {
		out = append(out, f.BackupFile)
		if f.Kind == desktopport.BackupDaily && f.Valid && f.CreatedAt.After(st.LastDailyAt) {
			st.LastDailyAt = f.CreatedAt
		}
	}
	if st.LastDailyAt.IsZero() {
		st.NextDueAt = b.Clock.Now()
	} else {
		st.NextDueAt = st.LastDailyAt.Add(DailyInterval)
	}
	return out, st, nil
}

// Find returns the path of a listed backup by its id (file name); desktopport.ErrNotFound otherwise.
func (b *Backups) Find(id string) (string, error) {
	if id != filepath.Base(id) {
		return "", desktopport.ErrNotFound
	}
	if _, _, ok := ParseBackupName(id, b.loc()); !ok {
		return "", desktopport.ErrNotFound
	}
	files, dir, _, err := b.list()
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.ID == id && f.Valid {
			return filepath.Join(dir, id), nil
		}
	}
	return "", desktopport.ErrNotFound
}

// TryLock reserves the backup slot (one backup or restore at a time); false when it is taken.
func (b *Backups) TryLock() bool { return b.busy.TryLock() }

// Unlock releases the slot taken by TryLock.
func (b *Backups) Unlock() { b.busy.Unlock() }

// Take writes a backup of kind (desktopport.ErrBackupInProgress when another one runs) and prunes.
func (b *Backups) Take(ctx context.Context, kind string, snap Snapshot) (desktopport.BackupFile, error) {
	if !b.TryLock() {
		return desktopport.BackupFile{}, desktopport.ErrBackupInProgress
	}
	defer b.Unlock()
	return b.TakeLocked(ctx, kind, snap)
}

// WaitIdle waits until no backup or restore runs (or ctx is done), so the database can be closed.
func (b *Backups) WaitIdle(ctx context.Context) {
	for !b.TryLock() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	b.Unlock()
}

// TakeWait is Take that waits (until ctx is done) for a running backup to finish instead of failing: the exit
// backup must not be skipped because the daily one was still running.
func (b *Backups) TakeWait(ctx context.Context, kind string, snap Snapshot) (desktopport.BackupFile, error) {
	for !b.TryLock() {
		select {
		case <-ctx.Done():
			return desktopport.BackupFile{}, desktopport.ErrBackupInProgress
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer b.Unlock()
	return b.TakeLocked(ctx, kind, snap)
}

// TakeLocked is Take for a caller that already holds the slot.
func (b *Backups) TakeLocked(ctx context.Context, kind string, snap Snapshot) (desktopport.BackupFile, error) {
	f, err := b.write(ctx, kind, snap)
	b.mu.Lock()
	if err != nil {
		b.lastErr = "backup_failed"
	} else {
		b.lastErr = ""
	}
	b.mu.Unlock()
	if err != nil {
		if b.Log != nil {
			b.Log.WarnContext(ctx, "desktop backup failed", "kind", kind, "error", err)
		}
		return desktopport.BackupFile{}, err
	}
	b.prune(ctx)
	return f, nil
}

func (b *Backups) write(ctx context.Context, kind string, snap Snapshot) (desktopport.BackupFile, error) {
	if !slices.Contains(backupKinds, kind) {
		return desktopport.BackupFile{}, fmt.Errorf("desktop: unknown backup kind %q", kind)
	}
	dir, _ := b.dir()
	at := b.Clock.Now().Truncate(time.Second)
	name := BackupName(at, b.loc(), kind)
	for i := 0; i < 60; i++ { // two backups in the same second: the next free second
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		at = at.Add(time.Second)
		name = BackupName(at, b.loc(), kind)
	}
	final := filepath.Join(dir, name)
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: our backup folder
	if err != nil {
		return desktopport.BackupFile{}, err
	}
	defer func() { _ = os.Remove(part) }()
	h := sha256.New()
	cw := &countWriter{w: io.MultiWriter(f, h)}
	if err := snap(ctx, cw); err != nil {
		_ = f.Close()
		return desktopport.BackupFile{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return desktopport.BackupFile{}, err
	}
	if err := f.Close(); err != nil {
		return desktopport.BackupFile{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(part, final); err != nil {
		return desktopport.BackupFile{}, err
	}
	if err := writeFileAtomic(final+".sha256", []byte(sum+"  "+name+"\n"), 0o600); err != nil {
		return desktopport.BackupFile{}, err
	}
	return desktopport.BackupFile{ID: name, Kind: kind, SizeBytes: cw.n, CreatedAt: at.UTC(), SHA256: sum}, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func (b *Backups) prune(ctx context.Context) {
	files, dir, _, err := b.list()
	if err != nil {
		return
	}
	s := b.Settings()
	for _, name := range PruneCandidates(files, s.BackupKeepDaily, s.BackupKeepExit) {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && b.Log != nil {
			b.Log.WarnContext(ctx, "desktop backup prune", "error", err)
			continue
		}
		_ = os.Remove(filepath.Join(dir, name+".sha256"))
	}
}

// DailyDue reports whether a daily backup is due: the newest valid daily file of the folder is at least 24 hours
// old (or there is none). Reading the folder makes the schedule survive restarts and sleeps.
func (b *Backups) DailyDue() bool {
	files, _, _, err := b.list()
	if err != nil {
		return true
	}
	now := b.Clock.Now()
	for _, f := range files {
		if f.Kind == desktopport.BackupDaily && f.Valid {
			return now.Sub(f.CreatedAt) >= DailyInterval
		}
	}
	return true
}

// Tick takes the daily backup when due. A failure is kept in the status and retried on the next tick; a running
// backup or restore just skips this tick.
func (b *Backups) Tick(ctx context.Context, snap Snapshot) {
	if !b.DailyDue() {
		return
	}
	if _, err := b.Take(ctx, desktopport.BackupDaily, snap); err != nil && b.Log != nil &&
		!errors.Is(err, desktopport.ErrBackupInProgress) {
		b.Log.DebugContext(ctx, "desktop daily backup will be retried")
	}
}

// RunScheduler calls Tick at start and every BackupTick until ctx is done.
func (b *Backups) RunScheduler(ctx context.Context, snap Snapshot) {
	b.Tick(ctx, snap)
	t := time.NewTicker(BackupTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.Tick(ctx, snap)
		}
	}
}
