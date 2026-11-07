package docs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSilhouette_ParityVectors: the Go mirror gives the path of web/shared/src/map/vessel-shape.ts for every
// shared vector (testdata/silhouette/vectors.json, also read by the Vitest parity test).
func TestSilhouette_ParityVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "silhouette", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vs []struct {
		LOA      int64  `json:"loa_cm"`
		Beam     int64  `json:"beam_cm"`
		HullType string `json:"hull_type"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) < 50 {
		t.Fatalf("only %d vectors", len(vs))
	}
	for _, v := range vs {
		if got := VesselPath(v.LOA, v.Beam, v.HullType); got != v.Path {
			t.Fatalf("%s %dx%d:\n got %s\nwant %s", v.HullType, v.LOA, v.Beam, got, v.Path)
		}
		if v.Path != "" && len(PathPolygons(v.Path)) == 0 {
			t.Fatalf("%s: no polygon", v.HullType)
		}
	}
}
