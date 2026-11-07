package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// InsertAPIKey inserts an API key. A prefix collision is ErrConflict (the caller draws a new prefix).
func InsertAPIKey(ctx context.Context, db bun.IDB, k *APIKey) error {
	_, err := db.NewInsert().Model(k).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// ListAPIKeys returns the API keys of a marina, newest first.
func ListAPIKeys(ctx context.Context, db bun.IDB, s MarinaScope) ([]APIKey, error) {
	out := []APIKey{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("id DESC").Scan(ctx)
	return out, err
}

// GetAPIKey returns one API key of a marina.
func GetAPIKey(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*APIKey, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	k := new(APIKey)
	if err := db.NewSelect().Model(k).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return k, nil
}

// RevokeAPIKey revokes a key of a marina. Revoking twice is a no-op.
func RevokeAPIKey(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) error {
	if _, err := GetAPIKey(ctx, db, s, id); err != nil {
		return err
	}
	_, err := db.NewUpdate().Table("api_keys").Set("revoked_at = ?", at).Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("revoked_at IS NULL").Exec(ctx)
	return err
}

// APIKeyByPrefix returns a key by its public prefix (authentication path).
func APIKeyByPrefix(ctx context.Context, db bun.IDB, prefix string) (*APIKey, error) {
	k := new(APIKey)
	if err := db.NewSelect().Model(k).Where("prefix = ?", prefix).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return k, nil
}

// TouchAPIKey records the last use of a key.
func TouchAPIKey(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("api_keys").Set("last_used_at = ?", at).Where("id = ?", id).Exec(ctx)
	return err
}

// InsertDevice inserts a device. A token prefix collision is ErrConflict.
func InsertDevice(ctx context.Context, db bun.IDB, d *Device) error {
	if d.Access == "" {
		d.Access = DeviceAccessToken
	}
	_, err := db.NewInsert().Model(d).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeviceRow is a device with the name of its user.
type DeviceRow struct {
	Device
	UserName string `bun:"user_name"`
}

// ListDevices returns the devices of a marina, newest first.
func ListDevices(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeviceRow, error) {
	out := []DeviceRow{}
	err := db.NewSelect().TableExpr("devices AS d").ColumnExpr("d.*").ColumnExpr("u.name AS user_name").
		Join("JOIN users AS u ON u.id = d.user_id").
		Where("d.marina_id = ?", s.ID()).OrderExpr("d.id DESC").Scan(ctx, &out)
	return out, err
}

// GetDevice returns one device of a marina.
func GetDevice(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Device, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(Device)
	if err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// RevokeDevice revokes a device of a marina. Revoking twice is a no-op.
func RevokeDevice(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) error {
	if _, err := GetDevice(ctx, db, s, id); err != nil {
		return err
	}
	_, err := db.NewUpdate().Table("devices").Set("revoked_at = ?", at).Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("revoked_at IS NULL").Exec(ctx)
	return err
}

// DeviceByID returns a device by id regardless of marina (authenticated device paths only).
func DeviceByID(ctx context.Context, db bun.IDB, id string) (*Device, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(Device)
	if err := db.NewSelect().Model(d).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// DeviceByPrefix returns a device by token prefix (authentication path).
func DeviceByPrefix(ctx context.Context, db bun.IDB, prefix string) (*Device, error) {
	d := new(Device)
	if err := db.NewSelect().Model(d).Where("token_prefix = ?", prefix).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// TouchDevice records the last use of a device.
func TouchDevice(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("devices").Set("last_used_at = ?", at).Where("id = ?", id).Exec(ctx)
	return err
}

// InsertPairingCode inserts a pairing code. A hash collision is ErrConflict.
func InsertPairingCode(ctx context.Context, db bun.IDB, p *PairingCode) error {
	_, err := db.NewInsert().Model(p).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// ConsumePairingCode marks a valid pairing code used and returns it, or ErrNotFound. Single conditional UPDATE:
// a code pairs at most one device, also under concurrency.
func ConsumePairingCode(ctx context.Context, db bun.IDB, hash string, now time.Time) (*PairingCode, error) {
	var rows []PairingCode
	err := db.NewRaw(`UPDATE pairing_codes SET used_at = ?, updated_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING id, marina_id, user_id, created_by_user_id, kind, code_hash, expires_at, used_at, device_id,
		created_at, updated_at`, now, now, hash, now).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// SetPairingDevice links a used pairing code to the device it created.
func SetPairingDevice(ctx context.Context, db bun.IDB, codeID, deviceID string, at time.Time) error {
	_, err := db.NewUpdate().Table("pairing_codes").Set("device_id = ?", deviceID).Set("updated_at = ?", at).
		Where("id = ?", codeID).Exec(ctx)
	return err
}

// DeleteStalePairingCodes removes pairing codes that expired or were used before the given instant.
func DeleteStalePairingCodes(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("pairing_codes").
		WhereOr("expires_at <= ?", before).WhereOr("used_at <= ?", before).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}
