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
