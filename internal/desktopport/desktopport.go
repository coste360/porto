// Package desktopport holds the types and interfaces shared by the desktop edition (m6a-plan §3.1): the launcher
// (internal/desktop) implements Runtime, the local-network server (internal/lan) implements LANController, and the
// HTTP layer (internal/http/desktop) and the composition root (internal/app) only see these interfaces.
//
// The package carries no logic and imports the standard library and internal/domain/auth only (depguard).
package desktopport

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
)

// Mode is how the desktop edition runs.
type Mode string

// Modes. Only ModeLocal exists in M6a; ModeRemote (the desktop as a client of an online instance, one token per
// device) is reserved for M6b and rejected by the settings validation.
const (
	ModeLocal  Mode = "local"
	ModeRemote Mode = "remote"
)

// Secret store kinds reported in Status.
const (
	SecretStoreKeyring = "keyring"
	SecretStoreFile    = "file"
)

// Backup kinds (file name suffix, m6a-plan §2.8).
const (
	BackupDaily      = "daily"
	BackupExit       = "exit"
	BackupManual     = "manual"
	BackupPreRestore = "pre-restore"
)

// Open targets of Runtime.Open: fixed folders, never a client-supplied path.
const (
	OpenDataDir   = "data_dir"
	OpenBackupDir = "backup_dir"
	OpenLogs      = "logs"
)

// Errors returned by Runtime and LANController; mapped to problem types in internal/http/respond.
var (
	// ErrFirstRunDone: the instance already has users (409 first_run_done).
	ErrFirstRunDone = errors.New("desktop: first run already completed")
	// ErrFirstRunToken: the first-run token is missing, wrong or already used (400 first_run_token_invalid).
	ErrFirstRunToken = errors.New("desktop: first-run token invalid")
	// ErrLANPortUnavailable: the local-network port cannot be bound (409 lan_port_unavailable).
	ErrLANPortUnavailable = errors.New("desktop: local network port unavailable")
	// ErrLANNoAddress: no private address to listen on (409 lan_no_address).
	ErrLANNoAddress = errors.New("desktop: no local network address")
	// ErrBackupInProgress: another backup or restore is running (409 backup_in_progress).
	ErrBackupInProgress = errors.New("desktop: backup in progress")
	// ErrRestoreFailed: the archive cannot be restored; the live database is untouched (422 restore_failed).
	ErrRestoreFailed = errors.New("desktop: restore failed")
	// ErrRestoreBackupNewer: the archive comes from a newer schema than this app knows; update the app first (422
	// restore_backup_newer). The live database is untouched.
	ErrRestoreBackupNewer = errors.New("desktop: backup made by a newer version")
	// ErrInvalidSettings: a settings patch is invalid; use *SettingsError for the fields (400 validation_failed).
	ErrInvalidSettings = errors.New("desktop: invalid settings")
	// ErrInstanceToken: the activation token of a second launch is wrong (401 unauthorized).
	ErrInstanceToken = errors.New("desktop: instance token invalid")
	// ErrNotFound: unknown backup or target (404 not_found).
	ErrNotFound = errors.New("desktop: not found")
)

// SettingsError lists the invalid fields of a settings patch (names only, never values). It matches
// ErrInvalidSettings with errors.Is.
type SettingsError struct {
	Fields []string
}

func (e *SettingsError) Error() string {
	return "desktop: invalid settings: " + strings.Join(e.Fields, ", ")
}

// Is makes errors.Is(err, ErrInvalidSettings) true.
func (e *SettingsError) Is(target error) bool { return target == ErrInvalidSettings }

// UpdateInfo is a newer version found by the version check (never downloaded or installed).
type UpdateInfo struct {
	Version string
	URL     string
}

// Status is what the loopback window learns without authentication (GET /api/desktop/v1/status).
type Status struct {
	Version         string
	Mode            Mode
	FirstRunPending bool
	// Update is nil when the check is off, failed or found nothing newer (or the version was dismissed).
	Update *UpdateInfo
	// SecretStore is SecretStoreKeyring or SecretStoreFile (the panel warns about the file fallback).
	SecretStore string
}

// FirstRunInput creates the first owner from the window (m6a-plan §2.6). Token is the single-use first-run token.
type FirstRunInput struct {
	Token        string
	Organization string
	Name         string
	Email        string
	Password     string
	UpdateCheck  bool
}

// FirstRunResult identifies the owner created by FirstRun and the session to set as cookies.
type FirstRunResult struct {
	UserID       string
	SessionToken string
	ExpiresAt    time.Time
}

// Settings are the desktop settings of desktop.json without secrets (m6a-plan §2.4).
type Settings struct {
	Mode            Mode
	Port            int
	LANEnabled      bool
	LANPort         int
	LANAddress      string // "" = automatic
	BackupDir       string // "" = <data>/backups
	BackupKeepDaily int
	BackupKeepExit  int
	UpdateCheck     bool
	DismissedUpdate string
	DataDir         string // read only
	LogsDir         string // read only
}

