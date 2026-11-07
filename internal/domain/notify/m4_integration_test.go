//go:build integration

package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type loader struct{ calls int }

func (l *loader) LoadAttachment(_ context.Context, _ string, ref AttachmentRef) (*Attachment, error) {
	l.calls++
	if ref.DocumentID == "missing" {
		return nil, errors.New("not ready")
	}
	return &Attachment{Name: "doc.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.7")}, nil
}

func TestNotify_Attachments(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		c := clock.NewFake(t0)
		tr := &flakyTransport{}
		s := newTestService(t, db, c, tr)
		l := &loader{}
		s.SetAttachmentLoader(l)
		ctx := context.Background()
		queue := func(key string, refs ...AttachmentRef) (string, bool) {
			var id string
			var ok bool
			err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				var err error
				id, ok, err = s.QueueMailTx(ctx, tx, Mail{
					Template: TemplateContractActivated, To: "anna@example.test", Locale: "it", DedupeKey: key,
					Values: map[string]string{ValueContractCode: "ORM-1"}, Attachments: refs,
				})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			return id, ok
		}
		id1, ok := queue("k1", AttachmentRef{DocumentID: "doc-1"})
		id2, again := queue("k1", AttachmentRef{DocumentID: "doc-1"})
		if !ok || again || id1 != id2 {
			t.Fatal("dedupe key not honoured")
		}
		queue("k2", AttachmentRef{DocumentID: "missing"})
		if _, err := s.DeliverDue(ctx, 10); err != nil {
			t.Fatal(err)
		}
		if len(tr.sent) != 1 || len(tr.sent[0].Attachments) != 1 || tr.sent[0].Attachments[0].Name != "doc.pdf" {
			t.Fatalf("sent %+v", tr.sent)
		}
		var m store.Message
		if err := db.NewSelect().Model(&m).Where("dedupe_key = ?", "k2").Scan(ctx); err != nil || m.Status != store.MessageQueued || m.Attempts != 1 {
			t.Fatalf("not-ready attachment should retry: %+v %v", m, err)
		}
	})
}
