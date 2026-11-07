// Package docs holds print templates and their versions, the field catalogue, the compose and render pipeline
// of generated documents (archive with SHA-256 and template version), signed download links and the map print
// (spec 15.1–15.2; m4-plan §6.1–§6.4).
//
// Compose runs inside the caller's transaction and stores an encrypted data snapshot (data_enc); rendering runs
// after commit (RenderPending, job documents.render) and never inside a write transaction (§15 D23). PDFs are
// rendered in pure Go (docs/markdown, docs/layout, docs/pdf) and stored encrypted through internal/filestore.
// Every exported method authorises first; state changes write audit and outbox in the same transaction.
package docs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Keys   *crypto.KeyRing
	Files  *filestore.Store
	Notify *notify.Service
	Log    *slog.Logger
}

// Service is the documents service.
type Service struct {
	d Deps

	mu      sync.RWMutex
	sources map[Kind]DataSource
	ready   []ReadyHook
	wake    chan struct{}
	linkKey []byte
}

// ReadyHook runs after a document became ready (outside any transaction). The signature service registers one
// to invite the signers of requests waiting for their document.
type ReadyHook func(ctx context.Context, marinaID, documentID string)

// NewService returns the service and registers it as the attachment loader of notify.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d, sources: map[Kind]DataSource{}, wake: make(chan struct{}, 1)}
	if d.Keys != nil {
		if k, err := d.Keys.Derive("docs.download-link", 32); err == nil {
			s.linkKey = k
		}
	}
	s.RegisterDataSource(marinaSource{s: s})
	if d.Notify != nil {
		d.Notify.SetAttachmentLoader(s)
	}
	return s
}

// RegisterDataSource registers the data source of its kinds (called by internal/app at wiring).
func (s *Service) RegisterDataSource(ds DataSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range ds.Kinds() {
		s.sources[k] = ds
	}
}

// DataSourceFor returns the data source registered for kind.
func (s *Service) DataSourceFor(kind Kind) (DataSource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.sources[kind]
	return ds, ok
}

// OnReady registers a hook run after a document is rendered.
func (s *Service) OnReady(h ReadyHook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = append(s.ready, h)
}

func (s *Service) readyHooks() []ReadyHook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ReadyHook(nil), s.ready...)
}

// Wake is signalled when a composed document waits for rendering (the documents.render job listens on it).
func (s *Service) Wake() <-chan struct{} { return s.wake }

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// today is the marina date of an instant.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope, at time.Time) string {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return calendar.In(at, time.UTC).String()
	}
	loc, err := calendar.LoadLocation(m.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return calendar.In(at, loc).String()
}

func actorOf(userID string) audit.Actor {
	if userID == "" {
		return audit.System
	}
	return audit.Actor{Type: audit.ActorUser, ID: userID}
}

func (s *Service) write(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entityType, id string, diff any, typ string, data any) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entityType,
		EntityID: id, Diff: diff,
	}); err != nil {
		return err
	}
	if typ == "" {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, typ, data)
	return err
}

// subjectPermission is the permission needed to read a document of a subject type (m4-plan §7.2).
func subjectPermission(subjectType string) auth.Permission {
	switch SubjectType(subjectType) {
	case SubjectRightTransfer, SubjectHolderChain, SubjectMandateStatement, SubjectFeeNotice,
		SubjectCommonChargesHolder, SubjectCommonChargesItems:
		return auth.PermRightsManage
	case SubjectMapPrint:
		return auth.PermMapView
	case SubjectCommunication:
		return auth.PermCommunicationsSend
	default:
		return auth.PermContractsRead
	}
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// ErrNotImplemented is returned by RenderEphemeral until CHARGES-EXPORT lands the implementation (ephemeral.go).
var ErrNotImplemented = errors.New("docs: not implemented")

// RenderEphemeral renders a document of an M5 ephemeral kind (receipt, statement_of_account,
// charge_export_summary, m5-plan §8.3) with the marina's published template of the kind or the built-in default,
// in the given locale, from a view (views.ReceiptView, views.StatementView, views.ChargeExportSummaryView). The
// PDF is not archived in generated_documents; the same view gives the same bytes.
func (s *Service) RenderEphemeral(ctx context.Context, scope store.MarinaScope, kind Kind, locale string, view any) ([]byte, error) {
	return s.renderEphemeralImpl(ctx, scope, kind, locale, view)
}
