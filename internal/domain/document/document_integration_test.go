//go:build integration

package document_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

type env struct {
	*fixture.Env
	files *filestore.Store
	cust  *customer.Service
	ves   *vessel.Service
	docs  *document.Service
	dl    *deadline.Service
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	return &env{
		Env: e, files: files,
		cust: customer.NewService(customer.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		ves:  vessel.NewService(vessel.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		docs: document.NewService(document.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
		dl:   deadline.NewService(deadline.Deps{DB: e.DB, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log}),
	}
}

func ptr[T any](v T) *T { return &v }

func (e *env) customer(t *testing.T) string {
	t.Helper()
	res, err := e.cust.Create(fixture.Ctx(), e.Owner, e.MarinaID, customer.Patch{Kind: ptr(customer.KindPerson), LastName: ptr("Rossi")})
	if err != nil {
		t.Fatal(err)
	}
	return res.Customer.ID
}

var samplePDF = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")

func samplePNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func blobs(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestDocument_FileEncryptedAtRest(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		cid := e.customer(t)
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: cid, Type: "insurance_policy"})
		if err != nil {
			t.Fatal(err)
		}
		// Content type checks: a PNG declared as PDF, a GIF, an empty body, an oversized body.
		for name, c := range map[string]struct {
			ct   string
			body []byte
			want error
		}{
			"mismatch": {"application/pdf", samplePNG(t), document.ErrInvalidFile},
			"svg":      {"image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), document.ErrUnsupportedMedia},
			"gif":      {"image/gif", []byte("GIF89a"), document.ErrUnsupportedMedia},
			"empty":    {"application/pdf", nil, document.ErrInvalidFile},
			"too big":  {"application/pdf", append(append([]byte{}, samplePDF...), make([]byte, document.MaxFileBytes)...), document.ErrFileTooLarge},
			"bad json": {"application/json", []byte("{}"), document.ErrUnsupportedMedia},
		} {
			if _, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, d.Row.ID, c.ct, "x", bytes.NewReader(c.body)); !errors.Is(err, c.want) {
				t.Errorf("%s: %v, want %v", name, err, c.want)
			}
		}
		if n := len(blobs(t, e.files.Dir())); n != 0 {
			t.Fatalf("rejected uploads left %d blobs", n)
		}
		up, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, d.Row.ID, "application/pdf; charset=binary", "../Polizza 2026.pdf", bytes.NewReader(samplePDF))
		if err != nil {
			t.Fatal(err)
		}
		if !up.HasFile || up.ContentType != "application/pdf" || up.SizeBytes != int64(len(samplePDF)) || up.Row.FileName != "Polizza 2026.pdf" || up.Row.Version != 2 {
			t.Fatalf("uploaded %+v", up)
		}
		files := blobs(t, e.files.Dir())
		if len(files) != 1 {
			t.Fatalf("blobs %v", files)
		}
		raw, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("%PDF")) || bytes.Contains(raw, []byte("Catalog")) {
			t.Fatal("blob on disk is not encrypted")
		}
		f, err := store.GetStoredFile(context.Background(), db.DB, mustScope(t, db, e.MarinaID), up.Row.FileID)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := e.files.Open(ctx, e.MarinaID, f.ID, f.StorageKey)
		if err != nil || !bytes.Equal(plain, samplePDF) || f.Purpose != "document" {
			t.Fatalf("decrypt: %v", err)
		}
		// Replacing the file removes the old blob; deleting it removes the new one.
		png := samplePNG(t)
		if _, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, d.Row.ID, "image/png", "", bytes.NewReader(png)); err != nil {
			t.Fatal(err)
		}
		if files := blobs(t, e.files.Dir()); len(files) != 1 {
			t.Fatalf("after replace %v", files)
		}
		if err := e.docs.DeleteFile(ctx, e.Owner, e.MarinaID, d.Row.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.docs.DeleteFile(ctx, e.Owner, e.MarinaID, d.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("second delete: %v", err)
		}
		if files := blobs(t, e.files.Dir()); len(files) != 0 {
			t.Fatalf("after delete %v", files)
		}
		// Writers only; another marina is not found.
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		if _, err := e.docs.UploadFile(ctx, dock, e.MarinaID, d.Row.ID, "application/pdf", "", bytes.NewReader(samplePDF)); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock upload: %v", err)
		}
		_, om, oo := e.OtherOrganization(t)
		if _, err := e.docs.UploadFile(ctx, oo, om, d.Row.ID, "image/gif", "", bytes.NewReader(nil)); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina upload: %v", err)
		}
		if n := len(e.Events(t, event.DocumentChanged)); n != 4 { // created, file, file, file deleted
			t.Fatalf("document.changed events: %d", n)
		}
	})
}

