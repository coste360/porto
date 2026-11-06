---
paths:
  - "**/*.go"
  - "go.mod"
---
# Go backend (Porto)

Spec: 03 (stack, dependency rules), 04 (layout), `docs/architecture.md`, `docs/m0-plan.md` §1.

- Layering: `internal/http/*` decodes, calls a domain service, encodes. Logic lives in `internal/domain/<area>`.
  SQL lives only in `internal/store` (Bun). `pkg/ext` imports nothing from `internal/`. depguard in
  `.golangci.yml` enforces this; do not add exclusions to make it pass.
- Pure packages (`geo`, `qty`, and later `layout`, `fit`, `pricing`, `accrual`, `formula`, `optimizer`): no
  `store`, no `clock`, no `time.Now`, no randomness without an explicit seed.
- Types: money `money.Cents`, lengths `geo.Cm` / areas `geo.Cm2`, fractional quantities `qty.Milli`, dates
  `calendar.Date` in the marina time zone. No `float32`/`float64` in domain logic.
- Time: inject `clock.Clock`; tests use `clock.Fake`. `time.Now` is banned outside `internal/clock` (forbidigo).
- IDs are UUIDv7. `context.Context` is the first parameter of anything doing I/O.
- Scoping: every operational query takes `marina_id` from the authorised scope
  (`Principal.Authorize(ctx, db, marinaID, perm)` → `store.MarinaScope`). A resource of another marina is
  `ErrNotFound` (404), never 403: existence must not leak.
- Permissions are code constants (spec 26.1) checked in the domain service, not in the handler.
- Writes that change state also write the audit row and, when relevant, the outbox event in the same
  transaction (`store.RunInTx`).
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`; domain errors are sentinel values per package, mapped
  to problem types in `internal/http/errmap.go`.
- Logging: `log/slog`, snake_case keys, key-value pairs. Never log emails, phone numbers, documents, tokens,
  passwords or secrets. `fmt.Print*` only under `cmd/`.
- Outbound HTTP only through the egress client of `internal/domain/event` (anti-SSRF).
- Tests: same package, table-driven, `TestXxx_Scenario`. DB tests carry `//go:build integration` and use
  `internal/testutil/testdb` (SQLite always, PostgreSQL when `PORTO_TEST_POSTGRES_URL` is set). Concurrency
  tests are `TestConcurrency_*` with at least 100 goroutines and pass under `-race`. Property tests use
  `pgregory.net/rapid`. `go test ./...` must not need Docker or network.
- Never edit `*.gen.go` / `*_gen.go`: change the source and run `make gen`.
- Formatting: gofumpt + goimports with local prefix `github.com/coste360/porto` (hook does it after each edit).
