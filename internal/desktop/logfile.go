package desktop

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Launcher log (m6a-plan §2.9): <data>/logs/porto-desktop.log, at most logLimit bytes per file and logKeep files
// in total (the live one plus logKeep-1 numbered backups, .1 being the newest).
const (
	LogFileName = "porto-desktop.log"
	logLimit    = 10 << 20
	logKeep     = 5
)

// countedFile is an append-only file that remembers how many bytes it holds.
type countedFile struct {
	file  *os.File
	bytes int64
}

func openCounted(path string) (*countedFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &countedFile{file: file, bytes: end}, nil
}

func (c *countedFile) Write(p []byte) (int, error) {
	n, err := c.file.Write(p)
	c.bytes += int64(n)
	return n, err
}

// shiftBackups makes room for a new backup of live: the oldest numbered backup is dropped and every other one
// moves up by one, then live becomes .1. With keep == 1 there are no backups and live is simply removed.
func shiftBackups(live string, keep int) error {
	name := func(i int) string {
		if i == 0 {
			return live
		}
		return live + "." + strconv.Itoa(i)
	}
	if err := os.Remove(name(keep - 1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for i := keep - 1; i > 0; i-- {
		if err := os.Rename(name(i-1), name(i)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// LogWriter writes log records to the launcher log, starting a new file when the next record would push the
// current one past its limit. One Write is one record and always lands in a single file. Safe for concurrent use.
type LogWriter struct {
	live  string
	limit int64
	keep  int

	mu  sync.Mutex
	cur *countedFile
}

// OpenLogWriter appends to dir/porto-desktop.log (dir is created 0700, files 0600). Non-positive limit or keep
// select the defaults.
func OpenLogWriter(dir string, limit int64, keep int) (*LogWriter, error) {
	if limit <= 0 {
		limit = logLimit
	}
	if keep <= 0 {
		keep = logKeep
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &LogWriter{live: filepath.Join(dir, LogFileName), limit: limit, keep: keep}
	cur, err := openCounted(w.live)
	if err != nil {
		return nil, err
	}
	w.cur = cur
	return w, nil
}

// Write implements io.Writer.
func (w *LogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur == nil {
		return 0, fs.ErrClosed
	}
	// An empty file always takes the record, even an oversized one, so a huge record cannot loop.
	if w.cur.bytes != 0 && w.cur.bytes+int64(len(p)) > w.limit {
		if err := w.roll(); err != nil {
			return 0, err
		}
	}
	return w.cur.Write(p)
}

// roll closes the live file, shifts the backups and opens a fresh live file.
func (w *LogWriter) roll() error {
	old := w.cur
	w.cur = nil
	if err := old.file.Close(); err != nil {
		return err
	}
	if err := shiftBackups(w.live, w.keep); err != nil {
		return err
	}
	cur, err := openCounted(w.live)
	if err != nil {
		return err
	}
	w.cur = cur
	return nil
}

// Close closes the live file; later writes fail with fs.ErrClosed.
func (w *LogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur == nil {
		return nil
	}
	err := w.cur.file.Close()
	w.cur = nil
	return err
}
