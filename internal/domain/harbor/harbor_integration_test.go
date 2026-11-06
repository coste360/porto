//go:build integration

package harbor

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/harborcheck"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestHarbor_CreateSaveReload(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 60000, 40000)
		if d.Version != 1 || d.GridCm != 50 || len(d.Berths) != 0 || d.Icon != "map" {
			t.Fatalf("created %+v", d)
		}
		req := d.Request()
		req.Structures = append(req.Structures, Structure{Key: "q", Kind: "quay", Code: "Q", Points: []geo.Point{{X: 0, Y: 1000}, {X: 60000, Y: 1000}}, Sides: []string{"right"}})
		addPier(t, &req, "a-", "A", 5000, 10)
		req.Structures[1].ParentID = "q"
		req.Structures[1].Sides = append(req.Structures[1].Sides, "head")
		req.WaterPolygons = [][]geo.Point{{{X: 0, Y: 1000}, {X: 60000, Y: 1000}, {X: 60000, Y: 40000}, {X: 0, Y: 40000}}}
		req.Pedestals = []Pedestal{{Key: "c1", Code: "C1", Position: geo.Point{X: 5100, Y: 1200}, Sockets: []Socket{{Code: "S1", Kind: "power", Amps: 16}}}}
		req.Berths[0].PedestalID, req.Berths[0].PedestalSocket = "c1", "S1"
		req.DynamicAreas = []DynamicArea{{
			Key: "t1", Kind: "linear", Code: "T-1", StructureID: "a-s1", Side: "head", FromCm: i64(0), ToCm: i64(300),
			MooringMode: "stern_to", MaxBeamCm: 500, BlockedCells: []int{}, Exits: []int{}, Active: true,
		}}
		req.LandResources = []LandResource{{Key: "p1", Kind: "parking", Code: "PK1", Anchor: geo.Point{X: 30000, Y: 200}, WidthCm: 250, LengthCm: 500, RotationDd: 900, Active: true}}
		req.Decorations = []Decoration{{Key: "d1", Kind: "symbol", Asset: "fuel_station", Position: &geo.Point{X: 100, Y: 100}, SizeCm: 300}}
		res := saveDoc(t, e, s, d.ID, 1, req)
		if res.Document.Version != 2 || len(res.Document.Berths) != 20 || len(res.IDMap) != 26 {
			t.Fatalf("saved version %d, %d berths, %d ids", res.Document.Version, len(res.Document.Berths), len(res.IDMap))
		}
		got, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		gb, _ := json.Marshal(got)
		rb, _ := json.Marshal(res.Document)
		if !bytes.Equal(gb, rb) {
			t.Fatal("reloaded document differs from the save result")
		}
		a := got.DynamicAreas[0]
		if a.CellCm == nil || *a.CellCm != 50 || *a.MarginCm != 50 || *a.CellCount != 6 || *a.FromCm != 0 {
			t.Fatalf("area defaults %+v", a)
		}
		if b := got.Berths[0]; b.PedestalID != res.IDMap["c1"] || b.StructureID != res.IDMap["a-s1"] || b.MaxLOACm != 1200 || b.MaxBeamCm != 350 {
			t.Fatalf("berth %+v", b)
		}
		if got.Structures[1].ParentID != res.IDMap["q"] {
			t.Fatal("pier parent not resolved")
		}
		// Saving the reloaded document unchanged is a no-op on the rows but bumps the version.
		res2 := saveDoc(t, e, s, d.ID, 2, got.Request())
		if res2.Document.Version != 3 || len(res2.IDMap) != 0 || res2.Document.Berths[0].ID != got.Berths[0].ID {
			t.Fatalf("resave %+v", res2.Document.Version)
		}
		list, err := s.ListMaps(ctx, e.Owner, e.MarinaID)
		if err != nil || len(list) != 1 || list[0].Counts.Berths != 20 || list[0].Counts.Structures != 2 || list[0].Counts.Decorations != 1 {
			t.Fatalf("list %+v %v", list, err)
		}
		page, err := s.ListBerths(ctx, e.Owner, e.MarinaID, BerthFilter{Q: "a-1"}, "", 0)
		if err != nil || len(page.Items) != 11 || page.Items[0].StructureCode != "A" { // A-1, A-10..A-19
			t.Fatalf("berth list %d %v", len(page.Items), err)
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func TestHarbor_Permissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 10000, 10000)
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		director := e.AddUser(t, "director@porto.test", auth.RoleDirector, e.OrgID, e.MarinaID)
		if _, err := s.GetMap(ctx, dock, e.MarinaID, d.ID); err != nil {
			t.Fatalf("dock reads: %v", err)
		}
		if _, err := s.SaveMap(ctx, dock, e.MarinaID, d.ID, 1, d.Request()); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock saves: %v", err)
		}
		if _, err := s.SaveMap(ctx, director, e.MarinaID, d.ID, 1, d.Request()); err != nil {
			t.Fatalf("director saves: %v", err)
		}
		_, other, otherOwner := e.OtherOrganization(t)
		if _, err := s.GetMap(ctx, otherOwner, e.MarinaID, d.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other organization: %v", err)
		}
		if _, err := s.GetMap(ctx, otherOwner, other, d.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("map of another marina through own marina: %v", err)
		}
	})
}

