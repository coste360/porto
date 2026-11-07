# M6a — Edizione desktop locale e rete locale: work order

Status: Plan phase output. This plan is authoritative for the M6a builders. If it conflicts with `docs/spec/`, the
spec wins: stop and report the conflict to the orchestrator instead of guessing. No open decision of
`docs/spec/33-decisioni-aperte.md` is touched by M6a. §13 lists every detail decided here where the spec is silent;
CONTRACT records the ones that bind later milestones in ADR 0010.

Sources: spec 03 (desktop row), 04 (`cmd/porto-desktop`), 26 (roles, device tokens), 27 (API surfaces), 29 (29.2 only
to keep seams for M6b), 30, 31 (all), 32 (M6), 33; `docs/invariants.md` (INV-01, INV-70, INV-72, INV-73); ADR 0001,
0007, 0008, 0009; `docs/m0-plan.md` (devices, pairing, `po_dev_`, §5 config, rate limits), `docs/m3-plan.md` §8.6
(dock phone panel `/m/$marinaId/dock`), `docs/m5-plan.md` §5.6 (`ext.FiscalDriver`, deferred drivers: bridge is
M6b), `docs/architecture.md`; `.claude/rules/*.md`; skills `new-endpoint`, `new-migration`, `shadcn`.

## 0. Ground rules for every builder

Everything in `docs/m0-plan.md` §0 .. `docs/m5-plan.md` §0 still applies. In short:

1. **Arenile is a reference, not a source** (ADR 0001), even though spec 31.2 says "derivato dal suo codice".
   Closest analogues, read for behaviour and edge cases only: `../arenile/cmd/arenile-desktop`,
   `../arenile/internal/desktop` (launcher sequence, secret store, backups, instance lock, first run),
   `../arenile/internal/desktop/lan` (certificate, private listener, address pick), `../arenile/scripts/desktop`,
   `../arenile/packaging/desktop`, `../arenile/docs/desktop-build.md`, its spec 29.1/29.2. Write Porto's own
   code with Porto names (`marina`, `PORTO_*`, `porto_*`, `po_dev_`). Never copy files, code, comments or tests;
   never modify `../arenile`. Copying dependency versions is allowed.
2. **Porto is not cloud.** Nothing in M6a calls Coste360 or any other service except the optional version check
   (§2.8), which can be switched off and whose failure is never an error. No telemetry (`PORTO_TELEMETRY` stays
   refused).
3. **One server per marina** (INV-01): the desktop is a full Porto server on SQLite; LAN clients talk to the same
   process. No sync, no second database, no client-side writes outside the existing API.
4. **CGO only in `cmd/porto-desktop` files with the `webview` build tag.** `CGO_ENABLED=0 go build ./...`,
   `make check`, `make build` (bin/porto) and CI keep working on a machine without the webview toolchain. Every
   decision lives in pure Go behind interfaces (`Window`, `SecretStore`, `LANController`, …) and is tested
   without a window.
5. Secrets: the secret key in the OS keychain (file fallback 0600); LAN TLS private keys encrypted with the
   `crypto.KeyRing` (HKDF purpose keys, never the raw key); device tokens and sessions stored hashed, shown once;
   pairing codes short-lived, single use by conditional UPDATE. **Never log** tokens, pairing codes, the first-run
   token, the instance token, the secret key, fingerprints next to codes, emails or names (INV-70).
6. API first: CONTRACT edits `api/openapi.yaml` and runs `make gen`; builders never edit generated files.
   Permissions in domain services/runtime methods, not in handlers. Every marina query through `store.MarinaScope`.
7. Time from `clock.Clock` (scheduler, certificate validity, pairing expiry); `time.Now` stays banned (forbidigo).
8. UI: shadcn only (CLI in `web/shared`), Tailwind tokens, no custom CSS, i18n it (source) + en, `BrandLogo` only.
   Desktop icons come from `assets/brand/porto/05_PNG_Icone/porto_app_navy_*.png` as they are (ADR 0007), never
   redrawn.
9. Do not `git commit`; no destructive git commands; no `rm -rf`; Docker only for your own throwaway test
   containers (`scripts/with-postgres.sh`). Edit only files you own (§11); request changes elsewhere in your report.
10. Test speed: while iterating run only the touched packages
    (`go test -tags integration ./internal/<pkg>/...`, with `PORTO_TEST_POSTGRES_URL` where relevant; vitest on
    touched files). Full gates only in §12.

## 1. Scope

### 1.1 In M6a

- `cmd/porto-desktop`: launcher with **local mode** (server on `127.0.0.1`, panel in the app window through the
  system webview: WKWebView on macOS, WebView2 on Windows, WebKitGTK 4.1 on Linux; browser fallback), data in the
  user folder, secret key in the OS keychain, daily backups and backup on close with retention, restore,
  version check (on by default, switch off, never required), single instance, rotated logs, first run without a
  terminal (create the owner in the window, or restore a backup).
- **Local-network mode**: opt-in TLS listener on the LAN with a self-generated certificate (local CA + leaf),
  private-peer filter, Host check, address choice, certificate rotation; office PCs open the panel in their
  browser, phones scan a QR and land on the dock panel (< 60 s).
- **Browser pairing**: the QR opens `https://<lan>/admin/pair#c=<code>&fp=<fp>`; the code is exchanged once for a
  device bound to a **device session cookie** (revocable from *Dispositivi*). Device-token pairing
  (`/api/device/v1/pair`) is unchanged.
- *Dispositivi* page: paired devices (browser and token), revoke, pairing dialog with QR, code, countdown,
  fingerprint, "enable the local network" notice when the QR would not be reachable.
- Local-mode explanations: public page, online payments (Stripe, payment links), remote signature (OTP to the
  customer) are unavailable in the desktop edition; a guide page explains how to move to an online instance
  (backup + secret key + `porto restore`).
- Desktop settings page (loopback only, owner): local network on/off and status, backups (folder, retention, list,
  backup now, restore), version check switch and update banner, reveal the secret key (password re-entry).
- Packaging scripts, **unsigned**: macOS `.app` + `.dmg` (universal), Windows installer (Inno Setup, x64), Linux
  AppImage (x86_64); documented signing/notarisation hooks that are never faked. `docs/desktop-build.md`.
- `desktop.yml` CI workflow building unsigned packages and smoke-testing `-version` (no artifact upload).

### 1.2 M6b (explicitly NOT now) and the seams M6a leaves

| M6b item | Seam left by M6a |
|---|---|
| PWA offline queue (29.2): shell cache, IndexedDB allowlist 48 h, queue with `Idempotency-Key` | Device sessions are long-lived cookies (§3.4) so a queued request authenticates after reconnection; `Idempotency` middleware already exists. A local **CA** is generated in M6a (§2.10) and its fingerprint exposed, because Chrome refuses service workers on a certificate-error origin: M6b serves the CA as an installable profile. |
| Phone screens per role (yard, rental, maintenance, direction) | `web/admin/src/lib/landing.ts` `landingFor(permissions, kind)` decides the post-pairing route; M6a maps dock → `/m/$id/dock`, everything else → `/m/$id`. |
| **Remote mode** (desktop as client of an online instance with token per device) | `desktopport.Mode` has `remote` reserved (rejected in M6a); `desktop.json` has `mode`; `/api/device/v1/pair` + `po_dev_` unchanged; QR keeps the `porto://pair` payload for native clients; `lan.PinnedClientConfig(fp)` exists (tests use it). |
| Kiosk mode for the desk | `Window` interface (`Options{Kiosk bool}` reserved, ignored). |
| Bridge to local peripherals (receipt printer, local `FiscalDriver`, QR reader, label printer) | `ext.FiscalDriver.Deferred()` contract from M5; `/api/device/v1` device auth. Nothing else in M6a. |
| Tray icon, keep running when the window is closed, autostart at login, window geometry memory | Launcher `UI` interface separates window from lifecycle; `desktop.json` reserves `keep_running`. |
| mDNS (`porto.local`) announcement | `lan.Announcer` interface with a no-op implementation. |
| Signed / notarised packages, Windows arm64, macOS thin packages | Hooks in the scripts (§5.2). |
| Owner password recovery without SMTP and without a terminal | none (listed, §13 D22). |

