## What and why

<!-- What changes, and the problem it solves. Title in Conventional Commits form: feat(team): ..., fix(auth): ... -->

## Spec and decisions

<!-- Sections of docs/spec/ implemented or touched (e.g. 26.2, 27.3) and ADRs involved. A changed decision needs a new ADR. -->

- Spec:
- ADR:

## Invariants touched

<!-- INV-xx from docs/invariants.md affected by this change, and the test that verifies each. Write "none" if none. -->

## How it was tested

- [ ] `make check-full` green (paste the summary)
- [ ] New behaviour has tests; a bug fix has a test that fails without the fix
- [ ] Integration tests run on SQLite and PostgreSQL (`make test-db`)
- [ ] `TestConcurrency_*` added where parallel writes on the same resource are possible

```
make check-full
```

## Checklist

- [ ] API changes start from `api/openapi.yaml` + `make gen`; no generated file edited by hand
- [ ] New migrations exist for both `sqlite/` and `postgres/`; no merged migration modified
- [ ] Every operational query is scoped by `marina_id` from the auth context
- [ ] No personal data, tokens or secrets in logs, SSE payloads or events
- [ ] User-facing strings use i18n keys (Italian and English)
- [ ] UI uses shadcn/ui components and theme tokens only
- [ ] New dependencies are MIT, BSD, Apache-2.0, ISC or MPL-2.0 and justified here
- [ ] `CHANGELOG.md` (Unreleased) and `docs/` updated
- [ ] No code copied from the reference project (ADR 0001)
