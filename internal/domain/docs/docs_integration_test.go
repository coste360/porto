//go:build integration

package docs_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// fakeSource serves the demo contract view for any subject (the contract data source is CONTRACTS').
type fakeSource struct{ broken bool }

func (fakeSource) Kinds() []docs.Kind { return []docs.Kind{docs.KindContractAnnual} }

func (f fakeSource) View(context.Context, bun.IDB, store.MarinaScope, docs.Kind, docs.SubjectType, string, string) (any, error) {
	if f.broken {
		return map[string]any{"marina": "not an object"}, nil // decodes into ContractView only at render time
	}
	return docs.DemoView(docs.KindContractAnnual), nil
}

func (fakeSource) DemoView(k docs.Kind, _ string) (any, error) { return docs.DemoView(k), nil }

type env struct {
	*fixture.Env
	files *filestore.Store
	dir   string
	svc   *docs.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	dir := t.TempDir()
	files := filestore.New(dir, e.Keys)
	svc := docs.NewService(docs.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Notify: e.Notify, Log: e.Log})
	svc.RegisterDataSource(fakeSource{})
	return &env{Env: e, files: files, dir: dir, svc: svc}
}

func (e *env) scope(t *testing.T) store.MarinaScope {
	t.Helper()
	s, err := store.SystemMarinaScope(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *env) compose(t *testing.T, subjectID string, pdfa bool, at time.Time) string {
	t.Helper()
	var id string
	scope := e.scope(t)
	err := e.DB.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
		var err error
		id, err = e.svc.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
			Kind: docs.KindContractAnnual, SubjectType: docs.SubjectContract, SubjectID: subjectID, PDFA: pdfa,
			CreatedBy: e.OwnerID, At: at,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) row(t *testing.T, id string) *store.GeneratedDocument {
	t.Helper()
	r, err := store.GetGeneratedDocumentAny(context.Background(), e.DB.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDocs_ComposeRenderArchive(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		subject := store.NewID()
		id := e.compose(t, subject, true, e.Clock.Now())
		r := e.row(t, id)
		if r.Status != store.DocPending || r.DataEnc == "" || r.BuiltinRef == "" || r.Locale != "it" {
			t.Fatalf("composed row %+v", r)
		}
		if bytes.Contains([]byte(r.DataEnc), []byte("Mario Rossi")) {
			t.Fatal("snapshot not encrypted")
		}
		if err := e.svc.RenderPending(fixture.Ctx(), id); err != nil {
			t.Fatal(err)
		}
		r = e.row(t, id)
		if r.Status != store.DocReady || r.FileID == "" || r.SHA256 == "" || r.Pages < 1 {
			t.Fatalf("rendered row %+v", r)
		}
		b, _, err := e.svc.DocumentPDF(context.Background(), db.DB, e.scope(t), id)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != r.SHA256 {
			t.Fatal("sha256 of the decrypted file differs from the row")
		}
		if err := pdf.Verify(b); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte("pdfaid:part")) {
			t.Fatal("document to sign not finalised as PDF/A")
		}
		// Encrypted at rest: no blob on disk is the PDF.
		_ = filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, _ error) error {
			if d != nil && !d.IsDir() {
				raw, _ := os.ReadFile(p)
				if bytes.HasPrefix(raw, []byte("%PDF")) || bytes.Equal(raw, b) {
					t.Fatalf("blob %s stored in clear", p)
				}
			}
			return nil
		})
		if len(e.Events(t, event.DocumentGenerated)) != 2 || e.AuditCount(t, "document.compose") != 1 || e.AuditCount(t, "document.render") != 1 {
			t.Fatal("audit or events missing")
		}
		// Re-rendering a ready document is a no-op.
		if err := e.svc.RenderPending(fixture.Ctx(), id); err != nil || e.row(t, id).SHA256 != r.SHA256 {
			t.Fatal("ready document re-rendered")
		}
		// A second composition of the same subject supersedes the first one.
		id2 := e.compose(t, subject, false, e.Clock.Now())
		if e.row(t, id).Status != store.DocSuperseded || e.row(t, id2).Status != store.DocPending {
			t.Fatal("previous document not superseded")
		}
		// Download link: bound to the user, 5 minutes.
		l, err := e.svc.DocumentLink(fixture.Ctx(), e.Owner, e.MarinaID, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.svc.OpenDocument(fixture.Ctx(), e.Owner, e.MarinaID, id, l.ExpiresAt.Unix(), "bad"); !errors.Is(err, docs.ErrLinkExpired) {
			t.Fatalf("tampered link: %v", err)
		}
		_, _, other := e.OtherOrganization(t)
		if _, err := e.svc.GetDocument(fixture.Ctx(), other, e.MarinaID, id); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
	})
}

