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

type loader struct {
	calls    int
	rendered bool // the "rendering" document is ready
}

func (l *loader) LoadAttachment(_ context.Context, _ string, ref AttachmentRef) (*Attachment, error) {
	l.calls++
	if ref.DocumentID == "missing" {
		return nil, errors.New("not ready")
	}
	if ref.DocumentID == "rendering" && !l.rendered {
		return nil, ErrAttachmentPending
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

		// A document still rendering: the message waits without a failed attempt and goes out as soon as the document
		// service reports it ready, with no time passing.
		queue("k3", AttachmentRef{DocumentID: "rendering"})
		if _, err := s.DeliverDue(ctx, 10); err != nil {
			t.Fatal(err)
		}
		var w store.Message
		if err := db.NewSelect().Model(&w).Where("dedupe_key = ?", "k3").Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if w.Status != store.MessageQueued || w.Attempts != 0 || w.LastError != store.MessageWaitingAttachment {
			t.Fatalf("pending attachment: %+v", w)
		}
		if sent, err := s.DeliverDue(ctx, 10); err != nil || sent != 0 {
			t.Fatalf("waiting message delivered before its attachment: %d %v", sent, err)
		}
		l.rendered = true
		if err := s.AttachmentReady(ctx, w.MarinaID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-s.Wake():
		default:
			t.Fatal("AttachmentReady did not wake the delivery")
		}
		before := len(tr.sent)
		if _, err := s.DeliverDue(ctx, 10); err != nil {
			t.Fatal(err)
		}
		if len(tr.sent) != before+1 {
			t.Fatalf("waiting message not sent after AttachmentReady: %d sent", len(tr.sent)-before)
		}
	})
}