func TestHarbor_SaveRejectsOverlapAndLeavesStateUnchanged(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 6)
		res := saveDoc(t, e, s, d.ID, 1, req)
		events, audits := len(e.Events(t, event.ConfigMapChanged)), e.AuditCount(t, "map.save")
		bad := res.Document.Request()
		bad.Berths[1].Anchor = bad.Berths[0].Anchor
		bad.Berths = append(bad.Berths, Berth{
			Key: "new", Code: "NEW", Side: "none", Anchor: geo.Point{X: 100, Y: 100}, WidthCm: 300, LengthCm: 800,
			MooringType: "mooring_line", Attributes: []string{}, GroupIDs: []string{},
		})
		_, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 2, bad)
		var mi *MapInvalidError
		if !errors.As(err, &mi) || !errors.Is(err, ErrMapInvalid) {
			t.Fatalf("overlap save: %v", err)
		}
		found := slices.ContainsFunc(mi.Report.Errors, func(is layout.Issue) bool {
			return is.Code == layout.CodeBerthOverlap && slices.Contains(is.Refs, layout.Ref(bad.Berths[0].ID)) && slices.Contains(is.Refs, layout.Ref(bad.Berths[1].ID))
		})
		if !found {
			t.Fatalf("issues %+v", mi.Report.Errors)
		}
		after, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		ab, _ := json.Marshal(after)
		rb, _ := json.Marshal(res.Document)
		if !bytes.Equal(ab, rb) {
			t.Fatal("a refused save changed the map")
		}
		if len(e.Events(t, event.ConfigMapChanged)) != events || e.AuditCount(t, "map.save") != audits {
			t.Fatal("a refused save wrote an event or an audit row")
		}
		rep, err := s.ValidateMap(ctx, e.Owner, e.MarinaID, d.ID, bad)
		if err != nil || !slices.ContainsFunc(rep.Errors, func(is layout.Issue) bool { return is.Code == layout.CodeBerthOverlap }) {
			t.Fatalf("validate %v %v", rep, err)
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func TestHarbor_SaveVersionConflict(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		d := newMap(t, e, s, 10000, 10000)
		saveDoc(t, e, s, d.ID, 1, d.Request())
		if _, err := s.SaveMap(fixture.Ctx(), e.Owner, e.MarinaID, d.ID, 1, d.Request()); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale save: %v", err)
		}
		if err := s.DeleteMap(fixture.Ctx(), e.Owner, e.MarinaID, d.ID, 1); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
	})
}

func TestHarbor_RenumberSwapCodes(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 3)
		res := saveDoc(t, e, s, d.ID, 1, req)
		doc := res.Document.Request()
		ids := []string{doc.Berths[0].ID, doc.Berths[1].ID}
		doc.Berths[0].Code, doc.Berths[1].Code = doc.Berths[1].Code, doc.Berths[0].Code
		// A new berth takes the code of a renumbered one.
		old := doc.Berths[2].Code
		doc.Berths[2].Code = "A-99"
		doc.Berths = append(doc.Berths, Berth{
			Key: "n", Code: old, Side: "none", Anchor: geo.Point{X: 15000, Y: 15000}, WidthCm: 300, LengthCm: 800,
			MooringType: "mooring_line", Attributes: []string{}, GroupIDs: []string{}, PriceClass: "L8",
		})
		res2 := saveDoc(t, e, s, d.ID, 2, doc)
		got := res2.Document
		if got.Berths[0].ID != ids[0] || got.Berths[0].Code != "A-2" || got.Berths[1].Code != "A-1" || got.Berths[2].Code != "A-99" {
			t.Fatalf("swap: %s %s %s", got.Berths[0].Code, got.Berths[1].Code, got.Berths[2].Code)
		}
		if got.Berths[len(got.Berths)-1].Code != old {
			t.Fatal("new berth did not take the freed code")
		}
	})
}

