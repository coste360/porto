// Package onlineconf loads, validates and publishes the online booking settings of a marina and its versioned
// texts (terms, privacy, marketing) (spec 23; m8-plan §3.1, §11). The settings are one row per marina
// (online_booking_settings, If-Match); a marina that never saved them gets Defaults(). Published texts are
// immutable (booking_texts, trigger).
//
// The desk side of the online quotas (configuration per berth group, usage table) lives here too.
package onlineconf

import (
	"errors"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
)

// Payment modes, refund kinds and berth reveal policies.
const (
	PaymentDeposit = "deposit"
	PaymentFull    = "full"

	RefundAsRefund = "refund"
	RefundAsCredit = "credit"

	RevealNever             = "never"
	RevealAfterConfirmation = "after_confirmation"
)

// Limits of the settings (m8-plan §3.1).
const (
	MaxNightsLimit      = 366
	MaxAdvanceDaysLimit = 1095
	MaxApprovalHours    = 144
	MaxTiers            = 10
)

// ErrNotImplemented is returned by the methods ACCOUNT has not landed yet.
var ErrNotImplemented = errors.New("onlineconf: not implemented")

// ErrTextOutdated means a consent names a text version that is not the current published one (409 text_outdated).
var ErrTextOutdated = errors.New("onlineconf: text outdated")

// LongText is a localised long text (booking texts, arrival information); it is the source language.
type LongText struct {
	It string `json:"it"`
	En string `json:"en,omitempty"`
}

// IsZero reports whether the text is empty.
func (t LongText) IsZero() bool { return t.It == "" && t.En == "" }

// Tier is one cancellation tier: cancelling at least MinDaysBefore days before arrival refunds RefundBP basis
// points of the paid amount. Tiers are kept sorted by MinDaysBefore descending.
type Tier struct {
	MinDaysBefore int `json:"min_days_before"`
	RefundBP      int `json:"refund_bp"`
}

// Approval are the rules that put an online booking in awaiting_approval.
type Approval struct {
	LOAOverCm *geo.Cm  `json:"loa_over_cm"`
	GroupIDs  []string `json:"group_ids"`
	Always    bool     `json:"always"`
	HoldHours int      `json:"hold_hours"`
}

// RequiredFields are the data the details step requires.
type RequiredFields struct {
	Documents      []string `json:"documents"` // booking document types
	PeopleOnBoard  bool     `json:"people_on_board"`
	Phone          bool     `json:"phone"`
	DocumentsLater bool     `json:"documents_later"` // m8-plan §16 D9
}

// ArrivalInfo is shown on the confirmation and in the customer area.
type ArrivalInfo struct {
	VHFChannel string   `json:"vhf_channel,omitempty"`
	Phone      string   `json:"phone,omitempty"` // marina contact, not personal data
	RulesURL   string   `json:"rules_url,omitempty"`
	Text       LongText `json:"text,omitzero"`
}

// Settings are the online booking settings of a marina (m8-plan §3.1).
type Settings struct {
	MinNights            int
	MaxNights            int
	MinAdvanceHours      int
	MaxAdvanceDays       int
	ArrivalFrom          string // HH:MM marina time
	ArrivalTo            string
	ClosedWeekdays       []time.Weekday
	PaymentMode          string // deposit, full
	DepositBP            int
	CancellationTiers    []Tier
	RefundAs             string // refund, credit
	Approval             Approval
	RequiredFields       RequiredFields
	RevealBerth          string // never, after_confirmation
	LatePaymentRecovery  bool
	AllowWithoutProvider bool
	MaxLiveHolds         int
	ArrivalInfo          ArrivalInfo
	ThemePreset          string
	HideHeaderInEmbed    bool
	ShowFeet             bool

	// Version is the row version (0 = never saved: Defaults).
	Version int
}

// Saved reports whether the settings come from a stored row.
func (s Settings) Saved() bool { return s.Version > 0 }

// Defaults returns the settings of a marina that never saved them (m8-plan §3.1, values in brackets).
func Defaults() Settings {
	return Settings{
		MinNights: 1, MaxNights: 30, MinAdvanceHours: 2, MaxAdvanceDays: 365,
		ArrivalFrom: "12:00", ArrivalTo: "20:00", ClosedWeekdays: []time.Weekday{},
		PaymentMode: PaymentFull, DepositBP: 3000,
		CancellationTiers: []Tier{{MinDaysBefore: 7, RefundBP: 10000}, {MinDaysBefore: 2, RefundBP: 5000}, {MinDaysBefore: 0, RefundBP: 0}},
		RefundAs:          RefundAsRefund,
		Approval:          Approval{GroupIDs: []string{}, HoldHours: 48},
		RequiredFields:    RequiredFields{Documents: []string{"insurance_policy"}, PeopleOnBoard: true, Phone: true},
		RevealBerth:       RevealAfterConfirmation, LatePaymentRecovery: true, MaxLiveHolds: 50,
		ThemePreset: "default", ShowFeet: true,
	}
}

// Text is one published version of a booking text.
type Text struct {
	ID          string
	Kind        string // terms, privacy, marketing
	Version     int
	Texts       LongText
	SHA256      string
	PublishedAt time.Time
	PublishedBy string // user id, "" when unknown
}

// QuotaNight is the online quota usage of one group and night (desk view).
type QuotaNight struct {
	Night     calendar.Date
	Cap, Used int
}
