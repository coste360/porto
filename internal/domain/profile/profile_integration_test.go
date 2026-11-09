//go:build integration

package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	svc   *Service
	files *filestore.Store
	scope store.MarinaScope
	mapID string
}

func newEnv(t testing.TB, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	prices := pricelist.NewService(pricelist.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	svc := NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Files: files, Prices: prices, BaseURL: "https://porto.example/"})
	scope, err := store.ResolveMarinaScope(context.Background(), db.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return &env{Env: e, svc: svc, files: files, scope: scope}
}

func ptr[T any](v T) *T { return &v }

// complete is a publishable input.
func complete() Input {
	return Input{
		Locales: []string{"it"}, EntranceLatE7: ptr(int64(437101234)), EntranceLonE7: ptr(int64(104012345)),
		Address: Address{Street: "Via del Porto 1", City: "Pisa", Country: "IT"}, Phone: "+39 050 000000",
		Email: "info@porto.example", Website: "https://porto.example", VHFChannels: []int{9, 16},
		OfficeHours: []Hours{{Weekday: 1, From: "08:00", To: "19:00"}}, Description: Text{"it": "Un porto", "en": "A marina"},
		DepthMinCm: ptr(int64(250)), DepthMaxCm: ptr(int64(600)), Seabed: "sand", Shelter: []string{"S", "N"},
		Services: Services{Water: true, WiFi: true, PowerAmps: []int{32, 16}}, ShowMap: true,
	}
}

