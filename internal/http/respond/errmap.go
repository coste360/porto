package respond

import (
	"errors"
	"net/http"

	"github.com/coste360/porto/internal/domain/apikey"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/domainerr"
	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/marina"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/team"
	"github.com/coste360/porto/internal/domain/vessel"
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
	// M1 (m1-plan §4.5)
	{harbor.ErrMapNotEmpty, problem.MapNotEmpty},
	{harbor.ErrNotGeoreferenced, problem.MapNotGeoreferenced},
	{harbor.ErrVersionMismatch, problem.VersionConflict},
	{harbor.ErrCSVInvalid, problem.CSVInvalid},
	{harbor.ErrInvalidFile, problem.InvalidFile},
	{harbor.ErrFileTooLarge, problem.FileTooLarge},
	{harbor.ErrUnsupportedMedia, problem.UnsupportedMediaType},
	{harbor.ErrMapInvalid, problem.MapInvalid},
	{customer.ErrAnonymized, problem.CustomerAnonymized},
	{customer.ErrMerged, problem.CustomerMerged},
	{customer.ErrVersionMismatch, problem.VersionConflict},
	{vessel.ErrVersionMismatch, problem.VersionConflict},
	{document.ErrInvalidFile, problem.InvalidFile},
	{document.ErrFileTooLarge, problem.FileTooLarge},
	{document.ErrUnsupportedMedia, problem.UnsupportedMediaType},
	{document.ErrLinkExpired, problem.DownloadLinkExpired},
	{document.ErrVersionMismatch, problem.VersionConflict},
	{deadline.ErrVersionMismatch, problem.VersionConflict},
	// M2 (m2-plan §4.6)
	{pricing.ErrNoPrice, problem.NoPrice},
	{pricing.ErrPromoNotApplicable, problem.PromoNotApplicable},
	{pricing.ErrInvalidInput, problem.ValidationFailed},
	{pricing.ErrOverflow, problem.ValidationFailed},
	{catalog.ErrInUse, problem.InUse},
	{catalog.ErrVersionMismatch, problem.VersionConflict},
	{pricelist.ErrImmutable, problem.PriceListImmutable},
	{pricelist.ErrOverlap, problem.PriceListOverlap},
	{pricelist.ErrInvalid, problem.PriceListInvalid},
	{pricelist.ErrVersionMismatch, problem.VersionConflict},
	{domainerr.ErrImmutable, problem.PriceListImmutable},
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
	var mi *harbor.MapInvalidError
	if errors.As(err, &mi) {
		return problem.MapInvalid, []problem.Option{problem.WithIssues(MapIssues(mi.Report))}, true
	}
	if t, opts, ok := mapPricing(err); ok {
		return t, opts, true
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

// MapIssue is the wire form of a layout.Issue (schema MapIssue of the contract).
type MapIssue struct {
	Code        string           `json:"code"`
	Severity    string           `json:"severity"`
	ElementType string           `json:"element_type"`
	Refs        []string         `json:"refs"`
	Field       string           `json:"field,omitempty"`
	Params      map[string]int64 `json:"params,omitempty"`
}

// MapIssues converts a validation report to the issues of a map_invalid problem: errors first, then warnings.
func MapIssues(r layout.Report) []MapIssue {
	out := make([]MapIssue, 0, len(r.Errors)+len(r.Warnings))
	for _, list := range [][]layout.Issue{r.Errors, r.Warnings} {
		for _, is := range list {
			refs := make([]string, len(is.Refs))
			for i, ref := range is.Refs {
				refs[i] = string(ref)
			}
			out = append(out, MapIssue{
				Code: is.Code, Severity: string(is.Severity), ElementType: is.Kind, Refs: refs, Field: is.Field,
				Params: is.Params,
			})
		}
	}
	return out
}

// mapPricing maps the typed errors of M2 that carry details or issues (m2-plan §4.6).
func mapPricing(err error) (problem.Type, []problem.Option, bool) {
	if errors.Is(err, pricelist.ErrNoActiveList) {
		return problem.NoPrice, []problem.Option{problem.WithDetails(map[string]string{"reason": pricelist.ReasonNoActiveList})}, true
	}
	if np, ok := errors.AsType[*pricing.NoPriceError](err); ok {
		return problem.NoPrice, []problem.Option{problem.WithDetails(np)}, true
	}
	if ie, ok := errors.AsType[*pricing.InputError](err); ok {
		return problem.ValidationFailed, []problem.Option{problem.Fields(problem.FieldError{Field: ie.Field, Code: ie.Code})}, true
	}
	if oe, ok := errors.AsType[*pricelist.OverlapError](err); ok {
		conflicts := oe.Conflicts
		if conflicts == nil {
			conflicts = []pricelist.Conflict{}
		}
		return problem.PriceListOverlap, []problem.Option{problem.WithDetails(map[string]any{"conflicts": conflicts})}, true
	}
	if ve, ok := errors.AsType[*pricelist.InvalidError](err); ok {
		issues := ve.Issues
		if issues == nil {
			issues = []pricing.Issue{}
		}
		return problem.PriceListInvalid, []problem.Option{problem.WithPriceListIssues(issues)}, true
	}
	if ue, ok := errors.AsType[*catalog.InUseError](err); ok {
		return problem.InUse, []problem.Option{problem.WithDetails(map[string]int{"references": ue.References})}, true
	}
	return "", nil, false
}
