package desktop

import (
	"errors"
	"fmt"
	"os"
)

// ErrAlreadyRunning: another launcher holds the lock of the data folder.
var ErrAlreadyRunning = errors.New("desktop: another instance is running on this data folder")

// Lock is the single-instance lock of a data folder (m6a-plan §2.7). The OS releases it when the process exits,
// so a stale lock file after a crash never blocks a new start.
type Lock struct {
	f *os.File
}

// AcquireLock takes the lock at path or returns ErrAlreadyRunning.
func AcquireLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // G304: fixed file in the data folder
	if err != nil {
		return nil, fmt.Errorf("desktop: open lock: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release frees the lock (idempotent).
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockFile(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}