func (e *env) addMap(t testing.TB) string {
	t.Helper()
	if e.mapID != "" {
		return e.mapID
	}
	now := store.UTC(e.Clock.Now())
	m := &store.Map{
		ID: store.NewID(), Kind: "water", Name: store.MustJSON(map[string]string{"it": "Acqua"}), Icon: "map", WidthCm: 100000,
		HeightCm: 80000, GridCm: 50, WaterPolygons: store.MustJSON([]any{}), LandPolygons: store.MustJSON([]any{}), Version: 3,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertMap(context.Background(), e.DB.DB, e.scope, m); err != nil {
		t.Fatal(err)
	}
	e.mapID = m.ID
	return m.ID
}

func (e *env) addBerth(t testing.TB, code, class string, x int64) string {
	t.Helper()
	mapID := e.addMap(t)
	now := store.UTC(e.Clock.Now())
	b := &store.Berth{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: mapID, Code: code, Side: "none", AnchorXCm: x, AnchorYCm: 1000,
		WidthCm: 400, LengthCm: 1200, MaxLOACm: 1180, MaxBeamCm: 380, MooringType: "mooring_line", PriceClass: class,
		Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(b).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b.ID
}

func (e *env) addCategory(t testing.TB, code, it, en string) {
	t.Helper()
	cat := catalog.NewService(catalog.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
	name := catalog.I18nText{It: it}
	if en != "" {
		name.En = &en
	}
	if _, err := cat.CreateCategory(fixture.Ctx(), e.Owner, e.MarinaID, catalog.CategoryInput{Code: code, Name: name, AppliesTo: []string{"berth"}}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) today() calendar.Date {
	loc, _ := calendar.LoadLocation("Europe/Rome")
	return calendar.In(e.Clock.Now(), loc)
}

// block occupies a berth tonight with a block allocation.
func (e *env) block(t testing.TB, berthID string, expires time.Time) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	night := e.today()
	kind := "block"
	a := &store.Allocation{
		ID: store.NewID(), MarinaID: e.MarinaID, ResourceType: "berth", ResourceID: berthID, MapID: e.addMap(t),
		FirstNight: night, LastNight: night, Kind: kind, Status: "active", BlockReason: "works", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if !expires.IsZero() {
		a.Kind, a.BlockReason, a.ExpiresAt = "hold", "", expires
		kind = "hold"
		vid := e.vessel(t)
		a.VesselID = vid
	}
	if _, err := e.DB.NewInsert().Model(a).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	o := &store.BerthOccupancy{MarinaID: e.MarinaID, BerthID: berthID, Night: night, AllocationID: a.ID, Kind: kind, ExpiresAt: expires}
	if _, err := e.DB.NewInsert().Model(o).Exec(ctx); err != nil {
		t.Fatal(err)
	}
}

func (e *env) vessel(t testing.TB) string {
	t.Helper()
	now := store.UTC(e.Clock.Now())
	v := &store.Vessel{
		ID: store.NewID(), MarinaID: e.MarinaID, Name: "Segreta", HullType: "monohull_motor", LOACm: 900, BeamCm: 300,
		DimensionsSource: "declared", Engines: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(v).Exec(context.Background()); err != nil {
		t.Fatalf("insert vessel: %v", err)
	}
	return v.ID
}

func (e *env) sensor(t testing.TB, berthID, physical string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	d := &store.SensorDevice{
		ID: store.NewID(), MarinaID: e.MarinaID, ExternalID: "dev-" + berthID[len(berthID)-6:], Code: "S" + berthID[len(berthID)-6:],
		Kind: "ultrasonic", ResourceType: "berth", ResourceID: berthID, MapID: e.addMap(t), Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(d).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSensorDeviceState(ctx, e.DB.DB, &store.SensorDeviceState{DeviceID: d.ID, MarinaID: e.MarinaID, Physical: physical, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func isField(err error, field string) bool {
	var ve *auth.ValidationError
	if !errors.As(err, &ve) {
		return false
	}
	for _, f := range ve.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}

func TestProfile_DefaultsAndCRUD(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		v, err := e.svc.Get(ctx, e.Owner, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Version != 0 || v.Published || v.PublicURL != "https://porto.example/p/marina-test" {
			t.Fatalf("defaults %+v", v)
		}
		if strings.Join(v.Missing, ",") != "position,contact" {
			t.Fatalf("missing %v", v.Missing)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 0); err == nil {
			t.Fatal("publish without position must fail")
		} else if ie, ok := errors.AsType[*IncompleteError](err); !ok || len(ie.Missing) != 2 {
			t.Fatalf("publish: %v", err)
		}
		v, err = e.svc.Put(ctx, e.Owner, e.MarinaID, 0, complete())
		if err != nil {
			t.Fatal(err)
		}
		if v.Version != 1 || len(v.Missing) != 0 {
			t.Fatalf("after put %+v", v)
		}
		var shelter []string
		_ = json.Unmarshal(v.Shelter, &shelter)
		if strings.Join(shelter, ",") != "N,S" {
			t.Fatalf("shelter not canonical: %v", shelter)
		}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, complete()); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale put: %v", err)
		}
		if _, err := e.svc.PublicProfile(ctx, "marina-test", "it"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("unpublished public: %v", err)
		}
		v, err = e.svc.Publish(ctx, e.Owner, e.MarinaID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Published || v.PublishedAt == nil || v.Version != 2 {
			t.Fatalf("published %+v", v)
		}
		pub, err := e.svc.PublicProfile(ctx, "marina-test", "en")
		if err != nil {
			t.Fatal(err)
		}
		if pub.Lang != "it" || pub.Data.Description != "Un porto" || pub.Data.Latitude != "43.7101234" || pub.Data.BookingURL != "" {
			t.Fatalf("public %+v", pub.Data) // en not offered by the marina: it
		}
		if !e.svc.Published(ctx, "marina-test") || e.svc.Published(ctx, "unknown-slug") {
			t.Fatal("Published")
		}
		in := complete()
		in.EntranceLatE7, in.EntranceLonE7 = nil, nil
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 2, in); err == nil {
			t.Fatal("a published profile must stay publishable")
		}
		if _, err := e.svc.Unpublish(ctx, e.Owner, e.MarinaID, 2); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublicProfile(ctx, "marina-test", "it"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("after unpublish: %v", err)
		}
		if _, err := e.svc.PageHead(ctx, "marina-test", "it"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("head after unpublish: %v", err)
		}
		if n := e.AuditCount(t, actPublish); n != 1 {
			t.Fatalf("publish audit %d", n)
		}
		if n := len(e.Events(t, "profile.changed")); n != 3 {
			t.Fatalf("profile.changed events %d", n)
		}
		for _, ev := range e.Events(t, "profile.changed") {
			if strings.Contains(string(ev.Payload), "@") || strings.Contains(string(ev.Payload), "050") {
				t.Fatalf("event carries contacts: %s", ev.Payload)
			}
		}
	})
}

func TestProfile_Validation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cases := []struct {
			field string
			mut   func(*Input)
		}{
			{"entrance_lat_e7", func(in *Input) { in.EntranceLonE7 = nil }},
			{"entrance_lat_e7", func(in *Input) { in.EntranceLatE7 = ptr(int64(900_000_001)) }},
			{"website", func(in *Input) { in.Website = "http://porto.example" }},
			{"email", func(in *Input) { in.Email = "nope" }},
			{"vhf_channels", func(in *Input) { in.VHFChannels = []int{89} }},
			{"office_hours[0].to", func(in *Input) { in.OfficeHours = []Hours{{Weekday: 1, From: "10:00", To: "09:00"}} }},
			{"office_hours[0].weekday", func(in *Input) { in.OfficeHours = []Hours{{Weekday: 8, From: "08:00", To: "09:00"}} }},
			{"depth_min_cm", func(in *Input) { in.DepthMinCm = ptr(int64(700)) }},
			{"seabed", func(in *Input) { in.Seabed = "lava" }},
			{"shelter", func(in *Input) { in.Shelter = []string{"NNE"} }},
			{"services.power_amps", func(in *Input) { in.Services.PowerAmps = []int{20} }},
			{"description.it", func(in *Input) { in.Description = Text{"it": strings.Repeat("a", 1001)} }},
			{"description", func(in *Input) { in.Description = Text{"fr": "x"} }},
			{"locales", func(in *Input) { in.Locales = []string{"en"} }}, // the marina has only it
			{"occupancy_categories", func(in *Input) { in.ShowOccupancy = true }},
			{"occupancy_categories", func(in *Input) { in.ShowOccupancy, in.OccupancyCategories = true, []string{"ZZ"} }},
			{"photos[0].file_id", func(in *Input) { in.Photos = []Photo{{FileID: store.NewID()}} }},
			{"map_id", func(in *Input) { in.MapID = store.NewID() }},
			{"poi[0].kind", func(in *Input) { in.POI = []POI{{Kind: "berth"}} }},
			{"max_loa_cm", func(in *Input) { in.MaxLoaCm = ptr(int64(0)) }},
		}
		for _, tc := range cases {
			in := complete()
			tc.mut(&in)
			if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); !isField(err, tc.field) {
				t.Errorf("%s: %v", tc.field, err)
			}
		}
	})
}

// jpegWithEXIF returns a JPEG carrying an APP1 Exif segment with a GPS marker string.
func jpegWithEXIF(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude=43.71;SECRET-GPS-MARKER")...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	raw := buf.Bytes()
	out := append([]byte{}, raw[:2]...) // SOI
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, raw[2:]...)
}

func TestProfile_PhotoReencodeStripsEXIF(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		src := jpegWithEXIF(t, 64, 48)
		if !bytes.Contains(src, []byte("SECRET-GPS-MARKER")) {
			t.Fatal("fixture has no marker")
		}
		f, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/jpeg", bytes.NewReader(src))
		if err != nil {
			t.Fatal(err)
		}
		if f.Width != 64 || f.Height != 48 || f.ContentType != "image/jpeg" {
			t.Fatalf("photo %+v", f)
		}
		img, err := e.svc.GetPhoto(ctx, e.Owner, e.MarinaID, f.FileID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(img.Body, []byte("Exif")) || bytes.Contains(img.Body, []byte("SECRET-GPS-MARKER")) {
			t.Fatal("re-encoded photo still carries EXIF")
		}
		if _, _, err := image.Decode(bytes.NewReader(img.Body)); err != nil {
			t.Fatalf("output does not decode: %v", err)
		}
		// A wide PNG is clamped to 2,400 px.
		big := image.NewNRGBA(image.Rect(0, 0, 3000, 1000))
		var pb bytes.Buffer
		if err := png.Encode(&pb, big); err != nil {
			t.Fatal(err)
		}
		f2, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/png", &pb)
		if err != nil {
			t.Fatal(err)
		}
		if f2.Width != 2400 || f2.Height != 800 {
			t.Fatalf("clamped %dx%d", f2.Width, f2.Height)
		}
		if _, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/png", bytes.NewReader(src)); !errors.Is(err, ErrPhotoInvalid) {
			t.Fatalf("jpeg declared as png: %v", err)
		}
		if _, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/gif", bytes.NewReader(src)); !errors.Is(err, ErrUnsupportedMedia) {
			t.Fatalf("gif: %v", err)
		}
		if _, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/jpeg", strings.NewReader("not an image")); !errors.Is(err, ErrPhotoInvalid) {
			t.Fatalf("garbage: %v", err)
		}
	})
}

