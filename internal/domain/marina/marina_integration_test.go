//go:build integration

package marina_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func ptr[T any](v T) *T { return &v }

func newSvc(e *fixture.Env) *marina.Service {
	return marina.NewService(e.DB, e.Clock, e.Audit, e.Outbox, e.Log)
}

func TestMarina_CreateGetUpdateArchive(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()

		m, err := s.CreateMarina(ctx, e.Owner, e.OrgID, marina.CreateMarinaInput{Name: "Porto Nuovo", Slug: "porto-nuovo", Locales: []string{"it", "en"}})
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind != "marina" || m.Currency != "EUR" || m.Version != 1 || len(m.Locales) != 2 || string(m.Settings) != "{}" {
			t.Fatalf("created %+v", m)
		}
		if _, err := s.CreateMarina(ctx, e.Owner, e.OrgID, marina.CreateMarinaInput{Name: "Dup", Slug: "porto-nuovo"}); !errors.Is(err, marina.ErrSlugTaken) {
			t.Fatalf("dup slug: %v", err)
		}
		if _, err := s.CreateMarina(ctx, e.Owner, e.OrgID, marina.CreateMarinaInput{Name: "Bad", Slug: "admin", Timezone: "Mars/Base"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("invalid: %v", err)
		}
		if len(e.Events(t, event.MarinaCreated)) != 1 || e.AuditCount(t, "marina.create") != 1 {
			t.Fatal("create not audited or evented")
		}

		settings := json.RawMessage(`{"berths": {"default_margin_cm": 50}}`)
		up, err := s.UpdateMarina(ctx, e.Owner, m.ID, 1, marina.MarinaPatch{
			Name: ptr("Porto Nuovo Sud"), Settings: &settings, BookingEnabled: ptr(true),
			Geo: &marina.Geo{LatE7: 412_345_678, LonE7: 133_456_789}, ConcessionExpiresOn: ptr(calendar.MustParse("2030-12-31")),
			Contacts:            &marina.Contacts{Email: "Info@Porto.test", VHFChannel: "9"},
			AllowedEmbedOrigins: &[]string{"https://www.porto.test/"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if up.Version != 2 || up.Name != "Porto Nuovo Sud" || !up.BookingEnabled || up.Geo == nil || up.Geo.LatE7 != 412_345_678 ||
			up.ConcessionExpiresOn.String() != "2030-12-31" || up.Contacts.Email != "info@porto.test" ||
			string(up.Settings) != `{"berths":{"default_margin_cm":50}}` || up.AllowedEmbedOrigins[0] != "https://www.porto.test" {
			t.Fatalf("updated %+v", up)
		}
		if _, err := s.UpdateMarina(ctx, e.Owner, m.ID, 1, marina.MarinaPatch{Name: ptr("Stale")}); !errors.Is(err, marina.ErrVersionMismatch) {
			t.Fatalf("stale If-Match: %v", err)
		}
		bad := json.RawMessage(`[1,2]`)
		if _, err := s.UpdateMarina(ctx, e.Owner, m.ID, 2, marina.MarinaPatch{Settings: &bad}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("array settings: %v", err)
		}
		got, err := s.GetMarina(ctx, e.Owner, m.ID)
		if err != nil || got.Version != 2 || got.ConcessionExpiresOn.String() != "2030-12-31" {
			t.Fatalf("get %+v %v", got, err)
		}
		cleared, err := s.UpdateMarina(ctx, e.Owner, m.ID, 2, marina.MarinaPatch{ClearGeo: true, ClearConcessionExpiresOn: true})
		if err != nil || cleared.Geo != nil || !cleared.ConcessionExpiresOn.IsZero() {
			t.Fatalf("clear %+v %v", cleared, err)
		}

		pub, err := s.PublicBySlug(ctx, "porto-nuovo")
		if err != nil || pub.Name != "Porto Nuovo Sud" || !pub.BookingEnabled {
			t.Fatalf("public %+v %v", pub, err)
		}
		if err := s.ArchiveMarina(ctx, e.Owner, m.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetMarina(ctx, e.Owner, m.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("archived marina visible: %v", err)
		}
		if _, err := s.PublicBySlug(ctx, "porto-nuovo"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("archived public: %v", err)
		}
		list, _ := s.ListMarinas(ctx, e.Owner, e.OrgID)
		if len(list) != 1 || list[0].ID != e.MarinaID {
			t.Fatalf("list after archive: %+v", list)
		}
		if len(e.Events(t, event.MarinaArchived)) != 1 {
			t.Fatal("no archive event")
		}
	})
}

func TestMarina_Permissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		second := e.AddMarina(t, "marina-due")
		director := e.AddUser(t, "director@porto.test", auth.RoleDirector, e.OrgID, e.MarinaID)
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		marinaOwner := e.AddUser(t, "mowner@porto.test", auth.RoleOwner, e.OrgID, e.MarinaID)

		if _, err := s.UpdateMarina(ctx, director, e.MarinaID, 1, marina.MarinaPatch{Name: ptr("By director")}); err != nil {
			t.Fatalf("director configure: %v", err)
		}
		if _, err := s.UpdateMarina(ctx, viewer, e.MarinaID, 2, marina.MarinaPatch{Name: ptr("By viewer")}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer configure: %v", err)
		}
		if _, err := s.GetMarina(ctx, viewer, second); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("viewer on other marina: %v", err)
		}
		list, err := s.ListMarinas(ctx, viewer, e.OrgID)
		if err != nil || len(list) != 1 || list[0].ID != e.MarinaID {
			t.Fatalf("viewer list: %+v %v", list, err)
		}
		if all, _ := s.ListMarinas(ctx, e.Owner, e.OrgID); len(all) != 2 {
			t.Fatalf("owner list: %+v", all)
		}
		if _, err := s.CreateMarina(ctx, marinaOwner, e.OrgID, marina.CreateMarinaInput{Name: "X", Slug: "marina-x"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("marina-level owner creating marina: %v", err)
		}
		if err := s.ArchiveMarina(ctx, marinaOwner, e.MarinaID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("marina-level owner archiving: %v", err)
		}
		orgs, _ := s.ListOrganizations(ctx, viewer)
		if len(orgs) != 1 || orgs[0].ID != e.OrgID {
			t.Fatalf("orgs %+v", orgs)
		}
		_, otherMarina, other := e.OtherOrganization(t)
		if _, err := s.GetMarina(ctx, other, e.MarinaID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-org get: %v", err)
		}
		if _, err := s.ListMarinas(ctx, other, e.OrgID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-org list: %v", err)
		}
		if _, err := s.GetMarina(ctx, e.Owner, otherMarina); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("owner on foreign marina: %v", err)
		}
	})
}

