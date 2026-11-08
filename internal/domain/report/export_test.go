package report

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/optimizer"
)

// SetStorageAccess replaces the accessibility evaluation of the storage report for a test; the returned function
// restores it. The accessibility itself is tested in the optimizer package.
func SetStorageAccess(f func(a layout.Area, exits []int, fixed []optimizer.Obstacle, items []optimizer.TimelineItem,
	from, to calendar.Date) []optimizer.Access,
) func() {
	old := storageAccess
	storageAccess = f
	return func() { storageAccess = old }
}
