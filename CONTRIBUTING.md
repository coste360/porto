# Contributing to Porto

The repository is private for now: contributions come from the Coste360 SRL team and its coding agents. Read
`AGENTS.md` first; the specification in `docs/spec/` (Italian) is authoritative.

## Setup and daily commands

Requirements: Go (version in `go.mod`), Node 22, Docker (for PostgreSQL tests and compose). Or open the repository
in the dev container (`.devcontainer/`).

```bash
make setup        # Go modules, golangci-lint in ./bin, web and e2e dependencies
make dev          # backend on :8080 + Vite dev servers (admin :5173, booking :5174)
make seed         # demo marina in tmp/data
make gen          # regenerate code after changing api/openapi.yaml
make check        # fast gate: gen-check, lint, test
make check-full   # full gate: check + test-db (SQLite + PostgreSQL) + test-race + e2e
docker compose up --build   # empty instance on http://localhost:8080, mail in Mailpit on :8025
```

## Rules

- **Spec first.** Implement what the cited spec section says. Open questions (`docs/spec/33-decisioni-aperte.md`)
  are not decided in code: ask.
- **ADR rule.** A change to an existing decision needs a new ADR in `docs/adr/`; never rewrite an accepted one.
- **API first.** Change `api/openapi.yaml`, run `make gen`, then implement. Never edit generated files.
- **Migrations** are added for both SQLite and PostgreSQL with the same number; a migration on `main` is never
  modified.
- **Invariants** in `docs/invariants.md` must stay true; each one names the test that verifies it.
- **Arenile is a reference, not a source** (ADR 0001): read `../arenile` to understand how a problem was solved,
  then write Porto's own code with Porto's names. Do not copy its files or code, and never modify it.
- **UI** uses shadcn/ui components added with `npx shadcn@latest add`, Tailwind on theme tokens only, i18n keys
  for every string (Italian source, English required), the logo only through `BrandLogo`.
- **Privacy.** Never log emails, phone numbers, documents, tokens or secrets.
- **Dependencies** must be licensed MIT, BSD-2/3-Clause, Apache-2.0, ISC or MPL-2.0 (OFL-1.1 only for fonts).
  `make licenses` checks them; justify every new dependency in the pull request.
- Code, identifiers and comments in English.

## Commits and pull requests

- Small, scoped changes. [Conventional Commits](https://www.conventionalcommits.org/):
  `feat(team): add invitation resend`, `fix(auth): rotate session on TOTP confirm`, `docs: ...`, `ci: ...`.
- Before opening a pull request run `make check-full` (or the `/pre-pr-check` skill) and fill in
  `.github/pull_request_template.md`: spec sections, invariants touched, test results.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md` for user-visible changes.
- CI must be green: Go on SQLite and PostgreSQL, web, generated code, licences, Docker, e2e, naming.

## Licence

Copyright (c) 2026 Coste360 SRL. All rights reserved (see `LICENSE`). The brand files in `assets/brand/` are
reserved and not covered by any code licence.