func TestOrganization_Update(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := fixture.New(t, db)
		s := newSvc(e)
		ctx := fixture.Ctx()
		o, err := s.UpdateOrganization(ctx, e.Owner, e.OrgID, 1, marina.OrganizationPatch{
			Name: ptr("Porto Test SRL"), VATNumber: ptr("it 01234567890"), SDICode: ptr("abc1234"),
			BillingAddress: &marina.Address{Line1: "Via del Porto 1", City: "Gaeta", Country: "IT"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if o.Version != 2 || o.VATNumber != "IT01234567890" || o.SDICode != "ABC1234" || o.BillingAddress.City != "Gaeta" {
			t.Fatalf("%+v", o)
		}
		if _, err := s.UpdateOrganization(ctx, e.Owner, e.OrgID, 1, marina.OrganizationPatch{Name: ptr("x")}); !errors.Is(err, marina.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if _, err := s.UpdateOrganization(ctx, e.Owner, e.OrgID, 2, marina.OrganizationPatch{SDICode: ptr("toolongcode")}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("bad sdi: %v", err)
		}
		dir := e.AddUser(t, "dir@porto.test", auth.RoleDirector, e.OrgID, "")
		if _, err := s.UpdateOrganization(ctx, dir, e.OrgID, 2, marina.OrganizationPatch{Name: ptr("x")}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("director org update: %v", err)
		}
		evs := e.Events(t, event.OrganizationUpdated)
		if len(evs) != 1 || evs[0].MarinaID != "" || evs[0].OrganizationID != e.OrgID {
			t.Fatalf("events %+v", evs)
		}
	})
}
