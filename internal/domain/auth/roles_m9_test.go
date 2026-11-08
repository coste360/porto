package auth

import (
	"regexp"
	"testing"

	apispec "github.com/coste360/porto/api"
)

// m9RoutePermissions is the M9 operation → permission table (m9-plan §3.2, §7.2, §8, §9, D3, D19): any listed
// permission opens the operation (the service narrows further, e.g. maintenance.report only on the assignee's
// tasks). The builders' handler tests walk it route by route.
var m9RoutePermissions = map[string][]Permission{
	// storage (spec 26.1 "Rimessaggio e ipotesi": dock reads)
	"listStorageAreas":          {PermStorageRead},
	"getStoragePlan":            {PermStorageRead},
	"getStorageAccessibility":   {PermStorageRead},
	"arrangeStorageArea":        {PermStorageManage},
	"evaluateStorageLayout":     {PermStorageRead},
	"createStorageContract":     {PermStorageManage},
	"getStorageContract":        {PermStorageRead},
	"updateStorageContract":     {PermStorageManage},
	"listStorageScenarios":      {PermStorageRead},
	"createStorageScenario":     {PermStorageManage},
	"getStorageScenario":        {PermStorageRead},
	"updateStorageScenario":     {PermStorageManage},
	"discardStorageScenario":    {PermStorageManage},
	"saveStorageScenarioItems":  {PermStorageManage},
	"addStorageScenarioItem":    {PermStorageManage},
	"removeStorageScenarioItem": {PermStorageManage},
	"listStorageTrayCandidates": {PermStorageRead},
	"optimizeStorageScenario":   {PermStorageManage},
	"compareStorageScenario":    {PermStorageRead},
	"applyStorageScenario":      {PermStorageManage},
	"listStorageMovements":      {PermStorageRead},
	"completeStorageMovement":   {PermStorageManage},
	// maintenance (spec 26.1 "Manutenzioni": yard and rental report only)
	"listEquipment":           {PermMaintenanceManage, PermMaintenanceReport},
	"createEquipment":         {PermMaintenanceManage},
	"getEquipment":            {PermMaintenanceManage, PermMaintenanceReport},
	"updateEquipment":         {PermMaintenanceManage},
	"listMaintenancePlans":    {PermMaintenanceManage},
	"createMaintenancePlan":   {PermMaintenanceManage},
	"getMaintenancePlan":      {PermMaintenanceManage},
	"updateMaintenancePlan":   {PermMaintenanceManage},
	"listMaintenanceTasks":    {PermMaintenanceManage, PermMaintenanceReport},
	"createMaintenanceTask":   {PermMaintenanceManage},
	"getMaintenanceTask":      {PermMaintenanceManage, PermMaintenanceReport},
	"updateMaintenanceTask":   {PermMaintenanceManage},
	"scheduleMaintenanceTask": {PermMaintenanceManage},
	"startMaintenanceTask":    {PermMaintenanceManage, PermMaintenanceReport},
	"completeMaintenanceTask": {PermMaintenanceManage, PermMaintenanceReport},
	"releaseMaintenanceTask":  {PermMaintenanceManage},
	"cancelMaintenanceTask":   {PermMaintenanceManage},
	"uploadMaintenancePhoto":  {PermMaintenanceManage, PermMaintenanceReport},
	"getMaintenanceFileLink":  {PermMaintenanceManage, PermMaintenanceReport},
	"getMaintenanceFileFile":  {PermMaintenanceManage, PermMaintenanceReport},
	"reportMaintenance":       {PermMaintenanceReport},
	"getMaintenanceWorklist":  {PermMaintenanceReport},
	"getMaintenanceMap":       {PermMaintenanceManage, PermMaintenanceReport},
	// utilities (D19)
	"listUtilityMeters":         {PermMarinaConfigure, PermPresenceWrite},
	"createUtilityMeter":        {PermMarinaConfigure},
	"getUtilityMeter":           {PermMarinaConfigure, PermPresenceWrite},
	"updateUtilityMeter":        {PermMarinaConfigure},
	"listMeterReadings":         {PermMarinaConfigure, PermPresenceWrite, PermChargesManage},
	"recordMeterReading":        {PermPresenceWrite},
	"uploadMeterReadingPhoto":   {PermPresenceWrite},
	"getMeterReadingPhotoLink":  {PermMarinaConfigure, PermPresenceWrite, PermChargesManage},
	"getMeterReadingPhotoFile":  {PermMarinaConfigure, PermPresenceWrite, PermChargesManage},
	"confirmMeterReading":       {PermChargesManage},
	"voidMeterReading":          {PermChargesManage},
	"listUtilityConsumptions":   {PermChargesManage},
	"excludeUtilityConsumption": {PermChargesManage},
	"assignUtilityConsumption":  {PermChargesManage},
	"runUtilityCharges":         {PermChargesManage},
	"getUtilitySettings":        {PermMarinaConfigure, PermChargesManage},
	"updateUtilitySettings":     {PermMarinaConfigure},
}

// TestM9_RouteTableMatchesContract: every M9 operation is in the contract with known permissions, and the role
// matrix of spec 26.1 holds for storage and maintenance.
func TestM9_RouteTableMatchesContract(t *testing.T) {
	src := string(apispec.OpenAPI)
	for op, perms := range m9RoutePermissions {
		if !regexp.MustCompile(`operationId: ` + op + `\n`).MatchString(src) {
			t.Errorf("%s is not in api/openapi.yaml", op)
		}
		for _, p := range perms {
			if !p.Valid() {
				t.Errorf("%s: unknown permission %s", op, p)
			}
		}
	}
	tagged := regexp.MustCompile(`tags: \[(storage|maintenance|utilities)\]\n`).FindAllString(src, -1)
	if len(tagged) != len(m9RoutePermissions) {
		t.Errorf("%d storage/maintenance/utilities operations in the contract, want %d", len(tagged), len(m9RoutePermissions))
	}
	for _, r := range []Role{RoleOwner, RoleDirector, RoleOffice, RoleYardMgr} {
		if !Can(r, PermStorageManage) {
			t.Errorf("role %s lacks storage.manage (26.1)", r)
		}
	}
	if !Can(RoleDock, PermStorageRead) || Can(RoleDock, PermStorageManage) {
		t.Error("dock must read storage only (26.1)")
	}
	for _, r := range []Role{RoleOwner, RoleDirector, RoleOffice, RoleDock, RoleYardMgr} {
		if !Can(r, PermMaintenanceManage) {
			t.Errorf("role %s lacks maintenance.manage (26.1)", r)
		}
	}
	for _, r := range []Role{RoleYard, RoleRental} {
		if Can(r, PermMaintenanceManage) || !Can(r, PermMaintenanceReport) {
			t.Errorf("role %s must only report maintenance (26.1)", r)
		}
	}
}