func TestHarbor_KeysResolvedAndReferencesKept(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 2)
		res := saveDoc(t, e, s, d.ID, 1, req)
		pierID := res.IDMap["a-s1"]
		if !store.ValidID(pierID) || res.Document.Berths[0].StructureID != pierID {
			t.Fatalf("id map %v", res.IDMap)
		}
		// Duplicate keys, keys colliding with ids, an id with a key: validation_failed.
		doc := res.Document.Request()
		doc.Berths = append(doc.Berths, Berth{Key: "x", Code: "X1", Side: "none", Anchor: geo.Point{X: 100, Y: 100}, WidthCm: 300, LengthCm: 800, MooringType: "mooring_line"},
			Berth{Key: "x", Code: "X2", Side: "none", Anchor: geo.Point{X: 1000, Y: 100}, WidthCm: 300, LengthCm: 800, MooringType: "mooring_line"},
			Berth{Key: pierID, Code: "X3", Side: "none", Anchor: geo.Point{X: 2000, Y: 100}, WidthCm: 300, LengthCm: 800, MooringType: "mooring_line"})
		_, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 2, doc)
		var ve *auth.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("bad keys: %v", err)
		}
		fields := map[string]string{}
		for _, f := range ve.Fields {
			fields[f.Field] = f.Code
		}
		if fields["berths[5].key"] != "duplicate" || fields["berths[6].key"] != "collides_with_id" {
			t.Fatalf("fields %v", ve.Fields)
		}
		// A reference to a key that does not exist is a dangling reference (map_invalid).
		doc = res.Document.Request()
		doc.Berths[0].StructureID = "nope"
		var mi *MapInvalidError
		if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 2, doc); !errors.As(err, &mi) || mi.Report.Errors[0].Code != layout.CodeDanglingReference {
			t.Fatalf("dangling: %v", err)
		}
	})
}

func TestHarbor_UnknownElementIDRejected(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d1, d2 := newMap(t, e, s, 20000, 20000), newMap(t, e, s, 20000, 20000)
		req := d1.Request()
		addPier(t, &req, "a-", "A", 5000, 2)
		res := saveDoc(t, e, s, d1.ID, 1, req)
		// An element id of another map of the same marina.
		doc := d2.Request()
		doc.Berths = []Berth{res.Document.Berths[0]}
		doc.Berths[0].StructureID = ""
		_, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d2.ID, 1, doc)
		var ve *auth.ValidationError
		if !errors.As(err, &ve) || ve.Fields[0].Field != "berths[0].id" || ve.Fields[0].Code != "unknown_element" {
			t.Fatalf("other map id: %v", err)
		}
		// An id of a structure used as a berth id (wrong kind).
		doc.Berths[0].ID = res.IDMap["a-s1"]
		if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d1.ID, 2, doc); !errors.As(err, &ve) {
			t.Fatalf("wrong kind: %v", err)
		}
		// Another marina's owner cannot reach the map at all.
		_, otherMarina, otherOwner := e.OtherOrganization(t)
		if _, err := s.SaveMap(ctx, otherOwner, otherMarina, d1.ID, 2, res.Document.Request()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if n := countRows(t, db, "berths"); n != 4 {
			t.Fatalf("%d berths", n)
		}
	})
}

func TestHarbor_SaveWritesAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 2)
		saveDoc(t, e, s, d.ID, 1, req)
		evs := e.Events(t, event.ConfigMapChanged)
		if len(evs) != 2 || e.AuditCount(t, "map.create") != 1 || e.AuditCount(t, "map.save") != 1 {
			t.Fatalf("%d events", len(evs))
		}
		var data event.MapChangedData
		if err := json.Unmarshal(evs[1].Payload, &data); err != nil || data.MapID != d.ID || data.Version != 2 || data.Change != event.ChangeUpdated {
			t.Fatalf("event %s %v", evs[1].Payload, err)
		}
		var diff struct {
			Added map[string]int `json:"added"`
		}
		var row store.AuditEntry
		if err := db.NewSelect().Model(&row).Where("action = ?", "map.save").Scan(fixture.Ctx()); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(row.Diff, &diff); err != nil || diff.Added["berth"] != 4 || diff.Added["structure"] != 1 {
			t.Fatalf("diff %s", row.Diff)
		}
	})
}

