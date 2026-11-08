//go:build integration

package contract

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestTransitQr_PayloadAndScope (domain half; the HTTP half renders and compares the SVG): PT1:<id> for a
// transit with contracts.read or transits.desk, 404 for mooring contracts, unknown ids and other marinas, 403
// for roles with neither permission.
func TestTransitQr_PayloadAndScope(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		tr := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-12", false)
		dock := e.AddUser(t, "dock-qr@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		yard := e.AddUser(t, "yard-qr@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		for _, p := range []*auth.Principal{e.Owner, dock} {
			got, err := e.svc.TransitQrPayload(ctx, p, e.MarinaID, tr.Row.ID)
			if err != nil || got != TransitQrPrefix+tr.Row.ID {
				t.Fatalf("payload %q %v", got, err)
			}
		}
		if _, err := e.svc.TransitQrPayload(ctx, yard, e.MarinaID, tr.Row.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard: %v", err)
		}
		_, other, otherOwner := e.OtherOrganization(t)
		if _, err := e.svc.TransitQrPayload(ctx, otherOwner, other, tr.Row.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other marina id: %v", err)
		}
		if _, err := e.svc.TransitQrPayload(ctx, otherOwner, e.MarinaID, tr.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina principal: %v", err)
		}
		if _, err := e.svc.TransitQrPayload(ctx, e.Owner, e.MarinaID, store.NewID()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown id: %v", err)
		}
		m := e.mooring(t, e.Owner, e.berths[1], "2027-03-01", false)
		if _, err := e.svc.TransitQrPayload(ctx, e.Owner, e.MarinaID, m.Row.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("mooring: %v", err)
		}
	})
}
