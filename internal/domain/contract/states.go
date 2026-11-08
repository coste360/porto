package contract

import "github.com/coste360/porto/internal/domain/event"

// Status is a contract, transit or quote status. Contracts and transits share contracts.status (the CHECK of
// 00005_contracts ties each status to its kind); quotes have their own column.
type Status string

// Mooring and charter contract statuses (m3-plan §4.1; M4 reaches awaiting_signature, renewed and suspended,
// m4-plan §3.1).
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

// Transit statuses (m3-plan §4.2). pending_payment is reached from M5 (payment link, m5-plan §8.1);
// awaiting_approval from M8 (approval flow, m8-plan §4.1). StatusConfirmed is assigned only by confirmTx, called by the manual
// driver or by the provider webhook (INV-09).
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

	// M4 (m4-plan §3.1).
	ActionRequestSignature Action = "request_signature"
	ActionSign             Action = "sign" // only from signature.Subject.OnSignedTx
	ActionSignatureFailed  Action = "signature_failed"
	ActionSuspend          Action = "suspend"
	ActionResume           Action = "resume"
	// ActionExpireRenewed is the expiry job on a contract whose renewal successor is active: active → renewed.
	ActionExpireRenewed Action = "expire_renewed"
	ActionTacitRenew    Action = "tacit_renew"
	ActionAddendum      Action = "addendum"
	ActionChangeVessel  Action = "change_vessel"

	// M5 (m5-plan §8.1): a payment link puts a held transit in pending_payment; an expired payment brings it back.
	ActionRequestPayment Action = "request_payment"
	ActionPaymentExpired Action = "payment_expired"

	// M8 (m8-plan §4.1): approval flow of online and channel bookings, and the late confirmation of an expired
	// online transit paid after its hold (only confirmTx with source provider, late_payment_recovery on).
	ActionRequestApproval   Action = "request_approval"
	ActionPaymentAuthorized Action = "payment_authorized"
	ActionApprove           Action = "approve"
	ActionReject            Action = "reject"
	ActionConfirmLate       Action = "confirm_late"
)

// Transition is one row of a state table: Events are the outbox types written by the transition, in order.
type Transition struct {
	From   Status
	Action Action
	To     Status
	Events []string
}

// MooringTransitions is the table of m3-plan §4.1 with the M4 rows of m4-plan §3.1. Anything else is
// ErrInvalidTransition. StatusRenewed is reached only from the expiry job (expire_renewed, tacit_renew);
// contract.signed is emitted only by sign and by activate (paper).
var MooringTransitions = []Transition{
	{StatusNone, ActionCreate, StatusDraft, []string{event.ContractCreated}},
	{StatusNone, ActionCreateActive, StatusActive, []string{event.ContractCreated, event.ContractActivated}},
	{StatusDraft, ActionRequestSignature, StatusAwaitingSignature, []string{event.ContractChanged}},
	{StatusDraft, ActionActivate, StatusActive, []string{event.ContractSigned, event.ContractActivated}},
	{StatusDraft, ActionCancel, StatusCancelled, []string{event.ContractChanged}},
	{StatusAwaitingSignature, ActionSign, StatusActive, []string{event.ContractSigned, event.ContractActivated}},
	{StatusAwaitingSignature, ActionActivate, StatusActive, []string{event.ContractSigned, event.ContractActivated}},
	{StatusAwaitingSignature, ActionSignatureFailed, StatusDraft, []string{event.ContractChanged}},
	{StatusAwaitingSignature, ActionCancel, StatusCancelled, []string{event.ContractChanged}},
	{StatusActive, ActionSuspend, StatusSuspended, []string{event.ContractChanged}},
	{StatusSuspended, ActionResume, StatusActive, []string{event.ContractChanged}},
	{StatusSuspended, ActionTerminate, StatusTerminated, []string{event.ContractTerminated}},
	{StatusActive, ActionTerminate, StatusTerminated, []string{event.ContractTerminated}},
	{StatusActive, ActionExpire, StatusExpired, []string{event.ContractChanged}},
	{StatusActive, ActionExpireRenewed, StatusRenewed, []string{event.ContractRenewed}},
	{StatusActive, ActionTacitRenew, StatusRenewed, []string{event.ContractRenewed}},
	{StatusDraft, ActionUpdate, StatusDraft, []string{event.ContractChanged}},
	{StatusAwaitingSignature, ActionUpdate, StatusAwaitingSignature, []string{event.ContractChanged}},
	{StatusActive, ActionUpdate, StatusActive, []string{event.ContractChanged}},
	{StatusSuspended, ActionUpdate, StatusSuspended, []string{event.ContractChanged}},
	{StatusDraft, ActionAddendum, StatusDraft, []string{event.ContractChanged}},
	{StatusAwaitingSignature, ActionAddendum, StatusAwaitingSignature, []string{event.ContractChanged}},
	{StatusActive, ActionAddendum, StatusActive, []string{event.ContractChanged}},
	{StatusSuspended, ActionAddendum, StatusSuspended, []string{event.ContractChanged}},
	{StatusDraft, ActionChangeVessel, StatusDraft, []string{event.ContractChanged}},
	{StatusAwaitingSignature, ActionChangeVessel, StatusAwaitingSignature, []string{event.ContractChanged}},
	{StatusActive, ActionChangeVessel, StatusActive, []string{event.ContractChanged}},
	{StatusSuspended, ActionChangeVessel, StatusSuspended, []string{event.ContractChanged}},
}

