package event

import "github.com/coste360/porto/internal/calendar"

// Event data payloads. They carry ids and enums only, never names, emails, phones or tokens
// (TestEvents_DataHasNoPII walks Samples).

// OrganizationData is the data of organization.* events.
type OrganizationData struct {
	OrganizationID string `json:"organization_id"`
}

// MarinaData is the data of marina.* events.
type MarinaData struct {
	MarinaID string `json:"marina_id"`
}

// MemberData is the data of member.* events. Scope is "organization" or "marina".
type MemberData struct {
	UserID string `json:"user_id"`
	Role   string `json:"role,omitempty"`
	Scope  string `json:"scope"`
}

// APIKeyData is the data of api_key.* events.
type APIKeyData struct {
	APIKeyID string `json:"api_key_id"`
}

// DeviceData is the data of device.* events.
type DeviceData struct {
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
	Kind     string `json:"kind,omitempty"`
}

// WebhookTestData is the data of webhook.test.
type WebhookTestData struct {
	EndpointID string `json:"endpoint_id"`
}

// Change values of M1 events.
const (
	ChangeCreated    = "created"
	ChangeUpdated    = "updated"
	ChangeDeleted    = "deleted"
	ChangeImported   = "imported"
	ChangeReordered  = "reordered"
	ChangeGroups     = "groups"
	ChangeSettings   = "settings"
	ChangeArchived   = "archived"
	ChangeUnarchived = "unarchived"
	ChangeConsent    = "consent"
	ChangeParties    = "parties"
	ChangeMeasured   = "measured"
	ChangeFile       = "file"
)

// MapChangedData is the data of config.map_changed. MapID and Version are empty for marina-wide changes
// (reordered, groups, settings).
type MapChangedData struct {
	MapID   string `json:"map_id,omitempty"`
	Version int    `json:"version,omitempty"`
	Change  string `json:"change"` // created, updated, deleted, imported, reordered, groups, settings
}

// CustomerData is the data of customer.created and customer.anonymized.
type CustomerData struct {
	CustomerID string `json:"customer_id"`
}

// CustomerUpdatedData is the data of customer.updated.
type CustomerUpdatedData struct {
	CustomerID string `json:"customer_id"`
	Change     string `json:"change"` // updated, archived, unarchived, consent
}

// CustomerMergedData is the data of customer.merged: the target and the merged sources.
type CustomerMergedData struct {
	CustomerID string   `json:"customer_id"`
	SourceIDs  []string `json:"source_ids"`
}

// VesselChangedData is the data of vessel.changed.
type VesselChangedData struct {
	VesselID string `json:"vessel_id"`
	Change   string `json:"change"` // created, updated, parties, measured, archived, unarchived
}

// DocumentChangedData is the data of document.changed.
type DocumentChangedData struct {
	DocumentID string `json:"document_id"`
	OwnerType  string `json:"owner_type"` // customer, vessel
	Change     string `json:"change"`     // created, updated, deleted, file
}

// DeadlineChangedData is the data of deadline.changed.
type DeadlineChangedData struct {
	DeadlineID string `json:"deadline_id"`
	Change     string `json:"change"` // updated
}

// Change values of config.pricing_changed (M2). ChangeCreated, ChangeDeleted, ChangeArchived and
// ChangeSettings above are reused.
const (
	ChangeRevenueClass = "revenue_class"
	ChangeItem         = "item"
	ChangeCategory     = "category"
	ChangeSaved        = "saved"
	ChangeActivated    = "activated"
	ChangeRenewed      = "renewed"
)

// PricingChangedData is the data of config.pricing_changed. Only the id of the changed entity is set; Change is
// one of settings, revenue_class, item, category (marina-wide catalogue changes) or created, saved, activated,
// archived, deleted, renewed (price lists). TruncatedIDs lists the predecessors whose valid_to an activation
// shortened. Never promo codes or names.
type PricingChangedData struct {
	PriceListID    string   `json:"price_list_id,omitempty"`
	ItemID         string   `json:"item_id,omitempty"`
	CategoryID     string   `json:"category_id,omitempty"`
	RevenueClassID string   `json:"revenue_class_id,omitempty"`
	Version        int      `json:"version,omitempty"`
	Change         string   `json:"change"`
	TruncatedIDs   []string `json:"truncated_ids,omitempty"`
}

// M3 payloads (m3-plan §7.5). From and To are inclusive nights (YYYY-MM-DD); the SSE message copies map_id,
// resource_ids and from/to so clients can narrow their invalidation (spec 27.4).

// Operations of occupancy.moved.
const (
	MoveOperationMove   = "move"
	MoveOperationSwap   = "swap"
	MoveOperationLayout = "layout"
	MoveOperationUndo   = "undo"
)

// Reasons of occupancy.released.
const (
	ReleasedRelease      = "release"
	ReleasedTruncate     = "truncate"
	ReleasedCancel       = "cancel"
	ReleasedBlockRemoved = "block_removed"
)

// Change values of the internal M3 events (quote.changed, contract.changed, transit.changed, inquiry.changed,
// config.occupancy_changed). ChangeCreated, ChangeUpdated, ChangeDeleted and ChangeSettings above are reused.
const (
	ChangeCancelled = "cancelled"
	ChangeExpired   = "expired"
	ChangeDeclined  = "declined"
	ChangeNoShow    = "no_show"
	ChangeStay      = "stay"
	ChangeRepriced  = "repriced"
	ChangeClosure   = "closure"
)

