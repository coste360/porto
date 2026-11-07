//go:build integration

package signature_test

import (
	"bytes"
	"context"
	"errors"
	"net/http" //nolint:depguard // ext.SignatureProvider.ParseWebhook takes the request: the fake provider needs it
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/pdf"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

type demoSource struct{}

func (demoSource) Kinds() []docs.Kind { return []docs.Kind{docs.KindContractAnnual} }
func (demoSource) View(context.Context, bun.IDB, store.MarinaScope, docs.Kind, docs.SubjectType, string, string) (any, error) {
	return docs.DemoView(docs.KindContractAnnual), nil
}
func (demoSource) DemoView(k docs.Kind, _ string) (any, error) { return docs.DemoView(k), nil }

// subject records the callbacks.
type subject struct {
	mu     sync.Mutex
	signed []string
	failed []string
	fail   bool
}

func (*subject) Kind() string { return "contract" }
func (s *subject) OnSignedTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, id, doc string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("subject refuses")
	}
	s.signed = append(s.signed, id+":"+doc)
	return nil
}

func (s *subject) OnFailedTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, id+":"+status)
	return nil
}

// provider is a fake ext.SignatureProvider (tests only; Porto ships none).
type provider struct {
	mu      sync.Mutex
	status  string
	signed  []byte
	reqs    []ext.SignatureRequest
	creds   map[string]string
	cancels int
}

func (*provider) Code() string { return "fakesign" }
func (p *provider) Request(ctx context.Context, r ext.SignatureRequest) (*ext.SignatureEnvelope, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, r)
	p.creds = ext.ProviderCredentials(ctx)
	return &ext.SignatureEnvelope{Ref: "env-1", Status: ext.EnvelopeSent}, nil
}

func (p *provider) Status(context.Context, string) (*ext.SignatureEnvelope, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &ext.SignatureEnvelope{Ref: "env-1", Status: p.status}, nil
}

func (p *provider) Download(context.Context, string) (*ext.SignedDocument, error) {
	return &ext.SignedDocument{PDF: p.signed}, nil
}
func (p *provider) Cancel(context.Context, string) error { p.cancels++; return nil }
func (p *provider) ParseWebhook(r *http.Request, secret []byte) (*ext.SignatureEvent, error) {
	if r.Header.Get("X-Sig") != string(secret) {
		return nil, signature.ErrInvalidWebhookSignature
	}
	return &ext.SignatureEvent{EnvelopeRef: "env-1", Status: ext.EnvelopeSigned}, nil
}

type env struct {
	*fixture.Env
	docs  *docs.Service
	links *magiclink.Service
	sig   *signature.Service
	sub   *subject
	prov  *provider
	cust  *customer.Service
	scope store.MarinaScope
}

func newEnv(t *testing.T, db *store.DB) *env {
	t.Helper()
	e := fixture.New(t, db)
	files := filestore.New(t.TempDir(), e.Keys)
	d := docs.NewService(docs.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Notify: e.Notify, Log: e.Log})
	d.RegisterDataSource(demoSource{})
	l := magiclink.NewService(magiclink.Deps{DB: db, Clock: e.Clock, BaseURL: "https://porto.test"})
	prov := &provider{status: ext.EnvelopeSent}
	s := signature.NewService(signature.Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Docs: d, Links: l,
		Notify: e.Notify, Providers: []ext.SignatureProvider{prov}, Log: e.Log,
	})
	sub := &subject{}
	s.RegisterSubject(sub)
	scope, err := store.SystemMarinaScope(context.Background(), db.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	cust := customer.NewService(customer.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: files, Log: e.Log})
	return &env{Env: e, docs: d, links: l, sig: s, sub: sub, prov: prov, cust: cust, scope: scope}
}

func ptr[T any](v T) *T { return &v }

