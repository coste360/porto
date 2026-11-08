package fiscal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Fiscal bridge (M6b, m6b-plan §8.4, D16): documents of the core driver "bridge" are issued by the desktop set
// as fiscal_bridge_device_id in the billing settings of the marina, a desktop app in remote mode with a local
// fiscal device. The desktop claims pending documents (60 s lease) and reports the outcome; the fiscal.issue job
// never touches them. A document is claimed by at most one caller at a time (INV-86).

// MaxBridgeClaim bounds one claim call; BridgeLease is the lease of a claim (desktopport.FiscalClaimLease).
const (
	MaxBridgeClaim = 10
	BridgeLease    = 60 * time.Second
)

// bridgeDriver is the registered driver "bridge": never called (the job skips its documents), it only makes the
// code valid for the billing settings and RecordTx.
type bridgeDriver struct{}

type bridgeDeferred struct{}

func (bridgeDeferred) Error() string  { return "fiscal: bridge documents are issued by the desktop" }
func (bridgeDeferred) Deferred() bool { return true }

func (bridgeDriver) Code() string { return DriverBridge }

func (bridgeDriver) Issue(context.Context, ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	return nil, bridgeDeferred{}
}

func (bridgeDriver) Void(context.Context, string) error { return nil }

func (bridgeDriver) Status(context.Context) (ext.DeviceStatus, error) {
	return ext.DeviceStatus{Online: true}, nil
}

// BridgeDriver returns the core driver "bridge".
func BridgeDriver() ext.FiscalDriver { return bridgeDriver{} }

// BridgeClaim is a document leased to the calling desktop.
type BridgeClaim struct {
	Document   store.FiscalDocument
	ClaimID    string
	ClaimUntil time.Time
	Lines      []Line
	VAT        []Bucket
	Method     string
}

// bridgeScope checks that p is the bridge device of its marina and returns the marina scope.
func (s *Service) bridgeScope(ctx context.Context, p *auth.Principal) (store.MarinaScope, error) {
	if p == nil || p.Kind != auth.KindDevice || p.DeviceID == "" {
		return store.MarinaScope{}, auth.ErrForbidden
	}
	d, err := store.DeviceByID(ctx, s.d.DB.DB, p.DeviceID)
	if err != nil || !d.RevokedAt.IsZero() {
		return store.MarinaScope{}, auth.ErrUnauthenticated
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, d.MarinaID)
	if err != nil {
		return store.MarinaScope{}, auth.ErrUnauthenticated
	}
	settings, err := store.GetBillingSettings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return store.MarinaScope{}, err
	}
	if settings.FiscalBridgeDeviceID == "" || settings.FiscalBridgeDeviceID != d.ID || d.Kind != "desktop" ||
		d.Access != store.DeviceAccessToken {
		return store.MarinaScope{}, ErrBridgeDeviceMismatch
	}
	return scope, nil
}

// ClaimBridge leases up to max (1..10) pending bridge documents of the device's marina to the calling device.
func (s *Service) ClaimBridge(ctx context.Context, p *auth.Principal, maxDocs int) ([]BridgeClaim, error) {
	scope, err := s.bridgeScope(ctx, p)
	if err != nil {
		return nil, err
	}
	maxDocs = min(max(maxDocs, 1), MaxBridgeClaim)
	now := s.now()
	until := now.Add(BridgeLease)
	ids, err := store.BridgeCandidates(ctx, s.d.DB.DB, scope, DriverBridge, now, maxDocs*2)
	if err != nil {
		return nil, err
	}
	out := []BridgeClaim{}
	for _, id := range ids {
		if len(out) >= maxDocs {
			break
		}
		claimID := store.NewID()
		won, err := store.ClaimBridgeDocument(ctx, s.d.DB.DB, scope, id, p.DeviceID, claimID, now, until)
		if err != nil {
			return out, err
		}
		if !won {
			continue
		}
		doc, err := store.GetFiscalDocument(ctx, s.d.DB.DB, scope, id)
		if err != nil {
			return out, err
		}
		c := BridgeClaim{Document: *doc, ClaimID: claimID, ClaimUntil: until}
		_ = json.Unmarshal(doc.Lines, &c.Lines)
		_ = json.Unmarshal(doc.VAT, &c.VAT)
		if pay, err := store.GetPayment(ctx, s.d.DB.DB, scope, doc.PaymentID); err == nil {
			c.Method = pay.Method
		}
		out = append(out, c)
	}
	return out, nil
}