func TestProfile_PhotosReferencedAndServed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		f, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/jpeg", bytes.NewReader(jpegWithEXIF(t, 16, 16)))
		if err != nil {
			t.Fatal(err)
		}
		other, err := e.svc.UploadPhoto(ctx, e.Owner, e.MarinaID, "image/jpeg", bytes.NewReader(jpegWithEXIF(t, 8, 8)))
		if err != nil {
			t.Fatal(err)
		}
		// A stored file not uploaded through the profile can never be referenced, read or deleted.
		now := store.UTC(e.Clock.Now())
		doc := &store.StoredFile{ID: store.NewID(), MarinaID: e.MarinaID, Purpose: "document", ContentType: "image/jpeg", SizeBytes: 1, SHA256: "x", StorageKey: e.MarinaID + "/x.bin", CreatedAt: now, UpdatedAt: now}
		doc.StorageKey = filestore.StorageKey(e.MarinaID, doc.ID)
		if err := store.InsertStoredFile(ctx, e.DB.DB, doc); err != nil {
			t.Fatal(err)
		}
		in := complete()
		in.Photos = []Photo{{FileID: doc.ID}}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); !isField(err, "photos[0].file_id") {
			t.Fatalf("foreign document referenced: %v", err)
		}
		if _, err := e.svc.GetPhoto(ctx, e.Owner, e.MarinaID, doc.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign document read: %v", err)
		}
		if _, err := e.svc.DeletePhoto(ctx, e.Owner, e.MarinaID, doc.ID, 0); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign document delete: %v", err)
		}
		in.Photos = []Photo{{FileID: f.FileID, Alt: Text{"it": "Banchina"}}, {FileID: f.FileID}}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); !isField(err, "photos[1].file_id") {
			t.Fatalf("duplicate photo: %v", err)
		}
		in.Photos = in.Photos[:1]
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublicPhoto(ctx, "marina-test", f.FileID); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("photo of an unpublished profile: %v", err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		img, err := e.svc.PublicPhoto(ctx, "marina-test", f.FileID)
		if err != nil || img.ContentType != "image/jpeg" || img.ETag == "" {
			t.Fatalf("public photo: %v %+v", err, img)
		}
		if _, err := e.svc.PublicPhoto(ctx, "marina-test", other.FileID); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("unreferenced photo served: %v", err)
		}
		pub, err := e.svc.PublicProfile(ctx, "marina-test", "it")
		if err != nil {
			t.Fatal(err)
		}
		if len(pub.Data.Photos) != 1 || pub.Data.Photos[0].URL != "/api/public/v1/marina-test/profile/photos/"+f.FileID || pub.Data.Photos[0].Alt != "Banchina" {
			t.Fatalf("public photos %+v", pub.Data.Photos)
		}
		if _, err := e.svc.DeletePhoto(ctx, e.Owner, e.MarinaID, f.FileID, 1); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
		v, err := e.svc.DeletePhoto(ctx, e.Owner, e.MarinaID, f.FileID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if v.Version != 3 || string(v.Photos) != "[]" {
			t.Fatalf("after delete %d %s", v.Version, v.Photos)
		}
		if _, err := e.svc.PublicPhoto(ctx, "marina-test", f.FileID); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("deleted photo served: %v", err)
		}
		// Cross-marina: another organization's owner sees 404.
		_, _, otherOwner := e.OtherOrganization(t)
		if _, err := e.svc.GetPhoto(ctx, otherOwner, e.MarinaID, other.FileID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-marina photo: %v", err)
		}
		if _, err := e.svc.Get(ctx, otherOwner, e.MarinaID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross-marina get: %v", err)
		}
	})
}

