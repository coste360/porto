package auth

import "slices"

// Role is a user's role on an organization or a marina (spec 26.1).
type Role string

// Roles.
const (
	RoleOwner      Role = "owner"
	RoleDirector   Role = "director"
	RoleOffice     Role = "office"
	RoleAccounting Role = "accounting"
	RoleDock       Role = "dock"
	RoleYardMgr    Role = "yard_mgr"
	RoleYard       Role = "yard"
	RoleRental     Role = "rental"
	RoleViewer     Role = "viewer"
)

// Roles returns every role in display order.
func Roles() []Role {
	return []Role{RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleDock, RoleYardMgr, RoleYard, RoleRental, RoleViewer}
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return slices.Contains(Roles(), r) }

// Permission is a capability checked by domain services.
type Permission string

// Permissions (m0-plan §3).
const (
	PermOrganizationManage   Permission = "organization.manage"
	PermMarinaConfigure      Permission = "marina.configure"
	PermMarinaRead           Permission = "marina.read"
	PermUsersManage          Permission = "users.manage"
	PermDevicesManage        Permission = "devices.manage"
	PermDevicesPairSelf      Permission = "devices.pair_self"
	PermAuditRead            Permission = "audit.read"
	PermCustomersWrite       Permission = "customers.write"
	PermCustomersRead        Permission = "customers.read"
	PermContractsWrite       Permission = "contracts.write"
	PermTransitsDesk         Permission = "transits.desk"
	PermOccupancyMove        Permission = "occupancy.move"
	PermPresenceWrite        Permission = "presence.write"
	PermOccupancyOverrideFit Permission = "occupancy.override_fit"
	PermRightsManage         Permission = "rights.manage"
	PermChargesManage        Permission = "charges.manage"
	PermChargesExport        Permission = "charges.export"
	PermPaymentsCollect      Permission = "payments.collect"
	PermPaymentsCollectCash  Permission = "payments.collect_cash"
	PermRefundsIssue         Permission = "refunds.issue"
	PermStorageManage        Permission = "storage.manage"
	PermStorageRead          Permission = "storage.read"
	PermYardManage           Permission = "yard.manage"
	PermYardRead             Permission = "yard.read"
	PermYardTimesheets       Permission = "yard.timesheets"
	PermRentalManage         Permission = "rental.manage"
	PermMaintenanceManage    Permission = "maintenance.manage"
	PermMaintenanceReport    Permission = "maintenance.report"
	PermReportsAll           Permission = "reports.all"
	PermReportsOperational   Permission = "reports.operational"
	PermReportsYard          Permission = "reports.yard"
	PermReportsRental        Permission = "reports.rental"
	PermMapView              Permission = "map.view"
	// PermHarborConfigure is the harbour map configuration (maps, elements, groups, harbour settings; spec 8 and
	// 26.1 "configurazione ... mappa"), owner and director only. Added in M1.
	PermHarborConfigure Permission = "harbor.configure"
	// PermPricingConfigure is the configuration of items, classifications and price lists (spec 12 and 26.1
	// "configurazione ... listini"), owner and director only. Added in M2.
	PermPricingConfigure Permission = "pricing.configure"
	// PermPricingRead reads items, price lists, the matrix, the coverage check and runs the simulator: every role
	// except yard (m2-plan §12 D1). Added in M2.
	PermPricingRead Permission = "pricing.read"
)

// Permissions returns every permission.
func Permissions() []Permission {
	return []Permission{
		PermOrganizationManage, PermMarinaConfigure, PermMarinaRead, PermUsersManage, PermDevicesManage,
		PermDevicesPairSelf, PermAuditRead, PermCustomersWrite, PermCustomersRead, PermContractsWrite,
		PermTransitsDesk, PermOccupancyMove, PermPresenceWrite, PermOccupancyOverrideFit, PermRightsManage,
		PermChargesManage, PermChargesExport, PermPaymentsCollect, PermPaymentsCollectCash, PermRefundsIssue,
		PermStorageManage, PermStorageRead, PermYardManage, PermYardRead, PermYardTimesheets, PermRentalManage,
		PermMaintenanceManage, PermMaintenanceReport, PermReportsAll, PermReportsOperational, PermReportsYard,
		PermReportsRental, PermMapView, PermHarborConfigure, PermPricingConfigure, PermPricingRead,
	}
}

// Valid reports whether p is a known permission.
func (p Permission) Valid() bool { return slices.Contains(Permissions(), p) }

