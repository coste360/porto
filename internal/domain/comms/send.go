package comms

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Contract statuses that make a customer reachable by service communications (an active relation).
var activeContract = []string{"active", "awaiting_signature", "suspended", "confirmed", "arrived", "held"}

func intersect(a []string, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}

// resolve returns the customer ids of an audience (every criterion narrows it), sorted.
func (s *Service) resolve(ctx context.Context, db bun.IDB, scope store.MarinaScope, a Audience) ([]string, error) {
	ids, err := store.ReachableCustomerIDs(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	narrow := func(more []string, err error) error {
		if err != nil {
			return err
		}
		ids = intersect(ids, more)
		return nil
	}
	loc := s.location(ctx, db, scope)
	today := calendar.In(s.d.Clock.Now(), loc)
	if len(a.CustomerIDs) > 0 {
		ids = intersect(ids, a.CustomerIDs)
	}
	if len(a.ContractKinds) > 0 || len(a.Terms) > 0 {
		if err := narrow(store.ContractCustomerIDs(ctx, db, scope, activeContract, a.ContractKinds, a.Terms)); err != nil {
			return nil, err
		}
	}
	from, to := today, today.AddDays(3650)
	if a.PresentFrom != "" {
		from, _ = calendar.Parse(a.PresentFrom)
	}
	if a.PresentTo != "" {
		to, _ = calendar.Parse(a.PresentTo)
	}
	if a.PresentFrom != "" || a.PresentTo != "" || len(a.ResourceIDs) > 0 || len(a.Groups) > 0 || len(a.Structures) > 0 {
		var res, groups, structures []string
		if len(a.ResourceIDs) > 0 {
			res = a.ResourceIDs
		}
		if len(a.Groups) > 0 {
			groups = a.Groups
		}
		if len(a.Structures) > 0 {
			structures = a.Structures
		}
		if err := narrow(store.AllocationCustomerIDs(ctx, db, scope, from, to, res, groups, structures)); err != nil {
			return nil, err
		}
	}
	if a.Holders {
		if err := narrow(store.RightHolderCustomerIDs(ctx, db, scope)); err != nil {
			return nil, err
		}
	}
	if a.WithDeadlinesWithinDays > 0 {
		if err := narrow(store.DeadlineCustomerIDs(ctx, db, scope, today.AddDays(a.WithDeadlinesWithinDays))); err != nil {
			return nil, err
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func (s *Service) location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(m.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// recipient is a customer of the audience with the outcome of the consent and email rules.
type recipient struct {
	customer *store.Customer
	email    string
	status   string // pending, skipped_no_consent, skipped_no_email
}

// classify applies the consent rules (m4-plan §6.7): service messages go to customers with an active contract,
// transit or right; promotional ones only to customers whose latest marketing consent is granted.
func (s *Service) classify(ctx context.Context, db bun.IDB, scope store.MarinaScope, category string, ids []string) ([]recipient, error) {
	rows, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	if category == CategoryPromotional {
		consents, err := store.LatestConsents(ctx, db, scope, "marketing", ids)
		if err != nil {
			return nil, err
		}
		for id, granted := range consents {
			allowed[id] = granted
		}
	} else {
		active, err := store.ContractCustomerIDs(ctx, db, scope, activeContract, nil, nil)
		if err != nil {
			return nil, err
		}
		holders, err := store.RightHolderCustomerIDs(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		for _, id := range append(active, holders...) {
			allowed[id] = true
		}
	}
	out := make([]recipient, 0, len(rows))
	for i := range rows {
		c := &rows[i]
		r := recipient{customer: c, email: docs.FirstContact(c.Emails), status: store.RecipientPending}
		switch {
		case !allowed[c.ID]:
			r.status = store.RecipientSkippedNoConsent
		case r.email == "":
			r.status = store.RecipientSkippedNoEmail
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b recipient) int {
		if a.customer.ID < b.customer.ID {
			return -1
		}
		return 1
	})
	return out, nil
}

// viewOf is the merge data of a communication for one customer.
func (s *Service) viewOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Customer) (views.CommunicationView, error) {
	mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return views.CommunicationView{}, err
	}
	out := views.CommunicationView{Marina: mv, Customer: docs.PartyOf(c), Vessels: []views.Vessel{}, Contracts: []views.ContractInfo{}}
	parties, err := store.PartiesOfCustomers(ctx, db, scope, []string{c.ID})
	if err != nil {
		return out, err
	}
	var vids []string
	for _, p := range parties {
		if !slices.Contains(vids, p.VesselID) {
			vids = append(vids, p.VesselID)
		}
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err == nil {
		for _, v := range vs {
			out.Vessels = append(out.Vessels, views.Vessel{
				Name: v.Name, Registration: v.Registration, Flag: v.Flag, HullType: v.HullType,
				LOA: views.Cm(v.LOACm), Beam: views.Cm(v.BeamCm), Draft: views.Cm(v.DraftCm),
			})
		}
	}
	return out, nil
}

// Preview is the recipient counts and the message rendered for one customer.
type Preview struct {
	Count, SkippedNoConsent, SkippedNoEmail int
	Sample                                  *rendered
}

// PreviewCommunication counts the recipients and renders the message for one customer (the given one, else the
// first recipient) (communications.send).
func (s *Service) PreviewCommunication(ctx context.Context, p *auth.Principal, marinaID, id, customerID string) (*Preview, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	row, err := store.GetCommCampaign(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	c := decode(row)
	ids, err := s.resolve(ctx, db, scope, c.AudienceOf)
	if err != nil {
		return nil, err
	}
	recs, err := s.classify(ctx, db, scope, row.Category, ids)
	if err != nil {
		return nil, err
	}
	out := &Preview{Count: len(recs)}
	var sample *store.Customer
	for _, r := range recs {
		switch r.status {
		case store.RecipientSkippedNoConsent:
			out.SkippedNoConsent++
		case store.RecipientSkippedNoEmail:
			out.SkippedNoEmail++
		default:
			if sample == nil {
				sample = r.customer
			}
		}
	}
	if customerID != "" {
		cu, err := store.GetCustomer(ctx, db, scope, customerID)
		if err != nil {
			return nil, notFound(err)
		}
		sample = cu
	}
	if sample != nil && p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead) {
		v, err := s.viewOf(ctx, db, scope, sample)
		if err != nil {
			return nil, err
		}
		if out.Sample, err = s.render(ctx, db, scope, row, v, localeOf(sample)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func localeOf(c *store.Customer) string {
	if c.Locale == "en" {
		return "en"
	}
	return "it"
}

// RunSend processes the due communications (job comms.send, idempotent): a scheduled one whose time came moves to
// sending and gets its recipients (consent and email rules applied once); pending recipients are queued one
// transaction each with the dedupe key comm:<campaign>:<customer>; when none is left the communication is sent.
func (s *Service) RunSend(ctx context.Context) error {
	rows, err := store.DueCommCampaigns(ctx, s.d.DB.DB, s.now(), 50)
	if err != nil {
		return err
	}
	var first error
	for i := range rows {
		if err := s.process(ctx, &rows[i]); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) process(ctx context.Context, row *store.CommCampaign) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, row.MarinaID)
	if err != nil {
		return err
	}
	if row.Status == store.CommScheduled {
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			c := decode(row)
			ids, err := s.resolve(ctx, tx, scope, c.AudienceOf)
			if err != nil {
				return err
			}
			recs, err := s.classify(ctx, tx, scope, row.Category, ids)
			if err != nil {
				return err
			}
			now := s.now()
			list := make([]store.CommRecipient, 0, len(recs))
			for _, r := range recs {
				list = append(list, store.CommRecipient{
					ID: store.NewID(), MarinaID: scope.ID(), CampaignID: row.ID, CustomerID: r.customer.ID,
					Status: r.status, CreatedAt: now, UpdatedAt: now,
				})
			}
			for start := 0; start < len(list); start += 500 {
				if err := store.InsertCommRecipients(ctx, tx, list[start:min(start+500, len(list))]); err != nil {
					return err
				}
			}
			row.Status, row.UpdatedAt = store.CommSending, now
			if err := store.UpdateCommCampaign(ctx, tx, scope, row.Version, []string{store.CommScheduled}, row); err != nil {
				return err
			}
			row.Version++
			return s.write(ctx, tx, scope, audit.System, "communication.send", row.ID, map[string]any{"recipients": len(list)}, "sending")
		})
		if errors.Is(err, store.ErrVersionMismatch) {
			return nil // another run started it
		}
		if err != nil {
			return err
		}
	}
	pending, err := store.PendingRecipients(ctx, s.d.DB.DB, scope, row.ID, sendBatch)
	if err != nil {
		return err
	}
	for i := range pending {
		if err := s.queue(ctx, scope, row, &pending[i]); err != nil {
			s.d.Log.WarnContext(ctx, "communication recipient not queued", "campaign_id", row.ID, "recipient_id", pending[i].ID)
		}
	}
	if len(pending) == sendBatch {
		return nil // more next run
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		fresh, err := store.GetCommCampaign(ctx, tx, scope, row.ID)
		if err != nil || fresh.Status != store.CommSending {
			return err
		}
		left, err := store.PendingRecipients(ctx, tx, scope, row.ID, 1)
		if err != nil || len(left) > 0 {
			return err
		}
		counts, err := store.RecipientCounts(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		fresh.Status, fresh.SentAt, fresh.Counts, fresh.UpdatedAt = store.CommSent, s.now(), store.MustJSON(counts), s.now()
		if err := store.UpdateCommCampaign(ctx, tx, scope, fresh.Version, []string{store.CommSending}, fresh); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return nil
			}
			return err
		}
		return s.write(ctx, tx, scope, audit.System, "communication.send", row.ID, map[string]any{"status": store.CommSent, "counts": counts}, "sent")
	})
}

// queue renders and queues the email of one recipient (and calls the sms notifiers when selected).
func (s *Service) queue(ctx context.Context, scope store.MarinaScope, row *store.CommCampaign, r *store.CommRecipient) error {
	var phone, text string
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.GetCustomer(ctx, tx, scope, r.CustomerID)
		if err != nil {
			return err
		}
		email := docs.FirstContact(c.Emails)
		if email == "" {
			_, err := store.SetRecipientStatus(ctx, tx, scope, r.ID, store.RecipientSkippedNoEmail, "", s.now())
			return err
		}
		v, err := s.viewOf(ctx, tx, scope, c)
		if err != nil {
			return err
		}
		locale := localeOf(c)
		msg, err := s.render(ctx, tx, scope, row, v, locale)
		if err != nil {
			return err
		}
		values := map[string]string{notify.ValueSubject: msg.Subject, notify.ValueMarinaName: v.Marina.Name}
		var headers map[string]string
		if row.Category == CategoryPromotional && s.d.Links != nil {
			token, _, err := s.d.Links.CreateTx(ctx, tx, scope, magiclink.PurposeUnsubscribe, "customer", c.ID, c.ID, s.now().Add(unsubscribeTTL))
			if err != nil {
				return err
			}
			m, err := store.GetMarina(ctx, tx, scope)
			if err != nil {
				return err
			}
			values[notify.ValueLink] = s.d.Links.URL(m.Slug, magiclink.PurposeUnsubscribe, token)
			headers = map[string]string{
				"List-Unsubscribe":      "<" + s.d.Links.BaseURL() + "/api/public/v1/" + m.Slug + "/links/" + token + "/unsubscribe>",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			}
		}
		var atts []notify.AttachmentRef
		var list []Attachment
		_ = json.Unmarshal(row.Attachments, &list)
		for _, a := range list {
			atts = append(atts, notify.AttachmentRef{DocumentID: a.DocumentID, FileID: a.FileID})
		}
		category := notify.CategoryService
		if row.Category == CategoryPromotional {
			category = notify.CategoryPromotional
		}
		id, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
			Template: notify.TemplateCommunication, MarinaID: scope.ID(), CustomerID: c.ID, To: email, Locale: locale,
			DedupeKey: "comm:" + row.ID + ":" + c.ID, Category: category, SourceType: "communication", SourceID: row.ID,
			Values: values, BodyHTML: msg.HTML, BodyText: msg.Text, Attachments: atts, Headers: headers,
		})
		if err != nil {
			return err
		}
		ok, err := store.SetRecipientStatus(ctx, tx, scope, r.ID, store.RecipientQueued, id, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return errAlreadyQueued
		}
		if slices.Contains(row.Channels, ChannelSMS) {
			phone, text = docs.FirstContact(c.Phones), msg.Subject
		}
		return nil
	})
	if errors.Is(err, errAlreadyQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	if phone != "" {
		for _, n := range s.d.Notifiers {
			if n.Channel() == ChannelSMS {
				_ = n.Notify(context.WithoutCancel(ctx), ext.Notification{
					Channel: ChannelSMS, Template: notify.TemplateCommunication, To: phone, Data: map[string]string{notify.ValueSubject: text},
				})
			}
		}
	}
	return nil
}