func TestPublicOccupancy_AggregateOnly(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.addCategory(t, "L10", "Fino a 10 m", "Up to 10 m")
		e.addCategory(t, "L12", "Fino a 12 m", "")
		e.addCategory(t, "L14", "Fino a 14 m", "")
		b1 := e.addBerth(t, "A-1", "L10", 1000)
		e.addBerth(t, "A-2", "L10", 2000)
		b3 := e.addBerth(t, "A-3", "L10", 3000)
		b4 := e.addBerth(t, "B-1", "L12", 4000)
		e.addBerth(t, "B-2", "L12", 5000)
		e.addBerth(t, "C-1", "L14", 6000) // category not published
		e.block(t, b1, time.Time{})
		e.sensor(t, b3, "occupied")
		e.sensor(t, b4, "free")
		in := complete()
		in.ShowOccupancy, in.OccupancyCategories = true, []string{"L10", "L12"}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.svc.PublicOccupancy(ctx, "marina-test"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("unpublished occupancy: %v", err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		o, etag, err := e.svc.PublicOccupancy(ctx, "marina-test")
		if err != nil {
			t.Fatal(err)
		}
		if len(o.Categories) != 2 || o.Categories[0].FreeBerths != 1 || o.Categories[0].TotalBerths != 3 ||
			o.Categories[1].FreeBerths != 2 || o.Categories[1].TotalBerths != 2 || o.Night != e.today().String() || etag == "" {
			t.Fatalf("occupancy %+v", o)
		}
		if o.Categories[0].Name.It != "Fino a 10 m" || o.Categories[0].Name.En == nil || o.AsOf.Minute()%5 != 0 || o.AsOf.Second() != 0 {
			t.Fatalf("names/as_of %+v", o)
		}
		raw, _ := json.Marshal(o)
		var keys map[string]any
		_ = json.Unmarshal(raw, &keys)
		for k := range keys {
			if k != "as_of" && k != "night" && k != "categories" {
				t.Fatalf("unexpected key %s", k)
			}
		}
		for _, c := range keys["categories"].([]any) {
			for k := range c.(map[string]any) {
				if k != "code" && k != "name" && k != "free_berths" && k != "total_berths" {
					t.Fatalf("unexpected category key %s", k)
				}
			}
		}
		for _, leak := range []string{"A-1", "A-3", "B-1", b1, b3, b4} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("aggregate leaks %s: %s", leak, raw)
			}
		}
		// Cached for 60 s: a new block is not visible until the cache expires.
		e.block(t, b4, time.Time{})
		o2, _, _ := e.svc.PublicOccupancy(ctx, "marina-test")
		if o2.Categories[1].FreeBerths != 2 {
			t.Fatal("cache not used")
		}
		e.Clock.Advance(61 * time.Second)
		o3, _, _ := e.svc.PublicOccupancy(ctx, "marina-test")
		if o3.Categories[1].FreeBerths != 1 {
			t.Fatalf("cache not refreshed: %+v", o3.Categories)
		}
		// A marina-wide closure makes every berth not free.
		if err := store.InsertClosure(ctx, e.DB.DB, e.scope, &store.Closure{
			ID: store.NewID(), MarinaID: e.MarinaID, ScopeType: store.ClosureMarina, FromDate: e.today(), ToDate: e.today(),
			Reason: "storm", Version: 1, CreatedAt: store.UTC(e.Clock.Now()), UpdatedAt: store.UTC(e.Clock.Now()),
		}); err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(61 * time.Second)
		o4, _, _ := e.svc.PublicOccupancy(ctx, "marina-test")
		if o4.Categories[0].FreeBerths != 0 || o4.Categories[1].FreeBerths != 0 {
			t.Fatalf("closure ignored: %+v", o4.Categories)
		}
	})
}

