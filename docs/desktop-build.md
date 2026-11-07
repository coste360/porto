# Desktop edition: build, packaging and operation

The desktop edition (`cmd/porto-desktop`, spec 31.2, `docs/m6a-plan.md`, ADR 0010) is a full Porto server on
SQLite bound to `127.0.0.1`, shown in the system webview (WKWebView on macOS, WebView2 on Windows, WebKitGTK 4.1
on Linux) or in the default browser. It works offline: no Coste360 service is needed (the version check is
optional and can be switched off).

## Two builds of the launcher

| Build | Command | CGO | UI |
|---|---|---|---|
| browser (default) | `make desktop-build` | no | the panel opens in the default browser |
| window | `make desktop-build DESKTOP_TAGS=webview` | yes, native toolchain | the panel in the app window |

`porto-desktop -version` prints `porto-desktop <version> (browser|window)`. The main binary `bin/porto` and
`make check` never need CGO or the webview toolchain: only the files of `cmd/porto-desktop` with the `webview`
build tag import `github.com/webview/webview_go`.

Window build prerequisites:

- macOS: Xcode command line tools (`xcode-select --install`).
- Windows: MSYS2 / Git Bash with MinGW-w64 gcc and binutils (`windres`); the WebView2 runtime on the target PC
  (preinstalled on Windows 10/11; without it the launcher opens the browser).
- Linux: `libwebkit2gtk-4.1-dev`, `libgtk-3-dev`, `pkg-config`.

## Packages (unsigned)

| Target | Command | Output in `dist/desktop/` |
|---|---|---|
| macOS universal (arm64 + amd64) | `make desktop-package-macos` | `Porto_<v>_macos_universal-unsigned.dmg` |
| Windows x64, per-user installer | `make desktop-package-windows` | `Porto_<v>_windows_amd64_setup-unsigned.exe` |
| Linux x86_64 AppImage | `make desktop-package-linux` | `Porto_<v>_linux_x86_64-unsigned.AppImage` |

Each package has a `.sha256` next to it. `SKIP_WEB_BUILD=1` skips `make web-build` (the SPAs must already be in
the embedded static folder). `<v>` is the numeric `MAJOR.MINOR.PATCH` of the git tag (`0.0.0` for development
builds). Builds are reproducible: `-trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=<v>"`,
`SOURCE_DATE_EPOCH` from the last commit.

- macOS: `Porto.app` with the icon made by `iconutil` from the brand kit PNGs (`assets/brand/porto/05_PNG_Icone/
  porto_app_navy_*.png`, copied as they are, ADR 0007), ad-hoc signature (required on Apple Silicon), `.dmg` with a
  link to Applications. First open: right-click on Porto, Open (Gatekeeper warns about an unidentified developer).
- Windows: icon and version resources (`scripts/desktop/mkico` writes the `.ico` from the kit PNGs without
  ImageMagick), `-H windowsgui`, Inno Setup 6 installer into `%LOCALAPPDATA%\Programs\Porto` without
  administrator rights, Start menu shortcut, uninstaller that keeps the data folder. SmartScreen warns on first
  run until the installer is signed.
- Linux: the AppImage carries the window build and the browser build; `AppRun` starts the window build when
  `libwebkit2gtk-4.1.so.0` is installed (`ldconfig -p`), else the browser build. `appimagetool` is never run
  unverified: set `APPIMAGETOOL` to a binary you trust, or `APPIMAGETOOL_SHA256` to the checksum of the pinned
  download in `scripts/desktop/package-linux.sh` (in CI: the repository variable `APPIMAGETOOL_SHA256`; without it
  the CI job builds and smoke-tests the AppDir only).

### Signing hooks

Signing and notarisation need certificates that are not in the repository. The scripts have hooks; they never
print secrets, never use a self-made certificate to look signed, and fail when a hook is set but the tool fails.

| Hook (environment) | Effect when set | When unset |
|---|---|---|
| `MACOS_SIGN_IDENTITY` | `codesign --options runtime --timestamp` with `packaging/desktop/macos/entitlements.plist` on the binary and the app, then the `.dmg` | ad-hoc `codesign -s -`, suffix `-unsigned` |
| `MACOS_NOTARY_PROFILE` (needs the identity) | `xcrun notarytool submit --wait` and `stapler staple` | no notarisation; suffix `-unnotarized` when signed |
| `WINDOWS_SIGN_CMD` | the command with the file path appended (signtool, AzureSignTool) on the exe and the installer | suffix `-unsigned` |
| `LINUX_SIGN_KEY` | `appimagetool --sign --sign-key` | suffix `-unsigned` |

CI (`.github/workflows/desktop.yml`) builds the three unsigned packages on pull requests touching the desktop
paths and smoke-tests `-version`; it uploads nothing. Distribution is from the Coste360 portal.

## Running

```
porto-desktop [-data-dir DIR] [-headless] [-browser]
porto-desktop -version
```