## 2. Launcher design (DESKTOP, LAN for §2.10–§2.12)

### 2.1 Process model and packages

**In-process.** `porto-desktop` is one process: the webview owns the main OS thread; the Porto server (`app.App`,
jobs, outbox) runs in goroutines of the same process. Justification: one binary, no IPC, ordered shutdown (stop
LAN → stop HTTP → stop jobs → exit backup → close DB), no orphaned child on crash, same bootstrap as `porto serve`
(migrations included). A child process would add supervision and port handshakes for no isolation gain (a crash of
either is a crash of the edition anyway).

```
cmd/porto-desktop/             main.go (flags, wiring, exit codes)                                  DESKTOP
                               ui_browser.go   (//go:build !webview: open the default browser or nothing)
                               ui_webview.go   (//go:build webview: webview_go window, bindings §2.13)
                               menu_darwin.go + menu_darwin.m (//go:build webview && darwin: app/Edit/View/Window menus, print)
internal/desktopport/          types + interfaces shared by app, http, desktop, lan (no logic)        CONTRACT
internal/desktop/              launcher, paths, settings, config, secret, lock, instance, backup,     DESKTOP
                               update, logfile, firstrun, runtime (implements desktopport.Runtime)
internal/http/desktop/         thin /api/desktop/v1 handlers over desktopport.Runtime                 DESKTOP (stub by CONTRACT)
internal/lan/                  cert (CA + leaf), addrs, listener (private peers), hostcheck, server   LAN
                               (implements desktopport.LANController), PinnedClientConfig, Announcer
```

Import graph (no cycles): `desktopport` imports stdlib and `internal/domain/auth` (for `*auth.Principal`; auth
imports nothing desktop-related). `internal/http/desktop` → `desktopport`, `auth`;
`internal/app` → `desktopport`; `internal/desktop` → `app`, `lan`, `desktopport`, `backup`, `config`, `crypto`,
`clock`, `event` (egress); `internal/lan` → `desktopport`, `crypto`, `clock`, `reqctx`.

### 2.2 Start sequence (`desktop.Launcher.Run`)

1. Parse flags: `-data-dir DIR`, `-headless` (no window, no browser: tests and e2e), `-browser` (panel in the
   default browser instead of the window), `-version` (prints `porto-desktop <version> (window|browser)`).
2. Resolve the data dir (§2.3), `MkdirAll 0700`; open the rotated log (§2.9).
3. Take the **instance lock** (§2.7). If held: activate the running instance and exit 0.
4. Load `desktop.json` (§2.4) and the desktop env overrides (§2.4).
5. Resolve the **secret key** (§2.5).
6. Pick the loopback port: remembered `port` if free, else `127.0.0.1:0`; persist it. Build the server config with
   the pure `desktop.ServerEnv(...)` → `config.Load`: `PORTO_ADDR=127.0.0.1:<port>`,
   `PORTO_BASE_URL=http://127.0.0.1:<port>`, `PORTO_DATA_DIR`, `PORTO_SECRET_KEY`, `PORTO_AUTO_MIGRATE=true`,
   `PORTO_MAIL_DIR=<data>/mail` (file transport unless `PORTO_SMTP_URL` is set in the environment); allow-listed
   pass-through from the real environment: `PORTO_LOG_LEVEL`, `PORTO_SMTP_URL`, `PORTO_MAIL_FROM`, `PORTO_DEV`,
   `PORTO_BOOTSTRAP_OWNER_*`, `PORTO_ADMIN_PASSWORD`, `PORTO_REQUIRE_TOTP_FOR_OWNERS`. SQLite only (D3).
7. `app.NewWithOptions(ctx, cfg, version, ext, log, app.Options{Desktop: runtime})`, `app.Prepare` (migrations),
   listen on the loopback listener (tagged `reqctx.ListenerLoopback`), `app.Run(ctx, listeners...)`.
8. If `store.CountUsers == 0` and no `PORTO_BOOTSTRAP_OWNER_*`: mint the first-run token (§2.6) and open
   `/admin/desktop/first-run#t=<token>`; else open `/admin/`.
9. If `lan_enabled` in settings: `LANController.Enable` (failure → status error shown in the panel, never fatal).
10. Start the backup scheduler and the version check (goroutines on the injected clock).
11. Run the UI (`Window.Run` blocks on the main thread until the window closes / SIGINT / SIGTERM in headless).
12. Stop: LAN off → HTTP shutdown (10 s) → jobs stop and wait → **exit backup** → DB close → release lock.

A restore request (§2.6) makes `app.Run` return with `ErrRestart`; the launcher performs the swap and loops back to
step 6 keeping the window open (the window reloads `/admin/`).

### 2.3 Data dir per OS (pure `desktop.DataDir(goos, env)`)

| OS | Default | Notes |
|---|---|---|
| macOS | `~/Library/Application Support/Porto` | |
| Windows | `%LOCALAPPDATA%\Porto` | not roaming: a SQLite file must not follow roaming profiles |
| Linux | `$XDG_DATA_HOME/porto`, else `~/.local/share/porto` | |

Override order: `-data-dir` flag, then `PORTO_DATA_DIR`. Contents: `porto.db*`, `files/` (file store),
`desktop.json`, `instance.json`, `porto-desktop.lock`, `lan/` (certs, §2.10), `backups/` (default backup folder),
`logs/`, `mail/`, `secret.key` (only with the file fallback).

### 2.4 Settings and desktop config

`desktop.json` (0600, atomic write tmp+rename, unknown fields preserved): `version` (1), `mode` (`local`; `remote`
reserved → validation error), `port`, `lan_enabled` (false), `lan_port` (8443), `lan_address` ("" = automatic),
`backup_dir` ("" = `<data>/backups`), `backup_keep_daily` (14, 1..365), `backup_keep_exit` (5, 1..50),
`update_check` (true), `dismissed_update` (""), `keep_running` (reserved, ignored).

Desktop env overrides (pure loader in `internal/desktop/config.go`, same style as `internal/config`, errors name
the variable never the value): `PORTO_UPDATE_CHECK` (bool, forces off when false), `PORTO_DESKTOP_PORT`,
`PORTO_DESKTOP_LAN_PORT`, `PORTO_DESKTOP_KEYRING` (`auto` | `file`; `file` for CI/e2e), and **dev only**
(`PORTO_DEV=true`, else a validation error): `PORTO_UPDATE_URL`, `PORTO_DESKTOP_LAN_LOOPBACK=true` (LAN listener on
127.0.0.1 for tests), `PORTO_DESKTOP_FIRST_RUN_FILE=true` (writes the first-run URL to `<data>/first-run.url`
0600 for e2e, deleted when consumed).

### 2.5 Secret key and keychain

`desktop.SecretStore` interface `{Get() (string, error); Set(string) error; Kind() string}`; implementations
`KeyringStore` (`github.com/zalando/go-keyring` v0.2.8, MIT, no CGO: `security` CLI on macOS, wincred on Windows,
Secret Service over D-Bus on Linux; service `it.coste360.porto`, account `secret-key/<first 12 hex of
sha256(abs data dir)>`), `FileStore` (`<data>/secret.key` 0600). `ResolveSecretKey(stores, dbExists)`:

- found in keychain → use; found only in file → use (and try to move it into the keychain, delete the file only
  after a successful read-back);
- found nowhere and **no database** → generate (`crypto.GenerateKey`), store in the keychain, else the file
  (the panel shows a warning when the file fallback is in use);
- found nowhere but a database exists → **never generate** (encrypted columns and files would be lost): stop with
  the "secret key missing" screen (browser fallback page served by the launcher on loopback) explaining how to put
  the key back (`PORTO_SECRET_KEY` env, or restore a backup with its key).

`PORTO_SECRET_KEY` in the environment wins over both (advanced use).

### 2.6 First run, restore, reveal key

