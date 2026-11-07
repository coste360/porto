package desktop

import (
	"errors"
	"path/filepath"
	"strings"
)

// AppDirName is the folder name of the desktop data under the per-user application data folder.
const AppDirName = "Porto"

// ErrNoDataDir is returned when no per-user data folder can be determined (no home folder).
var ErrNoDataDir = errors.New("desktop: cannot determine the data folder")

// DataDir returns the default data folder of the desktop edition for goos (m6a-plan §2.3). env reads the
// environment (os.Getenv); home is the user's home folder. PORTO_DATA_DIR overrides the default; the -data-dir
// flag, handled by the caller, overrides both. The result is cleaned but not created.
//
//   - darwin:  ~/Library/Application Support/Porto
//   - windows: %LOCALAPPDATA%\Porto (not the roaming profile: a SQLite file must not roam)
//   - others:  $XDG_DATA_HOME/porto, else ~/.local/share/porto
func DataDir(goos string, env func(string) string, home string) (string, error) {
	if v := env("PORTO_DATA_DIR"); v != "" {
		return filepath.Clean(v), nil
	}
	join := filepath.Join
	if goos == "windows" {
		// Joined with backslashes so the result is the same on any host (tests run the Windows case everywhere).
		join = func(elem ...string) string { return strings.Join(elem, `\`) }
	}
	switch goos {
	case "darwin":
		if home == "" {
			return "", ErrNoDataDir
		}
		return join(home, "Library", "Application Support", AppDirName), nil
	case "windows":
		if v := env("LOCALAPPDATA"); v != "" {
			return join(v, AppDirName), nil
		}
		if home == "" {
			return "", ErrNoDataDir
		}
		return join(home, "AppData", "Local", AppDirName), nil
	default:
		if v := env("XDG_DATA_HOME"); v != "" && filepath.IsAbs(v) {
			return join(v, "porto"), nil
		}
		if home == "" {
			return "", ErrNoDataDir
		}
		return join(home, ".local", "share", "porto"), nil
	}
}

// Paths are the files and folders inside the data folder.
type Paths struct {
	Root string
}

// DB is the SQLite database.
func (p Paths) DB() string { return filepath.Join(p.Root, "porto.db") }

// Files is the file store folder.
func (p Paths) Files() string { return filepath.Join(p.Root, "files") }

// Settings is desktop.json.
func (p Paths) Settings() string { return filepath.Join(p.Root, "desktop.json") }

// Instance is instance.json (pid, port, token hash of the running instance).
func (p Paths) Instance() string { return filepath.Join(p.Root, "instance.json") }

// InstanceToken holds the raw activation token of the running instance (0600).
func (p Paths) InstanceToken() string { return filepath.Join(p.Root, "instance.token") }

// Lock is the single-instance lock file.
func (p Paths) Lock() string { return filepath.Join(p.Root, "porto-desktop.lock") }

// LAN is the folder of the local-network certificates.
func (p Paths) LAN() string { return filepath.Join(p.Root, "lan") }

// Backups is the default backup folder.
func (p Paths) Backups() string { return filepath.Join(p.Root, "backups") }

// Logs is the log folder.
func (p Paths) Logs() string { return filepath.Join(p.Root, "logs") }

// Mail is the folder of the file mail transport.
func (p Paths) Mail() string { return filepath.Join(p.Root, "mail") }

// SecretFile is the file fallback of the secret key.
func (p Paths) SecretFile() string { return filepath.Join(p.Root, "secret.key") }

// FirstRunURL is the dev-only file with the first-run URL (e2e).
func (p Paths) FirstRunURL() string { return filepath.Join(p.Root, "first-run.url") }

// RestoreStaging is the folder where a restore prepares the new database before the swap.
func (p Paths) RestoreStaging() string { return filepath.Join(p.Root, "restore-staging") }
