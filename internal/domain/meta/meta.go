// Package meta describes the running instance: version, edition and capabilities (GET /api/admin/v1/meta).
package meta

import (
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/pricing"
)

// APIVersion is the version of the admin API surface.
const APIVersion = "v1"

// Editions (spec 31): the server binary (cmd/porto) and the desktop edition (cmd/porto-desktop, M6a).
const (
	EditionServer  = "server"
	EditionDesktop = "desktop"
)

// Desktop capabilities (m6a-plan §3.4): desktop.local always on the desktop edition, desktop.lan while the
// local-network listener runs (added per request through Meta.Dynamic).
const (
	CapDesktopLocal = "desktop.local"
	CapDesktopLAN   = "desktop.lan"
)

// Meta is the public description of the instance.
type Meta struct {
	Version, APIVersion, Edition string
	Capabilities                 []string
	DemoPairing                  bool
	// LocalMode is true when the online features (public page, online payments, remote signature) are unavailable
	// because the instance is not reachable from the internet (desktop edition).
	LocalMode bool
	// PricingEngineVersion is pricing.EngineVersion (M2): quotes store it with their snapshot (INV-20).
	PricingEngineVersion int
	// Dynamic, when set, returns capabilities that change while the instance runs (desktop.lan).
	Dynamic func() []string
}

// AllCapabilities returns the static capabilities plus the dynamic ones of this moment.
func (m Meta) AllCapabilities() []string {
	out := append([]string{}, m.Capabilities...)
	if m.Dynamic != nil {
		out = append(out, m.Dynamic()...)
	}
	return out
}

// Get returns the metadata of an instance configured by cfg, built as version, of the given edition
// (EditionServer or EditionDesktop; anything else is EditionServer).
func Get(cfg *config.Config, version, edition string) Meta {
	if edition != EditionDesktop {
		edition = EditionServer
	}
	m := Meta{
		Version:    version,
		APIVersion: APIVersion,
		Edition:    edition,
		Capabilities: []string{
			"auth.totp", "auth.password_reset", "devices.pairing", "webhooks", "sse",
			// M1
			"harbor.editor", "harbor.georef", "harbor.berth_import", "registry.customers", "registry.documents",
			// M2
			"pricing.engine", "pricing.simulator", "pricing.matrix", "pricing.coverage", "pricing.renewal",
			// M3
			"occupancy.engine", "occupancy.search", "occupancy.layout", "contracts.mooring_base", "transits.desk",
			"quotes.option", "presences",
			// M4 (m4-plan §7.6); signature.providers is added by WithSignatureProviders.
			"contracts.full", "installments", "renewals.campaigns", "charter", "rights", "common_charges",
			"templates.pdf", "signature.paper", "signature.otp", "communications", "deadlines.engine", "imports.csv",
			// M5 (m5-plan §7.6); fiscal.drivers and charge_exporters are added by WithBilling. Stripe is compiled in
			// (payments.providers) but disabled until a marina enables it.
			"charges", "charge_exports", "payments", "credits", "cash", "fiscal.none", "fiscal.export",
			"payment_requests", "reminders", "payments.providers", "payments.sepa", "imports.xlsx", "opening_balances",
			// M7 (m7-plan §8): reports, manager dashboard and comparison; yard and rental reports are M10/M11.
			"reports",
			// M9 (m9-plan §15.1): storage planner and scenarios, maintenance, utility meters and charging.
			"storage", "maintenance", "utilities",
		},
		DemoPairing:          cfg != nil && cfg.DemoPairingEnabled(),
		PricingEngineVersion: pricing.EngineVersion,
	}
	if edition == EditionDesktop {
		m.LocalMode = true
		m.Capabilities = append(m.Capabilities, CapDesktopLocal)
	} else {
		// M8 (m8-plan §11): the public booking page, widget and customer area need an internet-reachable server.
		m.Capabilities = append(m.Capabilities, CapOnlineBooking)
	}
	return m
}

// M8 capabilities: online_booking on the server edition, booking_channels when at least one ext.BookingChannel is
// registered (WithBookingChannels).
const (
	CapOnlineBooking   = "online_booking"
	CapBookingChannels = "booking_channels"
)

// WithBookingChannels adds booking_channels when booking channel extensions are registered (none ships with Porto;
// the fake channel counts only in development and tests).
func (m Meta) WithBookingChannels(registered int) Meta {
	if registered > 0 {
		m.Capabilities = append(append([]string(nil), m.Capabilities...), CapBookingChannels)
	}
	return m
}

// WithSignatureProviders adds the signature.providers capability when at least one ext.SignatureProvider is
// registered (none ships with Porto: InfoCert is an interface only, spec 33 D5).
func (m Meta) WithSignatureProviders(registered int) Meta {
	if registered > 0 {
		m.Capabilities = append(append([]string(nil), m.Capabilities...), "signature.providers")
	}
	return m
}

// WithBilling adds fiscal.drivers when fiscal device drivers are registered and charge_exporters when at least one
// ext.ChargeExporter is registered (none ships with Porto, spec 33 D4).
func (m Meta) WithBilling(fiscalDrivers, chargeExporters int) Meta {
	caps := append([]string(nil), m.Capabilities...)
	if fiscalDrivers > 0 {
		caps = append(caps, "fiscal.drivers")
	}
	if chargeExporters > 0 {
		caps = append(caps, "charge_exporters")
	}
	m.Capabilities = caps
	return m
}