func TestDocs_RenderIsReproducible(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		at := e.Clock.Now()
		a := e.compose(t, store.NewID(), true, at)
		b := e.compose(t, store.NewID(), true, at)
		for _, id := range []string{a, b} {
			if err := e.svc.RenderPending(fixture.Ctx(), id); err != nil {
				t.Fatal(err)
			}
		}
		if e.row(t, a).SHA256 != e.row(t, b).SHA256 {
			t.Fatal("same snapshot rendered to different bytes")
		}
	})
}

func TestDocs_RenderJobRecovers(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		id := e.compose(t, store.NewID(), false, e.Clock.Now())
		// A renderer crashed after claiming the row.
		if ok, err := store.ClaimDocument(context.Background(), db.DB, id, e.Clock.Now(), 2*time.Minute); err != nil || !ok {
			t.Fatal("claim", err)
		}
		if err := e.svc.RunRenderJob(fixture.Ctx()); err != nil || e.row(t, id).Status != store.DocRendering {
			t.Fatal("lease not respected")
		}
		e.Clock.Advance(3 * time.Minute)
		if err := e.svc.RunRenderJob(fixture.Ctx()); err != nil {
			t.Fatal(err)
		}
		if e.row(t, id).Status != store.DocReady {
			t.Fatal("expired lease not recovered")
		}

		// A document that cannot render is retried with backoff, then failed after 5 attempts.
		e.svc.RegisterDataSource(fakeSource{broken: true})
		bad := e.compose(t, store.NewID(), false, e.Clock.Now())
		for i := 0; i < 8; i++ {
			_ = e.svc.RunRenderJob(fixture.Ctx())
			e.Clock.Advance(time.Hour)
		}
		r := e.row(t, bad)
		if r.Status != store.DocFailed || r.Attempts != 5 || r.ErrorCode == "" {
			t.Fatalf("failed row %+v", r)
		}
		if e.AuditCount(t, "document.fail") != 1 {
			t.Fatal("failure not audited")
		}
		// Manual retry puts it back in the queue.
		e.svc.RegisterDataSource(fakeSource{})
		if err := e.svc.RetryRender(fixture.Ctx(), e.Owner, e.MarinaID, bad); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.RetryRender(fixture.Ctx(), e.Owner, e.MarinaID, id); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("retry of a ready document: %v", err)
		}
	})
}

func TestDocs_SignedNeverRerendered(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		src := e.compose(t, store.NewID(), true, e.Clock.Now())
		if err := e.svc.RenderPending(ctx, src); err != nil {
			t.Fatal(err)
		}
		b, _, err := e.svc.DocumentPDF(ctx, db.DB, e.scope(t), src)
		if err != nil {
			t.Fatal(err)
		}
		pf, err := e.svc.PutFile(ctx, e.scope(t), b, filestore.TypePDF)
		if err != nil {
			t.Fatal(err)
		}
		var signed string
		scope := e.scope(t)
		if err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			signed, err = e.svc.InsertSignedTx(ctx, tx, scope, src, pf, 2, e.OwnerID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		r := e.row(t, signed)
		if !r.Signed || r.SignedFromID != src || r.Status != store.DocReady {
			t.Fatalf("signed row %+v", r)
		}
		_, err = db.NewUpdate().Table("generated_documents").Set("status = ?", store.DocPending).Where("id = ?", signed).Exec(ctx)
		if !store.IsTriggerViolation(err, store.DocumentImmutableMessage) {
			t.Fatalf("signed row changed: %v", err)
		}
		_, err = db.NewUpdate().Table("generated_documents").Set("sha256 = ?", "x").Where("id = ?", src).Exec(ctx)
		if !store.IsTriggerViolation(err, store.DocumentImmutableMessage) {
			t.Fatalf("hash of the source changed: %v", err)
		}
		// A new composition of the subject does not supersede the signed source.
		e.compose(t, r.SubjectID, true, e.Clock.Now())
		if e.row(t, src).Status != store.DocReady {
			t.Fatal("signed source superseded")
		}
		if err := e.svc.RenderPending(ctx, signed); err != nil || e.row(t, signed).SHA256 != r.SHA256 {
			t.Fatal("signed document re-rendered")
		}
	})
}

