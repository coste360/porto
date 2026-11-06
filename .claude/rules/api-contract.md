---
paths:
  - "api/**"
  - "internal/http/**"
  - "web/shared/api/**"
---
# API contract (Porto)

Spec: 27 (surfaces, events, SSE), 26 (auth), 30 (security); `docs/m0-plan.md` §4.

- API first: change `api/openapi.yaml` (OpenAPI 3.1), run `make gen`, then implement. The Go server
  (`internal/http/api/api.gen.go`) and the TS client (`web/shared/api/generated/**`) are generated: never edit
  them; `make gen-check` in CI fails on drift.
- Surfaces: `/api/admin/v1` (session or `po_live_` key), `/api/device/v1` (`po_dev_` token),
  `/api/public/v1/{slug}` (no auth, rate limited). Later: `/api/ingest/v1`, `/webhooks/...`.
- JSON is snake_case. Instants RFC 3339 UTC, dates `YYYY-MM-DD`, ids UUID strings, money integer cents,
  lengths integer centimetres. No floats on the wire for money or measures.
- Collections: `?cursor=&limit=` (default 50, max 200) → `{ "items": [...], "next_cursor": string|null }`.
- Versioned resources return `ETag`; PATCH needs `If-Match` (missing → 428 `precondition_required`,
  mismatch → 412 `version_conflict`).
- Every creating POST accepts `Idempotency-Key`; same key + same body replays the stored response, same key +
  different body → 422 `idempotency_key_reused`, still running → 409 `idempotency_in_progress`.
- Cookie-authenticated unsafe requests send `X-CSRF-Token` equal to the `porto_csrf` cookie; bearer auth is
  exempt. Declare the `csrfToken` security requirement next to `cookieAuth` on unsafe operations.
- Every error is `application/problem+json` with a stable `type` slug (list in
  `internal/http/problem/types.go`). A new slug needs the i18n key `errors.<slug>` in it and en.
- Paths with `{marinaId}` resolve the marina through `Principal.Authorize`; another marina's resource is 404.
- Handlers stay thin: decode, call the domain service, map sentinel errors (`errmap.go`), encode.
- Responses never carry secrets except at creation time (API key, device token, recovery codes) and never PII
  in SSE or event payloads (INV-70).
- Webhooks out are signed with `Porto-Signature: t=<unix>,v1=<hex hmac-sha256>`; envelope in m0-plan §4.5.
- Each new operation gets: a handler test asserting status and problem `type`, a cross-marina 404 test, and an
  entry in the acceptance route sweep (`TestAcceptance_CrossMarinaIsolation`).