func TestHarbor_GroupMembersFollowSave(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		g1, err := s.CreateGroup(ctx, e.Owner, e.MarinaID, GroupInput{Code: "PONT-A", Name: I18nText{It: "Pontile A"}, Kind: "structure"})
		if err != nil {
			t.Fatal(err)
		}
		g2, err := s.CreateGroup(ctx, e.Owner, e.MarinaID, GroupInput{Code: "TRANSITI", Name: I18nText{It: "Transiti"}, Kind: "transit"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateGroup(ctx, e.Owner, e.MarinaID, GroupInput{Code: "TRANSITI", Name: I18nText{It: "Dup"}}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate group code: %v", err)
		}
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 2)
		for i := range req.Berths {
			req.Berths[i].GroupIDs = []string{g1.ID}
		}
		req.Berths[0].GroupIDs = []string{g1.ID, g2.ID}
		res := saveDoc(t, e, s, d.ID, 1, req)
		gs, _ := s.ListGroups(ctx, e.Owner, e.MarinaID)
		if gs[0].MemberCounts["berth"] != 4 || gs[1].MemberCounts["berth"] != 1 {
			t.Fatalf("counts %+v", gs)
		}
		// Removing a berth and a membership drops their rows.
		doc := res.Document.Request()
		doc.Berths = doc.Berths[1:]
		doc.Berths[0].GroupIDs = []string{}
		saveDoc(t, e, s, d.ID, 2, doc)
		gs, _ = s.ListGroups(ctx, e.Owner, e.MarinaID)
		if gs[0].MemberCounts["berth"] != 2 || gs[1].MemberCounts["berth"] != 0 {
			t.Fatalf("counts after save %+v", gs)
		}
		page, err := s.ListBerths(ctx, e.Owner, e.MarinaID, BerthFilter{GroupID: g1.ID}, "", 1)
		if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
			t.Fatalf("page %+v %v", page, err)
		}
		// Unknown group ids are validation errors.
		doc.Berths[0].GroupIDs = []string{store.NewID()}
		var ve *auth.ValidationError
		if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 3, doc); !errors.As(err, &ve) {
			t.Fatalf("unknown group: %v", err)
		}
		// Group CRUD with versions.
		up, err := s.UpdateGroup(ctx, e.Owner, e.MarinaID, g2.ID, 1, GroupPatch{Name: &I18nText{It: "Transiti estivi"}})
		if err != nil || up.Version != 2 {
			t.Fatalf("update group %+v %v", up, err)
		}
		if err := s.DeleteGroup(ctx, e.Owner, e.MarinaID, g1.ID, 9); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
		if err := s.DeleteGroup(ctx, e.Owner, e.MarinaID, g1.ID, 1); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, db, "berth_group_members"); n != 0 {
			t.Fatalf("%d members left", n)
		}
		if len(e.Events(t, event.ConfigMapChanged)) < 6 {
			t.Fatal("group changes not evented")
		}
	})
}

func TestHarbor_DeleteNonEmptyMap(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 1)
		saveDoc(t, e, s, d.ID, 1, req)
		if err := s.DeleteMap(ctx, e.Owner, e.MarinaID, d.ID, 2); !errors.Is(err, ErrMapNotEmpty) {
			t.Fatalf("non-empty: %v", err)
		}
		empty := newMap(t, e, s, 1000, 1000)
		req = empty.Request()
		req.Structures = []Structure{{Key: "q", Kind: "quay", Code: "Q", Points: []geo.Point{{X: 0, Y: 10}, {X: 1000, Y: 10}}, Sides: []string{"left"}}}
		saveDoc(t, e, s, empty.ID, 1, req)
		if err := s.DeleteMap(ctx, e.Owner, e.MarinaID, empty.ID, 2); err != nil {
			t.Fatalf("map with structures only: %v", err)
		}
		if n := countRows(t, db, "harbor_structures"); n != 1 {
			t.Fatalf("%d structures left", n)
		}
		if err := s.ReorderMaps(ctx, e.Owner, e.MarinaID, []string{store.NewID()}); err == nil {
			t.Fatal("reorder with a wrong set")
		}
		other := newMap(t, e, s, 1000, 1000)
		if err := s.ReorderMaps(ctx, e.Owner, e.MarinaID, []string{other.ID, d.ID}); err != nil {
			t.Fatal(err)
		}
		list, _ := s.ListMaps(ctx, e.Owner, e.MarinaID)
		if list[0].ID != other.ID || list[1].ID != d.ID {
			t.Fatalf("order %v", list)
		}
	})
}

