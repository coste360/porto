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
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auditlog"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/comms"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/meta"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/paydriver"
	"github.com/coste360/porto/internal/domain/paydriver/stripe"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/filestore"
	httpserver "github.com/coste360/porto/internal/http"
	"github.com/coste360/porto/internal/http/admin"
	httpbridge "github.com/coste360/porto/internal/http/bridge"
	httpdesktop "github.com/coste360/porto/internal/http/desktop"
	httpdevice "github.com/coste360/porto/internal/http/device"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/http/public"
	"github.com/coste360/porto/internal/http/sse"
	"github.com/coste360/porto/internal/http/webhooks"
	"github.com/coste360/porto/internal/jobs"
	"github.com/coste360/porto/internal/reqctx"
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

	// Desktop is the desktop launcher runtime (M6a, cmd/porto-desktop). nil = server edition: /api/desktop/v1
	// answers 404 and meta reports edition "server". Set, the instance reports edition "desktop", local_mode and
	// the desktop.local / desktop.lan capabilities.
	Desktop desktopport.Runtime
	// PairTarget tells where pairing QRs point (M6a). nil = the base URL (server edition).
	PairTarget func() desktopport.PairTarget
	// Peripherals returns the local peripherals of the desktop (M6b): the local fiscal device is registered as
	// the fiscal driver "desktop" (DesktopFiscalDriverCode). nil = Desktop.Peripherals when Desktop is set, else
	// no driver is registered (server edition).
	Peripherals func() desktopport.Peripherals
}

// ErrRestart is returned by Run after RequestRestart (desktop restore, m6a-plan §2.6): the caller stops, swaps the
// database and builds a new App.
var ErrRestart = errors.New("app: restart requested")

