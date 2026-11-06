//go:build integration

package vessel_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	cust *customer.Service
	ves  *vessel.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	return &env{
		Env:  e,
		cust: customer.NewService(customer.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		ves:  vessel.NewService(vessel.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
	}
}

func ptr[T any](v T) *T { return &v }

func (e *env) customer(t *testing.T, last string) string {
	t.Helper()
	res, err := e.cust.Create(fixture.Ctx(), e.Owner, e.MarinaID, customer.Patch{Kind: ptr(customer.KindPerson), LastName: &last})
	if err != nil {
		t.Fatal(err)
	}
	return res.Customer.ID
}

func fieldCode(err error) map[string]string {
	out := map[string]string{}
	var v *auth.ValidationError
	if errors.As(err, &v) {
		for _, f := range v.Fields {
			out[f.Field] = f.Code
		}
	}
	return out
}

func TestVessel_PartiesOwnerSharesAtMost1000(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a, b, c, co := e.customer(t, "A"), e.customer(t, "B"), e.customer(t, "C"), e.customer(t, "Co")
		v, err := e.ves.Create(ctx, e.Owner, e.MarinaID, vessel.Patch{Name: ptr("Aurora"), LOACm: ptr(int64(1200)), BeamCm: ptr(int64(400))},
			[]vessel.PartyInput{
				{CustomerID: a, Role: "owner", ShareMillis: ptr(int64(600))},
				{CustomerID: b, Role: "owner", ShareMillis: ptr(int64(400))},
				{CustomerID: co, Role: "billing"},
			})
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Parties) != 3 || v.Parties[0].Role != "owner" || v.Parties[2].Role != "billing" || v.Row.Version != 1 {
			t.Fatalf("parties %+v", v.Parties)
		}
		over := []vessel.PartyInput{
			{CustomerID: a, Role: "owner", ShareMillis: ptr(int64(600))},
			{CustomerID: b, Role: "owner", ShareMillis: ptr(int64(400))},
			{CustomerID: c, Role: "owner", ShareMillis: ptr(int64(100))},
		}
		if _, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 1, over); fieldCode(err)["parties"] != "owner_shares_exceed" {
			t.Fatalf("over 1000: %v", err)
		}
		for name, c := range map[string]struct {
			in    []vessel.PartyInput
			field string
			code  string
		}{
			"owner without share": {[]vessel.PartyInput{{CustomerID: a, Role: "owner"}}, "parties[0].share_millis", "owner_share_required"},
			"share on skipper":    {[]vessel.PartyInput{{CustomerID: a, Role: "skipper", ShareMillis: ptr(int64(10))}}, "parties[0].share_millis", "share_not_allowed"},
			"unknown customer":    {[]vessel.PartyInput{{CustomerID: "01900000-0000-7000-8000-000000000009", Role: "contact"}}, "parties[0].customer_id", "unknown_customer"},
			"duplicate":           {[]vessel.PartyInput{{CustomerID: a, Role: "contact"}, {CustomerID: a, Role: "contact"}}, "parties[1]", "duplicate"},
			"bad role":            {[]vessel.PartyInput{{CustomerID: a, Role: "captain"}}, "parties[0].role", "invalid"},
		} {
			if _, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 1, c.in); fieldCode(err)[c.field] != c.code {
				t.Errorf("%s: %v", name, err)
			}
		}
		// A rejected replacement leaves the vessel as it was.
		got, _ := e.ves.Get(ctx, e.Owner, e.MarinaID, v.Row.ID)
		if got.Row.Version != 1 || len(got.Parties) != 3 {
			t.Fatalf("after rejections %+v", got)
		}
		if _, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 5, nil); !errors.Is(err, vessel.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		ok, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 1, []vessel.PartyInput{
			{CustomerID: a, Role: "owner", ShareMillis: ptr(int64(1000))}, {CustomerID: a, Role: "skipper"},
		})
		if err != nil || ok.Row.Version != 2 || len(ok.Parties) != 2 {
			t.Fatalf("replace %+v %v", ok, err)
		}
		// A customer of another marina is unknown here.
		other := e.AddMarina(t, "altra")
		oc, err := e.cust.Create(ctx, e.Owner, other, customer.Patch{Kind: ptr(customer.KindPerson), LastName: ptr("Z")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 2, []vessel.PartyInput{{CustomerID: oc.Customer.ID, Role: "contact"}}); fieldCode(err)["parties[0].customer_id"] != "unknown_customer" {
			t.Fatalf("foreign customer: %v", err)
		}
		// An anonymised customer cannot be linked.
		if err := e.cust.Anonymize(ctx, e.Owner, e.MarinaID, c, true); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ves.ReplaceParties(ctx, e.Owner, e.MarinaID, v.Row.ID, 2, []vessel.PartyInput{{CustomerID: c, Role: "contact"}}); !errors.Is(err, customer.ErrAnonymized) {
			t.Fatalf("anonymised customer: %v", err)
		}
		if n := len(e.Events(t, event.VesselChanged)); n != 2 {
			t.Fatalf("vessel.changed events: %d", n)
		}
	})
}

