package layout_test

import (
	"bytes"
	"encoding/json"
	"os" //nolint:depguard // the test reads the shared parity vectors of testdata/footprint (not the engine)
	"path/filepath"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/domain/layout/internal/vectors"
)

// The vectors are shared with web/shared/src/map/footprint.parity.test.ts (m1-plan §2.8).
var vectorDir = filepath.Join("..", "..", "..", "testdata", "footprint")

// Hand-written cases have hand-written expectations: the Go implementation must agree with them.
func TestFootprint_Scenario(t *testing.T) {
	for _, c := range vectors.Handwritten() {
		t.Run(c.Name, func(t *testing.T) {
			if got := vectors.Run(c); !reflect.DeepEqual(got, c.Expect) {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(c.Expect)
				t.Fatalf("got %s, want %s", gb, wb)
			}
		})
	}
	if n := len(vectors.Handwritten()); n < 40 {
		t.Fatalf("only %d hand-written cases", n)
	}
}

func TestFootprint_ParityVectors(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(vectorDir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f vectors.File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.Cases) < 440 {
		t.Fatalf("version %d, %d cases", f.Version, len(f.Cases))
	}
	for _, c := range f.Cases {
		got := vectors.Run(c)
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(c.Expect)
		if !bytes.Equal(gb, wb) {
			t.Errorf("%s: got %s, want %s", c.Name, gb, wb)
		}
	}
	ab, err := os.ReadFile(filepath.Join(vectorDir, "area-cells.json"))
	if err != nil {
		t.Fatal(err)
	}
	var af struct {
		Cases []vectors.AreaCase `json:"cases"`
	}
	if err := json.Unmarshal(ab, &af); err != nil {
		t.Fatal(err)
	}
	for _, c := range af.Cases {
		got, err := vectors.RunArea(c)
		if err != nil || !reflect.DeepEqual(got, c.Expect) {
			t.Errorf("%s: got %+v %v, want %+v", c.Name, got, err, c.Expect)
		}
	}
}

func TestFootprint_VectorsUpToDate(t *testing.T) {
	want, err := vectors.Encode(vectors.Cases(1, 400))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vectorDir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("testdata/footprint/cases.json is stale: run go run ./internal/domain/layout/internal/genvectors -seed 1 -n 400")
	}
	wantAreas, err := vectors.EncodeAreas(vectors.AreaCases())
	if err != nil {
		t.Fatal(err)
	}
	gotAreas, err := os.ReadFile(filepath.Join(vectorDir, "area-cells.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotAreas, wantAreas) {
		t.Fatal("testdata/footprint/area-cells.json is stale: run genvectors")
	}
}

func TestAreaOf_Scenario(t *testing.T) {
	for _, c := range vectors.AreaCases() {
		t.Run(c.Name, func(t *testing.T) {
			got, err := vectors.RunArea(c)
			if err != nil || !reflect.DeepEqual(got, c.Expect) {
				t.Fatalf("got %+v %v, want %+v", got, err, c.Expect)
			}
		})
	}
}
