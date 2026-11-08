package auth

import (
	"regexp"
	"slices"
	"testing"

	apispec "github.com/coste360/porto/api"
)

// billingRoutePermissions is the M5 route → permission table of m5-plan §7.2 / §7.3 (the first permission
// listed is enough unless the operation says otherwise in its handler). TestBilling_PermissionsPerRoute in the
// builders' handler tests walks it route by route; here it is checked against the contract and the role matrix.
var billingRoutePermissions = map[string][]Permission{
	// 7.3.1 charges, exports, settings
	"listCharges":                 {PermChargesManage, PermPaymentsCollect},
	"createCharge":                {PermChargesManage},
	"getCharge":                   {PermChargesManage, PermPaymentsCollect},
	"updateCharge":                {PermChargesManage},
	"cancelCharge":                {PermChargesManage},
	"adjustCharge":                {PermChargesManage},
	"setChargeInvoice":            {PermChargesExport},
	"createTerminationCharge":     {PermChargesManage},
	"listChargeExports":           {PermChargesExport},
	"previewChargeExport":         {PermChargesExport},
	"createChargeExport":          {PermChargesExport},
	"reexportCharges":             {PermChargesExport},
	"getChargeExport":             {PermChargesExport},
	"downloadChargeExport":        {PermChargesExport},
	"listInvoiceReconciliations":  {PermChargesExport},
	"createInvoiceReconciliation": {PermChargesExport},
	"getBillingSettings":          {PermChargesManage},
	"updateBillingSettings":       {PermMarinaConfigure},
	"importOpeningBalances":       {PermChargesManage},
	"listOpeningBalanceImports":   {PermChargesManage},
	"undoOpeningBalanceImport":    {PermChargesManage},
	"exportPayments":              {PermChargesExport},
	// 7.3.2 payments, credits, receivables, dashboard
	"listPayments":         {PermPaymentsCollect, PermChargesManage},
	"recordPayment":        {PermPaymentsCollect, PermPaymentsCollectCash},
	"getPayment":           {PermPaymentsCollect, PermChargesManage},
	"matchPayment":         {PermPaymentsCollect},
	"reverseMatch":         {PermRefundsIssue},
	"refundPayment":        {PermRefundsIssue},
	"retainDeposit":        {PermChargesManage},
	"listRefunds":          {PermPaymentsCollect, PermChargesManage},
	"listCredits":          {PermChargesManage},
	"getCustomerCredit":    {PermChargesManage},
	"grantCustomerCredit":  {PermRefundsIssue},
	"applyCustomerCredit":  {PermPaymentsCollect},
	"getCustomerStatement": {PermChargesManage},
	"getCustomerDashboard": {PermCustomersRead},
	"listReceivables":      {PermChargesManage},
	"getReceivablesAgeing": {PermChargesManage},
	"getMapPaymentStatus":  {PermMapView},
	"settleMandate":        {PermRefundsIssue},
	"listWorklist":         {PermContractsRead, PermChargesManage},
	"updateWorkItem":       {PermContractsRead, PermChargesManage},
	// 7.3.3 cash, fiscal, collection, providers
	"listCashRegisters":             {PermPaymentsCollect, PermPaymentsCollectCash},
	"createCashRegister":            {PermMarinaConfigure},
	"updateCashRegister":            {PermMarinaConfigure},
	"listCashSessions":              {PermPaymentsCollect, PermPaymentsCollectCash},
	"openCashSession":               {PermPaymentsCollect, PermPaymentsCollectCash},
	"getCurrentCashSession":         {PermPaymentsCollect, PermPaymentsCollectCash},
	"getCashSession":                {PermPaymentsCollect, PermPaymentsCollectCash},
	"addCashMovement":               {PermPaymentsCollect, PermPaymentsCollectCash},
	"closeCashSession":              {PermPaymentsCollect, PermPaymentsCollectCash},
	"getZReport":                    {PermPaymentsCollect, PermPaymentsCollectCash},
	"createCounterSale":             {PermPaymentsCollect, PermPaymentsCollectCash},
	"getPaymentReceipt":             {PermPaymentsCollect, PermChargesManage, PermPaymentsCollectCash},
	"getFiscalDailyTotals":          {PermChargesExport},
	"listFiscalDocuments":           {PermChargesExport},
	"retryFiscalDocument":           {PermChargesExport},
	"voidFiscalDocument":            {PermRefundsIssue},
	"listPaymentRequests":           {PermChargesManage},
	"createPaymentRequest":          {PermChargesManage},
	"createPaymentRequestsBulk":     {PermChargesManage},
	"getPaymentRequest":             {PermChargesManage},
	"sendPaymentRequest":            {PermChargesManage},
	"cancelPaymentRequest":          {PermChargesManage},
	"createTransitPaymentLink":      {PermTransitsDesk},
	"listReminderRuns":              {PermChargesManage},
	"createReminderRun":             {PermChargesManage},
	"getReminderRun":                {PermChargesManage},
	"sendReminders":                 {PermChargesManage},
	"approveReminderItem":           {PermChargesManage},
	"dismissReminderItem":           {PermChargesManage},
	"listDunningExclusions":         {PermChargesManage},
	"createDunningExclusion":        {PermChargesManage},
	"deleteDunningExclusion":        {PermChargesManage},
	"listPaymentProviders":          {PermPaymentsCollect},
	"getPaymentProviderSettings":    {PermProvidersConfigure},
	"updatePaymentProviderSettings": {PermProvidersConfigure},
	"testPaymentProvider":           {PermProvidersConfigure},
	"listSepaMandates":              {PermChargesManage},
	"createSepaMandate":             {PermChargesManage},
	"revokeSepaMandate":             {PermChargesManage},
}

