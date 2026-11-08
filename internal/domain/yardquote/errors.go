package yardquote

import (
	"errors"

	"github.com/coste360/porto/internal/domain/yardquote/price"
)

var (
	// ErrNotImplemented is returned by the CONTRACT skeleton until QUOTE lands the bodies (501).
	ErrNotImplemented = errors.New("yardquote: not implemented")
	// ErrVersionMismatch means If-Match did not match (412 version_conflict).
	ErrVersionMismatch = errors.New("yardquote: version mismatch")
	// ErrInvalidTransition means the operation is not allowed from the quote status (409 invalid_transition).
	ErrInvalidTransition = errors.New("yardquote: invalid transition")
	// ErrQuoteExpired means the quote expired before acceptance (409 quote_expired).
	ErrQuoteExpired = errors.New("yardquote: quote expired")
	// ErrCodeTaken means a catalogue code or name is used (409 code_taken).
	ErrCodeTaken = errors.New("yardquote: code taken")
	// ErrInUse means a variable or table is referenced by a current package version or item formula (409 conflict).
	ErrInUse = errors.New("yardquote: in use")
	// ErrCustomerEmailMissing means the customer has no email for the send (422 customer_email_missing).
	ErrCustomerEmailMissing = errors.New("yardquote: customer email missing")
	// ErrNoPrice means a line has no catalogue price and no manual price: the quote cannot be sent or accepted
	// (422 no_price).
	ErrNoPrice = errors.New("yardquote: no price")
	// ErrFormulaInvalid is the sentinel of FormulaError (422 formula_invalid).
	ErrFormulaInvalid = errors.New("yardquote: formula invalid")
)

// Issue is a formula problem reported to the client (problem formula_invalid, details.issues).
type Issue struct {
	Code   string            `json:"code"`
	Field  string            `json:"field,omitempty"`
	Ref    string            `json:"ref,omitempty"`
	Pos    int               `json:"pos,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// FormulaError is 422 formula_invalid with the issues of every invalid formula; nothing was saved.
type FormulaError struct {
	Issues []Issue
}

func (e *FormulaError) Error() string { return "yardquote: formula invalid" }

// Unwrap lets errors.Is(err, ErrFormulaInvalid) match.
func (e *FormulaError) Unwrap() error { return ErrFormulaInvalid }

// BlockingError is 422 no_price (or formula_invalid) with the blocking line issues of a send or accept.
type BlockingError struct {
	Issues []price.LineIssue
}

func (e *BlockingError) Error() string { return "yardquote: blocking issues" }

// Unwrap lets errors.Is(err, ErrNoPrice) match.
func (e *BlockingError) Unwrap() error { return ErrNoPrice }