- **First-run token**: 32 random bytes base64url, kept in memory only (hash), single use (`atomic.Bool` CAS +
  constant-time compare), valid until used or the process exits. Passed to the window in the URL **fragment**
  (never sent in a request line, never in access logs).
- `POST /api/desktop/v1/first-run` (loopback, token in body) → in one transaction re-check `CountUsers == 0`,
  `auth.CreateOwner` (organization name, owner name, email, password), set `update_check`, create the session and
  set cookies; 201. Then the existing panel flow creates the first marina (`CreateMarinaForm`).
  `first_run_done` 409 when users exist; `first_run_token_invalid` 400.
- **Restore** (owner, loopback): from a listed backup (`POST /backups/{backupId}/restore`) or an uploaded file
  (`POST /restore-upload`, `application/gzip`, limit 4 GiB streamed to `<data>/restore-<ts>.tar.gz`). Also offered
  on the first-run page (token instead of owner). Flow: validate the manifest, take a **safety backup**
  (`kind=pre-restore`), migrate a fresh `porto.db.restoring`, `backup.Restore(force=false)` into it (+ files into
  `files.restoring/`), then `ErrRestart`: the launcher stops the app, renames `porto.db` → `porto.db.before-<ts>`
  and `files` → `files.before-<ts>`, moves the restored ones in place, starts again. Any failure before the swap
  leaves the live database untouched (`restore_failed` 422). Sessions are not in backups: everybody logs in again.
  The fresh database is migrated only to the archive's schema version, loaded, then migrated up, so a backup taken
  before an app update still restores; an archive from a newer app is `restore_backup_newer` (422). The
  `desktop.restore` audit row is written into the restored database too.
- **Reveal secret key** (owner, loopback, password re-entry checked with the auth service, rate limited 5/15 min):
  returns the key once for the move-online guide; audit `desktop.secret_reveal` (no value).

### 2.7 Single instance

`porto-desktop.lock` with `flock` (unix) / `LockFileEx` (windows) via `golang.org/x/sys` (already indirect).
`instance.json` (0600): `{pid, port, token_hash}` plus the raw instance token in a separate 0600 file
`instance.token` readable only by the user. A second launch on the same data dir reads them and calls
`POST http://127.0.0.1:<port>/api/desktop/v1/activate` with header `Porto-Instance-Token`; the running instance
brings its window to the front (`Window.Focus`) and the second process exits 0. Stale lock (process gone) is
reacquired by the OS lock semantics; `instance.json` is rewritten at each start.

### 2.8 Backups and version check

- Format: the existing portable `internal/backup` archive (tables JSONL + encrypted file store blobs), so a desktop
  backup restores on any server (`porto restore`). Name (pure): `porto-<YYYYMMDD>-<HHMMSS>-<kind>.tar.gz`, kind
  `daily|exit|manual|pre-restore`, local time; written to `<name>.part` then renamed; `.sha256` next to it.
- Scheduler (`clock.Clock`): tick every 15 min; a daily backup is due when the newest valid `daily` file in the
  folder is ≥ 24 h old (so it survives restarts and sleeps); failure → status `last_error`, retried on the next
  tick. **Exit backup** on every normal stop (after HTTP and jobs stop, before DB close, 60 s timeout).
- Retention (pure `PruneCandidates`): keep the newest `backup_keep_daily` daily and `backup_keep_exit` exit files;
  `manual` and `pre-restore` are never pruned; never delete the newest valid backup of any kind; only files that
  match the name pattern are touched.
- Folder unavailable (USB stick removed): write to `<data>/backups` and report `fallback: true` in the status.
- **Version check** (`internal/desktop/update.go`): when `update_check` is on, at start + every 24 h,
  `GET <update URL>` through the egress client (`event.NewEgress`: https, public addresses only), default URL
  `https://download.coste360.it/porto/desktop/latest.json` (D16), body ≤ 64 KiB
  `{"version":"1.2.3","url":"https://…","published_at":"…"}`, User-Agent `Porto-Desktop/<version> (<goos>; <goarch>)`
  only: no cookies, no identifiers, no marina data. Newer semver than the running one → `update` in the status →
  dismissible banner in the desktop window (dismissal per version). Any failure: debug log, no banner, no retry
  storm. Off → **no request at all**. Never auto-downloads or installs.

### 2.9 Logs

`slog` JSON to `<data>/logs/porto-desktop.log`, own rotator (10 MiB × 5 files, no new dependency); headless also
writes text to stderr. Same no-PII rules (`TestLogs_NoPII` extended, §10). Settings page offers "open logs folder"
through `POST /api/desktop/v1/open {target}` (`data_dir|backup_dir|logs`), executed by the launcher with the OS
opener (`open`, `explorer`, `xdg-open`) on fixed paths only, never a client-supplied path.

### 2.10 Local vs local-network mode, TLS (LAN)

- **Local** (default): only the loopback listener; nothing listens on other interfaces. Phones cannot reach it
  (31.3); the pairing dialog says so (§4.1).
- **Local network**: the owner switches it on in *App desktop* (`PUT /api/desktop/v1/lan`). The LAN listener is
  started **only then** and stopped when switched off or on exit. It binds `0.0.0.0:<lan_port>` (and `[::]`),
  default 8443 (fixed so phone bookmarks and future PWA installs keep their origin); port busy →
  `lan_port_unavailable` 409 and the setting stays off.
- `lan.PrivateListener` closes, before reading a byte, connections whose TCP peer is not loopback, RFC 1918, IPv6
  ULA `fc00::/7` or link-local (IPv4-mapped unmapped first; CGNAT 100.64/10 refused). Then `tls.NewListener`.
- **Certificates**: on first enable generate a **local CA** (ECDSA P-256, 10 years, `CN=Porto local CA <short id>`,
  `MaxPathLen 0`) and a **leaf** signed by it (ECDSA P-256, 397 days, SANs = every private address of up,
  non-virtual interfaces + `<hostname>.local` + `localhost` + `127.0.0.1` when the loopback test flag is on).
  Files in `<data>/lan/`: `ca.crt`, `leaf.crt` (PEM), `ca.key.enc`, `leaf.key.enc` = `KeyRing.Encrypt(PKCS#8 DER,
  aad "porto-lan-ca" / "porto-lan-leaf")` — no plaintext private key on disk; re-encrypted when
  `KeyRing.NeedsRotation`. **Rotation**: the leaf is re-issued (same CA) when it has < 30 days left, when the set of
  private addresses is no longer covered by its SANs, or on "Rigenera certificato"; the CA is regenerated only on
  explicit "Rigenera tutto" (which also invalidates installed CA profiles, M6b).
- Fingerprint = SHA-256 of the leaf DER; shown as 16 groups of 4 hex in the UI and as base64url (43 chars) in the
  QR (`fp`). CA fingerprint exposed in the LAN status (for M6b).
- **How phones trust it — decision: trust on first use with an out-of-band fingerprint, no HTTP page.** The QR opens
  the HTTPS URL directly; the phone browser shows the self-signed warning once per certificate, the user accepts it,
  and the panel loads. An HTTP pairing page cannot "pin" anything for a browser (JavaScript cannot see the TLS
  certificate, and an HTTP page is itself spoofable on the Wi-Fi), so it would add an insecure hop without adding
  trust; installing a CA profile takes several minutes and breaks the < 60 s target. The fingerprint in the QR
  serves (a) staff who want to compare it in the browser's certificate details (the pairing page shows the QR's `fp`
  in the same 4-hex grouping), and (b) native/remote clients that pin it (M6b, `lan.PinnedClientConfig`). Session
  cookies are `Secure`, the pairing code is in the fragment, single use and short-lived, so a code stolen by a
  rogue certificate on the Wi-Fi window is worth one device that the owner sees and revokes in *Dispositivi*.
- **Address**: pure `lan.PickAddress(ifaces)` prefers 192.168/16, then 10/8, then 172.16/12 on up, non-loopback
  interfaces, skipping virtual ones by name (`docker*`, `br-*`, `veth*`, `vboxnet*`, `vmnet*`, `utun*`, `awdl*`,
  `llw*`, `tailscale*`, `zt*`); `lan_address` in settings overrides it if still present. URL
  `https://<addr>:<port>`.
