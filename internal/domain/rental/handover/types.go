package handover

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Jobs (m11-plan §13.2).
const (
	// JobDeposits retries the capture or void of decided pre-authorisations (every minute).
	JobDeposits = "rental.deposits"
	// JobIdentityRetention wipes identity data and document photos after closed_at + identity_retention_days
	// (daily, audited).
	JobIdentityRetention = "rental.identity_retention"
)

// Missing steps of a handover (IncompleteError, View.Missing).
const (
	MissingIdentity    = "identity"
	MissingLicence     = "licence"
	MissingContract    = "contract"
	MissingDeposit     = "deposit"
	MissingChecklist   = "checklist"
	MissingPhotos      = "photos"
	MissingEngineHours = "engine_hours"
	MissingFuel        = "fuel"
)

// ChecklistEntry is one checklist outcome.
type ChecklistEntry struct {
	Key  string `json:"key"`
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

// IdentitySummary is the masked identity of the renter (numbers show their last 3 characters).
type IdentitySummary struct {
	DocumentType         string
	DocumentNumberMasked string
	DocumentExpiresOn    calendar.Date
	LicenceNumberMasked  string
	LicenceCategory      string
	LicenceExpiresOn     calendar.Date
	Purged               bool
}

// FileView is a handover file.
type FileView struct {
	store.RentalHandoverFile
	ContentType string
}

// View is a handover draft (Version 0 = nothing saved yet) with its files and missing steps.
type View struct {
	Handover       store.RentalHandover
	Checklist      []ChecklistEntry
	Identity       *IdentitySummary
	ContractSigned bool
	Files          []FileView
	Missing        []string
}

// ChecklistInput saves a handover draft (offline kind rental.checklist: ClientRef dedupes replays, DeviceAt is the
// device time, at most 48 h before and 5 min after the queueing).
type ChecklistInput struct {
	ClientRef   string
	DeviceAt    time.Time
	Items       []ChecklistEntry
	FuelEighths *int
	EngineHours *qty.Milli
	Refuel      *qty.Milli
	Notes       string
}

// PhotoInput is a raw photo upload.
type PhotoInput struct {
	Kind        string // id_document, licence, boat, damage
	DamageID    string
	ClientRef   string
	ContentType string
	Body        []byte
}

// IdentityInput is the renter's identity and licence typed by the operator (encrypted at rest, never logged).
type IdentityInput struct {
	DocumentType      string
	DocumentNumber    string
	DocumentExpiresOn calendar.Date
	DocumentIssuer    string
	LicenceNumber     string
	LicenceCategory   string
	LicenceExpiresOn  calendar.Date
}

// Signature drivers of the rental contract.
const (
	SignScreen = "screen"
	SignOTP    = "otp"
	SignPaper  = "paper"
)

// SignInput signs the rental contract.
type SignInput struct {
	Driver      string
	PNG         []byte // screen
	SignerName  string
	Email       string // otp
	DeviceLabel string
}

// SignatureView is the signature request of the contract.
type SignatureView struct {
	RequestID        string
	Driver           string
	Status           string
	DocumentID       string
	SignedDocumentID string
}

// PDF is a served document.
type PDF struct {
	Name  string
	Bytes []byte
}

// DepositInput takes the deposit (amount 0 = the unit's deposit).
type DepositInput struct {
	Mode         string // cash, card_pos, preauth, waived
	Amount       money.Cents
	Reason       string
	RegisterID   string
	ProviderCode string
}

// DepositView is the deposit state of a booking.
type DepositView struct {
	Mode        string
	Status      string
	Amount      money.Cents
	Retained    money.Cents
	Decision    *money.Cents
	PaymentID   string
	IntentID    string
	CheckoutURL string
	ExpiresAt   time.Time
	WaiveReason string
}

// CheckOutInput completes the check-out (zero ActualAt = now).
type CheckOutInput struct {
	ActualAt time.Time
	Note     string
}

// DamageInput records a damage.
type DamageInput struct {
	Description string
	Estimate    money.Cents
}

// DamagePatch changes a damage; Waived with a reason waives it.
type DamagePatch struct {
	Description *string
	Estimate    *money.Cents
	Waived      *bool
	WaiveReason string
}

// DamageView is a damage with its photos.
type DamageView struct {
	store.RentalDamage
	Files []FileView
}

// Deposit decisions at check-in.
const (
	DecisionRelease   = "release"
	DecisionRetainAll = "retain_all"
	DecisionRetain    = "retain"
)

// CheckInInput is the return (preview and commit).
type CheckInInput struct {
	ReturnedAt      time.Time
	Extras          []fleet.ExtraQty
	DepositDecision string
	Retain          money.Cents
	WaiveDamageIDs  []string
	WaiveReason     string
	Note            string
}

// PreviewLine is a line of the return charge.
type PreviewLine struct {
	Kind      string // late_fee, fuel, extra, damage
	ItemID    string
	ItemCode  string
	Qty       qty.Milli
	UnitCents money.Cents
	Amount    money.Cents
}

// CheckInPreview is the stateless return computation (m11-plan §5.4-§5.5).
type CheckInPreview struct {
	LateMinutes     int
	LateFee         money.Cents
	FuelLitres      qty.Milli
	Fuel            money.Cents
	Extras          money.Cents
	DamageEstimate  money.Cents
	DamageExcess    money.Cents
	ReturnTotal     money.Cents
	Deposit         money.Cents
	DepositProposal money.Cents
	Lines           []PreviewLine
}

// Quick cash targets and methods (§9.3).
const (
	TargetMain   = "main"
	TargetReturn = "return"
	TargetOpen   = "open"

	MethodCash    = "cash"
	MethodCardPOS = "card_pos"
	MethodLink    = "link"
)

// QuickCashInput collects the open rental charges in one step.
type QuickCashInput struct {
	Target     string
	Method     string
	RegisterID string
}

// QuickCashResult is the outcome of a quick cash.
type QuickCashResult struct {
	Charges           []reservation.ChargeRef
	PaymentID         string
	FiscalDocumentIDs []string
	ReceiptURL        string
	LinkURL           string
	LinkExpiresAt     time.Time
}
