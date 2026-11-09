package problem

import "net/http"

// Type is the stable slug of a problem ("validation_failed"). The web app shows it through the i18n key
// errors.<slug>, so slugs never change once released.
type Type string

// Problem types (m0-plan §4.4).
const (
	BadRequest            Type = "bad_request"
	ValidationFailed      Type = "validation_failed"
	WeakPassword          Type = "weak_password"
	InvalidResetToken     Type = "invalid_reset_token"
	InvalidTOTP           Type = "invalid_totp"
	PairingCodeInvalid    Type = "pairing_code_invalid"
	InvalidScope          Type = "invalid_scope"
	InvalidWebhookURL     Type = "invalid_webhook_url"
	UnknownEventType      Type = "unknown_event_type"
	Unauthorized          Type = "unauthorized"
	InvalidCredentials    Type = "invalid_credentials" //nolint:gosec // G101: a problem slug, not a credential
	TOTPRequired          Type = "totp_required"
	Forbidden             Type = "forbidden"
	CSRFFailed            Type = "csrf_failed"
	TOTPEnrolmentRequired Type = "totp_enrolment_required"
	TOTPRequiredByPolicy  Type = "totp_required_by_policy"
	NotFound              Type = "not_found"
	MethodNotAllowed      Type = "method_not_allowed"
	Conflict              Type = "conflict"
	SlugTaken             Type = "slug_taken"
	AlreadyMember         Type = "already_member"
	LastOwner             Type = "last_owner"
	TOTPAlreadyEnabled    Type = "totp_already_enabled"
	TOTPNotEnrolled       Type = "totp_not_enrolled"
	EndpointDisabled      Type = "endpoint_disabled"
	IdempotencyInProgress Type = "idempotency_in_progress"
	VersionConflict       Type = "version_conflict"
	UnsupportedMediaType  Type = "unsupported_media_type"
	IdempotencyKeyReused  Type = "idempotency_key_reused"
	PreconditionRequired  Type = "precondition_required"
	RateLimited           Type = "rate_limited"
	InternalError         Type = "internal_error"
	NotReady              Type = "not_ready"

	// M1 (m1-plan §4.5).
	MapInvalid          Type = "map_invalid"
	MapNotEmpty         Type = "map_not_empty"
	MapNotGeoreferenced Type = "map_not_georeferenced"
	InvalidFile         Type = "invalid_file"
	FileTooLarge        Type = "file_too_large"
	CSVInvalid          Type = "csv_invalid"
	CustomerAnonymized  Type = "customer_anonymized"
	CustomerMerged      Type = "customer_merged"
	DownloadLinkExpired Type = "download_link_expired"

	// M2 (m2-plan §4.6).
	NoPrice            Type = "no_price"
	PromoNotApplicable Type = "promo_not_applicable"
	PriceListInvalid   Type = "price_list_invalid"
	PriceListImmutable Type = "price_list_immutable"
	PriceListOverlap   Type = "price_list_overlap"
	InUse              Type = "in_use"

	// M3 (m3-plan §7.4).
	ResourceTaken     Type = "resource_taken"
	FitBlocked        Type = "fit_blocked"
	PlacementInvalid  Type = "placement_invalid"
	InvalidTransition Type = "invalid_transition"
	QuoteExpired      Type = "quote_expired"
	HoldExpired       Type = "hold_expired"
	ClosureActive     Type = "closure_active"
	UndoUnavailable   Type = "undo_unavailable"
	ReleaseInvalid    Type = "release_invalid"
	CustomerRequired  Type = "customer_required"

	// M4 (m4-plan §7.4). NotImplemented is answered only by the CONTRACT stubs while the builders work.
	NotImplemented       Type = "not_implemented"
	RenewalExists        Type = "renewal_exists"
	ProposalClosed       Type = "proposal_closed"
	SignatureOpen        Type = "signature_open"
	InvalidSignature     Type = "invalid_signature"
	OTPInvalid           Type = "otp_invalid"
	OTPLocked            Type = "otp_locked"
	OTPRateLimited       Type = "otp_rate_limited"
	LinkInvalid          Type = "link_invalid"
	TemplateInvalid      Type = "template_invalid"
	TemplateLimits       Type = "template_limits"
	PaperScanRequired    Type = "paper_scan_required"
	InsuranceRequired    Type = "insurance_required"
	CustomerEmailMissing Type = "customer_email_missing"
	SharesInvalid        Type = "shares_invalid"
	TableIncomplete      Type = "table_incomplete"
	TableActive          Type = "table_active"
	TransferRequirements Type = "transfer_requirements"
	MandateOverlap       Type = "mandate_overlap"
	VesselInOtherFleet   Type = "vessel_in_other_fleet"
	VesselNotInFleet     Type = "vessel_not_in_fleet"
	ImportNotUndoable    Type = "import_not_undoable"
	ProviderUnknown      Type = "provider_unknown"
	CodeTaken            Type = "code_taken"

	// M5 (m5-plan §7.4).
	ChargeImmutable           Type = "charge_immutable"
	ChargeCancelled           Type = "charge_cancelled"
	UseEdit                   Type = "use_edit"
	MatchExceeds              Type = "match_exceeds"
	RefundExceeds             Type = "refund_exceeds"
	AlreadyReversed           Type = "already_reversed"
	CreditInsufficient        Type = "credit_insufficient" //nolint:gosec // G101: a problem slug, not a credential
	NotDeposit                Type = "not_deposit"
	NothingToSettle           Type = "nothing_to_settle"
	ChargeExists              Type = "charge_exists"
	NoTerminationAmount       Type = "no_termination_amount"
	NotExported               Type = "not_exported"
	ExportEmpty               Type = "export_empty"
	ExportTooLarge            Type = "export_too_large"
	ChargesNotOpen            Type = "charges_not_open"
	ReleaseNotResold          Type = "release_not_resold"
	CashSessionRequired       Type = "cash_session_required"
	CashSessionOpen           Type = "cash_session_open"
	CashSessionClosed         Type = "cash_session_closed"
	CashInsufficient          Type = "cash_insufficient"
	RegisterInUse             Type = "register_in_use"
	ProviderDisabled          Type = "provider_disabled"
	ProviderRefundUnsupported Type = "provider_refund_unsupported"
	ProviderError             Type = "provider_error"
	BillingItemMissing        Type = "billing_item_missing"

	// M6a (m6a-plan §3.4).
	FirstRunTokenInvalid Type = "first_run_token_invalid"
	FirstRunDone         Type = "first_run_done"
	LANPortUnavailable   Type = "lan_port_unavailable"
	LANNoAddress         Type = "lan_no_address"
	BackupInProgress     Type = "backup_in_progress"
	RestoreFailed        Type = "restore_failed"
	RestoreBackupNewer   Type = "restore_backup_newer"

	// M6b (m6b-plan §9.1). concurrent_update is not added: a transaction that keeps failing on serialization
	// after the RunInTx retries surfaces as 500, which the offline queue already retries (D21).
	AllocationChanged     Type = "allocation_changed"
	OfflineNotAllowed     Type = "offline_not_allowed"
	RemoteUnreachable     Type = "remote_unreachable"
	RemotePinMismatch     Type = "remote_pin_mismatch"
	PeripheralUnavailable Type = "peripheral_unavailable"
	ClaimExpired          Type = "claim_expired"
	AppPinInvalid         Type = "app_pin_invalid"

	// M7 (m7-plan §8).
	InvalidPeriod       Type = "invalid_period"
	InvalidReportFilter Type = "invalid_report_filter"
	ReportTooLarge      Type = "report_too_large"
	UnknownIndicator    Type = "unknown_indicator"

	// M8 (m8-plan §11).
	BookingUnavailable     Type = "booking_unavailable"
	BookingRules           Type = "booking_rules"
	BookingBusy            Type = "booking_busy"
	ConsentRequired        Type = "consent_required"
	TextOutdated           Type = "text_outdated"
	RefundChanged          Type = "refund_changed"
	PriceChanged           Type = "price_changed"
	CancellationNotAllowed Type = "cancellation_not_allowed"
	NothingDue             Type = "nothing_due"
	IdempotencyMismatch    Type = "idempotency_mismatch"

	// M9 (m9-plan §3, §7, §8, §9).
	StorageTargetInvalid  Type = "storage_target_invalid"
	StorageNoSpace        Type = "storage_no_space"
	AreaExitsInvalid      Type = "area_exits_invalid"
	PinnedInvalid         Type = "pinned_invalid"
	TooManyItems          Type = "too_many_items"
	ScenarioWindowInvalid Type = "scenario_window_invalid"
	ScenarioNotDraft      Type = "scenario_not_draft"
	ScenarioConflicts     Type = "scenario_conflicts"
	MovementNotPending    Type = "movement_not_pending"
	ElementInvalid        Type = "element_invalid"
	MaintenanceConflicts  Type = "maintenance_conflicts"
	TaskAlreadyDone       Type = "task_already_done"
	ReadingCharged        Type = "reading_charged"
	ConsumptionFrozen     Type = "consumption_frozen"

	// M10 (m10-plan §13).
	FormulaInvalid            Type = "formula_invalid"
	WorkOrderClosed           Type = "work_order_closed"
	WorkOrderHasCharges       Type = "work_order_has_charges"
	QualificationMismatch     Type = "qualification_mismatch"
	CapacityExceeded          Type = "capacity_exceeded"
	DependencyViolation       Type = "dependency_violation"
	DurationTooLong           Type = "duration_too_long"
	OfflineTimeOutOfRange     Type = "offline_time_out_of_range"
	TimerAlreadyRunning       Type = "timer_already_running"
	ReturnExceedsIssued       Type = "return_exceeds_issued"
	NothingToBill             Type = "nothing_to_bill"
	EquipmentSlotTaken        Type = "equipment_slot_taken"
	OperatorBusy              Type = "operator_busy"
	EquipmentOutOfService     Type = "equipment_out_of_service"
	EquipmentCapacityExceeded Type = "equipment_capacity_exceeded"
	OperatorNotEnabled        Type = "operator_not_enabled"
	OutsideLiftWindow         Type = "outside_lift_window"

	// M11 (m11-plan §13.1).
	RentalSlotTaken       Type = "rental_slot_taken"
	SkipperBusy           Type = "skipper_busy"
	RentalHoldExpired     Type = "rental_hold_expired"
	FormulaRangeMismatch  Type = "formula_range_mismatch"
	OutsideRentalHours    Type = "outside_rental_hours"
	UnitNotBookable       Type = "unit_not_bookable"
	SkipperRequired       Type = "skipper_required"
	SkipperUnavailable    Type = "skipper_unavailable"
	SkipperLicenceExpired Type = "skipper_licence_expired"
	LicenceRequired       Type = "licence_required"
	LicenceExpired        Type = "licence_expired"
	HandoverIncomplete    Type = "handover_incomplete"
	HandoverLocked        Type = "handover_locked"
	EngineHoursDecreasing Type = "engine_hours_decreasing"
	PreauthUnavailable    Type = "preauth_unavailable"
	DepositExceeds        Type = "deposit_exceeds"
	NothingToCollect      Type = "nothing_to_collect"
	BandGapLtBuffer       Type = "band_gap_lt_buffer"

	// M12 (m12-plan §13). The ingestion errors use their own slugs so that vendors can tell a clock problem from a
	// key problem; 413 stays the generic body-limit problem (bad_request with status 413) of every operation.
	IngestUnauthorized        Type = "ingest_unauthorized"
	IngestTimestampOutOfRange Type = "ingest_timestamp_out_of_range"
	InvalidPayload            Type = "invalid_payload"
	SegmentOverlap            Type = "segment_overlap"
	ResourceHasSensor         Type = "resource_has_sensor"
	InvalidSensorResource     Type = "invalid_sensor_resource"
	AlertStale                Type = "alert_stale"
	AlertActionNotAllowed     Type = "alert_action_not_allowed"
	ProfileIncomplete         Type = "profile_incomplete"
	PhotoInvalid              Type = "photo_invalid"
)