// Listener is a listener Run serves on. Tag marks its connections (reqctx.ListenerKind); AllowedHosts, when not
// empty, restricts the Host header (middleware.HostCheck). Server, when set, is used as it is (autocert); else
// Run builds one with NewHTTPServer. A TLS listener is wrapped by the caller (tls.NewListener) unless
// Server.TLSConfig is set.
type Listener struct {
	net.Listener
	Tag          reqctx.ListenerKind
	AllowedHosts []string
	Server       *http.Server
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

	// M2
	Catalog    *catalog.Service
	PriceLists *pricelist.Service

	// M3
	Occupancy *occupancy.Service
	Contracts *contract.Service
	Presence  *presence.Service

	// M4
	Docs       *docs.Service
	Links      *magiclink.Service
	Signatures *signature.Service
	Rights     *rights.Service
	Comms      *comms.Service
	Imports    *csvimport.Registry

	// M5
	Charges    *charge.Service
	Payments   *payment.Service
	Fiscal     *fiscal.Service
	Cash       *cash.Service
	PayDrivers *paydriver.Registry
	Collect    *collect.Service

	// M6a
	Desktop desktopport.Runtime

	ownDB     bool
	restartMu sync.Mutex
	restart   chan struct{}
	restarted bool
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
	a := &App{Config: cfg, Version: version, Log: log, Clock: clk, DB: o.DB, Desktop: o.Desktop, restart: make(chan struct{})}
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
		PairTarget: o.PairTarget,
	})
	a.Files = filestore.New(filepath.Join(cfg.DataDir, "files"), cfg.Keys)
	// The occupancy service exists before the harbour service: it is the harbour guard (m3-plan §3.7).
	a.Occupancy = occupancy.NewService(occupancy.Deps{DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Log: log})
	a.Harbor = harbor.NewService(harbor.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
		Guard: a.Occupancy.Guard(),
	})
	a.Customers = customer.NewService(customer.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Vessels = vessel.NewService(vessel.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Deadlines = deadline.NewService(deadline.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Notify: a.Notify, Log: log,
	})
	a.Documents = document.NewService(document.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
	})
	a.Catalog = catalog.NewService(catalog.Deps{DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Log: log})
	a.PriceLists = pricelist.NewService(pricelist.Deps{DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Log: log})
	// M4 (m4-plan §2): documents and links first, then signature, rights and contracts, then the registrations
	// (callbacks go through interfaces, so docs and signature never import contract or rights).
	a.Docs = docs.NewService(docs.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Notify: a.Notify, Log: log,
	})
	a.Links = magiclink.NewService(magiclink.Deps{DB: db, Clock: clk, BaseURL: cfg.BaseURL.String(), Log: log})
	// M5 (m5-plan §2): charges exist before rights and contracts, which call them through their BillingHooks.
	a.Charges = charge.NewService(charge.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Prices: a.PriceLists,
		Docs: a.Docs, Log: log, Exporters: x.ChargeExporters,
	})
	a.Signatures = signature.NewService(signature.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Docs: a.Docs, Links: a.Links,
		Notify: a.Notify, Providers: x.SignatureProviders, Notifiers: x.Notifiers, Log: log,
	})
	a.Rights = rights.NewService(rights.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Occupancy: a.Occupancy, Prices: a.PriceLists,
		Docs: a.Docs, Signatures: a.Signatures, Notify: a.Notify, Log: log,
	})
	a.Rights.SetBilling(a.Charges.RightsHooks())
	a.Comms = comms.NewService(comms.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Docs: a.Docs, Links: a.Links, Notify: a.Notify,
		Notifiers: x.Notifiers, Log: log,
	})
	a.Contracts = contract.NewService(contract.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Log: log, Occupancy: a.Occupancy, Prices: a.PriceLists,
		Keys: cfg.Keys, Files: a.Files, Notify: a.Notify, Docs: a.Docs, Signatures: a.Signatures, Links: a.Links,
		Holders: a.Rights, Documents: a.Documents, Billing: a.Charges.ContractHooks(),
	})
	for _, sub := range a.Contracts.SignatureSubjects() {
		a.Signatures.RegisterSubject(sub)
	}
	a.Signatures.RegisterSubject(a.Rights.SignatureSubject())
	a.Docs.RegisterDataSource(a.Contracts.DataSource())
	a.Docs.RegisterDataSource(a.Rights.DataSource())
	a.Docs.RegisterDataSource(a.Comms.DataSource())
	a.Imports = csvimport.NewRegistry()
	a.Imports.Register(a.Customers.ImportHandler())
	a.Imports.Register(a.Contracts.ImportHandler())
	a.Imports.Register(a.Rights.ImportHandler())
	a.Imports.Register(a.Deadlines.ImportHandler())
	a.Occupancy.SetMovePricer(a.Contracts)
	if periph := o.Peripherals; periph != nil || o.Desktop != nil {
		if periph == nil {
			periph = o.Desktop.Peripherals
		}
		// M6b: the local fiscal device of the desktop, after the extensions' drivers (a copy: x is the caller's).
		x.FiscalDrivers = append(append([]ext.FiscalDriver(nil), x.FiscalDrivers...), desktopFiscal{peripherals: periph})
	}
	a.wireBilling(x)
	a.Presence = presence.NewService(presence.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Files: a.Files, Log: log,
		Contracts: a.Contracts, Occupancy: a.Occupancy,
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
			Meta: a.meta(x), SecureCookies: cfg.SecureCookies(),
			Harbor: a.Harbor, Customers: a.Customers, Vessels: a.Vessels, Documents: a.Documents,
			Deadlines: a.Deadlines, Catalog: a.Catalog, PriceLists: a.PriceLists,
			Occupancy: a.Occupancy, Contracts: a.Contracts, Presence: a.Presence,
			Rights: a.Rights, Docs: a.Docs, Signatures: a.Signatures, Links: a.Links, Comms: a.Comms, Imports: a.Imports,
			Charges: a.Charges, Payments: a.Payments, Cash: a.Cash, Fiscal: a.Fiscal, Collect: a.Collect,
			PayDrivers: a.PayDrivers,
		},
		Device: &httpdevice.Handler{
			Auth: authSvc, Devices: a.Devices, BaseURL: cfg.BaseURL.String(), SecureCookies: cfg.SecureCookies(),
			Fiscal: a.Fiscal,
		},
		Public: &public.Handler{
			Marinas: a.Marinas, Contracts: a.Contracts, Links: a.Links, Signatures: a.Signatures, Comms: a.Comms, Docs: a.Docs,
			Collect: a.Collect,
		},
		Webhooks:             &webhooks.Handler{Signatures: a.Signatures, Collect: a.Collect, Clock: clk},
		Desktop:              &httpdesktop.Handler{Runtime: o.Desktop, SecureCookies: cfg.SecureCookies()},
		Bridge:               &httpbridge.Handler{Runtime: o.Desktop},
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

// meta describes the instance: the desktop edition (Options.Desktop set) adds local_mode, desktop.local and, while
// the local-network listener runs, desktop.lan.
func (a *App) meta(x ext.Extensions) meta.Meta {
	edition := meta.EditionServer
	if a.Desktop != nil {
		edition = meta.EditionDesktop
	}
	m := meta.Get(a.Config, a.Version, edition).WithSignatureProviders(len(x.SignatureProviders)).
		WithBilling(len(x.FiscalDrivers), len(x.ChargeExporters))
	if rt := a.Desktop; rt != nil {
		m.Dynamic = func() []string {
			if lan := rt.LAN(); lan != nil && lan.Status().Listening {
				return []string{meta.CapDesktopLAN}
			}
			return nil
		}
	}
	return m
}