`-headless` shows nothing (tests, e2e); `-browser` opens the panel in the default browser instead of the window.
Exit codes: 0 (also a second launch that brought the running window to the front), 1 error, 2 usage.

### Data folder

| OS | Default |
|---|---|
| macOS | `~/Library/Application Support/Porto` |
| Windows | `%LOCALAPPDATA%\Porto` (not roaming) |
| Linux | `$XDG_DATA_HOME/porto`, else `~/.local/share/porto` |

`-data-dir` overrides `PORTO_DATA_DIR`, which overrides the default. Contents: `porto.db*`, `files/`,
`desktop.json` (settings), `instance.json` and `instance.token` (single instance), `porto-desktop.lock`, `lan/`
(local-network certificates, private keys encrypted), `backups/`, `logs/` (`porto-desktop.log`, 10 MiB x 5),
`mail/` (outgoing mail as files unless `PORTO_SMTP_URL` is set), `secret.key` (only with the file fallback).

### Secret key

The secret key encrypts personal data and files. It lives in the OS keychain (macOS Keychain, Windows Credential
Manager, Secret Service on Linux; service `it.coste360.porto`), else in `secret.key` (0600) with a warning in
the panel. A key is generated only on a new installation: if a database exists and its key is nowhere, Porto shows
a "secret key missing" page and never creates a new key. To recover, start Porto with `PORTO_SECRET_KEY` set to
the saved key, or move the data folder away and restore a backup with its key. Owners reveal the key in *App
desktop* (password re-entry) to keep it safe and to move online.

### Environment variables

| Variable | Meaning |
|---|---|
| `PORTO_UPDATE_CHECK=false` | no version request at all, whatever the settings say |
| `PORTO_DESKTOP_PORT` | loopback port (default: remembered in `desktop.json`, first one chosen by the OS) |
| `PORTO_DESKTOP_LAN_PORT` | local-network port (default 8443) |
| `PORTO_DESKTOP_KEYRING` | `auto` (keychain, file fallback) or `file` (CI, e2e) |
| `PORTO_SECRET_KEY` | advanced: wins over the keychain and the file |
| `PORTO_UPDATE_URL` | dev only (`PORTO_DEV=true`): another version manifest |
| `PORTO_DESKTOP_LAN_LOOPBACK=true` | dev only: the local-network listener on 127.0.0.1 (tests) |
| `PORTO_DESKTOP_FIRST_RUN_FILE=true` | dev only: writes the first-run URL to `<data>/first-run.url` (e2e) |

Passed to the embedded server: `PORTO_LOG_LEVEL`, `PORTO_SMTP_URL`, `PORTO_MAIL_FROM`, `PORTO_DEV`,
`PORTO_BOOTSTRAP_OWNER_*`, `PORTO_ADMIN_PASSWORD`, `PORTO_REQUIRE_TOTP_FOR_OWNERS`. `DATABASE_URL` is ignored: the
desktop edition uses SQLite only.

### First run

With no users, the window opens the first-run page with a single-use token in the URL fragment (never in a
request line or a log): it creates the owner and the organization, then the panel asks for the first marina. The
same page can instead restore a backup.

### Backups

Portable archives (the format of `porto backup`, restorable on any server with `porto restore`):
`porto-<YYYYMMDD>-<HHMMSS>-<kind>.tar.gz` with a `.sha256`, kinds `daily` (when the newest daily one is 24 hours
old, checked every 15 minutes), `exit` (every normal close), `manual` and `pre-restore`. Retention keeps 14 daily
and 5 exit backups by default; manual and pre-restore backups are never deleted. If the chosen folder is not
available (USB stick removed) backups go to `<data>/backups` and the panel says so. A restore takes a pre-restore
backup, prepares the restored database aside, then restarts the server in place: the previous database and files
stay as `porto.db.before-<ts>` and `files.before-<ts>`, everybody logs in again.

### Local network

Off by default: nothing listens outside `127.0.0.1`. The owner turns it on in *App desktop*; the TLS listener
(self-generated local CA and certificate) binds port 8443 on the private addresses and accepts private peers
only. On first enable the OS firewall asks for permission (macOS: "accept incoming connections"; Windows: allow
on private networks). Phones scan the pairing QR in *Dispositivi*, accept the certificate warning once and land on
the panel. Phones work only while the app is open: closing the window quits Porto (an exit backup is taken).

### Version check

On by default, switchable in the first run, in *App desktop* and with `PORTO_UPDATE_CHECK=false`. Once a day it
reads `https://download.coste360.it/porto/desktop/latest.json` with a User-Agent of product, version and OS only
(no cookies, identifiers or marina data). A newer version shows a dismissible banner; nothing is ever downloaded
or installed, and a failure is silent.

## Seams left for M6b

Remote mode (`desktopport.ModeRemote`, rejected in M6a), kiosk (`desktop.WindowOptions{Kiosk}`), tray and keep
running (`keep_running` in `desktop.json`), the bridge to local peripherals, mDNS (`lan.Announcer`), signed and
notarised packages, Windows arm64.
