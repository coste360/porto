//go:build integration

package document_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestDocuments_Verify(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid := e.customer(t)
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: document.OwnerCustomer, CustomerID: cid, Type: "insurance_policy"})
		if err != nil {
			t.Fatal(err)
		}
		v, err := e.docs.Verify(ctx, e.Owner, e.MarinaID, d.Row.ID, true)
		if err != nil || !v.Verified || v.VerifiedAt.IsZero() || v.VerifiedByUserID != e.OwnerID {
			t.Fatalf("verify %+v %v", v, err)
		}
		if e.AuditCount(t, "document.verify") != 1 {
			t.Fatal("verify audit missing")
		}
		v, err = e.docs.Verify(ctx, e.Owner, e.MarinaID, d.Row.ID, false)
		if err != nil || v.Verified || !v.VerifiedAt.IsZero() || v.VerifiedByUserID != "" {
			t.Fatalf("unverify %+v %v", v, err)
		}
		if e.AuditCount(t, "document.unverify") != 1 || len(e.Events(t, event.DocumentChanged)) < 3 {
			t.Fatal("unverify audit or events missing")
		}
		row, err := store.GetDocument(context.Background(), db.DB, scopeOf(t, db, e.MarinaID), d.Row.ID)
		if err != nil || row.Version != d.Row.Version+2 {
			t.Fatalf("version %+v %v", row, err)
		}

		// Another marina, an unknown id and a role without customers.write.
		_, otherMarina, other := e.OtherOrganization(t)
		if _, err := e.docs.Verify(ctx, other, e.MarinaID, d.Row.ID, true); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other organization: %v", err)
		}
		if _, err := e.docs.Verify(ctx, other, otherMarina, d.Row.ID, true); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("document of another marina: %v", err)
		}
		if _, err := e.docs.Verify(ctx, e.Owner, e.MarinaID, store.NewID(), true); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown document: %v", err)
		}
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.docs.Verify(ctx, dock, e.MarinaID, d.Row.ID, true); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock: %v", err)
		}
	})
}

func TestDocuments_CustomerFileURL(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		scope := scopeOf(t, db, e.MarinaID)
		cid, other := e.customer(t), e.customer(t)
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: document.OwnerCustomer, CustomerID: cid, Type: "insurance_policy"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.CustomerFileURL(ctx, db.DB, scope, "marina-test", cid, d.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("document without a file: %v", err)
		}
		if _, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, d.Row.ID, "application/pdf", "polizza.pdf", bytes.NewReader(samplePDF)); err != nil {
			t.Fatal(err)
		}
		u, err := e.docs.CustomerFileURL(ctx, db.DB, scope, "marina-test", cid, d.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.CustomerFileURL(ctx, db.DB, scope, "marina-test", other, d.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("another customer's document: %v", err)
		}
		parsed, err := url.Parse(u.URL)
		if err != nil {
			t.Fatal(err)
		}
		q := parsed.Query()
		exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
		f, err := e.docs.OpenCustomerFile(ctx, scope, cid, d.Row.ID, exp, q.Get("sig"))
		if err != nil || !bytes.Equal(f.Content, samplePDF) {
			t.Fatalf("open %v", err)
		}
		if _, err := e.docs.OpenCustomerFile(ctx, scope, other, d.Row.ID, exp, q.Get("sig")); err == nil {
			t.Fatal("link opened by another customer")
		}
		if _, err := e.docs.OpenCustomerFile(ctx, scope, cid, d.Row.ID, exp, q.Get("sig")+"x"); !errors.Is(err, document.ErrLinkExpired) {
			t.Fatalf("tampered signature: %v", err)
		}
		e.Clock.Advance(document.LinkTTL + time.Second)
		if _, err := e.docs.OpenCustomerFile(ctx, scope, cid, d.Row.ID, exp, q.Get("sig")); !errors.Is(err, document.ErrLinkExpired) {
			t.Fatalf("expired link: %v", err)
		}
	})
}

func scopeOf(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