// wireBilling builds the M5 services after contracts and rights (m5-plan §2): ledger, fiscal, cash, payment
// drivers and collection, then the registrations between them (interfaces, so charge and payment never import
// collect or cash).
func (a *App) wireBilling(x ext.Extensions) {
	cfg, db, clk, log := a.Config, a.DB, a.Clock, a.Log
	aw := audit.NewWriter(clk)
	a.Payments = payment.NewService(payment.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Charges: a.Charges, Contracts: a.Contracts,
		Rights: a.Rights, Docs: a.Docs, Notify: a.Notify, Log: log,
	})
	a.Fiscal = fiscal.NewService(fiscal.Deps{DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Log: log, Drivers: x.FiscalDrivers})
	a.Cash = cash.NewService(cash.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Charges: a.Charges, Payments: a.Payments, Fiscal: a.Fiscal,
		Prices: a.PriceLists, Docs: a.Docs, Notify: a.Notify, Log: log,
	})
	// The core Stripe driver goes over the anti-SSRF egress client (base URL cfg.StripeAPIBase only in dev); it is
	// registered but stays unused until a marina enables it, so the manual driver alone covers every flow.
	// A local base URL is accepted only with PORTO_DEV=true (config), so private/http stays dev-only.
	devStripe := cfg.WebhooksAllowPrivate || cfg.StripeAPIBase != ""
	stripePolicy := event.EgressPolicy{AllowPrivate: devStripe, AllowHTTP: devStripe}
	drivers := append([]ext.PaymentProvider{stripe.New(event.NewEgressClient(stripePolicy, 30*time.Second), cfg.StripeAPIBase)},
		x.PaymentProviders...)
	a.PayDrivers = paydriver.NewRegistry(drivers...)
	a.Collect = collect.NewService(collect.Deps{
		DB: db, Clock: clk, Audit: aw, Outbox: a.Outbox, Keys: cfg.Keys, Payments: a.Payments, Charges: a.Charges,
		Contracts: a.Contracts, Drivers: a.PayDrivers, Docs: a.Docs, Notify: a.Notify, BaseURL: cfg.BaseURL.String(),
		Log: log,
	})
	a.Charges.SetPaymentRequester(a.Collect)
	a.Charges.SetCreditGranter(a.Payments)
	a.Payments.SetCash(a.Cash)
	a.Payments.SetFiscal(a.Fiscal)
	a.Payments.SetProviderRefunder(a.Collect)
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
		// M3 (m3-plan §7.6): expired holds and the reconciliation of transits, quotes and contracts. M4 extends it
		// with renewed and tacit renewals (CONTRACTS).
		{Name: "contracts.expire", Every: time.Minute, RunAtStart: true, Run: func(ctx context.Context) error {
			return a.Contracts.RunExpiry(ctx)
		}},
		// M4 (m4-plan §7.6). Each job is idempotent and implemented by its owner.
		{Name: "installments.issue", Every: 10 * time.Minute, RunAtStart: true, Run: a.Contracts.RunInstallmentsIssue},
		{Name: "renewals.sync", Every: time.Minute, RunAtStart: true, Run: a.Contracts.RunRenewalsSync},
		// Daily; the job itself runs the accrual once per marina day (after 02:00 marina time).
		{Name: "mandates.accrue", Every: time.Hour, RunAtStart: true, Run: a.Rights.RunAccrual},
		{Name: "documents.render", Every: 5 * time.Second, RunAtStart: true, Wake: a.Docs.Wake(), Run: a.Docs.RunRenderJob},
		{Name: "signatures.sync", Every: 5 * time.Minute, RunAtStart: true, Run: a.Signatures.RunSync},
		{Name: "comms.send", Every: time.Minute, RunAtStart: true, Wake: a.Comms.Wake(), Run: a.Comms.RunSend},
		{Name: "deadlines.sync", Every: 15 * time.Minute, RunAtStart: true, Run: a.Deadlines.RunSync},
		{Name: "deadlines.notify", Every: time.Hour, Run: a.Deadlines.RunNotify},
		{Name: "links.cleanup", Every: 24 * time.Hour, Run: a.Links.Cleanup},
		// M5 (m5-plan §7.6). Each job is idempotent and implemented by its owner; the contract phase registers
		// no-ops.
		{Name: "charges.backfill", Every: time.Hour, RunAtStart: true, Run: a.Charges.RunBackfill},
		{Name: "charges.exporter", Every: time.Minute, Run: a.Charges.RunExporter},
		// Hourly; the job itself runs once per marina day after billing_settings.reminder_hour (arrears included).
		{Name: "payments.reminders", Every: time.Hour, RunAtStart: true, Run: a.Collect.RunReminders},
		{Name: "payments.sepa_collect", Every: 15 * time.Minute, Run: a.Collect.RunSEPACollect},
		{Name: "payments.reconcile", Every: 15 * time.Minute, Run: a.Collect.RunReconcile},
		{Name: "payments.intents_expire", Every: time.Minute, RunAtStart: true, Run: a.Collect.RunIntentsExpire},
		{Name: "fiscal.issue", Every: 30 * time.Second, Wake: a.Fiscal.Wake(), Run: a.Fiscal.RunIssue},
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
// shuts down gracefully within ShutdownTimeout. It is Prepare plus Run on the configured listeners.
func (a *App) Serve(ctx context.Context) error {
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	ls, err := a.listeners(ctx)
	if err != nil {
		return err
	}
	return a.Run(ctx, ls...)
}

