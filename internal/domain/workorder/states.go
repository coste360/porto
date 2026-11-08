package workorder

// The state table of work orders (m10-plan §5.2), pure data. Every transition writes audit and the event
// work_order.status_changed in the same transaction. invoiced means "final charge issued" (D18); the final billing
// moves completed or delivered orders to invoiced (ActionInvoice, charges.manage), deliver on an invoiced order sets
// delivered_at only.

// Work order statuses (work_orders.status; the same values as the store.WorkOrder* constants, kept here so that
// the table stays pure: no store import).
const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusOnHold     = "on_hold"
	StatusCompleted  = "completed"
	StatusDelivered  = "delivered"
	StatusInvoiced   = "invoiced"
	StatusCancelled  = "cancelled"
)

// Actions of transitionWorkOrder plus the internal invoice action of the final billing.
const (
	ActionStart    = "start"
	ActionHold     = "hold"
	ActionResume   = "resume"
	ActionComplete = "complete"
	ActionReopen   = "reopen"
	ActionDeliver  = "deliver"
	ActionCancel   = "cancel"
	ActionInvoice  = "invoice" // final billing only, never through transitionWorkOrder
)

// Transition is one allowed (from, action) pair and its target status.
type Transition struct {
	From   string
	Action string
	To     string
}

// transitions is the complete table; anything else is ErrInvalidTransition.
var transitions = []Transition{
	{StatusPlanned, ActionStart, StatusInProgress},
	{StatusInProgress, ActionHold, StatusOnHold},
	{StatusOnHold, ActionResume, StatusInProgress},
	{StatusInProgress, ActionComplete, StatusCompleted},
	{StatusCompleted, ActionReopen, StatusInProgress},
	{StatusCompleted, ActionDeliver, StatusDelivered},
	{StatusInvoiced, ActionDeliver, StatusInvoiced},
	{StatusCompleted, ActionInvoice, StatusInvoiced},
	{StatusDelivered, ActionInvoice, StatusInvoiced},
	{StatusPlanned, ActionCancel, StatusCancelled},
	{StatusInProgress, ActionCancel, StatusCancelled},
	{StatusOnHold, ActionCancel, StatusCancelled},
}

// Transitions returns a copy of the state table.
func Transitions() []Transition { return append([]Transition(nil), transitions...) }

// Next returns the status reached by action from status, or false when the action is not allowed.
func Next(from, action string) (string, bool) {
	for _, t := range transitions {
		if t.From == from && t.Action == action {
			return t.To, true
		}
	}
	return "", false
}

// Open reports whether an order in status still takes work (time entries, materials, tasks): planned,
// in_progress, on_hold, completed. Late entries after invoiced are handled by the final account (§10.3).
func Open(status string) bool {
	switch status {
	case StatusPlanned, StatusInProgress, StatusOnHold, StatusCompleted:
		return true
	}
	return false
}

// Billable reports whether the final billing may run in status (completed or delivered).
func Billable(status string) bool {
	return status == StatusCompleted || status == StatusDelivered
}
