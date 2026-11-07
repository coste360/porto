package collect

import "github.com/coste360/porto/internal/store"

// Reminder item statuses (m5-plan §5.5).
const (
	ItemQueued           = "queued"
	ItemAwaitingApproval = "awaiting_approval"
	ItemSkippedExcluded  = "skipped_excluded"
	ItemSkippedPaid      = "skipped_paid"
	ItemSkippedNoEmail   = "skipped_no_email"
	ItemSent             = "sent"
	ItemDismissed        = "dismissed"
)

// Payment request sources.
const (
	RequestManual   = "manual"
	RequestAuto     = "auto"
	RequestReminder = "reminder"
	RequestTransit  = "transit"
)

// Provider event statuses (payment_provider_events.status).
const (
	EventApplied  = "applied"
	EventIgnored  = "ignored"
	EventDeferred = "deferred"
)

// Store types re-exported for the HTTP layer (which never imports internal/store).
type (
	// RequestFilter narrows ListRequests.
	RequestFilter = store.PaymentRequestFilter
	// RequestPage is a page of payment requests.
	RequestPage = store.Page[RequestView]
	// Run is a reminder run.
	Run = store.ReminderRun
	// RunPage is a page of reminder runs.
	RunPage = store.Page[store.ReminderRun]
	// ItemPage is a page of reminder items.
	ItemPage = store.Page[ItemView]
	// Exclusion is a dunning exclusion.
	Exclusion = store.DunningExclusion
	// ExclusionFilter narrows ListExclusions.
	ExclusionFilter = store.DunningExclusionFilter
	// ExclusionPage is a page of exclusions.
	ExclusionPage = store.Page[store.DunningExclusion]
	// Mandate is a SEPA mandate.
	Mandate = store.SEPAMandate
)
