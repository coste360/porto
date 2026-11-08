package account

import (
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Source is the raw data of an order as the store reads it (the order side and the yard report read it through
// their own queries): Assemble turns it into the Input of Build, so that both compute the same account.
type Source struct {
	Mode                  string
	FixedIncludesExternal bool
	OrderWorkType         string
	Units                 map[string]money.Cents // rate snapshot: unit price per item
	LabourItems           map[string]string      // rate snapshot: qualification → labour item
	SnapshotVAT           map[string]VAT         // rate snapshot: VAT of the budget items
	ItemType              map[string]string      // item id → item type
	ItemVAT               map[string]VAT         // item id → current VAT of the item
	ItemCost              map[string]money.Cents // item id → yard cost
	Lines                 []BudgetLine           // ItemType is filled from ItemType
	TaskWorkType          map[string]string      // task id → work type
	EmployeeQuals         map[string][]string    // employee id → qualification ids (sorted)
	Entries               []SourceEntry
	Movements             []SourceMovement
	External              []ExternalCost
}

// SourceEntry is an approved entry with the qualification and work type of its task.
type SourceEntry struct {
	ID              string
	EmployeeID      string
	TaskID          string
	QualificationID string
	WorkTypeID      string
	Hours           qty.Milli
	CostCents       money.Cents
	Late            bool
}

// SourceMovement is a material movement with its task.
type SourceMovement struct {
	ID        string
	TaskID    string
	ItemID    string
	Return    bool
	Qty       qty.Milli
	UnitCost  money.Cents
	UnitPrice money.Cents
	Late      bool
}

// Assemble builds the Input of Build: VAT of the snapshot over the current item VAT; entries without a task
// qualification take the first qualification of the employee with a labour item; movements take the work type of
// their task, else of the first budget line of the item (else the order's, applied by Build).
func Assemble(s Source) Input {
	vat := map[string]VAT{}
	for id, v := range s.ItemVAT {
		vat[id] = v
	}
	for id, v := range s.SnapshotVAT {
		vat[id] = v
	}
	in := Input{
		Mode: s.Mode, FixedIncludesExternal: s.FixedIncludesExternal, ItemVAT: vat, ItemCost: s.ItemCost,
		Rates: RateSnapshot{Units: s.Units, LabourItems: s.LabourItems, OrderWorkType: s.OrderWorkType},
	}
	lineWT := map[string]string{}
	for _, l := range s.Lines {
		l.ItemType = s.ItemType[l.ItemID]
		in.Budget = append(in.Budget, l)
		if _, ok := lineWT[l.ItemID]; !ok {
			lineWT[l.ItemID] = l.WorkTypeID
		}
	}
	for _, e := range s.Entries {
		q := e.QualificationID
		if q == "" {
			for _, c := range s.EmployeeQuals[e.EmployeeID] {
				if s.LabourItems[c] != "" {
					q = c
					break
				}
			}
		}
		in.Entries = append(in.Entries, ApprovedEntry{ID: e.ID, QualificationID: q, WorkTypeID: e.WorkTypeID, Hours: e.Hours, CostCents: e.CostCents, Late: e.Late})
	}
	for _, m := range s.Movements {
		wt := s.TaskWorkType[m.TaskID]
		if wt == "" {
			wt = lineWT[m.ItemID]
		}
		in.Movements = append(in.Movements, Movement{
			ID: m.ID, ItemID: m.ItemID, WorkTypeID: wt, Return: m.Return, Qty: m.Qty, UnitCost: m.UnitCost, UnitPrice: m.UnitPrice, Late: m.Late,
		})
	}
	in.External = append(in.External, s.External...)
	return in
}
