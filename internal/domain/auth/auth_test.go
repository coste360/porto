package auth

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
)

// specMatrix is an independent copy of the table in m0-plan §3 (spec 26.1): permission -> roles.
var specMatrix = map[Permission][]Role{
	PermOrganizationManage:   {RoleOwner},
	PermMarinaConfigure:      {RoleOwner, RoleDirector},
	PermMarinaRead:           {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleDock, RoleYardMgr, RoleYard, RoleRental, RoleViewer},
	PermUsersManage:          {RoleOwner},
	PermDevicesManage:        {RoleOwner, RoleDirector},
	PermDevicesPairSelf:      {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleDock, RoleYardMgr, RoleYard, RoleRental, RoleViewer},
	PermAuditRead:            {RoleOwner, RoleDirector},
	PermCustomersWrite:       {RoleOwner, RoleDirector, RoleOffice},
	PermCustomersRead:        {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleDock, RoleYardMgr, RoleRental},
	PermContractsWrite:       {RoleOwner, RoleDirector, RoleOffice},
	PermTransitsDesk:         {RoleOwner, RoleDirector, RoleOffice, RoleDock},
	PermOccupancyMove:        {RoleOwner, RoleDirector, RoleOffice, RoleDock},
	PermPresenceWrite:        {RoleOwner, RoleDirector, RoleOffice, RoleDock},
	PermOccupancyOverrideFit: {RoleOwner, RoleDirector},
	PermRightsManage:         {RoleOwner, RoleDirector, RoleOffice, RoleAccounting},
	PermChargesManage:        {RoleOwner, RoleDirector, RoleOffice, RoleAccounting},
	PermChargesExport:        {RoleOwner, RoleDirector, RoleAccounting},
	PermPaymentsCollect:      {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleRental},
	PermPaymentsCollectCash:  {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleRental, RoleDock},
	PermRefundsIssue:         {RoleOwner, RoleDirector, RoleAccounting},
	PermStorageManage:        {RoleOwner, RoleDirector, RoleOffice, RoleYardMgr},
	PermStorageRead:          {RoleOwner, RoleDirector, RoleOffice, RoleYardMgr, RoleDock},
	PermYardManage:           {RoleOwner, RoleDirector, RoleYardMgr},
	PermYardRead:             {RoleOwner, RoleDirector, RoleYardMgr, RoleOffice, RoleAccounting},
	PermYardTimesheets:       {RoleOwner, RoleDirector, RoleYardMgr, RoleYard},
	PermRentalManage:         {RoleOwner, RoleDirector, RoleOffice, RoleRental},
	PermMaintenanceManage:    {RoleOwner, RoleDirector, RoleOffice, RoleDock, RoleYardMgr},
	PermMaintenanceReport:    {RoleOwner, RoleDirector, RoleOffice, RoleDock, RoleYardMgr, RoleYard, RoleRental},
	PermReportsAll:           {RoleOwner, RoleDirector, RoleAccounting, RoleViewer},
	PermReportsOperational:   {RoleOwner, RoleDirector, RoleAccounting, RoleViewer, RoleOffice},
	PermReportsYard:          {RoleOwner, RoleDirector, RoleAccounting, RoleViewer, RoleYardMgr},
	PermReportsRental:        {RoleOwner, RoleDirector, RoleAccounting, RoleViewer, RoleRental},
	PermMapView:              {RoleOwner, RoleDirector, RoleOffice, RoleAccounting, RoleDock, RoleYardMgr, RoleYard, RoleRental, RoleViewer},
}

func TestRoles_MatrixMatchesSpec(t *testing.T) {
	if len(specMatrix) != len(Permissions()) {
		t.Fatalf("spec lists %d permissions, code %d", len(specMatrix), len(Permissions()))
	}
	for _, perm := range Permissions() {
		want, ok := specMatrix[perm]
		if !ok {
			t.Errorf("%s not in spec", perm)
			continue
		}
		for _, r := range Roles() {
			if got := Can(r, perm); got != slices.Contains(want, r) {
				t.Errorf("Can(%s, %s) = %v, spec says %v", r, perm, got, !got)
			}
		}
	}
}

func TestRoles_EveryPermissionHasOwner(t *testing.T) {
	for _, perm := range Permissions() {
		if !Can(RoleOwner, perm) {
			t.Errorf("owner lacks %s", perm)
		}
		if !perm.Valid() {
			t.Errorf("%s invalid", perm)
		}
	}
	for _, r := range Roles() {
		if !r.Valid() || len(RolePermissions(r)) == 0 {
			t.Errorf("role %s", r)
		}
	}
	if Role("captain").Valid() || Permission("x.y").Valid() {
		t.Fatal("unknown values valid")
	}
}

