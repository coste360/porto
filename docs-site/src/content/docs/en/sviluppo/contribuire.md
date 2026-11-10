---
title: Contributing
description: Reports, pull requests, project rules and licence of contributions.
sidebar:
  order: 6
---

Reports from marinas and boatyards, fixes, translations, drivers and documentation are welcome. The full rules are in
[CONTRIBUTING.md](https://github.com/coste360/porto/blob/main/CONTRIBUTING.md).

## Before you start

1. Read `AGENTS.md`, the [specification](https://github.com/coste360/porto/tree/main/docs/spec) and the
   [ADRs](https://github.com/coste360/porto/tree/main/docs/adr).
2. Open an issue before a non-trivial change. The
   [open decisions](https://github.com/coste360/porto/blob/main/docs/spec/33-decisioni-aperte.md) are not decided in
   code.
3. **Vulnerabilities**: never in a public issue, write to security@coste360.it
   ([SECURITY.md](https://github.com/coste360/porto/blob/main/SECURITY.md)).

## Main rules

- **API first**: change `api/openapi.yaml`, run `make gen`, then implement. Never edit generated files.
- **Migrations** for SQLite and PostgreSQL with the same number; a migration already on `main` is never changed.
- **Invariants** always true and checked by tests ([Invariants](/en/sviluppo/invarianti/)).
- **UI** only with shadcn/ui components and theme tokens; every string is an i18n key (Italian source, English
  required).
- **Privacy**: never emails, phone numbers, documents, tokens or secrets in logs.
- **Dependencies** licensed MIT, BSD, Apache-2.0, ISC or MPL-2.0 (OFL-1.1 for fonts only): `make licenses`.
- Code, identifiers and comments in English; issues and pull requests in Italian or English.

## Pull requests

- Small and on one topic, with tests, [Conventional Commits](https://www.conventionalcommits.org/) messages.
- A line in `CHANGELOG.md` for user-visible changes.
- `make check` always; `make check-full` for changes to occupancy, pricing, charges, payments, migrations or the
  public flow.

## Documentation

This site lives in `docs-site/` (Astro Starlight). Pages are in `src/content/docs/` (Italian) and
`src/content/docs/en/` (English) with the same paths. `make docs-dev` runs the site with live reload; `make docs-build`
also checks every internal link.

## Licence of contributions

Contributions are distributed under the project licence (BSL 1.1, then AGPL-3.0). Contributing gives no right to
resell Porto or offer it as a service to third parties. The Porto name and brand files are not covered by the code
licence and are not changed in pull requests. See [Licence](/en/sviluppo/licenza/).