// Run starts the jobs and serves every listener until ctx is done, a listener fails or RequestRestart is called
// (then it returns ErrRestart), and shuts down within ShutdownTimeout: HTTP servers first, then the jobs. It does
// not migrate (call Prepare first). Each listener is closed by Run.
func (a *App) Run(ctx context.Context, listeners ...Listener) error {
	if len(listeners) == 0 {
		return errors.New("app: no listener")
	}
	jobsCtx, stopJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer stopJobs()
	a.Jobs.Start(jobsCtx)

	servers := make([]*http.Server, 0, len(listeners))
	errc := make(chan error, len(listeners))
	for _, l := range listeners {
		srv := l.Server
		if srv == nil {
			srv = a.NewHTTPServer(l.Tag, l.AllowedHosts)
		}
		servers = append(servers, srv)
		go func() {
			a.Log.InfoContext(ctx, "listening", "addr", l.Addr().String(), "listener", string(l.Tag),
				"tls", srv.TLSConfig != nil)
			var err error
			if srv.TLSConfig != nil {
				err = srv.ServeTLS(l.Listener, "", "")
			} else {
				err = srv.Serve(l.Listener)
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	case <-a.restart:
		runErr = ErrRestart
	}
	a.Log.InfoContext(ctx, "shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(sctx); err != nil {
			a.Log.WarnContext(sctx, "http shutdown", "error", err)
		}
	}
	stopJobs()
	a.Jobs.Wait()
	return runErr
}

// RequestRestart makes Run return ErrRestart (desktop restore). Safe to call more than once and from any goroutine.
func (a *App) RequestRestart() {
	a.restartMu.Lock()
	defer a.restartMu.Unlock()
	if !a.restarted {
		a.restarted = true
		close(a.restart)
	}
}

// NewHTTPServer returns an HTTP server for the router with Porto's timeouts. Its connections are tagged with the
// listener kind and, when hosts is not empty, the Host header must be one of hosts (421 otherwise). The desktop
// launcher uses it for the loopback listener, LAN for the local-network listener.
func (a *App) NewHTTPServer(tag reqctx.ListenerKind, hosts []string) *http.Server {
	if !tag.Valid() {
		tag = reqctx.ListenerServer
	}
	srv := a.newServer("", middleware.HostCheck(hosts)(a.Router))
	srv.ConnContext = middleware.ListenerTag(tag)
	return srv
}

func (a *App) newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
		ErrorLog: slog.NewLogLogger(a.Log.Handler(), slog.LevelWarn),
	}
}

// listeners opens the listeners of `porto serve`: PORTO_ADDR, or :443 (autocert) and :80 (redirect).
func (a *App) listeners(ctx context.Context) ([]Listener, error) {
	cfg := a.Config
	var lc net.ListenConfig
	listen := func(addr string) (net.Listener, error) { return lc.Listen(ctx, "tcp", addr) }
	if len(cfg.AutocertDomains) == 0 {
		l, err := listen(cfg.Addr)
		if err != nil {
			return nil, err
		}
		return []Listener{{Listener: l, Tag: reqctx.ListenerServer}}, nil
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.AutocertDomains...),
		Cache:      autocert.DirCache(filepath.Join(cfg.DataDir, "certs")),
	}
	https := a.NewHTTPServer(reqctx.ListenerServer, nil)
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
	plain := a.newServer("", m.HTTPHandler(redirect))
	plain.ConnContext = middleware.ListenerTag(reqctx.ListenerServer)
	l443, err := listen(":443")
	if err != nil {
		return nil, err
	}
	l80, err := listen(":80")
	if err != nil {
		_ = l443.Close()
		return nil, err
	}
	return []Listener{
		{Listener: l443, Tag: reqctx.ListenerServer, Server: https},
		{Listener: l80, Tag: reqctx.ListenerServer, Server: plain},
	}, nil
}

// Close releases the database (when the app opened it).
func (a *App) Close() error {
	if a.ownDB && a.DB != nil {
		return a.DB.Close()
	}
	return nil
}
