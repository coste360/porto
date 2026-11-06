//go:build integration

package store_test

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// The portable column kinds must not depend on the dialect, or a backup would not move between them.
func TestBackupColumns_DialectsAgree(t *testing.T) {
	if os.Getenv(testdb.EnvPostgresURL) == "" {
		t.Skip("needs PostgreSQL")
	}
	ctx := context.Background()
	lite, pg := testdb.SQLite(t), testdb.Postgres(t)
	for _, table := range append(store.BackupTables(), store.BackupExcludedTables()[:2]...) {
		a, err := store.TableColumns(ctx, lite.DB, migrations.SQLite, table)
		if err != nil {
			t.Fatal(err)
		}
		b, err := store.TableColumns(ctx, pg.DB, migrations.Postgres, table)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s:\nsqlite   %v\npostgres %v", table, a, b)
		}
	}
}

// Every table of the schema is either backed up or explicitly excluded.
func TestBackupTables_CoverSchema(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		q := "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
		if db.IsPostgres() {
			q = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
		}
		rows, err := db.QueryContext(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		known := map[string]bool{}
		for _, n := range append(store.BackupTables(), store.BackupExcludedTables()...) {
			known[n] = true
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			if !known[name] {
				t.Errorf("table %s is neither in BackupTables nor in BackupExcludedTables", name)
			}
		}
	})
}
