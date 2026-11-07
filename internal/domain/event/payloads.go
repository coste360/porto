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
	ConfirmationSource string        `json:"confirmation_source,omitempty"` // manual, provider
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
	}
}