- **Host check** (`middleware.HostCheck`, both listeners): the `Host` header must be one of the listener's allowed
  hosts (loopback: `127.0.0.1:<port>`, `localhost:<port>`; LAN: the leaf SANs with `:<lan_port>`), else 421; this
  blocks DNS rebinding against the loopback API.
- Every LAN connection context carries `reqctx.ListenerLAN`; `/api/desktop/v1/*` answers **404** on it (§3.3).
- `PORTO_BASE_URL` stays the loopback URL; absolute links that matter on the LAN (pair URL) are built from the LAN
  status (§3.2). Windows/macOS firewall prompts on first bind are documented in `docs/desktop-build.md`.

### 2.11 Rate limits on the LAN (LAN)

All phones of a marina share one /24: `middleware.ByIP` keys **private and loopback peers by full address** (/32,
/128) instead of the /24 or /56 prefix, everywhere (servers in sede benefit too). Public peers unchanged.

### 2.12 mDNS

Not in M6a (D12): the QR carries the IP; `.local` resolution on Android browsers is unreliable and a responder
would add a dependency. `lan.Announcer` (no-op) is the seam.

### 2.13 Window (DESKTOP, `webview` tag)

`github.com/webview/webview_go v0.0.0-20240831120633-6173450d4dd6` (MIT). Title "Porto", size 1280×800, min hint
1024×680, dev tools off unless `PORTO_DEV`. Fallbacks to the default browser: Windows without WebView2 runtime,
Linux without `libwebkit2gtk-4.1` (AppImage `AppRun` picks the bundled browser build, §5.1). Closing the window
quits the app (exit backup, LAN off) on every OS in M6a (tray/keep-running is M6b); the *App desktop* page warns
that phones disconnect when the app is closed. macOS: native menus (Porto: Informazioni, Esci ⌘Q; Modifica:
undo/redo/cut/copy/paste/select all — required for ⌘C/⌘V in WKWebView; Vista: Ricarica ⌘R, Apri nel browser;
Finestra: Riduci, Zoom) and printing through `menu_darwin.m`.

**JS bridge** (binding names are part of the CONTRACT, implemented by DESKTOP, used by WEB through
`web/shared/src/desktop/bridge.ts`): `window.portoDesktop = {version, saveFile(name, base64) → Promise<path>,
openExternal(url), print()}`, injected with `webview.Init` only in the window build. `saveFile` writes into the
user's Downloads folder with a unique name (sanitised base name, ≤ 50 MiB) and reveals it; `openExternal` opens
`https:`/`mailto:` URLs in the default browser (anything else refused); `print` runs the native print operation.
Without the bridge (browser, LAN clients) the web uses the normal anchor download and `window.print()`.

## 3. Server-side contract

### 3.1 `internal/desktopport` (CONTRACT, frozen)

```go
type Mode string // "local" ("remote" reserved, rejected)
type Runtime interface {
    Status(ctx context.Context) Status                                   // loopback, no auth: version, first_run_pending, update
    FirstRun(ctx context.Context, in FirstRunInput) (userID string, err error)
    FirstRunRestore(ctx context.Context, token string, src RestoreSource) error // 409 once an owner exists; token burnt only on success
    Settings(ctx context.Context, p *auth.Principal) (Settings, error)    // owner anywhere
    UpdateSettings(ctx context.Context, p *auth.Principal, in SettingsPatch) (Settings, error)
    LAN() LANController
    Backups(ctx context.Context, p *auth.Principal) ([]BackupFile, BackupStatus, error)
    BackupNow(ctx context.Context, p *auth.Principal) (BackupFile, error)
    Restore(ctx context.Context, p *auth.Principal, src RestoreSource) error // returns after scheduling the restart
    RevealSecretKey(ctx context.Context, p *auth.Principal, password string) (string, error)
    Activate(ctx context.Context, instanceToken string) error
    Open(ctx context.Context, p *auth.Principal, target string) error
}
type LANController interface {
    Status() LANStatus // enabled, listening, url, addresses[], port, fingerprint, ca_fingerprint, cert_expires_at, error
    Enable(ctx context.Context, o LANOptions) (LANStatus, error)
    Disable(ctx context.Context) error
    RotateCertificate(ctx context.Context, all bool) (LANStatus, error)
}
type PairTarget struct{ Origin, Fingerprint string; Reachable bool } // used by device.Service
```

Errors (sentinels in `desktopport`): `ErrFirstRunDone`, `ErrFirstRunToken`, `ErrLANPortUnavailable`,
`ErrLANNoAddress`, `ErrBackupInProgress`, `ErrRestoreFailed`, `ErrInvalidSettings` (with field list). Owner check:
`auth.(*Principal).IsOwnerAnywhere()` (CONTRACT adds it: owner role on any organization grant), first line of
every runtime method that takes a principal; audit rows `desktop.settings_update`, `desktop.lan_enable`,
`desktop.lan_disable`, `desktop.lan_cert_rotate`, `desktop.backup`, `desktop.restore`, `desktop.secret_reveal`
with the caller's first owned organization and no marina (no values, only field names / kinds).

### 3.2 Devices (LAN)

- `device.Options.PairTarget func() desktopport.PairTarget`; default (server edition) = `{BaseURL, "", true}`; the
  desktop app sets it from `LANController.Status()` (LAN on → `{lanURL, fp, true}`, off → `{loopback, "", false}`).
- `CreatePairingCode` returns additionally `PairURL = <origin>/admin/pair#c=<code>` (+ `&fp=<fp>` when set),
  `Fingerprint`, `Reachable`; **the QR SVG encodes `PairURL`** (phones open it with the camera). `QRPayload`
  (`porto://pair?v=1&url=…&code=…&fp=…`) stays for native/remote clients. TTL unchanged (default 10 min, 1–60).
- New `PairSession(ctx, in PairInput) (*PairSessionResult, error)`: same validation and the **same single
  conditional UPDATE** consumption (`store.ConsumePairingCode`), then in the same transaction: create the device
  with `access = 'browser'` (a random token hash is stored to satisfy the schema and is never returned), create a
  **device session** (`auth.Service.CreateDeviceSessionTx`), audit `device.pair` (`access: browser`), outbox
  `device.paired`. Returns session token, CSRF token, device, user, marina, effective permissions.
- `revoke` deletes the device's sessions (`store.DeleteSessionsOfDevice`) in the same transaction.
- `Device.Access` (`token|browser`) in list and view.

### 3.3 Auth and middleware (LAN)

- Device session: `sessions.device_id` set; idle expiry **30 days**, absolute **180 days** (D9) instead of 24 h /
  14 d; `totp_verified = !userHasTOTP` (a user with TOTP enters the code once on the phone, existing `TOTPGate`). The
  pending session of a TOTP user lives 10 minutes (idle and absolute); a successful code replaces it with a new,
  verified device session (rotated cookies), as a login rotates on TOTP confirmation.
  `Authenticate` rejects a session whose device is revoked (defence in depth) and touches
  `devices.last_used_at` at most every 5 minutes.
- Cookies: `Secure = cfg.SecureCookies() || r.TLS != nil`; `config.SecureCookies()` (CONTRACT) becomes false for an
  `http` base URL on a loopback host (desktop loopback), true otherwise as before.
- `middleware.ListenerTag` (ConnContext helper), `middleware.LoopbackOnly` (`/api/desktop/v1/*`: listener must be
  loopback **and** TCP peer loopback; otherwise 404 `not_found`), `middleware.HostCheck` (§2.10).
- `POST /api/device/v1/pair-session` is public (like `pair`), rate limited with the pairing limit (10/min per peer).

### 3.4 OpenAPI (`api/openapi.yaml`, CONTRACT)

New tag `desktop`. Security scheme `instanceToken` (apiKey header `Porto-Instance-Token`). D = `/api/desktop/v1`.

