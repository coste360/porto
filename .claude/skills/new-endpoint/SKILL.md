---
name: new-endpoint
description: Add a Porto REST operation end to end - OpenAPI first, make gen, thin handler, domain service with permission check and marina scoping, store method, tests. Use when a task asks for a new API operation.
argument-hint: "[METHOD] [path]"
---
# New endpoint: $ARGUMENTS

Read first: `docs/spec/27-api-eventi-estensioni.md`, `docs/spec/26-utenti-ruoli.md` (26.1 permissions),
`docs/architecture.md`, `.claude/rules/api-contract.md`, `.claude/rules/go-backend.md`.

1. **Contract.** In `api/openapi.yaml` copy the style of the closest existing operation in the same tag:
   `operationId` camelCase verb first, snake_case schemas in `components/schemas`, reusable problem responses
   (`BadRequest`, `NotFound`, `Conflict`, ...), the right `security` block (admin read / admin unsafe with
   `csrfToken` / device / none), `Idempotency-Key` for creating POSTs, `ETag` + `If-Match` for versioned
   resources, `?cursor=&limit=` for collections.
2. **Generate.** `make gen`. Never edit `internal/http/api/api.gen.go` or `web/shared/api/generated/**`. The build
   now fails until the new method of the generated interface is implemented.
3. **Domain.** In `internal/domain/<area>` add the service method. First line:
   `scope, err := p.Authorize(ctx, db, marinaID, auth.Perm<...>)` (another marina → `ErrNotFound`, missing
   permission → `ErrForbidden`). Writes run in `store.RunInTx` and write the audit row and, if the change is
   observable, the outbox event in the same transaction (skill `/new-domain-event`).
4. **Store.** SQL only in `internal/store`, every query filtered by `scope.MarinaID`. New table or column →
   skill `/new-migration`.
5. **Handler.** In `internal/http/<admin|device|public>`: decode, call the service, encode. Map new sentinel
   errors to problem slugs in `internal/http/errmap.go`; a new slug also needs `errors.<slug>` in the it/en
   locale files (ask the web owner if it is not your area).
6. **Tests.**
   - domain unit, table-driven `TestXxx_Scenario`, `clock.Fake`;
   - handler via the real router (`httptest`): status, problem `type`, `ETag`, idempotent replay;
   - cross-marina 404 and missing-permission 403; API key and device principals where relevant;
   - store integration test with `//go:build integration` on both dialects (`testdb`);
   - make sure `TestAcceptance_CrossMarinaIsolation` covers the new `{marinaId}` route.
7. **Web** (if needed): a TanStack Query hook in `web/shared` over the generated client; strings via i18n.
8. `make check`, then `make test-db`. Report changed files and tests added.