type typeInfo struct {
	status int
	title  string
}

// registry holds the default status and the English title of every type. Titles are for API consumers;
// the UI never shows them (it translates the slug).
var registry = map[Type]typeInfo{
	BadRequest:                {http.StatusBadRequest, "Bad request"},
	ValidationFailed:          {http.StatusBadRequest, "Validation failed"},
	WeakPassword:              {http.StatusBadRequest, "Password too weak"},
	InvalidResetToken:         {http.StatusBadRequest, "Invalid or expired reset token"},
	InvalidTOTP:               {http.StatusBadRequest, "Invalid authentication code"},
	PairingCodeInvalid:        {http.StatusBadRequest, "Invalid or expired pairing code"},
	InvalidScope:              {http.StatusBadRequest, "Invalid scope"},
	InvalidWebhookURL:         {http.StatusBadRequest, "Invalid webhook URL"},
	UnknownEventType:          {http.StatusBadRequest, "Unknown event type"},
	Unauthorized:              {http.StatusUnauthorized, "Authentication required"},
	InvalidCredentials:        {http.StatusUnauthorized, "Invalid credentials"},
	TOTPRequired:              {http.StatusUnauthorized, "Authentication code required"},
	Forbidden:                 {http.StatusForbidden, "Forbidden"},
	CSRFFailed:                {http.StatusForbidden, "CSRF check failed"},
	TOTPEnrolmentRequired:     {http.StatusForbidden, "Two-factor enrolment required"},
	TOTPRequiredByPolicy:      {http.StatusForbidden, "Two-factor authentication required by policy"},
	NotFound:                  {http.StatusNotFound, "Not found"},
	MethodNotAllowed:          {http.StatusMethodNotAllowed, "Method not allowed"},
	Conflict:                  {http.StatusConflict, "Conflict"},
	SlugTaken:                 {http.StatusConflict, "Slug already in use"},
	AlreadyMember:             {http.StatusConflict, "Already a member"},
	LastOwner:                 {http.StatusConflict, "Cannot remove the last owner"},
	TOTPAlreadyEnabled:        {http.StatusConflict, "Two-factor authentication already enabled"},
	TOTPNotEnrolled:           {http.StatusConflict, "Two-factor authentication not enrolled"},
	EndpointDisabled:          {http.StatusConflict, "Webhook endpoint disabled"},
	IdempotencyInProgress:     {http.StatusConflict, "A request with this idempotency key is in progress"},
	VersionConflict:           {http.StatusPreconditionFailed, "Version conflict"},
	UnsupportedMediaType:      {http.StatusUnsupportedMediaType, "Unsupported media type"},
	IdempotencyKeyReused:      {http.StatusUnprocessableEntity, "Idempotency key reused with a different request"},
	PreconditionRequired:      {http.StatusPreconditionRequired, "If-Match header required"},
	RateLimited:               {http.StatusTooManyRequests, "Too many requests"},
	InternalError:             {http.StatusInternalServerError, "Internal error"},
	NotReady:                  {http.StatusServiceUnavailable, "Service not ready"},
	MapInvalid:                {http.StatusUnprocessableEntity, "The map breaks a validation rule"},
	MapNotEmpty:               {http.StatusConflict, "The map still has berths, areas or land resources"},
	MapNotGeoreferenced:       {http.StatusConflict, "The map is not georeferenced"},
	InvalidFile:               {http.StatusBadRequest, "Invalid file"},
	FileTooLarge:              {http.StatusRequestEntityTooLarge, "File too large"},
	CSVInvalid:                {http.StatusBadRequest, "Invalid CSV"},
	CustomerAnonymized:        {http.StatusConflict, "The customer is anonymised"},
	CustomerMerged:            {http.StatusConflict, "The customer was merged into another"},
	DownloadLinkExpired:       {http.StatusForbidden, "Download link expired or invalid"},
	NoPrice:                   {http.StatusUnprocessableEntity, "No price for the request"},
	PromoNotApplicable:        {http.StatusUnprocessableEntity, "Promo code not applicable"},
	PriceListInvalid:          {http.StatusUnprocessableEntity, "The price list breaks a validation rule"},
	PriceListImmutable:        {http.StatusConflict, "Published price lists cannot change"},
	PriceListOverlap:          {http.StatusConflict, "Another active price list covers the same nights"},
	InUse:                     {http.StatusConflict, "Still in use"},
	ResourceTaken:             {http.StatusConflict, "The resource is already occupied on some nights"},
	FitBlocked:                {http.StatusUnprocessableEntity, "The vessel does not fit the resource"},
	PlacementInvalid:          {http.StatusUnprocessableEntity, "Invalid placement"},
	InvalidTransition:         {http.StatusConflict, "Action not allowed in the current state"},
	QuoteExpired:              {http.StatusConflict, "The quote has expired"},
	HoldExpired:               {http.StatusConflict, "The hold has expired"},
	ClosureActive:             {http.StatusConflict, "Arrivals are closed on that date"},
	UndoUnavailable:           {http.StatusConflict, "The move can no longer be undone"},
	ReleaseInvalid:            {http.StatusConflict, "Invalid release"},
	CustomerRequired:          {http.StatusUnprocessableEntity, "A customer is required"},
	NotImplemented:            {http.StatusNotImplemented, "Not implemented yet"},
	RenewalExists:             {http.StatusConflict, "The contract already has an open renewal proposal"},
	ProposalClosed:            {http.StatusConflict, "The renewal proposal is closed"},
	SignatureOpen:             {http.StatusConflict, "A signature request is already open"},
	InvalidSignature:          {http.StatusUnauthorized, "Invalid webhook signature"},
	OTPInvalid:                {http.StatusUnprocessableEntity, "Wrong or expired code"},
	OTPLocked:                 {http.StatusConflict, "Too many wrong codes: request a new one"},
	OTPRateLimited:            {http.StatusTooManyRequests, "Too many codes requested"},
	LinkInvalid:               {http.StatusNotFound, "The link is not valid"},
	TemplateInvalid:           {http.StatusUnprocessableEntity, "The template has errors"},
	TemplateLimits:            {http.StatusUnprocessableEntity, "The template exceeds the output limits"},
	PaperScanRequired:         {http.StatusUnprocessableEntity, "The scan of the signed paper is required"},
	InsuranceRequired:         {http.StatusUnprocessableEntity, "The vessel has no valid insurance"},
	CustomerEmailMissing:      {http.StatusUnprocessableEntity, "The customer has no email address"},
	SharesInvalid:             {http.StatusUnprocessableEntity, "The holder shares do not sum to 1000"},
	TableIncomplete:           {http.StatusUnprocessableEntity, "The millesimal table does not sum to 1000"},
	TableActive:               {http.StatusConflict, "Active millesimal tables cannot change"},
	TransferRequirements:      {http.StatusUnprocessableEntity, "The transfer requirements are not met"},
	MandateOverlap:            {http.StatusConflict, "Another mandate covers the same nights"},
	VesselInOtherFleet:        {http.StatusConflict, "The vessel is in another active fleet"},
	VesselNotInFleet:          {http.StatusUnprocessableEntity, "The vessel is not in the charter fleet"},
	ImportNotUndoable:         {http.StatusConflict, "The import can no longer be undone"},
	ProviderUnknown:           {http.StatusUnprocessableEntity, "Unknown provider"},
	CodeTaken:                 {http.StatusConflict, "Code already in use"},
	ChargeImmutable:           {http.StatusConflict, "Exported, invoiced, paid or offset charges cannot change: use an adjustment"},
	ChargeCancelled:           {http.StatusConflict, "The charge is cancelled"},
	UseEdit:                   {http.StatusConflict, "The charge can still be edited"},
	MatchExceeds:              {http.StatusConflict, "The match exceeds the payment or the charge"},
	RefundExceeds:             {http.StatusConflict, "The refund exceeds the unallocated amount"},
	AlreadyReversed:           {http.StatusConflict, "The match was already reversed"},
	CreditInsufficient:        {http.StatusConflict, "Not enough customer credit"},
	NotDeposit:                {http.StatusConflict, "The payment is not a deposit"},
	NothingToSettle:           {http.StatusConflict, "Nothing to settle"},
	ChargeExists:              {http.StatusConflict, "The charge already exists"},
	NoTerminationAmount:       {http.StatusConflict, "The contract has no termination amount"},
	NotExported:               {http.StatusConflict, "The charge is not exported"},
	ExportEmpty:               {http.StatusUnprocessableEntity, "Nothing to export"},
	ExportTooLarge:            {http.StatusUnprocessableEntity, "Too many charges for one export"},
	ChargesNotOpen:            {http.StatusUnprocessableEntity, "Some charges are not open"},
	ReleaseNotResold:          {http.StatusUnprocessableEntity, "The release was not resold"},
	CashSessionRequired:       {http.StatusConflict, "An open cash session is required"},
	CashSessionOpen:           {http.StatusConflict, "The register already has an open session"},
	CashSessionClosed:         {http.StatusConflict, "The cash session is closed"},
	CashInsufficient:          {http.StatusConflict, "Not enough cash in the session"},
	RegisterInUse:             {http.StatusConflict, "The cash register is in use"},
	ProviderDisabled:          {http.StatusUnprocessableEntity, "No online payment provider is enabled"},
	ProviderRefundUnsupported: {http.StatusUnprocessableEntity, "The provider cannot refund this payment"},
	ProviderError:             {http.StatusBadGateway, "The payment provider returned an error"},
	BillingItemMissing:        {http.StatusUnprocessableEntity, "No billing item is configured for this charge"},
	FirstRunTokenInvalid:      {http.StatusBadRequest, "The first-run link is invalid or already used"},
	FirstRunDone:              {http.StatusConflict, "The first run is already completed"},
	LANPortUnavailable:        {http.StatusConflict, "The local network port is not available"},
	LANNoAddress:              {http.StatusConflict, "No local network address is available"},
	BackupInProgress:          {http.StatusConflict, "A backup or restore is already running"},
	RestoreFailed:             {http.StatusUnprocessableEntity, "The backup cannot be restored"},
	RestoreBackupNewer:        {http.StatusUnprocessableEntity, "The backup was made by a newer version"},
	AllocationChanged:         {http.StatusConflict, "The allocation changed since the operation was queued"},
	OfflineNotAllowed:         {http.StatusUnprocessableEntity, "This operation cannot be replayed from the offline queue"},
	RemoteUnreachable:         {http.StatusServiceUnavailable, "The online Porto instance is unreachable"},
	RemotePinMismatch:         {http.StatusBadGateway, "The online Porto instance presented an unexpected certificate"},
	PeripheralUnavailable:     {http.StatusServiceUnavailable, "The peripheral is not configured or not ready"},
	ClaimExpired:              {http.StatusConflict, "The fiscal claim expired"},
	AppPinInvalid:             {http.StatusUnauthorized, "Wrong app PIN"},
	InvalidPeriod:             {http.StatusUnprocessableEntity, "Invalid report period"},
	InvalidReportFilter:       {http.StatusUnprocessableEntity, "Invalid report filter"},
	ReportTooLarge:            {http.StatusUnprocessableEntity, "Too many records to export"},
	UnknownIndicator:          {http.StatusUnprocessableEntity, "Unknown or not comparable indicator"},
	// M8
	BookingUnavailable:     {http.StatusConflict, "No place of the category is available any more"},
	BookingRules:           {http.StatusUnprocessableEntity, "The request breaks an online booking rule"},
	BookingBusy:            {http.StatusServiceUnavailable, "Too many bookings in progress, retry shortly"},
	ConsentRequired:        {http.StatusUnprocessableEntity, "Terms and privacy must be accepted"},
	TextOutdated:           {http.StatusConflict, "The accepted text is not the current version"},
	RefundChanged:          {http.StatusConflict, "The refund changed since the preview"},
	PriceChanged:           {http.StatusConflict, "The price changed since the preview"},
	CancellationNotAllowed: {http.StatusUnprocessableEntity, "The booking cannot be cancelled online"},
	NothingDue:             {http.StatusUnprocessableEntity, "Nothing is due"},
	IdempotencyMismatch:    {http.StatusUnprocessableEntity, "The idempotency key was used with a different request"},
	// M9
	StorageTargetInvalid:  {http.StatusUnprocessableEntity, "Storage needs a surface area of a yard or shed map or a trailer spot"},
	StorageNoSpace:        {http.StatusConflict, "No free position in the area for the period"},
	AreaExitsInvalid:      {http.StatusUnprocessableEntity, "An exit of the area is not usable or not on the border"},
	PinnedInvalid:         {http.StatusUnprocessableEntity, "A pinned item is not valid in its position"},
	TooManyItems:          {http.StatusUnprocessableEntity, "Too many boats for one optimizer run"},
	ScenarioWindowInvalid: {http.StatusUnprocessableEntity, "Invalid scenario window"},
	ScenarioNotDraft:      {http.StatusConflict, "The scenario is not a draft any more"},
	ScenarioConflicts:     {http.StatusConflict, "The scenario conflicts with the current state"},
	MovementNotPending:    {http.StatusConflict, "The movement is not pending"},
	ElementInvalid:        {http.StatusUnprocessableEntity, "Unknown or unsuitable element"},
	MaintenanceConflicts:  {http.StatusConflict, "Boats occupy the element on the maintenance nights"},
	TaskAlreadyDone:       {http.StatusConflict, "The task was already completed"},
	ReadingCharged:        {http.StatusConflict, "The reading is used by a charged consumption"},
	ConsumptionFrozen:     {http.StatusConflict, "The consumption is charged or excluded"},
	// M10
	FormulaInvalid:            {http.StatusUnprocessableEntity, "A formula is invalid (issues in details)"},
	WorkOrderClosed:           {http.StatusConflict, "The work order does not accept this change in its state"},
	WorkOrderHasCharges:       {http.StatusConflict, "The work order has live charges"},
	QualificationMismatch:     {http.StatusUnprocessableEntity, "The employee does not hold the task qualification"},
	CapacityExceeded:          {http.StatusConflict, "The assignment exceeds the employee capacity of the day"},
	DependencyViolation:       {http.StatusUnprocessableEntity, "The assignment is before the end of a predecessor task"},
	DurationTooLong:           {http.StatusUnprocessableEntity, "The time entry is longer than 16 hours"},
	OfflineTimeOutOfRange:     {http.StatusUnprocessableEntity, "The device time is outside the offline window"},
	TimerAlreadyRunning:       {http.StatusConflict, "The employee already has a running timer"},
	ReturnExceedsIssued:       {http.StatusUnprocessableEntity, "The return exceeds the quantity issued on the work order"},
	NothingToBill:             {http.StatusConflict, "Nothing to bill on the work order"},
	EquipmentSlotTaken:        {http.StatusConflict, "The lifting equipment is already booked in that window"},
	OperatorBusy:              {http.StatusConflict, "The operator is already booked in that window"},
	EquipmentOutOfService:     {http.StatusConflict, "The equipment is out of service that day"},
	EquipmentCapacityExceeded: {http.StatusUnprocessableEntity, "The vessel is heavier than the equipment capacity"},
	OperatorNotEnabled:        {http.StatusUnprocessableEntity, "The employee is not enabled on the equipment"},
	OutsideLiftWindow:         {http.StatusUnprocessableEntity, "The window is outside the lifting hours"},
	// M11
	RentalSlotTaken:       {http.StatusConflict, "The rental unit is already booked or blocked in that window"},
	SkipperBusy:           {http.StatusConflict, "The skipper is already booked or off in that window"},
	RentalHoldExpired:     {http.StatusConflict, "The rental option expired and its window is no longer free"},
	FormulaRangeMismatch:  {http.StatusUnprocessableEntity, "The window does not match the rental formula"},
	OutsideRentalHours:    {http.StatusUnprocessableEntity, "The window is outside the rental hours"},
	UnitNotBookable:       {http.StatusUnprocessableEntity, "The rental unit is in maintenance or retired"},
	SkipperRequired:       {http.StatusUnprocessableEntity, "The formula requires a skipper"},
	SkipperUnavailable:    {http.StatusUnprocessableEntity, "The skipper does not work in that window"},
	SkipperLicenceExpired: {http.StatusUnprocessableEntity, "The skipper licence expires before the end of the rental"},
	LicenceRequired:       {http.StatusUnprocessableEntity, "The renter licence is required for this unit"},
	LicenceExpired:        {http.StatusUnprocessableEntity, "The licence is expired"},
	HandoverIncomplete:    {http.StatusUnprocessableEntity, "Steps of the handover are missing (missing in details)"},
	HandoverLocked:        {http.StatusConflict, "The handover is completed and can no longer change"},
	EngineHoursDecreasing: {http.StatusUnprocessableEntity, "The engine hours are lower than the last recorded"},
	PreauthUnavailable:    {http.StatusUnprocessableEntity, "No enabled payment provider supports pre-authorisation"},
	DepositExceeds:        {http.StatusUnprocessableEntity, "The retention exceeds the deposit"},
	NothingToCollect:      {http.StatusConflict, "Nothing to collect on the rental"},
	BandGapLtBuffer:       {http.StatusUnprocessableEntity, "Two bands are closer than the buffer between rentals"},
	// M12
	IngestUnauthorized:        {http.StatusUnauthorized, "The sensor key or signature is not valid"},
	IngestTimestampOutOfRange: {http.StatusUnauthorized, "The signature timestamp is outside the 300-second tolerance"},
	InvalidPayload:            {http.StatusBadRequest, "The readings batch is malformed or has 0 or more than 500 readings"},
	SegmentOverlap:            {http.StatusUnprocessableEntity, "The segment overlaps another active device of the area"},
	ResourceHasSensor:         {http.StatusConflict, "The berth or pedestal socket already has an active device"},
	InvalidSensorResource:     {http.StatusUnprocessableEntity, "The resource cannot carry a sensor"},
	AlertStale:                {http.StatusConflict, "The condition of the alert no longer holds"},
	AlertActionNotAllowed:     {http.StatusUnprocessableEntity, "The action is not allowed for this alert"},
	ProfileIncomplete:         {http.StatusUnprocessableEntity, "The profile misses data required to publish (missing in details)"},
	PhotoInvalid:              {http.StatusUnprocessableEntity, "The photo is not a valid JPEG or PNG image"},
}

// All returns every declared type (unordered). Used by tests that check parity with the OpenAPI spec and
// the web i18n catalogue.
func All() []Type {
	out := make([]Type, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	return out
}

// Known reports whether t is a declared type.
func Known(t Type) bool {
	_, ok := registry[t]
	return ok
}

// StatusOf returns the default HTTP status of t; unknown types are 500.
func StatusOf(t Type) int {
	if info, ok := registry[t]; ok {
		return info.status
	}
	return http.StatusInternalServerError
}

// TitleOf returns the English title of t.
func TitleOf(t Type) string {
	if info, ok := registry[t]; ok {
		return info.title
	}
	return registry[InternalError].title
}