// OccupancyAllocatedData is the data of occupancy.allocated.
type OccupancyAllocatedData struct {
	AllocationIDs []string      `json:"allocation_ids"`
	ResourceType  string        `json:"resource_type"`
	ResourceIDs   []string      `json:"resource_ids"`
	MapID         string        `json:"map_id"`
	Kind          string        `json:"kind"`
	From          calendar.Date `json:"from"`
	To            calendar.Date `json:"to"`
	ContractID    string        `json:"contract_id,omitempty"`
}

// OccupancyMovedData is the data of occupancy.moved (moves, swaps, layout saves and their undo).
type OccupancyMovedData struct {
	AllocationIDs   []string      `json:"allocation_ids"`
	FromResourceIDs []string      `json:"from_resource_ids"`
	ResourceIDs     []string      `json:"resource_ids"`
	MapIDs          []string      `json:"map_ids"`
	MapID           string        `json:"map_id"`
	From            calendar.Date `json:"from"`
	To              calendar.Date `json:"to"`
	Operation       string        `json:"operation"` // move, swap, layout, undo
	Undo            bool          `json:"undo,omitempty"`
}

// OccupancyReleasedData is the data of occupancy.released (nights freed).
type OccupancyReleasedData struct {
	AllocationIDs []string      `json:"allocation_ids"`
	ResourceIDs   []string      `json:"resource_ids"`
	MapID         string        `json:"map_id"`
	From          calendar.Date `json:"from"`
	To            calendar.Date `json:"to"`
	Reason        string        `json:"reason"` // release, truncate, cancel, block_removed
}

// OccupancyHoldExpiredData is the data of occupancy.hold_expired.
type OccupancyHoldExpiredData struct {
	AllocationIDs []string      `json:"allocation_ids"`
	ResourceIDs   []string      `json:"resource_ids"`
	MapID         string        `json:"map_id"`
	From          calendar.Date `json:"from"`
	To            calendar.Date `json:"to"`
	QuoteID       string        `json:"quote_id,omitempty"`
	ContractID    string        `json:"contract_id,omitempty"`
}

// ReleaseData is the data of release.created and release.resold.
type ReleaseData struct {
	ReleaseID          string        `json:"release_id"`
	AllocationID       string        `json:"allocation_id"`
	ResoldAllocationID string        `json:"resold_allocation_id,omitempty"`
	ResourceIDs        []string      `json:"resource_ids"`
	MapID              string        `json:"map_id"`
	From               calendar.Date `json:"from"`
	To                 calendar.Date `json:"to"`
}

// PresenceData is the data of presence.arrived and presence.departed. Never notes or photos.
type PresenceData struct {
	PresenceID   string        `json:"presence_id"`
	VesselID     string        `json:"vessel_id"`
	AllocationID string        `json:"allocation_id,omitempty"`
	ContractID   string        `json:"contract_id,omitempty"`
	ResourceIDs  []string      `json:"resource_ids"`
	MapID        string        `json:"map_id,omitempty"`
	Night        calendar.Date `json:"night"`
}

// QuoteData is the data of quote.sent, quote.accepted and quote.expired.
type QuoteData struct {
	QuoteID    string `json:"quote_id"`
	Status     string `json:"status"`
	ContractID string `json:"contract_id,omitempty"`
}

// ContractData is the data of contract.created, contract.activated and contract.terminated.
type ContractData struct {
	ContractID string        `json:"contract_id"`
	Kind       string        `json:"kind"`
	Term       string        `json:"term,omitempty"`
	Status     string        `json:"status"`
	From       calendar.Date `json:"from"`
	To         calendar.Date `json:"to"`
}

// TransitData is the data of transit.held, .confirmed, .cancelled, .arrived and .departed. Never the
// confirmation reference.
type TransitData struct {
	ContractID         string        `json:"contract_id"`
	Status             string        `json:"status"`
	From               calendar.Date `json:"from"`
	To                 calendar.Date `json:"to"`
	ConfirmationSource string        `json:"confirmation_source,omitempty"` // manual, provider, channel (M8)
}

// QuoteChangedData is the data of quote.changed (internal).
type QuoteChangedData struct {
	QuoteID string `json:"quote_id"`
	Change  string `json:"change"` // created, updated, declined
}

// ContractChangedData is the data of contract.changed (internal).
type ContractChangedData struct {
	ContractID string `json:"contract_id"`
	Change     string `json:"change"` // updated, cancelled, expired
}

// TransitApprovalData is the data of transit.approval_requested (M8): an online or channel booking waits for the
// office's approval. Reason is loa, group, always or no_provider.
type TransitApprovalData struct {
	ContractID string `json:"contract_id"`
	Reason     string `json:"reason"`
}

// TransitChangedData is the data of transit.changed (internal).
type TransitChangedData struct {
	ContractID string `json:"contract_id"`
	Change     string `json:"change"` // updated, expired, no_show, stay
}

// InquiryChangedData is the data of inquiry.changed (internal).
type InquiryChangedData struct {
	InquiryID string `json:"inquiry_id"`
	Change    string `json:"change"` // created, updated
}

// OccupancyConfigChangedData is the data of config.occupancy_changed (internal): occupancy settings or closures.
type OccupancyConfigChangedData struct {
	ClosureID string `json:"closure_id,omitempty"`
	Version   int    `json:"version,omitempty"`
	Change    string `json:"change"` // settings, closure
}

