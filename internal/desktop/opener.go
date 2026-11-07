package desktop

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"time"
)

// Opener opens a fixed folder or an URL with the OS (Finder, Explorer, the file manager, the default browser).
type Opener func(ctx context.Context, target string) error

// SystemOpener returns the OS opener of goos: open (macOS), explorer (Windows), xdg-open (others). The target is
// passed as one argument, never through a shell.
func SystemOpener(goos string) Opener {
	return func(ctx context.Context, target string) error {
		if target == "" {
			return errors.New("desktop: empty open target")
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var cmd *exec.Cmd
		switch goos {
		case "darwin":
			cmd = exec.CommandContext(ctx, "open", target) //nolint:gosec // G204: fixed program, one argument
		case "windows":
			// explorer exits 1 even on success: start it and do not wait for it.
			c := exec.Command("explorer", target) //nolint:gosec,noctx // G204: fixed program, one argument
			if err := c.Start(); err != nil {
				return err
			}
			go func() { _ = c.Wait() }()
			return nil
		default:
			cmd = exec.CommandContext(ctx, "xdg-open", target) //nolint:gosec // G204: fixed program, one argument
		}
		return cmd.Run()
	}
}

// ExternalURLAllowed reports whether the window may hand u to the default browser: https and mailto only.
func ExternalURLAllowed(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch p.Scheme {
	case "https":
		return p.Host != "" && p.User == nil
	case "mailto":
		return p.Opaque != ""
	}
	return false
}
