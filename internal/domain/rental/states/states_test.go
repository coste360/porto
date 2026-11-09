package states

import "testing"

var allStatuses = []Status{None, Held, Confirmed, CheckedOut, Returned, Closed, Cancelled, NoShow}

var allActions = []Action{
	CreateHold, CreateConfirm, Confirm, ExtendHold, Reschedule, Extend, Cancel, HoldExpire, MarkNoShow, CheckOut, Return, Close,
}

// TestRentalStates_Table checks every allowed row of m11-plan §6 and refuses every other (status, action) pair
// (INV-97).
func TestRentalStates_Table(t *testing.T) {
	want := map[Status]map[Action]Status{
		None:       {CreateHold: Held, CreateConfirm: Confirmed},
		Held:       {Confirm: Confirmed, ExtendHold: Held, Reschedule: Held, Cancel: Cancelled, HoldExpire: Cancelled},
		Confirmed:  {Reschedule: Confirmed, Cancel: Cancelled, MarkNoShow: NoShow, CheckOut: CheckedOut},
		CheckedOut: {Extend: CheckedOut, Return: Returned},
		Returned:   {Close: Closed},
	}
	for _, from := range allStatuses {
		for _, a := range allActions {
			to, ok := Next(from, a)
			exp, allowed := want[from][a]
			if ok != allowed || to != exp {
				t.Fatalf("Next(%q, %q) = %q, %v; want %q, %v", from, a, to, ok, exp, allowed)
			}
		}
	}
	n := 0
	for _, m := range want {
		n += len(m)
	}
	if len(Table()) != n {
		t.Fatalf("table has %d rows, want %d", len(Table()), n)
	}
	for _, s := range []Status{Closed, Cancelled, NoShow} {
		if !Terminal(s) {
			t.Fatalf("%s must be terminal", s)
		}
		for _, a := range allActions {
			if _, ok := Next(s, a); ok {
				t.Fatalf("terminal %s allows %s", s, a)
			}
		}
	}
	for _, s := range []Status{Held, Confirmed, CheckedOut, Returned} {
		if Terminal(s) {
			t.Fatalf("%s is not terminal", s)
		}
	}
	for s, occ := range map[Status]bool{Held: true, Confirmed: true, CheckedOut: true, Returned: true, Closed: true, Cancelled: false, NoShow: false, None: false} {
		if Occupies(s) != occ {
			t.Fatalf("Occupies(%s) = %v", s, !occ)
		}
	}
	// Table returns a copy.
	tb := Table()
	tb[0].To = Closed
	if to, _ := Next(None, CreateHold); to != Held {
		t.Fatal("Table must return a copy")
	}
}
