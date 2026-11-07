package payment

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Auto-match strategies.
const AutoMatchOldestDueFirst = "oldest_due_first"

// MatchInput is one match of a payment to a charge.
type MatchInput struct {
	ChargeID string
	Amount   money.Cents
}

// RecordInput is a payment to record (m5-plan §4.2).
type RecordInput struct {
	CustomerID     string // empty only for walk-in counter sales
	Kind           string // payment, deposit, credit
	Method         string
	Source         string // manual, provider, backfill, adjustment, goodwill, release, mandate, opening_balance
	Amount         money.Cents
	ReceivedOn     calendar.Date // zero = today in the marina time zone
	Reference      string        // encrypted at rest (AAD payment-ref:<id>), never logged
	Notes          string
	Matches        []MatchInput
	AutoMatch      string // "" or oldest_due_first
	CashRegisterID string
	IdempotencyKey string
	SendReceipt    bool
	// Provider payments (source provider).
	ProviderCode string
	ProviderRef  string
	IntentID     string
	Fee          money.Cents
	// Credit sources.
	AdjustmentChargeID string
	ReleaseID          string
	MandateID          string
}

// RefundInput is a refund of a payment (m5-plan §4.3).
type RefundInput struct {
	Amount         money.Cents
	ViaProvider    bool
	Method         string
	Reason         string
	Unmatch        []UnmatchInput
	CashRegisterID string
	IdempotencyKey string
}

// UnmatchInput reverses (part of) a match before a refund.
type UnmatchInput struct {
	MatchID string
	Amount  money.Cents
}

// CreditInput is a goodwill or release credit (m5-plan §4.4).
type CreditInput struct {
	Amount    money.Cents
	Reason    string
	Source    string // goodwill, release
	ReleaseID string
}
