package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Render pipeline policy (m4-plan §6.4).
const (
	renderLease       = 2 * time.Minute
	maxRenderAttempts = 5
	renderRetryBase   = 30 * time.Second
	renderBatch       = 20
)

var timeZero time.Time

var audit0 = audit.System

// snapshot is the encrypted data of a composition: everything rendering needs besides the template text.
type snapshot struct {
	View   json.RawMessage `json:"view"`
	Today  string          `json:"today"`
	At     time.Time       `json:"at"`
	Title  string          `json:"title"`
	Author string          `json:"author"`
}

func dataAAD(id string) []byte { return []byte("docdata:" + id) }

// ComposeTx creates a pending generated document of a subject inside tx: it resolves the published template
// version (or the built-in default), asks the data source for the view and stores the encrypted snapshot. It
// returns the document id; the PDF is rendered after commit (RenderPending). The caller has authorised scope.
func (s *Service) ComposeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req ComposeRequest) (string, error) {
	info, ok := KindInfoOf(req.Kind)
	if !ok || info.View == nil {
		return "", fmt.Errorf("%w: %s", ErrKindNotPublishable, req.Kind)
	}
	ds, ok := s.DataSourceFor(req.Kind)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoDataSource, req.Kind)
	}
	m, err := store.GetMarina(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	locales := []string{}
	if req.Locale != "" {
		locales = append(locales, normLocale(req.Locale))
	}
	locales = append(locales, normLocale(m.DefaultLocale), "it")
	_, versionID, builtinRef, locale, err := s.resolveSource(ctx, tx, scope, req.Kind, req.TemplateID, locales)
	if err != nil {
		return "", err
	}
	view, err := ds.View(ctx, tx, scope, req.Kind, req.SubjectType, req.SubjectID, locale)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return "", fmt.Errorf("docs: view: %w", err)
	}
	at := req.At
	if at.IsZero() {
		at = s.now()
	}
	at = store.UTC(at).Truncate(time.Second)
	snap := snapshot{
		View: raw, Today: s.today(ctx, tx, scope, at), At: at, Title: Title(req.Kind, locale), Author: m.Name,
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	enc, err := s.d.Keys.Encrypt(data, dataAAD(id))
	if err != nil {
		return "", fmt.Errorf("docs: encrypt snapshot: %w", err)
	}
	now := s.now()
	row := &store.GeneratedDocument{
		ID: id, MarinaID: scope.ID(), Kind: string(req.Kind), TemplateVersionID: versionID, BuiltinRef: builtinRef,
		Locale: locale, SubjectType: string(req.SubjectType), SubjectID: req.SubjectID, Status: store.DocPending,
		DataEnc: enc, PDFA: req.PDFA, CreatedByUserID: req.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertGeneratedDocument(ctx, tx, row); err != nil {
		return "", err
	}
	if err := store.SupersedeDocuments(ctx, tx, scope, row.SubjectType, row.SubjectID, row.Kind, id, now); err != nil {
		return "", err
	}
	diff := map[string]any{"kind": req.Kind, "subject_type": req.SubjectType, "subject_id": req.SubjectID, "locale": locale, "pdfa": req.PDFA}
	if versionID != "" {
		diff["template_version_id"] = versionID
	} else {
		diff["builtin_ref"] = builtinRef
	}
	if err := s.write(ctx, tx, scope, actorOf(req.CreatedBy), "document.compose", "generated_document", id, diff,
		event.DocumentGenerated, event.DocumentGeneratedData{
			DocumentID: id, Kind: string(req.Kind), SubjectType: string(req.SubjectType), SubjectID: req.SubjectID,
			Status: store.DocPending,
		}); err != nil {
		return "", err
	}
	store.AfterCommit(ctx, s.kick)
	return id, nil
}

// sourceOfRow returns the template text a document was composed with.
func (s *Service) sourceOfRow(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.GeneratedDocument) (Source, error) {
	if row.TemplateVersionID != "" {
		v, err := store.GetTemplateVersionByID(ctx, db, scope, row.TemplateVersionID)
		if err != nil {
			return Source{}, err
		}
		return SourceOf(v), nil
	}
	src, _, _, ok := builtinByRef(row.BuiltinRef)
	if !ok {
		return Source{}, fmt.Errorf("docs: built-in %q not available", row.BuiltinRef)
	}
	return src, nil
}

// renderRow renders the snapshot of a document row (no database writes).
func (s *Service) renderRow(ctx context.Context, scope store.MarinaScope, row *store.GeneratedDocument) (*Rendered, error) {
	plain, err := s.d.Keys.Decrypt(row.DataEnc, dataAAD(row.ID))
	if err != nil {
		return nil, fmt.Errorf("docs: decrypt snapshot: %w", err)
	}
	var snap snapshot
	if err := json.Unmarshal(plain, &snap); err != nil {
		return nil, fmt.Errorf("docs: snapshot: %w", err)
	}
	view, err := decodeView(Kind(row.Kind), snap.View)
	if err != nil {
		return nil, err
	}
	src, err := s.sourceOfRow(ctx, s.d.DB.DB, scope, row)
	if err != nil {
		return nil, err
	}
	in := RenderInput{
		Kind: Kind(row.Kind), Source: src, View: view, Locale: row.Locale, Today: snap.Today, At: snap.At,
		PDFA: row.PDFA, Title: snap.Title, Author: snap.Author, Logo: s.logo(ctx, s.d.DB.DB, scope),
	}
	if Kind(row.Kind) == KindMapPrint {
		in.Extra = s.mapDrawing(view)
	}
	return Render(in)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrTemplateInvalid):
		return "template_invalid"
	case errors.Is(err, ErrTemplateLimits):
		return "template_limits"
	default:
		return "render_failed"
	}
}

