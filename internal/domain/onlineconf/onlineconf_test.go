package onlineconf

import (
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

func TestOnlineConf_DefaultsAreValid(t *testing.T) {
	v := Validate(Normalize(Defaults()))
	if len(v.Fields) > 0 {
		t.Fatalf("defaults invalid: %+v", v.Fields)
	}
}

func TestOnlineConf_Validate(t *testing.T) {
	loa := geo.Cm(0)
	cases := map[string]struct {
		mutate func(s *Settings)
		field  string
	}{
		"min nights":      {func(s *Settings) { s.MinNights = 0 }, "rules.min_nights"},
		"max below min":   {func(s *Settings) { s.MinNights, s.MaxNights = 10, 5 }, "rules.max_nights"},
		"max nights":      {func(s *Settings) { s.MaxNights = 400 }, "rules.max_nights"},
		"advance days":    {func(s *Settings) { s.MaxAdvanceDays = 0 }, "rules.max_advance_days"},
		"advance hours":   {func(s *Settings) { s.MinAdvanceHours, s.MaxAdvanceDays = 72, 2 }, "rules.min_advance_hours"},
		"arrival from":    {func(s *Settings) { s.ArrivalFrom = "9:00" }, "rules.arrival_from"},
		"arrival order":   {func(s *Settings) { s.ArrivalFrom, s.ArrivalTo = "18:00", "10:00" }, "rules.arrival_to"},
		"weekday":         {func(s *Settings) { s.ClosedWeekdays = []time.Weekday{7} }, "rules.closed_weekdays"},
		"all closed":      {func(s *Settings) { s.ClosedWeekdays = []time.Weekday{0, 1, 2, 3, 4, 5, 6} }, "rules.closed_weekdays"},
		"payment":         {func(s *Settings) { s.PaymentMode = "later" }, "payment_mode"},
		"deposit":         {func(s *Settings) { s.DepositBP = 0 }, "deposit_bp"},
		"no tiers":        {func(s *Settings) { s.CancellationTiers = nil }, "cancellation_tiers"},
		"tier refund":     {func(s *Settings) { s.CancellationTiers = []Tier{{0, 10001}} }, "cancellation_tiers"},
		"tier duplicate":  {func(s *Settings) { s.CancellationTiers = []Tier{{3, 100}, {3, 50}} }, "cancellation_tiers"},
		"tier increasing": {func(s *Settings) { s.CancellationTiers = []Tier{{7, 1000}, {1, 5000}} }, "cancellation_tiers"},
		"refund as":       {func(s *Settings) { s.RefundAs = "voucher" }, "refund_as"},
		"hold hours":      {func(s *Settings) { s.Approval.HoldHours = MaxApprovalHours + 1 }, "approval.hold_hours"},
		"loa":             {func(s *Settings) { s.Approval.LOAOverCm = &loa }, "approval.loa_over_cm"},
		"group id":        {func(s *Settings) { s.Approval.GroupIDs = []string{"x"} }, "approval.group_ids"},
		"document":        {func(s *Settings) { s.RequiredFields.Documents = []string{"passport"} }, "required_fields.documents"},
		"reveal":          {func(s *Settings) { s.RevealBerth = "always" }, "reveal_berth"},
		"live holds":      {func(s *Settings) { s.MaxLiveHolds = 0 }, "max_live_holds"},
		"theme":           {func(s *Settings) { s.ThemePreset = "Red Theme" }, "theme_preset"},
		"rules url":       {func(s *Settings) { s.ArrivalInfo.RulesURL = "ftp://x" }, "arrival_info.rules_url"},
	}
	for name, c := range cases {
		s := Defaults()
		c.mutate(&s)
		v := Validate(Normalize(s))
		found := false
		for _, f := range v.Fields {
			if f.Field == c.field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want error on %s, got %+v", name, c.field, v.Fields)
		}
	}
}

func TestOnlineConf_NormalizeSortsTiers(t *testing.T) {
	s := Defaults()
	s.CancellationTiers = []Tier{{0, 0}, {14, 10000}, {3, 5000}}
	s.ClosedWeekdays = []time.Weekday{3, 1, 3}
	n := Normalize(s)
	if n.CancellationTiers[0].MinDaysBefore != 14 || n.CancellationTiers[2].MinDaysBefore != 0 {
		t.Fatalf("tiers %+v", n.CancellationTiers)
	}
	if len(n.ClosedWeekdays) != 2 || n.ClosedWeekdays[0] != 1 {
		t.Fatalf("weekdays %+v", n.ClosedWeekdays)
	}
	if v := Validate(n); len(v.Fields) > 0 {
		t.Fatalf("normalized invalid: %+v", v.Fields)
	}
}

func TestOnlineConf_RowRoundTrip(t *testing.T) {
	s := Normalize(Defaults())
	loa := geo.Cm(1800)
	s.Approval.LOAOverCm = &loa
	s.ClosedWeekdays = []time.Weekday{time.Monday}
	s.ArrivalInfo = ArrivalInfo{VHFChannel: "9", Text: LongText{It: "Benvenuti"}}
	row := toRow(s)
	row.Version = 4
	back, err := fromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != 4 || *back.Approval.LOAOverCm != 1800 || back.ClosedWeekdays[0] != time.Monday ||
		back.ArrivalInfo.Text.It != "Benvenuti" || len(back.CancellationTiers) != 3 {
		t.Fatalf("round trip %+v", back)
	}
}

func TestOnlineConf_TextsAndQuotaCap(t *testing.T) {
	a, b := TextSHA256(LongText{It: "x"}), TextSHA256(LongText{It: "x", En: "y"})
	if len(a) != 64 || a == b || a != TextSHA256(LongText{It: "x"}) {
		t.Fatalf("sha %s %s", a, b)
	}
	cur := []Text{{Kind: store.BookingTextTerms, Version: 3}, {Kind: store.BookingTextPrivacy, Version: 1}}
	if tx, err := CheckConsentVersion(cur, store.BookingTextTerms, 3); err != nil || tx.Version != 3 {
		t.Fatalf("current %v", err)
	}
	if _, err := CheckConsentVersion(cur, store.BookingTextTerms, 2); !errors.Is(err, ErrTextOutdated) {
		t.Fatalf("outdated %v", err)
	}
	if _, err := CheckConsentVersion(cur, store.BookingTextMarketing, 1); !errors.Is(err, ErrTextOutdated) {
		t.Fatalf("unpublished %v", err)
	}
	for _, c := range []struct {
		kind    string
		value   int64
		members int
		want    int
	}{
		{store.QuotaCount, 3, 10, 3},
		{store.QuotaPercent, 50, 7, 3},
		{store.QuotaPercent, 100, 7, 7},
		{store.QuotaPercent, 0, 7, 0},
		{store.QuotaNone, 5, 7, -1},
	} {
		if got := QuotaCap(c.kind, c.value, c.members); got != c.want {
			t.Errorf("QuotaCap(%s, %d, %d) = %d, want %d", c.kind, c.value, c.members, got, c.want)
		}
	}
}
