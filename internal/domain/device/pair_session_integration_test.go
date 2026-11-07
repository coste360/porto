//go:build integration

package device_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/qrsvg"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

var browserPhone = device.PairInput{Name: "Telefono Android", Platform: "android", Kind: "mobile"}

func TestDevice_PairURLFromTarget(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		fp := strings.Repeat("A", 43)
		var target desktopport.PairTarget
		s := newSvc(e, device.Options{PairTarget: func() desktopport.PairTarget { return target }})

		// Desktop with the local network on: the QR opens the LAN URL with the fingerprint.
		target = desktopport.PairTarget{Origin: "https://192.168.1.20:8443/", Fingerprint: fp, Reachable: true}
		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if err != nil {
			t.Fatal(err)
		}
		if pc.PairURL != "https://192.168.1.20:8443/admin/pair#c="+pc.Code+"&fp="+fp || !pc.Reachable || pc.Fingerprint != fp {
			t.Fatalf("pairing code %+v", pc)
		}
		want, _ := qrsvg.Render(pc.PairURL)
		if pc.QRSVG != want {
			t.Fatal("QR does not encode pair_url")
		}
		if pc.QRPayload != "porto://pair?v=1&url=https%3A%2F%2F192.168.1.20%3A8443&code="+pc.Code+"&fp="+fp {
			t.Fatalf("native payload %s", pc.QRPayload)
		}

		// Local network off: unreachable, no fingerprint; the native payload keeps the base URL.
		target = desktopport.PairTarget{Origin: "http://127.0.0.1:18090"}
		off, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if err != nil {
			t.Fatal(err)
		}
		if off.Reachable || off.Fingerprint != "" || off.PairURL != "http://127.0.0.1:18090/admin/pair#c="+off.Code ||
			off.QRPayload != "porto://pair?v=1&url=http%3A%2F%2Flocalhost%3A8080&code="+off.Code {
			t.Fatalf("pairing code with LAN off %+v", off)
		}

		// Server edition: the base URL, reachable.
		srv, _ := newSvc(e, device.Options{}).CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if !srv.Reachable || srv.PairURL != "http://localhost:8080/admin/pair#c="+srv.Code {
			t.Fatalf("server edition %+v", srv)
		}
	})
}

func TestDevice_PairSessionCreatesBrowserDevice(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		s := newSvc(e, device.Options{})
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{UserID: dock.UserID, Kind: "mobile"})
		if err != nil {
			t.Fatal(err)
		}
		// Wrong kind or bad input consume nothing useful and give the uniform errors.
		bad := browserPhone
		bad.Code, bad.Platform = pc.Code, "symbian"
		var verr *auth.ValidationError
		if _, err := s.PairSession(ctx, bad); !errors.As(err, &verr) {
			t.Fatalf("invalid platform: %v", err)
		}
		in := browserPhone
		in.Code = strings.ToLower(pc.Code)
		res, err := s.PairSession(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if res.SessionToken == "" || res.TOTPRequired || res.Device.Access != store.DeviceAccessBrowser ||
			res.UserID != dock.UserID || res.MarinaID != e.MarinaID || res.MarinaSlug != "marina-test" ||
			!slices.Contains(res.Permissions, auth.PermTransitsDesk) || slices.Contains(res.Permissions, auth.PermUsersManage) {
			t.Fatalf("result %+v", res)
		}
		if _, err := s.PairSession(ctx, in); !errors.Is(err, device.ErrPairingCodeInvalid) {
			t.Fatalf("code reused: %v", err)
		}
		p, err := e.Auth.AuthenticateSession(ctx, res.SessionToken)
		if err != nil || p.UserID != dock.UserID || p.DeviceID != res.Device.ID {
			t.Fatalf("session principal %+v %v", p, err)
		}
		list, _ := s.List(ctx, e.Owner, e.MarinaID)
		if len(list) != 1 || list[0].Access != store.DeviceAccessBrowser || list[0].UserName == "" {
			t.Fatalf("list %+v", list)
		}
		if len(e.Events(t, event.DevicePaired)) != 1 || e.AuditCount(t, "device.pair") != 1 {
			t.Fatal("pairing not audited or published")
		}
		var diff string
		if err := db.NewRaw("SELECT diff FROM audit_log WHERE action = 'device.pair'").Scan(ctx, &diff); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ReplaceAll(diff, " ", ""), `"access":"browser"`) {
			t.Fatalf("audit diff %s", diff)
		}
		if strings.Contains(diff, res.SessionToken) {
			t.Fatal("token in the audit row")
		}
	})
}

func TestDeviceSession_RevokeKillsSession(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		s := newSvc(e, device.Options{})
		pc, _ := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		in := browserPhone
		in.Code = pc.Code
		res, err := s.PairSession(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := store.CountSessionsOfDevice(ctx, db.DB, res.Device.ID); n != 1 {
			t.Fatalf("sessions %d", n)
		}
		if err := s.Revoke(ctx, e.Owner, e.MarinaID, res.Device.ID); err != nil {
			t.Fatal(err)
		}
		if n, _ := store.CountSessionsOfDevice(ctx, db.DB, res.Device.ID); n != 0 {
			t.Fatalf("sessions after revoke %d", n)
		}
		if _, err := e.Auth.AuthenticateSession(ctx, res.SessionToken); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("revoked browser device still authenticates: %v", err)
		}
		if len(e.Events(t, event.DeviceRevoked)) != 1 {
			t.Fatal("revoke event")
		}
	})
}

