package auth

import (
	"regexp"
	"testing"

	apispec "github.com/coste360/porto/api"
)

// m11RoutePermissions is the M11 operation → permission table (m11-plan §13.1, D16, D20): all listed permissions
// are needed when the operation says "+", any one otherwise (the service narrows further: cash deposits and cash
// quick cash accept payments.collect_cash instead of payments.collect). The builders' handler tests walk it route
// by route.
var m11RoutePermissions = map[string][]Permission{
	// fleet: units and skippers are operations, formulas, extras and settings configuration (D20)
	"listRentalUnits":       {PermRentalManage},
	"createRentalUnit":      {PermRentalManage},
	"getRentalUnit":         {PermRentalManage},
	"updateRentalUnit":      {PermRentalManage},
	"listRentalSkippers":    {PermRentalManage},
	"createRentalSkipper":   {PermRentalManage},
	"updateRentalSkipper":   {PermRentalManage},
	"putRentalSkipperHours": {PermRentalManage},
	"listRentalFormulas":    {PermRentalManage},
	"createRentalFormula":   {PermMarinaConfigure},
	"updateRentalFormula":   {PermMarinaConfigure},
	"listRentalExtras":      {PermRentalManage},
	"createRentalExtra":     {PermMarinaConfigure},
	"updateRentalExtra":     {PermMarinaConfigure},
	"getRentalSettings":     {PermRentalManage},
	"putRentalSettings":     {PermMarinaConfigure},
	"quoteRental":           {PermRentalManage},
	// bookings
	"getRentalPlanner":         {PermRentalManage},
	"searchRentalAvailability": {PermRentalManage},
	"listRentalBookings":       {PermRentalManage},
	"createRentalBooking":      {PermRentalManage},
	"getRentalBooking":         {PermRentalManage},
	"updateRentalBooking":      {PermRentalManage},
	"transitionRentalBooking":  {PermRentalManage},
	"listRentalBlocks":         {PermRentalManage},
	"createRentalBlock":        {PermRentalManage},
	"updateRentalBlock":        {PermRentalManage},
	"deleteRentalBlock":        {PermRentalManage},
	"getRentalDay":             {PermRentalManage},
	// handover (D16: retention and release at check-in with rental.manage)
	"getHandover":           {PermRentalManage},
	"saveHandoverChecklist": {PermRentalManage},
	"uploadHandoverPhoto":   {PermRentalManage},
	"putHandoverIdentity":   {PermRentalManage},
	"signRentalContract":    {PermRentalManage},
	"getRentalContractPdf":  {PermRentalManage},
	"getRentalDeposit":      {PermRentalManage},
	"startRentalDeposit":    {PermRentalManage, PermPaymentsCollect},
	"checkOutRental":        {PermRentalManage},
	"createRentalDamage":    {PermRentalManage},
	"updateRentalDamage":    {PermRentalManage},
	"deleteRentalDamage":    {PermRentalManage},
	"previewRentalCheckIn":  {PermRentalManage},
	"checkInRental":         {PermRentalManage},
	"closeRental":           {PermRentalManage},
	"getRentalHandoverPdf":  {PermRentalManage},
	"quickCashRental":       {PermRentalManage, PermPaymentsCollect},
}

// TestM11_RouteTableMatchesContract: every M11 operation is in the contract with known permissions, and the role
// matrix of spec 26.1 holds for rentals.
func TestM11_RouteTableMatchesContract(t *testing.T) {
	src := string(apispec.OpenAPI)
	for op, perms := range m11RoutePermissions {
		if !regexp.MustCompile(`operationId: ` + op + `\n`).MatchString(src) {
			t.Errorf("%s is not in api/openapi.yaml", op)
		}
		for _, p := range perms {
			if !p.Valid() {
				t.Errorf("%s: unknown permission %s", op, p)
			}
		}
	}
	tagged := regexp.MustCompile(`tags: \[rental-(fleet|bookings|handover)\]\n`).FindAllString(src, -1)
	if len(tagged) != len(m11RoutePermissions) {
		t.Errorf("%d rental operations in the contract, want %d", len(tagged), len(m11RoutePermissions))
	}
	for _, r := range []Role{RoleOwner, RoleDirector, RoleOffice, RoleRental} {
		if !Can(r, PermRentalManage) {
			t.Errorf("role %s lacks rental.manage (26.1)", r)
		}
	}
	for _, r := range []Role{RoleAccounting, RoleDock, RoleYardMgr, RoleYard, RoleViewer} {
		if Can(r, PermRentalManage) {
			t.Errorf("role %s must not manage rentals (26.1)", r)
		}
	}
	if !Can(RoleRental, PermPaymentsCollect) || !Can(RoleRental, PermPaymentsCollectCash) || !Can(RoleRental, PermReportsRental) {
		t.Error("the rental role collects payments and reads the rental report (26.1)")
	}
	if Can(RoleRental, PermMarinaConfigure) || Can(RoleRental, PermCustomersWrite) {
		t.Error("the rental role neither configures the marina nor writes customers (26.1, D13, D20)")
	}
}
