//go:build integration

package harbor

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

func newEnv(t testing.TB, db *store.DB) (*fixture.Env, *Service) {
	t.Helper()
	e := fixture.New(t, db)
	s := NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: filestore.New(t.TempDir(), e.Keys), Log: e.Log})
	return e, s
}

func newMap(t testing.TB, e *fixture.Env, s *Service, w, h int64) *Document {
	t.Helper()
	d, err := s.CreateMap(fixture.Ctx(), e.Owner, e.MarinaID, MapCreate{Kind: "water", Name: I18nText{It: "Specchio acqueo"}, WidthCm: w, HeightCm: h})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// addPier appends a generated pier (berthsPerSide on both sides, pointing down from y = 1000) to req.
func addPier(t testing.TB, req *SaveRequest, prefix, code string, x geo.Cm, berthsPerSide int) {
	t.Helper()
	g, err := layout.GeneratePier(layout.PierParams{
		KeyPrefix: prefix, Origin: geo.Point{X: x, Y: 1000}, Rotation: 900, Length: geo.Cm(berthsPerSide) * 400, Width: 300, Code: code,
		Sides: []layout.Side{layout.SideLeft, layout.SideRight}, BerthsPerSide: berthsPerSide, BerthWidth: 400, BerthLength: 1200,
		Numbering: layout.Numbering{Prefix: code + "-"}, PriceRule: layout.DefaultPriceClassRule(), Depth: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	el := elementsOf(g, "")
	req.Structures = append(req.Structures, el.Structures...)
	req.Berths = append(req.Berths, el.Berths...)
}

func saveDoc(t testing.TB, e *fixture.Env, s *Service, mapID string, version int, req SaveRequest) *SaveResult {
	t.Helper()
	res, err := s.SaveMap(fixture.Ctx(), e.Owner, e.MarinaID, mapID, version, req)
	var mi *MapInvalidError
	if errors.As(err, &mi) {
		t.Fatalf("save: map invalid: %+v", mi.Report.Errors)
	}
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return res
}

func countRows(t testing.TB, db *store.DB, table string) int {
	t.Helper()
	n, err := db.NewSelect().Table(table).Count(fixture.Ctx())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}
