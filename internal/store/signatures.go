package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of signature requests, signers, OTP codes and provider settings (m4-plan §5.3, §6.5). Every state change
// is a conditional update on the current status, so concurrent signers, the sync job, provider webhooks and staff
// actions never apply a transition twice (replay safe).

// Signature request and signer statuses.
const (
	SigPendingDocument = "pending_document"
	SigSent            = "sent"
	SigPartiallySigned = "partially_signed"
	SigSigned          = "signed"
	SigDeclined        = "declined"
	SigExpired         = "expired"
	SigCancelled       = "cancelled"

	SignerWaiting  = "waiting"
	SignerInvited  = "invited"
	SignerSigned   = "signed"
	SignerDeclined = "declined"
)

// SigOpenStatuses are the statuses of an open request (signature_requests_open_uidx).
var SigOpenStatuses = []string{SigPendingDocument, SigSent, SigPartiallySigned}

// InsertSignatureRequest inserts a request (an open request of the same subject is a unique violation).
func InsertSignatureRequest(ctx context.Context, db bun.IDB, r *SignatureRequest) error {
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// InsertSignatureSigners inserts the signers of a request.
func InsertSignatureSigners(ctx context.Context, db bun.IDB, rows []SignatureSigner) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// GetSignatureRequest returns a request of a marina.
func GetSignatureRequest(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SignatureRequest, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(SignatureRequest)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// OpenSignatureRequestOf returns the open request of a subject, or ErrNotFound.
func OpenSignatureRequestOf(ctx context.Context, db bun.IDB, s MarinaScope, subjectType, subjectID string) (*SignatureRequest, error) {
	r := new(SignatureRequest)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("subject_type = ?", subjectType).
		Where("subject_id = ?", subjectID).Where("status IN (?)", bun.List(SigOpenStatuses)).Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// SignatureRequestFilter narrows ListSignatureRequests.
type SignatureRequestFilter struct {
	Status, SubjectType, SubjectID string
}

// ListSignatureRequests returns the requests of a marina, newest first.
func ListSignatureRequests(ctx context.Context, db bun.IDB, s MarinaScope, f SignatureRequestFilter, cursor string, limit int) (Page[SignatureRequest], error) {
	limit = ClampLimit(limit)
	var rows []SignatureRequest
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).OrderExpr("id DESC").Limit(int64(limit + 1))
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.SubjectType != "" {
		q = q.Where("subject_type = ?", f.SubjectType)
	}
	if f.SubjectID != "" {
		q = q.Where("subject_id = ?", f.SubjectID)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[SignatureRequest]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.Scan(ctx); err != nil {
		return Page[SignatureRequest]{}, err
	}
	return pageOf(rows, limit, func(r SignatureRequest) string { return r.ID }), nil
}

// SignersOf returns the signers of requests, in request and sequence order.
func SignersOf(ctx context.Context, db bun.IDB, s MarinaScope, requestIDs ...string) ([]SignatureSigner, error) {
	var rows []SignatureSigner
	if len(requestIDs) == 0 {
		return rows, nil
	}
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("request_id IN (?)", bun.List(requestIDs)).
		OrderExpr("request_id, seq").Scan(ctx)
	return rows, err
}

// GetSignatureSigner returns a signer of a marina.
func GetSignatureSigner(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*SignatureSigner, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(SignatureSigner)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// TransitionSignatureRequest moves a request from one of from to status, setting the given columns
// (name → value); false when the request is no longer in from. The version is bumped.
func TransitionSignatureRequest(ctx context.Context, db bun.IDB, s MarinaScope, id string, from []string, status string, set map[string]any, at time.Time) (bool, error) {
	q := db.NewUpdate().Table("signature_requests").Set("status = ?", status).Set("version = version + 1").
		Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status IN (?)", bun.List(from))
	for k, v := range set {
		q = q.Set(k+" = ?", v) // column names are code constants
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// TouchSignatureRequest bumps the version of a request when it equals ifMatch and sets columns (staff actions
// with If-Match that do not change the status); ErrVersionMismatch otherwise.
func TouchSignatureRequest(ctx context.Context, db bun.IDB, s MarinaScope, id string, ifMatch int, set map[string]any, at time.Time) error {
	q := db.NewUpdate().Table("signature_requests").Set("version = version + 1").Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", ifMatch)
	for k, v := range set {
		q = q.Set(k+" = ?", v) // column names are code constants
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	return nil
}

// TransitionSigner moves a signer from one of from to status with columns; false when it is no longer in from.
func TransitionSigner(ctx context.Context, db bun.IDB, s MarinaScope, id string, from []string, status string, set map[string]any, at time.Time) (bool, error) {
	q := db.NewUpdate().Table("signature_signers").Set("status = ?", status).Set("version = version + 1").
		Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status IN (?)", bun.List(from))
	for k, v := range set {
		q = q.Set(k+" = ?", v) // column names are code constants
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetSignerColumns sets columns of a signer without a status condition (opened_at, evidence draft, reminders).
func SetSignerColumns(ctx context.Context, db bun.IDB, s MarinaScope, id string, set map[string]any, at time.Time) error {
	q := db.NewUpdate().Table("signature_signers").Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id)
	for k, v := range set {
		q = q.Set(k+" = ?", v) // column names are code constants
	}
	_, err := q.Exec(ctx)
	return err
}

// SignatureRequestsWithDocument returns the requests of any marina waiting for a document (pending_document).
func SignatureRequestsWithDocument(ctx context.Context, db bun.IDB, marinaID, documentID string) ([]SignatureRequest, error) {
	var rows []SignatureRequest
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", marinaID).Where("document_id = ?", documentID).
		Where("status = ?", SigPendingDocument).Scan(ctx)
	return rows, err
}

// OpenSignatureRequests returns up to limit open requests of every marina (the sync job), oldest first.
func OpenSignatureRequests(ctx context.Context, db bun.IDB, afterID string, limit int) ([]SignatureRequest, error) {
	var rows []SignatureRequest
	q := db.NewSelect().Model(&rows).Where("status IN (?)", bun.List(SigOpenStatuses)).OrderExpr("id").Limit(int64(limit))
	if afterID != "" {
		q = q.Where("id > ?", afterID)
	}
	return rows, q.Scan(ctx)
}

// SignatureRequestByEnvelope returns the request of a provider envelope in a marina.
func SignatureRequestByEnvelope(ctx context.Context, db bun.IDB, s MarinaScope, provider, ref string) (*SignatureRequest, error) {
	r := new(SignatureRequest)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("provider_code = ?", provider).
		Where("envelope_ref = ?", ref).OrderExpr("id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// InsertSignatureOTP inserts a code.
func InsertSignatureOTP(ctx context.Context, db bun.IDB, o *SignatureOTP) error {
	_, err := db.NewInsert().Model(o).Exec(ctx)
	return err
}

// CountSignatureOTPsSince counts the codes of a signer created since.
func CountSignatureOTPsSince(ctx context.Context, db bun.IDB, s MarinaScope, signerID string, since time.Time) (int, error) {
	return toInt(db.NewSelect().Table("signature_otps").Where("marina_id = ?", s.ID()).Where("signer_id = ?", signerID).
		Where("created_at >= ?", UTC(since)).Count(ctx))
}

// LatestSignatureOTP returns the newest code of a signer, or ErrNotFound.
func LatestSignatureOTP(ctx context.Context, db bun.IDB, s MarinaScope, signerID string) (*SignatureOTP, error) {
	o := new(SignatureOTP)
	err := db.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Where("signer_id = ?", signerID).
		OrderExpr("created_at DESC, id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// CountOTPAttempt counts a wrong attempt on a live code: attempts = attempts + 1 WHERE attempts < 5 and not
// consumed. It returns the new count, or ok = false when the code is locked or consumed.
func CountOTPAttempt(ctx context.Context, db bun.IDB, id string, maxAttempts int, at time.Time) (int, bool, error) {
	var attempts []int
	err := db.NewRaw(`UPDATE signature_otps SET attempts = attempts + 1, updated_at = ?
		WHERE id = ? AND attempts < ? AND consumed_at IS NULL RETURNING attempts`, UTC(at), id, maxAttempts).Scan(ctx, &attempts)
	if err != nil {
		return 0, false, err
	}
	if len(attempts) == 0 {
		return 0, false, nil
	}
	return attempts[0], true, nil
}

// ConsumeOTP marks a live code consumed (conditional); false when it was consumed or locked meanwhile.
func ConsumeOTP(ctx context.Context, db bun.IDB, id string, maxAttempts int, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("signature_otps").Set("consumed_at = ?", UTC(at)).Set("updated_at = ?", UTC(at)).
		Where("id = ?", id).Where("consumed_at IS NULL").Where("attempts < ?", maxAttempts).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// GetSignatureSettings returns the provider settings of a marina, or ErrNotFound.
func GetSignatureSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*SignatureSettings, error) {
	r := new(SignatureSettings)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// SaveSignatureSettings inserts (ifMatch 0 and no row) or updates (version = ifMatch) the settings.
func SaveSignatureSettings(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, r *SignatureSettings) error {
	r.MarinaID = s.ID()
	if ifMatch == 0 {
		r.Version = 1
		_, err := db.NewInsert().Model(r).Exec(ctx)
		if IsUniqueViolation(err) {
			return ErrVersionMismatch
		}
		return err
	}
	res, err := db.NewUpdate().Model(r).Column("provider_code", "credentials_enc", "webhook_secret_enc", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", ifMatch).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	r.Version = ifMatch + 1
	return nil
}

// MarinaMemberUser returns an active user with a grant on the scope's marina (a marina-level grant or an
// organization-wide grant of its organization); any other user is ErrNotFound (signers of the marina, INV-73).
func MarinaMemberUser(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*User, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	u := new(User)
	err := db.NewSelect().Model(u).ModelTableExpr("users AS u").ColumnExpr("u.*").Where("u.id = ?", id).Where("u.active = ?", true).
		Where("EXISTS (SELECT 1 FROM memberships AS m WHERE m.user_id = u.id AND "+
			"(m.marina_id = ? OR (m.organization_id = ? AND m.marina_id IS NULL)))", s.ID(), s.OrgID()).
		Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return u, nil
}
