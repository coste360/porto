//go:build !windows

package desktop

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // G115: a file descriptor fits an int
	if errors.Is(err, unix.EWOULDBLOCK) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("desktop: lock: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // G115: a file descriptor fits an int
}
