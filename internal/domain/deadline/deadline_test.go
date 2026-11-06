package deadline

import (
	"testing"

	"github.com/coste360/porto/internal/calendar"
)

func TestKindForDocumentType(t *testing.T) {
	for typ, want := range map[string]string{
		"identity_card": KindIdentityDocument, "passport": KindIdentityDocument, "driving_licence": KindIdentityDocument,
		"boating_licence": KindBoatingLicence, "navigation_licence": KindNavigationLicence,
		"registration_certificate": KindRegistrationCertificate, "insurance_policy": KindInsurance,
		"safety_certificate": KindSafetyCertificate, "power_declaration": KindPowerDeclaration,
		"proxy": KindOtherDocument, "other": KindOtherDocument, "photo": "", "signed_contract": "",
	} {
		if got := KindForDocumentType(typ); got != want {
			t.Errorf("%s: %q, want %q", typ, got, want)
		}
	}
}

func TestUrgency_Scenario(t *testing.T) {
	today := calendar.MustParse("2026-11-06")
	for _, c := range []struct {
		due  string
		lead []int
		want string
	}{
		{"2026-11-05", []int{30}, UrgencyOverdue},
		{"2026-11-06", []int{30}, UrgencyDueSoon},
		{"2026-12-06", []int{30}, UrgencyDueSoon},
		{"2026-12-07", []int{30}, UrgencyLater},
		{"2027-01-05", []int{60, 15}, UrgencyDueSoon},
		{"2027-01-06", []int{60, 15}, UrgencyLater},
	} {
		if got := Urgency(calendar.MustParse(c.due), today, c.lead); got != c.want {
			t.Errorf("due %s lead %v: %s, want %s", c.due, c.lead, got, c.want)
		}
	}
	if d := DefaultLeadDays(KindInsurance); len(d) != 2 || d[0] != 60 || d[1] != 15 {
		t.Errorf("insurance default %v", d)
	}
	if d := DefaultLeadDays(KindIdentityDocument); len(d) != 1 || d[0] != 30 {
		t.Errorf("identity default %v", d)
	}
}