func mustScope(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDocument_SignedURLExpires(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: e.customer(t), Type: "passport"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.docs.FileURL(ctx, e.Owner, e.MarinaID, d.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("no file: %v", err)
		}
		if _, err := e.docs.UploadFile(ctx, e.Owner, e.MarinaID, d.Row.ID, "application/pdf", "p.pdf", bytes.NewReader(samplePDF)); err != nil {
			t.Fatal(err)
		}
		link, err := e.docs.FileURL(ctx, e.Owner, e.MarinaID, d.Row.ID)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(link.URL)
		if err != nil || !strings.HasPrefix(u.Path, "/api/admin/v1/marinas/"+e.MarinaID+"/documents/"+d.Row.ID+"/file") ||
			!link.ExpiresAt.Equal(e.Clock.Now().Add(5*time.Minute).Truncate(time.Second)) {
			t.Fatalf("link %+v", link)
		}
		exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
		sig := u.Query().Get("sig")
		f, err := e.docs.OpenFile(ctx, e.Owner, e.MarinaID, d.Row.ID, exp, sig)
		if err != nil || !bytes.Equal(f.Content, samplePDF) || f.ContentType != "application/pdf" || f.FileName != "p.pdf" {
			t.Fatalf("open: %v", err)
		}
		// Another user of the same marina, a tampered signature, a changed expiry, no signature.
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		for name, c := range map[string]struct {
			p   *auth.Principal
			exp int64
			sig string
		}{
			"other user": {office, exp, sig},
			"tampered":   {e.Owner, exp, sig[:len(sig)-2] + "AA"},
			"extended":   {e.Owner, exp + 3600, sig},
			"missing":    {e.Owner, 0, ""},
		} {
			if _, err := e.docs.OpenFile(ctx, c.p, e.MarinaID, d.Row.ID, c.exp, c.sig); !errors.Is(err, document.ErrLinkExpired) {
				t.Errorf("%s: %v", name, err)
			}
		}
		// Viewers have no customers.read.
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.docs.FileURL(ctx, viewer, e.MarinaID, d.Row.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		// Expired after 5 minutes.
		e.Clock.Advance(6 * time.Minute)
		if _, err := e.docs.OpenFile(ctx, e.Owner, e.MarinaID, d.Row.ID, exp, sig); !errors.Is(err, document.ErrLinkExpired) {
			t.Fatalf("expired: %v", err)
		}
	})
}

func TestDocument_NumberEncrypted(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		d, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", CustomerID: e.customer(t), Type: "identity_card", Number: " CA12345XY "})
		if err != nil {
			t.Fatal(err)
		}
		if d.Number != "CA12345XY" {
			t.Fatalf("number %q", d.Number)
		}
		var enc string
		if err := db.NewSelect().Table("documents").Column("number_enc").Where("id = ?", d.Row.ID).Scan(context.Background(), &enc); err != nil {
			t.Fatal(err)
		}
		if enc == "" || strings.Contains(enc, "CA12345XY") {
			t.Fatalf("number stored in clear: %q", enc)
		}
		// The ciphertext is bound to the document id.
		if _, err := e.Keys.DecryptString(enc, "document-number:"+store.NewID()); err == nil {
			t.Fatal("ciphertext decrypts with another document id")
		}
		up, err := e.docs.Update(ctx, e.Owner, e.MarinaID, d.Row.ID, 1, document.Patch{Number: ptr("")})
		if err != nil || up.Number != "" {
			t.Fatalf("clear number %+v %v", up, err)
		}
		// Owner rules: a vessel document needs an existing vessel of the marina.
		if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "vessel", VesselID: store.NewID(), Type: "insurance_policy"}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown vessel: %v", err)
		}
		if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{OwnerType: "customer", Type: "passport"}); err == nil {
			t.Fatal("customer document without customer")
		}
		if _, err := e.docs.Create(ctx, e.Owner, e.MarinaID, document.Input{
			OwnerType: "customer", CustomerID: d.Row.CustomerID, Type: "passport",
			IssuedOn: calendar.MustParse("2026-01-02"), ExpiresOn: calendar.MustParse("2026-01-01"),
		}); err == nil {
			t.Fatal("expiry before issue accepted")
		}
	})
}
