package onlineconf

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// ErrVersionMismatch means If-Match did not equal the stored version of the settings (412 version_conflict). It is
// the store sentinel, already mapped by the HTTP layer.
var ErrVersionMismatch = store.ErrVersionMismatch

// DocumentTypes are the document types the booking page may require (m8-plan §5.2).
var DocumentTypes = []string{
	"boating_licence", "navigation_licence", "insurance_policy", "registration_certificate", "identity_card",
}

// Field limits of the free texts of the settings.
const (
	maxShortText = 64
	maxURL       = 500
	maxLongText  = 4000
	maxAdvanceH  = 24 * 365
)

var (
	localTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	themeRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// Update validates and stores the settings when ifMatch equals the stored version (0 = never saved), writing the
// audit row in the same transaction (marina.configure). The approval group ids must be berth groups of the
// marina. The returned settings carry the new version.
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in Settings) (Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return Settings{}, err
	}
	in = Normalize(in)
	var out Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		v := Validate(in)
		if err := checkGroups(ctx, tx, scope, in.Approval.GroupIDs, v); err != nil {
			return err
		}
		if err := v.Err(); err != nil {
			return err
		}
		current, err := Load(ctx, tx, scope)
		if err != nil {
			return err
		}
		if current.Version != ifMatch {
			return ErrVersionMismatch
		}
		row := toRow(in)
		if err := store.SaveOnlineBookingSettings(ctx, tx, scope, ifMatch, row, s.d.Clock.Now()); err != nil {
			return err
		}
		if s.d.Audit != nil {
			if err := s.d.Audit.Write(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "online_booking_settings.update",
				EntityType: "online_booking_settings", EntityID: scope.ID(), Diff: map[string]any{
					"version": row.Version, "payment_mode": row.PaymentMode, "approval_always": in.Approval.Always,
					"tiers": len(in.CancellationTiers),
				},
			}); err != nil {
				return err
			}
		}
		out, err = fromRow(row)
		return err
	})
	if err != nil {
		return Settings{}, err
	}
	return out, nil
}

// Normalize trims the free texts, sorts the tiers by MinDaysBefore descending and the weekdays ascending, and
// replaces nil slices with empty ones.
func Normalize(in Settings) Settings {
	in.ArrivalFrom, in.ArrivalTo = strings.TrimSpace(in.ArrivalFrom), strings.TrimSpace(in.ArrivalTo)
	in.ThemePreset = strings.TrimSpace(in.ThemePreset)
	in.ClosedWeekdays = slices.Clone(in.ClosedWeekdays)
	if in.ClosedWeekdays == nil {
		in.ClosedWeekdays = []time.Weekday{}
	}
	slices.Sort(in.ClosedWeekdays)
	in.ClosedWeekdays = slices.Compact(in.ClosedWeekdays)
	in.CancellationTiers = slices.Clone(in.CancellationTiers)
	if in.CancellationTiers == nil {
		in.CancellationTiers = []Tier{}
	}
	sort.SliceStable(in.CancellationTiers, func(i, j int) bool {
		return in.CancellationTiers[i].MinDaysBefore > in.CancellationTiers[j].MinDaysBefore
	})
	in.Approval.GroupIDs = slices.Clone(in.Approval.GroupIDs)
	if in.Approval.GroupIDs == nil {
		in.Approval.GroupIDs = []string{}
	}
	slices.Sort(in.Approval.GroupIDs)
	in.Approval.GroupIDs = slices.Compact(in.Approval.GroupIDs)
	in.RequiredFields.Documents = slices.Clone(in.RequiredFields.Documents)
	if in.RequiredFields.Documents == nil {
		in.RequiredFields.Documents = []string{}
	}
	a := &in.ArrivalInfo
	a.VHFChannel, a.Phone, a.RulesURL = strings.TrimSpace(a.VHFChannel), strings.TrimSpace(a.Phone), strings.TrimSpace(a.RulesURL)
	a.Text.It, a.Text.En = strings.TrimSpace(a.Text.It), strings.TrimSpace(a.Text.En)
	return in
}

