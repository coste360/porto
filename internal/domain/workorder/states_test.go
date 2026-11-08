package workorder

import "testing"

// TestStates_Table: the table of m10-plan §5.2 exactly; invoiced and cancelled are final except deliver on invoiced.
func TestStates_Table(t *testing.T) {
	cases := []struct {
		from, action, to string
		ok               bool
	}{
		{StatusPlanned, ActionStart, StatusInProgress, true},
		{StatusPlanned, ActionComplete, "", false},
		{StatusInProgress, ActionHold, StatusOnHold, true},
		{StatusOnHold, ActionResume, StatusInProgress, true},
		{StatusOnHold, ActionComplete, "", false},
		{StatusInProgress, ActionComplete, StatusCompleted, true},
		{StatusCompleted, ActionReopen, StatusInProgress, true},
		{StatusCompleted, ActionDeliver, StatusDelivered, true},
		{StatusDelivered, ActionInvoice, StatusInvoiced, true},
		{StatusInvoiced, ActionDeliver, StatusInvoiced, true},
		{StatusInvoiced, ActionCancel, "", false},
		{StatusCompleted, ActionCancel, "", false},
		{StatusCancelled, ActionStart, "", false},
		{StatusOnHold, ActionCancel, StatusCancelled, true},
	}
	for _, c := range cases {
		to, ok := Next(c.from, c.action)
		if ok != c.ok || to != c.to {
			t.Errorf("%s -%s-> %q %v, want %q %v", c.from, c.action, to, ok, c.to, c.ok)
		}
	}
	if !Open(StatusCompleted) || Open(StatusInvoiced) || Open(StatusCancelled) {
		t.Error("Open")
	}
}
