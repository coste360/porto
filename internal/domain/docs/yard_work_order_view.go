package docs

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// The yard work order document (M10, kind yard_work_order, m10-plan §5; owned by ORDERS): the work sheet of an
// order with its budget, its tasks and, when present, the final account. The data source is the workorder service
// (registered at wiring); the view lives here with the field catalogue, the demo data and the built-in templates
// (defaults/{it,en}/yard_work_order.md).

// SubjectWorkOrder is generated_documents.subject_type of work orders.
const SubjectWorkOrder SubjectType = "work_order"

// YardWorkOrderInfo are the data of a work order.
type YardWorkOrderInfo struct {
	Number            string        `json:"number" doc:"number,fields.yard_work_order.number"`
	Status            string        `json:"status" doc:"status,fields.yard_work_order.status"`
	BillingMode       string        `json:"billing_mode" doc:"billing_mode,fields.yard_work_order.billing_mode"`
	QuoteNumber       string        `json:"quote_number" doc:"quote_number,fields.yard_work_order.quote_number"`
	PlannedStartOn    calendar.Date `json:"planned_start_on" doc:"planned_start_on,fields.yard_work_order.planned_start_on"`
	PlannedDeliveryOn calendar.Date `json:"planned_delivery_on" doc:"planned_delivery_on,fields.yard_work_order.planned_delivery_on"`
	Position          string        `json:"position" doc:"position,fields.yard_work_order.position"`
	Completion        views.BP      `json:"completion_bp" doc:"completion,fields.yard_work_order.completion"`
	Notes             views.Notes   `json:"notes" doc:"notes,fields.yard_work_order.notes"`
}

// YardWorkOrderLine is a budget line of a work order.
type YardWorkOrderLine struct {
	Seq         int         `json:"seq" doc:"seq,fields.yard_work_order.line_seq"`
	Description string      `json:"description" doc:"description,fields.yard_work_order.line_description"`
	Qty         string      `json:"qty" doc:"qty,fields.yard_work_order.line_qty"` // formatted quantity
	UnitPrice   money.Cents `json:"unit_price_cents" doc:"unit_price,fields.yard_work_order.line_unit_price"`
	Net         money.Cents `json:"net_cents" doc:"net,fields.yard_work_order.line_net"`
}

// YardWorkOrderTask is a task of a work order.
type YardWorkOrderTask struct {
	Seq            int      `json:"seq" doc:"seq,fields.yard_work_order.task_seq"`
	Title          string   `json:"title" doc:"title,fields.yard_work_order.task_title"`
	EstimatedHours string   `json:"estimated_hours" doc:"estimated_hours,fields.yard_work_order.task_hours"`
	Progress       views.BP `json:"progress_bp" doc:"progress,fields.yard_work_order.task_progress"`
	Status         string   `json:"status" doc:"status,fields.yard_work_order.task_status"`
	Checklist      []string `json:"checklist" doc:"checklist,fields.yard_work_order.task_checklist"`
}

// YardWorkOrderView is the data of kind yard_work_order.
type YardWorkOrderView struct {
	Marina   views.Marina        `json:"marina" doc:"marina,fields.groups.marina"`
	Customer views.Party         `json:"customer" doc:"customer,fields.customer"`
	Vessel   views.Vessel        `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Order    YardWorkOrderInfo   `json:"order" doc:"order,fields.groups.yard_work_order"`
	Lines    []YardWorkOrderLine `json:"lines" doc:"lines,fields.yard_work_order.lines"`
	Tasks    []YardWorkOrderTask `json:"tasks" doc:"tasks,fields.yard_work_order.tasks"`
	Net      money.Cents         `json:"net_cents" doc:"net,fields.yard_work_order.net"`
	Revenue  money.Cents         `json:"revenue_cents" doc:"revenue,fields.yard_work_order.revenue"` // final account revenue
	Charged  money.Cents         `json:"charged_cents" doc:"charged,fields.yard_work_order.charged"`
}

func init() {
	kindTitles["it"][KindYardWorkOrder] = "Commessa di cantiere"
	kindTitles["en"][KindYardWorkOrder] = "Yard work order"
}

// yardWorkOrderDemo is the demo data of kind yard_work_order: the antifouling job of m10-plan §4.4 in progress.
func yardWorkOrderDemo() YardWorkOrderView {
	return YardWorkOrderView{
		Marina: demoMarina, Customer: demoCustomer, Vessel: demoVessel,
		Order: YardWorkOrderInfo{
			Number: "C2026-000034", Status: "in_progress", BillingMode: "actual", QuoteNumber: "PC2026-000012",
			PlannedStartOn: calendar.MustParse("2027-02-01"), PlannedDeliveryOn: calendar.MustParse("2027-02-12"), Position: "Piazzale P3",
			Completion: 6000,
		},
		Lines: []YardWorkOrderLine{
			{Seq: 1, Description: "Alaggio", Qty: "12", UnitPrice: 1800, Net: 21600},
			{Seq: 2, Description: "Lavaggio carena", Qty: "56,1", UnitPrice: 400, Net: 22440},
			{Seq: 3, Description: "Ora operaio", Qty: "8,4", UnitPrice: 3800, Net: 31920},
			{Seq: 4, Description: "Ora verniciatore", Qty: "5,6", UnitPrice: 4500, Net: 25200},
			{Seq: 5, Description: "Antivegetativa 2,5 l", Qty: "5", UnitPrice: 9500, Net: 47500},
			{Seq: 6, Description: "Varo", Qty: "12", UnitPrice: 1800, Net: 21600},
		},
		Tasks: []YardWorkOrderTask{
			{Seq: 1, Title: "Carteggiatura", EstimatedHours: "8,4", Progress: 10000, Status: "done", Checklist: []string{"Carena"}},
			{Seq: 2, Title: "Antivegetativa", EstimatedHours: "5,6", Progress: 0, Status: "todo", Checklist: []string{}},
		},
		Net: 170260, Revenue: 0, Charged: 62315,
	}
}
