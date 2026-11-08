// Package desktopport holds the types and interfaces shared by the desktop edition (m6a-plan §3.1): the launcher
// (internal/desktop) implements Runtime, the local-network server (internal/lan) implements LANController, and the
// HTTP layer (internal/http/desktop) and the composition root (internal/app) only see these interfaces.
//
// M6b adds the remote mode (the app as a client of an online instance, m6b-plan §7), the kiosk and the local
// peripherals behind interfaces (peripheral.go, m6b-plan §8).
//
// The package carries no logic and imports the standard library, internal/domain/auth and pkg/ext only (depguard).
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

// Modes. ModeLocal (M6a) runs the server in the app; ModeRemote (M6b) makes the app a client of an online instance
// with one device token per app, kept in the SecretStore and never shown to the page.
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

	// M6b (m6b-plan §7, §8).

	// ErrRemoteUnreachable: the online instance of the remote mode cannot be reached (503 remote_unreachable).
	ErrRemoteUnreachable = errors.New("desktop: remote instance unreachable")
	// ErrRemotePinMismatch: the online instance presented a certificate that is neither the pinned leaf nor issued
	// by the pinned CA (502 remote_pin_mismatch). Use *PinMismatchError for the fingerprints.
	ErrRemotePinMismatch = errors.New("desktop: remote certificate does not match the pin")
	// ErrAppPinInvalid: the app PIN is missing or wrong (401 app_pin_invalid); rate limited (429 rate_limited).
	ErrAppPinInvalid = errors.New("desktop: app PIN invalid")
	// ErrPeripheralUnavailable: the peripheral is not configured or not ready (503 peripheral_unavailable).
	ErrPeripheralUnavailable = errors.New("desktop: peripheral unavailable")
	// ErrWrongMode: the operation does not exist in the current mode (local vs remote) (404 not_found).
	ErrWrongMode = errors.New("desktop: not available in this mode")
	// ErrAlreadyPaired: the app is already in remote mode, or the local instance already has users (409
	// first_run_done).
	ErrAlreadyPaired = errors.New("desktop: already paired")
)

// PinMismatchError carries the expected and presented fingerprints (lowercase hex SHA-256, no secrets). It matches
// ErrRemotePinMismatch with errors.Is.
type PinMismatchError struct {
	Expected  string
	Presented string
}

func (e *PinMismatchError) Error() string { return ErrRemotePinMismatch.Error() }

// Is makes errors.Is(err, ErrRemotePinMismatch) true.
func (e *PinMismatchError) Is(target error) bool { return target == ErrRemotePinMismatch }

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
	// Kiosk is true when the window runs in kiosk mode (M6b).
	Kiosk bool
	// Remote is set in remote mode only (M6b).
	Remote *RemoteStatus
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
	// M6b: kiosk, peripherals and (remote mode only) the remote instance.
	Kiosk       KioskSettings
	Peripherals PeripheralSettings
	Remote      *RemoteSettings
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

	// M6b (m6b-plan §7, §8). Methods taking AdminAuth accept the owner principal in local mode and the app PIN in
	// remote mode (ErrAppPinInvalid, rate limited with *auth.RateLimitError).

	// Peripherals returns the configured drivers; a field is nil when its driver is "none" (Fiscal: no local
	// fiscal device).
	Peripherals() Peripherals
	// PeripheralSettings reads the peripheral settings (local mode: owner; remote mode: any proxy user, a.AppPin
	// empty).
	PeripheralSettings(ctx context.Context, a AdminAuth) (PeripheralSettings, error)
	// UpdatePeripheralSettings validates (*SettingsError) and stores the settings, then reloads the drivers.
	UpdatePeripheralSettings(ctx context.Context, a AdminAuth, in PeripheralSettings) (PeripheralSettings, error)
	// SetKiosk stores the kiosk setting, applied from the next launch.
	SetKiosk(ctx context.Context, a AdminAuth, enabled bool) (KioskSettings, error)
	// ExitKiosk leaves kiosk mode until the next launch: local mode checks the owner credentials (any signed-in
	// principal in a.Principal plus owner email and password in cred), remote mode the app PIN.
	ExitKiosk(ctx context.Context, a AdminAuth, cred KioskExitCredentials) error
	// PairRemote pairs the app to an online instance on the first run and schedules the restart in remote mode:
	// ErrFirstRunToken, ErrAlreadyPaired, ErrRemoteUnreachable, ErrRemotePinMismatch, *SettingsError.
	PairRemote(ctx context.Context, in RemotePairInput) error
	// UnpairRemote revokes the device upstream (best effort), deletes the token, the shell cache and the remote
	// settings, and schedules the restart on the first run. ErrWrongMode in local mode.
	UnpairRemote(ctx context.Context, a AdminAuth) error
}

// AdminAuth authorises a local administration call of the desktop app (M6b §7.5): Principal (an owner) in local
// mode, AppPin in remote mode where no local users exist. The PIN is never logged or audited.
type AdminAuth struct {
	Principal *auth.Principal
	AppPin    string
}

// KioskExitCredentials are the owner credentials that leave kiosk mode in local mode (never logged).
type KioskExitCredentials struct {
	Email    string
	Password string
}

// KioskSettings is desktop.json kiosk (M6b §7.6).
type KioskSettings struct {
	Enabled bool
}

// RemoteSettings is desktop.json remote (M6b §7.1). The device token is not here: it lives in the SecretStore.
type RemoteSettings struct {
	URL        string
	DeviceID   string
	MarinaID   string
	MarinaName string
	// LeafFP and CAFP are lowercase hex SHA-256 fingerprints of the pinned leaf and of its issuing CA ("" when the
	// target uses a public CA and system roots).
	LeafFP   string
	CAFP     string
	PairedAt time.Time
}

// RemoteStatus is the remote part of Status (M6b).
type RemoteStatus struct {
	URL        string
	MarinaID   string
	MarinaName string
	Reachable  bool
	PairedAt   time.Time
	// VersionMismatch is true when the remote major version differs from the app's (banner "aggiorna l'app").
	VersionMismatch bool
}

// RemotePairInput is POST /api/desktop/v1/remote/pair: the porto://pair link, or URL + code (+ fingerprint), the
// first-run token and the new app PIN (6 to 12 digits).
type RemotePairInput struct {
	FirstRunToken string
	Link          string
	URL           string
	Code          string
	Fingerprint   string // base64url leaf fingerprint of the pairing payload
	Pin           string
	Name          string
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
	// CAConstrained is true when the local CA carries the M6b name constraints (private addresses, .local); the
	// CA download for phones is offered only then (M6b §8.5).
	CAConstrained bool
	// CADownloadURL is https://<address>:<port>/lan/porto-ca.crt while listening with a constrained CA, else "".
	CADownloadURL string
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
