package peripheral

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coste360/porto/internal/desktopport"
)

// Transport timeouts (m6b-plan §8.2).
const (
	tcpTimeout     = 5 * time.Second
	defaultRawPort = 9100
)

// ErrInvalidTarget: the target of a printer is not an allowed transport URL.
var ErrInvalidTarget = errors.New("peripheral: invalid target")

var cupsQueue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,127}$`)

// TargetOptions are the environment of a transport.
type TargetOptions struct {
	// Dev allows file:// targets (development, e2e).
	Dev bool
	// GOOS refuses cups:// on windows.
	GOOS string
	// Exec runs a command with stdin (tests); nil = os/exec.
	Exec func(ctx context.Context, stdin []byte, name string, args ...string) error
}

// ParseTarget validates a transport URL and returns its Transport:
//
//   - tcp://host[:port] (raw, default port 9100): host must be a private or loopback IP or a .local name;
//   - cups://queue: `lp -d queue -o raw` (macOS, Linux; refused on windows);
//   - file:///absolute/path: append the bytes to a file (development only).
func ParseTarget(target string, o TargetOptions) (desktopport.Transport, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrInvalidTarget
	}
	switch u.Scheme {
	case "tcp":
		host, port := u.Hostname(), defaultRawPort
		if p := u.Port(); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				return nil, ErrInvalidTarget
			}
			port = n
		}
		if (u.Path != "" && u.Path != "/") || !allowedHost(host) {
			return nil, ErrInvalidTarget
		}
		return tcpTransport{addr: net.JoinHostPort(host, strconv.Itoa(port))}, nil
	case "cups":
		if o.GOOS == "windows" || !cupsQueue.MatchString(u.Host) || (u.Path != "" && u.Path != "/") {
			return nil, ErrInvalidTarget
		}
		run := o.Exec
		if run == nil {
			run = execRun
		}
		return cupsTransport{queue: u.Host, run: run}, nil
	case "file":
		if !o.Dev || u.Host != "" || !filepath.IsAbs(filepath.FromSlash(u.Path)) {
			return nil, ErrInvalidTarget
		}
		return FileTransport{Path: filepath.FromSlash(u.Path)}, nil
	default:
		return nil, ErrInvalidTarget
	}
}

// allowedHost accepts private and loopback addresses and .local names: a printer is on the office network,
// never on the internet (a desktop setting must not become a way to send bytes anywhere).
func allowedHost(host string) bool {
	if host == "" {
		return false
	}
	if a, err := netip.ParseAddr(host); err == nil {
		a = a.Unmap()
		return a.IsPrivate() || a.IsLoopback()
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if !strings.HasSuffix(h, ".local") || len(h) <= len(".local") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

type tcpTransport struct{ addr string }

func (t tcpTransport) Write(ctx context.Context, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, tcpTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return fmt.Errorf("%w: %w", desktopport.ErrPeripheralUnavailable, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(b); err != nil {
		return fmt.Errorf("%w: %w", desktopport.ErrPeripheralUnavailable, err)
	}
	return nil
}

type cupsTransport struct {
	queue string
	run   func(ctx context.Context, stdin []byte, name string, args ...string) error
}

func (t cupsTransport) Write(ctx context.Context, b []byte) error {
	if err := t.run(ctx, b, "lp", "-d", t.queue, "-o", "raw"); err != nil {
		return fmt.Errorf("%w: %w", desktopport.ErrPeripheralUnavailable, err)
	}
	return nil
}

func execRun(ctx context.Context, stdin []byte, name string, args ...string) error {
	cctx, cancel := context.WithTimeout(ctx, tcpTimeout*2)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...) //nolint:gosec // G204: fixed command, validated queue name
	cmd.Stdin = bytes.NewReader(stdin)
	return cmd.Run()
}

// FileTransport appends the bytes to a file (development only).
type FileTransport struct{ Path string }

// Write implements desktopport.Transport.
func (t FileTransport) Write(_ context.Context, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(t.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
