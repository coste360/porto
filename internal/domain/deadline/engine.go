package deadline

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// The deadline engine (spec 11.5; m4-plan §6.8, §15 D28): a periodic reconciliation of the deadlines with
// read-only source queries, idempotent through deadlines_source_uidx, plus the lead-day notifications.

// M4 kinds fed by the engine (M1 document kinds keep syncing on write).
const (
	KindContractEnd           = "contract_end"
	KindInstallmentDue        = "installment_due"
	KindHolderInstallmentDue  = "holder_installment_due"
	KindRightEnd              = "right_end"
	KindMandateEnd            = "mandate_end"
	KindConcessionExpiry      = "concession_expiry"
	KindConcessionObligation  = "concession_obligation"
	KindRenewalDeadline       = "renewal_deadline"
	KindTacitRenewalBlocked   = "tacit_renewal_blocked" // written by contract, not reconciled here
	KindWaitlistOffer         = "waitlist_offer"
	KindDocumentFailed        = "document_failed"
	KindSignatureExpiring     = "signature_expiring"
	signatureExpiringLeadDays = 3
	syncChunk                 = 500
)

// EngineKinds are the kinds reconciled by the engine.
func EngineKinds() []string {
	return []string{
		KindContractEnd, KindInstallmentDue, KindHolderInstallmentDue, KindRightEnd, KindMandateEnd,
		KindConcessionExpiry, KindConcessionObligation, KindRenewalDeadline, KindWaitlistOffer, KindDocumentFailed,
		KindSignatureExpiring,
	}
}

// M4Kinds are the kinds added by M4 (engine kinds and the contract-written ones).
func M4Kinds() []string { return append(EngineKinds(), KindTacitRenewalBlocked) }

// AllKinds are the M1, M4 and M9 kinds.
func AllKinds() []string { return append(append(Kinds(), M4Kinds()...), KindMaintenanceDue) }

// Channels of the notifications.
const (
	ChannelPanel         = "panel"
	ChannelCustomerEmail = "customer_email"
	ChannelOfficeEmail   = "office_email"
)

// stateOf maps the status of a source row to the state of its deadline ("" = no deadline wanted).
func stateOf(kind, status string) string {
	switch kind {
	case KindContractEnd:
		switch status {
		case "active", "suspended":
			return store.SourceOpen
		case "cancelled":
			return store.SourceDismissed
		default:
			return store.SourceDone
		}
	case KindInstallmentDue, KindHolderInstallmentDue:
		switch status {
		case "scheduled", "issued", "partially_paid":
			return store.SourceOpen
		case "paid":
			return store.SourceDone
		default:
			return store.SourceDismissed
		}
	case KindRightEnd, KindMandateEnd, KindConcessionExpiry:
		switch status {
		case "active":
			return store.SourceOpen
		case "cancelled", "revoked":
			return store.SourceDismissed
		default:
			return store.SourceDone
		}
	case KindConcessionObligation:
		if status == "open" {
			return store.SourceOpen
		}
		return store.SourceDone
	case KindRenewalDeadline:
		switch status {
		case "proposed", "sent", "accepted":
			return store.SourceOpen
		case "signed", "completed":
			return store.SourceDone
		default:
			return store.SourceDismissed
		}
	case KindWaitlistOffer:
		switch status {
		case "offered":
			return store.SourceOpen
		case "converted":
			return store.SourceDone
		default:
			return store.SourceDismissed
		}
	case KindDocumentFailed:
		if status == store.DocFailed {
			return store.SourceOpen
		}
		return store.SourceDone
	case KindSignatureExpiring:
		switch status {
		case store.SigPendingDocument, store.SigSent, store.SigPartiallySigned:
			return store.SourceOpen
		case store.SigSigned:
			return store.SourceDone
		default:
			return store.SourceDismissed
		}
	}
	return ""
}

// SyncResult counts the changes of a reconciliation.
type SyncResult struct {
	Created, Updated, Closed int
}

func (r *SyncResult) add(o SyncResult) {
	r.Created += o.Created
	r.Updated += o.Updated
	r.Closed += o.Closed
}

type key struct{ sourceType, sourceID, kind string }