func TestSimpleMap_PublicNoCodes(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.addBerth(t, "SECRET-CODE-1", "L10", 1000)
		in := complete()
		in.POI = []POI{{Kind: "fuel", XCm: 1234, YCm: 5678}}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		m, etag, err := e.svc.PublicMap(ctx, "marina-test")
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Berths) != 1 || m.Berths[0].Category != "L10" || len(m.Berths[0].Points) != 4 || m.MaxXCm != 100000 || m.Version != 3 || etag != `"m3-2"` {
			t.Fatalf("map %+v %s", m, etag)
		}
		if len(m.POI) != 1 || m.POI[0].XCm != 1230 || m.POI[0].YCm != 5680 {
			t.Fatalf("poi %+v", m.POI)
		}
		raw, _ := json.Marshal(m)
		if strings.Contains(string(raw), "SECRET-CODE-1") {
			t.Fatal("map leaks a berth code")
		}
		in.ShowMap = false
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 2, in); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.svc.PublicMap(ctx, "marina-test"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("map off: %v", err)
		}
		if _, _, err := e.svc.PublicPrices(ctx, "marina-test", "it"); !errors.Is(err, ErrNotPublished) {
			t.Fatalf("prices off: %v", err)
		}
	})
}

func TestProfile_PageHead(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		in := complete()
		in.Description = Text{"it": `Porto "sicuro" </script><script>alert(1)</script>`}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		h, err := e.svc.PageHead(ctx, "marina-test", "it")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(h.HTML, `<script type="application/ld+json">`) != 1 || strings.Count(strings.ToLower(h.HTML), "</script>") != 1 {
			t.Fatalf("script break-out:\n%s", h.HTML)
		}
		for _, want := range []string{`<link rel="canonical" href="https://porto.example/p/marina-test">`, `hreflang="it"`, `hreflang="x-default"`, `<title>Marina Test</title>`, `og:title`} {
			if !strings.Contains(h.HTML, want) {
				t.Fatalf("head misses %s:\n%s", want, h.HTML)
			}
		}
		h2, _ := e.svc.PageHead(ctx, "marina-test", "it")
		if h2 != h {
			t.Fatal("head not cached")
		}
	})
}

