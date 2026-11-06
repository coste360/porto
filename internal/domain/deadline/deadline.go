package deadline

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Kinds of M1 deadlines (m1-plan §3.2), derived from document types. Later milestones add more; kinds are
// validated here, not by a DB CHECK (D17).
const (
	KindIdentityDocument        = "identity_document"
	KindBoatingLicence          = "boating_licence"
	KindNavigationLicence       = "navigation_licence"
	KindRegistrationCertificate = "registration_certificate"
	KindInsurance               = "insurance"
	KindSafetyCertificate       = "safety_certificate"
	KindPowerDeclaration        = "power_declaration"
	KindOtherDocument           = "other_document"
)

// Kinds returns the M1 kinds in a stable order.
func Kinds() []string {
	return []string{
		KindIdentityDocument, KindBoatingLicence, KindNavigationLicence, KindRegistrationCertificate,
		KindInsurance, KindSafetyCertificate, KindPowerDeclaration, KindOtherDocument,
	}
}

// Source types and statuses.
const (
	SourceDocument  = "document"
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusDismissed = "dismissed"
)

var statuses = []string{StatusOpen, StatusDone, StatusDismissed}

// Urgencies (computed when read).
const (
	UrgencyOverdue = store.UrgencyOverdue
	UrgencyDueSoon = store.UrgencyDueSoon
	UrgencyLater   = store.UrgencyLater
)

// Lead-day limits (m1-plan §3.1: sorted desc, each 1..730).
const (
	maxLeadDays    = 730
	maxLeadEntries = 5
	maxNote        = 1000
)

// KindForDocumentType returns the deadline kind fed by a document type, "" for types without a deadline
// (photo, signed_contract).
func KindForDocumentType(t string) string {
	switch t {
	case "identity_card", "passport", "driving_licence":
		return KindIdentityDocument
	case "boating_licence":
		return KindBoatingLicence
	case "navigation_licence":
		return KindNavigationLicence
	case "registration_certificate":
		return KindRegistrationCertificate
	case "insurance_policy":
		return KindInsurance
	case "safety_certificate":
		return KindSafetyCertificate
	case "power_declaration":
		return KindPowerDeclaration
	case "proxy", "other":
		return KindOtherDocument
	}
	return ""
}

// DefaultLeadDays returns the default lead days of a kind: 60 and 15 for insurance, 30 for the others.
func DefaultLeadDays(kind string) []int {
	if kind == KindInsurance {
		return []int{60, 15}
	}
	return []int{30}
}

// Urgency classifies a due date: overdue before today, due soon within the largest lead day, else later.
func Urgency(due, today calendar.Date, leadDays []int) string {
	switch {
	case due.Before(today):
		return UrgencyOverdue
	case !due.After(today.AddDays(maxOf(leadDays))):
		return UrgencyDueSoon
	default:
		return UrgencyLater
	}
}

func maxOf(xs []int) int {
	m := 0
	for _, x := range xs {
		m = max(m, x)
	}
	return m
}

