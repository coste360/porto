# Contributing to Porto

Porto is developed by Coste360 SRL and published as source-available software under the
[Business Source License 1.1](LICENSE) (ADR 0018). Bug reports, ideas and pull requests are welcome. Read
`AGENTS.md` first; the specification in `docs/spec/` (Italian) is authoritative.

## How to contribute

- **Security problems**: never in a public issue. Follow [SECURITY.md](SECURITY.md) (security@coste360.it).
- **Bugs**: open an issue with the version (`porto version`, or the image tag), the edition (server, Docker,
  desktop), the database (SQLite or PostgreSQL), the steps to reproduce and what you expected. Never attach real
  customer data, logs with personal data or your `PORTO_SECRET_KEY`.
- **Features**: open an issue before writing code. Porto follows its specification and the decisions in
  `docs/adr/`; a change of behaviour may need a spec change or a new ADR, and some topics are deliberately open
  (`docs/spec/33-decisioni-aperte.md`).
- **Pull requests**: small and focused, one topic each, with tests. Issues and pull requests may be written in
  Italian or English; code, identifiers and comments are in English.
- **Translations**: Italian is the source language; English is required for every string. Corrections to either
  are welcome.

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
- Before opening a pull request run `make check-full` (maintainers' coding agents: the `/pre-pr-check` skill) and fill in
  `.github/pull_request_template.md`: spec sections, invariants touched, test results.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md` for user-visible changes.
- CI must be green: Go on SQLite and PostgreSQL, web, generated code, licences, Docker, e2e, naming.
- Releases are cut by the maintainers ([docs/release.md](docs/release.md)).

## Licence of contributions

Porto is licensed under the Business Source License 1.1 with the Additional Use Grant for Marina Facilities in
[`LICENSE`](LICENSE); each version converts to the GNU AGPL v3.0 or later three years after its first public
release (ADR 0018). By opening a pull request you agree that your contribution is provided under the same terms as
the repository and that you have the right to submit it (it is your own work, or you include the licence and
copyright notices of the work it comes from, which must be compatible with these terms).

No contributor licence agreement (CLA) or DCO sign-off is required at the moment. Whether one will be asked for is
an open decision (D15 in `docs/spec/33-decisioni-aperte.md`); if that changes, this file will say so before it
applies to new pull requests.

The name Porto, the logo and every file in `assets/brand/` are reserved by Coste360 SRL and not covered by the code
licence (ADR 0007): do not change them in a pull request, and give a fork its own name and logo. Copyright (c) 2026
Coste360 SRL.
