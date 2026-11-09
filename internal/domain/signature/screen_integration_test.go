//go:build integration

package signature_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestSignature_ScreenSignsInTransaction (m11-plan §7 step 2, D14): an on-screen signature of a rendered document
// creates the request already signed (driver screen), stores the signed copy with the evidence page appended,
// calls the subject in the same transaction, and refuses an image that is not a PNG or is too large.
func TestSignature_ScreenSignsInTransaction(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		subjectID := store.NewID()
		var docID string
		if err := db.RunInTx(ctx, func(c context.Context, tx bun.Tx) error {
			var err error
			docID, err = e.docs.ComposeTx(c, tx, e.scope, docs.ComposeRequest{
				Kind: docs.KindContractAnnual, SubjectType: docs.SubjectContract, SubjectID: subjectID, PDFA: true, At: e.Clock.Now(),
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := e.docs.RenderNow(ctx, docID); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 300, 100)))
		sign := func(img []byte) (*signature.ScreenSignResult, error) {
			var out *signature.ScreenSignResult
			err := db.RunInTx(ctx, func(c context.Context, tx bun.Tx) error {
				var err error
				out, err = e.sig.SignOnScreenTx(c, tx, e.scope, e.Owner.Actor(), signature.ScreenSignInput{
					SubjectType: "contract", SubjectID: subjectID, DocumentID: docID, SignerName: "Mario Rossi", PNG: img,
					DeviceLabel: "Tablet", At: e.Clock.Now(),
				})
				return err
			})
			return out, err
		}
		if _, err := sign([]byte("GIF89a not a png")); !errors.Is(err, signature.ErrScreenImageInvalid) {
			t.Fatalf("not a png: %v", err)
		}
		if _, err := sign(append(buf.Bytes(), make([]byte, signature.MaxScreenSignatureBytes)...)); !errors.Is(err, signature.ErrScreenImageInvalid) {
			t.Fatalf("too large: %v", err)
		}
		res, err := sign(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		r := e.request(t, res.RequestID)
		if r.Status != signature.StatusSigned || r.Driver != signature.DriverScreen || r.SignedDocumentID != res.SignedDocumentID {
			t.Fatalf("request %+v", r.SignatureRequest)
		}
		src, _, err := e.docs.DocumentPDF(ctx, db.DB, e.scope, docID)
		if err != nil {
			t.Fatal(err)
		}
		signed, row, err := e.docs.DocumentPDF(ctx, db.DB, e.scope, res.SignedDocumentID)
		if err != nil || !row.Signed || row.SignedFromID != docID {
			t.Fatalf("signed document %+v %v", row, err)
		}
		a, _ := pdf.PageCount(src)
		b, _ := pdf.PageCount(signed)
		if b != a+1 {
			t.Fatalf("pages %d -> %d, want the evidence page appended", a, b)
		}
		if len(e.sub.signed) != 1 || e.sub.signed[0] != subjectID+":"+res.SignedDocumentID {
			t.Fatalf("subject callbacks %v", e.sub.signed)
		}
	})
}
