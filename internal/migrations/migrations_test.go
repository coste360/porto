package migrations

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestMigrations_SameNumbering(t *testing.T) {
	names := map[Dialect][]string{}
	for _, d := range []Dialect{SQLite, Postgres} {
		fsys, err := FS(d)
		if err != nil {
			t.Fatal(err)
		}
		files, err := fs.Glob(fsys, "*.sql")
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(files)
		names[d] = files
	}
	if !slices.Equal(names[SQLite], names[Postgres]) {
		t.Fatalf("migration files differ:\nsqlite:   %v\npostgres: %v", names[SQLite], names[Postgres])
	}
	for i, n := range names[SQLite] {
		v, err := versionOf(n)
		if err != nil {
			t.Fatal(err)
		}
		if v != int64(i+1) {
			t.Errorf("file %s: version %d, want %d (no gaps)", n, v, i+1)
		}
	}
	latest, err := Latest(SQLite)
	if err != nil || latest != int64(len(names[SQLite])) {
		t.Errorf("Latest = %d, %v", latest, err)
	}
}

func TestMigrations_UpAndDownSections(t *testing.T) {
	for _, d := range []Dialect{SQLite, Postgres} {
		fsys, _ := FS(d)
		files, _ := fs.Glob(fsys, "*.sql")
		for _, f := range files {
			b, err := fs.ReadFile(fsys, f)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			if !strings.Contains(s, "-- +goose Up") || !strings.Contains(s, "-- +goose Down") {
				t.Errorf("%s/%s: missing Up or Down section", d, f)
			}
		}
	}
}

func TestFS_UnknownDialect(t *testing.T) {
	if _, err := FS("mysql"); err == nil {
		t.Fatal("want error")
	}
}