func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHarbor_BackgroundUploadSniffing(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 20000, 20000)
		img := pngBytes(t, 64, 32)
		for _, c := range []struct {
			name, ct string
			body     []byte
			want     error
		}{
			{"png declared as jpeg", "image/jpeg", img, ErrInvalidFile},
			{"pdf", "application/pdf", []byte("%PDF-1.4\n"), ErrUnsupportedMedia},
			{"text", "text/plain", []byte("hello"), ErrUnsupportedMedia},
			{"svg with script", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><script>alert(1)</script></svg>`), ErrInvalidFile},
			{"too large", "image/png", append(slices.Clone(img), make([]byte, 15<<20)...), ErrFileTooLarge},
			{"empty", "image/png", nil, ErrInvalidFile},
		} {
			if _, err := s.UploadBackground(ctx, e.Owner, e.MarinaID, d.ID, c.ct, bytes.NewReader(c.body)); !errors.Is(err, c.want) {
				t.Errorf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		f, err := s.UploadBackground(ctx, e.Owner, e.MarinaID, d.ID, "image/png", bytes.NewReader(img))
		if err != nil || f.WidthPx != 64 || f.HeightPx != 32 || f.ContentType != "image/png" {
			t.Fatalf("upload %+v %v", f, err)
		}
		svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"><rect width="10" height="10"/></svg>`)
		if sf, err := s.UploadBackground(ctx, e.Owner, e.MarinaID, d.ID, "image/svg+xml", bytes.NewReader(svg)); err != nil || sf.WidthPx != 800 {
			t.Fatalf("svg %+v %v", sf, err)
		}
		b, ct, err := s.Background(ctx, e.Owner, e.MarinaID, d.ID, f.FileID)
		if err != nil || ct != "image/png" || !bytes.Equal(b, img) {
			t.Fatalf("download %s %v", ct, err)
		}
		// Calibration saved with the map; the stored file fixes type and size.
		req := d.Request()
		req.Background = &Background{FileID: f.FileID, ContentType: "image/jpeg", Origin: geo.Point{X: 0, Y: 0}, ScaleMcmPerPx: 10000, OpacityPct: 60, Visible: true}
		res := saveDoc(t, e, s, d.ID, 1, req)
		if bg := res.Document.Background; bg == nil || bg.ContentType != "image/png" || bg.WidthPx != 64 || bg.OpacityPct != 60 {
			t.Fatalf("background %+v", res.Document.Background)
		}
		req.Background.FileID = store.NewID()
		var ve *auth.ValidationError
		if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 2, req); !errors.As(err, &ve) {
			t.Fatalf("unknown background: %v", err)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := s.UploadBackground(ctx, dock, e.MarinaID, d.ID, "image/png", bytes.NewReader(img)); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock upload: %v", err)
		}
		if _, _, err := s.Background(ctx, dock, e.MarinaID, d.ID, f.FileID); err != nil {
			t.Fatalf("dock download: %v", err)
		}
	})
}

