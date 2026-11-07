// Package meta describes the running instance: version, edition and capabilities (GET /api/admin/v1/meta).
package meta

import (
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/pricing"
)

// APIVersion is the version of the admin API surface.
const APIVersion = "v1"

// Edition is "server" for this binary (the desktop edition arrives later, spec 31).
const Edition = "server"

// Meta is the public description of the instance.
type Meta struct {
	Version, APIVersion, Edition string
	Capabilities                 []string
	DemoPairing                  bool
	// PricingEngineVersion is pricing.EngineVersion (M2): quotes store it with their snapshot (INV-20).
	PricingEngineVersion int
}

// Get returns the metadata of an instance configured by cfg and built as version.
func Get(cfg *config.Config, version string) Meta {
	return Meta{
		Version:    version,
		APIVersion: APIVersion,
		Edition:    Edition,
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
		},
		DemoPairing:          cfg != nil && cfg.DemoPairingEnabled(),
		PricingEngineVersion: pricing.EngineVersion,
	}
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