func TestConcurrency_ProfilePublish(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, complete()); err != nil {
			t.Fatal(err)
		}
		const n = 100
		var wins, conflicts atomic.Int32
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					_, err = e.svc.Publish(ctx, e.Owner, e.MarinaID, 1)
				} else {
					in := complete()
					in.Phone = "+39 050 00000" + string(rune('0'+i%10))
					_, err = e.svc.Put(ctx, e.Owner, e.MarinaID, 1, in)
				}
				switch {
				case err == nil:
					wins.Add(1)
				case errors.Is(err, ErrVersionMismatch):
					conflicts.Add(1)
				default:
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 || conflicts.Load() != n-1 {
			t.Fatalf("wins %d conflicts %d", wins.Load(), conflicts.Load())
		}
		v, err := e.svc.Get(ctx, e.Owner, e.MarinaID)
		if err != nil || v.Version != 2 {
			t.Fatalf("final version %v %v", v, err)
		}
	})
}

// TestPublicProfile_ETagFollowsLiveFigures: the ETag of getPublicProfile changes when a live figure of the body
// changes (a new active berth) with the profile and marina rows untouched, so If-None-Match never returns stale
// counts.
func TestPublicProfile_ETagFollowsLiveFigures(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.addBerth(t, "A-1", "L10", 1000)
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, complete()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		p1, err := e.svc.PublicProfile(ctx, "marina-test", "it")
		if err != nil {
			t.Fatal(err)
		}
		again, _ := e.svc.PublicProfile(ctx, "marina-test", "it")
		if again.ETag != p1.ETag {
			t.Fatalf("unstable etag %s %s", p1.ETag, again.ETag)
		}
		e.addBerth(t, "A-2", "L10", 2000)
		p2, err := e.svc.PublicProfile(ctx, "marina-test", "it")
		if err != nil {
			t.Fatal(err)
		}
		if *p2.Data.BerthsTotal != *p1.Data.BerthsTotal+1 || p2.ETag == p1.ETag {
			t.Fatalf("etag %s -> %s with berths %d -> %d", p1.ETag, p2.ETag, *p1.Data.BerthsTotal, *p2.Data.BerthsTotal)
		}
	})
}

