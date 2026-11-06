# M0 — Fondamenta: work order

Status: Plan phase output. Authoritative for the M0 builders. If this plan conflicts with `docs/spec/`, the spec wins:
stop and report the conflict to the orchestrator instead of guessing.

Sources: spec 03, 04, 06 (§6.1, §6.9), 26, 27, 28, 29, 30, 31 (§31.4), 32 (§32.1–32.3); ADR 0001, 0007;
`docs/invariants.md`; `AGENTS.md`.

## 0. Ground rules for every builder

1. **Arenile is a reference, not a source** (ADR 0001). You may read `../arenile`
   to see how a problem was solved. You must not copy its files or large chunks of its code, and you must never
   modify anything under it. Porto code uses Porto names: `marina` (never `establishment`), `porto_*` cookies,
   `PORTO_*` env vars, `po_live_` / `po_dev_` tokens, `Porto-Signature`. A CI grep rejects the words `arenile`,
   `establishment`, `ARENILE_` under `cmd/ internal/ pkg/ api/ web/*/src deploy/` (see §11).
2. shadcn/ui components are added **only** with the official CLI (`npx shadcn@latest add …`), never copied.
3. Do not `git commit`, do not run destructive git commands (`reset --hard`, `checkout -- .`, `clean -fd`, `push`).
   The orchestrator commits.
4. Only edit files you own (§10). If you need a change in a file someone else owns, write the request in your final
   report. Do not edit the file.
5. Code, identifiers and comments are in English. Every user-facing string goes through i18n keys (Italian is the
   source language, English is required). Never log emails, phone numbers, tokens, secrets or passwords.
6. No floats in domain logic. Money is `money.Cents`, lengths are `geo.Cm`, fractional quantities are `qty.Milli`,
   calendar dates are `calendar.Date`, time comes from `clock.Clock`.
7. New dependencies must be MIT, BSD-2/3, Apache-2.0, ISC or MPL-2.0. The only other licence allowed is OFL-1.1, and
   only for font files (Titillium Web, ADR 0007). Every dependency listed in this plan already passes that check.
   Anything else needs the orchestrator's approval.
8. Go version: **1.27.2** (same as `arenile/go.mod`). Node 22 LTS. golangci-lint **v2.15.0**.

---

## 1. Package layout (M0)

M0 only creates the packages below (subset of spec 04). Paths are relative to the repository root.

```
cmd/porto/                      main.go, commands.go (CLI)
internal/app/                   composition root
internal/config/                PORTO_* parsing and validation
internal/clock/ money/ calendar/ reqctx/ crypto/ geo/ qty/   pure/shared primitives (BOOTSTRAP)
internal/http/                  server.go (router assembly), errmap.go (domain error -> problem)
internal/http/problem/          RFC 9457 writer (BOOTSTRAP)
internal/http/api/              GENERATED from api/openapi.yaml (api.gen.go)
internal/http/middleware/       request id, real ip, access log, recover, security headers, body limit,
                                authenticate, require auth, CSRF, TOTP gate, rate limits, idempotency
internal/http/admin/            /api/admin/v1 handlers
internal/http/device/           /api/device/v1 handlers
internal/http/public/           /api/public/v1/{slug} handlers (M0: marina info only)
internal/http/sse/              SSE endpoint + ReplayBus
internal/http/static/           embed.FS of admin/booking SPAs, placeholder page, favicon/manifest
internal/domain/auth/           users, passwords (Argon2id), sessions, TOTP, recovery codes, password reset,
                                principals, roles & permissions, login throttle
internal/domain/team/           memberships, invitations, last-owner protection
internal/domain/marina/         organizations, marinas (create, update with If-Match, archive)
internal/domain/apikey/         API keys
internal/domain/device/         pairing codes, device tokens, QR payload
internal/domain/audit/          audit writer (insert-only)
internal/domain/event/          event types, outbox writer, dispatcher, in-memory broker, egress client, signing
internal/domain/webhook/        webhook endpoints, deliveries, deliverer, retention
internal/domain/notify/         email queue (message_log), templates, transports (smtp/file/log)
internal/domain/meta/           version and capabilities
internal/store/                 Bun repositories: the ONLY package with SQL
internal/migrations/            embed + goose runner; sqlite/00001_init.sql, postgres/00001_init.sql
internal/jobs/                  in-process job runner + core jobs
internal/backup/                portable backup/restore (JSONL per table, both dialects)
internal/testutil/testdb/       test helper: run a test on SQLite (always) and PostgreSQL (if URL set)
internal/acceptance/            M0 system acceptance test (tag integration), API only
pkg/ext/                        public extension interfaces (M0 subset)
api/openapi.yaml, api/oapi-codegen.yaml
web/{shared,admin,booking}/     npm workspaces
e2e/                            Playwright
```

### 1.1 Dependency rules (enforced by `.golangci.yml` depguard + tests)

| Package | May import | Must not import |
|---|---|---|
| `internal/{clock,money,calendar,reqctx,geo,qty}` | stdlib; `qty` may import `money` | anything else in `internal/`, `bun`, `net/http` |
| `internal/crypto` | stdlib (`crypto/*`, `crypto/hkdf`) | `internal/*` |
| `internal/http/problem` | stdlib, `internal/reqctx` | `internal/http/api`, `internal/domain/*`, `internal/store` |
| `internal/domain/*` | primitives, `internal/store`, other domain pkgs (no cycles), `pkg/ext`, `bun` (only `bun.IDB`/`bun.Tx` types) | `net/http` (exception: `domain/event` egress client and `domain/notify` SMTP transport), `internal/http/*`, SQL driver packages |
| `internal/http/*` | `internal/http/api`, `internal/http/problem`, `internal/domain/*`, primitives | `internal/store` (exception: `internal/http/middleware/idempotency.go` uses store through an interface defined in the middleware, implemented in `store`), raw SQL |
| `internal/store` | `bun`, dialects, drivers, `internal/migrations`, primitives | `internal/domain/*`, `internal/http/*` |
| `pkg/ext` | stdlib, `chi` (for `HTTPRoutes`) | anything in `internal/` |
| pure packages (`geo`, `qty`, later `layout`, `fit`, `pricing`, `accrual`, `formula`, `optimizer`) | — | `internal/store`, `internal/clock`, `time.Now` (forbidigo) |

`.golangci.yml` (BOOTSTRAP) must already list the rules for the future pure packages (`internal/domain/{layout,fit,pricing,accrual,formula,optimizer}/**`), so INV-21 holds as soon as those packages exist.

### 1.2 Public API per package

Signatures are normative: builders may add helpers, but must not change these names or shapes without telling the
orchestrator.

#### BOOTSTRAP primitives

```go
// internal/clock
type Clock interface{ Now() time.Time }           // always UTC
type System struct{}                               // System{}.Now()
type Fake struct{ /* mutex */ }
func NewFake(t time.Time) *Fake
func (f *Fake) Now() time.Time
func (f *Fake) Advance(d time.Duration)
func (f *Fake) Set(t time.Time)

// internal/money
type Cents int64
func (c Cents) String() string                     // "-12.34" canonical, '.' separator
func ParseCents(s string) (Cents, error)           // "12", "12.3", "12,30", "-0.05"; >2 decimals -> error
func DivRoundHalfEven(num, den int64) int64        // den != 0, panics on den == 0 (programming error)
func (c Cents) MulRat(num, den int64) Cents        // half-even, 128-bit intermediate
func (c Cents) PercentBP(bp int64) Cents           // basis points, half-even
func VATSplit(gross Cents, rateBP int64) (net, vat Cents)  // net+vat == gross
func Allocate(total Cents, weights []int64) ([]Cents, error) // largest remainder; sum == total exactly
var ErrInvalidAmount, ErrOverflow error

// internal/calendar
type Date struct{ /* unexported time.Time at 00:00 UTC */ }
func New(y int, m time.Month, d int) Date
func Parse(s string) (Date, error)                 // strict YYYY-MM-DD
func MustParse(s string) Date
func In(t time.Time, loc *time.Location) Date      // calendar date of instant t in marina timezone
func (d Date) String() string; IsZero; Year; Month; Day; AddDays(n int) Date
func (d Date) Before(o Date) bool; After; Equal; Compare(o Date) int
func (d Date) DaysUntil(o Date) int; ISOWeekday() int
func Range(from, to Date) []Date                   // inclusive
type Nights struct{ First, Last Date }              // occupancy unit = night (ADR 0002), inclusive
func NightsBetween(arrival, departure Date) (Nights, error) // departure > arrival; Last = departure-1
func (n Nights) Count() int; Contains(d Date) bool; Each() []Date; Overlaps(o Nights) bool
// JSON (string), Text, sql.Scanner/driver.Valuer (SQLite TEXT, PostgreSQL date/time.Time) for Date.
func LoadLocation(name string) (*time.Location, error) // wraps time.LoadLocation; binary imports time/tzdata

// internal/reqctx
type AuthMethod string // "none" | "session" | "api_key" | "device"
type Info struct {
    RequestID  string
    ClientIP   netip.Addr
    Scheme     string // http|https
    Method     string
    Route      string // chi route pattern, not raw path
    UserAgent  string // truncated to 256
    AuthMethod AuthMethod
}
func With(ctx context.Context, info *Info) context.Context
func From(ctx context.Context) *Info               // never nil (returns zero Info)

// internal/crypto
type KeyRing struct{ /* keys[0] active */ }
func ParseKeyRing(s string) (*KeyRing, error)      // PORTO_SECRET_KEY: comma-separated base64 (std or url) 32-byte keys
func GenerateKey() (string, error)                 // base64 32 bytes
func (k *KeyRing) Encrypt(plaintext, aad []byte) (string, error)   // "v1.<keyid8>.<b64url(nonce|ct)>", AES-256-GCM
func (k *KeyRing) Decrypt(ciphertext string, aad []byte) ([]byte, error)
func (k *KeyRing) EncryptString(s string, aad string) (string, error)
func (k *KeyRing) DecryptString(c string, aad string) (string, error)
func (k *KeyRing) NeedsRotation(ciphertext string) bool
func (k *KeyRing) Derive(purpose string, n int) ([]byte, error)    // HKDF-SHA256(active key, info=purpose)
func RandomToken(nBytes int) (string, error)       // base64url, no padding
func RandomBase32(n int) (string, error)           // Crockford alphabet, uppercase
func SHA256Hex(s string) string
func ConstantTimeEqual(a, b string) bool
var ErrInvalidKey, ErrNoKeys, ErrUnknownKey, ErrMalformed, ErrDecrypt error

// internal/http/problem — see §4.4 for the full Type list
type Type string
type FieldError struct{ Field, Code, Message string } // json: field, code, message
type Problem struct {
    Type      Type         `json:"type"`
    Title     string       `json:"title"`
    Status    int          `json:"status"`
    Detail    string       `json:"detail,omitempty"`
    Instance  string       `json:"instance,omitempty"`
    RequestID string       `json:"request_id,omitempty"`
    Errors    []FieldError `json:"errors,omitempty"`
    RetryAfterSeconds int  `json:"retry_after_seconds,omitempty"`
}
type Option func(*Problem)
func Detail(s string) Option; Fields(f ...FieldError) Option; RetryAfter(d time.Duration) Option; Status(code int) Option
func New(t Type, opts ...Option) Problem
func Write(w http.ResponseWriter, r *http.Request, t Type, opts ...Option) // Content-Type application/problem+json; sets Retry-After
func StatusOf(t Type) int
type Mapper func(err error) (Type, []Option, bool)
func WriteError(w http.ResponseWriter, r *http.Request, err error, m Mapper, log *slog.Logger) // unmapped -> internal_error, logged with request id
```

`internal/geo` and `internal/qty` are specified in §9.

#### BACKEND packages

