package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Device sessions (M6a): a browser paired by QR on the local network authenticates with an ordinary session row
// whose device_id is set. These queries complete sessions.go and the device queries of apikeys.go.

// DeleteSessionsOfDevice removes every session bound to a device (revoking a device logs its browser out in the
// same transaction). It returns how many sessions were removed.
func DeleteSessionsOfDevice(ctx context.Context, db bun.IDB, deviceID string) (int64, error) {
	if !ValidID(deviceID) {
		return 0, nil
	}
	res, err := db.NewDelete().Table("sessions").Where("device_id = ?", deviceID).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// CountSessionsOfDevice returns how many sessions are bound to a device.
func CountSessionsOfDevice(ctx context.Context, db bun.IDB, deviceID string) (int, error) {
	return toInt(db.NewSelect().Table("sessions").Where("device_id = ?", deviceID).Count(ctx))
}

// DeletePendingDeviceSession removes a device session still waiting for its second factor. ErrNotFound when it
// is gone or already verified, so two concurrent confirmations of one pending session replace it only once.
func DeletePendingDeviceSession(ctx context.Context, db bun.IDB, id string) error {
	res, err := db.NewDelete().Table("sessions").
		Where("id = ?", id).Where("device_id IS NOT NULL").Where("totp_verified = ?", false).Exec(ctx)
	return expectOne(res, err)
}