func georeferenced(t testing.TB, e *fixture.Env, s *Service) *SaveResult {
	t.Helper()
	d := newMap(t, e, s, 60000, 40000)
	req := d.Request()
	addPier(t, &req, "a-", "A", 5000, 3)
	req.Georef = &Georef{Points: []ControlPoint{
		{X: 0, Y: 0, LatE7: 409_000_000, LonE7: 96_000_000},
		{X: 60000, Y: 0, LatE7: 409_000_000, LonE7: 96_070_260},
		{X: 0, Y: 40000, LatE7: 408_964_068, LonE7: 96_000_000},
	}}
	req.ConcessionOutline = []geo.Point{{X: 1000, Y: 1000}, {X: 50000, Y: 1000}, {X: 50000, Y: 30000}, {X: 1000, Y: 30000}}
	g, err := s.CreateGroup(fixture.Ctx(), e.Owner, e.MarinaID, GroupInput{Code: "PONT-A", Name: I18nText{It: "Pontile A"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range req.Berths {
		req.Berths[i].GroupIDs = []string{g.ID}
	}
	return saveDoc(t, e, s, d.ID, 1, req)
}

func TestHarbor_ExportImportRoundTrip(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		res := georeferenced(t, e, s)
		exp, err := s.ExportMap(ctx, e.Owner, e.MarinaID, res.Document.ID)
		if err != nil || exp.Format != ExportFormat || len(exp.Groups) != 1 || len(exp.Groups[0].Members) != 6 {
			t.Fatalf("export %+v %v", exp, err)
		}
		// Through JSON, as a file would.
		b, _ := json.Marshal(exp)
		var back Export
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		// Import into another marina of the organization: the group is created there.
		other := e.AddMarina(t, "marina-due")
		sum, err := s.ImportMap(ctx, e.Owner, other, back)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Counts.Berths != 6 || sum.Counts.Structures != 1 || !sum.Georeferenced || sum.Version != 2 {
			t.Fatalf("imported %+v", sum)
		}
		imported, err := s.GetMap(ctx, e.Owner, other, sum.ID)
		if err != nil {
			t.Fatal(err)
		}
		if imported.Berths[0].ID == res.Document.Berths[0].ID || imported.Berths[0].StructureID != imported.Structures[0].ID ||
			imported.Berths[0].Code != res.Document.Berths[0].Code || len(imported.Berths[0].GroupIDs) != 1 {
			t.Fatalf("imported berth %+v", imported.Berths[0])
		}
		gs, _ := s.ListGroups(ctx, e.Owner, other)
		if len(gs) != 1 || gs[0].Code != "PONT-A" || gs[0].MemberCounts["berth"] != 6 {
			t.Fatalf("groups %+v", gs)
		}
		// An invalid export creates nothing.
		back.Map.Berths[1].Anchor = back.Map.Berths[0].Anchor
		maps := countRows(t, db, "maps")
		if _, err := s.ImportMap(ctx, e.Owner, other, back); !errors.Is(err, ErrMapInvalid) {
			t.Fatalf("invalid import: %v", err)
		}
		if countRows(t, db, "maps") != maps {
			t.Fatal("invalid import left a map")
		}
		if len(e.Events(t, event.ConfigMapChanged)) == 0 || e.AuditCount(t, "map.import") != 1 {
			t.Fatal("import not audited")
		}
	})
}

func TestHarbor_GeoJSONExport(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		res := georeferenced(t, e, s)
		b, err := s.ExportGeoJSON(ctx, e.Owner, e.MarinaID, res.Document.ID)
		if err != nil {
			t.Fatal(err)
		}
		var fc struct {
			Type     string `json:"type"`
			Features []struct {
				Geometry struct {
					Type        string          `json:"type"`
					Coordinates json.RawMessage `json:"coordinates"`
				} `json:"geometry"`
				Properties map[string]any `json:"properties"`
			} `json:"features"`
		}
		if err := json.Unmarshal(b, &fc); err != nil || fc.Type != "FeatureCollection" || len(fc.Features) != 1+1+6 {
			t.Fatalf("geojson %d features %v", len(fc.Features), err)
		}
		if !regexp.MustCompile(`"coordinates":\[\[\[9\.\d{7},40\.\d{7}\],`).Match(b) {
			t.Fatalf("coordinates are not exact decimals: %.300s", b)
		}
		// Concession import: the exported concession converts back to the same outline (±1 cm).
		conc := fc.Features[0]
		raw, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": json.RawMessage(conc.Geometry.Coordinates)})
		out, err := s.ConvertConcession(ctx, e.Owner, e.MarinaID, res.Document.ID, raw, nil)
		if err != nil || len(out) != 4 {
			t.Fatalf("concession %v %v", out, err)
		}
		for i, p := range out {
			w := res.Document.ConcessionOutline[i]
			if d := p.Sub(w); d.X < -1 || d.X > 1 || d.Y < -1 || d.Y > 1 {
				t.Fatalf("point %d: %v, want %v", i, p, w)
			}
		}
		plain := newMap(t, e, s, 1000, 1000)
		if _, err := s.ExportGeoJSON(ctx, e.Owner, e.MarinaID, plain.ID); !errors.Is(err, ErrNotGeoreferenced) {
			t.Fatalf("not georeferenced: %v", err)
		}
		if _, err := s.ConvertConcession(ctx, e.Owner, e.MarinaID, plain.ID, raw, nil); !errors.Is(err, ErrNotGeoreferenced) {
			t.Fatalf("concession without georef: %v", err)
		}
		if _, err := s.ConvertConcession(ctx, e.Owner, e.MarinaID, res.Document.ID, json.RawMessage(`{"type":"Point","coordinates":[1,2]}`), nil); err == nil {
			t.Fatal("point accepted as a concession")
		}
	})
}

