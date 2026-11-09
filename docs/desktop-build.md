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
- Linux: the AppImage carries the window build and the browser build; `AppRun` test-starts the window build with
  `-version` (which loads every linked library) and starts it when that works, else the browser build, so a
  missing WebKitGTK 4.1 or an older glibc falls back instead of failing (`PORTO_APPIMAGE_UI=browser|window` forces
  one). `appimagetool` is never run
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
| `PORTO_DESKTOP_FIRST_RUN_FILE=true` | dev only: writes the first-run URL to `<data>/first-run.url` and, in remote mode, the enter URL to `<data>/remote-enter.url` (e2e) |

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
reads `https://updates.coste360.it/porto/latest.json` (the release manifest, `docs/release.md`) with the
User-Agent `porto/<version> (desktop; <goos>/<goarch>)` only (no cookies, identifiers or marina data). A newer version shows a dismissible banner; nothing is ever downloaded
or installed, and a failure is silent.

### Local CA for phones (M6b)

New local CAs are name constrained: they can vouch only for private and loopback addresses (10/8, 172.16/12,
192.168/16, 127/8, fc00::/7) and for `.local` and `localhost` names. While the local network listens, a
constrained CA is offered to phones at `https://<address>:<port>/lan/porto-ca.crt` (Android: install CA
certificate) and `/lan/porto-ca.mobileconfig` (iOS: install the profile, then enable full trust in Settings >
General > About > Certificate Trust Settings). With the CA installed the phone trusts the desktop origin and its
service worker works (offline start). A CA created by M6a has no constraints and is not offered: use "Rigenera
tutto" once (every device accepts the certificate again).

## Remote mode (M6b)

The app as a client of an online Porto: no database, no local users. On the first run, *Collega a un Porto
online* takes the link copied from *Dispositivi* (desktop kind, "Copia collegamento") of the online instance, or
its address and pairing code, and an **app PIN** (6 to 12 digits, Argon2id in `desktop.json`) that protects the
local administration (peripherals, kiosk, unpair). The app pairs as a `kind: desktop` device, keeps the device token
in the OS keychain (account `device-token/<data folder hash>`, file fallback `<data>/device.token`, 0600), never in
`desktop.json` or in the page, and restarts in remote mode on the same loopback port.

- **TLS**: `https` only (`http` with `PORTO_DEV=true`). A link with `fp` (self-signed or LAN target) pins that
  leaf; the CA that issued it is pinned at pairing, so a leaf rotated under the same CA is accepted; anything else
  is refused (`remote_pin_mismatch`, both fingerprints shown). Without `fp`: system roots, no pinning.
- **Proxy**: `127.0.0.1:<port>` reverse-proxies the panel and the API with `Authorization: Bearer <device token>`
  (cookies stripped both ways, SSE streamed). Only the app window can use it: the window opens
  `/__porto/enter#k=<per-launch secret>` once and gets an HttpOnly `porto_remote` cookie; foreign `Host` values
  are refused and unsafe requests must be same-origin. `/api/desktop/v1` and `/api/bridge/v1` are answered by the
  app itself.
- **Offline start**: the panel shell (index and assets) is cached in `<data>/remote-cache`; with the online
  instance unreachable the shell is served from it and `/api/*` answers 503 `remote_unreachable`, which the offline
  queue retries. The settings page shows the reachability.
- **Revocation and unpairing**: revoking the device on the online instance, or *Scollega* with the PIN, deletes the
  token, the cache and the remote settings and restarts on the first run ("Dispositivo revocato").
- `PORTO_DESKTOP_FIRST_RUN_FILE=true` (dev) also writes the enter URL to `<data>/remote-enter.url` (e2e);
  `POST /api/desktop/v1/dev/remote-offline` (dev) makes the proxy behave as unreachable.

## Kiosk (M6b)

*App desktop* > *Modalità chiosco* (owner in local mode, PIN in remote mode), applied from the next launch and only
to the webview window (never on the first run): full screen, no decorations, menus or developer tools, navigation
limited to the panel origin, no context menu, `window.open` and external links blocked, close shortcuts ignored
(macOS presentation options hide the Dock and the menu bar and disable process switching; Windows: topmost popup
window on its monitor; Linux: GTK full screen, undecorated, kept above). **Cmd/Ctrl+Shift+K** opens
`/admin/desktop/kiosk-exit`: the PIN (remote mode) or the email and password of an owner (local mode) leave kiosk
mode until the next launch. The desk start page in remote mode is `/admin/m/<marina>`.

## Peripherals and bridge (M6b)

Configured in *App desktop* > *Periferiche* (`desktop.json peripherals`), used by the loopback-only bridge
`/api/bridge/v1` ("Stampa scontrino" on the payment page and the dock Cassa tab, test buttons):

| Device | Drivers | Target |
|---|---|---|
| Receipt printer | `none`, `fake`, `escpos` (generic ESC/POS, CP858 with €, 80/58 mm, partial cut) | `tcp://<private IP or .local>[:9100]`, `cups://<queue>` (macOS, Linux), `file://` (dev) |
| Label printer | `none`, `fake`, `zpl` (generic ZPL text label) | as above |
| QR reader | `keyboard` (USB HID readers type into the page), `fake` | — |
| Local fiscal device | `none`, `fake` (`ok`, `offline`, `fail`) | — |

Fake drivers write JSON lines to `<data>/peripherals/<kind>.jsonl`. Printer targets on public addresses are
refused. Courtesy receipts are built from `getPaymentReceipt?format=json` (no customer data) and always say "Non
valido ai fini fiscali".

**Fiscal device.** Local mode: the configured local device is the fiscal driver `desktop` (documents stay pending
while none is configured or it is unreachable). Remote mode: the online instance uses the core driver `bridge` with
`fiscal_bridge_device_id` = this desktop in its billing settings; the app's fiscal agent claims the pending
documents every 10 s while the instance is reachable (60 s lease), issues them on the local device and reports the
outcome. Nothing is issued offline. Real device models (Epson FP-81 II RT / FP-90III RT, RCH Print!F / MyCash,
Custom KUBE II / Q3X, Olivetti Form 200 RT / Nettuna 7000, Ditron Zip/Sole, Axon, Micrelec) are extensions
implementing `ext.FiscalDriver`; Windows raw USB printing, serial QR readers and Bluetooth printers are later.

Manual checks: a real ESC/POS network printer prints a courtesy receipt from a remote-mode desktop; kiosk on macOS
hides the Dock and the menu bar and exits only with the PIN.

## Seams left after M6b

Tray and keep running (`keep_running` in `desktop.json`), mDNS (`lan.Announcer`), signed and notarised packages,
Windows arm64, Windows raw USB / serial / Bluetooth peripheral transports (`desktopport.Transport`), real fiscal
device models (extensions).
