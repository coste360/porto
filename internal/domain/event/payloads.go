package event

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

const sampleID = "01900000-0000-7000-8000-000000000000"

// Samples returns one example payload per event type.
func Samples() map[string]any {
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
	}
}