// Change values of M4 events (contract.changed, installment.changed, rights.changed, template.changed,
// communication.changed).
const (
	ChangeSignatureRequested = "signature_requested"
	ChangeSignatureFailed    = "signature_failed"
	ChangeSuspended          = "suspended"
	ChangeResumed            = "resumed"
	ChangeAddendum           = "addendum"
	ChangeVesselChanged      = "vessel_changed"
	ChangeRotated            = "rotated"
	ChangeIssued             = "issued"
	ChangeSettled            = "settled"
	ChangeReplanned          = "replanned"
	ChangePlanned            = "planned"
	ChangeReminded           = "reminded"
	ChangePublished          = "published"
	ChangeVersion            = "version"
	ChangeScheduled          = "scheduled"
	ChangeSent               = "sent"
	ChangeLaunched           = "launched"
	ChangePreviewed          = "previewed"
	ChangeClosed             = "closed"
	ChangeDiscarded          = "discarded"
)

// ContractSignedData is the data of contract.signed: the contract became active after a signature.
type ContractSignedData struct {
	ContractID         string `json:"contract_id"`
	Kind               string `json:"kind"`
	Term               string `json:"term,omitempty"`
	Status             string `json:"status"`
	SignatureRequestID string `json:"signature_request_id,omitempty"`
	Driver             string `json:"driver"` // paper, otp, provider
}

// ContractRenewedData is the data of contract.renewed.
type ContractRenewedData struct {
	ContractID          string        `json:"contract_id"`
	SuccessorContractID string        `json:"successor_contract_id"`
	Kind                string        `json:"kind"`
	Term                string        `json:"term,omitempty"`
	From                calendar.Date `json:"from"`
	To                  calendar.Date `json:"to"`
	Tacit               bool          `json:"tacit"`
}

// InstallmentOverdueData is the data of installment.overdue.
type InstallmentOverdueData struct {
	InstallmentID string        `json:"installment_id"`
	ContractID    string        `json:"contract_id"`
	Seq           int           `json:"seq"`
	DueOn         calendar.Date `json:"due_on"`
	AmountCents   int64         `json:"amount_cents"`
}

// InstallmentChangedData is the data of installment.changed (internal). InstallmentID is empty for plan-wide
// changes.
type InstallmentChangedData struct {
	InstallmentID string `json:"installment_id,omitempty"`
	ContractID    string `json:"contract_id"`
	Change        string `json:"change"` // planned, issued, settled, cancelled, replanned, reminded
	PlanVersion   int    `json:"plan_version"`
}

// RenewalData is the data of renewal.proposed, .accepted, .declined, .expired and .completed.
type RenewalData struct {
	ProposalID       string `json:"proposal_id"`
	CampaignID       string `json:"campaign_id,omitempty"`
	SourceContractID string `json:"source_contract_id"`
	NewContractID    string `json:"new_contract_id,omitempty"`
	Status           string `json:"status"`
}

// RenewalCampaignData is the data of renewal_campaign.changed (internal).
type RenewalCampaignData struct {
	CampaignID string         `json:"campaign_id"`
	Status     string         `json:"status"`
	Counts     map[string]int `json:"counts"`
}

// RightData is the data of right.created, right.transferred and right.ended. To is the inclusive last night.
type RightData struct {
	RightID    string        `json:"right_id"`
	BerthID    string        `json:"berth_id"`
	Kind       string        `json:"kind"`
	From       calendar.Date `json:"from"`
	To         calendar.Date `json:"to"`
	NewRightID string        `json:"new_right_id,omitempty"`
	TransferID string        `json:"transfer_id,omitempty"`
}

// MandateData is the data of mandate.created and mandate.ended.
type MandateData struct {
	MandateID string        `json:"mandate_id"`
	RightID   string        `json:"right_id"`
	BerthID   string        `json:"berth_id"`
	From      calendar.Date `json:"from"`
	To        calendar.Date `json:"to"`
}

// MandateRevenueData is the data of mandate.revenue_accrued.
type MandateRevenueData struct {
	MandateID       string        `json:"mandate_id"`
	Rows            int           `json:"rows"`
	RevenueCents    int64         `json:"revenue_cents"`
	OwnerShareCents int64         `json:"owner_share_cents"`
	Through         calendar.Date `json:"through"`
}

// RunIssuedData is the data of management_fee.issued and common_charges.issued. Kind is advance or balance for
// common charges.
type RunIssuedData struct {
	RunID      string `json:"run_id"`
	Year       int    `json:"year"`
	Kind       string `json:"kind,omitempty"`
	TotalCents int64  `json:"total_cents"`
	Holders    int    `json:"holders"`
}

// RightsChangedData is the data of rights.changed (internal): tables, budgets, runs, holder instalments, concession.
type RightsChangedData struct {
	Entity string `json:"entity"` // fee_table, fee_run, millesimal_table, budget, common_run, holder_installment, concession, transfer, right
	ID     string `json:"entity_id,omitempty"`
	Change string `json:"change"`
}