// SyncDocument keeps the deadline of a document in step with it, inside the caller's transaction (m1-plan
// §3.2): a document with an expiry and a kind upserts its deadline (a changed due date reopens a done one),
// otherwise the document's deadlines are deleted. It returns the deadline id, "" when there is none.
func SyncDocument(ctx context.Context, tx bun.IDB, scope store.MarinaScope, d *store.Document, now time.Time) (string, error) {
	kind := KindForDocumentType(d.Type)
	if kind == "" || d.ExpiresOn.IsZero() {
		return "", store.DeleteDeadlinesOfSource(ctx, tx, scope, SourceDocument, d.ID, "")
	}
	if err := store.DeleteDeadlinesOfSource(ctx, tx, scope, SourceDocument, d.ID, kind); err != nil {
		return "", err
	}
	existing, err := store.DeadlinesOfSource(ctx, tx, scope, SourceDocument, d.ID)
	if err != nil {
		return "", err
	}
	if len(existing) == 0 {
		row := &store.Deadline{
			ID: store.NewID(), MarinaID: scope.ID(), SourceType: SourceDocument, SourceID: d.ID, Kind: kind,
			CustomerID: d.CustomerID, VesselID: d.VesselID, DueOn: d.ExpiresOn, Status: StatusOpen,
			NotifiedAt: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		return row.ID, store.InsertDeadline(ctx, tx, row)
	}
	row := &existing[0]
	if row.DueOn.Equal(d.ExpiresOn) && row.CustomerID == d.CustomerID && row.VesselID == d.VesselID {
		return row.ID, nil
	}
	if !row.DueOn.Equal(d.ExpiresOn) && row.Status == StatusDone {
		row.Status = StatusOpen
	}
	row.DueOn, row.CustomerID, row.VesselID, row.UpdatedAt = d.ExpiresOn, d.CustomerID, d.VesselID, now
	return row.ID, store.UpdateDeadline(ctx, tx, scope, row.Version, row)
}

// Policy is the lead days of a kind.
type Policy struct {
	Kind     string
	LeadDays []int
}

// policies returns the lead days of every M1 kind, stored values first, defaults otherwise.
func policies(ctx context.Context, db bun.IDB, scope store.MarinaScope) (map[string][]int, error) {
	rows, err := store.ListDeadlinePolicies(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := map[string][]int{}
	for _, k := range Kinds() {
		out[k] = DefaultLeadDays(k)
	}
	for _, r := range rows {
		var days []int
		if json.Unmarshal(r.LeadDays, &days) == nil && len(days) > 0 {
			out[r.Kind] = days
		}
	}
	return out, nil
}

// Ref names a related customer or vessel.
type Ref struct {
	ID, Name string
}

// Deadline is the API view of a deadline.
type Deadline struct {
	ID, Kind, SourceType, SourceID string
	Customer                       *Ref
	Vessel                         *Ref
	DocumentType                   string
	DueOn                          calendar.Date
	Status, Urgency, Note          string
	Version                        int
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// today returns the current date in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) (calendar.Date, error) {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return calendar.Date{}, err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return calendar.In(s.d.Clock.Now(), loc), nil
}

func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Deadline) ([]Deadline, error) {
	today, err := s.today(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	leads, err := policies(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var cids, vids, dids []string
	for _, r := range rows {
		cids = append(cids, r.CustomerID)
		vids = append(vids, r.VesselID)
		if r.SourceType == SourceDocument {
			dids = append(dids, r.SourceID)
		}
	}
	custs, err := store.CustomersByIDs(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	cnames := map[string]string{}
	for i := range custs {
		cnames[custs[i].ID] = customer.DisplayName(&custs[i])
	}
	vessels, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vnames := map[string]string{}
	for _, v := range vessels {
		vnames[v.ID] = v.Name
	}
	dtypes, err := store.DocumentTypesByIDs(ctx, db, scope, dids)
	if err != nil {
		return nil, err
	}
	out := make([]Deadline, 0, len(rows))
	for _, r := range rows {
		lead, ok := leads[r.Kind]
		if !ok {
			lead = DefaultLeadDays(r.Kind)
		}
		d := Deadline{
			ID: r.ID, Kind: r.Kind, SourceType: r.SourceType, SourceID: r.SourceID, DocumentType: dtypes[r.SourceID],
			DueOn: r.DueOn, Status: r.Status, Urgency: Urgency(r.DueOn, today, lead), Note: r.Note, Version: r.Version,
		}
		if r.CustomerID != "" {
			d.Customer = &Ref{ID: r.CustomerID, Name: cnames[r.CustomerID]}
		}
		if r.VesselID != "" {
			d.Vessel = &Ref{ID: r.VesselID, Name: vnames[r.VesselID]}
		}
		out = append(out, d)
	}
	return out, nil
}

// Filter narrows List. Status "" means open; "any" lists every status.
type Filter struct {
	Status     string
	Kind       string
	Urgency    string
	DueBefore  calendar.Date
	DueAfter   calendar.Date
	CustomerID string
	VesselID   string
}

// StatusAny lists deadlines in every status.
const StatusAny = "any"

// List returns a page of deadlines ordered by due date (customers.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[Deadline], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return store.Page[Deadline]{}, err
	}
	v := &auth.ValidationError{}
	sf := store.DeadlineFilter{Kind: f.Kind, CustomerID: f.CustomerID, VesselID: f.VesselID, DueBefore: f.DueBefore, DueAfter: f.DueAfter, Urgency: f.Urgency}
	switch {
	case f.Status == "":
		sf.Statuses = []string{StatusOpen}
	case f.Status == StatusAny:
	case slices.Contains(statuses, f.Status):
		sf.Statuses = []string{f.Status}
	default:
		v.Add("status", "invalid")
	}
	if f.Kind != "" && !slices.Contains(Kinds(), f.Kind) {
		v.Add("kind", "invalid")
	}
	if f.Urgency != "" && !slices.Contains([]string{UrgencyOverdue, UrgencyDueSoon, UrgencyLater}, f.Urgency) {
		v.Add("urgency", "invalid")
	}
	if err := v.Err(); err != nil {
		return store.Page[Deadline]{}, err
	}
	var out store.Page[Deadline]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if f.Urgency != "" {
			if err := s.urgencyFilter(ctx, tx, scope, &sf); err != nil {
				return err
			}
		}
		page, err := store.ListDeadlines(ctx, tx, scope, sf, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.views(ctx, tx, scope, page.Items)
		if err != nil {
			return err
		}
		out = store.Page[Deadline]{Items: items, NextCursor: page.NextCursor}
		return nil
	})
	return out, err
}

func (s *Service) urgencyFilter(ctx context.Context, db bun.IDB, scope store.MarinaScope, sf *store.DeadlineFilter) error {
	today, err := s.today(ctx, db, scope)
	if err != nil {
		return err
	}
	leads, err := policies(ctx, db, scope)
	if err != nil {
		return err
	}
	sf.Today = today
	sf.SoonUntil = map[string]calendar.Date{}
	for k, days := range leads {
		sf.SoonUntil[k] = today.AddDays(maxOf(days))
	}
	sf.DefaultSoonUntil = today.AddDays(maxOf(DefaultLeadDays("")))
	return nil
}

// ForCustomer returns the open deadlines of a customer and of its vessels (customer page and GDPR export).
// all=true includes every status.
func (s *Service) ForCustomer(ctx context.Context, p *auth.Principal, marinaID, customerID string, vesselIDs []string, all bool) ([]Deadline, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	sf := store.DeadlineFilter{CustomerOrVessels: &store.CustomerVessels{CustomerID: customerID, VesselIDs: vesselIDs}}
	if !all {
		sf.Statuses = []string{StatusOpen}
	}
	var out []Deadline
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cursor := ""
		for {
			page, err := store.ListDeadlines(ctx, tx, scope, sf, cursor, store.MaxLimit)
			if err != nil {
				return err
			}
			items, err := s.views(ctx, tx, scope, page.Items)
			if err != nil {
				return err
			}
			out = append(out, items...)
			if page.NextCursor == "" {
				return nil
			}
			cursor = page.NextCursor
		}
	})
	if out == nil {
		out = []Deadline{}
	}
	return out, err
}