// CharterTransitions is the table of charter contracts (m4-plan §3.1): identical to the mooring table (charters
// are never created from quotes, so the quote acceptance path never uses it).
var CharterTransitions = MooringTransitions

// StorageTransitions is the table of storage contracts (m9-plan §3.1, D1): the mooring rows, kind storage. The
// renewal rows (expire_renewed, tacit_renew) are never used: storage has no renewal campaigns in M9.
var StorageTransitions = MooringTransitions

// RenewalProposalStatus is a status of renewal_proposals (m4-plan §3.4).
type RenewalProposalStatus string

// Renewal proposal statuses. The open ones are under renewal_proposals_open_uidx (INV-33).
const (
	ProposalPreviewed RenewalProposalStatus = "previewed"
	ProposalProposed  RenewalProposalStatus = "proposed"
	ProposalSent      RenewalProposalStatus = "sent"
	ProposalAccepted  RenewalProposalStatus = "accepted"
	ProposalSigned    RenewalProposalStatus = "signed"
	ProposalCompleted RenewalProposalStatus = "completed"
	ProposalDeclined  RenewalProposalStatus = "declined"
	ProposalExpired   RenewalProposalStatus = "expired"
	ProposalExcluded  RenewalProposalStatus = "excluded"
)

// Proposal actions (m4-plan §3.4).
const (
	ProposalActionPreview  Action = "preview"
	ProposalActionPropose  Action = "propose" // simple renewal, or include of an excluded proposal after launch
	ProposalActionExclude  Action = "exclude"
	ProposalActionLaunch   Action = "launch"
	ProposalActionSend     Action = "send"
	ProposalActionAccept   Action = "accept"
	ProposalActionSign     Action = "sign"     // successor signed or activated, deposit > 0
	ProposalActionComplete Action = "complete" // deposit settled, or successor signed with no deposit
	ProposalActionDecline  Action = "decline"
	ProposalActionExpire   Action = "expire"
	ProposalActionInclude  Action = "include"
)

// ProposalTransition is one row of the proposal table.
type ProposalTransition struct {
	From   RenewalProposalStatus
	Action Action
	To     RenewalProposalStatus
	Events []string
}