// TestBilling_RouteTableMatchesContract: every M5 admin operation of the contract is in the table (and only
// those), every permission is a constant of roles.go, and the roles of m5-plan §7.2 hold them.
func TestBilling_RouteTableMatchesContract(t *testing.T) {
	src := string(apispec.OpenAPI)
	for op, perms := range billingRoutePermissions {
		if !regexp.MustCompile(`operationId: ` + op + `\n`).MatchString(src) {
			t.Errorf("%s is not in api/openapi.yaml", op)
		}
		for _, p := range perms {
			if !p.Valid() {
				t.Errorf("%s: unknown permission %s", op, p)
			}
		}
	}
	want := map[Permission][]Role{
		PermChargesManage:       {RoleOwner, RoleDirector, RoleOffice, RoleAccounting},
		PermChargesExport:       {RoleOwner, RoleDirector, RoleAccounting},
		PermPaymentsCollect:     {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleRental},
		PermRefundsIssue:        {RoleOwner, RoleDirector, RoleAccounting},
		PermProvidersConfigure:  {RoleOwner},
		PermPaymentsCollectCash: {RoleDock},
	}
	for p, roles := range want {
		for _, r := range roles {
			if !Can(r, p) {
				t.Errorf("role %s lacks %s (m5-plan §7.2)", r, p)
			}
		}
	}
	if !slices.Contains(Permissions(), PermMapView) {
		t.Fatal("map.view missing")
	}
}

func TestPrincipal_IsOwnerAnywhere(t *testing.T) {
	orgOwner := Grant{OrganizationID: "o2", Role: RoleOwner}
	cases := []struct {
		name string
		p    *Principal
		want bool
		org  string
	}{
		{"nil", nil, false, ""},
		{"organization owner", &Principal{Kind: KindUser, Grants: []Grant{{OrganizationID: "o1", MarinaID: "m", Role: RoleDock}, orgOwner}}, true, "o2"},
		{"marina-level owner", &Principal{Kind: KindUser, Grants: []Grant{{OrganizationID: "o", MarinaID: "m", Role: RoleOwner}}}, false, ""},
		{"director", &Principal{Kind: KindUser, Grants: []Grant{{OrganizationID: "o", Role: RoleDirector}}}, false, ""},
		{"device of an owner", &Principal{Kind: KindDevice, Grants: []Grant{orgOwner}}, false, "o2"},
		{"api key", &Principal{Kind: KindAPIKey, Grants: []Grant{orgOwner}}, false, "o2"},
		{"owner enrolling TOTP", &Principal{Kind: KindUser, TOTPEnrolmentRequired: true, Grants: []Grant{orgOwner}}, false, "o2"},
	}
	for _, c := range cases {
		if got := c.p.IsOwnerAnywhere(); got != c.want {
			t.Errorf("%s: IsOwnerAnywhere = %v", c.name, got)
		}
		if got := c.p.FirstOwnedOrganization(); got != c.org {
			t.Errorf("%s: FirstOwnedOrganization = %q", c.name, got)
		}
	}
}

