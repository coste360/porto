package comms

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/store"
)

// consentPolicyVersion is the policy version recorded with an unsubscription.
const consentPolicyVersion = "unsubscribe-link"

// PublicView reports whether the customer of an unsubscribe link still receives promotional emails.
func (s *Service) PublicView(ctx context.Context, l *magiclink.Link) (subscribed bool, err error) {
	if l.Purpose != magiclink.PurposeUnsubscribe || l.CustomerID == "" {
		return false, magiclink.ErrInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, l.MarinaID)
	if err != nil {
		return false, magiclink.ErrInvalid
	}
	m, err := store.LatestConsents(ctx, s.d.DB.DB, scope, "marketing", []string{l.CustomerID})
	if err != nil {
		return false, err
	}
	return m[l.CustomerID], nil
}

// Unsubscribe records the one-click unsubscription of the link's customer: a marketing consent granted=false with
// source online (idempotent: an already unsubscribed customer gets no new row).
func (s *Service) Unsubscribe(ctx context.Context, slug, token string) error {
	l, err := s.d.Links.ResolvePurpose(ctx, slug, token, magiclink.PurposeUnsubscribe)
	if err != nil {
		return err
	}
	if l.CustomerID == "" {
		return magiclink.ErrInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, l.MarinaID)
	if err != nil {
		return magiclink.ErrInvalid
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LatestConsents(ctx, tx, scope, "marketing", []string{l.CustomerID})
		if err != nil {
			return err
		}
		if v, ok := cur[l.CustomerID]; ok && !v {
			return nil
		}
		now := s.now()
		if err := store.InsertConsent(ctx, tx, &store.Consent{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: l.CustomerID, Kind: "marketing",
			PolicyVersion: consentPolicyVersion, Granted: false, Source: "online", RecordedAt: now, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: audit.System, Action: "consent.unsubscribe",
			EntityType: "customer", EntityID: l.CustomerID, Diff: map[string]any{"kind": "marketing", "granted": false},
		}); err != nil {
			return err
		}
		_, err = s.d.Outbox.Write(ctx, tx, &scope, event.CustomerUpdated, event.CustomerUpdatedData{CustomerID: l.CustomerID, Change: event.ChangeConsent})
		return err
	})
}

// DataSource returns the docs data source of kind communication (a letter-like PDF for one customer: subject
// communication, subject id the customer).
func (s *Service) DataSource() docs.DataSource { return commSource{s: s} }

type commSource struct{ s *Service }

func (commSource) Kinds() []docs.Kind { return []docs.Kind{docs.KindCommunication} }

func (c commSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, _ docs.Kind, _ docs.SubjectType, subjectID, _ string) (any, error) {
	cu, err := store.GetCustomer(ctx, tx, scope, subjectID)
	if err != nil {
		return nil, notFound(err)
	}
	return c.s.viewOf(ctx, tx, scope, cu)
}

func (commSource) DemoView(docs.Kind, string) (any, error) {
	return docs.DemoView(docs.KindCommunication), nil
}

var _ views.CommunicationView