// rolePermissions is the role matrix (spec 26.1). organization.manage is listed for owner but only applies to an
// organization-wide grant (Principal.Can enforces it).
var rolePermissions = map[Role][]Permission{
	RoleOwner: {
		PermOrganizationManage, PermMarinaConfigure, PermMarinaRead, PermUsersManage, PermDevicesManage,
		PermDevicesPairSelf, PermAuditRead, PermCustomersWrite, PermCustomersRead, PermContractsWrite,
		PermTransitsDesk, PermOccupancyMove, PermPresenceWrite, PermOccupancyOverrideFit, PermRightsManage,
		PermChargesManage, PermChargesExport, PermPaymentsCollect, PermPaymentsCollectCash, PermRefundsIssue,
		PermStorageManage, PermStorageRead, PermYardManage, PermYardRead, PermYardTimesheets, PermRentalManage,
		PermMaintenanceManage, PermMaintenanceReport, PermReportsAll, PermReportsOperational, PermReportsYard,
		PermReportsRental, PermMapView, PermHarborConfigure, PermPricingConfigure, PermPricingRead,
	},
	RoleDirector: {
		PermMarinaConfigure, PermMarinaRead, PermDevicesManage, PermDevicesPairSelf, PermAuditRead,
		PermCustomersWrite, PermCustomersRead, PermContractsWrite, PermTransitsDesk, PermOccupancyMove,
		PermPresenceWrite, PermOccupancyOverrideFit, PermRightsManage, PermChargesManage, PermChargesExport,
		PermPaymentsCollect, PermPaymentsCollectCash, PermRefundsIssue, PermStorageManage, PermStorageRead,
		PermYardManage, PermYardRead, PermYardTimesheets, PermRentalManage, PermMaintenanceManage,
		PermMaintenanceReport, PermReportsAll, PermReportsOperational, PermReportsYard, PermReportsRental,
		PermMapView, PermHarborConfigure, PermPricingConfigure, PermPricingRead,
	},
	RoleOffice: {
		PermMarinaRead, PermDevicesPairSelf, PermCustomersWrite, PermCustomersRead, PermContractsWrite,
		PermTransitsDesk, PermOccupancyMove, PermPresenceWrite, PermRightsManage, PermChargesManage,
		PermPaymentsCollect, PermPaymentsCollectCash, PermStorageManage, PermStorageRead, PermYardRead,
		PermRentalManage, PermMaintenanceManage, PermMaintenanceReport, PermReportsOperational, PermMapView,
		PermPricingRead,
	},
	RoleAccounting: {
		PermMarinaRead, PermDevicesPairSelf, PermCustomersRead, PermRightsManage, PermChargesManage,
		PermChargesExport, PermPaymentsCollect, PermPaymentsCollectCash, PermRefundsIssue, PermYardRead,
		PermReportsAll, PermReportsOperational, PermReportsYard, PermReportsRental, PermMapView, PermPricingRead,
	},
	RoleDock: {
		PermMarinaRead, PermDevicesPairSelf, PermCustomersRead, PermTransitsDesk, PermOccupancyMove,
		PermPresenceWrite, PermPaymentsCollectCash, PermStorageRead, PermMaintenanceManage, PermMaintenanceReport,
		PermMapView, PermPricingRead,
	},
	RoleYardMgr: {
		PermMarinaRead, PermDevicesPairSelf, PermCustomersRead, PermStorageManage, PermStorageRead,
		PermYardManage, PermYardRead, PermYardTimesheets, PermMaintenanceManage, PermMaintenanceReport,
		PermReportsYard, PermMapView, PermPricingRead,
	},
	RoleYard: {
		PermMarinaRead, PermDevicesPairSelf, PermYardTimesheets, PermMaintenanceReport, PermMapView,
	},
	RoleRental: {
		PermMarinaRead, PermDevicesPairSelf, PermCustomersRead, PermPaymentsCollect, PermPaymentsCollectCash,
		PermRentalManage, PermMaintenanceReport, PermReportsRental, PermMapView, PermPricingRead,
	},
	RoleViewer: {
		PermMarinaRead, PermDevicesPairSelf, PermReportsAll, PermReportsOperational, PermReportsYard,
		PermReportsRental, PermMapView, PermPricingRead,
	},
}

// Can reports whether role r carries permission p (ignoring the organization-wide condition of
// organization.manage, which Principal.Can applies).
func Can(r Role, p Permission) bool { return slices.Contains(rolePermissions[r], p) }

// RolePermissions returns the permissions of a role.
func RolePermissions(r Role) []Permission { return slices.Clone(rolePermissions[r]) }

// apiKeyForbidden lists permissions an API key can never carry.
var apiKeyForbidden = []Permission{PermUsersManage, PermOrganizationManage}

// APIKeyScopeAllowed reports whether p may be granted to an API key at all.
func APIKeyScopeAllowed(p Permission) bool { return p.Valid() && !slices.Contains(apiKeyForbidden, p) }

// requiresTOTPByPolicy lists the roles that must enrol TOTP when PORTO_REQUIRE_TOTP_FOR_OWNERS is on.
var requiresTOTPByPolicy = []Role{RoleOwner, RoleAccounting}