const berthsCSV = "codice;pontile;lunghezza;larghezza;fondale\n" +
	"F-1;F;12,00;4,00;3\n" +
	"F-2;F;12;4;\n" +
	"F-3;X;12;4;3\n" + // unknown pier
	"F-4;F;abc;4;3\n" + // bad length
	"F-1;F;12;4;3\n" + // duplicate in file
	"A-1;F;12;4;3\n" + // code on the map
	"F-5;F;10;3,5;2,5\n"

func importInput(dry bool) BerthImport {
	return BerthImport{
		CSV: berthsCSV, Delimiter: "semicolon", HasHeader: true, Units: "m", Start: "after_existing", SidePolicy: "alternate",
		DryRun: dry, Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3, Depth: intp(4)},
	}
}

func intp(v int) *int { return &v }

func pierFMap(t testing.TB, e *fixture.Env, s *Service) *SaveResult {
	t.Helper()
	d := newMap(t, e, s, 40000, 40000)
	req := d.Request()
	addPier(t, &req, "a-", "A", 5000, 2)
	req.Structures = append(req.Structures, Structure{Key: "f", Kind: "pier", Code: "F", Points: []geo.Point{{X: 20000, Y: 1000}, {X: 20000, Y: 9000}}, WidthCm: 300})
	return saveDoc(t, e, s, d.ID, 1, req)
}

