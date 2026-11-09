package simplemap

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSimpleMap_Round(t *testing.T) {
	cases := map[int64]int64{0: 0, 4: 0, 5: 0, 6: 10, 15: 20, 25: 20, 14: 10, -4: 0, -5: 0, -6: -10, -15: -20, -25: -20, 1234: 1230}
	for in, want := range cases {
		if got := Round(in); got != want {
			t.Errorf("Round(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestSimpleMap_Golden(t *testing.T) {
	in := Input{
		MinXCm: 3, MinYCm: -7, MaxXCm: 10004, MaxYCm: 8006, Version: 7,
		Structures: []Shape{
			{Points: []Point{{0, 0}, {4, 3}, {1001, 0}}, WidthCm: 296, Category: "leak"},
			{Points: []Point{{0, 0}}, Closed: true}, // degenerate: dropped
		},
		Berths: []Shape{{Points: []Point{{100, 100}, {396, 104}, {394, 1203}, {98, 1197}}, Closed: true, Category: "B10"}},
		Areas:  []Shape{{Points: []Point{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}}, Closed: true, Category: "leak"}},
		Land:   []Shape{{Points: []Point{{50, 50}, {54, 52}}}},
		POI:    []POI{{Kind: "fuel", XCm: 1236, YCm: 45}, {Kind: "berth", XCm: 1, YCm: 1}},
	}
	got, err := json.Marshal(Build(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"MinXCm":0,"MinYCm":-10,"MaxXCm":10000,"MaxYCm":8010,` +
		`"Structures":[{"Points":[[0,0],[1000,0]],"Closed":false,"WidthCm":300,"Category":""}],` +
		`"Berths":[{"Points":[[100,100],[400,100],[390,1200],[100,1200]],"Closed":true,"WidthCm":0,"Category":"B10"}],` +
		`"Areas":[{"Points":[[0,0],[10,0],[10,10],[0,10]],"Closed":true,"WidthCm":0,"Category":""}],` +
		`"Land":[],` +
		`"POI":[{"Kind":"fuel","XCm":1240,"YCm":40}],"Version":7}`
	if string(got) != want {
		t.Fatalf("Build =\n%s\nwant\n%s", got, want)
	}
}

func TestSimpleMap_Decimate(t *testing.T) {
	var open, closed []Point
	for i := range 100 {
		open = append(open, Point{int64(i) * 100, 0})
		closed = append(closed, Point{int64(i) * 100, int64(i%2) * 1000})
	}
	m := Build(Input{Structures: []Shape{{Points: open}}, Areas: []Shape{{Points: closed, Closed: true}}})
	s := m.Structures[0].Points
	if len(s) != MaxPoints || s[0] != (Point{0, 0}) || s[len(s)-1] != (Point{9900, 0}) {
		t.Fatalf("open line decimated to %d points, ends %v %v", len(s), s[0], s[len(s)-1])
	}
	if a := m.Areas[0].Points; len(a) != MaxPoints || a[0] != (Point{0, 0}) {
		t.Fatalf("closed ring decimated to %d points", len(a))
	}
	again := Build(Input{Structures: []Shape{{Points: open}}, Areas: []Shape{{Points: closed, Closed: true}}})
	if !reflect.DeepEqual(m, again) {
		t.Fatal("Build is not deterministic")
	}
}

// TestSimpleMap_NoCodesNoOccupancy checks INV-103 on the shape of the output: no field can carry a code, a name,
// an id, an occupancy or a sensor, and categories survive on berths only.
func TestSimpleMap_NoCodesNoOccupancy(t *testing.T) {
	allowed := map[string]map[string]bool{
		"Map":   {"MinXCm": true, "MinYCm": true, "MaxXCm": true, "MaxYCm": true, "Structures": true, "Berths": true, "Areas": true, "Land": true, "POI": true, "Version": true},
		"Shape": {"Points": true, "Closed": true, "WidthCm": true, "Category": true},
		"POI":   {"Kind": true, "XCm": true, "YCm": true},
	}
	for _, v := range []any{Map{}, Shape{}, POI{}} {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			if !allowed[typ.Name()][typ.Field(i).Name] {
				t.Errorf("%s.%s is not an allowed public field", typ.Name(), typ.Field(i).Name)
			}
		}
	}
	m := Build(Input{
		Structures: []Shape{{Points: []Point{{0, 0}, {100, 0}}, Category: "S-CODE"}},
		Areas:      []Shape{{Points: []Point{{0, 0}, {100, 0}, {100, 100}}, Closed: true, Category: "A-CODE"}},
		Land:       []Shape{{Points: []Point{{0, 0}, {100, 0}, {100, 100}}, Closed: true, Category: "L-CODE"}},
	})
	b, _ := json.Marshal(m)
	for _, leak := range []string{"S-CODE", "A-CODE", "L-CODE"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("output carries %s", leak)
		}
	}
}