// sources loads every engine source of a marina with its state and due date.
func (s *Service) sources(ctx context.Context, db bun.IDB, scope store.MarinaScope, loc *time.Location, today calendar.Date) ([]store.DeadlineSource, error) {
	loaders := []func(context.Context, bun.IDB, store.MarinaScope) ([]store.DeadlineSource, error){
		store.ContractEndSources, store.InstallmentSources, store.HolderInstallmentSources, store.RightEndSources,
		store.MandateEndSources, store.ConcessionSources, store.RenewalDeadlineSources, store.WaitlistOfferSources,
		store.FailedDocumentSources, store.SignatureExpirySources,
	}
	var out []store.DeadlineSource
	for _, load := range loaders {
		rows, err := load(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			r.State = stateOf(r.Kind, r.Status)
			if !r.At.IsZero() {
				r.DueOn = calendar.In(r.At, loc)
			}
			if r.Kind == KindSignatureExpiring && r.State == store.SourceOpen && r.DueOn.After(today.AddDays(signatureExpiringLeadDays)) {
				r.State = "" // not expiring yet
			}
			if r.State == "" || r.DueOn.IsZero() {
				continue
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// SyncMarina reconciles the engine deadlines of a marina with their sources: a new open source inserts an open
// deadline, a changed due date updates it (and reopens it), a settled or ended source closes it as done, a
// cancelled or vanished source dismisses it. Manual dismissals stay unless the due date changes. Running it twice
// changes nothing. The caller has authorised scope.
func (s *Service) SyncMarina(ctx context.Context, scope store.MarinaScope, a audit.Actor) (SyncResult, error) {
	db := s.d.DB.DB
	loc := s.location(ctx, db, scope)
	today := calendar.In(s.d.Clock.Now(), loc)
	srcs, err := s.sources(ctx, db, scope, loc, today)
	if err != nil {
		return SyncResult{}, err
	}
	existing, err := store.EngineDeadlines(ctx, db, scope, EngineKinds())
	if err != nil {
		return SyncResult{}, err
	}
	rows := map[key]*store.Deadline{}
	for i := range existing {
		d := &existing[i]
		rows[key{d.SourceType, d.SourceID, d.Kind}] = d
	}
	seen := map[key]bool{}
	type op struct {
		src *store.DeadlineSource
		row *store.Deadline
	}
	var ops []op
	for i := range srcs {
		k := key{srcs[i].SourceType, srcs[i].SourceID, srcs[i].Kind}
		seen[k] = true
		ops = append(ops, op{src: &srcs[i], row: rows[k]})
	}
	for k, d := range rows {
		if !seen[k] && d.Status == StatusOpen {
			ops = append(ops, op{row: d})
		}
	}
	sort.SliceStable(ops, func(i, j int) bool {
		return keyOf(ops[i].src, ops[i].row) < keyOf(ops[j].src, ops[j].row)
	})
	var total SyncResult
	for start := 0; start < len(ops); start += syncChunk {
		chunk := ops[start:min(start+syncChunk, len(ops))]
		var res SyncResult
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			res = SyncResult{}
			for _, o := range chunk {
				change, err := s.apply(ctx, tx, scope, o.src, o.row)
				if err != nil {
					return err
				}
				switch change {
				case event.ChangeCreated:
					res.Created++
				case event.ChangeUpdated:
					res.Updated++
				case "closed":
					res.Closed++
				}
			}
			if res == (SyncResult{}) {
				return nil
			}
			return s.d.Audit.Write(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: "deadline.sync",
				EntityType: "marina", EntityID: scope.ID(),
				Diff: map[string]int{"created": res.Created, "updated": res.Updated, "closed": res.Closed},
			})
		})
		if err != nil {
			return total, err
		}
		total.add(res)
	}
	return total, nil
}

func keyOf(src *store.DeadlineSource, row *store.Deadline) string {
	if src != nil {
		return src.SourceType + "|" + src.SourceID + "|" + src.Kind
	}
	return row.SourceType + "|" + row.SourceID + "|" + row.Kind
}

// apply makes one deadline match its source; it returns the change made ("" = none).
func (s *Service) apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, src *store.DeadlineSource, row *store.Deadline) (string, error) {
	now := s.now()
	if row == nil {
		if src == nil || src.State != store.SourceOpen {
			return "", nil
		}
		d := &store.Deadline{
			ID: store.NewID(), MarinaID: scope.ID(), SourceType: src.SourceType, SourceID: src.SourceID, Kind: src.Kind,
			CustomerID: src.CustomerID, VesselID: src.VesselID, DueOn: src.DueOn, Status: StatusOpen,
			NotifiedAt: store.JSON("[]"), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		ok, err := store.InsertDeadlineIfAbsent(ctx, tx, d)
		if err != nil || !ok {
			return "", err
		}
		return event.ChangeCreated, s.emit(ctx, tx, scope, d.ID, event.ChangeCreated)
	}
	next := *row
	change := ""
	switch {
	case src == nil || src.State == store.SourceDismissed:
		if row.Status == StatusOpen {
			next.Status, change = StatusDismissed, "closed"
		}
	case src.State == store.SourceDone:
		if row.Status == StatusOpen {
			next.Status, change = StatusDone, "closed"
		}
	default: // open source
		if !row.DueOn.Equal(src.DueOn) {
			next.DueOn, next.Status, next.NotifiedAt, change = src.DueOn, StatusOpen, store.JSON("[]"), event.ChangeUpdated
		} else if row.Status == StatusDone {
			next.Status, change = StatusOpen, event.ChangeUpdated // the source reopened (e.g. a retried render failed again)
		}
		if row.CustomerID != src.CustomerID || row.VesselID != src.VesselID {
			next.CustomerID, next.VesselID = src.CustomerID, src.VesselID
			if change == "" {
				change = event.ChangeUpdated
			}
		}
	}
	if change == "" {
		return "", nil
	}
	next.UpdatedAt = now
	update := store.UpdateDeadline
	if !row.DueOn.Equal(next.DueOn) {
		update = store.UpdateDeadlineAndNotified // a new due date notifies again from the first lead day
	}
	if err := update(ctx, tx, scope, row.Version, &next); err != nil {
		if errors.Is(err, store.ErrVersionMismatch) {
			return "", nil // a parallel sync got there first
		}
		return "", err
	}
	return change, s.emit(ctx, tx, scope, row.ID, event.ChangeUpdated)
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id, change string) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.DeadlineChanged, event.DeadlineChangedData{DeadlineID: id, Change: change})
	return err
}

