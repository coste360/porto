package device

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// PairSessionResult is a browser pairing (m6a-plan §3.2): the device (access browser) and a device session whose
// token goes into the porto_session cookie, never into the response body.
type PairSessionResult struct {
	SessionToken string
	ExpiresAt    time.Time
	// TOTPRequired: the user has a second factor; the session is usable after auth.VerifyDeviceSessionTOTP.
	TOTPRequired bool
	Device       Device
	UserID       string
	UserName     string
	MarinaID     string
	MarinaSlug   string
	MarinaName   string
	Permissions  []auth.Permission
}

// PairSession exchanges a pairing code for a browser device bound to a device session. The code is consumed by
// the same single conditional UPDATE as Pair, so a code creates at most one device whichever endpoint uses it
// (INV-78). Device, audit row, device.paired event and session are written in one transaction.
func (s *Service) PairSession(ctx context.Context, in PairInput) (*PairSessionResult, error) {
	in.AppVersion = ""
	if err := validatePairInput(&in); err != nil {
		return nil, err
	}
	if len(normalizeCode(in.Code)) != 8 {
		return nil, ErrPairingCodeInvalid
	}
	var out *PairSessionResult
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		pc, err := store.ConsumePairingCode(ctx, tx, hashCode(in.Code), now)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrPairingCodeInvalid
			}
			return err
		}
		if pc.Kind != "" && pc.Kind != in.Kind {
			return ErrPairingCodeInvalid
		}
		scope, err := store.ResolveMarinaScope(ctx, tx, pc.MarinaID)
		if err != nil {
			return ErrPairingCodeInvalid
		}
		u, err := store.GetUser(ctx, tx, pc.UserID)
		if err != nil || !u.Active {
			return ErrPairingCodeInvalid
		}
		ms, err := store.MembershipsOfUser(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		grants := auth.NarrowGrants(ms, scope.OrgID(), scope.ID())
		if len(grants) == 0 {
			return ErrPairingCodeInvalid
		}
		res, err := s.createDevice(ctx, tx, scope, u.ID, pc.ID, store.DeviceAccessBrowser, in, now)
		if err != nil {
			return err
		}
		if err := store.SetPairingDevice(ctx, tx, pc.ID, res.Device.ID, now); err != nil {
			return err
		}
		sess, err := auth.CreateDeviceSessionTx(ctx, tx, u.ID, res.Device.ID, now)
		if err != nil {
			return err
		}
		m, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return err
		}
		p := &auth.Principal{Kind: auth.KindUser, UserID: u.ID, Grants: grants}
		out = &PairSessionResult{
			SessionToken: sess.Token, ExpiresAt: sess.ExpiresAt, TOTPRequired: sess.TOTPRequired,
			Device: res.Device, UserID: u.ID, UserName: u.Name, MarinaID: m.ID, MarinaSlug: m.Slug, MarinaName: m.Name,
			Permissions: p.PermissionsOn(scope.OrgID(), scope.ID()),
		}
		return nil
	})
	return out, err
}