func (e *env) customer(t *testing.T, email string) string {
	t.Helper()
	res, err := e.cust.Create(fixture.Ctx(), e.Owner, e.MarinaID, customer.Patch{
		Kind: ptr(customer.KindPerson), FirstName: ptr("Mario"), LastName: ptr("Rossi"),
		Emails: &[]customer.Contact{{Label: "home", Value: email}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.Customer.ID
}

// open composes a document and opens a request in one transaction; the document renders after commit.
func (e *env) open(t *testing.T, driver string, sequential bool, signers ...signature.SignerSpec) (string, string) {
	t.Helper()
	subjectID := store.NewID()
	var reqID string
	err := e.DB.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
		docID, err := e.docs.ComposeTx(ctx, tx, e.scope, docs.ComposeRequest{
			Kind: docs.KindContractAnnual, SubjectType: docs.SubjectContract, SubjectID: subjectID, PDFA: true, At: e.Clock.Now(),
		})
		if err != nil {
			return err
		}
		reqID, err = e.sig.OpenTx(ctx, tx, e.scope, signature.OpenRequest{
			SubjectType: "contract", SubjectID: subjectID, DocumentID: docID, Driver: driver, ProviderCode: map[bool]string{true: "fakesign"}[driver == signature.DriverProvider],
			Signers: signers, Sequential: sequential, ExpiresAt: e.Clock.Now().Add(14 * 24 * time.Hour),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.docs.RunRenderJob(fixture.Ctx())
	_ = e.sig.RunSync(fixture.Ctx())
	return reqID, subjectID
}

func (e *env) request(t *testing.T, id string) *signature.Request {
	t.Helper()
	r, err := e.sig.GetRequest(fixture.Ctx(), e.Owner, e.MarinaID, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var (
	reToken = regexp.MustCompile(`/m/signature/([0-9A-Z]{52})`)
	reCode  = regexp.MustCompile(`: ([0-9]{6})`)
)

// lastMail delivers the queued emails and returns the last text sent to the address with a template.
func (e *env) lastMail(t *testing.T, to, template string) string {
	t.Helper()
	if _, err := e.Notify.DeliverDue(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	msgs := e.Mailbox.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].To == to && msgs[i].Template == template {
			return msgs[i].Text
		}
	}
	t.Fatalf("no %s mail to %s", template, to)
	return ""
}

func (e *env) token(t *testing.T, to string) string {
	t.Helper()
	m := reToken.FindStringSubmatch(e.lastMail(t, to, "signature_request"))
	if m == nil {
		t.Fatal("no link in the invitation")
	}
	return m[1]
}

func (e *env) code(t *testing.T, to string) string {
	t.Helper()
	m := reCode.FindStringSubmatch(e.lastMail(t, to, "signature_otp"))
	if m == nil {
		t.Fatal("no code in the email")
	}
	return m[1]
}

var meta = signature.ClientMeta{IP: "198.51.100.7", UserAgent: "Mozilla/5.0 test"}

func TestSignature_OTPFlow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.customer(t, "mario@example.test")
		id, subj := e.open(t, signature.DriverOTP, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		if r := e.request(t, id); r.Status != signature.StatusSent || r.Signers[0].Status != "invited" || r.Signers[0].Email != "mario@example.test" {
			t.Fatalf("request after dispatch %+v", r)
		}
		tok := e.token(t, "mario@example.test")
		l, err := e.links.Resolve(ctx, "marina-test", tok)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.links.Resolve(ctx, "marina-altra", tok); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatal("token valid for another slug")
		}
		pv, err := e.sig.PublicView(ctx, l, meta)
		if err != nil || pv.Name != "Mario Rossi" || pv.SignerStatus != "invited" {
			t.Fatalf("public view %+v %v", pv, err)
		}
		if _, err := e.sig.PublicDocument(ctx, l); err != nil {
			t.Fatal(err)
		}
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, false, meta); err == nil {
			t.Fatal("code sent without identity confirmation")
		}
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
			t.Fatal(err)
		}
		code := e.code(t, "mario@example.test")
		wrong := "000000"
		if code == wrong {
			wrong = "111111"
		}
		_, err = e.sig.VerifyOTP(ctx, "marina-test", tok, wrong, meta)
		var oe *signature.OTPError
		if !errors.As(err, &oe) || oe.AttemptsLeft != 4 {
			t.Fatalf("wrong code: %v", err)
		}
		res, err := e.sig.VerifyOTP(ctx, "marina-test", tok, code, meta)
		if err != nil || res.SignerStatus != "signed" || res.RequestStatus != signature.StatusSigned {
			t.Fatalf("verify: %+v %v", res, err)
		}
		r := e.request(t, id)
		if r.SignedDocumentID == "" || len(e.sub.signed) != 1 || !strings.HasPrefix(e.sub.signed[0], subj+":") {
			t.Fatalf("completion %+v %v", r, e.sub.signed)
		}
		signed, row, err := e.docs.DocumentPDF(ctx, db.DB, e.scope, r.SignedDocumentID)
		if err != nil {
			t.Fatal(err)
		}
		orig, _, _ := e.docs.DocumentPDF(ctx, db.DB, e.scope, r.DocumentID)
		if !row.Signed || row.SignedFromID != r.DocumentID || !bytes.HasPrefix(signed, orig) {
			t.Fatal("signed PDF does not keep the original as prefix")
		}
		if err := pdf.Verify(signed); err != nil {
			t.Fatal(err)
		}
		if n, _ := pdf.PageCount(signed); n <= row.Pages-1 {
			t.Fatal("evidence page missing")
		}
		// The link stops working once signed.
		if _, err := e.links.Resolve(ctx, "marina-test", tok); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatal("link still valid after signature")
		}
		if e.AuditCount(t, "signature.sign") != 2 || len(e.Events(t, "signature.completed")) != 1 {
			t.Fatal("audit or event missing")
		}
	})
}

