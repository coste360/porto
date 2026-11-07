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
)

type typeInfo struct {
	status int
	title  string
}

// registry holds the default status and the English title of every type. Titles are for API consumers;
// the UI never shows them (it translates the slug).
var registry = map[Type]typeInfo{
	BadRequest:            {http.StatusBadRequest, "Bad request"},
	ValidationFailed:      {http.StatusBadRequest, "Validation failed"},
	WeakPassword:          {http.StatusBadRequest, "Password too weak"},
	InvalidResetToken:     {http.StatusBadRequest, "Invalid or expired reset token"},
	InvalidTOTP:           {http.StatusBadRequest, "Invalid authentication code"},
	PairingCodeInvalid:    {http.StatusBadRequest, "Invalid or expired pairing code"},
	InvalidScope:          {http.StatusBadRequest, "Invalid scope"},
	InvalidWebhookURL:     {http.StatusBadRequest, "Invalid webhook URL"},
	UnknownEventType:      {http.StatusBadRequest, "Unknown event type"},
	Unauthorized:          {http.StatusUnauthorized, "Authentication required"},
	InvalidCredentials:    {http.StatusUnauthorized, "Invalid credentials"},
	TOTPRequired:          {http.StatusUnauthorized, "Authentication code required"},
	Forbidden:             {http.StatusForbidden, "Forbidden"},
	CSRFFailed:            {http.StatusForbidden, "CSRF check failed"},
	TOTPEnrolmentRequired: {http.StatusForbidden, "Two-factor enrolment required"},
	TOTPRequiredByPolicy:  {http.StatusForbidden, "Two-factor authentication required by policy"},
	NotFound:              {http.StatusNotFound, "Not found"},
	MethodNotAllowed:      {http.StatusMethodNotAllowed, "Method not allowed"},
	Conflict:              {http.StatusConflict, "Conflict"},
	SlugTaken:             {http.StatusConflict, "Slug already in use"},
	AlreadyMember:         {http.StatusConflict, "Already a member"},
	LastOwner:             {http.StatusConflict, "Cannot remove the last owner"},
	TOTPAlreadyEnabled:    {http.StatusConflict, "Two-factor authentication already enabled"},
	TOTPNotEnrolled:       {http.StatusConflict, "Two-factor authentication not enrolled"},
	EndpointDisabled:      {http.StatusConflict, "Webhook endpoint disabled"},
	IdempotencyInProgress: {http.StatusConflict, "A request with this idempotency key is in progress"},
	VersionConflict:       {http.StatusPreconditionFailed, "Version conflict"},
	UnsupportedMediaType:  {http.StatusUnsupportedMediaType, "Unsupported media type"},
	IdempotencyKeyReused:  {http.StatusUnprocessableEntity, "Idempotency key reused with a different request"},
	PreconditionRequired:  {http.StatusPreconditionRequired, "If-Match header required"},
	RateLimited:           {http.StatusTooManyRequests, "Too many requests"},
	InternalError:         {http.StatusInternalServerError, "Internal error"},
	NotReady:              {http.StatusServiceUnavailable, "Service not ready"},
	MapInvalid:            {http.StatusUnprocessableEntity, "The map breaks a validation rule"},
	MapNotEmpty:           {http.StatusConflict, "The map still has berths, areas or land resources"},
	MapNotGeoreferenced:   {http.StatusConflict, "The map is not georeferenced"},
	InvalidFile:           {http.StatusBadRequest, "Invalid file"},
	FileTooLarge:          {http.StatusRequestEntityTooLarge, "File too large"},
	CSVInvalid:            {http.StatusBadRequest, "Invalid CSV"},
	CustomerAnonymized:    {http.StatusConflict, "The customer is anonymised"},
	CustomerMerged:        {http.StatusConflict, "The customer was merged into another"},
	DownloadLinkExpired:   {http.StatusForbidden, "Download link expired or invalid"},
	NoPrice:               {http.StatusUnprocessableEntity, "No price for the request"},
	PromoNotApplicable:    {http.StatusUnprocessableEntity, "Promo code not applicable"},
	PriceListInvalid:      {http.StatusUnprocessableEntity, "The price list breaks a validation rule"},
	PriceListImmutable:    {http.StatusConflict, "Published price lists cannot change"},
	PriceListOverlap:      {http.StatusConflict, "Another active price list covers the same nights"},
	InUse:                 {http.StatusConflict, "Still in use"},
	ResourceTaken:         {http.StatusConflict, "The resource is already occupied on some nights"},
	FitBlocked:            {http.StatusUnprocessableEntity, "The vessel does not fit the resource"},
	PlacementInvalid:      {http.StatusUnprocessableEntity, "Invalid placement"},
	InvalidTransition:     {http.StatusConflict, "Action not allowed in the current state"},
	QuoteExpired:          {http.StatusConflict, "The quote has expired"},
	HoldExpired:           {http.StatusConflict, "The hold has expired"},
	ClosureActive:         {http.StatusConflict, "Arrivals are closed on that date"},
	UndoUnavailable:       {http.StatusConflict, "The move can no longer be undone"},
	ReleaseInvalid:        {http.StatusConflict, "Invalid release"},
	CustomerRequired:      {http.StatusUnprocessableEntity, "A customer is required"},
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