```go
// internal/config — §5
type Config struct { /* fields in §5 */ }
func Load(env func(string) (string, bool)) (*Config, error)   // pure; errors.Join of all problems
func FromEnviron() (*Config, error)

// internal/store
type Dialect = migrations.Dialect
type Options struct{ PostgresURL, SQLitePath string; SQLiteMemory bool }
type DB struct{ *bun.DB; Kind Dialect }
func Open(ctx context.Context, o Options) (*DB, error)        // SQLite: WAL, busy_timeout=10000, foreign_keys=1, _txlock=immediate
func (db *DB) RunInTx(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error // SQLite: in-process write gate; retries 40001/40P01/SQLITE_BUSY up to 3x
func NewID() string                                            // UUIDv7 string
type JSON []byte                                               // Valuer/Scanner for TEXT/jsonb
type OrgScope struct{ /* unexported orgID */ }; func (s OrgScope) ID() string
type MarinaScope struct{ /* unexported marinaID, orgID */ }; func (s MarinaScope) ID() string; OrgID() string
func ResolveMarinaScope(ctx context.Context, db bun.IDB, marinaID string) (MarinaScope, error) // only called by auth.Principal.Authorize; ErrNotFound if missing/archived
func ResolveOrgScope(ctx context.Context, db bun.IDB, orgID string) (OrgScope, error)
type Page[T any] struct{ Items []T; NextCursor string }
var ErrNotFound, ErrConflict, ErrVersionMismatch error
func IsUniqueViolation(err error) bool; IsSerializationFailure(err error) bool
func LockOrganization(ctx context.Context, tx bun.IDB, orgID string) error // PG: SELECT … FOR UPDATE; SQLite: no-op (immediate tx)
// One file per area: models.go, organizations.go, marinas.go, users.go, memberships.go, sessions.go, totp.go,
// password_resets.go, apikeys.go, devices.go, pairing.go, idempotency.go, audit.go, outbox.go, webhooks.go,
// messages.go, backup_tables.go (ordered table registry for internal/backup).
// Every operational function takes MarinaScope (or OrgScope), never a raw marina id from the request.

// internal/migrations
type Dialect string; const SQLite Dialect = "sqlite"; const Postgres Dialect = "postgres"
type Status struct{ Version int64; Name string; Applied bool; AppliedAt string }
type Runner struct{}
func NewRunner(db *sql.DB, d Dialect) (*Runner, error)
func (r *Runner) Up(ctx) ([]int64, error); Status(ctx) ([]Status, error); Aligned(ctx) (bool, error); Version(ctx) (int64, error)
func FS(d Dialect) (fs.FS, error)

// internal/domain/auth
type Role string        // §3
type Permission string  // §3
func (r Role) Valid() bool; func (p Permission) Valid() bool
func Can(r Role, p Permission) bool
func RolePermissions(r Role) []Permission
type PrincipalKind string // "user" | "api_key" | "device"
type Grant struct{ OrganizationID, MarinaID string; Role Role } // MarinaID "" = whole organization
type Principal struct {
    Kind PrincipalKind
    UserID, SessionID, APIKeyID, DeviceID string
    Grants []Grant
    Scopes []Permission          // api keys only: intersection applied in Can
    TOTPEnrolmentRequired bool   // session limited to enrolment endpoints
    Locale string
}
func (p *Principal) Can(orgID, marinaID string, perm Permission) bool
func (p *Principal) Authorize(ctx context.Context, db bun.IDB, marinaID string, perm Permission) (store.MarinaScope, error) // ErrNotFound if no grant on that marina (no existence leak), ErrForbidden if grant lacks perm
func (p *Principal) AuthorizeOrg(ctx context.Context, db bun.IDB, orgID string, perm Permission) (store.OrgScope, error)
func (p *Principal) Actor() audit.Actor
func WithPrincipal(ctx context.Context, p *Principal) context.Context
func PrincipalFrom(ctx context.Context) (*Principal, bool)
type Hasher struct{ Params Argon2Params } // Argon2id m=64MiB t=3 p=2 keyLen=32 salt=16; PHC string
func (h *Hasher) Hash(pw string) (string, error); Verify(pw, phc string) (bool, error); NeedsRehash(phc string) bool
func ValidatePassword(pw string) error             // 12..256 runes, not all same char; ErrWeakPassword
type Service struct{}
func NewService(db *store.DB, c clock.Clock, keys *crypto.KeyRing, aw *audit.Writer, n Mailer, log *slog.Logger, o Options) (*Service, error)
type Mailer interface{ EnqueuePasswordReset(ctx context.Context, tx bun.IDB, in notify.PasswordResetMail) error; EnqueueInvitation(ctx context.Context, tx bun.IDB, in notify.InvitationMail) error }
func (s *Service) Login(ctx, LoginInput{Email, Password, TOTPCode, RecoveryCode string}) (*LoginResult{Token string; Principal *Principal; ExpiresAt time.Time}, error)
func (s *Service) Logout(ctx, token string) error
func (s *Service) AuthenticateSession(ctx, token string) (*Principal, error)  // sliding idle expiry
func (s *Service) AuthenticateAPIKey(ctx, key string) (*Principal, error)     // po_live_
func (s *Service) AuthenticateDevice(ctx, token string) (*Principal, error)   // po_dev_, grants narrowed to device marina
func (s *Service) ChangePassword(ctx, p *Principal, current, next string) error // revokes other sessions
func (s *Service) UpdateProfile(ctx, p *Principal, name, locale *string) error
func (s *Service) BeginTOTPEnrolment(ctx, p *Principal) (*TOTPEnrolment{Secret, OTPAuthURL, QRSVG string}, error)
func (s *Service) ConfirmTOTP(ctx, p *Principal, code string) (recoveryCodes []string, newToken string, err error) // rotates session
func (s *Service) DisableTOTP(ctx, p *Principal, password, code string) error // ErrTOTPRequiredByPolicy
func (s *Service) RegenerateRecoveryCodes(ctx, p *Principal, code string) ([]string, error)
func (s *Service) RequestPasswordReset(ctx, email string) error // always nil to caller except rate limit; no enumeration
func (s *Service) ConfirmPasswordReset(ctx, token, newPassword string) error // single use; revokes all sessions
func (s *Service) CreateOwner(ctx, CreateOwnerInput{Email, Name, Password, Organization, MarinaName, MarinaSlug, Timezone, Locale string}) (*CreateOwnerResult, error) // CLI + bootstrap
func (s *Service) CleanupExpired(ctx) (int64, error) // sessions, reset tokens
type LoginThrottle struct{}; func NewLoginThrottle(c clock.Clock) *LoginThrottle
func (t *LoginThrottle) Check(ip netip.Addr, account string) (ok bool, retryAfter time.Duration)
func (t *LoginThrottle) Failure(ip netip.Addr, account string); Success(account string)
// errors: ErrInvalidCredentials, ErrTOTPRequired, ErrInvalidTOTP, ErrTOTPAlreadyEnabled, ErrTOTPNotEnrolled,
// ErrTOTPRequiredByPolicy, ErrUnauthenticated, ErrForbidden, ErrNotFound, ErrRateLimited (RateLimitError{RetryAfter}),
// ErrWeakPassword, ErrUserExists, ErrInvalidResetToken, ErrInvalidInput (ValidationError{Fields})
// TOTP: RFC 6238 SHA1, 6 digits, 30 s, window ±1 step, replay blocked via last_step. Implemented in-house (no dependency).
// Recovery codes: 10 codes, format XXXX-XXXX (Crockford base32), stored SHA-256, single use.

// internal/domain/team
func NewService(db *store.DB, c clock.Clock, auth *auth.Service, aw *audit.Writer, ob *event.Outbox, log *slog.Logger) *Service
func (s *Service) ListMembers(ctx, p *auth.Principal, marinaID string) ([]Member, error)
func (s *Service) AddMember(ctx, p, marinaID string, in AddMemberInput{Email, Name, Locale string; Role auth.Role}) (*Member, error) // creates user (no usable password, invitation mail) or reuses existing
func (s *Service) ChangeRole(ctx, p, marinaID, userID string, role auth.Role) (*Member, error)
func (s *Service) RemoveMember(ctx, p, marinaID, userID string) error
func (s *Service) SendPasswordReset(ctx, p, marinaID, userID string) error
func (s *Service) ListOrgOwners(ctx, p, orgID string) ([]Member, error)
func (s *Service) AddOrgOwner(ctx, p, orgID string, in AddMemberInput) (*Member, error)
func (s *Service) RemoveOrgOwner(ctx, p, orgID, userID string) error // ErrLastOwner
// Last-owner rule: an organization always keeps >= 1 org-level owner with an active user (LockOrganization in tx).

// internal/domain/marina
func NewService(db, clock, aw, ob, log) *Service
func (s *Service) ListOrganizations(ctx, p) ([]Organization, error)
func (s *Service) UpdateOrganization(ctx, p, orgID string, ifMatch int, in OrganizationPatch) (*Organization, error)
func (s *Service) ListMarinas(ctx, p, orgID string) ([]Marina, error)
func (s *Service) CreateMarina(ctx, p, orgID string, in CreateMarinaInput) (*Marina, error)
func (s *Service) GetMarina(ctx, p, marinaID string) (*Marina, error)
func (s *Service) UpdateMarina(ctx, p, marinaID string, ifMatch int, in MarinaPatch) (*Marina, error) // ErrVersionMismatch
func (s *Service) ArchiveMarina(ctx, p, marinaID string) error
func (s *Service) PublicBySlug(ctx, slug string) (*PublicMarina, error)
func ValidateSlug(s string) error // ^[a-z0-9]+(-[a-z0-9]+)*$, 3..63, reserved: admin, api, b, static, assets, healthz, readyz, webhooks

// internal/domain/apikey
func (s *Service) List(ctx, p, marinaID string) ([]APIKey, error)
func (s *Service) Create(ctx, p, marinaID string, in CreateInput{Name string; Scopes []auth.Permission; ExpiresAt *time.Time}) (secret string, key *APIKey, err error)
func (s *Service) Revoke(ctx, p, marinaID, keyID string) error
// Format: "po_live_" + 8-char lowercase base32 prefix + "_" + 32 random bytes base64url. Stored: prefix, SHA-256 hash.
// Scopes must be a subset of the creator's permissions and must not include users.manage.

// internal/domain/device
func (s *Service) CreatePairingCode(ctx, p, marinaID string, in PairingInput{UserID string; Kind string; TTL time.Duration}) (*PairingCode{Code, QRPayload, QRSVG string; ExpiresAt time.Time}, error)
func (s *Service) Pair(ctx, in PairInput{Code, Name, Platform, Kind, AppVersion string}) (*PairResult{Token string; Device Device; MarinaID, UserID string}, error) // single use under concurrency
func (s *Service) DemoPair(ctx, in PairInput) (*PairResult, error) // only if PORTO_DEMO_PAIRING_* set
func (s *Service) List(ctx, p, marinaID string) ([]Device, error)
func (s *Service) Revoke(ctx, p, marinaID, deviceID string) error
func (s *Service) RevokeSelf(ctx, p *auth.Principal) error
// Code: 8 chars Crockford base32 shown as XXXX-XXXX, TTL default 10 min (max 60), stored SHA-256.
// QR payload: "porto://pair?v=1&url=<urlencoded PORTO_BASE_URL>&code=<code>". QR SVG built from github.com/skip2/go-qrcode bitmap (MIT) by device/qrsvg.go.
// Token: "po_dev_" + 8-char prefix + "_" + 32 bytes base64url.

// internal/domain/audit
type Actor struct{ Type string /* user|api_key|device|system */; ID string }
type Entry struct{ OrganizationID, MarinaID string; Actor Actor; Action, EntityType, EntityID string; Diff any }
type Writer struct{}
func NewWriter(c clock.Clock) *Writer
func (w *Writer) Write(ctx context.Context, tx bun.IDB, e Entry) error // request id + ip from reqctx; Diff redacted by Redact
func Redact(v any) any // drops keys: password*, *token*, *secret*, email, phone, totp*, code; recursively
func (w *Writer) List(ctx, db bun.IDB, s store.MarinaScope, f Filter, cursor string, limit int) (store.Page[store.AuditEntry], error)
// Domain services call audit.Writer.Write and event.Outbox.Write inside the same RunInTx as the change.

// internal/domain/event
const ( // M0 event types (version 1)
    OrganizationUpdated = "organization.updated"; MarinaCreated = "marina.created"; MarinaUpdated = "marina.updated"
    MarinaArchived = "marina.archived"; MemberAdded = "member.added"; MemberRoleChanged = "member.role_changed"
    MemberRemoved = "member.removed"; APIKeyCreated = "api_key.created"; APIKeyRevoked = "api_key.revoked"
    DevicePaired = "device.paired"; DeviceRevoked = "device.revoked"; WebhookTest = "webhook.test"
)
func Types() []TypeInfo // {Type, Topic, Public bool}; Public = can be subscribed by webhooks
type Outbox struct{}; func NewOutbox(c clock.Clock) *Outbox
func (o *Outbox) Write(ctx context.Context, tx bun.IDB, marina *store.MarinaScope, typ string, data any) (ext.Event, error) // data: ids only, PII check in tests
type Dispatcher struct{}
func NewDispatcher(db *store.DB, c clock.Clock, sinks []ext.EventSink, bus ext.EventBus, log *slog.Logger) *Dispatcher
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) // claim with lease (locked_until), PG FOR UPDATE SKIP LOCKED, mark dispatched_at; failing sink -> attempts++, backoff
type Broker struct{} // in-memory ext.EventBus, per-marina fan-out, non-blocking (drop + counter)
func NewBroker(log *slog.Logger, buffer int) *Broker
func (b *Broker) Publish(ctx, ev ext.Event) error; Subscribe(ctx, marinaID string) (<-chan ext.Event, func(), error)
type EgressPolicy struct{ AllowPrivate bool; AllowHTTP bool }
func (p EgressPolicy) CheckAddr(a netip.Addr) error // rejects loopback, private, link-local, CGNAT 100.64/10, ULA fc00::/7, multicast, unspecified, 0.0.0.0/8, 169.254.169.254, IPv4-mapped forms
func (p EgressPolicy) CheckURL(u *url.URL) error    // https only unless AllowHTTP; no userinfo; port any
func NewEgressClient(p EgressPolicy, timeout time.Duration) *http.Client // DialContext Control re-checks the resolved IP; no redirects; no proxy from env
func Sign(secret string, body []byte, at time.Time) string // "t=<unix>,v1=<hex HMAC-SHA256(secret, "<unix>.<body>")>"
func SignAll(secrets []string, body []byte, at time.Time) string // one v1= per secret (rotation)
func VerifySignature(secret, header string, body []byte, now time.Time, tolerance time.Duration) error
func Backoff(failedAttempts int) time.Duration // 30s * 2^(n-1), cap 6h

// internal/domain/webhook
func (s *Service) List/Get/Create/Update/Delete(ctx, p, marinaID, …)  // Create returns secret once ("whsec_" + 32 bytes base64url)
func (s *Service) RotateSecret(ctx, p, marinaID, endpointID string) (secret string, err error) // previous valid 24h
func (s *Service) SendTest(ctx, p, marinaID, endpointID string) (deliveryID string, err error)
func (s *Service) ListDeliveries(ctx, p, marinaID, endpointID, cursor string, limit int) (store.Page[Delivery], error)
func (s *Service) Redeliver(ctx, p, marinaID, endpointID, deliveryID string) error
type FanOutSink struct{} // ext.EventSink: inserts webhook_deliveries for matching active endpoints (ON CONFLICT DO NOTHING)
type Deliverer struct{}
func (d *Deliverer) DeliverDue(ctx context.Context, limit int) (int, error)
// Headers: Content-Type: application/json; Porto-Signature; Porto-Event: <type>; Porto-Delivery: <delivery id>; User-Agent: Porto-Webhooks/<version>.
// Body = event envelope (§4.5). Max payload 256 KiB. Timeout 10 s. 2xx = delivered. Give up after 24 h from first attempt.
// Disable endpoint after 20 consecutive failed deliveries (audit + disabled_reason). Response excerpt <= 1 KiB, sanitized.
func (s *Service) PurgeOld(ctx context.Context, olderThan time.Duration) (int64, error) // retention 30 days

// internal/domain/notify
type Transport interface{ Send(ctx context.Context, m Message) (providerID string, err error) }
func NewSMTPTransport(rawURL string, from string, timeout time.Duration) (Transport, error) // smtp:// (STARTTLS if offered, required when auth), smtps://, user:pass in URL
func NewFileTransport(dir string) Transport  // writes RFC 5322 .eml files (PORTO_MAIL_DIR)
func NewLogTransport(log *slog.Logger) Transport // logs template + recipient hash only
type Service struct{}
func NewService(db *store.DB, c clock.Clock, keys *crypto.KeyRing, t Transport, baseURL string, log *slog.Logger) *Service
type PasswordResetMail struct{ UserID, Email, Name, Locale, Token string; ExpiresAt time.Time }
type InvitationMail struct{ UserID, Email, Name, Locale, Token, MarinaName, InviterName string; ExpiresAt time.Time }
func (s *Service) EnqueuePasswordReset(ctx, tx bun.IDB, m PasswordResetMail) error // implements auth.Mailer
func (s *Service) EnqueueInvitation(ctx, tx bun.IDB, m InvitationMail) error
func (s *Service) DeliverDue(ctx context.Context, limit int) (int, error) // claim with lease, backoff 1m*2^n cap 1h, max 8 attempts -> failed
func (s *Service) Wake() <-chan struct{}
// Templates: embedded text/template (plain) + html/template (simple HTML) in notify/templates/{it,en}/{password_reset,invitation}.{subject,txt,html}.tmpl.
// Footer "Porto — Coste360". Links: <base_url>/admin/reset-password#token=<token> (token in hash, never in query/log).
// Recipient and render payload are encrypted in message_log (recipient_enc, payload_enc); recipient_hash = HMAC-SHA256(Derive("notify.recipient"), email).

// internal/domain/meta
type Meta struct{ Version, APIVersion, Edition string; Capabilities []string; DemoPairing bool }
func Get(cfg *config.Config, version string) Meta // capabilities M0: "auth.totp","auth.password_reset","devices.pairing","webhooks","sse"

// internal/jobs
type Job struct{ Name string; Every time.Duration; RunAtStart bool; Run func(ctx context.Context) error; Wake <-chan struct{} }
type Runner struct{}
func NewRunner(log *slog.Logger, lock Locker) *Runner
type Locker interface{ TryLock(ctx context.Context, name string) (unlock func(), ok bool, err error) } // PG: pg_try_advisory_lock(hashtext(name)); SQLite: in-process
func (r *Runner) Register(j Job) error; Start(ctx context.Context); Wait(); RunNow(ctx, name string) error; Jobs() []string
// Each run: recover panic, slog duration, never overlap the same job.
// Core jobs (§1.3) registered by app.

// internal/http/sse
type Server struct{}
func NewServer(bus ext.EventBus, replay *ReplayBus, log *slog.Logger) *Server
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, marinaID string) error
type ReplayBus struct{} // decorator of ext.EventBus: ring of last 256 events per marina, Last-Event-ID resume
func NewReplayBus(inner ext.EventBus, size int) *ReplayBus
// Wire format: "retry: 3000", "id: <event id>", "event: <topic>", "data: {\"topic\",\"type\",\"marina_id\",\"entity_ids\":[…]}". Heartbeat ": ping" every 25 s.
// Max 5 concurrent streams per principal (429). INV-70: SSE data never contains PII (structural: only ids).

// internal/http/middleware (all func(http.Handler) http.Handler)
func RequestID() ; RealIP(trusted []netip.Prefix) ; AccessLog(log) ; Recover(log) ; SecurityHeaders(o SecurityOptions) ; BodyLimit(n int64)
func Authenticate(a Authenticator) // cookie porto_session | Authorization: Bearer po_live_… | Bearer po_dev_…
func RequireAuth(public []string) ; CSRF(secure bool) ; TOTPGate(allowedPrefixes []string)
func RateLimitByIP(l *Limiter) ; RateLimitAPIKey(l *Limiter) ; Idempotency(o IdempotencyOptions)
// Cookies: porto_session (HttpOnly, Secure unless dev over http, SameSite=Lax, Path=/), porto_csrf (not HttpOnly, same flags).
// CSRF: unsafe methods with cookie auth require header X-CSRF-Token == porto_csrf cookie (constant-time); bearer auth exempt.
// Idempotency (POST only): header Idempotency-Key 8..128 chars [A-Za-z0-9_-:.]; scope = principal id + route + method;
//   request hash = SHA-256(method, route, body); first request stores state in_progress then the response (<= 1 MiB);
//   same key + same hash -> stored response replayed with header Idempotent-Replayed: true;
//   same key + different hash -> 422 idempotency_key_reused; concurrent duplicate -> 409 idempotency_in_progress; TTL 24 h.
//   Skipped for /auth/* (cookies) and SSE.

// internal/http (server.go)
func NewRouter(d Deps) http.Handler // order: RequestID, RealIP, AccessLog, Recover, SecurityHeaders, BodyLimit(1 MiB),
                                    // static routes; /api/*: Authenticate, RequireAuth, TOTPGate, CSRF, rate limits, Idempotency, generated handlers
// errmap.go: Mapper from domain sentinel errors to problem types (§4.4).

// internal/app
type App struct{ Config *config.Config; DB *store.DB; Router http.Handler; Jobs *jobs.Runner; /* services */ }
func New(ctx context.Context, cfg *config.Config, version string, ext ext.Extensions, log *slog.Logger) (*App, error)
func (a *App) Serve(ctx context.Context) error   // HTTP(S) + jobs; graceful shutdown 15 s on SIGINT/SIGTERM
func (a *App) Close() error
func (a *App) SeedDemo(ctx context.Context, password string) (*SeedResult, error)

// internal/backup — §1.4
func Backup(ctx context.Context, db *store.DB, out io.Writer, version string) (*Manifest, error)
func Restore(ctx context.Context, db *store.DB, in io.Reader, force bool) (*Manifest, error)

// pkg/ext (M0 subset; the other interfaces of spec 27.5 arrive with their milestones)
type Event struct{ ID, Type string; Version int; MarinaID string; OccurredAt time.Time; Data json.RawMessage }
type EventSink interface{ Name() string; Handle(ctx context.Context, ev Event) error }
type EventBus interface{ Publish(ctx context.Context, ev Event) error; Subscribe(ctx context.Context, marinaID string) (<-chan Event, func(), error) }
type AuthProvider interface{ Name() string /* SSO, not used in M0 */ }
type TenantResolver interface{ ResolveSlug(ctx context.Context, host string) (slug string, ok bool) }
type Extensions struct{ EventSinks []EventSink; EventBus EventBus; AuthProviders []AuthProvider; TenantResolver TenantResolver; HTTPRoutes func(r chi.Router) }
```

