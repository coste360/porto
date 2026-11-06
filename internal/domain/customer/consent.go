package customer

import (
	"context"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Consent kinds and sources (m1-plan §3.1).
var (
	consentKinds   = []string{"privacy", "marketing", "third_party"}
	consentSources = []string{"desk", "online", "import", "api"}
)

// UserRef names a staff user.
type UserRef struct {
	ID, Name string
}

// Consent is one recorded consent (consents are versioned and never updated).
type Consent struct {
	ID            string
	Kind          string
	PolicyVersion string
	Granted       bool
	Source        string
	RecordedBy    *UserRef
	RecordedAt    time.Time
}

// ConsentInput records a consent.
type ConsentInput struct {
	Kind          string
	PolicyVersion string
	Granted       bool
	Source        string
}

// UserRefs resolves user ids to names (staff users are not customer PII).
func UserRefs(ctx context.Context, db bun.IDB, ids []string) (map[string]*UserRef, error) {
	out := map[string]*UserRef{}
	var uniq []string
	for _, id := range ids {
		if id != "" && !slices.Contains(uniq, id) {
			uniq = append(uniq, id)
		}
	}
	if len(uniq) == 0 {
		return out, nil
	}
	users, err := store.UsersByIDs(ctx, db, uniq)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = &UserRef{ID: u.ID, Name: u.Name}
	}
	return out, nil
}

func toConsents(ctx context.Context, db bun.IDB, rows []store.Consent) ([]Consent, error) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.RecordedByUserID)
	}
	refs, err := UserRefs(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Consent, 0, len(rows))
	for _, r := range rows {
		out = append(out, Consent{
			ID: r.ID, Kind: r.Kind, PolicyVersion: r.PolicyVersion, Granted: r.Granted, Source: r.Source,
			RecordedBy: refs[r.RecordedByUserID], RecordedAt: r.RecordedAt,
		})
	}
	return out, nil
}

// CurrentConsents returns the latest consent of each kind from a history sorted newest first.
func CurrentConsents(history []Consent) []Consent {
	out := []Consent{}
	seen := map[string]bool{}
	for _, c := range history {
		if !seen[c.Kind] {
			seen[c.Kind] = true
			out = append(out, c)
		}
	}
	return out
}

// ListConsents returns the consent history of a customer, newest first (customers.read).
func (s *Service) ListConsents(ctx context.Context, p *auth.Principal, marinaID, customerID string) ([]Consent, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetCustomer(ctx, s.d.DB.DB, scope, customerID); err != nil {
		return nil, notFound(err)
	}
	rows, err := store.ListConsents(ctx, s.d.DB.DB, scope, customerID)
	if err != nil {
		return nil, err
	}
	return toConsents(ctx, s.d.DB.DB, rows)
}

// RecordConsent appends a consent record (customers.write). Anonymised or merged customers are refused.
func (s *Service) RecordConsent(ctx context.Context, p *auth.Principal, marinaID, customerID string, in ConsentInput) (*Consent, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !slices.Contains(consentKinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	if !slices.Contains(consentSources, in.Source) {
		v.Add("source", "invalid")
	}
	pv, ok := clean(in.PolicyVersion, 32)
	if !ok || pv == "" {
		v.Add("policy_version", "invalid")
	}
	var out *Consent
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.GetCustomer(ctx, tx, scope, customerID)
		if err != nil {
			return notFound(err)
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := modifiable(c); err != nil {
			return err
		}
		now := s.now()
		row := &store.Consent{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: c.ID, Kind: in.Kind, PolicyVersion: pv,
			Granted: in.Granted, Source: in.Source, RecordedByUserID: p.UserID, RecordedAt: now, CreatedAt: now,
		}
		if err := store.InsertConsent(ctx, tx, row); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "customer.consent", c.ID, map[string]any{
			"kind": row.Kind, "granted": row.Granted, "policy_version": row.PolicyVersion, "source": row.Source,
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerUpdated, event.CustomerUpdatedData{CustomerID: c.ID, Change: event.ChangeConsent}); err != nil {
			return err
		}
		cs, err := toConsents(ctx, tx, []store.Consent{*row})
		if err != nil {
			return err
		}
		out = &cs[0]
		return nil
	})
	return out, err
}
