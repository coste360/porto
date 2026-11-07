// Package manual is the always-present payment driver of Porto (spec 16.2): money collected at the desk or by
// bank transfer (cash, POS card, bank transfer, cheque, other) is recorded by staff, so the driver has no hosted
// checkout, no mandates, no webhooks and does no I/O. Every M5 flow works with this driver alone (offline first).
package manual

import (
	"context"
	"net/http"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// Code is the driver code.
const Code = "manual"

// Methods are the methods staff record with the manual driver.
var Methods = []string{"cash", "card_pos", "bank_transfer", "cheque", "other"}

// Driver is the manual payment driver.
type Driver struct{}

// New returns the manual driver.
func New() Driver { return Driver{} }

// Code implements ext.PaymentProvider.
func (Driver) Code() string { return Code }

// Capabilities implements ext.PaymentProvider: nothing online; manual refunds are recorded by staff.
func (Driver) Capabilities() ext.PaymentCapabilities {
	return ext.PaymentCapabilities{Methods: append([]string(nil), Methods...)}
}

// CreateCheckout implements ext.PaymentProvider.
func (Driver) CreateCheckout(context.Context, ext.CheckoutRequest) (*ext.CheckoutSession, error) {
	return nil, ext.ErrNotSupported
}

// CreateMandateSetup implements ext.PaymentProvider.
func (Driver) CreateMandateSetup(context.Context, ext.MandateSetupRequest) (*ext.CheckoutSession, error) {
	return nil, ext.ErrNotSupported
}

// ChargeMandate implements ext.PaymentProvider.
func (Driver) ChargeMandate(context.Context, ext.MandateChargeRequest) (*ext.ProviderPayment, error) {
	return nil, ext.ErrNotSupported
}

// Refund implements ext.PaymentProvider: manual refunds are recorded by staff, never sent anywhere.
func (Driver) Refund(context.Context, ext.RefundRequest) (*ext.RefundResult, error) {
	return nil, ext.ErrNotSupported
}

// Status implements ext.PaymentProvider.
func (Driver) Status(context.Context, ext.ProviderRef) (*ext.PaymentEvent, error) {
	return nil, ext.ErrNotSupported
}

// ParseWebhook implements ext.PaymentProvider: the manual driver has no webhooks, every callback is refused.
func (Driver) ParseWebhook(*http.Request, []byte, time.Time) (*ext.PaymentEvent, error) {
	return nil, ext.ErrInvalidSignature
}

// CloseCheckout implements ext.PaymentProvider.
func (Driver) CloseCheckout(context.Context, string) error { return ext.ErrNotSupported }

var _ ext.PaymentProvider = Driver{}