// DocumentGeneratedData is the data of document.generated (internal).
type DocumentGeneratedData struct {
	DocumentID  string `json:"document_id"`
	Kind        string `json:"kind"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Status      string `json:"status"`
	Signed      bool   `json:"signed"`
}

// SignatureData is the data of signature.requested, .completed and .failed (internal).
type SignatureData struct {
	SignatureRequestID string `json:"signature_request_id"`
	SubjectType        string `json:"subject_type"`
	SubjectID          string `json:"subject_id"`
	Status             string `json:"status"`
	Driver             string `json:"driver"`
}

// TemplateChangedData is the data of template.changed (internal). Never the template body (only its hash in audit).
type TemplateChangedData struct {
	TemplateID string `json:"template_id"`
	Change     string `json:"change"` // created, updated, version, published, archived
}

// CommunicationChangedData is the data of communication.changed (internal).
type CommunicationChangedData struct {
	CampaignID string `json:"campaign_id"`
	Change     string `json:"change"` // created, updated, scheduled, sent, cancelled
}

// M5 payloads (m5-plan §7.5). Amounts are signed cents; customer_id is an id, never a name or fiscal data.

// ChargeCreatedData is the data of charge.created.
type ChargeCreatedData struct {
	ChargeID         string        `json:"charge_id"`
	OriginType       string        `json:"origin_type"`
	OriginID         string        `json:"origin_id"`
	ContractID       string        `json:"contract_id,omitempty"`
	CustomerID       string        `json:"customer_id,omitempty"`
	TotalCents       int64         `json:"total_cents"`
	IssuedOn         calendar.Date `json:"issued_on"`
	DueOn            calendar.Date `json:"due_on"`
	CorrectsChargeID string        `json:"corrects_charge_id,omitempty"`
}

// ChargePaidData is the data of charge.paid.
type ChargePaidData struct {
	ChargeID        string `json:"charge_id"`
	TotalCents      int64  `json:"total_cents"`
	PaidAmountCents int64  `json:"paid_amount_cents"`
}

// ChargeCancelledData is the data of charge.cancelled.
type ChargeCancelledData struct {
	ChargeID   string `json:"charge_id"`
	OriginType string `json:"origin_type"`
}

// ChargeExportedData is the data of charge.exported.
type ChargeExportedData struct {
	ExportID    string         `json:"export_id"`
	Kind        string         `json:"kind"` // export, reexport
	ChargeCount int            `json:"charge_count"`
	PeriodFrom  *calendar.Date `json:"period_from,omitempty"`
	PeriodTo    *calendar.Date `json:"period_to,omitempty"`
}

// ChargeInvoicedData is the data of charge.invoiced (never the invoice number).
type ChargeInvoicedData struct {
	ChargeID    string        `json:"charge_id"`
	InvoiceDate calendar.Date `json:"invoice_date"`
	Source      string        `json:"source"` // csv, manual, exporter
}

// ChargeChangedData is the data of charge.changed (internal).
type ChargeChangedData struct {
	ChargeID string `json:"charge_id"`
	Change   string `json:"change"` // updated, offset, due_changed
}

// InstallmentChargedData is the data of installment.charged. Holder is true for holder instalments (then RightID
// is set instead of ContractID).
type InstallmentChargedData struct {
	InstallmentID string `json:"installment_id"`
	Holder        bool   `json:"holder"`
	ContractID    string `json:"contract_id,omitempty"`
	RightID       string `json:"right_id,omitempty"`
	ChargeID      string `json:"charge_id"`
}

// PaymentSucceededData is the data of payment.succeeded.
type PaymentSucceededData struct {
	PaymentID      string `json:"payment_id"`
	CustomerID     string `json:"customer_id,omitempty"`
	Kind           string `json:"kind"`
	Method         string `json:"method"`
	Source         string `json:"source"`
	ProviderCode   string `json:"provider_code,omitempty"`
	AmountCents    int64  `json:"amount_cents"`
	AllocatedCents int64  `json:"allocated_cents"`
}

// PaymentFailedData is the data of payment.failed.
type PaymentFailedData struct {
	IntentID     string `json:"intent_id"`
	ProviderCode string `json:"provider_code"`
	Purpose      string `json:"purpose"`
	FailureCode  string `json:"failure_code,omitempty"`
}

// PaymentRefundedData is the data of payment.refunded.
type PaymentRefundedData struct {
	RefundID    string `json:"refund_id"`
	PaymentID   string `json:"payment_id,omitempty"`
	Kind        string `json:"kind"` // refund, payout
	AmountCents int64  `json:"amount_cents"`
	Status      string `json:"status"`
}

// PaymentChangedData is the data of payment.changed (internal).
type PaymentChangedData struct {
	PaymentID string   `json:"payment_id"`
	Change    string   `json:"change"` // matched, reversed, credit_applied
	ChargeIDs []string `json:"charge_ids"`
}

// CashChangedData is the data of cash.changed (internal).
type CashChangedData struct {
	RegisterID string `json:"register_id"`
	SessionID  string `json:"session_id,omitempty"`
	Change     string `json:"change"` // opened, movement, closed, register
}

// FiscalDocumentChangedData is the data of fiscal_document.changed (internal).
type FiscalDocumentChangedData struct {
	FiscalDocumentID string `json:"fiscal_document_id"`
	Status           string `json:"status"`
}

// PaymentRequestChangedData is the data of payment_request.changed (internal).
type PaymentRequestChangedData struct {
	RequestID   string `json:"request_id"`
	Status      string `json:"status"` // created, sent, paid, cancelled, expired
	ChargeCount int    `json:"charge_count"`
}

// ReminderChangedData is the data of reminder.changed (internal).
type ReminderChangedData struct {
	RunID  string         `json:"run_id,omitempty"`
	ItemID string         `json:"item_id,omitempty"`
	Level  string         `json:"level,omitempty"`
	Change string         `json:"change"` // run, queued, approved, dismissed
	Counts map[string]int `json:"counts,omitempty"`
}

// BillingChangedData is the data of billing.changed (internal): settings, exclusions, worklist, providers,
// mandates.
type BillingChangedData struct {
	Entity string `json:"entity"` // settings, origin_items, exclusion, work_item, provider, sepa_mandate, import
	ID     string `json:"entity_id,omitempty"`
	Change string `json:"change"`
}

// Change values of M5 events.
const (
	ChangeOffset        = "offset"
	ChangeDueChanged    = "due_changed"
	ChangeMatched       = "matched"
	ChangeReversed      = "reversed"
	ChangeCreditApplied = "credit_applied"
	ChangeOpened        = "opened"
	ChangeMovement      = "movement"
	ChangeQueued        = "queued"
	ChangeApproved      = "approved"
	ChangeDismissed     = "dismissed"
	ChangeRun           = "run"
	ChangePaid          = "paid"
)

// M9 payloads (m9-plan §15.1, D28): ids, enums, dates and counts only; meter values stay out of events.

// StorageScenarioAppliedData is the data of storage.scenario_applied.
type StorageScenarioAppliedData struct {
	ScenarioID    string   `json:"scenario_id"`
	AreaID        string   `json:"area_id"`
	Moves         int      `json:"moves"`
	AllocationIDs []string `json:"allocation_ids"`
}

// StorageMovementDoneData is the data of storage.movement_done.
type StorageMovementDoneData struct {
	MovementID   string `json:"movement_id"`
	AreaID       string `json:"area_id"`
	AllocationID string `json:"allocation_id"`
}

// MaintenanceDueData is the data of maintenance.due (public).
type MaintenanceDueData struct {
	TaskID      string `json:"task_id"`
	PlanID      string `json:"plan_id,omitempty"`
	ElementType string `json:"element_type"`
	ElementID   string `json:"element_id"`
	DueOn       string `json:"due_on"`
}

// MaintenanceStatusChangedData is the data of maintenance.status_changed (also equipment and plan changes, with
// Entity set).
type MaintenanceStatusChangedData struct {
	Entity      string `json:"entity"` // task, plan, equipment
	ID          string `json:"entity_id"`
	ElementType string `json:"element_type,omitempty"`
	ElementID   string `json:"element_id,omitempty"`
	Status      string `json:"status,omitempty"`
}

// MeterReadingRecordedData is the data of meter.reading_recorded (also meter, consumption and settings changes, with
// Entity set).
type MeterReadingRecordedData struct {
	Entity  string `json:"entity"` // reading, meter, consumption, settings
	ID      string `json:"entity_id"`
	MeterID string `json:"meter_id,omitempty"`
	Status  string `json:"status,omitempty"`
}

// M10 payloads (m10-plan §13, D34): ids, enums, dates and order amounts only; employee, customer and supplier names
// never leave the database.

// YardCatalogChangedData is the data of yard_catalog.changed.
type YardCatalogChangedData struct {
	Entity string `json:"entity"` // work_type, qualification, item_settings, variable, table, package, settings
	ID     string `json:"entity_id"`
}

// YardQuoteData is the data of yard_quote.changed, yard_quote.sent, yard_quote.accepted and yard_quote.expired.
type YardQuoteData struct {
	QuoteID     string `json:"quote_id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	CustomerID  string `json:"customer_id"`
	VesselID    string `json:"vessel_id"`
	WorkOrderID string `json:"work_order_id,omitempty"`
	GrossCents  int64  `json:"gross_cents"`
}