| Method path | operationId | Auth | Request → Response | Errors |
|---|---|---|---|---|
| GET D/status | getDesktopStatus | none (loopback) | → 200 `DesktopStatus{version, mode, first_run_pending, update?{version,url}, secret_store: keyring\|file}` | 404 |
| POST D/first-run | desktopFirstRun | none (loopback) | `{token, organization, name, email, password, update_check}` → 201 `{user_id}` + cookies | 400 first_run_token_invalid, 409 first_run_done, 422 validation |
| GET D/settings | getDesktopSettings | cookie | → `DesktopSettings` | 403, 404 |
| PATCH D/settings | updateDesktopSettings | cookie+csrf | `DesktopSettingsPatch` → `DesktopSettings` | 400, 403 |
| GET D/lan | getDesktopLan | cookie | → `LanStatus` | 403 |
| PUT D/lan | setDesktopLan | cookie+csrf | `{enabled, port?, address?}` → `LanStatus` | 409 lan_port_unavailable, lan_no_address |
| POST D/lan/certificate | rotateLanCertificate | cookie+csrf | `{all: bool}` → `LanStatus` | 403 |
| GET D/backups | listDesktopBackups | cookie | → `{items: BackupFile[], status: BackupStatus}` | 403 |
| POST D/backups | createDesktopBackup | cookie+csrf | → 201 `BackupFile` | 409 backup_in_progress |
| POST D/backups/{backupId}/restore | restoreDesktopBackup | cookie+csrf | → 202 | 404, 422 restore_failed |
| POST D/restore-upload | uploadDesktopRestore | cookie+csrf, or `first_run_token` query on first run | `application/gzip` → 202 | 413, 422 restore_failed |
| POST D/secret-key/reveal | revealDesktopSecretKey | cookie+csrf | `{password}` → `{secret_key}` | 400 invalid_credentials, 429 |
| POST D/open | openDesktopFolder | cookie+csrf | `{target: data_dir\|backup_dir\|logs}` → 204 | 400 |
| POST D/activate | activateDesktop | instanceToken | → 204 | 401 |
| POST `/api/device/v1/pair-session` | pairDeviceSession | none | `{code, name, platform, kind}` → 201 `PairSessionResult{device, marina{id,slug,name}, user{id,name}, permissions[]}` + Set-Cookie | 400 pairing_code_invalid, 429 |

Schema changes: `PairingCode` += `pair_url`, `fingerprint` (nullable), `reachable`; `Device` += `access`
(`token|browser`); `Meta.edition` enum `[server, desktop]`, `Meta` += `local_mode` (bool: online features
unavailable) — the desktop edition reports `edition: desktop`, `local_mode: true`, capabilities `desktop.local`
always and `desktop.lan` while the LAN listener runs. `BackupFile{id (file name), kind, size_bytes, created_at,
sha256}`; `BackupStatus{dir, fallback, last_daily_at?, last_error?, next_due_at?}`; `LanStatus` per §3.1;
`DesktopSettings` per §2.4 (no secrets). Problem slugs: `first_run_token_invalid`, `first_run_done`,
`lan_port_unavailable`, `lan_no_address`, `backup_in_progress`, `restore_failed`, `restore_backup_newer` (+ `errors.*` i18n keys).
`/api/desktop/v1` handlers return 404 when `app.Options.Desktop` is nil (server edition).

### 3.5 Migration `00013_device_sessions.sql` (CONTRACT, both dialects, same number)

- `sessions.device_id TEXT NULL REFERENCES devices (id) ON DELETE CASCADE` + index `sessions_device_id_idx`.
- `devices.access TEXT NOT NULL DEFAULT 'token' CHECK (access IN ('token','browser'))`.
- Down drops both (SQLite: table rebuild as in earlier migrations). Store models `Session.DeviceID`,
  `Device.Access`. `devices` stays in `store.BackupTables()` (new column round-trips; `sessions` stays excluded).

### 3.6 App seams (CONTRACT, real code, small)

`app.Options.Desktop desktopport.Runtime`; `app.Options.PairTarget`; `App.Run(ctx, listeners ...Listener)` where
`Listener{net.Listener; Tag reqctx.ListenerKind; AllowedHosts []string}` (Serve becomes a wrapper building its
listeners, so `porto serve` behaviour is unchanged), `App.NewHTTPServer(tag, hosts)` for the LAN server (same
timeouts, `ConnContext` tagging), `app.ErrRestart`; `meta.Get` takes the edition. `reqctx.ListenerKind`
(`loopback|lan|server`) helpers.

## 4. Pairing and Devices (LAN server side, WEB UI)

### 4.1 Flow (phone, target < 60 s)

1. Office user opens *Dispositivi* → "Abbina un dispositivo" → user (self, or another user with
   `devices.manage`) and kind → `createPairingCode`.
2. `reachable=false` (desktop, LAN off) → the dialog shows the "Attiva la rete locale" notice with a link to *App
   desktop* (owner on loopback) and no QR. `reachable=true` → QR of `pair_url`, code `XXXX-XXXX`, countdown,
   fingerprint (LAN), "Nuovo codice". The `pair_url` is also in `data-pair-url` for tests (never logged).
3. Phone camera → `https://192.168.1.20:8443/admin/pair#c=ABCD-EFGH&fp=…` → certificate warning once → `PairPage`
   (public route, `PlainLayout`, phone-first): reads the fragment, clears it from the address bar
   (`history.replaceState`), shows the fingerprint, a prefilled device name ("Telefono" + platform from the UA,
   editable), one "Collega" button; no fragment → code input.
4. `pairDeviceSession` → cookies set → `landingFor(permissions, kind)`: `presence.write` or `map.view` with
   kind `mobile` → `/admin/m/<id>/dock`; otherwise `/admin/m/<id>`.
5. Errors: `pairing_code_invalid` → "Codice scaduto o già usato: chiedi un nuovo QR"; network/TLS failure → help
   text about the Wi-Fi and the certificate warning.

### 4.2 *Dispositivi* page (`DevicesPage.tsx`, WEB)

Table: name, user, kind, platform, access (Browser / App), last used, created, state; revoke with confirm (for a
browser device the phone is logged out at its next request). SSE `team.changed` already refreshes the list.
Permissions unchanged (`devices.manage` list/revoke; `devices.pair_self` for own codes).

### 4.3 Local-mode explanations (WEB)

`LocalModeNotice` (shared pattern, `Alert` + link) rendered when `meta.local_mode`:
`PaymentSettingsPage` (online providers), `PaymentRequestsPage` (payment links are not reachable from outside),
`ContractSettingsPage` (remote signature: OTP to customers), `MarinaSettingsPage` (public page section), plus a
dashboard card once. Each links to `/admin/desktop/online` (**MoveOnlinePage**, readable by every role): what is
unavailable and why (no server reachable from the internet; INV-01: no sync), steps (backup now → reveal and store
the secret key → on the server `PORTO_SECRET_KEY=… porto restore --in FILE` then `porto serve` with an https
`PORTO_BASE_URL` → pair phones again → stop using the desktop copy). Owner-only actions are buttons on loopback,
plain text elsewhere.

### 4.4 Desktop pages (WEB; visible only when `meta.edition == 'desktop'` and `getDesktopStatus` answers, i.e. on loopback)

- `/admin/desktop/first-run` (`FirstRunPage`): BrandLogo, organization, name, email, password ×2, "controlla gli
  aggiornamenti" (checked), submit → `/admin/`; secondary "Ripristina da un backup" (upload).
- `/admin/desktop` (`DesktopSettingsPage`, owner; user-menu entry "App desktop"): cards *Rete locale* (switch,
  URL, addresses select, port, fingerprint, certificate expiry, rotate, errors, "phones work only while this app is
  open"), *Backup* (folder via text input with "open folder", retention, list with kind/size/date, backup now,
  restore with confirm and the pre-restore note), *Aggiornamenti* (switch, latest version, link), *Chiave
  segreta* (reveal with password dialog, copy, warning), *Dati* (open data folder, logs).
