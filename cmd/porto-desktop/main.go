// Command porto-desktop is the desktop edition of Porto (spec 31.2, m6a-plan §2): a full Porto server on SQLite
// bound to 127.0.0.1, shown in a system webview window (build tag "webview", CGO) or in the default browser
// (default build, CGO_ENABLED=0), with the optional local-network listener for office PCs and phones.
//
//	porto-desktop [-data-dir DIR] [-headless] [-browser]
//	porto-desktop -version
//
// Exit codes: 0 ok (also a second launch that brought the running window to the front), 1 runtime error,
// 2 usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/desktop"
	"github.com/coste360/porto/internal/store"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

// uiKind is "browser" in the default build and "window" in the webview build (ui_webview.go).
var uiKind = "browser"

// newWindow builds the window UI; nil in the default build (the panel opens in the browser).
var newWindow func(log *slog.Logger) desktop.UI

// newLAN builds the local-network server (internal/lan); nil until it is wired (lan_wiring.go).
var newLAN desktop.LANFactory

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run parses the flags and returns the exit code: 0 ok, 1 runtime error, 2 usage error.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("porto-desktop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	dataDir := fs.String("data-dir", "", "data folder (default: the per-user application data folder)")
	headless := fs.Bool("headless", false, "no window and no browser (tests)")
	browser := fs.Bool("browser", false, "open the panel in the default browser instead of the window")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "porto-desktop: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "porto-desktop %s (%s)\n", version, uiKind)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, _ := os.UserHomeDir()
	err := desktop.Run(ctx, desktop.Options{
		Version: version, DataDir: *dataDir, Headless: *headless, Browser: *browser,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Home: home, LookupEnv: os.LookupEnv, Stderr: stderr,
		NewWindow: newWindow, CountUsers: countUsers, NewLAN: newLAN,
	})
	if err != nil {
		if errors.Is(err, desktop.ErrSecretMissing) {
			fmt.Fprintln(stderr, "porto-desktop: the secret key of the existing database is missing (see docs/desktop-build.md)")
		} else {
			fmt.Fprintf(stderr, "porto-desktop: %v\n", err)
		}
		return 1
	}
	return 0
}

// countUsers is desktop.UserCounter over the store (internal/desktop does not import it).
func countUsers(ctx context.Context, a *app.App) (int, error) { return store.CountUsers(ctx, a.DB.DB) }
