---
paths:
  - "internal/migrations/**"
  - "internal/store/**"
---
# Migrations (Porto)

Spec: 06 (schema), 03 (SQLite and PostgreSQL); `docs/m0-plan.md` §2.

- goose, one directory per dialect: `internal/migrations/sqlite/` and `internal/migrations/postgres/`. Every
  migration exists in **both** with the **same file name** (`NNNNN_snake_case.sql`, next number, no gaps).
  `TestMigrations_SameNumbering` and `TestMigrations_SchemaParity` fail otherwise.
- A migration already on `main` is never modified (the PreToolUse hook blocks it). Fix forward with a new one.
- Each file has `-- +goose Up` and `-- +goose Down`; Down really reverts Up (`TestMigrations_UpDownUp`).
  Multi-statement PostgreSQL functions/triggers go inside `-- +goose StatementBegin` / `StatementEnd`.
- Same logical schema on both dialects: same tables, columns, index and constraint names. Differences are only
  types and syntax: `text`/`timestamptz`/`boolean`/`jsonb`/`bytea` on PostgreSQL, `TEXT`/`INTEGER` with CHECKs on
  SQLite; `char_length()` vs `length()`.
- Ids UUIDv7 text; money `bigint` cents; lengths `bigint` cm; quantities `bigint` milli; dates `YYYY-MM-DD` text
  on SQLite and `date` on PostgreSQL only if both round-trip identically through `calendar.Date`.
- Every operational table has `marina_id` (FK to `marinas`) and an index starting with it. Tables that must
  belong to the same organization use a composite FK `(organization_id, marina_id)`.
- Invariants that must hold under concurrency are DB constraints (unique, check, FK), never application checks
  (ADR 0003): the occupancy tables of M3 rely on unique keys per `(resource, night)`.
- `audit_log` is insert-only on both dialects (trigger), INV-72.
- Tables written by `internal/backup` are registered in FK order in `store.BackupTables()`; a new table must be
  added there (or explicitly excluded) and covered by the backup round-trip test.
- Run `make test-db` (SQLite + PostgreSQL) before declaring a migration done. Skill: `/new-migration`.
