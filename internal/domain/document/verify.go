package document

import (
	"context"
	"crypto/hmac"
	"net/url"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// M8 additions (m8-plan §5.2, §7.2): office verification of documents uploaded online, and the short-lived signed
// links of the customer area (bound to the marina, the document and the customer, like the desk links).

// Verification is the verification state of a document.
type Verification struct {
	DocumentID       string
	Verified         bool
	VerifiedAt       time.Time
	VerifiedByUserID string
}

// Verify records or clears the office verification of a document (customers.write), with the audit row and the
// document.changed event in the same transaction.
func (s *Service) Verify(ctx context.Context, p *auth.Principal, marinaID, id string, verified bool) (*Verification, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Verification
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.SetDocumentVerified(ctx, tx, scope, id, verified, p.UserID, s.now()); err != nil {
			return notFound(err)
		}
		row, err := store.GetDocument(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		action := "document.verify"
		if !verified {
			action = "document.unverify"
		}
		if err := s.writeAudit(ctx, tx, scope, p, action, row.ID, map[string]any{"verified": verified}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row, event.ChangeUpdated); err != nil {
			return err
		}
		out = &Verification{
			DocumentID: row.ID, Verified: !row.VerifiedAt.IsZero(), VerifiedAt: row.VerifiedAt,
			VerifiedByUserID: row.VerifiedByUserID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// customerSubject is the principal part of a customer-area link signature.
func customerSubject(customerID string) string { return "customer:" + customerID }

// CustomerFileURL returns a 5-minute download link of a document of the customer (the caller already checked the
// customer-area session and that the document is the customer's). A document without a file is not found. The
// link points to the public file route of the customer area: {base}/api/public/v1/{slug}/account/documents/{id}/file.
func (s *Service) CustomerFileURL(ctx context.Context, db bun.IDB, scope store.MarinaScope, slug, customerID, id string) (*FileURL, error) {
	row, err := store.DocumentOfCustomer(ctx, db, scope, customerID, id)
	if err != nil {
		return nil, notFound(err)
	}
	if row.FileID == "" {
		return nil, auth.ErrNotFound
	}
	expires := s.d.Clock.Now().Add(LinkTTL).UTC().Truncate(time.Second)
	sig, err := s.signature(scope.ID(), row.ID, customerSubject(customerID), expires.Unix())
	if err != nil {
		return nil, err
	}
	u := "/api/public/v1/" + url.PathEscape(slug) + "/account/documents/" + row.ID + "/file?c=" + customerID +
		"&exp=" + strconv.FormatInt(expires.Unix(), 10) + "&sig=" + url.QueryEscape(sig)
	return &FileURL{URL: u, ExpiresAt: expires}, nil
}

// OpenCustomerFile checks a customer-area download link and returns the decrypted file. A missing, tampered,
// foreign or expired signature is ErrLinkExpired; a document that is not the customer's is not found.
func (s *Service) OpenCustomerFile(ctx context.Context, scope store.MarinaScope, customerID, id string, exp int64, sig string) (*File, error) {
	row, err := store.DocumentOfCustomer(ctx, s.d.DB.DB, scope, customerID, id)
	if err != nil {
		return nil, notFound(err)
	}
	if row.FileID == "" {
		return nil, auth.ErrNotFound
	}
	want, err := s.signature(scope.ID(), row.ID, customerSubject(customerID), exp)
	if err != nil {
		return nil, err
	}
	if sig == "" || !hmac.Equal([]byte(want), []byte(sig)) || !s.d.Clock.Now().Before(time.Unix(exp, 0)) {
		return nil, ErrLinkExpired
	}
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, row.FileID)
	if err != nil {
		return nil, notFound(err)
	}
	content, err := s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
	if err != nil {
		return nil, err
	}
	return &File{Content: content, ContentType: f.ContentType, FileName: row.FileName}, nil
}
