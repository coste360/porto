package respond

import (
	"errors"
	"net/http"

	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/domainerr"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/webhook"
	"github.com/coste360/porto/internal/http/problem"
)

// sentinels maps domain sentinel errors to problem types (m0-plan §4.4). Order matters only where one error
// wraps another; the specific ones come first.
var sentinels = []struct {
	err error
	typ problem.Type
}{
	{auth.ErrInvalidCredentials, problem.InvalidCredentials},
	{auth.ErrTOTPRequired, problem.TOTPRequired},
	{auth.ErrInvalidTOTP, problem.InvalidTOTP},
	{auth.ErrTOTPAlreadyEnabled, problem.TOTPAlreadyEnabled},
	{auth.ErrTOTPNotEnrolled, problem.TOTPNotEnrolled},
	{auth.ErrTOTPRequiredByPolicy, problem.TOTPRequiredByPolicy},
	{auth.ErrTOTPEnrolmentRequired, problem.TOTPEnrolmentRequired},
	{auth.ErrUnauthenticated, problem.Unauthorized},
	{auth.ErrForbidden, problem.Forbidden},
	{auth.ErrNotFound, problem.NotFound},
	{auth.ErrRateLimited, problem.RateLimited},
	{auth.ErrWeakPassword, problem.WeakPassword},
	{auth.ErrUserExists, problem.AlreadyMember},
	{auth.ErrInvalidResetToken, problem.InvalidResetToken},
	{team.ErrAlreadyMember, problem.AlreadyMember},
	{team.ErrLastOwner, problem.LastOwner},
	{marina.ErrSlugTaken, problem.SlugTaken},
	{marina.ErrVersionMismatch, problem.VersionConflict},
	{marina.ErrAlreadyArchived, problem.Conflict},
	{apikey.ErrInvalidScope, problem.InvalidScope},
	{device.ErrPairingCodeInvalid, problem.PairingCodeInvalid},
	{webhook.ErrInvalidURL, problem.InvalidWebhookURL},
	{webhook.ErrUnknownEventType, problem.UnknownEventType},
	{webhook.ErrEndpointDisabled, problem.EndpointDisabled},
	{webhook.ErrVersionMismatch, problem.VersionConflict},
	{domainerr.ErrInvalidCursor, problem.BadRequest},
	{domainerr.ErrNotFound, problem.NotFound},
	{domainerr.ErrVersionMismatch, problem.VersionConflict},
	{domainerr.ErrConflict, problem.Conflict},
	{ErrBadJSON, problem.BadRequest},
	{errUnsupportedMedia, problem.UnsupportedMediaType},
}

// MapError is the problem.Mapper of the API.
func MapError(err error) (problem.Type, []problem.Option, bool) {
	var rl *auth.RateLimitError
	if errors.As(err, &rl) {
		return problem.RateLimited, []problem.Option{problem.RetryAfter(rl.RetryAfter)}, true
	}
	var v *auth.ValidationError
	if errors.As(err, &v) {
		fields := make([]problem.FieldError, 0, len(v.Fields))
		for _, f := range v.Fields {
			fields = append(fields, problem.FieldError{Field: f.Field, Code: f.Code})
		}
		return problem.ValidationFailed, []problem.Option{problem.Fields(fields...)}, true
	}
	if errors.Is(err, auth.ErrInvalidInput) {
		return problem.ValidationFailed, nil, true
	}
	if errors.Is(err, errBodyTooLarge) {
		return problem.BadRequest, []problem.Option{problem.Status(http.StatusRequestEntityTooLarge), problem.Detail("request body too large")}, true
	}
	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return s.typ, nil, true
		}
	}
	return "", nil, false
}