// pairOutcome counts the results of concurrent pairings on one code.
type pairOutcome struct{ ok, invalid atomic.Int32 }

func (o *pairOutcome) record(t *testing.T, err error) {
	switch {
	case err == nil:
		o.ok.Add(1)
	case errors.Is(err, device.ErrPairingCodeInvalid):
		o.invalid.Add(1)
	default:
		t.Error(err)
	}
}

func TestConcurrency_PairSessionSingleUse(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		s := newSvc(e, device.Options{})
		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if err != nil {
			t.Fatal(err)
		}
		var out pairOutcome
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				in := browserPhone
				in.Code = pc.Code
				_, err := s.PairSession(ctx, in)
				out.record(t, err)
			})
		}
		wg.Wait()
		if out.ok.Load() != 1 || out.invalid.Load() != 99 {
			t.Fatalf("ok=%d invalid=%d", out.ok.Load(), out.invalid.Load())
		}
		assertOneDevice(t, e, s)
		var sessions int
		if err := db.NewRaw("SELECT COUNT(*) FROM sessions WHERE device_id IS NOT NULL").Scan(ctx, &sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 1 {
			t.Fatalf("device sessions = %d", sessions)
		}
	})
}

func TestConcurrency_PairingMixedEndpoints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		s := newSvc(e, device.Options{})
		pc, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{})
		if err != nil {
			t.Fatal(err)
		}
		var out pairOutcome
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				if i%2 == 0 {
					in := phone
					in.Code = pc.Code
					_, err := s.Pair(ctx, in)
					out.record(t, err)
					return
				}
				in := browserPhone
				in.Code = pc.Code
				_, err := s.PairSession(ctx, in)
				out.record(t, err)
			})
		}
		wg.Wait()
		if out.ok.Load() != 1 || out.invalid.Load() != 99 {
			t.Fatalf("ok=%d invalid=%d", out.ok.Load(), out.invalid.Load())
		}
		assertOneDevice(t, e, s)
	})
}

func assertOneDevice(t *testing.T, e *fixture.Env, s *device.Service) {
	t.Helper()
	list, _ := s.List(fixture.Ctx(), e.Owner, e.MarinaID)
	if len(list) != 1 {
		t.Fatalf("devices = %d", len(list))
	}
	if n := len(e.Events(t, event.DevicePaired)); n != 1 {
		t.Fatalf("device.paired events = %d", n)
	}
}

// TestDeviceSession_OtherMarinaIs404 covers INV-81 at the device service: an organization-wide owner pairs a
// browser on marina A; with that device session, marina B and its devices do not exist (404), A still works.
func TestDeviceSession_OtherMarinaIs404(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		ctx := fixture.Ctx()
		s := newSvc(e, device.Options{})
		marinaB := e.AddMarina(t, "marina-b")
		if _, err := s.List(ctx, e.Owner, marinaB); err != nil {
			t.Fatalf("the owner login reaches marina B: %v", err)
		}
		// A device of marina B, paired by the owner from the login session.
		pcB, err := s.CreatePairingCode(ctx, e.Owner, marinaB, device.PairingInput{Kind: "mobile"})
		if err != nil {
			t.Fatal(err)
		}
		inB := browserPhone
		inB.Code = pcB.Code
		devB, err := s.PairSession(ctx, inB)
		if err != nil || devB.MarinaID != marinaB {
			t.Fatalf("pair on B: %+v %v", devB, err)
		}
		// The phone of marina A.
		pcA, err := s.CreatePairingCode(ctx, e.Owner, e.MarinaID, device.PairingInput{Kind: "mobile"})
		if err != nil {
			t.Fatal(err)
		}
		inA := browserPhone
		inA.Code = pcA.Code
		resA, err := s.PairSession(ctx, inA)
		if err != nil {
			t.Fatal(err)
		}
		p, err := e.Auth.AuthenticateSession(ctx, resA.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Authorize(ctx, db.DB, marinaB, auth.PermMarinaRead); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("marina B from the A device session: %v", err)
		}
		if _, err := s.List(ctx, p, marinaB); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("device list of B: %v", err)
		}
		if _, err := s.CreatePairingCode(ctx, p, marinaB, device.PairingInput{Kind: "mobile"}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("pairing code on B: %v", err)
		}
		if err := s.Revoke(ctx, p, marinaB, devB.Device.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("revoke on B: %v", err)
		}
		// B's device through A's scope is not found either, and stays alive.
		if err := s.Revoke(ctx, p, e.MarinaID, devB.Device.ID); err == nil {
			t.Fatal("revoked a device of B through marina A")
		}
		if _, err := e.Auth.AuthenticateSession(ctx, devB.SessionToken); err != nil {
			t.Fatalf("device of B was harmed: %v", err)
		}
		if list, err := s.List(ctx, p, e.MarinaID); err != nil || len(list) != 1 || list[0].ID != resA.Device.ID {
			t.Fatalf("device list of A: %+v %v", list, err)
		}
	})
}
