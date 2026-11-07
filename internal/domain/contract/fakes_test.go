//go:build integration

package contract

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/store"
)

// Fakes of the M4 collaborators (m4-plan §13.3): the contract tests run without the documents, signature and
// link services. Each fake records its calls; the composer writes nothing (a document id only).

type fakeComposer struct {
	mu   sync.Mutex
	reqs []docs.ComposeRequest
}

func (f *fakeComposer) ComposeTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, req docs.ComposeRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return store.NewID(), nil
}

func (f *fakeComposer) RenderPending(context.Context, string) error { return nil }

type fakeSigner struct {
	mu     sync.Mutex
	open   map[string]string // subject id → request id
	opened []signature.OpenRequest
}

var errSignatureOpen = errors.New("fake: signature open")

func (f *fakeSigner) OpenTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, req signature.OpenRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.open == nil {
		f.open = map[string]string{}
	}
	if _, ok := f.open[req.SubjectID]; ok {
		return "", errSignatureOpen
	}
	id := store.NewID()
	f.open[req.SubjectID] = id
	f.opened = append(f.opened, req)
	return id, nil
}

func (f *fakeSigner) CancelTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, _, subjectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.open, subjectID)
	return nil
}

type fakeLinks struct {
	mu    sync.Mutex
	links map[string]*magiclink.Link // token → link
	n     int
}

func (f *fakeLinks) CreateTx(_ context.Context, _ bun.IDB, scope store.MarinaScope, purpose, st, sid, cust string, exp time.Time) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.links == nil {
		f.links = map[string]*magiclink.Link{}
	}
	for tok, l := range f.links { // rotation revokes the previous link
		if l.Purpose == purpose && l.SubjectID == sid && l.CustomerID == cust {
			delete(f.links, tok)
		}
	}
	f.n++
	tok := store.NewID()
	id := store.NewID()
	f.links[tok] = &magiclink.Link{ID: id, MarinaID: scope.ID(), Purpose: purpose, SubjectType: st, SubjectID: sid, CustomerID: cust, ExpiresAt: exp}
	return tok, id, nil
}

func (f *fakeLinks) Resolve(_ context.Context, _, token string) (*magiclink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l, ok := f.links[token]; ok {
		return l, nil
	}
	return nil, magiclink.ErrInvalid
}

func (f *fakeLinks) URL(slug, purpose, token string) string {
	return "https://porto.test/b/" + slug + "/m/" + purpose + "/" + token
}

func (f *fakeLinks) tokenOf(subjectID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, l := range f.links {
		if l.SubjectID == subjectID {
			return tok
		}
	}
	return ""
}

type fakeMailer struct {
	mu    sync.Mutex
	mails []Mail
	keys  map[string]bool
}

func (f *fakeMailer) QueueCustomerMailTx(_ context.Context, _ bun.IDB, _ store.MarinaScope, m Mail) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys == nil {
		f.keys = map[string]bool{}
	}
	if f.keys[m.DedupeKey] {
		return "dup", nil
	}
	f.keys[m.DedupeKey] = true
	f.mails = append(f.mails, m)
	return store.NewID(), nil
}

func (f *fakeMailer) count(template string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.mails {
		if m.Template == template {
			n++
		}
	}
	return n
}

// m4env adds the fakes to a test environment.
type m4env struct {
	*tenv
	comp   *fakeComposer
	sig    *fakeSigner
	links  *fakeLinks
	mailer *fakeMailer
}

func withFakes(e *tenv) *m4env {
	m := &m4env{tenv: e, comp: &fakeComposer{}, sig: &fakeSigner{}, links: &fakeLinks{}, mailer: &fakeMailer{}}
	e.svc.composer, e.svc.signer, e.svc.links, e.svc.mailer = m.comp, m.sig, m.links, m.mailer
	e.svc.d.Keys = e.Keys
	m.insure(e.vessel)
	return m
}

// insure gives a vessel an insurance policy without expiry (insurance policy checks).
func (m *m4env) insure(vesselID string) {
	now := store.UTC(m.Clock.Now())
	_, err := m.DB.NewInsert().Model(&store.Document{
		ID: store.NewID(), MarinaID: m.MarinaID, OwnerType: "vessel", VesselID: vesselID, Type: "insurance_policy",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Exec(context.Background())
	if err != nil {
		panic(err)
	}
}