func TestSignature_OTPLockout(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.customer(t, "lock@example.test")
		_, _ = e.open(t, signature.DriverOTP, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		tok := e.token(t, "lock@example.test")
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
			t.Fatal(err)
		}
		code := e.code(t, "lock@example.test")
		bad := "123456"
		if bad == code {
			bad = "654321"
		}
		for i := 4; i >= 0; i-- {
			_, err := e.sig.VerifyOTP(ctx, "marina-test", tok, bad, meta)
			var oe *signature.OTPError
			if !errors.As(err, &oe) || oe.AttemptsLeft != i {
				t.Fatalf("attempt %d: %v", 5-i, err)
			}
		}
		if _, err := e.sig.VerifyOTP(ctx, "marina-test", tok, code, meta); !errors.Is(err, signature.ErrOTPLocked) {
			t.Fatalf("locked code accepted: %v", err)
		}
		// Two more codes, then the 15-minute limit.
		for range 2 {
			if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); !errors.Is(err, signature.ErrOTPRateLimited) {
			t.Fatalf("rate limit: %v", err)
		}
		e.Clock.Advance(16 * time.Minute)
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
			t.Fatal(err)
		}
		// Codes expire after 10 minutes.
		code = e.code(t, "lock@example.test")
		e.Clock.Advance(11 * time.Minute)
		var oe *signature.OTPError
		if _, err := e.sig.VerifyOTP(ctx, "marina-test", tok, code, meta); !errors.As(err, &oe) {
			t.Fatalf("expired code: %v", err)
		}
	})
}

func TestSignature_SequentialSigners(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a, b := e.customer(t, "a@example.test"), e.customer(t, "b@example.test")
		id, _ := e.open(t, signature.DriverOTP, true,
			signature.SignerSpec{Role: "outgoing_holder", CustomerID: a}, signature.SignerSpec{Role: "incoming_holder", CustomerID: b})
		r := e.request(t, id)
		if r.Signers[0].Status != "invited" || r.Signers[1].Status != "waiting" {
			t.Fatalf("sequential invitations %+v", r.Signers)
		}
		sign := func(email string) {
			tok := e.token(t, email)
			if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
				t.Fatal(err)
			}
			if _, err := e.sig.VerifyOTP(ctx, "marina-test", tok, e.code(t, email), meta); err != nil {
				t.Fatal(err)
			}
		}
		sign("a@example.test")
		r = e.request(t, id)
		if r.Status != signature.StatusPartiallySigned || r.Signers[1].Status != "invited" {
			t.Fatalf("after the first signer %+v", r)
		}
		sign("b@example.test")
		if e.request(t, id).Status != signature.StatusSigned {
			t.Fatal("not signed")
		}
	})
}