// RenderPending renders a pending document (idempotent: a ready document is left as it is). Called after commit
// by the request that composed it and by the documents.render job. Rendering never runs inside a write
// transaction; the result is stored with a conditional update.
func (s *Service) RenderPending(ctx context.Context, documentID string) error {
	row, err := store.GetGeneratedDocumentAny(ctx, s.d.DB.DB, documentID)
	if err != nil {
		return notFound(err)
	}
	if row.Signed || (row.Status != store.DocPending && row.Status != store.DocRendering) {
		return nil
	}
	ok, err := store.ClaimDocument(ctx, s.d.DB.DB, row.ID, s.now(), renderLease)
	if err != nil || !ok {
		return err
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, row.MarinaID)
	if err != nil {
		return err
	}
	r, rerr := s.renderRow(ctx, scope, row)
	if rerr != nil {
		return s.renderFailed(ctx, scope, row, rerr)
	}
	pf, err := s.PutFile(ctx, scope, r.PDF, filestore.TypePDF)
	if err != nil {
		return s.renderFailed(ctx, scope, row, err)
	}
	committed := false
	defer func() {
		if !committed {
			s.DiscardFile(pf)
		}
	}()
	stored := false
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.InsertFileTx(ctx, tx, scope, pf, ""); err != nil {
			return err
		}
		ok, err := store.MarkDocumentReady(ctx, tx, row.ID, pf.ID, pf.SHA256, r.Pages, s.now())
		if err != nil {
			return mapImmutable(err)
		}
		if !ok {
			return errLeaseLost
		}
		stored = true
		return s.write(ctx, tx, scope, audit0, "document.render", "generated_document", row.ID,
			map[string]any{"sha256": pf.SHA256, "pages": r.Pages},
			event.DocumentGenerated, event.DocumentGeneratedData{
				DocumentID: row.ID, Kind: row.Kind, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
				Status: store.DocReady,
			})
	})
	if errors.Is(err, errLeaseLost) {
		return nil
	}
	if err != nil {
		return err
	}
	committed = stored
	for _, h := range s.readyHooks() {
		h(ctx, row.MarinaID, row.ID)
	}
	// Emails queued before their attachment was ready go out now, not at their safety-net recheck.
	if s.d.Notify != nil {
		if err := s.d.Notify.AttachmentReady(ctx, row.MarinaID); err != nil {
			s.d.Log.WarnContext(ctx, "document ready: waking emails failed", "document_id", row.ID, "error", err)
		}
	}
	return nil
}

var errLeaseLost = errors.New("docs: render lease lost")

func (s *Service) renderFailed(ctx context.Context, scope store.MarinaScope, row *store.GeneratedDocument, cause error) error {
	attempts := row.Attempts + 1
	code := errorCode(cause)
	var next time.Time
	permanent := code != "render_failed"
	if attempts < maxRenderAttempts && !permanent {
		d := renderRetryBase << (attempts - 1)
		next = s.now().Add(d)
	}
	s.d.Log.WarnContext(ctx, "document render failed", "document_id", row.ID, "kind", row.Kind, "attempts", attempts,
		"code", code, "gave_up", next.IsZero())
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.MarkDocumentRetry(ctx, tx, row.ID, code, next, s.now()); err != nil {
			return err
		}
		if !next.IsZero() {
			return nil
		}
		return s.write(ctx, tx, scope, audit0, "document.fail", "generated_document", row.ID,
			map[string]any{"error_code": code, "attempts": attempts},
			event.DocumentGenerated, event.DocumentGeneratedData{
				DocumentID: row.ID, Kind: row.Kind, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
				Status: store.DocFailed,
			})
	})
}

// RunRenderJob renders the pending documents whose time came and recovers the expired leases (job
// documents.render).
func (s *Service) RunRenderJob(ctx context.Context) error {
	ids, err := store.DueDocumentIDs(ctx, s.d.DB.DB, s.now(), renderBatch)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if err := s.RenderPending(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// RetryRender puts a failed document back in the queue and renders it after commit (contracts.write).
func (s *Service) RetryRender(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetGeneratedDocument(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		ok, err := store.ResetFailedDocument(ctx, tx, scope, row.ID, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return store.ErrConflict
		}
		return s.write(ctx, tx, scope, p.Actor(), "document.render", "generated_document", row.ID,
			map[string]any{"retry": true}, "", nil)
	})
	if err != nil {
		return err
	}
	if err := s.RenderPending(context.WithoutCancel(ctx), id); err != nil {
		s.d.Log.WarnContext(ctx, "document render deferred to the job", "document_id", id)
	}
	return nil
}

// RenderNow renders a document synchronously when it is pending (used by callers that need the bytes right away,
// such as the evidence and cover pages of signed documents).
func (s *Service) RenderNow(ctx context.Context, documentID string) error {
	return s.RenderPending(ctx, documentID)
}

// bytesReader adapts a byte slice for filestore.Put.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
