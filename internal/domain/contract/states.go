package contract

import "github.com/coste360/porto/internal/domain/event"

// Status is a contract, transit or quote status. Contracts and transits share contracts.status (the CHECK of
// 00005_contracts ties each status to its kind); quotes have their own column.
type Status string

// Mooring contract statuses (m3-plan §4.1). awaiting_signature, renewed and suspended exist for M4/M5; no M3
// transition reaches them.
const (
	StatusDraft             Status = "draft"
	StatusAwaitingSignature Status = "awaiting_signature"
	StatusActive            Status = "active"
	StatusExpired           Status = "expired" // mooring and transit
	StatusRenewed           Status = "renewed"
	StatusTerminated        Status = "terminated"
	StatusCancelled         Status = "cancelled" // mooring and transit
	StatusSuspended         Status = "suspended"
)

// Transit statuses (m3-plan §4.2). pending_payment (M5) and awaiting_approval (M8) are in the CHECK only.
// StatusConfirmed is assigned only by ConfirmTransit, the manual driver entry point (INV-09).
const (
	StatusHeld             Status = "held"
	StatusPendingPayment   Status = "pending_payment"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusConfirmed        Status = "confirmed"
	StatusArrived          Status = "arrived"
	StatusDeparted         Status = "departed"
	StatusNoShow           Status = "no_show"
)

// Quote statuses (m3-plan §4.3).
const (
	QuoteDraft    Status = "draft"
	QuoteSent     Status = "sent"
	QuoteAccepted Status = "accepted"
	QuoteDeclined Status = "declined"
	QuoteExpired  Status = "expired"
)

// StatusNone is the "from" state of creations.
const StatusNone Status = ""

// Action is a state-changing operation of the contract service.
type Action string

// Actions of the transition tables.
const (
	ActionCreate       Action = "create"        // mooring draft, held transit, draft quote
	ActionCreateActive Action = "create_active" // mooring created active
	ActionActivate     Action = "activate"      // "firmato su carta"
	ActionCancel       Action = "cancel"
	ActionTerminate    Action = "terminate"
	ActionExpire       Action = "expire" // job contracts.expire
	ActionConfirm      Action = "confirm"
	ActionNoShow       Action = "no_show"
	ActionArrive       Action = "arrive" // presence arrived
	ActionDepart       Action = "depart" // presence departed
	ActionChangeStay   Action = "change_stay"
	ActionUpdate       Action = "update"
	ActionSend         Action = "send"
	ActionAccept       Action = "accept"
	ActionDecline      Action = "decline"
)

// Transition is one row of a state table: Events are the outbox types written by the transition, in order.
type Transition struct {
	From   Status
	Action Action
	To     Status
	Events []string
}

// MooringTransitions is the table of m3-plan §4.1. Anything else is ErrInvalidTransition.
var MooringTransitions = []Transition{
	{StatusNone, ActionCreate, StatusDraft, []string{event.ContractCreated}},
	{StatusNone, ActionCreateActive, StatusActive, []string{event.ContractCreated, event.ContractActivated}},
	{StatusDraft, ActionActivate, StatusActive, []string{event.ContractActivated}},
	{StatusDraft, ActionCancel, StatusCancelled, []string{event.ContractChanged}},
	{StatusDraft, ActionUpdate, StatusDraft, []string{event.ContractChanged}},
	{StatusActive, ActionUpdate, StatusActive, []string{event.ContractChanged}},
	{StatusActive, ActionTerminate, StatusTerminated, []string{event.ContractTerminated}},
	{StatusActive, ActionExpire, StatusExpired, []string{event.ContractChanged}},
}

// TransitTransitions is the table of m3-plan §4.2. Anything else is ErrInvalidTransition.
var TransitTransitions = []Transition{
	{StatusNone, ActionCreate, StatusHeld, []string{event.TransitHeld}},
	{StatusHeld, ActionConfirm, StatusConfirmed, []string{event.TransitConfirmed}},
	{StatusHeld, ActionCancel, StatusCancelled, []string{event.TransitCancelled}},
	{StatusHeld, ActionExpire, StatusExpired, []string{event.TransitChanged, event.OccupancyHoldExpired}},
	{StatusHeld, ActionArrive, StatusHeld, nil}, // presence recorded, the transit stays held (§13 D13)
	{StatusHeld, ActionChangeStay, StatusHeld, []string{event.TransitChanged}},
	{StatusHeld, ActionUpdate, StatusHeld, []string{event.TransitChanged}},
	{StatusConfirmed, ActionCancel, StatusCancelled, []string{event.TransitCancelled}},
	{StatusConfirmed, ActionNoShow, StatusNoShow, []string{event.TransitChanged}},
	{StatusConfirmed, ActionArrive, StatusArrived, []string{event.TransitArrived}},
	{StatusConfirmed, ActionChangeStay, StatusConfirmed, []string{event.TransitChanged}},
	{StatusConfirmed, ActionUpdate, StatusConfirmed, []string{event.TransitChanged}},
	{StatusArrived, ActionDepart, StatusDeparted, []string{event.TransitDeparted}},
	{StatusArrived, ActionChangeStay, StatusArrived, []string{event.TransitChanged}},
	{StatusArrived, ActionUpdate, StatusArrived, []string{event.TransitChanged}},
}

// QuoteTransitions is the table of m3-plan §4.3. Anything else is ErrInvalidTransition.
var QuoteTransitions = []Transition{
	{StatusNone, ActionCreate, QuoteDraft, []string{event.QuoteChanged}},
	{QuoteDraft, ActionUpdate, QuoteDraft, []string{event.QuoteChanged}},
	{QuoteDraft, ActionSend, QuoteSent, []string{event.QuoteSent}},
	{QuoteDraft, ActionAccept, QuoteAccepted, []string{event.QuoteAccepted}},
	{QuoteSent, ActionAccept, QuoteAccepted, []string{event.QuoteAccepted}},
	{QuoteDraft, ActionDecline, QuoteDeclined, []string{event.QuoteChanged}},
	{QuoteSent, ActionDecline, QuoteDeclined, []string{event.QuoteChanged}},
	{QuoteDraft, ActionExpire, QuoteExpired, []string{event.QuoteExpired, event.OccupancyHoldExpired}},
	{QuoteSent, ActionExpire, QuoteExpired, []string{event.QuoteExpired, event.OccupancyHoldExpired}},
}

// Next returns the transition of table for (from, action), or a *TransitionError.
func Next(table []Transition, from Status, action Action) (Transition, error) {
	for _, t := range table {
		if t.From == from && t.Action == action {
			return t, nil
		}
	}
	return Transition{}, &TransitionError{From: string(from), Action: action}
}
