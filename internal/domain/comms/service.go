// Package comms holds the customer communications (spec 28.2; m4-plan §6.7): mass sends from a filter or a map
// selection, template or free text with merge fields, attachments, preview per recipient, scheduling, consent
// rules (service vs promotional), the per-customer message registry and the one-click unsubscribe. Extra channels
// only through registered ext.Notifier (best effort, not in message_log).
package comms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/markdown"
	"github.com/coste360/porto/internal/domain/docs/tmpl"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Categories (comm_campaigns.category).
const (
	CategoryService     = "service"
	CategoryPromotional = "promotional"
)

// Channels of a communication.
const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
)

// Unsubscribe links last a year; a new promotional email rotates the customer's link.
const unsubscribeTTL = 365 * 24 * time.Hour

// sendBatch is the number of recipients queued per job run and communication.
const sendBatch = 200

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Keys      *crypto.KeyRing
	Docs      *docs.Service
	Links     *magiclink.Service
	Notify    *notify.Service
	Notifiers []ext.Notifier
	Log       *slog.Logger
}

// Service is the communications service.
type Service struct {
	d    Deps
	wake chan struct{}
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d, wake: make(chan struct{}, 1)}
}

// Wake is signalled when a communication is sent now, so the comms.send job runs without waiting its period.
func (s *Service) Wake() <-chan struct{} { return s.wake }

func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// Audience selects the recipients: a filter, explicit customer ids, or a map selection (m4-plan §6.7). Every
// criterion given narrows the audience.
type Audience struct {
	Groups                  []string `json:"groups,omitempty"`
	Structures              []string `json:"structures,omitempty"`
	ContractKinds           []string `json:"contract_kinds,omitempty"`
	Terms                   []string `json:"terms,omitempty"`
	Holders                 bool     `json:"holders,omitempty"`
	PresentFrom             string   `json:"present_from,omitempty"`
	PresentTo               string   `json:"present_to,omitempty"`
	WithDeadlinesWithinDays int      `json:"with_deadlines_within_days,omitempty"`
	CustomerIDs             []string `json:"customer_ids,omitempty"`
	MapID                   string   `json:"map_id,omitempty"`
	ResourceIDs             []string `json:"resource_ids,omitempty"`
}

// Attachment is a generated document or an uploaded file attached to every email.
type Attachment struct {
	DocumentID string `json:"document_id,omitempty"`
	FileID     string `json:"file_id,omitempty"`
}

// Input creates a communication; Patch changes a draft (nil = unchanged).
type Input struct {
	Name, Category string
	Audience       Audience
	Channels       []string
	TemplateID     string
	Subject        string
	BodyMD         string
	Attachments    []Attachment
	ScheduledAt    time.Time
}

// Patch changes a draft.
type Patch struct {
	Name, Category    *string
	Audience          *Audience
	Channels          *[]string
	TemplateID        *string // "" clears
	Subject, BodyMD   *string
	Attachments       *[]Attachment
	ClearTemplateNull bool
}

// Communication is a campaign with its decoded fields.
type Communication struct {
	store.CommCampaign
	AudienceOf    Audience
	AttachmentsOf []Attachment
	CountsOf      map[string]int
}

func decode(row *store.CommCampaign) *Communication {
	c := &Communication{CommCampaign: *row, CountsOf: map[string]int{}}
	_ = json.Unmarshal(row.Audience, &c.AudienceOf)
	_ = json.Unmarshal(row.Attachments, &c.AttachmentsOf)
	_ = json.Unmarshal(row.Counts, &c.CountsOf)
	if c.AttachmentsOf == nil {
		c.AttachmentsOf = []Attachment{}
	}
	return c
}

func vErr(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v.Err()
}

// validateBody checks the subject and body as templates of kind communication (422 template_invalid).
func validateBody(subject, body string) error {
	if strings.ContainsAny(subject, "\r\n") {
		return vErr("subject", "invalid")
	}
	return docs.ValidateSource(docs.KindCommunication, docs.Source{Body: subject + "\n\n" + body})
}