func TestSignature_PaperFlow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.customer(t, "paper@example.test")
		id, _ := e.open(t, signature.DriverPaper, false, signature.SignerSpec{Role: "customer", CustomerID: c}, signature.SignerSpec{Role: "marina"})
		r := e.request(t, id)
		if r.Status != signature.StatusSent {
			t.Fatalf("status %s", r.Status)
		}
		if _, err := e.sig.ConfirmPaper(ctx, e.Owner, e.MarinaID, id, r.Version, nil, ""); !errors.Is(err, signature.ErrPaperScanRequired) {
			t.Fatalf("confirmation without scan: %v", err)
		}
		if _, err := e.sig.UploadPaperScan(ctx, e.Owner, e.MarinaID, id, "application/pdf", strings.NewReader("not a scan")); err == nil {
			t.Fatal("garbage scan accepted")
		}
		orig, _, _ := e.docs.DocumentPDF(ctx, db.DB, e.scope, r.DocumentID)
		scan, err := e.sig.UploadPaperScan(ctx, e.Owner, e.MarinaID, id, "application/pdf", bytes.NewReader(orig))
		if err != nil {
			t.Fatal(err)
		}
		out, err := e.sig.ConfirmPaper(ctx, e.Owner, e.MarinaID, id, r.Version, nil, scan)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != signature.StatusSigned || out.Signers[0].PaperScanFileID != scan {
			t.Fatalf("paper completion %+v", out)
		}
		signed, _, err := e.docs.DocumentPDF(ctx, db.DB, e.scope, out.SignedDocumentID)
		if err != nil || !bytes.HasPrefix(signed, orig) || !bytes.Contains(signed, []byte("/Count 3")) {
			t.Fatalf("cover page: %v", err)
		}
		if e.AuditCount(t, "signature.paper_confirm") != 2 {
			t.Fatal("paper confirmation not audited")
		}
	})
}

func TestSignature_ExpiryAndReminders(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c := e.customer(t, "late@example.test")
		id, subj := e.open(t, signature.DriverOTP, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		e.Clock.Advance(3*24*time.Hour + time.Minute)
		for range 3 { // idempotent
			if err := e.sig.RunSync(fixture.Ctx()); err != nil {
				t.Fatal(err)
			}
		}
		if r := e.request(t, id); r.RemindersSent != 1 {
			t.Fatalf("reminders %d", r.RemindersSent)
		}
		_ = e.lastMail(t, "late@example.test", "signature_reminder")
		e.Clock.Advance(12 * 24 * time.Hour)
		if err := e.sig.RunSync(fixture.Ctx()); err != nil {
			t.Fatal(err)
		}
		if r := e.request(t, id); r.Status != signature.StatusExpired {
			t.Fatalf("status %s", r.Status)
		}
		if len(e.sub.failed) != 1 || e.sub.failed[0] != subj+":expired" {
			t.Fatalf("subject callbacks %v", e.sub.failed)
		}
	})
}

