// Package app is Porto's composition root: it opens the database, builds the domain services, the event
// pipeline, the job runner and the HTTP router, and runs them. It is the only place that knows every package.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auditlog"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/meta"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/filestore"
	httpserver "github.com/coste360/porto/internal/http"
	"github.com/coste360/porto/internal/http/admin"
	httpdevice "github.com/coste360/porto/internal/http/device"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/public"
	"github.com/coste360/porto/internal/http/sse"
	"github.com/coste360/porto/internal/jobs"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// ShutdownTimeout bounds the graceful shutdown.
const ShutdownTimeout = 15 * time.Second

// Options tune New (tests).
type Options struct {
	// Clock defaults to clock.System.
	Clock clock.Clock
	// DB, when set, is used instead of opening the configured database (tests); App.Close does not close it.
	DB *store.DB
	// Argon2 overrides the password hashing cost (tests).
	Argon2 auth.Argon2Params
	// Transport overrides the email transport (tests).
	Transport notify.Transport
}

// App is a wired Porto instance.
type App struct {
	Config  *config.Config
	DB      *store.DB
	Router  http.Handler
	Jobs    *jobs.Runner
	Version string
	Log     *slog.Logger
	Clock   clock.Clock

	Auth       *auth.Service
	Team       *team.Service
	Marinas    *marina.Service
	APIKeys    *apikey.Service
	Devices    *device.Service
	Webhooks   *webhook.Service
	Notify     *notify.Service
	Dispatcher *event.Dispatcher
	Deliverer  *webhook.Deliverer
	Outbox     *event.Outbox
	Bus        *sse.ReplayBus

	// M1
	Files     *filestore.Store
	Harbor    *harbor.Service
	Customers *customer.Service
	Vessels   *vessel.Service
	Documents *document.Service
	Deadlines *deadline.Service

	ownDB bool
}

// New wires an instance. It opens the database but does not migrate it (Serve and the CLI decide).
func New(ctx context.Context, cfg *config.Config, version string, x ext.Extensions, log *slog.Logger) (*App, error) {
	return NewWithOptions(ctx, cfg, version, x, log, Options{})
}