- `UpdateBanner` in `AppShell` (desktop loopback only, dismissal stored per version via `PATCH settings`).
- Downloads and print go through `web/shared/src/lib/download.ts` (`saveBlob`, `printPage`), which uses the bridge
  when present; WEB replaces the scattered `URL.createObjectURL` downloads (`features/maps/lib.ts`,
  `features/registry/lib/format.ts`, `features/contracts-m4/lib/files.ts` and any other `grep` hit) with it.

## 5. Packaging (DESKTOP)

### 5.1 Files

```
scripts/desktop/common.sh            version (scripts/version.sh), numeric MAJOR.MINOR.PATCH, names, ldflags, sha256
scripts/desktop/build.sh             launcher for GOOS/GOARCH -> dist/desktop/bin/<os>_<arch>/; DESKTOP_TAGS (""|webview)
scripts/desktop/package-macos.sh     arm64+amd64 webview builds (clang -arch), lipo, Porto.app, ad-hoc sign, .dmg (hdiutil)
scripts/desktop/package-windows.sh   Git Bash + MinGW: exe with windres resources, Inno Setup per-user installer
scripts/desktop/package-linux.sh     webview build + browser build, AppDir, AppRun, appimagetool (pinned + sha256)
scripts/desktop/mkico/main.go        pure Go .ico writer from the kit PNGs (no ImageMagick)
packaging/desktop/macos/Info.plist.in, entitlements.plist (hardened runtime, empty)
packaging/desktop/windows/porto.iss, porto.rc.in, porto.exe.manifest
packaging/desktop/linux/porto.desktop, AppRun, it.coste360.porto.metainfo.xml
.github/workflows/desktop.yml        PRs touching desktop paths + workflow_dispatch; macOS, Windows, Linux jobs
docs/desktop-build.md
```

Outputs in `dist/desktop/` (git-ignored): `Porto_<v>_macos_universal-unsigned.dmg`,
`Porto_<v>_windows_amd64_setup-unsigned.exe`, `Porto_<v>_linux_x86_64-unsigned.AppImage`, each with `.sha256`.
Icons built at package time from `assets/brand/porto/05_PNG_Icone/porto_app_navy_{64,128,256,512,1024}.png`
(`iconutil` for `.icns`, `mkico` for `.ico`, PNG for Linux); a Go test checks the copies byte-equal the kit.
Windows installs per user to `%LOCALAPPDATA%\Programs\Porto` (no admin), Start menu shortcut, uninstaller that
keeps the data dir. Linux `AppRun` starts `porto-desktop-webview` when `libwebkit2gtk-4.1.so.0` resolves
(`ldconfig -p`), else the bundled `porto-desktop` browser build. Reproducibility: `-trimpath -buildvcs=false
-ldflags "-s -w -buildid= -X main.version=<v>"`, `SOURCE_DATE_EPOCH` for the squashfs.

### 5.2 Signing hooks (documented, never faked)

| Hook (env) | Effect when set | When unset |
|---|---|---|
| `MACOS_SIGN_IDENTITY` | `codesign --options runtime --timestamp` with entitlements on the binary and the `.app`, then the `.dmg` | ad-hoc `codesign -s -` (required on Apple Silicon), suffix `-unsigned` |
| `MACOS_NOTARY_PROFILE` (needs the identity) | `xcrun notarytool submit --wait` + `stapler staple` | no notarisation; suffix `-unnotarized` if signed |
| `WINDOWS_SIGN_CMD` | command template run with the file path (signtool / AzureSignTool) on the exe and the installer | suffix `-unsigned` |
| `LINUX_SIGN_KEY` | `appimagetool --sign --sign-key` | suffix `-unsigned` |

A script never prints secrets, never uses a self-made certificate to look signed, and fails if a hook is set but
the tool fails. Distribution is from the Coste360 portal (31.2); CI uploads no artifacts.

## 6. Make targets and config (CONTRACT)

- `desktop-build` (`DESKTOP_TAGS` default empty → `CGO_ENABLED=0`; `webview` → `CGO_ENABLED=1`, native OS),
  `desktop-package-macos`, `desktop-package-windows`, `desktop-package-linux` (call the scripts; `SKIP_WEB_BUILD=1`
  skips `make web-build`), `desktop-test` (`go test ./internal/desktop/... ./internal/lan/...
  ./cmd/porto-desktop/...`), `e2e-desktop` (`make web-build`, `CGO_ENABLED=0 go build -o bin/porto-desktop
  ./cmd/porto-desktop`, `cd e2e && npx playwright test -c playwright.desktop.config.ts`). `check-full` adds
  `e2e-desktop`. `clean` removes `dist/desktop` and `bin/porto-desktop`. `.gitignore`: `dist/`.
- `make licenses` also runs `go tool go-licenses check ./cmd/porto-desktop --ignore $(MODULE)` with
  `GOFLAGS=-tags=webview` so `webview_go` is checked.
- `go.mod`: `github.com/zalando/go-keyring v0.2.8`, `github.com/webview/webview_go v0.0.0-20240831120633-6173450d4dd6`,
  `golang.org/x/sys` direct.
- `.golangci.yml`: depguard rule — only `cmd/porto-desktop` may import `github.com/webview/webview_go`;
  `internal/desktopport` imports stdlib and `internal/domain/auth` only; `internal/lan` and `internal/desktop` may
  not import `internal/store` directly (they go through `app`/`backup`/services).
- `.env.example` and `docs/desktop-build.md` list the desktop variables of §2.4.

## 7. Web structure (CONTRACT creates, WEB fills)

- `web/admin/src/router.tsx`: public route `/pair` (outside the auth guard) → `pages/PairPage.tsx`; factory
  `desktopRoutes` from `features/desktop/routes.tsx` (`/desktop`, `/desktop/first-run` outside the guard,
  `/desktop/online` inside). Stub components rendering a placeholder.
- `web/shared/src/desktop/bridge.ts` (types of §2.13, `isDesktopWindow()`), `web/shared/src/lib/download.ts`
  (signatures only), `web/shared/src/api/queryKeys.ts` keys `desktop.status|settings|lan|backups`.
- i18n: new namespace `desktop` (it/en, registered in `resources.ts`), new keys in `devices`, `errors`.
- shadcn components needed and not yet in `web/shared` are added by CONTRACT with the CLI (expected: `switch`
  if missing; check `web/shared/src/ui/` first).

## 8. Docs (CONTRACT unless noted)

ADR `docs/adr/0010-edizione-desktop-m6a.md` (Italian, "proposta"): in-process launcher; TOFU certificate with local
CA, no HTTP pairing page; browser pairing with device sessions (30/180 days); version check on by default,
switchable, never required; close = quit in M6a; SQLite only on desktop. `docs/invariants.md`: **INV-78** (a
pairing code creates at most one device, verified by `TestConcurrency_PairingCodeSingleUse` and
`TestConcurrency_PairSessionSingleUse`), **INV-79** (the LAN listener exists only while enabled and accepts only
private peers: `TestLANServer_OnlyWhenEnabled`, `TestPrivateListener_RejectsPublicPeers`), **INV-80** (the desktop
API answers only on the loopback listener from a loopback peer with an allowed Host:
`TestDesktopAPI_LoopbackOnly`, `TestHostCheck_RejectsRebinding`), status planned. `docs/architecture.md` §2/§9:
desktop packages and edition. `docs/desktop-build.md` is DESKTOP's.

## 9. Demo and seed

No seed change. e2e bootstraps with `PORTO_BOOTSTRAP_OWNER_*` and the API (§10.4).

## 10. Tests

### 10.1 DESKTOP (unit unless marked; all without a window)