// Patch changes a deadline (nil = unchanged).
type Patch struct {
	Status *string
	Note   *string
}

// Update marks a deadline done or dismissed (or reopens it) and edits its note (customers.write, If-Match).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in Patch) (*Deadline, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Deadline
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetDeadline(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		v := &auth.ValidationError{}
		var changed []string
		if in.Status != nil {
			if !slices.Contains(statuses, *in.Status) {
				v.Add("status", "invalid")
			} else if row.Status != *in.Status {
				row.Status = *in.Status
				changed = append(changed, "status")
			}
		}
		if in.Note != nil {
			if utf8.RuneCountInString(*in.Note) > maxNote {
				v.Add("note", "too_long")
			} else if row.Note != *in.Note {
				row.Note = *in.Note
				changed = append(changed, "note")
			}
		}
		if err := v.Err(); err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		row.UpdatedAt = s.now()
		if err := store.UpdateDeadline(ctx, tx, scope, ifMatch, row); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return notFound(err)
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "deadline.update",
			EntityType: "deadline", EntityID: row.ID, Diff: map[string]any{"fields": changed, "status": row.Status},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.DeadlineChanged, event.DeadlineChangedData{DeadlineID: row.ID, Change: event.ChangeUpdated}); err != nil {
			return err
		}
		views, err := s.views(ctx, tx, scope, []store.Deadline{*row})
		if err != nil {
			return err
		}
		out = &views[0]
		return nil
	})
	return out, err
}

// ListPolicies returns the lead days of every M1 kind, defaults included (customers.read).
func (s *Service) ListPolicies(ctx context.Context, p *auth.Principal, marinaID string) ([]Policy, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	leads, err := policies(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Policy, 0, len(leads))
	for _, k := range Kinds() {
		out = append(out, Policy{Kind: k, LeadDays: leads[k]})
	}
	return out, nil
}

// ReplacePolicies stores the lead days of the listed kinds; kinds not listed go back to their defaults
// (marina.configure).
func (s *Service) ReplacePolicies(ctx context.Context, p *auth.Principal, marinaID string, in []Policy) ([]Policy, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	now := s.now()
	var rows []store.DeadlinePolicy
	seen := map[string]bool{}
	for i, pol := range in {
		field := "items[" + strconv.Itoa(i) + "]"
		if !slices.Contains(Kinds(), pol.Kind) || seen[pol.Kind] {
			v.Add(field+".kind", "invalid")
			continue
		}
		seen[pol.Kind] = true
		days := slices.Clone(pol.LeadDays)
		slices.Sort(days)
		days = slices.Compact(days)
		slices.Reverse(days)
		if len(days) == 0 || len(days) > maxLeadEntries || days[len(days)-1] < 1 || days[0] > maxLeadDays {
			v.Add(field+".lead_days", "invalid")
			continue
		}
		rows = append(rows, store.DeadlinePolicy{
			ID: store.NewID(), MarinaID: scope.ID(), Kind: pol.Kind, LeadDays: store.MustJSON(days), CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.ReplaceDeadlinePolicies(ctx, tx, scope, rows); err != nil {
			return err
		}
		kinds := make([]string, 0, len(rows))
		for _, r := range rows {
			kinds = append(kinds, r.Kind)
		}
		return s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "deadline_policies.replace",
			EntityType: "marina", EntityID: scope.ID(), Diff: map[string]any{"kinds": kinds},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.ListPolicies(ctx, p, marinaID)
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}