func TestTemplates_VersionsAndPublish(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		tp, err := e.svc.CreateTemplate(ctx, e.Owner, e.MarinaID, docs.TemplateInput{
			Kind: string(docs.KindContractAnnual), Code: "annual", Name: "Annuale", IsDefault: true, FromBuiltin: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateTemplate(ctx, e.Owner, e.MarinaID, docs.TemplateInput{
			Kind: string(docs.KindContractAnnual), Code: "annual", Name: "Dup",
		}); !errors.Is(err, docs.ErrCodeTaken) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := e.svc.CreateTemplate(ctx, e.Owner, e.MarinaID, docs.TemplateInput{
			Kind: string(docs.KindReceipt), Code: "r", Name: "Ricevuta",
		}); !errors.Is(err, docs.ErrKindNotPublishable) {
			t.Fatalf("future kind: %v", err)
		}
		_, err = e.svc.CreateTemplateVersion(ctx, e.Owner, e.MarinaID, tp.ID, docs.VersionInput{Locale: "it", Body: "ok\n\n{{customer.nome}}"})
		var te *docs.TemplateError
		if !errors.As(err, &te) || te.Issues[0].Line != 3 || te.Issues[0].Col != 1 || te.Issues[0].Code != "unknown_path" {
			t.Fatalf("invalid body: %v", err)
		}
		v, err := e.svc.CreateTemplateVersion(ctx, e.Owner, e.MarinaID, tp.ID, docs.VersionInput{
			Locale: "it", Body: "# Contratto {{contract.code}}\n\nCliente {{customer.display_name}}\n\n{{signature \"customer\"}}",
		})
		if err != nil || v.Version != 2 {
			t.Fatalf("draft: %+v %v", v, err)
		}
		r, err := e.svc.PreviewTemplate(ctx, e.Owner, e.MarinaID, tp.ID, docs.PreviewInput{Version: 2, Locale: "it"})
		if err != nil || r.Pages != 1 || len(r.PDF) == 0 {
			t.Fatalf("preview: %v", err)
		}
		if _, err := e.svc.PublishTemplateVersion(ctx, e.Owner, e.MarinaID, tp.ID, 1, "it"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublishTemplateVersion(ctx, e.Owner, e.MarinaID, tp.ID, 2, "it"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublishTemplateVersion(ctx, e.Owner, e.MarinaID, tp.ID, 2, "it"); !errors.Is(err, docs.ErrImmutable) {
			t.Fatalf("republish: %v", err)
		}
		vs, _ := e.svc.ListTemplateVersions(ctx, e.Owner, e.MarinaID, tp.ID, "it")
		statuses := map[int]string{}
		for _, x := range vs {
			statuses[x.Version] = x.Status
		}
		if statuses[1] != store.TemplateSuperseded || statuses[2] != store.TemplatePublished {
			t.Fatalf("statuses %v", statuses)
		}
		// Published rows are frozen by the trigger.
		_, err = db.NewUpdate().Table("template_versions").Set("body_md = ?", "x").Where("id = ?", v.ID).Exec(ctx)
		if !store.IsTriggerViolation(err, store.TemplateVersionImmutableMessage) {
			t.Fatalf("published version changed: %v", err)
		}
		// Compositions use the published default template and record its version.
		id := e.compose(t, store.NewID(), true, e.Clock.Now())
		if e.row(t, id).TemplateVersionID != v.ID {
			t.Fatal("composition ignores the published version")
		}
		got, err := e.svc.GetTemplate(ctx, e.Owner, e.MarinaID, tp.ID)
		if err != nil || got.Locales["it"] != 2 {
			t.Fatalf("locales %v %v", got, err)
		}
		// Office staff cannot configure templates.
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		if _, err := e.svc.CreateTemplateVersion(ctx, office, e.MarinaID, tp.ID, docs.VersionInput{Locale: "it", Body: "x"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office: %v", err)
		}
	})
}