### 1.3 Core jobs (registered in `internal/app`)

| Name | Every | Wakeable | Does |
|---|---|---|---|
| `outbox.dispatch` | 1 s | yes (after each commit that wrote the outbox) | `Dispatcher.DispatchPending(200)` |
| `webhooks.deliver` | 5 s | yes | `Deliverer.DeliverDue(50)` |
| `email.deliver` | 5 s | yes | `notify.DeliverDue(50)` |
| `sessions.cleanup` | 1 h | no | expired sessions, reset tokens, used/expired pairing codes |
| `idempotency.cleanup` | 1 h | no | expired idempotency keys |
| `webhooks.retention` | 24 h | no | deliveries older than 30 days |

### 1.4 CLI (`cmd/porto`)

```
porto serve                                  # migrate if PORTO_AUTO_MIGRATE, bootstrap owner if PORTO_BOOTSTRAP_OWNER_* and no users
porto migrate up | status                    # status: table version/name/applied/applied_at; exit 1 if pending on "status --check"
porto admin create --email E --name N --org ORG [--marina NAME --slug SLUG --timezone Europe/Rome --locale it] [--password-stdin]
                                             # password: --password-stdin, else PORTO_ADMIN_PASSWORD, else interactive prompt (x/term); never a flag
porto seed demo [--password-stdin]           # minimal demo: org "Porto Demo", marina "Marina Demo" slug "demo",
                                             # one user per role <role>@demo.porto.test; idempotent (no-op if slug exists)
porto backup --out FILE                      # portable .tar.gz (see below); "-" = stdout
porto restore --in FILE [--force]            # into an empty, migrated database of either dialect; --force wipes first
porto version                                # "porto <version> (<go version>, schema <n>)"
```

Exit codes: 0 ok, 1 runtime error, 2 usage error. Messages to stderr in English (operator tool). The version is injected
with `-ldflags "-X main.version=…"`.

**Backup format** (`internal/backup`): gzip tar with `manifest.json` (`format: "porto-backup/1"`, porto version,
schema version, source dialect, created_at, table row counts, SHA-256 per file) plus `tables/<table>.jsonl` (one JSON
object per row; ids/timestamps as strings, booleans as JSON booleans, JSON columns as objects, BLOB/bytea base64).
Tables are written and restored in FK order from `store.BackupTables()`. Excluded: `sessions`, `idempotency_keys`,
`goose_db_version`. The import checks schema version equality. SQLite reads run in one read transaction. This
format lets an installation move between SQLite and PostgreSQL (spec 31.1).

---

## 2. Data model: `00001_init.sql` per dialect

Conventions (spec 06): every table has `id` (UUIDv7), `created_at`, `updated_at` (insert-only tables have no
`updated_at`). Operational tables have an indexed `marina_id`. Instants are UTC. Calendar dates are `DATE`. JSON is
`TEXT` on SQLite and `jsonb` on PostgreSQL. `archived_at` is used only where listed. Both files share the number
`00001` and have `-- +goose Up` / `-- +goose Down` sections. The Down section drops everything in reverse order.

Type mapping (SQLite → PostgreSQL): `TEXT` id → `uuid`; `TIMESTAMP` → `timestamptz`; `DATE` (TEXT `YYYY-MM-DD`) →
`date`; JSON `TEXT` → `jsonb`; `INTEGER` 0/1 bool + `CHECK (x IN (0,1))` → `boolean`; `INTEGER` → `bigint` (or
`integer` for counters); `BLOB` → `bytea`. In COALESCE-based unique indexes on nullable uuid, PostgreSQL uses
`COALESCE(marina_id, '00000000-0000-0000-0000-000000000000'::uuid)` and SQLite uses `COALESCE(marina_id, '')`.

Authoritative DDL (SQLite form; PostgreSQL is the mechanical translation, plus the trigger in §2.1):

```sql
-- +goose Up
CREATE TABLE organizations (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    vat_number      TEXT,
    fiscal_code     TEXT,
    billing_address TEXT NOT NULL DEFAULT '{}',   -- {line1,line2,postal_code,city,province,country}
    sdi_code        TEXT,
    pec             TEXT,
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX organizations_name_idx ON organizations (name);

CREATE TABLE marinas (
    id                    TEXT PRIMARY KEY,
    organization_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    slug                  TEXT NOT NULL UNIQUE CHECK (length(slug) BETWEEN 3 AND 63 AND slug = lower(slug)),
    name                  TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind                  TEXT NOT NULL DEFAULT 'marina' CHECK (kind IN ('marina', 'yard', 'both')),
    timezone              TEXT NOT NULL DEFAULT 'Europe/Rome',
    currency              TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    default_locale        TEXT NOT NULL DEFAULT 'it',
    locales               TEXT NOT NULL DEFAULT '["it"]',
    contacts              TEXT NOT NULL DEFAULT '{}',  -- {email,phone,website,vhf_channel}
    address               TEXT NOT NULL DEFAULT '{}',
    geo                   TEXT,                        -- {lat_e7,lon_e7} integers (WGS84 * 1e7), no floats
    concession_ref        TEXT,
    concession_expires_on DATE,
    settings              TEXT NOT NULL DEFAULT '{}',
    settings_version      INTEGER NOT NULL DEFAULT 1,  -- schema version of the settings JSON
    theme                 TEXT NOT NULL DEFAULT '{}',
    booking_enabled       INTEGER NOT NULL DEFAULT 0 CHECK (booking_enabled IN (0, 1)),
    allowed_embed_origins TEXT NOT NULL DEFAULT '[]',
    version               INTEGER NOT NULL DEFAULT 1,  -- optimistic concurrency, ETag "\"<version>\""
    archived_at           TIMESTAMP,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, organization_id)                       -- target of composite FKs
);
CREATE INDEX marinas_organization_id_idx ON marinas (organization_id);

CREATE TABLE users (
    id                   TEXT PRIMARY KEY,
    email                TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    password_hash        TEXT,                          -- NULL until the invitation is accepted
    name                 TEXT NOT NULL,
    locale               TEXT NOT NULL DEFAULT 'it',
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    password_changed_at  TIMESTAMP,
    last_login_at        TIMESTAMP,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL
);

CREATE TABLE user_totp (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    secret_enc    TEXT NOT NULL,                        -- crypto.KeyRing, AAD "totp:<user_id>"
    confirmed_at  TIMESTAMP,                            -- NULL = enrolment pending
    last_step     INTEGER NOT NULL DEFAULT 0,           -- replay protection
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL
);

CREATE TABLE recovery_codes (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL UNIQUE,
    used_at     TIMESTAMP,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX recovery_codes_user_id_idx ON recovery_codes (user_id);

CREATE TABLE memberships (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    organization_id    TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    marina_id          TEXT,                            -- NULL = organization-wide grant
    role               TEXT NOT NULL CHECK (role IN ('owner','director','office','accounting','dock','yard_mgr','yard','rental','viewer')),
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    FOREIGN KEY (marina_id, organization_id) REFERENCES marinas (id, organization_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX memberships_user_scope_uidx ON memberships (user_id, organization_id, COALESCE(marina_id, ''));
CREATE INDEX memberships_marina_id_idx ON memberships (marina_id);
CREATE INDEX memberships_organization_id_idx ON memberships (organization_id);

CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    token_hash      TEXT NOT NULL UNIQUE,               -- SHA-256 of the cookie token
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    totp_verified   INTEGER NOT NULL DEFAULT 0 CHECK (totp_verified IN (0, 1)),
    expires_at      TIMESTAMP NOT NULL,                 -- absolute: created + 14 days
    idle_expires_at TIMESTAMP NOT NULL,                 -- sliding: last_seen + 24 h
    last_seen_at    TIMESTAMP NOT NULL,
    ip              TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE password_reset_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    purpose      TEXT NOT NULL CHECK (purpose IN ('reset', 'invite')),  -- reset TTL 1 h, invite TTL 7 days
    expires_at   TIMESTAMP NOT NULL,
    used_at      TIMESTAMP,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL
);
CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);
CREATE INDEX password_reset_tokens_expires_at_idx ON password_reset_tokens (expires_at);

CREATE TABLE api_keys (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name               TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    prefix             TEXT NOT NULL UNIQUE,
    key_hash           TEXT NOT NULL UNIQUE,
    scopes             TEXT NOT NULL DEFAULT '[]',
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    expires_at         TIMESTAMP,
    last_used_at       TIMESTAMP,
    revoked_at         TIMESTAMP,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX api_keys_marina_id_idx ON api_keys (marina_id);

CREATE TABLE devices (
    id              TEXT PRIMARY KEY,
    marina_id       TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    platform        TEXT NOT NULL CHECK (platform IN ('ios','android','windows','macos','linux','other')),
    kind            TEXT NOT NULL CHECK (kind IN ('mobile', 'desktop')),
    app_version     TEXT,
    token_prefix    TEXT NOT NULL UNIQUE,
    token_hash      TEXT NOT NULL UNIQUE,
    pairing_code_id TEXT,
    last_used_at    TIMESTAMP,
    revoked_at      TIMESTAMP,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX devices_marina_id_idx ON devices (marina_id);
CREATE INDEX devices_user_id_idx ON devices (user_id);

CREATE TABLE pairing_codes (
    id                 TEXT PRIMARY KEY,
    marina_id          TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    user_id            TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,  -- user the device will act as
    created_by_user_id TEXT REFERENCES users (id) ON DELETE SET NULL,
    kind               TEXT CHECK (kind IN ('mobile', 'desktop')),
    code_hash          TEXT NOT NULL UNIQUE,
    expires_at         TIMESTAMP NOT NULL,
    used_at            TIMESTAMP,
    device_id          TEXT REFERENCES devices (id) ON DELETE SET NULL,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX pairing_codes_marina_id_idx ON pairing_codes (marina_id);
CREATE INDEX pairing_codes_expires_at_idx ON pairing_codes (expires_at);
-- Single use: UPDATE pairing_codes SET used_at=? WHERE code_hash=? AND used_at IS NULL AND expires_at > ? (1 row or fail)

CREATE TABLE idempotency_keys (
    id                    TEXT PRIMARY KEY,
    scope                 TEXT NOT NULL,
    key                   TEXT NOT NULL,
    request_hash          TEXT NOT NULL,
    state                 TEXT NOT NULL CHECK (state IN ('in_progress', 'completed')),
    response_status       INTEGER,
    response_content_type TEXT,
    response_body         BLOB,
    expires_at            TIMESTAMP NOT NULL,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (scope, key)
);
CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

CREATE TABLE audit_log (                                -- insert-only (triggers), no updated_at
    id              TEXT PRIMARY KEY,
    organization_id TEXT,
    marina_id       TEXT,
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('user', 'api_key', 'device', 'system')),
    actor_id        TEXT,
    action          TEXT NOT NULL,                      -- e.g. "member.add", "auth.login", "auth.login_failed"
    entity_type     TEXT NOT NULL,
    entity_id       TEXT,
    diff            TEXT,                               -- redacted JSON
    request_id      TEXT NOT NULL DEFAULT '',
    ip              TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL
);
CREATE INDEX audit_log_marina_created_idx ON audit_log (marina_id, created_at);
CREATE INDEX audit_log_org_created_idx ON audit_log (organization_id, created_at);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id);
-- +goose StatementBegin
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is insert-only'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is insert-only'); END;
-- +goose StatementEnd

CREATE TABLE outbox_events (
    id              TEXT PRIMARY KEY,                   -- UUIDv7: ordering = id
    organization_id TEXT,
    marina_id       TEXT,
    type            TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    payload         TEXT NOT NULL,                      -- event data (ids only)
    occurred_at     TIMESTAMP NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP,
    locked_until    TIMESTAMP,
    last_error      TEXT,
    dispatched_at   TIMESTAMP,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX outbox_events_pending_idx ON outbox_events (id) WHERE dispatched_at IS NULL;
CREATE INDEX outbox_events_marina_created_idx ON outbox_events (marina_id, created_at);

CREATE TABLE webhook_endpoints (
    id                         TEXT PRIMARY KEY,
    marina_id                  TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    url                        TEXT NOT NULL,
    description                TEXT NOT NULL DEFAULT '',
    secret_enc                 TEXT NOT NULL,           -- AAD = endpoint id
    previous_secret_enc        TEXT,                    -- AAD = endpoint id + ":previous"
    previous_secret_expires_at TIMESTAMP,
    events                     TEXT NOT NULL DEFAULT '[]',  -- list of types, or ["*"]
    active                     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    consecutive_failures       INTEGER NOT NULL DEFAULT 0,
    disabled_at                TIMESTAMP,
    disabled_reason            TEXT,
    version                    INTEGER NOT NULL DEFAULT 1,
    created_at                 TIMESTAMP NOT NULL,
    updated_at                 TIMESTAMP NOT NULL
);
CREATE INDEX webhook_endpoints_marina_id_idx ON webhook_endpoints (marina_id);

CREATE TABLE webhook_deliveries (
    id               TEXT PRIMARY KEY,
    marina_id        TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    endpoint_id      TEXT NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id         TEXT NOT NULL REFERENCES outbox_events (id) ON DELETE CASCADE,
    event_type       TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_status_code INTEGER,
    last_error       TEXT,
    response_excerpt TEXT,
    first_attempt_at TIMESTAMP,
    next_attempt_at  TIMESTAMP NOT NULL,
    locked_until     TIMESTAMP,
    delivered_at     TIMESTAMP,
    failed_at        TIMESTAMP,
    created_at       TIMESTAMP NOT NULL,
    updated_at       TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX webhook_deliveries_endpoint_event_uidx ON webhook_deliveries (endpoint_id, event_id);
CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_endpoint_created_idx ON webhook_deliveries (marina_id, endpoint_id, created_at);

CREATE TABLE message_log (                              -- email queue + delivery log (spec 6.9, 28)
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT REFERENCES marinas (id) ON DELETE CASCADE,   -- NULL for account mails (reset)
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,
    channel             TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email')),
    template            TEXT NOT NULL,
    locale              TEXT NOT NULL DEFAULT 'it',
    recipient_hash      TEXT NOT NULL,
    recipient_enc       TEXT NOT NULL,
    payload_enc         TEXT NOT NULL,                  -- encrypted render inputs (may contain links with tokens)
    dedupe_key          TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sending','sent','failed','cancelled')),
    attempts            INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     TIMESTAMP,
    locked_until        TIMESTAMP,
    last_error          TEXT,
    provider_message_id TEXT,
    sent_at             TIMESTAMP,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX message_log_dedupe_uidx ON message_log (COALESCE(marina_id, ''), dedupe_key);
CREATE INDEX message_log_due_idx ON message_log (next_attempt_at) WHERE status = 'queued';
CREATE INDEX message_log_marina_created_idx ON message_log (marina_id, created_at);
```

