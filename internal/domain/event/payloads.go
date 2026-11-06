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
	}
}
