package app

import (
	"context"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/pkg/ext"
)

// DesktopFiscalDriverCode is the fiscal driver code of the local fiscal device of a desktop in local mode (M6b,
// m6b-plan §8.4): the M5 path (fiscal.issue job, Deferred errors) is unchanged, the device is whatever the
// peripheral settings configure at the time of the call.
const DesktopFiscalDriverCode = "desktop"

// desktopFiscal delegates to Runtime.Peripherals().Fiscal on every call, so a settings change needs no restart.
// Without a configured local device every call is deferred: documents stay pending.
type desktopFiscal struct {
	peripherals func() desktopport.Peripherals
}

type fiscalNotConfigured struct{}

func (fiscalNotConfigured) Error() string  { return "desktop: no local fiscal device configured" }
func (fiscalNotConfigured) Deferred() bool { return true }

func (d desktopFiscal) Code() string { return DesktopFiscalDriverCode }

func (d desktopFiscal) driver() (ext.FiscalDriver, error) {
	if f := d.peripherals().Fiscal; f != nil {
		return f, nil
	}
	return nil, fiscalNotConfigured{}
}

func (d desktopFiscal) Issue(ctx context.Context, doc ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	f, err := d.driver()
	if err != nil {
		return nil, err
	}
	return f.Issue(ctx, doc)
}

func (d desktopFiscal) Void(ctx context.Context, ref string) error {
	f, err := d.driver()
	if err != nil {
		return err
	}
	return f.Void(ctx, ref)
}

func (d desktopFiscal) Status(ctx context.Context) (ext.DeviceStatus, error) {
	f, err := d.driver()
	if err != nil {
		return ext.DeviceStatus{}, nil //nolint:nilerr // not configured is a status (offline), not a failure
	}
	return f.Status(ctx)
}