`TestDataDir_PerOS` (table over goos/env), `TestDesktopConfig_Load` (dev-only variables refused outside dev, errors
never echo values), `TestServerEnv_Build`, `TestSettings_RoundTripPreservesUnknown`, `TestSettings_RejectsRemote`,
`TestSecret_ResolveOrder` (env > keychain > file; file migrates into keychain), `TestSecret_NeverRegeneratesForExistingDB`,
`TestInstanceLock_SecondLaunchActivates` (fake activation server), `TestFirstRunToken_SingleUse`,
`TestBackupName_ParseFormat`, `TestBackup_PruneCandidates` (property test with `rapid`: never removes manual,
pre-restore or the newest valid file; keeps exactly N per kind), `TestBackupScheduler_DailyDueAcrossRestarts`
(`clock.Fake`), `TestBackup_FallbackFolder`, `TestUpdate_DisabledMakesNoRequest`, `TestUpdate_FailureIsSilent`,
`TestUpdate_UserAgentHasNoIdentifiers`, `TestUpdate_NewerVersionOnly`, `TestLogFile_Rotation`,
`TestBrandIcons_MatchKit`; integration: `TestLauncher_StartStop` (fake `Window`, `FileStore`, temp dir: `/readyz`
200, first run pending, stop writes an exit backup and releases the lock), `TestLauncher_RestoreSwapsDatabase`
(pre-restore backup exists, live DB untouched on a corrupt archive), `TestConcurrency_FirstRunSingleOwner`
(100 goroutines, one token → exactly one user), `TestDesktopAPI_OwnerOnly` (director 403, owner 200),
`TestDesktopAPI_NilRuntime404` (server edition), `TestCLI_DesktopVersion` (`-version` output).

### 10.2 LAN

Unit: `TestPickAddress_Preferences`, `TestIsPrivatePeer` (table incl. mapped IPv6, CGNAT refused),
`TestLANCert_GenerateReloadRotate` (stable fingerprint across reload; re-issue on < 30 days, on address change and on
request; CA kept unless `all`), `TestLANCert_KeysEncryptedAtRest` (no `PRIVATE KEY` PEM on disk; another KeyRing
fails), `TestHostCheck_RejectsRebinding`, `TestRateLimit_PrivatePeersFullAddress`.
Integration (`testdb`, both dialects where DB is involved): `TestPrivateListener_RejectsPublicPeers`,
`TestLANServer_OnlyWhenEnabled` (no listener before enable/after disable, port busy → error),
`TestLANServer_PinnedClient` (pinned fp connects, wrong pin refused), `TestLANServer_DesktopAPINotReachable`
(404 over LAN even from a loopback peer), `TestDevice_PairURLFromTarget` (QR SVG equals `qrsvg.Render(pair_url)`;
`reachable=false` when LAN off), `TestDevice_PairSessionCreatesBrowserDevice`, `TestDeviceSession_Expiry`
(30 d idle / 180 d absolute with `clock.Fake`), `TestDeviceSession_RevokeKillsSession`,
`TestDeviceSession_TOTPUserMustVerify`, **`TestConcurrency_PairSessionSingleUse`** (100 goroutines, one code →
exactly one 201, 99 `pairing_code_invalid`, one `devices` row, one session row, one `device.paired` outbox event;
`-race`, SQLite and PostgreSQL), `TestConcurrency_PairingMixedEndpoints` (50 `pair` + 50 `pair-session` on one
code → one device), `TestLogs_NoPII` extended (pair-session, first run, LAN enable: no code, token, key, email in
captured logs), `TestEvents_DataHasNoPII` unchanged for `device.paired`. Acceptance
`internal/acceptance/m6a_desktop_test.go` (LAN owns): **`TestAcceptance_M6aLanPairing`** — in-process desktop
runtime (headless, `FileStore`, LAN on loopback with the dev flag): owner logs in on loopback, enables LAN,
creates a code for a dock user, pairs over LAN with a pinned TLS client, `GET /api/admin/v1/me` with the cookie
over LAN 200, desktop API over LAN 404, revoke → 401; `TestAcceptance_CrossMarinaIsolation` covers the new routes.

### 10.3 WEB (Vitest + Testing Library)

`PairPage` (fragment parsed and cleared, invalid code message, landing by permissions), `landingFor` table,
`DevicesPage` (QR from `pair_url`, unreachable notice, access column, revoke), `DesktopSettingsPage` (LAN switch,
backup list, reveal dialog), `FirstRunPage` validation, `LocalModeNotice` shown only when `local_mode`,
`download.ts` uses the bridge when present, i18n keys present in it and en.

### 10.4 Playwright (WEB): `e2e/playwright.desktop.config.ts`, `e2e/tests-desktop/*`, `e2e/support/desktop.ts`

webServer: `bin/porto-desktop -headless -data-dir <tmp>` with `PORTO_DEV=true`, `PORTO_DESKTOP_KEYRING=file`,
`PORTO_DESKTOP_PORT=18090`, `PORTO_DESKTOP_LAN_PORT=18443`, `PORTO_DESKTOP_LAN_LOOPBACK=true`,
`PORTO_UPDATE_CHECK=false`, `PORTO_DESKTOP_FIRST_RUN_FILE=true`; projects chromium desktop + `devices['Pixel 7']`.

- `first-run.spec.ts`: read `first-run.url`, create the owner in the window flow, create the marina, land on the
  dashboard; second visit of the URL → token invalid.
- **`lan-pairing.spec.ts`**: owner context on `http://127.0.0.1:18090`: enable LAN on *App desktop*, add a dock
  member (API helper), *Dispositivi* → pair for the dock user → read `data-pair-url`; new context
  `devices['Pixel 7']`, `ignoreHTTPSErrors: true` (stands for accepting the warning), `t0 = Date.now()`,
  `page.goto(pairUrl)`, tap "Collega", expect `dock-page` visible, assert `Date.now() - t0 < 60_000`; the URL bar
  has no fragment; owner revokes the device → phone reload lands on login. Also: with LAN off the dialog shows the
  notice and no QR.
- `local-mode.spec.ts`: `LocalModeNotice` on payment settings, move-online page reachable, update banner absent with
  the check off.

## 11. Ownership (CONTRACT first, then three builders in parallel)

### 11.1 CONTRACT (then frozen for M6a)

`api/openapi.yaml` + `make gen` outputs; `internal/migrations/{sqlite,postgres}/00013_device_sessions.sql`;
`internal/store/models.go` (fields only) and `internal/store` migration/backup registration if required by the
`new-migration` skill; `internal/desktopport/desktopport.go`; `internal/reqctx/listener.go`;
`internal/app/app.go` (Options, `Run`, `NewHTTPServer`, `ErrRestart`, wiring of desktop handler and
`PairTarget`), `internal/domain/meta/meta.go` (+ test); `internal/domain/auth/roles.go` (`IsOwnerAnywhere` only)
+ test; `internal/config/config.go` (`SecureCookies`) + test; `internal/http/server.go` (desktop handler
registration, `LoopbackOnly`/`HostCheck` hook points); `internal/http/problem/types.go`, `internal/http/errmap.go`
(new slugs); stubs: `internal/http/desktop/desktop.go`, `internal/http/device/pair_session.go`,
`internal/http/middleware/listener.go`, `internal/lan/doc.go`, `internal/desktop/doc.go`,
`cmd/porto-desktop/main.go` (prints version, exits); `Makefile`, `.gitignore`, `go.mod`/`go.sum`,
`.golangci.yml`, `.env.example`; web stubs of §7 (`router.tsx`, `features/desktop/*.tsx`, `pages/PairPage.tsx`,
`shared/src/desktop/bridge.ts`, `shared/src/lib/download.ts`, `queryKeys.ts`, i18n files and `resources.ts`, shadcn
adds); docs of §8 except `desktop-build.md`. CONTRACT ends with `make gen && make check` green.

### 11.2 Builders