### 2.1 PostgreSQL specifics

- Audit insert-only trigger: `CREATE FUNCTION porto_audit_log_insert_only() RETURNS trigger … RAISE EXCEPTION
  'audit_log is insert-only'` plus `CREATE TRIGGER audit_log_no_update_delete BEFORE UPDATE OR DELETE ON audit_log FOR
  EACH ROW EXECUTE FUNCTION …`, wrapped in `-- +goose StatementBegin/End`.
- `length()` becomes `char_length()` in CHECKs. Booleans are `boolean NOT NULL DEFAULT false/true` without CHECK.
- `consents` (spec 6.9) is postponed to M1 together with `customers`. Nothing in M0 needs it.

### 2.2 Migration tests (BACKEND, `internal/migrations`, tag integration where DB needed)

`TestMigrations_SameNumbering` (identical file names in both dirs, no gaps), `TestMigrations_UpDownUp` (SQLite and PG),
`TestMigrations_SchemaParity` (same tables, columns and index names on both dialects, from `sqlite_master` /
`information_schema`), `TestAuditLog_InsertOnly` (UPDATE/DELETE fail on both), `TestMemberships_MarinaMustBelongToOrg`
(composite FK).

---

## 3. Roles and permissions (spec 26.1)

`internal/domain/auth/roles.go`. Roles: `owner, director, office, accounting, dock, yard_mgr, yard, rental, viewer`.

| Permission constant | Value | Roles |
|---|---|---|
| PermOrganizationManage | `organization.manage` | owner (**org-level membership only**): edit org, create/archive marinas, org owners |
| PermMarinaConfigure | `marina.configure` | owner, director (marina settings; later map, price lists, templates) |
| PermMarinaRead | `marina.read` | all |
| PermUsersManage | `users.manage` | owner (members, roles, API keys, webhooks, provider credentials) |
| PermDevicesManage | `devices.manage` | owner, director (pairing codes for any user, revoke) |
| PermDevicesPairSelf | `devices.pair_self` | all (pairing code for oneself) |
| PermAuditRead | `audit.read` | owner, director |
| PermCustomersWrite | `customers.write` | owner, director, office |
| PermCustomersRead | `customers.read` | owner, director, office, accounting, dock, yard_mgr, rental |
| PermContractsWrite | `contracts.write` | owner, director, office |
| PermTransitsDesk | `transits.desk` | owner, director, office, dock |
| PermOccupancyMove | `occupancy.move` | owner, director, office, dock |
| PermPresenceWrite | `presence.write` | owner, director, office, dock |
| PermOccupancyOverrideFit | `occupancy.override_fit` | owner, director |
| PermRightsManage | `rights.manage` | owner, director, office, accounting |
| PermChargesManage | `charges.manage` | owner, director, office, accounting |
| PermChargesExport | `charges.export` | owner, director, accounting |
| PermPaymentsCollect | `payments.collect` | owner, director, office, accounting, rental |
| PermPaymentsCollectCash | `payments.collect_cash` | owner, director, office, accounting, rental, dock |
| PermRefundsIssue | `refunds.issue` | owner, director, accounting |
| PermStorageManage | `storage.manage` | owner, director, office, yard_mgr |
| PermStorageRead | `storage.read` | owner, director, office, yard_mgr, dock |
| PermYardManage | `yard.manage` | owner, director, yard_mgr |
| PermYardRead | `yard.read` | owner, director, yard_mgr, office, accounting |
| PermYardTimesheets | `yard.timesheets` | owner, director, yard_mgr, yard |
| PermRentalManage | `rental.manage` | owner, director, office, rental |
| PermMaintenanceManage | `maintenance.manage` | owner, director, office, dock, yard_mgr |
| PermMaintenanceReport | `maintenance.report` | owner, director, office, dock, yard_mgr, yard, rental |
| PermReportsAll | `reports.all` | owner, director, accounting, viewer |
| PermReportsOperational | `reports.operational` | owner, director, accounting, viewer, office |
| PermReportsYard | `reports.yard` | owner, director, accounting, viewer, yard_mgr |
| PermReportsRental | `reports.rental` | owner, director, accounting, viewer, rental |
| PermMapView | `map.view` | all |

Rules:
- The matrix is one `var rolePermissions = map[Role][]Permission{…}` literal. `TestRoles_MatrixMatchesSpec` holds a
  second, independent copy of the table above and compares the two. `TestRoles_EveryPermissionHasOwner` checks that
  owner has every permission (except that `organization.manage` needs an org-level grant).
- Checks happen in **domain services**, never in handlers. The first line of every service method is
  `scope, err := p.Authorize(ctx, db, marinaID, auth.PermX)`, or `AuthorizeOrg` for org operations.
  `Authorize` returns `ErrNotFound` when the principal has no grant on that marina (HTTP 404, so existence does not
  leak) and `ErrForbidden` when the grant lacks the permission (HTTP 403).
- Effective grants for a marina are the org-level membership of its organization plus the marina-level membership.
  When both exist, the union of their permissions applies.
- API keys get the scopes stored on the key (fixed at creation, ⊆ creator permissions, never `users.manage`).
- Device tokens get the user's role on the device's marina at request time. Revoking the membership revokes the
  device's access.
- TOTP policy: when `PORTO_REQUIRE_TOTP_FOR_OWNERS=true`, users with role `owner` or `accounting` anywhere must
  enrol. Until they do, their session only reaches `/auth/me`, `/auth/totp/*` and `/auth/logout`, and the API answers
  403 `totp_enrolment_required`.
- `PermMapView` and later read permissions may be checked in M1+. M0 services use: organization.manage,
  marina.configure, marina.read, users.manage, devices.manage, devices.pair_self, audit.read.

---

## 4. OpenAPI contract (`api/openapi.yaml`, OpenAPI 3.1)

`info.title: Porto API`, `version: 0.1.0`. `info.license` is omitted (private repository). `servers: [{url: /}]`.
Tags: ops, auth, organizations, marinas, team, api-keys, devices, webhooks, audit, realtime, meta, device, public.
`api/oapi-codegen.yaml`: `package: api`, `output: internal/http/api/api.gen.go`,
`generate: {chi-server: true, models: true}`, `output-options: {skip-prune: true}`. The TS client is generated by
`openapi-typescript` into `web/shared/api/generated/schema.d.ts`.

### 4.1 Conventions

- JSON field names are snake_case. Instants are RFC 3339 UTC, dates `YYYY-MM-DD`. Ids are UUID strings. Money is
  integer cents and lengths integer cm (none in M0).
- Collections: `?cursor=&limit=` (default 50, max 200) → `{ "items": [...], "next_cursor": string|null }`.
- Versioned resources (organization, marina, webhook endpoint) return `ETag: "<version>"`. PATCH requires
  `If-Match`: a missing header is 428 `precondition_required`, a mismatch is 412 `version_conflict`.
- `Idempotency-Key` header parameter on every creating POST (marina, member, api key, pairing code, webhook endpoint,
  test, redeliver, pair).