func (s *Service) checkInput(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.CommCampaign) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(c.Name)); n < 1 || n > 120 {
		return vErr("name", "invalid")
	}
	if c.Category != CategoryService && c.Category != CategoryPromotional {
		return vErr("category", "invalid")
	}
	for _, ch := range c.Channels {
		if ch != ChannelEmail && ch != ChannelSMS {
			return vErr("channels", "invalid")
		}
	}
	if c.TemplateID != "" {
		t, err := store.GetTemplate(ctx, db, scope, c.TemplateID)
		if err != nil {
			return notFound(err)
		}
		if t.Kind != string(docs.KindCommunication) {
			return vErr("template_id", "kind_mismatch")
		}
	} else if strings.TrimSpace(c.BodyMD) == "" && c.Status != store.CommDraft {
		return vErr("body_md", "required")
	}
	var atts []Attachment
	_ = json.Unmarshal(c.Attachments, &atts)
	if len(atts) > 5 {
		return vErr("attachments", "too_many")
	}
	for _, a := range atts {
		switch {
		case a.DocumentID != "":
			if _, err := store.GetGeneratedDocument(ctx, db, scope, a.DocumentID); err != nil {
				return notFound(err)
			}
		case a.FileID != "":
			if _, err := store.GetStoredFile(ctx, db, scope, a.FileID); err != nil {
				return notFound(err)
			}
		default:
			return vErr("attachments", "invalid")
		}
	}
	var aud Audience
	_ = json.Unmarshal(c.Audience, &aud)
	for _, d := range []string{aud.PresentFrom, aud.PresentTo} {
		if d != "" {
			if _, err := calendar.Parse(d); err != nil {
				return vErr("audience.present", "invalid")
			}
		}
	}
	return validateBody(c.Subject, c.BodyMD)
}

func (s *Service) write(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, id string, diff any, change string) error {
	if err := s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "communication",
		EntityID: id, Diff: diff,
	}); err != nil {
		return err
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.CommunicationChanged, event.CommunicationChangedData{CampaignID: id, Change: change})
	return err
}

// Create creates a draft communication (communications.send).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in Input) (*Communication, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return nil, err
	}
	if len(in.Channels) == 0 {
		in.Channels = []string{ChannelEmail}
	}
	if in.Attachments == nil {
		in.Attachments = []Attachment{}
	}
	now := s.now()
	row := &store.CommCampaign{
		ID: store.NewID(), MarinaID: scope.ID(), Name: strings.TrimSpace(in.Name), Category: in.Category,
		Audience: store.MustJSON(in.Audience), Channels: in.Channels, TemplateID: in.TemplateID, Subject: in.Subject,
		BodyMD: in.BodyMD, Attachments: store.MustJSON(in.Attachments), Status: store.CommDraft, Counts: store.JSON(`{}`),
		CreatedByUserID: p.UserID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.checkInput(ctx, tx, scope, row); err != nil {
			return err
		}
		if err := store.InsertCommCampaign(ctx, tx, row); err != nil {
			return err
		}
		return s.write(ctx, tx, scope, p.Actor(), "communication.create", row.ID,
			map[string]any{"category": row.Category, "sha256": crypto.SHA256Hex(row.Subject + row.BodyMD)}, event.ChangeCreated)
	})
	if err != nil {
		return nil, err
	}
	if !in.ScheduledAt.IsZero() {
		return s.Schedule(ctx, p, marinaID, row.ID, row.Version, in.ScheduledAt)
	}
	return decode(row), nil
}

// Get returns a communication (communications.send).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Communication, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return nil, err
	}
	row, err := store.GetCommCampaign(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return decode(row), nil
}

// List returns the communications of the marina (communications.send).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID, status, cursor string, limit int) (store.Page[Communication], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return store.Page[Communication]{}, err
	}
	page, err := store.ListCommCampaigns(ctx, s.d.DB.DB, scope, status, cursor, limit)
	if err != nil {
		return store.Page[Communication]{}, err
	}
	out := store.Page[Communication]{NextCursor: page.NextCursor, Items: make([]Communication, 0, len(page.Items))}
	for i := range page.Items {
		out.Items = append(out.Items, *decode(&page.Items[i]))
	}
	return out, nil
}

// Update changes a draft (communications.send).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in Patch) (*Communication, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return nil, err
	}
	var out *Communication
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCommCampaign(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if row.Status != store.CommDraft {
			return ErrNotDraft
		}
		if in.Name != nil {
			row.Name = strings.TrimSpace(*in.Name)
		}
		if in.Category != nil {
			row.Category = *in.Category
		}
		if in.Audience != nil {
			row.Audience = store.MustJSON(*in.Audience)
		}
		if in.Channels != nil {
			row.Channels = *in.Channels
		}
		if in.TemplateID != nil {
			row.TemplateID = *in.TemplateID
		}
		if in.Subject != nil {
			row.Subject = *in.Subject
		}
		if in.BodyMD != nil {
			row.BodyMD = *in.BodyMD
		}
		if in.Attachments != nil {
			row.Attachments = store.MustJSON(*in.Attachments)
		}
		row.UpdatedAt = s.now()
		if err := s.checkInput(ctx, tx, scope, row); err != nil {
			return err
		}
		if err := store.UpdateCommCampaign(ctx, tx, scope, ifMatch, []string{store.CommDraft}, row); err != nil {
			return err
		}
		out = decode(row)
		return s.write(ctx, tx, scope, p.Actor(), "communication.update", row.ID,
			map[string]any{"sha256": crypto.SHA256Hex(row.Subject + row.BodyMD)}, event.ChangeUpdated)
	})
	return out, err
}

