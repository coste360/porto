package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/backup"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// checkFailed is a failed `--check` (exit 1, no usage).
type checkFailed struct{ msg string }

func (c checkFailed) Error() string { return c.msg }

func newFlags(name string, e env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_ = e
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usagef("%s: %v", fs.Name(), err)
	}
	if fs.NArg() > 0 {
		return usagef("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
}

// loadConfig reads PORTO_* and, in dev mode, creates the dev secret.
func loadConfig(e env) (*config.Config, error) {
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return nil, err
	}
	generated, err := cfg.EnsureDevSecret()
	if err != nil {
		return nil, err
	}
	if generated {
		fmt.Fprintf(e.stderr, "porto: dev mode: generated a secret key in %s/secret.key (keep it to read encrypted data)\n", cfg.DataDir)
	}
	return cfg, nil
}

func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func openStore(ctx context.Context, cfg *config.Config) (*store.DB, error) {
	o := store.Options{PostgresURL: cfg.DatabaseURL}
	if !cfg.UsesPostgres() {
		o.SQLitePath = cfg.SQLitePath()
	}
	return store.Open(ctx, o)
}

func newApp(ctx context.Context, e env) (*app.App, error) {
	cfg, err := loadConfig(e)
	if err != nil {
		return nil, err
	}
	return app.New(ctx, cfg, version, ext.Extensions{}, newLogger(cfg, e.stderr))
}

func cmdServe(ctx context.Context, args []string, e env) error {
	if err := parse(newFlags("serve", e), args); err != nil {
		return err
	}
	a, err := newApp(ctx, e)
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Serve(ctx)
}