// SettingsPatch changes some settings; nil fields stay as they are. The local network is switched with
// LANController (PUT /lan), not here.
type SettingsPatch struct {
	BackupDir       *string
	BackupKeepDaily *int
	BackupKeepExit  *int
	UpdateCheck     *bool
	DismissedUpdate *string
}

// BackupFile is one archive of the backup folder. ID is the file name.
type BackupFile struct {
	ID        string
	Kind      string
	SizeBytes int64
	CreatedAt time.Time
	SHA256    string
}

// BackupStatus describes the scheduler and the folder in use.
type BackupStatus struct {
	Dir         string
	Fallback    bool // the configured folder is unavailable: backups go to <data>/backups
	LastDailyAt time.Time
	LastError   string // a short, PII-free message
	NextDueAt   time.Time
}

// RestoreSource is a listed backup (BackupID) or an uploaded archive (Upload, streamed, limit MaxRestoreUpload).
type RestoreSource struct {
	BackupID string
	Upload   io.Reader
}

// MaxRestoreUpload bounds an uploaded restore archive (streamed to disk, never held in memory).
const MaxRestoreUpload int64 = 4 << 30

// Runtime is the desktop launcher as seen by the HTTP layer. Every method that takes a principal checks
// Principal.IsOwnerAnywhere first (auth.ErrForbidden otherwise) and audits with the caller's first owned
// organization and no marina. The HTTP layer reaches it only on the loopback listener from a loopback peer.
type Runtime interface {
	// Status needs no authentication: version, first run pending, update, secret store kind.
	Status(ctx context.Context) Status
	// FirstRun creates the first owner and a session; ErrFirstRunToken or ErrFirstRunDone.
	FirstRun(ctx context.Context, in FirstRunInput) (*FirstRunResult, error)
	// FirstRunRestore restores an archive instead of creating the owner on the first run: ErrFirstRunDone once an
	// owner exists, ErrFirstRunToken on a wrong or used token; the token is burnt only when the restore succeeds.
	FirstRunRestore(ctx context.Context, token string, src RestoreSource) error
	Settings(ctx context.Context, p *auth.Principal) (Settings, error)
	UpdateSettings(ctx context.Context, p *auth.Principal, in SettingsPatch) (Settings, error)
	LAN() LANController
	Backups(ctx context.Context, p *auth.Principal) ([]BackupFile, BackupStatus, error)
	BackupNow(ctx context.Context, p *auth.Principal) (BackupFile, error)
	// Restore validates the archive, takes a pre-restore backup, prepares the restored database and returns after
	// scheduling the restart (the launcher swaps the database). p is nil on the first-run restore (called by
	// FirstRunRestore after checking the first-run token).
	Restore(ctx context.Context, p *auth.Principal, src RestoreSource) error
	// RevealSecretKey returns the secret key once after checking the password (rate limited, audited).
	RevealSecretKey(ctx context.Context, p *auth.Principal, password string) (string, error)
	// Activate brings the window to the front for a second launch; ErrInstanceToken.
	Activate(ctx context.Context, instanceToken string) error
	// Open opens one of the fixed folders (OpenDataDir, OpenBackupDir, OpenLogs) with the OS opener.
	Open(ctx context.Context, p *auth.Principal, target string) error
}

// LANStatus describes the local-network listener (m6a-plan §2.10).
type LANStatus struct {
	Enabled   bool
	Listening bool
	// URL is https://<address>:<port> while listening, else "".
	URL       string
	Address   string   // address in use
	Addresses []string // private addresses available
	Port      int
	// Fingerprint is the SHA-256 of the leaf certificate DER, lowercase hex ("" before the first enable).
	Fingerprint   string
	CAFingerprint string
	CertExpiresAt time.Time
	Error         string // last enable error slug (lan_port_unavailable, lan_no_address) or ""
}

// LANOptions are the choices of PUT /lan; zero values keep the stored settings.
type LANOptions struct {
	Port    int
	Address string
}

// LANController starts and stops the local-network listener. The listener exists only while enabled (INV-79).
type LANController interface {
	Status() LANStatus
	// Enable starts the listener; ErrLANPortUnavailable or ErrLANNoAddress (the setting stays off).
	Enable(ctx context.Context, o LANOptions) (LANStatus, error)
	Disable(ctx context.Context) error
	// RotateCertificate re-issues the leaf (all=false) or the CA and the leaf (all=true).
	RotateCertificate(ctx context.Context, all bool) (LANStatus, error)
}

// PairTarget is where a pairing QR points (used by device.Service): Origin is the scheme://host[:port] phones
// open, Fingerprint the leaf fingerprint (base64url, "" outside the local network), Reachable false when phones
// cannot reach Origin (desktop with the local network off).
type PairTarget struct {
	Origin      string
	Fingerprint string
	Reachable   bool
}
