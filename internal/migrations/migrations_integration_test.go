//go:build integration

package migrations_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestMigrations_UpDownUp(t *testing.T) {
	testdb.RunUnmigrated(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		r, err := db.Migrator()
		if err != nil {
			t.Fatal(err)
		}
		if v, err := r.Version(ctx); err != nil || v != 0 {
			t.Fatalf("empty db version = %d, %v", v, err)
		}
		if ok, _ := r.Aligned(ctx); ok {
			t.Fatal("empty db reported aligned")
		}
		applied, err := r.Up(ctx)
		if err != nil || len(applied) == 0 {
			t.Fatalf("up: %v %v", applied, err)
		}
		if ok, err := r.Aligned(ctx); err != nil || !ok {
			t.Fatalf("aligned after up = %v, %v", ok, err)
		}
		if err := r.Reset(ctx); err != nil {
			t.Fatalf("down: %v", err)
		}
		if tables := tableNames(t, db); len(tables) != 0 {
			t.Fatalf("tables left after down: %v", tables)
		}
		if _, err := r.Up(ctx); err != nil {
			t.Fatalf("second up: %v", err)
		}
		st, err := r.Status(ctx)
		if err != nil || len(st) == 0 || !st[0].Applied || st[0].Name != "00001_init.sql" || st[0].AppliedAt == "" {
			t.Fatalf("status = %+v, %v", st, err)
		}
		latest, _ := migrations.Latest(db.Kind)
		if v, _ := r.Version(ctx); v != latest {
			t.Fatalf("version %d, want %d", v, latest)
		}
	})
}

func TestMigrations_SchemaParity(t *testing.T) {
	lite := testdb.SQLite(t)
	pg := testdb.Postgres(t)
	ctx := context.Background()

	liteTables := tableNames(t, lite)
	pgTables := tableNames(t, pg)
	if !slices.Equal(liteTables, pgTables) {
		t.Fatalf("tables differ:\nsqlite   %v\npostgres %v", liteTables, pgTables)
	}

	var liteCols, pgCols []string
	for _, tbl := range liteTables {
		var names []string
		if err := lite.NewRaw("SELECT name FROM pragma_table_info(?) ORDER BY cid", tbl).Scan(ctx, &names); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			liteCols = append(liteCols, tbl+"."+n)
		}
	}
	var rows []struct {
		Table  string `bun:"table_name"`
		Column string `bun:"column_name"`
	}
	if err := pg.NewRaw(`SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name <> 'goose_db_version' ORDER BY table_name, ordinal_position`).
		Scan(ctx, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		pgCols = append(pgCols, r.Table+"."+r.Column)
	}
	slices.Sort(liteCols)
	slices.Sort(pgCols)
	if !slices.Equal(liteCols, pgCols) {
		t.Fatalf("columns differ:\nsqlite   %v\npostgres %v", liteCols, pgCols)
	}

	var liteIdx, pgIdx []string
	if err := lite.NewRaw(`SELECT name FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL ORDER BY name`).
		Scan(ctx, &liteIdx); err != nil {
		t.Fatal(err)
	}
	if err := pg.NewRaw(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema()
		AND indexname NOT LIKE '%_pkey' AND indexname NOT LIKE '%_key' AND tablename <> 'goose_db_version'
		ORDER BY indexname`).Scan(ctx, &pgIdx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(liteIdx, pgIdx) {
		t.Fatalf("explicit indexes differ:\nsqlite   %v\npostgres %v", liteIdx, pgIdx)
	}
}

func tableNames(t *testing.T, db *store.DB) []string {
	t.Helper()
	ctx := context.Background()
	var names []string
	var err error
	if db.IsPostgres() {
		err = db.NewRaw(`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()
			AND table_type = 'BASE TABLE' AND table_name <> 'goose_db_version' ORDER BY table_name`).Scan(ctx, &names)
	} else {
		err = db.NewRaw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
			AND name <> 'goose_db_version' ORDER BY name`).Scan(ctx, &names)
	}
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestAuditLog_InsertOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		now := store.UTC(time.Now())
		e := &store.AuditEntry{ID: store.NewID(), ActorType: "system", Action: "test", EntityType: "test", CreatedAt: now}
		if _, err := db.NewInsert().Model(e).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := db.NewUpdate().Table("audit_log").Set("action = ?", "changed").Where("id = ?", e.ID).Exec(ctx)
		if err == nil || !strings.Contains(err.Error(), "insert-only") {
			t.Fatalf("update: want insert-only error, got %v", err)
		}
		_, err = db.NewDelete().Table("audit_log").Where("id = ?", e.ID).Exec(ctx)
		if err == nil || !strings.Contains(err.Error(), "insert-only") {
			t.Fatalf("delete: want insert-only error, got %v", err)
		}
	})
}

func TestMemberships_MarinaMustBelongToOrg(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		now := store.UTC(time.Now())
		orgA := &store.Organization{ID: store.NewID(), Name: "A", BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		orgB := &store.Organization{ID: store.NewID(), Name: "B", BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
		for _, o := range []*store.Organization{orgA, orgB} {
			if _, err := db.NewInsert().Model(o).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
		m := &store.Marina{
			ID: store.NewID(), OrganizationID: orgA.ID, Slug: "porto-a", Name: "Porto A", Kind: "marina",
			Timezone: "Europe/Rome", Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"},
			Contacts: store.JSON(`{}`), Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1,
			Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := db.NewInsert().Model(m).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		u := &store.User{ID: store.NewID(), Email: "u@example.test", Name: "U", Locale: "it", Active: true, CreatedAt: now, UpdatedAt: now}
		if _, err := db.NewInsert().Model(u).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		ok := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgA.ID, MarinaID: m.ID, Role: "office", CreatedAt: now, UpdatedAt: now}
		if _, err := db.NewInsert().Model(ok).Exec(ctx); err != nil {
			t.Fatalf("valid membership: %v", err)
		}
		bad := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgB.ID, MarinaID: m.ID, Role: "office", CreatedAt: now, UpdatedAt: now}
		_, err := db.NewInsert().Model(bad).Exec(ctx)
		if err == nil || !store.IsForeignKeyViolation(err) {
			t.Fatalf("cross-org membership: want FK violation, got %v", err)
		}
		dup := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgA.ID, MarinaID: m.ID, Role: "viewer", CreatedAt: now, UpdatedAt: now}
		if _, err := db.NewInsert().Model(dup).Exec(ctx); !store.IsUniqueViolation(err) {
			t.Fatalf("duplicate membership: want unique violation, got %v", err)
		}
		// Two organization-wide grants for the same user and organization collide too (COALESCE index).
		org1 := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgA.ID, Role: "owner", CreatedAt: now, UpdatedAt: now}
		org2 := &store.Membership{ID: store.NewID(), UserID: u.ID, OrganizationID: orgA.ID, Role: "viewer", CreatedAt: now, UpdatedAt: now}
		if _, err := db.NewInsert().Model(org1).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewInsert().Model(org2).Exec(ctx); !store.IsUniqueViolation(err) {
			t.Fatalf("duplicate org grant: want unique violation, got %v", err)
		}

		// Round trip of the typed columns.
		var got store.Marina
		if err := db.NewSelect().Model(&got).Where("id = ?", m.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if !got.CreatedAt.Equal(now) || got.BookingEnabled || !slices.Equal(got.Locales, []string{"it"}) || !got.ArchivedAt.IsZero() ||
			!got.ConcessionExpiresOn.IsZero() || len(got.Geo) != 0 {
			t.Fatalf("round trip mismatch: %+v", got)
		}
	})
}