// WorkOrderStatusChangedData is the data of work_order.status_changed (public).
type WorkOrderStatusChangedData struct {
	WorkOrderID string `json:"work_order_id"`
	From        string `json:"from,omitempty"`
	To          string `json:"to"`
}

// WorkOrderChangedData is the data of work_order.changed (order data, lines, materials, external costs).
type WorkOrderChangedData struct {
	WorkOrderID string `json:"work_order_id"`
	Entity      string `json:"entity"` // order, line, stock_movement, external_cost
	ID          string `json:"entity_id"`
}

// WorkTaskChangedData is the data of work_task.changed.
type WorkTaskChangedData struct {
	WorkOrderID string `json:"work_order_id"`
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	ProgressBP  int    `json:"progress_bp"`
}

// TimesheetChangedData is the data of timesheet.changed.
type TimesheetChangedData struct {
	TimesheetID string `json:"timesheet_id"`
	EmployeeID  string `json:"employee_id"`
	WorkOrderID string `json:"work_order_id"`
	TaskID      string `json:"task_id,omitempty"`
	Status      string `json:"status"`
}

// WorkOrderBilledData is the data of work_order.billed.
type WorkOrderBilledData struct {
	WorkOrderID string `json:"work_order_id"`
	BillingID   string `json:"billing_id"`
	Kind        string `json:"kind"`
	ChargeID    string `json:"charge_id,omitempty"`
	GrossCents  int64  `json:"gross_cents"`
}

// YardPlanChangedData is the data of yard_plan.changed (employees, calendars, absences, assignments).
type YardPlanChangedData struct {
	Entity string   `json:"entity"` // employee, calendar, absence, assignments
	ID     string   `json:"entity_id,omitempty"`
	Dates  []string `json:"dates,omitempty"`
}

// EquipmentBookingChangedData is the data of equipment.booking_changed (public).
type EquipmentBookingChangedData struct {
	BookingID   string `json:"booking_id"`
	EquipmentID string `json:"equipment_id,omitempty"`
	Date        string `json:"date,omitempty"`
	Status      string `json:"status"`
}