// ProposalTransitions is the table of m4-plan §3.4. "" is the creation (preview or simple renewal). Including an
// excluded proposal leads to previewed in a draft campaign and to proposed afterwards (two rows, the service picks
// by campaign state). Anything else is ErrInvalidTransition.
var ProposalTransitions = []ProposalTransition{
	{"", ProposalActionPreview, ProposalPreviewed, []string{event.RenewalCampaignChanged}},
	{"", ProposalActionPropose, ProposalProposed, []string{event.RenewalProposed}},
	{"", ProposalActionExclude, ProposalExcluded, nil},
	{ProposalPreviewed, ProposalActionLaunch, ProposalProposed, []string{event.RenewalProposed}},
	{ProposalProposed, ProposalActionSend, ProposalSent, nil},
	{ProposalSent, ProposalActionSend, ProposalSent, nil}, // resend rotates the link
	{ProposalSent, ProposalActionAccept, ProposalAccepted, []string{event.RenewalAccepted}},
	{ProposalAccepted, ProposalActionSign, ProposalSigned, nil},
	{ProposalAccepted, ProposalActionComplete, ProposalCompleted, []string{event.RenewalCompleted}},
	{ProposalSigned, ProposalActionComplete, ProposalCompleted, []string{event.RenewalCompleted}},
	{ProposalProposed, ProposalActionDecline, ProposalDeclined, []string{event.RenewalDeclined}},
	{ProposalSent, ProposalActionDecline, ProposalDeclined, []string{event.RenewalDeclined}},
	{ProposalPreviewed, ProposalActionExpire, ProposalExpired, []string{event.RenewalExpired}},
	{ProposalProposed, ProposalActionExpire, ProposalExpired, []string{event.RenewalExpired}},
	{ProposalSent, ProposalActionExpire, ProposalExpired, []string{event.RenewalExpired}},
	{ProposalAccepted, ProposalActionExpire, ProposalExpired, []string{event.RenewalExpired}},
	{ProposalExcluded, ProposalActionInclude, ProposalPreviewed, nil},
	{ProposalExcluded, ProposalActionPropose, ProposalProposed, []string{event.RenewalProposed}},
}

// NextProposal returns the transition of the proposal table for (from, action), or a *TransitionError.
func NextProposal(from RenewalProposalStatus, action Action) (ProposalTransition, error) {
	for _, t := range ProposalTransitions {
		if t.From == from && t.Action == action {
			return t, nil
		}
	}
	return ProposalTransition{}, &TransitionError{From: string(from), Action: action}
}

// CampaignStatus is a status of renewal_campaigns (m4-plan §3.4).
type CampaignStatus string

// Campaign statuses.
const (
	CampaignDraft     CampaignStatus = "draft"
	CampaignSending   CampaignStatus = "sending"
	CampaignOpen      CampaignStatus = "open"
	CampaignClosed    CampaignStatus = "closed"
	CampaignDiscarded CampaignStatus = "discarded"
)

// Campaign actions.
const (
	CampaignActionCreate  Action = "create"
	CampaignActionUpdate  Action = "update"
	CampaignActionPreview Action = "preview"
	CampaignActionLaunch  Action = "launch"
	CampaignActionOpen    Action = "open" // sync job: every proposal sent, excluded or failed
	CampaignActionClose   Action = "close"
	CampaignActionDiscard Action = "discard"
)

// CampaignTransition is one row of the campaign table.
type CampaignTransition struct {
	From   CampaignStatus
	Action Action
	To     CampaignStatus
}

// CampaignTransitions is the table of m4-plan §3.4. Every row writes renewal_campaign.changed.
var CampaignTransitions = []CampaignTransition{
	{"", CampaignActionCreate, CampaignDraft},
	{CampaignDraft, CampaignActionUpdate, CampaignDraft},
	{CampaignDraft, CampaignActionPreview, CampaignDraft},
	{CampaignDraft, CampaignActionLaunch, CampaignSending},
	{CampaignSending, CampaignActionOpen, CampaignOpen},
	{CampaignSending, CampaignActionClose, CampaignClosed},
	{CampaignOpen, CampaignActionClose, CampaignClosed},
	{CampaignDraft, CampaignActionDiscard, CampaignDiscarded},
}

