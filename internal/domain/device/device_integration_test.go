//go:build integration

package device_test

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func newSvc(e *fixture.Env, o device.Options) *device.Service {
	if o.BaseURL == "" {
		o.BaseURL = "http://localhost:8080/"
	}
	return device.NewService(e.DB, e.Clock, e.Audit, e.Outbox, e.Log, o)
}

var phone = device.PairInput{Name: "iPhone banchina", Platform: "ios", Kind: "mobile", AppVersion: "1.0.0"}

func TestDevice_PairAuthenticateRevoke(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e, device.Options{})
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)

		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{UserID: dock.UserID, Kind: "mobile"})
		if err != nil {
			t.Fatal(err)
		}
		if len(pc.Code) != 9 || pc.Code[4] != '-' || !pc.ExpiresAt.Equal(fixture.T0.Add(device.DefaultPairingTTL)) ||
			pc.QRPayload != "porto://pair?v=1&url=http%3A%2F%2Flocalhost%3A8080&code="+pc.Code || !strings.HasPrefix(pc.QRSVG, "<svg") {
			t.Fatalf("pairing code %+v", pc)
		}
		in := phone
		in.Code = strings.ToLower(strings.ReplaceAll(pc.Code, "-", " "))
		res, err := s.Pair(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(res.Token, "po_dev_") || res.UserID != dock.UserID || res.MarinaID != e.MarinaID || res.Device.UserName == "" {
			t.Fatalf("pair result %+v", res)
		}
		if _, err := s.Pair(ctx, in); !errors.Is(err, device.ErrPairingCodeInvalid) {
			t.Fatalf("second pairing: %v", err)
		}
		p, err := e.Auth.AuthenticateDevice(ctx, res.Token)
		if err != nil || p.Kind != auth.KindDevice || p.UserID != dock.UserID {
			t.Fatalf("authenticate: %+v %v", p, err)
		}
		if !p.Can(e.OrgID, e.MarinaID, auth.PermTransitsDesk) || p.Can(e.OrgID, e.MarinaID, auth.PermUsersManage) {
			t.Fatal("device permissions should be the dock role")
		}
		self, err := s.Self(ctx, p)
		if err != nil || self.MarinaSlug != "marina-test" || len(self.Permissions) == 0 {
			t.Fatalf("self %+v %v", self, err)
		}

		// Removing the membership revokes the device's access at once.
		tm := team.NewService(e.DB, e.Clock, e.Auth, e.Audit, e.Outbox, e.Log)
		if err := tm.RemoveMember(ctx, e.Owner, e.MarinaID, dock.UserID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Auth.AuthenticateDevice(ctx, res.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("device of removed member: %v", err)
		}

		// Owner pairs itself, then revokes the device.
		own, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{TTL: 5 * time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		in.Code = own.Code
		in.Kind = "desktop"
		in.Platform = "macos"
		res2, err := s.Pair(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		// An organization-wide owner's device is narrowed to its marina.
		p2, _ := e.Auth.AuthenticateDevice(ctx, res2.Token)
		other := e.AddMarina(t, "marina-due")
		if p2.Can(e.OrgID, other, auth.PermMarinaRead) || p2.Can(e.OrgID, "", auth.PermOrganizationManage) {
			t.Fatal("device not narrowed to its marina")
		}
		list, _ := s.List(ctx, e.Owner, e.MarinaID)
		if len(list) != 2 {
			t.Fatalf("list %+v", list)
		}
		if err := s.Revoke(ctx, e.Owner, e.MarinaID, res2.Device.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Auth.AuthenticateDevice(ctx, res2.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("revoked device accepted")
		}
		if len(e.Events(t, event.DevicePaired)) != 2 || len(e.Events(t, event.DeviceRevoked)) != 1 {
			t.Fatal("events")
		}
	})
}

func TestDevice_PairingRules(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e, device.Options{})
		ctx := fixture.Ctx()
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		if _, err := s.CreatePairingCode(ctx, office, e.MarinaID, device.PairingInput{}); err != nil {
			t.Fatalf("pair self: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, office, e.MarinaID, device.PairingInput{UserID: e.OwnerID}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office pairs for another user: %v", err)
		}
		// No escalation: a device token acts with the target's grants, so a director (devices.manage without
		// users.manage) cannot pair a device for an owner, and an API key never pairs for other users.
		director := e.AddUser(t, "director@porto.test", auth.RoleDirector, e.OrgID, e.MarinaID)
		if _, err := s.CreatePairingCode(ctx, director, e.MarinaID, device.PairingInput{UserID: e.OwnerID}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("director pairs for owner: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, director, e.MarinaID, device.PairingInput{UserID: office.UserID}); err != nil {
			t.Fatalf("director pairs for office: %v", err)
		}
		key := &auth.Principal{
			Kind: auth.KindAPIKey, APIKeyID: store.NewID(), Scopes: []auth.Permission{auth.PermDevicesManage},
			Grants: []auth.Grant{{OrganizationID: e.OrgID, MarinaID: e.MarinaID, Role: auth.RoleOwner}},
		}
		if _, err := s.CreatePairingCode(ctx, key, e.MarinaID, device.PairingInput{UserID: e.OwnerID}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("api key pairs for owner: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, key, e.MarinaID, device.PairingInput{UserID: office.UserID}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("api key pairs for office: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{UserID: store.NewID()}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("pair for stranger: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{TTL: 2 * time.Hour}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("ttl too long: %v", err)
		}
		pc, _ := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{Kind: "desktop"})
		in := phone
		in.Code = pc.Code
		if _, err := s.Pair(ctx, in); !errors.Is(err, device.ErrPairingCodeInvalid) {
			t.Fatalf("kind mismatch: %v", err)
		}
		e.Clock.Advance(device.DefaultPairingTTL + time.Second)
		in.Kind = "desktop"
		if _, err := s.Pair(ctx, in); !errors.Is(err, device.ErrPairingCodeInvalid) {
			t.Fatalf("expired code: %v", err)
		}
		if _, err := s.Pair(ctx, device.PairInput{Code: "x", Name: "", Platform: "palm", Kind: "watch"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("invalid input: %v", err)
		}
		if _, err := s.DemoPair(ctx, phone); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("demo disabled: %v", err)
		}
		demo := newSvc(e, device.Options{DemoUserEmail: "office@porto.test", DemoMarinaSlug: "marina-test"})
		res, err := demo.DemoPair(ctx, phone)
		if err != nil || res.UserID != office.UserID {
			t.Fatalf("demo pair: %+v %v", res, err)
		}
	})
}

func TestConcurrency_PairingCodeSingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e, device.Options{})
		ctx := fixture.Ctx()
		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if err != nil {
			t.Fatal(err)
		}
		var ok, invalid atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				in := phone
				in.Code = pc.Code
				switch _, err := s.Pair(ctx, in); {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, device.ErrPairingCodeInvalid):
					invalid.Add(1)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if ok.Load() != 1 || invalid.Load() != 99 {
			t.Fatalf("ok=%d invalid=%d", ok.Load(), invalid.Load())
		}
		list, _ := s.List(ctx, e.Owner, e.MarinaID)
		if len(list) != 1 {
			t.Fatalf("devices = %d", len(list))
		}
	})
}
