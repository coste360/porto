// Package meta describes the running instance: version, edition and capabilities (GET /api/admin/v1/meta).
package meta

import "github.com/coste360/porto/internal/config"

// APIVersion is the version of the admin API surface.
const APIVersion = "v1"

// Edition is "server" for this binary (the desktop edition arrives later, spec 31).
const Edition = "server"

// Meta is the public description of the instance.
type Meta struct {
	Version, APIVersion, Edition string
	Capabilities                 []string
	DemoPairing                  bool
}

// Get returns the metadata of an instance configured by cfg and built as version.
func Get(cfg *config.Config, version string) Meta {
	return Meta{
		Version:      version,
		APIVersion:   APIVersion,
		Edition:      Edition,
		Capabilities: []string{"auth.totp", "auth.password_reset", "devices.pairing", "webhooks", "sse"},
		DemoPairing:  cfg != nil && cfg.DemoPairingEnabled(),
	}
}
