//go:build integration

package harbor

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/harborcheck"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestHarbor_SavedMapsNeverOverlap (INV-04) runs random sequences of map operations: valid saves, saves with an
// injected overlap, generator previews then save, CSV imports, renumbering and concurrent saves. After every
// step no saved map has overlapping berths, and a refused save leaves the version and the rows unchanged.
func TestHarbor_SavedMapsNeverOverlap(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		rapid.Check(t, func(rt *rapid.T) {
			d := newMap(t, e, s, 100000, 30000)
			version := 1
			cur := d.Request()
			piers := 0
			reload := func() {
				got, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
				if err != nil {
					rt.Fatal(err)
				}
				if got.Version != version {
					rt.Fatalf("version %d, want %d", got.Version, version)
				}
				cur = got.Request()
			}
			steps := rapid.IntRange(1, 6).Draw(rt, "steps")
			for range steps {
				switch rapid.IntRange(0, 5).Draw(rt, "op") {
				case 0: // valid save: a new pier on its own
					req := cloneRequest(cur)
					addPier(t, &req, fmt.Sprintf("p%d-", piers), fmt.Sprintf("P%d", piers), geo.Cm(5000+piers*6000), rapid.IntRange(1, 4).Draw(rt, "n"))
					piers++
					if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, version, req); err != nil {
						rt.Fatalf("valid save: %v", err)
					}
					version++
				case 1: // injected overlap: refused, nothing changes
					if len(cur.Berths) < 2 {
						continue
					}
					req := cloneRequest(cur)
					i := rapid.IntRange(1, len(req.Berths)-1).Draw(rt, "i")
					if req.Berths[i].MooringType == string(layout.MooringBuoy) {
						continue
					}
					req.Berths[i].Anchor = req.Berths[0].Anchor
					req.Berths[i].RotationDd = req.Berths[0].RotationDd
					if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, version, req); !errors.Is(err, ErrMapInvalid) {
						rt.Fatalf("overlap accepted: %v", err)
					}
				case 2: // generator preview with the current document, then save
					origin := geo.Point{X: geo.Cm(5000 + piers*6000), Y: 1000}
					rot := int64(rapid.SampledFrom([]int64{900, 850, 950}).Draw(rt, "rot"))
					res, err := s.Generate(ctx, e.Owner, e.MarinaID, d.ID, GenerateInput{Document: &cur, Generator: GeneratorSpec{
						Type: "pier", KeyPrefix: fmt.Sprintf("g%d-", piers), Origin: &origin, RotationDd: &rot, LengthCm: i64(6000), WidthCm: i64(300),
						Code: ptr(fmt.Sprintf("G%d", piers)), Sides: &[]string{"left", "right"}, BerthWidthCm: i64(350), BerthLengthCm: i64(1000),
						Numbering: &Numbering{Scheme: "continuous", Prefix: ptr(fmt.Sprintf("G%d-", piers))},
					}})
					piers++
					if err != nil {
						rt.Fatalf("generate: %v", err)
					}
					req := cloneRequest(cur)
					req.Structures = append(req.Structures, res.Elements.Structures...)
					req.Berths = append(req.Berths, res.Elements.Berths...)
					_, err = s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, version, req)
					if res.Report.OK() != (err == nil) {
						rt.Fatalf("preview report ok=%v but save: %v", res.Report.OK(), err)
					}
					if err == nil {
						version++
					}
				case 3: // CSV import along the first pier
					if len(cur.Structures) == 0 || cur.Structures[0].Kind != "pier" {
						continue
					}
					code := cur.Structures[0].Code
					csv := fmt.Sprintf("X%d-1,%s,10,3\nX%d-2,%s,10,3\n", version, code, version, code)
					rep, err := s.ImportBerths(ctx, e.Owner, e.MarinaID, d.ID, version, BerthImport{
						CSV: csv, Units: "m", Start: "after_existing",
						SidePolicy: "alternate", Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3},
					})
					switch {
					case err == nil:
						version++
						if rep.Document == nil {
							rt.Fatal("applied import without a document")
						}
					case errors.Is(err, ErrMapInvalid), isValidation(err):
					default:
						rt.Fatalf("import: %v", err)
					}
				case 4: // renumber: swap two codes
					if len(cur.Berths) < 2 {
						continue
					}
					req := cloneRequest(cur)
					req.Berths[0].Code, req.Berths[1].Code = req.Berths[1].Code, req.Berths[0].Code
					if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, version, req); err != nil {
						rt.Fatalf("renumber: %v", err)
					}
					version++
				default: // two concurrent saves with the same If-Match: exactly one wins
					var wins atomic.Int32
					var wg sync.WaitGroup
					for range 2 {
						wg.Go(func() {
							if _, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, version, cloneRequest(cur)); err == nil {
								wins.Add(1)
							} else if !errors.Is(err, ErrVersionMismatch) {
								t.Errorf("concurrent save: %v", err)
							}
						})
					}
					wg.Wait()
					if wins.Load() != 1 {
						rt.Fatalf("%d winners", wins.Load())
					}
					version++
				}
				reload()
				harborcheck.AssertNoOverlappingBerthsInMap(rt, db, d.ID)
			}
		})
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

