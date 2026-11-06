---
name: pre-pr-check
description: Final gate before opening or updating a Porto pull request - make check-full, generated code and migration hygiene, naming, docs and changelog, reviewer and invariants-guardian subagents, PR description from the template.
---
# Pre-PR check

1. **Scope.** `git status`, `git diff main...HEAD --stat`. Flag files outside the task.
2. **Full gate.** `make check-full` (gen-check, lint, test, test-db on SQLite + PostgreSQL, test-race, e2e). Fix
   and re-run until green; never continue with a failure.
3. **Generated code.** `*.gen.go`, `*_gen.go` and `web/shared/api/generated/**` change only together with their
   source (`api/openapi.yaml`, generators); `make gen` leaves no diff.
4. **Migrations.** `git diff main...HEAD --name-status -- internal/migrations` shows only `A` (added) entries;
   every new file exists in both `sqlite/` and `postgres/` with the same name.
5. **Naming.** `scripts/agents/check-naming.sh` passes (no `arenile` / `establishment` in code).
6. **Licences.** New dependencies? `make licenses` passes and the PR says why each is needed.
7. **Tests.** New behaviour is tested; bug fixes have a failing-before test; parallel writes have
   `TestConcurrency_*`.
8. **Invariants.** If the change touches occupancy, geometry, pricing, contracts, rights, charges, payments,
   reports, audit, events or scoping, run the `invariants-guardian` subagent and resolve `INVARIANTS AT RISK`;
   update states in `docs/invariants.md`.
9. **Docs.** `CHANGELOG.md` entry under `## [Unreleased]`; `docs/architecture.md` / spec references updated; a
   new ADR if a decision changed.
10. **Review.** Run the `reviewer` subagent and resolve every `CHANGES REQUESTED` item.
11. **PR text** from `.github/pull_request_template.md`: what and why, spec sections and ADRs, invariants touched,
    the `make check-full` summary, open questions. Title in Conventional Commits form.

Done when `make check-full` is green, the reviewer says `APPROVE` and (if run) the guardian says `INVARIANTS OK`.