- Every cookie-authenticated unsafe request sends `X-CSRF-Token` (parameter `CSRFToken` in components).
- Every error is `application/problem+json` with schema `Problem` (§1.2) and a reusable response per status
  (`BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `PreconditionFailed`, `PreconditionRequired`,
  `UnprocessableEntity`, `TooManyRequests`, `ServiceUnavailable`).

### 4.2 Security schemes

```yaml
securitySchemes:
  cookieAuth: { type: apiKey, in: cookie, name: porto_session }
  csrfToken:  { type: apiKey, in: header, name: X-CSRF-Token }   # required together with cookieAuth on unsafe methods
  apiKeyAuth: { type: http, scheme: bearer, bearerFormat: "po_live_<prefix>_<secret>" }
  deviceAuth: { type: http, scheme: bearer, bearerFormat: "po_dev_<prefix>_<secret>" }
```
Admin read operations: `security: [{cookieAuth: []}, {apiKeyAuth: []}, {deviceAuth: []}]`. Admin unsafe operations:
`[{cookieAuth: [], csrfToken: []}, {apiKeyAuth: []}, {deviceAuth: []}]`. Account operations (`/auth/*` except login and
password reset) accept cookie only. Device API: `deviceAuth` only. Public, health, meta, login, reset and pair:
`security: []`.

### 4.3 Paths

Prefix `A = /api/admin/v1`, `M = A/marinas/{marinaId}`.

| Method & path | operationId | Request | 2xx response | Errors |
|---|---|---|---|---|
| GET `/healthz` | healthz | – | 200 `{status:"ok"}` | – |
| GET `/readyz` | readyz | – | 200 `{status:"ok", schema_version}` | 503 `not_ready` |
| GET `A/meta` | getMeta | – | 200 `Meta{version, api_version, edition:"server", capabilities[], demo_pairing}` | – |
| POST `A/auth/login` | login | `LoginRequest{email, password, totp_code?, recovery_code?}` | 200 `Me` + Set-Cookie session & csrf | 400 validation_failed, 401 invalid_credentials / totp_required / invalid_totp, 429 rate_limited |
| POST `A/auth/logout` | logout | – | 204 (cookies cleared) | 401, 403 csrf_failed |
| GET `A/auth/me` | getMe | – | 200 `Me` | 401 |
| PATCH `A/auth/me` | updateMe | `MePatch{name?, locale?}` | 200 `Me` | 400, 401, 403 |
| POST `A/auth/password` | changePassword | `{current_password, new_password}` | 204 | 400 weak_password, 401 invalid_credentials, 403 |
| POST `A/auth/password-reset/request` | requestPasswordReset | `{email}` | 204 (always) | 400, 429 |
| POST `A/auth/password-reset/confirm` | confirmPasswordReset | `{token, new_password}` | 204 | 400 invalid_reset_token / weak_password, 429 |
| POST `A/auth/totp/enroll` | enrollTotp | – | 200 `TotpEnrollment{secret, otpauth_url, qr_svg}` | 401, 403, 409 totp_already_enabled |
| POST `A/auth/totp/confirm` | confirmTotp | `{code}` | 200 `RecoveryCodes{codes[10]}` (session rotated) | 400 invalid_totp, 401, 403, 409 totp_not_enrolled |
| POST `A/auth/totp/disable` | disableTotp | `{password, code}` | 204 | 400, 401, 403 totp_required_by_policy |
| POST `A/auth/recovery-codes` | regenerateRecoveryCodes | `{code}` | 200 `RecoveryCodes` | 400 invalid_totp, 401, 403 |
| GET `A/organizations` | listOrganizations | – | 200 `{items: Organization[]}` | 401 |
| PATCH `A/organizations/{organizationId}` | updateOrganization | `OrganizationPatch` + If-Match | 200 `Organization` | 400, 403, 404, 412, 428 |
| GET `A/organizations/{organizationId}/marinas` | listMarinas | – | 200 `{items: Marina[]}` | 401, 404 |
| POST `A/organizations/{organizationId}/marinas` | createMarina | `MarinaCreate{name, slug, kind?, timezone?, currency?, default_locale?, locales?}` | 201 `Marina` | 400, 403, 404, 409 slug_taken |
| GET `A/organizations/{organizationId}/owners` | listOrgOwners | – | 200 `{items: Member[]}` | 403, 404 |
| POST `A/organizations/{organizationId}/owners` | addOrgOwner | `MemberCreate` (role ignored = owner) | 201 `Member` | 400, 403, 404, 409 already_member |
| DELETE `A/organizations/{organizationId}/owners/{userId}` | removeOrgOwner | – | 204 | 403, 404, 409 last_owner |
| GET `M` | getMarina | – | 200 `Marina` + ETag | 404 |
| PATCH `M` | updateMarina | `MarinaPatch` + If-Match | 200 `Marina` + ETag | 400, 403, 404, 412, 428 |
| POST `M/archive` | archiveMarina | – | 204 | 403, 404, 409 |
| GET `M/members` | listMembers | – | 200 `{items: Member[]}` | 403, 404 |
| POST `M/members` | addMember | `MemberCreate{email, name, role, locale?}` | 201 `Member` | 400, 403, 404, 409 already_member |
| PATCH `M/members/{userId}` | updateMember | `{role}` | 200 `Member` | 400, 403, 404, 409 last_owner |
| DELETE `M/members/{userId}` | removeMember | – | 204 | 403, 404, 409 last_owner |
| POST `M/members/{userId}/password-reset` | sendMemberPasswordReset | – | 202 | 403, 404 |
| GET `M/api-keys` | listApiKeys | – | 200 `{items: ApiKey[]}` | 403, 404 |
| POST `M/api-keys` | createApiKey | `ApiKeyCreate{name, scopes[], expires_at?}` | 201 `ApiKeyCreated{api_key: ApiKey, secret}` | 400 invalid_scope, 403, 404 |
| DELETE `M/api-keys/{apiKeyId}` | revokeApiKey | – | 204 | 403, 404 |
| POST `M/devices/pairing-codes` | createPairingCode | `{user_id?, kind?, ttl_seconds?}` | 201 `PairingCode{code, expires_at, qr_payload, qr_svg}` | 400, 403, 404 |
| GET `M/devices` | listDevices | – | 200 `{items: Device[]}` | 403, 404 |
| DELETE `M/devices/{deviceId}` | revokeDevice | – | 204 | 403, 404 |
| GET `A/event-types` | listEventTypes | – | 200 `{items: [{type, topic, public}]}` | 401 |
| GET `M/webhook-endpoints` | listWebhookEndpoints | – | 200 `{items: WebhookEndpoint[]}` | 403, 404 |
| POST `M/webhook-endpoints` | createWebhookEndpoint | `{url, description?, events[]}` | 201 `WebhookEndpointCreated{endpoint, secret}` | 400 invalid_webhook_url / unknown_event_type, 403, 404 |
| GET `M/webhook-endpoints/{endpointId}` | getWebhookEndpoint | – | 200 + ETag | 403, 404 |
| PATCH `M/webhook-endpoints/{endpointId}` | updateWebhookEndpoint | `{url?, description?, events?, active?}` + If-Match | 200 | 400, 403, 404, 412, 428 |
| DELETE `M/webhook-endpoints/{endpointId}` | deleteWebhookEndpoint | – | 204 | 403, 404 |
| POST `M/webhook-endpoints/{endpointId}/rotate-secret` | rotateWebhookSecret | – | 200 `{secret, previous_valid_until}` | 403, 404 |
| POST `M/webhook-endpoints/{endpointId}/test` | testWebhookEndpoint | – | 202 `{delivery_id}` | 403, 404, 409 endpoint_disabled |
| GET `M/webhook-endpoints/{endpointId}/deliveries` | listWebhookDeliveries | cursor, limit, status? | 200 page of `WebhookDelivery` | 403, 404 |
| POST `M/webhook-endpoints/{endpointId}/deliveries/{deliveryId}/redeliver` | redeliverWebhook | – | 202 | 403, 404 |
| GET `M/audit-log` | listAuditLog | cursor, limit, entity_type?, entity_id?, actor_id?, from?, to? | 200 page of `AuditEntry` | 403, 404 |
| GET `A/stream` | stream | query `marina_id` (required), header Last-Event-ID | 200 `text/event-stream` | 401, 404, 429 |
| POST `/api/device/v1/pair` | pairDevice | `{code, name, platform, kind, app_version?}` | 201 `PairResult{token, device, marina{id,slug,name}, user{id,name}, base_url}` | 400 pairing_code_invalid, 429 |
| POST `/api/device/v1/demo-pairing` | demoPairDevice | `{name, platform, kind, app_version?}` | 201 `PairResult` | 404 (disabled), 429 |
| GET `/api/device/v1/self` | getDeviceSelf | – | 200 `{device, user, marina, permissions[]}` | 401 |
| DELETE `/api/device/v1/self` | unpairDeviceSelf | – | 204 | 401 |
| GET `/api/public/v1/{slug}` | getPublicMarina | – | 200 `PublicMarina{slug, name, default_locale, locales, booking_enabled}` | 404, 429 |

Schemas: `Me{user{id,email,name,locale,totp_enabled}, auth_method, totp_enrolment_required, organizations[{id,name,role?}],
marinas[{id, organization_id, slug, name, roles[], permissions[]}]}`; `Organization{id,name,vat_number,fiscal_code,
billing_address(Address),sdi_code,pec,version,created_at,updated_at}`; `Address{line1,line2,postal_code,city,province,country}`;
`Marina{id, organization_id, slug, name, kind, timezone, currency, default_locale, locales[], contacts{email,phone,website,vhf_channel},
address, geo{lat_e7,lon_e7}|null, concession_ref, concession_expires_on(date)|null, settings(object), theme(object),
booking_enabled, allowed_embed_origins[], version, archived_at|null, created_at, updated_at}`; `Member{user_id,email,name,role,
scope("organization"|"marina"), active, totp_enabled, invitation_pending, last_login_at|null}`; `ApiKey{id,name,prefix
("po_live_xxxxxxxx"),scopes[],created_by,expires_at,last_used_at,revoked_at,created_at}`; `Device{id,name,platform,kind,
app_version,user{id,name},token_prefix,last_used_at,revoked_at,created_at}`; `WebhookEndpoint{id,url,description,events[],
active,consecutive_failures,disabled_at,disabled_reason,has_previous_secret,version,created_at,updated_at}`;
`WebhookDelivery{id,event_id,event_type,status,attempts,last_status_code,last_error,response_excerpt,next_attempt_at,
delivered_at,failed_at,created_at}`; `AuditEntry{id,actor{type,id},action,entity_type,entity_id,diff,request_id,created_at}`
(no IP in the API); `Role` and `Permission` enums with the values of §3.

### 4.4 Problem types (stable slugs; default status)

`bad_request` 400, `validation_failed` 400 (with `errors[]`), `weak_password` 400, `invalid_reset_token` 400,
`invalid_totp` 400 (401 on login), `pairing_code_invalid` 400, `invalid_scope` 400, `invalid_webhook_url` 400,
`unknown_event_type` 400, `unauthorized` 401, `invalid_credentials` 401, `totp_required` 401, `forbidden` 403,
`csrf_failed` 403, `totp_enrolment_required` 403, `totp_required_by_policy` 403, `not_found` 404,
`method_not_allowed` 405, `conflict` 409, `slug_taken` 409, `already_member` 409, `last_owner` 409,
`totp_already_enabled` 409, `totp_not_enrolled` 409, `endpoint_disabled` 409, `idempotency_in_progress` 409,
`version_conflict` 412, `unsupported_media_type` 415, `idempotency_key_reused` 422, `precondition_required` 428,
`rate_limited` 429 (Retry-After), `internal_error` 500, `not_ready` 503.
The web app shows each one through the i18n key `errors.<slug>`. BOOTSTRAP declares all of these in
`internal/http/problem/types.go`.

### 4.5 Event envelope (outbox, webhooks)

```json
{"id":"evt_<uuidv7>","type":"member.added","version":1,"marina_id":"<uuid>","occurred_at":"2026-11-06T10:00:00Z",
 "data":{"user_id":"<uuid>","role":"office"}}
```
`data` contains ids and enums only, never names, emails, phones or tokens. `TestEvents_DataHasNoPII` walks every event
type's sample data and rejects keys matching `email|phone|name|token|secret|password|address`.
Topics (SSE event names) in M0: `marina.changed` (organization.*, marina.*), `team.changed` (member.*, device.*,
api_key.*). `webhook.test` is not published to SSE.

---

## 5. Configuration (`internal/config`)

| Variable | Default | Validation / meaning |
|---|---|---|
| `PORTO_ADDR` | `:8080` | `host:port` |
| `PORTO_BASE_URL` | `http://localhost:8080` | absolute http(s) URL without query/fragment; https required unless `PORTO_DEV=true` or host is localhost/127.0.0.1/::1 |
| `PORTO_DATA_DIR` | `./data` (`/data` in image) | created with 0700; SQLite file `porto.db`, dev secret `secret.key`, autocert cache `certs/` |
| `DATABASE_URL` | empty (SQLite) | if set must be `postgres://` or `postgresql://` |
| `PORTO_SECRET_KEY` | required | comma-separated base64 32-byte keys, first active. If `PORTO_DEV=true` and unset: generate and persist `DATA_DIR/secret.key` (0600), print once to stderr |
| `PORTO_AUTO_MIGRATE` | `true` | bool; when false `serve` refuses to start if migrations pending (`readyz` 503) |
| `PORTO_TLS_AUTOCERT_DOMAINS` | empty | comma list of hostnames; when set `serve` listens :443 (autocert, `golang.org/x/crypto/acme/autocert`) + :80 redirect/ACME |
| `PORTO_SMTP_URL` | empty | `smtp://user:pass@host:587` or `smtps://…:465`; empty → file transport if `PORTO_MAIL_DIR`, else log transport |
| `PORTO_MAIL_FROM` | `Porto <noreply@localhost>` | RFC 5322 address; required to be a non-localhost domain when SMTP is set and not dev |
| `PORTO_MAIL_DIR` | empty | directory for `.eml` files (dev, tests) |
| `PORTO_HOLD_TTL` | `10m` | duration 1m..60m (used from M3; parsed now) |
| `PORTO_LOG_LEVEL` | `info` | debug, info, warn, error |
| `PORTO_LOG_FORMAT` | `json` (`text` when dev) | json, text |
| `PORTO_TRUSTED_PROXIES` | empty | comma CIDR list; X-Forwarded-For/Proto honoured only from these peers |
| `PORTO_REQUIRE_TOTP_FOR_OWNERS` | `false` | bool; applies to roles owner and accounting (§3) |
| `PORTO_TELEMETRY` | `false` | bool; must be false in M0 (no telemetry exists); true → validation error "not supported" |
| `PORTO_SENSOR_RAW_RETENTION` | `30d` | duration with `d` suffix allowed, 1d..3650d (used from M12) |
| `PORTO_DEMO_PAIRING_USER` / `PORTO_DEMO_PAIRING_MARINA` | empty | email / marina slug; both or neither |
| `PORTO_DEV` | `false` | dev mode: generated secret, non-Secure cookies over http, text logs |
| `PORTO_WEBHOOKS_ALLOW_PRIVATE` | `false` | disables private-address egress check and allows http (dev/tests only) |
| `PORTO_PUBLIC_READS_PER_MINUTE` | `120` | int 1..10000, per IP prefix on `/api/public/v1` |
| `PORTO_BOOTSTRAP_OWNER_EMAIL` / `_NAME` / `_ORG` | empty | all three or none; with `PORTO_ADMIN_PASSWORD` creates the first owner on `serve` when there are no users |
| `PORTO_ADMIN_PASSWORD` | empty | password for `admin create` / bootstrap / `seed demo`; never logged |
| `PORTO_TEST_POSTGRES_URL` | empty | tests only (not read by `config`) |

`Load` gathers every error with `errors.Join`. Messages name the variable but never echo secret values.
`TestConfig_Defaults`, `TestConfig_Invalid` (table: one row per variable), `TestConfig_SecretNotEchoed`.

Fixed constants (not configurable in M0): session idle 24 h, absolute 14 days; login throttle 5 failures / 15 min per
account and 30 attempts / min per IP prefix (/24 IPv4, /56 IPv6); password-reset requests 3 / hour per account and
10 / hour per IP prefix; pairing 10 / min per IP prefix; API key 600 req / min per key; body limit 1 MiB.

---

## 6. Makefile (BOOTSTRAP)

GNU Make 3.81 compatible (macOS): no `.ONESHELL`, `SHELL := /bin/bash`, `.DEFAULT_GOAL := help`, `## ` descriptions
read by `help`. Variables: `MODULE := github.com/coste360/porto`, `VERSION ?= $(shell scripts/version.sh)`,
`IMAGE ?= ghcr.io/coste360/porto`, `GOLANGCI_LINT_VERSION ?= v2.15.0`, `POSTGRES_IMAGE ?= postgres:17-alpine`,
`PKG ?= ./...`. Targets degrade gracefully while `web/`, `e2e/` or `api/openapi.yaml` are missing (print a notice, exit 0),
because BOOTSTRAP runs before the other builders.

Dev env: `PORTO_DEV=true PORTO_DATA_DIR=$(CURDIR)/tmp/data PORTO_ADDR=:8080 PORTO_BASE_URL=http://localhost:8080 PORTO_LOG_LEVEL=debug PORTO_AUTO_MIGRATE=true PORTO_MAIL_DIR=$(CURDIR)/tmp/mail`.

| Target | Runs exactly |
|---|---|
| `help` | prints targets |
| `setup` | `go mod download`; install golangci-lint `$(GOLANGCI_LINT_VERSION)` into `./bin` with `GOBIN=./bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@…` (skip if same version present); `npm --prefix web ci` (or `install` without lockfile); `npm --prefix e2e ci` + `cd e2e && npx playwright install chromium` |
| `dev` | backend: `air` if installed (build `./cmd/porto` → `tmp/bin/porto serve`), else `go run ./cmd/porto serve`, with dev env; plus `npm --prefix web run dev`; trap kills both |
| `seed` | `mkdir -p tmp/data && <dev env> go run ./cmd/porto seed demo` (password from `PORTO_ADMIN_PASSWORD`, default `porto-demo-password` in dev only) |
| `gen` | `go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml`; `go generate ./internal/geo/...`; `npm --prefix web run gen` |
| `gen-check` | `gen`, then fail if `git status --porcelain` shows changes in `internal/http/api/api.gen.go internal/geo/sintable_gen.go web/shared/api/generated` |
| `fmt` | `go tool gofumpt -w` + `go tool goimports -local $(MODULE) -w` on all non-generated `.go` files; `npm --prefix web run format` |
| `lint` | `./bin/golangci-lint run $(PKG)` (fallback `go vet` with warning); `npm --prefix web run lint`; `npm --prefix web run format:check`; `npm --prefix web run typecheck` |
| `test` | `go test $(PKG)`; `npm --prefix web run test` |
| `test-db` | `scripts/with-postgres.sh go test -tags=integration $(PKG)` (script: use `PORTO_TEST_POSTGRES_URL` if set, else start `$(POSTGRES_IMAGE)` with docker on a random port, export the URL, run, remove the container; if docker is missing run SQLite-only with a warning) |
| `test-race` | count `go test -tags=integration -list '^TestConcurrency' ./internal/...`; fail if 0; `scripts/with-postgres.sh go test -race -tags=integration -run '^TestConcurrency' ./internal/...` |
| `e2e` | `build`, then `cd e2e && npx playwright test` |
| `check` | `gen-check lint test` (target < 2 min) |
| `check-full` | `check test-db test-race e2e` |
| `web-build` | `npm --prefix web run build`; copy `web/admin/dist` → `internal/http/static/admin`, `web/booking/dist` → `internal/http/static/booking` (keep `.gitkeep`) |
| `build` | `web-build`; `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/porto ./cmd/porto` |
| `docker` | `docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE):dev .` |
| `up` | `docker compose up --build` |
| `licenses` | `go tool go-licenses check ./... --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0`; `npm --prefix web run licenses` |
| `clean` | remove `bin/porto tmp dist web/*/dist e2e/test-results e2e/playwright-report` and static copies (keep `.gitkeep` and `bin/golangci-lint`) |

BOOTSTRAP also writes `scripts/version.sh` (`git describe --tags --always --dirty`, fallback `dev`) and
`scripts/with-postgres.sh`. `go.mod` `tool` directives: `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen`,
`mvdan.cc/gofumpt`, `golang.org/x/tools/cmd/goimports`, `github.com/google/go-licenses/v2` (Apache-2.0).

`.golangci.yml` (v2 format): linters `govet, staticcheck, errcheck, ineffassign, unused, gosec, revive, misspell,
bodyclose, noctx, errorlint, depguard, forbidigo, sloglint, gocritic`; formatters gofumpt + goimports (local prefix);
`exclusions.paths: ['internal/http/api/api.gen.go', '_gen\.go$']`. depguard follows §1.1. forbidigo bans `time.Now`
outside `internal/clock` (except `_test.go`, `cmd/`), and `fmt.Print*` outside `cmd/`. sloglint uses key-value
attributes, snake_case keys.

---

## 7. Web skeleton (`web/`)

### 7.1 Workspaces

`web/package.json` (`name: porto-web`, private, `workspaces: ["shared", "admin", "booking"]`). Scripts, with names
fixed for Makefile, Dockerfile and CI: `gen`, `dev` (concurrently admin:5173 + booking:5174), `build` (admin then
booking), `lint` (`node scripts/css-guard.mjs && node scripts/i18n-check.mjs && eslint . --max-warnings 0`), `format`,
`format:check`, `typecheck` (all workspaces), `test` (`vitest run`), `budget` (booking), `licenses`
(`license-checker-rseidelsohn --production --onlyAllow 'MIT;BSD-2-Clause;BSD-3-Clause;Apache-2.0;ISC;MPL-2.0;0BSD;OFL-1.1;CC0-1.0;BlueOak-1.0.0' --excludePrivatePackages`;
OFL-1.1 only appears through `@fontsource-variable/titillium-web`).
Packages: `@porto/shared`, `@porto/admin`, `@porto/booking`. Use the same major versions as the Arenile web
(React 19, Vite 8, Tailwind 4 with `@tailwindcss/vite`, TanStack Router 1 + Query 5, i18next 26 + react-i18next 17,
react-hook-form 7 + zod 4 + @hookform/resolvers 5, openapi-fetch 0.17 + openapi-typescript 7, Vitest 5 + Testing
Library + jsdom, ESLint 10 + typescript-eslint, Prettier 3, TypeScript strict). Read the versions from
`arenile/web/**/package.json` and pin them in `package.json` (copying version numbers is not copying code). Extra deps:
`@fontsource-variable/titillium-web`, `eslint-plugin-i18next` (ISC, rule `no-literal-string` on `admin/src` and
`booking/src` except tests). `tsconfig.base.json` uses strict, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`,
`moduleResolution: bundler`.

Vite: admin `base: '/admin/'`, dev proxy `/api` → `http://localhost:8080` (SSE included). Booking
`base: '/b/'` (the server rewrites `/b/{slug}` to the booking `index.html`).

### 7.2 `web/shared` contents

```
shared/components.json        shadcn config: style new-york, tsx, rsc false, css src/styles/globals.css, baseColor neutral,
                              cssVariables true, iconLibrary lucide, aliases → @porto/shared/ui, @porto/shared/lib/utils, …
shared/src/styles/globals.css THE ONLY stylesheet: @import tailwindcss, tw-animate-css, font; :root + .dark tokens (§7.4); @theme inline mapping
shared/src/ui/*               shadcn components (CLI output) + index.ts barrel + README.md (how to add one)
shared/src/ui/patterns/       Brand.tsx (BrandLogo), PageHeader, EmptyState, ConfirmDialog, SecretReveal (copy-once secret with warning),
                              DataTable (@tanstack/react-table), FormField helpers (react-hook-form + shadcn Field), ProblemAlert
shared/src/brand/             verbatim copies of the SVGs in §7.5
shared/src/theme/             ThemeProvider (light|dark|system, class "dark" on <html>, preference in localStorage with try/catch),
                              useTheme, contrast.ts (WCAG ratio), theme.test.ts, contrast.test.ts, brand.test.ts
shared/src/i18n/              init (i18next + react-i18next, fallback it, supported it/en), namespaces common, auth, errors, team,
                              marina, devices, webhooks, audit, booking; locales/{it,en}/*.json; useT helper
shared/src/api/               client.ts (openapi-fetch createClient<paths>, credentials same-origin, CSRF middleware adds
                              X-CSRF-Token from porto_csrf cookie on unsafe methods, 401 → onUnauthorized callback),
                              problem.ts (ProblemError class with type/status/detail/errors, parse problem+json),
                              queryKeys.ts, sse.ts (EventSource wrapper: invalidates query keys by topic), index.ts
shared/api/generated/schema.d.ts  GENERATED (openapi-typescript); never hand-edited
shared/src/lib/               utils.ts (cn), format.ts (formatDateTime(instant, locale, tz), formatIsoDate dd/MM/yyyy, formatCm → "12,50 m",
                              formatCents (Intl, stub for later)), cookies.ts
shared/src/testing/           renderWithProviders (QueryClient, i18n it, Theme, Router memory history), msw-free fetch mock helper
```
Package `exports`: `./api`, `./ui`, `./ui/*`, `./ui/patterns`, `./theme`, `./i18n`, `./lib/*`, `./styles/globals.css`, `./testing`.

**shadcn components to add with the CLI** (run in `web/shared`; init first with the components.json above):
```
cd web/shared && npx shadcn@latest add alert alert-dialog avatar badge breadcrumb button card checkbox collapsible \
  command dialog dropdown-menu empty field input input-group input-otp kbd label popover radio-group scroll-area \
  select separator sheet sidebar skeleton sonner spinner switch table tabs textarea tooltip
```
After the CLI runs: fix the import aliases if the CLI wrote relative paths, export everything from
`src/ui/index.ts`, and do not edit the component internals except where TS strict or lint requires it (note each such
change in `ui/README.md`).

### 7.3 Admin SPA (`web/admin`), TanStack Router (code-based route tree in `src/router.tsx`)

| Route | Page | API |
|---|---|---|
| `/admin/login` | email + password; second step for TOTP code (`input-otp`) or recovery code when `totp_required`; link to forgot password | login |
| `/admin/forgot-password` | email form, always a neutral confirmation | requestPasswordReset |
| `/admin/reset-password` | reads `#token=` from the hash (the invitation uses the same page, with different copy when `#invite=1`), new password + confirm, zod policy (12+ chars) | confirmPasswordReset |
| `/admin/totp-setup` | forced enrolment when `totp_enrolment_required`: QR (`qr_svg` rendered as `<img src="data:image/svg+xml…">`), manual secret, confirm code, recovery codes shown once with copy/download | enrollTotp, confirmTotp |
| `/admin/` | marina picker: 0 marinas + org owner → "create first marina" form; 1 marina → redirect; n → list | getMe, createMarina |
| `/admin/m/$marinaId/` | shell (shadcn sidebar: BrandLogo, marina switcher, nav, user menu with theme toggle + language + logout) + dashboard placeholder ("coming in next milestones") | getMarina, stream |
| `/admin/m/$marinaId/settings` | marina general settings form (name, kind, timezone, currency, locales, contacts, address, concession) with If-Match; 412 → reload prompt | getMarina, updateMarina |
| `/admin/m/$marinaId/team` | members table, add member dialog (email, name, role), change role, remove, send reset; last-owner errors shown | members ops |
| `/admin/m/$marinaId/api-keys` | list, create dialog (name, scopes checklist limited to own permissions, expiry), SecretReveal once, revoke | api-keys ops |
| `/admin/m/$marinaId/devices` | list, "pair a device" dialog: user select, QR + code + countdown; revoke | devices ops |
| `/admin/m/$marinaId/webhooks` | endpoints list/create/edit (event types multi-select from listEventTypes), SecretReveal, rotate, test, deliveries table with redeliver | webhooks ops |
| `/admin/m/$marinaId/audit` | audit table with cursor pagination and entity filter | listAuditLog |
| `/admin/account` | profile (name, locale), change password, TOTP enable/disable, regenerate recovery codes | auth ops |
| `/admin/org/$organizationId` | organization data (If-Match), marinas list + create, org owners | org ops |

Navigation items are shown or hidden from `Me.marinas[].permissions`. The server still enforces them. Every page
handles loading (Skeleton), empty (Empty), error (ProblemAlert with `errors.<type>`) and 403 states. Toasts use sonner.
SSE (`/api/admin/v1/stream?marina_id=`) invalidates `marina` and `team` query keys.
Static assets: `web/admin/public/` holds byte-identical copies of `assets/brand/porto/06_Favicon_Web/{favicon.ico,
favicon.svg,favicon-16x16.png,favicon-32x32.png,apple-touch-icon.png,android-chrome-192x192.png,android-chrome-512x512.png,
maskable.svg,safari-pinned-tab.svg}` and its own `site.webmanifest` (start_url and icons under `/admin/`, name "Porto",
description via the Italian product string). No PWA service worker in M0.

Tests (Vitest + Testing Library): login success / invalid credentials / TOTP step; CSRF header added on POST;
ProblemError parsing; router guards (unauthenticated → login with redirect param; enrolment required → totp-setup);
team page renders and add-member form validation; SecretReveal shows the secret once; nav filtered by permissions;
i18n: every `errors.<type>` of §4.4 exists in it and en.

### 7.4 Theme tokens (`globals.css`, from `08_Developer_Tokens/porto-tokens.css`, ADR 0007)

Brand variables: `--brand-navy: #0b2542; --brand-sky: #38bdf8; --brand-black: #111827; --brand-white: #ffffff`.
Font: `--font-sans: "Titillium Web Variable", "Titillium Web", "Inter", system-ui, sans-serif` (font from
`@fontsource-variable/titillium-web`).

| Token | Light | Dark |
|---|---|---|
| `--background` / `--foreground` | `#ffffff` / `#111827` | `#07182c` / `#f8fafc` |
| `--card`, `--popover` (+ `-foreground`) | `#ffffff` / `#111827` | `#0b2542` / `#f8fafc` |
| `--primary` / `--primary-foreground` | `#0b2542` / `#ffffff` | `#38bdf8` / `#0b2542` |
| `--secondary`, `--muted` / fg | `#f1f5f9` / `#0b2542`, `#475569` | `#132f4f` / `#e2e8f0`, `#94a3b8` |
| `--accent` / `--accent-foreground` | `#e0f2fe` / `#0b2542` | `#123a5e` / `#f8fafc` |
| `--destructive` | `#b91c1c` | `#f87171` |
| `--border` / `--input` | `#e2e8f0` / `#cbd5e1` | `#1e3a5f` / `#26476e` |
| `--ring`, `--link` | `#0369a1` | `#7dd3fc` |
| `--sidebar` / `-foreground` / `-primary` / `-accent` | `#0b2542` / `#f8fafc` / `#38bdf8` / `#132f4f` | `#061424` / `#f8fafc` / `#38bdf8` / `#132f4f` |
| `--chart-1..5` | `#0b2542 #0369a1 #38bdf8 #64748b #d97706` | `#38bdf8 #7dd3fc #e0f2fe #94a3b8 #fbbf24` |
| `--radius` | `0.625rem` | — |

Sky `#38bdf8` never carries text on white (it fails 3:1). Focus rings and links use `--ring`/`--link`.
`contrast.test.ts` parses globals.css and asserts ≥ 4.5:1 for every fg/bg text pair, in light and dark, and ≥ 3:1 for
ring vs background. `theme.test.ts` asserts that the brand variables equal the values in
`assets/brand/porto/08_Developer_Tokens/porto-tokens.json`.

### 7.5 BrandLogo (`web/shared/src/ui/patterns/Brand.tsx`)

Copies in `web/shared/src/brand/` (same file names): `porto_logo_horizontal_primary.svg`,
`porto_logo_horizontal_inverted_2color.svg`, `porto_logo_horizontal_on_navy.svg`, `porto_logo_horizontal_white.svg`,
`porto_logo_horizontal_navy.svg`, `porto_symbol_primary.svg`, `porto_symbol_inverted_2color.svg`, `porto_symbol_white.svg`,
`porto_symbol_navy.svg`, `porto_app_maskable.svg`. `brand.test.ts` asserts that each one is byte-identical to its
original under `assets/brand/porto/0{1,2,3}_*`.

```tsx
type BrandLogoProps = { variant?: 'horizontal' | 'symbol'; tone?: 'auto' | 'onLight' | 'onDark' | 'onNavy';
                        size?: 'sm' | 'md' | 'lg'; className?: string }
```
`auto` renders the light-background file plus the dark one, using Tailwind `dark:hidden` / `hidden dark:block`.
`onDark` uses `inverted_2color` and `onNavy` uses `horizontal_on_navy`. Size classes keep the minimum sizes
(horizontal ≥ 120 px wide via `min-w-30`, symbol ≥ 24 px via `size-6`), and padding gives the clear space. `alt="Porto"`.
No shadows, no filters. Titillium is not loaded just for the logo (the logo is paths). Test: renders the correct `src`
per tone; minimum-size class present.

### 7.6 Booking SPA (`web/booking`)

Route `/b/$slug`: fetches `getPublicMarina`. It shows the marina name, "powered by Porto" (small symbol) and an i18n
placeholder ("La prenotazione online arriverà presto"). Not found → i18n 404 page. Budget: `scripts/budget.mjs` fails
when the gzip JS exceeds 180 KB (spec 04). Locale comes from `?lang=` or the browser (it/en).

### 7.7 Web lint scripts (`web/scripts/`, written from scratch)

`css-guard.mjs`: fails on any `.css` other than `shared/src/styles/globals.css`, `style={`, `<style`,
`.style.` assignments, hex/rgb/hsl colours in `.ts/.tsx`, and Tailwind arbitrary values (`-[`…`]`) in class strings.
Allowed exceptions sit in a commented list (empty in M0). `i18n-check.mjs`: it and en have identical key sets per
namespace, there are no empty values, and every `t('ns:key')` literal used in sources exists. Both scripts have
Vitest tests.

### 7.8 e2e (`e2e/`, WEB-owned)

`e2e/package.json` (`@playwright/test`, `@axe-core/playwright`), `playwright.config.ts`. Its `webServer` runs
`../bin/porto serve` with a temp `PORTO_DATA_DIR`, `PORTO_DEV=true`, `PORTO_MAIL_DIR` and `PORTO_ADDR=127.0.0.1:18080`.
`global-setup.ts` runs `bin/porto admin create … --password-stdin`. Specs: `login.spec.ts` (login, logout, wrong
password), `totp.spec.ts` (enrol with code computed in test via otplib-free RFC 6238 helper, re-login with TOTP),
`password-reset.spec.ts` (reads `.eml` from mail dir), `team.spec.ts`, `api-keys.spec.ts`, `webhooks.spec.ts` (create +
test delivery to a local receiver with `PORTO_WEBHOOKS_ALLOW_PRIVATE=true`), `booking.spec.ts` (placeholder),
`a11y.spec.ts` (axe: no serious/critical on login, shell, team, in light and dark).

---

## 8. Infra

### 8.1 Dockerfile (`deploy/Dockerfile`)

Multi-stage, multi-arch (`--platform=$BUILDPLATFORM` + Go cross-compile):
1. `node:22-alpine` → `npm --prefix web ci` (manifests copied first for cache), copies `web` and `api`, then `npm --prefix web run build`.
2. `golang:1.27-alpine` → `go mod download`, `COPY . .`, copies `web/admin/dist` and `web/booking/dist` into `internal/http/static/{admin,booking}`, then `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/porto ./cmd/porto`. Creates `/out/data` owned by 65532.
3. `gcr.io/distroless/static-debian12:nonroot` → `/porto`, `/data` (VOLUME), `ENV PORTO_DATA_DIR=/data PORTO_ADDR=:8080`, `EXPOSE 8080`, `USER nonroot`, `ENTRYPOINT ["/porto"]`, `CMD ["serve"]`. OCI labels (title Porto, vendor Coste360 SRL, licenses "LicenseRef-Proprietary").

`.dockerignore`: `.git node_modules web/*/dist e2e tmp bin dist data *.db* .env` and static dirs except `.gitkeep`.

### 8.2 Compose files

- `compose.yaml` (root, dev/test, **M0 acceptance**): service `porto` built from `deploy/Dockerfile`, `127.0.0.1:${PORTO_PUBLISH_PORT:-8080}:8080`,
  volume `porto-dev-data:/data`, env `PORTO_DEV=true`, `PORTO_BASE_URL=http://localhost:8080`, `PORTO_SMTP_URL=smtp://mailpit:1025`,
  `PORTO_MAIL_FROM=noreply@porto.localhost`, `PORTO_WEBHOOKS_ALLOW_PRIVATE=true`, optional `PORTO_BOOTSTRAP_OWNER_*`/`PORTO_ADMIN_PASSWORD`
  passthrough; `read_only: true`, `tmpfs: /tmp`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`; service `mailpit`
  (`axllent/mailpit`, MIT, UI on 127.0.0.1:8025). Header comment explains `docker compose up --build` and `docker compose exec porto /porto admin create …`.
- `deploy/docker-compose.yml` (production, SQLite): image `ghcr.io/coste360/porto:${PORTO_VERSION:-latest}`, `env_file: .env`, volume `porto-data:/data`, restart unless-stopped, same hardening.
- `deploy/docker-compose.postgres.yml`: adds `postgres:17-alpine` with volume and healthcheck; `DATABASE_URL=postgres://porto:${POSTGRES_PASSWORD}@postgres:5432/porto?sslmode=disable`; porto `depends_on: {postgres: {condition: service_healthy}}`.
- `.env.example`: every variable of §5 with defaults and comments (secret placeholder `PORTO_SECRET_KEY=` + how to generate: `openssl rand -base64 32`).

### 8.3 CI (`.github/workflows/ci.yml`), on push to main, pull_request, workflow_dispatch; concurrency cancel-in-progress

Use the same pinned action versions as `arenile/.github/workflows/ci.yml`. Copying the version numbers is fine.

| Job | Steps |
|---|---|
| `go` | checkout; setup-go (`go-version-file: go.mod`); `make setup` (Go part only: `go mod download` + golangci-lint); `./bin/golangci-lint run ./...`; `go test ./...`; service `postgres:17-alpine` → env `PORTO_TEST_POSTGRES_URL`; `make test-db`; `make test-race`; `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| `web` | setup-node 22 with npm cache; `npm --prefix web ci`; `run lint`; `run format:check`; `run typecheck`; `run test`; `run build`; `run budget` |
| `gen-check` | setup-go + node; `npm --prefix web ci`; `make gen-check` |
| `licenses` | `make licenses` |
| `docker` | buildx; `docker build -f deploy/Dockerfile`; Trivy scan (fail on CRITICAL/HIGH fixed); smoke: run container with `PORTO_DEV=true`, wait for `/readyz`, `docker exec … /porto admin create` with password via env, curl login → 200 |
| `e2e` | setup-go + node; `npm --prefix web ci`; `npm --prefix e2e ci`; `npx playwright install --with-deps chromium`; `make e2e`; upload report on failure |
| `naming` | `scripts/agents/check-naming.sh` (grep of §0.1; fails on hits) |

Also: `.github/pull_request_template.md` (spec refs, invariants touched, `make check-full` result) and
`.github/dependabot.yml` (gomod, npm `/web`, npm `/e2e`, github-actions; weekly).

### 8.4 Devcontainer (`.devcontainer/`)

`Dockerfile`: `mcr.microsoft.com/devcontainers/go` base, Go 1.27.2 (`ARG GO_VERSION`), golangci-lint v2.15.0 into
`/usr/local/bin`, `air`. `devcontainer.json`: name "Porto", features node 22, docker-in-docker, github-cli;
`containerEnv: {GOTOOLCHAIN: local, PORTO_DATA_DIR: ${containerWorkspaceFolder}/tmp/data}`; forward 8080/5173/5174/8025;
`postCreateCommand: make setup`; VS Code extensions go, eslint, prettier, editorconfig, yaml, playwright, openapi;
gopls `formatting.gofumpt` and `formatting.local: github.com/coste360/porto`.

### 8.5 `.claude/` and `scripts/agents/` (spec 32.2), written for Porto

- `.claude/settings.json`: allow `make *`, `go test *`, `go build *`, `go vet *`, `go tool *`, `npm run *`,
  `npm --prefix web run *`, `npx playwright *`, `npx shadcn@latest *`, read-only git (`status`, `diff`, `log`, `show`);
  deny `git push --force *`, `git reset --hard *`, `rm -rf *`, `Read(./.env)`, `Edit(../arenile/**)`, `Write(../arenile/**)`.
  Hooks: PreToolUse `Edit|Write|MultiEdit` → `scripts/agents/guard-generated.sh`; PostToolUse → `scripts/agents/format-file.sh`.
- `scripts/agents/guard-generated.sh`: blocks (exit 2 + reason) edits to `*.gen.go`, `*_gen.go`,
  `web/shared/api/generated/**`, migrations already present on `main` (`git cat-file -e main:<path>`), `assets/brand/**`,
  and any path that resolves inside the Arenile repository. It exits 0 on anything unparseable.
- `scripts/agents/format-file.sh`: gofumpt + goimports for `.go`; prettier for web files; never fails the tool call.
- `scripts/agents/check-naming.sh`: the grep of §0.1 / §11.
- Rules (`.claude/rules/*.md` with `paths:` front-matter): `go-backend.md`, `api-contract.md`, `migrations.md` (both
  dialects, same number, never edit merged), `frontend.md` (shadcn CLI only, tokens only, i18n, BrandLogo),
  `occupancy.md` (INV-01..09, unique constraints, 100-parallel tests), `geometry.md` (geo/layout: cm int64, rotation_dd,
  half-even, property tests, Go↔TS parity), `charges.md` (INV-50..53, exported immutable), `security.md` (no PII in
  logs/SSE/events, egress client for outbound HTTP, secrets via KeyRing), `arenile-reference.md` (ADR 0001: read,
  never copy, never modify).
- Agents (`.claude/agents/`): `reviewer.md` (spec conformance, dependency rules, marina scoping, tests, no Arenile copy;
  ends `APPROVE` / `CHANGES REQUESTED`), `invariants-guardian.md` (Porto INV list; ends `INVARIANTS OK` / `INVARIANTS AT
  RISK`), `test-writer.md` (table-driven, `TestXxx_Scenario`, rapid property tests, `TestConcurrency_*`, testdb on both
  dialects).
- Skills (`.claude/skills/<name>/SKILL.md`): `new-endpoint`, `new-migration`, `new-domain-event`, `new-map-element`,
  `new-price-rule-test`, `new-payment-driver`, `pre-pr-check` (runs `make check-full` + reviewer +
  invariants-guardian), `shadcn` (CLI usage in `web/shared`, export from barrel, token-only styling). The skills for
  future domains describe the procedure against the spec sections they cover. They are not code.

### 8.6 Docs (INFRA)

`docs/architecture.md` (Porto package map, request flow, outbox/event flow, auth flow, marina scoping, as built in
M0), `CHANGELOG.md` (Keep a Changelog, `## [Unreleased]` with M0 entries), `CONTRIBUTING.md` (make targets,
Conventional Commits, ADR rule, Arenile-as-reference rule, licence of dependencies), `AGENTS.md` status line
(change "specification only, no code yet (pre-M0)" to "M0 foundations in progress"), `docs/invariants.md`: add
**INV-72 — Audit log insert-only** (Verificata da: `TestAuditLog_InsertOnly`, partial) and **INV-73 — Operational
queries are marina-scoped** (Verificata da: `TestAcceptance_CrossMarinaIsolation`, partial), set INV-70 to partial
(`TestSSE_PayloadHasNoPII`, `TestEvents_DataHasNoPII`).

---

## 9. `internal/geo` and `internal/qty` (BOOTSTRAP, pure)

### 9.1 `internal/geo`

Integer geometry in centimetres. The y axis points down and the origin is top-left (spec 7.1). No floats at runtime:
trigonometry comes from a committed fixed-point table.

```go
type Cm int64                       // length
type Cm2 int64                      // area
const MaxCoord Cm = 100_000_000     // |x|,|y| bound (1000 km) so cross products fit int64
type Angle int32                    // tenths of degree, normalized 0..3599 (= rotation_dd)
func NewAngle(dd int64) Angle       // normalizes any integer (negative ok)
func (a Angle) Add(b Angle) Angle
const TrigScale int64 = 1_000_000_000
func Sin(a Angle) int64; Cos(a Angle) int64   // from sinTable[0..900] (generated, sintable_gen.go) + symmetry; exact at 0/90/180/270
type Point struct{ X, Y Cm }                  // JSON {"x":…,"y":…}
func (p Point) Add(q Point) Point; Sub(q Point) Point
func Dist2(a, b Point) int64                  // squared distance
func Cross(o, a, b Point) int64              // (a-o)×(b-o)
func Orientation(a, b, c Point) int          // -1 clockwise, 0 collinear, 1 counter-clockwise (in math orientation; doc the y-down meaning)
func Rotate(p, center Point, a Angle) Point  // half-even rounding to the nearest cm
type Box struct{ Min, Max Point }            // axis-aligned, inclusive
func (b Box) Intersects(o Box) bool; Contains(p Point) bool; Union(o Box) Box
type Segment struct{ A, B Point }
func SegmentsIntersect(s, t Segment) bool    // inclusive (touching/collinear overlap = true)
func SegmentsCrossProperly(s, t Segment) bool// interiors cross at a single point
type Rect struct {                           // rotated rectangle (berth, land resource)
    Anchor   Point // midpoint of the mooring side (spec 7.3)
    Width    Cm    // along the mooring side
    Length   Cm    // from the anchor towards Rotation
    Rotation Angle // direction from anchor to the open water side
}
func (r Rect) Validate() error              // Width>0, Length>0, coords within MaxCoord
func (r Rect) Corners() [4]Point            // anchor-left, anchor-right, far-right, far-left; consistent winding
func (r Rect) Polygon() Polygon
func (r Rect) Bounds() Box
func (r Rect) Area() Cm2                     // Width*Length (exact, not from rounded corners)
func (r Rect) Contains(p Point) bool         // inclusive, via polygon
type Polygon []Point                         // implicit closing edge
func (p Polygon) Validate() error            // >=3 points, no consecutive duplicates, non-zero area, simple (no self-intersection), coords bound
func (p Polygon) Area2() Cm2                 // twice the signed area (exact)
func (p Polygon) Area() Cm2                  // |Area2|/2 half-even
func (p Polygon) Bounds() Box
func (p Polygon) Contains(pt Point) bool     // inclusive: boundary counts as inside (crossing number + on-segment test)
func (p Polygon) ContainsStrict(pt Point) bool
func (p Polygon) IsConvex() bool
func PolygonsIntersect(a, b Polygon) bool    // inclusive: any edge intersection or containment
func ConvexOverlap(a, b Polygon) bool        // interiors overlap (touching edges/corners = false); SAT with integer edge normals
func RectsOverlap(a, b Rect) bool            // = ConvexOverlap of their polygons (berths sharing a side do not overlap)
func SegmentIntersectsRect(s Segment, r Rect) bool // inclusive
type Circle struct{ Center Point; Radius Cm }// buoys (spec 7.3)
func CirclesOverlap(a, b Circle) bool        // interiors: Dist2 < (ra+rb)^2
var ErrInvalidGeometry error
```
`sintable_gen.go` is produced by `go generate` (`//go:generate go run ./internal/gentrig` in `geo.go`; the generator is
in `internal/geo/internal/gentrig/main.go`). It holds `sin(i * 0.1°) * 1e9` rounded half-even for i = 0..900, with
the header `// Code generated … DO NOT EDIT.` The same table will be exported to `testdata/geo/sintable.json` for
Go↔TS parity later.

Tests (table-driven, `geo_test.go`, plus property tests with `pgregory.net/rapid` in `geo_rapid_test.go`):
- `TestNewAngle_Normalizes` (−1 → 3599, 3600 → 0, 7205 → 5); `TestSinCos_ExactAtRightAngles`;
  `TestSinTable_MatchesMath` (each entry within 1 of `math.Sin`·1e9; this test is the only place where float is used);
  `TestRotate_QuarterTurnsExact`; `TestRotate_FullTurnIdentity` (rapid: 36 rotations of 100 dd return the point within 1 cm per step bound).
- `TestRect_CornersAxisAligned` (rotation 0/900/1800/2700 for known anchors); `TestRect_AreaExact`;
  `TestRect_ContainsAnchor`; `TestRect_Validate`.
- `TestSegmentsIntersect_Cases` (crossing, T-touch, collinear overlap, collinear disjoint, parallel, shared endpoint);
  `TestSegmentsCrossProperly_Cases`.
- `TestPolygon_Validate` (bow-tie rejected, duplicate points, colinear-only); `TestPolygon_Area2` (square, L-shape, CW vs CCW sign);
  `TestPolygon_Contains` (inside, outside, on edge, on vertex, concave notch, ray through vertex);
  `TestPolygon_ContainsStrict`.
- `TestRectsOverlap_Cases` (identical, nested, adjacent sharing an edge = false, corner touch = false, rotated 45° crossing,
  far apart); `TestSegmentIntersectsRect_Cases`; `TestCirclesOverlap_Cases` (tangent = false).
- rapid: `TestRapid_RectsOverlapSymmetric`; `TestRapid_RectOverlapsItself`; `TestRapid_TranslationInvariance` (overlap
  unchanged by translating both rects); `TestRapid_BoundsContainCorners`; `TestRapid_ContainsImpliesBoundsContains`;
  `TestRapid_SeparatedBoxesNeverOverlap` (if the bounds do not intersect → no overlap);
  `TestRapid_AxisAlignedOverlapMatchesBoxMath` (rotation 0: overlap ⇔ open-interval box overlap);
  `TestRapid_PolygonAreaSignFlipsOnReverse`.
- Fuzz: `FuzzPolygonContains` (no panic, Contains ⇒ in Bounds).

### 9.2 `internal/qty`

```go
type Milli int64                     // value × 1000 (hours, metres, m², kWh, quantities)
const Scale = 1000
const One Milli = 1000
var ErrInvalid, ErrTooPrecise, ErrOverflow, ErrDivByZero error
func FromInt(n int64) (Milli, error)
func FromFraction(num, den int64) (Milli, error)   // half-even; e.g. minutes → hours: FromFraction(90, 60) = 1.500
func Parse(s string) (Milli, error)  // trims spaces; optional sign; digits; one '.' or ','; ≤3 decimals (more → ErrTooPrecise,
                                     // never silent rounding); no thousands separators, no exponent; overflow → ErrOverflow
func MustParse(s string) Milli
func (m Milli) String() string       // canonical: "12", "12.5", "-0.125" ('.' separator, trailing zeros trimmed)
func (m Milli) Format(decimals int, sep byte) string // fixed 0..3 decimals, half-even when decimals < 3; e.g. Format(2, ',') "12,50"
func (m Milli) Round(decimals int) Milli            // half-even to 0..3 decimals
func (m Milli) Add(o Milli) (Milli, error); Sub(o Milli) (Milli, error)   // overflow-checked
func (m Milli) Mul(o Milli) (Milli, error)          // (a*b)/1000 half-even, 128-bit intermediate (math/bits)
func (m Milli) Div(o Milli) (Milli, error)          // (a*1000)/b half-even
func (m Milli) MulInt(n int64) (Milli, error); DivInt(n int64) (Milli, error)
func (m Milli) MulCents(c money.Cents) (money.Cents, error) // unit price × quantity, half-even to the cent
func (m Milli) Sign() int; IsZero() bool; Cmp(o Milli) int
// JSON: integer (milli units); Text: canonical String/Parse; sql Valuer/Scanner as int64.
```
Internal helper `mulDivHalfEven(a, b, d int64) (int64, error)` uses 128-bit `bits.Mul64` / `bits.Div64` and handles
signs; ties round to even.

Tests:
- `TestParse_Valid` (table: "0", "1", "1.5", "1,5", "-0.125", "+3", " 2.000 ", "999999999999.999"); `TestParse_Invalid`
  ("", ".", "1.", ".5", ",5", "1.2.3", "1,2.3", "1e3", "1 000", "abc", "--1"; a separator needs digits on both sides);
  `TestParse_TooPrecise` ("1.2345"); `TestParse_Overflow`.
- `TestString_Canonical`; `TestFormat_FixedDecimals` (half-even: 1.2345 not representable; 1.125 → Format(2) "1.12",
  1.135 → "1.14", -1.125 → "-1.12"); `TestRound_HalfEven`.
- `TestMul_HalfEven` (0.001×0.5 = 0.000 (tie → even 0), 0.003×0.5 = 0.002, 1.5×1.5 = 2.25); `TestMul_Overflow`;
  `TestDiv_HalfEven` (1 / 3 = 0.333, 2 / 3 = 0.667, 1 / 8 = 0.125, 0.001 / 2 = 0.000 tie → even); `TestDiv_ByZero`;
  `TestFromFraction` (90/60 = 1.5, 1/3 = 0.333); `TestMulCents` (3.5 h × 4000 c = 14000 c; 0.333 × 100 c = 33 c (33.3); 0.005 × 100 = 0 (tie 0.5 → 0));
  `TestJSON_RoundTrip`; `TestSQL_ScanValue`.
- rapid: `TestRapid_ParseStringRoundTrip` (Parse(m.String()) == m); `TestRapid_FormatParseRoundTrip3`;
  `TestRapid_MulCommutative`; `TestRapid_MulByOneIdentity`; `TestRapid_DivMulBound` (|Div(Mul(a,b), b) − a| ≤ 1 milli for
  b ≠ 0 within ranges); `TestRapid_AddSubInverse`; `TestRapid_RoundIdempotent`; `TestRapid_MulMatchesBigRat`
  (compare against `math/big.Rat` half-even reference, test-only).
- Fuzz: `FuzzParse` (no panic; accepted ⇒ String re-parses to the same value).

BOOTSTRAP also tests the other primitives: money (`TestDivRoundHalfEven`, `TestVATSplit_SumsToGross` rapid,
`TestAllocate_SumsExactly` rapid, `TestParseCents`); calendar (`TestParse_Strict`, `TestNightsBetween`,
`TestIn_Timezone` around DST, `TestDate_SQLRoundTrip`); crypto (`TestKeyRing_EncryptDecrypt`, `TestKeyRing_Rotation`,
`TestDecrypt_WrongAAD`, `TestDerive_Deterministic`); clock (`TestFake_Advance` concurrent-safe); problem
(`TestWrite_ContentTypeAndBody`, `TestWriteError_UnmappedIsInternal`, `TestRetryAfterHeader`).

---

## 10. Work split and strict file ownership

Order: **BOOTSTRAP** runs first, alone. Then **BACKEND**, **WEB** and **INFRA** run in parallel. Each builder edits
only its own files. Generated files belong to the owner of the generator run (`internal/http/api/api.gen.go` →
BACKEND; `web/shared/api/generated/**` → WEB; `internal/geo/sintable_gen.go` → BOOTSTRAP).

| Owner | Files |
|---|---|
| **BOOTSTRAP** | `go.mod`, `go.sum` (initial), `Makefile`, `LICENSE` (`Copyright (c) 2026 Coste360 SRL. All rights reserved.`), `.editorconfig`, `.gitattributes`, `.golangci.yml`, `.gitignore` (append `/data/`, `/tmp/`, `web/*/dist`, `e2e/test-results`, `e2e/playwright-report`, `internal/http/static/*/*` except `.gitkeep`), `scripts/version.sh`, `scripts/with-postgres.sh`, `internal/clock/**`, `internal/money/**`, `internal/calendar/**`, `internal/reqctx/**`, `internal/crypto/**`, `internal/geo/**`, `internal/qty/**`, `internal/http/problem/**` |
| **BACKEND** | `internal/**` except BOOTSTRAP-owned packages, `cmd/**`, `api/**`, `pkg/**`, additions to `go.mod`/`go.sum`, `internal/http/static/{admin,booking}/.gitkeep`, Go `testdata/**` under its packages |
| **WEB** | `web/**`, `e2e/**` |
| **INFRA** | `deploy/**`, `.github/**`, `.devcontainer/**`, `.claude/**`, `scripts/agents/**`, `docs/architecture.md`, `docs/invariants.md` (§8.6 edits only), `AGENTS.md` (status line only), `CHANGELOG.md`, `CONTRIBUTING.md`, `.dockerignore`, `compose.yaml`, `.env.example` |
| orchestrator | `docs/m0-plan.md`, `README.md`, `CLAUDE.md`, `docs/spec/**`, `docs/adr/**`, `assets/**` (read-only for all builders) |

After BOOTSTRAP, its files are frozen. A builder that needs a change there reports it to the orchestrator.

### 10.1 BOOTSTRAP steps
1. `go mod init github.com/coste360/porto`; `go 1.27.2`; tool directives (§6); `require pgregory.net/rapid` (tests).
2. Write the primitives (§1.2, §9) with tests. Run `go generate ./internal/geo/...`.
3. Write the Makefile, scripts, `.golangci.yml`, `.editorconfig` (LF, utf-8, tabs for Go/Makefile, 2 spaces for
   web/yaml/json/md), `.gitattributes` (`* text=auto eol=lf`, `*.gen.go linguist-generated`,
   `web/shared/api/generated/** linguist-generated`, binary for png/ico).
4. Done when `go build ./... && go test ./... && make lint && make help` pass (web/e2e parts print skip notices).

### 10.2 BACKEND steps (in order; B1 first because WEB depends on it)
- **B1** `api/openapi.yaml` (complete per §4) + `api/oapi-codegen.yaml`; `go tool oapi-codegen …` → `api.gen.go`.
  Tell the orchestrator when B1 is done so that WEB can run `npm --prefix web run gen`.
- **B2** `internal/migrations` (two `00001_init.sql`, runner, tests), `internal/store` (Open, RunInTx, scopes, models,
  repositories), `internal/testutil/testdb`.
- **B3** `internal/config`; `internal/domain/{audit,event,auth,team,marina,apikey,device,webhook,notify,meta}`; `pkg/ext`.
- **B4** `internal/jobs`; `internal/http/{middleware,sse,static,admin,device,public}`, `server.go`, `errmap.go`.
  `static` serves `/admin/*` (SPA fallback to index.html, assets with immutable cache), `/b/{slug}` → booking
  index.html, `/favicon.ico`, plus a placeholder page when `index.html` is missing.
- **B5** `internal/app`, `internal/backup`, `cmd/porto` (all commands of §1.4), bootstrap owner, `seed demo`.
- **B6** tests: unit (`TestXxx_Scenario`); integration (tag `integration`, both dialects via `testdb`) for every store
  file and service; concurrency (`TestConcurrency_IdempotencyKeySingleExecution`,
  `TestConcurrency_PairingCodeSingleUse`, `TestConcurrency_LastOwnerAlwaysKept`,
  `TestConcurrency_OutboxClaimNoDoubleDispatch`, each with ≥100 goroutines); security (`TestEgress_BlocksPrivate`
  with table of addresses, `TestEgress_DNSRebinding` (resolver returns public then private),
  `TestCSRF_RequiredForCookieWrites`, `TestLogin_RateLimited`, `TestLogin_NoUserEnumeration` (same error and similar
  work for unknown email via dummy hash), `TestPasswordReset_SingleUse`, `TestTOTP_ReplayRejected`,
  `TestSession_RotatedOnLogin`, `TestAPIKey_CannotHaveUsersManage`, `TestAuthorize_OtherMarinaIs404`,
  `TestSSE_PayloadHasNoPII`, `TestEvents_DataHasNoPII`, `TestLogs_NoPII` (capture slog during an acceptance run, assert
  that no email/token appears)); webhook (`TestSign_Vector` with fixed secret/time/body → known header,
  `TestDeliverer_BackoffAndDisable`, `TestDeliverer_RotationSendsBothSignatures`); notify (`TestTransport_File`,
  `TestDeliver_DedupeAndRetry`); backup (`TestBackup_RoundTripSQLiteToPostgres`, `TestRestore_RejectsSchemaMismatch`);
  CLI (`TestCLI_AdminCreate`, `TestCLI_MigrateStatus`, `TestCLI_Version`).
- **B7** `internal/acceptance` (tag integration): `TestAcceptance_M0` drives the API through `httptest`: bootstrap via
  `auth.CreateOwner`, login, me, create a second marina, add member, member login, member forbidden on users.manage
  (403), API key reads marina but cannot manage members, pairing → device token → `/device/v1/self`, webhook endpoint
  on a local receiver gets a signed `member.added` delivery that verifies, SSE receives `team.changed`, audit rows exist
  for every write, idempotent replay. `TestAcceptance_CrossMarinaIsolation`: a user of marina A gets 404 on every
  marina-B route (iterate over every route with `{marinaId}` taken from the chi router).

### 10.3 WEB steps
1. Workspaces, tsconfig, eslint/prettier/vitest configs, scripts (§7.1, §7.7).
2. `web/shared`: components.json, then run the shadcn CLI (§7.2), theme/globals.css (§7.4), brand copies + BrandLogo
   (§7.5), i18n, lib, testing.
3. Until B1 lands, build against `web/shared/src/api/types.ts` placeholders shaped like §4. Once B1 lands, run
   `npm --prefix web run gen` and switch to the generated `paths`. No hand-written API types may remain.
4. Admin pages (§7.3), booking (§7.6), tests, e2e (§7.8).
5. Done when `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run test && npm --prefix web run build && npm --prefix web run budget` pass.

### 10.4 INFRA steps
1. Dockerfile, `.dockerignore`, compose files, `.env.example` (§8.1–8.2).
2. CI workflow, PR template, dependabot (§8.3); `scripts/agents/*` (§8.5).
3. Devcontainer (§8.4); `.claude/**` (§8.5).
4. Docs (§8.6).
5. Done when `docker compose config` validates, `actionlint` (if available) passes on the workflow, the hook scripts
   pass `shellcheck` and a dry-run (`echo '{"tool_input":{"file_path":"internal/http/api/api.gen.go"}}' | scripts/agents/guard-generated.sh; echo $?` → 2).

---

## 11. M0 acceptance (spec 32.3) — exact commands

Spec 32.3 requires that `docker compose up` starts an empty instance, that `porto admin create` makes login possible,
and that CI is green on SQLite and PostgreSQL.

```bash
cd porto

# 1. Toolchain and fast gate
make setup
make check                                   # gen-check + lint + test, exit 0

# 2. Full gate (needs docker for the ephemeral PostgreSQL)
make test-db                                 # integration on SQLite + PostgreSQL, exit 0
make test-race                               # >=4 TestConcurrency_* found, all pass with -race
make e2e                                     # Playwright green
go test ./internal/geo/... ./internal/qty/... -count=1 -run . -rapid.checks=2000

# 3. Empty instance from compose
docker compose up --build -d
curl -fsS http://localhost:8080/healthz      # {"status":"ok"}
curl -fsS http://localhost:8080/readyz       # {"status":"ok","schema_version":1}
docker compose exec porto /porto version
docker compose exec porto /porto migrate status   # 00001_init.sql applied
docker compose exec -e PORTO_ADMIN_PASSWORD='prova-password-123' porto \
  /porto admin create --email owner@porto.test --name "Owner Prova" --org "Porto Prova SRL" --marina "Marina Prova" --slug prova

# 4. Login works (cookie + CSRF)
curl -fsS -c /tmp/porto.jar -H 'Content-Type: application/json' \
  -d '{"email":"owner@porto.test","password":"prova-password-123"}' http://localhost:8080/api/admin/v1/auth/login
curl -fsS -b /tmp/porto.jar http://localhost:8080/api/admin/v1/auth/me          # 200, marinas[0].slug == "prova"
curl -s -o /dev/null -w '%{http_code}\n' -b /tmp/porto.jar -X POST http://localhost:8080/api/admin/v1/auth/logout   # 403 (no CSRF header)
curl -fsS -o /dev/null -w '%{http_code}\n' http://localhost:8080/admin/          # 200, SPA served
curl -fsS -o /dev/null -w '%{http_code}\n' http://localhost:8080/b/prova         # 200, booking placeholder
curl -fsS http://localhost:8080/api/public/v1/prova                               # {"slug":"prova",...}

# 5. Backup / restore round trip
docker compose exec porto /porto backup --out /data/m0.porto-backup
docker compose exec porto /porto restore --in /data/m0.porto-backup --force
docker compose down -v

# 6. PostgreSQL deployment smoke
docker compose -f deploy/docker-compose.postgres.yml config >/dev/null

# 7. Naming / no Arenile residue
scripts/agents/check-naming.sh               # = ! grep -rniE 'arenile|establishment' cmd internal pkg api web/admin/src web/booking/src web/shared/src deploy compose.yaml

# 8. Licences
make licenses

# 9. CI green on main (after the orchestrator pushes): jobs go (SQLite+PostgreSQL), web, gen-check, licenses, docker, e2e, naming
gh run list --workflow ci.yml --branch main --limit 1 --json conclusion -q '.[0].conclusion'   # "success"
```
