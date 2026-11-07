package contract

import "github.com/coste360/porto/internal/store"

// Row types of the read models, named here so the HTTP views convert them without importing internal/store
// (depguard: handlers and views call domain packages only).
type (
	// Row is a contract or transit row.
	Row = store.Contract
	// AllocationRow is an allocation row.
	AllocationRow = store.Allocation
	// ReleaseRow is a release row.
	ReleaseRow = store.Release
	// ResourceRef is the code and map of a resource.
	ResourceRef = store.ResourceRef
	// VesselRow is a vessel row.
	VesselRow = store.Vessel
)