// Validate checks the settings (m8-plan §3.1) and returns the field errors (the group ids are checked against the
// database by Update).
func Validate(in Settings) *auth.ValidationError {
	v := &auth.ValidationError{}
	rng := func(field string, x, lo, hi int) {
		if x < lo || x > hi {
			v.Add(field, "out_of_range")
		}
	}
	rng("rules.min_nights", in.MinNights, 1, MaxNightsLimit)
	rng("rules.max_nights", in.MaxNights, 1, MaxNightsLimit)
	if in.MinNights > in.MaxNights {
		v.Add("rules.max_nights", "less_than_min")
	}
	rng("rules.min_advance_hours", in.MinAdvanceHours, 0, maxAdvanceH)
	rng("rules.max_advance_days", in.MaxAdvanceDays, 1, MaxAdvanceDaysLimit)
	if in.MinAdvanceHours > in.MaxAdvanceDays*24 {
		v.Add("rules.min_advance_hours", "beyond_max_advance")
	}
	fromOK, toOK := localTimeRe.MatchString(in.ArrivalFrom), localTimeRe.MatchString(in.ArrivalTo)
	if !fromOK {
		v.Add("rules.arrival_from", "invalid")
	}
	if !toOK {
		v.Add("rules.arrival_to", "invalid")
	}
	if fromOK && toOK && in.ArrivalFrom > in.ArrivalTo {
		v.Add("rules.arrival_to", "before_from")
	}
	for _, d := range in.ClosedWeekdays {
		if d < 0 || d > 6 {
			v.Add("rules.closed_weekdays", "invalid")
			break
		}
	}
	if len(in.ClosedWeekdays) >= 7 {
		v.Add("rules.closed_weekdays", "all_closed")
	}
	if in.PaymentMode != PaymentDeposit && in.PaymentMode != PaymentFull {
		v.Add("payment_mode", "invalid")
	}
	rng("deposit_bp", in.DepositBP, 1, 10000)
	validateTiers(v, in.CancellationTiers)
	if in.RefundAs != RefundAsRefund && in.RefundAs != RefundAsCredit {
		v.Add("refund_as", "invalid")
	}
	rng("approval.hold_hours", in.Approval.HoldHours, 1, MaxApprovalHours)
	if in.Approval.LOAOverCm != nil && (*in.Approval.LOAOverCm <= 0 || *in.Approval.LOAOverCm > 10000) {
		v.Add("approval.loa_over_cm", "out_of_range")
	}
	for _, id := range in.Approval.GroupIDs {
		if !store.ValidID(id) {
			v.Add("approval.group_ids", "invalid")
			break
		}
	}
	seen := map[string]bool{}
	for _, d := range in.RequiredFields.Documents {
		if !slices.Contains(DocumentTypes, d) || seen[d] {
			v.Add("required_fields.documents", "invalid")
			break
		}
		seen[d] = true
	}
	if in.RevealBerth != RevealNever && in.RevealBerth != RevealAfterConfirmation {
		v.Add("reveal_berth", "invalid")
	}
	rng("max_live_holds", in.MaxLiveHolds, 1, 10000)
	if !themeRe.MatchString(in.ThemePreset) {
		v.Add("theme_preset", "invalid")
	}
	validateArrivalInfo(v, in.ArrivalInfo)
	return v
}

func validateTiers(v *auth.ValidationError, tiers []Tier) {
	if len(tiers) == 0 {
		v.Add("cancellation_tiers", "required")
		return
	}
	if len(tiers) > MaxTiers {
		v.Add("cancellation_tiers", "too_many")
		return
	}
	days := map[int]bool{}
	for _, t := range tiers {
		switch {
		case t.MinDaysBefore < 0 || t.MinDaysBefore > MaxAdvanceDaysLimit:
			v.Add("cancellation_tiers", "invalid_days")
			return
		case t.RefundBP < 0 || t.RefundBP > 10000:
			v.Add("cancellation_tiers", "invalid_refund")
			return
		case days[t.MinDaysBefore]:
			v.Add("cancellation_tiers", "duplicate_days")
			return
		}
		days[t.MinDaysBefore] = true
	}
	// Refunds never grow when cancelling closer to arrival (tiers are sorted by MinDaysBefore descending).
	for i := 1; i < len(tiers); i++ {
		if tiers[i].MinDaysBefore < tiers[i-1].MinDaysBefore && tiers[i].RefundBP > tiers[i-1].RefundBP {
			v.Add("cancellation_tiers", "not_decreasing")
			return
		}
	}
}

func validateArrivalInfo(v *auth.ValidationError, a ArrivalInfo) {
	if utf8.RuneCountInString(a.VHFChannel) > maxShortText {
		v.Add("arrival_info.vhf_channel", "too_long")
	}
	if utf8.RuneCountInString(a.Phone) > maxShortText {
		v.Add("arrival_info.phone", "too_long")
	}
	if a.RulesURL != "" {
		u, err := url.Parse(a.RulesURL)
		if err != nil || len(a.RulesURL) > maxURL || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			v.Add("arrival_info.rules_url", "invalid")
		}
	}
	if utf8.RuneCountInString(a.Text.It) > maxLongText || utf8.RuneCountInString(a.Text.En) > maxLongText {
		v.Add("arrival_info.text", "too_long")
	}
}

// checkGroups adds a field error when an approval group is not a berth group of the marina.
func checkGroups(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string, v *auth.ValidationError) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if !store.ValidID(id) {
			return nil // already reported by Validate
		}
		if _, err := store.GetBerthGroup(ctx, db, scope, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				v.Add("approval.group_ids", "unknown")
				return nil
			}
			return err
		}
	}
	return nil
}

func toRow(in Settings) *store.OnlineBookingSettings {
	days := make([]int, 0, len(in.ClosedWeekdays))
	for _, d := range in.ClosedWeekdays {
		days = append(days, int(d))
	}
	return &store.OnlineBookingSettings{
		MinNights: in.MinNights, MaxNights: in.MaxNights, MinAdvanceHours: in.MinAdvanceHours,
		MaxAdvanceDays: in.MaxAdvanceDays, ArrivalFrom: in.ArrivalFrom, ArrivalTo: in.ArrivalTo,
		ClosedWeekdays: store.MustJSON(days), PaymentMode: in.PaymentMode, DepositBP: in.DepositBP,
		CancellationTiers: store.MustJSON(in.CancellationTiers), RefundAs: in.RefundAs,
		Approval: store.MustJSON(in.Approval), RequiredFields: store.MustJSON(in.RequiredFields),
		RevealBerth: in.RevealBerth, LatePaymentRecovery: in.LatePaymentRecovery,
		AllowWithoutProvider: in.AllowWithoutProvider, MaxLiveHolds: in.MaxLiveHolds,
		ArrivalInfo: store.MustJSON(in.ArrivalInfo), ThemePreset: in.ThemePreset,
		HideHeaderInEmbed: in.HideHeaderInEmbed, ShowFeet: in.ShowFeet,
	}
}
