package aggregate

import (
	"reflect"
	"testing"
)

func TestAggregate_Table(t *testing.T) {
	berths := []Berth{
		{ID: "b1", Category: "A"},
		{ID: "b2", Category: "A", Occupied: true},
		{ID: "b3", Category: "A", Closed: true},
		{ID: "b4", Category: "A"}, // sensor occupied
		{ID: "b5", Category: "B"},
		{ID: "b6", Category: "B"}, // sensor free
		{ID: "b7", Category: "C"}, // category not published
		{ID: "b8", Category: ""},  // no category
		{ID: "b9", Category: "B"}, // sensor unknown
	}
	physical := map[string]string{
		"b4": "occupied", "b6": "free", "b9": "unknown",
		"b7": "occupied", "pedestal-device-berth": "occupied", // informational / unknown berths are ignored
	}
	cases := []struct {
		name       string
		categories []string
		berths     []Berth
		physical   map[string]string
		want       []Category
	}{
		{"rows closures sensors", []string{"A", "B"}, berths, physical, []Category{{"A", 1, 4}, {"B", 3, 3}}},
		{"order kept, unpublished excluded", []string{"B"}, berths, physical, []Category{{"B", 3, 3}}},
		{"empty category", []string{"Z", "A"}, berths, physical, []Category{{"Z", 0, 0}, {"A", 1, 4}}},
		{"duplicates and blank skipped", []string{"A", "", "A"}, berths, physical, []Category{{"A", 1, 4}}},
		{"no sensors", []string{"A"}, berths, nil, []Category{{"A", 2, 4}}},
		{"nothing published", nil, berths, physical, []Category{}},
		{"no berths", []string{"A"}, nil, physical, []Category{{"A", 0, 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Count(tc.categories, tc.berths, tc.physical)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Count = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAggregate_OutputHasNoIDs(t *testing.T) {
	got := Count([]string{"A"}, []Berth{{ID: "secret-id", Category: "A"}}, nil)
	typ := reflect.TypeOf(got[0])
	for i := range typ.NumField() {
		if n := typ.Field(i).Name; n != "Code" && n != "Free" && n != "Total" {
			t.Fatalf("unexpected field %s in Category", n)
		}
	}
}