func TestSignature_ProviderAdapter(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		if _, err := e.sig.UpdateSettings(ctx, e.Owner, e.MarinaID, 0, signature.SettingsInput{ProviderCode: ptr("nope")}); !errors.Is(err, signature.ErrProviderUnknown) {
			t.Fatalf("unknown provider: %v", err)
		}
		st, err := e.sig.UpdateSettings(ctx, e.Owner, e.MarinaID, 0, signature.SettingsInput{
			ProviderCode: ptr("fakesign"), Credentials: map[string]string{"api_key": "k-123"}, WebhookSecret: ptr("s3cret"),
		})
		if err != nil || !st.HasCredentials || !strings.Contains(st.WebhookURL, "/webhooks/signature/fakesign/") {
			t.Fatalf("settings %+v %v", st, err)
		}
		c := e.customer(t, "prov@example.test")
		id, _ := e.open(t, signature.DriverProvider, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		r := e.request(t, id)
		if r.Status != signature.StatusSent || r.EnvelopeRef != "env-1" || len(e.prov.reqs) != 1 || e.prov.creds["api_key"] != "k-123" {
			t.Fatalf("provider request %+v", r)
		}
		if e.prov.reqs[0].Signers[0].Email != "prov@example.test" || len(e.prov.reqs[0].Document) == 0 {
			t.Fatal("provider did not receive the signers and the document")
		}
		// Webhook: wrong provider or marina is unknown; the right one verifies with the stored secret.
		if _, _, err := e.sig.WebhookProvider(ctx, "other", e.MarinaID); !errors.Is(err, signature.ErrProviderUnknown) {
			t.Fatal("unknown provider accepted")
		}
		if _, _, err := e.sig.WebhookProvider(ctx, "fakesign", store.NewID()); !errors.Is(err, signature.ErrProviderUnknown) {
			t.Fatal("unknown marina accepted")
		}
		prov, secret, err := e.sig.WebhookProvider(ctx, "fakesign", e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		orig, _, _ := e.docs.DocumentPDF(ctx, db.DB, e.scope, r.DocumentID)
		e.prov.signed = orig
		req, _ := http.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sig", "wrong")
		if _, err := prov.ParseWebhook(req, secret); !errors.Is(err, signature.ErrInvalidWebhookSignature) {
			t.Fatal("bad signature accepted")
		}
		req.Header.Set("X-Sig", string(secret))
		ev, err := prov.ParseWebhook(req, secret)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 { // replay safe
			if err := e.sig.ApplyProviderEvent(ctx, "fakesign", e.MarinaID, ev); err != nil {
				t.Fatal(err)
			}
		}
		if r := e.request(t, id); r.Status != signature.StatusSigned || len(e.sub.signed) != 1 {
			t.Fatalf("after webhook %+v", r)
		}
		// Polling a second request finds it declined.
		id2, _ := e.open(t, signature.DriverProvider, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		e.prov.status = ext.EnvelopeDeclined
		if err := e.sig.RunSync(ctx); err != nil {
			t.Fatal(err)
		}
		if e.request(t, id2).Status != signature.StatusDeclined {
			t.Fatal("poll not applied")
		}
	})
}

func TestSignature_OpenTwiceIsSignatureOpen(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		c := e.customer(t, "x@example.test")
		id, subj := e.open(t, signature.DriverPaper, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		doc := e.request(t, id).DocumentID
		err := db.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
			_, err := e.sig.OpenTx(ctx, tx, e.scope, signature.OpenRequest{
				SubjectType: "contract", SubjectID: subj, DocumentID: doc, Driver: signature.DriverPaper,
				Signers: []signature.SignerSpec{{Role: "customer", CustomerID: c}}, ExpiresAt: e.Clock.Now().Add(time.Hour),
			})
			return err
		})
		if !errors.Is(err, signature.ErrSignatureOpen) {
			t.Fatalf("second open: %v", err)
		}
		err = db.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
			return e.sig.CancelTx(ctx, tx, e.scope, "contract", subj)
		})
		if err != nil || e.request(t, id).Status != signature.StatusCancelled {
			t.Fatalf("cancel: %v", err)
		}
		_, _, other := e.OtherOrganization(t)
		if _, err := e.sig.GetRequest(fixture.Ctx(), other, e.MarinaID, id); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
	})
}

func TestConcurrency_OTPAttempts(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.customer(t, "race@example.test")
		id, _ := e.open(t, signature.DriverOTP, false, signature.SignerSpec{Role: "customer", CustomerID: c})
		tok := e.token(t, "race@example.test")
		if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
			t.Fatal(err)
		}
		code := e.code(t, "race@example.test")
		bad := "999999"
		if bad == code {
			bad = "888888"
		}
		var invalid, locked, ok atomic.Int32
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				guess := bad
				if i == 50 {
					guess = code
				}
				_, err := e.sig.VerifyOTP(ctx, "marina-test", tok, guess, meta)
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, signature.ErrOTPLocked), errors.Is(err, signature.ErrNotSigner):
					locked.Add(1)
				case errors.Is(err, signature.ErrOTPInvalid):
					invalid.Add(1)
				default:
					t.Errorf("unexpected: %v", err)
				}
			}(i)
		}
		wg.Wait()
		o, err := store.LatestSignatureOTP(context.Background(), db.DB, e.scope, e.request(t, id).Signers[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if o.Attempts > 5 || ok.Load() > 1 {
			t.Fatalf("attempts %d, signatures %d", o.Attempts, ok.Load())
		}
		if ok.Load() == 0 && o.Attempts != 5 {
			t.Fatalf("attempts %d without signature", o.Attempts)
		}
		if len(e.sub.signed) != int(ok.Load()) {
			t.Fatalf("subject called %d times for %d signatures", len(e.sub.signed), ok.Load())
		}
	})
}

