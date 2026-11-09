// Package aggregate counts the free berths per published category tonight for the public page (m12-plan §11,
// D20): a berth is free when it has no occupancy row for the night, is not closed and its sensor (if any, not
// informational) is not physically occupied. Never ids, codes or positions (INV-103). Pure.
package aggregate

// PhysicalOccupied is the physical state that makes a berth not free.
const PhysicalOccupied = "occupied"

// Berth is one active berth.
type Berth struct {
	ID       string
	Category string // price_class
	Occupied bool   // a berth_occupancies row for tonight
	Closed   bool   // a closure covers tonight
}

// Category is the count of a published category.
type Category struct {
	Code        string
	Free, Total int
}

// Count returns the counts of the published categories in their order; physical maps berth id to the physical
// state of the sensor watching it directly (occupied, free, unknown). Berths of other categories, physical states
// of unknown berths and duplicated category codes are ignored; a category without berths counts 0 of 0.
func Count(categories []string, berths []Berth, physical map[string]string) []Category {
	idx := make(map[string]int, len(categories))
	out := make([]Category, 0, len(categories))
	for _, c := range categories {
		if _, dup := idx[c]; dup || c == "" {
			continue
		}
		idx[c] = len(out)
		out = append(out, Category{Code: c})
	}
	for _, b := range berths {
		i, ok := idx[b.Category]
		if !ok || b.Category == "" {
			continue
		}
		out[i].Total++
		if !b.Occupied && !b.Closed && physical[b.ID] != PhysicalOccupied {
			out[i].Free++
		}
	}
	return out
}