func TestVessel_MeasurementSetsSource(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		v, err := e.ves.Create(ctx, e.Owner, e.MarinaID, vessel.Patch{Name: ptr("Misurata"), LOACm: ptr(int64(1180)), BeamCm: ptr(int64(390))}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if v.EffectiveLOACm != 1180 || v.Row.DimensionsSource != "declared" || v.UnitClass != vessel.UnitImbarcazione {
			t.Fatalf("declared %+v", v)
		}
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.ves.RecordMeasurement(ctx, viewer, e.MarinaID, v.Row.ID, vessel.Measurement{LOACm: 1250, BeamCm: 400}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		accounting := e.AddUser(t, "acc@porto.test", auth.RoleAccounting, e.OrgID, e.MarinaID)
		if _, err := e.ves.RecordMeasurement(ctx, accounting, e.MarinaID, v.Row.ID, vessel.Measurement{LOACm: 1250, BeamCm: 400}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("accounting: %v", err)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.ves.RecordMeasurement(ctx, dock, e.MarinaID, v.Row.ID, vessel.Measurement{LOACm: 0, BeamCm: 400}); fieldCode(err)["loa_cm"] != "invalid" {
			t.Fatalf("zero loa: %v", err)
		}
		m, err := e.ves.RecordMeasurement(ctx, dock, e.MarinaID, v.Row.ID, vessel.Measurement{LOACm: 1250, BeamCm: 405, DraftCm: 160})
		if err != nil {
			t.Fatal(err)
		}
		if m.Row.DimensionsSource != "measured" || m.EffectiveLOACm != 1250 || m.EffectiveBeamCm != 405 || m.EffectiveDraftCm != 160 ||
			m.Row.MeasuredAt.IsZero() || m.MeasuredBy == nil || m.MeasuredBy.ID != dock.UserID || m.Row.Version != 2 {
			t.Fatalf("measured %+v", m)
		}
		// Back to declared, then measured again through the patch.
		back, err := e.ves.Update(ctx, e.Owner, e.MarinaID, v.Row.ID, 2, vessel.Patch{DimensionsSource: ptr("declared")})
		if err != nil || back.EffectiveLOACm != 1180 {
			t.Fatalf("declared again %+v %v", back, err)
		}
		// Unit class thresholds come from the marina settings.
		mk := marina.NewService(e.DB, e.Clock, e.Audit, e.Outbox, e.Log)
		mr, err := mk.GetMarina(ctx, e.Owner, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		settings := json.RawMessage(`{"registry":{"unit_class_thresholds_cm":[1200,3000]}}`)
		if _, err := mk.UpdateMarina(ctx, e.Owner, e.MarinaID, mr.Version, marina.MarinaPatch{Settings: &settings}); err != nil {
			t.Fatal(err)
		}
		again, _ := e.ves.Get(ctx, e.Owner, e.MarinaID, v.Row.ID)
		if again.UnitClass != vessel.UnitNatante {
			t.Fatalf("thresholds from settings: %s", again.UnitClass)
		}
		if n := len(e.Events(t, event.VesselChanged)); n != 3 {
			t.Fatalf("vessel.changed events: %d", n)
		}
	})
}