func isValidation(err error) bool {
	var ve *auth.ValidationError
	return errors.As(err, &ve)
}

func ptr[T any](v T) *T { return &v }

// TestConcurrency_MapSaveSingleWinner: 100 parallel saves with the same If-Match: exactly one succeeds and the
// version goes up by exactly one.
func TestConcurrency_MapSaveSingleWinner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 40000, 40000)
		var wins, conflicts atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				req := d.Request()
				addPier(t, &req, "c-", fmt.Sprintf("C%d", i), 5000, 2)
				_, err := s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, 1, req)
				switch {
				case err == nil:
					wins.Add(1)
				case errors.Is(err, ErrVersionMismatch):
					conflicts.Add(1)
				default:
					t.Errorf("save %d: %v", i, err)
				}
			})
		}
		wg.Wait()
		if wins.Load() != 1 || conflicts.Load() != 99 {
			t.Fatalf("%d wins, %d conflicts", wins.Load(), conflicts.Load())
		}
		got, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
		if err != nil || got.Version != 2 || len(got.Berths) != 4 {
			t.Fatalf("version %d, %d berths, %v", got.Version, len(got.Berths), err)
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}

// TestConcurrency_BerthImportVsSave: 100 parallel mixed writes (berth imports and saves that move berths, some
// onto their neighbours): the final state is valid and the version counts exactly the successful writes.
func TestConcurrency_BerthImportVsSave(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		d := newMap(t, e, s, 100000, 40000)
		req := d.Request()
		addPier(t, &req, "a-", "A", 5000, 4)
		req.Structures = append(req.Structures, Structure{Key: "f", Kind: "pier", Code: "F", Points: []geo.Point{{X: 50000, Y: 1000}, {X: 50000, Y: 39000}}, WidthCm: 300})
		saveDoc(t, e, s, d.ID, 1, req)
		var ok, imported atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				for attempt := 0; attempt < 500; attempt++ {
					cur, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
					if err != nil {
						t.Error(err)
						return
					}
					if i%2 == 0 {
						_, err = s.ImportBerths(ctx, e.Owner, e.MarinaID, d.ID, cur.Version, BerthImport{
							CSV: fmt.Sprintf("I%03d,F,8,3\n", i), Units: "m", Start: "after_existing", SidePolicy: "left",
							Mapping: ImportMapping{Code: 0, Pier: 1, Length: 2, Width: 3},
						})
					} else {
						doc := cur.Request()
						b := &doc.Berths[i%4]
						if i%3 == 0 {
							b.Anchor = doc.Berths[(i+1)%4].Anchor // overlap: refused
						} else {
							b.Position++
						}
						_, err = s.SaveMap(ctx, e.Owner, e.MarinaID, d.ID, cur.Version, doc)
					}
					switch {
					case err == nil:
						ok.Add(1)
						if i%2 == 0 {
							imported.Add(1)
						}
						return
					case errors.Is(err, ErrVersionMismatch):
						continue // retry on the new version
					case errors.Is(err, ErrMapInvalid):
						return
					default:
						t.Errorf("write %d: %v", i, err)
						return
					}
				}
			})
		}
		wg.Wait()
		got, err := s.GetMap(ctx, e.Owner, e.MarinaID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 2+int(ok.Load()) {
			t.Fatalf("version %d after %d successful writes", got.Version, ok.Load())
		}
		if len(got.Berths) != 8+int(imported.Load()) || imported.Load() < 40 {
			t.Fatalf("%d berths after %d imports", len(got.Berths), imported.Load())
		}
		harborcheck.AssertNoOverlappingBerths(t, db)
	})
}
