package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/store"
)

// M4 additions (m4-plan §6.7): customer and office emails with display values and attachments, the per-customer
// message registry (customer_messages) and attachment loading at send time.

// MaxAttachmentBytes is the total size of the attachments of one email.
const MaxAttachmentBytes = 10 << 20

// Message categories of the customer registry (customer_messages.category).
const (
	CategoryTransactional = "transactional"
	CategoryService       = "service"
	CategoryPromotional   = "promotional"
)

// ErrAttachmentsTooLarge means the attachments of an email exceed MaxAttachmentBytes.
var ErrAttachmentsTooLarge = errors.New("notify: attachments too large")

// AttachmentRef points to an attachment loaded at send time: a generated document or a stored file of the marina.
// Only the reference travels in the encrypted payload.
type AttachmentRef struct {
	DocumentID string `json:"document_id,omitempty"`
	FileID     string `json:"file_id,omitempty"`
}

// Attachment is a loaded attachment.
type Attachment struct {
	Name        string
	ContentType string
	Content     []byte
}

// AttachmentLoader loads an attachment of a marina (docs registers itself at wiring).
type AttachmentLoader interface {
	LoadAttachment(ctx context.Context, marinaID string, ref AttachmentRef) (*Attachment, error)
}

// SetAttachmentLoader registers the loader of attachments (called by docs.NewService).
func (s *Service) SetAttachmentLoader(l AttachmentLoader) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.attach = l
}

func (s *Service) loader() AttachmentLoader {
	s.attachMu.RLock()
	defer s.attachMu.RUnlock()
	return s.attach
}

// safeHTML marks a body already escaped by docs/markdown as HTML for the template.
func safeHTML(s string) htmltemplate.HTML { return htmltemplate.HTML(s) } //nolint:gosec // escaped by markdown.HTML

// Mail is one customer or office email of M4. Values are display values for the template; they and the address
// are encrypted in message_log and never logged.
type Mail struct {
	Template    string
	MarinaID    string
	CustomerID  string // set for customer emails: the message is recorded in the customer registry
	UserID      string
	To          string
	Locale      string
	DedupeKey   string // unique per logical email in the marina
	Category    string // transactional (default), service, promotional
	SourceType  string // contract, quote, installment, renewal_proposal, signature, deadline, communication, ...
	SourceID    string
	Values      map[string]string
	BodyHTML    string // pre-rendered escaped HTML (communications)
	BodyText    string
	Attachments []AttachmentRef
	Headers     map[string]string // List-Unsubscribe and List-Unsubscribe-Post of promotional emails
}

// QueueMailTx queues m in tx. A dedupe key already used in the marina is not an error: the existing message id
// is returned and nothing is written (queued reports whether a new message was written).
func (s *Service) QueueMailTx(ctx context.Context, tx bun.IDB, m Mail) (messageID string, queued bool, err error) {
	if m.To == "" {
		return "", false, errors.New("notify: recipient is required")
	}
	if m.DedupeKey == "" {
		return "", false, errors.New("notify: dedupe key is required")
	}
	if !knownTemplate(m.Template) {
		return "", false, fmt.Errorf("notify: unknown template %q", m.Template)
	}
	if m.Category == "" {
		m.Category = CategoryTransactional
	}
	id := store.NewID()
	recipientEnc, err := s.keys.EncryptString(m.To, "notify.recipient:"+id)
	if err != nil {
		return "", false, fmt.Errorf("notify: encrypt recipient: %w", err)
	}
	data := RenderData{
		Values: m.Values, BodyText: m.BodyText, Attachments: m.Attachments, Headers: m.Headers,
		MarinaName: m.Values[ValueMarinaName],
	}
	data.BodyHTML = safeHTML(m.BodyHTML)
	payload, err := json.Marshal(data)
	if err != nil {
		return "", false, err
	}
	payloadEnc, err := s.keys.Encrypt(payload, []byte("notify.payload:"+id))
	if err != nil {
		return "", false, fmt.Errorf("notify: encrypt payload: %w", err)
	}
	now := store.UTC(s.clock.Now())
	row := &store.Message{
		ID: id, MarinaID: m.MarinaID, UserID: m.UserID, Channel: "email", Template: m.Template,
		Locale: normalizeLocale(m.Locale), RecipientHash: s.RecipientHash(m.To), RecipientEnc: recipientEnc,
		PayloadEnc: payloadEnc, DedupeKey: m.DedupeKey, Status: store.MessageQueued, NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	ok, err := store.InsertMessage(ctx, tx, row)
	if err != nil {
		return "", false, fmt.Errorf("notify: queue %s: %w", m.Template, err)
	}
	if !ok {
		existing, err := store.MessageIDByDedupe(ctx, tx, m.MarinaID, m.DedupeKey)
		if err != nil {
			return "", false, fmt.Errorf("notify: dedupe %s: %w", m.Template, err)
		}
		return existing, false, nil
	}
	if m.CustomerID != "" && m.MarinaID != "" {
		cm := &store.CustomerMessage{
			ID: store.NewID(), MarinaID: m.MarinaID, CustomerID: m.CustomerID, MessageID: id, Category: m.Category,
			SourceType: m.SourceType, SourceID: m.SourceID, Template: m.Template, CreatedAt: now,
		}
		if err := store.InsertCustomerMessage(ctx, tx, cm); err != nil {
			return "", false, fmt.Errorf("notify: customer registry: %w", err)
		}
	}
	store.AfterCommit(ctx, s.kick)
	return id, true, nil
}

func knownTemplate(name string) bool {
	_, ok := templates["it/"+name]
	return ok
}

// loadAttachments loads the attachments of a payload (total ≤ MaxAttachmentBytes).
func (s *Service) loadAttachments(ctx context.Context, marinaID string, refs []AttachmentRef) ([]Attachment, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	l := s.loader()
	if l == nil {
		return nil, errors.New("notify: no attachment loader")
	}
	var out []Attachment
	total := 0
	for _, r := range refs {
		a, err := l.LoadAttachment(ctx, marinaID, r)
		if err != nil {
			return nil, fmt.Errorf("notify: attachment: %w", err)
		}
		total += len(a.Content)
		if total > MaxAttachmentBytes {
			return nil, ErrAttachmentsTooLarge
		}
		out = append(out, *a)
	}
	return out, nil
}

// MessageToken is the hash used as dedupe key component for a token (never the token itself).
func MessageToken(token string) string { return crypto.SHA256Hex(token)[:16] }