// NextCampaign returns the transition of the campaign table for (from, action), or a *TransitionError.
func NextCampaign(from CampaignStatus, action Action) (CampaignTransition, error) {
	for _, t := range CampaignTransitions {
		if t.From == from && t.Action == action {
			return t, nil
		}
	}
	return CampaignTransition{}, &TransitionError{From: string(from), Action: action}
}

// AddendumStatus is a status of contract_addenda (m4-plan §3.5).
type AddendumStatus string

// Addendum statuses.
const (
	AddendumDraft             AddendumStatus = "draft"
	AddendumAwaitingSignature AddendumStatus = "awaiting_signature"
	AddendumActive            AddendumStatus = "active"
	AddendumCancelled         AddendumStatus = "cancelled"
)

// AddendumTransitions: draft → awaiting_signature → active | cancelled (and draft → active on paper).
var AddendumTransitions = []struct {
	From   AddendumStatus
	Action Action
	To     AddendumStatus
}{
	{"", ActionCreate, AddendumDraft},
	{AddendumDraft, ActionRequestSignature, AddendumAwaitingSignature},
	{AddendumDraft, ActionActivate, AddendumActive},
	{AddendumAwaitingSignature, ActionSign, AddendumActive},
	{AddendumAwaitingSignature, ActionActivate, AddendumActive},
	{AddendumAwaitingSignature, ActionSignatureFailed, AddendumDraft},
	{AddendumDraft, ActionCancel, AddendumCancelled},
	{AddendumAwaitingSignature, ActionCancel, AddendumCancelled},
}

// InstallmentStatus is a status of installments and holder_installments (m4-plan §3.3).
type InstallmentStatus string

// Instalment statuses. Paid is set only by the settlement entry points (INV-32); partially_paid is M5.
const (
	InstallmentScheduled     InstallmentStatus = "scheduled"
	InstallmentIssued        InstallmentStatus = "issued"
	InstallmentPartiallyPaid InstallmentStatus = "partially_paid"
	InstallmentPaid          InstallmentStatus = "paid"
	InstallmentCancelled     InstallmentStatus = "cancelled"
)

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
	// M5 (m5-plan §8.1): online payment of a held transit. pending_payment → confirmed only through confirmTx
	// (provider webhook or manual driver, INV-09).
	{StatusHeld, ActionRequestPayment, StatusPendingPayment, []string{event.TransitChanged}},
	{StatusPendingPayment, ActionConfirm, StatusConfirmed, []string{event.TransitConfirmed}},
	{StatusPendingPayment, ActionPaymentExpired, StatusHeld, []string{event.TransitChanged}},
	{StatusPendingPayment, ActionExpire, StatusExpired, []string{event.TransitChanged, event.OccupancyHoldExpired}},
	{StatusPendingPayment, ActionCancel, StatusCancelled, []string{event.TransitCancelled}},
	// M8 (m8-plan §4.1): approval flow. awaiting_approval → confirmed only through confirmTx (capture webhook or
	// manual driver); expired → confirmed only through confirmTx with confirm_late (provider, online, recovery on).
	{StatusHeld, ActionRequestApproval, StatusAwaitingApproval, []string{event.TransitChanged, event.TransitApprovalRequested}},
	{StatusPendingPayment, ActionPaymentAuthorized, StatusAwaitingApproval, []string{event.TransitChanged, event.TransitApprovalRequested}},
	{StatusAwaitingApproval, ActionApprove, StatusPendingPayment, []string{event.TransitChanged}},
	{StatusAwaitingApproval, ActionConfirm, StatusConfirmed, []string{event.TransitConfirmed}},
	{StatusAwaitingApproval, ActionReject, StatusCancelled, []string{event.TransitCancelled}},
	{StatusAwaitingApproval, ActionExpire, StatusCancelled, []string{event.TransitCancelled}},
	{StatusAwaitingApproval, ActionCancel, StatusCancelled, []string{event.TransitCancelled}},
	{StatusAwaitingApproval, ActionUpdate, StatusAwaitingApproval, []string{event.TransitChanged}},
	{StatusExpired, ActionConfirmLate, StatusConfirmed, []string{event.TransitConfirmed}},
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
