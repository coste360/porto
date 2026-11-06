---
name: new-migration
description: Add a database migration to Porto for both SQLite and PostgreSQL with the same number, matching schema, a working Down, store models and backup registration. Use whenever a table, column, index or constraint changes.
argument-hint: "[short_snake_case_name]"
---
# New migration: $ARGUMENTS

Read first: `.claude/rules/migrations.md`, `docs/spec/06-schema-dati.md` (the section for the table),
`docs/m0-plan.md` §2 for the conventions used by `00001_init.sql`.

1. **Number.** `ls internal/migrations/sqlite internal/migrations/postgres`; the next number is the highest + 1
   (five digits). Name: `NNNNN_$ARGUMENTS.sql`, identical in both directories.
2. **Never edit an existing migration on `main`** (the hook refuses). Changing a recent unmerged one on your
   branch is fine.
3. **SQLite file.** `-- +goose Up` / `-- +goose Down`. `TEXT` ids, `INTEGER` for cents/cm/milli and booleans with
   `CHECK (x IN (0,1))`, `TEXT` timestamps RFC 3339 UTC, `length()` in CHECKs. Recreate tables for changes SQLite
   cannot `ALTER`.
4. **PostgreSQL file.** Same tables, columns, constraint and index names. `text`/`uuid` as used in `00001`,
   `bigint`, `boolean`, `timestamptz`, `jsonb`, `char_length()`. Functions/triggers between
   `-- +goose StatementBegin` and `-- +goose StatementEnd`.
5. **Rules.** Operational tables carry `marina_id` (FK) with an index starting with it; rules that must hold
   under concurrency are constraints (unique/check/FK), not Go code; money/lengths/quantities are integers.
6. **Down** reverts Up exactly (drop in reverse FK order).
7. **Store.** Update models and repositories in `internal/store`; register new tables in `store.BackupTables()`
   in FK order (or exclude them on purpose with a comment).
8. **Tests.** `TestMigrations_SameNumbering`, `TestMigrations_UpDownUp`, `TestMigrations_SchemaParity` must pass;
   add a constraint test for each new invariant-bearing constraint (both dialects), and keep
   `TestBackup_RoundTripSQLiteToPostgres` green.
9. `make test-db` (SQLite + PostgreSQL). Report the file names and the schema change.