| Builder | Owns (exclusive) |
|---|---|
| **DESKTOP** | `cmd/porto-desktop/**`; `internal/desktop/**`; `internal/http/desktop/**`; `scripts/desktop/**`; `packaging/desktop/**`; `.github/workflows/desktop.yml`; `docs/desktop-build.md`; `scripts/agents/check-naming.sh` (add the new paths) |
| **LAN** | `internal/lan/**`; `internal/domain/device/**`; `internal/domain/auth/device_session.go` (new) and the session hooks in `internal/domain/auth/service.go` (`newSession` gains a device/expiry parameter; no other change there); `internal/store/device_sessions.go` (new) + `internal/store/devices.go` / `sessions.go` query edits; `internal/http/device/**` (after the stub); `internal/http/middleware/{listener,hostcheck}.go` (new), `auth.go` (cookie Secure + device session checks only), `ratelimit.go` (private peers only); `internal/acceptance/m6a_desktop_test.go`; the device/session/LAN tests of §10.2 |
| **WEB** | `web/admin/src/pages/{PairPage,DevicesPage}.tsx`; `web/admin/src/features/desktop/**` (after the stub); `web/admin/src/lib/landing.ts` (new); `web/admin/src/components/AppShell.tsx` (UpdateBanner slot) and `UserMenu.tsx` (entry); the one-line `LocalModeNotice` inserts in `PaymentSettingsPage.tsx`, `PaymentRequestsPage.tsx`, `ContractSettingsPage.tsx`, `MarinaSettingsPage.tsx`, `DashboardPage.tsx`; download call sites (§4.4); `web/shared/src/desktop/**`, `web/shared/src/lib/download.ts`, `web/shared/src/ui/patterns/LocalModeNotice.tsx` (new), `web/shared/src/api/hooks/desktop.ts` (new); i18n values in `desktop`, `devices`, `errors` (it, en); `e2e/playwright.desktop.config.ts`, `e2e/tests-desktop/**`, `e2e/support/desktop.ts`; Vitest specs of §10.3 |

### 11.3 Rules for the parallel phase

- Interfaces in `internal/desktopport` and the OpenAPI are frozen; a builder that needs a change writes it in its
  report (the orchestrator reruns CONTRACT).
- DESKTOP composes `lan.New(...)` through `desktopport.LANController` only; until LAN lands, DESKTOP tests use a
  fake controller. LAN tests the controller with a fake `http.Handler`. `TestAcceptance_M6aLanPairing` builds the
  real app with `app.Options{Desktop: <minimal runtime>, PairTarget: lanServer.PairTarget}` where the minimal
  runtime is `internal/desktop/desktoptest.NewRuntime(...)` (DESKTOP delivers it first, as a priority, with only
  `Status`, `Settings` owner check and `LAN()`); LAN does not import other `internal/desktop` packages.
- WEB works against the generated client and MSW-style mocks; Playwright specs run once DESKTOP's headless
  launcher and LAN's listener exist (`make e2e-desktop`).
- Shared files are edited only at the listed spots; `internal/domain/auth/service.go` changes are limited to
  session creation/lookup.

## 12. Acceptance (exact commands, repo root)

```bash
make setup
make gen && git status --porcelain internal/http/api web/shared/api/generated              # empty
make check                                                     # gen-check + lint + unit, no webview toolchain needed
CGO_ENABLED=0 go build ./... && make build && go version -m bin/porto | grep -q 'CGO_ENABLED=0'
CGO_ENABLED=0 go build -o bin/porto-desktop ./cmd/porto-desktop && bin/porto-desktop -version   # "(browser)"
make desktop-test
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/migrations/... -run 'TestMigrations_'
scripts/with-postgres.sh go test -tags=integration -count=1 ./internal/lan/... ./internal/desktop/... ./internal/domain/device/... ./internal/domain/auth/... ./internal/http/... ./internal/store/...
scripts/with-postgres.sh go test -race -tags=integration -count=1 -run '^TestConcurrency_(PairingCodeSingleUse|PairSessionSingleUse|PairingMixedEndpoints|FirstRunSingleOwner)$' ./internal/...
make test-db && make test-race
scripts/with-postgres.sh go test -tags=integration -count=1 -timeout 40m ./internal/acceptance/... -run 'TestAcceptance_M6a|TestAcceptance_M0|TestAcceptance_CrossMarina|TestLogs_NoPII|TestEvents_DataHasNoPII'
make e2e && make e2e-desktop                                   # incl. lan-pairing < 60 s on Pixel 7 viewport
make licenses && scripts/agents/check-naming.sh
# macOS host with Xcode CLT:
make desktop-build DESKTOP_TAGS=webview && dist/desktop/bin/darwin_arm64/porto-desktop -version         # "(window)"
SKIP_WEB_BUILD=1 make desktop-package-macos && ls dist/desktop/Porto_*_macos_universal-unsigned.dmg*
# Linux host: make desktop-package-linux; Windows (Git Bash, MinGW, Inno Setup): make desktop-package-windows
```

Manual check (recorded in the final report): fresh macOS user, open the `.dmg`, right-click → Open, first run to
the dashboard in < 10 minutes without a terminal; enable the local network; a real phone on the same Wi-Fi scans
the QR, accepts the warning and sees the dock panel in < 60 s; quit → an `exit` backup exists; relaunch while
running → the window comes to the front.

## 13. Decisions taken where the spec is silent

| # | Decision |
|---|---|
| D1 | Server **in-process**, not a child process (§2.1). |
| D2 | Loopback port remembered in `desktop.json`, first one picked by the OS; LAN port fixed 8443 by default (configurable) so the phone origin is stable. |
| D3 | Desktop edition uses **SQLite only** (`DATABASE_URL` ignored with a warning); PostgreSQL is for server installs (31.1). |
| D4 | Windows data in `%LOCALAPPDATA%` (not roaming). |
| D5 | Keychain via `go-keyring` (no CGO) with a 0600 file fallback and a visible warning; a key is never generated for an existing database. |
| D6 | First run creates the owner and organization in the window (token in the URL fragment, single use); the marina is created by the existing panel flow. First run can instead restore a backup. |
| D7 | Phone trust: **TOFU** on the self-signed leaf with the fingerprint in the QR; no HTTP pairing page; a local CA is generated now for M6b CA-profile installs (§2.10). |
| D8 | Browser phones authenticate with a **device-bound session cookie** created by `pair-session` (single use of the same code path); device tokens stay for native/remote clients. |
| D9 | Device sessions: idle 30 days, absolute 180 days; TOTP users verify once on the phone. |
| D10 | QR encodes an `https://…/admin/pair#c=…&fp=…` URL (camera-openable); `porto://` payload kept in the API for native clients. |
| D11 | Rate limits key private/loopback peers by full address (a marina shares one /24). |
| D12 | No mDNS in M6a. |
| D13 | Leaf certificate 397 days, re-issued < 30 days before expiry, on address change or on request; CA 10 years; ECDSA P-256; private keys KeyRing-encrypted at rest. |
| D14 | Backups: existing portable format, daily (≥ 24 h since the newest daily, checked every 15 min) + on exit + manual + pre-restore; keep 14 daily and 5 exit; manual/pre-restore never pruned; fallback folder when the chosen one is unavailable. |
| D15 | Restore swaps the database and file store after a safety backup and restarts the in-process server; the window stays open. |
| D16 | Version check **on by default** (spec: "disattivabile"), off in first run/settings/`PORTO_UPDATE_CHECK=false`; daily, default URL `https://download.coste360.it/porto/desktop/latest.json` (to be confirmed and published by Coste360), only version/OS in the User-Agent, notify only, never install. |
| D17 | Closing the window quits the app on every OS in M6a; tray and keep-running are M6b. |
| D18 | Downloads/print/external links in the window through a small JS bridge (`window.portoDesktop`); the browser keeps native behaviour. |
| D19 | Desktop administration (settings, LAN, backups, restore, secret key) is **owner only** and **loopback only**; no new permission constant (`IsOwnerAnywhere`). |
| D20 | Linux AppImage ships a WebKitGTK 4.1 build and a browser build; `AppRun` chooses. Windows without WebView2 falls back to the browser. |
| D21 | Packaging targets: macOS universal `.dmg`, Windows x64 per-user Inno Setup installer, Linux x86_64 AppImage, all `-unsigned` unless the hooks are set; Windows arm64 and thin macOS builds deferred. |
| D22 | Owner password recovery offline is not solved in M6a (reset needs SMTP); listed for M6b. Desktop mail defaults to the file transport in `<data>/mail`. |
| D23 | In local mode the panel explains unavailable features (public page, online payments and payment links, remote signature) with `LocalModeNotice` and a move-online guide; the features are not removed from the API. |