// RentalData is the data of rental.changed, rental.confirmed, rental.cancelled, rental.checked_out and
// rental.returned (M11): ids, enums and dates only (INV-70). Change says what happened for rental.changed
// (created, rescheduled, extended, no_show, closed, hold_extended, block, unit, skipper, formula, settings,
// handover, damage, deposit); Entity and EntityID name a non-booking entity (unit, skipper, formula, extra,
// settings, block).
type RentalData struct {
	BookingID string `json:"booking_id,omitempty"`
	UnitID    string `json:"unit_id,omitempty"`
	SkipperID string `json:"skipper_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Change    string `json:"change,omitempty"`
	StartsOn  string `json:"starts_on,omitempty"`
	EndsOn    string `json:"ends_on,omitempty"`
	Entity    string `json:"entity,omitempty"`
	EntityID  string `json:"entity_id,omitempty"`
}

const sampleID = "01900000-0000-7000-8000-000000000000"

// Samples returns one example payload per event type.
func Samples() map[string]any {
	ids := []string{sampleID}
	night := calendar.MustParse("2027-07-01")
	return map[string]any{
		OrganizationUpdated: OrganizationData{OrganizationID: sampleID},
		MarinaCreated:       MarinaData{MarinaID: sampleID},
		MarinaUpdated:       MarinaData{MarinaID: sampleID},
		MarinaArchived:      MarinaData{MarinaID: sampleID},
		MemberAdded:         MemberData{UserID: sampleID, Role: "office", Scope: "marina"},
		MemberRoleChanged:   MemberData{UserID: sampleID, Role: "dock", Scope: "marina"},
		MemberRemoved:       MemberData{UserID: sampleID, Scope: "marina"},
		APIKeyCreated:       APIKeyData{APIKeyID: sampleID},
		APIKeyRevoked:       APIKeyData{APIKeyID: sampleID},
		DevicePaired:        DeviceData{DeviceID: sampleID, UserID: sampleID, Kind: "mobile"},
		DeviceRevoked:       DeviceData{DeviceID: sampleID, UserID: sampleID},
		WebhookTest:         WebhookTestData{EndpointID: sampleID},
		ConfigMapChanged:    MapChangedData{MapID: sampleID, Version: 2, Change: ChangeUpdated},
		CustomerCreated:     CustomerData{CustomerID: sampleID},
		CustomerAnonymized:  CustomerData{CustomerID: sampleID},
		CustomerUpdated:     CustomerUpdatedData{CustomerID: sampleID, Change: ChangeConsent},
		CustomerMerged:      CustomerMergedData{CustomerID: sampleID, SourceIDs: []string{sampleID}},
		VesselChanged:       VesselChangedData{VesselID: sampleID, Change: ChangeMeasured},
		DocumentChanged:     DocumentChangedData{DocumentID: sampleID, OwnerType: "customer", Change: ChangeFile},
		DeadlineChanged:     DeadlineChangedData{DeadlineID: sampleID, Change: ChangeUpdated},
		ConfigPricingChanged: PricingChangedData{
			PriceListID: sampleID, Version: 3, Change: ChangeActivated, TruncatedIDs: []string{sampleID},
		},
		OccupancyAllocated: OccupancyAllocatedData{
			AllocationIDs: ids, ResourceType: "berth", ResourceIDs: ids, MapID: sampleID, Kind: "transit",
			From: night, To: night.AddDays(2), ContractID: sampleID,
		},
		OccupancyMoved: OccupancyMovedData{
			AllocationIDs: ids, FromResourceIDs: ids, ResourceIDs: ids, MapIDs: ids, MapID: sampleID,
			From: night, To: night.AddDays(2), Operation: MoveOperationMove,
		},
		OccupancyReleased: OccupancyReleasedData{
			AllocationIDs: ids, ResourceIDs: ids, MapID: sampleID, From: night, To: night, Reason: ReleasedTruncate,
		},
		OccupancyHoldExpired: OccupancyHoldExpiredData{
			AllocationIDs: ids, ResourceIDs: ids, MapID: sampleID, From: night, To: night, QuoteID: sampleID,
		},
		ReleaseCreated: ReleaseData{ReleaseID: sampleID, AllocationID: sampleID, ResourceIDs: ids, MapID: sampleID, From: night, To: night},
		ReleaseResold: ReleaseData{
			ReleaseID: sampleID, AllocationID: sampleID, ResoldAllocationID: sampleID, ResourceIDs: ids, MapID: sampleID,
			From: night, To: night,
		},
		PresenceArrived:  PresenceData{PresenceID: sampleID, VesselID: sampleID, AllocationID: sampleID, ContractID: sampleID, ResourceIDs: ids, MapID: sampleID, Night: night},
		PresenceDeparted: PresenceData{PresenceID: sampleID, VesselID: sampleID, ResourceIDs: ids, Night: night},
		QuoteSent:        QuoteData{QuoteID: sampleID, Status: "sent"},
		QuoteAccepted:    QuoteData{QuoteID: sampleID, Status: "accepted", ContractID: sampleID},
		QuoteExpired:     QuoteData{QuoteID: sampleID, Status: "expired"},
		ContractCreated:  ContractData{ContractID: sampleID, Kind: "mooring", Term: "annual", Status: "draft", From: night, To: night.AddDays(364)},
		ContractActivated: ContractData{
			ContractID: sampleID, Kind: "mooring", Term: "annual", Status: "active", From: night, To: night.AddDays(364),
		},
		ContractTerminated: ContractData{
			ContractID: sampleID, Kind: "mooring", Term: "seasonal", Status: "terminated", From: night, To: night.AddDays(30),
		},
		TransitHeld:           TransitData{ContractID: sampleID, Status: "held", From: night, To: night.AddDays(2)},
		TransitConfirmed:      TransitData{ContractID: sampleID, Status: "confirmed", From: night, To: night.AddDays(2), ConfirmationSource: "manual"},
		TransitCancelled:      TransitData{ContractID: sampleID, Status: "cancelled", From: night, To: night.AddDays(2)},
		TransitArrived:        TransitData{ContractID: sampleID, Status: "arrived", From: night, To: night.AddDays(2)},
		TransitDeparted:       TransitData{ContractID: sampleID, Status: "departed", From: night, To: night.AddDays(1)},
		QuoteChanged:          QuoteChangedData{QuoteID: sampleID, Change: ChangeCreated},
		ContractChanged:       ContractChangedData{ContractID: sampleID, Change: ChangeExpired},
		TransitChanged:        TransitChangedData{ContractID: sampleID, Change: ChangeStay},
		InquiryChanged:        InquiryChangedData{InquiryID: sampleID, Change: ChangeCreated},
		ConfigOccupancyChange: OccupancyConfigChangedData{ClosureID: sampleID, Change: ChangeClosure},
		// M4
		ContractSigned: ContractSignedData{
			ContractID: sampleID, Kind: "mooring", Term: "annual", Status: "active", SignatureRequestID: sampleID, Driver: "otp",
		},
		ContractRenewed: ContractRenewedData{
			ContractID: sampleID, SuccessorContractID: sampleID, Kind: "mooring", Term: "annual", From: night,
			To: night.AddDays(365), Tacit: false,
		},
		InstallmentOverdue: InstallmentOverdueData{InstallmentID: sampleID, ContractID: sampleID, Seq: 2, DueOn: night, AmountCents: 120000},
		InstallmentChanged: InstallmentChangedData{InstallmentID: sampleID, ContractID: sampleID, Change: ChangeSettled, PlanVersion: 1},
		RenewalProposed:    RenewalData{ProposalID: sampleID, CampaignID: sampleID, SourceContractID: sampleID, NewContractID: sampleID, Status: "proposed"},
		RenewalAccepted:    RenewalData{ProposalID: sampleID, SourceContractID: sampleID, NewContractID: sampleID, Status: "accepted"},
		RenewalDeclined:    RenewalData{ProposalID: sampleID, SourceContractID: sampleID, Status: "declined"},
		RenewalExpired:     RenewalData{ProposalID: sampleID, SourceContractID: sampleID, Status: "expired"},
		RenewalCompleted:   RenewalData{ProposalID: sampleID, SourceContractID: sampleID, NewContractID: sampleID, Status: "completed"},
		RenewalCampaignChanged: RenewalCampaignData{
			CampaignID: sampleID, Status: "open", Counts: map[string]int{"sent": 300, "excluded_arrears": 5},
		},
		RightCreated:     RightData{RightID: sampleID, BerthID: sampleID, Kind: "ownership_share", From: night, To: night.AddDays(3650)},
		RightTransferred: RightData{RightID: sampleID, BerthID: sampleID, Kind: "ownership_share", From: night, To: night.AddDays(30), NewRightID: sampleID, TransferID: sampleID},
		RightEnded:       RightData{RightID: sampleID, BerthID: sampleID, Kind: "long_term_use", From: night, To: night.AddDays(30)},
		MandateCreated:   MandateData{MandateID: sampleID, RightID: sampleID, BerthID: sampleID, From: night, To: night.AddDays(61)},
		MandateEnded:     MandateData{MandateID: sampleID, RightID: sampleID, BerthID: sampleID, From: night, To: night.AddDays(20)},
		MandateRevenueAccrued: MandateRevenueData{
			MandateID: sampleID, Rows: 3, RevenueCents: 45000, OwnerShareCents: 27000, Through: night.AddDays(10),
		},
		ManagementFeeIssued: RunIssuedData{RunID: sampleID, Year: 2027, TotalCents: 12345600, Holders: 72},
		CommonChargesIssued: RunIssuedData{RunID: sampleID, Year: 2027, Kind: "advance", TotalCents: 9876543, Holders: 380},
		RightsChanged:       RightsChangedData{Entity: "millesimal_table", ID: sampleID, Change: ChangeActivated},
		DocumentGenerated: DocumentGeneratedData{
			DocumentID: sampleID, Kind: "contract_annual", SubjectType: "contract", SubjectID: sampleID, Status: "ready", Signed: true,
		},
		SignatureRequested:   SignatureData{SignatureRequestID: sampleID, SubjectType: "contract", SubjectID: sampleID, Status: "sent", Driver: "otp"},
		SignatureCompleted:   SignatureData{SignatureRequestID: sampleID, SubjectType: "contract", SubjectID: sampleID, Status: "signed", Driver: "otp"},
		SignatureFailed:      SignatureData{SignatureRequestID: sampleID, SubjectType: "addendum", SubjectID: sampleID, Status: "expired", Driver: "paper"},
		TemplateChanged:      TemplateChangedData{TemplateID: sampleID, Change: ChangePublished},
		CommunicationChanged: CommunicationChangedData{CampaignID: sampleID, Change: ChangeScheduled},
		// M5
		ChargeCreated: ChargeCreatedData{
			ChargeID: sampleID, OriginType: "installment", OriginID: sampleID, ContractID: sampleID, CustomerID: sampleID,
			TotalCents: 120000, IssuedOn: night, DueOn: night.AddDays(30),
		},
		ChargePaid:      ChargePaidData{ChargeID: sampleID, TotalCents: 120000, PaidAmountCents: 120000},
		ChargeCancelled: ChargeCancelledData{ChargeID: sampleID, OriginType: "manual"},
		ChargeExported:  ChargeExportedData{ExportID: sampleID, Kind: "export", ChargeCount: 412, PeriodFrom: &night, PeriodTo: &night},
		ChargeInvoiced:  ChargeInvoicedData{ChargeID: sampleID, InvoiceDate: night, Source: "csv"},
		ChargeChanged:   ChargeChangedData{ChargeID: sampleID, Change: ChangeOffset},
		InstallmentCharged: InstallmentChargedData{
			InstallmentID: sampleID, Holder: false, ContractID: sampleID, ChargeID: sampleID,
		},
		PaymentSucceeded: PaymentSucceededData{
			PaymentID: sampleID, CustomerID: sampleID, Kind: "payment", Method: "card_online", Source: "provider",
			ProviderCode: "stripe", AmountCents: 50000, AllocatedCents: 45000,
		},
		PaymentFailed:         PaymentFailedData{IntentID: sampleID, ProviderCode: "stripe", Purpose: "sepa_collect", FailureCode: "insufficient_funds"},
		PaymentRefunded:       PaymentRefundedData{RefundID: sampleID, PaymentID: sampleID, Kind: "refund", AmountCents: 2000, Status: "succeeded"},
		PaymentChanged:        PaymentChangedData{PaymentID: sampleID, Change: ChangeMatched, ChargeIDs: ids},
		CashChanged:           CashChangedData{RegisterID: sampleID, SessionID: sampleID, Change: ChangeClosed},
		FiscalDocumentChanged: FiscalDocumentChangedData{FiscalDocumentID: sampleID, Status: "issued"},
		PaymentRequestChanged: PaymentRequestChangedData{RequestID: sampleID, Status: "sent", ChargeCount: 3},
		ReminderChanged:       ReminderChangedData{RunID: sampleID, Change: ChangeRun, Counts: map[string]int{"sent": 12, "awaiting_approval": 1}},
		BillingChanged:        BillingChangedData{Entity: "settings", ID: sampleID, Change: ChangeUpdated},
		// M8
		TransitApprovalRequested: TransitApprovalData{ContractID: sampleID, Reason: "loa"},
		// M9
		StorageScenarioApplied: StorageScenarioAppliedData{ScenarioID: sampleID, AreaID: sampleID, Moves: 3, AllocationIDs: ids},
		StorageMovementDone:    StorageMovementDoneData{MovementID: sampleID, AreaID: sampleID, AllocationID: sampleID},
		MaintenanceDue: MaintenanceDueData{
			TaskID: sampleID, PlanID: sampleID, ElementType: "berth", ElementID: sampleID, DueOn: "2026-05-01",
		},
		MaintenanceStatusChanged: MaintenanceStatusChangedData{
			Entity: "task", ID: sampleID, ElementType: "pedestal", ElementID: sampleID, Status: "scheduled",
		},
		MeterReadingRecorded: MeterReadingRecordedData{Entity: "reading", ID: sampleID, MeterID: sampleID, Status: "valid"},
		// M10
		YardCatalogChanged: YardCatalogChangedData{Entity: "package", ID: sampleID},
		YardQuoteChanged:   YardQuoteData{QuoteID: sampleID, Kind: "job", Status: "draft", CustomerID: sampleID, VesselID: sampleID, GrossCents: 207717},
		YardQuoteSent:      YardQuoteData{QuoteID: sampleID, Kind: "job", Status: "sent", CustomerID: sampleID, VesselID: sampleID, GrossCents: 207717},
		YardQuoteAccepted: YardQuoteData{
			QuoteID: sampleID, Kind: "job", Status: "accepted", CustomerID: sampleID, VesselID: sampleID, WorkOrderID: sampleID,
			GrossCents: 207717,
		},
		YardQuoteExpired:        YardQuoteData{QuoteID: sampleID, Kind: "job", Status: "expired", CustomerID: sampleID, VesselID: sampleID},
		WorkOrderStatusChanged:  WorkOrderStatusChangedData{WorkOrderID: sampleID, From: "planned", To: "in_progress"},
		WorkOrderChanged:        WorkOrderChangedData{WorkOrderID: sampleID, Entity: "external_cost", ID: sampleID},
		WorkTaskChanged:         WorkTaskChangedData{WorkOrderID: sampleID, TaskID: sampleID, Status: "in_progress", ProgressBP: 5000},
		TimesheetChanged:        TimesheetChangedData{TimesheetID: sampleID, EmployeeID: sampleID, WorkOrderID: sampleID, Status: "submitted"},
		WorkOrderBilled:         WorkOrderBilledData{WorkOrderID: sampleID, BillingID: sampleID, Kind: "final", ChargeID: sampleID, GrossCents: 169595},
		YardPlanChanged:         YardPlanChangedData{Entity: "assignments", Dates: []string{night.String()}},
		EquipmentBookingChanged: EquipmentBookingChangedData{BookingID: sampleID, EquipmentID: sampleID, Date: night.String(), Status: "booked"},
		// M11
		RentalChanged: RentalData{
			BookingID: sampleID, UnitID: sampleID, SkipperID: sampleID, Status: "held", Change: "created",
			StartsOn: night.String(), EndsOn: night.String(),
		},
		RentalConfirmed:  RentalData{BookingID: sampleID, UnitID: sampleID, Status: "confirmed", StartsOn: night.String(), EndsOn: night.String()},
		RentalCancelled:  RentalData{BookingID: sampleID, UnitID: sampleID, Status: "cancelled", Change: "hold_expired", StartsOn: night.String(), EndsOn: night.String()},
		RentalCheckedOut: RentalData{BookingID: sampleID, UnitID: sampleID, Status: "checked_out", StartsOn: night.String(), EndsOn: night.String()},
		RentalReturned:   RentalData{BookingID: sampleID, UnitID: sampleID, Status: "returned", StartsOn: night.String(), EndsOn: night.String()},
	}
}