// reportRoutePermissions lists the M7 operations (m7-plan §1.1, §8, D17) and the permissions that open them: any
// of reports.operational or reports.all for the operational reports, the dashboard and the comparison (which
// then narrow to what the principal may see), reports.all for the money reports.
var reportRoutePermissions = map[string][]Permission{
	"getDashboard":             {PermReportsOperational, PermReportsAll},
	"getReportComparison":      {PermReportsOperational, PermReportsAll},
	"getOccupancyReport":       {PermReportsOperational, PermReportsAll},
	"getEfficiencyReport":      {PermReportsOperational, PermReportsAll},
	"getAllocationWasteReport": {PermReportsOperational, PermReportsAll},
	"getLostDemandReport":      {PermReportsOperational, PermReportsAll},
	"getPresencesReport":       {PermReportsOperational, PermReportsAll},
	"getVesselsReport":         {PermReportsOperational, PermReportsAll},
	"getCustomersReport":       {PermReportsAll},
	"getProductionReport":      {PermReportsAll},
	"getAccrualsReport":        {PermReportsAll},
	"getChargesReport":         {PermReportsAll},
	"getPaymentsReport":        {PermReportsAll},
	"getRevenueClassesReport":  {PermReportsAll},
	"getReceivablesReport":     {PermReportsAll},
	"getRightsReport":          {PermReportsAll},
	"getCashReport":            {PermReportsAll},
	"getBerthRevenueReport":    {PermReportsAll},
	// M9 (m9-plan §11): amounts and costs only with reports.all.
	"getStorageReport":     {PermReportsOperational, PermReportsAll},
	"getMaintenanceReport": {PermReportsOperational, PermReportsAll},
	"getUtilitiesReport":   {PermReportsOperational, PermReportsAll},
	// M10 (m10-plan §12, D32): the yard report with reports.yard (yard_mgr) as well as reports.all.
	"getYardReport": {PermReportsYard, PermReportsAll},
}

// TestReports_RouteTableMatchesContract: every M7 operation is in the contract with known permissions, and the
// role matrix of spec 26.1 holds (office operational only; dock, yard and rental without reports).
func TestReports_RouteTableMatchesContract(t *testing.T) {
	src := string(apispec.OpenAPI)
	for op, perms := range reportRoutePermissions {
		if !regexp.MustCompile(`operationId: ` + op + `\n`).MatchString(src) {
			t.Errorf("%s is not in api/openapi.yaml", op)
		}
		for _, p := range perms {
			if !p.Valid() {
				t.Errorf("%s: unknown permission %s", op, p)
			}
		}
	}
	if n := len(regexp.MustCompile(`/reports/[a-z-]+:\n`).FindAllString(src, -1)); n != len(reportRoutePermissions)-1 {
		t.Errorf("%d report paths in the contract, want %d", n, len(reportRoutePermissions)-1)
	}
	for _, r := range []Role{RoleOwner, RoleDirector, RoleAccounting, RoleViewer} {
		if !Can(r, PermReportsAll) || !Can(r, PermReportsOperational) {
			t.Errorf("role %s lacks the reports permissions", r)
		}
	}
	if !Can(RoleOffice, PermReportsOperational) || Can(RoleOffice, PermReportsAll) {
		t.Error("office must have reports.operational only")
	}
	for _, r := range []Role{RoleDock, RoleYard, RoleYardMgr, RoleRental} {
		if Can(r, PermReportsAll) || Can(r, PermReportsOperational) {
			t.Errorf("role %s must not see the M7 reports", r)
		}
	}
}
