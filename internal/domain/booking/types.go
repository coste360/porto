package booking

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// Domain types of the public booking page, the customer area operations on transits and the desk list (m8-plan
// §3-§7). Handlers map them to the generated API types; they never carry other customers' data.

// CategoryStandard is the category of resources without a price class (m8-plan §16 D2).
const CategoryStandard = "standard"

// Name is a short localised label (it required).
type Name struct {
	It string
	En string
}

// Category is an online category (price_class of M2 berth categories, or standard).
type Category struct {
	Code      string
	Name      Name
	MaxLOACm  geo.Cm // 0 = no limit known
	MaxBeamCm geo.Cm
}

// ServiceOption is an online service offered at checkout.
type ServiceOption struct {
	ItemID          string
	Name            Name
	Description     Name
	Unit            string
	UnitPriceCents  money.Cents // indicative
	MaxQtyMilli     int64       // 0 = no limit
	DefaultSelected bool
}

// TextRef is the current published version of a booking text.
type TextRef struct {
	Kind    string
	Version int
	Text    onlineconf.LongText
}

// Config is the public configuration of the booking page (getBookingConfig).
type Config struct {
	Enabled    bool
	Payable    bool
	Currency   string
	Settings   onlineconf.Settings
	Categories []Category
	Services   []ServiceOption
	Texts      []TextRef
}

// Vessel are the declared dimensions of the searched vessel.
type Vessel struct {
	LOACm    geo.Cm
	BeamCm   geo.Cm
	DraftCm  geo.Cm // 0 = unknown
	HullType string // "" = unknown
}

// AvailabilityInput is a public search. VesselID needs a customer session of the same customer (SessionToken).
// ClientKey is the dedupe key of lost demand (derived from the IP prefix by the handler, never stored).
type AvailabilityInput struct {
	Arrival, Departure calendar.Date
	Vessel             Vessel
	VesselID           string
	SessionToken       string
	ClientKey          string
}

// CategoryAvailability is the yes/no and "from" price of one category.
type CategoryAvailability struct {
	Code             string
	Name             Name
	Available        bool
	FromCents        money.Cents // 0 with !Available
	ApprovalRequired bool
}

// Availability is the outcome of a public search.
type Availability struct {
	Arrival, Departure calendar.Date
	Nights             int
	Reason             string // policy.Block* when the whole search is blocked
	Categories         []CategoryAvailability
}

// ServiceRequest is a service chosen at checkout.
type ServiceRequest struct {
	ItemID   string
	QtyMilli int64
}

// QuoteInput prices a stay in a category (quoteBooking; writes nothing).
type QuoteInput struct {
	Arrival, Departure calendar.Date
	Vessel             Vessel
	Category           string
	Services           []ServiceRequest
	PromoCode          string
}

// PriceLine is one line of the public breakdown.
type PriceLine struct {
	Kind       string // berth, service, discount, rounding
	Label      Name
	ItemID     string
	QtyMilli   int64
	TotalCents money.Cents
}

// Quote is the public price of a stay.
type Quote struct {
	Category          string
	Nights            int
	Lines             []PriceLine
	TotalCents        money.Cents
	NetCents          money.Cents
	VATCents          money.Cents
	DueNowCents       money.Cents
	PaymentMode       string
	CancellationTiers []onlineconf.Tier
	PromoApplied      bool
	ApprovalRequired  bool
	Currency          string
}

// CreateInput holds a berth of a category (createBooking). IdempotencyKey is required; BodySHA256 is the hash of
// the canonical body computed by the handler.
type CreateInput struct {
	Arrival, Departure calendar.Date
	Vessel             Vessel
	VesselID           string
	SessionToken       string
	Category           string
	Services           []ServiceRequest
	PromoCode          string
	Locale             string
	EmbedParentOrigin  string
	IdempotencyKey     string
	BodySHA256         string
	BotToken           string
	RemoteIP           string // for BotProtection only, never stored or logged
}

// Created is the outcome of createBooking (or its replay).
type Created struct {
	BookingID        string
	Reference        string
	BookingToken     string
	Status           string
	HoldExpiresAt    time.Time
	Quote            Quote
	ApprovalRequired bool
	AmountDueNow     money.Cents
}

