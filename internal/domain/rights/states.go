package rights

// Right statuses (berth_rights.status).
const (
	RightActive      = "active"
	RightTransferred = "transferred"
	RightEnded       = "ended"
	RightCancelled   = "cancelled"
)

// Transfer statuses (right_transfers.status): draft → awaiting_signature → completed | cancelled (draft →
// completed on paper).
const (
	TransferDraft             = "draft"
	TransferAwaitingSignature = "awaiting_signature"
	TransferCompleted         = "completed"
	TransferCancelled         = "cancelled"
)

// Mandate statuses.
const (
	MandateActive    = "active"
	MandateEnded     = "ended"
	MandateCancelled = "cancelled"
)

// Fee run statuses: draft → simulated → issued | discarded (one issued run per marina, year and table).
const (
	FeeRunDraft     = "draft"
	FeeRunSimulated = "simulated"
	FeeRunIssued    = "issued"
	FeeRunDiscarded = "discarded"
)

// Millesimal table statuses: draft → active (Σ = 1000, INV-41) → archived. Active tables are immutable.
const (
	TableDraft    = "draft"
	TableActive   = "active"
	TableArchived = "archived"
)

// Budget statuses: draft → approved → closed.
const (
	BudgetDraft    = "draft"
	BudgetApproved = "approved"
	BudgetClosed   = "closed"
)

// Common charge run statuses: draft → issued | discarded (allocations of issued runs are frozen, INV-44).
const (
	CommonRunDraft     = "draft"
	CommonRunIssued    = "issued"
	CommonRunDiscarded = "discarded"
)

// transition is one allowed (from, action) → to of a state table.
type transition struct{ from, action, to string }

// TransferTransitions, FeeRunTransitions and CommonRunTransitions are the state tables of m4-plan §4.
var (
	TransferTransitions = []transition{
		{"", "create", TransferDraft},
		{TransferDraft, "request_signature", TransferAwaitingSignature},
		{TransferDraft, "complete", TransferCompleted},
		{TransferAwaitingSignature, "sign", TransferCompleted},
		{TransferAwaitingSignature, "complete", TransferCompleted},
		{TransferAwaitingSignature, "signature_failed", TransferDraft},
		{TransferDraft, "cancel", TransferCancelled},
		{TransferAwaitingSignature, "cancel", TransferCancelled},
	}
	FeeRunTransitions = []transition{
		{"", "create", FeeRunDraft},
		{FeeRunDraft, "simulate", FeeRunSimulated},
		{FeeRunSimulated, "simulate", FeeRunSimulated},
		{FeeRunSimulated, "issue", FeeRunIssued},
		{FeeRunDraft, "discard", FeeRunDiscarded},
		{FeeRunSimulated, "discard", FeeRunDiscarded},
	}
	CommonRunTransitions = []transition{
		{"", "create", CommonRunDraft},
		{CommonRunDraft, "issue", CommonRunIssued},
		{CommonRunDraft, "discard", CommonRunDiscarded},
	}
)

// Next returns the target state of (from, action) in table, or false.
func Next(table []transition, from, action string) (string, bool) {
	for _, t := range table {
		if t.from == from && t.action == action {
			return t.to, true
		}
	}
	return "", false
}