var errAlreadyQueued = errors.New("comms: recipient already queued")

// Recipient is a recipient with its delivery status.
type Recipient struct {
	store.CommRecipientRow
	DisplayName string
}

// Recipients lists the recipients of a communication (communications.send; names with customers.read).
func (s *Service) Recipients(ctx context.Context, p *auth.Principal, marinaID, id, status, cursor string, limit int) (store.Page[Recipient], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return store.Page[Recipient]{}, err
	}
	if _, err := store.GetCommCampaign(ctx, s.d.DB.DB, scope, id); err != nil {
		return store.Page[Recipient]{}, notFound(err)
	}
	page, err := store.ListCommRecipients(ctx, s.d.DB.DB, scope, id, status, cursor, limit)
	if err != nil {
		return store.Page[Recipient]{}, err
	}
	pii := p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
	ids := make([]string, 0, len(page.Items))
	for _, r := range page.Items {
		ids = append(ids, r.CustomerID)
	}
	names := map[string]string{}
	if pii {
		cs, err := store.CustomersByIDs(ctx, s.d.DB.DB, scope, ids)
		if err != nil {
			return store.Page[Recipient]{}, err
		}
		for i := range cs {
			names[cs[i].ID] = docs.PartyOf(&cs[i]).DisplayName
		}
	}
	out := store.Page[Recipient]{NextCursor: page.NextCursor, Items: make([]Recipient, 0, len(page.Items))}
	for _, r := range page.Items {
		out.Items = append(out.Items, Recipient{CommRecipientRow: r, DisplayName: names[r.CustomerID]})
	}
	return out, nil
}

// CustomerMessages lists the emails sent to a customer with their delivery status (customers.read).
func (s *Service) CustomerMessages(ctx context.Context, p *auth.Principal, marinaID, customerID, cursor string, limit int) (store.Page[store.CustomerMessageRow], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return store.Page[store.CustomerMessageRow]{}, err
	}
	if _, err := store.GetCustomer(ctx, s.d.DB.DB, scope, customerID); err != nil {
		return store.Page[store.CustomerMessageRow]{}, notFound(err)
	}
	return store.ListCustomerMessages(ctx, s.d.DB.DB, scope, customerID, cursor, limit)
}

// Message is a row of the customer message registry with its delivery status.
type Message = store.CustomerMessageRow