// ErrNotDraft means the communication is no longer a draft (409 conflict).
var ErrNotDraft = fmt.Errorf("comms: not a draft: %w", store.ErrConflict)

func (s *Service) transition(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, from []string, action, change string, mutate func(*store.CommCampaign) error) (*Communication, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCommunicationsSend)
	if err != nil {
		return nil, err
	}
	var out *Communication
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCommCampaign(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !contains(from, row.Status) {
			return ErrNotDraft
		}
		if err := mutate(row); err != nil {
			return err
		}
		row.UpdatedAt = s.now()
		if err := s.checkInput(ctx, tx, scope, row); err != nil {
			return err
		}
		if err := store.UpdateCommCampaign(ctx, tx, scope, ifMatch, from, row); err != nil {
			return err
		}
		out = decode(row)
		return s.write(ctx, tx, scope, p.Actor(), action, row.ID, map[string]any{"status": row.Status}, change)
	})
	return out, err
}

// Send starts the sending now; the comms.send job queues the recipients (communications.send).
func (s *Service) Send(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Communication, error) {
	defer s.kick() // after the commit of the transition: the job picks the communication up at once
	return s.transition(ctx, p, marinaID, id, ifMatch, []string{store.CommDraft, store.CommScheduled}, "communication.send", "sent",
		func(c *store.CommCampaign) error {
			c.Status, c.ScheduledAt = store.CommScheduled, s.now()
			return nil
		})
}

// Schedule plans the sending at an instant in the future (communications.send).
func (s *Service) Schedule(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, at time.Time) (*Communication, error) {
	return s.transition(ctx, p, marinaID, id, ifMatch, []string{store.CommDraft, store.CommScheduled}, "communication.schedule", "scheduled",
		func(c *store.CommCampaign) error {
			if !at.After(s.now()) {
				return vErr("scheduled_at", "past")
			}
			c.Status, c.ScheduledAt = store.CommScheduled, store.UTC(at)
			return nil
		})
}

// Cancel cancels a draft or scheduled communication (communications.send).
func (s *Service) Cancel(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Communication, error) {
	return s.transition(ctx, p, marinaID, id, ifMatch, []string{store.CommDraft, store.CommScheduled}, "communication.cancel", "cancelled",
		func(c *store.CommCampaign) error {
			c.Status, c.CancelledAt = store.CommCancelled, s.now()
			return nil
		})
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

// rendered is a message rendered for one customer.
type rendered struct {
	Subject, HTML, Text string
}

// render evaluates the subject and the body (free text or the published template in the customer's locale) on
// the customer's view; every value is escaped before the Markdown is parsed.
func (s *Service) render(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.CommCampaign, view views.CommunicationView, locale string) (*rendered, error) {
	body := c.BodyMD
	if c.TemplateID != "" {
		v, err := store.PublishedTemplateVersion(ctx, db, scope, c.TemplateID, locale)
		if err != nil {
			m, merr := store.GetMarina(ctx, db, scope)
			if merr != nil {
				return nil, merr
			}
			if v, err = store.PublishedTemplateVersion(ctx, db, scope, c.TemplateID, m.DefaultLocale); err != nil {
				return nil, fmt.Errorf("comms: template %s has no published version: %w", c.TemplateID, docs.ErrNotReady)
			}
		}
		body = v.BodyMD
	}
	data := docs.ToValue(view)
	env := tmpl.Env{Locale: locale, Today: calendar.In(s.d.Clock.Now(), time.UTC).String()}
	eval := func(src string) (string, error) {
		t, err := tmpl.Parse(src)
		if err != nil {
			return "", err
		}
		return t.Eval(data, env)
	}
	subj, err := eval(c.Subject)
	if err != nil {
		return nil, err
	}
	md, err := eval(body)
	if err != nil {
		return nil, err
	}
	blocks := markdown.Parse(md)
	subject := strings.TrimSpace(markdown.Text(markdown.Parse(subj)))
	return &rendered{Subject: subject, HTML: markdown.HTML(blocks), Text: markdown.Text(blocks)}, nil
}