// Sync runs the engine for one marina now (contracts.write; POST deadlines/sync).
func (s *Service) Sync(ctx context.Context, p *auth.Principal, marinaID string) (SyncResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return SyncResult{}, err
	}
	return s.SyncMarina(ctx, scope, p.Actor())
}

// RunSync reconciles the deadlines of every marina with their sources (job deadlines.sync).
func (s *Service) RunSync(ctx context.Context) error {
	ids, err := store.ActiveMarinaIDs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if _, err := s.SyncMarina(ctx, scope, audit.System); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// policyOf is the lead days and channels of every kind of a marina, defaults included.
type policyOf struct {
	leads    map[string][]int
	channels map[string][]string
}

func loadPolicies(ctx context.Context, db bun.IDB, scope store.MarinaScope) (policyOf, error) {
	rows, err := store.ListDeadlinePolicies(ctx, db, scope)
	if err != nil {
		return policyOf{}, err
	}
	p := policyOf{leads: map[string][]int{}, channels: map[string][]string{}}
	for _, k := range AllKinds() {
		p.leads[k] = DefaultLeadDays(k)
		p.channels[k] = []string{ChannelPanel}
	}
	for _, r := range rows {
		var days []int
		if json.Unmarshal(r.LeadDays, &days) == nil && len(days) > 0 {
			p.leads[r.Kind] = days
		}
		var ch []string
		if json.Unmarshal(r.Channels, &ch) == nil && len(ch) > 0 {
			p.channels[r.Kind] = ch
		}
	}
	return p, nil
}

func validChannels(ch []string) bool {
	for _, c := range ch {
		if !slices.Contains([]string{ChannelPanel, ChannelCustomerEmail, ChannelOfficeEmail}, c) {
			return false
		}
	}
	return true
}