func TestHarbor_BerthImport_DryRunAndApply(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		res := pierFMap(t, e, s)
		mapID := res.Document.ID
		rep, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, importInput(true))
		if err != nil {
			t.Fatal(err)
		}
		if rep.RowsTotal != 7 || rep.Valid != 3 || len(rep.Rejected) != 2 || len(rep.Duplicates) != 2 || rep.Document != nil {
			t.Fatalf("dry run %+v", rep)
		}
		if rep.Planned[0].Side != "left" || rep.Planned[1].Side != "right" || rep.Planned[2].Side != "left" || rep.Planned[0].Row != 2 {
			t.Fatalf("planned %+v", rep.Planned)
		}
		if n := countRows(t, db, "berths"); n != 4 {
			t.Fatalf("dry run wrote berths: %d", n)
		}
		if _, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 1, importInput(false)); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale apply: %v", err)
		}
		rep, err = s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 2, importInput(false))
		if err != nil {
			t.Fatal(err)
		}
		doc := rep.Document.Document
		if doc.Version != 3 || len(doc.Berths) != 7 || e.AuditCount(t, "map.berth_import") != 1 {
			t.Fatalf("applied version %d, %d berths", doc.Version, len(doc.Berths))
		}
		var f5 Berth
		for _, b := range doc.Berths {
			if b.Code == "F-5" {
				f5 = b
			}
		}
		if f5.WidthCm != 350 || f5.LengthCm != 1000 || f5.DepthCm != 250 || f5.PriceClass != "L10" || f5.Anchor.X != 20150 {
			t.Fatalf("F-5 %+v", f5)
		}
		// Again after the first import: rows already there are duplicates on the map; new ones go after them.
		rep, err = s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 3, BerthImport{
			CSV: "F-6,F,12,4\n", Units: "m", Start: "after_existing",
			SidePolicy: "left", Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range rep.Document.Document.Berths {
			if b.Code == "F-6" && b.Anchor.Y != 1000+400+350+200 {
				t.Fatalf("F-6 placed at %+v", b.Anchor)
			}
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func TestHarbor_BerthImport_RejectsBadRows(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		res := pierFMap(t, e, s)
		mapID := res.Document.ID
		in := importInput(true)
		in.CSV = "a,\"b\nc"
		if _, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, in); !errors.Is(err, ErrCSVInvalid) {
			t.Fatalf("malformed csv: %v", err)
		}
		in = importInput(true)
		in.CSV = strings.Repeat("F-1;F;1;1\n", MaxImportRows+2)
		if _, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, in); !errors.Is(err, ErrCSVInvalid) {
			t.Fatalf("too many rows: %v", err)
		}
		in = importInput(true)
		in.Units = "ft"
		var ve *auth.ValidationError
		if _, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, in); !errors.As(err, &ve) {
			t.Fatalf("bad units: %v", err)
		}
		// A row longer than the pier is rejected, the others stay valid.
		in = BerthImport{
			CSV: "Z-1,F,12,4\nZ-2,F,12,9000\n", Units: "m", Start: "root", SidePolicy: "right",
			DryRun: true, Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3},
		}
		rep, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, in)
		if err != nil || rep.Valid != 1 || len(rep.Rejected) != 1 || rep.Rejected[0].Errors[0].Field != "width" {
			t.Fatalf("report %+v %v", rep, err)
		}
		in.CSV = "Z-1,F,12,4\nZ-2,F,12,79\n"
		rep, err = s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 0, in)
		if err != nil || rep.Valid != 1 || rep.Rejected[0].Errors[0].Code != "does_not_fit" {
			t.Fatalf("does not fit %+v %v", rep, err)
		}
		// Placement from the root over existing berths is refused at apply by the save validation.
		in = BerthImport{CSV: "Z-1,A,12,4\n", Units: "m", Start: "root", SidePolicy: "left", Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3}}
		if _, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, mapID, 2, in); !errors.Is(err, ErrMapInvalid) {
			t.Fatalf("overlapping apply: %v", err)
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func TestHarbor_Settings(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		st, err := s.GetSettings(ctx, e.Owner, e.MarinaID)
		if err != nil || st.Version != 1 || st.LOAToleranceCm != 50 || st.PriceClassRule.Fallback != "L25" {
			t.Fatalf("defaults %+v %v", st, err)
		}
		tol := int64(100)
		up, err := s.UpdateSettings(ctx, e.Owner, e.MarinaID, 1, SettingsPatch{LOAToleranceCm: &tol})
		if err != nil || up.Version != 2 || up.LOAToleranceCm != 100 {
			t.Fatalf("update %+v %v", up, err)
		}
		bad := layout.PriceClassRule{Basis: "width", Bands: []layout.PriceBand{{MaxCm: 400, Class: "A"}, {MaxCm: 300, Class: "B"}}, Fallback: "C"}
		var ve *auth.ValidationError
		if _, err := s.UpdateSettings(ctx, e.Owner, e.MarinaID, 2, SettingsPatch{PriceClassRule: &bad}); !errors.As(err, &ve) {
			t.Fatalf("bad rule: %v", err)
		}
		if _, err := s.UpdateSettings(ctx, e.Owner, e.MarinaID, 1, SettingsPatch{LOAToleranceCm: &tol}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		// The tolerance applies to the save validation.
		d := newMap(t, e, s, 20000, 20000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 1)
		req.Berths[0].MaxLOACm = 1290
		saveDoc(t, e, s, d.ID, 1, req)
		// The generator uses the marina's rule.
		rule := layout.PriceClassRule{Basis: "width", Bands: []layout.PriceBand{{MaxCm: 1000, Class: "ALL"}}, Fallback: "BIG"}
		if _, err := s.UpdateSettings(ctx, e.Owner, e.MarinaID, 2, SettingsPatch{PriceClassRule: &rule}); err != nil {
			t.Fatal(err)
		}
		gen, err := s.Generate(ctx, e.Owner, e.MarinaID, d.ID, GenerateInput{Generator: GeneratorSpec{
			Type: "quay_berths", KeyPrefix: "g1-",
			QuayID: &req.Structures[0].Key,
		}})
		if err == nil || !errors.As(err, &ve) {
			t.Fatalf("generator on a key of an unsaved document without the document: %v", err)
		}
		_ = gen
	})
}