// Document is a document attached to a booking.
type Document struct {
	ID         string
	Type       string
	UploadedAt time.Time
	Verified   bool
}

// VesselView is the vessel of a booking as its token holder sees it.
type VesselView struct {
	Name     string
	LOACm    geo.Cm
	BeamCm   geo.Cm
	DraftCm  geo.Cm
	HullType string
}

// Booking is the booking of the token holder (getBooking).
type Booking struct {
	BookingID        string
	Reference        string
	Status           string
	Arrival          calendar.Date
	Departure        calendar.Date
	Category         string
	Vessel           VesselView
	HoldExpiresAt    time.Time // zero when no live hold
	Quote            Quote
	ApprovalRequired bool
	ApprovalDeadline time.Time
	AmountDueNow     money.Cents
	DetailsComplete  bool
	Documents        []Document
	MissingDocuments []string
}

// CustomerDetails is the customer part of the details step. Personal data: never logged.
type CustomerDetails struct {
	FirstName, LastName string
	Email               string
	Phone               string
	Country             string
	Locale              string
}

// VesselDetails is the vessel part of the details step.
type VesselDetails struct {
	Name         string
	Registration string
	Flag         string
	HullType     string
	DraftCm      geo.Cm
}

// Consents are the accepted text versions.
type Consents struct {
	TermsVersion     int
	PrivacyVersion   int
	Marketing        bool
	MarketingVersion int
}

// DetailsInput is submitBookingDetails.
type DetailsInput struct {
	Customer            CustomerDetails
	Vessel              VesselDetails
	PeopleOnBoard       int
	ExpectedArrivalTime string
	Notes               string
	Consents            Consents
}

// DocumentUpload is an uploaded file (raw body, already size-limited by the router).
type DocumentUpload struct {
	Type        string
	ExpiresOn   calendar.Date
	FileName    string
	ContentType string
	Body        []byte
}

// PayInput is startBookingPayment.
type PayInput struct {
	IdempotencyKey string
	ReturnTo       string
}

// PayResult is the outcome of startBookingPayment.
type PayResult struct {
	Status      string
	RedirectURL string // provider hosted page; "" when sent for approval
	StatusToken string
}

// StatusView is the PII-free status of the return page.
type StatusView struct {
	BookingID     string
	Reference     string
	Status        string
	Verifying     bool
	HoldExpiresAt time.Time
}

// Tokens are the credentials a public request may carry (headers only).
type Tokens struct {
	Booking string // Porto-Booking-Token
	Status  string // Porto-Status-Token
}

// ---- Customer area operations on transits (called by portal with the session's scope and customer) ----

// Receipt is a payment of the transit.
type Receipt struct {
	PaymentID  string
	ReceivedOn calendar.Date
	Amount     money.Cents
	Method     string
}

// CancellationPreview is the refund a cancellation would give now.
type CancellationPreview struct {
	Allowed  bool
	Reason   string // status, arrival_passed
	Paid     money.Cents
	Refund   money.Cents
	RefundAs string
	Tier     *onlineconf.Tier
}

// CustomerBooking is one transit of the customer in the customer area.
type CustomerBooking struct {
	ContractID       string
	Reference        string
	Code             string
	Status           string
	Channel          string
	Arrival          calendar.Date
	Departure        calendar.Date
	VesselName       string
	Category         *Name
	Total            money.Cents
	Paid             money.Cents
	Residual         money.Cents
	ApprovalPending  bool
	BerthCode        string // only when revealed
	QRPayload        string // PT1:<contract_id>, confirmed or arrived only
	ArrivalInfo      *onlineconf.ArrivalInfo
	Vessel           *VesselView
	Documents        []Document
	MissingDocuments []string
	Receipts         []Receipt
	Cancellation     *CancellationPreview
	CanCancel        bool
	CanChangeDates   bool
	CanPayBalance    bool
}

// CancelResult is the outcome of a customer cancellation.
type CancelResult struct {
	Status   string
	Refund   money.Cents
	RefundAs string
}

