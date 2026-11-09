package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uptrace/bun"
)

// Demo reset (`porto seed demo --reset`): a public demo instance recreates its data every night. The reset wipes
// the whole database (every migration rolled back and applied again, the same path as `porto restore --force`,
// on SQLite and PostgreSQL alike) and the stored files, so it is guarded twice: the command needs
// PORTO_DEMO_INSTANCE=true, and CheckDemoReset refuses a database that holds anything but the demo organization.
//
// The demo organization is identified by a marker file in the data directory (demoMarkerFile), written by
// SeedDemo when it creates the organization. Visitors of a public demo log in as the demo owner and can rename
// the organization or the marina, add marinas or remove team members through the API, but they cannot touch the
// data directory, so none of that stops the nightly reset.

// ErrNotDemoDatabase is the refusal of CheckDemoReset.
var ErrNotDemoDatabase = errors.New("seed: the database is not a demo database")

// demoMarkerFile, in the data directory, holds the id of the demo organization created by SeedDemo.
const demoMarkerFile = "demo-organization"

func (a *App) demoMarkerPath() string { return filepath.Join(a.Config.DataDir, demoMarkerFile) }

// writeDemoMarker records orgID as the demo organization of this data directory.
func (a *App) writeDemoMarker(orgID string) error {
	if err := os.MkdirAll(a.Config.DataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.demoMarkerPath(), []byte(orgID+"\n"), 0o600)
}

// readDemoMarker returns the demo organization id, "" when the data directory has no marker.
func (a *App) readDemoMarker() (string, error) {
	b, err := os.ReadFile(a.demoMarkerPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// CheckDemoReset refuses (ErrNotDemoDatabase, wrapped with the reason) unless the database holds only the demo
// data. An empty (just migrated) database passes. With the marker of SeedDemo, the database passes when every
// organization is the demo one: its name, its marinas and its users (members or not) do not matter. Without a
// marker (a demo seeded before the marker existed) the stricter rule of names applies: at most one organization,
// named DemoOrganization, every marina in it with the demo slug, every user a member of that organization only.
func (a *App) CheckDemoReset(ctx context.Context) error {
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrNotDemoDatabase, why) }
	marker, err := a.readDemoMarker()
	if err != nil {
		return err
	}
	if marker != "" {
		n, err := a.DB.NewSelect().TableExpr("organizations").Where("CAST(id AS TEXT) <> ?", marker).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return refuse(fmt.Sprintf("%d organizations besides the demo one", n))
		}
		return nil
	}
	var orgs []struct {
		ID   string `bun:"id"`
		Name string `bun:"name"`
	}
	if err := a.DB.NewSelect().TableExpr("organizations").ColumnExpr("CAST(id AS TEXT) AS id, name").OrderExpr("id").Scan(ctx, &orgs); err != nil {
		return err
	}
	switch {
	case len(orgs) > 1:
		return refuse(fmt.Sprintf("%d organizations", len(orgs)))
	case len(orgs) == 1 && orgs[0].Name != DemoOrganization:
		return refuse("the organization is not " + DemoOrganization)
	}
	orgID := ""
	if len(orgs) == 1 {
		orgID = orgs[0].ID
	}
	n, err := a.DB.NewSelect().TableExpr("marinas").WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("CAST(organization_id AS TEXT) <> ?", orgID).WhereOr("slug <> ?", DemoMarinaSlug)
	}).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return refuse(fmt.Sprintf("%d marinas besides the demo one", n))
	}
	// Users with no membership, or with a membership in another organization.
	n, err = a.DB.NewSelect().TableExpr("users AS u").
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("NOT EXISTS (SELECT 1 FROM memberships AS m WHERE m.user_id = u.id)").
				WhereOr("EXISTS (SELECT 1 FROM memberships AS m WHERE m.user_id = u.id AND CAST(m.organization_id AS TEXT) <> ?)", orgID)
		}).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return refuse(fmt.Sprintf("%d users outside the demo organization", n))
	}
	return nil
}

// ResetDemo checks the database (CheckDemoReset), then rolls every migration back and applies them again, removes
// the demo marker, and empties the file store directory and the mail directory (PORTO_MAIL_DIR) when set. The
// caller seeds the demo again (SeedDemo). The confirmation of the operator (PORTO_DEMO_INSTANCE) is the caller's.
// It is not atomic: a failure after the check leaves an empty or partly reset database.
func (a *App) ResetDemo(ctx context.Context) error {
	if err := a.CheckDemoReset(ctx); err != nil {
		return err
	}
	mig, err := a.DB.Migrator()
	if err != nil {
		return err
	}
	if err := mig.Reset(ctx); err != nil {
		return err
	}
	// The marker names an organization that no longer exists; SeedDemo writes the new one.
	if err := os.Remove(a.demoMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := mig.Up(ctx); err != nil {
		return err
	}
	if err := resetFilesDir(filepath.Join(a.Config.DataDir, "files")); err != nil {
		return err
	}
	if a.Config.MailDir != "" {
		return resetFilesDir(a.Config.MailDir) // the .eml files of a demo without SMTP
	}
	return nil
}

// resetFilesDir removes the content of the file store directory (the directory itself stays).
func resetFilesDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