// Bridge outcome statuses.
const (
	BridgeIssued   = "issued"
	BridgeFailed   = "failed"
	BridgeDeferred = "deferred"
)

// BridgeOutcomeInput is the outcome of a claimed document.
type BridgeOutcomeInput struct {
	ClaimID     string
	Status      string
	ExternalRef string
	ErrorCode   string
	IssuedAt    time.Time
}

// BridgeOutcome records the outcome of a claim: issued (frozen afterwards), failed, or deferred (the lease is
// released, the document stays pending). A stale or foreign claim is ErrClaimExpired; repeating the outcome that
// was already applied with the same claim succeeds without a second effect.
func (s *Service) BridgeOutcome(ctx context.Context, p *auth.Principal, documentID string, in BridgeOutcomeInput) error {
	scope, err := s.bridgeScope(ctx, p)
	if err != nil {
		return err
	}
	switch {
	case in.ClaimID == "" || len(in.ClaimID) > 64:
		return &auth.ValidationError{Fields: []auth.FieldError{{Field: "claim_id", Code: "invalid"}}}
	case in.Status != BridgeIssued && in.Status != BridgeFailed && in.Status != BridgeDeferred:
		return &auth.ValidationError{Fields: []auth.FieldError{{Field: "status", Code: "invalid"}}}
	case len(in.ExternalRef) > 100:
		return &auth.ValidationError{Fields: []auth.FieldError{{Field: "external_ref", Code: "too_long"}}}
	case len(in.ErrorCode) > 64:
		return &auth.ValidationError{Fields: []auth.FieldError{{Field: "error_code", Code: "too_long"}}}
	}
	doc, err := store.GetFiscalDocument(ctx, s.d.DB.DB, scope, documentID)
	if err != nil {
		return err
	}
	if doc.Driver != DriverBridge {
		return ErrNotFound
	}
	if doc.BridgeClaimID == in.ClaimID && doc.BridgeDeviceID == p.DeviceID &&
		((in.Status == BridgeIssued && doc.Status == store.FiscalIssued) || (in.Status == BridgeFailed && doc.Status == store.FiscalFailed)) {
		return nil // the same outcome again
	}
	now := s.now()
	o := store.BridgeOutcome{Status: store.FiscalPending, ExternalRef: in.ExternalRef, ErrorCode: in.ErrorCode, IssuedAt: in.IssuedAt}
	switch in.Status {
	case BridgeIssued:
		o.Status = store.FiscalIssued
		if o.IssuedAt.IsZero() || o.IssuedAt.After(now.Add(10*time.Minute)) {
			o.IssuedAt = now
		}
	case BridgeFailed:
		o.Status = store.FiscalFailed
		if o.ErrorCode == "" {
			o.ErrorCode = "driver_error"
		}
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		changed, err := store.SetBridgeOutcome(ctx, tx, scope, documentID, in.ClaimID, o, now)
		if err != nil {
			return err
		}
		if !changed {
			return ErrClaimExpired
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "fiscal.bridge_issue", documentID, map[string]any{
			"status": in.Status, "device_id": p.DeviceID, "claim_id": in.ClaimID,
		}); err != nil {
			return err
		}
		if o.Status == store.FiscalPending {
			return nil
		}
		out := *doc
		out.Status = o.Status
		return s.emit(ctx, tx, scope, &out)
	})
}

// ValidBridgeDevice reports whether deviceID is a desktop device of the marina paired with a token (the only
// kind that can claim): billing settings validation.
func ValidBridgeDevice(ctx context.Context, db bun.IDB, scope store.MarinaScope, deviceID string) bool {
	if !store.ValidID(deviceID) {
		return false
	}
	d, err := store.DeviceByID(ctx, db, deviceID)
	return err == nil && d.MarinaID == scope.ID() && d.Kind == "desktop" && d.Access == store.DeviceAccessToken &&
		d.RevokedAt.IsZero()
}
