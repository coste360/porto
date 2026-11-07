// Package paydriver is the registry of payment drivers (m5-plan §5.4): the core manual driver, the optional core
// Stripe driver and the extension providers. A marina uses an online driver only with an enabled
// payment_provider_settings row; credentials are decrypted per call and passed in the context
// (ext.WithProviderCredentials), never kept in memory.
package paydriver

import (
	"sort"

	"github.com/coste360/porto/internal/domain/paydriver/manual"
	"github.com/coste360/porto/pkg/ext"
)

// Registry holds the drivers by code.
type Registry struct {
	drivers map[string]ext.PaymentProvider
}

// NewRegistry returns a registry with the manual driver and the given ones (the core Stripe driver and the
// extensions). A later driver with the same code replaces an earlier one, except manual, which is fixed.
func NewRegistry(drivers ...ext.PaymentProvider) *Registry {
	r := &Registry{drivers: map[string]ext.PaymentProvider{}}
	for _, d := range drivers {
		if d == nil || d.Code() == manual.Code {
			continue
		}
		r.drivers[d.Code()] = d
	}
	r.drivers[manual.Code] = manual.New()
	return r
}

// Get returns the driver of a code.
func (r *Registry) Get(code string) (ext.PaymentProvider, bool) {
	d, ok := r.drivers[code]
	return d, ok
}

// Codes returns the registered codes, sorted.
func (r *Registry) Codes() []string {
	out := make([]string, 0, len(r.drivers))
	for c := range r.drivers {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