func cmdMigrate(ctx context.Context, args []string, e env) error {
	if len(args) == 0 {
		return usagef("migrate: want up or status")
	}
	sub := args[0]
	fs := newFlags("migrate "+sub, e)
	check := false
	if sub == "status" {
		fs.BoolVar(&check, "check", false, "exit 1 when migrations are pending")
	} else if sub != "up" {
		return usagef("migrate: unknown subcommand %q", sub)
	}
	if err := parse(fs, args[1:]); err != nil {
		return err
	}
	cfg, err := loadConfig(e)
	if err != nil {
		return err
	}
	db, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	m, err := db.Migrator()
	if err != nil {
		return err
	}
	if sub == "up" {
		applied, err := m.Up(ctx)
		if err != nil {
			return err
		}
		v, err := m.Version(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "applied %d migration(s); schema version %d\n", len(applied), v)
		return nil
	}
	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tNAME\tAPPLIED\tAPPLIED_AT")
	pending := 0
	for _, s := range st {
		at := s.AppliedAt
		if at == "" {
			at = "-"
		}
		if !s.Applied {
			pending++
		}
		fmt.Fprintf(tw, "%d\t%s\t%t\t%s\n", s.Version, s.Name, s.Applied, at)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if check && pending > 0 {
		return checkFailed{fmt.Sprintf("%d migration(s) pending", pending)}
	}
	return nil
}

// readPassword returns the password from stdin (--password-stdin), PORTO_ADMIN_PASSWORD or an interactive
// prompt. It is never a flag, so that it does not end up in the shell history or the process list.
func readPassword(e env, fromStdin bool, cfg *config.Config, allowEmpty bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(e.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if cfg.AdminPassword != "" {
		return cfg.AdminPassword, nil
	}
	f, ok := e.stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("no password: use --password-stdin or PORTO_ADMIN_PASSWORD")
	}
	fmt.Fprint(e.stderr, "Password: ")
	p1, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(e.stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(e.stderr, "Repeat password: ")
	p2, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(e.stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("the passwords do not match")
	}
	return string(p1), nil
}

func describe(err error) error {
	var v *auth.ValidationError
	if errors.As(err, &v) {
		parts := make([]string, 0, len(v.Fields))
		for _, f := range v.Fields {
			parts = append(parts, f.Field+": "+f.Code)
		}
		return fmt.Errorf("invalid input (%s)", strings.Join(parts, ", "))
	}
	switch {
	case errors.Is(err, auth.ErrWeakPassword):
		return fmt.Errorf("password too weak: at least %d characters, not all the same", auth.MinPasswordRunes)
	case errors.Is(err, auth.ErrUserExists):
		return errors.New("a user with this email already exists")
	}
	return err
}

func cmdAdminCreate(ctx context.Context, args []string, e env) error {
	fs := newFlags("admin create", e)
	var in auth.CreateOwnerInput
	var fromStdin bool
	fs.StringVar(&in.Email, "email", "", "owner email")
	fs.StringVar(&in.Name, "name", "", "owner name")
	fs.StringVar(&in.Organization, "org", "", "organization name")
	fs.StringVar(&in.MarinaName, "marina", "", "first marina name (optional)")
	fs.StringVar(&in.MarinaSlug, "slug", "", "first marina slug (with --marina)")
	fs.StringVar(&in.Timezone, "timezone", "Europe/Rome", "marina timezone")
	fs.StringVar(&in.Locale, "locale", "it", "owner and marina locale (it, en)")
	fs.BoolVar(&fromStdin, "password-stdin", false, "read the password from the first line of stdin")
	if err := parse(fs, args); err != nil {
		return err
	}
	if in.Email == "" || in.Name == "" || in.Organization == "" {
		return usagef("admin create: --email, --name and --org are required")
	}
	if (in.MarinaName == "") != (in.MarinaSlug == "") {
		return usagef("admin create: --marina and --slug go together")
	}
	a, err := newApp(ctx, e)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	if in.Password, err = readPassword(e, fromStdin, a.Config, false); err != nil {
		return err
	}
	res, err := a.Auth.CreateOwner(ctx, in)
	if err != nil {
		return describe(err)
	}
	fmt.Fprintf(e.stdout, "created owner user %s\norganization %s\n", res.UserID, res.OrganizationID)
	if res.MarinaID != "" {
		fmt.Fprintf(e.stdout, "marina %s (%s)\n", res.MarinaID, in.MarinaSlug)
	}
	return nil
}

func cmdSeedDemo(ctx context.Context, args []string, e env) error {
	fs := newFlags("seed demo", e)
	var fromStdin bool
	fs.BoolVar(&fromStdin, "password-stdin", false, "read the demo password from the first line of stdin")
	if err := parse(fs, args); err != nil {
		return err
	}
	a, err := newApp(ctx, e)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	pw, err := readPassword(e, fromStdin, a.Config, true)
	if err != nil {
		return err
	}
	res, err := a.SeedDemo(ctx, pw)
	if err != nil {
		return describe(err)
	}
	m1 := fmt.Sprintf("maps %d, berths %d, customers %d, vessels %d, documents %d, items %d, price_lists %d, price_rules %d, "+
		"contracts %d, transits %d, quotes %d, inquiries %d, allocations %d, rows berth/cell/land %d/%d/%d, releases %d, presences %d",
		res.Maps, res.Berths, res.Customers, res.Vessels, res.Documents, res.Items, res.PriceLists, res.PriceRules,
		res.Contracts, res.Transits, res.Quotes, res.Inquiries, res.Allocations, res.BerthRows, res.CellRows, res.LandRows,
		res.Releases, res.Presences)
	m4 := fmt.Sprintf("installments %d, renewal_campaigns %d, renewal_proposals %d, charters %d, addenda %d, waitlist %d, "+
		"rights %d, mandates %d, transfers %d, fee_runs %d, millesimal_tables %d, common_charge_runs %d, holder_installments %d, "+
		"templates %d, generated_documents %d, communications %d, deadlines %d",
		res.Installments, res.RenewalCampaigns, res.RenewalProposals, res.Charters, res.Addenda, res.WaitlistEntries,
		res.Rights, res.Mandates, res.Transfers, res.FeeRuns, res.MillesimalTables, res.CommonChargeRuns, res.HolderInstallments,
		res.Templates, res.GeneratedDocuments, res.Communications, res.Deadlines)
	if !res.Created {
		fmt.Fprintf(e.stdout, "demo marina %q already exists (%s); added: %s; M4: %s\n", app.DemoMarinaSlug, res.MarinaID, m1, m4)
		return nil
	}
	fmt.Fprintf(e.stdout, "demo organization %q, marina %q (slug %s)\n", app.DemoOrganization, app.DemoMarinaName, app.DemoMarinaSlug)
	fmt.Fprintf(e.stdout, "users (one per role): %s\n", strings.Join(res.Users, ", "))
	fmt.Fprintf(e.stdout, "owner login: %s\n", res.OwnerEmail)
	fmt.Fprintf(e.stdout, "harbour and registry: %s\n", m1)
	fmt.Fprintf(e.stdout, "contracts, rights and documents (M4): %s\n", m4)
	if pw == "" {
		// Shown once: it is not stored anywhere in clear.
		fmt.Fprintf(e.stdout, "generated password (shown once): %s\n", res.Password)
	}
	return nil
}

func cmdBackup(ctx context.Context, args []string, e env) error {
	fs := newFlags("backup", e)
	var out string
	fs.StringVar(&out, "out", "", `output file ("-" = stdout)`)
	if err := parse(fs, args); err != nil {
		return err
	}
	if out == "" {
		return usagef("backup: --out is required")
	}
	cfg, err := loadConfig(e)
	if err != nil {
		return err
	}
	db, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	w := e.stdout
	var f *os.File
	if out != "-" {
		if f, err = os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600); err != nil {
			return err
		}
		w = f
	}
	m, err := backup.Backup(ctx, db, w, version, filepath.Join(cfg.DataDir, "files"))
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(out)
		}
	}
	if err != nil {
		return err
	}
	var rows int64
	for _, t := range m.Tables {
		rows += t.Rows
	}
	fmt.Fprintf(e.stderr, "backup: %d tables, %d rows, %d files, schema %d, dialect %s\n", len(m.Tables), rows, len(m.Files), m.SchemaVersion, m.Dialect)
	return nil
}

func cmdRestore(ctx context.Context, args []string, e env) error {
	fs := newFlags("restore", e)
	var in string
	var force bool
	fs.StringVar(&in, "in", "", `backup file ("-" = stdin)`)
	fs.BoolVar(&force, "force", false, "wipe the database first")
	if err := parse(fs, args); err != nil {
		return err
	}
	if in == "" {
		return usagef("restore: --in is required")
	}
	cfg, err := loadConfig(e)
	if err != nil {
		return err
	}
	db, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if cfg.AutoMigrate && !force {
		if err := db.Migrate(ctx); err != nil {
			return err
		}
	}
	r := e.stdin
	if in != "-" {
		f, err := os.Open(in)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	m, err := backup.Restore(ctx, db, r, force, filepath.Join(cfg.DataDir, "files"))
	if err != nil {
		return err
	}
	var rows int64
	for _, t := range m.Tables {
		rows += t.Rows
	}
	fmt.Fprintf(e.stderr, "restore: %d tables, %d rows from %s backup of %s\n", len(m.Tables), rows, m.Dialect, m.CreatedAt.Format("2006-01-02 15:04:05Z07:00"))
	return nil
}

func cmdVersion(e env) error {
	schema, err := migrations.Latest(migrations.SQLite)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "porto %s (%s, schema %d)\n", version, runtime.Version(), schema)
	return nil
}
