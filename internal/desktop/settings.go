package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/coste360/porto/internal/desktopport"
)

// SettingsVersion is the format version of desktop.json.
const SettingsVersion = 1

// Defaults and bounds of desktop.json (m6a-plan §2.4).
const (
	DefaultLANPort         = 8443
	DefaultBackupKeepDaily = 14
	DefaultBackupKeepExit  = 5
	maxBackupKeepDaily     = 365
	maxBackupKeepExit      = 50
	maxDismissedUpdate     = 64
)

// FileSettings is desktop.json. Unknown fields are kept in extra and written back, so a newer version's settings
// survive a run of an older one.
type FileSettings struct {
	Version         int              `json:"version"`
	Mode            desktopport.Mode `json:"mode"`
	Port            int              `json:"port"`
	LANEnabled      bool             `json:"lan_enabled"`
	LANPort         int              `json:"lan_port"`
	LANAddress      string           `json:"lan_address"`
	BackupDir       string           `json:"backup_dir"`
	BackupKeepDaily int              `json:"backup_keep_daily"`
	BackupKeepExit  int              `json:"backup_keep_exit"`
	UpdateCheck     bool             `json:"update_check"`
	DismissedUpdate string           `json:"dismissed_update"`
	// KeepRunning is reserved (tray, keep running when the window is closed); ignored.
	KeepRunning bool `json:"keep_running"`
	// M6b (m6b-plan §7, §8): kiosk, peripherals, the remote instance (remote mode) and the app PIN hash (Argon2id,
	// set when the app is paired; it protects the local administration in remote mode). The device token is not
	// here: it lives in the SecretStore.
	Kiosk       KioskFile       `json:"kiosk"`
	Peripherals PeripheralsFile `json:"peripherals"`
	Remote      *RemoteFile     `json:"remote,omitempty"`
	AppPinHash  string          `json:"app_pin_hash,omitempty"`

	extra map[string]json.RawMessage
}

// DefaultSettings returns the settings of a new installation.
func DefaultSettings() FileSettings {
	return FileSettings{
		Version: SettingsVersion, Mode: desktopport.ModeLocal, LANPort: DefaultLANPort,
		BackupKeepDaily: DefaultBackupKeepDaily, BackupKeepExit: DefaultBackupKeepExit, UpdateCheck: true,
	}
}

var knownSettingsFields = []string{
	"version", "mode", "port", "lan_enabled", "lan_port", "lan_address", "backup_dir", "backup_keep_daily",
	"backup_keep_exit", "update_check", "dismissed_update", "keep_running", "kiosk", "peripherals", "remote", "app_pin_hash",
}

// UnmarshalJSON reads the known fields over the defaults and keeps the others.
func (s *FileSettings) UnmarshalJSON(b []byte) error {
	type plain FileSettings
	p := plain(DefaultSettings())
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for _, k := range knownSettingsFields {
		delete(all, k)
	}
	*s = FileSettings(p)
	if len(all) > 0 {
		s.extra = all
	}
	return nil
}

// MarshalJSON writes the known fields and the preserved unknown ones.
func (s FileSettings) MarshalJSON() ([]byte, error) {
	type plain FileSettings
	b, err := json.Marshal(plain(s))
	if err != nil || len(s.extra) == 0 {
		return b, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	for k, v := range s.extra {
		if _, known := all[k]; !known {
			all[k] = v
		}
	}
	return json.Marshal(all)
}

// Validate reports the invalid fields (names only). The remote mode needs the remote instance and the app PIN.
func (s *FileSettings) Validate() error {
	var bad []string
	switch s.Mode {
	case desktopport.ModeLocal:
	case desktopport.ModeRemote:
		if s.Remote == nil || s.AppPinHash == "" {
			bad = append(bad, "mode")
		}
	default:
		bad = append(bad, "mode")
	}
	if s.Remote != nil {
		bad = append(bad, s.Remote.invalid()...)
	}
	bad = append(bad, s.Peripherals.invalid()...)
	if s.Port < 0 || s.Port > 65535 {
		bad = append(bad, "port")
	}
	if s.LANPort < 1 || s.LANPort > 65535 {
		bad = append(bad, "lan_port")
	}
	if s.BackupDir != "" && !filepath.IsAbs(s.BackupDir) {
		bad = append(bad, "backup_dir")
	}
	if s.BackupKeepDaily < 1 || s.BackupKeepDaily > maxBackupKeepDaily {
		bad = append(bad, "backup_keep_daily")
	}
	if s.BackupKeepExit < 1 || s.BackupKeepExit > maxBackupKeepExit {
		bad = append(bad, "backup_keep_exit")
	}
	if len(s.DismissedUpdate) > maxDismissedUpdate {
		bad = append(bad, "dismissed_update")
	}
	if len(bad) > 0 {
		return &desktopport.SettingsError{Fields: bad}
	}
	return nil
}

// Apply returns s with the patch applied, validated.
func (s FileSettings) Apply(p desktopport.SettingsPatch) (FileSettings, []string, error) {
	var changed []string
	if p.BackupDir != nil {
		s.BackupDir, changed = filepath.Clean(*p.BackupDir), append(changed, "backup_dir")
		if *p.BackupDir == "" {
			s.BackupDir = ""
		}
	}
	if p.BackupKeepDaily != nil {
		s.BackupKeepDaily, changed = *p.BackupKeepDaily, append(changed, "backup_keep_daily")
	}
	if p.BackupKeepExit != nil {
		s.BackupKeepExit, changed = *p.BackupKeepExit, append(changed, "backup_keep_exit")
	}
	if p.UpdateCheck != nil {
		s.UpdateCheck, changed = *p.UpdateCheck, append(changed, "update_check")
	}
	if p.DismissedUpdate != nil {
		s.DismissedUpdate, changed = *p.DismissedUpdate, append(changed, "dismissed_update")
	}
	if err := s.Validate(); err != nil {
		return s, nil, err
	}
	return s, changed, nil
}

// LoadSettings reads desktop.json; a missing file gives the defaults. An invalid file is an error (the launcher
// never overwrites settings it cannot read).
func LoadSettings(path string) (FileSettings, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: fixed file inside the data folder
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return FileSettings{}, fmt.Errorf("desktop: read settings: %w", err)
	}
	var s FileSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return FileSettings{}, fmt.Errorf("desktop: settings file is not valid JSON: %w", err)
	}
	if err := s.Validate(); err != nil {
		return FileSettings{}, err
	}
	return s, nil
}

// SaveSettings writes desktop.json atomically (temporary file and rename), mode 0600.
func SaveSettings(path string, s FileSettings) error {
	s.Version = SettingsVersion
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o600)
}

// writeFileAtomic writes data to path through a temporary file in the same folder and a rename.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// view turns FileSettings into the desktopport view.
func (s FileSettings) view(p Paths) desktopport.Settings {
	return desktopport.Settings{
		Mode: s.Mode, Port: s.Port, LANEnabled: s.LANEnabled, LANPort: s.LANPort, LANAddress: s.LANAddress,
		BackupDir: s.BackupDir, BackupKeepDaily: s.BackupKeepDaily, BackupKeepExit: s.BackupKeepExit,
		UpdateCheck: s.UpdateCheck, DismissedUpdate: s.DismissedUpdate, DataDir: p.Root, LogsDir: p.Logs(),
		Kiosk: desktopport.KioskSettings{Enabled: s.Kiosk.Enabled}, Peripherals: s.Peripherals.Port(),
		Remote: s.Remote.Port(),
	}
}
