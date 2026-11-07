package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of generated documents (m4-plan §5.3, §6.4). Rendering claims a row with a lease (pending → rendering)
// and finishes with a conditional update, so concurrent renderers and the documents.render job never render a row
// twice in a way that changes its hash (the trigger freezes sha256 and file_id once set).

// Generated document statuses.
const (
	DocPending    = "pending"
	DocRendering  = "rendering"
	DocReady      = "ready"
	DocFailed     = "failed"
	DocSuperseded = "superseded"
)

// InsertGeneratedDocument inserts a document row.
func InsertGeneratedDocument(ctx context.Context, db bun.IDB, d *GeneratedDocument) error {
	_, err := db.NewInsert().Model(d).Exec(ctx)
	return err
}

// GetGeneratedDocument returns one document of a marina.
func GetGeneratedDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*GeneratedDocument, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(GeneratedDocument)
	if err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// GetGeneratedDocumentAny returns a document by id without a marina scope (job and post-commit rendering only;
// the row carries its marina).
func GetGeneratedDocumentAny(ctx context.Context, db bun.IDB, id string) (*GeneratedDocument, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(GeneratedDocument)
	if err := db.NewSelect().Model(d).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// GeneratedDocumentFilter narrows ListGeneratedDocuments.
type GeneratedDocumentFilter struct {
	SubjectType, SubjectID, Status string
}

// ListGeneratedDocuments returns the documents of a marina, newest first (id order, UUIDv7).
func ListGeneratedDocuments(ctx context.Context, db bun.IDB, s MarinaScope, f GeneratedDocumentFilter, cursor string, limit int) (Page[GeneratedDocument], error) {
	limit = ClampLimit(limit)
	var rows []GeneratedDocument
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).OrderExpr("id DESC").Limit(int64(limit + 1))
	if f.SubjectType != "" {
		q = q.Where("subject_type = ?", f.SubjectType)
	}
	if f.SubjectID != "" {
		q = q.Where("subject_id = ?", f.SubjectID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[GeneratedDocument]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.Scan(ctx); err != nil {
		return Page[GeneratedDocument]{}, err
	}
	return pageOf(rows, limit, func(d GeneratedDocument) string { return d.ID }), nil
}

// LatestDocumentOf returns the newest non-superseded document of a subject (kind "" = any), or ErrNotFound.
func LatestDocumentOf(ctx context.Context, db bun.IDB, s MarinaScope, subjectType, subjectID, kind string, signed bool) (*GeneratedDocument, error) {
	d := new(GeneratedDocument)
	q := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("subject_type = ?", subjectType).
		Where("subject_id = ?", subjectID).Where("status <> ?", DocSuperseded).Where("signed = ?", signed).
		OrderExpr("id DESC").Limit(1)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// SupersedeDocuments marks the unsigned documents of a subject and kind superseded, except exceptID; signed
// documents and their sources are never touched.
func SupersedeDocuments(ctx context.Context, db bun.IDB, s MarinaScope, subjectType, subjectID, kind, exceptID string, at time.Time) error {
	_, err := db.NewUpdate().Table("generated_documents").Set("status = ?", DocSuperseded).Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("subject_type = ?", subjectType).Where("subject_id = ?", subjectID).
		Where("kind = ?", kind).Where("id <> ?", exceptID).Where("signed = ?", false).
		Where("status IN (?)", bun.List([]string{DocPending, DocReady, DocFailed})).
		Where("NOT EXISTS (SELECT 1 FROM generated_documents g2 WHERE g2.signed_from_id = generated_documents.id)").
		Where("NOT EXISTS (SELECT 1 FROM signature_requests r WHERE r.document_id = generated_documents.id)").
		Exec(ctx)
	return err
}

// ClaimDocument leases a document for rendering: pending (due) or rendering with an expired lease becomes
// rendering until now+lease. False when another renderer holds it or it is not pending.
func ClaimDocument(ctx context.Context, db bun.IDB, id string, now time.Time, lease time.Duration) (bool, error) {
	now = UTC(now)
	res, err := db.NewUpdate().Table("generated_documents").Set("status = ?", DocRendering).
		Set("lease_until = ?", now.Add(lease)).Set("updated_at = ?", now).Where("id = ?", id).
		Where("((status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND lease_until <= ?))",
			DocPending, now, DocRendering, now).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// DueDocumentIDs returns up to limit documents waiting for rendering (pending and due, or rendering with an
// expired lease), oldest first.
func DueDocumentIDs(ctx context.Context, db bun.IDB, now time.Time, limit int) ([]string, error) {
	now = UTC(now)
	var ids []string
	err := db.NewSelect().Table("generated_documents").Column("id").
		Where("status IN (?)", bun.List([]string{DocPending, DocRendering})).
		Where("((status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND lease_until <= ?))",
			DocPending, now, DocRendering, now).OrderExpr("id").Limit(int64(limit)).Scan(ctx, &ids)
	return ids, err
}

// MarkDocumentReady stores the rendered file of a document being rendered (conditional on rendering).
func MarkDocumentReady(ctx context.Context, db bun.IDB, id, fileID, sha string, pages int, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("generated_documents").Set("status = ?", DocReady).Set("file_id = ?", fileID).
		Set("sha256 = ?", sha).Set("pages = ?", pages).Set("rendered_at = ?", UTC(at)).Set("lease_until = NULL").
		Set("error_code = NULL").Set("updated_at = ?", UTC(at)).Where("id = ?", id).Where("status = ?", DocRendering).
		Where("file_id IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// MarkDocumentRetry records a failed rendering: back to pending at next, or failed when next is zero.
func MarkDocumentRetry(ctx context.Context, db bun.IDB, id, code string, next time.Time, at time.Time) error {
	q := db.NewUpdate().Table("generated_documents").Set("attempts = attempts + 1").Set("error_code = ?", code).
		Set("lease_until = NULL").Set("updated_at = ?", UTC(at)).Where("id = ?", id).Where("status = ?", DocRendering)
	if next.IsZero() {
		q = q.Set("status = ?", DocFailed).Set("next_attempt_at = NULL")
	} else {
		q = q.Set("status = ?", DocPending).Set("next_attempt_at = ?", UTC(next))
	}
	_, err := q.Exec(ctx)
	return err
}

// ResetFailedDocument puts a failed document back to pending (manual retry); false when it is not failed.
func ResetFailedDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("generated_documents").Set("status = ?", DocPending).Set("attempts = 0").
		Set("next_attempt_at = NULL").Set("error_code = NULL").Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", DocFailed).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SignedDocumentOf returns the signed document made from a source document, or ErrNotFound.
func SignedDocumentOf(ctx context.Context, db bun.IDB, s MarinaScope, sourceID string) (*GeneratedDocument, error) {
	d := new(GeneratedDocument)
	err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("signed_from_id = ?", sourceID).
		OrderExpr("id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return d, nil
}
