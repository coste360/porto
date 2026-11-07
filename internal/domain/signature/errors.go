package signature

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors (m4-plan §7.4; names fixed).
var (
	// ErrOTPInvalid means a wrong or expired code; the error is an *OTPError. 422 otp_invalid.
	ErrOTPInvalid = errors.New("signature: otp invalid")
	// ErrOTPLocked means the code has no attempts left (a new code can be requested). 409 otp_locked.
	ErrOTPLocked = errors.New("signature: otp locked")
	// ErrOTPRateLimited means more than 3 codes in 15 minutes for the signer. 429 otp_rate_limited.
	ErrOTPRateLimited = errors.New("signature: otp rate limited")
	// ErrSignatureOpen means the subject already has an open signature request. 409 signature_open.
	ErrSignatureOpen = errors.New("signature: request already open")
	// ErrInvalidWebhookSignature means a provider callback failed verification. 401 invalid_signature.
	ErrInvalidWebhookSignature = errors.New("signature: invalid webhook signature")
	// ErrProviderUnknown means no ext.SignatureProvider is registered with the code. 422 provider_unknown (404 on
	// the webhook route).
	ErrProviderUnknown = errors.New("signature: provider unknown")
	// ErrPaperScanRequired means the paper confirmation needs an uploaded scan. 422 paper_scan_required.
	ErrPaperScanRequired = errors.New("signature: paper scan required")
	// ErrNotSigner means the signer cannot act now (already signed or declined, not yet invited in a sequential
	// request, or the request is closed). 409 conflict.
	ErrNotSigner = fmt.Errorf("signature: signer cannot act: %w", store.ErrConflict)
)

// OTPError is a wrong code with the attempts left (details.attempts_left).
type OTPError struct {
	AttemptsLeft int
}

func (e *OTPError) Error() string {
	return fmt.Sprintf("%s: %d attempt(s) left", ErrOTPInvalid.Error(), e.AttemptsLeft)
}

// Is makes errors.Is(err, ErrOTPInvalid) true.
func (e *OTPError) Is(target error) bool { return target == ErrOTPInvalid }