// DateChange is a date change request of the customer area.
type DateChange struct {
	Arrival, Departure calendar.Date
	ExpectedTotalCents *money.Cents // required by ChangeDates
	IdempotencyKey     string
}

// DateChangePreview is the price effect of new dates.
type DateChangePreview struct {
	Available  bool
	Reason     string
	OldTotal   money.Cents
	NewTotal   money.Cents
	Delta      money.Cents
	Refund     money.Cents
	Supplement money.Cents
}

// DateChangeResult is the outcome of ChangeDates.
type DateChangeResult struct {
	Status             string
	Arrival, Departure calendar.Date
	NewTotal           money.Cents
	Delta              money.Cents
	Refund             money.Cents
	Supplement         money.Cents
}

// PayBalanceResult is the checkout of the open residual.
type PayBalanceResult struct {
	CheckoutURL string
	Amount      money.Cents
}

// ---- Desk ----

// ListFilter filters the desk list of online bookings.
type ListFilter struct {
	Status   string
	Channel  string
	From, To calendar.Date // arrival range, zero = open
	Cursor   string
	Limit    int
}

// Summary is one row of the desk list.
type Summary struct {
	ContractID       string
	Reference        string
	Code             string
	Status           string
	Channel          string
	ChannelCode      string
	Arrival          calendar.Date
	Departure        calendar.Date
	LOACm, BeamCm    geo.Cm
	Category         string
	ApprovalReason   string
	ApprovalDeadline time.Time
	HoldExpiresAt    time.Time
	Total            money.Cents
	DueNow           money.Cents
	CreatedAt        time.Time
	Version          int
}

// Page is a page of the desk list.
type Page struct {
	Items      []Summary
	NextCursor string
}

// Contact is the decrypted contact submitted with a booking (customers.read only).
type Contact struct {
	FirstName, LastName string
	Email, Phone        string
	Country, Locale     string
}

// DeskDocument is a document of a booking in the desk view.
type DeskDocument struct {
	ID               string
	Type             string
	FileName         string
	ExpiresOn        calendar.Date
	UploadedAt       time.Time
	VerifiedAt       time.Time
	VerifiedByUserID string
}

// DeskConsent is a consent recorded with the booking.
type DeskConsent struct {
	Kind       string
	Version    string
	Granted    bool
	RecordedAt time.Time
}

// PaymentState is the online payment state of a booking.
type PaymentState struct {
	State         string // none, open, authorized, processing, succeeded, failed, expired, voided
	IntentID      string
	ProviderCode  string
	Amount        money.Cents
	CaptureMethod string
	AuthorizedAt  time.Time
	CapturedAt    time.Time
	CaptureFailed bool
}

// Detail is the desk detail of an online booking.
type Detail struct {
	Summary
	TransitCode         string
	CustomerID          string
	VesselID            string
	Contact             *Contact // nil without customers.read
	PeopleOnBoard       int
	ExpectedArrivalTime string
	Notes               string
	Locale              string
	Documents           []DeskDocument
	Consents            []DeskConsent
	Payment             PaymentState
	ApprovedAt          time.Time
	ApprovedByUserID    string
	RejectedAt          time.Time
	RejectReason        string
}

// ---- Channels ----

// ChannelBookingInput is an inbound channel reservation of kind new, already mapped to a Porto category
// (m8-plan §8.2). Guest data are personal data: never logged.
type ChannelBookingInput struct {
	ChannelCode        string
	ExternalRef        string
	Category           string
	Arrival, Departure calendar.Date
	Vessel             Vessel
	Guest              CustomerDetails
	AmountCents        money.Cents
	PaidToChannel      bool
	// Plan is the result of PlanChannelBooking computed before the transaction (nil: computed in it).
	Plan *ChannelPlan
}

// ChannelBookingResult is the outcome of CreateChannelBookingTx: Booked with the contract, or rejected with a
// reason (store.ChannelReject*).
type ChannelBookingResult struct {
	Booked       bool
	ContractID   string
	Reference    string
	RejectReason string
	InquiryID    string
}