func TestConcurrency_SignatureCompleteOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		a, b := e.customer(t, "p1@example.test"), e.customer(t, "p2@example.test")
		id, _ := e.open(t, signature.DriverOTP, false,
			signature.SignerSpec{Role: "customer", CustomerID: a}, signature.SignerSpec{Role: "co_holder", CustomerID: b})
		type cred struct{ tok, code string }
		var creds []cred
		for _, m := range []string{"p1@example.test", "p2@example.test"} {
			tok := e.token(t, m)
			if _, err := e.sig.RequestOTP(ctx, "marina-test", tok, true, meta); err != nil {
				t.Fatal(err)
			}
			creds = append(creds, cred{tok, e.code(t, m)})
		}
		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c := creds[i%2]
				_, _ = e.sig.VerifyOTP(ctx, "marina-test", c.tok, c.code, meta)
			}(i)
		}
		wg.Wait()
		_ = e.sig.RunSync(ctx)
		r := e.request(t, id)
		if r.Status != signature.StatusSigned || len(e.sub.signed) != 1 {
			t.Fatalf("status %s, subject calls %d", r.Status, len(e.sub.signed))
		}
		n, err := db.NewSelect().Table("generated_documents").Where("signed_from_id = ?", r.DocumentID).Count(ctx)
		if err != nil || n != 1 {
			t.Fatalf("signed documents %d %v", n, err)
		}
	})
}

func TestMagicLink_RotateRevokes(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		c := e.customer(t, "link@example.test")
		quoteID := store.NewID()
		create := func() string {
			var tok string
			err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				var err error
				tok, _, err = e.links.CreateTx(ctx, tx, e.scope, magiclink.PurposeQuote, "quote", quoteID, c, e.Clock.Now().Add(time.Hour))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}
		first := create()
		second := create()
		if _, err := e.links.Resolve(ctx, "marina-test", first); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatal("rotated link still valid")
		}
		l, err := e.links.Resolve(ctx, "marina-test", second)
		if err != nil || l.SubjectID != quoteID || l.CustomerID != c {
			t.Fatalf("resolve %+v %v", l, err)
		}
		var hash string
		_ = db.NewSelect().Table("access_links").Column("token_hash").Where("id = ?", l.ID).Scan(ctx, &hash)
		if hash == second || hash != magiclink.HashToken(second) {
			t.Fatal("token not stored as its hash")
		}
		e.Clock.Advance(2 * time.Hour)
		if _, err := e.links.Resolve(ctx, "marina-test", second); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatal("expired link valid")
		}
		e.Clock.Advance(91 * 24 * time.Hour)
		if err := e.links.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		if n, _ := db.NewSelect().Table("access_links").Count(ctx); n != 0 {
			t.Fatalf("%d links left after cleanup", n)
		}
	})
}

// TestSignature_SignerUserScopedToMarina: a marina signer must hold a grant on the marina (INV-73); the user of
// another organisation is not found and nothing is written or queued.
func TestSignature_SignerUserScopedToMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		_, _, other := e.OtherOrganization(t)
		openWith := func(userID string) error {
			return db.RunInTx(fixture.Ctx(), func(ctx context.Context, tx bun.Tx) error {
				subj := store.NewID()
				docID, err := e.docs.ComposeTx(ctx, tx, e.scope, docs.ComposeRequest{
					Kind: docs.KindContractAnnual, SubjectType: docs.SubjectContract, SubjectID: subj, PDFA: true, At: e.Clock.Now(),
				})
				if err != nil {
					return err
				}
				_, err = e.sig.OpenTx(ctx, tx, e.scope, signature.OpenRequest{
					SubjectType: "contract", SubjectID: subj, DocumentID: docID, Driver: signature.DriverOTP,
					Signers: []signature.SignerSpec{{Role: "marina", UserID: userID}}, ExpiresAt: e.Clock.Now().Add(time.Hour),
				})
				return err
			})
		}
		before, err := db.NewSelect().TableExpr("signature_signers").Count(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := openWith(other.UserID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("signer of another organisation: %v", err)
		}
		after, _ := db.NewSelect().TableExpr("signature_signers").Count(context.Background())
		if after != before {
			t.Fatalf("signer rows written: %d -> %d", before, after)
		}
		// A member of the marina (marina-level grant) and the organisation owner are accepted.
		staff := e.AddUser(t, "office@porto.test", "office", e.OrgID, e.MarinaID)
		for _, id := range []string{staff.UserID, e.Owner.UserID} {
			if err := openWith(id); err != nil {
				t.Fatalf("member signer %s: %v", id, err)
			}
		}
	})
}