func TestPrincipal_Can(t *testing.T) {
	orgOwner := &Principal{Kind: KindUser, Grants: []Grant{{OrganizationID: "o", Role: RoleOwner}}}
	marinaOwner := &Principal{Kind: KindUser, Grants: []Grant{{OrganizationID: "o", MarinaID: "m1", Role: RoleOwner}}}
	mixed := &Principal{Kind: KindUser, Grants: []Grant{
		{OrganizationID: "o", Role: RoleViewer}, {OrganizationID: "o", MarinaID: "m1", Role: RoleDock},
	}}
	key := &Principal{Kind: KindAPIKey, Grants: []Grant{{OrganizationID: "o", MarinaID: "m1"}}, Scopes: []Permission{PermMarinaRead, PermUsersManage}}
	cases := []struct {
		name        string
		p           *Principal
		org, marina string
		perm        Permission
		want        bool
	}{
		{"org owner manages org", orgOwner, "o", "", PermOrganizationManage, true},
		{"org owner on any marina", orgOwner, "o", "m9", PermUsersManage, true},
		{"org owner other org", orgOwner, "x", "m1", PermMarinaRead, false},
		{"marina owner cannot manage org", marinaOwner, "o", "m1", PermOrganizationManage, false},
		{"marina owner on own marina", marinaOwner, "o", "m1", PermUsersManage, true},
		{"marina owner on other marina", marinaOwner, "o", "m2", PermMarinaRead, false},
		{"union: dock on m1", mixed, "o", "m1", PermTransitsDesk, true},
		{"union: viewer only on m2", mixed, "o", "m2", PermTransitsDesk, false},
		{"union: viewer reports on m1", mixed, "o", "m1", PermReportsAll, true},
		{"key in scope", key, "o", "m1", PermMarinaRead, true},
		{"key never users.manage", key, "o", "m1", PermUsersManage, false},
		{"key out of scope", key, "o", "m1", PermAuditRead, false},
		{"key other marina", key, "o", "m2", PermMarinaRead, false},
	}
	for _, tc := range cases {
		if got := tc.p.Can(tc.org, tc.marina, tc.perm); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
	gated := &Principal{Kind: KindUser, Grants: orgOwner.Grants, TOTPEnrolmentRequired: true}
	if gated.Can("o", "m1", PermMarinaRead) {
		t.Fatal("enrolment-gated principal has permissions")
	}
	if got := mixed.RolesOn("o", "m1"); !slices.Equal(got, []Role{RoleViewer, RoleDock}) {
		t.Fatalf("RolesOn = %v", got)
	}
}

func TestHasher_HashVerifyRehash(t *testing.T) {
	h := &Hasher{Params: TestArgon2Params}
	enc, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("phc %s", enc)
	}
	if ok, err := h.Verify("correct horse battery", enc); !ok || err != nil {
		t.Fatalf("verify: %v %v", ok, err)
	}
	if ok, _ := h.Verify("wrong", enc); ok {
		t.Fatal("wrong password verified")
	}
	if h.NeedsRehash(enc) {
		t.Fatal("same params need rehash")
	}
	if !(&Hasher{Params: DefaultArgon2Params}).NeedsRehash(enc) {
		t.Fatal("weaker params must need rehash")
	}
	other, _ := h.Hash("correct horse battery")
	if other == enc {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "$bcrypt$x", "$argon2id$v=19$m=x$a$b", "$argon2id$v=18$m=64,t=1,p=1$YQ$YQ"} {
		if _, err := h.Verify("x", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	for _, ok := range []string{"twelve chars", "passwordpassword", strings.Repeat("ab", 128), "àèìòùàèìòùàè"} {
		if err := ValidatePassword(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "short", "elevenchars", strings.Repeat("a", 20), strings.Repeat("ab", 129), "\xff\xfe" + strings.Repeat("a", 12)} {
		if err := ValidatePassword(bad); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTOTP_RFC6238Vectors(t *testing.T) {
	// RFC 6238 appendix B, SHA1 seed "12345678901234567890"; the 8-digit codes end with these 6 digits.
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	vectors := map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"}
	for unix, want := range vectors {
		got, err := totpCode(secret, totpStep(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("t=%d: got %s want %s (%v)", unix, got, want, err)
		}
	}
	now := time.Unix(1111111111, 0)
	code, _ := totpCode(secret, totpStep(now)-1)
	if step, ok := matchTOTP(secret, code, now); !ok || step != totpStep(now)-1 {
		t.Fatal("previous step not accepted")
	}
	old, _ := totpCode(secret, totpStep(now)-2)
	if _, ok := matchTOTP(secret, old, now); ok {
		t.Fatal("code two steps old accepted")
	}
	if _, ok := matchTOTP(secret, "12345", now); ok {
		t.Fatal("short code accepted")
	}
	u := otpauthURL(secret, "anna@example.test")
	if !strings.HasPrefix(u, "otpauth://totp/Porto:anna@example.test?") || !strings.Contains(u, "issuer=Porto") {
		t.Fatalf("url %s", u)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil || len(codes) != 10 || len(hashes) != 10 {
		t.Fatal(codes, err)
	}
	for i, c := range codes {
		if len(c) != 9 || c[4] != '-' {
			t.Errorf("format %q", c)
		}
		if hashRecoveryCode(strings.ToLower(strings.ReplaceAll(c, "-", " "))) != hashes[i] {
			t.Errorf("normalisation of %q", c)
		}
	}
	if normalizeCrockford("il-o0") != "1100" {
		t.Fatal("crockford aliases")
	}
}

func TestLoginThrottle(t *testing.T) {
	c := clock.NewFake(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	th := NewLoginThrottle(c)
	ip := netip.MustParseAddr("203.0.113.7")
	for range 5 {
		if ok, _ := th.Check(ip, "Anna@example.test"); !ok {
			t.Fatal("blocked early")
		}
		th.Failure(ip, "anna@example.test")
		th.Release("anna@example.test")
	}
	ok, wait := th.Check(ip, "anna@example.test")
	if ok || wait <= 0 || wait > 15*time.Minute {
		t.Fatalf("not blocked after 5 failures: %v %v", ok, wait)
	}
	if ok, _ := th.Check(ip, "bruno@example.test"); !ok {
		t.Fatal("other account blocked")
	}
	th.Release("bruno@example.test")
	c.Advance(15*time.Minute + time.Second)
	if ok, _ := th.Check(ip, "anna@example.test"); !ok {
		t.Fatal("still blocked after the window")
	}
	th.Success("anna@example.test")
	th.Release("anna@example.test")

	// Attempts in flight count against the account limit until released.
	c.Advance(time.Hour)
	for i := range 5 {
		if ok, _ := th.Check(ip, "carla@example.test"); !ok {
			t.Fatalf("in-flight attempt %d blocked", i)
		}
	}
	if ok, _ := th.Check(ip, "carla@example.test"); ok {
		t.Fatal("6th concurrent attempt allowed")
	}
	for range 5 {
		th.Release("carla@example.test")
	}
	if ok, _ := th.Check(ip, "carla@example.test"); !ok {
		t.Fatal("blocked after releases")
	}
	th.Release("carla@example.test")

	// 30 attempts per minute per /24.
	c.Advance(time.Hour)
	for i := range 30 {
		if ok, _ := th.Check(netip.MustParseAddr("198.51.100."+string(rune('1'+i%9))), "x@y.z"); !ok {
			t.Fatalf("attempt %d blocked", i)
		}
		th.Release("x@y.z")
	}
	if ok, _ := th.Check(netip.MustParseAddr("198.51.100.200"), "other@y.z"); ok {
		t.Fatal("31st attempt from the same /24 allowed")
	}
	if ok, _ := th.Check(netip.MustParseAddr("198.51.101.1"), "other@y.z"); !ok {
		t.Fatal("other /24 blocked")
	}
}

func TestIPPrefixKey(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":         "203.0.113.0/24",
		"::ffff:203.0.113.7":  "203.0.113.0/24",
		"2001:db8:1:2:3::1":   "2001:db8:1::/56",
		"2001:db8:1:ff:3::1":  "2001:db8:1::/56",
		"2001:db8:1:100:3::1": "2001:db8:1:100::/56",
	}
	for in, want := range cases {
		if got := IPPrefixKey(netip.MustParseAddr(in)); got != want {
			t.Errorf("%s: %s want %s", in, got, want)
		}
	}
}

func TestParseToken(t *testing.T) {
	if p, ok := parseToken("po_live_abcd1234_secretsecret", APIKeyPrefix); !ok || p != "po_live_abcd1234" {
		t.Fatal(p, ok)
	}
	for _, bad := range []string{"", "po_live_", "po_live_abcd1234", "po_live_abcd12345secret", "po_dev_abcd1234_s"} {
		if _, ok := parseToken(bad, APIKeyPrefix); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if e, err := NormalizeEmail("  Anna@Example.TEST "); err != nil || e != "anna@example.test" {
		t.Fatal(e, err)
	}
	for _, bad := range []string{"", "a", "anna", "Anna <a@b.c>", "a@b.c, d@e.f", strings.Repeat("a", 251) + "@b.c"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRateLimitError_Is(t *testing.T) {
	var err error = &RateLimitError{RetryAfter: time.Minute}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatal("not ErrRateLimited")
	}
	v := &ValidationError{}
	if v.Err() != nil {
		t.Fatal("empty validation error")
	}
	v.Add("email", "invalid")
	if !errors.Is(v.Err(), ErrInvalidInput) {
		t.Fatal("not ErrInvalidInput")
	}
}
