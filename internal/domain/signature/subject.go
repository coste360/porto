package signature

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// Subject is what a signature request signs (m4-plan §6.5): contract and addendum (registered by contract),
// right_transfer (registered by rights). signature never imports them: the callbacks run in the transaction that
// completes or fails the request, and an error rolls the whole completion back (the signer may retry).
type Subject interface {
	// Kind is the subject_type of signature_requests (contract, addendum, right_transfer).
	Kind() string
	// OnSignedTx runs when every signer has signed, in the transaction that stores the signed document.
	OnSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, signedDocumentID string, at time.Time) error
	// OnFailedTx runs when the request is declined, expired or cancelled (status is the request status).
	OnFailedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, status string) error
}