// NewWithOptions is New with test overrides.
func NewWithOptions(ctx context.Context, cfg *config.Config, version string, x ext.Extensions, log *slog.Logger, o Options) (*App, error) {
	if cfg.Keys == nil {
		return nil, errors.New("app: PORTO_SECRET_KEY is required")
	}
	clk := o.Clock
	if clk == nil {
		clk = clock.System{}
	}
	a := &App{Config: cfg, Version: version, Log: log, Clock: clk, DB: o.DB}
	if a.DB == nil {
		db, err := store.Open(ctx, store.Options{PostgresURL: cfg.DatabaseURL, SQLitePath: sqlitePath(cfg)})
		if err != nil {
			return nil, err
		}
		a.DB, a.ownDB = db, true
	}
	if err := a.wire(x, o); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func sqlitePath(cfg *config.Config) string {
	if cfg.UsesPostgres() {
		return ""
	}
	return cfg.SQLitePath()
}

func (a *App) wire(x ext.Extensions, o Options) error {
	cfg, db, clk, log := a.Config, a.DB, a.Clock, a.Log
	transport := o.Transport
	if transport == nil {
		var err error
		if transport, err = mailTransport(cfg, log); err != nil {
			return err
		}
	}
	aw := audit.NewWriter(clk)
	a.Outbox = event.NewOutbox(clk)
	a.Notify = notify.NewService(db, clk, cfg.Keys, transport, cfg.BaseURL.String(), log)
	authSvc, err := auth.NewService(db, clk, cfg.Keys, aw, a.Notify, log, auth.Options{
		RequireTOTPForOwners: cfg.RequireTOTPForOwners, Argon2: o.Argon2,
	})
	if err != nil {
		return err
	}
	a.Auth = authSvc
	a.Team = team.NewService(db, clk, authSvc, aw, a.Outbox, log)
	a.Marinas = marina.NewService(db, clk, aw, a.Outbox, log)
	a.APIKeys = apikey.NewService(db, clk, aw, a.Outbox, log)
	a.Devices = device.NewService(db, clk, aw, a.Outbox, log, device.Options{
		BaseURL: cfg.BaseURL.String(), DemoUserEmail: cfg.DemoPairingUser, DemoMarinaSlug: cfg.DemoPairingMarina,
	})
	a.Files = filestore.New(filepath.Join(cfg.DataDir, "files"), cfg.Keys)
	a.Harbor = harbor.NewService(harbor.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Customers = customer.NewService(customer.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Vessels = vessel.NewService(vessel.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Deadlines = deadline.NewService(deadline.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Documents = document.NewService(document.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	policy := event.EgressPolicy{AllowPrivate: cfg.WebhooksAllowPrivate, AllowHTTP: cfg.WebhooksAllowPrivate}
	a.Webhooks = webhook.NewService(db, clk, cfg.Keys, aw, a.Outbox, policy, log)
	a.Deliverer = webhook.NewDeliverer(db, clk, cfg.Keys, event.NewEgress(policy, webhook.DeliveryTimeout), aw, a.Version, log)

	var inner ext.EventBus = event.NewBroker(log, 64)
	if x.EventBus != nil {
		inner = x.EventBus
	}
	a.Bus = sse.NewReplayBus(inner, sse.DefaultReplaySize)
	sinks := append([]ext.EventSink{webhook.NewFanOutSink(db, clk, a.Webhooks)}, x.EventSinks...)
	a.Dispatcher = event.NewDispatcher(db, clk, sinks, a.Bus, log)

	if err := a.registerJobs(); err != nil {
		return err
	}

	router, err := httpserver.NewRouter(httpserver.Deps{
		Log: log, Clock: clk,
		Admin: &admin.Handler{
			Clock: clk, Auth: authSvc, Team: a.Team, Marinas: a.Marinas, APIKeys: a.APIKeys, Devices: a.Devices,
			Webhooks: a.Webhooks, Audit: auditlog.NewService(db, aw), SSE: sse.NewServer(a.Bus, a.Bus, log),
			Meta: meta.Get(cfg, a.Version), SecureCookies: cfg.SecureCookies(),
			Harbor: a.Harbor, Customers: a.Customers, Vessels: a.Vessels, Documents: a.Documents,
			Deadlines: a.Deadlines,
		},
		Device:               &httpdevice.Handler{Auth: authSvc, Devices: a.Devices, BaseURL: cfg.BaseURL.String()},
		Public:               &public.Handler{Marinas: a.Marinas},
		Authenticator:        authSvc,
		Idempotency:          middleware.StoreIdempotency{DB: db.DB},
		Ready:                a.Ready,
		TrustedProxies:       cfg.TrustedProxies,
		HSTS:                 cfg.BaseURL.Scheme == "https",
		PublicReadsPerMinute: cfg.PublicReadsPerMinute,
		ExtraRoutes:          x.HTTPRoutes,
	})
	if err != nil {
		return err
	}
	a.Router = router
	return nil
}

func mailTransport(cfg *config.Config, log *slog.Logger) (notify.Transport, error) {
	switch {
	case cfg.SMTPURL != "":
		return notify.NewSMTPTransport(cfg.SMTPURL, cfg.MailFrom, 30*time.Second)
	case cfg.MailDir != "":
		return notify.NewFileTransportFrom(cfg.MailDir, cfg.MailFrom), nil
	default:
		return notify.NewLogTransport(log), nil
	}
}

// registerJobs registers the core jobs (m0-plan §1.3).
func (a *App) registerJobs() error {
	a.Jobs = jobs.NewRunner(a.Log, jobs.NewDBLocker(a.DB))
	defs := []jobs.Job{
		{Name: "outbox.dispatch", Every: time.Second, RunAtStart: true, Wake: a.Outbox.Wake(), Run: func(ctx context.Context) error {
			_, err := a.Dispatcher.DispatchPending(ctx, 200)
			return err
		}},
		{Name: "webhooks.deliver", Every: 5 * time.Second, RunAtStart: true, Wake: a.Webhooks.Wake(), Run: func(ctx context.Context) error {
			_, err := a.Deliverer.DeliverDue(ctx, 50)
			return err
		}},
		{Name: "email.deliver", Every: 5 * time.Second, RunAtStart: true, Wake: a.Notify.Wake(), Run: func(ctx context.Context) error {
			_, err := a.Notify.DeliverDue(ctx, 50)
			return err
		}},
		{Name: "sessions.cleanup", Every: time.Hour, RunAtStart: true, Run: func(ctx context.Context) error {
			_, err := a.Auth.CleanupExpired(ctx)
			return err
		}},
		{Name: "idempotency.cleanup", Every: time.Hour, RunAtStart: true, Run: func(ctx context.Context) error {
			_, err := store.DeleteExpiredIdempotencyKeys(ctx, a.DB.DB, a.Clock.Now())
			return err
		}},
		{Name: "webhooks.retention", Every: 24 * time.Hour, Run: func(ctx context.Context) error {
			_, err := a.Webhooks.PurgeOld(ctx, webhook.Retention)
			return err
		}},
	}
	for _, j := range defs {
		if err := a.Jobs.Register(j); err != nil {
			return err
		}
	}
	return nil
}

// Ready reports the applied schema version when every migration is applied.
func (a *App) Ready(ctx context.Context) (int64, error) {
	m, err := a.DB.Migrator()
	if err != nil {
		return 0, err
	}
	ok, err := m.Aligned(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("app: migrations pending")
	}
	return m.Version(ctx)
}

// Prepare migrates (PORTO_AUTO_MIGRATE) or checks the schema, then creates the bootstrap owner when configured
// and the database has no users yet.
func (a *App) Prepare(ctx context.Context) error {
	if a.Config.AutoMigrate {
		if err := a.DB.Migrate(ctx); err != nil {
			return fmt.Errorf("app: migrate: %w", err)
		}
	} else if _, err := a.Ready(ctx); err != nil {
		return fmt.Errorf("app: database schema not up to date (run `porto migrate up`): %w", err)
	}
	return a.bootstrapOwner(ctx)
}

func (a *App) bootstrapOwner(ctx context.Context) error {
	cfg := a.Config
	if !cfg.BootstrapOwnerEnabled() {
		return nil
	}
	n, err := store.CountUsers(ctx, a.DB.DB)
	if err != nil || n > 0 {
		return err
	}
	if cfg.AdminPassword == "" {
		return errors.New("app: PORTO_BOOTSTRAP_OWNER_* needs PORTO_ADMIN_PASSWORD")
	}
	res, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: cfg.BootstrapOwnerEmail, Name: cfg.BootstrapOwnerName, Password: cfg.AdminPassword,
		Organization: cfg.BootstrapOwnerOrg,
	})
	if err != nil {
		return fmt.Errorf("app: bootstrap owner: %w", err)
	}
	a.Log.InfoContext(ctx, "bootstrap owner created", "user_id", res.UserID, "organization_id", res.OrganizationID)
	return nil
}

// Serve prepares the database, starts the jobs and serves HTTP (or HTTPS with autocert) until ctx is done, then
// shuts down gracefully within ShutdownTimeout.
func (a *App) Serve(ctx context.Context) error {
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	jobsCtx, stopJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer stopJobs()
	a.Jobs.Start(jobsCtx)

	servers, err := a.servers()
	if err != nil {
		return err
	}
	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			a.Log.InfoContext(ctx, "listening", "addr", s.srv.Addr, "tls", s.tls)
			var err error
			if s.tls {
				err = s.srv.ListenAndServeTLS("", "")
			} else {
				err = s.srv.ListenAndServe()
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
	}
	a.Log.InfoContext(ctx, "shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()
	for _, s := range servers {
		if err := s.srv.Shutdown(sctx); err != nil {
			a.Log.WarnContext(sctx, "http shutdown", "error", err)
		}
	}
	stopJobs()
	a.Jobs.Wait()
	return serveErr
}

type listener struct {
	srv *http.Server
	tls bool
}

func (a *App) newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
		ErrorLog: slog.NewLogLogger(a.Log.Handler(), slog.LevelWarn),
	}
}

func (a *App) servers() ([]listener, error) {
	cfg := a.Config
	if len(cfg.AutocertDomains) == 0 {
		return []listener{{srv: a.newServer(cfg.Addr, a.Router)}}, nil
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.AutocertDomains...),
		Cache:      autocert.DirCache(filepath.Join(cfg.DataDir, "certs")),
	}
	https := a.newServer(":443", a.Router)
	https.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1", "acme-tls/1"}}
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !slices.Contains(cfg.AutocertDomains, host) {
			host = cfg.AutocertDomains[0]
		}
		//nolint:gosec // G710: host is one of the configured autocert domains, never client input
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
	return []listener{{srv: https, tls: true}, {srv: a.newServer(":80", m.HTTPHandler(redirect))}}, nil
}

// Close releases the database (when the app opened it).
func (a *App) Close() error {
	if a.ownDB && a.DB != nil {
		return a.DB.Close()
	}
	return nil
}