// pedestalSensor inserts a pedestal serving berths (socket i+1 for berth i when sockets, none otherwise) and an
// active pedestal device on socket (empty = whole pedestal) with the given physical state.
func (e *env) pedestalSensor(t testing.TB, code string, berths []string, sockets bool, socket, physical string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(e.Clock.Now())
	ped := &store.Pedestal{
		ID: store.NewID(), MarinaID: e.MarinaID, MapID: e.addMap(t), Code: code, XCm: 1200, YCm: 1500,
		Sockets: store.JSON(`[{"code":"S1","kind":"power"},{"code":"S2","kind":"power"}]`), Meters: store.JSON(`[]`),
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(ped).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for i, b := range berths {
		q := e.DB.NewUpdate().Table("berths").Set("pedestal_id = ?", ped.ID).Where("id = ?", b)
		if sockets {
			q = q.Set("pedestal_socket = ?", []string{"S1", "S2"}[i])
		}
		if _, err := q.Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	d := &store.SensorDevice{
		ID: store.NewID(), MarinaID: e.MarinaID, ExternalID: "ped-" + code, Code: "P" + code, Kind: "other",
		ResourceType: "pedestal", ResourceID: ped.ID, PedestalSocket: socket, MapID: e.addMap(t), Active: true, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(d).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSensorDeviceState(ctx, e.DB.DB, &store.SensorDeviceState{DeviceID: d.ID, MarinaID: e.MarinaID, Physical: physical, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

// TestPublicOccupancy_PedestalSensor: a pedestal device that maps to one berth (its socket's) counts like a berth
// sensor in the public aggregate (m12-plan §8.1, §11); an informational one (a pedestal serving two berths, no
// socket) never counts.
func TestPublicOccupancy_PedestalSensor(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.addCategory(t, "L10", "Fino a 10 m", "")
		b1 := e.addBerth(t, "A-1", "L10", 1000)
		b2 := e.addBerth(t, "A-2", "L10", 2000)
		b3 := e.addBerth(t, "A-3", "L10", 3000)
		b4 := e.addBerth(t, "A-4", "L10", 4000)
		e.pedestalSensor(t, "1", []string{b1, b2}, true, "S1", "occupied") // maps to b1
		e.pedestalSensor(t, "2", []string{b3, b4}, false, "", "occupied")  // informational
		in := complete()
		in.ShowOccupancy, in.OccupancyCategories = true, []string{"L10"}
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		o, _, err := e.svc.PublicOccupancy(ctx, "marina-test")
		if err != nil {
			t.Fatal(err)
		}
		if len(o.Categories) != 1 || o.Categories[0].TotalBerths != 4 || o.Categories[0].FreeBerths != 3 {
			t.Fatalf("occupancy %+v", o.Categories)
		}
	})
}

// TestPublicProfile_NoPII: a seeded customer's name, email and phone, a vessel name and the berth codes are absent
// from every public response of the profile (profile, map, occupancy, prices when published) and from the page
// head (INV-103, m12-plan §14).
func TestPublicProfile_NoPII(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		now := store.UTC(e.Clock.Now())
		cust := &store.Customer{
			ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", FirstName: "Gianfranco", LastName: "Riservatissimo",
			BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[{"label":"","value":"gianfranco@privato.example"}]`),
			Phones: store.JSON(`[{"label":"","value":"+39 333 9876543"}]`), Tags: store.Strings{}, PaymentTerms: store.JSON(`{}`),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := db.NewInsert().Model(cust).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.addCategory(t, "L10", "Fino a 10 m", "")
		b1 := e.addBerth(t, "PONTE-SEGRETO-1", "L10", 1000)
		e.addBerth(t, "PONTE-SEGRETO-2", "L10", 2000)
		e.addBerth(t, "PONTE-SEGRETO-3", "L10", 3000)
		e.block(t, b1, now.Add(time.Hour)) // a hold with the vessel "Segreta"
		in := complete()
		in.ShowOccupancy, in.OccupancyCategories, in.ShowMap = true, []string{"L10"}, true
		if _, err := e.svc.Put(ctx, e.Owner, e.MarinaID, 0, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Publish(ctx, e.Owner, e.MarinaID, 1); err != nil {
			t.Fatal(err)
		}
		var blobs []string
		add := func(v any, err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(v)
			blobs = append(blobs, string(raw))
		}
		for _, lang := range []string{"it", "en"} {
			p, err := e.svc.PublicProfile(ctx, "marina-test", lang)
			add(p, err)
			h, err := e.svc.PageHead(ctx, "marina-test", lang)
			if err != nil {
				t.Fatal(err)
			}
			blobs = append(blobs, h.HTML)
		}
		m, _, err := e.svc.PublicMap(ctx, "marina-test")
		add(m, err)
		o, _, err := e.svc.PublicOccupancy(ctx, "marina-test")
		add(o, err)
		for _, b := range blobs {
			for _, leak := range []string{"Gianfranco", "Riservatissimo", "gianfranco@privato.example", "9876543", "Segreta", "PONTE-SEGRETO", b1, cust.ID} {
				if strings.Contains(b, leak) {
					t.Fatalf("public output leaks %q: %s", leak, b)
				}
			}
		}
	})
}
